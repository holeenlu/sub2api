package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	openAIWSResponseAccountCachePrefix = "openai:response:"
	openAIWSStateStoreCleanupInterval  = time.Minute
	openAIWSStateStoreCleanupMaxPerMap = 512
	openAIWSStateStoreMaxEntriesPerMap = 65536
	CacheTimeout                       = 3 * time.Second
)

var ErrAccountBindingNotFound = errors.New("sticky session not found")

// AccountCache stores routing affinity, not an authorization grant. Shared
// cache errors retain the existing routing miss semantics.
type AccountCache interface {
	SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error
	GetSessionAccountID(context.Context, int64, string) (int64, error)
	DeleteSessionAccountID(context.Context, int64, string) error
}

// HTTPResponseOwnerCache is authoritative whenever a shared AccountCache is
// configured. A missing capability or failed read never grants from local data.
type HTTPResponseOwnerCache interface {
	SetHTTPResponseOwner(context.Context, int64, string, int64, int64, time.Duration) error
	GetHTTPResponseOwner(context.Context, int64, string) (int64, int64, bool, error)
}

type openAIWSAccountBinding struct {
	accountID int64
	expiresAt time.Time
}

type openAIHTTPResponseOwnerBinding struct {
	userID    int64
	apiKeyID  int64
	expiresAt time.Time
}

// ResponseMode is a constraint recorded by the producer, never inferred from
// an account's current switches. It does not grant permission to continue.
type ResponseMode int64

const (
	ResponseModeUnknown ResponseMode = iota
	ResponseModeNative
	ResponseModeBPS
	ResponseModeCloud
)

type responseModeBinding struct {
	mode      ResponseMode
	expiresAt time.Time
}

// ResponseScope describes the producer, never a later request's store flag.
// Unknown preserves upstream resolution for external persisted response IDs.
type ResponseScope uint8

const (
	ResponseScopeUnknown ResponseScope = iota
	ResponseScopePersisted
	ResponseScopeConnectionLocal
)

type openAIWSConnBinding struct {
	connID    string
	scope     ResponseScope
	expiresAt time.Time
}

type openAIWSTurnStateBinding struct {
	turnState string
	expiresAt time.Time
}

type openAIWSSessionConnBinding struct {
	connID    string
	expiresAt time.Time
}

// Bindings separates routing affinity, authoritative HTTP ownership and local
// connection hints. Neither a response ID nor a connection hint authorizes a
// continuation; consumers still validate principal, resource and grant identity.
// - response_id -> account_id 用于续链路由
// - response_id -> conn_id 用于连接内上下文复用
//
// response_id -> account_id 优先走 GatewayCache（Redis），同时维护本地热缓存。
// response_id -> conn_id 仅在本进程内有效。
type Bindings interface {
	BindResponseAccount(ctx context.Context, groupID int64, responseID string, accountID int64, ttl time.Duration) error
	GetResponseAccount(ctx context.Context, groupID int64, responseID string) (int64, error)
	DeleteResponseAccount(ctx context.Context, groupID int64, responseID string) error
	BindHTTPResponseOwner(ctx context.Context, groupID int64, responseID string, userID, apiKeyID int64, ttl time.Duration) error
	GetHTTPResponseOwner(ctx context.Context, groupID int64, responseID string) (userID, apiKeyID int64, found bool, err error)
	BindResponseMode(context.Context, int64, string, ResponseMode, time.Duration) error
	GetResponseMode(context.Context, int64, string) (ResponseMode, error)

	BindResponseConn(responseID, connID string, ttl time.Duration, scope ResponseScope)
	GetResponseConn(responseID string) (string, bool)
	GetResponseContinuation(responseID string) (string, ResponseScope, bool)
	DeleteResponseConn(responseID string, expectedConnID ...string)

	BindSessionTurnState(groupID int64, sessionHash, turnState string, ttl time.Duration)
	GetSessionTurnState(groupID int64, sessionHash string) (string, bool)
	DeleteSessionTurnState(groupID int64, sessionHash string, expectedTurnState ...string)

	BindSessionConn(groupID int64, sessionHash, connID string, ttl time.Duration)
	GetSessionConn(groupID int64, sessionHash string) (string, bool)
	DeleteSessionConn(groupID int64, sessionHash string, expectedConnID ...string)
}

type StateStore struct {
	cache AccountCache

	responseToAccountMu  sync.RWMutex
	responseToAccount    map[string]openAIWSAccountBinding
	responseOwnerMu      sync.RWMutex
	responseOwners       map[string]openAIHTTPResponseOwnerBinding
	responseModeMu       sync.RWMutex
	responseModes        map[string]responseModeBinding
	responseToConnMu     sync.RWMutex
	responseToConn       map[string]openAIWSConnBinding
	sessionToTurnStateMu sync.RWMutex
	sessionToTurnState   map[string]openAIWSTurnStateBinding
	sessionToConnMu      sync.RWMutex
	sessionToConn        map[string]openAIWSSessionConnBinding

	lastCleanupUnixNano atomic.Int64
}

// NewStateStore constructs bounded process-local indexes. It starts no workers.
// A nil shared cache explicitly selects standalone local ownership. Redis wire
// keys, TTL defaults and optional shared-owner behavior remain unchanged.
func NewStateStore(cache AccountCache) *StateStore {
	store := &StateStore{
		cache:              cache,
		responseToAccount:  make(map[string]openAIWSAccountBinding, 256),
		responseOwners:     make(map[string]openAIHTTPResponseOwnerBinding, 256),
		responseModes:      make(map[string]responseModeBinding, 256),
		responseToConn:     make(map[string]openAIWSConnBinding, 256),
		sessionToTurnState: make(map[string]openAIWSTurnStateBinding, 256),
		sessionToConn:      make(map[string]openAIWSSessionConnBinding, 256),
	}
	store.lastCleanupUnixNano.Store(time.Now().UnixNano())
	return store
}

func (s *StateStore) BindHTTPResponseOwner(ctx context.Context, groupID int64, responseID string, userID, apiKeyID int64, ttl time.Duration) error {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" || userID <= 0 || apiKeyID <= 0 {
		return nil
	}
	if s.cache != nil {
		shared, ok := s.cache.(HTTPResponseOwnerCache)
		if !ok {
			return errors.New("HTTP response owner cache is unavailable")
		}
		cacheCtx, cancel := withCacheTimeout(ctx)
		defer cancel()
		return shared.SetHTTPResponseOwner(cacheCtx, groupID, id, userID, apiKeyID, normalizeOpenAIWSTTL(ttl))
	}
	ttl = normalizeOpenAIWSTTL(ttl)
	s.maybeCleanup()

	mapKey := openAIWSResponseAccountMapKey(groupID, id)
	s.responseOwnerMu.Lock()
	ensureBindingCapacity(s.responseOwners, mapKey, openAIWSStateStoreMaxEntriesPerMap)
	s.responseOwners[mapKey] = openAIHTTPResponseOwnerBinding{
		userID: userID, apiKeyID: apiKeyID, expiresAt: time.Now().Add(ttl),
	}
	s.responseOwnerMu.Unlock()

	return nil
}

func (s *StateStore) GetHTTPResponseOwner(ctx context.Context, groupID int64, responseID string) (int64, int64, bool, error) {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" {
		return 0, 0, false, nil
	}
	if s.cache != nil {
		shared, ok := s.cache.(HTTPResponseOwnerCache)
		if !ok {
			return 0, 0, false, errors.New("HTTP response owner cache is unavailable")
		}
		cacheCtx, cancel := withCacheTimeout(ctx)
		defer cancel()
		return shared.GetHTTPResponseOwner(cacheCtx, groupID, id)
	}
	s.maybeCleanup()

	now := time.Now()
	mapKey := openAIWSResponseAccountMapKey(groupID, id)
	s.responseOwnerMu.RLock()
	if binding, ok := s.responseOwners[mapKey]; ok && now.Before(binding.expiresAt) {
		s.responseOwnerMu.RUnlock()
		return binding.userID, binding.apiKeyID, true, nil
	}
	s.responseOwnerMu.RUnlock()

	return 0, 0, false, nil
}

func (s *StateStore) BindResponseAccount(ctx context.Context, groupID int64, responseID string, accountID int64, ttl time.Duration) error {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" || accountID <= 0 {
		return nil
	}
	ttl = normalizeOpenAIWSTTL(ttl)
	s.maybeCleanup()

	expiresAt := time.Now().Add(ttl)
	mapKey := openAIWSResponseAccountMapKey(groupID, id)
	s.responseToAccountMu.Lock()
	ensureBindingCapacity(s.responseToAccount, mapKey, openAIWSStateStoreMaxEntriesPerMap)
	s.responseToAccount[mapKey] = openAIWSAccountBinding{accountID: accountID, expiresAt: expiresAt}
	s.responseToAccountMu.Unlock()

	if s.cache == nil {
		return nil
	}
	cacheKey := openAIWSResponseAccountCacheKey(id)
	cacheCtx, cancel := withCacheTimeout(ctx)
	defer cancel()
	return s.cache.SetSessionAccountID(cacheCtx, groupID, cacheKey, accountID, ttl)
}

func cleanupExpiredHTTPResponseOwnerBindings(bindings map[string]openAIHTTPResponseOwnerBinding, now time.Time, maxScan int) {
	if len(bindings) == 0 || maxScan <= 0 {
		return
	}
	scanned := 0
	for key, binding := range bindings {
		if !now.Before(binding.expiresAt) {
			delete(bindings, key)
		}
		scanned++
		if scanned >= maxScan {
			break
		}
	}
}

func (s *StateStore) GetResponseAccount(ctx context.Context, groupID int64, responseID string) (int64, error) {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" {
		return 0, nil
	}
	s.maybeCleanup()

	now := time.Now()
	mapKey := openAIWSResponseAccountMapKey(groupID, id)
	s.responseToAccountMu.RLock()
	if binding, ok := s.responseToAccount[mapKey]; ok {
		if now.Before(binding.expiresAt) {
			accountID := binding.accountID
			s.responseToAccountMu.RUnlock()
			return accountID, nil
		}
	}
	s.responseToAccountMu.RUnlock()

	if s.cache == nil {
		return 0, nil
	}

	cacheKey := openAIWSResponseAccountCacheKey(id)
	cacheCtx, cancel := withCacheTimeout(ctx)
	defer cancel()
	accountID, err := s.cache.GetSessionAccountID(cacheCtx, groupID, cacheKey)
	if err != nil || accountID <= 0 {
		// 缓存读取失败不阻断主流程，按未命中降级。
		return 0, nil
	}
	return accountID, nil
}

func (s *StateStore) DeleteResponseAccount(ctx context.Context, groupID int64, responseID string) error {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" {
		return nil
	}
	s.responseToAccountMu.Lock()
	delete(s.responseToAccount, openAIWSResponseAccountMapKey(groupID, id))
	s.responseToAccountMu.Unlock()

	if s.cache == nil {
		return nil
	}
	cacheCtx, cancel := withCacheTimeout(ctx)
	defer cancel()
	return s.cache.DeleteSessionAccountID(cacheCtx, groupID, openAIWSResponseAccountCacheKey(id))
}

func (s *StateStore) BindResponseConn(responseID, connID string, ttl time.Duration, scope ResponseScope) {
	id := normalizeOpenAIWSResponseID(responseID)
	conn := strings.TrimSpace(connID)
	if id == "" || (conn == "" && scope == ResponseScopeUnknown) || scope > ResponseScopeConnectionLocal {
		return
	}
	ttl = normalizeOpenAIWSTTL(ttl)
	s.maybeCleanup()

	s.responseToConnMu.Lock()
	ensureBindingCapacity(s.responseToConn, id, openAIWSStateStoreMaxEntriesPerMap)
	s.responseToConn[id] = openAIWSConnBinding{
		connID:    conn,
		scope:     scope,
		expiresAt: time.Now().Add(ttl),
	}
	s.responseToConnMu.Unlock()
}

func (s *StateStore) GetResponseConn(responseID string) (string, bool) {
	connID, _, found := s.GetResponseContinuation(responseID)
	return connID, found && connID != ""
}

func (s *StateStore) GetResponseContinuation(responseID string) (string, ResponseScope, bool) {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" {
		return "", ResponseScopeUnknown, false
	}
	s.maybeCleanup()

	now := time.Now()
	s.responseToConnMu.RLock()
	binding, ok := s.responseToConn[id]
	s.responseToConnMu.RUnlock()
	if !ok || !now.Before(binding.expiresAt) {
		return "", ResponseScopeUnknown, false
	}
	return binding.connID, binding.scope, true
}

func (s *StateStore) DeleteResponseConn(responseID string, expectedConnID ...string) {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" {
		return
	}
	s.responseToConnMu.Lock()
	if binding, found := s.responseToConn[id]; found && (len(expectedConnID) == 0 || binding.connID == expectedConnID[0]) {
		if binding.scope == ResponseScopeUnknown {
			delete(s.responseToConn, id)
		} else {
			// Socket loss does not make a known local response portable.
			// Retain its original expiry; cleanup is not a lease renewal.
			binding.connID = ""
			s.responseToConn[id] = binding
		}
	}
	s.responseToConnMu.Unlock()
}

func (s *StateStore) BindSessionTurnState(groupID int64, sessionHash, turnState string, ttl time.Duration) {
	key := openAIWSSessionTurnStateKey(groupID, sessionHash)
	state := strings.TrimSpace(turnState)
	if key == "" || state == "" {
		return
	}
	ttl = normalizeOpenAIWSTTL(ttl)
	s.maybeCleanup()

	s.sessionToTurnStateMu.Lock()
	ensureBindingCapacity(s.sessionToTurnState, key, openAIWSStateStoreMaxEntriesPerMap)
	s.sessionToTurnState[key] = openAIWSTurnStateBinding{
		turnState: state,
		expiresAt: time.Now().Add(ttl),
	}
	s.sessionToTurnStateMu.Unlock()
}

func (s *StateStore) GetSessionTurnState(groupID int64, sessionHash string) (string, bool) {
	key := openAIWSSessionTurnStateKey(groupID, sessionHash)
	if key == "" {
		return "", false
	}
	s.maybeCleanup()

	now := time.Now()
	s.sessionToTurnStateMu.RLock()
	binding, ok := s.sessionToTurnState[key]
	s.sessionToTurnStateMu.RUnlock()
	if !ok || !now.Before(binding.expiresAt) || strings.TrimSpace(binding.turnState) == "" {
		return "", false
	}
	return binding.turnState, true
}

func (s *StateStore) DeleteSessionTurnState(groupID int64, sessionHash string, expectedTurnState ...string) {
	key := openAIWSSessionTurnStateKey(groupID, sessionHash)
	if key == "" {
		return
	}
	s.sessionToTurnStateMu.Lock()
	if len(expectedTurnState) == 0 || s.sessionToTurnState[key].turnState == expectedTurnState[0] {
		delete(s.sessionToTurnState, key)
	}
	s.sessionToTurnStateMu.Unlock()
}

func (s *StateStore) BindSessionConn(groupID int64, sessionHash, connID string, ttl time.Duration) {
	key := openAIWSSessionTurnStateKey(groupID, sessionHash)
	conn := strings.TrimSpace(connID)
	if key == "" || conn == "" {
		return
	}
	ttl = normalizeOpenAIWSTTL(ttl)
	s.maybeCleanup()

	s.sessionToConnMu.Lock()
	ensureBindingCapacity(s.sessionToConn, key, openAIWSStateStoreMaxEntriesPerMap)
	s.sessionToConn[key] = openAIWSSessionConnBinding{
		connID:    conn,
		expiresAt: time.Now().Add(ttl),
	}
	s.sessionToConnMu.Unlock()
}

func (s *StateStore) GetSessionConn(groupID int64, sessionHash string) (string, bool) {
	key := openAIWSSessionTurnStateKey(groupID, sessionHash)
	if key == "" {
		return "", false
	}
	s.maybeCleanup()

	now := time.Now()
	s.sessionToConnMu.RLock()
	binding, ok := s.sessionToConn[key]
	s.sessionToConnMu.RUnlock()
	if !ok || !now.Before(binding.expiresAt) || strings.TrimSpace(binding.connID) == "" {
		return "", false
	}
	return binding.connID, true
}

func (s *StateStore) DeleteSessionConn(groupID int64, sessionHash string, expectedConnID ...string) {
	key := openAIWSSessionTurnStateKey(groupID, sessionHash)
	if key == "" {
		return
	}
	s.sessionToConnMu.Lock()
	if len(expectedConnID) == 0 || s.sessionToConn[key].connID == expectedConnID[0] {
		delete(s.sessionToConn, key)
	}
	s.sessionToConnMu.Unlock()
}

func (s *StateStore) maybeCleanup() {
	if s == nil {
		return
	}
	now := time.Now()
	last := time.Unix(0, s.lastCleanupUnixNano.Load())
	if now.Sub(last) < openAIWSStateStoreCleanupInterval {
		return
	}
	if !s.lastCleanupUnixNano.CompareAndSwap(last.UnixNano(), now.UnixNano()) {
		return
	}

	// 增量限额清理，避免高规模下一次性全量扫描导致长时间阻塞。
	s.responseToAccountMu.Lock()
	cleanupExpiredAccountBindings(s.responseToAccount, now, openAIWSStateStoreCleanupMaxPerMap)
	s.responseToAccountMu.Unlock()

	s.responseOwnerMu.Lock()
	cleanupExpiredHTTPResponseOwnerBindings(s.responseOwners, now, openAIWSStateStoreCleanupMaxPerMap)
	s.responseOwnerMu.Unlock()

	s.responseModeMu.Lock()
	scanned := 0
	for key, binding := range s.responseModes {
		if !now.Before(binding.expiresAt) {
			delete(s.responseModes, key)
		}
		scanned++
		if scanned >= openAIWSStateStoreCleanupMaxPerMap {
			break
		}
	}
	s.responseModeMu.Unlock()

	s.responseToConnMu.Lock()
	cleanupExpiredConnBindings(s.responseToConn, now, openAIWSStateStoreCleanupMaxPerMap)
	s.responseToConnMu.Unlock()

	s.sessionToTurnStateMu.Lock()
	cleanupExpiredTurnStateBindings(s.sessionToTurnState, now, openAIWSStateStoreCleanupMaxPerMap)
	s.sessionToTurnStateMu.Unlock()

	s.sessionToConnMu.Lock()
	cleanupExpiredSessionConnBindings(s.sessionToConn, now, openAIWSStateStoreCleanupMaxPerMap)
	s.sessionToConnMu.Unlock()

}

func cleanupExpiredAccountBindings(bindings map[string]openAIWSAccountBinding, now time.Time, maxScan int) {
	if len(bindings) == 0 || maxScan <= 0 {
		return
	}
	scanned := 0
	for key, binding := range bindings {
		if !now.Before(binding.expiresAt) {
			delete(bindings, key)
		}
		scanned++
		if scanned >= maxScan {
			break
		}
	}
}

func cleanupExpiredConnBindings(bindings map[string]openAIWSConnBinding, now time.Time, maxScan int) {
	if len(bindings) == 0 || maxScan <= 0 {
		return
	}
	scanned := 0
	for key, binding := range bindings {
		if !now.Before(binding.expiresAt) {
			delete(bindings, key)
		}
		scanned++
		if scanned >= maxScan {
			break
		}
	}
}

func cleanupExpiredTurnStateBindings(bindings map[string]openAIWSTurnStateBinding, now time.Time, maxScan int) {
	if len(bindings) == 0 || maxScan <= 0 {
		return
	}
	scanned := 0
	for key, binding := range bindings {
		if !now.Before(binding.expiresAt) {
			delete(bindings, key)
		}
		scanned++
		if scanned >= maxScan {
			break
		}
	}
}

func cleanupExpiredSessionConnBindings(bindings map[string]openAIWSSessionConnBinding, now time.Time, maxScan int) {
	if len(bindings) == 0 || maxScan <= 0 {
		return
	}
	scanned := 0
	for key, binding := range bindings {
		if !now.Before(binding.expiresAt) {
			delete(bindings, key)
		}
		scanned++
		if scanned >= maxScan {
			break
		}
	}
}

func ensureBindingCapacity[T any](bindings map[string]T, incomingKey string, maxEntries int) {
	if len(bindings) < maxEntries || maxEntries <= 0 {
		return
	}
	if _, exists := bindings[incomingKey]; exists {
		return
	}
	// 固定上限保护：淘汰任意一项，优先保证内存有界。
	for key := range bindings {
		delete(bindings, key)
		return
	}
}

func normalizeOpenAIWSResponseID(responseID string) string {
	return strings.TrimSpace(responseID)
}

func openAIWSResponseAccountCacheKey(responseID string) string {
	sum := sha256.Sum256([]byte(responseID))
	return openAIWSResponseAccountCachePrefix + hex.EncodeToString(sum[:])
}

// openAIWSResponseAccountMapKey 本地热缓存按分组隔离的 key，与 Redis 层保持一致，避免跨组命中。
func openAIWSResponseAccountMapKey(groupID int64, responseID string) string {
	return fmt.Sprintf("%d:%s", groupID, responseID)
}

func normalizeOpenAIWSTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return time.Hour
	}
	return ttl
}

func openAIWSSessionTurnStateKey(groupID int64, sessionHash string) string {
	hash := strings.TrimSpace(sessionHash)
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("%d:%s", groupID, hash)
}

func withCacheTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, CacheTimeout)
}

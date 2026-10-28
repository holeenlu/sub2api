package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// ConcurrencyCache 定义并发控制的缓存接口
// 使用有序集合存储槽位，按时间戳清理过期条目
type ConcurrencyCache interface {
	// 账号槽位管理
	// 键格式: concurrency:account:{accountID}（有序集合，成员为 requestID）
	AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error)
	ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error
	GetAccountConcurrency(ctx context.Context, accountID int64) (int, error)
	GetAccountConcurrencyBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error)

	// 账号等待队列（账号级）
	IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error)
	DecrementAccountWaitCount(ctx context.Context, accountID int64) error
	GetAccountWaitingCount(ctx context.Context, accountID int64) (int, error)

	// 用户槽位管理
	// 键格式: concurrency:user:{userID}（有序集合，成员为 requestID）
	AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error)
	ReleaseUserSlot(ctx context.Context, userID int64, requestID string) error
	GetUserConcurrency(ctx context.Context, userID int64) (int, error)

	// 等待队列计数（每次入队都会刷新 TTL，避免长时间排队时计数提前过期）
	IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error)
	DecrementWaitCount(ctx context.Context, userID int64) error

	// 批量负载查询（只读）
	GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error)
	GetUsersLoadBatch(ctx context.Context, users []UserWithConcurrency) (map[int64]*UserLoadInfo, error)

	// 清理过期槽位（后台任务）
	CleanupExpiredAccountSlots(ctx context.Context, accountID int64) error
	CleanupExpiredAccountSlotKeys(ctx context.Context) error

	// 启动时清理旧进程遗留槽位与等待计数
	CleanupStaleProcessSlots(ctx context.Context, activeRequestPrefix string) error
}

type APIKeyConcurrencyCache interface {
	TrackAPIKeySlot(ctx context.Context, apiKeyID int64, requestID string) error
	ReleaseAPIKeySlot(ctx context.Context, apiKeyID int64, requestID string) error
	GetAPIKeyConcurrencyBatch(ctx context.Context, apiKeyIDs []int64) (map[int64]int, error)
}

// OpenAIWSIngressLease preserves the return type used by the existing ingress
// helper during its owner-managed migration. State and renewal live only in
// routing; delete this alias when handler/gateway_helper accepts routing directly.
type OpenAIWSIngressLease = routing.IngressLease

// Keep the protocol boundary's existing error identity during ingress migration.
var ErrOpenAIWSIngressLeaseLost = routing.ErrIngressLeaseLost

var (
	requestIDPrefix  = initRequestIDPrefix()
	requestIDCounter atomic.Uint64
)

func initRequestIDPrefix() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err == nil {
		return "r" + strconv.FormatUint(binary.BigEndian.Uint64(b), 36)
	}
	fallback := uint64(time.Now().UnixNano()) ^ (uint64(os.Getpid()) << 16)
	return "r" + strconv.FormatUint(fallback, 36)
}

func RequestIDPrefix() string {
	return requestIDPrefix
}

func generateRequestID() string {
	seq := requestIDCounter.Add(1)
	return requestIDPrefix + "-" + strconv.FormatUint(seq, 36)
}

func (s *ConcurrencyService) CleanupStaleProcessSlots(ctx context.Context) error {
	if s == nil || s.cache == nil {
		return nil
	}
	return s.cache.CleanupStaleProcessSlots(ctx, RequestIDPrefix())
}

const (
	// 默认等待队列额外槽位
	defaultExtraWaitSlots = 20

	defaultAccountLoadBatchCacheTTL = 200 * time.Millisecond
	accountLoadBatchFetchTimeout    = 3 * time.Second
	maxAccountLoadCacheEntries      = 32768
	apiKeyConcurrencyFetchTimeout   = 3 * time.Second
	apiKeySlotTrackTimeout          = 2 * time.Second
)

// ConcurrencyService 管理账号和用户的并发限制。
type ConcurrencyService struct {
	cache         ConcurrencyCache
	cleanupMu     sync.Mutex
	cleanupWorker *SlotCleanupWorker

	accountLoadCacheTTL atomic.Int64
	accountLoadCacheMu  sync.RWMutex
	accountLoadCache    map[int64]cachedAccountLoad
	accountLoadGroup    singleflight.Group
}

type cachedAccountLoad struct {
	load      AccountLoadInfo
	expiresAt time.Time
}

// NewConcurrencyService 创建并发控制服务。
func NewConcurrencyService(cache ConcurrencyCache) *ConcurrencyService {
	svc := &ConcurrencyService{
		cache:            cache,
		accountLoadCache: make(map[int64]cachedAccountLoad),
	}
	svc.SetAccountLoadBatchCacheTTL(defaultAccountLoadBatchCacheTTL)
	return svc
}

// AcquireOpenAIWSIngressLease atomically reserves one live ingress connection
// for an API key. A non-positive limit explicitly disables this protection.
func (s *ConcurrencyService) AcquireOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, maxConnections int) (*OpenAIWSIngressLease, bool, error) {
	if maxConnections <= 0 {
		return nil, true, nil
	}
	if s == nil || s.cache == nil || apiKeyID <= 0 {
		return nil, false, errors.New("openai websocket ingress lease cache is unavailable")
	}
	cache, ok := s.cache.(routing.IngressLeaseStore)
	if !ok {
		return nil, false, errors.New("openai websocket ingress lease cache is unsupported")
	}
	return routing.AcquireIngressLease(ctx, cache, apiKeyID, maxConnections, generateRequestID(), func(err error, lost bool) {
		if lost {
			logger.L().Error("openai_ws_ingress_lease_lost", zap.Int64("api_key_id", apiKeyID), zap.Error(err))
		} else {
			logger.L().Warn("openai_ws_ingress_lease_unconfirmed", zap.Int64("api_key_id", apiKeyID), zap.Error(err))
		}
	})
}

// SetAccountLoadBatchCacheTTL 设置账号负载批量读取的极短 TTL 缓存；非正数表示禁用缓存。
func (s *ConcurrencyService) SetAccountLoadBatchCacheTTL(ttl time.Duration) {
	if s == nil {
		return
	}
	s.accountLoadCacheTTL.Store(int64(ttl))
	if ttl <= 0 {
		s.accountLoadCacheMu.Lock()
		s.accountLoadCache = make(map[int64]cachedAccountLoad)
		s.accountLoadCacheMu.Unlock()
	}
}

// AcquireResult is shared with existing ingress consumers while their imports
// migrate. routing owns acquisition and idempotent release; no parallel state.
type AcquireResult = routing.AcquireResult

type AccountWithConcurrency struct {
	ID             int64
	MaxConcurrency int
}

type UserWithConcurrency struct {
	ID             int64
	MaxConcurrency int
}

type AccountLoadInfo struct {
	AccountID          int64
	CurrentConcurrency int
	WaitingCount       int
	LoadRate           int // 0-100+ (percent)
}

type UserLoadInfo struct {
	UserID             int64
	CurrentConcurrency int
	WaitingCount       int
	LoadRate           int // 0-100+ (percent)
}

// AcquireAccountSlot attempts to acquire a concurrency slot for an account.
// It does not wait; the ingress owns polling and its existing deadline.
// Returns a release function that MUST be called when the request completes.
func (s *ConcurrencyService) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int) (*AcquireResult, error) {
	requestID := generateRequestID()
	result, err := routing.AcquireCapacity(ctx, s.cache, routing.CapacityRequest{ID: accountID, Token: requestID, Limit: maxConcurrency}, func(err error) {
		logger.LegacyPrintf("service.concurrency", "Warning: failed to release account slot for %d (req=%s): %v", accountID, requestID, err)
	})
	if err == nil && result.Acquired && maxConcurrency > 0 {
		s.observeWaitAdmission(ctx, "account", accountID)
	}
	if err == nil && result.Acquired && ctx.Err() != nil {
		// Diagnostics run after routing transferred the lease. Cancellation
		// during that Redis call must return ownership instead of a stale success.
		result.ReleaseFunc()
		return nil, context.Cause(ctx)
	}
	return result, err
}

// AcquireUserSlot uses the same token owner, with the independent user namespace.
func (s *ConcurrencyService) AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int) (*AcquireResult, error) {
	requestID := generateRequestID()
	result, err := routing.AcquireCapacity(ctx, s.cache, routing.CapacityRequest{ID: userID, Token: requestID, Limit: maxConcurrency, User: true}, func(err error) {
		logger.LegacyPrintf("service.concurrency", "Warning: failed to release user slot for %d (req=%s): %v", userID, requestID, err)
	})
	if err == nil && result.Acquired {
		if maxConcurrency > 0 {
			s.observeWaitAdmission(ctx, "user", userID)
		}
		if ctx.Err() != nil {
			result.ReleaseFunc()
			return nil, context.Cause(ctx)
		}
		openAIRequestTimingFromContext(ctx).mark("user_slot", time.Now())
	}
	return result, err
}

// TrackAPIKeySlot records one active request slot for an API key without
// applying key-level concurrency limits. It is fail-open: Redis errors are
// logged and return a no-op release function.
func (s *ConcurrencyService) TrackAPIKeySlot(ctx context.Context, apiKeyID int64) func() {
	if s == nil || s.cache == nil || apiKeyID <= 0 {
		return func() {}
	}
	cache, ok := s.cache.(APIKeyConcurrencyCache)
	if !ok {
		return func() {}
	}

	requestID := generateRequestID()
	baseCtx := context.Background()
	if ctx != nil {
		baseCtx = context.WithoutCancel(ctx)
	}
	trackCtx, cancel := context.WithTimeout(baseCtx, apiKeySlotTrackTimeout)
	err := cache.TrackAPIKeySlot(trackCtx, apiKeyID, requestID)
	cancel()
	if err != nil {
		logger.LegacyPrintf("service.concurrency", "Warning: failed to track api key slot for %d (req=%s): %v", apiKeyID, requestID, err)
		return func() {}
	}

	return func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cache.ReleaseAPIKeySlot(bgCtx, apiKeyID, requestID); err != nil {
			logger.LegacyPrintf("service.concurrency", "Warning: failed to release api key slot for %d (req=%s): %v", apiKeyID, requestID, err)
		}
	}
}

// GetAPIKeyConcurrencyBatch gets real-time active request counts for API keys.
// Stats are best-effort: missing Redis support or Redis errors return zeroes.
func (s *ConcurrencyService) GetAPIKeyConcurrencyBatch(ctx context.Context, apiKeyIDs []int64) (map[int64]int, error) {
	result := zeroAPIKeyConcurrencyMap(apiKeyIDs)
	if len(apiKeyIDs) == 0 {
		return result, nil
	}
	if s == nil || s.cache == nil {
		return result, nil
	}
	cache, ok := s.cache.(APIKeyConcurrencyCache)
	if !ok {
		return result, nil
	}

	redisCtx, cancel := context.WithTimeout(context.Background(), apiKeyConcurrencyFetchTimeout)
	defer cancel()

	counts, err := cache.GetAPIKeyConcurrencyBatch(redisCtx, apiKeyIDs)
	if err != nil {
		logger.LegacyPrintf("service.concurrency", "Warning: get api key concurrency batch failed: %v", err)
		return result, nil
	}
	for _, apiKeyID := range apiKeyIDs {
		result[apiKeyID] = counts[apiKeyID]
	}
	return result, nil
}

func zeroAPIKeyConcurrencyMap(apiKeyIDs []int64) map[int64]int {
	result := make(map[int64]int, len(apiKeyIDs))
	for _, apiKeyID := range apiKeyIDs {
		result[apiKeyID] = 0
	}
	return result
}

// ============================================
// Wait Queue Count Methods
// ============================================

// IncrementWaitCount attempts to increment the wait queue counter for a user.
// Returns true if successful, false if the wait queue is full.
// maxWait should be user.Concurrency + defaultExtraWaitSlots
func (s *ConcurrencyService) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	if s.cache == nil {
		// Redis not available, allow request
		return true, nil
	}

	result, err := s.cache.IncrementWaitCount(ctx, userID, maxWait)
	if err != nil {
		// On error, allow the request to proceed (fail open)
		logger.LegacyPrintf("service.concurrency", "Warning: increment wait count failed for user %d: %v", userID, err)
		return true, nil
	}
	return result, nil
}

// DecrementWaitCount decrements the wait queue counter for a user.
// Should be called when a request completes or exits the wait queue.
func (s *ConcurrencyService) DecrementWaitCount(ctx context.Context, userID int64) {
	if s.cache == nil {
		return
	}

	// Use background context to ensure decrement even if original context is cancelled
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.cache.DecrementWaitCount(bgCtx, userID); err != nil {
		logger.LegacyPrintf("service.concurrency", "Warning: decrement wait count failed for user %d: %v", userID, err)
	}
}

// IncrementAccountWaitCount increments the wait queue counter for an account.
func (s *ConcurrencyService) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	if s.cache == nil {
		return true, nil
	}

	result, err := s.cache.IncrementAccountWaitCount(ctx, accountID, maxWait)
	if err != nil {
		logger.LegacyPrintf("service.concurrency", "Warning: increment wait count failed for account %d: %v", accountID, err)
		return true, nil
	}
	return result, nil
}

// DecrementAccountWaitCount decrements the wait queue counter for an account.
func (s *ConcurrencyService) DecrementAccountWaitCount(ctx context.Context, accountID int64) {
	if s.cache == nil {
		return
	}

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.cache.DecrementAccountWaitCount(bgCtx, accountID); err != nil {
		logger.LegacyPrintf("service.concurrency", "Warning: decrement wait count failed for account %d: %v", accountID, err)
	}
}

// GetAccountWaitingCount gets current wait queue count for an account.
func (s *ConcurrencyService) GetAccountWaitingCount(ctx context.Context, accountID int64) (int, error) {
	if s.cache == nil {
		return 0, nil
	}
	return s.cache.GetAccountWaitingCount(ctx, accountID)
}

// CalculateMaxWait calculates the maximum wait queue size for a user
// maxWait = userConcurrency + defaultExtraWaitSlots
func CalculateMaxWait(userConcurrency int) int {
	if userConcurrency <= 0 {
		userConcurrency = 1
	}
	return userConcurrency + defaultExtraWaitSlots
}

// GetAccountsLoadBatch 批量获取账号负载信息。
func (s *ConcurrencyService) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	return s.getAccountsLoadBatch(ctx, accounts, true)
}

// GetAccountsLoadBatchFresh 绕过极短 TTL 缓存，用于抢槽失败后的实时刷新兜底。
func (s *ConcurrencyService) GetAccountsLoadBatchFresh(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	return s.getAccountsLoadBatch(ctx, accounts, false)
}

func (s *ConcurrencyService) getAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency, allowCache bool) (map[int64]*AccountLoadInfo, error) {
	if len(accounts) == 0 {
		return map[int64]*AccountLoadInfo{}, nil
	}
	if s.cache == nil {
		return map[int64]*AccountLoadInfo{}, nil
	}

	ttl := time.Duration(s.accountLoadCacheTTL.Load())
	if !allowCache || ttl <= 0 {
		return s.fetchAccountsLoadBatch(ctx, accounts)
	}

	cached, missing := s.getCachedAccountLoads(accounts, time.Now())
	if len(missing) == 0 {
		return cached, nil
	}
	pending := s.accountLoadGroup.DoChan(accountLoadBatchCacheKey(missing), func() (any, error) {
		now := time.Now()
		result, unread := s.getCachedAccountLoads(missing, now)
		if len(unread) == 0 {
			return result, nil
		}
		loads, err := s.fetchAccountsLoadBatch(ctx, unread)
		if err != nil {
			return nil, err
		}
		s.storeCachedAccountLoads(loads, now.Add(ttl))
		for id, load := range loads {
			result[id] = load
		}
		return result, nil
	})
	var result singleflight.Result
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result = <-pending:
	}
	if result.Err != nil {
		return nil, result.Err
	}
	loads, _ := result.Val.(map[int64]*AccountLoadInfo)
	for id, load := range loads {
		if load != nil {
			copy := *load
			cached[id] = &copy
		}
	}
	return cached, nil
}

func (s *ConcurrencyService) fetchAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	if s.cache == nil {
		return map[int64]*AccountLoadInfo{}, nil
	}
	baseCtx := context.Background()
	if ctx != nil {
		baseCtx = ctx
	}
	redisCtx, cancel := context.WithTimeout(baseCtx, accountLoadBatchFetchTimeout)
	defer cancel()
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "load", Action: "batch_read", Candidates: len(accounts)})
	return s.cache.GetAccountsLoadBatch(redisCtx, accounts)
}

// Cache observations by account, not by sampled batch membership. Otherwise
// random batches defeat the shared short TTL and duplicate the same account in
// hundreds of cache entries. Capacity is request-owned and applied on read.
func (s *ConcurrencyService) getCachedAccountLoads(accounts []AccountWithConcurrency, now time.Time) (map[int64]*AccountLoadInfo, []AccountWithConcurrency) {
	result := make(map[int64]*AccountLoadInfo, len(accounts))
	missing := make([]AccountWithConcurrency, 0, len(accounts))
	s.accountLoadCacheMu.RLock()
	defer s.accountLoadCacheMu.RUnlock()
	for _, account := range accounts {
		cached, ok := s.accountLoadCache[account.ID]
		if !ok || !now.Before(cached.expiresAt) {
			missing = append(missing, account)
			continue
		}
		copy := cached.load
		copy.LoadRate = 0
		if account.MaxConcurrency > 0 {
			copy.LoadRate = (copy.CurrentConcurrency + copy.WaitingCount) * 100 / account.MaxConcurrency
		}
		result[account.ID] = &copy
	}
	return result, missing
}
func (s *ConcurrencyService) storeCachedAccountLoads(loads map[int64]*AccountLoadInfo, expiresAt time.Time) {
	s.accountLoadCacheMu.Lock()
	defer s.accountLoadCacheMu.Unlock()
	if s.accountLoadCache == nil {
		s.accountLoadCache = make(map[int64]cachedAccountLoad)
	}
	if len(s.accountLoadCache)+len(loads) > maxAccountLoadCacheEntries {
		now := time.Now()
		for id, cached := range s.accountLoadCache {
			if !now.Before(cached.expiresAt) {
				delete(s.accountLoadCache, id)
			}
		}
	}
	for id, load := range loads {
		if load == nil {
			continue
		}
		if _, exists := s.accountLoadCache[id]; !exists && len(s.accountLoadCache) >= maxAccountLoadCacheEntries {
			for evict := range s.accountLoadCache {
				delete(s.accountLoadCache, evict)
				break
			}
		}
		s.accountLoadCache[id] = cachedAccountLoad{load: *load, expiresAt: expiresAt}
	}
}

func accountLoadBatchCacheKey(accounts []AccountWithConcurrency) string {
	hash := sha256.New()
	var buf [16]byte
	for _, account := range accounts {
		binary.LittleEndian.PutUint64(buf[:8], uint64(account.ID))
		binary.LittleEndian.PutUint64(buf[8:], uint64(int64(account.MaxConcurrency)))
		_, _ = hash.Write(buf[:])
	}
	sum := hash.Sum(nil)
	return strconv.Itoa(len(accounts)) + ":" + hex.EncodeToString(sum)
}

// GetUsersLoadBatch returns load info for multiple users.
func (s *ConcurrencyService) GetUsersLoadBatch(ctx context.Context, users []UserWithConcurrency) (map[int64]*UserLoadInfo, error) {
	if s.cache == nil {
		return map[int64]*UserLoadInfo{}, nil
	}
	return s.cache.GetUsersLoadBatch(ctx, users)
}

// CleanupExpiredAccountSlots removes expired slots for one account (background task).
func (s *ConcurrencyService) CleanupExpiredAccountSlots(ctx context.Context, accountID int64) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.CleanupExpiredAccountSlots(ctx, accountID)
}

// SlotCleanupWorker is an explicitly stoppable/joinable worker handle.
// Stop is safe concurrently and waits for the in-flight bounded cleanup call.
type SlotCleanupWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *SlotCleanupWorker) Stop() {
	if w == nil {
		return
	}
	w.cancel()
	<-w.done
}

// StartSlotCleanupWorker retains the current Wire constructor signature. Wire
// must register StopSlotCleanupWorker (or the returned handle's Stop) at shutdown.
// AccountRepository is retained only until MAIN updates that existing caller.
func (s *ConcurrencyService) StartSlotCleanupWorker(_ AccountRepository, interval time.Duration) *SlotCleanupWorker {
	if s == nil || s.cache == nil || interval <= 0 {
		return nil
	}
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if s.cleanupWorker != nil {
		select {
		case <-s.cleanupWorker.done:
		default:
			return s.cleanupWorker
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &SlotCleanupWorker{cancel: cancel, done: make(chan struct{})}
	s.cleanupWorker = worker
	go func() { defer close(worker.done); s.RunSlotCleanup(ctx, interval) }()
	return worker
}

func (s *ConcurrencyService) StopSlotCleanupWorker() {
	if s == nil {
		return
	}
	s.cleanupMu.Lock()
	worker := s.cleanupWorker
	s.cleanupMu.Unlock()
	worker.Stop()
}

// RunSlotCleanup lets host own the worker context directly. It creates no
// goroutines and returns only after the active cleanup has respected cancellation.
func (s *ConcurrencyService) RunSlotCleanup(ctx context.Context, interval time.Duration) {
	if s == nil || s.cache == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		cleanup, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.cache.CleanupExpiredAccountSlotKeys(cleanup)
		cancel()
		if err != nil && ctx.Err() == nil {
			logger.LegacyPrintf("service.concurrency", "Warning: cleanup expired account slots failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// GetAccountConcurrencyBatch gets current concurrency counts for multiple accounts.
// Uses a detached context with timeout to prevent HTTP request cancellation from
// causing the entire batch to fail (which would show all concurrency as 0).
func (s *ConcurrencyService) GetAccountConcurrencyBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error) {
	if len(accountIDs) == 0 {
		return map[int64]int{}, nil
	}
	if s.cache == nil {
		result := make(map[int64]int, len(accountIDs))
		for _, accountID := range accountIDs {
			result[accountID] = 0
		}
		return result, nil
	}

	// Use a detached context so that a cancelled HTTP request doesn't cause
	// the Redis pipeline to fail and return all-zero concurrency counts.
	redisCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return s.cache.GetAccountConcurrencyBatch(redisCtx, accountIDs)
}

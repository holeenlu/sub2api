//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptrace"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const nativeHandlerFixtureGroupID int64 = 3131

// These are separate stored and ingress snapshots; changing a context key does
// not rewrite the authoritative records. Revalidation always calls GetByID.
func newNativeHandlerAPIKey() *service.APIKey {
	groupID := nativeHandlerFixtureGroupID
	return &service.APIKey{ID: 99, UserID: 100, Key: "native-handler-fixture-key", Status: service.StatusActive, GroupID: &groupID,
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1, AllowMessagesDispatch: true},
		User:  &service.User{ID: 100, Role: service.RoleUser, Status: service.StatusActive, Concurrency: 0, Balance: 100}}
}

type nativeHandlerStore struct {
	journal       service.UsageBillingRepository // assigned at construction; real validating fixture port
	mu            sync.RWMutex
	key           *service.APIKey
	accounts      []service.Account
	tokens        map[string]string
	keyReads      atomic.Int32
	userReads     atomic.Int32
	accountReads  atomic.Int32
	beforeKeyRead func(context.Context, int32)
}

func (s *nativeHandlerStore) update(change func(*nativeHandlerStore)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}

type nativeHandlerKeys struct {
	service.APIKeyRepository
	store *nativeHandlerStore
}

func (r *nativeHandlerKeys) GetByID(ctx context.Context, id int64) (*service.APIKey, error) {
	count := r.store.keyReads.Add(1)
	if r.store.beforeKeyRead != nil {
		r.store.beforeKeyRead(ctx, count)
	}
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	if r.store.key == nil || r.store.key.ID != id {
		return nil, service.ErrAPIKeyNotFound
	}
	key := *r.store.key
	if key.User != nil {
		user := *key.User
		user.AllowedGroups = slices.Clone(user.AllowedGroups)
		key.User = &user
	}
	if key.Group != nil {
		group := *key.Group
		key.Group = &group
	}
	if key.GroupID != nil {
		id := *key.GroupID
		key.GroupID = &id
	}
	key.IPWhitelist = slices.Clone(key.IPWhitelist)
	key.IPBlacklist = slices.Clone(key.IPBlacklist)
	return &key, nil
}
func (*nativeHandlerKeys) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

type nativeHandlerUsers struct {
	service.UserRepository
	store *nativeHandlerStore
}

func (r *nativeHandlerUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	r.store.userReads.Add(1)
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	if r.store.key == nil || r.store.key.User == nil || r.store.key.User.ID != id {
		return nil, service.ErrUserNotFound
	}
	user := *r.store.key.User
	user.AllowedGroups = slices.Clone(user.AllowedGroups)
	return &user, nil
}

type nativeHandlerAccounts struct {
	service.AccountRepository
	store *nativeHandlerStore
}

func cloneNativeHandlerAccount(a service.Account) service.Account {
	a.Credentials = maps.Clone(a.Credentials)
	a.Extra = maps.Clone(a.Extra)
	a.GroupIDs = slices.Clone(a.GroupIDs)
	return a
}
func (r *nativeHandlerAccounts) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.store.accountReads.Add(1)
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	for _, a := range r.store.accounts {
		if a.ID == id {
			copy := cloneNativeHandlerAccount(a)
			return &copy, nil
		}
	}
	return nil, service.ErrAccountNotFound
}
func (r *nativeHandlerAccounts) list(platform string) []service.Account {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	var result []service.Account
	for _, a := range r.store.accounts {
		if a.Platform == platform && a.Status == service.StatusActive && a.Schedulable {
			result = append(result, cloneNativeHandlerAccount(a))
		}
	}
	return result
}
func (r *nativeHandlerAccounts) ListSchedulableByPlatform(_ context.Context, p string) ([]service.Account, error) {
	return r.list(p), nil
}
func (r *nativeHandlerAccounts) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, p string) ([]service.Account, error) {
	accounts := r.list(p)
	return slices.DeleteFunc(accounts, func(a service.Account) bool { return !slices.Contains(a.GroupIDs, groupID) }), nil
}
func (r *nativeHandlerAccounts) ListSchedulableUngroupedByPlatform(_ context.Context, p string) ([]service.Account, error) {
	accounts := r.list(p)
	return slices.DeleteFunc(accounts, func(a service.Account) bool { return len(a.GroupIDs) != 0 }), nil
}
func (r *nativeHandlerAccounts) ValidateVendorSupply(ctx context.Context, a *service.Account, _ *int64) error {
	if a == nil {
		return service.ErrAccountNotFound
	}
	current, err := r.GetByID(ctx, a.ID)
	if err != nil {
		return err
	}
	// This fixture provisions only ordinary owned accounts, not supplier grants.
	if current.VendorID != nil || a.VendorID != nil || current.Platform != a.Platform || !current.IsActive() || !current.Schedulable {
		return service.ErrVendorSupplyUnavailable
	}
	return nil
}
func (r *nativeHandlerAccounts) ValidateVendorAuthorizationMode(ctx context.Context, a *service.Account, group *int64, mode string) error {
	if mode != service.VendorOpenAIModeNative {
		return service.ErrVendorSupplyUnavailable
	}
	return r.ValidateVendorSupply(ctx, a, group)
}

type nativeHandlerTokenCache struct {
	service.OpenAITokenCache
	store *nativeHandlerStore
}

func (c *nativeHandlerTokenCache) GetAccessToken(_ context.Context, key string) (string, error) {
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()
	return c.store.tokens[key], nil
}
func (c *nativeHandlerTokenCache) SetAccessToken(_ context.Context, key, token string, _ time.Duration) error {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.tokens[key] = token
	return nil
}
func (c *nativeHandlerTokenCache) DeleteAccessToken(_ context.Context, key string) error {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	delete(c.store.tokens, key)
	return nil
}

// The old deterministic responder is reused only behind the concrete native
// send port. The legacy methods on this transport fail, proving the production
// path reaches Prepare -> Admit -> Open rather than falling back to Do.
type nativeHandlerTransport struct {
	responder                service.HTTPUpstream
	prepared, opened, legacy atomic.Int32
	beforePrepare            func(*http.Request)
	onOpen                   func(*http.Request, native.TransportOptions)
	redirectPolicy           func(*http.Request, []*http.Request, string) error
}

var _ service.NativeHTTPUpstream = (*nativeHandlerTransport)(nil)

func (u *nativeHandlerTransport) PrepareNativeRequest(req *http.Request, maxBody int64) (*http.Request, error) {
	if req == nil || req.URL == nil || maxBody <= 0 {
		return nil, errors.New("invalid handler fixture preparation")
	}
	if req.URL.Hostname() != "chatgpt.com" && req.URL.Hostname() != "api.openai.com" {
		return nil, fmt.Errorf("unconfigured fixture host: %s", req.URL.Hostname())
	}
	u.prepared.Add(1)
	if u.beforePrepare != nil {
		u.beforePrepare(req)
	}
	return req, nil
}
func (u *nativeHandlerTransport) ValidateNativeRedirect(req *http.Request, via []*http.Request, proxy string) error {
	if u.redirectPolicy != nil {
		return u.redirectPolicy(req, via, proxy)
	}
	return errors.New("unexpected redirect: fixture has no redirect policy")
}
func (u *nativeHandlerTransport) RoundTripOnce(req *http.Request, options native.TransportOptions) (*http.Response, error) {
	u.opened.Add(1)
	if u.onOpen != nil {
		u.onOpen(req, options)
	}
	trace := httptrace.ContextClientTrace(req.Context())
	if trace != nil && trace.GetConn != nil {
		trace.GetConn(req.URL.Host)
	}
	if failure, ok := u.responder.(interface{ nextNativeDialFailure() error }); ok {
		if err := failure.nextNativeDialFailure(); err != nil {
			// The scripted connection fails before any connection or write event.
			// The responder records the failed attempt without accepting a request.
			if trace != nil {
				if trace.ConnectStart != nil {
					trace.ConnectStart("tcp", req.URL.Host)
				}
				if trace.ConnectDone != nil {
					trace.ConnectDone("tcp", req.URL.Host, err)
				}
			}
			return u.responder.Do(req, options.ProxyURL, options.AccountID, options.AccountConcurrency)
		}
	}
	if trace != nil && trace.GotConn != nil {
		trace.GotConn(httptrace.GotConnInfo{})
	}
	// GotConn invokes the real permit authority again. A revoked preparation
	// must never reach the scripted server, even though Open already started.
	if err := context.Cause(req.Context()); err != nil {
		return nil, err
	}
	if trace != nil && trace.WroteHeaders != nil {
		trace.WroteHeaders()
	}
	if err := context.Cause(req.Context()); err != nil {
		return nil, err
	}
	if trace != nil && trace.WroteRequest != nil {
		trace.WroteRequest(httptrace.WroteRequestInfo{})
	}
	return u.responder.Do(req, options.ProxyURL, options.AccountID, options.AccountConcurrency)
}
func (u *nativeHandlerTransport) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.legacy.Add(1)
	return nil, errors.New("unexpected legacy HTTP send")
}
func (u *nativeHandlerTransport) DoWithTLS(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
	u.legacy.Add(1)
	return nil, errors.New("unexpected legacy TLS send")
}

func newNativeHTTPHandlerFixture(t *testing.T, accounts []service.Account, responder service.HTTPUpstream) (*OpenAIGatewayHandler, *nativeHandlerStore, *nativeHandlerTransport) {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 32 << 20}}
	return newConfiguredNativeHTTPHandlerFixture(t, accounts, responder, cfg, nil)
}

func newConfiguredNativeHTTPHandlerFixture(t *testing.T, accounts []service.Account, responder service.HTTPUpstream, cfg *config.Config, pricing *service.BillingService, journals ...service.UsageBillingRepository) (*OpenAIGatewayHandler, *nativeHandlerStore, *nativeHandlerTransport) {
	t.Helper()
	store := &nativeHandlerStore{key: newNativeHandlerAPIKey(), accounts: accounts, tokens: map[string]string{}}
	for _, a := range accounts {
		store.tokens[service.OpenAITokenCacheKey(&a)] = a.GetOpenAIAccessToken()
	}
	accountRepo := &nativeHandlerAccounts{store: store}
	userRepo := &nativeHandlerUsers{store: store}
	keyRepo := &nativeHandlerKeys{store: store}
	transport := &nativeHandlerTransport{responder: responder}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(redisClient, 5, 30))
	tokenProvider := service.NewOpenAITokenProvider(accountRepo, &nativeHandlerTokenCache{store: store}, nil)
	// Capture actual production pricing and preserve pre-send intent through
	// the shared validating in-memory journal. This fixture never manufactures
	// a price card, permission, monetary Apply result or PostgreSQL durability.
	if pricing == nil {
		pricing = service.NewBillingService(cfg, nil)
	}
	var journal service.UsageBillingRepository = &wsNativeFixtureJournal{}
	if len(journals) > 0 {
		journal = journals[0] // explicit caller journals (including failures) win
	}
	store.journal = journal
	gateway := service.NewOpenAIGatewayService(accountRepo, nil, journal, userRepo, nil, nil, nil, cfg, nil, concurrency, pricing, nil, nil, transport, &service.DeferredService{}, tokenProvider, nil, nil, nil, nil, nil, nil)
	t.Cleanup(gateway.CloseOpenAIWSPool)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	cleanupHandlerBillingCache(t, billing)
	require.NoError(t, billing.Start(t.Context()))
	keys := service.NewAPIKeyService(keyRepo, userRepo, nil, nil, nil, nil, cfg)
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billing, keys, nil, nil, nil, nil, cfg)
	handler.maxAccountSwitches = 10
	t.Cleanup(func() {
		require.Zero(t, transport.legacy.Load(), "native fixture must never send through the legacy port")
	})
	return handler, store, transport
}

package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type vendorNativeClaudeStub struct {
	ClaudeOAuthClient
	calls atomic.Int32
}

func (c *vendorNativeClaudeStub) ExchangeCodeForToken(context.Context, string, string, string, string, bool) (*oauth.TokenResponse, error) {
	c.calls.Add(1)
	return nil, errors.New("upstream rejected")
}

type vendorNativeOpenAIStub struct {
	OpenAIOAuthClient
	calls atomic.Int32
}

func (c *vendorNativeOpenAIStub) ExchangeCode(context.Context, string, string, string, string, string) (*openai.TokenResponse, error) {
	c.calls.Add(1)
	return nil, errors.New("upstream rejected")
}
func TestVendorNativeOAuthRedisClaimPrecedesUpstreamAcrossInstances(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rdb.Close()
	ctx := context.Background()
	claude := &vendorNativeClaudeStub{}
	oa := &vendorNativeOpenAIStub{}
	a := NewOAuthService(nil, claude, rdb)
	b := NewOAuthService(nil, claude, rdb)
	defer a.Stop()
	defer b.Stop()
	c := NewOpenAIOAuthService(nil, oa, rdb)
	d := NewOpenAIOAuthService(nil, oa, rdb)
	defer c.Stop()
	defer d.Stop()
	require.NoError(t, oauth.PutSharedSession(ctx, rdb, "oauth:anthropic:session", &oauth.OAuthSession{State: "state", CreatedAt: time.Now()}, time.Minute))
	require.NoError(t, oauth.PutSharedSession(ctx, rdb, "oauth:openai:session", &openai.OAuthSession{State: "state", CreatedAt: time.Now()}, time.Minute))
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			x, y := a, c
			if i%2 == 1 {
				x, y = b, d
			}
			_, _ = x.ExchangeCode(ctx, &ExchangeCodeInput{SessionID: "session", Code: "code#state"})
			_, _ = y.ExchangeCode(ctx, &OpenAIExchangeCodeInput{SessionID: "session", Code: "code", State: "state"})
		}(i)
	}
	wg.Wait()
	require.Equal(t, int32(1), claude.calls.Load())
	require.Equal(t, int32(1), oa.calls.Load())
}
func TestVendorNativeClaudeWrongStateDoesNotConsume(t *testing.T) {
	stub := &vendorNativeClaudeStub{}
	s := NewOAuthService(nil, stub)
	defer s.Stop()
	s.sessionStore.Set("id", &oauth.OAuthSession{State: "correct", CreatedAt: time.Now()})
	_, err := s.ExchangeCode(context.Background(), &ExchangeCodeInput{SessionID: "id", Code: "code#wrong"})
	require.Error(t, err)
	require.Zero(t, stub.calls.Load())
	_, ok := s.sessionStore.Get("id")
	require.True(t, ok)
}
func TestVendorTokenLockSharesNativeKeyAndDoesNotReleaseSuccessor(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rdb.Close()
	s := &VendorService{rdb: rdb}
	ctx := context.Background()
	a := &Account{ID: 7, Platform: PlatformOpenAI}
	require.NoError(t, rdb.Set(ctx, "oauth:refresh_lock:"+OpenAITokenCacheKey(a), 1, time.Minute).Err())
	_, err := s.vendorTokenLock(ctx, a)
	require.ErrorIs(t, err, ErrVendorAccountConflict)
	require.NoError(t, rdb.Del(ctx, "oauth:refresh_lock:"+OpenAITokenCacheKey(a)).Err())
	release, err := s.vendorTokenLock(ctx, a)
	require.NoError(t, err)
	server.FastForward(91 * time.Second)
	require.NoError(t, rdb.Set(ctx, "oauth:refresh_lock:"+OpenAITokenCacheKey(a), "new-worker", time.Minute).Err())
	release()
	require.Equal(t, "new-worker", rdb.Get(ctx, "oauth:refresh_lock:"+OpenAITokenCacheKey(a)).Val())
}
func TestVendorOpenAIVerificationNeverUsesUnsignedJWTIdentity(t *testing.T) {
	server := newChatGPTBackendTestServer(t, chatGPTBackendTestServerConfig{accountsCheck: map[string]any{"accounts": map[string]any{"trusted": map[string]any{"account": map[string]any{"account_id": "trusted", "plan_type": "plus", "is_default": true}}}}})
	defer server.Close()
	native := NewOpenAIOAuthService(nil, nil)
	defer native.Stop()
	native.privacyClientFactory = newTestPrivacyClientFactory()
	s := &VendorService{openaiOAuth: native}
	verified, err := s.vendorVerifyOpenAI(context.Background(), &openai.TokenResponse{AccessToken: "authenticated", IDToken: "eyJhbGciOiJub25lIn0.eyJlbWFpbCI6ImF0dGFja2VyQGV4YW1wbGUuY29tIn0.", RefreshToken: "rt", ExpiresIn: 3600}, "oauth", "")
	require.NoError(t, err)
	require.Equal(t, "trusted", verified.Identity)
	require.Empty(t, verified.Email)
	require.NotContains(t, verified.Credentials, "id_token")
	require.NotContains(t, verified.Credentials, "email")
	native.privacyClientFactory = nil
	_, err = s.vendorVerifyOpenAI(context.Background(), &openai.TokenResponse{AccessToken: "arbitrary", IDToken: "unverified"}, "oauth", "")
	require.ErrorIs(t, err, ErrVendorIdentityUnverified)
}
func TestVendorNativeLeaseRejectsAnotherSessionBeforeConsume(t *testing.T) {
	stub := &vendorNativeClaudeStub{}
	s := NewOAuthService(nil, stub)
	defer s.Stop()
	lease := &authz.Lease{UserID: 1, Role: authz.SuperAdmin, SessionID: "original"}
	s.sessionStore.Set("id", &oauth.OAuthSession{State: "state", CreatedAt: time.Now(), Authorization: lease})
	_, err := s.ExchangeCode(context.Background(), &ExchangeCodeInput{SessionID: "id", Code: "code#state"})
	require.ErrorIs(t, err, authz.ErrAuthorizationExpired)
	require.Zero(t, stub.calls.Load())
	_, ok := s.sessionStore.Get("id")
	require.True(t, ok)
}

func TestVendorLeaseBindsPolicyActorSessionAndNativeNamespace(t *testing.T) {
	actor := authz.Subject{UserID: 11, Role: authz.VendorLevel1, SessionID: "session-a", SessionGeneration: 4}
	scope := authz.VendorScope{VendorID: 3, PolicyVersion: 7, ActorUserID: 11}
	ctx := authz.WithVendorScope(authz.WithSubject(context.Background(), actor), scope)
	lease := authz.CaptureLease(ctx)
	require.NoError(t, authz.CheckLease(ctx, lease, "accounts.authorize"))
	changed := scope
	changed.PolicyVersion++
	require.ErrorIs(t, authz.CheckLease(authz.WithVendorScope(ctx, changed), lease, "accounts.authorize"), authz.ErrAuthorizationExpired)
	changed = scope
	changed.VendorID++
	require.ErrorIs(t, authz.CheckLease(authz.WithVendorScope(ctx, changed), lease, "accounts.authorize"), authz.ErrAuthorizationExpired)
	actor.SessionID = "session-b"
	require.ErrorIs(t, authz.CheckLease(authz.WithSubject(ctx, actor), lease, "accounts.authorize"), authz.ErrAuthorizationExpired)
	require.ErrorIs(t, authz.CheckLease(context.Background(), lease, "accounts.authorize"), authz.ErrAuthorizationExpired)
	native := NewOAuthService(nil, &vendorNativeClaudeStub{})
	defer native.Stop()
	_, err := native.GenerateAuthURL(ctx, nil)
	require.ErrorIs(t, err, authz.ErrAuthorizationExpired)
	nativeOA := NewOpenAIOAuthService(nil, &vendorNativeOpenAIStub{})
	defer nativeOA.Stop()
	_, err = nativeOA.ExchangeCode(ctx, &OpenAIExchangeCodeInput{SessionID: "id", Code: "code", State: "state"})
	require.ErrorIs(t, err, authz.ErrAuthorizationExpired)
}

func TestVendorNetworkSlotsBoundCrossInstanceConcurrency(t *testing.T) {
	server := miniredis.RunT(t)
	a := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer a.Close()
	b := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer b.Close()
	first, err := (&VendorService{rdb: a}).vendorNetworkSlot(context.Background(), 3, "first")
	require.NoError(t, err)
	second, err := (&VendorService{rdb: b}).vendorNetworkSlot(context.Background(), 3, "second")
	require.NoError(t, err)
	_, err = (&VendorService{rdb: a}).vendorNetworkSlot(context.Background(), 3, "third")
	require.Error(t, err)
	first()
	third, err := (&VendorService{rdb: b}).vendorNetworkSlot(context.Background(), 3, "third")
	require.NoError(t, err)
	second()
	third()
}

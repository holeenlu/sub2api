//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// openAITokenCacheStub implements OpenAITokenCache for testing
type openAITokenCacheStub struct {
	mu             sync.Mutex
	tokens         map[string]string
	getErr         error
	setErr         error
	deleteErr      error
	lockAcquired   bool
	lockErr        error
	releaseLockErr error
	getCalled      int32
	setCalled      int32
	lockCalled     int32
	unlockCalled   int32
	lastTTL        time.Duration
	onAcquire      func()
}

func newOpenAITokenCacheStub() *openAITokenCacheStub {
	return &openAITokenCacheStub{
		tokens:       make(map[string]string),
		lockAcquired: true,
	}
}

func (s *openAITokenCacheStub) GetAccessToken(ctx context.Context, cacheKey string) (string, error) {
	atomic.AddInt32(&s.getCalled, 1)
	if s.getErr != nil {
		return "", s.getErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[cacheKey], nil
}

func (s *openAITokenCacheStub) SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error {
	atomic.AddInt32(&s.setCalled, 1)
	if s.setErr != nil {
		return s.setErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[cacheKey] = token
	s.lastTTL = ttl
	return nil
}

func (s *openAITokenCacheStub) DeleteAccessToken(ctx context.Context, cacheKey string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, cacheKey)
	return nil
}

func (s *openAITokenCacheStub) AcquireRefreshLock(ctx context.Context, cacheKey string, ttl time.Duration) (bool, error) {
	atomic.AddInt32(&s.lockCalled, 1)
	if s.lockErr != nil {
		return false, s.lockErr
	}
	if s.onAcquire != nil {
		s.onAcquire()
	}
	return s.lockAcquired, nil
}

func (s *openAITokenCacheStub) ReleaseRefreshLock(ctx context.Context, cacheKey string) error {
	atomic.AddInt32(&s.unlockCalled, 1)
	return s.releaseLockErr
}

func TestOpenAITokenProvider_CacheHit(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	account := &Account{
		ID:       100,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "db-token",
		},
	}
	cacheKey := OpenAITokenCacheKey(account)
	cache.tokens[cacheKey] = "cached-token"

	provider := NewOpenAITokenProvider(nil, cache, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "cached-token", token)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.getCalled))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalled))
}

func TestOpenAITokenProvider_CacheMiss_FromCredentials(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	// Token expires in far future, no refresh needed
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       101,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "credential-token",
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "credential-token", token)

	// Should have stored in cache
	cacheKey := OpenAITokenCacheKey(account)
	require.Equal(t, "credential-token", cache.tokens[cacheKey])
}

func TestOpenAITokenProvider_NilAccount(t *testing.T) {
	provider := NewOpenAITokenProvider(nil, nil, nil)

	token, err := provider.GetAccessToken(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "account is nil")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_WrongPlatform(t *testing.T) {
	provider := NewOpenAITokenProvider(nil, nil, nil)
	account := &Account{
		ID:       104,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an openai oauth account")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_WrongAccountType(t *testing.T) {
	provider := NewOpenAITokenProvider(nil, nil, nil)
	account := &Account{
		ID:       105,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an openai oauth account")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_NilCache(t *testing.T) {
	// Token doesn't need refresh
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       106,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "nocache-token",
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, nil, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "nocache-token", token)
}

func TestOpenAITokenProvider_CacheGetError(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	cache.getErr = errors.New("redis connection failed")

	// Token doesn't need refresh
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       107,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "fallback-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)

	// Should gracefully degrade and return from credentials
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "fallback-token", token)
}

func TestOpenAITokenProvider_CacheSetError(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	cache.setErr = errors.New("redis write failed")

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       108,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "still-works-token",
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)

	// Should still work even if cache set fails
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "still-works-token", token)
}

func TestOpenAITokenProvider_MissingAccessToken(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       109,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"expires_at": expiresAt,
			// missing access_token
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_TTLCalculation(t *testing.T) {
	tests := []struct {
		name      string
		expiresIn time.Duration
	}{
		{
			name:      "far_future_expiry",
			expiresIn: 1 * time.Hour,
		},
		{
			name:      "medium_expiry",
			expiresIn: 10 * time.Minute,
		},
		{
			name:      "near_expiry",
			expiresIn: 6 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newOpenAITokenCacheStub()
			expiresAt := time.Now().Add(tt.expiresIn).Format(time.RFC3339)
			account := &Account{
				ID:       200,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "test-token",
					"expires_at":   expiresAt,
				},
			}

			provider := NewOpenAITokenProvider(nil, cache, nil)

			_, err := provider.GetAccessToken(context.Background(), account)
			require.NoError(t, err)

			// Verify token was cached
			cacheKey := OpenAITokenCacheKey(account)
			require.Equal(t, "test-token", cache.tokens[cacheKey])
		})
	}
}

func TestOpenAITokenProvider_NoExpiryWithoutRefreshToken(t *testing.T) {
	cache := newOpenAITokenCacheStub()

	// Token with nil expires_at (no expiry set) - should use credentials
	account := &Account{
		ID:       202,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "no-expiry-token",
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	// A token without an expiry or refresh token retains the existing contract.
	require.NoError(t, err)
	require.Equal(t, "no-expiry-token", token)
}

func TestOpenAITokenProvider_Real_WhitespaceToken(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	cacheKey := "openai:account:203"
	cache.tokens[cacheKey] = "   " // Whitespace only - should be treated as empty

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       203,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "real-token",
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "real-token", token) // Should fall back to credentials
}

func TestOpenAITokenProvider_Real_WhitespaceCredentialToken(t *testing.T) {
	cache := newOpenAITokenCacheStub()

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       205,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "   ", // Whitespace only
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_Real_NilCredentials(t *testing.T) {
	cache := newOpenAITokenCacheStub()

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       206,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"expires_at": expiresAt,
			// No access_token
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

// Exercise the production provider and unified API; only the upstream exchange
// and storage boundaries are stubbed.
type openAITestRefreshExecutor struct{ *refreshAPIExecutorStub }

func (e openAITestRefreshExecutor) CacheKey(account *Account) string {
	return OpenAITokenCacheKey(account)
}

func TestOpenAITokenProvider_TokenRefresh(t *testing.T) {
	account := nativeRefreshTestAccount()
	repo := &refreshAPIAccountRepo{account: account}
	cache := newOpenAITokenCacheStub()
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{
		"access_token": "new-at", "refresh_token": "new-rt", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}
	provider := NewOpenAITokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), openAITestRefreshExecutor{executor})
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "new-at", token)
	require.Equal(t, 1, executor.refreshCalls)
	require.Equal(t, 1, repo.successCASCalls)
	require.Equal(t, "new-at", cache.tokens[OpenAITokenCacheKey(account)])
}

func TestOpenAITokenProvider_RefreshFailureExpiryAndTTL(t *testing.T) {
	for _, expiresIn := range []time.Duration{-time.Minute, 20 * time.Second} {
		t.Run(expiresIn.String(), func(t *testing.T) {
			account := nativeRefreshTestAccount()
			account.Credentials["expires_at"] = time.Now().Add(expiresIn).Format(time.RFC3339)
			repo := &refreshAPIAccountRepo{account: account}
			cache := newOpenAITokenCacheStub()
			if expiresIn < 0 {
				cache.tokens[OpenAITokenCacheKey(account)] = "old-at"
			}
			failure := errors.New("temporary token endpoint failure")
			executor := &refreshAPIExecutorStub{needsRefresh: true, err: failure}
			provider := NewOpenAITokenProvider(repo, cache, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), openAITestRefreshExecutor{executor})
			token, err := provider.GetAccessToken(context.Background(), account)
			if expiresIn < 0 {
				require.ErrorIs(t, err, failure)
				require.Empty(t, token)
				require.Zero(t, cache.setCalled)
			} else {
				require.NoError(t, err)
				require.Equal(t, "old-at", token)
				require.Positive(t, cache.lastTTL)
				require.LessOrEqual(t, cache.lastTTL, expiresIn)
			}
			require.Equal(t, StatusActive, account.Status, "transient refresh failures must not permanently disable the account")
		})
	}
}

func TestOpenAITokenProvider_RequiresUnifiedRefresh(t *testing.T) {
	account := nativeRefreshTestAccount()
	cache := newOpenAITokenCacheStub()
	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	var configuration *providerConfigurationRefreshError
	require.ErrorAs(t, err, &configuration)
	require.Empty(t, token)
	require.Zero(t, cache.lockCalled)
	require.Zero(t, cache.setCalled)
}

func TestOpenAITokenProvider_RefreshCannotReturnLosingOrPausedGrant(t *testing.T) {
	for _, change := range []string{"reauthorized", "disabled", "vendor", "parent", "pat", "excel", "type"} {
		t.Run(change, func(t *testing.T) {
			account := nativeRefreshTestAccount()
			repo := &refreshAPIAccountRepo{account: account}
			repo.beforeSuccessCAS = func(r *refreshAPIAccountRepo) {
				r.account = snapshotOAuthRefreshAccount(r.account)
				otherID := int64(900)
				switch change {
				case "disabled":
					r.account.Status = StatusDisabled
				case "vendor":
					r.account.VendorID = &otherID
				case "parent":
					r.account.ParentAccountID = &otherID
				case "pat":
					r.account.Credentials["auth_mode"] = OpenAIAuthModePersonalAccessToken
				case "excel":
					r.account.Credentials["client_id"] = openai.ExcelClientID
				case "type":
					r.account.Type = AccountTypeAPIKey
				default:
					r.account.Credentials = map[string]any{"access_token": "new-login-at", "refresh_token": "new-login-rt", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)}
				}
			}
			cache := newOpenAITokenCacheStub()
			executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "losing-at", "refresh_token": "rotated-rt"}}
			provider := NewOpenAITokenProvider(repo, cache, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), openAITestRefreshExecutor{executor})
			token, err := provider.GetAccessToken(context.Background(), account)
			if change != "reauthorized" {
				require.ErrorIs(t, err, errOAuthRefreshAccountStateChanged)
				require.Empty(t, token)
				require.Zero(t, cache.setCalled)
			} else {
				require.NoError(t, err)
				require.Equal(t, "new-login-at", token)
			}
		})
	}
}

func TestOpenAITokenProvider_LockWaitUsesUnifiedAPI(t *testing.T) {
	for _, cancelWait := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelWait), func(t *testing.T) {
			account := nativeRefreshTestAccount()
			repo := &refreshAPIAccountRepo{account: account}
			cache := newOpenAITokenCacheStub()
			cache.lockAcquired = false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cache.onAcquire = func() {
				if cancelWait {
					cancel()
				} else {
					cache.tokens[OpenAITokenCacheKey(account)] = "winner-at"
				}
			}
			executor := &refreshAPIExecutorStub{needsRefresh: true}
			provider := NewOpenAITokenProvider(repo, cache, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), openAITestRefreshExecutor{executor})
			token, err := provider.GetAccessToken(ctx, account)
			if cancelWait {
				require.ErrorIs(t, err, context.Canceled)
				require.Empty(t, token)
			} else {
				require.NoError(t, err)
				require.Equal(t, "winner-at", token)
				metrics := provider.SnapshotRuntimeMetrics()
				require.Equal(t, int64(1), metrics.LockContention)
				require.Equal(t, int64(1), metrics.LockWaitHit)
			}
			require.Zero(t, executor.refreshCalls)
		})
	}
}

func TestOpenAITokenProvider_NoRefreshTokenExpired_ConditionalDisable(t *testing.T) {
	for _, reauthorized := range []bool{true, false} {
		t.Run(fmt.Sprint(reauthorized), func(t *testing.T) {
			account := nativeRefreshTestAccount()
			delete(account.Credentials, "refresh_token")
			account.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
			repo := &refreshAPIAccountRepo{account: account}
			if reauthorized {
				repo.beforeSuccessCAS = func(r *refreshAPIAccountRepo) {
					r.account = snapshotOAuthRefreshAccount(r.account)
					r.account.Credentials["refresh_token"] = "new-login-rt"
				}
			}
			cache := newOpenAITokenCacheStub()
			cache.tokens[OpenAITokenCacheKey(account)] = "old-at"
			provider := NewOpenAITokenProvider(repo, cache, nil)
			blocker := &runtimeBlockRecorder{}
			provider.SetAccountRuntimeBlocker(blocker)
			token, err := provider.GetAccessToken(context.Background(), account)
			require.ErrorContains(t, err, "refresh_token is missing")
			require.Empty(t, token)
			if reauthorized {
				require.Equal(t, StatusActive, repo.account.Status)
				require.Empty(t, blocker.accounts)
			} else {
				require.Equal(t, StatusError, repo.account.Status)
				require.Len(t, blocker.accounts, 1)
				require.Empty(t, cache.tokens)
			}
		})
	}
}

func TestOpenAITokenProvider_LockWaitExpiredCacheRequiresDurableRotation(t *testing.T) {
	for _, rotated := range []bool{true, false} {
		t.Run(fmt.Sprint(rotated), func(t *testing.T) {
			account := nativeRefreshTestAccount()
			account.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
			repo := &refreshAPIAccountRepo{account: account}
			cache := newOpenAITokenCacheStub()
			cache.lockAcquired = false
			cache.tokens[OpenAITokenCacheKey(account)] = "old-at"
			cache.onAcquire = func() {
				if rotated {
					repo.account = snapshotOAuthRefreshAccount(account)
					repo.account.Credentials["access_token"] = "durable-new-at"
					repo.account.Credentials["_token_version"] = int64(2)
					repo.account.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
				}
			}
			executor := &refreshAPIExecutorStub{needsRefresh: true}
			provider := NewOpenAITokenProvider(repo, cache, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), openAITestRefreshExecutor{executor})
			token, err := provider.GetAccessToken(context.Background(), account)
			if rotated {
				require.NoError(t, err)
				require.Equal(t, "durable-new-at", token)
			} else {
				require.ErrorContains(t, err, "expired")
				require.Empty(t, token)
			}
			require.Zero(t, executor.refreshCalls)
			require.Zero(t, cache.setCalled)
		})
	}
}

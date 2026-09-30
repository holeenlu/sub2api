//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAuthLifecyclePostgresSerialization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := integrationEntClient
	repo := newUserRepositoryWithSQL(client, integrationDB)
	user := &service.User{Email: fmt.Sprintf("lifecycle-pg-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, user))
	pending := service.NewAuthPendingIdentityService(client)
	input := service.CreatePendingAuthSessionInput{Intent: "authorize_bind", Identity: service.PendingAuthIdentityKey{ProviderType: "oidc", ProviderKey: "oidc", ProviderSubject: "subject"}, TargetUserID: &user.ID, BrowserSessionKey: "browser"}
	original, err := pending.CreatePendingSession(ctx, input)
	require.NoError(t, err, "migration 258 accepts the new authorization intent")
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- repo.RevokeUserSessions(ctx, user.ID) }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	current, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.EqualValues(t, 16, current.SessionGeneration)
	_, err = pending.GetBrowserSession(ctx, original.SessionToken, "browser")
	require.ErrorIs(t, err, service.ErrPendingAuthSessionConsumed)
	input.Intent = "login"
	continuation, err := pending.CreatePendingSession(ctx, input)
	require.NoError(t, err)
	_, err = client.AuthIdentity.Create().SetUserID(user.ID).SetProviderType("oidc").SetProviderKey("oidc").SetProviderSubject(fmt.Sprintf("subject-%d", user.ID)).Save(ctx)
	require.NoError(t, err)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = service.LockAuthLifecycleUser(ctx, tx.Client(), user.ID)
	require.NoError(t, err)
	unbound := make(chan error, 1)
	go func() { unbound <- repo.UnbindUserAuthProvider(ctx, user.ID, "oidc") }()
	require.Eventually(t, func() bool {
		var n int
		err := integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'UPDATE "users"%'`).Scan(&n)
		return err == nil && n > 0
	}, 3*time.Second, 10*time.Millisecond, "unbind must wait behind pending credential mutation")
	_, err = service.NewAuthPendingIdentityService(tx.Client()).ConsumeBrowserSession(ctx, continuation.SessionToken, "browser")
	require.NoError(t, err)
	_, err = tx.Client().AuthIdentity.Create().SetUserID(user.ID).SetProviderType("oidc").SetProviderKey("oidc").SetProviderSubject(fmt.Sprintf("bound-%d", user.ID)).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-unbound)
	exists, err := client.AuthIdentity.Query().Where(authidentity.UserIDEQ(user.ID), authidentity.ProviderTypeEQ("oidc")).Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists, "unbind wins after the authorized in-flight mutation and removes both identities")
	current, err = repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.EqualValues(t, 17, current.SessionGeneration)
	singleUse, err := pending.CreatePendingSession(ctx, input)
	require.NoError(t, err)
	var winners atomic.Int32
	results = make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := pending.ConsumeBrowserSession(ctx, singleUse.SessionToken, "browser")
			if err == nil {
				winners.Add(1)
			} else {
				results <- err
			}
		}()
	}
	wg.Wait()
	close(results)
	require.EqualValues(t, 1, winners.Load())
	for err := range results {
		require.ErrorIs(t, err, service.ErrPendingAuthSessionConsumed)
	}
}

func TestAuthLifecycleRedisRealAtomic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("lifecycle-%d", time.Now().UnixNano())
	email := suffix + "@example.com"
	emailCache := &emailCache{rdb: integrationRedis}
	require.NoError(t, emailCache.SetPasswordResetToken(ctx, email, &service.PasswordResetTokenData{Token: "correct"}, time.Minute))
	ok, err := emailCache.ConsumePasswordResetToken(ctx, email, "wrong")
	require.NoError(t, err)
	require.False(t, ok)
	var winners atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := emailCache.ConsumePasswordResetToken(ctx, email, "correct")
			if ok {
				winners.Add(1)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	require.EqualValues(t, 1, winners.Load())
	for err := range errs {
		require.NoError(t, err)
	}
	cache := &refreshTokenCache{rdb: integrationRedis}
	data := &service.RefreshTokenData{UserID: 999999, FamilyID: suffix, ExpiresAt: time.Now().Add(time.Minute), FamilyTTLMillis: time.Hour.Milliseconds()}
	require.NoError(t, cache.StoreRefreshToken(ctx, suffix, data, time.Minute))
	winners.Store(0)
	errs = make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cache.ConsumeRefreshToken(ctx, suffix)
			if err == nil {
				winners.Add(1)
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	require.EqualValues(t, 1, winners.Load())
	for err := range errs {
		require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)
	}
	require.ErrorIs(t, cache.StoreRefreshToken(ctx, suffix+"child", data, time.Minute), service.ErrTokenRevoked)
	ttl, err := integrationRedis.PTTL(ctx, "revoked_family:"+suffix).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 59*time.Minute, "replay revocation covers the access-token lifetime")
}

type lifecyclePausedStore struct {
	service.RefreshTokenCache
	real    *refreshTokenCache
	reached chan struct{}
	resume  chan struct{}
}

func (c *lifecyclePausedStore) StoreRefreshToken(ctx context.Context, key string, data *service.RefreshTokenData, ttl time.Duration) error {
	close(c.reached)
	select {
	case <-c.resume:
	case <-ctx.Done():
		return ctx.Err()
	}
	return c.real.StoreRefreshToken(ctx, key, data, ttl)
}
func (c *lifecyclePausedStore) ConsumeRefreshToken(ctx context.Context, key string) (*service.RefreshTokenData, error) {
	return c.real.ConsumeRefreshToken(ctx, key)
}

func TestAuthLifecyclePostgresRefreshRevocationRace(t *testing.T) {
	for _, familyOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("family-%t", familyOnly), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			client := integrationEntClient
			repo := newUserRepositoryWithSQL(client, integrationDB)
			u := &service.User{Email: fmt.Sprintf("rotation-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
			require.NoError(t, repo.Create(ctx, u))
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "integration-secret", AccessTokenExpireMinutes: 60, RefreshTokenExpireDays: 7}}
			cache := &refreshTokenCache{rdb: integrationRedis}
			auth := service.NewAuthService(client, repo, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			pair, err := auth.GenerateTokenPair(ctx, u, "")
			require.NoError(t, err)
			claims, err := auth.ValidateToken(pair.AccessToken)
			require.NoError(t, err)
			pause := &lifecyclePausedStore{RefreshTokenCache: cache, real: cache, reached: make(chan struct{}), resume: make(chan struct{})}
			rotator := service.NewAuthService(client, repo, nil, pause, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			type result struct {
				pair *service.TokenPairWithUser
				err  error
			}
			done := make(chan result, 1)
			go func() { p, e := rotator.RefreshTokenPair(ctx, pair.RefreshToken); done <- result{p, e} }()
			select {
			case <-pause.reached:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if familyOnly {
				require.NoError(t, auth.RevokeSessionFamily(ctx, claims.SessionID))
			} else {
				require.NoError(t, auth.RevokeAllUserTokens(ctx, u.ID))
			}
			close(pause.resume)
			out := <-done
			if familyOnly {
				require.ErrorIs(t, out.err, service.ErrTokenRevoked)
				require.Nil(t, out.pair)
			} else {
				require.NoError(t, out.err)
				latest, err := repo.GetByID(ctx, u.ID)
				require.NoError(t, err)
				returned, err := auth.ValidateToken(out.pair.AccessToken)
				require.NoError(t, err)
				require.ErrorIs(t, auth.ValidateAccessSession(ctx, returned, latest), service.ErrTokenRevoked)
				_, err = auth.RefreshTokenPair(ctx, out.pair.RefreshToken)
				require.ErrorIs(t, err, service.ErrTokenRevoked)
			}
		})
	}
}

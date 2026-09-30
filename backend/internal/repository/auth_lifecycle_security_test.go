package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func lifecycleRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestAuthLifecycleAtomicPasswordReset(t *testing.T) {
	server, client := lifecycleRedis(t)
	cache := &emailCache{rdb: client}
	svc := service.NewEmailService(nil, cache)
	ctx := context.Background()
	require.NoError(t, cache.SetPasswordResetToken(ctx, "Owner@example.com", &service.PasswordResetTokenData{Token: "correct", CreatedAt: time.Now()}, time.Minute))
	require.ErrorIs(t, svc.ConsumePasswordResetToken(ctx, "owner@example.com", "wrong"), service.ErrInvalidResetToken)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.ConsumePasswordResetToken(ctx, "OWNER@example.com", "correct") == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load())
	require.ErrorIs(t, svc.ConsumePasswordResetToken(ctx, "owner@example.com", "correct"), service.ErrInvalidResetToken)
	require.NoError(t, cache.SetPasswordResetToken(ctx, "owner@example.com", &service.PasswordResetTokenData{Token: "new"}, time.Second))
	server.FastForward(2 * time.Second)
	require.ErrorIs(t, svc.ConsumePasswordResetToken(ctx, "owner@example.com", "new"), service.ErrInvalidResetToken)
	server.SetError("storage unavailable")
	require.ErrorIs(t, svc.ConsumePasswordResetToken(ctx, "owner@example.com", "new"), service.ErrServiceUnavailable)
}

func TestAuthLifecycleAtomicRefreshAndFamilyTombstone(t *testing.T) {
	server, client := lifecycleRedis(t)
	cache := &refreshTokenCache{rdb: client}
	ctx := context.Background()
	data := &service.RefreshTokenData{UserID: 1, FamilyID: "family", ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, cache.StoreRefreshToken(ctx, "original", data, time.Hour))
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.ConsumeRefreshToken(ctx, "original"); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load())
	require.ErrorIs(t, cache.StoreRefreshToken(ctx, "racing-child", data, time.Hour), service.ErrTokenRevoked)
	data.FamilyID = "legitimate"
	require.NoError(t, cache.StoreRefreshToken(ctx, "parent", data, time.Hour))
	_, err := cache.ConsumeRefreshToken(ctx, "parent")
	require.NoError(t, err)
	require.NoError(t, cache.StoreRefreshToken(ctx, "child", data, time.Hour))
	_, err = cache.ConsumeRefreshToken(ctx, "parent")
	require.Error(t, err)
	_, err = cache.GetRefreshToken(ctx, "child")
	require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)
	require.ErrorIs(t, cache.StoreRefreshToken(ctx, "late-child", data, time.Hour), service.ErrTokenRevoked)
	data.FamilyID = "explicit-revocation"
	require.NoError(t, cache.StoreRefreshToken(ctx, "live", data, time.Hour))
	require.NoError(t, cache.DeleteTokenFamily(ctx, data.FamilyID))
	require.ErrorIs(t, cache.StoreRefreshToken(ctx, "after-revoke", data, time.Hour), service.ErrTokenRevoked)
	require.NoError(t, cache.RevokeTokenFamily(ctx, data.FamilyID, time.Hour))
	require.NoError(t, cache.RevokeTokenFamily(ctx, data.FamilyID, time.Minute))
	require.Greater(t, server.TTL("revoked_family:"+data.FamilyID), 59*time.Minute, "repeated revocation must not shorten the tombstone")

	server.SetError("storage unavailable")
	_, err = cache.ConsumeRefreshToken(ctx, "anything")
	require.Error(t, err)
}

// The wrapper omits unrelated optional avatar/activity interfaces; user reads use the real repository.
type lifecycleJWTRepo struct{ service.UserRepository }

func (lifecycleJWTRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}
func TestAuthLifecycleDurableRevokeRejectsJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	entity, err := client.User.Create().SetEmail("revoke@example.com").SetPasswordHash("hash").SetRole(service.RoleAdmin).Save(ctx)
	require.NoError(t, err)
	user, err := repo.GetByID(ctx, entity.ID)
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(strings.ToLower(user.Email) + "\n" + user.PasswordHash))
	require.Equal(t, int64(binary.BigEndian.Uint64(sum[:8])&0x7fffffffffffffff), service.CredentialVersion(user), "legacy generation-zero token contract")
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "lifecycle-test-secret", AccessTokenExpireMinutes: 60}}
	auth := service.NewAuthService(client, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	token, err := auth.GenerateToken(ctx, user)
	require.NoError(t, err)
	users := service.NewUserService(lifecycleJWTRepo{repo}, nil, nil, nil)
	router := gin.New()
	router.GET("/jwt", gin.HandlerFunc(middleware.NewJWTAuthMiddleware(auth, users, nil, nil)), func(c *gin.Context) { c.Status(204) })
	router.GET("/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)), func(c *gin.Context) { c.Status(204) })
	router.GET("/optional", gin.HandlerFunc(middleware.NewOptionalJWTAuthMiddleware(auth, users, nil, nil)), func(c *gin.Context) { c.Status(204) })
	request := func(path, token string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, r)
		return w.Code
	}
	require.Equal(t, 204, request("/jwt", token))
	require.NoError(t, auth.RevokeAllUserTokens(ctx, user.ID))
	require.Equal(t, 401, request("/jwt", token))
	require.Equal(t, 401, request("/admin", token))
	require.Equal(t, 401, request("/optional", token))
	require.Equal(t, 204, request("/optional", ""))
	fresh, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, fresh.SessionGeneration)
	freshToken, err := auth.GenerateToken(ctx, fresh)
	require.NoError(t, err)
	require.Equal(t, 204, request("/jwt", freshToken))
	require.Equal(t, 204, request("/admin", freshToken))
	staleToken, err := auth.GenerateToken(ctx, user)
	require.NoError(t, err)
	require.Equal(t, 401, request("/jwt", staleToken), "issuance from a pre-revocation snapshot cannot revive the session")
}

func TestAuthLifecycleUnbindInvalidatesPendingAndSetupCAS(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	u, err := client.User.Create().SetEmail("unbind@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	_, err = client.AuthIdentity.Create().SetUserID(u.ID).SetProviderType("wechat").SetProviderKey("wechat").SetProviderSubject("subject").Save(ctx)
	require.NoError(t, err)
	pending := service.NewAuthPendingIdentityService(client)
	session, err := pending.CreatePendingSession(ctx, service.CreatePendingAuthSessionInput{Intent: "login", Identity: service.PendingAuthIdentityKey{ProviderType: "wechat", ProviderKey: "wechat", ProviderSubject: "subject"}, TargetUserID: &u.ID, BrowserSessionKey: "browser"})
	require.NoError(t, err)
	require.NoError(t, repo.UnbindUserAuthProvider(ctx, u.ID, "wechat"))
	_, err = pending.GetBrowserSession(ctx, session.SessionToken, "browser")
	require.ErrorIs(t, err, service.ErrPendingAuthSessionConsumed)
	current, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, current.SessionGeneration)
	require.NoError(t, repo.ActivateTotp(ctx, current, "existing-secret"))
	require.ErrorIs(t, repo.ActivateTotp(ctx, current, "attacker-secret"), service.ErrTokenRevoked)
	stored, err := client.User.Get(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, "existing-secret", *stored.TotpSecretEncrypted)
	require.NoError(t, repo.DisableTotp(ctx, u.ID))
	require.ErrorIs(t, repo.ActivateTotp(ctx, current, "stale-setup"), service.ErrTokenRevoked)
}

func TestAuthLifecycleRefreshServiceControls(t *testing.T) {
	server, rdb := lifecycleRedis(t)
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	u, err := client.User.Create().SetEmail("refresh@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	user, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test", AccessTokenExpireMinutes: 60, RefreshTokenExpireDays: 7}}
	auth := service.NewAuthService(client, repo, nil, NewRefreshTokenCache(rdb), cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	pair, err := auth.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	rotated, err := auth.RefreshTokenPair(ctx, pair.RefreshToken)
	require.NoError(t, err)
	_, err = auth.RefreshTokenPair(ctx, pair.RefreshToken)
	require.Error(t, err)
	_, err = auth.RefreshTokenPair(ctx, rotated.RefreshToken)
	require.Error(t, err)
	pair, err = auth.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	server.SetError("storage unavailable")
	_, err = auth.RefreshTokenPair(ctx, pair.RefreshToken)
	require.True(t, errors.Is(err, service.ErrServiceUnavailable))
}

func TestAuthLifecycleUnchangedProfilePreservesSession(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	e, err := client.User.Create().SetEmail("profile@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	u, err := repo.GetByID(ctx, e.ID)
	require.NoError(t, err)
	version := service.CredentialVersion(u)
	u.Username = "new display name"
	require.NoError(t, repo.Update(ctx, u, service.UserUpdateFields{Username: true, Email: true}))
	current, err := repo.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, version, service.CredentialVersion(current))
	require.Equal(t, "new display name", current.Username)
}

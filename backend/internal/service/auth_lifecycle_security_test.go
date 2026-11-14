//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lifecycleGrantCache struct {
	TotpCache
	grants map[string]bool
}

func (c *lifecycleGrantCache) SetStepUpGrant(_ context.Context, _ int64, key string, _ time.Duration) error {
	c.grants[key] = true
	return nil
}
func (c *lifecycleGrantCache) HasStepUpGrant(_ context.Context, _ int64, key string) (bool, error) {
	return c.grants[key], nil
}
func TestAuthLifecycleStepUpGenerationAndFactor(t *testing.T) {
	ctx := context.Background()
	secret := "existing-factor"
	user := &User{ID: 1, Email: "admin@example.com", PasswordHash: "hash", Role: RoleAdmin, TotpEnabled: true, TotpSecretEncrypted: &secret}
	repo := &totpVMUserRepoStub{user: user}
	cache := &lifecycleGrantCache{grants: map[string]bool{}}
	settings := NewSettingService(&totpVMSettingRepoStub{values: map[string]string{SettingKeyStepUpEnabled: "true"}}, nil)
	svc := NewTotpService(repo, nil, cache, settings, nil, nil)
	for _, sid := range []string{"family", "u1"} {
		key, err := svc.stepUpCredentialKey(ctx, 1, sid)
		require.NoError(t, err)
		cache.grants[key] = true
		ok, err := svc.HasStepUpGrant(ctx, 1, sid)
		require.NoError(t, err)
		require.True(t, ok)
		user.SessionGeneration++
		ok, err = svc.HasStepUpGrant(ctx, 1, sid)
		require.NoError(t, err)
		require.False(t, ok)
		key, err = svc.stepUpCredentialKey(ctx, 1, sid)
		require.NoError(t, err)
		cache.grants[key] = true
		changed := "replacement-factor"
		user.TotpSecretEncrypted = &changed
		ok, err = svc.HasStepUpGrant(ctx, 1, sid)
		require.NoError(t, err)
		require.False(t, ok)
		user.TotpSecretEncrypted = &secret
	}
	require.NoError(t, user.SetPassword("correct-password"))
	require.Error(t, svc.DisableWithSession(ctx, 1, "", "correct-password", "unverified"))
	require.False(t, repo.disableCalled)
	key, err := svc.stepUpCredentialKey(ctx, 1, "verified")
	require.NoError(t, err)
	cache.grants[key] = true
	require.NoError(t, svc.DisableWithSession(ctx, 1, "", "correct-password", "verified"))
	require.True(t, repo.disableCalled)
}

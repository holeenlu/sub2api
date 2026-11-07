package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

type vendorIdentityUsers struct {
	UserRepository
	user   *User
	denied bool
}

func (r *vendorIdentityUsers) GetByID(context.Context, int64) (*User, error) { return r.user, nil }
func (r *vendorIdentityUsers) ValidateVendorIdentity(context.Context, int64, string) error {
	if r.denied {
		return ErrVendorDenied
	}
	return nil
}
func TestVendorIdentityLoginRealmAndRevocation(t *testing.T) {
	repo := &vendorIdentityUsers{}
	s := &AuthService{userRepo: repo, cfg: &config.Config{JWT: config.JWTConfig{Secret: "local-test-key", ExpireHour: 1}}}
	for _, role := range []string{RoleUser, RoleAdmin, RoleSuperAdmin, RoleVendorLevel1, RoleVendorLevel2} {
		t.Run(role, func(t *testing.T) {
			user := &User{ID: 3, Role: role, Status: StatusActive, Email: "test@example.invalid"}
			repo.user = user
			supplier := authz.WithSupplierOrigin(context.Background(), true)
			customer := authz.WithSupplierOrigin(context.Background(), false)
			e := s.ValidateLoginIdentity(supplier, user)
			if role == RoleUser || role == RoleAdmin {
				require.ErrorIs(t, e, ErrVendorDenied)
			} else {
				require.NoError(t, e)
			}
			e = s.ValidateLoginIdentity(customer, user)
			if user.IsVendor() {
				require.ErrorIs(t, e, ErrVendorDenied)
			} else {
				require.NoError(t, e)
			}
		})
	}
	user := &User{ID: 3, Role: RoleVendorLevel2, Status: StatusActive, Email: "vendor@example.invalid"}
	ctx := authz.WithSupplierOrigin(context.Background(), true)
	token, err := s.GenerateToken(ctx, user)
	require.NoError(t, err)
	claims, err := s.ValidateToken(token)
	require.NoError(t, err)
	require.NoError(t, s.ValidateAccessSession(ctx, claims, user))
	repo.denied = true
	require.ErrorIs(t, s.ValidateAccessSession(ctx, claims, user), ErrVendorDenied)
	_, err = s.GenerateToken(ctx, user)
	require.ErrorIs(t, err, ErrVendorDenied)
	repo.denied = false
	user.SessionGeneration++
	require.ErrorIs(t, s.ValidateAccessSession(ctx, claims, user), ErrTokenRevoked)
}
func TestVendorIdentityCannotAcquireConsumerQualifications(t *testing.T) {
	for _, role := range []string{RoleVendorLevel1, RoleVendorLevel2} {
		user := &User{ID: 3, Role: role, Status: StatusActive, AllowedGroups: []int64{1}}
		require.False(t, user.CanBindGroup(1, false))
		require.False(t, user.CanBindGroup(1, true))
		s := &APIKeyService{userRepo: &vendorIdentityUsers{user: user}}
		require.False(t, s.canUserBindGroup(context.Background(), user, &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription}))
		_, err := s.Create(context.Background(), user.ID, CreateAPIKeyRequest{Name: "should fail"})
		require.ErrorIs(t, err, ErrVendorDenied)
		_, err = normalizeUserRole(role, RoleUser)
		require.Error(t, err, "vendor identities must use atomic enrollment")
	}
}

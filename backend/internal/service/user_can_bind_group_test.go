package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Policy combinations live in access; these cases exercise the entity adapter,
// especially role classification, grants, and the public restriction flag.
func TestUserCanBindGroup(t *testing.T) {
	for _, role := range []string{RoleUser, RoleAdmin, RoleSuperAdmin, RoleVendorLevel1, RoleVendorLevel2} {
		t.Run(role, func(t *testing.T) {
			user := &User{Role: role, AllowedGroups: []int64{0, 20}}
			vendor := role == RoleVendorLevel1 || role == RoleVendorLevel2
			require.Equal(t, !vendor, user.CanBindGroup(10, false))
			require.Equal(t, !vendor, user.CanBindGroup(20, true))
			require.Equal(t, !vendor, user.CanBindGroup(0, true))
			require.False(t, user.CanBindGroup(21, true))
			user.RestrictPublicGroups = true
			require.False(t, user.CanBindGroup(10, false))
			require.Equal(t, !vendor, user.CanBindGroup(20, false))
			require.Equal(t, []int64{0, 20}, user.AllowedGroups)
		})
	}
}

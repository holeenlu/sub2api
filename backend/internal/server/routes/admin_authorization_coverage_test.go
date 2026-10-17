package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestExactRoutesEnforceReadWriteAndNeverImplySuperAuthority(t *testing.T) {
	reader := authz.Subject{UserID: 1, Role: authz.Admin, PolicyVersion: 3, Permissions: []string{"users.read", "channels.read", "usage.read"}}
	cases := []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/api/v1/admin/users", true},
		{"PUT", "/api/v1/admin/users/:id", false},
		{"DELETE", "/api/v1/admin/users/:id", false},
		{"GET", "/api/v1/admin/channels/pricing/sync-models", false},
		{"POST", "/api/v1/admin/dashboard/users-usage", true},
		{"GET", "/api/v1/admin/settings/admin-api-key", false},
		{"PUT", "/api/v1/admin/roles/admin/permissions", false},
		{"GET", "/api/v1/admin/audit-logs", false},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			rule, ok := authz.RuleFor(tc.method, tc.path)
			require.True(t, ok)
			require.Equal(t, tc.allowed, rule.Allows(reader))
			require.True(t, rule.Allows(authz.Subject{Role: authz.SuperAdmin}))
			require.False(t, rule.Allows(authz.Subject{Role: authz.User}))
		})
	}
	_, known := authz.RuleFor("GET", "/api/v1/admin/new-unclassified-route")
	require.False(t, known)
	all := []string{}
	for _, permission := range authz.Catalogue() {
		all = append(all, permission.Key)
	}
	limited := authz.Subject{Role: authz.Admin, PolicyVersion: 3, Permissions: all}
	for _, route := range []string{"/api/v1/admin/backups", "/api/v1/admin/settings/admin-api-key", "/api/v1/admin/plugins"} {
		rule, ok := authz.RuleFor("GET", route)
		require.True(t, ok)
		require.False(t, rule.Allows(limited))
	}
}

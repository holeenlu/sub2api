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
	rule, ok := authz.RuleFor("POST", "/api/v1/admin/accounts/:id/telemetry/import")
	require.True(t, ok)
	require.False(t, rule.Allows(limited))
	require.True(t, rule.Allows(authz.Subject{Role: authz.SuperAdmin}))
	_, generated := authz.RuleFor("POST", "/api/v1/admin/accounts/:id/telemetry/generate")
	require.False(t, generated)
	for _, path := range []string{"/api/v1/admin/ops/client-telemetry", "/api/v1/admin/ops/client-telemetry/:id"} {
		_, known := authz.RuleFor("GET", path)
		require.False(t, known)
	}

}

func TestRetiredFinancialFeaturesCannotBeDelegated(t *testing.T) {
	for _, permission := range authz.Catalogue() {
		require.NotContains(t, permission.Key, "redeem_codes")
		require.NotContains(t, permission.Key, "affiliates")
		require.NotEqual(t, "staff.manage", permission.Key)
	}
	for _, path := range []string{"/api/v1/admin/redeem-codes", "/api/v1/admin/affiliates/users"} {
		_, known := authz.RuleFor("GET", path)
		require.False(t, known)
	}
	rule, known := authz.RuleFor("POST", "/api/v1/admin/users/:id/balance")
	require.True(t, known)
	require.False(t, rule.Allows(authz.Subject{Role: authz.Admin, PolicyVersion: 1, Permissions: []string{"users.read"}}))
	require.True(t, rule.Allows(authz.Subject{Role: authz.Admin, PolicyVersion: 1, Permissions: []string{"users.read", "billing.balance.adjust"}}))
}

func TestRetiredBPSDefaultTemplateRoutesHaveNoPolicyEntry(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		_, exists := authz.RuleFor(method, "/api/v1/admin/settings/excel-bps-defaults")
		require.False(t, exists)
	}
}

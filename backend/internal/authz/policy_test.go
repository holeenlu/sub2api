package authz

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyRejectsUnknownRootOnlyAndBrokenDependencies(t *testing.T) {
	for _, values := range [][]string{{"*"}, {"super_admin"}, {"settings.admin_api_key"}, {"users.update"}, {"accounts.authorize", "accounts.read"}} {
		_, err := NormalizePermissions(values)
		require.Error(t, err, "%v", values)
	}
	permissions, err := NormalizePermissions([]string{"users.read", "users.create", "users.read"})
	require.NoError(t, err)
	require.Equal(t, []string{"users.create", "users.read"}, permissions)
	require.Error(t, Policy{Permissions: permissions}.Validate())
}

func TestSnapshotsSupportRootOnlyDeploymentsAndRestrictedSettings(t *testing.T) {
	root := Subject{UserID: 1, Role: SuperAdmin}.Snapshot()
	require.Contains(t, root.Pages, "/admin/users")
	require.NotEmpty(t, root.Permissions)
	limited := Subject{Role: Admin, PolicyVersion: 4, Permissions: []string{"settings.content.read"}}.Snapshot()
	require.Contains(t, limited.Pages, "/admin/settings")
	require.NotContains(t, limited.Pages, "/admin/accounts")
	require.Empty(t, Subject{Role: User}.Snapshot().Pages)
}

func TestResponseProjectionPreservesBusinessMetadataWithoutCostOrCredentials(t *testing.T) {
	actor := Subject{Role: Admin, PolicyVersion: 1, Permissions: []string{"usage.read"}}
	data := map[string]any{"actual_cost": json.Number("12.34567891"), "account_cost": json.Number("4.3"), "upstream_total_cost": 5, "profit": 8,
		"api_key":     map[string]any{"id": 2, "name": "customer key"},
		"keys":        []any{map[string]any{"user_id": 7, "name": "one", "key": "secret-value"}},
		"credentials": map[string]any{"access_token": "secret-token", "service_account_json": "private-json", "region": "us"},
		"auditLogs":   []any{map[string]any{"operator": "super admin", "detail": "private"}},
	}
	result := FilterResponse(data, "/api/v1/admin/usage", actor, false).(map[string]any)
	require.Equal(t, json.Number("12.34567891"), result["actual_cost"])
	require.Equal(t, "customer key", result["api_key"].(map[string]any)["name"])
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, secret := range []string{"account_cost", "upstream_total_cost", "profit", "secret-value", "secret-token", "private-json", "auditLogs"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestReviewedCostLeaksAndNestedCustomerFields(t *testing.T) {
	actor := Subject{Role: Admin, PolicyVersion: 1, Permissions: []string{"accounts.read", "usage.read", "channels.read", "redeem_codes.read"}}
	stats := map[string]any{"actual_cost": 12.0, "total_account_cost": 7.0, "today_account_cost": 2.0, "account_cost": 3.0}
	result := FilterResponse(stats, "/api/v1/admin/usage/stats", actor, false).(map[string]any)
	require.Equal(t, map[string]any{"actual_cost": 12.0}, result)
	account := map[string]any{"id": 1, "quota_used": 5, "rate_multiplier": .1, "avg_daily_cost": 3, "extra": map[string]any{"quota_daily_used": 1, "quota_weekly_used": 2, "ollama_cloud_usage": map[string]any{"spent": 8}, "opencode_go_usage": map[string]any{"spent": 7}}, "groups": []any{map[string]any{"id": 2, "rate_multiplier": 1.5}}}
	result = FilterResponse(account, "/api/v1/admin/accounts", actor, false).(map[string]any)
	for _, key := range []string{"quota_used", "rate_multiplier", "avg_daily_cost"} {
		require.NotContains(t, result, key)
	}
	require.Empty(t, result["extra"])
	require.Equal(t, 1.5, result["groups"].([]any)[0].(map[string]any)["rate_multiplier"])
	channel := FilterResponse(map[string]any{"account_stats_pricing_rules": []any{1}, "model_pricing": []any{2}}, "/api/v1/admin/channels", actor, false).(map[string]any)
	require.NotContains(t, channel, "account_stats_pricing_rules")
	require.Contains(t, channel, "model_pricing")
	code := func() map[string]any {
		return map[string]any{"code": "spendable-secret", "type": "balance", "value": 100}
	}
	require.Equal(t, "[redacted]", FilterResponse(code(), "/api/v1/admin/redeem-codes", actor, false).(map[string]any)["code"])
	require.Equal(t, "spendable-secret", FilterResponse(code(), "/api/v1/admin/redeem-codes/generate", actor, false).(map[string]any)["code"])
	actor.Permissions = append(actor.Permissions, "redeem_codes.export")
	require.Equal(t, "spendable-secret", FilterResponse(code(), "/api/v1/admin/redeem-codes/:id", actor, false).(map[string]any)["code"])
}

func TestSettingsAndAuditBodyUseTheSameFieldAndCostPolicy(t *testing.T) {
	actor := Subject{Role: Admin, PolicyVersion: 1, Permissions: []string{"settings.general.read", "audit.read", "accounts.read"}}
	result := FilterResponse(map[string]any{"data": map[string]any{"site_name": "Brand", "smtp_password": "private", "step_up_enabled": false, "admin_role_policy": "private"}}, "/api/v1/admin/settings", actor, false).(map[string]any)
	require.Equal(t, map[string]any{"site_name": "Brand"}, result["data"])
	log := map[string]any{"path": "/api/v1/admin/accounts/:id", "method": "PUT", "request_body": `{"credentials":{"access_token":"secret"},"account_cost":123}`}
	result = FilterResponse(log, "/api/v1/admin/audit-logs/:id", actor, false).(map[string]any)
	require.NotContains(t, result, "request_body", "audit.read must not grant the logged operation's body")
}

func TestOAuthLeaseUsesCurrentPermissionsAndOriginalIdentity(t *testing.T) {
	actor := Subject{UserID: 2, Role: Admin, PolicyVersion: 3, SessionGeneration: 5, SessionID: "session", Permissions: []string{"accounts.authorize"}}
	ctx := WithSubject(context.Background(), actor)
	lease := CaptureLease(ctx)
	require.NoError(t, CheckLease(ctx, lease, "accounts.authorize"))
	for _, mutate := range []func(*Subject){func(s *Subject) { s.UserID++ }, func(s *Subject) { s.SessionID = "other" }, func(s *Subject) { s.SessionGeneration++ }, func(s *Subject) { s.Permissions = nil }} {
		other := actor
		mutate(&other)
		require.Error(t, CheckLease(WithSubject(context.Background(), other), lease, "accounts.authorize"))
	}
	actor.PolicyVersion++
	require.NoError(t, CheckLease(WithSubject(context.Background(), actor), lease, "accounts.authorize"))
	require.Error(t, CheckLease(ctx, nil, "accounts.authorize"))
}

//go:build unit

package admin

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestRequestFieldSchemasClassifyEveryBoundField(t *testing.T) {
	// A new top-level field cannot silently acquire the module's coarse grant.
	for name, typ := range authz.RequestSchemas() {
		t.Run(name, func(t *testing.T) {
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				if f.Tag.Get("json") != "" && f.Tag.Get("json") != "-" {
					require.NotEmpty(t, f.Tag.Get("authz"), "unclassified field %s.%s", name, f.Name)
				}
			}
		})
	}
}

func TestPricingPermissionIsCheckedBeforeAnyBulkWrite(t *testing.T) {
	actor := authz.Subject{Role: authz.Admin, PolicyVersion: 1, Permissions: []string{"groups.read", "groups.manage", "channels.read", "channels.manage", "accounts.read", "accounts.manage", "accounts.authorize", "plans.read", "plans.manage"}}
	cases := []struct{ schema, body string }{
		{"group.update", `{"subscription_type":"subscription"}`}, {"group.update", `{"daily_limit_usd":null}`},
		{"group.update", `{"peak_start":"09:00"}`}, {"group.update", `{"force_openai_fast":false}`},
		{"group.update", `{"image_rate_independent":false}`}, {"channel.update", `{"billing_model_source":"upstream"}`},
		{"plan.update", `{"currency":"CNY"}`}, {"plan.update", `{"validity_unit":"month"}`}, {"plan.update", `{"group_id":7}`},
		{"account.batch", `{"accounts":[{"name":"first"},{"rate_multiplier":0}]}`},
		{"account.import", `{"data":{"proxies":[],"accounts":[{"rate_multiplier":2}]}}`},
		{"account.codex", `{"rate_multiplier":0}`}, {"account.pat", `{"rate_multiplier":0}`}, {"account.sso", `{"rate_multiplier":0}`},
	}
	for _, tc := range cases {
		t.Run(tc.schema+tc.body, func(t *testing.T) {
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.body), &body))
			_, err := authz.ValidateRequestFields(tc.schema, body, actor)
			require.Error(t, err)
			granted := actor
			granted.Permissions = append(append([]string{}, actor.Permissions...), "billing.rates.update")
			sensitive, err := authz.ValidateRequestFields(tc.schema, body, granted)
			require.NoError(t, err)
			require.True(t, sensitive)
		})
	}
	_, err := authz.ValidateRequestFields("group.update", map[string]any{"future_field": true}, actor)
	require.Error(t, err)
	_, err = authz.ValidateRequestFields("group.update", map[string]any{"future_field": true}, authz.Subject{Role: authz.SuperAdmin})
	require.NoError(t, err)
	_, err = authz.ValidateRequestFields("account.import", map[string]any{"data": map[string]any{"proxies": []any{map[string]any{"host": "proxy.example"}}, "accounts": []any{}}}, actor)
	require.Error(t, err)
}

func TestEveryFinancialJSONFieldHasAnExplicitClassification(t *testing.T) {
	// Source traversal covers every DTO, not merely a hand-picked list of types.
	// Naming is used only to detect fields needing review, never to authorize or
	// remove response data at runtime.
	patterns := []string{"../dto/*.go", "../../pkg/usagestats/*.go", "../../service/account_usage_service.go", "channel_handler.go"}
	count := 0
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		require.NoError(t, err)
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			tree, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			require.NoError(t, err)
			ast.Inspect(tree, func(node ast.Node) bool {
				f, ok := node.(*ast.Field)
				if !ok || f.Tag == nil {
					return true
				}
				tag, err := strconv.Unquote(f.Tag.Value)
				require.NoError(t, err)
				name := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
				for _, term := range []string{"cost", "multiplier", "quota", "pricing", "profit", "margin"} {
					if strings.Contains(name, term) {
						_, known := authz.FinancialFieldClass(name)
						require.True(t, known, "unclassified monetary field %s in %s", name, path)
						count++
						break
					}
				}
				return true
			})
		}
	}
	require.Greater(t, count, 80)
}

func TestAccountExtraCreationAndReplacementHaveDistinctAuthority(t *testing.T) {
	actor := authz.Subject{Role: authz.Admin, PolicyVersion: 1, Permissions: []string{"accounts.read", "accounts.manage", "accounts.authorize"}}
	_, err := authz.ValidateRequestFields("account.create", map[string]any{"extra": map[string]any{"org_uuid": "org", "openai_oauth_responses_websockets_v2_mode": "off"}}, actor)
	require.NoError(t, err, "normal OAuth metadata must remain usable without financial permissions")
	for _, schema := range []string{"account.create", "account.codex", "account.pat", "account.sso"} {
		_, err = authz.ValidateRequestFields(schema, map[string]any{"extra": map[string]any{"quota_limit": 0}}, actor)
		require.Error(t, err)
		_, err = authz.ValidateRequestFields(schema, map[string]any{"extra": map[string]any{"future_cost_field": 0}}, actor)
		require.Error(t, err)
	}
	for _, schema := range []string{"account.update", "account.bulk"} {
		for _, extra := range []any{map[string]any{}, nil, map[string]any{"org_uuid": "replacement"}} {
			_, err = authz.ValidateRequestFields(schema, map[string]any{"extra": extra}, actor)
			require.Error(t, err, "replacing projected extra must not clear hidden transport or financial fields")
		}
		_, err = authz.ValidateRequestFields(schema, map[string]any{"name": "basic edit"}, actor)
		require.NoError(t, err)
	}
}

func TestEditableFieldMetadataUsesTheSameBindingRules(t *testing.T) {
	actor := authz.Subject{Role: authz.Admin, PolicyVersion: 1, Permissions: []string{"groups.read", "groups.manage"}}
	fields := actor.Snapshot().WriteFields
	require.Contains(t, fields["group.update"], "name")
	require.NotContains(t, fields["group.update"], "peak_start")
	require.NotContains(t, fields["account.update"], "extra")
	actor.Permissions = append(actor.Permissions, "billing.rates.update")
	require.Contains(t, actor.Snapshot().WriteFields["group.update"], "peak_start")
}

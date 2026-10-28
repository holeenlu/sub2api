//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

// The real selection budget accounts each policy repository read, just as the
// storage instrumentation does. No new budget is created between stages.
type managedBudgetSettingsRepo struct{ *settingRepoStub }

func (r *managedBudgetSettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	if err := selectionbudget.Take(ctx, "sql", 0); err != nil {
		return "", err
	}
	return r.settingRepoStub.GetValue(ctx, key)
}
func managedBudgetPolicy(t *testing.T) (*SettingService, *settingRepoStub, *ModelPolicy) {
	t.Helper()
	repo := &settingRepoStub{values: map[string]string{}}
	p := modelPolicySeed(PlatformOpenAI)
	p.Platform = PlatformOpenAI
	p.Models["mapped-provider-model"] = ManagedModel{ID: "mapped-provider-model", Enabled: true}
	setConsumerPolicy(t, repo, p)
	raw, err := json.Marshal(ModelMappings{Mappings: map[string]string{"client-model": "mapped-provider-model"}})
	require.NoError(t, err)
	repo.values[ModelManagementKeyPrefix+"mappings"] = string(raw)
	return NewSettingService(&managedBudgetSettingsRepo{repo}, nil), repo, p
}
func TestManagedBudgetSingleStageReadsEachSectionOnce(t *testing.T) {
	for _, name := range []string{"client-model", "mapped-provider-model"} {
		t.Run(name, func(t *testing.T) {
			settings, _, _ := managedBudgetPolicy(t)
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{AccountModelReferencesKey: []string{"mapped-provider-model"}}}
			ctx, budget, cancel := selectionbudget.Start(t.Context())
			defer cancel()
			// Exactly two external operations are available, not the normal 64.
			ctx = selectionbudget.Reserve(ctx, selectionbudget.Limit-2)
			require.NoError(t, settings.checkManagedAccountModels(ctx, account, name, "mapped-provider-model"))
			require.Equal(t, 2, budget.Snapshot().SQL)
			require.Zero(t, selectionbudget.Remaining(ctx))
			require.False(t, budget.Snapshot().Exhausted)
			t.Logf("account policy: model=%s SQL=%d limit=2", name, budget.Snapshot().SQL)
		})
	}
}
func TestManagedBudgetNativeHTTPUsesSameBudgetAcrossStages(t *testing.T) {
	for _, name := range []string{"client-model", "mapped-provider-model"} {
		t.Run(name, func(t *testing.T) {
			settings, _, _ := managedBudgetPolicy(t)
			f := newNativeOperationFixture(t, true)
			f.service.settingService = settings
			f.op.settings = settings
			f.resource.Credentials[AccountModelReferencesKey] = []string{"mapped-provider-model"}
			f.resource.Credentials["model_mapping"] = map[string]any{}
			var received atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), `"model":"mapped-provider-model"`)
				received.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"resp_local_budget","status":"completed","model":"mapped-provider-model","usage":{"input_tokens":10,"output_tokens":2}}`)
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL + "/v1/responses")
			require.NoError(t, err)
			f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
				return http.DefaultTransport.RoundTrip(req)
			}
			ctx, budget, cancel := selectionbudget.Start(f.ctx)
			defer cancel()
			ctx = selectionbudget.Reserve(ctx, selectionbudget.Limit-12)
			channel := &ChannelService{modelSettings: settings}
			mapping, restricted := channel.ResolveChannelMappingAndRestrict(ctx, nil, name)
			require.False(t, restricted)
			require.Equal(t, "mapped-provider-model", mapping.MappedModel)
			channelSQL := 2
			if name == "mapped-provider-model" {
				channelSQL = 1
			} // Unclassified name needs only mapping lookup until account platform is known.
			require.Equal(t, channelSQL, budget.Snapshot().SQL)
			candidate, err := f.service.recheckSelectedOpenAIAccountFromDBBeforeProfit(ctx, f.resource, nil, PlatformOpenAI, mapping.MappedModel, false, "")
			require.NoError(t, err)
			require.NotNil(t, candidate)
			require.Equal(t, channelSQL+2, budget.Snapshot().SQL)
			_, err = f.service.planResponseExecution(ctx, candidate, mapping.MappedModel)
			require.NoError(t, err)
			require.Equal(t, channelSQL+4, budget.Snapshot().SQL)
			// Continue through the original production operation/permit, to a real local
			// HTTP server. Storage other than policy is provided by the existing fixture.
			req := f.request(t).WithContext(ctx)
			req.URL = endpoint
			req.Host = endpoint.Host
			response, err := f.service.doOpenAIUpstream(req, "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			_, err = io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.EqualValues(t, 1, received.Load())
			require.Equal(t, channelSQL+10, budget.Snapshot().SQL)
			require.False(t, budget.Snapshot().Exhausted)
			require.Equal(t, 2-channelSQL, selectionbudget.Remaining(ctx))
			t.Logf("real loopback HTTP: model=%s SQL=%d (channel=%d, selection=2, plan=2, Admit=2, Consume=2, GotConn=2), limit=12", name, budget.Snapshot().SQL, channelSQL)
		})
	}
}
func TestManagedBudgetDispatchFreshAfterAdmitRejectsRevocation(t *testing.T) {
	for _, change := range []string{"disabled_target", "disabled_rewrite", "mapping_retargeted"} {
		t.Run(change, func(t *testing.T) {
			settings, repo, policy := managedBudgetPolicy(t)
			f := newNativeOperationFixture(t, true)
			f.service.settingService = settings
			f.op.settings = settings
			f.resource.Credentials[AccountModelReferencesKey] = []string{"mapped-provider-model"}
			f.resource.Credentials["model_mapping"] = map[string]any{}
			ctx, budget, cancel := selectionbudget.Start(f.ctx)
			defer cancel()
			ctx = selectionbudget.Reserve(ctx, selectionbudget.Limit-4)
			prepared, target, err := f.op.prepare(f.request(t).WithContext(ctx), f.resource, "", false)
			require.NoError(t, err)
			defer prepared.Close()
			permit, err := f.op.dispatcher.Admit(ctx, prepared.Identity(), target)
			require.NoError(t, err)
			require.Equal(t, 2, budget.Snapshot().SQL)
			switch change {
			case "disabled_target":
				row := policy.Models["mapped-provider-model"]
				row.Enabled = false
				policy.Models["mapped-provider-model"] = row
			case "disabled_rewrite":
				policy.Models["blocked"] = ManagedModel{ID: "blocked", Enabled: false}
				f.resource.Credentials["model_mapping"] = map[string]any{"mapped-provider-model": "blocked"}
			case "mapping_retargeted":
				policy.Models["blocked"] = ManagedModel{ID: "blocked", Enabled: false}
				raw, err := json.Marshal(ModelMappings{Mappings: map[string]string{"client-model": "blocked"}})
				require.NoError(t, err)
				repo.values[ModelManagementKeyPrefix+"mappings"] = string(raw)
			}
			setConsumerPolicy(t, repo, policy)
			err = permit.Consume(ctx, prepared.Identity())
			require.ErrorIs(t, err, ErrManagedModelNotFound)
			require.Equal(t, 4, budget.Snapshot().SQL)
			require.Zero(t, f.transport.sends.Load())
		})
	}
}

func TestManagedBudgetManifestAndCatalogDoNotReadPerModel(t *testing.T) {
	for _, size := range []int{1, 128, 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			settings, repo, policy := managedBudgetPolicy(t)
			policy.Models["blocked"] = ManagedModel{ID: "blocked", Enabled: false}
			setConsumerPolicy(t, repo, policy)
			entries := make([]map[string]any, 0, size+1)
			for i := 0; i < size; i++ {
				entries = append(entries, map[string]any{"id": "mapped-provider-model", "index": i, "capability": map[string]any{"supports_tool": true}})
			}
			entries = append(entries, map[string]any{"id": "blocked"})
			body, err := json.Marshal(map[string]any{"models": entries, "data": entries, "meta": "preserved"})
			require.NoError(t, err)
			ctx, budget, cancel := selectionbudget.Start(t.Context())
			defer cancel()
			ctx = selectionbudget.Reserve(ctx, selectionbudget.Limit-2)
			filtered, err := settings.FilterManagedManifest(ctx, PlatformOpenAI, body)
			require.NoError(t, err)
			var result struct {
				Models, Data []map[string]any
				Meta         string
			}
			require.NoError(t, json.Unmarshal(filtered, &result))
			require.Len(t, result.Models, size)
			require.Len(t, result.Data, size)
			require.Equal(t, "preserved", result.Meta)
			require.Equal(t, 2, budget.Snapshot().SQL)
			require.Zero(t, selectionbudget.Remaining(ctx))
			t.Logf("manifest %d rows in each of models/data: SQL=%d limit=2", size, budget.Snapshot().SQL)
			// A new listing phase gets a fresh pair, then all row/account/reference
			// expansion remains pure. The central catalog reader consumes that pair's
			// cache publications rather than querying again for every row.
			ctx2, budget2, cancel2 := selectionbudget.Start(t.Context())
			defer cancel2()
			ctx2 = selectionbudget.Reserve(ctx2, selectionbudget.Limit-2)
			account := Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{AccountModelReferencesKey: []string{"mapped-provider-model"}}}
			ids, err := settings.modelIDsForAccounts(ctx2, PlatformOpenAI, []Account{account})
			require.NoError(t, err)
			require.Contains(t, ids, "mapped-provider-model")
			require.Contains(t, ids, "client-model")
			require.NotContains(t, ids, "blocked")
			require.Equal(t, 2, budget2.Snapshot().SQL)
			rows, err := settings.GetManagedModelCatalog(ctx2, PlatformOpenAI)
			require.NoError(t, err)
			require.NotEmpty(t, rows)
			require.Equal(t, 2, budget2.Snapshot().SQL)
		})
	}
}

type managedBatchFailureRepo struct{ *managedBudgetSettingsRepo }

func (r *managedBatchFailureRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == ModelManagementKeyPrefix+"mappings" {
		if err := selectionbudget.Take(ctx, "sql", 0); err != nil {
			return "", err
		}
		return "", errors.New("synthetic policy dependency unavailable")
	}
	return r.managedBudgetSettingsRepo.GetValue(ctx, key)
}
func TestManagedBudgetBatchDependencyFailureReturnsNoPartialResult(t *testing.T) {
	settings, repo, _ := managedBudgetPolicy(t)
	settings.settingRepo = &managedBatchFailureRepo{&managedBudgetSettingsRepo{repo}}
	ctx, budget, cancel := selectionbudget.Start(t.Context())
	defer cancel()
	ctx = selectionbudget.Reserve(ctx, selectionbudget.Limit-2)
	body, err := settings.FilterManagedManifest(ctx, PlatformOpenAI, []byte(`{"data":[{"id":"mapped-provider-model"},{"id":"client-model"}]}`))
	require.ErrorIs(t, err, ErrModelPolicyUnavailable)
	require.Nil(t, body)
	require.Equal(t, 2, budget.Snapshot().SQL)
}

func TestManagedBudgetCapabilityBatchSharesValuesThenRechecksFresh(t *testing.T) {
	settings, repo, policy := managedBudgetPolicy(t)
	f := newNativeOperationFixture(t, true)
	f.service.settingService = settings
	f.op.settings = settings
	f.resource.Credentials[AccountModelReferencesKey] = []string{"mapped-provider-model"}
	f.resource.Credentials["model_mapping"] = map[string]any{}
	ctx, budget, cancel := selectionbudget.Start(f.ctx)
	defer cancel()
	ctx = selectionbudget.Reserve(ctx, selectionbudget.Limit-4)
	snapshot, err := settings.managedModelSnapshot(ctx, PlatformOpenAI, "mapped-provider-model")
	require.NoError(t, err)
	scheduler := &defaultOpenAIAccountScheduler{service: f.service}
	req := OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "mapped-provider-model", RequiredCapability: OpenAIEndpointCapabilityResponses, managedModels: map[string]managedModelSnapshot{PlatformOpenAI: snapshot}}
	for i := 0; i < 128; i++ {
		compatible, reason := scheduler.isAccountRequestCompatibleReason(ctx, f.resource, req)
		require.True(t, compatible, reason)
	}
	require.Equal(t, 2, budget.Snapshot().SQL)
	row := policy.Models["mapped-provider-model"]
	row.Enabled = false
	policy.Models["mapped-provider-model"] = row
	setConsumerPolicy(t, repo, policy)
	req.managedModels = nil // The next stage must not retain a capability snapshot.
	compatible, _ := scheduler.isAccountRequestCompatibleReason(ctx, f.resource, req)
	require.False(t, compatible)
	require.Equal(t, 4, budget.Snapshot().SQL)
	t.Logf("128 capability checks: SQL=2; next phase sees revoke: cumulative SQL=%d", budget.Snapshot().SQL)
}

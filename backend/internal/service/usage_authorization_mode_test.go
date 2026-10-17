package service

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type authorizationUsageBillingRepo struct {
	UsageBillingRepository
	commands map[string]*UsageBillingCommand
}

func (r *authorizationUsageBillingRepo) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	if previous := r.commands[cmd.RequestID]; previous != nil {
		if previous.RequestFingerprint != cmd.RequestFingerprint {
			return nil, ErrUsageBillingRequestConflict
		}
		return &UsageBillingApplyResult{Applied: false}, nil
	}
	r.commands[cmd.RequestID] = cmd
	return &UsageBillingApplyResult{Applied: true}, nil
}

func TestOpenAIUsageAuthorizationModeSameAccountRecovery(t *testing.T) {
	for _, ws := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "ws"}[ws], func(t *testing.T) {
			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			billing := &authorizationUsageBillingRepo{commands: make(map[string]*UsageBillingCommand)}
			svc.usageBillingRepo = billing
			owner := int64(19)
			account := &Account{ID: 3, VendorID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Extra: map[string]any{"openai_excel_bps": true}}
			previous := &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeBPS, Model: "gpt-5.1", RequestID: "bps-partial", OpenAIWSMode: ws,
				UpstreamEndpoint: "/basispoints/api/responses", Usage: OpenAIUsage{InputTokens: 120, OutputTokens: 30}}
			result := &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeNative, Model: "gpt-5.1", RequestID: "native-final", OpenAIWSMode: ws,
				UpstreamEndpoint: "/v1/responses", Usage: OpenAIUsage{InputTokens: 400, OutputTokens: 50}}
			finishExcelBPSNativeFallback(&result, previous, account)
			require.Equal(t, 520, result.Usage.InputTokens)
			// Async accounting observes the captured result even after switches change.
			account.Extra["openai_excel_bps"] = false
			ctx := context.WithValue(t.Context(), ctxkey.ClientRequestID, "shared-client-id")
			input := &OpenAIRecordUsageInput{Result: result, Account: account, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, PricingAt: time.Now()}
			for range 2 {
				require.NoError(t, svc.RecordUsage(ctx, input))
			}
			require.Len(t, billing.commands, 1, "both authorization facts share the original, idempotent customer bill")
			require.Len(t, repo.logs, 4)
			require.Equal(t, repo.logs[0].RequestID, repo.logs[2].RequestID)
			require.Equal(t, repo.logs[1].RequestID, repo.logs[3].RequestID)
			require.NotEqual(t, repo.logs[0].RequestID, repo.logs[1].RequestID)
			require.Equal(t, UsageAuthorizationModeBPS, repo.logs[0].AuthorizationMode)
			require.Equal(t, UsageAuthorizationModeNative, repo.logs[1].AuthorizationMode)
			require.Equal(t, 120, repo.logs[0].InputTokens)
			require.Equal(t, 400, repo.logs[1].InputTokens)
			for _, log := range repo.logs {
				require.Equal(t, owner, *log.VendorIDSnapshot)
				require.LessOrEqual(t, len(log.RequestID), 64)
			}
			want := expectedOpenAICost(t, svc, "gpt-5.1", OpenAIUsage{InputTokens: 520, OutputTokens: 80}, 1.1)
			require.InDelta(t, want.TotalCost, repo.logs[0].TotalCost+repo.logs[1].TotalCost, 1e-12)
			require.Equal(t, 520, result.Usage.InputTokens, "recording does not mutate the downstream combined measurement")
		})
	}
}

func TestOpenAIUsageUnknownDoesNotInferFromAccount(t *testing.T) {
	repo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	input := &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{Model: "gpt-5.1", RequestID: "unknown", Usage: OpenAIUsage{InputTokens: 10}},
		Account: &Account{ID: 3, Platform: PlatformOpenAI, Extra: map[string]any{"openai_excel_bps": true}}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}}
	require.NoError(t, svc.RecordUsage(t.Context(), input))
	require.Empty(t, repo.lastLog.AuthorizationMode)
	require.Equal(t, "unknown", DisplayUsageAuthorizationMode(repo.lastLog.AuthorizationMode))
	require.Nil(t, UsageAuthorizationModePtr(repo.lastLog.AuthorizationMode))
}

func TestOpenAIUsageAuthorizationModePreservesPricingAndBillingIdentity(t *testing.T) {
	for _, pricing := range []string{"long_context", "per_request", "cache_write_as_input"} {
		t.Run(pricing, func(t *testing.T) {
			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			swapInOpenAILadderCatalog(t, svc)
			billing := &authorizationUsageBillingRepo{commands: make(map[string]*UsageBillingCommand)}
			svc.usageBillingRepo = billing
			key := openAIRecordUsageAPIKeyWithGroup(svc, 2, true)
			key.GroupID = i64p(1)
			cache := newEmptyChannelCache()
			cache.channelByGroupID[1] = &Channel{ID: 1, Status: StatusActive}
			cache.groupPlatform[1], cache.loadedAt = PlatformOpenAI, time.Now()
			svc.channelService = &ChannelService{}
			if pricing == "per_request" {
				price := 0.25
				cache.pricingByGroupModel[channelModelKey{groupID: 1, platform: PlatformOpenAI, model: "gpt-5.4"}] = &ChannelModelPricing{BillingMode: BillingModePerRequest, PerRequestPrice: &price}
			}
			svc.channelService.cache.Store(cache)
			svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
			account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_long_context_billing_enabled": true, "openai_excel_bps": true}}
			prior := &OpenAIForwardResult{AuthorizationMode: "bps", Model: "gpt-5.4", UpstreamEndpoint: "/basispoints/api/responses", Usage: OpenAIUsage{InputTokens: 180000, OutputTokens: 500}}
			if pricing == "cache_write_as_input" {
				account.Extra["openai_excel_bps_cache_creation_as_input"] = true
				prior.Usage.CacheCreationInputTokens = 30000
			}
			result := &OpenAIForwardResult{AuthorizationMode: "native", Model: "gpt-5.4", RequestID: "combined", UpstreamEndpoint: "/v1/responses", Usage: OpenAIUsage{InputTokens: 120000, OutputTokens: 1500}}
			finishExcelBPSNativeFallback(&result, prior, account)
			if pricing == "cache_write_as_input" {
				require.Zero(t, result.PriorAuthorizationResults[0].Usage.CacheCreationInputTokens)
			}
			baseline := *result
			baseline.PriorAuthorizationResults, baseline.authorizationUsage, baseline.RequestID = nil, nil, "baseline"
			input := &OpenAIRecordUsageInput{Account: account, User: &User{ID: 1}, APIKey: key, Result: &baseline, PricingAt: time.Now()}
			require.NoError(t, svc.RecordUsage(t.Context(), input))
			input.Result = result
			require.NoError(t, svc.RecordUsage(t.Context(), input))
			require.Len(t, billing.commands, 2)
			require.Equal(t, billing.commands["baseline"].RequestFingerprint, billing.commands["combined"].RequestFingerprint,
				"mode attribution must not change the original debit, quotas or billing fingerprint")
			require.Len(t, repo.logs, 3)
			original, bps, native := repo.logs[0], repo.logs[1], repo.logs[2]
			require.InDelta(t, original.TotalCost, bps.TotalCost+native.TotalCost, 1e-10)
			require.InDelta(t, original.ActualCost, bps.ActualCost+native.ActualCost, 1e-10)
			require.InDelta(t, *original.AccountStatsCost, *bps.AccountStatsCost+*native.AccountStatsCost, 1e-10)
			require.Equal(t, original.TotalTokens(), bps.TotalTokens()+native.TotalTokens())
			require.Positive(t, bps.TotalCost)
			require.Positive(t, native.TotalCost)
			if pricing == "per_request" {
				require.InDelta(t, 0.25, original.TotalCost, 1e-10)
			}
		})
	}
	// Native can fail before any upstream usage. Keep only the actual BPS fact.
	prior := &OpenAIForwardResult{AuthorizationMode: "bps", Usage: OpenAIUsage{InputTokens: 7}}
	var failed *OpenAIForwardResult
	finishExcelBPSNativeFallback(&failed, prior, &Account{})
	require.Same(t, prior, failed)
	require.Empty(t, failed.PriorAuthorizationResults)
}

func TestVendorUsagePostgresAuthorizationModeSnapshots(t *testing.T) {
	s, admin, supplier := vendorSettlementPostgres(t)
	for _, mode := range []string{"native", "bps", ""} {
		_, err := s.db.Exec(`INSERT INTO usage_logs(vendor_id_snapshot,account_id,account_name_snapshot,platform_snapshot,created_at,model,total_cost,request_id,api_key_id,authorization_mode)
 VALUES(1,11,'frozen','openai','2026-10-01T12:00:00Z','gpt-5.1',2,$1,1,NULLIF($2,''))`, "request-"+mode, mode)
		require.NoError(t, err)
	}
	_, err := s.db.Exec("UPDATE accounts SET extra='{}',platform='anthropic' WHERE id=11")
	require.NoError(t, err)
	_, err = s.db.Exec("UPDATE usage_logs SET authorization_mode='bps' WHERE authorization_mode IS NULL")
	require.ErrorContains(t, err, "immutable", "historical unknown must never be backfilled from switches")
	_, err = s.db.Exec("UPDATE usage_logs SET authorization_mode='bps' WHERE authorization_mode='native'")
	require.ErrorContains(t, err, "immutable")
	f := VendorUsageFilter{VendorID: 1, StartDate: "2026-10-01", EndDate: "2026-10-01", Timezone: "UTC"}
	all, err := s.VendorUsage(supplier, f, false)
	require.NoError(t, err)
	require.Equal(t, int64(3), all.Summary.Requests)
	require.Equal(t, "mixed", all.Items[0].AuthorizationMode)
	for _, mode := range []string{"native", "bps"} {
		f.AuthorizationMode = mode
		got, err := s.VendorUsage(supplier, f, false)
		require.NoError(t, err)
		require.Equal(t, int64(1), got.Summary.Requests)
		require.Equal(t, mode, got.Items[0].AuthorizationMode)
		require.Equal(t, "2", got.Summary.CostUSD)
		data, err := s.VendorUsageCSV(admin, f)
		require.NoError(t, err)
		rows, err := csv.NewReader(strings.NewReader(string(data[3:]))).ReadAll()
		require.NoError(t, err)
		require.Len(t, rows, 3)
		require.Equal(t, "authorization_mode", rows[0][len(rows[0])-1])
		require.Equal(t, mode, rows[1][len(rows[1])-1])
		require.Equal(t, mode, rows[2][len(rows[2])-1])
		require.Equal(t, "2", rows[2][16], "CSV totals must use the same mode-filtered snapshot")
	}
	f.AuthorizationMode, f.Dimension = "", "request"
	got, err := s.VendorUsage(admin, f, true)
	require.NoError(t, err)
	modes := []string{}
	for _, row := range got.Items {
		modes = append(modes, row.AuthorizationMode)
	}
	require.ElementsMatch(t, []string{"native", "bps", "unknown"}, modes)
}

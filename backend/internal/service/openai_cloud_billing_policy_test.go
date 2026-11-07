package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCloudBillingV2FrozenPolicy(t *testing.T) {
	for _, mode := range []string{"log_only", "key_window_only", "full_billing"} {
		t.Run(mode, func(t *testing.T) {
			svc, input, repo := cloudBillingFixture(t)
			ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
			require.NoError(t, err)
			var wire map[string]any
			require.NoError(t, json.Unmarshal(CloudBillingSnapshotFromContext(ctx), &wire))
			wire["version"], wire["billing_mode"] = 2, mode
			wire["simple_mode"] = mode == "full_billing" // v2 must not infer policy from this legacy field.
			raw, err := json.Marshal(wire)
			require.NoError(t, err)
			// Current configuration cannot reclassify an accepted turn after a restart.
			svc.cfg.RunMode = config.RunModeSimple
			svc.cfg.SimpleModeKeyRateLimitEnabled = mode != "key_window_only"
			settlement := &OpenAICloudSettlement{
				OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "frozen-policy"},
				BillingSnapshot:    raw, Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 1_000_000, TotalTokens: 1_000_000}, ObservedAt: input.PricingAt.Add(time.Minute),
			}
			require.NoError(t, (&openAICloudBillingWorker{service: svc}).settle(context.Background(), settlement))
			logs := svc.usageLogRepo.(*openAIRecordUsageLogRepoStub)
			if mode == "log_only" {
				require.Zero(t, logs.calls, "delayed usage must use the durable dedup/log transaction")
				require.Equal(t, 1, repo.calls)
				requireCloudLogOnlyCommand(t, repo.lastCmd)
				return
			}
			require.Equal(t, 1, logs.calls)
			require.Equal(t, 1, repo.calls)
			if mode == "key_window_only" {
				require.Positive(t, repo.lastCmd.APIKeyRateLimitCost)
				require.Zero(t, repo.lastCmd.BalanceCost)
				require.Zero(t, repo.lastCmd.APIKeyQuotaCost)
				require.Zero(t, repo.lastCmd.AccountQuotaCost)
				require.Zero(t, repo.lastCmd.SubscriptionCost)
			} else {
				require.InDelta(t, 4, repo.lastCmd.BalanceCost, 1e-9)
				require.Positive(t, repo.lastCmd.APIKeyQuotaCost)
			}
		})
	}
}

func TestCloudBillingSnapshotWriterFencesLegacySamplingConsumers(t *testing.T) {
	svc, input, _ := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(CloudBillingSnapshotFromContext(ctx), &wire))
	require.Equal(t, float64(3), wire["version"], "v1/v2 consumers ignore sampling facts and would price the cumulative root")
	require.NotContains(t, wire, "billing_mode", "the sampling fence preserves v1 frozen policy; it does not enable the separate v2 policy rollout")
}

func TestCloudBillingLegacyPolicyIgnoresCurrentConfiguration(t *testing.T) {
	for _, legacySimple := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "windows"}[legacySimple], func(t *testing.T) {
			svc, input, repo := cloudBillingFixture(t)
			if legacySimple {
				svc.cfg.RunMode = config.RunModeSimple
			}
			ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
			require.NoError(t, err)
			cloudBillingContext(ctx).Version = 1 // Persisted v1 policy survives the v3 writer rollout.
			svc.cfg.RunMode, svc.cfg.SimpleModeKeyRateLimitEnabled = config.RunModeSimple, !legacySimple
			settlement := &OpenAICloudSettlement{
				OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "legacy-policy"},
				BillingSnapshot:    CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 1_000_000, TotalTokens: 1_000_000}, ObservedAt: input.PricingAt.Add(time.Minute),
			}
			require.NoError(t, (&openAICloudBillingWorker{service: svc}).settle(t.Context(), settlement))
			require.Equal(t, 1, repo.calls)
			if legacySimple {
				require.Zero(t, repo.lastCmd.BalanceCost)
				require.Positive(t, repo.lastCmd.APIKeyRateLimitCost)
			} else {
				require.Positive(t, repo.lastCmd.BalanceCost)
			}
		})
	}
}

func requireCloudLogOnlyCommand(t *testing.T, cmd *UsageBillingCommand) {
	t.Helper()
	require.NotNil(t, cmd)
	require.NotNil(t, cmd.UsageLog)
	require.Positive(t, cmd.UsageLog.InputTokens)
	require.Zero(t, cmd.BalanceCost)
	require.Zero(t, cmd.SubscriptionCost)
	require.Zero(t, cmd.APIKeyQuotaCost)
	require.Zero(t, cmd.APIKeyRateLimitCost)
	require.Zero(t, cmd.AccountQuotaCost)
	require.Zero(t, cmd.UserPlatformQuotaCost())
	require.Empty(t, cmd.Platform)
	require.Nil(t, cmd.Pricing)
	require.Nil(t, cmd.SubscriptionID)
}

func TestCloudBillingV2LogOnlyUsesDurableSettlement(t *testing.T) {
	svc, input, repo := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	snapshot := cloudBillingContext(ctx)
	snapshot.Version, snapshot.BillingMode = 2, UsageBillingModeLogOnly
	settlement := &OpenAICloudSettlement{
		OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "log-only-retry"},
		BillingSnapshot:    CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}, ObservedAt: input.PricingAt.Add(time.Minute),
	}
	store := &cloudBillingStoreStub{pending: []*OpenAICloudSettlement{settlement}}
	worker := &openAICloudBillingWorker{service: svc, store: store, id: "worker", holds: make(map[string]openAICloudHeldReservation)}
	repo.err = errors.New("ledger temporarily unavailable")
	worker.consume(t.Context())
	require.Equal(t, 1, repo.calls)
	require.Zero(t, store.acks, "a failed durable log transaction must not be acknowledged")
	requireCloudLogOnlyCommand(t, repo.lastCmd)
	fingerprint := repo.lastCmd.RequestFingerprint
	require.NotEmpty(t, fingerprint)
	repo.err = nil
	repo.result = &UsageBillingApplyResult{Applied: true}
	store.ackError = errors.New("ACK lost after commit")
	worker.consume(t.Context())
	require.Equal(t, 2, repo.calls)
	require.False(t, store.acked[settlement.RootRequestID])
	require.Equal(t, fingerprint, repo.lastCmd.RequestFingerprint)
	repo.result = &UsageBillingApplyResult{Applied: false}
	store.ackError = nil
	worker.consume(t.Context())
	require.Equal(t, 3, repo.calls)
	require.True(t, store.acked[settlement.RootRequestID])
	require.Equal(t, "cloud:log-only-retry", repo.lastCmd.RequestID)
	require.Equal(t, fingerprint, repo.lastCmd.RequestFingerprint, "lost ACK must retry the same existing-ledger key")
	requireCloudLogOnlyCommand(t, repo.lastCmd)
	require.Zero(t, svc.usageLogRepo.(*openAIRecordUsageLogRepoStub).calls, "never append a separate best-effort log on error or replay")
	worker.consume(t.Context())
	require.Equal(t, 3, repo.calls)
}

func TestCloudBillingV2PolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  int
		mode     any
		noLimits bool
	}{
		{"missing", 2, nil, false}, {"unknown", 2, "everything", false}, {"wrong_type", 2, true, false}, {"unknown_version", 99, "full_billing", false}, {"window_without_limits", 2, "key_window_only", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, input, repo := cloudBillingFixture(t)
			ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
			require.NoError(t, err)
			var wire map[string]any
			require.NoError(t, json.Unmarshal(CloudBillingSnapshotFromContext(ctx), &wire))
			wire["version"], wire["billing_mode"] = tc.version, tc.mode
			if tc.noLimits {
				wire["key_rate_limit_5h"], wire["key_rate_limit_1d"], wire["key_rate_limit_7d"] = 0, 0, 0
			}
			raw, err := json.Marshal(wire)
			require.NoError(t, err)
			settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "invalid-policy"}, BillingSnapshot: raw, Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}}
			require.ErrorIs(t, (&openAICloudBillingWorker{service: svc}).settle(t.Context(), settlement), cloudPermanentSettlementError("invalid_settlement"))
			require.Zero(t, repo.calls)
		})
	}
}

func TestUsageBillingPolicyModes(t *testing.T) {
	for _, simple := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, hasLimits := range []bool{false, true} {
				cfg := &config.Config{SimpleModeKeyRateLimitEnabled: enabled}
				if simple {
					cfg.RunMode = config.RunModeSimple
				}
				key := &APIKey{}
				if hasLimits {
					key.RateLimit1d = 10
				}
				expected := UsageBillingModeFull
				if simple {
					expected = UsageBillingModeLogOnly
					if enabled && hasLimits {
						expected = UsageBillingModeKeyWindowOnly
					}
				}
				require.Equal(t, expected, resolveUsageBillingMode(cfg, key), "simple=%v enabled=%v limits=%v", simple, enabled, hasLimits)
			}
		}
	}
	require.Equal(t, UsageBillingModeFull, resolveUsageBillingMode(nil, nil))
	require.Equal(t, UsageBillingModeLogOnly, resolveUsageBillingMode(&config.Config{RunMode: config.RunModeSimple, SimpleModeKeyRateLimitEnabled: true}, nil))
}

func TestCloudBillingV2ResponseModelUsesFrozenEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		version                        int
		missingPrice, conflict, hosted bool
		source                         string
		inputPrice                     float64
		identified                     bool
		want                           float64
	}{
		{name: "cheaper_frozen_group", version: 2, source: PricingSourceGroup, inputPrice: 1e-6, want: 2},
		{name: "old_snapshot_keeps_policy", version: 1, source: PricingSourceGroup, inputPrice: 1e-6, want: 4},
		{name: "missing_frozen_price", version: 2, missingPrice: true, want: 4},
		{name: "conflicting_declaration", version: 2, conflict: true, source: PricingSourceGroup, inputPrice: 1e-6, want: 4},
		{name: "higher_price", version: 2, source: PricingSourceGroup, inputPrice: 6e-6, want: 4},
		{name: "free_substitution", version: 2, source: PricingSourceGroup, inputPrice: 0, want: 4},
		{name: "unidentified_fallback", version: 2, source: PricingSourceFallback, inputPrice: 1e-6, want: 4},
		{name: "cannot_cross_configured_price", version: 2, source: PricingSourceFallback, identified: true, inputPrice: 1e-6, want: 4},
		{name: "hosted_mixed_not_substituted", version: 2, hosted: true, source: PricingSourceGroup, inputPrice: 1e-6, want: 4.02},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, input, repo := cloudBillingFixture(t)
			ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
			require.NoError(t, err)
			snapshot := cloudBillingContext(ctx)
			snapshot.Version = tc.version
			if tc.version == 2 {
				snapshot.BillingMode = UsageBillingModeFull
			}
			if !tc.missingPrice {
				snapshot.Prices["reported-model"] = openAICloudPriceSnapshot{Resolved: ResolvedPricing{Source: tc.source, Mode: BillingModeToken, BasePricing: &ModelPricing{InputPricePerToken: tc.inputPrice}}, ResponsePriceIdentified: tc.identified}
			}
			ctx = context.WithValue(ctx, openAICloudSettlingContextKey{}, true)
			input.BillingModelSource = BillingModelSourceResponse
			input.Result.RequestID = "response-price"
			input.Result.UpstreamResponseModel = "reported-model"
			input.Result.UpstreamResponseModelConflict = tc.conflict
			input.Result.Usage = OpenAIUsage{InputTokens: 1_000_000}
			if tc.hosted {
				input.Result.WebSearchCalls = 1
			}
			input.APIKeyService = &cloudBillingKeyStub{key: input.APIKey}
			// A changed live card must not substitute for immutable price evidence.
			*input.APIKey.Group.ModelPricing[0].InputPrice = 99
			require.NoError(t, svc.RecordUsage(ctx, input))
			require.InDelta(t, tc.want, repo.lastCmd.BalanceCost, 1e-9)
		})
	}
}

func TestCloudBillingV2PreviousTurnInheritsReasoningEffort(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mode         UsageBillingMode
		noLimits     bool
		wrongOwner   bool
		wrongModel   bool
		wantRejected bool
	}{
		{name: "full", mode: UsageBillingModeFull},
		{name: "log_only", mode: UsageBillingModeLogOnly},
		{name: "key_windows", mode: UsageBillingModeKeyWindowOnly},
		{name: "missing_mode", wantRejected: true},
		{name: "unknown_mode", mode: "invalid", wantRejected: true},
		{name: "windows_without_limits", mode: UsageBillingModeKeyWindowOnly, noLimits: true, wantRejected: true},
		{name: "different_owner", mode: UsageBillingModeFull, wrongOwner: true, wantRejected: true},
		{name: "different_model", mode: UsageBillingModeFull, wrongModel: true, wantRejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, input, _ := cloudBillingFixture(t)
			previousCtx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
			require.NoError(t, err)
			previousCtx, err = svc.FinalizeCloudBillingSnapshot(previousCtx, "gpt-5.1", "", "high")
			require.NoError(t, err)
			previous := cloudBillingContext(previousCtx)
			previous.Version, previous.BillingMode = 2, tc.mode
			if tc.noLimits {
				previous.KeyRateLimit5h, previous.KeyRateLimit1d, previous.KeyRateLimit7d = 0, 0, 0
			}
			if tc.wrongOwner {
				previous.UserID++
			}
			if tc.wrongModel {
				previous.UpstreamModel = "another-model"
			}
			owner := OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}
			binding := OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "session", RootRequestID: "previous-root", Status: "completed"}
			store := &cloudBillingInheritanceStore{record: &OpenAICloudSessionRecord{OpenAICloudBinding: binding, BillingSnapshot: CloudBillingSnapshotFromContext(previousCtx)}}
			svc.cloudSessions = &OpenAICloudSessions{store: store}
			protocol := cloudBillingProtocol(t, []byte("{\"model\":\"gpt-5.1\",\"previous_response_id\":\"resp_cloud_old\",\"input\":\"next\"}"))
			effort, err := svc.resolveCloudBillingEffortPlan(t.Context(), &binding, owner, protocol)
			if tc.wantRejected {
				require.Error(t, err)
				require.Empty(t, effort)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "high", effort)
			require.Empty(t, protocol.effort, "billing must not manufacture a partial reasoning object")
			_, supplied := protocol.source["reasoning"]
			require.False(t, supplied, "the actor must still inherit the full reasoning object")
		})
	}
}

// A consumer that cannot interpret a snapshot must retain the exact source
// bytes, including sampling facts it cannot decode, without a debit or ACK.
func TestCloudBillingUnsupportedVersionPreservesSamplingFacts(t *testing.T) {
	svc, input, ledger := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	snapshot := cloudBillingContext(ctx)
	snapshot.Version = 4
	settlement := &OpenAICloudSettlement{
		OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "future-sampling"},
		BillingSnapshot:    CloudBillingSnapshotFromContext(ctx), Status: "completed",
		Usage:         &OpenAICloudUsage{InputTokens: 20, TotalTokens: 20},
		SamplingUsage: []OpenAICloudUsage{{InputTokens: 10, TotalTokens: 10}, {InputTokens: 10, TotalTokens: 10}},
		ObservedAt:    input.PricingAt.Add(time.Minute),
	}
	raw, err := json.Marshal(settlement)
	require.NoError(t, err)
	settlement.ArchivePayload = raw
	store := &cloudBillingStoreStub{pending: []*OpenAICloudSettlement{settlement}}
	worker := &openAICloudBillingWorker{service: svc, store: store, id: "old-reader"}
	worker.consume(t.Context())
	require.Zero(t, ledger.calls)
	require.Zero(t, store.acks)
	require.True(t, store.archived[settlement.RootRequestID])
	archive := svc.usageBillingRepo.(*cloudBillingArchiveRepoStub)
	require.Equal(t, raw, archive.archives[settlement.RootRequestID])
	require.Equal(t, "invalid_settlement", archive.reasons[settlement.RootRequestID])
	var retained OpenAICloudSettlement
	require.NoError(t, json.Unmarshal(archive.archives[settlement.RootRequestID], &retained))
	require.Equal(t, settlement.SamplingUsage, retained.SamplingUsage)
}

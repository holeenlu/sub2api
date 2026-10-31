//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Public billing adapter + explicit synthetic frozen-input/realPG. The policy
// and prepared identities are component-test inputs, not runtime capture or
// send-permit evidence. Prices use the public pinned fallback resolver; the
// original application builder and real PG ledger produce every money command
// and receipt. No network/upstream, replacement builder or fabricated ACK.
func nativePublicAdapterPGFixture(t *testing.T, runMode ...string) (*service.OpenAIGatewayService, *service.NativeUsageEvidenceService, *usageBillingRepository, service.NativeOperationFinalization) {
	t.Helper()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Balance: 100})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "native-public-adapter"})
	account := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString(), Type: service.AccountTypeAPIKey, Platform: service.PlatformOpenAI})
	repo := NewUsageBillingRepository(client, integrationDB).(*usageBillingRepository)
	cfg := &config.Config{}
	if len(runMode) > 0 {
		cfg.RunMode = runMode[0]
	}
	cfg.Default.RateMultiplier = 1
	prices := service.NewBillingService(cfg, nil)
	// Existing static fallback prices; no pricing download, upstream, Redis,
	// notification sender or background lifecycle owner is started here.
	gateway := service.NewOpenAIGatewayService(
		NewAccountRepository(client, integrationDB, nil), NewUsageLogRepository(client, integrationDB), repo,
		nil, nil, nil, nil, cfg, nil, nil, prices,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	evidence, err := service.ProvideNativeUsageEvidenceService(repo)
	require.NoError(t, err)
	id := billing.Identity{RequestID: uuid.NewString(), APIKeyID: key.ID}
	opID := "operation:" + uuid.NewString()
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM usage_billing_events WHERE api_key_id=$1 AND ((event_type='native_intent' AND operation_id=$2) OR request_id=$3)", key.ID, opID, id.RequestID)
		require.NoError(t, err)
	})
	wire := sha256.Sum256([]byte("native fixture original wire"))
	prepared := model.PreparedIdentity{OperationID: opID, ResourceID: account.ID, Owner: model.ResourceOwner{AccountID: account.ID},
		UserID: user.ID, APIKeyID: key.ID, Mode: "native", Transport: "http", GrantDigest: wire, TargetDigest: wire,
		WireDigest: wire, BodyKind: model.BodyReplayable, BodyDigest: wire}
	first := model.AttemptFact{Sequence: 1, Identity: prepared, Send: model.SendAmbiguous, Closed: true}
	prepared.TargetDigest = sha256.Sum256([]byte("native fixture final target"))
	requestID, responseID := "upstream:"+uuid.NewString(), "response:"+uuid.NewString()
	final := model.AttemptFact{Sequence: 2, Identity: prepared, Send: model.SendSent, RequestID: requestID, ResponseID: responseID,
		Terminal: model.TerminalCompleted, Closed: true, EndOfStream: true,
		Usage: &model.UsageFact{Input: &model.TokenMeasurement{Value: 1000}, Output: &model.TokenMeasurement{Value: 12},
			CacheRead: &model.TokenMeasurement{Value: 100}, CacheWrite: &model.TokenMeasurement{Value: 200},
			Confidence: model.UsageFinal, TerminalConfirmed: true, ResponseID: responseID, ReturnedModel: "gpt-5.1"}}
	input := service.NativeOperationFinalization{Identity: id,
		Operation: model.OperationResult{OperationID: opID, UserID: user.ID, APIKeyID: key.ID, Protocol: model.ProtocolResponses,
			OriginalWireDigest: wire, Attempts: []model.AttemptFact{first, final}}, CurrentSequences: []uint64{2},
		UsageInput: &service.OpenAIRecordUsageInput{APIKey: key, User: user, Account: account,
			PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
			Result: &service.OpenAIForwardResult{RequestID: requestID, ResponseID: responseID, Model: "gpt-5.1",
				Usage: service.OpenAIUsage{InputTokens: 1000, OutputTokens: 12, CacheReadInputTokens: 100, CacheCreationInputTokens: 200}}}}
	intent := service.NativeUsageIntent{Key: billing.NativeIntentKey{OperationID: opID, APIKeyID: key.ID}, UserID: user.ID, OriginalWireDigest: wire}
	mode := service.UsageBillingModeFull
	if cfg.RunMode == config.RunModeSimple {
		mode = service.UsageBillingModeLogOnly
	}
	images, freeFast := false, false
	longContext := account.IsOpenAILongContextBillingEnabled()
	intent.Request = service.NativeIntentRequestScope{Mode: mode, PricingAt: input.UsageInput.PricingAt, UserRole: user.Role,
		OriginalModel:  input.UsageInput.Result.Model,
		BaseMultiplier: 1, TokenMultiplier: 1, ImageMultiplier: 1, VideoMultiplier: 1, GroupLongContextEnabled: true,
		Billing: &service.NativeIntentRequestBillingPolicy{Version: 1, GroupImageCountBilling: &images, KnownCanonical: &id}}
	view, err := service.NewModelPricingResolver(nil, prices).Pin(t.Context(), intent.Request.PricingAt)
	require.NoError(t, err)
	resolved := view.Resolve(t.Context(), service.PricingInput{Model: "gpt-5.1"})
	require.NotNil(t, resolved)
	frozen, err := service.FreezeNativeUsagePricing(resolved, &service.UsageBillingPricingSnapshot{
		Model: "gpt-5.1", PricingAt: intent.Request.PricingAt, Balance: mode == service.UsageBillingModeFull, AccountRateMultiplier: account.BillingRateMultiplier(),
	}, false)
	require.NoError(t, err)
	attribution := &service.UsageLog{}
	attribution.CaptureVendorSnapshot(account, key.Group)
	intent.Attempt = service.NativeIntentAttemptScope{BaselineModel: "gpt-5.1", CandidateModels: []string{"gpt-5.1"}, FrozenPricing: frozen, LongContextEnabled: &longContext,
		Billing: &service.NativeIntentAttemptBillingPolicy{Version: 1, AccountType: account.Type, AccountPlatform: account.Platform,
			FreeFastEligible: &freeFast, OpenAICacheWriteTTL: true,
			VendorIDSnapshot: attribution.VendorIDSnapshot, AccountNameSnapshot: attribution.AccountNameSnapshot,
			GroupNameSnapshot: attribution.GroupNameSnapshot, PlatformSnapshot: attribution.PlatformSnapshot,
			Stats: &service.NativeIntentStatsPricing{State: service.NativeIntentStatsNotApplicable, Model: "gpt-5.1", LongContextEnabled: longContext}}}
	for _, attempt := range input.Operation.Attempts {
		intent.Attempt.Sequence, intent.Attempt.Prepared = attempt.Sequence, attempt.Identity
		intent.Attempt.CapturedAt = intent.Request.PricingAt.Add(time.Duration(attempt.Sequence) * time.Second)
		receipt, err := repo.StoreNativeUsageIntent(t.Context(), &intent)
		require.NoError(t, err)
		require.True(t, receipt.Durable())
		if attempt.Sequence == input.CurrentSequences[0] {
			input.Intent = receipt.NativeIntentReference
		}
	}
	saved, err := repo.LoadNativeUsageIntent(t.Context(), intent.Key)
	require.NoError(t, err)
	require.Len(t, saved.Attempts, 2)
	for i, attempt := range input.Operation.Attempts {
		require.Equal(t, attempt.Sequence, saved.Attempts[i].Sequence)
		require.Equal(t, attempt.Identity, saved.Attempts[i].Prepared)
	}
	var status string
	var factsAbsent, moneyAbsent bool
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT status,native_evidence IS NULL,request_id IS NULL AND canonical_request_id IS NULL AND payload IS NULL AND request_fingerprint='' FROM usage_billing_events WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2", opID, key.ID).Scan(&status, &factsAbsent, &moneyAbsent))
	require.Equal(t, "intent_preserved", status)
	require.True(t, factsAbsent, "preserving scopes must not manufacture usage")
	require.True(t, moneyAbsent, "preserving scopes must not manufacture money identity or payload")
	return gateway, evidence, repo, input
}

func TestNativeUsageEvidencePostgresLogOnlyPublicAdapterIsDurableAndNonBilling(t *testing.T) {
	gateway, evidence, _, input := nativePublicAdapterPGFixture(t, config.RunModeSimple)
	for range 2 {
		receipt, err := gateway.RecordNativeOperationUsage(t.Context(), cloneNativePublicFinalization(t, input))
		require.NoError(t, err)
		require.True(t, receipt.Intent.Durable())
		require.Equal(t, input.Intent, receipt.Intent.NativeIntentReference)
		require.Equal(t, billing.NativeIntentBound, receipt.Intent.Disposition)
		require.NotNil(t, receipt.Canonical)
		require.True(t, receipt.Canonical.Receipt.Durable())
		require.Equal(t, billing.NativeSettled, receipt.Canonical.Receipt.Disposition)
	}
	var balance, quota, window5h, window1d, window7d float64
	var logs, dedup int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1", input.Operation.UserID).Scan(&balance))
	require.Equal(t, 100.0, balance)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT quota_used,usage_5h,usage_1d,usage_7d FROM api_keys WHERE id=$1", input.Identity.APIKeyID).Scan(&quota, &window5h, &window1d, &window7d))
	require.Zero(t, quota+window5h+window1d+window7d)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_logs WHERE api_key_id=$1", input.Identity.APIKeyID).Scan(&logs))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=$1", input.Identity.APIKeyID).Scan(&dedup))
	require.Equal(t, 1, logs)
	require.Equal(t, 1, dedup)
	var payload []byte
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT payload FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Identity.RequestID, input.Identity.APIKeyID).Scan(&payload))
	var command service.UsageBillingCommand
	require.NoError(t, json.Unmarshal(payload, &command))
	require.Empty(t, command.Platform)
	require.False(t, command.BalanceExempt)
	require.Nil(t, command.SubscriptionID)
	for _, amount := range []float64{command.BalanceCost, command.SubscriptionCost, command.APIKeyQuotaCost, command.APIKeyRateLimitCost, command.AccountQuotaCost} {
		require.Zero(t, amount)
	}
	saved, err := evidence.Load(t.Context(), input.Identity)
	require.NoError(t, err)
	require.Equal(t, billing.NativeSettled, saved.Receipt.Disposition)
	require.NotNil(t, saved.SettlementUsage, "raw/canonical mapping belongs to the same durable log transaction")
	require.Equal(t, 700, saved.SettlementUsage.Tokens.InputTokens)
}

func cloneNativePublicFinalization(t *testing.T, input service.NativeOperationFinalization) service.NativeOperationFinalization {
	t.Helper()
	// Each caller owns its mutable ForwardResult; concurrent replay must not
	// share the object that the existing image/tier preparation updates.
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	var clone service.NativeOperationFinalization
	require.NoError(t, json.Unmarshal(raw, &clone))
	return clone
}

func TestNativeUsageEvidencePostgresPublicAdapterNormalizesAndSettlesOnce(t *testing.T) {
	gateway, evidence, repo, input := nativePublicAdapterPGFixture(t)
	input.UsageInput.UserAgent = "codex-native-attribution-test"
	input.UsageInput.IPAddress = "192.0.2.7"
	const callers = 4
	start := make(chan struct{})
	results := make(chan service.NativeIntentFinalizationResult, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		request := cloneNativePublicFinalization(t, input)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			receipt, err := gateway.RecordNativeOperationUsage(t.Context(), request)
			results <- receipt
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for receipt := range results {
		require.True(t, receipt.Intent.Durable())
		require.Equal(t, input.Intent, receipt.Intent.NativeIntentReference)
		require.Equal(t, billing.NativeIntentBound, receipt.Intent.Disposition)
		require.NotNil(t, receipt.Canonical)
		require.True(t, receipt.Canonical.Receipt.Durable())
		require.Equal(t, input.Identity, receipt.Canonical.Receipt.Identity)
		require.Equal(t, billing.NativeSettled, receipt.Canonical.Receipt.Disposition)
	}
	var balance, charge float64
	var agent, ip string
	var inTokens, outTokens, cacheRead, cacheWrite, logs, dedup int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1", input.Operation.UserID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,actual_cost,user_agent,ip_address FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", input.Identity.RequestID, input.Identity.APIKeyID).Scan(&inTokens, &outTokens, &cacheRead, &cacheWrite, &charge, &agent, &ip))
	require.Equal(t, input.UsageInput.UserAgent, agent)
	require.Equal(t, input.UsageInput.IPAddress, ip)
	require.Equal(t, 700, inTokens)
	require.Equal(t, 12, outTokens)
	require.Equal(t, 100, cacheRead)
	require.Equal(t, 200, cacheWrite)
	require.Positive(t, charge, "the original fallback pricing must actually bill")
	require.InDelta(t, charge, 100-balance, 0.00000001)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_logs WHERE api_key_id=$1", input.Identity.APIKeyID).Scan(&logs))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=$1", input.Identity.APIKeyID).Scan(&dedup))
	require.Equal(t, 1, logs)
	require.Equal(t, 1, dedup)

	var payload []byte
	var fingerprint, dedupFingerprint string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT payload,request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Identity.RequestID, input.Identity.APIKeyID).Scan(&payload, &fingerprint))
	// Decode the command produced by the real builder and persisted by the real
	// ledger. This is an observed SQL result, not a hand-built expected command.
	var persisted service.UsageBillingCommand
	require.NoError(t, json.Unmarshal(payload, &persisted))
	require.Equal(t, 1, persisted.PayloadVersion)
	require.Equal(t, input.Identity, persisted.BillingIdentity())
	require.Equal(t, 700, persisted.InputTokens)
	require.Equal(t, fingerprint, persisted.RequestFingerprint)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT request_fingerprint FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2", input.Identity.RequestID, input.Identity.APIKeyID).Scan(&dedupFingerprint))
	require.Equal(t, fingerprint, dedupFingerprint)
	require.NotEqual(t, input.Operation.OperationID, persisted.RequestID)
	require.NotEqual(t, input.UsageInput.Result.RequestID, persisted.RequestID)
	saved, err := evidence.Load(t.Context(), input.Identity)
	require.NoError(t, err)
	require.Len(t, saved.Observations, 2)
	require.Nil(t, saved.Observations[0].Usage)
	require.Equal(t, input.Operation.OperationID, saved.Evidence.OperationID)
	require.Equal(t, input.Identity, saved.Evidence.Identity)
	require.Equal(t, uint64(2), *saved.SettlementSequence)
	require.Equal(t, int64(1000), *saved.Evidence.Usage.InputTokens)
	require.Equal(t, int64(100), *saved.Evidence.Usage.CacheReadTokens)
	require.Equal(t, int64(200), *saved.Evidence.Usage.CacheCreationTokens)
	require.Equal(t, 1000, saved.SettlementUsage.OpenAI.Usage.InputTokens)
	require.Equal(t, 700, saved.SettlementUsage.Tokens.InputTokens)
	recovery, err := repo.LoadNativeIntentRecovery(t.Context(), input.Intent.Key)
	require.NoError(t, err)
	require.Equal(t, input.Intent, recovery.Finalization.Intent)
	require.Equal(t, input.UsageInput.UserAgent, recovery.Finalization.Preparation.UserAgent)
	require.Equal(t, input.UsageInput.IPAddress, recovery.Finalization.Preparation.IPAddress)
	require.Len(t, recovery.Intent.Attempts, 2)
	require.Len(t, recovery.Finalization.Evidence.Observations, 2)
	var technicalStatus, canonicalID string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT status,canonical_request_id FROM usage_billing_events WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2", input.Operation.OperationID, input.Identity.APIKeyID).Scan(&technicalStatus, &canonicalID))
	require.Equal(t, "intent_bound", technicalStatus)
	require.Equal(t, input.Identity.RequestID, canonicalID)

	// A later public-adapter replay keeps the same V1 root and money outcome.
	receipt, err := gateway.RecordNativeOperationUsage(t.Context(), cloneNativePublicFinalization(t, input))
	require.NoError(t, err)
	require.True(t, receipt.Intent.Durable())
	require.Equal(t, billing.NativeIntentBound, receipt.Intent.Disposition)
	require.NotNil(t, receipt.Canonical)
	require.True(t, receipt.Canonical.Receipt.Durable())
	require.Equal(t, billing.NativeSettled, receipt.Canonical.Receipt.Disposition)
	var replayBalance float64
	var replayFingerprint string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1", input.Operation.UserID).Scan(&replayBalance))
	require.Equal(t, balance, replayBalance)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Identity.RequestID, input.Identity.APIKeyID).Scan(&replayFingerprint))
	require.Equal(t, fingerprint, replayFingerprint)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_logs WHERE api_key_id=$1", input.Identity.APIKeyID).Scan(&logs))
	require.Equal(t, 1, logs)
}

func TestNativeUsageEvidencePostgresPublicAdapterUnknownAndPartialDoNotBill(t *testing.T) {
	for _, tc := range []struct {
		name       string
		confidence model.UsageConfidence
	}{{"unknown", model.UsageUnknown}, {"partial", model.UsagePartial}} {
		t.Run(tc.name, func(t *testing.T) {
			gateway, evidence, repo, input := nativePublicAdapterPGFixture(t)
			attempt := &input.Operation.Attempts[1]
			attempt.Terminal, attempt.EndOfStream = model.TerminalUnknown, false
			attempt.ResponseID = ""
			if tc.confidence == model.UsageUnknown {
				attempt.Usage = nil
			} else {
				attempt.Usage.Confidence, attempt.Usage.TerminalConfirmed = tc.confidence, false
				attempt.Usage.ResponseID = ""
			}
			input.UsageInput.Result = nil
			receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
			require.NoError(t, err)
			require.True(t, receipt.Intent.Durable())
			require.Equal(t, input.Intent, receipt.Intent.NativeIntentReference)
			require.Equal(t, billing.NativeIntentPendingUsage, receipt.Intent.Disposition)
			require.Nil(t, receipt.Canonical)
			var balance float64
			var logs, dedup, canonicalEvents int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1", input.Operation.UserID).Scan(&balance))
			require.Equal(t, float64(100), balance)
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_logs WHERE api_key_id=$1", input.Identity.APIKeyID).Scan(&logs))
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=$1", input.Identity.APIKeyID).Scan(&dedup))
			require.Zero(t, logs, "no fabricated zero-token usage row")
			require.Zero(t, dedup)
			var status, fingerprint string
			var noPayload, noMoneyID, noCanonicalID bool
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT status,request_fingerprint,payload IS NULL,request_id IS NULL,canonical_request_id IS NULL FROM usage_billing_events WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2", input.Operation.OperationID, input.Identity.APIKeyID).Scan(&status, &fingerprint, &noPayload, &noMoneyID, &noCanonicalID))
			require.Equal(t, "pending_usage", status)
			require.Empty(t, fingerprint)
			require.True(t, noPayload)
			require.True(t, noMoneyID)
			require.True(t, noCanonicalID)
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Identity.RequestID, input.Identity.APIKeyID).Scan(&canonicalEvents))
			require.Zero(t, canonicalEvents, "pending facts belong only to the technical intent")
			_, err = evidence.Load(t.Context(), input.Identity)
			require.ErrorIs(t, err, sql.ErrNoRows)
			recovery, err := repo.LoadNativeIntentRecovery(t.Context(), input.Intent.Key)
			require.NoError(t, err)
			require.Equal(t, input.Intent, recovery.Finalization.Intent)
			require.Len(t, recovery.Intent.Attempts, 2)
			require.Nil(t, recovery.Finalization.Preparation)
			saved := recovery.Finalization.Evidence
			require.NotNil(t, saved)
			require.Nil(t, saved.SettlementUsage)
			require.Nil(t, saved.SettlementSequence)
			require.Len(t, saved.Observations, 2)
			require.Equal(t, model.SendAmbiguous, saved.Observations[0].SendState)
			require.Nil(t, saved.Observations[0].Usage)
			require.Empty(t, saved.Evidence.UpstreamResponseID)
			if tc.confidence == model.UsageUnknown {
				require.Nil(t, saved.Evidence.Usage)
			} else {
				require.Equal(t, billing.NativeUsagePartial, saved.Evidence.Confidence)
				require.Equal(t, int64(1000), *saved.Evidence.Usage.InputTokens)
				require.False(t, saved.Evidence.TerminalConfirmed)
			}
		})
	}
}

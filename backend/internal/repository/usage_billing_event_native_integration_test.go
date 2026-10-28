//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func nativePGFixture(t *testing.T) (*usageBillingRepository, *service.NativeUsageEvidenceService, service.NativeUsageEvidenceInput) {
	t.Helper()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Balance: 100})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "native-evidence-test"})
	account := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString(), Type: service.AccountTypeAPIKey, Platform: service.PlatformOpenAI})
	id := uuid.NewString()
	input := nativeRepositoryInput(id, user.ID, key.ID, account.ID)
	r := &usageBillingRepository{db: integrationDB, invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
	svc, err := service.ProvideNativeUsageEvidenceService(r)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", id, key.ID)
		require.NoError(t, err)
	})
	return r, svc, input
}
func nativePGMoney(t *testing.T, input service.NativeUsageEvidenceInput, wantBalance float64, wantRows int) {
	t.Helper()
	var balance float64
	var rows, dedup int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1", input.Evidence.UserID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Identity.RequestID, input.Evidence.Identity.APIKeyID).Scan(&rows))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Identity.RequestID, input.Evidence.Identity.APIKeyID).Scan(&dedup))
	require.Equal(t, wantBalance, balance)
	require.Equal(t, wantRows, rows)
	require.Equal(t, wantRows, dedup)
}
func TestNativeUsageEvidencePostgresUnknownSurvivesRestartWithoutDebit(t *testing.T) {
	r, svc, input := nativePGFixture(t)
	receipt, err := svc.Record(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, billing.DurablePendingUsage, receipt.Receipt.Disposition)
	require.True(t, receipt.Receipt.Durable())
	fresh, err := service.ProvideNativeUsageEvidenceService(&usageBillingRepository{db: integrationDB})
	require.NoError(t, err)
	stored, err := fresh.Load(t.Context(), input.Evidence.Identity)
	require.NoError(t, err)
	require.Nil(t, stored.Evidence.Usage)
	require.Empty(t, stored.Evidence.UpstreamRequestID)
	require.Empty(t, stored.Evidence.UpstreamResponseID)
	require.Len(t, stored.Observations, 1)
	require.Equal(t, receipt.Receipt, stored.Receipt)
	_, err = r.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	var status, fingerprint string
	var payloadNull bool
	var attempts int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT status,request_fingerprint,payload IS NULL,attempts FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Identity.RequestID, input.Evidence.Identity.APIKeyID).Scan(&status, &fingerprint, &payloadNull, &attempts))
	require.Equal(t, "pending_usage", status)
	require.Empty(t, fingerprint)
	require.True(t, payloadNull)
	require.Zero(t, attempts)
	nativePGMoney(t, input, 100, 0)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	input.Evidence.Identity.RequestID = uuid.NewString()
	receipt, err = svc.Record(canceled, input)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, receipt.Receipt.Durable())
}
func TestNativeUsageEvidencePostgresMonotonicPromotionAndImmutableSettlement(t *testing.T) {
	_, svc, input := nativePGFixture(t)
	_, err := svc.Record(t.Context(), input)
	require.NoError(t, err)
	partial := input
	n := int64(4)
	partial.Evidence.Confidence = billing.NativeUsagePartial
	partial.Evidence.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, Source: "response.usage", ReturnedModel: "native-test", InputTokens: &n}
	receipt, err := svc.Record(t.Context(), partial)
	require.NoError(t, err)
	require.Equal(t, billing.DurablePendingUsage, receipt.Receipt.Disposition)
	nativePGMoney(t, input, 100, 0)
	final := nativeRepositoryFinal(t, input)
	// Two real repository transactions race on the same journal key.
	var wg sync.WaitGroup
	receipts := make(chan service.NativeUsageRecordResult, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); got, e := svc.Record(t.Context(), final); receipts <- got; errs <- e }()
	}
	wg.Wait()
	close(receipts)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	for got := range receipts {
		require.Equal(t, billing.NativeSettled, got.Receipt.Disposition)
		require.True(t, got.Receipt.Durable())
	}
	nativePGMoney(t, input, 98.75, 1)
	stored, err := svc.Load(t.Context(), input.Evidence.Identity)
	require.NoError(t, err)
	require.Len(t, stored.Observations, 3)
	require.Equal(t, billing.NativeUsageFinal, stored.Evidence.Confidence)
	require.Equal(t, "fixture-observed-response", stored.Evidence.UpstreamResponseID)
	require.Equal(t, 0.125, stored.FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	replay, err := svc.Record(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, billing.NativeSettled, replay.Receipt.Disposition)
	before := ""
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Identity.RequestID, input.Evidence.Identity.APIKeyID).Scan(&before))
	altered, err := service.PrepareNativeUsageEvidence(final)
	require.NoError(t, err)
	altered.Settlement.BalanceCost = 2.5
	altered.Settlement.RequestFingerprint = ""
	failed, err := svc.Record(t.Context(), *altered)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	require.False(t, failed.Receipt.Durable())
	altered, err = service.PrepareNativeUsageEvidence(final)
	require.NoError(t, err)
	altered.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 9
	failed, err = svc.Record(t.Context(), *altered)
	require.ErrorIs(t, err, billing.ErrNativeUsageConflict)
	require.False(t, failed.Receipt.Durable())
	conflict := partial
	lower := int64(3)
	conflict.Evidence.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, Source: "response.usage", InputTokens: &lower}
	failed, err = svc.Record(t.Context(), conflict)
	require.ErrorIs(t, err, billing.ErrNativeUsageConflict)
	require.False(t, failed.Receipt.Durable())
	after := ""
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Identity.RequestID, input.Evidence.Identity.APIKeyID).Scan(&after))
	require.Equal(t, before, after)
	nativePGMoney(t, input, 98.75, 1)
}
func TestNativeUsageEvidencePostgresLegacyV1OrderAndConcurrency(t *testing.T) {
	for _, order := range []string{"legacy_first", "evidence_first", "concurrent"} {
		t.Run(order, func(t *testing.T) {
			repo, svc, input := nativePGFixture(t)
			final := nativeRepositoryFinal(t, input)
			switch order {
			case "legacy_first":
				_, err := repo.Apply(t.Context(), final.Settlement)
				require.NoError(t, err)
				receipt, err := svc.Record(t.Context(), input)
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
				require.False(t, receipt.Receipt.Durable())
			case "evidence_first":
				_, err := svc.Record(t.Context(), input)
				require.NoError(t, err)
				_, err = repo.Apply(t.Context(), final.Settlement)
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
				nativePGMoney(t, input, 100, 0)
			case "concurrent":
				var wg sync.WaitGroup
				errs := make(chan error, 2)
				wg.Add(2)
				go func() { defer wg.Done(); _, err := svc.Record(t.Context(), input); errs <- err }()
				go func() { defer wg.Done(); _, err := repo.Apply(t.Context(), final.Settlement); errs <- err }()
				wg.Wait()
				close(errs)
				conflicts := 0
				for err := range errs {
					if errors.Is(err, service.ErrUsageBillingRequestConflict) {
						conflicts++
					} else {
						require.NoError(t, err)
					}
				}
				require.Equal(t, 1, conflicts, "one existing-key owner must win without overwriting the other")
			}
			receipt, err := svc.Record(t.Context(), final)
			require.NoError(t, err)
			require.Equal(t, billing.NativeSettled, receipt.Receipt.Disposition)
			nativePGMoney(t, input, 98.75, 1)
			var fingerprint string
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Identity.RequestID, input.Evidence.Identity.APIKeyID).Scan(&fingerprint))
			require.Equal(t, final.Settlement.RequestFingerprint, fingerprint)
		})
	}
}

func TestNativeUsageEvidencePostgresRecordedThenPricingRecovery(t *testing.T) {
	repo, svc, input := nativePGFixture(t)
	input.FrozenPricing.Resolved = nil // Price genuinely unavailable at capture.
	final := nativeRepositoryFinal(t, input)
	observed := final
	observed.Settlement = nil
	receipt, err := svc.Record(t.Context(), observed)
	require.NoError(t, err)
	require.Equal(t, billing.UsageRecorded, receipt.Receipt.Disposition)
	nativePGMoney(t, input, 100, 0)
	final.Settlement.BalanceCost = 0
	final.Settlement.UsageLog.ActualCost = 0
	final.Settlement.UsageLog.TotalCost = 0
	final.Settlement.RequestFingerprint = ""
	final.Settlement.Pricing = &service.UsageBillingPricingSnapshot{Pending: true, Model: "native-test", Balance: true}
	receipt, err = svc.Record(t.Context(), final)
	require.NoError(t, err)
	require.Equal(t, billing.NativePendingPricing, receipt.Receipt.Disposition)
	nativePGMoney(t, input, 100, 0)
	repo.reprice = func(_ context.Context, c *service.UsageBillingCommand) error {
		c.BalanceCost = 1.25
		c.UsageLog.ActualCost = 1.25
		c.UsageLog.TotalCost = 1.25
		c.Pricing.Pending = false
		return nil
	}
	_, err = repo.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	restored, err := svc.Load(t.Context(), input.Evidence.Identity)
	require.NoError(t, err)
	require.Equal(t, billing.NativeSettled, restored.Receipt.Disposition)
	nativePGMoney(t, input, 98.75, 1)
}

func TestNativeUsageEvidencePostgresExplicitZeroIsNotUnknown(t *testing.T) {
	_, svc, input := nativePGFixture(t)
	receipt, err := svc.Record(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, billing.DurablePendingUsage, receipt.Receipt.Disposition)
	nativePGMoney(t, input, 100, 0)
	final := nativeRepositoryFinal(t, input)
	zero := int64(0)
	final.Evidence.Usage.InputTokens = &zero
	final.SettlementUsage.OpenAI.Usage.InputTokens = 0
	final.SettlementUsage.Tokens.InputTokens = 0
	final.Settlement.InputTokens = 0
	final.Settlement.UsageLog.InputTokens = 0
	final.Settlement.BalanceCost = 0
	final.Settlement.UsageLog.ActualCost = 0
	final.Settlement.UsageLog.TotalCost = 0
	final.Settlement.RequestFingerprint = ""
	receipt, err = svc.Record(t.Context(), final)
	require.NoError(t, err)
	require.Equal(t, billing.NativeSettled, receipt.Receipt.Disposition)
	nativePGMoney(t, input, 100, 1)
	stored, err := svc.Load(t.Context(), input.Evidence.Identity)
	require.NoError(t, err)
	require.NotNil(t, stored.Evidence.Usage.InputTokens)
	require.Zero(t, *stored.Evidence.Usage.InputTokens)
}

func TestNativeUsageEvidencePostgresSettlementRollbackKeepsReceiptPending(t *testing.T) {
	repo, svc, input := nativePGFixture(t)
	final := nativeRepositoryFinal(t, input)
	_, err := svc.Record(t.Context(), input)
	require.NoError(t, err)
	// Only this fixture user's balance row is locked. Evidence can commit, but
	// the existing money transaction must cancel/roll back when it reaches SQL.
	blocker, err := integrationDB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	var balance float64
	require.NoError(t, blocker.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1 FOR UPDATE", input.Evidence.UserID).Scan(&balance))
	bounded, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	receipt, err := svc.Record(bounded, final)
	cancel()
	require.Error(t, err)
	require.True(t, receipt.Receipt.Durable())
	require.Equal(t, billing.SettlementPending, receipt.Receipt.Disposition)
	require.Nil(t, receipt.ActualApplyResult, "rolled-back transaction cannot produce an actual committed outcome")
	require.NoError(t, blocker.Rollback())
	nativePGMoney(t, input, 100, 0)
	stored, err := svc.Load(t.Context(), input.Evidence.Identity)
	require.NoError(t, err)
	require.Equal(t, billing.SettlementPending, stored.Receipt.Disposition)
	require.Equal(t, billing.NativeUsageFinal, stored.Evidence.Confidence)
	_, err = repo.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	stored, err = svc.Load(t.Context(), input.Evidence.Identity)
	require.NoError(t, err)
	require.Equal(t, billing.NativeSettled, stored.Receipt.Disposition)
	nativePGMoney(t, input, 98.75, 1)
}

func TestNativeUsageEvidencePostgresBatchAttemptsAndCacheMapping(t *testing.T) {
	_, svc, input := nativePGFixture(t)
	input.Evidence.AttemptSequence = 1
	input.Evidence.SendState = model.SendNotSent
	receipt, err := svc.Record(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, billing.DurablePendingUsage, receipt.Receipt.Disposition)
	nativePGMoney(t, input, 100, 0)
	first := input.Evidence
	second := first
	second.AttemptSequence = 2
	second.SendState = model.SendSent
	second.UpstreamRequestID = "real-redirect-request"
	redirect := *second.Prepared
	redirect.TargetDigest = sha256.Sum256([]byte("real-redirect-target"))
	second.Prepared = &redirect
	// A subsequent account attempt still owns the original request/key journal root.
	account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: uuid.NewString(), Type: service.AccountTypeAPIKey, Platform: service.PlatformOpenAI})
	terminal := input
	terminal.Evidence.AttemptSequence = 3
	prepared := *terminal.Evidence.Prepared
	prepared.ResourceID = account.ID
	prepared.Owner.AccountID = account.ID
	terminal.Evidence.Prepared = &prepared
	terminal = nativeRepositoryFinal(t, terminal)
	n, read, write := int64(1000), int64(100), int64(200)
	terminal.Evidence.Usage.InputTokens = &n
	terminal.Evidence.Usage.CacheReadTokens = &read
	terminal.Evidence.Usage.CacheCreationTokens = &write
	terminal.SettlementUsage.OpenAI.Usage = service.OpenAIUsage{InputTokens: 1000, CacheReadInputTokens: 100, CacheCreationInputTokens: 200}
	terminal.SettlementUsage.Tokens = service.UsageTokens{InputTokens: 700, CacheReadTokens: 100, CacheCreationTokens: 200}
	cmd := terminal.Settlement
	cmd.InputTokens = 700
	cmd.CacheReadTokens = 100
	cmd.CacheCreationTokens = 200
	cmd.UsageLog.InputTokens = 700
	cmd.UsageLog.CacheReadTokens = 100
	cmd.UsageLog.CacheCreationTokens = 200
	price, err := terminal.FrozenPricing.Resolved.Restore()
	require.NoError(t, err)
	cost, err := service.NewBillingService(nil, nil).CalculateCostUnified(service.CostInput{Ctx: t.Context(), Model: "native-test", Tokens: terminal.SettlementUsage.Tokens, RateMultiplier: 1, Resolver: &service.ModelPricingResolver{}, Resolved: price})
	require.NoError(t, err)
	cmd.BalanceCost = cost.ActualCost
	cmd.UsageLog.ActualCost = cost.ActualCost
	cmd.UsageLog.TotalCost = cost.TotalCost
	cmd.RequestFingerprint = ""
	terminal.Observations = []billing.NativeUsageEvidence{first, second, terminal.Evidence}
	validated, err := service.PrepareNativeUsageEvidence(terminal)
	require.NoError(t, err)
	terminal = *validated
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	receipts := make(chan service.NativeUsageRecordResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); got, e := svc.Record(t.Context(), terminal); receipts <- got; errs <- e }()
	}
	wg.Wait()
	close(errs)
	close(receipts)
	for e := range errs {
		require.NoError(t, e)
	}
	for r := range receipts {
		require.Equal(t, billing.NativeSettled, r.Receipt.Disposition)
	}
	nativePGMoney(t, input, 100-terminal.Settlement.BalanceCost, 1)
	fresh, err := service.ProvideNativeUsageEvidenceService(&usageBillingRepository{db: integrationDB})
	require.NoError(t, err)
	saved, err := fresh.Load(t.Context(), input.Evidence.Identity)
	require.NoError(t, err)
	require.Len(t, saved.Observations, 3)
	require.Equal(t, uint64(3), *saved.SettlementSequence)
	require.Equal(t, first.Prepared.ResourceID, saved.Observations[0].Prepared.ResourceID)
	require.Equal(t, account.ID, saved.Observations[2].Prepared.ResourceID)
	require.NotEqual(t, input.Evidence.OperationID, saved.Evidence.Identity.RequestID)
	require.Equal(t, int64(1000), *saved.Evidence.Usage.InputTokens)
	require.Equal(t, 700, saved.SettlementUsage.Tokens.InputTokens)
	require.Empty(t, saved.Observations[0].UpstreamResponseID)
	receipt, err = svc.Record(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, billing.NativeSettled, receipt.Receipt.Disposition)
	// Different-sequence facts survive, but cannot rebind the existing charge.
	switched, err := service.PrepareNativeUsageEvidence(terminal)
	require.NoError(t, err)
	switched.Evidence.AttemptSequence = 4
	switched.Observations = []billing.NativeUsageEvidence{switched.Evidence}
	receipt, err = svc.Record(t.Context(), *switched)
	require.ErrorIs(t, err, billing.ErrNativeUsageConflict)
	require.False(t, receipt.Receipt.Durable())
	mismatch, err := service.PrepareNativeUsageEvidence(terminal)
	require.NoError(t, err)
	mismatch.SettlementUsage.OpenAI.Usage.InputTokens++
	receipt, err = svc.Record(t.Context(), *mismatch)
	require.ErrorIs(t, err, billing.ErrNativeUsageConflict)
	require.False(t, receipt.Receipt.Durable())
	nativePGMoney(t, input, 100-terminal.Settlement.BalanceCost, 1)
	var fingerprint string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Identity.RequestID, input.Evidence.Identity.APIKeyID).Scan(&fingerprint))
	require.Equal(t, terminal.Settlement.RequestFingerprint, fingerprint)
}

func TestNativeUsageEvidencePostgresBatchConflictRollbackAndExistingV1(t *testing.T) {
	t.Run("atomic_batch_conflict", func(t *testing.T) {
		_, svc, input := nativePGFixture(t)
		input.Evidence.AttemptSequence = 1
		input.Evidence.UpstreamRequestID = "real-request-one"
		_, err := svc.Record(t.Context(), input)
		require.NoError(t, err)
		fresh := input.Evidence
		fresh.AttemptSequence = 2
		fresh.UpstreamRequestID = "real-request-two"
		conflict := input.Evidence
		conflict.UpstreamRequestID = "conflicting-one"
		batch := input
		batch.Evidence = fresh
		batch.Observations = []billing.NativeUsageEvidence{fresh, conflict}
		receipt, err := svc.Record(t.Context(), batch)
		require.ErrorIs(t, err, billing.ErrNativeUsageConflict)
		require.False(t, receipt.Receipt.Durable())
		saved, err := svc.Load(t.Context(), input.Evidence.Identity)
		require.NoError(t, err)
		require.Len(t, saved.Observations, 1)
		require.Equal(t, "real-request-one", saved.Observations[0].UpstreamRequestID)
		nativePGMoney(t, input, 100, 0)
	})
	t.Run("matching_existing_v1", func(t *testing.T) {
		repo, svc, input := nativePGFixture(t)
		input.Evidence.AttemptSequence = 2
		final := nativeRepositoryFinal(t, input)
		_, err := repo.Apply(t.Context(), final.Settlement)
		require.NoError(t, err)
		earlier := input.Evidence
		earlier.AttemptSequence = 1
		earlier.UpstreamRequestID = "earlier-redirect"
		final.Observations = []billing.NativeUsageEvidence{earlier, final.Evidence}
		receipt, err := svc.Record(t.Context(), final)
		require.NoError(t, err)
		require.Equal(t, billing.NativeSettled, receipt.Receipt.Disposition)
		saved, err := svc.Load(t.Context(), input.Evidence.Identity)
		require.NoError(t, err)
		require.Len(t, saved.Observations, 2)
		require.Equal(t, uint64(2), *saved.SettlementSequence)
		nativePGMoney(t, input, 98.75, 1)
		changed, err := service.PrepareNativeUsageEvidence(final)
		require.NoError(t, err)
		changed.Settlement.BalanceCost = 2.5
		changed.Settlement.RequestFingerprint = ""
		receipt, err = svc.Record(t.Context(), *changed)
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		require.False(t, receipt.Receipt.Durable())
		var fingerprint string
		require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Identity.RequestID, input.Evidence.Identity.APIKeyID).Scan(&fingerprint))
		require.Equal(t, final.Settlement.RequestFingerprint, fingerprint)
		nativePGMoney(t, input, 98.75, 1)
	})
}

func TestNativeUsageEvidencePostgresActualApplyResult(t *testing.T) {
	t.Run("commit_and_settled_replay", func(t *testing.T) {
		_, svc, input := nativePGFixture(t)
		result, err := svc.Record(t.Context(), input)
		require.NoError(t, err)
		require.Equal(t, billing.DurablePendingUsage, result.Receipt.Disposition)
		require.Nil(t, result.ActualApplyResult)
		_, err = integrationDB.ExecContext(t.Context(), "UPDATE api_keys SET quota=1 WHERE id=$1", input.Evidence.Identity.APIKeyID)
		require.NoError(t, err)
		final := nativeRepositoryFinal(t, input)
		final.Settlement.AccountType = service.AccountTypeAPIKey
		final.Settlement.APIKeyQuotaCost = 1.25
		final.Settlement.AccountQuotaCost = 1.25
		final.Settlement.RequestFingerprint = ""
		result, err = svc.Record(t.Context(), final)
		require.NoError(t, err)
		require.Equal(t, billing.NativeSettled, result.Receipt.Disposition)
		require.NotNil(t, result.ActualApplyResult)
		require.True(t, result.ActualApplyResult.Applied)
		require.True(t, result.ActualApplyResult.APIKeyQuotaExhausted)
		require.NotNil(t, result.ActualApplyResult.NewBalance)
		require.Equal(t, 98.75, *result.ActualApplyResult.NewBalance)
		require.False(t, result.ActualApplyResult.BalanceOverdrafted)
		require.NotNil(t, result.ActualApplyResult.QuotaState)
		require.Equal(t, 1.25, result.ActualApplyResult.QuotaState.TotalUsed)
		replay, err := svc.Record(t.Context(), final)
		require.NoError(t, err)
		require.Equal(t, billing.NativeSettled, replay.Receipt.Disposition)
		require.Nil(t, replay.ActualApplyResult, "a durable settled receipt is not an Applied result for this call")
		nativePGMoney(t, input, 98.75, 1)
	})
	t.Run("commit_survives_followup_receipt_read_failure", func(t *testing.T) {
		repo, svc, input := nativePGFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		repo.invalidate = func(context.Context, *service.UsageBillingCommand) error { cancel(); return context.Canceled }
		result, err := svc.Record(ctx, nativeRepositoryFinal(t, input))
		require.ErrorIs(t, err, context.Canceled)
		require.True(t, result.Receipt.Durable())
		require.Equal(t, billing.SettlementPending, result.Receipt.Disposition)
		require.NotNil(t, result.ActualApplyResult)
		require.True(t, result.ActualApplyResult.Applied)
		require.NotNil(t, result.ActualApplyResult.NewBalance)
		require.Equal(t, 98.75, *result.ActualApplyResult.NewBalance)
		stored, err := svc.Load(t.Context(), input.Evidence.Identity)
		require.NoError(t, err)
		require.Equal(t, billing.NativeSettled, stored.Receipt.Disposition)
		require.True(t, stored.Receipt.RecoveryPending)
		nativePGMoney(t, input, 98.75, 1)
	})
}

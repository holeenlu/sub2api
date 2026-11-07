//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This fixture tests the journal/worker/SQL contract. Its completed quote is
// the existing transaction fixture, not proof of MAIN's future frozen builder.
func nativeI2PGFixture(t *testing.T) (*usageBillingRepository, *service.NativeBillingIntentService, service.NativeUsageIntent, service.NativeIntentFinalizationInput) {
	t.Helper()
	repo, intents, intent, evidence := nativeIntentPGFixture(t)
	images := false
	freeFastEligible := false
	canonical := evidence.Evidence.Identity
	intent.Request.Billing = &service.NativeIntentRequestBillingPolicy{Version: 1, QuotaPlatform: service.PlatformOpenAI, GroupImageCountBilling: &images, KnownCanonical: &canonical}
	intent.Attempt.Billing = &service.NativeIntentAttemptBillingPolicy{Version: 1, AccountType: service.AccountTypeAPIKey, AccountPlatform: service.PlatformOpenAI, FreeFastEligible: &freeFastEligible, Stats: &service.NativeIntentStatsPricing{State: service.NativeIntentStatsNotApplicable}}
	quote := nativeRepositoryFinal(t, evidence)
	quote.Settlement.AccountType = service.AccountTypeAPIKey
	quote.Settlement.RequestFingerprint = ""
	input := service.NativeIntentFinalizationInput{Evidence: quote, Preparation: &service.NativeUsagePreparation{
		QuotaPlatform: service.PlatformOpenAI, Result: service.NativeUsagePreparationResult{Model: "native-test", ResponseID: quote.Evidence.UpstreamResponseID, Usage: service.OpenAIUsage{InputTokens: 10}}}}
	input = nativeI2RebindFixture(t, intent, input)
	return repo, intents, intent, input
}

func nativeI2Pending(t *testing.T, input service.NativeIntentFinalizationInput) service.NativeIntentFinalizationInput {
	t.Helper()
	copy, err := service.PrepareNativeIntentFinalization(input)
	require.NoError(t, err)
	copy.Evidence.Settlement = nil
	copy.Evidence.SettlementUsage = nil
	copy.Reason = billing.NativeBuilderUnavailable
	return *copy
}

type nativeI2PreparedQuoteFixture struct {
	quote service.NativeIntentPreparedUsage
	calls atomic.Int32
}

func (p *nativeI2PreparedQuoteFixture) PrepareNativeIntentUsage(ctx context.Context, in service.NativeIntentRecoveryInput) (service.NativeIntentPreparedUsage, error) {
	p.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return service.NativeIntentPreparedUsage{}, err
	}
	if in.Intent == nil || in.Finalization == nil || in.Finalization.Preparation == nil {
		return service.NativeIntentPreparedUsage{}, errors.New("missing durable preparation")
	}
	// No model/upstream port exists here. MAIN's actual frozen builder is tested
	// separately; this boundary supplies a detached original V1 quote fixture.
	raw, err := json.Marshal(p.quote)
	if err != nil {
		return service.NativeIntentPreparedUsage{}, err
	}
	var copy service.NativeIntentPreparedUsage
	err = json.Unmarshal(raw, &copy)
	return copy, err
}

func nativeI2AssertMoney(t *testing.T, input service.NativeIntentFinalizationInput, want float64, rows int) {
	t.Helper()
	var balance float64
	var logs, dedup int
	id := input.Evidence.Evidence.Identity
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1", input.Evidence.Evidence.UserID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", id.RequestID, id.APIKeyID).Scan(&logs))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2", id.RequestID, id.APIKeyID).Scan(&dedup))
	require.Equal(t, want, balance)
	require.Equal(t, rows, logs)
	require.Equal(t, rows, dedup)
}

type nativeI2WorkerCache struct {
	service.BillingCache
	once    sync.Once
	applied chan struct{}
}

func (*nativeI2WorkerCache) InvalidateUserBalance(context.Context, int64) error { return nil }
func (c *nativeI2WorkerCache) DeleteUserPlatformQuotaCache(context.Context, int64, string) error {
	c.once.Do(func() { close(c.applied) })
	return nil
}

func TestNativeIntentPostgresPreparationRestartsThroughActualUsageWorker(t *testing.T) {
	repo, intents, intent, input := nativeI2PGFixture(t)
	_, err := intents.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	pending := nativeI2Pending(t, input)
	receipt, err := repo.FinalizeNativeUsageIntent(t.Context(), &pending)
	require.NoError(t, err)
	require.True(t, receipt.Intent.Durable())
	require.Equal(t, billing.NativeIntentPendingPreparation, receipt.Intent.Disposition)
	require.Nil(t, receipt.Canonical)
	nativeI2AssertMoney(t, input, 100, 0)
	events, err := (&usageBillingLedger{repo: repo}).Claim(t.Context(), 16)
	require.NoError(t, err)
	require.Empty(t, events, "no configured preparer means no technical claim")
	// Drop the submitting repository/service. The production factory installs
	// the existing recovery callback against a fresh SQL repository.
	fresh := &usageBillingRepository{db: integrationDB}
	quote := &nativeI2PreparedQuoteFixture{quote: service.NativeIntentPreparedUsage{Settlement: input.Evidence.Settlement, SettlementUsage: input.Evidence.SettlementUsage}}
	_, err = service.ProvideNativeIntentFinalizationService(fresh, quote)
	require.NoError(t, err)
	cache := &nativeI2WorkerCache{applied: make(chan struct{})}
	cfg := &config.Config{}
	cfg.Gateway.UsageRecord.WorkerCount = 1
	cfg.Gateway.UsageRecord.TaskTimeoutSeconds = 3
	pool := service.ProvideUsageRecordWorkerPool(cfg, integrationDB, fresh, nil, nil, nil, cache, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, pool.Stop(ctx))
	})
	require.NoError(t, pool.Start(t.Context()))
	select {
	case <-cache.applied:
	case <-time.After(10 * time.Second):
		t.Fatal("actual periodic usage worker did not bind and settle the preserved preparation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	require.NoError(t, pool.Drain(ctx))
	require.NoError(t, pool.Stop(ctx))
	require.EqualValues(t, 1, quote.calls.Load())
	nativeI2AssertMoney(t, input, 98.75, 1)
	var status, canonical string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT status,canonical_request_id FROM usage_billing_events WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID).Scan(&status, &canonical))
	require.Equal(t, "intent_bound", status)
	require.Equal(t, input.Evidence.Evidence.Identity.RequestID, canonical)
	stored, err := fresh.LoadNativeIntentRecovery(t.Context(), intent.Key)
	require.NoError(t, err)
	require.Equal(t, billing.NativeUsageFinal, stored.Finalization.Evidence.Evidence.Confidence)
	require.Equal(t, 10, stored.Finalization.Preparation.Result.Usage.InputTokens)
}

func TestNativeIntentPostgresFinalFactsWaitForActualPreparation(t *testing.T) {
	repo, intents, intent, input := nativeI2PGFixture(t)
	_, err := intents.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	facts := nativeI2Pending(t, input)
	facts.Preparation = nil
	result, err := repo.FinalizeNativeUsageIntent(t.Context(), &facts)
	require.NoError(t, err)
	require.True(t, result.Intent.Durable())
	require.Equal(t, billing.NativeIntentPendingPreparation, result.Intent.Disposition)
	require.Nil(t, result.Canonical)
	nativeI2AssertMoney(t, input, 100, 0)

	// A process restart must load exactly the observed final counts, even
	// though the original usage projection was unavailable. The original
	// recovery worker cannot synthesize that projection from current policy.
	fresh := &usageBillingRepository{db: integrationDB}
	quote := &nativeI2PreparedQuoteFixture{quote: service.NativeIntentPreparedUsage{Settlement: input.Evidence.Settlement, SettlementUsage: input.Evidence.SettlementUsage}}
	_, err = service.ProvideNativeIntentFinalizationService(fresh, quote)
	require.NoError(t, err)
	stored, err := fresh.LoadNativeIntentRecovery(t.Context(), intent.Key)
	require.NoError(t, err)
	require.Equal(t, billing.NativeUsagePreparationUnavailable, stored.Finalization.Reason)
	require.Equal(t, billing.NativeUsageFinal, stored.Finalization.Evidence.Evidence.Confidence)
	require.True(t, stored.Finalization.Evidence.Evidence.TerminalConfirmed)
	require.EqualValues(t, 10, *stored.Finalization.Evidence.Evidence.Usage.InputTokens)
	require.Nil(t, stored.Finalization.Preparation)
	claimed, err := (&usageBillingLedger{repo: fresh}).Claim(t.Context(), 16)
	require.NoError(t, err)
	require.Empty(t, claimed, "missing original preparation is not a retryable builder outage")
	require.Zero(t, quote.calls.Load())

	// An actual late projection must match the durable parser facts. A
	// conflicting projection cannot replace the pending evidence or debit.
	bad := nativeI2Pending(t, input)
	bad.Preparation.Result.Usage.InputTokens++
	invalid, err := fresh.FinalizeNativeUsageIntent(t.Context(), &bad)
	require.Error(t, err)
	require.False(t, invalid.Intent.Durable())
	require.Nil(t, invalid.Canonical)
	nativeI2AssertMoney(t, input, 100, 0)

	prepared := nativeI2Pending(t, input)
	result, err = fresh.FinalizeNativeUsageIntent(t.Context(), &prepared)
	require.NoError(t, err)
	require.Equal(t, billing.NativeIntentPendingPreparation, result.Intent.Disposition)
	// A late duplicate of the original missing-projection facts must not
	// erase the now complete inputs or prevent the existing worker claiming.
	_, err = fresh.FinalizeNativeUsageIntent(t.Context(), &facts)
	require.NoError(t, err)
	stored, err = fresh.LoadNativeIntentRecovery(t.Context(), intent.Key)
	require.NoError(t, err)
	require.Equal(t, billing.NativeBuilderUnavailable, stored.Finalization.Reason)
	require.Equal(t, prepared.Preparation, stored.Finalization.Preparation)
	n, err := fresh.RecoverPendingUsageBilling(t.Context(), 16, nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.EqualValues(t, 1, quote.calls.Load())
	nativeI2AssertMoney(t, input, 98.75, 1)
	_, err = fresh.FinalizeNativeUsageIntent(t.Context(), &input)
	require.NoError(t, err)
	nativeI2AssertMoney(t, input, 98.75, 1)
}

func TestNativeIntentPostgresBoundAcknowledgementLostUsesCanonicalRecovery(t *testing.T) {
	repo, intents, intent, input := nativeI2PGFixture(t)
	_, err := intents.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	adapter := &usageBillingLedger{repo: repo}
	_, err = adapter.PreserveNativeFinalization(t.Context(), &input)
	require.NoError(t, err)
	bound, canonical, _, err := adapter.BindNativeFinalization(t.Context(), &input)
	require.NoError(t, err)
	require.Equal(t, billing.NativeIntentBound, bound.Disposition)
	require.Equal(t, billing.SettlementPending, canonical.Disposition)
	// Both binding rows committed, but the process/ack is lost before money.
	nativeI2AssertMoney(t, input, 100, 0)
	fresh := &usageBillingRepository{db: integrationDB, invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
	n, err := fresh.RecoverPendingUsageBilling(t.Context(), 16, nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	nativeI2AssertMoney(t, input, 98.75, 1)
	replay, err := fresh.FinalizeNativeUsageIntent(t.Context(), &input)
	require.NoError(t, err)
	require.NotNil(t, replay.Canonical)
	require.Equal(t, billing.NativeSettled, replay.Canonical.Receipt.Disposition)
	require.Nil(t, replay.Canonical.ActualApplyResult, "a receipt read cannot invent a new Apply result")
	nativeI2AssertMoney(t, input, 98.75, 1)
}

func TestNativeIntentPostgresMoneyCommitBeforeCacheAckIsNotDebitedAgain(t *testing.T) {
	repo, intents, intent, input := nativeI2PGFixture(t)
	_, err := intents.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	repo.invalidate = func(context.Context, *service.UsageBillingCommand) error {
		return errors.New("isolated cache acknowledgement lost")
	}
	got, err := repo.FinalizeNativeUsageIntent(t.Context(), &input)
	require.NoError(t, err)
	require.NotNil(t, got.Canonical)
	require.True(t, got.Canonical.ActualApplyResult.Applied)
	require.True(t, got.Canonical.Receipt.RecoveryPending)
	nativeI2AssertMoney(t, input, 98.75, 1)
	var status string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT status FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Evidence.Identity.RequestID, intent.Key.APIKeyID).Scan(&status))
	require.Equal(t, "settled", status)
	fresh := &usageBillingRepository{db: integrationDB, invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
	n, err := fresh.RecoverPendingUsageBilling(t.Context(), 16, nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	nativeI2AssertMoney(t, input, 98.75, 1)
}

func TestNativeIntentPostgresLegacyFirstAndIntentFirstRequireKnownCanonical(t *testing.T) {
	for _, order := range []string{"legacy_first", "intent_first"} {
		for _, known := range []bool{true, false} {
			name := order + "/ambiguous"
			if known {
				name = order + "/known_http_owner"
			}
			t.Run(name, func(t *testing.T) {
				repo, intents, intent, input := nativeI2PGFixture(t)
				if !known {
					intent.Request.Billing.KnownCanonical = nil
					input = nativeI2RebindFixture(t, intent, input)
				}
				if order == "intent_first" {
					_, err := intents.PreserveBeforeSend(t.Context(), intent)
					require.NoError(t, err)
				}
				original := input.Evidence.Settlement.RequestFingerprint
				_, err := repo.Apply(t.Context(), input.Evidence.Settlement)
				require.NoError(t, err)
				if order == "legacy_first" {
					_, err = intents.PreserveBeforeSend(t.Context(), intent)
					require.NoError(t, err)
				}
				result, err := repo.FinalizeNativeUsageIntent(t.Context(), &input)
				if known {
					require.NoError(t, err)
					require.Equal(t, billing.NativeIntentBound, result.Intent.Disposition)
				} else {
					require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
					require.True(t, result.Intent.Durable())
					require.Nil(t, result.Canonical)
				}
				var fingerprint string
				require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Evidence.Evidence.Identity.RequestID, intent.Key.APIKeyID).Scan(&fingerprint))
				require.Equal(t, original, fingerprint)
				nativeI2AssertMoney(t, input, 98.75, 1)
			})
		}
	}
}

func TestNativeIntentPostgresConcurrentSameRootAndAmbiguousOtherOperation(t *testing.T) {
	repo, intents, intent, input := nativeI2PGFixture(t)
	_, err := intents.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	type outcome struct {
		result service.NativeIntentFinalizationResult
		err    error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for range 2 {
		copy, err := service.PrepareNativeIntentFinalization(input)
		require.NoError(t, err)
		go func(in *service.NativeIntentFinalizationInput) {
			<-start
			result, err := repo.FinalizeNativeUsageIntent(t.Context(), in)
			results <- outcome{result, err}
		}(copy)
	}
	close(start)
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		require.Equal(t, billing.NativeIntentBound, got.result.Intent.Disposition)
	}
	nativeI2AssertMoney(t, input, 98.75, 1)
	other, err := service.PrepareNativeUsageIntent(intent)
	require.NoError(t, err)
	other.Key.OperationID = "operation:" + uuid.NewString()
	other.Attempt.Prepared.OperationID = other.Key.OperationID
	otherInput := nativeI2RebindFixture(t, *other, input)
	_, err = intents.PreserveBeforeSend(t.Context(), *other)
	require.NoError(t, err)
	got, err := repo.FinalizeNativeUsageIntent(t.Context(), &otherInput)
	require.Error(t, err, "301 supports same-technical-root recovery only; equal money fingerprints do not prove cross-operation replay")
	require.True(t, got.Intent.Durable())
	require.Nil(t, got.Canonical)
	var bound int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_events WHERE api_key_id=$1 AND status='intent_bound'", intent.Key.APIKeyID).Scan(&bound))
	require.Equal(t, 1, bound)
	nativeI2AssertMoney(t, input, 98.75, 1)
}

func TestNativeIntentPostgresMissingPreparationPolicyKeepsFinalFactsUnclaimed(t *testing.T) {
	for _, missing := range []string{"request_policy", "attempt_policy", "stats", "image_flag", "hosted"} {
		t.Run(missing, func(t *testing.T) {
			repo, intents, intent, input := nativeI2PGFixture(t)
			switch missing {
			case "request_policy":
				intent.Request.Billing = nil
			case "attempt_policy":
				intent.Attempt.Billing = nil
			case "stats":
				intent.Attempt.Billing.Stats = nil
			case "image_flag":
				intent.Request.Billing.GroupImageCountBilling = nil
			case "hosted":
				input.Preparation.Result.ImageCount = 1
				count := int64(1)
				input.Evidence.Evidence.Usage.ImageCount = &count
			}
			input.Evidence.Settlement = nil
			input.Evidence.SettlementUsage = nil
			input = nativeI2RebindFixture(t, intent, input)
			_, err := intents.PreserveBeforeSend(t.Context(), intent)
			require.NoError(t, err)
			got, err := repo.FinalizeNativeUsageIntent(t.Context(), &input)
			require.NoError(t, err)
			require.True(t, got.Intent.Durable())
			require.Equal(t, billing.NativeIntentPendingPreparation, got.Intent.Disposition)
			require.Nil(t, got.Canonical)
			stored, err := repo.LoadNativeIntentRecovery(t.Context(), intent.Key)
			require.NoError(t, err)
			require.Equal(t, billing.NativeUsageFinal, stored.Finalization.Evidence.Evidence.Confidence)
			require.EqualValues(t, 10, *stored.Finalization.Evidence.Evidence.Usage.InputTokens)
			require.Equal(t, 10, stored.Finalization.Preparation.Result.Usage.InputTokens)
			require.NotEmpty(t, stored.Finalization.Reason)
			quote := &nativeI2PreparedQuoteFixture{}
			_, err = service.ProvideNativeIntentFinalizationService(repo, quote)
			require.NoError(t, err)
			claimed, err := (&usageBillingLedger{repo: repo}).Claim(t.Context(), 16)
			require.NoError(t, err)
			require.Empty(t, claimed)
			require.Zero(t, quote.calls.Load())
			nativeI2AssertMoney(t, input, 100, 0)
		})
	}
}

func TestNativeIntentPostgresCanceledPersistenceCannotReturnDurableFacts(t *testing.T) {
	repo, intents, intent, input := nativeI2PGFixture(t)
	_, err := intents.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := repo.FinalizeNativeUsageIntent(ctx, &input)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, result.Intent.Durable())
	require.Nil(t, result.Canonical)
	_, err = repo.LoadNativeIntentRecovery(t.Context(), intent.Key)
	require.ErrorIs(t, err, sql.ErrNoRows)
	nativeI2AssertMoney(t, input, 100, 0)
}

func TestNativeIntentPostgresNotSentReprepareKeepsDistinctPriceScopes(t *testing.T) {
	repo, intents, intent, final := nativeI2PGFixture(t)
	_, err := intents.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	first, err := service.PrepareNativeIntentFinalization(final)
	require.NoError(t, err)
	first.Evidence.Settlement = nil
	first.Evidence.SettlementUsage = nil
	first.Preparation = nil
	first.Evidence.Evidence.SendState = model.SendNotSent
	first.Evidence.Evidence.Confidence = billing.NativeUsageUnknown
	first.Evidence.Evidence.TerminalConfirmed = false
	first.Evidence.Evidence.Terminal = model.TerminalUnknown
	first.Evidence.Evidence.Usage = nil
	first.Evidence.Evidence.UpstreamResponseID = ""
	first.Evidence.Observations = nil
	first, err = service.PrepareNativeIntentFinalization(*first)
	require.NoError(t, err)
	result, err := repo.FinalizeNativeUsageIntent(t.Context(), first)
	require.NoError(t, err)
	require.Equal(t, billing.NativeIntentPendingUsage, result.Intent.Disposition)
	nativeI2AssertMoney(t, final, 100, 0)
	second, err := service.PrepareNativeUsageIntent(intent)
	require.NoError(t, err)
	account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: uuid.NewString(), Type: service.AccountTypeAPIKey, Platform: service.PlatformOpenAI})
	second.Attempt.Sequence = 2
	second.Attempt.Prepared.ResourceID = account.ID
	second.Attempt.Prepared.Owner.AccountID = account.ID
	second.Attempt.Prepared.TargetDigest = [32]byte{8}
	second.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 0.25
	second.Attempt.FrozenPricing.Policy.AccountRateMultiplier = 3
	_, err = intents.PreserveBeforeSend(t.Context(), *second)
	require.NoError(t, err, "pending unknown evidence must not block a new actual scope authorized by the same execution owner")
	final.Evidence.Settlement.AccountID = account.ID
	final.Evidence.Settlement.UsageLog.AccountID = account.ID
	final.Evidence.Settlement.BalanceCost = 2.5
	final.Evidence.Settlement.UsageLog.ActualCost = 2.5
	final.Evidence.Settlement.UsageLog.TotalCost = 2.5
	final.Evidence.Settlement.RequestFingerprint = ""
	final = nativeI2RebindFixture(t, *second, final)
	final.Evidence.Observations = []billing.NativeUsageEvidence{first.Evidence.Evidence, final.Evidence.Evidence}
	prepared, err := service.PrepareNativeIntentFinalization(final)
	require.NoError(t, err)
	require.Equal(t, first.Evidence.Evidence.Fingerprint(), prepared.Evidence.Evidence.Fingerprint())
	result, err = repo.FinalizeNativeUsageIntent(t.Context(), prepared)
	require.NoError(t, err)
	require.Equal(t, billing.NativeIntentBound, result.Intent.Disposition)
	nativeI2AssertMoney(t, *prepared, 97.5, 1)
	stored, err := repo.LoadNativeIntentRecovery(t.Context(), intent.Key)
	require.NoError(t, err)
	require.Len(t, stored.Intent.Attempts, 2)
	require.Equal(t, 0.125, stored.Intent.Attempts[0].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	require.Equal(t, 0.25, stored.Intent.Attempts[1].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	require.Len(t, stored.Finalization.Evidence.Observations, 2)
	require.NotEqual(t, stored.Finalization.Evidence.Observations[0].AttemptScopeDigest, stored.Finalization.Evidence.Observations[1].AttemptScopeDigest)
}

func TestNativeIntentPostgresExistingV1EvidenceCannotBeImplicitlyUpgraded(t *testing.T) {
	repo, intents, intent, input := nativeI2PGFixture(t)
	legacy, err := service.PrepareNativeIntentFinalization(input)
	require.NoError(t, err)
	legacy.Evidence.Intent = nil
	legacy.Evidence.Evidence.Version = 1
	legacy.Evidence.Evidence.AttemptScopeDigest = [32]byte{}
	legacy.Evidence.Evidence.PricingDigest = [32]byte{}
	legacy.Evidence.Observations = nil
	_, err = repo.RecordNativeUsageEvidence(t.Context(), &legacy.Evidence)
	require.NoError(t, err)
	old, err := repo.LoadNativeUsageEvidence(t.Context(), input.Evidence.Evidence.Identity)
	require.NoError(t, err)
	require.Equal(t, 1, old.Version)
	_, err = intents.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	got, err := repo.FinalizeNativeUsageIntent(t.Context(), &input)
	require.ErrorIs(t, err, billing.ErrNativeUsageConflict, "KnownCanonical may attach a money-only row, never silently convert existing V1 native evidence")
	require.True(t, got.Intent.Durable())
	require.Nil(t, got.Canonical)
	after, err := repo.LoadNativeUsageEvidence(t.Context(), input.Evidence.Evidence.Identity)
	require.NoError(t, err)
	require.Equal(t, 1, after.Version)
	require.Equal(t, old.Evidence.Fingerprint(), after.Evidence.Fingerprint())
	nativeI2AssertMoney(t, input, 98.75, 1)
}

func TestNativeIntentPostgresDifferentOperationsRaceOneCanonicalOwner(t *testing.T) {
	repo, intents, first, input := nativeI2PGFixture(t)
	first.Request.Billing.KnownCanonical = nil
	input = nativeI2RebindFixture(t, first, input)
	other, err := service.PrepareNativeUsageIntent(first)
	require.NoError(t, err)
	other.Key.OperationID = "operation:" + uuid.NewString()
	other.Attempt.Prepared.OperationID = other.Key.OperationID
	second := nativeI2RebindFixture(t, *other, input)
	require.Equal(t, input.Evidence.Settlement.RequestFingerprint, second.Evidence.Settlement.RequestFingerprint, "equal money is the ambiguous case, not a proof of one generation")
	_, err = intents.PreserveBeforeSend(t.Context(), first)
	require.NoError(t, err)
	_, err = intents.PreserveBeforeSend(t.Context(), *other)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []service.NativeIntentFinalizationInput{input, second} {
		copy, err := service.PrepareNativeIntentFinalization(candidate)
		require.NoError(t, err)
		wg.Add(1)
		go func(in *service.NativeIntentFinalizationInput) {
			defer wg.Done()
			<-start
			_, err := repo.FinalizeNativeUsageIntent(t.Context(), in)
			results <- err
		}(copy)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			require.True(t, errors.Is(err, billing.ErrNativeIntentConflict) || errors.Is(err, billing.ErrNativeUsageConflict), "unexpected non-conflict: %v", err)
			conflict++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
	nativeI2AssertMoney(t, input, 98.75, 1)
}

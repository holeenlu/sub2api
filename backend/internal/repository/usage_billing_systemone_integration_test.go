//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func systemOnePGFixture(t *testing.T) (*usageBillingRepository, service.SystemOneUsagePreparation, service.NativeUsageEvidenceInput) {
	t.Helper()
	repo, intents, scope, native := nativeIntentPGFixture(t)
	p := systemOneJournalFixture(t, scope, native.Evidence.Identity)
	receipt, err := intents.PreserveBeforeSend(t.Context(), p.Scope)
	require.NoError(t, err)
	require.True(t, receipt.Durable())
	return repo, p, native
}

func TestSystemOneJournalPostgresConcurrentSettlementAndReplay(t *testing.T) {
	repo, p, money := systemOnePGFixture(t)
	var wg sync.WaitGroup
	var applied atomic.Int32
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := p
			in.Settlement, in.SettlementUsage = systemOneJournalCommand(in)
			got, err := (&usageBillingRepository{db: integrationDB, invalidate: repo.invalidate}).RecordSystemOneUsage(t.Context(), &in)
			if got.ActualApplyResult != nil && got.ActualApplyResult.Applied {
				applied.Add(1)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, applied.Load())
	nativePGMoney(t, money, 98.75, 1)
	stored, err := repo.LoadNativeIntentRecovery(t.Context(), p.Intent.Key)
	require.NoError(t, err)
	require.Equal(t, service.NativeIntentProtocolSystemOne, stored.Finalization.Evidence.Protocol)
	require.NotNil(t, stored.Finalization.SystemOne)
	require.Nil(t, stored.Finalization.Preparation)
	// An old pending delivery cannot erase a bound canonical settlement.
	got, err := repo.RecordSystemOneUsage(t.Context(), &p)
	require.NoError(t, err)
	require.Equal(t, model.BillingSettled, got.Billing.Disposition)
	nativePGMoney(t, money, 98.75, 1)
	stale := p
	stale.InputTokens, stale.OutputTokens = nil, nil
	stale.Operation.Attempts = append([]model.AttemptFact(nil), p.Operation.Attempts...)
	stale.Operation.Attempts[0].Usage = nil
	got, err = repo.RecordSystemOneUsage(t.Context(), &stale)
	require.NoError(t, err)
	require.Equal(t, model.BillingSettled, got.Billing.Disposition)
	nativePGMoney(t, money, 98.75, 1)
}

func TestSystemOneJournalPostgresUnknownUsageStaysUncharged(t *testing.T) {
	repo, p, money := systemOnePGFixture(t)
	p.InputTokens, p.OutputTokens = nil, nil
	p.Operation.Attempts[0].Usage = nil
	got, err := repo.RecordSystemOneUsage(t.Context(), &p)
	require.NoError(t, err)
	require.True(t, got.Billing.Durable)
	require.Equal(t, model.BillingPendingUsage, got.Billing.Disposition)
	fresh := &usageBillingRepository{db: integrationDB}
	fresh.ConfigureSystemOneIntentPreparation(func(context.Context, *service.SystemOneUsagePreparation) (*service.UsageBillingCommand, *service.UsagePricingRequest, error) {
		t.Error("unknown usage must not reach the calculator")
		return nil, nil, billing.ErrNativePreparationPending
	})
	n, err := fresh.RecoverPendingUsageBilling(t.Context(), 16, nil)
	require.NoError(t, err)
	require.Zero(t, n)
	stored, err := fresh.LoadNativeIntentRecovery(t.Context(), p.Intent.Key)
	require.NoError(t, err)
	require.Nil(t, stored.Finalization.SystemOne.InputTokens)
	nativePGMoney(t, money, 100, 0)
}

func TestSystemOneJournalPostgresActualWorkerRestartsPendingPreparation(t *testing.T) {
	repo, p, money := systemOnePGFixture(t)
	got, err := repo.RecordSystemOneUsage(t.Context(), &p)
	require.NoError(t, err)
	require.Equal(t, model.BillingRecoveryPending, got.Billing.Disposition)
	fresh := &usageBillingRepository{db: integrationDB}
	cfg := &config.Config{}
	cfg.Gateway.UsageRecord.WorkerCount = 1
	cfg.Gateway.UsageRecord.TaskTimeoutSeconds = 3
	pool := service.ProvideUsageRecordWorkerPool(cfg, integrationDB, fresh, nil, nil, nil, nil, nil)
	fresh.ConfigureUsageBillingRecovery(nil, func(context.Context, *service.UsageBillingCommand) error { return nil })
	var calls atomic.Int32
	fresh.ConfigureSystemOneIntentPreparation(func(_ context.Context, saved *service.SystemOneUsagePreparation) (*service.UsageBillingCommand, *service.UsagePricingRequest, error) {
		calls.Add(1)
		command, pricing := systemOneJournalCommand(*saved)
		return command, pricing, nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		require.NoError(t, pool.Stop(ctx))
	})
	require.NoError(t, pool.Start(t.Context()))
	require.Eventually(t, func() bool {
		var n int
		err := integrationDB.QueryRowContext(t.Context(), "SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2", p.Identity.RequestID, p.Identity.APIKeyID).Scan(&n)
		return err == nil && n == 1
	}, 12*time.Second, 50*time.Millisecond)
	require.Positive(t, calls.Load())
	nativePGMoney(t, money, 98.75, 1)
}

func TestSystemOneJournalPostgresRejectsForgedScopeAndCrossProtocol(t *testing.T) {
	for _, kind := range []string{"price", "protocol", "identity", "funding"} {
		t.Run(kind, func(t *testing.T) {
			repo, p, money := systemOnePGFixture(t)
			// Detach nested frozen policy before intentionally forging the scope.
			raw, err := json.Marshal(p)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &p))
			switch kind {
			case "price":
				p.Scope.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 99
			case "protocol":
				p.Scope.Request.Protocol = ""
			case "identity":
				p.Identity.APIKeyID++
			case "funding":
				p.Scope.Attempt.FrozenPricing.Policy.AccountRateMultiplier = 99
			}
			p.Intent, err = p.Scope.NativeIntentReference()
			require.NoError(t, err)
			p.Settlement, p.SettlementUsage = systemOneJournalCommand(p)
			got, err := repo.RecordSystemOneUsage(t.Context(), &p)
			require.Error(t, err)
			require.False(t, got.Billing.Durable)
			nativePGMoney(t, money, 100, 0)
		})
	}
}

func TestSystemOneJournalPostgresRejectedAttemptThenSuccessKeepsOneRoot(t *testing.T) {
	repo, p, money := systemOnePGFixture(t)
	rejected := p
	rejected.StatusCode = 403
	rejected.InputTokens, rejected.OutputTokens = nil, nil
	rejected.Operation.Attempts = append([]model.AttemptFact(nil), p.Operation.Attempts...)
	rejected.Operation.Attempts[0].Usage = nil
	rejected.Operation.Attempts[0].Terminal = model.TerminalUnknown
	rejected.Operation.Attempts[0].Generation = model.GenerationRejected
	rejected.Operation.Attempts[0].RequestID = "upstream-rejection"
	got, err := repo.RecordSystemOneUsage(t.Context(), &rejected)
	require.NoError(t, err)
	require.Equal(t, model.BillingPendingUsage, got.Billing.Disposition)
	next := p.Scope
	next.Attempt.Sequence = 2
	_, err = repo.StoreNativeUsageIntent(t.Context(), &next)
	require.NoError(t, err)
	p.Scope, p.Intent = next, billing.NativeIntentReference{}
	p.Intent, err = next.NativeIntentReference()
	require.NoError(t, err)
	success := p.Operation.Attempts[0]
	success.Sequence, success.RequestID = 2, "upstream-success"
	p.Operation.Attempts = []model.AttemptFact{rejected.Operation.Attempts[0], success}
	p.Settlement, p.SettlementUsage = systemOneJournalCommand(p)
	got, err = repo.RecordSystemOneUsage(t.Context(), &p)
	require.NoError(t, err)
	require.Equal(t, model.BillingSettled, got.Billing.Disposition)
	stored, err := repo.LoadNativeIntentRecovery(t.Context(), p.Intent.Key)
	require.NoError(t, err)
	require.Len(t, stored.Finalization.Evidence.Observations, 2)
	require.Equal(t, uint64(2), stored.Finalization.Intent.AttemptSequence)
	nativePGMoney(t, money, 98.75, 1)
}

func TestSystemOneJournalPostgresMissingMoneyIdentityDoesNotClaim(t *testing.T) {
	repo, p, money := systemOnePGFixture(t)
	p.Identity.RequestID = ""
	got, err := repo.RecordSystemOneUsage(t.Context(), &p)
	require.NoError(t, err)
	require.True(t, got.Billing.Durable)
	require.Equal(t, model.BillingPendingUsage, got.Billing.Disposition)
	repo.ConfigureSystemOneIntentPreparation(func(context.Context, *service.SystemOneUsagePreparation) (*service.UsageBillingCommand, *service.UsagePricingRequest, error) {
		t.Error("missing money identity must not be invented by recovery")
		return nil, nil, billing.ErrNativePreparationPending
	})
	n, err := repo.RecoverPendingUsageBilling(t.Context(), 16, nil)
	require.NoError(t, err)
	require.Zero(t, n)
	nativePGMoney(t, money, 100, 0)
	// Only authoritative late identity may bind the original final facts.
	p.Identity = money.Evidence.Identity
	p.Settlement, p.SettlementUsage = systemOneJournalCommand(p)
	got, err = repo.RecordSystemOneUsage(t.Context(), &p)
	require.NoError(t, err)
	require.Equal(t, model.BillingSettled, got.Billing.Disposition)
	nativePGMoney(t, money, 98.75, 1)
}

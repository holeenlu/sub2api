//go:build unit

package repository

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func nativeI2SQLFixture(t *testing.T) (service.NativeUsageIntent, service.NativeIntentFinalizationInput) {
	t.Helper()
	intent := nativeIntentRepositoryInput(t)
	images := false
	intent.Request.Billing = &service.NativeIntentRequestBillingPolicy{Version: 1, QuotaPlatform: service.PlatformOpenAI, GroupImageCountBilling: &images}
	intent.Attempt.Billing = &service.NativeIntentAttemptBillingPolicy{Version: 1, AccountType: service.AccountTypeAPIKey, AccountPlatform: service.PlatformOpenAI, Stats: &service.NativeIntentStatsPricing{State: service.NativeIntentStatsNotApplicable}}
	evidence := nativeRepositoryFinal(t, nativeRepositoryInput("intent-sql", 3, 2, 7))
	evidence.Settlement.AccountType = service.AccountTypeAPIKey
	evidence.Settlement.RequestFingerprint = ""
	input := service.NativeIntentFinalizationInput{Evidence: evidence, Preparation: &service.NativeUsagePreparation{Result: service.NativeUsagePreparationResult{Model: "native-test", Usage: service.OpenAIUsage{InputTokens: 10}}}}
	return intent, nativeI2RebindFixture(t, intent, input)
}

func TestNativeIntentV2CannotBypassTechnicalBindingThroughOldEntries(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	_, input := nativeI2SQLFixture(t)
	repo := &usageBillingRepository{db: db}
	got, err := repo.RecordNativeUsageEvidence(t.Context(), &input.Evidence)
	require.ErrorIs(t, err, billing.ErrNativeIntentInvalid)
	require.False(t, got.Receipt.Durable())
	require.Nil(t, got.ActualApplyResult)
	receipt, command, err := (&usageBillingLedger{repo: repo}).PreserveNativeUsage(t.Context(), &input.Evidence)
	require.ErrorIs(t, err, billing.ErrNativeIntentInvalid)
	require.False(t, receipt.Durable())
	require.Nil(t, command)
	require.NoError(t, mock.ExpectationsWereMet(), "old public entries must not issue even an INSERT for self-declared v2 evidence")
}

func TestNativeIntentPrivateCanonicalWriterRequiresMatchingBinding(t *testing.T) {
	for _, kind := range []string{"absent", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			_, input := nativeI2SQLFixture(t)
			mock.ExpectBegin()
			mock.ExpectRollback()
			tx, err := db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			var binding *nativeCanonicalBinding
			if kind == "foreign" {
				binding = &nativeCanonicalBinding{key: billing.NativeIntentKey{OperationID: "another-root", APIKeyID: input.Intent.Key.APIKeyID}}
			}
			receipt, command, err := (&usageBillingLedger{repo: &usageBillingRepository{db: db}}).preserveNativeUsageTx(t.Context(), tx, &input.Evidence, binding)
			require.ErrorIs(t, err, billing.ErrNativeIntentInvalid, "v2 helper admission must precede canonical SQL")
			require.False(t, receipt.Durable())
			require.Nil(t, command)
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestNativeIntentFinalizationSQLFailureCannotAcknowledgePreparedFacts(t *testing.T) {
	for _, stage := range []string{"begin", "load", "update", "missing_row", "commit"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			intent, input := nativeI2SQLFixture(t)
			record, err := service.MergeNativeUsageIntent(nil, intent)
			require.NoError(t, err)
			raw, err := json.Marshal(record)
			require.NoError(t, err)
			input.Evidence.Settlement = nil
			input.Evidence.SettlementUsage = nil
			input.Reason = billing.NativeBuilderUnavailable
			failure := errors.New("isolated technical journal SQL failure")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				load := mock.ExpectQuery("SELECT native_intent,native_intent_fingerprint,status,proposed_request_id,canonical_request_id,native_evidence,native_evidence_fingerprint")
				if stage == "load" {
					load.WillReturnError(failure)
				} else {
					load.WillReturnRows(sqlmock.NewRows([]string{"intent", "fingerprint", "status", "proposed", "canonical", "evidence", "evidence_fingerprint"}).AddRow(raw, input.Intent.IntentFingerprint, "intent_preserved", nil, nil, nil, nil))
					update := mock.ExpectExec("UPDATE usage_billing_events SET native_evidence")
					if stage == "update" {
						update.WillReturnError(failure)
					} else if stage == "missing_row" {
						update.WillReturnResult(sqlmock.NewResult(0, 0))
					} else {
						update.WillReturnResult(sqlmock.NewResult(0, 1))
						mock.ExpectCommit().WillReturnError(failure)
					}
				}
				if stage != "commit" {
					mock.ExpectRollback()
				}
			}
			got, err := (&usageBillingRepository{db: db}).FinalizeNativeUsageIntent(t.Context(), &input)
			require.False(t, got.Intent.Durable())
			require.Nil(t, got.Canonical)
			if stage == "missing_row" {
				require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
			} else {
				require.ErrorIs(t, err, failure)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

//go:build unit

package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNativeIntentSQLFailureNeverConfirmsPreservation(t *testing.T) {
	for _, stage := range []string{"begin", "insert", "select", "update", "missing_row", "commit", "success"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			input := nativeIntentRepositoryInput(t)
			record, err := service.MergeNativeUsageIntent(nil, input)
			require.NoError(t, err)
			raw, err := json.Marshal(record)
			require.NoError(t, err)
			ref, err := input.NativeIntentReference()
			require.NoError(t, err)
			failure := errors.New("isolated intent SQL failure")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				insert := mock.ExpectExec("INSERT INTO usage_billing_events")
				if stage == "insert" {
					insert.WillReturnError(failure)
				} else {
					insert.WillReturnResult(sqlmock.NewResult(0, 1))
					read := mock.ExpectQuery("SELECT request_id,request_fingerprint,payload,status,native_intent,native_intent_fingerprint").WithArgs(input.Key.OperationID, input.Key.APIKeyID)
					if stage == "select" {
						read.WillReturnError(failure)
					} else {
						read.WillReturnRows(sqlmock.NewRows([]string{"request_id", "request_fingerprint", "payload", "status", "native_intent", "native_intent_fingerprint"}).AddRow(nil, "", nil, "intent_preserved", raw, ref.IntentFingerprint))
						update := mock.ExpectExec("UPDATE usage_billing_events SET")
						if stage == "update" {
							update.WillReturnError(failure)
						} else if stage == "missing_row" {
							update.WillReturnResult(sqlmock.NewResult(0, 0))
						} else {
							update.WillReturnResult(sqlmock.NewResult(0, 1))
							commit := mock.ExpectCommit()
							if stage == "commit" {
								commit.WillReturnError(failure)
							}
						}
					}
				}
				if stage == "insert" || stage == "select" || stage == "update" || stage == "missing_row" {
					mock.ExpectRollback()
				}
			}
			svc, err := service.ProvideNativeBillingIntentService(&usageBillingRepository{db: db})
			require.NoError(t, err)
			receipt, err := svc.PreserveBeforeSend(t.Context(), input)
			if stage == "success" {
				require.NoError(t, err)
				require.True(t, receipt.Durable())
				require.Equal(t, ref, receipt.NativeIntentReference)
			} else {
				require.False(t, receipt.Durable())
				if stage == "missing_row" {
					require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
				} else {
					require.ErrorIs(t, err, failure)
				}
			}
			require.NoError(t, mock.ExpectationsWereMet(), "intent must never invoke a monetary Apply")
		})
	}
}

func TestNativeIntentSQLRejectsMoneyShapedTechnicalRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO usage_billing_events").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT request_id,request_fingerprint,payload,status,native_intent,native_intent_fingerprint").WillReturnRows(sqlmock.NewRows([]string{"request_id", "request_fingerprint", "payload", "status", "native_intent", "native_intent_fingerprint"}).AddRow("provider-id", "old-v1", []byte("money"), "applied", nil, nil))
	mock.ExpectRollback()
	input := nativeIntentRepositoryInput(t)
	receipt, err := (&usageBillingRepository{db: db}).StoreNativeUsageIntent(t.Context(), &input)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
	require.False(t, receipt.Durable())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNativeIntentSQLLoadFailureIsNotPreservation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT native_intent,native_intent_fingerprint").WillReturnError(sql.ErrConnDone)
	got, err := (&usageBillingRepository{db: db}).LoadNativeUsageIntent(t.Context(), billing.NativeIntentKey{OperationID: "operation", APIKeyID: 2})
	require.Nil(t, got)
	require.ErrorIs(t, err, sql.ErrConnDone)
	require.NoError(t, mock.ExpectationsWereMet())
}

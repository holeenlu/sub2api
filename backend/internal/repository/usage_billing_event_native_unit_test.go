//go:build unit

package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNativeUsageEvidenceSQLFailureNeverClaimsDurability(t *testing.T) {
	for _, stage := range []string{"begin", "insert", "select", "update", "commit", "success"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			input, err := service.PrepareNativeUsageEvidence(nativeRepositoryInput("native-sql", 1, 2, 3))
			require.NoError(t, err)
			record := service.NativeUsageEvidenceRecord{Version: 1, Evidence: input.Evidence, Observations: []billing.NativeUsageEvidence{input.Evidence}, FrozenPricing: input.FrozenPricing}
			raw, err := json.Marshal(record)
			require.NoError(t, err)
			failure := errors.New("isolated SQL failure")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				insert := mock.ExpectExec("INSERT INTO usage_billing_events")
				if stage == "insert" {
					insert.WillReturnError(failure)
				} else {
					insert.WillReturnResult(sqlmock.NewResult(0, 1))
					query := mock.ExpectQuery("SELECT request_fingerprint,payload,status,native_evidence,native_evidence_fingerprint").WithArgs("native-sql", int64(2))
					if stage == "select" {
						query.WillReturnError(failure)
					} else {
						query.WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint", "payload", "status", "native_evidence", "native_evidence_fingerprint"}).AddRow("", nil, "pending_usage", raw, input.Evidence.Fingerprint()))
						update := mock.ExpectExec("UPDATE usage_billing_events SET")
						if stage == "update" {
							update.WillReturnError(failure)
						} else {
							update.WillReturnResult(sqlmock.NewResult(0, 1))
							commit := mock.ExpectCommit()
							if stage == "commit" {
								commit.WillReturnError(failure)
							}
						}
					}
				}
				if stage == "insert" || stage == "select" || stage == "update" {
					mock.ExpectRollback()
				}
			}
			svc, err := service.ProvideNativeUsageEvidenceService(&usageBillingRepository{db: db})
			require.NoError(t, err)
			receipt, err := svc.Record(t.Context(), *input)
			if stage == "success" {
				require.NoError(t, err)
				require.True(t, receipt.Receipt.Durable())
				require.Equal(t, billing.DurablePendingUsage, receipt.Receipt.Disposition)
				require.False(t, receipt.Receipt.RecoveryPending, "unknown facts are not an automatic monetary recovery job")
			} else {
				require.ErrorIs(t, err, failure)
				require.False(t, receipt.Receipt.Durable())
			}
			require.Nil(t, receipt.ActualApplyResult, "journal-only SQL never observes a money commit")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func TestNativeUsageEvidenceSQLRejectsUnknownOverExistingV1(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO usage_billing_events").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT request_fingerprint,payload,status,native_evidence,native_evidence_fingerprint").WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint", "payload", "status", "native_evidence", "native_evidence_fingerprint"}).AddRow("existing-fingerprint", []byte("old-v1"), "applied", nil, nil))
	mock.ExpectRollback()
	receipt, err := (&usageBillingRepository{db: db}).RecordNativeUsageEvidence(t.Context(), ptrNativeInput(nativeRepositoryInput("old-root", 1, 2, 3)))
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	require.False(t, receipt.Receipt.Durable())
	require.NoError(t, mock.ExpectationsWereMet())
}
func ptrNativeInput(input service.NativeUsageEvidenceInput) *service.NativeUsageEvidenceInput {
	return &input
}
func TestNativeUsageEvidenceSQLLoadFailureIsNotReceipt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT native_evidence,native_evidence_fingerprint,status,payload,request_fingerprint").WillReturnError(sql.ErrConnDone)
	value, err := (&usageBillingRepository{db: db}).LoadNativeUsageEvidence(t.Context(), billing.Identity{RequestID: "load", APIKeyID: 2})
	require.ErrorIs(t, err, sql.ErrConnDone)
	require.Nil(t, value)
	require.NoError(t, mock.ExpectationsWereMet())
}

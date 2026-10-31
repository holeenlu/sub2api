//go:build unit

package repository

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNativeIntentCloseSQLRequiresCommitAndCompleteScope(t *testing.T) {
	for _, stage := range []string{"begin", "select", "already_unknown", "missing_scope", "update", "missing_row", "commit", "success"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			intent := nativeIntentRepositoryInput(t)
			record, err := service.MergeNativeUsageIntent(nil, intent)
			require.NoError(t, err)
			ref, err := intent.NativeIntentReference()
			require.NoError(t, err)
			in := service.NativeIntentNotSentInput{Key: intent.Key, IntentFingerprint: ref.IntentFingerprint, Operation: model.OperationResult{OperationID: intent.Key.OperationID, APIKeyID: intent.Key.APIKeyID, UserID: intent.UserID, OriginalWireDigest: intent.OriginalWireDigest, State: model.OperationCanceled, Attempts: []model.AttemptFact{{Sequence: 1, Identity: intent.Attempt.Prepared, Send: model.SendNotSent}}}}
			if stage == "missing_scope" {
				intent.Attempt.Sequence = 2
				record, err = service.MergeNativeUsageIntent(record, intent)
				require.NoError(t, err)
			}
			raw, err := json.Marshal(record)
			require.NoError(t, err)
			failure := errors.New("isolated closure SQL failure")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				read := mock.ExpectQuery("SELECT native_intent,native_intent_fingerprint,status,native_evidence,canonical_request_id,native_intent_closure,native_intent_closure_fingerprint")
				if stage == "select" {
					read.WillReturnError(failure)
				} else {
					status := "intent_preserved"
					if stage == "already_unknown" {
						status = "pending_usage"
					}
					read.WillReturnRows(sqlmock.NewRows([]string{"intent", "fingerprint", "status", "evidence", "canonical", "closure", "closure_fingerprint"}).AddRow(raw, ref.IntentFingerprint, status, nil, nil, nil, nil))
					if stage != "already_unknown" && stage != "missing_scope" {
						update := mock.ExpectExec("UPDATE usage_billing_events SET status='intent_not_sent'")
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
				if stage != "commit" && stage != "success" {
					mock.ExpectRollback()
				}
			}
			svc, err := service.ProvideNativeBillingIntentService(&usageBillingRepository{db: db})
			require.NoError(t, err)
			got, err := svc.CloseNotSent(t.Context(), in)
			if stage == "success" {
				require.NoError(t, err)
				require.True(t, got.Durable())
			} else {
				require.Error(t, err)
				require.False(t, got.Durable())
				if stage == "already_unknown" || stage == "missing_scope" || stage == "missing_row" {
					require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
				} else {
					require.ErrorIs(t, err, failure)
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

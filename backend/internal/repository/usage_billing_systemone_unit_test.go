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

func TestSystemOneJournalSQLFailureNeverAcknowledgesFacts(t *testing.T) {
	for _, stage := range []string{"begin", "load", "write", "missing_row", "commit"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			p := systemOneJournalFixture(t, nativeIntentRepositoryInput(t), billing.Identity{RequestID: "original-money", APIKeyID: 2})
			stored, err := service.MergeNativeUsageIntent(nil, p.Scope)
			require.NoError(t, err)
			raw, err := json.Marshal(stored)
			require.NoError(t, err)
			mock.ExpectQuery("SELECT native_intent,native_intent_fingerprint FROM usage_billing_events").
				WillReturnRows(sqlmock.NewRows([]string{"intent", "fingerprint"}).AddRow(raw, p.Intent.IntentFingerprint))
			failure := errors.New("fixture durable write failure")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				load := mock.ExpectQuery("SELECT native_intent,native_intent_fingerprint,status,proposed_request_id,canonical_request_id,native_evidence,native_evidence_fingerprint")
				if stage == "load" {
					load.WillReturnError(failure)
				} else {
					load.WillReturnRows(sqlmock.NewRows([]string{"intent", "fingerprint", "status", "proposed", "canonical", "evidence", "hash"}).
						AddRow(raw, p.Intent.IntentFingerprint, "intent_preserved", nil, nil, nil, nil))
					write := mock.ExpectExec("UPDATE usage_billing_events SET native_evidence")
					switch stage {
					case "write":
						write.WillReturnError(failure)
					case "missing_row":
						write.WillReturnResult(sqlmock.NewResult(0, 0))
					case "commit":
						write.WillReturnResult(sqlmock.NewResult(0, 1))
						mock.ExpectCommit().WillReturnError(failure)
					}
				}
				if stage != "commit" {
					mock.ExpectRollback()
				}
			}
			got, err := (&usageBillingRepository{db: db}).RecordSystemOneUsage(t.Context(), &p)
			require.Error(t, err)
			require.False(t, got.Billing.Durable)
			require.Nil(t, got.ActualApplyResult)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

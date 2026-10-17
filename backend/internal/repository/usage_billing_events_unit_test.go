//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

type sanitizedBillingPayload struct{ t *testing.T }

func (m sanitizedBillingPayload) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	require.NotContains(m.t, raw, "secret-key")
	require.NotContains(m.t, raw, "secret-token")
	var cmd service.UsageBillingCommand
	require.NoError(m.t, json.Unmarshal([]byte(raw), &cmd))
	return cmd.UsageLog != nil && cmd.UsageLog.APIKey == nil && cmd.UsageLog.Account == nil
}
func TestUsageBillingEventTransactionAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback_log_failure"}[fail], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := &usageBillingRepository{db: db, invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
			log := &service.UsageLog{UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "journal", Model: "test", ActualCost: 2, TotalCost: 2, CreatedAt: time.Now(), APIKey: &service.APIKey{Key: "secret-key"}, Account: &service.Account{Credentials: map[string]any{"access_token": "secret-token"}}}
			cmd := &service.UsageBillingCommand{UsageLog: log, RequestID: log.RequestID, UserID: 1, APIKeyID: 2, AccountID: 3, BalanceCost: 2}
			cmd.Normalize()
			mock.ExpectQuery("INSERT INTO usage_billing_events").WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint, sanitizedBillingPayload{t}, "pending").WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
			mock.ExpectBegin()
			mock.ExpectQuery("INSERT INTO usage_billing_dedup ").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectQuery("SELECT request_fingerprint.*").WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery(conditionalBalanceDeductSQL).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(98))
			exec := mock.ExpectExec("INSERT INTO usage_logs")
			if fail {
				exec.WillReturnError(errors.New("log insert failed"))
				mock.ExpectRollback()
				mock.ExpectExec("UPDATE usage_billing_events SET status").WillReturnResult(sqlmock.NewResult(0, 1))
			} else {
				exec.WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("UPDATE usage_billing_events SET status='settled'").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
				mock.ExpectExec("UPDATE usage_billing_events SET status='applied'").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			_, err = repo.Apply(context.Background(), cmd)
			if fail {
				require.Error(t, err)
				require.False(t, log.SettlementRecorded)
			} else {
				require.NoError(t, err)
				require.True(t, log.SettlementRecorded)
				require.Equal(t, "settled", log.BillingStatus)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func TestUsageBillingPendingPricingDoesNotDebit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cmd := &service.UsageBillingCommand{RequestID: "unknown", APIKeyID: 2, UsageLog: &service.UsageLog{Model: "unknown"}, Pricing: &service.UsageBillingPricingSnapshot{Pending: true}}
	cmd.Normalize()
	mock.ExpectQuery("INSERT INTO usage_billing_events").WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
	_, err = (&usageBillingRepository{db: db}).Apply(context.Background(), cmd)
	require.ErrorIs(t, err, service.ErrUsagePricingPending)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestUsageSettlementUpsertIncludesBillingState(t *testing.T) {
	var queries []string
	db, mock := newSQLCapturingMock(t, &queries)
	mock.ExpectExec("INSERT").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, execUsageSettlementLog(context.Background(), db, prepareUsageLogInsert(&service.UsageLog{RequestID: "x", APIKeyID: 1})))
	require.Contains(t, queries[0], "DO UPDATE SET")
	require.Contains(t, queries[0], "billing_status=EXCLUDED.billing_status")
	require.Contains(t, queries[0], "billing_mode=EXCLUDED.billing_mode")
	require.False(t, strings.Contains(queries[0], "DO NOTHING"))
}

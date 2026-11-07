//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
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
	if cmd.UsageLog == nil || cmd.UsageLog.APIKey != nil || cmd.UsageLog.Account != nil {
		return false
	}
	require.NotNil(m.t, cmd.UsageLog.LatencyMetrics)
	require.Equal(m.t, 1, cmd.UsageLog.LatencyMetrics.Version)
	require.NotNil(m.t, cmd.UsageLog.LatencyMetrics.FirstEventMs)
	require.Zero(m.t, *cmd.UsageLog.LatencyMetrics.FirstEventMs)
	require.Nil(m.t, cmd.UsageLog.LatencyMetrics.FirstTextMs)
	require.Equal(m.t, 42, cmd.UsageLog.LatencyMetrics.DurationMs)
	return true
}
func TestUsageBillingEventTransactionAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback_log_failure"}[fail], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := &usageBillingRepository{db: db, invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
			log := &service.UsageLog{UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "journal", Model: "test", ActualCost: 2, TotalCost: 2, CreatedAt: time.Now(), APIKey: &service.APIKey{Key: "secret-key"}, Account: &service.Account{Credentials: map[string]any{"access_token": "secret-token"}}}
			first := 0
			log.LatencyMetrics = &service.UsageLatencyMetrics{Version: 1, FirstEventMs: &first, DurationMs: 42}
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

// V1 keeps lower-bound evidence across journal/reprice/crash boundaries without
// adding an external field or deriving usage from a price/token zero value.
func TestUsageBillingV1SnapshotPreservesLowerBoundAndFrozenFacts(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for _, amount := range []float64{0, 1.234567895} {
			t.Run(fmt.Sprintf("pending_%t_amount_%v", pending, amount), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer db.Close()
				vendorID := int64(71)
				log := &service.UsageLog{RequestID: "partial", APIKeyID: 2, ActualCost: amount, UsageLowerBound: true, BillingStatus: "pending_pricing", VendorIDSnapshot: &vendorID, AuthorizationMode: "openai_cloud"}
				cmd := &service.UsageBillingCommand{RequestID: "partial", APIKeyID: 2, BalanceExempt: true, APIKeyQuotaCost: amount, UsageLog: log, UsageLogs: []*service.UsageLog{log}, Pricing: &service.UsageBillingPricingSnapshot{Pending: pending}}
				cmd.Normalize()
				fingerprint := cmd.RequestFingerprint
				var payload []byte
				status := "pending"
				if pending {
					status = "pending_pricing"
				}
				mock.ExpectQuery("INSERT INTO usage_billing_events").WithArgs(cmd.RequestID, cmd.APIKeyID, fingerprint, billingPayloadCapture{&payload}, status).WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(fingerprint))
				repo := &usageBillingRepository{db: db}
				require.NoError(t, repo.persistUsageBillingEvent(context.Background(), cmd))
				decoded, err := (&usageBillingLedger{}).Decode(billing.Event{Identity: cmd.BillingIdentity(), Payload: payload})
				require.NoError(t, err)
				got := decoded.(*service.UsageBillingCommand)
				require.Equal(t, 1, got.PayloadVersion)
				require.Equal(t, fingerprint, got.RequestFingerprint)
				require.True(t, got.BalanceExempt)
				require.Zero(t, got.BalanceCost)
				require.Equal(t, cmd.APIKeyQuotaCost, got.APIKeyQuotaCost)
				require.Equal(t, pending, got.Pricing.Pending)
				require.True(t, got.UsageLog.UsageLowerBound)
				require.True(t, got.UsageLogs[0].UsageLowerBound)
				require.Equal(t, "lower_bound", service.SettledUsageBillingStatus(got.UsageLog))
				require.Equal(t, &vendorID, got.UsageLog.VendorIDSnapshot)
				require.Equal(t, "openai_cloud", got.UsageLog.AuthorizationMode)
				require.NotContains(t, string(payload), "UsageLowerBound")
				require.Equal(t, "pending_pricing", log.BillingStatus, "serializing evidence must not mutate the live usage row")
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

type billingPayloadCapture struct{ target *[]byte }

func (m billingPayloadCapture) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if ok {
		*m.target = []byte(raw)
	}
	return ok
}

func TestUsageBillingRecoveryRejectsInvalidV1Evidence(t *testing.T) {
	valid := service.UsageBillingCommand{PayloadVersion: 1, RequestID: "root", APIKeyID: 2, UsageLog: &service.UsageLog{}}
	for _, tc := range []struct {
		name   string
		mutate func(*service.UsageBillingCommand)
	}{
		{"unknown version", func(c *service.UsageBillingCommand) { c.PayloadVersion = 2 }},
		{"absent version", func(c *service.UsageBillingCommand) { c.PayloadVersion = 0 }},
		{"different request", func(c *service.UsageBillingCommand) { c.RequestID = "other" }},
		{"different key", func(c *service.UsageBillingCommand) { c.APIKeyID = 9 }},
		{"missing usage", func(c *service.UsageBillingCommand) { c.UsageLog = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := valid
			tc.mutate(&cmd)
			raw, err := json.Marshal(cmd)
			require.NoError(t, err)
			_, err = (&usageBillingLedger{}).Decode(billing.Event{Identity: valid.BillingIdentity(), Payload: raw})
			require.ErrorContains(t, err, "settlement identity mismatch")
		})
	}
	_, err := (&usageBillingLedger{}).Decode(billing.Event{Identity: valid.BillingIdentity(), Payload: []byte("invalid-json")})
	require.ErrorContains(t, err, "invalid persisted settlement snapshot")
}

func TestUsageBillingRecoverySavedPriceIdentityIsRevalidated(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	original := &service.UsageBillingCommand{PayloadVersion: 1, RequestID: "root", APIKeyID: 2, UsageLog: &service.UsageLog{}}
	wrong := *original
	wrong.APIKeyID = 3
	raw, err := json.Marshal(wrong)
	require.NoError(t, err)
	mock.ExpectQuery("UPDATE usage_billing_events SET payload=CASE").WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(raw))
	_, err = (&usageBillingLedger{repo: &usageBillingRepository{db: db}}).FreezePrice(context.Background(), original)
	require.ErrorContains(t, err, "settlement identity mismatch")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingApplyRejectsMissingFactsBeforeDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := &usageBillingRepository{db: db}
	for _, cmd := range []*service.UsageBillingCommand{nil, {RequestID: "no-usage", APIKeyID: 2, BalanceCost: 1}} {
		result, err := repo.Apply(context.Background(), cmd)
		require.Nil(t, result)
		require.ErrorIs(t, err, service.ErrUsageBillingRecordRequired)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

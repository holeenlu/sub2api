//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Stage the same atomic technical+money rows as RecordBPSUsageEvidence, then
// lose the submitting process before Apply. The production serializer owns
// counters, lower-bound markers, canonical identity and the monetary snapshot.
func stageBPSReviewJournal(t *testing.T, input service.BPSUsageJournalInput) {
	t.Helper()
	record, err := prepareBPSUsageEvidence(input)
	require.NoError(t, err)
	raw, fingerprint, err := encodeBPSUsageEvidence(record)
	require.NoError(t, err)
	require.NotNil(t, record.Command)
	payload, err := usageBillingEventPayload(record.Command)
	require.NoError(t, err)
	tx, err := integrationDB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(t.Context(), `INSERT INTO usage_billing_events
 (request_id,api_key_id,request_fingerprint,payload,status)
 VALUES($1,$2,$3,$4,'pending')`, record.Command.RequestID, record.Command.APIKeyID,
		record.Command.RequestFingerprint, string(payload))
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO usage_billing_events
 (event_type,request_id,api_key_id,request_fingerprint,payload,status,bps_operation_id,bps_first_sequence,bps_evidence,bps_evidence_fingerprint,bps_request_id)
 VALUES('bps_usage',NULL,$2,'',NULL,'pending',$1,$3,$4,$5,$6)`,
		record.Operation.OperationID, record.Operation.APIKeyID, record.Sequences[0], string(raw), fingerprint, record.CanonicalID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func TestBPSJournalReviewPostgresSingleUsageRowKeepsLowerBoundAfterRestart(t *testing.T) {
	for _, charged := range []bool{true, false} {
		name := "charged"
		if !charged {
			name = "zero_cost"
		}
		t.Run(name, func(t *testing.T) {
			_, input := bpsPGFixture(t)
			input = bpsPGFinal(input)
			attempt := &input.Finalization.Operation.Attempts[0]
			attempt.Terminal, attempt.Usage.Confidence, attempt.Usage.TerminalConfirmed = model.TerminalUnknown, model.UsagePartial, false
			input.Command.UsageLog.UsageLowerBound = true
			// This is the normal single-row result of splitOpenAIUsageAuthorization.
			// JSON decode must restore its separate object, not only UsageLog.
			input.Command.UsageLogs = []*service.UsageLog{input.Command.UsageLog}
			wantBalance := 98.75
			if !charged {
				input.Command.BalanceCost = 0
				input.Command.UsageLog.TotalCost, input.Command.UsageLog.ActualCost = 0, 0
				wantBalance = 100
			}
			stageBPSReviewJournal(t, input)
			bpsPGMoney(t, input, 100, 0)
			restarted := &usageBillingRepository{db: integrationDB,
				invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
			completed, err := restarted.recoverBPSUsageEvidence(t.Context(), 64)
			require.NoError(t, err)
			require.Positive(t, completed)
			var status, requestID string
			var inputTokens, outputTokens int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(),
				`SELECT billing_status,request_id,input_tokens,output_tokens FROM usage_logs WHERE api_key_id=$1`,
				input.Command.APIKeyID).Scan(&status, &requestID, &inputTokens, &outputTokens))
			require.Equal(t, "lower_bound", status, "neither a paid nor a free lower bound becomes a full settlement")
			require.Equal(t, input.Finalization.CanonicalRequestID, requestID)
			require.Equal(t, 7, inputTokens)
			require.Equal(t, 2, outputTokens)
			bpsPGMoney(t, input, wantBalance, 1)
			record, _, _, _ := bpsPGLoad(t, input)
			require.True(t, record.Command.UsageLog.UsageLowerBound)
			require.Len(t, record.Command.UsageLogs, 1)
			require.True(t, record.Command.UsageLogs[0].UsageLowerBound)
			_, err = restarted.RecordBPSUsageEvidence(t.Context(), record.input())
			require.NoError(t, err)
			bpsPGMoney(t, input, wantBalance, 1)
			require.NoError(t, integrationDB.QueryRowContext(t.Context(),
				"SELECT billing_status FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", requestID, input.Command.APIKeyID).Scan(&status))
			require.Equal(t, "lower_bound", status)
		})
	}
}

func TestBPSJournalReviewPostgresBadRowDoesNotBlockBPSOrMoney(t *testing.T) {
	for _, invalid := range []string{"unsupported_version", "invalid_shape", "fingerprint_mismatch"} {
		t.Run(invalid, func(t *testing.T) {
			repo, input := bpsPGFixture(t)
			input = bpsPGFinal(input)
			stageBPSReviewJournal(t, input)
			key := input.Finalization.Operation.APIKeyID
			// A separate ordinary money event must progress in the same host tick.
			ordinary := *input.Command
			ordinaryLog := *input.Command.UsageLog
			ordinary.RequestID, ordinary.RequestFingerprint, ordinary.BalanceCost = uuid.NewString(), "", 2
			ordinaryLog.RequestID, ordinaryLog.TotalCost, ordinaryLog.ActualCost = ordinary.RequestID, 2, 2
			ordinary.UsageLog, ordinary.UsageLogs = &ordinaryLog, []*service.UsageLog{&ordinaryLog}
			require.NoError(t, repo.persistUsageBillingEvent(t.Context(), &ordinary))
			badRaw := `{"Version":99}`
			if invalid == "invalid_shape" {
				badRaw = `{"Version":1}`
			} else if invalid == "fingerprint_mismatch" {
				record, err := prepareBPSUsageEvidence(input)
				require.NoError(t, err)
				raw, _, err := encodeBPSUsageEvidence(record)
				require.NoError(t, err)
				badRaw = string(raw)
			}
			fingerprint := strings.Repeat("0", 64)
			var badID int64
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), `INSERT INTO usage_billing_events
 (event_type,request_id,api_key_id,request_fingerprint,payload,status,bps_operation_id,bps_first_sequence,bps_evidence,bps_evidence_fingerprint,available_at)
 VALUES('bps_usage',NULL,$1,'',NULL,'pending_usage',$2,1,$3,$4,NOW()-INTERVAL '1 day') RETURNING journal_event_id`,
				key, uuid.NewString(), badRaw, fingerprint).Scan(&badID))
			var original []byte
			require.NoError(t, integrationDB.QueryRowContext(t.Context(),
				"SELECT bps_evidence FROM usage_billing_events WHERE journal_event_id=$1", badID).Scan(&original))
			bpsPGMoney(t, input, 100, 0)
			restarted := &usageBillingRepository{db: integrationDB,
				invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
			completed, err := restarted.RecoverPendingUsageBilling(t.Context(), 64, nil)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict, "the bad row remains an error after valid work finishes")
			require.GreaterOrEqual(t, completed, 2)
			bpsPGMoney(t, input, 96.75, 2)
			var current []byte
			var storedFingerprint, status, lastError string
			var attempts int
			var delayed bool
			require.NoError(t, integrationDB.QueryRowContext(t.Context(),
				`SELECT bps_evidence,bps_evidence_fingerprint,status,last_error,attempts,available_at>NOW()
 FROM usage_billing_events WHERE journal_event_id=$1`, badID).
				Scan(&current, &storedFingerprint, &status, &lastError, &attempts, &delayed))
			require.Equal(t, original, current)
			require.Equal(t, fingerprint, storedFingerprint)
			require.Equal(t, "pending_usage", status)
			require.Equal(t, "invalid_bps_evidence", lastError)
			require.Equal(t, 1, attempts)
			require.True(t, delayed)
			var money int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(),
				"SELECT COUNT(*) FROM usage_billing_events WHERE event_type='money' AND api_key_id=$1", key).Scan(&money))
			require.Equal(t, 2, money, "invalid evidence cannot create a zero-charge or synthetic money command")
			_, err = restarted.RecoverPendingUsageBilling(t.Context(), 64, nil)
			require.NoError(t, err, "the invalid row is not immediately reclaimed")
			bpsPGMoney(t, input, 96.75, 2)
			// Expiring only this fixture's lease proves repeated failure backs off
			// without removing evidence, changing money or repeatedly charging it.
			_, err = integrationDB.ExecContext(t.Context(),
				"UPDATE usage_billing_events SET attempts=8,available_at=NOW()-INTERVAL '1 second' WHERE journal_event_id=$1", badID)
			require.NoError(t, err)
			_, err = restarted.RecoverPendingUsageBilling(t.Context(), 64, nil)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			require.NoError(t, integrationDB.QueryRowContext(t.Context(),
				`SELECT bps_evidence,attempts,available_at>NOW()+INTERVAL '1 minute'
 FROM usage_billing_events WHERE journal_event_id=$1`, badID).Scan(&current, &attempts, &delayed))
			require.Equal(t, original, current)
			require.Equal(t, 9, attempts)
			require.True(t, delayed)
			bpsPGMoney(t, input, 96.75, 2)
		})
	}
}

//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Use the real migrated PostgreSQL and original money transaction. These are
// journal observations/quotes, not a substitute for the actual handler tests.
func bpsPGFixture(t *testing.T) (*usageBillingRepository, service.BPSUsageJournalInput) {
	t.Helper()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Balance: 100})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "bps-evidence"})
	account := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString(), Type: service.AccountTypeOAuth, Platform: service.PlatformOpenAI})
	id, operation := uuid.NewString(), uuid.NewString()
	digest := sha256.Sum256([]byte(operation))
	prepared := model.PreparedIdentity{OperationID: operation, UserID: user.ID, APIKeyID: key.ID, ResourceID: account.ID, Owner: model.ResourceOwner{AccountID: account.ID},
		Mode: "bps", Transport: "http", Role: model.AttemptGenerate, Binding: model.ModeBinding{BillingScope: operation}, BodyKind: model.BodyReplayable,
		BodyDigest: digest, GrantDigest: digest, TargetDigest: digest, WireDigest: digest}
	input := service.BPSUsageJournalInput{Finalization: service.BPSOperationFinalization{
		Operation: model.OperationResult{OperationID: operation, UserID: user.ID, APIKeyID: key.ID, Protocol: model.ProtocolResponses, OriginalWireDigest: digest,
			Attempts: []model.AttemptFact{{Sequence: 1, Identity: prepared, Send: model.SendSent, RequestID: id, Closed: true, EndOfStream: true}}},
		CurrentSequences: []uint64{1}, CanonicalRequestID: id}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Every row belongs to this fixture's new key, including unbound facts.
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM usage_billing_events WHERE event_type='bps_usage' AND api_key_id=$1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM usage_billing_events WHERE api_key_id=$1", key.ID)
		require.NoError(t, err)
	})
	return &usageBillingRepository{db: integrationDB, invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}, input
}

func bpsPGFinal(input service.BPSUsageJournalInput) service.BPSUsageJournalInput {
	input.Finalization.Operation.Attempts = append([]model.AttemptFact(nil), input.Finalization.Operation.Attempts...)
	a := &input.Finalization.Operation.Attempts[0]
	if a.ResponseID == "" {
		a.ResponseID = "response-" + a.RequestID
	}
	a.Terminal = model.TerminalCompleted
	a.Usage = &model.UsageFact{Input: &model.TokenMeasurement{Value: 7}, Output: &model.TokenMeasurement{Value: 2}, Confidence: model.UsageFinal, TerminalConfirmed: true}
	input.Finalization.UsageByAttempt = map[uint64]service.OpenAIUsage{1: {InputTokens: 7, OutputTokens: 2}}
	root := input.Finalization.Operation
	id := input.Finalization.CanonicalRequestID
	input.Command = &service.UsageBillingCommand{RequestID: id, APIKeyID: root.APIKeyID, UserID: root.UserID, AccountID: a.Identity.ResourceID,
		AccountType: service.AccountTypeOAuth, Model: "gpt-6-astra", InputTokens: 7, OutputTokens: 2, BalanceCost: 1.25,
		UsageLog: &service.UsageLog{RequestID: id, APIKeyID: root.APIKeyID, UserID: root.UserID, AccountID: a.Identity.ResourceID, Model: "gpt-6-astra",
			InputTokens: 7, OutputTokens: 2, TotalCost: 1.25, ActualCost: 1.25, CreatedAt: time.Now()}}
	input.ChargedSequences = []uint64{1}
	return input
}

func bpsPGBeforeSend(input service.BPSUsageJournalInput, sequence uint64) service.BPSUsageJournalInput {
	input.BeforeSendSequence = sequence
	input.Command, input.UsageLog, input.ChargedSequences = nil, nil, nil
	input.Finalization.CanonicalRequestID, input.Finalization.UsageByAttempt = "", nil
	input.Finalization.Operation.Attempts = append([]model.AttemptFact(nil), input.Finalization.Operation.Attempts...)
	for i := range input.Finalization.Operation.Attempts {
		attempt := &input.Finalization.Operation.Attempts[i]
		if attempt.Sequence != sequence {
			continue
		}
		attempt.Send, attempt.Closed, attempt.EndOfStream = model.SendUnknown, false, false
		attempt.RequestID, attempt.ResponseID, attempt.Terminal, attempt.Usage = "", "", model.TerminalUnknown, nil
	}
	return input
}

func bpsPGMoney(t *testing.T, input service.BPSUsageJournalInput, wantBalance float64, wantRows int) {
	t.Helper()
	root := input.Finalization.Operation
	var balance float64
	var rows, dedup int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1", root.UserID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_logs WHERE api_key_id=$1", root.APIKeyID).Scan(&rows))
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=$1", root.APIKeyID).Scan(&dedup))
	require.Equal(t, wantBalance, balance)
	require.Equal(t, wantRows, rows)
	require.Equal(t, wantRows, dedup)
}

func bpsPGLoad(t *testing.T, input service.BPSUsageJournalInput) (*bpsUsageEvidence, string, string, int) {
	t.Helper()
	var raw []byte
	var fingerprint, status string
	var attempts int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), `SELECT bps_evidence,bps_evidence_fingerprint,status,attempts FROM usage_billing_events
 WHERE event_type='bps_usage' AND bps_operation_id=$1 AND api_key_id=$2 AND bps_first_sequence=$3`,
		input.Finalization.Operation.OperationID, input.Finalization.Operation.APIKeyID, input.Finalization.CurrentSequences[0]).Scan(&raw, &fingerprint, &status, &attempts))
	record, err := decodeBPSUsageEvidence(raw, fingerprint)
	require.NoError(t, err, "PostgreSQL jsonb round-trip must preserve the evidence fingerprint")
	return record, fingerprint, status, attempts
}

func TestBPSJournalPostgres304MigrationShapesAndCanonicalCascade(t *testing.T) {
	tx := testTx(t)
	schema := "bps_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := tx.ExecContext(t.Context(), "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema+"; CREATE TABLE usage_logs(id BIGINT)")
	require.NoError(t, err)
	for _, name := range []string{"281_usage_billing_events.sql", "298_usage_billing_native_evidence.sql", "299_usage_billing_native_intent.sql", "300_usage_billing_native_intent_closure.sql", "301_usage_billing_native_canonical_binding.sql"} {
		ddl, err := dbmigrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), string(ddl))
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(t.Context(), `INSERT INTO usage_billing_events(request_id,api_key_id,request_fingerprint,payload,status)
 VALUES('original-money',2,'original-fingerprint','{"PayloadVersion":1,"BalanceCost":1.25}','pending')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO usage_billing_events(event_type,operation_id,api_key_id,request_id,request_fingerprint,payload,status,native_intent,native_intent_fingerprint)
 VALUES('native_intent','native-operation',2,NULL,'',NULL,'intent_preserved','{}','native-fingerprint')`)
	require.NoError(t, err)
	ddl, err := dbmigrations.FS.ReadFile("304_usage_billing_bps_evidence.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), string(ddl))
	require.NoError(t, err, "304 must execute on the existing customer event journal")
	var fingerprint, payload string
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT request_fingerprint,payload::text FROM usage_billing_events WHERE request_id='original-money' AND api_key_id=2").Scan(&fingerprint, &payload))
	require.Equal(t, "original-fingerprint", fingerprint)
	require.JSONEq(t, `{"PayloadVersion":1,"BalanceCost":1.25}`, payload)
	for _, operation := range []string{"bps-pending", "bps-completed"} {
		_, err = tx.ExecContext(t.Context(), `INSERT INTO usage_billing_events(event_type,request_id,api_key_id,request_fingerprint,payload,status,bps_operation_id,bps_first_sequence,bps_evidence,bps_evidence_fingerprint)
 VALUES('bps_usage',NULL,2,'',NULL,'pending_usage',$1,1,'{}',repeat('a',64))`, operation)
		require.NoError(t, err)
	}
	for _, change := range []string{
		"request_id='fake-money'", "payload='{}'", "request_fingerprint='fake-charge'", "status='applied'",
		"status='usage_recorded'", "bps_operation_id=NULL", "bps_first_sequence=NULL", "bps_first_sequence=0",
		"bps_evidence_fingerprint=NULL", "bps_evidence_fingerprint='malformed'", "bps_request_id='absent-money'", "api_key_id=0",
	} {
		_, err = tx.ExecContext(t.Context(), "SAVEPOINT bps_shape")
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), "UPDATE usage_billing_events SET "+change+" WHERE bps_operation_id='bps-pending'")
		require.Error(t, err, change)
		_, err = tx.ExecContext(t.Context(), "ROLLBACK TO SAVEPOINT bps_shape")
		require.NoError(t, err)
	}
	var deleteAction string
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT confdeltype::text FROM pg_constraint WHERE conrelid='usage_billing_events'::regclass AND conname='usage_billing_events_bps_canonical_fk'`).Scan(&deleteAction))
	require.Equal(t, "c", deleteAction)
	_, err = tx.ExecContext(t.Context(), "UPDATE usage_billing_events SET status='usage_recorded',bps_request_id='original-money' WHERE bps_operation_id='bps-completed'")
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), "DELETE FROM usage_billing_events WHERE request_id='original-money' AND api_key_id=2")
	require.NoError(t, err)
	var pending, completed, native int
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT COUNT(*) FILTER (WHERE bps_operation_id='bps-pending'),COUNT(*) FILTER (WHERE bps_operation_id='bps-completed'),COUNT(*) FILTER (WHERE event_type='native_intent') FROM usage_billing_events`).Scan(&pending, &completed, &native))
	require.Equal(t, 1, pending, "unbound unknown facts are independent of deleted money")
	require.Zero(t, completed)
	require.Equal(t, 1, native)
}

func TestBPSJournalPostgresUnknownSurvivesRestartWithoutMoney(t *testing.T) {
	repo, input := bpsPGFixture(t)
	input.Finalization.CanonicalRequestID = ""
	input.Finalization.Operation.Attempts[0].RequestID = ""
	result, err := repo.RecordBPSUsageEvidence(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, model.BillingPendingUsage, result.Receipts[0].Receipt.Disposition)
	require.True(t, result.Receipts[0].Receipt.Durable)
	require.Empty(t, result.Receipts[0].RootRequestID)
	fresh := &usageBillingRepository{db: integrationDB}
	record, fingerprint, status, attempts := bpsPGLoad(t, input)
	require.Nil(t, record.Command)
	require.Nil(t, record.Operation.Attempts[0].Usage)
	require.Empty(t, record.Usage)
	require.Equal(t, result.Receipts[0].Receipt.EvidenceID, fingerprint)
	require.Equal(t, "pending_usage", status)
	require.Zero(t, attempts)
	ledger := &usageBillingLedger{repo: fresh}
	claimed, err := ledger.Claim(t.Context(), 64)
	require.NoError(t, err)
	for _, event := range claimed {
		require.NotEqual(t, input.Finalization.Operation.APIKeyID, event.Identity.APIKeyID, "unknown BPS facts are never claimed as a money command")
	}
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET available_at=NOW()-INTERVAL '1 second',updated_at=NOW()-INTERVAL '31 days' WHERE bps_operation_id=$1 AND api_key_id=$2", input.Finalization.Operation.OperationID, input.Finalization.Operation.APIKeyID)
	require.NoError(t, err)
	_, err = fresh.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	record, _, status, attempts = bpsPGLoad(t, input)
	require.Equal(t, "pending_usage", status)
	require.Greater(t, attempts, 0, "the real host recovery entry must claim BPS facts after restart")
	require.Nil(t, record.Command)
	require.Nil(t, record.Operation.Attempts[0].Usage)
	var money, technicalMoney, reserved int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FILTER (WHERE event_type='money'),COUNT(*) FILTER (WHERE request_id IS NOT NULL),COUNT(*) FILTER (WHERE available_at>NOW()) FROM usage_billing_events WHERE api_key_id=$1`, input.Finalization.Operation.APIKeyID).Scan(&money, &technicalMoney, &reserved))
	require.Zero(t, money)
	require.Zero(t, technicalMoney)
	require.Equal(t, 1, reserved)
	bpsPGMoney(t, input, 100, 0)
}

func TestBPSJournalPostgresBeforeSendCrashAndSameAttemptCompletion(t *testing.T) {
	repo, input := bpsPGFixture(t)
	before := bpsPGBeforeSend(input, 1)
	result, err := repo.RecordBPSUsageEvidence(t.Context(), before)
	require.NoError(t, err)
	require.Len(t, result.Receipts, 1)
	require.True(t, result.Receipts[0].Receipt.Durable)
	require.Equal(t, model.BillingPendingUsage, result.Receipts[0].Receipt.Disposition)
	require.Empty(t, result.Receipts[0].RootRequestID)
	// No live operation survives. A fresh repository/host can only recover
	// journal work, never manufacture a send permit from this unknown intent.
	fresh := &usageBillingRepository{db: integrationDB, invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
	_, err = fresh.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	stored, _, state, claims := bpsPGLoad(t, before)
	require.Equal(t, uint64(1), stored.BeforeSendSequence)
	require.Equal(t, model.SendUnknown, stored.Operation.Attempts[0].Send)
	require.False(t, stored.Operation.Attempts[0].Closed)
	require.Nil(t, stored.Operation.Attempts[0].Usage)
	require.Nil(t, stored.Command)
	require.Empty(t, stored.CanonicalID)
	require.Equal(t, "pending_usage", state)
	require.Positive(t, claims)
	bpsPGMoney(t, input, 100, 0)
	final := bpsPGFinal(input)
	result, err = fresh.RecordBPSUsageEvidence(t.Context(), final)
	require.NoError(t, err)
	require.Equal(t, model.BillingSettled, result.Receipts[0].Receipt.Disposition)
	stored, _, state, _ = bpsPGLoad(t, final)
	require.Zero(t, stored.BeforeSendSequence)
	require.Equal(t, input.Finalization.CanonicalRequestID, stored.CanonicalID)
	require.Equal(t, "usage_recorded", state)
	var facts int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_events WHERE event_type='bps_usage' AND api_key_id=$1", input.Finalization.Operation.APIKeyID).Scan(&facts))
	require.Equal(t, 1, facts, "completion must promote the pre-send row, not create another charge")
	bpsPGMoney(t, input, 98.75, 1)
	_, err = fresh.RecordBPSUsageEvidence(t.Context(), before)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict, "a finalized attempt cannot become a fresh pre-send permission")
}

func TestBPSJournalPostgresBeforeSendCompactionAndGenerationShareOriginalMoney(t *testing.T) {
	repo, input := bpsPGFixture(t)
	input.Finalization.Operation.Attempts[0].Identity.Role = model.AttemptCompact
	_, err := repo.RecordBPSUsageEvidence(t.Context(), bpsPGBeforeSend(input, 1))
	require.NoError(t, err)
	completed := bpsPGFinal(input)
	generation := completed.Finalization.Operation.Attempts[0]
	generation.Sequence, generation.Identity.Role = 2, model.AttemptGenerate
	generation.RequestID, generation.ResponseID = uuid.NewString(), "generation-response"
	generation.Identity.BodyDigest = sha256.Sum256([]byte("actual-generation-wire"))
	completed.Finalization.Operation.Attempts = append(completed.Finalization.Operation.Attempts, generation)
	completed.Finalization.CurrentSequences = []uint64{1, 2}
	_, err = repo.RecordBPSUsageEvidence(t.Context(), bpsPGBeforeSend(completed, 2))
	require.NoError(t, err)
	stored, _, _, _ := bpsPGLoad(t, completed)
	require.Len(t, stored.Operation.Attempts, 2)
	require.Equal(t, model.TerminalCompleted, stored.Operation.Attempts[0].Terminal)
	require.Equal(t, model.SendUnknown, stored.Operation.Attempts[1].Send)
	require.Equal(t, uint64(2), stored.BeforeSendSequence)
	completed.BeforeSendSequence = 0
	completed.Finalization.CanonicalRequestID = generation.RequestID
	completed.Finalization.UsageByAttempt[2] = service.OpenAIUsage{InputTokens: 7, OutputTokens: 2}
	completed.Command.RequestID, completed.Command.UsageLog.RequestID = generation.RequestID, generation.RequestID
	completed.Command.InputTokens, completed.Command.OutputTokens, completed.Command.BalanceCost = 14, 4, 2.5
	completed.Command.UsageLog.InputTokens, completed.Command.UsageLog.OutputTokens = 14, 4
	completed.Command.UsageLog.TotalCost, completed.Command.UsageLog.ActualCost = 2.5, 2.5
	completed.ChargedSequences = []uint64{1, 2}
	result, err := repo.RecordBPSUsageEvidence(t.Context(), completed)
	require.NoError(t, err)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, generation.RequestID, result.Receipts[0].RootRequestID)
	require.Equal(t, []uint64{1, 2}, result.Receipts[0].Attempts)
	bpsPGMoney(t, completed, 97.5, 1)
}

func TestBPSJournalPostgresBeforeSendThenNotSentClosesOriginalEvidence(t *testing.T) {
	repo, input := bpsPGFixture(t)
	before := bpsPGBeforeSend(input, 1)
	_, err := repo.RecordBPSUsageEvidence(t.Context(), before)
	require.NoError(t, err)
	final := bpsPGBeforeSend(input, 1)
	final.BeforeSendSequence = 0
	final.Finalization.Operation.Attempts[0].Send = model.SendNotSent
	final.Finalization.Operation.Attempts[0].Closed = true
	result, err := repo.RecordBPSUsageEvidence(t.Context(), final)
	require.NoError(t, err)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, model.BillingNotRequired, result.Receipts[0].Receipt.Disposition)
	require.True(t, result.Receipts[0].Receipt.Durable)
	require.Empty(t, result.Receipts[0].RootRequestID)
	stored, _, state, _ := bpsPGLoad(t, input)
	require.Equal(t, "not_required", state)
	require.Equal(t, model.SendNotSent, stored.Operation.Attempts[0].Send)
	require.True(t, stored.Operation.Attempts[0].Closed)
	bpsPGMoney(t, input, 100, 0)
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET updated_at=NOW()-INTERVAL '31 days' WHERE bps_operation_id=$1 AND api_key_id=$2", input.Finalization.Operation.OperationID, input.Finalization.Operation.APIKeyID)
	require.NoError(t, err)
	(&usageBillingLedger{repo: repo}).Compact(t.Context())
	var rows int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_events WHERE api_key_id=$1", input.Finalization.Operation.APIKeyID).Scan(&rows))
	require.Zero(t, rows)
}

func TestBPSJournalPostgresRejectedControlAndSameRootRetry(t *testing.T) {
	repo, base := bpsPGFixture(t)
	base.Finalization.ClientTransport = service.OpenAIClientTransportWS
	first := bpsPGBeforeSend(base, 1)
	first.Finalization.Operation.Attempts[0].Identity.Role = model.AttemptControl
	first.Finalization.Operation.Attempts[0].Identity.Purpose = model.DispatchControl
	_, err := repo.RecordBPSUsageEvidence(t.Context(), first)
	require.NoError(t, err)
	first.BeforeSendSequence = 0
	rejected := &first.Finalization.Operation.Attempts[0]
	rejected.Send, rejected.Closed, rejected.Terminal = model.SendSent, true, model.TerminalFailed
	rejected.Generation, rejected.RejectionEvidenceID = model.GenerationRejected, "observed-upload-429"
	result, err := repo.RecordBPSUsageEvidence(t.Context(), first)
	require.NoError(t, err)
	require.Equal(t, model.BillingNotRequired, result.Receipts[0].Receipt.Disposition)
	_, firstFingerprint, status, _ := bpsPGLoad(t, first)
	require.Equal(t, "not_required", status)

	// Same operation and sequence owner; the durable rejected group is kept
	// intact while the actual next control/generation form their own group.
	next := bpsPGBeforeSend(base, 1)
	next.BeforeSendSequence = 2
	next.Finalization.CurrentSequences = []uint64{2}
	control := next.Finalization.Operation.Attempts[0]
	control.Sequence, control.Identity.Role, control.Identity.Purpose = 2, model.AttemptControl, model.DispatchControl
	next.Finalization.Operation.Attempts = []model.AttemptFact{*rejected, control}
	_, err = repo.RecordBPSUsageEvidence(t.Context(), next)
	require.NoError(t, err)
	next.Finalization.Operation.Attempts[1].Send = model.SendSent
	next.Finalization.Operation.Attempts[1].Closed = true
	next.Finalization.Operation.Attempts[1].Terminal = model.TerminalCompleted
	generation := bpsPGBeforeSend(base, 1).Finalization.Operation.Attempts[0]
	generation.Sequence = 3
	next.BeforeSendSequence = 3
	next.Finalization.CurrentSequences = []uint64{2, 3}
	next.Finalization.Operation.Attempts = append(next.Finalization.Operation.Attempts, generation)
	_, err = repo.RecordBPSUsageEvidence(t.Context(), next)
	require.NoError(t, err)
	final := bpsPGFinal(base)
	final.Finalization.Operation.Attempts[0].Sequence = 3
	final.Finalization.Operation.Attempts[0].ResponseID = base.Finalization.CanonicalRequestID
	final.Finalization.Operation.Attempts = append(next.Finalization.Operation.Attempts[:2:2], final.Finalization.Operation.Attempts[0])
	final.Finalization.CurrentSequences, final.ChargedSequences = []uint64{2, 3}, []uint64{3}
	final.Finalization.UsageByAttempt = map[uint64]service.OpenAIUsage{3: {InputTokens: 7, OutputTokens: 2}}
	result, err = repo.RecordBPSUsageEvidence(t.Context(), final)
	require.NoError(t, err)
	require.Len(t, result.Receipts, 2)
	bpsPGMoney(t, final, 98.75, 1)
	_, fingerprint, status, _ := bpsPGLoad(t, first)
	require.Equal(t, firstFingerprint, fingerprint, "later recovery cannot rewrite the closed rejection")
	require.Equal(t, "not_required", status)
	var evidence int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_events WHERE bps_operation_id=$1 AND api_key_id=$2", base.Finalization.Operation.OperationID, base.Finalization.Operation.APIKeyID).Scan(&evidence))
	require.Equal(t, 2, evidence)
}

func TestBPSJournalPostgresParallelOpenControlsAndByteBoundedEvidence(t *testing.T) {
	repo, input := bpsPGFixture(t)
	input.Finalization.Operation.Attempts[0].Identity.Role = model.AttemptControl
	input.Finalization.Operation.Attempts[0].Identity.Purpose = model.DispatchControl
	input = bpsPGBeforeSend(input, 1)
	_, err := repo.RecordBPSUsageEvidence(t.Context(), input)
	require.NoError(t, err)
	// Earlier uploads may still be open when the next registered control is
	// preserved. No arbitrary 64-attempt gate supersedes the 1-MiB journal cap.
	for sequence := uint64(2); sequence <= 65; sequence++ {
		next := input.Finalization.Operation.Attempts[0]
		next.Sequence = sequence
		next.Identity.WireDigest = sha256.Sum256([]byte(fmt.Sprint(sequence)))
		input.Finalization.Operation.Attempts = append(input.Finalization.Operation.Attempts, next)
		input.Finalization.CurrentSequences = append(input.Finalization.CurrentSequences, sequence)
		input.BeforeSendSequence = sequence
		_, err = repo.RecordBPSUsageEvidence(t.Context(), input)
		require.NoError(t, err, "registered sequence %d", sequence)
	}
	stored, _, _, _ := bpsPGLoad(t, input)
	require.Len(t, stored.Operation.Attempts, 65)
	require.Equal(t, uint64(65), stored.BeforeSendSequence)
	for _, attempt := range stored.Operation.Attempts {
		require.False(t, attempt.Closed)
		require.Equal(t, model.SendUnknown, attempt.Send)
		require.Nil(t, attempt.Usage)
	}
	input.BeforeSendSequence = 0
	for i := range input.Finalization.Operation.Attempts {
		input.Finalization.Operation.Attempts[i].Closed = true
		input.Finalization.Operation.Attempts[i].Send = model.SendSent
		input.Finalization.Operation.Attempts[i].Terminal = model.TerminalCompleted
	}
	result, err := repo.RecordBPSUsageEvidence(t.Context(), input)
	require.NoError(t, err)
	for _, receipt := range result.Receipts {
		require.Equal(t, model.BillingNotRequired, receipt.Receipt.Disposition)
	}
	bpsPGMoney(t, input, 100, 0)
}

func TestBPSJournalPostgresCanonicalIdempotenceAndLateConflict(t *testing.T) {
	repo, input := bpsPGFixture(t)
	// The actual WS bridge bills response.id even when HTTP correlation is
	// also available. Preserve both observations and the root's transport.
	input.Finalization.ClientTransport = service.OpenAIClientTransportWS
	input.Finalization.Operation.Attempts[0].ResponseID = input.Finalization.CanonicalRequestID
	input.Finalization.Operation.Attempts[0].RequestID = "http-" + input.Finalization.CanonicalRequestID
	unknown := input
	unknown.Finalization.Operation.Attempts = append([]model.AttemptFact(nil), input.Finalization.Operation.Attempts...)
	unknown.Finalization.CanonicalRequestID, unknown.Finalization.Operation.Attempts[0].RequestID = "", ""
	unknown.Finalization.Operation.Attempts[0].ResponseID = ""
	_, err := repo.RecordBPSUsageEvidence(t.Context(), unknown)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan service.BPSUsageJournalResult, 2)
	errors := make(chan error, 2)
	for range 2 {
		final := bpsPGFinal(input) // each caller owns its mutable money/log objects
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := repo.RecordBPSUsageEvidence(t.Context(), final)
			results <- result
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	applied := 0
	for result := range results {
		require.Len(t, result.Receipts, 1)
		require.Equal(t, model.BillingSettled, result.Receipts[0].Receipt.Disposition)
		require.Equal(t, input.Finalization.CanonicalRequestID, result.Receipts[0].RootRequestID)
		if result.ActualApply != nil && result.ActualApply.Applied {
			applied++
		}
	}
	require.Equal(t, 1, applied)
	bpsPGMoney(t, input, 98.75, 1)
	_, originalFingerprint, status, _ := bpsPGLoad(t, input)
	require.Equal(t, "usage_recorded", status)
	for _, change := range []string{"grant", "response", "canonical", "transport", "quantity", "price"} {
		late := bpsPGFinal(input)
		switch change {
		case "grant":
			late.Finalization.Operation.Attempts[0].Identity.GrantDigest = [32]byte{99}
		case "response":
			late.Finalization.Operation.Attempts[0].ResponseID = "foreign-response"
		case "canonical":
			late.Finalization.CanonicalRequestID = "foreign-money"
			late.Command.RequestID, late.Command.UsageLog.RequestID = "foreign-money", "foreign-money"
		case "transport":
			late.Finalization.ClientTransport = service.OpenAIClientTransportHTTP
		case "quantity":
			late.Finalization.Operation.Attempts[0].Usage.Input.Value++
		case "price":
			late.Command.BalanceCost++
		}
		result, err := repo.RecordBPSUsageEvidence(t.Context(), late)
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict, change)
		require.Empty(t, result.Receipts, change)
		_, fingerprint, _, _ := bpsPGLoad(t, input)
		require.Equal(t, originalFingerprint, fingerprint, "rejected late evidence must roll back")
	}
	bpsPGMoney(t, input, 98.75, 1)
}

func TestBPSJournalPostgresRecoveryAfterMoneyRollbackAndRestart(t *testing.T) {
	repo, input := bpsPGFixture(t)
	final := bpsPGFinal(input)
	// Inject one fixture-key-only SQL failure after the evidence transaction
	// commits. Recovery must use the original payload without repairing bytes.
	trigger := "bps_rollback_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	function := trigger + "_fn"
	_, err := integrationDB.ExecContext(t.Context(), fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fixture$
 BEGIN IF NEW.api_key_id=%d THEN RAISE EXCEPTION 'fixture money rollback'; END IF; RETURN NEW; END; $fixture$`, function, input.Finalization.Operation.APIKeyID))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(t.Context(), "CREATE TRIGGER "+trigger+" BEFORE INSERT ON usage_logs FOR EACH ROW EXECUTE FUNCTION "+function+"()")
	require.NoError(t, err)
	removeFault := func(ctx context.Context) {
		t.Helper()
		_, err := integrationDB.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+trigger+" ON usage_logs")
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DROP FUNCTION IF EXISTS "+function+"()")
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		removeFault(ctx)
	})
	result, err := repo.RecordBPSUsageEvidence(t.Context(), final)
	require.Error(t, err)
	require.Len(t, result.Receipts, 1)
	require.True(t, result.Receipts[0].Receipt.Durable)
	require.Equal(t, model.BillingRecoveryPending, result.Receipts[0].Receipt.Disposition)
	bpsPGMoney(t, input, 100, 0)
	stored, fingerprint, _, _ := bpsPGLoad(t, input)
	require.NotNil(t, stored.Command)
	removeFault(t.Context())
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET available_at=NOW()-INTERVAL '1 second' WHERE api_key_id=$1", input.Finalization.Operation.APIKeyID)
	require.NoError(t, err)
	fresh := &usageBillingRepository{db: integrationDB, invalidate: func(context.Context, *service.UsageBillingCommand) error { return nil }}
	_, err = fresh.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	bpsPGMoney(t, input, 98.75, 1)
	// The original host consumes the BPS row and original V1 money command;
	// only the transaction receipt can mark the technical binding complete.
	recovered, currentFingerprint, status, _ := bpsPGLoad(t, input)
	require.Equal(t, "usage_recorded", status)
	require.Equal(t, fingerprint, currentFingerprint)
	require.Equal(t, stored.Command.RequestFingerprint, recovered.Command.RequestFingerprint)
	receipts, err := fresh.bpsUsageReceipts(t.Context(), recovered, currentFingerprint)
	require.NoError(t, err)
	require.Equal(t, model.BillingSettled, receipts[0].Receipt.Disposition)
	_, err = fresh.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	bpsPGMoney(t, input, 98.75, 1)
}

func TestBPSJournalPostgresRetentionPrunesOnlyCompletedBindings(t *testing.T) {
	for _, state := range []string{"completed", "recent", "unknown", "mixed_unknown"} {
		t.Run(state, func(t *testing.T) {
			repo, input := bpsPGFixture(t)
			write := bpsPGFinal(input)
			if state == "unknown" {
				write = input
				write.Finalization.CanonicalRequestID = ""
			}
			if state == "mixed_unknown" {
				pending := input.Finalization.Operation.Attempts[0]
				pending.Sequence, pending.RequestID, pending.Identity.Role = 2, uuid.NewString(), model.AttemptRepair
				write.Finalization.Operation.Attempts = append(write.Finalization.Operation.Attempts, pending)
				write.Finalization.CurrentSequences = []uint64{1, 2}
			}
			_, err := repo.RecordBPSUsageEvidence(t.Context(), write)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET updated_at=NOW()-INTERVAL '31 days',available_at=NOW()+INTERVAL '1 hour' WHERE api_key_id=$1", input.Finalization.Operation.APIKeyID)
			require.NoError(t, err)
			if state == "recent" {
				_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET updated_at=NOW() WHERE event_type='bps_usage' AND api_key_id=$1", input.Finalization.Operation.APIKeyID)
				require.NoError(t, err)
			}
			(&usageBillingLedger{repo: repo}).Compact(t.Context())
			var money, facts int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FILTER (WHERE event_type='money'),COUNT(*) FILTER (WHERE event_type='bps_usage') FROM usage_billing_events WHERE api_key_id=$1", input.Finalization.Operation.APIKeyID).Scan(&money, &facts))
			switch state {
			case "completed":
				require.Zero(t, money)
				require.Zero(t, facts)
			case "unknown":
				require.Zero(t, money)
				require.Equal(t, 1, facts)
			default:
				require.Equal(t, 1, money)
				require.Equal(t, 1, facts)
			}
			if state != "unknown" {
				bpsPGMoney(t, input, 98.75, 1)
			} else {
				bpsPGMoney(t, input, 100, 0)
			}
			if state == "completed" {
				var status string
				err := integrationDB.QueryRowContext(t.Context(), "SELECT status FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", input.Finalization.CanonicalRequestID, input.Finalization.Operation.APIKeyID).Scan(&status)
				require.True(t, errors.Is(err, sql.ErrNoRows))
			}
		})
	}
}

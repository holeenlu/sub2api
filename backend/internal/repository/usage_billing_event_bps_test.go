//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func bpsRepositoryFixture(final bool) service.BPSUsageJournalInput {
	h := [32]byte{1}
	p := model.PreparedIdentity{OperationID: "bps-real-operation", UserID: 3, APIKeyID: 2, ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7},
		Mode: "bps", Transport: "http", Role: model.AttemptGenerate, Binding: model.ModeBinding{BillingScope: "bps-real-operation"}, BodyKind: model.BodyReplayable,
		BodyDigest: h, GrantDigest: h, TargetDigest: h, WireDigest: h}
	a := model.AttemptFact{Sequence: 1, Identity: p, Send: model.SendSent, RequestID: "bps-provider-money", Closed: true, EndOfStream: true}
	in := service.BPSUsageJournalInput{Finalization: service.BPSOperationFinalization{Operation: model.OperationResult{OperationID: p.OperationID, UserID: 3, APIKeyID: 2,
		Protocol: model.ProtocolResponses, OriginalWireDigest: h}, CurrentSequences: []uint64{1}, CanonicalRequestID: a.RequestID}}
	if final {
		a.Usage = &model.UsageFact{Input: &model.TokenMeasurement{Value: 7}, Output: &model.TokenMeasurement{Value: 2}, Confidence: model.UsageFinal, TerminalConfirmed: true}
		a.Terminal = model.TerminalCompleted
		in.Finalization.UsageByAttempt = map[uint64]service.OpenAIUsage{1: {InputTokens: 7, OutputTokens: 2}}
		in.Command = &service.UsageBillingCommand{RequestID: a.RequestID, APIKeyID: 2, UserID: 3, AccountID: 7, Model: "gpt-6-astra", InputTokens: 7, OutputTokens: 2,
			Pricing: &service.UsageBillingPricingSnapshot{Pending: true}, UsageLog: &service.UsageLog{RequestID: a.RequestID, APIKeyID: 2, UserID: 3, AccountID: 7, Model: "gpt-6-astra", InputTokens: 7, OutputTokens: 2}}
		in.ChargedSequences = []uint64{1}
	}
	in.Finalization.Operation.Attempts = []model.AttemptFact{a}
	return in
}

func expectBPSPreserve(t *testing.T, mock sqlmock.Sqlmock, input service.BPSUsageJournalInput, commitErr error) (*bpsUsageEvidence, string) {
	t.Helper()
	record, err := prepareBPSUsageEvidence(input)
	require.NoError(t, err)
	raw, fingerprint, err := encodeBPSUsageEvidence(record)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO usage_billing_events\\(event_type").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT bps_evidence,bps_evidence_fingerprint").WillReturnRows(sqlmock.NewRows([]string{"bps_evidence", "bps_evidence_fingerprint"}).AddRow(raw, fingerprint))
	if record.Command != nil {
		mock.ExpectQuery("INSERT INTO usage_billing_events\\(request_id").WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(record.Command.RequestFingerprint))
	}
	mock.ExpectExec("UPDATE usage_billing_events SET bps_evidence").WillReturnResult(sqlmock.NewResult(0, 1))
	if commitErr != nil {
		mock.ExpectCommit().WillReturnError(commitErr)
	} else {
		mock.ExpectCommit()
	}
	return record, fingerprint
}

func TestBPSJournalUnknownDurableReceiptRequiresCommittedFacts(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "uncertain_commit"}[fails], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			var commitErr error
			if fails {
				commitErr = errors.New("commit unavailable")
			}
			input := bpsRepositoryFixture(false)
			_, fingerprint := expectBPSPreserve(t, mock, input, commitErr)
			result, err := (&usageBillingRepository{db: db}).RecordBPSUsageEvidence(t.Context(), input)
			if fails {
				require.Error(t, err)
				require.Empty(t, result.Receipts)
			} else {
				require.NoError(t, err)
				require.Len(t, result.Receipts, 1)
				require.True(t, result.Receipts[0].Receipt.Durable)
				require.Equal(t, model.BillingPendingUsage, result.Receipts[0].Receipt.Disposition)
				require.Equal(t, fingerprint, result.Receipts[0].Receipt.EvidenceID)
			}
			require.Nil(t, result.ActualApply)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestBPSJournalPendingPriceUsesOriginalMoneyEventAndReceipt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	input := bpsRepositoryFixture(true)
	record, _ := expectBPSPreserve(t, mock, input, nil)
	// Original Apply journals the same V1 command and leaves repricing to its
	// existing recoverer. No second money mutation or fabricated settled ACK.
	mock.ExpectQuery("INSERT INTO usage_billing_events\\(request_id").WithArgs(record.CanonicalID, int64(2), record.Command.RequestFingerprint, sqlmock.AnyArg(), "pending_pricing").WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(record.Command.RequestFingerprint))
	mock.ExpectQuery("SELECT status,request_fingerprint").WillReturnRows(sqlmock.NewRows([]string{"status", "request_fingerprint"}).AddRow("pending_pricing", record.Command.RequestFingerprint))
	result, err := (&usageBillingRepository{db: db}).RecordBPSUsageEvidence(t.Context(), input)
	require.ErrorIs(t, err, service.ErrUsagePricingPending)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, model.BillingPendingPricing, result.Receipts[0].Receipt.Disposition)
	require.True(t, result.Receipts[0].Receipt.Durable)
	require.Equal(t, "bps-provider-money", result.Receipts[0].RootRequestID)
	require.Nil(t, result.ActualApply)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBPSJournalRecoveryClaimsUnknownWithoutInventingZeroCommand(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	record, err := prepareBPSUsageEvidence(bpsRepositoryFixture(false))
	require.NoError(t, err)
	raw, fingerprint, err := encodeBPSUsageEvidence(record)
	require.NoError(t, err)
	mock.ExpectQuery("WITH candidates AS").WithArgs(8).WillReturnRows(sqlmock.NewRows([]string{"journal_event_id", "bps_evidence", "bps_evidence_fingerprint"}).AddRow(int64(1), raw, fingerprint))
	mock.ExpectExec("UPDATE usage_billing_events SET available_at").WithArgs(record.Operation.OperationID, int64(2), uint64(1), "usage_unknown").WillReturnResult(sqlmock.NewResult(0, 1))
	completed, err := (&usageBillingRepository{db: db}).recoverBPSUsageEvidence(context.Background(), 8)
	require.NoError(t, err)
	require.Zero(t, completed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBPSJournalRejectsScopeDriftAndPreservesLowerBound(t *testing.T) {
	in := bpsRepositoryFixture(true)
	in.Finalization.Operation.Attempts[0].Usage.Confidence = model.UsagePartial
	in.Finalization.Operation.Attempts[0].Usage.TerminalConfirmed = false
	in.Finalization.Operation.Attempts[0].Terminal = model.TerminalUnknown
	in.Command.UsageLog.UsageLowerBound = true
	prior, err := prepareBPSUsageEvidence(in)
	require.NoError(t, err)
	require.Equal(t, "lower_bound", prior.Command.UsageLog.BillingStatus)
	require.True(t, prior.Command.UsageLog.UsageLowerBound)
	in.Finalization.Operation.Attempts[0].Identity.GrantDigest = [32]byte{4}
	next, err := prepareBPSUsageEvidence(in)
	require.NoError(t, err)
	_, err = mergeBPSUsageEvidence(prior, next)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
}

func TestBPSJournalCanonicalIDUsesObservedIngressContract(t *testing.T) {
	for _, transport := range []service.OpenAIClientTransport{service.OpenAIClientTransportHTTP, service.OpenAIClientTransportWS} {
		t.Run(string(transport), func(t *testing.T) {
			input := bpsRepositoryFixture(true)
			input.Finalization.ClientTransport = transport
			input.Finalization.Operation.Attempts[0].ResponseID = "bps-response-money"
			id := "bps-provider-money"
			if transport == service.OpenAIClientTransportWS {
				id = "bps-response-money"
			}
			input.Finalization.CanonicalRequestID = id
			input.Command.RequestID, input.Command.UsageLog.RequestID = id, id
			record, err := prepareBPSUsageEvidence(input)
			require.NoError(t, err)
			raw, fingerprint, err := encodeBPSUsageEvidence(record)
			require.NoError(t, err)
			restored, err := decodeBPSUsageEvidence(raw, fingerprint)
			require.NoError(t, err)
			require.Equal(t, transport, restored.ClientTransport)
			require.Equal(t, id, restored.CanonicalID)
			input.Finalization.CanonicalRequestID = input.Finalization.Operation.OperationID
			input.Command.RequestID, input.Command.UsageLog.RequestID = input.Finalization.CanonicalRequestID, input.Finalization.CanonicalRequestID
			_, err = prepareBPSUsageEvidence(input)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		})
	}
}

package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func responseModeFixture(t *testing.T, mode string) (*Operation, *Dispatcher, model.PreparedIdentity, model.AttemptTarget) {
	t.Helper()
	d, identity, target := dispatchFixture(t, t.Context(), nil)
	d.output.Configure(time.Minute, true, false)
	core, err := NewOperation(d, model.OperationResult{OperationID: identity.OperationID, UserID: identity.UserID, APIKeyID: identity.APIKeyID, OriginalWireDigest: [32]byte{11}, Protocol: model.ProtocolResponses})
	require.NoError(t, err)
	require.NoError(t, core.Begin(t.Context()))
	identity.Mode, identity.Role = mode, model.AttemptGenerate
	if mode == "bps" {
		identity.Binding = model.ModeBinding{BillingScope: identity.OperationID}
	} else {
		identity.Binding = model.ModeBinding{BillingRoot: "existing-root", SessionID: "session", OwnerEpoch: [32]byte{9}}
	}
	target.Limit = 3
	return core, d, identity, target
}

func responseModeTestAuthority() DispatchAuthority {
	return dispatchAuthorityFunc(func(_ context.Context, i model.PreparedIdentity) (DispatchValidation, error) {
		return DispatchValidation{Purpose: i.Purpose, Authorization: [32]byte{1}, Leases: [32]byte{2}}, nil
	})
}

func openModeAttempt(t *testing.T, op *Operation, identity model.PreparedIdentity, target model.AttemptTarget) uint64 {
	t.Helper()
	opens := 0
	sequence, err := op.OpenPreparedOnce(t.Context(), &operationProtocolPrepared{identity: identity, target: target, opens: &opens})
	require.NoError(t, err)
	require.Equal(t, 1, opens)
	return sequence
}

func TestResponseModeActionsUseOneBudgetAndOutputFence(t *testing.T) {
	op, d, identity, target := responseModeFixture(t, "bps")
	require.NoError(t, op.BindModeAuthority("bps", responseModeTestAuthority()))
	deadline := d.output.Snapshot().Deadline
	for _, role := range []model.AttemptRole{model.AttemptControl, model.AttemptPreparationRetry, model.AttemptCompact, model.AttemptRepair, model.AttemptGenerate} {
		identity.Role = role
		identity.Purpose, _ = role.Purpose()
		sequence := openModeAttempt(t, op, identity, target)
		op.Closed(sequence, nil)
	}
	require.Equal(t, 3, d.budget.Snapshot().Attempts)
	require.Equal(t, 1, d.budget.Snapshot().PreparationRetries)
	require.Equal(t, deadline, d.output.Snapshot().Deadline)
	require.Len(t, op.Snapshot().Attempts, 5)
	require.Zero(t, op.Snapshot().DelegatedCycles)
	op.Delivered(5, model.DeliveryReceipt{PossiblyCommitted: true})
	_, err := d.Admit(t.Context(), identity, target)
	require.ErrorIs(t, err, ErrDispatchOutputCommitted)
	require.Equal(t, 3, d.budget.Snapshot().Attempts)
}

func TestResponseModeBPSPinAndCanonicalReceiptScopes(t *testing.T) {
	op, d, identity, target := responseModeFixture(t, "bps")
	require.NoError(t, op.BindModeAuthority("bps", responseModeTestAuthority()))
	compact := openModeAttempt(t, op, identity, target)
	op.Closed(compact, nil)
	generation := openModeAttempt(t, op, identity, target)
	op.Closed(generation, nil)
	require.Empty(t, op.Snapshot().Attempts[0].BillingRoot)
	require.Nil(t, op.Snapshot().Attempts[0].Usage, "unknown remains unknown")
	settled := model.BillingReceipt{Disposition: model.BillingSettled, Durable: true, EvidenceID: "actual-journal"}
	require.NoError(t, op.RecordModeBilling(model.ModeBillingReceipt{RootRequestID: "actual-compact-id", APIKeyID: identity.APIKeyID, Attempts: []uint64{compact}, Receipt: settled}))
	require.Equal(t, model.BillingPendingUsage, op.Snapshot().Billing.Disposition, "one receipt cannot settle another attempt")
	require.NoError(t, op.RecordModeBilling(model.ModeBillingReceipt{RootRequestID: "upstream-request-id", APIKeyID: identity.APIKeyID, Attempts: []uint64{generation}, Receipt: settled}))
	require.Equal(t, "actual-compact-id", op.Snapshot().Attempts[0].BillingRoot)
	require.Equal(t, "upstream-request-id", op.Snapshot().Attempts[1].BillingRoot)
	require.ErrorIs(t, op.RecordModeBilling(model.ModeBillingReceipt{RootRequestID: "overwritten-root", APIKeyID: identity.APIKeyID, Attempts: []uint64{compact}, Receipt: settled}), ErrOperationFacts)
	identity.Mode = "native"
	_, err := d.Admit(t.Context(), identity, target)
	require.ErrorIs(t, err, ErrInvalidPreparedIdentity)
	require.ErrorIs(t, op.BindModeAuthority("cloud", responseModeTestAuthority()), ErrDispatchChanged)
}

func TestResponseModeBPSMergedCompactionUsesExplicitAttemptSet(t *testing.T) {
	op, _, identity, target := responseModeFixture(t, "bps")
	require.NoError(t, op.BindModeAuthority("bps", responseModeTestAuthority()))
	one := openModeAttempt(t, op, identity, target)
	two := openModeAttempt(t, op, identity, target)
	r := model.ModeBillingReceipt{RootRequestID: "actual-upstream-id", APIKeyID: identity.APIKeyID, Attempts: []uint64{one, two}, Receipt: model.BillingReceipt{Disposition: model.BillingSettled, Durable: true, EvidenceID: "same-existing-journal"}}
	require.NoError(t, op.RecordModeBilling(r))
	require.NoError(t, op.RecordModeBilling(r), "same durable receipt is idempotent")
	require.Equal(t, r.RootRequestID, op.Snapshot().Attempts[0].BillingRoot)
	require.Equal(t, r.RootRequestID, op.Snapshot().Attempts[1].BillingRoot)
	bad := r
	bad.Attempts = []uint64{one, one}
	require.ErrorIs(t, op.RecordModeBilling(bad), ErrOperationFacts)
}

type responseTransferAuthority struct{ claimed bool }

func (a *responseTransferAuthority) RevalidateDispatch(_ context.Context, identity model.PreparedIdentity) (DispatchValidation, error) {
	return DispatchValidation{Purpose: identity.Purpose, Authorization: [32]byte{1}, Leases: [32]byte{2}, ValidUntil: time.Now().Add(time.Minute)}, nil
}
func (a *responseTransferAuthority) ClaimOperationTransfer(_ context.Context, transfer OperationTransfer) (TransferClaim, error) {
	if a.claimed {
		return TransferClaim{}, ErrOperationConsumed
	}
	a.claimed = true
	return TransferClaim{OperationID: transfer.Operation.OperationID, CommandID: transfer.CommandID, Digest: transfer.Digest(), SessionID: "session", OwnerEpoch: [32]byte{9}, Leases: [32]byte{2}, ValidUntil: time.Now().Add(time.Minute)}, nil
}

type responseReturnAuthority struct{ calls int }

func (a *responseReturnAuthority) ClaimOperationReturn(_ context.Context, transfer OperationTransfer, returned OperationReturn) (ReturnAuthorization, error) {
	a.calls++
	if a.calls > 1 {
		return ReturnAuthorization{}, ErrOperationConsumed
	}
	return ReturnAuthorization{CommandID: transfer.CommandID, TransferDigest: transfer.Digest(), ReturnDigest: returned.Digest(), Claim: [32]byte{8}, Authority: responseModeTestAuthority()}, nil
}

func rejectedCloudReturn(t *testing.T) (*Operation, *Dispatcher, OperationReturn, model.PreparedIdentity, model.AttemptTarget) {
	t.Helper()
	source, d, identity, target := responseModeFixture(t, "cloud")
	transfer, err := source.Transfer(t.Context(), "command", "cloud", [32]byte{5})
	require.NoError(t, err)
	require.ErrorIs(t, d.budget.Take(&target), ErrOperationTransferred)
	_, err = d.Admit(t.Context(), identity, target)
	require.ErrorIs(t, err, ErrOperationConsumed)
	gate := &responseTransferAuthority{}
	owner, budget, output, err := ResumeTransferredOperation(t.Context(), transfer, gate)
	require.NoError(t, err)
	require.True(t, owner.UsesResources(budget, output))
	require.Equal(t, transfer.Output.Deadline, output.Snapshot().Deadline)
	_, _, _, err = ResumeTransferredOperation(t.Context(), transfer, gate)
	require.ErrorIs(t, err, ErrOperationConsumed)
	sequence := openModeAttempt(t, owner, identity, target)
	require.NoError(t, owner.Observe(sequence, model.FactDelta{Generation: model.GenerationRejected, RejectionEvidenceID: "actual-pre-inference-rejection", Terminal: model.TerminalFailed}))
	owner.Closed(sequence, nil)
	cause := errors.New("provider explicitly rejected inference")
	require.ErrorIs(t, owner.Finish(t.Context(), cause), cause)
	returned, err := owner.ExportReturn()
	require.NoError(t, err)
	require.ErrorIs(t, budget.Take(&target), ErrOperationTransferred)
	return source, d, returned, identity, target
}

func TestResponseModeCloudAuthoritativeHandbackPreservesObjectsAndCandidatePolicy(t *testing.T) {
	source, d, returned, identity, target := rejectedCloudReturn(t)
	budget, output := d.budget, d.output
	require.NoError(t, source.RecoverTransferred(t.Context(), returned, &responseReturnAuthority{}))
	require.Same(t, budget, d.budget)
	require.Same(t, output, d.output)
	require.Equal(t, 1, budget.Snapshot().Attempts)
	require.Equal(t, returned.Output.Deadline, output.Snapshot().Deadline)
	require.Equal(t, model.OperationRetryPlanning, source.Snapshot().State)
	// The existing Cloud policy can choose a BPS candidate after proved rejection.
	require.NoError(t, source.BindModeAuthority("bps", responseModeTestAuthority()))
	identity.Mode, identity.Binding = "bps", model.ModeBinding{BillingScope: identity.OperationID}
	identity.ResourceID, identity.Owner.AccountID, target.Owner.AccountID = 8, 8, 8
	openModeAttempt(t, source, identity, target)
	require.Equal(t, 2, budget.Snapshot().Attempts)
	require.ErrorIs(t, source.RecoverTransferred(t.Context(), returned, &responseReturnAuthority{}), ErrOperationConsumed)
}

func TestResponseModeCloudHandbackRejectsUnknownDeliveryAndResetState(t *testing.T) {
	for _, change := range []string{"unknown_generation", "ambiguous_send", "partial_output", "reset_budget", "extended_deadline", "different_key"} {
		t.Run(change, func(t *testing.T) {
			source, d, returned, _, _ := rejectedCloudReturn(t)
			switch change {
			case "unknown_generation":
				returned.Result.Attempts[0].Generation = model.GenerationUnknown
				returned.Result.Attempts[0].RejectionEvidenceID = ""
			case "ambiguous_send":
				returned.Result.Attempts[0].Send = model.SendAmbiguous
			case "partial_output":
				returned.Result.Delivery.PossiblyCommitted = true
			case "reset_budget":
				returned.Budget.Attempts = 0
			case "extended_deadline":
				returned.Output.Deadline = returned.Output.Deadline.Add(time.Hour)
			case "different_key":
				returned.Result.APIKeyID++
			}
			gate := &responseReturnAuthority{}
			require.Error(t, source.RecoverTransferred(t.Context(), returned, gate))
			require.Zero(t, gate.calls, "invalid handback cannot consume a reclaim grant")
			require.True(t, d.stopped.Load())
			require.True(t, d.budget.retired)
		})
	}
}

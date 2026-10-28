package execution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

var ErrOperationTransferred = errors.New("operation execution belongs to another owner")

// OperationTransfer carries values only. It is admitted through a trusted host
// channel, never from client JSON. It deliberately cannot carry a SendPermit,
// socket, callback, credential or Go pointer. OperationID is the client
// operation; CommandID is its unique transport delivery, not its billing root.
type OperationTransfer struct {
	Version   int
	CommandID string
	Mode      string
	Operation model.OperationResult
	Budget    BudgetTransfer
	Output    OutputSnapshot
	Sequence  uint64
	// ContextDigest binds the host's serialized original authorization and
	// real capacity-token references. It is not an authorization fingerprint.
	ContextDigest [32]byte
}

func (t OperationTransfer) Digest() [32]byte {
	raw, err := json.Marshal(t)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(raw)
}

// TransferClaim is returned by a real atomic command/owner claim. The store
// checks command identity/digest, current cancellation and the session's live
// owner token and deadline together. Replays must not issue a second claim,
// including after a process restart; they may resolve an existing local owner
// or return the persisted outcome. Unknown acceptance cannot authorize replay.
type TransferClaim struct {
	OperationID string
	CommandID   string
	Digest      [32]byte
	SessionID   string
	OwnerEpoch  [32]byte
	Leases      [32]byte
	ValidUntil  time.Time
}

type TransferAuthority interface {
	DispatchAuthority
	ClaimOperationTransfer(context.Context, OperationTransfer) (TransferClaim, error)
}

// Transfer relinquishes the SOURCE execution atomically. No source permit or
// legacy budget debit can succeed afterwards, even if enqueue acknowledgement
// is lost. The host owns a bounded cancellation relay until remote completion.
// Only proved-unsent, closed attempts can move: this is not crash recovery for
// a possibly accepted generation. Call before Cloud's old ingress-side debit.
func (o *Operation) Transfer(ctx context.Context, commandID, mode string, contextDigest [32]byte) (OperationTransfer, error) {
	var denied OperationTransfer
	if o == nil || ctx == nil || commandID == "" || mode != "cloud" || contextDigest == ([32]byte{}) {
		return denied, ErrDispatchUnavailable
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	d := o.dispatcher
	d.mu.Lock()
	defer d.mu.Unlock()
	if !o.running || o.finished || o.transfer != nil || o.inFlight != 0 || d.stopped.Load() || (d.mode != "" && d.mode != mode) {
		return denied, ErrOperationConsumed
	}
	if err := d.check(ctx); err != nil {
		return denied, err
	}
	if !transferableAttempts(o.result) || d.sequence != uint64(len(o.result.Attempts)) {
		return denied, ErrOperationFacts
	}
	d.budget.mu.Lock()
	defer d.budget.mu.Unlock()
	if d.budget.retired {
		return denied, ErrOperationTransferred
	}
	transfer := OperationTransfer{Version: 1, CommandID: commandID, Mode: mode, Operation: o.snapshot(0), Budget: d.budget.transferState(), Output: d.output.Snapshot(), Sequence: d.sequence, ContextDigest: contextDigest}
	// Every previously spent generation must have a proved-unsent fact. A
	// legacy Take without an attempt is not evidence of non-dispatch.
	var generations int
	for _, a := range transfer.Operation.Attempts {
		if a.Identity.Purpose == model.DispatchGeneration {
			generations++
		}
	}
	if transfer.Budget.Attempts != generations || transfer.Output.Started.IsZero() || transfer.Output.Committed {
		return denied, ErrOperationFacts
	}
	d.stopped.Store(true)
	d.budget.retired = true
	// Keep a detached copy, not slices a caller can alter before completion.
	saved := transfer
	saved.Operation = o.snapshot(0)
	saved.Budget = d.budget.transferState()
	o.transfer = &saved
	return transfer, nil
}

func transferableAttempts(r model.OperationResult) bool {
	if r.Delivery.BusinessCommitted || r.Delivery.PossiblyCommitted || r.DelegatedCycles != 0 {
		return false
	}
	for i, a := range r.Attempts {
		if a.Sequence != uint64(i+1) || !a.Closed || a.Delivery.BusinessCommitted || a.Delivery.PossiblyCommitted || !noGeneration(a) {
			return false
		}
	}
	return true
}

// ResumeTransferredOperation is the only receiving-side constructor. Claim is
// mandatory and precedes creating an executable owner. The returned budget and
// clock are the SAME instances used by its Dispatcher; service integration must
// install them in OpenAITurnBudget instead of calling NewOpenAITurnBudget.
func ResumeTransferredOperation(ctx context.Context, transfer OperationTransfer, authority TransferAuthority) (*Operation, *AttemptBudget, *FirstOutput, error) {
	if ctx == nil || nilDispatchAuthority(authority) || transfer.Version != 1 || transfer.Mode != "cloud" || transfer.CommandID == "" || transfer.ContextDigest == ([32]byte{}) ||
		transfer.Operation.OperationID == "" || !model.ValidExecutionPrincipal(transfer.Operation.Principal, transfer.Operation.UserID, transfer.Operation.APIKeyID, transfer.Operation.GroupID, transfer.Operation.GroupBound, transfer.Operation.ProbeID, transfer.Operation.ProbeClaimDigest) || transfer.Operation.OriginalWireDigest == ([32]byte{}) ||
		transfer.Output.Started.IsZero() || transfer.Output.Committed || transfer.Output.Paused < 0 || !transferableAttempts(transfer.Operation) || transfer.Sequence != uint64(len(transfer.Operation.Attempts)) {
		return nil, nil, nil, ErrOperationFacts
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, context.Cause(ctx)
	}
	if !transfer.Output.Deadline.IsZero() && !time.Now().Before(transfer.Output.Deadline) {
		return nil, nil, nil, ErrDispatchExpired
	}
	budget, err := restoreBudget(transfer.Budget)
	if err != nil || !budgetMatchesAttempts(budget, transfer.Operation.Attempts) {
		return nil, nil, nil, ErrOperationFacts
	}
	claim, err := authority.ClaimOperationTransfer(ctx, transfer)
	if err != nil {
		return nil, nil, nil, err
	}
	if claim.OperationID != transfer.Operation.OperationID || claim.CommandID != transfer.CommandID || claim.Digest == ([32]byte{}) || claim.Digest != transfer.Digest() || claim.SessionID == "" || claim.OwnerEpoch == ([32]byte{}) || claim.Leases == ([32]byte{}) || claim.ValidUntil.IsZero() || !time.Now().Before(claim.ValidUntil) {
		return nil, nil, nil, ErrDispatchChanged
	}
	output := &FirstOutput{started: transfer.Output.Started, deadline: transfer.Output.Deadline, paused: transfer.Output.Paused}
	gate := &transferredAuthority{authority: authority, claim: claim, input: transfer.Operation}
	dispatcher, err := NewDispatcher(ctx, transfer.Operation.OperationID, budget, output, gate)
	if err != nil {
		return nil, nil, nil, err
	}
	dispatcher.mode, dispatcher.sequence = transfer.Mode, transfer.Sequence
	result := transfer.Operation
	result.Attempts = cloneAttempts(result.Attempts)
	result.State = model.OperationWaiting
	result.Failure = nil
	saved := transfer
	saved.Operation.Attempts = cloneAttempts(transfer.Operation.Attempts)
	saved.Budget.Owners = append([]OwnerDebit(nil), transfer.Budget.Owners...)
	saved.Budget.Targets = append([]TargetDebit(nil), transfer.Budget.Targets...)
	op := &Operation{dispatcher: dispatcher, result: result, running: true, cycleStart: len(result.Attempts), received: &saved}
	return op, budget, output, nil
}

type transferredAuthority struct {
	authority TransferAuthority
	claim     TransferClaim
	input     model.OperationResult
}

func (a *transferredAuthority) RevalidateDispatch(ctx context.Context, identity model.PreparedIdentity) (DispatchValidation, error) {
	if identity.Principal != a.input.Principal || identity.ProbeID != a.input.ProbeID || identity.ProbeClaimDigest != a.input.ProbeClaimDigest || identity.OperationID != a.input.OperationID || identity.UserID != a.input.UserID || identity.APIKeyID != a.input.APIKeyID || identity.GroupID != a.input.GroupID || identity.GroupBound != a.input.GroupBound || identity.Binding.SessionID != a.claim.SessionID || identity.Binding.OwnerEpoch != a.claim.OwnerEpoch {
		return DispatchValidation{}, ErrInvalidPreparedIdentity
	}
	// The original claim is not a perpetual lease. This required authority
	// checks current owned tokens/deadlines and cancellation at every send.
	return a.authority.RevalidateDispatch(ctx, identity)
}

func cloneAttempts(attempts []model.AttemptFact) []model.AttemptFact {
	copy := append([]model.AttemptFact(nil), attempts...)
	for i := range copy {
		copy[i].Usage = model.CloneUsageFact(copy[i].Usage)
	}
	return copy
}

// CompleteTransfer imports the actual owner's final result through the trusted
// reply channel. It retires no leases, settles no money and cannot enable sends.
// A lost reply leaves the source transferred, not successfully completed.
func (o *Operation) CompleteTransfer(returned OperationReturn) error {
	if o == nil {
		return ErrDispatchUnavailable
	}
	returned = cloneReturn(returned)
	result := returned.Result
	o.mu.Lock()
	defer o.mu.Unlock()
	t := o.transfer
	if t == nil || returned.CommandID != t.CommandID || returned.TransferDigest != t.Digest() || o.finished || o.returning {
		return ErrOperationConsumed
	}
	budget, err := restoreBudget(returned.Budget)
	if err != nil || !monotonicBudget(t.Budget, budget) || !budgetMatchesAttempts(budget, result.Attempts) || returned.Sequence != uint64(len(result.Attempts)) || !returned.Output.Started.Equal(t.Output.Started) || (!returned.Output.Committed && !monotonicOutput(t.Output, returned.Output)) {
		return ErrOperationFacts
	}
	r := t.Operation
	if result.Principal != r.Principal || result.ProbeID != r.ProbeID || result.ProbeClaimDigest != r.ProbeClaimDigest || result.OperationID != r.OperationID || result.UserID != r.UserID || result.APIKeyID != r.APIKeyID || result.GroupID != r.GroupID || result.GroupBound != r.GroupBound || result.Protocol != r.Protocol || result.OriginalWireDigest != r.OriginalWireDigest || result.DelegatedCycles != 0 || len(result.Attempts) < len(r.Attempts) {
		return ErrOperationFacts
	}
	switch result.State {
	case model.OperationCompleted, model.OperationSuspended, model.OperationFailed, model.OperationCanceled:
	default:
		return ErrOperationFacts
	}
	for i, a := range result.Attempts {
		if a.Sequence != uint64(i+1) || a.Identity.OperationID != r.OperationID || a.Identity.UserID != r.UserID || a.Identity.APIKeyID != r.APIKeyID || a.Identity.GroupID != r.GroupID || a.Identity.GroupBound != r.GroupBound || a.Identity.Principal != r.Principal || a.Identity.ProbeID != r.ProbeID || a.Identity.ProbeClaimDigest != r.ProbeClaimDigest || !a.Closed {
			return ErrOperationFacts
		}
		if i < len(r.Attempts) {
			old, _ := json.Marshal(r.Attempts[i])
			current, _ := json.Marshal(a)
			if string(old) != string(current) {
				return ErrOperationFacts
			}
		} else if a.Identity.Mode != t.Mode || !a.Identity.Valid() || !a.Identity.Binding.Valid(t.Mode) {
			return ErrOperationFacts
		}
	}
	result.Attempts = cloneAttempts(result.Attempts)
	o.result, o.finished = result, true
	d := o.dispatcher
	d.mu.Lock()
	d.budget.mu.Lock()
	d.budget.attempts, d.budget.preparationRetries, d.budget.retryWait = budget.attempts, budget.preparationRetries, budget.retryWait
	d.budget.owners, d.budget.targets = budget.owners, budget.targets
	d.sequence = returned.Sequence
	d.budget.mu.Unlock()
	d.mu.Unlock()
	if result.Delivery.BusinessCommitted || result.Delivery.PossiblyCommitted {
		o.dispatcher.output.Commit()
	}
	return nil
}

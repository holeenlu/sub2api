package execution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// OperationReturn is an owner-produced checkpoint, not permission to retry.
// ExportReturn stops the owner. The transport must persist that retirement with
// this exact digest before the source's ReturnAuthority can reclaim execution.
type OperationReturn struct {
	CommandID      string
	TransferDigest [32]byte
	Result         model.OperationResult
	Budget         BudgetTransfer
	Output         OutputSnapshot
	Sequence       uint64
}

func (r OperationReturn) Digest() [32]byte {
	data, err := json.Marshal(r)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(data)
}

// ReturnAuthorization is issued by the original root's recovery policy after
// an atomic claim of the persisted handback. Authority is the original source
// dispatch authority for subsequent candidate selection, not a remote closure.
// The host verifies continuation restrictions and mode policy; a Cloud rejection
// does not automatically authorize switching modes or resetting a grant.
type ReturnAuthorization struct {
	CommandID      string
	TransferDigest [32]byte
	ReturnDigest   [32]byte
	Claim          [32]byte
	Authority      DispatchAuthority
}

type ReturnAuthority interface {
	ClaimOperationReturn(context.Context, OperationTransfer, OperationReturn) (ReturnAuthorization, error)
}

func noGeneration(a model.AttemptFact) bool {
	if a.Generation == model.GenerationStarted || a.Terminal == model.TerminalCompleted || a.Delivery.BusinessCommitted || a.Delivery.PossiblyCommitted {
		return false
	}
	if u := a.Usage; u != nil {
		for _, v := range []*model.TokenMeasurement{u.Total, u.Reasoning, u.Input, u.Output, u.CacheRead, u.CacheWrite, u.ImageInput, u.ImageOutput, u.HostedImageInput, u.HostedImageOutput} {
			if v != nil && v.Value != 0 {
				return false
			}
		}
		if u.ImageCount != nil && *u.ImageCount != 0 {
			return false
		}
	}
	if a.Send == model.SendNotSent {
		return a.ResponseID == "" && a.ProviderTurnID == "" && a.Terminal == model.TerminalUnknown && a.Usage == nil && !a.EndOfStream
	}
	if a.Identity.Purpose == model.DispatchControl || a.Identity.Purpose == model.DispatchInternalPreparation {
		return a.Send == model.SendSent && a.Generation == model.GenerationUnknown && a.Usage == nil
	}
	return a.Send == model.SendSent && a.Generation == model.GenerationRejected && a.RejectionEvidenceID != ""
}

// ExportReturn runs only after the original actor has stopped/joined every
// attempt. It is valid for normal completion as well; only RecoverTransferred
// can turn a separately authorized pre-inference rejection into more sends.
func (o *Operation) ExportReturn() (OperationReturn, error) {
	if o == nil {
		return OperationReturn{}, ErrDispatchUnavailable
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.received == nil || !o.finished || o.inFlight != 0 {
		return OperationReturn{}, ErrOperationConsumed
	}
	for _, a := range o.result.Attempts {
		if !a.Closed {
			return OperationReturn{}, ErrOperationFacts
		}
	}
	d := o.dispatcher
	d.mu.Lock()
	defer d.mu.Unlock()
	d.budget.mu.Lock()
	defer d.budget.mu.Unlock()
	d.stopped.Store(true)
	d.budget.retired = true
	return OperationReturn{CommandID: o.received.CommandID, TransferDigest: o.received.Digest(), Result: o.snapshot(0), Budget: d.budget.transferState(), Output: d.output.Snapshot(), Sequence: d.sequence}, nil
}

func monotonicBudget(before BudgetTransfer, after *AttemptBudget) bool {
	if before.Limits != after.limits || after.attempts < before.Attempts || after.preparationRetries < before.PreparationRetries || after.retryWait < before.RetryWait {
		return false
	}
	for _, debit := range before.Owners {
		if after.owners[debit.Owner] < debit.Count {
			return false
		}
	}
	for _, debit := range before.Targets {
		if after.targets[debit.Target] < debit.Count {
			return false
		}
	}
	return true
}

func budgetMatchesAttempts(budget *AttemptBudget, attempts []model.AttemptFact) bool {
	count := 0
	owners := make(map[model.ResourceOwner]int)
	for _, a := range attempts {
		if a.Identity.Purpose == model.DispatchGeneration {
			count++
			owners[a.Identity.Owner]++
		}
	}
	if count != budget.attempts || len(owners) != len(budget.owners) {
		return false
	}
	for owner, count := range owners {
		if budget.owners[owner] != count {
			return false
		}
	}
	return true
}

func cloneReturn(r OperationReturn) OperationReturn {
	r.Result.Attempts = cloneAttempts(r.Result.Attempts)
	r.Budget.Owners = append([]OwnerDebit(nil), r.Budget.Owners...)
	r.Budget.Targets = append([]TargetDebit(nil), r.Budget.Targets...)
	return r
}

func monotonicOutput(before, after OutputSnapshot) bool {
	return !before.Committed && !after.Committed && before.Started.Equal(after.Started) && after.Paused >= before.Paused &&
		(before.Deadline.IsZero() || !after.Deadline.IsZero() && !after.Deadline.After(before.Deadline.Add(after.Paused-before.Paused)))
}

// RecoverTransferred is an explicit recovery decision, never a side effect of
// CompleteTransfer or receiving a successful RPC reply. The SAME source
// Operation/AttemptBudget/FirstOutput objects resume with monotonic state. The
// required store claim proves the owner retired and consumes this handback once.
// BPS cannot use this path; its within-operation mode pin remains irreversible.
func (o *Operation) RecoverTransferred(ctx context.Context, returned OperationReturn, authority ReturnAuthority) error {
	if o == nil || ctx == nil || nilDispatchAuthority(authority) {
		return ErrDispatchUnavailable
	}
	returned = cloneReturn(returned)
	o.mu.Lock()
	t := o.transfer
	if t == nil || t.Mode != "cloud" || o.finished || o.returning || returned.CommandID != t.CommandID || returned.TransferDigest != t.Digest() {
		o.mu.Unlock()
		return ErrOperationConsumed
	}
	o.returning = true
	o.mu.Unlock()
	defer func() { o.mu.Lock(); o.returning = false; o.mu.Unlock() }()
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if err := o.dispatcher.control.Err(); err != nil {
		return context.Cause(o.dispatcher.control)
	}
	b, err := restoreBudget(returned.Budget)
	if err != nil || !monotonicBudget(t.Budget, b) || !budgetMatchesAttempts(b, returned.Result.Attempts) || !monotonicOutput(t.Output, returned.Output) || !transferableAttempts(returned.Result) || returned.Sequence != uint64(len(returned.Result.Attempts)) || len(returned.Result.Attempts) < len(t.Operation.Attempts) {
		return ErrOperationFacts
	}
	initial, result := t.Operation, returned.Result
	if result.Principal != initial.Principal || result.ProbeID != initial.ProbeID || result.ProbeClaimDigest != initial.ProbeClaimDigest || result.OperationID != initial.OperationID || result.UserID != initial.UserID || result.APIKeyID != initial.APIKeyID || result.GroupID != initial.GroupID || result.GroupBound != initial.GroupBound || result.Protocol != initial.Protocol || result.OriginalWireDigest != initial.OriginalWireDigest || result.State != model.OperationFailed {
		return ErrOperationFacts
	}
	for i, a := range result.Attempts {
		if i < len(initial.Attempts) {
			old, next := initial.Attempts[i], a
			old.Failure, next.Failure = nil, nil
			oldBytes, _ := json.Marshal(old)
			nextBytes, _ := json.Marshal(next)
			if string(oldBytes) != string(nextBytes) {
				return ErrOperationFacts
			}
		} else if !a.Identity.Valid() || a.Identity.Mode != "cloud" || a.Identity.OperationID != result.OperationID || a.Identity.UserID != result.UserID || a.Identity.APIKeyID != result.APIKeyID || !a.Identity.Binding.Valid("cloud") {
			return ErrOperationFacts
		}
	}
	grant, err := authority.ClaimOperationReturn(ctx, *t, returned)
	if err != nil {
		return err
	}
	if grant.CommandID != t.CommandID || grant.TransferDigest != t.Digest() || grant.ReturnDigest != returned.Digest() || grant.Claim == ([32]byte{}) || nilDispatchAuthority(grant.Authority) {
		return ErrDispatchChanged
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	d := o.dispatcher
	d.mu.Lock()
	defer d.mu.Unlock()
	if o.transfer != t || o.finished || ctx.Err() != nil || d.control.Err() != nil {
		return ErrOperationConsumed
	}
	if !returned.Output.Deadline.IsZero() && !time.Now().Before(returned.Output.Deadline) {
		return ErrDispatchExpired
	}
	d.budget.mu.Lock()
	defer d.budget.mu.Unlock()
	d.output.mu.Lock()
	defer d.output.mu.Unlock()
	if d.output.committed {
		return ErrOperationCommitted
	}
	// Keep the original pointers: existing ingress watchers and recovery
	// callers must observe the returned debits rather than a fresh allowance.
	d.budget.attempts, d.budget.preparationRetries, d.budget.retryWait = b.attempts, b.preparationRetries, b.retryWait
	d.budget.owners, d.budget.targets, d.budget.retired = b.owners, b.targets, false
	d.output.deadline, d.output.paused, d.output.automatic = returned.Output.Deadline, returned.Output.Paused, time.Time{}
	d.authority, d.mode, d.sequence, d.recoverySelection = grant.Authority, "", returned.Sequence, true
	d.authorityVersion++
	d.stopped.Store(false)
	result.Attempts = cloneAttempts(result.Attempts)
	result.State, result.Failure = model.OperationRetryPlanning, nil
	o.result, o.transfer, o.cycleStart = result, nil, len(result.Attempts)
	return nil
}

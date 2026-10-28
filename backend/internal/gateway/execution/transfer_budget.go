package execution

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// BudgetTransfer is an internal owner-handoff DTO, not a client-supplied limit
// or permission. Every debit survives the transfer, including targets with old
// grant/wire fingerprints. The receiving host must claim the exact transfer.
type BudgetTransfer struct {
	Limits             AttemptLimits
	Attempts           int
	PreparationRetries int
	RetryWait          time.Duration
	Owners             []OwnerDebit
	Targets            []TargetDebit
}

type OwnerDebit struct {
	Owner model.ResourceOwner
	Count int
}
type TargetDebit struct {
	Target model.AttemptTarget
	Count  int
}

// Caller holds b.mu and retires the source in the same critical section.
func (b *AttemptBudget) transferState() BudgetTransfer {
	r := BudgetTransfer{Limits: b.limits, Attempts: b.attempts, PreparationRetries: b.preparationRetries, RetryWait: b.retryWait}
	for owner, count := range b.owners {
		r.Owners = append(r.Owners, OwnerDebit{owner, count})
	}
	for target, count := range b.targets {
		r.Targets = append(r.Targets, TargetDebit{target, count})
	}
	sort.Slice(r.Owners, func(i, j int) bool {
		a, c := r.Owners[i].Owner, r.Owners[j].Owner
		return a.AccountID < c.AccountID || a.AccountID == c.AccountID && a.VendorID < c.VendorID
	})
	sort.Slice(r.Targets, func(i, j int) bool {
		a, _ := json.Marshal(r.Targets[i].Target)
		c, _ := json.Marshal(r.Targets[j].Target)
		return string(a) < string(c)
	})
	return r
}

func restoreBudget(r BudgetTransfer) (*AttemptBudget, error) {
	l := r.Limits
	if l.Total <= 0 || l.PerOwner <= 0 || l.Preparation < 0 || l.RetryWait < 0 ||
		r.Attempts < 0 || r.Attempts > l.Total || r.PreparationRetries < 0 || r.PreparationRetries > l.Preparation || r.RetryWait < 0 || r.RetryWait > l.RetryWait {
		return nil, ErrOperationFacts
	}
	b := NewAttemptBudget(l)
	b.attempts, b.preparationRetries, b.retryWait = r.Attempts, r.PreparationRetries, r.RetryWait
	b.owners, b.targets = make(map[model.ResourceOwner]int), make(map[model.AttemptTarget]int)
	remaining := r.Attempts
	for _, debit := range r.Owners {
		if debit.Count <= 0 || debit.Count > l.PerOwner || debit.Count > remaining || b.owners[debit.Owner] != 0 {
			return nil, ErrOperationFacts
		}
		b.owners[debit.Owner] = debit.Count
		remaining -= debit.Count
	}
	ownerTotals := make(map[model.ResourceOwner]int)
	for _, debit := range r.Targets {
		if debit.Count <= 0 || debit.Count > debit.Target.Limit || b.targets[debit.Target] != 0 || debit.Count > b.owners[debit.Target.Owner]-ownerTotals[debit.Target.Owner] {
			return nil, ErrOperationFacts
		}
		b.targets[debit.Target] = debit.Count
		ownerTotals[debit.Target.Owner] += debit.Count
	}
	for owner, count := range b.owners {
		if ownerTotals[owner] != count {
			return nil, ErrOperationFacts
		}
	}
	return b, nil
}

// Package execution owns operation-wide execution accounting and recovery.
// It does not depend on ingress, concrete providers or persistence.
package execution

import (
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

var (
	// Keep existing error text while service callers migrate to this owner.
	ErrAttemptBudgetExhausted     = errors.New("OpenAI turn attempt budget exhausted before upstream send")
	ErrTargetBudgetExhausted      = errors.New("upstream recovery target attempt budget exhausted")
	ErrPreparationBudgetExhausted = errors.New("turn preparation retry budget exhausted")
	ErrRetryWaitBudgetExhausted   = errors.New("turn retry waiting budget exhausted")
)

// AttemptLimits are fixed for one logical operation. Nonpositive limits do not
// authorize work. Production policy is supplied by the composition boundary.
type AttemptLimits struct {
	Total       int
	PerOwner    int
	Preparation int
	RetryWait   time.Duration
}

// BudgetSnapshot is a detached observation, never authority to send.
type BudgetSnapshot struct {
	Limit              int
	Attempts           int
	Owners             int
	PreparationRetries int
	RetryWait          time.Duration
}

// AttemptBudget accounts for all targets in an operation. It is safe for
// concurrent use and must not be copied after first use. Its zero value rejects
// work; callers construct it with explicit limits. Cancellation, deadlines,
// authorization, preparation and send permits are separate execution concerns.
type AttemptBudget struct {
	mu                 sync.Mutex
	retired            bool
	limits             AttemptLimits
	attempts           int
	preparationRetries int
	retryWait          time.Duration
	owners             map[model.ResourceOwner]int
	targets            map[model.AttemptTarget]int
}

func NewAttemptBudget(limits AttemptLimits) *AttemptBudget {
	return &AttemptBudget{limits: limits}
}

// Take atomically spends total, physical-owner and target allowances. A rejected
// attempt spends none of them. A nil target only spends the operation allowance;
// this supports existing ingress paths that do not yet supply a target identity.
// Taking a budget is not a substitute for authorization or a send permit.
func (b *AttemptBudget) Take(target *model.AttemptTarget) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.retired {
		return ErrOperationTransferred
	}
	if b.attempts >= b.limits.Total {
		return ErrAttemptBudgetExhausted
	}
	if target != nil {
		if b.owners[target.Owner] >= b.limits.PerOwner ||
			(target.Limit == 1 && !target.TransportContinuation && b.owners[target.Owner] >= 1) ||
			b.targets[*target] >= target.Limit {
			return ErrTargetBudgetExhausted
		}
		if b.owners == nil {
			b.owners = make(map[model.ResourceOwner]int)
			b.targets = make(map[model.AttemptTarget]int)
		}
		b.owners[target.Owner]++
		b.targets[*target]++
	}
	b.attempts++
	return nil
}

func (b *AttemptBudget) TakePreparation() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.retired {
		return ErrOperationTransferred
	}
	if b.preparationRetries >= b.limits.Preparation {
		return ErrPreparationBudgetExhausted
	}
	b.preparationRetries++
	return nil
}

// ReserveWait accounts for planned backoff, even if cancellation interrupts the
// wait. Positive jitter never shortens Retry-After and is clipped to the shared
// wait cap. The caller owns the actual cancellable timer.
func (b *AttemptBudget) ReserveWait(delay, jitter time.Duration) (time.Duration, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.retired {
		return 0, ErrOperationTransferred
	}
	if delay <= 0 || delay > b.limits.RetryWait-b.retryWait {
		return 0, ErrRetryWaitBudgetExhausted
	}
	jitter = min(max(jitter, 0), b.limits.RetryWait-b.retryWait-delay)
	delay += jitter
	b.retryWait += delay
	return delay, nil
}

// OwnerAvailable is a selection hint. Take checks all limits again atomically.
func (b *AttemptBudget) OwnerAvailable(owner model.ResourceOwner) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.retired && b.owners[owner] < b.limits.PerOwner
}

func (b *AttemptBudget) Snapshot() BudgetSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BudgetSnapshot{
		Limit: b.limits.Total, Attempts: b.attempts, Owners: len(b.owners),
		PreparationRetries: b.preparationRetries, RetryWait: b.retryWait,
	}
}

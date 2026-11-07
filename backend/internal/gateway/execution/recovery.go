package execution

import (
	"context"
	"errors"
	"time"
)

// RecoveryAction describes control flow only. Continue and RetrySameAccount do
// not grant permission to send: the existing attempt/owner budget and dispatch
// authorization must still be consumed and checked by the sender.
type RecoveryAction int

const (
	RecoveryContinue RecoveryAction = iota
	RecoveryExhausted
	RecoveryCanceled
	RecoveryRetrySameAccount
)

const (
	MaxSameAccountRetries      = 1
	MaxProfitVetoAttempts      = 10
	sameAccountRetryDelay      = 500 * time.Millisecond
	maxRequestScopedRetryDelay = 8 * time.Second
)

// RecoveryFailure is a detached projection of the failed attempt. A nil fact
// means no failure is available. False RequestNotSent is unknown/possibly sent,
// never evidence that generation did not start. RejectedBeforeGeneration is
// distinct from transport proof of NotSent. No raw response or entity is owned
// here; protocol classification and the final client error remain at ingress.
type RecoveryFailure struct {
	StatusCode               int
	ContentRefusal           bool
	RetryNextAccount         bool
	RetryableOnSameAccount   bool
	SameAccountRetryDelay    time.Duration
	SameAccountRetryDeadline time.Time
	SameAccountRetryMax      int
	RequestScopedTransient   bool
	RequestNotSent           bool
	RejectedBeforeGeneration bool
	ForceCacheBilling        bool
	CredentialFailure        bool
	TargetExhausted          bool
	CapacityShed             bool
	SwitchLimitExempt        bool
}

// RecoveryBudget observes the one existing operation owner. Prepare and Wait
// spend its preparation/wait allowances; implementations must not create or
// reset budgets, send requests, or detach from ctx. OutputCommitted and
// RetryAllowed read current owner state, not a cached output/budget copy.
// Errors preserve identity and cause; cancellation is not successful waiting.
type RecoveryBudget interface {
	OutputCommitted(context.Context) bool
	RetryAllowed(context.Context) bool
	Prepare(context.Context) error
	Wait(context.Context, time.Duration) error
}

// RecoveryAccounts binds effects to the selected resource at ingress. Exclude
// records the current recovery target and clears its pin, in that order. Pin
// pins only this resource. TempUnschedule preserves the existing health policy.
// StopSwitch applies the live scheduler restriction AFTER the counter advances.
// These synchronous callbacks neither select nor send; they cannot alter this
// Recovery's counters. None creates goroutines or owns a longer-lived context.
type RecoveryAccounts interface {
	Pin(context.Context)
	Exclude(context.Context)
	TempUnschedule(context.Context)
	StopSwitch(context.Context, int) bool
}

// RecoveryPorts must contain Budget and Accounts for the requested operation.
// Now is optional (time.Now by default); observation callbacks are optional and
// must not influence policy, mutate state, or start another recovery. All are
// called synchronously by the operation's single owner, without holding locks.
type RecoveryPorts struct {
	Budget    RecoveryBudget
	Accounts  RecoveryAccounts
	Now       func() time.Time
	Retrying  func(count, limit int, delay time.Duration)
	Switching func(count int)
}

func (p RecoveryPorts) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// RecoveryResult retains failures from preparation/waiting for ingress mapping.
// Reason is a stable scheduling observation, not a public protocol error.
type RecoveryResult struct {
	Action RecoveryAction
	Reason string
	Err    error
}

// SelectionFailure contains selection and continuation facts, not a retry
// decision. PoolExhausted excludes dependency failures and compact-only absence.
type SelectionFailure struct {
	PinnedAccountID int64
	HasPinned       bool
	PoolExhausted   bool
	Portable        bool
}

// Recovery owns request-local retry counts, exclusions, switches and cache
// billing across loop iterations. Construct once per logical operation and use
// serially; do not copy it or share it across concurrent streams. It does NOT
// own attempt/physical-owner allowances, output commitment or total deadlines.
// Exported fields are the same storage used by ingress during migration, not
// mirrored counters. Remove direct field writes when those callers migrate.
type Recovery struct {
	SwitchCount           int
	MaxSwitches           int
	FailedAccountIDs      map[int64]struct{}
	SameAccountRetryCount map[int64]int
	ForceCacheBilling     bool
	ReplayAllowed         bool
	UncertainReplaySafe   bool
	hasBoundSession       bool
	profitVetoCount       int
	operationSequence     uint64
}

// NewRecovery initializes one operation; it does not create a send/wait budget.
func NewRecovery(maxSwitches int, hasBoundSession bool) *Recovery {
	return &Recovery{
		MaxSwitches: maxSwitches, hasBoundSession: hasBoundSession, operationSequence: 1,
		ReplayAllowed: true, UncertainReplaySafe: true,
		FailedAccountIDs:      make(map[int64]struct{}),
		SameAccountRetryCount: make(map[int64]int),
	}
}

// HasBoundSession exposes the fixed affinity fact used for cache billing.
func (r *Recovery) HasBoundSession() bool { return r.hasBoundSession }

// ProfitVetoCount is observational and does not advance any budget.
func (r *Recovery) ProfitVetoCount() int { return r.profitVetoCount }

// BeginOperation clears recovery state only for a strictly newer client
// operation on the same serialized stream. Sequence 1 is initialized by the
// constructor. Duplicate/stale notifications and attempt reconnects retain all
// consumed counts. Ingress supplies the stream's monotonic client-turn sequence;
// this is not an authorization token. No attempt/owner budget, output owner or
// deadline is replaced here. MaxSwitches and fixed replay/affinity policy remain.
func (r *Recovery) BeginOperation(sequence uint64) bool {
	if sequence <= r.operationSequence {
		return false
	}
	r.operationSequence = sequence
	clear(r.SameAccountRetryCount)
	clear(r.FailedAccountIDs)
	r.SwitchCount = 0
	r.profitVetoCount = 0
	r.ForceCacheBilling = false
	return true
}

// RecordProfitVeto excludes a rejected candidate without spending a send or
// switch. A separate bound prevents a stale candidate plan from spinning.
func (r *Recovery) RecordProfitVeto(accountID int64) RecoveryAction {
	return RecordProfitVeto(r.FailedAccountIDs, accountID, &r.profitVetoCount)
}

// RecordProfitVeto shares the same policy with the remaining image/media/search
// loops that do not yet own a Recovery. The map and count must be the existing
// operation-local storage; this function allocates or resets neither.
func RecordProfitVeto(excluded map[int64]struct{}, accountID int64, count *int) RecoveryAction {
	excluded[accountID] = struct{}{}
	*count++
	if *count >= MaxProfitVetoAttempts {
		return RecoveryExhausted
	}
	return RecoveryContinue
}

func recoveryStopped(reason string, err error) RecoveryResult {
	return RecoveryResult{Action: RecoveryExhausted, Reason: reason, Err: err}
}

func recoveryCanceled(ctx context.Context) RecoveryResult {
	return RecoveryResult{Action: RecoveryCanceled, Reason: "client_canceled", Err: ctx.Err()}
}

// RecoverFailure preserves the generic gateway's ordering: terminal/budget
// checks, preparation, billing, same-account wait, exclusion, portability,
// health update, switch count, then optional linear switch waiting.
// switchDelayStep is the existing platform policy supplied by ingress (zero
// disables it); execution does not interpret platform names or configuration.
func (r *Recovery) RecoverFailure(ctx context.Context, accountID int64, failure *RecoveryFailure, retryLimit int, switchDelayStep time.Duration, p RecoveryPorts) RecoveryResult {
	if ctx.Err() != nil {
		return recoveryCanceled(ctx)
	}
	if p.Budget.OutputCommitted(ctx) {
		return recoveryStopped("output_committed", nil)
	}
	if failure != nil && failure.ContentRefusal {
		return recoveryStopped("content_refusal", nil)
	}
	if failure == nil || !p.Budget.RetryAllowed(ctx) || (!r.UncertainReplaySafe && !failure.RequestNotSent && !failure.RejectedBeforeGeneration && failure.StatusCode >= 500) {
		return recoveryStopped("retry_policy_stopped", nil)
	}
	if err := p.Budget.Prepare(ctx); err != nil {
		return recoveryStopped("preparation_budget_exhausted", err)
	}
	same := SameAccountRetryAllowed(failure, r.SameAccountRetryCount[accountID], retryLimit, p.now())
	if (r.hasBoundSession && !same) || failure.ForceCacheBilling {
		r.ForceCacheBilling = true
	}
	if same {
		r.SameAccountRetryCount[accountID]++
		if err := waitRecoveryRetry(ctx, failure, r.SameAccountRetryCount[accountID], retryLimit, p); err != nil {
			if ctx.Err() != nil {
				return recoveryCanceled(ctx)
			}
			return recoveryStopped("retry_wait_exhausted", err)
		}
		return RecoveryResult{Action: RecoveryContinue, Reason: "same_account_retry"}
	}
	p.Accounts.Exclude(ctx)
	if !failure.RetryNextAccount || !r.ReplayAllowed {
		return recoveryStopped("retry_policy_stopped", nil)
	}
	if failure.RetryableOnSameAccount {
		p.Accounts.TempUnschedule(ctx)
	}
	r.FailedAccountIDs[accountID] = struct{}{}
	if r.SwitchCount >= r.MaxSwitches {
		return recoveryStopped("switch_budget_exhausted", nil)
	}
	r.SwitchCount++
	if p.Switching != nil {
		p.Switching(r.SwitchCount)
	}
	if switchDelayStep > 0 {
		if err := p.Budget.Wait(ctx, time.Duration(r.SwitchCount-1)*switchDelayStep); err != nil {
			if ctx.Err() != nil {
				return recoveryCanceled(ctx)
			}
			return recoveryStopped("retry_wait_exhausted", err)
		}
	}
	return RecoveryResult{Action: RecoveryContinue, Reason: "switch_account"}
}

// SelectionExhausted never reopens exclusions. Only an unavailable pinned owner
// can move, and only while replay, output and the live turn budget permit it.
// availablePoolExhausted distinguishes an empty pool from dependency failures.
func (r *Recovery) SelectionExhausted(ctx context.Context, pinnedID int64, hasPinned, availablePoolExhausted bool, last *RecoveryFailure, p RecoveryPorts) RecoveryResult {
	if ctx.Err() != nil {
		return recoveryCanceled(ctx)
	}
	if p.Budget.OutputCommitted(ctx) || !p.Budget.RetryAllowed(ctx) || (last != nil && last.ContentRefusal) {
		return recoveryStopped("retry_policy_stopped", nil)
	}
	if availablePoolExhausted && hasPinned && last != nil && last.RetryNextAccount && r.ReplayAllowed && r.SwitchCount < r.MaxSwitches {
		p.Accounts.Exclude(ctx)
		r.FailedAccountIDs[pinnedID] = struct{}{}
		r.SwitchCount++
		return RecoveryResult{Action: RecoveryContinue, Reason: "switch_account"}
	}
	return recoveryStopped("retry_policy_stopped", nil)
}

// RecoverUnavailableOwner owns the native ingress selection-failure policy.
// handled=false leaves the original selection error for ingress mapping: no
// pinned owner, dependency/compact absence, nonportable context, a terminal
// previous failure or an already reached switch cap does not become a new
// failover attempt. Once eligible, SwitchAccount preserves exclusion, live
// budget/output checks and scheduler ordering; handled=true maps its stop to
// the previous upstream failure exactly as the existing HTTP/SSE/WS routes do.
func (r *Recovery) RecoverUnavailableOwner(ctx context.Context, selected SelectionFailure, last *RecoveryFailure, p RecoveryPorts) (RecoveryResult, bool) {
	if !selected.HasPinned || !selected.PoolExhausted || last == nil || !last.RetryNextAccount || !selected.Portable || r.SwitchCount >= r.MaxSwitches {
		return recoveryStopped("selection_error", nil), false
	}
	return r.SwitchAccount(ctx, selected.PinnedAccountID, last, p), true
}

// RecoverBoundAccount handles the native/owner-bound route. Same-account
// preparation is permitted before testing cross-account portability. The fact
// may gain the existing one-repeat allowance; ingress projects that change back
// onto its error for existing downstream consumers. It is operation-local.
func (r *Recovery) RecoverBoundAccount(ctx context.Context, accountID int64, failure *RecoveryFailure, portable, retrySame bool, retryLimit int, p RecoveryPorts) RecoveryResult {
	if ctx.Err() != nil {
		return recoveryCanceled(ctx)
	}
	if failure == nil {
		return recoveryStopped("retry_policy_stopped", nil)
	}
	if p.Budget.OutputCommitted(ctx) || !p.Budget.RetryAllowed(ctx) || failure.ContentRefusal {
		return recoveryStopped("turn_budget", nil)
	}
	if !failure.RetryNextAccount && !failure.RetryableOnSameAccount {
		return recoveryStopped("retry_policy_stopped", nil)
	}
	if retrySame {
		if err := p.Budget.Prepare(ctx); err != nil {
			return recoveryStopped("preparation_budget_exhausted", err)
		}
		AllowAccountBoundRetry(failure)
		retried, err := RetrySameAccount(ctx, accountID, failure, retryLimit, r.SameAccountRetryCount, p)
		if err != nil && !(failure.StatusCode == 429 && ctx.Err() == nil && errors.Is(err, ErrRetryWaitBudgetExhausted)) {
			return recoveryStopped("retry_wait_exhausted", err)
		}
		// A 429 wait may exceed this operation's allowance. Keep Retry-After
		// and the original failure intact; portability and the existing switch
		// budget below still decide whether another account can be tried.
		if retried {
			return RecoveryResult{Action: RecoveryRetrySameAccount, Reason: "same_account_retry"}
		}
	}
	if !portable || !failure.RetryNextAccount {
		return recoveryStopped("context_not_portable", nil)
	}
	return r.SwitchAccount(ctx, accountID, failure, p)
}

// SwitchAccount is also consumed when selection loses the pinned owner. It
// records exclusion before checking the switch cap, then rechecks the current
// turn budget, advances the counter and applies the scheduler's 429 restriction.
func (r *Recovery) SwitchAccount(ctx context.Context, accountID int64, failure *RecoveryFailure, p RecoveryPorts) RecoveryResult {
	if ctx.Err() != nil {
		return recoveryCanceled(ctx)
	}
	p.Accounts.Exclude(ctx)
	r.FailedAccountIDs[accountID] = struct{}{}
	if r.SwitchCount >= r.MaxSwitches {
		return recoveryStopped("account_switch_limit", nil)
	}
	if failure.ContentRefusal || p.Budget.OutputCommitted(ctx) || !p.Budget.RetryAllowed(ctx) {
		return recoveryStopped("turn_budget", nil)
	}
	r.SwitchCount++
	if !failure.SwitchLimitExempt && p.Accounts.StopSwitch(ctx, r.SwitchCount) {
		return recoveryStopped("account_switch_limit", nil)
	}
	if p.Switching != nil {
		p.Switching(r.SwitchCount)
	}
	return RecoveryResult{Action: RecoveryContinue, Reason: "switch_account"}
}

// SameAccountRetryDelay keeps explicit Retry-After intact and caps only the
// existing request-scoped exponential fallback. Nil uses the fixed default.
func SameAccountRetryDelay(f *RecoveryFailure, count int) time.Duration {
	if f == nil {
		return sameAccountRetryDelay
	}
	if f.SameAccountRetryDelay > 0 {
		return f.SameAccountRetryDelay
	}
	if !f.RequestScopedTransient || count <= 1 {
		return sameAccountRetryDelay
	}
	delay := sameAccountRetryDelay
	for i := 1; i < count; i++ {
		if delay >= maxRequestScopedRetryDelay/2 {
			return maxRequestScopedRetryDelay
		}
		delay *= 2
	}
	return delay
}

// SameAccountRetryDeadlineAllows requires the earliest next attempt to precede
// the absolute window. A zero deadline adds no window beyond the owner budget.
func SameAccountRetryDeadlineAllows(f *RecoveryFailure, now time.Time) bool {
	return f == nil || f.SameAccountRetryDeadline.IsZero() || now.Add(SameAccountRetryDelay(f, 1)).Before(f.SameAccountRetryDeadline)
}

// SameAccountRetryAllowed never lets a longer window override account opt-out
// or the one-repeat count. The caller supplies the authoritative current time.
func SameAccountRetryAllowed(f *RecoveryFailure, count, limit int, now time.Time) bool {
	return f != nil && f.RetryableOnSameAccount && SameAccountRetryDeadlineAllows(f, now) && limit > 0 && count < min(limit, MaxSameAccountRetries)
}

// EffectiveSameAccountRetryLimit applies a stricter failure cap without
// overriding an explicitly disabled account limit.
func EffectiveSameAccountRetryLimit(f *RecoveryFailure, limit int) int {
	if limit > 0 && f != nil && f.SameAccountRetryMax > 0 && f.SameAccountRetryMax < limit {
		return f.SameAccountRetryMax
	}
	return limit
}

// AllowAccountBoundRetry preserves account opt-out in the subsequent limit
// check and never reopens a capacity-shed cooldown or exhausted target.
func AllowAccountBoundRetry(f *RecoveryFailure) {
	if f != nil && f.StatusCode >= 500 && !f.CredentialFailure && !f.TargetExhausted && f.RetryNextAccount && !f.RetryableOnSameAccount && !f.CapacityShed {
		f.RetryableOnSameAccount = true
		f.SameAccountRetryMax = MaxSameAccountRetries
	}
}

// RetrySameAccount consumes the caller's existing count map, never a fresh
// recovery budget. The map belongs to one serialized operation. This entry is
// shared by the remaining image/search adapters until their loops migrate.
func RetrySameAccount(ctx context.Context, accountID int64, f *RecoveryFailure, limit int, counts map[int64]int, p RecoveryPorts) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	limit = EffectiveSameAccountRetryLimit(f, limit)
	if !SameAccountRetryAllowed(f, counts[accountID], limit, p.now()) {
		return false, nil
	}
	counts[accountID]++
	p.Accounts.Pin(ctx)
	if err := waitRecoveryRetry(ctx, f, counts[accountID], limit, p); err != nil {
		return false, err
	}
	return true, nil
}

func waitRecoveryRetry(ctx context.Context, f *RecoveryFailure, count, limit int, p RecoveryPorts) error {
	delay := SameAccountRetryDelay(f, count)
	if p.Retrying != nil {
		p.Retrying(count, limit, delay)
	}
	waitCtx := ctx
	if !f.SameAccountRetryDeadline.IsZero() {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithDeadlineCause(ctx, f.SameAccountRetryDeadline, ErrRetryWaitBudgetExhausted)
		defer cancel()
	}
	if err := p.Budget.Wait(waitCtx, delay); err != nil {
		return err
	}
	// Positive jitter and delayed wake-up must never extend the retry window.
	if !f.SameAccountRetryDeadline.IsZero() && !p.now().Before(f.SameAccountRetryDeadline) {
		return ErrRetryWaitBudgetExhausted
	}
	return ctx.Err()
}

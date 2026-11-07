package execution

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// No network, service, sleep or provider fixture: callbacks record the order
// against one existing owner and can move its clock or revoke its allowance.
type recoveryFixture struct {
	events      []string
	now         time.Time
	committed   bool
	allowed     bool
	preparation error
	waiting     error
	stopSwitch  bool
	onWait      func(context.Context, time.Duration) error
	onPrepare   func(context.Context) error
	allowance   []bool
}

func newRecoveryFixture() *recoveryFixture { return &recoveryFixture{now: time.Now(), allowed: true} }
func (f *recoveryFixture) OutputCommitted(context.Context) bool {
	f.events = append(f.events, "output")
	return f.committed
}
func (f *recoveryFixture) RetryAllowed(context.Context) bool {
	f.events = append(f.events, "allowed")
	if len(f.allowance) > 0 {
		result := f.allowance[0]
		f.allowance = f.allowance[1:]
		return result
	}
	return f.allowed
}
func (f *recoveryFixture) Prepare(ctx context.Context) error {
	f.events = append(f.events, "prepare")
	if f.onPrepare != nil {
		return f.onPrepare(ctx)
	}
	return f.preparation
}
func (f *recoveryFixture) Wait(ctx context.Context, delay time.Duration) error {
	f.events = append(f.events, "wait:"+delay.String())
	if f.onWait != nil {
		return f.onWait(ctx, delay)
	}
	return f.waiting
}
func (f *recoveryFixture) Pin(context.Context)            { f.events = append(f.events, "pin") }
func (f *recoveryFixture) Exclude(context.Context)        { f.events = append(f.events, "exclude") }
func (f *recoveryFixture) TempUnschedule(context.Context) { f.events = append(f.events, "unschedule") }
func (f *recoveryFixture) StopSwitch(_ context.Context, count int) bool {
	f.events = append(f.events, fmt.Sprintf("cap:%d", count))
	return f.stopSwitch
}
func (f *recoveryFixture) ports() RecoveryPorts {
	return RecoveryPorts{
		Budget: f, Accounts: f, Now: func() time.Time { return f.now },
		Retrying: func(count, limit int, delay time.Duration) {
			f.events = append(f.events, fmt.Sprintf("retry:%d/%d:%s", count, limit, delay))
		},
		Switching: func(count int) { f.events = append(f.events, fmt.Sprintf("switch:%d", count)) },
	}
}
func recoveryTestFailure() *RecoveryFailure {
	return &RecoveryFailure{StatusCode: 502, RetryNextAccount: true, SameAccountRetryDelay: time.Millisecond}
}

func TestRecoveryGenericOwnsAcrossIterations(t *testing.T) {
	r := NewRecovery(2, true)
	f := newRecoveryFixture()
	fact := recoveryTestFailure()
	fact.RetryableOnSameAccount = true
	result := r.RecoverFailure(t.Context(), 41, fact, 9, time.Second, f.ports())
	require.Equal(t, RecoveryContinue, result.Action)
	require.Equal(t, []string{"output", "allowed", "prepare", "retry:1/9:1ms", "wait:1ms"}, f.events)
	require.Equal(t, 1, r.SameAccountRetryCount[41])
	require.Zero(t, r.SwitchCount)
	require.False(t, r.ForceCacheBilling)
	require.Empty(t, r.FailedAccountIDs)
	f.events = nil
	result = r.RecoverFailure(t.Context(), 41, fact, 9, time.Second, f.ports())
	require.Equal(t, RecoveryContinue, result.Action)
	require.Equal(t, []string{"output", "allowed", "prepare", "exclude", "unschedule", "switch:1", "wait:0s"}, f.events)
	require.True(t, r.ForceCacheBilling)
	require.Contains(t, r.FailedAccountIDs, int64(41))
	require.Equal(t, 1, r.SameAccountRetryCount[41])
	f.events = nil
	fact.RetryableOnSameAccount = false
	result = r.RecoverFailure(t.Context(), 42, fact, 0, time.Second, f.ports())
	require.Equal(t, RecoveryContinue, result.Action)
	require.Equal(t, []string{"output", "allowed", "prepare", "exclude", "switch:2", "wait:1s"}, f.events)
	f.events = nil
	result = r.RecoverFailure(t.Context(), 43, fact, 0, time.Second, f.ports())
	require.Equal(t, RecoveryExhausted, result.Action)
	require.Equal(t, "switch_budget_exhausted", result.Reason)
	require.Equal(t, []string{"output", "allowed", "prepare", "exclude"}, f.events)
	require.Equal(t, 2, r.SwitchCount)
	require.Len(t, r.FailedAccountIDs, 3)
}

func TestRecoveryGenericTerminalOrderAndSendEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                                                               string
		committed, refusal, denied, notSent, rejected, ambiguous, canceled bool
		status                                                             int
		want                                                               RecoveryAction
		events                                                             []string
	}{
		{name: "canceled", canceled: true, want: RecoveryCanceled},
		{name: "committed overrides not sent", committed: true, notSent: true, want: RecoveryExhausted, events: []string{"output"}},
		{name: "content refusal before budget", refusal: true, want: RecoveryExhausted, events: []string{"output"}},
		{name: "budget denied", denied: true, want: RecoveryExhausted, events: []string{"output", "allowed"}},
		{name: "ambiguous hosted generation", ambiguous: true, status: 502, want: RecoveryExhausted, events: []string{"output", "allowed"}},
		{name: "proven not sent", ambiguous: true, status: 502, notSent: true, want: RecoveryContinue},
		{name: "explicit rejection", ambiguous: true, status: 502, rejected: true, want: RecoveryContinue},
		{name: "replay safe", status: 502, want: RecoveryContinue},
		{name: "four hundred preserves existing gate", ambiguous: true, status: 429, want: RecoveryContinue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			f := newRecoveryFixture()
			f.committed = tc.committed
			f.allowed = !tc.denied
			r := NewRecovery(2, true)
			r.UncertainReplaySafe = !tc.ambiguous
			fact := &RecoveryFailure{StatusCode: tc.status, ContentRefusal: tc.refusal, RequestNotSent: tc.notSent, RejectedBeforeGeneration: tc.rejected, RetryNextAccount: true, ForceCacheBilling: true}
			result := r.RecoverFailure(ctx, 7, fact, 0, 0, f.ports())
			require.Equal(t, tc.want, result.Action)
			if tc.want != RecoveryContinue {
				require.Equal(t, tc.events, f.events)
				require.False(t, r.ForceCacheBilling)
				require.Empty(t, r.FailedAccountIDs)
				require.Zero(t, r.SwitchCount)
			}
		})
	}
	t.Run("missing failure", func(t *testing.T) {
		f := newRecoveryFixture()
		r := NewRecovery(1, true)
		require.Equal(t, RecoveryExhausted, r.RecoverFailure(t.Context(), 1, nil, 1, 0, f.ports()).Action)
		require.Equal(t, []string{"output"}, f.events)
	})
}

func TestRecoveryPreparationWaitAndPortabilityFailures(t *testing.T) {
	sentinel := errors.New("dependency failed")
	for _, mode := range []string{"preparation", "waiting", "canceled waiting", "switch waiting", "canceled switch waiting", "not portable", "next account blocked"} {
		t.Run(mode, func(t *testing.T) {
			r := NewRecovery(3, true)
			f := newRecoveryFixture()
			fact := recoveryTestFailure()
			fact.RetryableOnSameAccount = true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			step := time.Duration(0)
			switch mode {
			case "preparation":
				f.preparation = sentinel
			case "waiting":
				f.waiting = sentinel
			case "canceled waiting":
				f.onWait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
			case "switch waiting":
				r.SwitchCount = 1
				fact.RetryableOnSameAccount = false
				step = time.Second
				f.waiting = sentinel
			case "canceled switch waiting":
				r.SwitchCount = 1
				fact.RetryableOnSameAccount = false
				step = time.Second
				f.onWait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
			case "not portable":
				r.ReplayAllowed = false
				r.SameAccountRetryCount[1] = 1
			case "next account blocked":
				fact.RetryNextAccount = false
				r.SameAccountRetryCount[1] = 1
			}
			result := r.RecoverFailure(ctx, 1, fact, 1, step, f.ports())
			switch mode {
			case "preparation":
				require.ErrorIs(t, result.Err, sentinel)
				require.Equal(t, "preparation_budget_exhausted", result.Reason)
				require.False(t, r.ForceCacheBilling)
				require.Empty(t, r.SameAccountRetryCount)
			case "waiting", "switch waiting":
				require.ErrorIs(t, result.Err, sentinel)
				require.Equal(t, "retry_wait_exhausted", result.Reason)
			case "canceled waiting", "canceled switch waiting":
				require.Equal(t, RecoveryCanceled, result.Action)
				require.ErrorIs(t, result.Err, context.Canceled)
			default:
				require.Equal(t, []string{"output", "allowed", "prepare", "exclude"}, f.events)
				require.Empty(t, r.FailedAccountIDs)
				require.True(t, r.ForceCacheBilling)
			}
			if mode != "switch waiting" && mode != "canceled switch waiting" {
				require.Zero(t, r.SwitchCount)
			}
		})
	}
}

func TestRecoveryBoundOwnerOrderingAndLimits(t *testing.T) {
	r := NewRecovery(1, false)
	f := newRecoveryFixture()
	fact := recoveryTestFailure()
	result := r.RecoverBoundAccount(t.Context(), 12, fact, true, true, 3, f.ports())
	require.Equal(t, RecoveryRetrySameAccount, result.Action)
	require.Equal(t, []string{"output", "allowed", "prepare", "pin", "retry:1/1:1ms", "wait:1ms"}, f.events)
	require.True(t, fact.RetryableOnSameAccount)
	require.Equal(t, 1, fact.SameAccountRetryMax)
	require.False(t, r.ForceCacheBilling)
	f.events = nil
	result = r.RecoverBoundAccount(t.Context(), 12, fact, true, true, 3, f.ports())
	require.Equal(t, RecoveryContinue, result.Action)
	require.Equal(t, []string{"output", "allowed", "prepare", "exclude", "output", "allowed", "cap:1", "switch:1"}, f.events)
	f.events = nil
	result = r.RecoverBoundAccount(t.Context(), 13, recoveryTestFailure(), true, false, 0, f.ports())
	require.Equal(t, RecoveryExhausted, result.Action)
	require.Equal(t, "account_switch_limit", result.Reason)
	require.Equal(t, []string{"output", "allowed", "exclude"}, f.events)
	require.Equal(t, 1, r.SwitchCount)
	require.Contains(t, r.FailedAccountIDs, int64(13))
}

func TestRecoveryBoundOwnerStopsWithoutEffects(t *testing.T) {
	for _, mode := range []string{"canceled", "missing", "committed", "refusal", "budget", "preparation", "not portable", "terminal", "wait"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := NewRecovery(2, false)
			f := newRecoveryFixture()
			fact := recoveryTestFailure()
			retrySame := true
			portable := true
			sentinel := errors.New("owner failed")
			switch mode {
			case "canceled":
				cancel()
			case "missing":
				fact = nil
			case "committed":
				f.committed = true
			case "refusal":
				fact.ContentRefusal = true
			case "budget":
				f.allowed = false
			case "preparation":
				f.preparation = sentinel
			case "not portable":
				portable = false
				retrySame = false
			case "terminal":
				fact.RetryNextAccount = false
			case "wait":
				f.waiting = sentinel
			}
			result := r.RecoverBoundAccount(ctx, 1, fact, portable, retrySame, 1, f.ports())
			require.NotEqual(t, RecoveryContinue, result.Action)
			require.NotEqual(t, RecoveryRetrySameAccount, result.Action)
			require.Zero(t, r.SwitchCount)
			require.Empty(t, r.FailedAccountIDs)
			if mode == "wait" {
				require.Equal(t, 1, r.SameAccountRetryCount[1])
			} else {
				require.Empty(t, r.SameAccountRetryCount)
			}
			if mode == "preparation" || mode == "wait" {
				require.ErrorIs(t, result.Err, sentinel)
			}
			if mode == "canceled" {
				require.ErrorIs(t, result.Err, context.Canceled)
			}
			require.NotContains(t, f.events, "exclude")
		})
	}
}

func TestRecoveryBoundAccount429WaitBudgetUsesOriginalSwitchPolicy(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		for _, mode := range []string{"portable", "nonportable", "hard-bound 429", "switch budget", "scheduler cap", "scheduler exempt", "committed while waiting", "budget revoked while waiting"} {
			t.Run(fmt.Sprintf("%s/wrapped=%t", mode, wrapped), func(t *testing.T) {
				budget := NewAttemptBudget(AttemptLimits{Total: 3, PerOwner: 3, Preparation: 2, RetryWait: time.Minute})
				require.NoError(t, budget.Take(nil))
				_, err := budget.ReserveWait(time.Second, 0)
				require.NoError(t, err)
				wantBudget := budget.Snapshot()
				wantBudget.PreparationRetries++
				r := NewRecovery(2, true)
				f := newRecoveryFixture()
				fact := &RecoveryFailure{StatusCode: 429, RetryNextAccount: true, RetryableOnSameAccount: true, SameAccountRetryDelay: 67 * time.Second}
				portable := mode != "nonportable"
				wantAction, wantReason, wantSwitches := RecoveryContinue, "switch_account", 1
				events := []string{"output", "allowed", "prepare", "pin", "retry:1/1:1m7s", "wait:1m7s"}
				switch mode {
				case "nonportable", "hard-bound 429":
					if mode == "hard-bound 429" {
						fact.RetryNextAccount = false
					}
					wantAction, wantReason, wantSwitches = RecoveryExhausted, "context_not_portable", 0
				case "switch budget":
					r.SwitchCount = r.MaxSwitches
					wantAction, wantReason, wantSwitches = RecoveryExhausted, "account_switch_limit", r.MaxSwitches
					events = append(events, "exclude")
				case "scheduler cap":
					f.stopSwitch = true
					wantAction, wantReason = RecoveryExhausted, "account_switch_limit"
					events = append(events, "exclude", "output", "allowed", "cap:1")
				case "scheduler exempt":
					f.stopSwitch = true
					fact.SwitchLimitExempt = true
					events = append(events, "exclude", "output", "allowed", "switch:1")
				case "committed while waiting":
					wantAction, wantReason, wantSwitches = RecoveryExhausted, "turn_budget", 0
					events = append(events, "exclude", "output")
				case "budget revoked while waiting":
					wantAction, wantReason, wantSwitches = RecoveryExhausted, "turn_budget", 0
					events = append(events, "exclude", "output", "allowed")
				default:
					events = append(events, "exclude", "output", "allowed", "cap:1", "switch:1")
				}
				original := *fact
				f.onPrepare = func(context.Context) error { return budget.TakePreparation() }
				f.onWait = func(ctx context.Context, delay time.Duration) error {
					require.NoError(t, ctx.Err())
					require.Equal(t, 67*time.Second, delay, "never shorten the provider's Retry-After")
					_, err := budget.ReserveWait(delay, 0)
					require.ErrorIs(t, err, ErrRetryWaitBudgetExhausted)
					f.committed = mode == "committed while waiting"
					f.allowed = mode != "budget revoked while waiting"
					if wrapped {
						return fmt.Errorf("retry wait: %w", err)
					}
					return err
				}
				result := r.RecoverBoundAccount(t.Context(), 12, fact, portable, true, 1, f.ports())
				require.Equal(t, wantAction, result.Action)
				require.Equal(t, wantReason, result.Reason)
				require.NoError(t, result.Err, "leave the original provider 429 for ingress mapping")
				require.Equal(t, original, *fact)
				require.Equal(t, events, f.events)
				require.Equal(t, 1, r.SameAccountRetryCount[12])
				require.Equal(t, wantSwitches, r.SwitchCount)
				if mode == "nonportable" || mode == "hard-bound 429" {
					require.Empty(t, r.FailedAccountIDs)
				} else {
					require.Contains(t, r.FailedAccountIDs, int64(12))
				}
				require.False(t, r.ForceCacheBilling)
				require.Equal(t, wantBudget, budget.Snapshot(), "retain the same send, preparation and wait owner")
			})
		}
	}
}

func TestRecoveryBoundAccountOtherWaitErrorsRemainTerminal(t *testing.T) {
	wrappedBudget := fmt.Errorf("retry wait: %w", ErrRetryWaitBudgetExhausted)
	for _, tc := range []struct {
		name       string
		status     int
		waitErr    error
		cancelWait bool
	}{
		{name: "502 budget", status: 502, waitErr: ErrRetryWaitBudgetExhausted},
		{name: "503 wrapped budget", status: 503, waitErr: wrappedBudget},
		{name: "408 budget", status: 408, waitErr: ErrRetryWaitBudgetExhausted},
		{name: "429 dependency", status: 429, waitErr: errors.New("wait dependency unavailable")},
		{name: "429 matching text is not identity", status: 429, waitErr: errors.New(ErrRetryWaitBudgetExhausted.Error())},
		{name: "429 child deadline", status: 429, waitErr: context.DeadlineExceeded},
		{name: "429 child cancellation", status: 429, waitErr: context.Canceled},
		{name: "429 canceled parent budget", status: 429, waitErr: ErrRetryWaitBudgetExhausted, cancelWait: true},
		{name: "429 canceled parent wrapped budget", status: 429, waitErr: wrappedBudget, cancelWait: true},
		{name: "429 parent cancellation", status: 429, waitErr: context.Canceled, cancelWait: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := NewRecovery(2, true)
			f := newRecoveryFixture()
			fact := recoveryTestFailure()
			fact.StatusCode = tc.status
			fact.RetryableOnSameAccount = true
			original := *fact
			f.onWait = func(context.Context, time.Duration) error {
				if tc.cancelWait {
					cancel()
				}
				return tc.waitErr
			}
			result := r.RecoverBoundAccount(ctx, 12, fact, true, true, 1, f.ports())
			require.Equal(t, RecoveryExhausted, result.Action)
			require.Equal(t, "retry_wait_exhausted", result.Reason)
			require.ErrorIs(t, result.Err, tc.waitErr)
			require.Equal(t, original, *fact)
			require.Equal(t, []string{"output", "allowed", "prepare", "pin", "retry:1/1:1ms", "wait:1ms"}, f.events)
			require.Equal(t, 1, r.SameAccountRetryCount[12])
			require.Zero(t, r.SwitchCount)
			require.Empty(t, r.FailedAccountIDs)
			require.False(t, r.ForceCacheBilling)
		})
	}
}

func TestRecoverySwitchLiveGateAndSchedulerCap(t *testing.T) {
	for _, mode := range []string{"cap", "exempt", "budget", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			r := NewRecovery(3, false)
			f := newRecoveryFixture()
			fact := recoveryTestFailure()
			f.stopSwitch = true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "exempt":
				fact.SwitchLimitExempt = true
			case "budget":
				f.allowed = false
			case "canceled":
				cancel()
			}
			result := r.SwitchAccount(ctx, 4, fact, f.ports())
			switch mode {
			case "cap":
				require.Equal(t, "account_switch_limit", result.Reason)
				require.Equal(t, 1, r.SwitchCount)
				require.Equal(t, []string{"exclude", "output", "allowed", "cap:1"}, f.events)
			case "exempt":
				require.Equal(t, RecoveryContinue, result.Action)
				require.Equal(t, []string{"exclude", "output", "allowed", "switch:1"}, f.events)
			case "budget":
				require.Zero(t, r.SwitchCount)
				require.Equal(t, "turn_budget", result.Reason)
				require.Equal(t, []string{"exclude", "output", "allowed"}, f.events)
			case "canceled":
				require.Empty(t, r.FailedAccountIDs)
				require.Empty(t, f.events)
				require.Equal(t, RecoveryCanceled, result.Action)
			}
		})
	}
	f := newRecoveryFixture()
	f.allowance = []bool{true, false}
	r := NewRecovery(3, false)
	result := r.RecoverBoundAccount(t.Context(), 1, recoveryTestFailure(), true, false, 0, f.ports())
	require.Equal(t, "turn_budget", result.Reason)
	require.Zero(t, r.SwitchCount)
	require.Equal(t, []string{"output", "allowed", "exclude", "output", "allowed"}, f.events, "switch checks live owner, not the earlier snapshot")
}

func TestRecoverySelectionAndProfitVetoRemainBounded(t *testing.T) {
	for _, mode := range []string{"allowed", "dependency", "unpinned", "no failure", "terminal", "not portable", "cap", "output", "budget", "refusal", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			r := NewRecovery(2, false)
			r.FailedAccountIDs[99] = struct{}{}
			f := newRecoveryFixture()
			fact := recoveryTestFailure()
			pinned, empty := true, true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "dependency":
				empty = false
			case "unpinned":
				pinned = false
			case "no failure":
				fact = nil
			case "terminal":
				fact.RetryNextAccount = false
			case "not portable":
				r.ReplayAllowed = false
			case "cap":
				r.SwitchCount = 2
			case "output":
				f.committed = true
			case "budget":
				f.allowed = false
			case "refusal":
				fact.ContentRefusal = true
			case "canceled":
				cancel()
			}
			result := r.SelectionExhausted(ctx, 7, pinned, empty, fact, f.ports())
			require.Contains(t, r.FailedAccountIDs, int64(99), "never reopen previously excluded pool")
			if mode == "allowed" {
				require.Equal(t, RecoveryContinue, result.Action)
				require.Equal(t, 1, r.SwitchCount)
				require.Contains(t, r.FailedAccountIDs, int64(7))
				require.Equal(t, []string{"output", "allowed", "exclude"}, f.events)
			} else {
				require.NotEqual(t, RecoveryContinue, result.Action)
				require.NotContains(t, r.FailedAccountIDs, int64(7))
				require.NotContains(t, f.events, "exclude")
			}
		})
	}
	r := NewRecovery(1, true)
	for i := 1; i <= MaxProfitVetoAttempts+1; i++ {
		want := RecoveryContinue
		if i >= MaxProfitVetoAttempts {
			want = RecoveryExhausted
		}
		require.Equal(t, want, r.RecordProfitVeto(int64(i)))
		require.Equal(t, i, r.ProfitVetoCount())
	}
	require.Zero(t, r.SwitchCount)
	require.Empty(t, r.SameAccountRetryCount)
	require.False(t, r.ForceCacheBilling)
}

func TestRecoveryRetryWindowAndCancellation(t *testing.T) {
	for _, mode := range []string{"does not fit", "fits exactly", "jitter overruns", "deadline cause", "canceled after wait", "wait failure", "successful wait"} {
		t.Run(mode, func(t *testing.T) {
			f := newRecoveryFixture()
			fact := recoveryTestFailure()
			fact.RetryableOnSameAccount = true
			fact.SameAccountRetryDelay = time.Second
			fact.SameAccountRetryDeadline = f.now.Add(2 * time.Second)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "does not fit":
				fact.SameAccountRetryDeadline = f.now.Add(time.Millisecond)
			case "fits exactly":
				fact.SameAccountRetryDeadline = f.now.Add(time.Second)
			case "jitter overruns":
				f.onWait = func(context.Context, time.Duration) error { f.now = fact.SameAccountRetryDeadline; return nil }
			case "deadline cause":
				f.onWait = func(wait context.Context, _ time.Duration) error {
					deadline, ok := wait.Deadline()
					require.True(t, ok)
					require.Equal(t, fact.SameAccountRetryDeadline, deadline)
					// The wait owner propagates the shared budget identity, not success.
					return ErrRetryWaitBudgetExhausted
				}
			case "canceled after wait":
				f.onWait = func(context.Context, time.Duration) error { cancel(); return nil }
			case "wait failure":
				f.waiting = errors.New("wait dependency")
			}
			counts := map[int64]int{}
			retried, err := RetrySameAccount(ctx, 1, fact, 4, counts, f.ports())
			switch mode {
			case "does not fit", "fits exactly":
				require.False(t, retried)
				require.NoError(t, err)
				require.Empty(t, counts)
				require.Empty(t, f.events)
			case "jitter overruns", "deadline cause":
				require.False(t, retried)
				require.ErrorIs(t, err, ErrRetryWaitBudgetExhausted)
				require.Equal(t, 1, counts[1])
			case "canceled after wait":
				require.False(t, retried)
				require.ErrorIs(t, err, context.Canceled)
			case "wait failure":
				require.ErrorIs(t, err, f.waiting)
				require.False(t, retried)
			case "successful wait":
				require.True(t, retried)
				require.NoError(t, err)
			}
		})
	}
	// Generic recovery now consumes the same deadline-aware wait, too. It must
	// not allow a delayed wake-up to start generation outside the declared window.
	f := newRecoveryFixture()
	fact := recoveryTestFailure()
	fact.RetryableOnSameAccount = true
	fact.SameAccountRetryDeadline = f.now.Add(time.Second)
	f.onWait = func(context.Context, time.Duration) error { f.now = fact.SameAccountRetryDeadline; return nil }
	r := NewRecovery(2, false)
	result := r.RecoverFailure(t.Context(), 1, fact, 1, 0, f.ports())
	require.Equal(t, RecoveryExhausted, result.Action)
	require.ErrorIs(t, result.Err, ErrRetryWaitBudgetExhausted)
	require.Zero(t, r.SwitchCount)
	require.Empty(t, r.FailedAccountIDs)
}

func TestRecoveryCountDelayAndCapacityPolicy(t *testing.T) {
	now := time.Now()
	f := &RecoveryFailure{RetryableOnSameAccount: true, RequestScopedTransient: true}
	for _, tc := range []struct {
		count int
		delay time.Duration
	}{{0, 500 * time.Millisecond}, {1, 500 * time.Millisecond}, {2, time.Second}, {3, 2 * time.Second}, {4, 4 * time.Second}, {5, 8 * time.Second}, {500, 8 * time.Second}} {
		require.Equal(t, tc.delay, SameAccountRetryDelay(f, tc.count))
	}
	require.Equal(t, 500*time.Millisecond, SameAccountRetryDelay(nil, 9))
	f.SameAccountRetryDelay = 9 * time.Second
	require.Equal(t, 9*time.Second, SameAccountRetryDelay(f, 99))
	f.SameAccountRetryDelay = 0
	f.RequestScopedTransient = false
	require.Equal(t, 500*time.Millisecond, SameAccountRetryDelay(f, 99))
	for _, limit := range []int{-1, 0, 1, 9} {
		require.Equal(t, limit > 0, SameAccountRetryAllowed(f, 0, limit, now))
		require.False(t, SameAccountRetryAllowed(f, 1, limit, now))
	}
	f.SameAccountRetryMax = 1
	require.Equal(t, 0, EffectiveSameAccountRetryLimit(f, 0))
	require.Equal(t, 1, EffectiveSameAccountRetryLimit(f, 8))
	require.Equal(t, 2, EffectiveSameAccountRetryLimit(nil, 2))
	require.False(t, SameAccountRetryAllowed(nil, 0, 1, now))
	require.True(t, SameAccountRetryDeadlineAllows(nil, now))
	f.SameAccountRetryDeadline = now.Add(-time.Second)
	require.False(t, SameAccountRetryAllowed(f, 0, 99, now))
	for _, mode := range []string{"eligible", "credential", "target exhausted", "capacity shed", "explicit stop", "already enabled", "four hundred", "nil"} {
		t.Run(mode, func(t *testing.T) {
			fact := recoveryTestFailure()
			switch mode {
			case "credential":
				fact.CredentialFailure = true
			case "target exhausted":
				fact.TargetExhausted = true
			case "capacity shed":
				fact.CapacityShed = true
			case "explicit stop":
				fact.RetryNextAccount = false
			case "already enabled":
				fact.RetryableOnSameAccount = true
				fact.SameAccountRetryMax = 7
			case "four hundred":
				fact.StatusCode = 429
			case "nil":
				fact = nil
			}
			AllowAccountBoundRetry(fact)
			if mode == "eligible" {
				require.True(t, fact.RetryableOnSameAccount)
				require.Equal(t, 1, fact.SameAccountRetryMax)
			} else if mode == "already enabled" {
				require.Equal(t, 7, fact.SameAccountRetryMax)
			} else if fact != nil {
				require.False(t, fact.RetryableOnSameAccount)
				require.Zero(t, fact.SameAccountRetryMax)
			}
		})
	}
}

func TestRecoveryUsesExistingPreparationAndWaitBudget(t *testing.T) {
	budget := NewAttemptBudget(AttemptLimits{Total: 1, PerOwner: 1, Preparation: 1, RetryWait: time.Millisecond})
	require.NoError(t, budget.Take(nil))
	f := newRecoveryFixture()
	f.onPrepare = func(context.Context) error { return budget.TakePreparation() }
	f.onWait = func(_ context.Context, d time.Duration) error { _, err := budget.ReserveWait(d, 0); return err }
	r := NewRecovery(3, false)
	fact := recoveryTestFailure()
	fact.RetryableOnSameAccount = true
	result := r.RecoverFailure(t.Context(), 1, fact, 1, 0, f.ports())
	require.Equal(t, RecoveryContinue, result.Action)
	before := budget.Snapshot()
	require.Equal(t, 1, before.Attempts)
	require.Equal(t, 1, before.PreparationRetries)
	require.Equal(t, time.Millisecond, before.RetryWait)
	result = r.RecoverFailure(t.Context(), 2, fact, 1, 0, f.ports())
	require.Equal(t, RecoveryExhausted, result.Action)
	require.ErrorIs(t, result.Err, ErrPreparationBudgetExhausted)
	require.Equal(t, before, budget.Snapshot())
	require.Empty(t, r.FailedAccountIDs)
	// A legacy caller passes the SAME map and budget to the standalone adapter.
	// Neither re-entry nor a different target resets the exhausted wait allowance.
	retried, err := RetrySameAccount(t.Context(), 2, fact, 1, r.SameAccountRetryCount, f.ports())
	require.False(t, retried)
	require.ErrorIs(t, err, ErrRetryWaitBudgetExhausted)
	require.Equal(t, before, budget.Snapshot())
}

func TestRecoveryUnavailableOwnerSelectionPolicy(t *testing.T) {
	for _, mode := range []string{"move", "unpinned", "dependency", "missing failure", "terminal", "nonportable", "switch limit", "canceled", "output", "refusal", "budget", "scheduler cap", "scheduler exempt"} {
		t.Run(mode, func(t *testing.T) {
			r := NewRecovery(2, false)
			f := newRecoveryFixture()
			last := recoveryTestFailure()
			selected := SelectionFailure{PinnedAccountID: 12, HasPinned: true, PoolExhausted: true, Portable: true}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			wantHandled := true
			wantAction := RecoveryExhausted
			switch mode {
			case "move":
				wantAction = RecoveryContinue
			case "unpinned":
				selected.HasPinned = false
				wantHandled = false
			case "dependency":
				selected.PoolExhausted = false
				wantHandled = false
			case "missing failure":
				last = nil
				wantHandled = false
			case "terminal":
				last.RetryNextAccount = false
				wantHandled = false
			case "nonportable":
				selected.Portable = false
				wantHandled = false
			case "switch limit":
				r.SwitchCount = 2
				wantHandled = false
			case "canceled":
				cancel()
				wantAction = RecoveryCanceled
			case "output":
				f.committed = true
			case "refusal":
				last.ContentRefusal = true
			case "budget":
				f.allowed = false
			case "scheduler cap":
				f.stopSwitch = true
			case "scheduler exempt":
				f.stopSwitch = true
				last.SwitchLimitExempt = true
				wantAction = RecoveryContinue
			}
			result, handled := r.RecoverUnavailableOwner(ctx, selected, last, f.ports())
			require.Equal(t, wantHandled, handled)
			require.Equal(t, wantAction, result.Action)
			if !handled || mode == "canceled" {
				require.Empty(t, f.events)
				require.Empty(t, r.FailedAccountIDs)
			} else {
				require.Contains(t, r.FailedAccountIDs, int64(12))
			}
			if mode == "output" {
				require.Equal(t, []string{"exclude", "output"}, f.events)
				require.Zero(t, r.SwitchCount)
			}
			if mode == "refusal" {
				require.Equal(t, []string{"exclude"}, f.events)
				require.Zero(t, r.SwitchCount)
			}
			require.Empty(t, r.SameAccountRetryCount)
			require.False(t, r.ForceCacheBilling)
		})
	}
}

func TestRecoveryBeginOperationNeverResetsCurrentOrStaleOperation(t *testing.T) {
	r := NewRecovery(3, true)
	r.ReplayAllowed = false
	r.UncertainReplaySafe = false
	budget := NewAttemptBudget(AttemptLimits{Total: 1, PerOwner: 1, Preparation: 1, RetryWait: time.Second})
	require.NoError(t, budget.Take(nil))
	require.NoError(t, budget.TakePreparation())
	_, err := budget.ReserveWait(time.Second, 0)
	require.NoError(t, err)
	before := budget.Snapshot()
	r.RecordProfitVeto(7)
	r.SameAccountRetryCount[8] = 1
	r.SwitchCount = 2
	r.ForceCacheBilling = true
	for _, sequence := range []uint64{0, 1, 1} {
		require.False(t, r.BeginOperation(sequence))
		require.Equal(t, 1, r.ProfitVetoCount())
		require.Equal(t, 2, r.SwitchCount)
		require.Equal(t, 1, r.SameAccountRetryCount[8])
		require.Contains(t, r.FailedAccountIDs, int64(7))
		require.True(t, r.ForceCacheBilling)
	}
	require.True(t, r.BeginOperation(2))
	require.Empty(t, r.FailedAccountIDs)
	require.Empty(t, r.SameAccountRetryCount)
	require.Zero(t, r.SwitchCount)
	require.Zero(t, r.ProfitVetoCount())
	require.False(t, r.ForceCacheBilling)
	require.Equal(t, 3, r.MaxSwitches)
	require.True(t, r.HasBoundSession())
	require.False(t, r.ReplayAllowed)
	require.False(t, r.UncertainReplaySafe)
	r.RecordProfitVeto(9)
	r.SameAccountRetryCount[10] = 1
	require.False(t, r.BeginOperation(2), "the second WS next-turn hook must be idempotent")
	require.False(t, r.BeginOperation(1), "a delayed prior-turn callback cannot replenish recovery")
	require.Equal(t, 1, r.ProfitVetoCount())
	require.Equal(t, 1, r.SameAccountRetryCount[10])
	require.Contains(t, r.FailedAccountIDs, int64(9))
	require.Equal(t, before, budget.Snapshot(), "recovery owns neither send nor wait/preparation budget")
}

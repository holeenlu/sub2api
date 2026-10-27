package service

import "context"

// This state belongs to one selection, never to a shared session or another
// concurrent turn. The durable home remains in the existing sticky cache.
type schedulingAffinityKey struct{}
type schedulingAffinity struct {
	session string
	home    *Account
	wait    *AccountWaitPlan
}

func withSchedulingAffinity(ctx context.Context) context.Context {
	return context.WithValue(ctx, schedulingAffinityKey{}, &schedulingAffinity{})
}
func schedulingAffinityFrom(ctx context.Context) *schedulingAffinity {
	state, _ := ctx.Value(schedulingAffinityKey{}).(*schedulingAffinity)
	return state
}
func rememberBusyStickyHome(ctx context.Context, session string, account *Account, wait *AccountWaitPlan) {
	if state := schedulingAffinityFrom(ctx); state != nil && session != "" {
		state.session, state.home, state.wait = session, account, wait
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "affinity", Action: "borrow_busy", AccountID: account.ID})
	}
}
func preserveBorrowedStickyHome(ctx context.Context, session string, accountID int64) bool {
	state := schedulingAffinityFrom(ctx)
	return state != nil && state.home != nil && state.session == session && state.home.ID != accountID
}

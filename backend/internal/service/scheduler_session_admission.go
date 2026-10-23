package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

var errSessionAdmissionUnavailable = errors.New("session admission dependency unavailable")

type sessionTrackingKey struct{}
type sessionSelectionKey struct{}
type sessionRegistrationKey struct {
	accountID int64
	sessionID string
}
type sessionTracking struct {
	mu      sync.Mutex
	created map[sessionRegistrationKey]bool
}
type sessionSelection struct {
	admission     map[int64]SessionAdmission
	confirmations int
	dependencyErr error
}

// WithGatewaySessionTracking records only registrations created by this request.
// The atomic registration result, never a stale preflight, decides ownership.
func WithGatewaySessionTracking(ctx context.Context) context.Context {
	if _, ok := ctx.Value(sessionTrackingKey{}).(*sessionTracking); ok {
		return ctx
	}
	return context.WithValue(ctx, sessionTrackingKey{}, &sessionTracking{created: make(map[sessionRegistrationKey]bool)})
}
func rememberSessionRegistration(ctx context.Context, id int64, session string, created bool) {
	if !created {
		return
	}
	if state, _ := ctx.Value(sessionTrackingKey{}).(*sessionTracking); state != nil {
		state.mu.Lock()
		state.created[sessionRegistrationKey{id, session}] = true
		state.mu.Unlock()
	}
}
func takeSessionRegistration(ctx context.Context, id int64, session string) bool {
	state, _ := ctx.Value(sessionTrackingKey{}).(*sessionTracking)
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	key := sessionRegistrationKey{id, session}
	owned := state.created[key]
	delete(state.created, key)
	return owned
}
func sessionCanEnter(ctx context.Context, a *Account, sessionID string) bool {
	if a == nil {
		return false
	}
	if !a.IsAnthropicOAuthOrSetupToken() || a.GetMaxSessions() <= 0 || sessionID == "" {
		return true
	}
	if state, _ := ctx.Value(sessionSelectionKey{}).(*sessionSelection); state != nil {
		entry := state.admission[a.ID]
		return !entry.Known || entry.SessionActive || entry.ActiveCount < a.GetMaxSessions()
	}
	return true
}
func (s *GatewayService) prefetchSessionAdmissions(ctx context.Context, accounts []Account, sessionID string) {
	state, _ := ctx.Value(sessionSelectionKey{}).(*sessionSelection)
	if state == nil || sessionID == "" || s.sessionLimitCache == nil {
		return
	}
	ids := make([]int64, 0)
	timeouts := make(map[int64]time.Duration)
	for i := range accounts {
		a := &accounts[i]
		if a.IsAnthropicOAuthOrSetupToken() && a.GetMaxSessions() > 0 {
			ids = append(ids, a.ID)
			timeouts[a.ID] = time.Duration(a.GetSessionIdleTimeoutMinutes()) * time.Minute
		}
	}
	if len(ids) == 0 {
		return
	}
	state.admission, state.dependencyErr = s.sessionLimitCache.GetSessionAdmissionBatch(ctx, ids, sessionID, timeouts)
}

// Each layer has a budget ceiling. The last layer can use all remaining work;
// exhausted layers yield, including when a snapshot understated actual occupancy.
type gatewayLayerBudget struct {
	base              context.Context
	end, probeCeiling int
}

func (b *gatewayLayerBudget) enter(ctx context.Context, index int, ordered []*Account, probes int) (context.Context, bool) {
	if b.base == nil {
		b.base = ctx
	}
	if index >= b.end {
		b.end = index + 1
		for b.end < len(ordered) && ordered[index].Priority == ordered[b.end].Priority {
			b.end++
		}
		ctx = b.base
		b.probeCeiling = openAIAccountSelectionProbeLimit
		if b.end < len(ordered) {
			ctx = selectionbudget.Reserve(ctx, min(selectionbudget.Remaining(ctx), max(8, (selectionbudget.Remaining(ctx)+3)/4)))
			b.probeCeiling -= (openAIAccountSelectionProbeLimit - probes + 3) / 4
		}
		if index > 0 {
			RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "selection", Action: "next_layer"})
		}
	}
	return ctx, probes < b.probeCeiling && selectionbudget.Remaining(ctx) >= 8
}

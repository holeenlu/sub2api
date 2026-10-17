package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

const (
	opsSchedulingTrace       = "scheduling_trace"
	maxSchedulingTraceEvents = 32
	maxSchedulingTraceBytes  = 16 * 1024
)

// SchedulingDecision contains local decisions only. A selection or retry is not
// evidence of an upstream send. Bodies, credentials and session identifiers must
// never enter this record; actual failures retain their existing Ops events.
type SchedulingDecision struct {
	Policy     string         `json:"policy,omitempty"`
	Stage      string         `json:"stage"`
	Layer      string         `json:"layer,omitempty"`
	Action     string         `json:"action"`
	Reason     string         `json:"reason,omitempty"`
	AccountID  int64          `json:"account_id,omitempty"`
	Pool       int            `json:"pool,omitempty"`
	Candidates int            `json:"candidates,omitempty"`
	TopK       int            `json:"top_k,omitempty"`
	Excluded   map[string]int `json:"excluded,omitempty"`
	ElapsedMs  int64          `json:"elapsed_ms"`
	DurationMs int64          `json:"duration_ms,omitempty"`
}

type schedulingTraceKey struct{}

type schedulingTrace struct {
	mu             sync.Mutex
	started        time.Time
	events         []SchedulingDecision
	bytes, dropped int
	escaped        bool
	summary        schedulingSummary
	recovered      bool
}

// WithSchedulingTrace creates a request-owned bounded collector. Keeping it in
// context lets HTTP, WS and retry code share diagnostics without depending on Gin.
func WithSchedulingTrace(ctx context.Context) context.Context {
	return context.WithValue(ctx, schedulingTraceKey{}, &schedulingTrace{started: time.Now()})
}

func schedulingTraceFromContext(ctx context.Context) *schedulingTrace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(schedulingTraceKey{}).(*schedulingTrace)
	return trace
}

func resetSchedulingTrace(ctx context.Context) {
	if trace := schedulingTraceFromContext(ctx); trace != nil {
		trace.mu.Lock()
		defer trace.mu.Unlock()
		trace.started = time.Now()
		trace.events = nil
		trace.bytes, trace.dropped = 0, 0
		trace.recovered = false
		trace.summary = schedulingSummary{}
		trace.escaped = false
	}
}

func recordSchedulingSelection(ctx context.Context, event SchedulingDecision, started time.Time, selection *AccountSelectionResult, err error) {
	event.Stage, event.Action = "selection", "selected"
	event.DurationMs = time.Since(started).Milliseconds()
	if selection != nil && selection.Account != nil {
		event.AccountID = selection.Account.ID
		if selection.Acquired {
			event.Action = "acquired"
		} else if selection.WaitPlan != nil {
			event.Action = "wait"
		}
	} else {
		event.Action, event.Reason = "stopped", "no_selection"
	}
	if err != nil {
		event.Action, event.Reason = "stopped", "selection_error"
	}
	if ctx.Err() != nil {
		event.Action, event.Reason = "stopped", "context_canceled"
	}
	RecordSchedulingDecision(ctx, event)
}

func recordSchedulingSlot(ctx context.Context, accountID int64, result *AcquireResult, err error) {
	action := "busy"
	if err != nil {
		action = "error"
	} else if result != nil && result.Acquired {
		action = "acquired"
	}
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "slot", Action: action, AccountID: accountID})
}

func RecordSchedulingDecision(ctx context.Context, event SchedulingDecision) {
	logGatewayCacheSelection(ctx, event)
	if event.Action == "next_layer" {
		schedulingCounters.crossLayer.Add(1)
	}
	trace := schedulingTraceFromContext(ctx)
	if trace == nil {
		return
	}
	event.Policy = truncateString(event.Policy, 64)
	event.Stage = truncateString(event.Stage, 32)
	event.Layer = truncateString(event.Layer, 64)
	event.Action = truncateString(event.Action, 32)
	event.Reason = truncateString(event.Reason, 128)
	if len(event.Excluded) > 0 {
		counts := make(map[string]int)
		for reason, count := range event.Excluded {
			if len(counts) == 32 {
				break
			}
			counts[truncateString(reason, 32)] += count
		}
		event.Excluded = counts
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if event.Stage == "slot" {
		trace.summary.Attempts++
	}
	event.ElapsedMs = time.Since(trace.started).Milliseconds()
	if event.Stage == "filter" {
		trace.summary.Pool = max(trace.summary.Pool, event.Pool)
		trace.summary.Candidates = max(trace.summary.Candidates, event.Candidates)
		if trace.summary.Excluded == nil {
			trace.summary.Excluded = make(map[string]int)
		}
		for k, v := range event.Excluded {
			if len(trace.summary.Excluded) < 32 {
				trace.summary.Excluded[k] += v
			}
		}
	}
	if event.Stage != "slot" {
		if trace.summary.First == nil {
			copy := event
			copy.Excluded = nil
			trace.summary.First = &copy
		}
		copy := event
		copy.Excluded = nil
		trace.summary.Last = &copy
	}
	encoded, _ := json.Marshal(event)
	// Reserve space for the JSON envelope and separators as well as the events.
	for len(trace.events) > 0 && (len(trace.events) >= maxSchedulingTraceEvents || trace.bytes+len(encoded)+1 > maxSchedulingTraceBytes-8192) {
		old, _ := json.Marshal(trace.events[0])
		trace.bytes -= len(old) + 1
		trace.events = trace.events[1:]
		trace.dropped++
	}
	trace.events = append(trace.events, event)
	trace.bytes += len(encoded) + 1
	trace.recovered = trace.recovered || event.Stage == "retry"
}

func schedulingTraceJSON(ctx context.Context) (string, bool) {
	trace := schedulingTraceFromContext(ctx)
	if trace == nil {
		return "", false
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if len(trace.events) == 0 {
		return "", false
	}
	var encoded []byte
	for {
		encoded, _ = json.Marshal(struct {
			Summary schedulingSummary    `json:"summary"`
			Version int                  `json:"version"`
			Dropped int                  `json:"dropped_events,omitempty"`
			Events  []SchedulingDecision `json:"events"`
		}{trace.summary, 2, trace.dropped, trace.events})
		if len(encoded) <= maxSchedulingTraceBytes || len(trace.events) == 0 {
			break
		}
		old, _ := json.Marshal(trace.events[0])
		trace.bytes -= len(old) + 1
		trace.events = trace.events[1:]
		trace.dropped++
	}

	return string(encoded), trace.recovered
}

// WithOpsSchedulingTrace adds one diagnostic envelope to the existing JSON
// column. It has no status, account attribution or upstream attempt semantics.
func WithOpsSchedulingTrace(ctx context.Context, events []*OpsUpstreamErrorEvent) []*OpsUpstreamErrorEvent {
	detail, _ := schedulingTraceJSON(ctx)
	if detail == "" {
		return events
	}
	result := make([]*OpsUpstreamErrorEvent, 0, len(events)+1)
	for _, event := range events {
		if event == nil || event.Kind != opsSchedulingTrace {
			result = append(result, event)
		}
	}
	return append(result, &OpsUpstreamErrorEvent{
		Kind: opsSchedulingTrace, Stage: "scheduling", Scope: "request",
		Reason: "local_decisions", Detail: detail,
	})
}

var schedulingTraceSamples atomic.Uint64

// LogSchedulingTrace keeps failures/recoveries and samples one in 100 ordinary
// successes. It never creates an Ops failure row for a successful selection.
func LogSchedulingTrace(ctx context.Context, failed bool) {
	detail, recovered := schedulingTraceJSON(ctx)
	if detail == "" {
		return
	}
	if !failed && !recovered && schedulingTraceSamples.Add(1)%100 != 0 {
		return
	}
	logger.FromContext(ctx).Info("gateway.scheduling_trace", zap.Bool("request_failed", failed), zap.String("decisions", detail))
}

// The summary survives event-ring eviction. No identity becomes a metric label.
type schedulingSummary struct {
	Attempts   int                      `json:"candidate_attempts"`
	Pool       int                      `json:"pool"`
	Candidates int                      `json:"candidates"`
	Excluded   map[string]int           `json:"excluded,omitempty"`
	Budget     selectionbudget.Snapshot `json:"budget"`
	StopReason string                   `json:"stop_reason,omitempty"`
	First      *SchedulingDecision      `json:"first,omitempty"`
	Last       *SchedulingDecision      `json:"last,omitempty"`
}

func finishSchedulingBudget(ctx context.Context, b *selectionbudget.Budget, err *error) {
	schedulingCounters.selections.Add(1)
	snapshot := b.Snapshot()
	reason := "complete"
	if err != nil && *err != nil {
		switch {
		case ctx.Err() != nil:
			reason = "canceled"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				reason = "deadline_exceeded"
			}
			*err = ctx.Err()
		case snapshot.Exhausted || errors.Is(*err, selectionbudget.ErrExhausted):
			reason = "budget_exhausted"
			snapshot.Exhausted = true
			*err = selectionbudget.ErrExhausted
		case b.DependencyError() != nil:
			reason = "dependency_error"
			*err = fmt.Errorf("%w: %w", ErrSelectionDependency, b.DependencyError())
		case errors.Is(*err, ErrNoAvailableAccounts) || errors.Is(*err, ErrNoAvailableCompactAccounts):
			reason = "no_available_accounts"
		default:
			reason = "dependency_error"
			*err = fmt.Errorf("%w: %w", ErrSelectionDependency, *err)
		}
	}
	if reason == "budget_exhausted" {
		schedulingCounters.exhausted.Add(1)
	}
	if trace := schedulingTraceFromContext(ctx); trace != nil {
		trace.mu.Lock()
		trace.summary.Budget = snapshot
		trace.summary.StopReason = reason
		trace.mu.Unlock()
	}
}

var schedulingHealthRejections sync.Map // bounded reason enum only
func recordSchedulingHealthRejection(reason string) {
	value, _ := schedulingHealthRejections.LoadOrStore(reason, &atomic.Uint64{})
	if counter, ok := value.(*atomic.Uint64); ok {
		counter.Add(1)
	}
}

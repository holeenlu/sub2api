package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	Policy             string         `json:"policy,omitempty"`
	Stage              string         `json:"stage"`
	Layer              string         `json:"layer,omitempty"`
	Action             string         `json:"action"`
	Reason             string         `json:"reason,omitempty"`
	AccountID          int64          `json:"account_id,omitempty"`
	Pool               int            `json:"pool,omitempty"`
	Candidates         int            `json:"candidates,omitempty"`
	TopK               int            `json:"top_k,omitempty"`
	Excluded           map[string]int `json:"excluded,omitempty"`
	ElapsedMs          int64          `json:"elapsed_ms"`
	DurationMs         int64          `json:"duration_ms,omitempty"`
	ConfiguredCapacity int            `json:"configured_capacity,omitempty"`
	EffectiveCapacity  int            `json:"effective_capacity,omitempty"`
	OldestWaitMs       int64          `json:"oldest_wait_ms,omitempty"`
	BypassedWaiters    int            `json:"bypassed_waiters,omitempty"`
}

type schedulingTraceKey struct{}

type schedulingTrace struct {
	mu              sync.Mutex
	started         time.Time
	events          []SchedulingDecision
	bytes, dropped  int
	affinityEscaped bool // runtime token cohort; independent of diagnostic clipping
	truncated       bool
	summary         schedulingSummary
	recovered       bool
	publish         func(func()) bool
}

// WithSchedulingTrace creates a request-owned bounded collector. Keeping it in
// context lets HTTP, WS and retry code share diagnostics without depending on Gin.
func WithSchedulingTrace(ctx context.Context, publish func(func()) bool) context.Context {
	return context.WithValue(ctx, schedulingTraceKey{}, &schedulingTrace{started: time.Now(), publish: publish})
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
		trace.affinityEscaped = false
		trace.truncated = false
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
	if root := openAIRequestTimingFromContext(ctx); root != nil {
		if event.Stage == "selection" && event.AccountID > 0 {
			root.mark("account_selected", time.Now())
		}
		if (event.Stage == "slot" && event.Action == "acquired") || (event.Stage == "queue" && event.Action == "slot_admitted") {
			stage := "account_slot"
			if event.Layer == "user" {
				stage = "user_slot"
			}
			root.mark(stage, time.Now())
		}
	}
	logGatewayCacheSelection(ctx, event)
	if event.Stage == "queue" {
		index := 0
		if event.Layer == "user" {
			index = 1
		}
		age := event.OldestWaitMs
		if event.Action == "slot_admitted" {
			schedulingCounters.queueAdmissions[index].Add(1)
			if event.BypassedWaiters > 0 {
				schedulingCounters.queueBypasses[index].Add(1)
			}
		}
		if event.Action == "wait_finished" && event.DurationMs > age {
			age = event.DurationMs
		}
		for previous := schedulingCounters.oldestWaitMs[index].Load(); age > 0 && uint64(age) > previous; previous = schedulingCounters.oldestWaitMs[index].Load() {
			if schedulingCounters.oldestWaitMs[index].CompareAndSwap(previous, uint64(age)) {
				break
			}
		}
	}

	if event.Stage == "load" {
		schedulingCounters.dynamicAccounts.Add(uint64(max(0, event.Candidates)))
	}

	if event.Action == "next_layer" {
		schedulingCounters.crossLayer.Add(1)
	}
	trace := schedulingTraceFromContext(ctx)
	if trace == nil {
		return
	}
	truncated := len(event.Policy) > 64 || len(event.Stage) > 32 || len(event.Layer) > 64 || len(event.Action) > 32 || len(event.Reason) > 128
	event.Policy = truncateString(event.Policy, 64)
	event.Stage = truncateString(event.Stage, 32)
	event.Layer = truncateString(event.Layer, 64)
	event.Action = truncateString(event.Action, 32)
	event.Reason = truncateString(event.Reason, 128)
	if len(event.Excluded) > 0 {
		// A capped map must retain the same reasons regardless of map iteration.
		keys := make([]string, 0, len(event.Excluded))
		for reason := range event.Excluded {
			keys = append(keys, reason)
		}
		sort.Strings(keys)
		counts := make(map[string]int)
		for _, reason := range keys {
			key := truncateString(reason, 32)
			truncated = truncated || key != reason
			if _, exists := counts[key]; !exists && len(counts) == 32 {
				truncated = true
				continue
			}
			counts[key] += event.Excluded[reason]
		}
		event.Excluded = counts
	} else {
		event.Excluded = nil
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.truncated = trace.truncated || truncated
	if event.Stage == "queue" && event.Action == "slot_admitted" {
		trace.summary.QueueAdmissions++
		if event.OldestWaitMs > trace.summary.OldestWaitMs {
			trace.summary.OldestWaitMs = event.OldestWaitMs
		}
		if event.BypassedWaiters > 0 {
			trace.summary.QueueBypasses++
		}
	}
	if event.Stage == "load" {
		trace.summary.DynamicAccounts += event.Candidates
	}
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
		keys := make([]string, 0, len(event.Excluded))
		for k := range event.Excluded {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, exists := trace.summary.Excluded[k]; exists || len(trace.summary.Excluded) < 32 {
				trace.summary.Excluded[k] += event.Excluded[k]
			} else {
				trace.truncated = true
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
	// Keep collection bounded without serializing on every selection/slot.
	// Ordinary decisions cannot evict the last failure/recovery evidence.
	critical := schedulingDecisionCritical(event)
	limit, byteLimit := maxSchedulingTraceEvents, maxSchedulingTraceBytes-8192
	if !critical {
		limit -= 4
		byteLimit -= 1024
	}
	cost := schedulingDecisionBytes(event)
	trace.recovered = trace.recovered || event.Stage == "retry"
	for len(trace.events) >= limit || trace.bytes+cost > byteLimit {
		if !trace.discardEvent(critical) {
			trace.dropped++
			return
		}
	}
	trace.events = append(trace.events, event)
	trace.bytes += cost
}

func schedulingTraceJSON(ctx context.Context) (string, bool) {
	trace := schedulingTraceFromContext(ctx)
	if trace == nil {
		return "", false
	}
	return trace.json()
}

func (trace *schedulingTrace) json() (string, bool) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if len(trace.events) == 0 && trace.dropped == 0 {
		return "", false
	}
	var encoded []byte
	for {
		encoded, _ = json.Marshal(struct {
			Summary           schedulingSummary    `json:"summary"`
			Version           int                  `json:"version"`
			Dropped           int                  `json:"dropped_events,omitempty"`
			DecisionsComplete bool                 `json:"decisions_complete"`
			Events            []SchedulingDecision `json:"events"`
		}{
			Summary: trace.summary, Version: 2, Dropped: trace.dropped,
			DecisionsComplete: trace.dropped == 0 && !trace.truncated, Events: trace.events,
		})
		if len(encoded) <= maxSchedulingTraceBytes || len(trace.events) == 0 {
			break
		}
		trace.discardEvent(true)
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
	trace := schedulingTraceFromContext(ctx)
	if trace == nil {
		return
	}
	trace.mu.Lock()
	if trace.publish == nil || (len(trace.events) == 0 && trace.dropped == 0) || (!failed && !trace.recovered && schedulingTraceSamples.Add(1)%100 != 0) {
		trace.mu.Unlock()
		return
	}
	// A WS turn can reset the collector immediately after this returns.
	// Snapshot only bounded, credential-free facts; never retain Gin/context.
	snapshot := &schedulingTrace{events: append([]SchedulingDecision(nil), trace.events...), bytes: trace.bytes, dropped: trace.dropped, truncated: trace.truncated, summary: trace.summary, recovered: trace.recovered}
	if trace.summary.Excluded != nil {
		snapshot.summary.Excluded = make(map[string]int, len(trace.summary.Excluded))
		for key, value := range trace.summary.Excluded {
			snapshot.summary.Excluded[key] = value
		}
	}
	publish := trace.publish
	trace.mu.Unlock()
	log := logger.FromContext(ctx)
	publish(func() {
		detail, _ := snapshot.json()
		log.Info("gateway.scheduling_trace", zap.Bool("request_failed", failed), zap.String("decisions", detail))
	})
}

func schedulingDecisionCritical(event SchedulingDecision) bool {
	return event.Stage == "retry" || event.Action == "stopped" || event.Action == "error"
}

// Bound retained text and map payloads separately from the final JSON byte cap.
func schedulingDecisionBytes(event SchedulingDecision) int {
	size := 256 + len(event.Policy) + len(event.Stage) + len(event.Layer) + len(event.Action) + len(event.Reason)
	for key := range event.Excluded {
		size += len(key) + 32
	}
	return size
}

func (trace *schedulingTrace) discardEvent(allowCritical bool) bool {
	index := -1
	for i, event := range trace.events {
		if !schedulingDecisionCritical(event) {
			index = i
			break
		}
	}
	if index < 0 {
		if !allowCritical || len(trace.events) == 0 {
			return false
		}
		index = 0
	}
	trace.bytes -= schedulingDecisionBytes(trace.events[index])
	copy(trace.events[index:], trace.events[index+1:])
	trace.events[len(trace.events)-1] = SchedulingDecision{}
	trace.events = trace.events[:len(trace.events)-1]
	trace.dropped++
	return true
}

// The summary survives event-ring eviction. No identity becomes a metric label.
type schedulingSummary struct {
	OldestWaitMs    int64                    `json:"oldest_wait_ms,omitempty"`
	QueueAdmissions int                      `json:"queue_admissions,omitempty"`
	QueueBypasses   int                      `json:"queue_bypasses,omitempty"`
	DynamicAccounts int                      `json:"dynamic_accounts_read,omitempty"`
	Attempts        int                      `json:"candidate_attempts"`
	Pool            int                      `json:"pool"`
	Candidates      int                      `json:"candidates"`
	Excluded        map[string]int           `json:"excluded,omitempty"`
	Budget          selectionbudget.Snapshot `json:"budget"`
	StopReason      string                   `json:"stop_reason,omitempty"`
	First           *SchedulingDecision      `json:"first,omitempty"`
	Last            *SchedulingDecision      `json:"last,omitempty"`
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
		case errors.Is(*err, ErrOpenAIContinuationUnavailable):
			// A known unavailable parent is a client recovery signal, not a
			// storage outage. Preserve it without spending preparation retries.
			reason = "continuation_unavailable"
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

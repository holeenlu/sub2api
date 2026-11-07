package service

import (
	"context"
	"sync/atomic"
)

type schedulerCounters struct {
	selections, exhausted, crossLayer            atomic.Uint64
	queueAdmissions, queueBypasses, oldestWaitMs [2]atomic.Uint64 // account, user
	dynamicAccounts                              atomic.Uint64
	escapes                                      [3]atomic.Uint64 // capacity, health, qualification
	overload                                     [2]atomic.Uint64 // HTTP, WS turn
	input, cached                                [2]atomic.Uint64 // ordinary selections, escaped affinity
}

var schedulingCounters schedulerCounters

// All dimensions are fixed enums. These counters describe this process, not a
// cluster total; Ops emits their snapshot with the existing collection cadence.
type SchedulingRuntimeMetrics struct {
	QueueAdmissions      [2]uint64         `json:"queue_admissions_account_user"`
	QueueBypasses        [2]uint64         `json:"queue_bypasses_account_user"`
	OldestObservedWaitMs [2]uint64         `json:"oldest_observed_wait_ms_account_user"`
	DynamicAccountsRead  uint64            `json:"dynamic_accounts_read"`
	Selections           uint64            `json:"selections"`
	BudgetExhausted      uint64            `json:"budget_exhausted"`
	CrossLayer           uint64            `json:"cross_layer"`
	Escapes              [3]uint64         `json:"escapes_capacity_health_qualification"`
	Overload             [2]uint64         `json:"overload_http_ws"`
	InputTokens          [2]uint64         `json:"input_tokens_ordinary_escaped"`
	CacheReadTokens      [2]uint64         `json:"cached_tokens_ordinary_escaped"`
	HealthRejected       map[string]uint64 `json:"health_samples_rejected"`
}

func SnapshotSchedulingRuntimeMetrics() SchedulingRuntimeMetrics {
	out := SchedulingRuntimeMetrics{Selections: schedulingCounters.selections.Load(), BudgetExhausted: schedulingCounters.exhausted.Load(), CrossLayer: schedulingCounters.crossLayer.Load(), HealthRejected: make(map[string]uint64)}
	for i := range out.QueueAdmissions {
		out.QueueAdmissions[i] = schedulingCounters.queueAdmissions[i].Load()
		out.QueueBypasses[i] = schedulingCounters.queueBypasses[i].Load()
		out.OldestObservedWaitMs[i] = schedulingCounters.oldestWaitMs[i].Load()
	}
	out.DynamicAccountsRead = schedulingCounters.dynamicAccounts.Load()
	for i := range out.Escapes {
		out.Escapes[i] = schedulingCounters.escapes[i].Load()
	}
	for i := range out.Overload {
		out.Overload[i] = schedulingCounters.overload[i].Load()
		out.InputTokens[i] = schedulingCounters.input[i].Load()
		out.CacheReadTokens[i] = schedulingCounters.cached[i].Load()
	}
	schedulingHealthRejections.Range(func(k, v any) bool {
		key, keyOK := k.(string)
		count, countOK := v.(*atomic.Uint64)
		if keyOK && countOK {
			out.HealthRejected[key] = count.Load()
		}
		return true
	})
	return out
}
func RecordSchedulingOverload(protocol string) {
	index := 0
	if protocol == "websocket" {
		index = 1
	}
	schedulingCounters.overload[index].Add(1)
}
func recordSchedulingEscape(ctx context.Context, reason string) {
	index := 2
	switch reason {
	case "concurrency_full":
		index = 0
	case "error_rate":
		index = 1
	}
	schedulingCounters.escapes[index].Add(1)
	if trace := schedulingTraceFromContext(ctx); trace != nil {
		trace.mu.Lock()
		trace.affinityEscaped = true
		trace.mu.Unlock()
	}
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "affinity", Action: "escaped", Reason: reason})
}
func RecordSchedulingCacheTokens(ctx context.Context, input, cached int) {
	escaped, known := ctx.Value(schedulingCohortKey{}).(bool)
	if !known {
		trace := schedulingTraceFromContext(ctx)
		if trace == nil {
			return
		}
		trace.mu.Lock()
		escaped = trace.affinityEscaped
		trace.mu.Unlock()
	}
	index := 0
	if escaped {
		index = 1
	}
	schedulingCounters.input[index].Add(uint64(max(0, input)))
	schedulingCounters.cached[index].Add(uint64(max(0, cached)))
}

type schedulingCohortKey struct{}

func FreezeSchedulingMetricsContext(ctx context.Context) context.Context {
	if trace := schedulingTraceFromContext(ctx); trace != nil {
		trace.mu.Lock()
		escaped := trace.affinityEscaped
		trace.mu.Unlock()
		return context.WithValue(ctx, schedulingCohortKey{}, escaped)
	}
	return ctx
}

func WithSchedulingMetricsContext(base, parent context.Context) context.Context {
	if cohort, ok := parent.Value(schedulingCohortKey{}).(bool); ok {
		base = context.WithValue(base, schedulingCohortKey{}, cohort)
	}
	if d, ok := parent.Value(gatewayCacheDiagnosticKey{}).(gatewayCacheDiagnostic); ok {
		base = context.WithValue(base, gatewayCacheDiagnosticKey{}, d)
	}
	return base
}

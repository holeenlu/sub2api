package service

import (
	"fmt"
	"net/http"
	"strings"
)

// A catalog or original-image handoff must remain retryable until all of its
// known payloads fit. Size the new catalog before any interrupt/archive, and
// reserve the largest accepted thread ID for history and the actual turn/start
// envelope, including strict schema or best-effort schema guidance.
func cloudPreflightPendingHandoff(p *cloudProtocolRequest, history []any) error {
	encoded, err := cloudEncodeJSON(map[string]any{
		"id": ^uint64(0), "method": "thread/start", "params": cloudThreadParams(p),
	})
	if err != nil {
		return &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud tool catalog cannot be encoded"}
	}
	if len(encoded) > OpenAICloudMaxRPCBytes {
		return &cloudGatewayError{http.StatusRequestEntityTooLarge, "cloud_message_too_large", "Cloud tool catalog cannot fit within one upstream message; reduce the catalog or use the native protocol"}
	}
	// startThread accepts up to 512 bytes. Reserve their worst JSON escaping,
	// not just 512 printable bytes; the replacement identity is not known yet.
	threadID := strings.Repeat("\x00", 512)
	turn, err := cloudTurnParams(p, threadID, strings.Repeat("m", 36))
	if err != nil {
		return &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud turn payload cannot be encoded"}
	}
	encoded, err = cloudEncodeJSON(map[string]any{"id": ^uint64(0), "method": "turn/start", "params": turn})
	if err != nil {
		return &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud turn payload cannot be encoded"}
	}
	if len(encoded) > OpenAICloudMaxRPCBytes {
		return &cloudGatewayError{http.StatusRequestEntityTooLarge, "cloud_message_too_large", "Cloud turn payload cannot fit within one upstream message; reduce the input or output schema, or use the native protocol"}
	}
	snapshot := *p
	snapshot.history = history
	_, err = cloudInjectionBatches(threadID, cloudInjectionHistory(&snapshot))
	return err
}

const (
	// Real-host injection-only probe, 2026-10-25: one 6 MiB history failed
	// against a 4 MiB decoded-message boundary; three ~2 MiB appends on one
	// thread succeeded, each replying {}. Target is batching, not admission.
	cloudInjectionTargetBytes = 2 << 20
	cloudInjectionMaxBytes    = (4 << 20) - 1024
)

// cloudInjectionBatches preserves item contents and order using slice views.
// Size the exact JSON encoding without HTML escaping, including UTF-8, separators and
// the complete RPC envelope with its largest numeric ID. Keep 1 KiB below the
// observed provider boundary for its additional transport representation.
// Preflight every item before returning a batch: a late oversize item must not
// leave the owner with a partially injected prefix. Items are never split.
func cloudInjectionBatches(threadID string, items []any) ([][]any, error) {
	if len(items) == 0 {
		return nil, nil
	}
	empty, err := cloudEncodeJSON(map[string]any{
		"id": ^uint64(0), "method": "thread/inject_items",
		"params": map[string]any{"threadId": threadID, "items": []any{}},
	})
	if err != nil {
		return nil, &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud history envelope cannot be encoded"}
	}
	overhead := len(empty)
	sizes := make([]int, len(items))
	for i, item := range items {
		encoded, encodeErr := cloudEncodeJSON(item)
		if encodeErr != nil {
			return nil, &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud history item cannot be encoded"}
		}
		if len(encoded) > cloudInjectionMaxBytes-overhead {
			return nil, &cloudGatewayError{http.StatusRequestEntityTooLarge, "cloud_history_item_too_large", fmt.Sprintf("History item %d needs %d encoded bytes including its RPC envelope and cannot fit within a Cloud upstream message; reduce this item or use the native protocol", i, len(encoded)+overhead)}
		}
		sizes[i] = len(encoded)
	}
	var batches [][]any
	start, frameBytes := 0, overhead
	for i, size := range sizes {
		added := size
		if i > start {
			added++ // array separator
		}
		if i > start && frameBytes+added > cloudInjectionTargetBytes {
			batches = append(batches, items[start:i])
			start, frameBytes, added = i, overhead, size
		}
		frameBytes += added
	}
	return append(batches, items[start:]), nil
}

// Both initial migration and queued tool-continuation messages use this sender.
// Callers preflight all batches before any effect and own recovery decisions.
func (a *cloudSessionActor) injectHistoryBatches(batches [][]any, checkDispatch func() bool) (bool, error) {
	for _, batch := range batches {
		if !checkDispatch() {
			return false, nil
		}
		ctx, cancel := a.historyRPCContext()
		_, err := a.rpc.Call(ctx, "thread/inject_items", map[string]any{"threadId": a.record.ThreadID, "items": batch})
		cancel()
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// Injection has a measured atomic-item boundary. Other messages keep the
// existing transport limit: a turn/start rejection alone does not distinguish
// its wire envelope from the provider-expanded thread. Never impose an
// unverified smaller image limit or assume compression fixes decoded size.
func cloudPreflightRPCCall(method string, params any) error {
	return cloudPreflightRPCPayload(method, map[string]any{"id": ^uint64(0), "method": method, "params": params})
}
func cloudPreflightRPCPayload(method string, payload any) error {
	raw, err := cloudEncodeJSON(payload)
	if err != nil {
		return &cloudGatewayError{400, "cloud_invalid_request", "Cloud RPC envelope cannot be encoded"}
	}
	return cloudCheckRPCSize(method, len(raw))
}
func cloudCheckRPCSize(method string, size int) error {
	limit := OpenAICloudMaxRPCBytes
	if method == "thread/inject_items" {
		limit = cloudInjectionMaxBytes
	}
	if size > limit {
		return cloudWithFailureReason(&cloudGatewayError{413, "cloud_message_too_large", fmt.Sprintf("Cloud %s envelope needs %d encoded bytes and exceeds its %d byte dispatch limit; no message was sent", method, size, limit)}, "turn_input_too_large", "input")
	}
	return nil
}

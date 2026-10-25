package service

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	// Real-host injection-only probe, 2026-10-25: one 6 MiB history failed
	// against a 4 MiB decoded-message boundary; three ~2 MiB appends on one
	// thread succeeded, each replying {}. Target is batching, not admission.
	cloudInjectionTargetBytes = 2 << 20
	cloudInjectionMaxBytes    = (4 << 20) - 1024
)

// cloudInjectionBatches preserves item contents and order using slice views.
// Size the exact JSON encoding, including HTML escaping, UTF-8, separators and
// the complete RPC envelope with its largest numeric ID. Keep 1 KiB below the
// observed provider boundary for its additional transport representation.
// Preflight every item before returning a batch: a late oversize item must not
// leave the owner with a partially injected prefix. Items are never split.
func cloudInjectionBatches(threadID string, items []any) ([][]any, error) {
	if len(items) == 0 {
		return nil, nil
	}
	empty, err := json.Marshal(map[string]any{
		"id": ^uint64(0), "method": "thread/inject_items",
		"params": map[string]any{"threadId": threadID, "items": []any{}},
	})
	if err != nil {
		return nil, &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud history envelope cannot be encoded"}
	}
	overhead := len(empty)
	sizes := make([]int, len(items))
	for i, item := range items {
		encoded, encodeErr := json.Marshal(item)
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

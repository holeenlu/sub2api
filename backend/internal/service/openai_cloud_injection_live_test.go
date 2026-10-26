//go:build cloudlive

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Probe the real host's injection envelope boundary independently of inference.
// Only synthetic ASCII messages are sent; neither thread starts a turn or offers
// tools. A single large rejection is observational, while cumulative small
// injections must succeed to establish that batching actually preserves history.
func TestCloudLiveInjectionBatchingWithoutInference(t *testing.T) {
	path := os.Getenv("SUB2API_CLOUD_LIVE_AUTH_FILE")
	if path == "" {
		t.Skip("real Cloud injection validation requires SUB2API_CLOUD_LIVE_AUTH_FILE; no upstream request was made")
	}
	token, workspace := cloudLiveReadAuthorization(t, path)
	model := os.Getenv("SUB2API_CLOUD_LIVE_MODEL")
	if model == "" {
		model = "gpt-6.1-sol"
	}
	cloudLiveCheck(t, len(model) <= 100 && strings.IndexFunc(model, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r))
	}) < 0, "live Cloud model name is invalid")

	items := make([]any, 6)
	for i := range items {
		items[i] = map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": strings.Repeat(string(rune('A'+i)), 1<<20)}},
		}
	}
	original, err := json.Marshal(items)
	cloudLiveCheck(t, err == nil && len(original) > 6<<20, "synthetic injection fixture is invalid")

	t.Run("single_six_mib", func(t *testing.T) {
		ctx, rpc, thread := cloudLiveInjectionThread(t, token, workspace, model)
		params := map[string]any{"threadId": thread, "items": items}
		encoded, encodeErr := json.Marshal(params)
		cloudLiveCheck(t, encodeErr == nil, "synthetic injection parameters could not be encoded")
		result, callErr := rpc.Call(ctx, "thread/inject_items", params)
		cloudLiveInjectionOutcome(t, callErr, len(encoded), len(result))
		cloudLiveInjectionEventCounts(t, rpc)
		if callErr != nil {
			var rejection *cloudRPCError
			cloudLiveCheck(t, errors.As(callErr, &rejection), "single injection did not yield a success or a classified RPC rejection")
			if cloudLiveInjectionSizeError.MatchString(rejection.Reason) {
				status, code, _, _ := cloudPublicError(callErr)
				cloudLiveCheck(t, status == 413 && code == "cloud_message_too_large", "the real size refusal lost its distinct public classification")
			}
			return
		}
		cloudLiveInjectionSmallReply(t, result)
	})

	t.Run("bounded_batches", func(t *testing.T) {
		ctx, rpc, thread := cloudLiveInjectionThread(t, token, workspace, model)
		batches, batchErr := cloudInjectionBatches(thread, items)
		cloudLiveCheck(t, batchErr == nil && len(batches) > 1, "production injection batching could not prepare the synthetic history")
		var submitted []any
		for _, batch := range batches {
			params := map[string]any{"threadId": thread, "items": batch}
			encoded, encodeErr := json.Marshal(params)
			cloudLiveCheck(t, encodeErr == nil && len(encoded) < 3<<20, "synthetic batch exceeds its probe budget")
			result, callErr := rpc.Call(ctx, "thread/inject_items", params)
			cloudLiveInjectionOutcome(t, callErr, len(encoded), len(result))
			cloudLiveInjectionEventCounts(t, rpc)
			cloudLiveCheck(t, callErr == nil, "cumulative batched history injection was rejected")
			cloudLiveInjectionSmallReply(t, result)
			submitted = append(submitted, batch...)
		}
		rejoined, encodeErr := json.Marshal(submitted)
		cloudLiveCheck(t, encodeErr == nil && bytes.Equal(original, rejoined), "batching changed synthetic history content or ordering")
		t.Logf("injection-only completed: batches=%d items=%d cumulative_history_bytes=%d turns_started=0", len(batches), len(submitted), len(rejoined))
	})
}

func cloudLiveInjectionThread(t *testing.T, token, workspace, model string) (context.Context, *cloudRPC, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	rpc, err := dialOpenAICloud(ctx, token, workspace, "")
	if err != nil {
		cancel()
		cloudLiveInjectionOutcome(t, err, 0, 0)
		t.Fatal("live Cloud injection connection could not be initialized")
	}
	thread := ""
	t.Cleanup(func() {
		defer cancel()
		defer rpc.Close()
		if thread == "" {
			return
		}
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		attempt, endAttempt := context.WithTimeout(cleanup, 5*time.Second)
		_, archiveErr := rpc.Call(attempt, "thread/archive", map[string]any{"threadId": thread})
		endAttempt()
		if archiveErr != nil && !cloudThreadDiscarded(archiveErr) {
			// Cleanup may reconnect only to archive this test's known thread; it
			// never repeats injection or starts inference after a failed send.
			if retry, dialErr := dialOpenAICloud(cleanup, token, workspace, ""); dialErr == nil {
				_, archiveErr = retry.Call(cleanup, "thread/archive", map[string]any{"threadId": thread})
				_ = retry.Close()
			}
		}
		if archiveErr != nil && !cloudThreadDiscarded(archiveErr) {
			cloudLiveInjectionOutcome(t, archiveErr, 0, 0)
			t.Error("live injection probe could not confirm scoped thread archival")
		}
	})
	result, err := rpc.Call(ctx, "thread/start", cloudThreadParams(&cloudProtocolRequest{model: model}))
	if err != nil {
		cloudLiveInjectionOutcome(t, err, 0, 0)
		t.Fatal("live injection thread could not be created")
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	cloudLiveCheck(t, json.Unmarshal(result, &started) == nil && started.Thread.ID != "", "live injection thread identity was not confirmed")
	thread = started.Thread.ID
	return ctx, rpc, thread
}

func cloudLiveInjectionSmallReply(t *testing.T, raw json.RawMessage) {
	t.Helper()
	cloudLiveCheck(t, len(raw) <= 1024, "injection returned a large response instead of a small acknowledgement")
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	var result map[string]json.RawMessage
	cloudLiveCheck(t, json.Unmarshal(raw, &result) == nil && result != nil, "injection acknowledgement was not an object")
}

var cloudLiveInjectionSizeError = regexp.MustCompile(`decoded message length too large: found ([0-9]+) bytes, the limit is: ([0-9]+) bytes`)

func cloudLiveInjectionOutcome(t *testing.T, err error, requestBytes, responseBytes int) {
	t.Helper()
	var rpcErr *cloudRPCError
	rpcCode, observedBytes, limitBytes := 0, uint64(0), uint64(0)
	eventsObserved := false
	if errors.As(err, &rpcErr) {
		rpcCode, eventsObserved = rpcErr.Code, rpcErr.EventsObserved
		if match := cloudLiveInjectionSizeError.FindStringSubmatch(rpcErr.Reason); len(match) == 3 {
			observedBytes, _ = strconv.ParseUint(match[1], 10, 64)
			limitBytes, _ = strconv.ParseUint(match[2], 10, 64)
		}
	}
	status, code, _, _ := cloudPublicError(err)
	t.Logf("injection probe: success=%t rpc_code=%d status=%d code=%s request_bytes=%d response_bytes=%d observed_message_bytes=%d limit_bytes=%d events_observed=%t",
		err == nil, rpcCode, status, code, requestBytes, responseBytes, observedBytes, limitBytes, eventsObserved)
}

func cloudLiveInjectionEventCounts(t *testing.T, rpc *cloudRPC) {
	t.Helper()
	counts := make(map[string]int)
	rpc.mu.Lock()
	for event := rpc.eventHead; event != nil; event = event.next {
		method := event.message.Method
		switch method {
		case "thread/started", "thread/status/changed", "rawResponseItem/completed", "item/started", "item/completed", "turn/started", "turn/completed", "thread/tokenUsage/updated", "item/agentMessage/delta", "error":
		default:
			method = "other"
		}
		counts[method]++
	}
	queuedBytes, receivedEvents := rpc.eventBytes, rpc.eventCount
	rpc.mu.Unlock()
	// The test never consumes notifications. These are cumulative local receipt
	// counts at acknowledgement time, not a claim to have read back remote history.
	t.Logf("injection event snapshot: methods=%v received=%d queued_bytes=%d", counts, receivedEvents, queuedBytes)
}

//go:build cloudlive

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Run a migrated conversation through the production actor against real Cloud.
// The only client tool has no side effects; no production request is replayed.
func TestCloudLiveMigratedHistoryQueuedMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	history := []any{
		cloudUserMessage("This earlier turn asked for an echo of OLD_CONTEXT."),
		map[string]any{"type": "function_call", "call_id": "call_native_before_cloud", "name": "echo", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "call_native_before_cloud", "output": "OLD_CONTEXT"},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "OLD_CONTEXT"}}},
		cloudUserMessage("Call the client echo tool exactly once with {}. Wait for its result. Do not use any other tool."),
	}
	request := map[string]any{"input": history, "tools": cloudHistoryTools, "reasoning": map[string]any{"effort": "low"}}
	pending := h.run(request, false)
	first := cloudLiveResponse(t, pending.Response)
	cloudLiveCheck(t, pending.AwaitingTools && !pending.Terminal, "migrated history did not reach its client tool")
	var callID string
	for _, item := range first.Output {
		if item.Type == "function_call" {
			cloudLiveCheck(t, item.Name == "echo" && callID == "", "unexpected live tool call")
			callID = item.CallID
		}
	}
	cloudLiveCheck(t, callID != "", "no client tool call returned")
	var original struct{ Output []json.RawMessage }
	cloudLiveCheck(t, json.Unmarshal(pending.Response, &original) == nil, "could not read first public output")
	for _, item := range original.Output {
		history = append(history, item)
	}
	history = append(history,
		map[string]any{"type": "function_call_output", "call_id": callID, "output": "Tool has finished. Follow the newly queued messages without calling any more tools."},
		map[string]any{"role": "system", "content": "For this short protocol check, the first word is CLOUD."},
		map[string]any{"role": "developer", "content": "For this short protocol check, the second word is HISTORY."},
		cloudUserMessage("The third word is RETAINED. Reply with exactly the three words, in their given order, separated by spaces. No tools."),
	)
	request["input"] = history
	completed := h.run(request, true)
	final := h.completed(completed)
	t.Logf("synthetic queued-message answer=%q", final.text())
	cloudLiveCheck(t, strings.TrimSpace(final.text()) == "CLOUD HISTORY RETAINED", "queued role context was not applied to the resumed inference")
	retry := h.run(request, false)
	cloudLiveCheck(t, retry.Replayed && cloudLiveResponse(t, retry.Response).ID == final.ID && h.store.settlementCount() == 1, "live retry executed or settled the same continuation again")
	next := h.run(map[string]any{"previous_response_id": final.ID, "input": "Repeat those three words exactly once, without tools."}, true)
	nextResponse := cloudLiveResponse(t, next.Response)
	cloudLiveCheck(t, next.Terminal && next.ErrorStatus == 0 && next.UsageCredible && next.ThreadID == completed.ThreadID, "subsequent user turn did not complete on the same thread")
	cloudLiveCheck(t, strings.TrimSpace(nextResponse.text()) == "CLOUD HISTORY RETAINED", "next turn lost the queued history")
	cloudLiveCheck(t, h.store.settlementCount() == 2, "two turns must settle exactly twice")
	h.log(completed)
	h.log(next)
	h.close(next)
}

// Exercise discovery with an actually new tool: no catalog superset is known
// when the first Cloud thread starts, and no completed tool is executed twice.
func TestCloudLiveMigratedToolDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	seed := newCloudLiveHarness(t, ctx)
	initial := cloudUserMessage("Calculate (127 multiplied by 83, minus 47 multiplied by 19, plus 61 multiplied by 29), then divide that result by 7. Return only the integer. Do not use tools.")
	seedResult := seed.run(map[string]any{"input": []any{initial}, "reasoning": map[string]any{"effort": "high"}, "include": []string{"reasoning.encrypted_content"}}, false)
	seedResponse := seed.completed(seedResult)
	var seedOutput struct{ Output []json.RawMessage }
	cloudLiveCheck(t, json.Unmarshal(seedResult.Response, &seedOutput) == nil, "encrypted seed output is invalid")
	history := []any{initial}
	encrypted := false
	for i, item := range seedResponse.Output {
		encrypted = encrypted || item.Type == "reasoning" && item.EncryptedContent != ""
		history = append(history, seedOutput.Output[i])
	}
	cloudLiveCheck(t, encrypted, "discovery probe needs real encrypted prior history")
	seed.close(seedResult)
	h := newCloudLiveHarness(t, ctx)
	tools := []any{map[string]any{"type": "tool_search", "execution": "client"}}
	history = append(history, cloudUserMessage("Search for a client echo tool using tool_search. After discovery, call echo once with {}. After its result, reply with exactly the returned text and do not use any other tools."))
	pending := h.run(map[string]any{"input": history, "tools": tools, "reasoning": map[string]any{"effort": "low"}}, false)
	var output struct{ Output []map[string]any }
	cloudLiveCheck(t, json.Unmarshal(pending.Response, &output) == nil && pending.AwaitingTools, "client discovery was not requested")
	searchID := ""
	for _, item := range output.Output {
		history = append(history, item)
		if item["type"] == "tool_search_call" {
			searchID, _ = item["call_id"].(string)
		}
	}
	cloudLiveCheck(t, searchID != "", "response did not restore client tool-search semantics")
	history = append(history, map[string]any{"type": "tool_search_output", "call_id": searchID, "execution": "client", "status": "completed", "tools": cloudHistoryTools})
	request := map[string]any{"input": history, "tools": tools, "reasoning": map[string]any{"effort": "low"}}
	discovered := h.run(request, true)
	cloudLiveCheck(t, discovered.AwaitingTools && discovered.ThreadID != pending.ThreadID && discovered.RootRequestID == pending.RootRequestID, "catalog did not hand off on the same billing root")
	output.Output = nil
	cloudLiveCheck(t, json.Unmarshal(discovered.Response, &output) == nil, "invalid discovered-tool response")
	echoID := ""
	for _, item := range output.Output {
		history = append(history, item)
		if item["type"] == "function_call" && item["name"] == "echo" {
			echoID, _ = item["call_id"].(string)
		}
	}
	cloudLiveCheck(t, echoID != "", "newly discovered tool was not callable")
	retry := h.run(request, false)
	cloudLiveCheck(t, retry.Replayed && retry.ThreadID == discovered.ThreadID, "discovery retry repeated its handoff")
	history = append(history, map[string]any{"type": "function_call_output", "call_id": echoID, "output": "DISCOVERED_CONTEXT_RETAINED"})
	completed := h.run(map[string]any{"input": history, "tools": tools, "reasoning": map[string]any{"effort": "low"}}, true)
	final := h.completed(completed)
	cloudLiveCheck(t, strings.TrimSpace(final.text()) == "DISCOVERED_CONTEXT_RETAINED", "discovered tool output was not retained")
	h.log(completed)
	h.close(completed)
}

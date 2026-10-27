//go:build cloudlive

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Each harness owns one freshly created thread and one fake routing store. All
// inference goes through production Run; cleanup may reconnect only to archive
// that known thread. There is no inference retry or handler/database substitute.
type cloudLiveHarness struct {
	t         *testing.T
	ctx       context.Context
	owner     OpenAICloudOwner
	store     *cloudLiveRoutingStore
	first     *OpenAICloudSessions
	second    *OpenAICloudSessions
	dials     atomic.Int32
	connected atomic.Pointer[cloudRPC]
	seen      map[string]int
	archived  bool
}

// Completed-parent changes materialize the owner's history, without replaying
// its old inference or dropping the newly supplied client context.
func TestCloudLiveLatestParentContextAndSettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	marker := "CLOUD_CONTEXT_" + uuid.NewString()
	first := h.run(map[string]any{"instructions": "Remember the user's marker. Reply briefly.", "reasoning": map[string]any{"effort": "low"}, "input": "Remember this marker for the next turn: " + marker + ". Reply only OK; do not use tools."}, false)
	previous := h.completed(first)
	second := h.run(map[string]any{"previous_response_id": previous.ID, "instructions": "Follow the latest developer context for the answer format.", "input": []any{
		map[string]any{"role": "developer", "content": "For this turn, return the remembered marker followed by /CONTEXT_UPDATED. Use no tools."},
		map[string]any{"role": "user", "content": "Return the marker in the updated format."},
	}}, true)
	answer := cloudLiveResponse(t, second.Response)
	cloudLiveCheck(t, second.Terminal && second.ErrorStatus == 0 && answer.Status == "completed", "updated latest-parent context/settings failed")
	cloudLiveCheck(t, strings.TrimSpace(answer.text()) == marker+"/CONTEXT_UPDATED", "new thread lost history or the appended developer context")
	cloudLiveCheck(t, second.ThreadID != first.ThreadID && second.SessionID == first.SessionID && second.RootRequestID != first.RootRequestID, "completed settings change must replace only the upstream thread and start one new root")
	cloudLiveCheck(t, h.store.settlementCount() == 2 && h.dials.Load() == 1, "context/settings continuation duplicated settlement or reconnected")
	h.close(second)
	h.log(second)
}

func TestCloudLiveCachedCustomToolContinuation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	first := h.run(map[string]any{"tools": []any{cloudApplyPatchTool}, "parallel_tool_calls": false, "reasoning": map[string]any{"effort": "low"}, "input": "This is a synthetic tool-protocol test. Call apply_patch to add a.txt containing one line A. Wait for its result, then make a second separate apply_patch call to add b.txt containing one line B. After both tool results reply only DONE. Do not use other tools."}, false)
	response := cloudLiveResponse(t, first.Response)
	findCall := func(r cloudLiveResponseView) string {
		id, count := "", 0
		for _, item := range r.Output {
			if item.Type == "custom_tool_call" && item.Name == "apply_patch" {
				id, count = item.CallID, count+1
			}
		}
		cloudLiveCheck(t, count == 1 && id != "", "Cloud did not publish exactly one custom patch call")
		return id
	}
	cloudLiveCheck(t, first.AwaitingTools, "first custom tool was not handed to the client")
	reply := map[string]any{"previous_response_id": response.ID, "input": []any{map[string]any{"type": "custom_tool_call_output", "call_id": findCall(response), "output": "Synthetic patch accepted."}}}
	second := h.run(reply, true)
	secondResponse := cloudLiveResponse(t, second.Response)
	cloudLiveCheck(t, second.AwaitingTools, "second custom tool was not handed to the client")
	secondCall := findCall(secondResponse)
	replayed := h.run(reply, true)
	cloudLiveCheck(t, replayed.Replayed && bytes.Equal(second.Response, replayed.Response) && h.store.settlementCount() == 0, "cached custom response was not replayed exactly without settlement")
	final := h.run(map[string]any{"previous_response_id": secondResponse.ID, "input": []any{map[string]any{"type": "custom_tool_call_output", "call_id": secondCall, "output": "Synthetic patch accepted."}}}, true)
	answer := h.completed(final)
	cloudLiveCheck(t, strings.TrimSpace(answer.text()) == "DONE" && final.RootRequestID == first.RootRequestID, "custom continuation lost its root or final answer")
	h.close(final)
	h.log(final)
}

// Imported Codex conversations can emit hundreds of replay notifications before
// turn/start replies. Exercise the production actor with history beyond the
// transport notification queue, without private client content or tool effects.
func TestCloudLiveLargeImportedHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	defer func() {
		if rpc := h.connected.Load(); rpc != nil {
			err := rpc.Err()
			t.Logf("transport overflow=%t closed=%t protocol=%t", errors.Is(err, errCloudRPCEventOverflow), errors.Is(err, errCloudRPCClosed), errors.Is(err, errCloudRPCProtocol))
		}
	}()
	history := make([]any, 0, 401)
	for i := 0; i < 200; i++ {
		history = append(history, map[string]any{"role": "user", "content": "Synthetic history entry " + strconv.Itoa(i)}, map[string]any{"role": "assistant", "content": "Acknowledged."})
	}
	history = append(history, map[string]any{"role": "user", "content": "Reply with exactly OK. Do not use tools."})
	result := h.run(map[string]any{"input": history}, false)
	answer := h.completed(result)
	cloudLiveCheck(t, strings.TrimSpace(answer.text()) == "OK", "large imported history answer was incorrect")
	h.close(result)
	h.log(result)
}

func newCloudLiveHarness(t *testing.T, ctx context.Context) *cloudLiveHarness {
	t.Helper()
	path := os.Getenv("SUB2API_CLOUD_LIVE_AUTH_FILE")
	if path == "" {
		t.Skip("real Cloud validation requires SUB2API_CLOUD_LIVE_AUTH_FILE; no upstream request was made")
	}
	token, workspace := cloudLiveReadAuthorization(t, path)
	model := os.Getenv("SUB2API_CLOUD_LIVE_MODEL")
	if model == "" {
		model = "gpt-5.6-luna"
	}
	cloudLiveCheck(t, len(model) <= 100 && strings.IndexFunc(model, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r))
	}) < 0, "live Cloud model name is invalid")
	h := &cloudLiveHarness{t: t, ctx: ctx, store: newCloudLiveRoutingStore(), seen: make(map[string]int), owner: OpenAICloudOwner{Probe: true, UserID: 1, APIKeyID: 0, GroupID: 0, AccountID: 1, GrantGeneration: uuid.NewString(), Model: model, SettingsDigest: "isolated-live-protocol-check"}}
	connect := func(ctx context.Context, expected OpenAICloudOwner) (*cloudRPC, error) {
		if expected != h.owner {
			return nil, errors.New("live Cloud owner mismatch")
		}
		if h.dials.Add(1) != 1 {
			return nil, errors.New("live Cloud check refuses another inference connection")
		}
		rpc, err := dialOpenAICloud(ctx, token, workspace, "")
		if err == nil {
			h.connected.Store(rpc)
		}
		return rpc, err
	}
	validate := func(_ context.Context, expected OpenAICloudOwner) error {
		if expected != h.owner {
			return errors.New("live Cloud owner mismatch")
		}
		return nil
	}
	h.first = NewOpenAICloudSessions(h.store, connect, validate)
	h.second = NewOpenAICloudSessions(h.store, connect, validate)
	t.Cleanup(func() {
		defer h.first.Close()
		defer h.second.Close()
		if h.archived {
			return
		}
		record := h.store.snapshot()
		if record == nil || record.ThreadID == "" {
			return
		}
		// Only this test's saved thread ID can reach the cleanup connection.
		// It cannot run a turn, inject history, or answer a pending tool request.
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		var archiveErr error = errors.New("live Cloud connection is unavailable")
		if rpc := h.connected.Load(); rpc != nil {
			attempt, cancel := context.WithTimeout(cleanup, 5*time.Second)
			if record.TurnID != "" && (record.Status == "running" || record.Status == "awaiting_tools") {
				_, _ = rpc.Call(attempt, "turn/interrupt", map[string]any{"threadId": record.ThreadID, "turnId": record.TurnID})
			}
			_, archiveErr = rpc.Call(attempt, "thread/archive", map[string]any{"threadId": record.ThreadID})
			cancel()
		}
		// Ephemeral threads may already be discarded after an upstream failure.
		// Accept only the exact, sanitized missing-thread response to this archive.
		_, archiveCode, _, _ := cloudPublicError(archiveErr)
		if archiveCode == "cloud_thread_unavailable" {
			archiveErr = nil
		}
		if archiveErr != nil {
			rpc, err := dialOpenAICloud(cleanup, token, workspace, "")
			if err == nil {
				_, archiveErr = rpc.Call(cleanup, "thread/archive", map[string]any{"threadId": record.ThreadID})
				_ = rpc.Close()
				_, archiveCode, _, _ = cloudPublicError(archiveErr)
				if archiveCode == "cloud_thread_unavailable" {
					archiveErr = nil
				}
			}
		}
		if archiveErr != nil {
			t.Error("live Cloud cleanup could not confirm thread archival")
		}
	})
	return h
}

func (h *cloudLiveHarness) run(body map[string]any, secondManager bool) *OpenAICloudResult {
	h.t.Helper()
	body["model"] = h.owner.Model
	manager := h.first
	if secondManager {
		manager = h.second
	}
	result, err := manager.Run(h.ctx, OpenAICloudRequest{Owner: h.owner, Body: cloudLiveBody(h.t, body), RootRequestID: "live-root-" + uuid.NewString()}, func(raw json.RawMessage) error {
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &event) != nil || event.Type == "" {
			return errors.New("live Cloud event shape is invalid")
		}
		h.seen[event.Type]++
		return nil
	})
	if err != nil {
		h.t.Logf("live adapter control error=%v", err)
		status, code, message, known := cloudPublicError(err)
		if known {
			h.t.Logf("live adapter status=%d code=%s message=%s", status, code, message)
		} else {
			// Inputs in this opt-in harness are synthetic. Transport and RPC
			// errors have already discarded provider payloads and credentials.
			h.t.Logf("live adapter local error=%v", err)
		}
	}
	cloudLiveCheck(h.t, err == nil && result != nil, "live Cloud production adapter request failed")
	return result
}

func (h *cloudLiveHarness) completed(result *OpenAICloudResult) cloudLiveResponseView {
	h.t.Helper()
	response := cloudLiveResponse(h.t, result.Response)
	if record := h.store.snapshot(); record != nil && response.Status != "completed" {
		h.t.Logf("live state=%s status=%d code=%s safe_failure=%s", record.Status, result.ErrorStatus, response.Error.Code, record.Failure)
	}
	cloudLiveCheck(h.t, result.Terminal && !result.AwaitingTools && result.ErrorStatus == 0 && response.Status == "completed", "live Cloud response did not complete successfully")
	cloudLiveCheck(h.t, result.UsageCredible && result.Usage != nil && result.Usage.InputTokens > 0 && result.Usage.TotalTokens == result.Usage.InputTokens+result.Usage.OutputTokens, "live Cloud response lacks credible token usage")
	cloudLiveCheck(h.t, h.store.settlementCount() == 1 && h.store.settlementMatches(result.RootRequestID, result.Usage), "live Cloud root does not have exactly one matching settlement")
	cloudLiveCheck(h.t, h.dials.Load() == 1, "live Cloud inference unexpectedly reconnected")
	return response
}

func (h *cloudLiveHarness) close(result *OpenAICloudResult) {
	h.t.Helper()
	err := h.second.CloseCompleted(h.ctx, h.owner, result.SessionID)
	if err != nil {
		status, code, _, known := cloudPublicError(err)
		h.t.Logf("live close failed status=%d code=%s classified=%t", status, code, known)
	}
	cloudLiveCheck(h.t, err == nil && h.store.wasReleased(), "live Cloud completed thread could not be archived and released")
	h.archived = true
}

func (h *cloudLiveHarness) log(result *OpenAICloudResult) {
	h.t.Helper()
	u := result.Usage
	cloudLiveCheck(h.t, u != nil, "live Cloud log requires measured usage")
	h.t.Logf("model=%s status=passed input_tokens=%d output_tokens=%d web_search_count=%d image_count=%d", h.owner.Model, u.InputTokens, u.OutputTokens, u.WebSearchCalls, u.ImageCount)
}

// Real Cloud must follow client permission context, including after a stale
// read-only assistant response. Only the exact synthetic client write is run.
func TestCloudLiveClientPermissionContext(t *testing.T) {
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			h := newCloudLiveHarness(t, ctx)
			workspace := t.TempDir()
			marker := "CLOUD_PERMISSION_" + uuid.NewString()
			permission := "Filesystem sandboxing defines which files can be read or written. `sandbox_mode` is `" + mode + "`. "
			if mode == "read-only" {
				permission += "The sandbox only permits reading files."
			} else if mode == "workspace-write" {
				permission += "The sandbox permits reading files, and editing files in cwd and writable_roots."
			} else {
				permission += "No filesystem sandboxing - all commands are permitted."
			}
			pending := h.run(map[string]any{
				"reasoning": map[string]any{"effort": "low"},
				"tools":     []any{map[string]any{"type": "function", "name": "write_file", "description": "Write a UTF-8 file in the client workspace, subject to the client's filesystem permissions.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []string{"path", "content"}, "additionalProperties": false}}},
				"input": []any{
					map[string]any{"role": "user", "content": "Can you edit the project?"},
					map[string]any{"role": "assistant", "content": "The previous session was read-only."},
					map[string]any{"role": "developer", "content": "<permissions instructions>" + permission + " Approval policy is currently never. Do not request escalation.</permissions instructions>"},
					map[string]any{"role": "user", "content": "<environment_context><cwd>" + workspace + "</cwd></environment_context>"},
					map[string]any{"role": "user", "content": "Create permission-check.txt in the current workspace with exactly this content: " + marker + ". Use write_file if the current permissions allow it; otherwise briefly explain the current restriction. Do not claim to write a file without calling the tool."},
				},
			}, false)
			response := cloudLiveResponse(t, pending.Response)
			path := filepath.Join(workspace, "permission-check.txt")
			if mode == "read-only" {
				cloudLiveCheck(t, !pending.AwaitingTools, "a read-only client must not be instructed to write")
				h.completed(pending)
				_, err := os.Stat(path)
				cloudLiveCheck(t, os.IsNotExist(err), "read-only check unexpectedly wrote a file")
				h.close(pending)
				h.log(pending)
				return
			}
			cloudLiveCheck(t, pending.AwaitingTools && !pending.Terminal, "writable client did not receive its file-write tool call")
			callID, count := "", 0
			for _, item := range response.Output {
				if item.Type != "function_call" {
					continue
				}
				count++
				var args struct{ Path, Content string }
				cloudLiveCheck(t, item.Name == "write_file" && json.Unmarshal([]byte(item.Arguments), &args) == nil, "unexpected write tool or arguments")
				cloudLiveCheck(t, (args.Path == "permission-check.txt" || args.Path == path) && args.Content == marker, "write target/content must match the synthetic task")
				callID = item.CallID
			}
			cloudLiveCheck(t, count == 1 && callID != "" && h.store.settlementCount() == 0, "client write must have one pending call and no early settlement")
			// The harness executes only this exact synthetic write. No model-supplied
			// path or shell command is executed, and t.TempDir removes the output.
			cloudLiveCheck(t, os.WriteFile(path, []byte(marker), 0600) == nil, "client test workspace write failed")
			result := h.run(map[string]any{"previous_response_id": response.ID, "input": []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": "Created permission-check.txt successfully."}}}, true)
			h.completed(result)
			data, err := os.ReadFile(path)
			cloudLiveCheck(t, err == nil && string(data) == marker, "client write result was not preserved")
			cloudLiveCheck(t, result.RootRequestID == pending.RootRequestID && result.SessionID == pending.SessionID, "permission handling changed the tool continuation owner or billing root")
			h.close(result)
			h.log(result)
		})
	}
}

// This opt-in test performs exactly two real Cloud turns on one persistent
// thread, with one client-tool round trip. The synchronized store below tests
// manager-to-owner routing, not Redis durability, OAuth callbacks or paid billing.
func TestCloudLiveAdapterToolRoutingAndTurnWatermark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	owner, model := h.owner, h.owner.Model
	store, firstManager, secondManager := h.store, h.first, h.second
	marker := "CLOUD_ECHO_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	rootOne, rootTwo := "live-root-"+uuid.NewString(), "live-root-"+uuid.NewString()
	request := cloudLiveBody(t, map[string]any{
		"model": model, "reasoning": map[string]any{"effort": "low"},
		"tools": []any{map[string]any{"type": "function", "name": "cloud_live_echo", "description": "Return the exact supplied text; this isolated client tool has no side effects.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}, "additionalProperties": false}}},
		"input": []any{
			map[string]any{"role": "developer", "content": "This is an isolated protocol check. The remembered marker is " + marker + ". Use only the declared client echo tool when explicitly asked. Do not use any built-in tool or environment."},
			map[string]any{"role": "user", "content": "Remember the marker from the developer message for the next request."},
			map[string]any{"role": "assistant", "content": "I have retained the marker."},
			map[string]any{"role": "user", "content": "Call cloud_live_echo exactly once with the remembered marker as text, then reply exactly with the returned text. Do not use other tools."},
		},
	})
	seen := make(map[string]int)
	emit := func(raw json.RawMessage) error {
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &event) != nil {
			return errors.New("live Cloud event is invalid")
		}
		seen[event.Type]++
		return nil
	}
	pending, err := firstManager.Run(ctx, OpenAICloudRequest{Owner: owner, Body: request, RootRequestID: rootOne}, emit)
	cloudLiveCheck(t, err == nil, "first Cloud request failed")
	if pending == nil || !pending.AwaitingTools || pending.Terminal || pending.Usage != nil {
		record := store.snapshot()
		if record != nil {
			t.Logf("live state=%s safe_failure=%s", record.Status, record.Failure)
		}
	}
	cloudLiveCheck(t, pending != nil && pending.AwaitingTools && !pending.Terminal && pending.Usage == nil, "first Cloud response did not preserve the pending tool state")
	firstResponse := cloudLiveResponse(t, pending.Response)
	var callID string
	toolCount := 0
	for _, item := range firstResponse.Output {
		if item.Type == "function_call" {
			toolCount++
			callID = item.CallID
			var args struct {
				Text string `json:"text"`
			}
			cloudLiveCheck(t, item.Name == "cloud_live_echo" && json.Unmarshal([]byte(item.Arguments), &args) == nil && args.Text == marker, "Cloud tool arguments did not preserve the injected role history")
		}
	}
	cloudLiveCheck(t, toolCount == 1 && callID != "", "Cloud must return exactly the requested client tool")
	cloudLiveCheck(t, store.settlementCount() == 0, "pending tool response must not settle the root")
	continuation := cloudLiveBody(t, map[string]any{"model": model, "previous_response_id": firstResponse.ID, "input": []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": marker}}})
	completed, err := secondManager.Run(ctx, OpenAICloudRequest{Owner: owner, Body: continuation, RootRequestID: "ignored-live-continuation"}, emit)
	cloudLiveCheck(t, err == nil, "cross-manager Cloud tool continuation failed")
	cloudLiveCheck(t, completed != nil && completed.Terminal && !completed.AwaitingTools && completed.UsageCredible && completed.Usage != nil && completed.Usage.InputTokens > 0, "first Cloud turn did not return credible completed usage")
	cloudLiveCheck(t, completed.RootRequestID == rootOne && completed.SessionID == pending.SessionID, "tool continuation changed its billing root or live session")
	finalOne := cloudLiveResponse(t, completed.Response)
	cloudLiveCheck(t, strings.TrimSpace(finalOne.text()) == marker, "first Cloud turn did not return the echo result")
	totalOne := cloudLiveTotal(t, firstManager, completed.SessionID)
	cloudLiveCheck(t, reflect.DeepEqual(totalOne, *completed.Usage), "first root usage differs from its observed thread total")
	cloudLiveCheck(t, store.settlementCount() == 1 && store.settlementMatches(rootOne, completed.Usage), "first root did not produce exactly one matching settlement")
	newTurn := cloudLiveBody(t, map[string]any{"model": model, "previous_response_id": finalOne.ID, "input": "Reply exactly CLOUD_SECOND_OK. Do not call any tools."})
	second, err := secondManager.Run(ctx, OpenAICloudRequest{Owner: owner, Body: newTurn, RootRequestID: rootTwo}, emit)
	cloudLiveCheck(t, err == nil, "second Cloud turn failed")
	cloudLiveCheck(t, second != nil && second.Terminal && !second.AwaitingTools && second.UsageCredible && second.Usage != nil && second.Usage.InputTokens > 0, "second Cloud turn did not return credible completed usage")
	cloudLiveCheck(t, second.RootRequestID == rootTwo && second.SessionID == completed.SessionID, "second turn did not preserve its session and create a new billing root")
	finalTwo := cloudLiveResponse(t, second.Response)
	cloudLiveCheck(t, strings.TrimSpace(finalTwo.text()) == "CLOUD_SECOND_OK", "second Cloud turn did not return the requested short answer")
	totalTwo := cloudLiveTotal(t, firstManager, second.SessionID)
	difference, diffErr := cloudUsageDifference(&totalTwo, &totalOne)
	cloudLiveCheck(t, diffErr == nil && reflect.DeepEqual(difference, second.Usage), "second root usage does not equal the verified thread total difference")
	cloudLiveCheck(t, store.settlementCount() == 2 && store.settlementMatches(rootOne, completed.Usage) && store.settlementMatches(rootTwo, second.Usage), "completed turns did not produce exactly one settlement per root")
	cloudLiveCheck(t, h.dials.Load() == 1 && firstManager.instance != secondManager.instance, "the two managers did not use the original live RPC owner")
	cloudLiveCheck(t, seen["response.function_call_arguments.delta"] > 0 && seen["response.output_text.delta"] > 0 && seen["response.completed"] == 3, "formal Responses events were incomplete")
	err = secondManager.CloseCompleted(ctx, owner, second.SessionID)
	cloudLiveCheck(t, err == nil && store.wasReleased(), "completed Cloud test thread could not be archived and released")
	h.archived = true
	t.Logf("model=%s status=passed first_input_tokens=%d first_output_tokens=%d second_input_tokens=%d second_output_tokens=%d", model, completed.Usage.InputTokens, completed.Usage.OutputTokens, second.Usage.InputTokens, second.Usage.OutputTokens)
}

func cloudLiveImage(t *testing.T) string {
	t.Helper()
	bitmap := image.NewRGBA(image.Rect(0, 0, 160, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 160; x++ {
			pixel := color.RGBA{R: 255, G: 255, A: 255}
			if x < 80 {
				pixel = color.RGBA{B: 255, A: 255}
			}
			bitmap.SetRGBA(x, y, pixel)
		}
	}
	var out bytes.Buffer
	cloudLiveCheck(t, png.Encode(&out, bitmap) == nil, "live Cloud image fixture could not be encoded")
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}

const cloudLiveIdentifyColors = "Identify the solid color of the left panel and then the right panel in the image. Reply with exactly the two English color names in uppercase, separated by one space. No other text."

func TestCloudLiveAdapterInlinePNG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	result := h.run(map[string]any{"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": cloudLiveIdentifyColors}, map[string]any{"type": "input_image", "image_url": cloudLiveImage(t)}}}}}, false)
	response := h.completed(result)
	cloudLiveCheck(t, strings.TrimSpace(response.text()) == "BLUE YELLOW", "live Cloud did not recognize the inline image panels")
	cloudLiveCheck(t, h.seen["response.output_text.delta"] > 0 && h.seen["response.completed"] == 1, "live Cloud image input response stream was incomplete")
	h.close(result)
	h.log(result)
}

// Exercise the client-visible serial contract, unmodified output history and a
// completed thread's next-turn reasoning/schema settings against the real host.
func TestCloudLiveAdapterSerialToolsAndOutputHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	tools := []any{map[string]any{"type": "function", "name": "cloud_live_echo", "description": "Echo the supplied text; no side effects.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}, "additionalProperties": false}}}
	history := []any{map[string]any{"role": "user", "content": "Call cloud_live_echo twice: once with ALPHA and once with BETA. Both calls are required. Then answer ALPHA BETA. Do not use other tools."}}
	body := map[string]any{"input": history, "tools": tools, "parallel_tool_calls": false, "reasoning": map[string]any{"effort": "low"}, "store": false}
	calls := 0
	seen := map[string]bool{}
	var final *OpenAICloudResult
	for step := 0; step < 4; step++ {
		result := h.run(body, step%2 == 1)
		var response struct {
			ID     string           `json:"id"`
			Output []map[string]any `json:"output"`
		}
		cloudLiveCheck(t, json.Unmarshal(result.Response, &response) == nil, "live serial response was invalid")
		var current map[string]any
		for _, item := range response.Output {
			history = append(history, item)
			if item["type"] == "function_call" {
				cloudLiveCheck(t, current == nil, "serial response exposed more than one tool")
				current = item
			}
		}
		if current == nil {
			cloudLiveCheck(t, result.Terminal && result.UsageCredible && calls == 2, "serial turn finished without both tools or credible usage")
			final = result
			break
		}
		call, _ := current["call_id"].(string)
		arguments, _ := current["arguments"].(string)
		var args struct {
			Text string `json:"text"`
		}
		cloudLiveCheck(t, call != "" && !seen[call] && current["name"] == "cloud_live_echo" && json.Unmarshal([]byte(arguments), &args) == nil && (args.Text == "ALPHA" || args.Text == "BETA"), "serial tool identity or arguments were invalid")
		seen[call] = true
		calls++
		cloudLiveCheck(t, calls <= 2 && result.AwaitingTools && result.Usage == nil, "serial tool pause had unexpected usage or extra calls")
		output := map[string]any{"type": "function_call_output", "call_id": call, "output": args.Text}
		history = append(history, output)
		body = map[string]any{"previous_response_id": response.ID, "input": []any{output}}
	}
	cloudLiveCheck(t, final != nil && h.store.settlementCount() == 1, "serial tools did not settle exactly once")
	previous := cloudLiveResponse(t, final.Response)
	history = append(history, map[string]any{"role": "user", "content": "Return value 7 using the requested schema. Do not call tools."})
	next := h.run(map[string]any{"previous_response_id": previous.ID, "input": history, "reasoning": map[string]any{"effort": "medium", "summary": "concise"}, "text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "result", "strict": true, "schema": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "integer"}}, "required": []string{"value"}, "additionalProperties": false}}}}, true)
	cloudLiveCheck(t, next.Terminal && next.UsageCredible && next.SessionID == final.SessionID && next.RootRequestID != final.RootRequestID, "next turn did not preserve its session and new billing root")
	var answer struct {
		Value int `json:"value"`
	}
	cloudLiveCheck(t, json.Unmarshal([]byte(cloudLiveResponse(t, next.Response).text()), &answer) == nil && answer.Value == 7, "next turn schema result was invalid")
	cloudLiveCheck(t, h.store.settlementCount() == 2, "next turn did not settle exactly once")
	h.close(next)
	h.log(next)
}

func TestCloudLiveAdapterToolPNGAcrossManagers(t *testing.T) {
	testCloudLiveAdapterToolPNG(t, false)
}

func TestCloudLiveOriginalToolPNGAndNonStrictSchema(t *testing.T) {
	testCloudLiveAdapterToolPNG(t, true)
}

func testCloudLiveAdapterToolPNG(t *testing.T, original bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	request := map[string]any{
		"tools": []any{map[string]any{"type": "function", "name": "cloud_live_screenshot", "description": "Return a synthetic screenshot with two solid panels, with no side effects.", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}},
		"input": "Call cloud_live_screenshot exactly once, then inspect the returned image. " + cloudLiveIdentifyColors,
	}
	if original {
		request["input"] = "Call cloud_live_screenshot exactly once, then identify the two color panels left to right. Put their uppercase color names, separated by one space, in the final JSON colors string."
		request["text"] = map[string]any{"format": map[string]any{"type": "json_schema", "name": "codex_output_schema", "strict": false, "schema": map[string]any{"type": "object", "properties": map[string]any{"colors": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}}, "required": []string{"colors"}, "additionalProperties": false}}}
	}
	pending := h.run(request, false)
	cloudLiveCheck(t, pending.AwaitingTools && !pending.Terminal && pending.Usage == nil && h.store.settlementCount() == 0, "live Cloud screenshot call did not remain pending")
	response := cloudLiveResponse(t, pending.Response)
	callID, count := "", 0
	for _, item := range response.Output {
		if item.Type == "function_call" {
			count++
			var arguments map[string]any
			cloudLiveCheck(t, item.Name == "cloud_live_screenshot" && json.Unmarshal([]byte(item.Arguments), &arguments) == nil && arguments != nil && len(arguments) == 0, "live Cloud requested an unexpected screenshot tool")
			callID = item.CallID
		}
	}
	cloudLiveCheck(t, count == 1 && callID != "", "live Cloud did not request exactly one screenshot")
	image := map[string]any{"type": "input_image", "image_url": cloudLiveImage(t)}
	if original {
		image["detail"] = "original"
	}
	result := h.run(map[string]any{"previous_response_id": response.ID, "input": []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": []any{map[string]any{"type": "input_text", "text": "Synthetic screenshot attached."}, image}}}}, true)
	final := h.completed(result)
	cloudLiveCheck(t, result.SessionID == pending.SessionID && result.RootRequestID == pending.RootRequestID, "live Cloud screenshot continuation changed session or root")
	answer := strings.TrimSpace(final.text())
	if original {
		var structured struct{ Colors string }
		cloudLiveCheck(t, json.Unmarshal([]byte(answer), &structured) == nil, "non-strict output was not JSON")
		answer = structured.Colors
	}
	if answer != "BLUE YELLOW" {
		t.Logf("synthetic image answer=%q", final.text())
	}
	cloudLiveCheck(t, h.first.instance != h.second.instance && answer == "BLUE YELLOW", "live Cloud did not recognize the cross-manager tool image")
	cloudLiveCheck(t, h.seen["response.function_call_arguments.delta"] > 0 && h.seen["response.completed"] == 2, "live Cloud screenshot continuation events were incomplete")
	h.close(result)
	h.log(result)
}

func TestCloudLiveAdapterEncryptedHistoryAcrossThreads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	first := newCloudLiveHarness(t, ctx)
	initialMessage := map[string]any{"role": "user", "content": "Calculate (127 multiplied by 83, minus 47 multiplied by 19, plus 61 multiplied by 29), then divide that result by 7. Return only the integer. Do not use tools."}
	result := first.run(map[string]any{"reasoning": map[string]any{"effort": "high"}, "include": []string{"reasoning.encrypted_content"}, "input": []any{initialMessage}}, false)
	response := first.completed(result)
	cloudLiveCheck(t, strings.TrimSpace(response.text()) == "1631", "live Cloud encrypted-history first answer was incorrect")
	var original struct {
		Output []json.RawMessage `json:"output"`
	}
	cloudLiveCheck(t, json.Unmarshal(result.Response, &original) == nil, "live Cloud encrypted output could not be decoded")
	encryptedCount := 0
	history := []any{initialMessage}
	for i, item := range response.Output {
		if item.Type == "reasoning" && item.EncryptedContent != "" {
			encryptedCount++
			var fields map[string]json.RawMessage
			cloudLiveCheck(t, json.Unmarshal(original.Output[i], &fields) == nil, "live Cloud reasoning item shape is invalid")
			for key := range fields {
				cloudLiveCheck(t, key == "id" || key == "type" || key == "status" || key == "summary" || key == "encrypted_content", "live Cloud exposed a private reasoning field")
			}
		}
		history = append(history, original.Output[i])
	}
	cloudLiveCheck(t, encryptedCount > 0, "live Cloud returned no real encrypted reasoning")
	first.close(result)
	first.log(result)
	// A new fake store and owner creates an independent Cloud thread. Passing
	// the public items unchanged exercises production thread/inject_items.
	second := newCloudLiveHarness(t, ctx)
	history = append(history, map[string]any{"role": "user", "content": "Square the final result of the previous calculation. Return only the integer. Do not use tools."})
	next := second.run(map[string]any{"include": []string{"reasoning.encrypted_content"}, "input": history}, false)
	final := second.completed(next)
	cloudLiveCheck(t, next.ThreadID != result.ThreadID && next.SessionID != result.SessionID, "live Cloud encrypted history was not tested on an independent thread")
	cloudLiveCheck(t, strings.TrimSpace(final.text()) == "2660161", "live Cloud rejected or misread the complete encrypted history")
	second.close(next)
	second.log(next)
}

// One isolated real turn validates that Cloud accepts empty turn/start input
// after imported client-tool history, rather than just the local fake RPC.
func TestCloudLiveAdapterImportedToolHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	result := h.run(map[string]any{"instructions": "Reply with the last tool result verbatim. Do not call tools.", "input": []any{
		map[string]any{"role": "user", "content": "Read the echo result."},
		map[string]any{"type": "function_call", "call_id": "call_native_history", "name": "echo", "arguments": `{}`},
		map[string]any{"type": "function_call_output", "call_id": "call_native_history", "output": "CLOUD_HISTORY_OK"},
	}, "tools": cloudHistoryTools}, false)
	response := h.completed(result)
	cloudLiveCheck(t, strings.TrimSpace(response.text()) == "CLOUD_HISTORY_OK", "live Cloud did not read the imported tool result")
	h.close(result)
	h.log(result)
}

func TestCloudLiveAdapterHostedSearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	result := h.run(map[string]any{
		"tools": []any{map[string]any{"type": "web_search"}}, "include": []string{"web_search_call.action.sources"},
		"input": "Use web search now to find the official OpenAI Responses API documentation. Return its official documentation URL in one short sentence. Do not rely on memory and do not use any other tool.",
	}, false)
	response := h.completed(result)
	searchCount, sourceCount := 0, 0
	for _, item := range response.Output {
		cloudLiveCheck(t, item.Type != "function_call" && item.Type != "custom_tool_call", "live Cloud exposed a hosted search as a client tool")
		if item.Type != "web_search_call" {
			continue
		}
		searchCount++
		cloudLiveCheck(t, item.Status == "completed" && item.Action.Type != "", "live Cloud search did not contain a completed action")
		if item.Action.Type == "search" {
			cloudLiveCheck(t, item.Action.Query != "" || len(item.Action.Queries) > 0, "live Cloud search action has no real query")
		}
		for _, source := range item.Action.Sources {
			parsed, err := url.Parse(source.URL)
			cloudLiveCheck(t, err == nil && parsed.Hostname() != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") && source.Type == "url", "live Cloud search source is not a valid URL source")
			sourceCount++
		}
	}
	cloudLiveCheck(t, searchCount > 0 && sourceCount > 0 && result.Usage.WebSearchCalls == searchCount, "live Cloud search lacks actual sources or matching hosted usage")
	cloudLiveCheck(t, h.seen["response.web_search_call.completed"] == searchCount && h.seen["response.completed"] == 1, "live Cloud hosted search events were incomplete")
	h.close(result)
	h.log(result)
}

func TestCloudLiveAdapterHostedImageGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	result := h.run(map[string]any{"tools": []any{map[string]any{"type": "image_generation"}}, "input": "Use the image generation tool to generate exactly one simple square image: one red circle centered on a plain white background, no text. Return the generated image, not instructions or code. Do not use any other tool."}, false)
	response := h.completed(result)
	count := 0
	var sizes []string
	tiers := make(map[string]int)
	for _, item := range response.Output {
		cloudLiveCheck(t, item.Type != "function_call" && item.Type != "custom_tool_call", "live Cloud exposed hosted image generation as a client tool")
		if item.Type != "image_generation_call" {
			continue
		}
		count++
		cloudLiveCheck(t, item.Status == "completed" && item.OutputFormat == "png" && item.Result != "", "live Cloud generated image is incomplete")
		data, err := base64.StdEncoding.Strict().DecodeString(item.Result)
		cloudLiveCheck(t, err == nil && len(data) > 0 && len(data) <= OpenAICloudMaxResultBytes, "live Cloud generated image is not real base64 data")
		config, err := png.DecodeConfig(bytes.NewReader(data))
		cloudLiveCheck(t, err == nil && config.Width > 0 && config.Height > 0 && config.Width <= 8192 && config.Height <= 8192 && int64(config.Width)*int64(config.Height) <= 64<<20, "live Cloud generated PNG dimensions are invalid")
		reader := bytes.NewReader(data)
		_, err = png.Decode(reader)
		cloudLiveCheck(t, err == nil && reader.Len() == 0, "live Cloud generated PNG does not fully decode")
		size := strconv.Itoa(config.Width) + "x" + strconv.Itoa(config.Height)
		cloudLiveCheck(t, item.Size == size, "live Cloud generated image size differs from actual pixels")
		tier, ok := ClassifyImageBillingTier(size)
		cloudLiveCheck(t, ok, "live Cloud generated image has no supported billing tier")
		tiers[tier]++
		sizes = append(sizes, size)
	}
	cloudLiveCheck(t, count == 1 && result.Usage.ImageCount == count && reflect.DeepEqual(sizes, result.Usage.ImageOutputSizes) && reflect.DeepEqual(tiers, result.Usage.ImageSizes), "live Cloud image counts or dimensions do not match measured hosted usage")
	cloudLiveCheck(t, h.seen["response.image_generation_call.completed"] == count && h.seen["response.completed"] == 1, "live Cloud hosted image events were incomplete")
	h.close(result)
	h.log(result)
}

func TestCloudLiveAdapterInvalidEncryptedHistoryRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	result := h.run(map[string]any{"input": []any{
		map[string]any{"type": "reasoning", "id": "rs_sub2api_invalid_encrypted_live", "summary": []any{}, "encrypted_content": "sub2api-intentionally-invalid-encrypted-context"},
		map[string]any{"role": "user", "content": "Reply OK."},
	}}, false)
	response := cloudLiveResponse(t, result.Response)
	if response.Status == "completed" {
		h.completed(result)
		recovered := false
		for _, decision := range result.Adaptations {
			recovered = recovered || decision.MatrixID == "CLOUD-RECOVERY"
		}
		cloudLiveCheck(t, recovered, "invalid cipher was completed without an observable approved reduction")
		cloudLiveCheck(t, strings.EqualFold(strings.TrimSpace(response.text()), "OK"), "readable turn input was lost during recovery")
		h.close(result)
		h.log(result)
		return
	}
	t.Logf("invalid-history result terminal=%t awaiting=%t status=%s http_status=%d code=%s", result.Terminal, result.AwaitingTools, response.Status, result.ErrorStatus, response.Error.Code)
	if record := h.store.snapshot(); record != nil {
		t.Logf("invalid-history state=%s safe_failure=%s", record.Status, record.Failure)
	}
	providerRejected := result.ErrorStatus == 400 && response.Error.Code == "invalid_encrypted_content"
	threadDiscarded := result.ErrorStatus == 502 && response.Error.Code == "cloud_thread_unavailable"
	cloudLiveCheck(t, result.Terminal && !result.AwaitingTools && response.Status == "failed" && (providerRejected || threadDiscarded), "live Cloud malformed encrypted history did not preserve an observed upstream rejection")
	if threadDiscarded {
		cloudLiveCheck(t, result.RejectedBeforeInference && result.UsageCredible && result.Usage != nil && result.Usage.TotalTokens == 0,
			"lost ephemeral thread must prove that this history never reached inference")
	}
	cloudLiveCheck(t, h.seen["response.completed"] == 0 && h.seen["response.failed"] == 1, "live Cloud invalid encrypted history was reported as success")
	cloudLiveCheck(t, h.dials.Load() == 1, "live Cloud invalid encrypted history was retried")
	t.Logf("model=%s status=expected_failure http_status=%d code=%s count=1", h.owner.Model, result.ErrorStatus, response.Error.Code)
}

func TestCloudLiveParserCompleteClientContract(t *testing.T) {
	if os.Getenv("SUB2API_CLOUD_LIVE_AUTH_FILE") == "" {
		t.Skip("real Cloud validation requires SUB2API_CLOUD_LIVE_AUTH_FILE; no upstream request was made")
	}
	raw, err := os.ReadFile("testdata/cloud_codex_0_164_catalog_shapes.json")
	cloudLiveCheck(t, err == nil, "sanitized client fixture is unavailable")
	var fixture struct {
		Requests map[string]map[string]any `json:"requests"`
	}
	cloudLiveCheck(t, json.Unmarshal(raw, &fixture) == nil, "sanitized client fixture is invalid")
	for _, shape := range []string{"standard", "lite"} {
		t.Run(shape, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			h := newCloudLiveHarness(t, ctx)
			body := fixture.Requests[shape]
			items := body["input"].([]any)
			items[len(items)-1] = map[string]any{"role": "user", "content": "Reply exactly CLOUD_CLIENT_OK. Do not call tools or use any environment."}
			result := h.run(body, false)
			response := h.completed(result)
			cloudLiveCheck(t, strings.TrimSpace(response.text()) == "CLOUD_CLIENT_OK", "complete client contract did not preserve the user turn")
			h.close(result)
			h.log(result)
		})
	}
}

func cloudLiveCheck(t *testing.T, ok bool, message string) {
	t.Helper()
	if !ok {
		t.Fatal(message)
	}
}
func cloudLiveBody(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	cloudLiveCheck(t, err == nil, "live Cloud fixture could not be encoded")
	return raw
}

func cloudLiveReadAuthorization(t *testing.T, path string) (string, string) {
	t.Helper()
	file, err := os.Open(path)
	cloudLiveCheck(t, err == nil, "live native authorization file could not be opened")
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 128<<10+1))
	cloudLiveCheck(t, err == nil && len(raw) <= 128<<10, "live native authorization file could not be read within its limit")
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	err = json.Unmarshal(raw, &auth)
	for i := range raw {
		raw[i] = 0
	}
	cloudLiveCheck(t, err == nil && auth.Tokens.AccessToken != "", "live native authorization has no access token")
	workspace := auth.Tokens.AccountID
	if workspace == "" {
		parts := strings.Split(auth.Tokens.AccessToken, ".")
		cloudLiveCheck(t, len(parts) == 3, "live native authorization has no workspace")
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		cloudLiveCheck(t, err == nil, "live native authorization workspace could not be read")
		var claims struct {
			Auth struct {
				AccountID string `json:"chatgpt_account_id"`
			} `json:"https://api.openai.com/auth"`
		}
		err = json.Unmarshal(payload, &claims)
		for i := range payload {
			payload[i] = 0
		}
		cloudLiveCheck(t, err == nil, "live native authorization workspace is invalid")
		workspace = claims.Auth.AccountID
	}
	cloudLiveCheck(t, workspace != "", "live native authorization has no workspace")
	return auth.Tokens.AccessToken, workspace
}

type cloudLiveResponseView struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  struct {
		Code string `json:"code"`
	} `json:"error"`
	Output []struct {
		ID               string `json:"id"`
		Type             string `json:"type"`
		Status           string `json:"status"`
		Phase            string `json:"phase"`
		CallID           string `json:"call_id"`
		Name             string `json:"name"`
		Arguments        string `json:"arguments"`
		EncryptedContent string `json:"encrypted_content"`
		Result           string `json:"result"`
		OutputFormat     string `json:"output_format"`
		Size             string `json:"size"`
		Action           struct {
			Type    string   `json:"type"`
			Query   string   `json:"query"`
			Queries []string `json:"queries"`
			Sources []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"sources"`
		} `json:"action"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func cloudLiveResponse(t *testing.T, raw json.RawMessage) cloudLiveResponseView {
	t.Helper()
	var response cloudLiveResponseView
	cloudLiveCheck(t, json.Unmarshal(raw, &response) == nil && response.ID != "", "live Cloud response shape is invalid")
	return response
}
func (r cloudLiveResponseView) text() string {
	hasFinal := false
	for _, item := range r.Output {
		if item.Type == "message" && item.Phase == "final_answer" {
			hasFinal = true
			break
		}
	}
	var out strings.Builder
	for _, item := range r.Output {
		if item.Type == "message" && (!hasFinal || item.Phase == "final_answer") {
			for _, content := range item.Content {
				if content.Type == "output_text" {
					out.WriteString(content.Text)
				}
			}
		}
	}
	return out.String()
}

func cloudLiveTotal(t *testing.T, manager *OpenAICloudSessions, id string) OpenAICloudUsage {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	actor := manager.actors[id]
	cloudLiveCheck(t, actor != nil && actor.threadTotal != nil, "live Cloud actor lost its observed total")
	// A completed result has crossed the store's mutex after the usage write.
	// No new command is submitted until this snapshot is copied; completed-turn
	// duplicate notifications do not mutate the actor's total.
	return *actor.threadTotal
}

// Large opaque history can enter Cloud's automatic compaction before sampling.
// Only the explicit ciphertext rejection permits the existing bounded recovery.
func TestCloudLiveRemoteCompactionRejectedHistoryRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	items := []any{cloudUserMessage("Synthetic old context")}
	for i := range 256 {
		items = append(items, map[string]any{"type": "reasoning", "id": "rs_synthetic_" + strconv.Itoa(i), "summary": []any{}, "encrypted_content": "gAAAA" + strings.Repeat("A", 8192)})
	}
	items = append(items, cloudUserMessage("Reply exactly OK. Do not use tools."))
	result := h.run(map[string]any{"input": items}, false)
	response := h.completed(result)
	cloudLiveCheck(t, strings.TrimSpace(response.text()) == "OK", "large rejected history did not recover")
	recovered := false
	for _, d := range result.Adaptations {
		recovered = recovered || d.MatrixID == "CLOUD-RECOVERY"
	}
	cloudLiveCheck(t, recovered, "opaque history must have an explicit recovery decision")
	h.close(result)
	h.log(result)
}

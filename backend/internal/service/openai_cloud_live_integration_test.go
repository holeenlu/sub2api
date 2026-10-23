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
	"reflect"
	"strconv"
	"strings"
	"sync"
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
		if archiveErr != nil {
			rpc, err := dialOpenAICloud(cleanup, token, workspace, "")
			if err == nil {
				_, archiveErr = rpc.Call(cleanup, "thread/archive", map[string]any{"threadId": record.ThreadID})
				_ = rpc.Close()
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
	cloudLiveCheck(h.t, err == nil && h.store.wasReleased(), "live Cloud completed thread could not be archived and released")
	h.archived = true
}

func (h *cloudLiveHarness) log(result *OpenAICloudResult) {
	h.t.Helper()
	u := result.Usage
	cloudLiveCheck(h.t, u != nil, "live Cloud log requires measured usage")
	h.t.Logf("model=%s status=passed input_tokens=%d output_tokens=%d web_search_count=%d image_count=%d", h.owner.Model, u.InputTokens, u.OutputTokens, u.WebSearchCalls, u.ImageCount)
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

func TestCloudLiveAdapterToolPNGAcrossManagers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	pending := h.run(map[string]any{
		"tools": []any{map[string]any{"type": "function", "name": "cloud_live_screenshot", "description": "Return a synthetic screenshot with two solid panels, with no side effects.", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}},
		"input": "Call cloud_live_screenshot exactly once, then inspect the returned image. " + cloudLiveIdentifyColors,
	}, false)
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
	result := h.run(map[string]any{"previous_response_id": response.ID, "input": []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": []any{map[string]any{"type": "input_text", "text": "Synthetic screenshot attached."}, map[string]any{"type": "input_image", "image_url": cloudLiveImage(t)}}}}}, true)
	final := h.completed(result)
	cloudLiveCheck(t, result.SessionID == pending.SessionID && result.RootRequestID == pending.RootRequestID, "live Cloud screenshot continuation changed session or root")
	if strings.TrimSpace(final.text()) != "BLUE YELLOW" {
		t.Logf("synthetic image answer=%q", final.text())
	}
	cloudLiveCheck(t, h.first.instance != h.second.instance && strings.TrimSpace(final.text()) == "BLUE YELLOW", "live Cloud did not recognize the cross-manager tool image")
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

func TestCloudLiveAdapterRejectsInvalidEncryptedHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newCloudLiveHarness(t, ctx)
	result := h.run(map[string]any{"input": []any{
		map[string]any{"type": "reasoning", "id": "rs_sub2api_invalid_encrypted_live", "summary": []any{}, "encrypted_content": "sub2api-intentionally-invalid-encrypted-context"},
		map[string]any{"role": "user", "content": "Reply OK."},
	}}, false)
	response := cloudLiveResponse(t, result.Response)
	cloudLiveCheck(t, result.Terminal && !result.AwaitingTools && response.Status == "failed" && result.ErrorStatus == 400 && response.Error.Code == "invalid_encrypted_content", "live Cloud invalid encrypted history did not return terminal HTTP 400 invalid_encrypted_content")
	cloudLiveCheck(t, h.seen["response.completed"] == 0 && h.seen["response.failed"] == 1, "live Cloud invalid encrypted history was reported as success")
	cloudLiveCheck(t, h.dials.Load() == 1, "live Cloud invalid encrypted history was retried")
	t.Logf("model=%s status=expected_failure count=1", h.owner.Model)
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

// Shared by both managers, but deliberately named as a fake routing store. It
// reuses the existing actor fixture with synchronization, owner fencing, bounded
// streams and snapshot copies; it makes no claim about actual Redis behavior.
type cloudLiveRoutingStore struct {
	*cloudActorTestStore
	mu       sync.Mutex
	changed  chan struct{}
	commands map[string][]OpenAICloudStreamMessage
	events   map[string][]OpenAICloudStreamMessage
	bytes    map[string]int
	closed   map[string]bool
	released bool
	sequence uint64
}

func newCloudLiveRoutingStore() *cloudLiveRoutingStore {
	return &cloudLiveRoutingStore{cloudActorTestStore: &cloudActorTestStore{refs: map[string]bool{}}, changed: make(chan struct{}), commands: map[string][]OpenAICloudStreamMessage{}, events: map[string][]OpenAICloudStreamMessage{}, bytes: map[string]int{}, closed: map[string]bool{}}
}
func (s *cloudLiveRoutingStore) signal() { close(s.changed); s.changed = make(chan struct{}) }
func cloudLiveClone[T any](value T) T {
	raw, _ := json.Marshal(value)
	var copy T
	_ = json.Unmarshal(raw, &copy)
	return copy
}
func (s *cloudLiveRoutingStore) CreateCloudSession(_ context.Context, r *OpenAICloudSessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record != nil {
		return errors.New("live check allows one Cloud thread")
	}
	s.record = cloudLiveClone(r)
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) LoadCloudSession(_ context.Context, id string) (*OpenAICloudSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.SessionID != id {
		return nil, nil
	}
	return cloudLiveClone(s.record), nil
}
func (s *cloudLiveRoutingStore) FindCloudSession(_ context.Context, user, key, group int64, ref string) (*OpenAICloudSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.UserID != user || s.record.APIKeyID != key || s.record.GroupID != group || !s.refs[ref] {
		return nil, nil
	}
	return cloudLiveClone(s.record), nil
}
func (s *cloudLiveRoutingStore) SaveCloudSession(_ context.Context, r *OpenAICloudSessionRecord, refs []string, settlement *OpenAICloudSettlement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.released || s.record.OwnerToken != r.OwnerToken || s.record.OpenAICloudOwner != r.OpenAICloudOwner || !time.Now().Before(s.record.LeaseUntil) || r.Sequence <= s.record.Sequence {
		return errors.New("live Cloud owner fence rejected the update")
	}
	copy := cloudLiveClone(r)
	copy.LeaseUntil = s.record.LeaseUntil
	if settlement != nil {
		for _, old := range s.settlements {
			if old.RootRequestID == settlement.RootRequestID {
				return errors.New("live Cloud root was settled twice")
			}
		}
		if settlement.Sequence != copy.Sequence || settlement.RootRequestID != copy.RootRequestID {
			return errors.New("live Cloud settlement snapshot mismatch")
		}
	}
	if err := s.cloudActorTestStore.SaveCloudSession(context.Background(), copy, refs, cloudLiveClone(settlement)); err != nil {
		return err
	}
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) RenewCloudSession(_ context.Context, id, owner string, until time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.released || s.record.SessionID != id || s.record.OwnerToken != owner || !time.Now().Before(s.record.LeaseUntil) {
		return false, nil
	}
	s.record.LeaseUntil = until
	return true, nil
}
func (s *cloudLiveRoutingStore) EnqueueCloudCommand(_ context.Context, id string, command OpenAICloudCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.SessionID != id || s.released || command.Request.Owner != s.record.OpenAICloudOwner {
		return errors.New("live Cloud command owner mismatch")
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return errors.New("live Cloud command encoding failed")
	}
	if len(s.commands[id]) >= OpenAICloudMaxCommands || s.bytes["command:"+id]+len(raw) > OpenAICloudMaxEventStreamBytes {
		return errors.New("live Cloud command limit exceeded")
	}
	s.sequence++
	s.commands[id] = append(s.commands[id], OpenAICloudStreamMessage{ID: strconv.FormatUint(s.sequence, 10) + "-0", Payload: raw})
	s.bytes["command:"+id] += len(raw)
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) PublishCloudEvent(_ context.Context, id string, event OpenAICloudStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed[id] {
		return errors.New("live Cloud request is closed")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return errors.New("live Cloud event encoding failed")
	}
	if len(s.events[id]) >= OpenAICloudMaxStreamEntries || s.bytes["event:"+id]+len(raw) > OpenAICloudMaxEventStreamBytes {
		return errors.New("live Cloud event limit exceeded")
	}
	s.sequence++
	s.events[id] = append(s.events[id], OpenAICloudStreamMessage{ID: strconv.FormatUint(s.sequence, 10) + "-0", Payload: raw})
	s.bytes["event:"+id] += len(raw)
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) ReadCloudCommands(ctx context.Context, id, after string) ([]OpenAICloudStreamMessage, error) {
	return s.read(ctx, id, after, true)
}
func (s *cloudLiveRoutingStore) ReadCloudEvents(ctx context.Context, id, after string) ([]OpenAICloudStreamMessage, error) {
	return s.read(ctx, id, after, false)
}
func (s *cloudLiveRoutingStore) read(ctx context.Context, id, after string, command bool) ([]OpenAICloudStreamMessage, error) {
	cursor, _ := strconv.ParseUint(strings.SplitN(after, "-", 2)[0], 10, 64)
	for {
		s.mu.Lock()
		messages := s.events[id]
		if command {
			messages = s.commands[id]
		}
		var out []OpenAICloudStreamMessage
		for _, message := range messages {
			number, _ := strconv.ParseUint(strings.SplitN(message.ID, "-", 2)[0], 10, 64)
			if number > cursor {
				out = append(out, cloudLiveClone(message))
			}
		}
		changed := s.changed
		s.mu.Unlock()
		if len(out) > 0 {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		case <-time.After(time.Second):
			return nil, nil
		}
	}
}
func (s *cloudLiveRoutingStore) DeleteCloudEvents(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.events, id)
	delete(s.bytes, "event:"+id)
	s.closed[id] = true
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) ReleaseCloudSession(_ context.Context, id, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.SessionID != id || s.record.OwnerToken != owner || s.record.Status != "completed" {
		return errors.New("live Cloud release owner mismatch")
	}
	s.released = true
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) snapshot() *OpenAICloudSessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloudLiveClone(s.record)
}
func (s *cloudLiveRoutingStore) settlementCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.settlements)
}
func (s *cloudLiveRoutingStore) settlementMatches(root string, usage *OpenAICloudUsage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, settle := range s.settlements {
		if settle.RootRequestID == root {
			if settle.Unknown || settle.Status != "completed" || !reflect.DeepEqual(settle.Usage, usage) {
				return false
			}
			count++
		}
	}
	return count == 1
}
func (s *cloudLiveRoutingStore) wasReleased() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.released
}

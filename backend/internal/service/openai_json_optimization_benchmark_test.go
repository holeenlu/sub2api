package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var (
	benchmarkToolContinuationBoolSink bool
	benchmarkWSParseStringSink        string
	benchmarkWSParseMapSink           map[string]any
	benchmarkUsageSink                OpenAIUsage
)

func BenchmarkToolContinuationValidationLegacy(b *testing.B) {
	reqBody := benchmarkToolContinuationRequestBody()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkToolContinuationBoolSink = legacyValidateFunctionCallOutputContext(reqBody)
	}
}

func BenchmarkToolContinuationValidationOptimized(b *testing.B) {
	reqBody := benchmarkToolContinuationRequestBody()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkToolContinuationBoolSink = optimizedValidateFunctionCallOutputContext(reqBody)
	}
}

func BenchmarkWSIngressPayloadParseLegacy(b *testing.B) {
	raw := benchmarkWSIngressPayloadBytes()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventType, model, promptCacheKey, previousResponseID, payload, err := legacyParseWSIngressPayload(raw)
		if err == nil {
			benchmarkWSParseStringSink = eventType + model + promptCacheKey + previousResponseID
			benchmarkWSParseMapSink = payload
		}
	}
}

func BenchmarkWSIngressPayloadParseOptimized(b *testing.B) {
	raw := benchmarkWSIngressPayloadBytes()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventType, model, promptCacheKey, previousResponseID, payload, err := optimizedParseWSIngressPayload(raw)
		if err == nil {
			benchmarkWSParseStringSink = eventType + model + promptCacheKey + previousResponseID
			benchmarkWSParseMapSink = payload
		}
	}
}

func BenchmarkOpenAIUsageExtractLegacy(b *testing.B) {
	body := benchmarkOpenAIUsageJSONBytes()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		usage, ok := legacyExtractOpenAIUsageFromJSONBytes(body)
		if ok {
			benchmarkUsageSink = usage
		}
	}
}

func BenchmarkOpenAIUsageExtractOptimized(b *testing.B) {
	body := benchmarkOpenAIUsageJSONBytes()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		usage, ok := extractOpenAIUsageFromJSONBytes(body)
		if ok {
			benchmarkUsageSink = usage
		}
	}
}

func benchmarkToolContinuationRequestBody() map[string]any {
	input := make([]any, 0, 64)
	for i := 0; i < 24; i++ {
		input = append(input, map[string]any{
			"type": "text",
			"text": "benchmark text",
		})
	}
	for i := 0; i < 10; i++ {
		callID := "call_" + strconv.Itoa(i)
		input = append(input, map[string]any{
			"type":    "tool_call",
			"call_id": callID,
		})
		input = append(input, map[string]any{
			"type":    "function_call_output",
			"call_id": callID,
		})
		input = append(input, map[string]any{
			"type": "item_reference",
			"id":   callID,
		})
	}
	return map[string]any{
		"model": "gpt-5.3-codex",
		"input": input,
	}
}

func benchmarkWSIngressPayloadBytes() []byte {
	return []byte(`{"type":"response.create","model":"gpt-5.3-codex","prompt_cache_key":"cache_bench","previous_response_id":"resp_prev_bench","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
}

func benchmarkOpenAIUsageJSONBytes() []byte {
	return []byte(`{"id":"resp_bench","object":"response","model":"gpt-5.3-codex","usage":{"input_tokens":3210,"output_tokens":987,"input_tokens_details":{"cached_tokens":456}}}`)
}

func legacyValidateFunctionCallOutputContext(reqBody map[string]any) bool {
	if !legacyHasFunctionCallOutput(reqBody) {
		return true
	}
	previousResponseID, _ := reqBody["previous_response_id"].(string)
	if strings.TrimSpace(previousResponseID) != "" {
		return true
	}
	if legacyHasToolCallContext(reqBody) {
		return true
	}
	if legacyHasFunctionCallOutputMissingCallID(reqBody) {
		return false
	}
	callIDs := legacyFunctionCallOutputCallIDs(reqBody)
	return legacyHasItemReferenceForCallIDs(reqBody, callIDs)
}

func optimizedValidateFunctionCallOutputContext(reqBody map[string]any) bool {
	validation := ValidateFunctionCallOutputContext(reqBody)
	if !validation.HasFunctionCallOutput {
		return true
	}
	previousResponseID, _ := reqBody["previous_response_id"].(string)
	if strings.TrimSpace(previousResponseID) != "" {
		return true
	}
	if validation.HasToolCallContext {
		return true
	}
	if validation.HasFunctionCallOutputMissingCallID {
		return false
	}
	return validation.HasItemReferenceForAllCallIDs
}

func legacyHasFunctionCallOutput(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType == "function_call_output" {
			return true
		}
	}
	return false
}

func legacyHasToolCallContext(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "tool_call" && itemType != "function_call" {
			continue
		}
		if callID, ok := itemMap["call_id"].(string); ok && strings.TrimSpace(callID) != "" {
			return true
		}
	}
	return false
}

func legacyFunctionCallOutputCallIDs(reqBody map[string]any) []string {
	if reqBody == nil {
		return nil
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return nil
	}
	ids := make(map[string]struct{})
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "function_call_output" {
			continue
		}
		if callID, ok := itemMap["call_id"].(string); ok && strings.TrimSpace(callID) != "" {
			ids[callID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	callIDs := make([]string, 0, len(ids))
	for id := range ids {
		callIDs = append(callIDs, id)
	}
	return callIDs
}

func legacyHasFunctionCallOutputMissingCallID(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "function_call_output" {
			continue
		}
		callID, _ := itemMap["call_id"].(string)
		if strings.TrimSpace(callID) == "" {
			return true
		}
	}
	return false
}

func legacyHasItemReferenceForCallIDs(reqBody map[string]any, callIDs []string) bool {
	if reqBody == nil || len(callIDs) == 0 {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	referenceIDs := make(map[string]struct{})
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "item_reference" {
			continue
		}
		idValue, _ := itemMap["id"].(string)
		idValue = strings.TrimSpace(idValue)
		if idValue == "" {
			continue
		}
		referenceIDs[idValue] = struct{}{}
	}
	if len(referenceIDs) == 0 {
		return false
	}
	for _, callID := range callIDs {
		if _, ok := referenceIDs[callID]; !ok {
			return false
		}
	}
	return true
}

func legacyParseWSIngressPayload(raw []byte) (eventType, model, promptCacheKey, previousResponseID string, payload map[string]any, err error) {
	values := gjson.GetManyBytes(raw, "type", "model", "prompt_cache_key", "previous_response_id")
	eventType = strings.TrimSpace(values[0].String())
	if eventType == "" {
		eventType = "response.create"
	}
	model = strings.TrimSpace(values[1].String())
	promptCacheKey = strings.TrimSpace(values[2].String())
	previousResponseID = strings.TrimSpace(values[3].String())
	payload = make(map[string]any)
	if err = json.Unmarshal(raw, &payload); err != nil {
		return "", "", "", "", nil, err
	}
	if _, exists := payload["type"]; !exists {
		payload["type"] = "response.create"
	}
	return eventType, model, promptCacheKey, previousResponseID, payload, nil
}

func optimizedParseWSIngressPayload(raw []byte) (eventType, model, promptCacheKey, previousResponseID string, payload map[string]any, err error) {
	payload = make(map[string]any)
	if err = json.Unmarshal(raw, &payload); err != nil {
		return "", "", "", "", nil, err
	}
	eventType = openAIWSPayloadString(payload, "type")
	if eventType == "" {
		eventType = "response.create"
		payload["type"] = eventType
	}
	model = openAIWSPayloadString(payload, "model")
	promptCacheKey = openAIWSPayloadString(payload, "prompt_cache_key")
	previousResponseID = openAIWSPayloadString(payload, "previous_response_id")
	return eventType, model, promptCacheKey, previousResponseID, payload, nil
}

func legacyExtractOpenAIUsageFromJSONBytes(body []byte) (OpenAIUsage, bool) {
	var response struct {
		Usage struct {
			InputTokens       int `json:"input_tokens"`
			OutputTokens      int `json:"output_tokens"`
			InputTokenDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return OpenAIUsage{}, false
	}
	return OpenAIUsage{
		InputTokens:          response.Usage.InputTokens,
		OutputTokens:         response.Usage.OutputTokens,
		CacheReadInputTokens: response.Usage.InputTokenDetails.CachedTokens,
	}, true
}

// BenchmarkNativeOpenAIResponsesPreparation covers request-view construction,
// decoding, protocol/history validation, account preparation and final encoding.
// HTTP/context setup is intentionally included and fresh on every iteration;
// this is not a final-encoder-only benchmark or a measurement of network TTFT.
func BenchmarkNativeOpenAIResponsesPreparation(b *testing.B) {
	gin.SetMode(gin.TestMode)
	message := func(role, text string) any {
		kind := "input_text"
		if role == "assistant" {
			kind = "output_text"
		}
		return map[string]any{"type": "message", "role": role, "content": []any{map[string]any{"type": kind, "text": text}}}
	}
	history := make([]any, 0, 128)
	for i := 0; i < 64; i++ {
		history = append(history,
			message("user", strings.Repeat("Synthetic user context. ", 128)),
			message("assistant", strings.Repeat("Synthetic answer context. ", 128)))
	}
	tools := []any{map[string]any{
		"type": "function", "name": "lookup_fixture", "description": "Synthetic local benchmark tool",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []any{"query"}, "additionalProperties": false},
		"strict":     true,
	}}
	reasoningTools := make([]any, 0, 129)
	for i := 0; i < 32; i++ {
		callID := "fc_fixture_" + strconv.Itoa(i)
		reasoningTools = append(reasoningTools,
			message("user", "Look up synthetic fixture "+strconv.Itoa(i)),
			map[string]any{"type": "reasoning", "encrypted_content": strings.Repeat("opaque_fixture", 128), "summary": []any{}},
			map[string]any{"type": "function_call", "call_id": callID, "name": "lookup_fixture", "arguments": `{"query":"fixture"}`},
			map[string]any{"type": "function_call_output", "call_id": callID, "output": strings.Repeat("Synthetic tool result. ", 128)})
	}
	reasoningTools = append(reasoningTools, message("user", "Summarize the fixture results."))
	imageInput := []any{map[string]any{"type": "message", "role": "user", "content": []any{
		map[string]any{"type": "input_text", "text": "Describe this synthetic image fixture."},
		map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + strings.Repeat("A", 4<<20)},
	}}}
	for _, fixture := range []struct {
		name  string
		input []any
		tools []any
	}{
		{"long_history", history, nil},
		{"reasoning_tools", reasoningTools, tools},
		{"large_image", imageInput, nil},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			bodyMap := map[string]any{
				"model": "gpt-5.4", "instructions": "Use only synthetic benchmark context.",
				"store": false, "stream": true, "reasoning": map[string]any{"effort": "medium"},
				"tool_choice": "auto", "parallel_tool_calls": len(fixture.tools) > 0,
				"include": []any{"reasoning.encrypted_content"}, "input": fixture.input,
			}
			if fixture.tools != nil {
				bodyMap["tools"] = fixture.tools
			}
			body, err := json.Marshal(bodyMap)
			if err != nil {
				b.Fatal(err)
			}
			account := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{codexFingerprintModeExtraKey: "off"}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			prepare := func() (*openAIPreparedResponses, error) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				return svc.prepareNativeOpenAIResponses(c.Request.Context(), c, account, openAIResponsesPreparation{
					body: body, canonicalBody: body, view: newOpenAIRequestView(body), codexClient: true,
				})
			}
			prepared, err := prepare()
			if err != nil {
				b.Fatal(err)
			}
			if !bytes.Equal(prepared.body, body) {
				b.Fatal("normalized fixture unexpectedly changed")
			}
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				prepared, err := prepare()
				if err != nil {
					b.Fatal(err)
				}
				runtime.KeepAlive(prepared)
			}
		})
	}
}

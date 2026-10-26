package basispoints

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A bare argument object (unnamed JSON) describes function arguments. A
// correction may re-frame it as a named function envelope with the same
// values, but never redirect it into CUSTOM input or a raw code transport.
func TestToolRepairBareArgumentObjectCannotBecomeCustomInput(t *testing.T) {
	// Every key is a declared property of run_script: this is an argument object.
	raw := `{"code":"print(1)","description":"Run","timeoutMs":5}`
	for _, kind := range []string{"custom"} {
		t.Run(kind, func(t *testing.T) {
			source := testSource()
			source["tools"] = []any{functionCodeTestTool("run_script"), object{"type": "custom", "name": "exec"}}
			cache := new(ReplayCache)
			_, bridge := mustPrepare(t, source, "repair-scope", cache)
			original := repairResponse("initial", 3, 1, repairCall("bad", "Run", raw))
			corrected := repairCall("fixed", customTransportPrefix+"exec", raw)
			body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": original}))), func(context.Context, object, error) (object, error) {
				return repairResponse("fixed", 2, 1, corrected), nil
			})
			events := repairEvents(t, body)
			require.Equal(t, "response.failed", events[len(events)-1]["type"])
			for _, event := range events {
				require.False(t, isToolEvent(text(event["type"])), "no tool may be dispatched: %v", event)
			}
			failure := ObservedStreamFailure(body)
			require.NotNil(t, failure)
			require.Contains(t, string(failure.Diagnostic), "original_payload_unverifiable")
			require.NotContains(t, string(failure.Diagnostic), "print(1)")
			require.Nil(t, cache.get("repair-scope", text(corrected["call_id"])))
		})
	}
	t.Run("same arguments in a named envelope still pass", func(t *testing.T) {
		source := testSource()
		source["tools"] = []any{functionCodeTestTool("run_script"), object{"type": "custom", "name": "exec"}}
		cache := new(ReplayCache)
		_, bridge := mustPrepare(t, source, "repair-scope", cache)
		original := repairResponse("initial", 3, 1, repairCall("bad", "Run", raw))
		// FUNCTION_CODE re-frames the same values: code becomes the raw source and
		// the remaining arguments the metadata JSON.
		corrected := functionCodeTestNative(t, "run_script", "print(1)", `{"description":"Run","timeoutMs":5}`)
		body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": original}))), func(context.Context, object, error) (object, error) {
			return repairResponse("fixed", 2, 1, corrected), nil
		})
		events := repairEvents(t, body)
		if last := events[len(events)-1]; last["type"] != "response.completed" {
			t.Fatalf("expected completion, got %s: %s", last["type"], mustJSON(t, last))
		}
		dispatched := false
		for _, event := range events {
			if isToolEvent(text(event["type"])) {
				dispatched = true
			}
		}
		require.True(t, dispatched, "an argument-preserving envelope correction is dispatched")
	})
}

// Client history may carry a tool result before its call. The result restores
// the native call from the replay cache; the later explicit call must not emit
// a second native call with the same call_id.
func TestHistoryResultBeforeCallDoesNotDuplicateNativeCall(t *testing.T) {
	call := object{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "shell", "arguments": `{"cmd":"ls"}`}
	output := object{"type": "function_call_output", "call_id": "call_1", "output": "file.txt"}
	tool := object{"type": "function", "name": "shell", "parameters": object{"type": "object", "properties": object{"cmd": object{"type": "string"}}}}
	cache := new(ReplayCache)
	first := testSource()
	first["tools"] = []any{tool}
	first["input"] = []any{message("user", "list"), call, output}
	mustPrepare(t, first, "scope", cache)

	second := testSource()
	second["tools"] = []any{tool}
	second["input"] = []any{message("user", "list"), output, call, message("user", "again")}
	body, _ := mustPrepare(t, second, "scope", cache)
	items := mustTestValue[[]any](t, body["input"])
	calls, outputs := 0, 0
	for _, raw := range items {
		item, _ := raw.(object)
		switch {
		case text(item["type"]) == "function_call" && text(item["call_id"]) == "call_1":
			calls++
		case text(item["type"]) == "function_call_output" && text(item["call_id"]) == "call_1":
			outputs++
		}
	}
	require.Equal(t, 1, calls, "one native call for call_1: %s", mustJSON(t, items))
	require.Equal(t, 1, outputs)
	// The restored call still precedes its result.
	callIndex, outputIndex := -1, -1
	for i, raw := range items {
		item, _ := raw.(object)
		if text(item["call_id"]) == "call_1" {
			if text(item["type"]) == "function_call" {
				callIndex = i
			} else {
				outputIndex = i
			}
		}
	}
	require.Less(t, callIndex, outputIndex)
}

func TestPrepareWarnsAboutDroppedRequestFields(t *testing.T) {
	source := testSource()
	source["max_output_tokens"] = 4096
	source["truncation"] = "auto"
	source["include"] = []any{"reasoning.encrypted_content"}
	source["reasoning"] = object{"effort": "medium", "summary": "auto"}
	_, bridge := mustPrepare(t, source, "scope", new(ReplayCache))
	joined := strings.Join(bridge.Warnings, "\n")
	for _, field := range []string{"max_output_tokens", "truncation", "include", "reasoning.summary"} {
		require.Contains(t, joined, "not forwarded to Basispoints: "+field)
	}
	plain := testSource()
	_, bridge = mustPrepare(t, plain, "scope", new(ReplayCache))
	require.NotContains(t, strings.Join(bridge.Warnings, "\n"), "not forwarded")
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

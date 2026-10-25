package basispoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func repairCall(id, summary, code string) object {
	args, _ := json.Marshal(object{"summary": summary, "code": code, "extended_summary": "{}", "destructive": false, "references": []any{}})
	return object{"type": "function_call", "name": "run_officejs", "id": "fc_" + id, "call_id": id, "arguments": string(args), "status": "completed"}
}

func repairResponse(id string, input, output int, items ...any) object {
	return object{"id": id, "model": "gpt-5.6-sol", "status": "completed", "output": items, "usage": object{
		"input_tokens": input, "output_tokens": output, "total_tokens": input + output, "input_tokens_details": object{"cached_tokens": 2}, "output_tokens_details": object{"reasoning_tokens": 1},
	}}
}

func repairBridge(t *testing.T, cache *ReplayCache) (object, *Bridge) {
	t.Helper()
	source := testSource()
	source["tools"] = []any{object{"type": "custom", "name": "functions.exec"}, object{"type": "function", "name": "shell"}}
	return mustPrepare(t, source, "repair-scope", cache)
}

func repairEvents(t *testing.T, body io.ReadCloser) []object {
	t.Helper()
	defer func() { _ = body.Close() }()
	var events []object
	err := readEvents(body, func(_ string, raw []byte) error {
		var event object
		require.NoError(t, decode(raw, &event))
		events = append(events, event)
		return nil
	})
	require.NoError(t, err)
	for i, event := range events {
		require.Equal(t, json.Number(fmt.Sprint(i)), event["sequence_number"])
	}
	return events
}

func repairValue[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	require.True(t, ok, "unexpected decoded value type: %T", value)
	return result
}

type repairTrackedBody struct {
	io.Reader
	closed atomic.Int32
}

func (r *repairTrackedBody) Close() error { r.closed.Add(1); return nil }

func TestToolRepairKeepsOneResponseAndAtomicToolBatch(t *testing.T) {
	cache := new(ReplayCache)
	source := testSource()
	source["stream"] = true
	source["tools"] = []any{object{"type": "custom", "name": "functions.exec"}, object{"type": "function", "name": "shell"}}
	_, bridge := mustPrepare(t, source, "repair-scope", cache)
	code := "const value = 'literal';\ntext(value);"
	valid := nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}})
	invalid := repairCall("bad", "Run client tool", code)
	message := object{"type": "message", "id": "msg_original", "role": "assistant", "content": []any{object{"type": "output_text", "text": "Working."}}}
	original := repairResponse("resp_original", 10, 3, message, valid, invalid)
	wire := sse(object{"type": "response.created", "response": object{"id": "resp_original", "output": []any{}}}) + sse(object{"type": "response.output_text.delta", "delta": "Working."}) + sse(object{"type": "response.completed", "response": original})
	upstream := &repairTrackedBody{Reader: strings.NewReader(wire)}
	repairedValid := nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}})
	repairedValid["call_id"], repairedValid["id"] = "fixed_shell", "fc_fixed_shell"
	repairedCustom := repairCall("fixed_exec", "codex2api.custom/functions.exec", code)
	calls := 0
	stream := bridge.StreamWithToolRepair(context.Background(), upstream, func(ctx context.Context, failed object, validation error) (object, error) {
		calls++
		if upstream.closed.Load() != 1 || cache.get("repair-scope", "call_native") != nil {
			return nil, errors.New("upstream lease or partial cache retained before correction")
		}
		if validation == nil || text(failed["id"]) != "resp_original" || ctx.Err() != nil {
			return nil, errors.New("incorrect correction context")
		}
		return repairResponse("resp_hidden", 20, 4, object{"type": "message", "content": "Hidden commentary"}, repairedValid, repairedCustom), nil
	})
	events := repairEvents(t, stream)
	require.Equal(t, 1, calls)
	counts := make(map[string]int)
	var indexes []json.Number
	for _, event := range events {
		kind := text(event["type"])
		counts[kind]++
		if kind == "response.output_item.added" {
			indexes = append(indexes, repairValue[json.Number](t, event["output_index"]))
		}
	}
	require.Equal(t, []json.Number{"1", "2"}, indexes)
	require.Equal(t, 1, counts["response.created"])
	require.Equal(t, 1, counts["response.completed"])
	require.Equal(t, 1, counts["response.output_text.delta"])
	require.Zero(t, counts["response.failed"])
	last := repairValue[object](t, events[len(events)-1]["response"])
	require.Equal(t, "resp_original", last["id"])
	output := repairValue[[]any](t, last["output"])
	require.Equal(t, "msg_original", repairValue[object](t, output[0])["id"])
	require.Equal(t, code, repairValue[object](t, output[2])["input"])
	usage := repairValue[object](t, last["usage"])
	require.Equal(t, json.Number("30"), usage["input_tokens"])
	require.Equal(t, json.Number("7"), usage["output_tokens"])
	require.Equal(t, json.Number("37"), usage["total_tokens"])
	require.Equal(t, json.Number("4"), repairValue[object](t, usage["input_tokens_details"])["cached_tokens"])
	require.True(t, reflect.DeepEqual(repairedCustom, cache.get("repair-scope", "fixed_exec")))
	require.Nil(t, cache.get("repair-scope", "bad"))
	require.EqualValues(t, 1, upstream.closed.Load())
}

func TestToolRepairBoundsAttemptsAndPreservesFailureUsage(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			_, bridge := repairBridge(t, nil)
			initial := repairResponse("resp_initial", 10, 2, repairCall("bad", "Run", "text(1)"))
			calls := 0
			stream := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(_ context.Context, failed object, _ error) (object, error) {
				calls++
				if calls == 2 && failed["id"] != "resp_1" {
					return nil, errors.New("did not continue latest failed response")
				}
				summary := "Run"
				if success && calls == 2 {
					summary = "codex2api.custom/functions.exec"
				}
				return repairResponse(fmt.Sprintf("resp_%d", calls), 20, 3, repairCall(fmt.Sprintf("retry_%d", calls), summary, fmt.Sprintf("text(%d)", calls+1))), nil
			})
			events := repairEvents(t, stream)
			require.Equal(t, 2, calls)
			last := events[len(events)-1]
			response := repairValue[object](t, last["response"])
			if success {
				require.Equal(t, "response.completed", last["type"])
				output := repairValue[[]any](t, response["output"])
				require.Equal(t, "text(1)", repairValue[object](t, output[0])["input"], "retain the initial source across both corrections")
			} else {
				require.Equal(t, "response.failed", last["type"])
				require.Len(t, events, 1)
				require.Contains(t, repairValue[object](t, response["error"])["message"], "after 2 corrections")
				require.Empty(t, response["output"])
			}
			usage := repairValue[object](t, response["usage"])
			require.Equal(t, json.Number("50"), usage["input_tokens"])
			require.Equal(t, json.Number("8"), usage["output_tokens"])
		})
	}
}

func TestToolRepairDoesNotRetryOtherFailures(t *testing.T) {
	for _, name := range []string{"unsupported_native", "undeclared_target", "missing_call_id", "invalid_outer_json", "incomplete", "failed", "missing_terminal_tool", "truncated", "structured"} {
		t.Run(name, func(t *testing.T) {
			_, bridge := repairBridge(t, nil)
			item := repairCall("bad", "Run", "text(1)")
			kind := "response.completed"
			prefix := ""
			switch name {
			case "unsupported_native":
				item["name"] = "unknown"
			case "undeclared_target":
				item = nativeCall(object{"name": "outside_catalog", "arguments": object{}})
			case "missing_call_id":
				delete(item, "call_id")
			case "invalid_outer_json":
				item["arguments"] = "invalid"
			case "incomplete", "failed":
				kind = "response." + name
			case "missing_terminal_tool":
				prefix = sse(object{"type": "response.output_item.done", "item": repairCall("missing", "Run", "text(1)")})
			case "structured":
				source := testSource()
				source["text"] = object{"format": object{"type": "json_object"}}
				_, bridge = mustPrepare(t, source, "", nil)
			}
			wire := prefix + sse(object{"type": kind, "response": repairResponse("resp", 10, 2, item)})
			if name == "truncated" {
				wire = sse(object{"type": "response.output_item.done", "item": item})
			}
			calls := 0
			body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(wire)), func(context.Context, object, error) (object, error) {
				calls++
				return nil, errors.New("unexpected repair")
			})
			out, err := io.ReadAll(body)
			_ = body.Close()
			if name == "truncated" {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, calls)
			require.NotContains(t, string(out), "response.custom_tool_call_input")
		})
	}
}

func TestToolRepairRestoresOriginalRawPayload(t *testing.T) {
	for _, tc := range []struct {
		name, code                                    string
		functionCode, functionCmd, envelope, explicit bool
	}{
		{name: "patch", code: "*** Begin Patch\n*** Update File: sample.tex\n@@\n-($x,y)\n+(x,y)\n*** End Patch"},
		{name: "javascript", code: "text(1)"},
		{name: "raw_json", code: "{\"payload\":1}"},
		{name: "fenced_json_custom", code: "```json\n{\"payload\":1}\n```", envelope: true},
		{name: "function_code", code: "const price = '$1';\ntext(price);", functionCode: true},
		{name: "ordinary_custom", code: "text('$1')", envelope: true},
		{name: "ordinary_function_code", code: "text('$1')", functionCode: true, envelope: true},
		{name: "ordinary_function_cmd", code: "printf '%s\\n' \"$value\"", functionCmd: true, envelope: true},
		{name: "explicit_raw_JSON_with_business_name", code: "{\"name\":\"business-label\",\"arguments\":{\"value\":1}}", functionCode: true, explicit: true},
		{name: "explicit_raw_malformed_JSON_source", code: `{"code":"print("text")"}`, functionCode: true, explicit: true},
		{name: "explicit_fenced_JSON_source", code: "```json\n{\"code\":\"business-text\"}\n```", functionCode: true, explicit: true, envelope: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := tc.code
			cache := new(ReplayCache)
			source := testSource()
			definition := object{"type": "custom", "name": "apply_patch"}
			marker := customTransportPrefix + "apply_patch"
			if tc.functionCode {
				definition = object{"type": "function", "name": "run_script", "parameters": object{"type": "object", "properties": object{"code": object{"type": "string"}, "timeout_ms": object{"type": "integer"}}}}
				marker = functionCodeTransportPrefix + "run_script"
			}
			if tc.functionCmd {
				definition = functionCmdTestTool("exec_command")
				marker = functionCmdTransportPrefix + "exec_command"
			}
			source["tools"] = []any{definition}
			_, bridge := mustPrepare(t, source, "repair-scope", cache)
			initial := repairResponse("resp_original", 10, 2, repairCall("bad", "Run", code))
			corrected := repairCall("fixed", marker, strings.ReplaceAll(code, "$", "")+"\n")
			if tc.functionCode {
				args := transportArguments(corrected)
				args["extended_summary"] = "{\"timeout_ms\":1500}"
				encoded, err := json.Marshal(args)
				require.NoError(t, err)
				corrected["arguments"] = string(encoded)
			}
			if tc.explicit {
				original := repairCall("bad", marker, code)
				args := transportArguments(original)
				args["extended_summary"] = quoted(object{"code": code, "timeout_ms": 1500})
				original["arguments"] = quoted(args)
				initial["output"] = []any{original}
			}
			if tc.envelope {
				target := text(definition["name"])
				wrapped := object{"name": target, "input": "changed source"}
				if tc.functionCode {
					wrapped = object{"name": target, "arguments": object{"code": "changed source", "timeout_ms": 1500}}
				}
				if tc.functionCmd {
					wrapped = object{"name": target, "arguments": object{"cmd": "changed source", "description": "Run command"}}
				}
				corrected = repairCall("fixed", "Run", quoted(wrapped))
			}
			modelArguments := corrected["arguments"]
			body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
				return repairResponse("resp_fixed", 20, 3, corrected), nil
			})
			events := repairEvents(t, body)
			last := events[len(events)-1]
			require.Equal(t, "response.completed", last["type"])
			response := repairValue[object](t, last["response"])
			output := repairValue[[]any](t, response["output"])
			require.Len(t, output, 1)
			call := repairValue[object](t, output[0])
			if tc.functionCode || tc.functionCmd {
				var args object
				require.NoError(t, decode([]byte(text(call["arguments"])), &args))
				if tc.functionCmd {
					require.Equal(t, code, args["cmd"])
				} else {
					require.Equal(t, code, args["code"])
					require.Equal(t, json.Number("1500"), args["timeout_ms"])
				}
			} else {
				require.Equal(t, code, call["input"])
			}
			require.Equal(t, "fixed", call["call_id"])
			check := *bridge
			check.replay = nil
			replayed, err := check.translateCall(cache.get("repair-scope", "fixed"))
			require.NoError(t, err)
			require.Equal(t, historyCallFingerprint(call), historyCallFingerprint(replayed))
			require.Equal(t, modelArguments, corrected["arguments"], "leave the model response untouched")
			require.Nil(t, cache.get("repair-scope", "bad"))
			source["input"] = []any{call, object{"type": text(call["type"]) + "_output", "call_id": "fixed", "output": "done"}}
			prepared, _ := mustPrepare(t, source, "repair-scope", cache)
			found := false
			for _, raw := range repairValue[[]any](t, prepared["input"]) {
				item := repairValue[object](t, raw)
				if text(item["call_id"]) == "fixed" && text(item["type"]) == "function_call" {
					found = true
					replayed, err := check.translateCall(item)
					require.NoError(t, err)
					require.Equal(t, historyCallFingerprint(call), historyCallFingerprint(replayed))
				}
			}
			require.True(t, found, "next request must replay the dispatched source text")
		})
	}
}

func TestToolRepairRejectsChangedBatchAndOperations(t *testing.T) {
	for _, change := range []string{"extra_tool", "text_only", "unrelated_function_field", "changed_target", "explicit_target", "oversized_raw_code"} {
		t.Run(change, func(t *testing.T) {
			cache := new(ReplayCache)
			_, bridge := repairBridge(t, cache)
			code := "text(1)"
			original := []any{repairCall("bad", "Run", code)}
			corrected := []any{repairCall("fixed", "codex2api.custom/functions.exec", code)}
			switch change {
			case "extra_tool":
				corrected = append(corrected, repairCall("extra", "codex2api.custom/functions.exec", code))
			case "text_only":
				corrected = nil
			case "unrelated_function_field":
				corrected = []any{nativeCall(object{"name": "shell", "arguments": object{"code": "text(2)"}})}
			case "changed_target":
				original = append(original, nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}}))
				corrected = append(corrected, repairCall("changed_target", customTransportPrefix+"functions.exec", "text(1)"))
			case "explicit_target":
				original = []any{nativeCall(object{"name": "shell", "arguments": 42})}
			case "oversized_raw_code":
				original = []any{repairCall("bad", "Run", strings.Repeat("x", maxEnvelopeBytes+1))}
			}
			calls := 0
			initial := repairResponse("resp", 10, 2, original...)
			body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
				calls++
				return repairResponse("resp_fixed", 20, 3, corrected...), nil
			})
			events := repairEvents(t, body)
			wantCalls := 1
			if change == "unrelated_function_field" {
				wantCalls = 2
			}
			require.Equal(t, wantCalls, calls)
			require.Len(t, events, 1)
			require.Equal(t, "response.failed", events[0]["type"])
			require.Nil(t, cache.get("repair-scope", "fixed"))
		})
	}
}

func TestToolRepairDoesNotRetryCallbackFailure(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	initial := repairResponse("resp", 10, 2, repairCall("bad", "Run", "text(1)"))
	calls := 0
	body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
		calls++
		return repairResponse("resp_failed", 20, 3), io.ErrUnexpectedEOF
	})
	events := repairEvents(t, body)
	require.Len(t, events, 1)
	require.Equal(t, 1, calls)
	require.Equal(t, "response.failed", events[0]["type"])
	response := repairValue[object](t, events[0]["response"])
	require.Equal(t, json.Number("30"), repairValue[object](t, response["usage"])["input_tokens"])
}

func TestToolRepairCloseCancelsInFlightCorrection(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	initial := repairResponse("resp_initial", 10, 2, repairCall("bad", "Run", "text(1)"))
	started, canceled := make(chan struct{}), make(chan struct{})
	body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(ctx context.Context, _ object, _ error) (object, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("correction did not start")
	}
	require.NoError(t, body.Close())
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("correction was not canceled")
	}
}

func TestToolRepairBuilderPreservesNativeHistoryAndSettings(t *testing.T) {
	prepared, _ := repairBridge(t, nil)
	encoded, err := json.Marshal(prepared)
	require.NoError(t, err)
	initialLen := len(repairValue[[]any](t, prepared["input"]))
	for attempt := 0; attempt < 2; attempt++ {
		failed := repairResponse("resp", 10, 2, repairCall(fmt.Sprint(attempt), "Run", "text(1)"))
		encoded, err = BuildToolRepairRequest(encoded, failed, errors.New("missing CUSTOM marker"))
		require.NoError(t, err)
		var request object
		require.NoError(t, decode(encoded, &request))
		for _, field := range []string{"model", "reasoning_effort", "model_selection", "prompt_cache_key", "context_management"} {
			require.Equal(t, prepared[field], request[field])
		}
		require.NotContains(t, request, "tools")
		require.NotContains(t, request, "previous_response_id")
		metadata := repairValue[object](t, request["metadata"])
		require.Equal(t, repairValue[object](t, prepared["metadata"])["task_id"], metadata["task_id"])
		require.Equal(t, repairValue[object](t, prepared["metadata"])["turn_id"], metadata["turn_id"])
		require.Equal(t, fmt.Sprint(attempt+2), metadata["agent_iteration"])
		input := repairValue[[]any](t, request["input"])
		require.Len(t, input, initialLen+(attempt+1)*3)
		require.Equal(t, prepared["input"], input[:initialLen])
		result := repairValue[object](t, input[len(input)-2])
		require.Equal(t, fmt.Sprint(attempt), result["call_id"])
		var feedback object
		require.NoError(t, decode([]byte(text(result["output"])), &feedback))
		require.Equal(t, false, feedback["executed"])
		require.Equal(t, "invalid_client_tool_transport", repairValue[object](t, feedback["error"])["code"])
	}
}

func TestToolRepairReaderRejectsIncompleteOrMissingTools(t *testing.T) {
	item := repairCall("good", "codex2api.custom/functions.exec", "text(1)")
	for _, kind := range []string{"response.completed", "response.failed", "response.incomplete", "missing_tool", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			response := repairResponse("resp", 10, 2, item)
			event := kind
			if kind == "missing_tool" {
				response["output"], event = []any{}, "response.completed"
			}
			wire := sse(object{"type": "response.output_item.done", "item": item})
			if kind != "truncated" {
				wire += sse(object{"type": event, "response": response})
			}
			var observed []string
			actual, err := ReadToolRepairResponse(strings.NewReader(wire), func(raw []byte) {
				var upstream object
				_ = decode(raw, &upstream)
				observed = append(observed, text(upstream["type"]))
			})
			wantObserved := []string{"response.output_item.done"}
			if kind != "truncated" {
				wantObserved = append(wantObserved, event)
			}
			require.Equal(t, wantObserved, observed, "correction observations precede response validation")
			if kind == "response.completed" {
				require.NoError(t, err)
				require.Equal(t, "resp", actual["id"])
			} else {
				require.Error(t, err)
			}
			if kind != "truncated" {
				require.Equal(t, json.Number("10"), repairValue[object](t, actual["usage"])["input_tokens"])
			}
		})
	}
}

func TestToolRepairBindsOriginalRawCommand(t *testing.T) {
	source := testSource()
	source["tools"] = []any{functionCmdTestTool("exec_command")}
	cache := new(ReplayCache)
	_, bridge := mustPrepare(t, source, "cmd-repair", cache)
	original := "printf '%s\\n' \"literal $value\"\n"
	initial := repairResponse("initial", 3, 1, repairCall("bad", "Run command", original))
	attempts := 0
	body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
		attempts++
		fixed := functionCmdTestNative(t, "exec_command", "rewritten command", "{\"description\":\"Run command\"}")
		return repairResponse("fixed", 2, 1, fixed), nil
	})
	events := repairEvents(t, body)
	require.Equal(t, 1, attempts)
	var final object
	for _, event := range events {
		if event["type"] == "response.completed" {
			final = repairValue[object](t, event["response"])
		}
	}
	require.NotNil(t, final)
	output := repairValue[[]any](t, final["output"])
	call := repairValue[object](t, output[0])
	require.Equal(t, original, functionCmdTestArguments(t, call)["cmd"])
	cached := cache.get("cmd-repair", "call_code")
	require.Equal(t, original, transportArguments(cached)["code"])
}

func TestToolRepairPreservesAlreadyValidOperations(t *testing.T) {
	for _, kind := range []string{"function", "custom", "prefixed_function"} {
		t.Run(kind, func(t *testing.T) {
			cache := new(ReplayCache)
			_, bridge := repairBridge(t, cache)
			valid := nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd", "workdir": "/original"}})
			changed := nativeCall(object{"name": "shell", "arguments": object{"cmd": "rm changed", "workdir": "/changed"}})
			if kind == "prefixed_function" {
				valid = nativeCall(object{"name": "functions.shell", "arguments": object{"cmd": "pwd", "workdir": "/original"}})
			}
			if kind == "custom" {
				valid = repairCall("valid", customTransportPrefix+"functions.exec", "text('original')")
				changed = repairCall("changed", customTransportPrefix+"functions.exec", "text('changed')")
			}
			changed["id"], changed["call_id"] = "fc_changed", "changed"
			initial := repairResponse("resp_original", 10, 2, valid, repairCall("bad", "Run", "text(42)"))
			corrected := repairResponse("resp_correction", 20, 3, changed, repairCall("fixed", customTransportPrefix+"functions.exec", "text(42)"))
			before, err := json.Marshal(corrected)
			require.NoError(t, err)
			calls := 0
			body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
				calls++
				return corrected, nil
			})
			events := repairEvents(t, body)
			last := events[len(events)-1]
			require.Equal(t, "response.completed", last["type"])
			require.Equal(t, 1, calls)
			response := repairValue[object](t, last["response"])
			require.Equal(t, "resp_original", response["id"])
			output := repairValue[[]any](t, response["output"])
			require.Len(t, output, 2)
			first := repairValue[object](t, output[0])
			if kind == "custom" {
				require.Equal(t, "text('original')", first["input"])
			} else {
				require.JSONEq(t, "{\"cmd\":\"pwd\",\"workdir\":\"/original\"}", text(first["arguments"]))
			}
			cached := cache.get("repair-scope", "changed")
			require.NotNil(t, cached)
			check := *bridge
			check.replay = nil
			replayed, err := check.translateCall(cached)
			require.NoError(t, err)
			require.Equal(t, historyCallFingerprint(first), historyCallFingerprint(replayed))
			after, err := json.Marshal(corrected)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "keep native continuation history immutable")
			require.Equal(t, json.Number("30"), repairValue[object](t, response["usage"])["input_tokens"])
		})
	}
}

func TestToolRepairPreservesUnframedJSONArguments(t *testing.T) {
	for _, format := range []string{"plain", "fenced", "prose", "encoded", "missing_separator"} {
		for _, transport := range []string{"function", "code_envelope", "code_raw", "cmd_raw"} {
			for _, changed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/changed=%t", format, transport, changed), func(t *testing.T) {
					cache := new(ReplayCache)
					_, bridge := repairBridge(t, cache)
					name, field := "shell", "cmd"
					if transport != "function" {
						name, field = "run_script", "code"
						if transport == "cmd_raw" {
							name, field = "exec_command", "cmd"
						}
						source := testSource()
						source["tools"] = []any{object{"type": "function", "name": name, "parameters": object{"type": "object", "properties": object{field: object{"type": "string"}, "workdir": object{"type": "string"}, "limit": object{"type": "integer"}}}}}
						_, bridge = mustPrepare(t, source, "repair-scope", cache)
					}
					original := object{field: "pwd", "workdir": "/original", "limit": json.Number("9007199254740993")}
					raw := quoted(original)
					switch format {
					case "fenced":
						raw = "```json\n" + raw + "\n```"
					case "prose":
						raw = "Tool arguments: " + raw
					case "encoded":
						raw = quoted(raw)
					case "missing_separator":
						raw = strings.Replace(raw, `,"workdir":`, ` "workdir":`, 1)
					}
					initial := repairResponse("initial", 3, 1, repairCall("bad", "Run command", raw))
					args := object{field: "pwd", "workdir": "/original", "limit": json.Number("9007199254740993")}
					if changed {
						args["workdir"] = "/changed"
					}
					corrected := nativeCall(object{"name": name, "arguments": args})
					if strings.HasSuffix(transport, "_raw") {
						prefix := functionCodeTransportPrefix
						if field == "cmd" {
							prefix = functionCmdTransportPrefix
						}
						corrected = repairCall("fixed", prefix+name, text(args[field]))
						metadata := object{"workdir": args["workdir"], "limit": args["limit"]}
						outer := transportArguments(corrected)
						outer["extended_summary"] = quoted(metadata)
						corrected["arguments"] = quoted(outer)
					}
					modelArguments := corrected["arguments"]
					body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
						return repairResponse("fixed", 2, 1, corrected), nil
					})
					events := repairEvents(t, body)
					last := events[len(events)-1]
					if changed {
						require.Equal(t, "response.failed", last["type"])
						failure := ObservedStreamFailure(body)
						require.NotNil(t, failure)
						require.Contains(t, string(failure.Diagnostic), "arguments_changed")
						var diagnostic, detail object
						require.NoError(t, decode(failure.Diagnostic, &diagnostic))
						require.NoError(t, decode([]byte(text(repairValue[object](t, diagnostic["error"])["param"])), &detail))
						require.Equal(t, transportShape(raw), detail["original_code_shape"])
						require.Equal(t, "function_call", detail["corrected_tool_kind"])
						require.NotContains(t, string(failure.Diagnostic), "/changed")
						require.NotContains(t, string(failure.Diagnostic), "9007199254740993")
						require.NotContains(t, quoted(events), "gateway_tool_transport")
						for _, event := range events {
							require.False(t, isToolEvent(text(event["type"])))
						}
						require.Nil(t, cache.get("repair-scope", text(corrected["call_id"])))
					} else {
						require.Equal(t, "response.completed", last["type"])
						response := repairValue[object](t, last["response"])
						output := repairValue[[]any](t, response["output"])
						require.Len(t, output, 1)
						call := repairValue[object](t, output[0])
						var actual object
						require.NoError(t, decode([]byte(text(call["arguments"])), &actual))
						require.Equal(t, original, actual, "retain exact values, including large integers; never execute the wrapper as source")
						check := *bridge
						check.replay = nil
						replayed, err := check.translateCall(cache.get("repair-scope", text(call["call_id"])))
						require.NoError(t, err)
						require.Equal(t, historyCallFingerprint(call), historyCallFingerprint(replayed))
					}
					require.Equal(t, modelArguments, corrected["arguments"], "leave model response unchanged")
				})
			}
		}
	}
}

func TestToolRepairCannotChangeMalformedEnvelopeOperation(t *testing.T) {
	for _, tc := range []struct{ custom, conflicting, framed bool }{
		{false, false, false}, {false, true, false}, {true, false, false}, {true, true, false},
		{false, false, true}, {false, true, true}, {true, false, true}, {true, true, true},
	} {
		custom, conflicting := tc.custom, tc.conflicting
		t.Run(fmt.Sprintf("custom=%t/conflicting=%t/framed=%t", custom, conflicting, tc.framed), func(t *testing.T) {
			_, bridge := repairBridge(t, nil)
			envelope := object{"name": "shell", "arguments": 42}
			replacement := object{"cmd": "pwd"}
			if conflicting {
				envelope["tool"] = "functions.exec"
				// Reinterpreting the entire bad envelope as function arguments also fails.
				replacement = envelope
			}
			original := nativeCall(envelope)
			if tc.framed {
				args := transportArguments(original)
				args["code"] = "```json\n" + text(args["code"]) + "\n```"
				original["arguments"] = quoted(args)
			}
			initial := repairResponse("initial", 3, 1, original)
			body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
				corrected := nativeCall(object{"name": "shell", "arguments": replacement})
				if custom {
					corrected = repairCall("fixed", customTransportPrefix+"functions.exec", quoted(envelope))
				}
				return repairResponse("fixed", 2, 1, corrected), nil
			})
			events := repairEvents(t, body)
			require.Equal(t, "response.failed", events[len(events)-1]["type"])
		})
	}
}

func TestToolRepairCannotExecuteUndecodableJSON(t *testing.T) {
	for _, raw := range []string{
		`{"code":"text("private-value")","workdir":"/original"}`,
		`{"code":"text(1)" "workdir":"/original","workdir":"/other"}`,
		"```json\n{\"code\":\"unfinished\"\n```",
		`Tool request: {"name":"run_script","arguments":`,
	} {
		for _, kind := range []string{"custom", "function_code", "function"} {
			t.Run(kind+"/"+transportShape(raw), func(t *testing.T) {
				source := testSource()
				source["tools"] = []any{functionCodeTestTool("run_script"), object{"type": "custom", "name": "exec"}}
				cache := new(ReplayCache)
				_, bridge := mustPrepare(t, source, "repair-scope", cache)
				original := repairResponse("initial", 3, 1, repairCall("bad", "Run", raw))
				corrected := functionCodeTestNative(t, "run_script", "text(1)", `{"description":"Run"}`)
				if kind == "custom" {
					corrected = repairCall("fixed", customTransportPrefix+"exec", raw)
				} else if kind == "function" {
					corrected = nativeCall(object{"name": "run_script", "arguments": object{"code": "text(1)", "description": "Run"}})
				}
				body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": original}))), func(context.Context, object, error) (object, error) {
					return repairResponse("fixed", 2, 1, corrected), nil
				})
				events := repairEvents(t, body)
				require.Equal(t, "response.failed", events[len(events)-1]["type"])
				for _, event := range events {
					require.False(t, isToolEvent(text(event["type"])))
				}
				failure := ObservedStreamFailure(body)
				require.NotNil(t, failure)
				require.Contains(t, string(failure.Diagnostic), "original_payload_unverifiable")
				require.Contains(t, failure.Message, "could not be verified")
				require.NotContains(t, failure.Message, "changed an operation")
				require.NotContains(t, string(failure.Diagnostic), "private-value")
				require.Nil(t, cache.get("repair-scope", text(corrected["call_id"])))
			})
		}
	}
}

func TestToolRepairMissingSeparatorPreservesOperationWithoutContinuation(t *testing.T) {
	// Synthetic payload with the reported length and parser offset; no production
	// arguments were provided. A quote-escaping failure can have the same shape.
	const prefix = `{"name":"shell","arguments":{"cmd":"`
	const tail = `"workdir":"`
	cmd := strings.Repeat("x", 3060-len(prefix)-2)
	dir := strings.Repeat("y", 3502-3060-len(tail)-3)
	raw := prefix + cmd + `" ` + tail + dir + `"}}`
	require.Equal(t, "format=json_object; bytes=3502; json_offset=3061; json_failure=missing_separator", transportShape(raw))
	cache := new(ReplayCache)
	_, bridge := repairBridge(t, cache)
	native := repairCall("original", "Run", raw)
	initial := repairResponse("initial", 3, 1, native)
	body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
		return nil, errors.New("unnecessary model correction")
	})
	events := repairEvents(t, body)
	last := events[len(events)-1]
	require.Equal(t, "response.completed", last["type"])
	response := repairValue[object](t, last["response"])
	call := repairValue[object](t, repairValue[[]any](t, response["output"])[0])
	require.JSONEq(t, quoted(object{"cmd": cmd, "workdir": dir}), text(call["arguments"]))
	require.Equal(t, json.Number("3"), repairValue[object](t, response["usage"])["input_tokens"])
	require.Equal(t, native, cache.get("repair-scope", "original"))
}

func TestToolRepairRawCommandCanGainOrdinaryEnvelope(t *testing.T) {
	for _, namespaced := range []bool{false, true} {
		t.Run(fmt.Sprint(namespaced), func(t *testing.T) {
			source := testSource()
			source["tools"] = []any{functionCmdTestTool("exec_command")}
			name := "exec_command"
			if namespaced {
				source["tools"] = []any{object{"type": "namespace", "name": "functions", "tools": source["tools"]}}
				name = "functions.exec_command"
			}
			_, bridge := mustPrepare(t, source, "cmd-envelope", nil)
			code := "printf '%s' \"literal $value\""
			initial := repairResponse("initial", 3, 1, repairCall("bad", "Run command", code))
			body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
				return repairResponse("fixed", 2, 1, nativeCall(object{"name": name, "arguments": object{"cmd": code, "description": "Run command"}})), nil
			})
			events := repairEvents(t, body)
			require.Equal(t, "response.completed", events[len(events)-1]["type"])
		})
	}
}

func TestToolRepairRejectsChangedExplicitMetadata(t *testing.T) {
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "run_script", "parameters": object{"type": "object", "properties": object{"code": object{"type": "string"}, "workdir": object{"type": "string"}}}}}
	_, bridge := mustPrepare(t, source, "metadata-repair", nil)
	original := repairCall("bad", functionCodeTransportPrefix+"run_script", "text(1)")
	args := transportArguments(original)
	// An identical duplicate is malformed in FUNCTION_CODE, but its original
	// operation and metadata are known and must not silently change in repair.
	args["extended_summary"] = quoted(object{"code": "text(1)", "workdir": "/original"})
	original["arguments"] = quoted(args)
	initial := repairResponse("initial", 3, 1, original)
	corrected := repairCall("fixed", functionCodeTransportPrefix+"run_script", "text(1)")
	args = transportArguments(corrected)
	args["extended_summary"] = quoted(object{"workdir": "/changed"})
	corrected["arguments"] = quoted(args)
	body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
		return repairResponse("fixed", 2, 1, corrected), nil
	})
	events := repairEvents(t, body)
	require.Equal(t, "response.failed", events[len(events)-1]["type"])
	failure := ObservedStreamFailure(body)
	require.NotNil(t, failure)
	require.Contains(t, string(failure.Diagnostic), "metadata_changed")
	require.NotContains(t, string(failure.Diagnostic), "/changed")
}

func TestToolRepairArgumentDriftUsesRemainingBudget(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			source := testSource()
			source["tools"] = []any{object{"type": "function", "name": "shell", "parameters": object{"type": "object", "properties": object{"cmd": object{"type": "string"}}}}}
			cache := new(ReplayCache)
			_, bridge := mustPrepare(t, source, "repair-scope", cache)
			original := repairResponse("initial", 3, 1, repairCall("bad", "Run", quoted(object{"cmd": "pwd"})))
			calls := 0
			stream := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": original}))), func(_ context.Context, failed object, validation error) (object, error) {
				calls++
				require.Nil(t, cache.get("repair-scope", "call_native"), "corrections must stay unexecuted")
				if calls == 2 {
					require.Equal(t, "corrected", text(failed["id"]))
					var mismatch *toolOperationMismatch
					require.ErrorAs(t, validation, &mismatch)
					require.Equal(t, "arguments_changed", mismatch.reason)
				}
				cmd := "changed operation"
				if recover && calls == 2 {
					cmd = "pwd"
				}
				return repairResponse("corrected", 2, 1, nativeCall(object{"name": "shell", "arguments": object{"cmd": cmd}})), nil
			})
			events := repairEvents(t, stream)
			require.Equal(t, 2, calls)
			last := events[len(events)-1]
			response := repairValue[object](t, last["response"])
			usage := repairValue[object](t, response["usage"])
			require.Equal(t, json.Number("7"), usage["input_tokens"])
			if recover {
				require.Equal(t, "response.completed", text(last["type"]))
				require.Contains(t, quoted(response["output"]), "pwd")
				require.NotContains(t, quoted(response["output"]), "changed operation")
			} else {
				require.Equal(t, "response.failed", text(last["type"]))
				require.Nil(t, cache.get("repair-scope", "call_native"))
				require.Contains(t, string(ObservedStreamFailure(stream).Diagnostic), "arguments_changed")
			}
		})
	}
}

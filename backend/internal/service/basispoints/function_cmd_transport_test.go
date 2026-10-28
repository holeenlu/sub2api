package basispoints

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFunctionCmdContractFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		target, reason string
		definition     object
	}{
		{"missing", "unknown_target", functionCmdTestTool("exec_command")},
		{"exec_command", "wrong_tool_kind", object{"type": "custom", "name": "exec_command"}},
		{"exec_command", "cmd_not_string", object{"type": "function", "name": "exec_command", "parameters": object{"type": "object", "properties": object{"cmd": object{"type": "number"}}}}},
		{"exec_command", "code_transport_preferred", object{"type": "function", "name": "exec_command", "parameters": object{"type": "object", "properties": object{"code": object{"type": "string"}, "cmd": object{"type": "string"}}}}},
		{"shell", "raw_transport_not_declared", functionCmdTestTool("shell")},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			source := testSource()
			source["stream"] = true
			source["tools"] = []any{tc.definition}
			_, bridge := mustPrepare(t, source, "contract", nil)
			bad := functionCmdTestNative(t, tc.target, "PRIVATE_COMMAND", `{"description":"PRIVATE_METADATA"}`)
			original := repairResponse("resp_contract", 10, 2, bad)
			regenerations := 0
			body := bridge.StreamWithRepairs(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": original}))), nil, func(context.Context) (io.ReadCloser, error) { regenerations++; return nil, nil })
			events := repairEvents(t, body)
			require.Equal(t, "response.failed", events[len(events)-1]["type"])
			require.Zero(t, regenerations, "a raw marker cannot authorize choosing a replacement target")
			failure := ObservedStreamFailure(body)
			require.NotNil(t, failure)
			require.True(t, failure.Local)
			var diagnostic, detail object
			require.NoError(t, decode(failure.Diagnostic, &diagnostic))
			require.NoError(t, decode([]byte(text(diagnostic["error"].(object)["param"])), &detail))
			require.Equal(t, "gateway_tool_transport", detail["source"])
			require.Equal(t, "function_cmd", detail["transport"])
			require.Equal(t, tc.reason, detail["reason"])
			require.Equal(t, tc.target, detail["rejected_tool"])
			require.Equal(t, false, detail["executed"])
			require.NotEmpty(t, detail["catalog_fingerprint"])
			require.Equal(t, "1", fmt.Sprint(detail["catalog_tool_count"]))
			require.NotContains(t, string(failure.Diagnostic), "PRIVATE_")
			require.NotContains(t, quoted(events), "PRIVATE_")
			require.Equal(t, "10", fmt.Sprint(ObservedStreamUsage(body)["input_tokens"]))
		})
	}
}

func TestFunctionCmdUnknownTargetExplainsWithoutExecuting(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		calls        int
	}{
		{"first_turn", "", 1}, {"complete_history", "", 1}, {"reasoning_null", "", 1},
		{"tool_output", "tool_output_forbidden", 1}, {"unknown_tool_output", "tool_output_forbidden", 1},
		{"hosted_output", "invalid_output", 1}, {"empty_explanation", "invalid_explanation", 1},
		{"visible", "", 0}, {"incomplete_history", "", 0}, {"standalone", "", 0}, {"structured", "", 0},
		{"bad_metadata", "", 0}, {"nonstring_cmd", "", 0}, {"conflicting_cmd", "", 0},
		{"reused_call", "", 0}, {"reused_item", "", 0}, {"missing_item", "", 0},
		{"multiple_original", "", 0}, {"tool_not_last", "", 0}, {"hosted_original", "", 0},
		{"cancelled", "", 1}, {"upstream_failure", "", 1}, {"request_failure", "request_failed", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := testSource()
			source["stream"] = true
			source["tools"] = []any{functionCmdTestTool("shell")}
			history := []any{message("user", "inspect"),
				object{"type": "function_call", "id": "fc_old", "call_id": "old", "name": "shell", "arguments": `{"cmd":"pwd","description":"inspect"}`},
				object{"type": "function_call_output", "call_id": "old", "output": "recorded result"},
			}
			switch tc.name {
			case "first_turn":
				history = history[:1]
			case "incomplete_history":
				history = history[:2]
			case "standalone":
				history = append(history, object{"type": "function_call_output", "name": "external_context", "output": "external context"})
			case "structured":
				source["text"] = object{"format": object{"type": "json_object"}}
			}
			source["input"] = append(history, object{"type": "compaction_trigger"})
			cache := new(ReplayCache)
			prepared, bridge := mustPrepare(t, source, "cmd-explanation", cache)
			previous := quoted(cache.get("cmd-explanation", "old"))
			var code, metadata any = "PRIVATE_COMMAND", `{"description":"PRIVATE_METADATA"}`
			switch tc.name {
			case "bad_metadata":
				metadata = "{"
			case "nonstring_cmd":
				code = 42
			case "conflicting_cmd":
				metadata = `{"cmd":"different"}`
			}
			bad := functionCmdTestNative(t, "exec_command", code, metadata)
			switch tc.name {
			case "reused_call":
				bad["call_id"] = "old"
			case "reused_item":
				bad["id"] = "fc_old"
			case "missing_item":
				delete(bad, "id")
			}
			original := repairResponse("resp_original", 10, 2, bad)
			switch tc.name {
			case "multiple_original":
				original["output"] = append(original["output"].([]any), repairCall("second", "Run", `{"name":"shell","arguments":{}}`))
			case "tool_not_last":
				original["output"] = append(original["output"].([]any), message("assistant", ""))
			case "hosted_original":
				original["output"] = []any{object{"type": "web_search_call"}, bad}
			}
			prefix := ""
			if tc.name == "visible" {
				prefix = sse(object{"type": "response.output_text.delta", "delta": "Checking."})
			}
			upstream := &repairTrackedBody{Reader: strings.NewReader(prefix + sse(object{"type": "response.completed", "response": original}))}
			upstreamFailure := &UpstreamFailure{Status: 503, Code: "server_is_overloaded", Type: "service_unavailable_error", Message: "upstream overloaded"}
			calls, regenerations := 0, 0
			body := bridge.StreamWithRepairs(t.Context(), upstream, func(ctx context.Context, failed object, validation error) (object, error) {
				calls++
				require.Equal(t, int32(1), upstream.closed.Load())
				wire, err := BuildToolRepairRequest([]byte(quoted(prepared)), failed, validation)
				require.NoError(t, err)
				var request object
				require.NoError(t, decode(wire, &request))
				input := request["input"].([]any)
				require.Equal(t, "compaction_trigger", input[len(input)-1].(object)["type"])
				prompt := quoted(input[len(input)-2])
				require.Contains(t, prompt, "Return only a nonempty assistant explanation")
				require.Contains(t, prompt, "Do not call any tool")
				require.NotContains(t, prompt, "ordinary FUNCTION envelope")
				feedback := input[len(input)-3].(object)
				require.Contains(t, text(feedback["output"]), `"executed":false`)
				part := object{"type": "output_text", "text": "The requested command tool is unavailable in this catalog.", "annotations": []any{}}
				message := object{"type": "message", "id": "msg_explanation", "role": "assistant", "status": "completed", "content": []any{part}}
				fixed := repairResponse("resp_correction", 7, 1, message)
				switch tc.name {
				case "reasoning_null":
					fixed["output"] = []any{object{"type": "reasoning", "summary": []any{}, "encrypted_content": nil}, message}
				case "tool_output", "unknown_tool_output":
					target := "shell"
					if tc.name == "unknown_tool_output" {
						target = "another_unknown"
					}
					fixed["output"] = append(fixed["output"].([]any), repairCall("new", "Run", quoted(object{"name": target, "arguments": object{"cmd": "PRIVATE_COMMAND", "description": "inspect"}})))
				case "hosted_output":
					fixed["output"] = append(fixed["output"].([]any), object{"type": "web_search_call"})
				case "empty_explanation":
					part["text"] = " "
				case "cancelled":
					return fixed, context.Canceled
				case "upstream_failure":
					return fixed, upstreamFailure
				case "request_failure":
					return fixed, errors.New("PRIVATE_REQUEST_ERROR")
				}
				return fixed, nil
			}, func(context.Context) (io.ReadCloser, error) {
				regenerations++
				return nil, errors.New("unexpected executable regeneration")
			})
			if tc.name == "cancelled" {
				_, err := io.ReadAll(body)
				require.ErrorIs(t, err, context.Canceled)
				_ = body.Close()
			} else {
				events := repairEvents(t, body)
				last := events[len(events)-1]
				require.Equal(t, "resp_original", last["response"].(object)["id"])
				for _, event := range events {
					require.False(t, isToolEvent(text(event["type"])))
					item, _ := event["item"].(object)
					require.False(t, isTool(item))
				}
				if tc.name == "first_turn" || tc.name == "complete_history" || tc.name == "reasoning_null" {
					require.Equal(t, "response.completed", last["type"])
					require.Nil(t, ObservedStreamFailure(body))
				} else if tc.name == "upstream_failure" {
					require.Equal(t, "response.failed", last["type"])
					require.Same(t, upstreamFailure, ObservedStreamFailure(body))
					require.Equal(t, "server_is_overloaded", last["response"].(object)["error"].(object)["code"])
				} else {
					require.Equal(t, "response.failed", last["type"])
					failure := ObservedStreamFailure(body)
					require.NotNil(t, failure)
					var diagnostic, detail object
					require.NoError(t, decode(failure.Diagnostic, &diagnostic))
					require.NoError(t, decode([]byte(text(diagnostic["error"].(object)["param"])), &detail))
					require.Equal(t, "gateway_tool_transport", detail["source"])
					require.Equal(t, "function_cmd", detail["transport"])
					require.Equal(t, "unknown_target", detail["reason"])
					require.Equal(t, "exec_command", detail["rejected_tool"])
					require.Equal(t, false, detail["executed"])
					require.Equal(t, fmt.Sprint(tc.calls), fmt.Sprint(detail["correction_attempt"]))
					if tc.calls > 0 {
						require.Equal(t, "failed", detail["correction_outcome"])
						require.Equal(t, tc.reason, detail["correction_reason"])
					} else {
						require.NotContains(t, detail, "correction_outcome")
					}
					require.NotContains(t, string(failure.Diagnostic), "PRIVATE_")
				}
				require.NotContains(t, quoted(events), "PRIVATE_")
			}
			require.Equal(t, tc.calls, calls)
			require.Zero(t, regenerations)
			require.Nil(t, cache.get("cmd-explanation", "call_code"))
			require.Nil(t, cache.get("cmd-explanation", "new"))
			require.Equal(t, previous, quoted(cache.get("cmd-explanation", "old")))
			require.Equal(t, fmt.Sprint(10+7*tc.calls), fmt.Sprint(ObservedStreamUsage(body)["input_tokens"]))
		})
	}
}

func TestFunctionCmdUndeclaredRawTransportRepairsOnlySameOperation(t *testing.T) {
	for _, scenario := range []string{"same", "changed_cmd", "changed_metadata", "changed_target", "duplicate_equal", "duplicate_conflict", "invalid_metadata", "invalid_schema"} {
		t.Run(scenario, func(t *testing.T) {
			source := testSource()
			source["stream"] = true
			source["tools"] = []any{functionCmdTestTool("shell"), functionCmdTestTool("other_shell")}
			cache := new(ReplayCache)
			_, bridge := mustPrepare(t, source, "contract", cache)
			const originalCode = "printf '%s\\n' \"$PRIVATE_VALUE\"\n"
			metadata := object{"description": "inspect", "workdir": "/private-original"}
			if strings.HasPrefix(scenario, "duplicate_") {
				metadata["cmd"] = originalCode
			}
			if scenario == "duplicate_conflict" {
				metadata["cmd"] = "another command"
			}
			if scenario == "invalid_schema" {
				delete(metadata, "description")
			}
			encoded := quoted(metadata)
			if scenario == "invalid_metadata" {
				encoded = "{"
			}
			bad := functionCmdTestNative(t, "shell", originalCode, encoded)
			original := repairResponse("resp_original", 10, 2, bad)
			calls := 0
			body := bridge.StreamWithToolRepair(t.Context(), io.NopCloser(strings.NewReader(sse(object{"type": "response.output_text.delta", "delta": "Checking."})+sse(object{"type": "response.completed", "response": original}))), func(context.Context, object, error) (object, error) {
				calls++
				args := object{"cmd": originalCode, "description": "inspect", "workdir": "/private-original"}
				target := "shell"
				if scenario == "changed_cmd" {
					args["cmd"] = "changed command"
				}
				if scenario == "changed_metadata" {
					args["workdir"] = "/private-changed"
				}
				if scenario == "changed_target" {
					target = "other_shell"
				}
				return repairResponse("resp_fixed", 7, 1, repairCall("fixed", "Run", quoted(object{"name": target, "arguments": args}))), nil
			})
			events := repairEvents(t, body)
			response := events[len(events)-1]["response"].(object)
			require.Equal(t, "resp_original", response["id"])
			require.Equal(t, "Checking.", events[0]["delta"])
			if scenario == "same" || scenario == "changed_cmd" || scenario == "duplicate_equal" {
				require.Equal(t, "response.completed", events[len(events)-1]["type"])
				call := response["output"].([]any)[0].(object)
				args := functionCmdTestArguments(t, call)
				require.Equal(t, originalCode, args["cmd"])
				require.Equal(t, "/private-original", args["workdir"])
				require.NotNil(t, cache.get("contract", "fixed"))
			} else {
				require.Equal(t, "response.failed", events[len(events)-1]["type"])
				require.Nil(t, cache.get("contract", "fixed"))
				require.NotContains(t, quoted(events), "PRIVATE_VALUE")
			}
			wantCalls := 1
			if scenario == "duplicate_conflict" || scenario == "invalid_metadata" || scenario == "invalid_schema" {
				wantCalls = 0
			}
			require.Equal(t, wantCalls, calls)
			require.Equal(t, fmt.Sprint(10+7*calls), fmt.Sprint(ObservedStreamUsage(body)["input_tokens"]))
		})
	}
}

func functionCmdTestTool(name string) object {
	return object{"type": "function", "name": name, "parameters": object{
		"type": "object", "required": []any{"cmd", "description"},
		"properties": object{
			"cmd": object{"type": "string"}, "description": object{"type": "string"},
			"justification": object{"type": "string"}, "sandbox_permissions": object{"type": "string"},
			"timeoutMs": object{"type": "number"},
		},
	}}
}

func functionCmdTestNative(t *testing.T, name string, code any, metadata any) object {
	t.Helper()
	outer, err := json.Marshal(object{
		"summary": functionCmdTransportPrefix + name, "code": code,
		"extended_summary": metadata, "destructive": false, "references": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return object{"type": "function_call", "id": "fc_code", "call_id": "call_code", "name": "run_officejs", "arguments": string(outer), "status": "completed"}
}

func functionCmdTestArguments(t *testing.T, call object) object {
	t.Helper()
	var args object
	if err := decode([]byte(text(call["arguments"])), &args); err != nil {
		t.Fatal(err)
	}
	return args
}

func TestFunctionCmdTransportPreservesObservedQuotePatterns(t *testing.T) {
	// Synthetic variants of the reported PowerShell quote failure. No captured
	// business data or executable test commands are copied into this fixture.
	for _, code := range []string{
		"await tools.exec({command:'Write-Output \"First suite passed\"',cwd:'C:/fixture'});",
		"console.log(await tools.exec({command:'Write-Output \"Second suite passed\"'}));",
	} {
		source := testSource()
		source["tools"] = []any{functionCmdTestTool("exec_command")}
		_, bridge := mustPrepare(t, source, "scope", nil)
		broken := "{\"name\":\"exec_command\",\"arguments\":{\"cmd\":\"" + code + "\",\"description\":\"Run checks\"}}"
		if _, err := decodeTransportEnvelope(broken); err == nil || !strings.Contains(err.Error(), "missing_separator") {
			t.Fatal("the unescaped nested JSON regression must still be rejected")
		}
		native := functionCmdTestNative(t, "exec_command", code, "{\"description\":\"Run checks\"}")
		call, err := bridge.translateCall(native)
		if err != nil {
			t.Fatal(err)
		}
		args := functionCmdTestArguments(t, call)
		if args["cmd"] != code || args["description"] != "Run checks" || call["name"] != "exec_command" {
			t.Fatal("raw code or additional function arguments changed")
		}
	}
}

func TestFunctionCmdTransportPreservesPayloadAndOptionalArguments(t *testing.T) {
	for _, code := range []string{
		"", "  \tconst value = \"中文\";\r\nconsole.log(value);\n",
		"C:\\fixture\\path\\file; literal \\n \\t \\r \\d+ \\u1234",
		"\x00\b\f", "{\"name\":\"other_tool\",\"arguments\":{}}",
		strings.Repeat("\tWrite-Output \"quotes \\\"inside\\\" 中文\"\r\n", 300),
	} {
		source := testSource()
		source["tools"] = []any{functionCmdTestTool("exec_command")}
		_, bridge := mustPrepare(t, source, "scope", nil)
		metadata := object{
			"description": "Inspect \"quoted\" output", "justification": "Requested check",
			"sandbox_permissions": "use_default", "timeoutMs": json.Number("9007199254740993"),
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			t.Fatal(err)
		}
		native := functionCmdTestNative(t, "exec_command", code, string(encoded))
		before, _ := json.Marshal(native)
		call, err := bridge.translateCall(native)
		if err != nil {
			t.Fatal(err)
		}
		got := functionCmdTestArguments(t, call)
		metadata["cmd"] = code
		if !reflect.DeepEqual(got, metadata) {
			t.Fatal("code, permission metadata or numeric precision changed")
		}
		after, _ := json.Marshal(native)
		if !bytes.Equal(before, after) {
			t.Fatal("native arguments were mutated")
		}
		if encrypted, ok := call["encrypted_function_args"].([]string); !ok || len(encrypted) != 0 {
			t.Fatal("plaintext arguments must retain explicit empty encryption metadata")
		}
	}
}

func TestFunctionCmdTransportRejectsInvalidMetadataAndBounds(t *testing.T) {
	source := testSource()
	source["tools"] = []any{functionCmdTestTool("exec_command")}
	_, bridge := mustPrepare(t, source, "scope", nil)
	const private = "private-code-never-in-errors"
	for _, metadata := range []any{
		nil, 42, object{}, "", "null", "[]", "{} {}", "{", "plain prose",
		"{\"description\":\"" + private + "\" trailing}",
		"{\"cmd\":null}", "{\"cmd\":\"duplicate\"}", strings.Repeat(" ", maxEnvelopeBytes),
	} {
		_, err := bridge.translateCall(functionCmdTestNative(t, "exec_command", private, metadata))
		if err == nil || strings.Contains(err.Error(), private) {
			t.Fatal("invalid metadata must fail without leaking payload text")
		}
	}
	for _, code := range []any{nil, 7, object{}, []any{}, strings.Repeat("x", maxEnvelopeBytes), strings.Repeat("\"", maxEnvelopeBytes/2)} {
		if _, err := bridge.translateCall(functionCmdTestNative(t, "exec_command", code, "{}")); err == nil {
			t.Fatal("invalid or oversized raw/serialized arguments were accepted")
		}
	}
	empty, _ := json.Marshal(object{"cmd": "", "description": "Run checks"})
	exact := strings.Repeat("x", maxEnvelopeBytes-len(empty))
	for _, tc := range []struct {
		code string
		ok   bool
	}{{exact, true}, {exact + "x", false}} {
		call, err := bridge.translateCall(functionCmdTestNative(t, "exec_command", tc.code, "{\"description\":\"Run checks\"}"))
		if (err == nil) != tc.ok {
			t.Fatal("serialized argument byte limit was not enforced exactly")
		}
		if tc.ok && len(text(call["arguments"])) != maxEnvelopeBytes {
			t.Fatal("expected the exact serialized byte boundary")
		}
	}
}

func TestFunctionCmdTransportRequiresExplicitCatalogContract(t *testing.T) {
	source := testSource()
	source["tools"] = []any{
		functionCmdTestTool("exec_command"), object{"type": "custom", "name": "custom_code"},
		object{"type": "function", "name": "plain"},
		object{"type": "function", "name": "numeric", "parameters": object{"type": "object", "properties": object{"cmd": object{"type": "number"}}}},
	}
	_, bridge := mustPrepare(t, source, "scope", nil)
	for _, name := range []string{"", "missing", "custom_code", "plain", "numeric", "exec_command ", " exec_command", "functions.functions.exec_command", "other.exec_command", "exec_command\n", "exec_command/extra"} {
		if _, err := bridge.translateCall(functionCmdTestNative(t, name, "private payload", "{}")); err == nil {
			t.Fatalf("undeclared, wrong-kind or approximate catalog name was accepted: %q", name)
		}
	}
	for _, summary := range []any{nil, 1, "Run code", "codex2api.function_cmd", " codex2api.function_cmd/exec_command", "Codex2api.function_cmd/exec_command"} {
		if envelope, marked, err := bridge.functionCmdTransportEnvelope(object{"summary": summary, "code": "source", "extended_summary": "{}"}); envelope != nil || marked || err != nil {
			t.Fatal("an approximate marker activated raw code transport")
		}
	}
	for _, patch := range []object{{"call_id": ""}, {"name": "not_run_officejs"}} {
		native := functionCmdTestNative(t, "exec_command", "source", "{}")
		for key, value := range patch {
			native[key] = value
		}
		if _, err := bridge.translateCall(native); err == nil {
			t.Fatal("native call identity must still be validated")
		}
	}
}

func TestFunctionCmdCatalogAndHistoryUseRawTransport(t *testing.T) {
	source := testSource()
	source["tools"] = []any{object{"type": "namespace", "name": "client", "tools": []any{functionCmdTestTool("exec_command")}}}
	body, bridge := mustPrepare(t, source, "scope", new(ReplayCache))
	items := mustTestValue[[]any](t, body["input"])
	protocolMessage := mustTestValue[object](t, items[1])
	protocol := text(mustTestValue[object](t, mustTestValue[[]any](t, protocolMessage["content"])[0])["text"])
	for _, want := range []string{functionCmdTransportPrefix + "client.exec_command", "FUNCTION_CODE", "all other supplied arguments", "only fields declared", "Do not include cmd"} {
		if !strings.Contains(protocol, want) {
			t.Fatalf("missing transport contract: %s", want)
		}
	}
	const code = "Write-Output \"history survives\"\r\nC:\\fixture\\file"
	native := functionCmdTestNative(t, "client.exec_command", code, "{\"description\":\"Run checks\",\"timeoutMs\":123}")
	call, err := bridge.translateCall(native)
	if err != nil {
		t.Fatal(err)
	}
	if call["namespace"] != "client" || call["name"] != "exec_command" {
		t.Fatal("namespaced identity changed")
	}
	source["input"] = []any{message("user", "continue"), call, object{"type": "function_call_output", "call_id": "call_code", "output": "recorded result"}}
	for i, cache := range []*ReplayCache{bridge.replay, new(ReplayCache)} {
		next, replayBridge := mustPrepare(t, source, "scope", cache)
		input := mustTestValue[[]any](t, next["input"])
		restored := mustTestValue[object](t, input[len(input)-2])
		if i == 0 && !reflect.DeepEqual(native, restored) {
			t.Fatal("cached native tool item must replay exactly")
		}
		outer := functionCmdTestArguments(t, restored)
		if outer["summary"] != functionCmdTransportPrefix+"client.exec_command" || outer["code"] != code {
			t.Fatal("cache-miss history reintroduced nested code JSON")
		}
		roundTrip, err := replayBridge.translateCall(restored)
		if err != nil || !reflect.DeepEqual(functionCmdTestArguments(t, call), functionCmdTestArguments(t, roundTrip)) {
			t.Fatal("history changed function arguments")
		}
	}
}

func TestFunctionCmdTransportStreamingIsAtomic(t *testing.T) {
	for _, metadata := range []string{"{\"description\":\"Run checks\"}", "{invalid"} {
		source := testSource()
		source["tools"] = []any{functionCmdTestTool("exec_command")}
		_, bridge := mustPrepare(t, source, "scope", nil)
		const code = "Write-Output \"exact code\"\r\nC:\\fixture\\file"
		native := functionCmdTestNative(t, "exec_command", code, metadata)
		wire := sse(object{"type": "response.output_item.added", "output_index": 0, "item": native}) +
			sse(object{"type": "response.function_call_arguments.delta", "output_index": 0, "delta": "UNVALIDATED_NATIVE_CODE"}) +
			sse(object{"type": "response.output_item.done", "output_index": 0, "item": native}) +
			sse(object{"type": "response.completed", "response": object{"id": "resp_code", "output": []any{native}}})
		stream := bridge.Stream(io.NopCloser(strings.NewReader(wire)))
		output, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(output, []byte("UNVALIDATED_NATIVE_CODE")) || bytes.Contains(output, []byte(functionCmdTransportPrefix)) {
			t.Fatal("raw native transport leaked before conversion")
		}
		calls, completed, failed := 0, 0, 0
		err = readEvents(bytes.NewReader(output), func(_ string, data []byte) error {
			var event object
			if err := decode(data, &event); err != nil {
				return err
			}
			switch event["type"] {
			case "response.output_item.done":
				calls++
				if functionCmdTestArguments(t, mustTestValue[object](t, event["item"]))["cmd"] != code {
					t.Fatal("streaming changed code content")
				}
			case "response.completed":
				completed++
			case "response.failed":
				failed++
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if metadata == "{invalid" {
			if calls != 0 || completed != 0 || failed != 1 {
				t.Fatal("invalid metadata must fail without emitting a client tool call")
			}
		} else if calls != 1 || completed != 1 || failed != 0 {
			t.Fatal("valid raw code must produce exactly one complete client call")
		}
	}
}

func TestFunctionCmdEligibilityAndLegacyCompatibility(t *testing.T) {
	for _, name := range []string{"exec_command", "functions.exec_command", "client.exec_command"} {
		spec := functionCmdTestTool(name)
		if !supportsFunctionCmdTransport(name, "function", spec["parameters"]) {
			t.Fatal("expected declared command tool")
		}
	}
	for _, name := range []string{"shell", "fake_exec_command", "exec_command.extra", "exec_command "} {
		if supportsFunctionCmdTransport(name, "function", functionCmdTestTool(name)["parameters"]) {
			t.Fatal("unrelated tool enabled")
		}
	}
	spec := functionCmdTestTool("exec_command")
	parameters, ok := spec["parameters"].(object)
	if !ok {
		t.Fatal("expected command tool parameters object")
	}
	props, ok := parameters["properties"].(object)
	if !ok {
		t.Fatal("expected command tool properties object")
	}
	props["code"] = object{"type": "string"}
	if supportsFunctionCmdTransport("exec_command", "function", spec["parameters"]) {
		t.Fatal("existing code contract overridden")
	}
	source := testSource()
	source["tools"] = []any{functionCmdTestTool("exec_command")}
	_, bridge := mustPrepare(t, source, "scope", nil)
	args := object{"cmd": "echo legacy", "description": "legacy check"}
	payload, _ := json.Marshal(object{"name": "exec_command", "arguments": args})
	outer, _ := json.Marshal(object{"code": string(payload)})
	native := functionCmdTestNative(t, "exec_command", "", "{}")
	native["arguments"] = string(outer)
	call, err := bridge.translateCall(native)
	if err != nil || !reflect.DeepEqual(functionCmdTestArguments(t, call), args) {
		t.Fatal("legacy envelope changed", err)
	}
}

func TestFunctionCmdRealParametersPassThrough(t *testing.T) {
	params := object{"type": "object", "required": []any{"cmd"}, "additionalProperties": false,
		"properties": object{
			"cmd": object{"type": "string"}, "workdir": object{"type": "string"},
			"login": object{"type": "boolean"}, "yield_time_ms": object{"type": "number"},
			"prefix_rule":         object{"type": "array", "items": object{"type": "string"}},
			"sandbox_permissions": object{"type": "string", "enum": []any{"use_default", "require_escalated"}},
		}}
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "exec_command", "parameters": params}}
	_, bridge := mustPrepare(t, source, "scope", nil)
	const cmd = "printf \"quoted\""
	good := object{"workdir": "/tmp/fixture", "login": false, "yield_time_ms": json.Number("1000"), "prefix_rule": []any{"echo"}, "sandbox_permissions": "use_default"}
	raw, _ := json.Marshal(good)
	call, err := bridge.translateCall(functionCmdTestNative(t, "exec_command", cmd, string(raw)))
	good["cmd"] = cmd
	if err != nil || !reflect.DeepEqual(functionCmdTestArguments(t, call), good) {
		t.Fatal("real argument types changed", err)
	}
	for _, extra := range []object{
		{"workdir": 7}, {"login": "false"}, {"yield_time_ms": "1000"},
		{"prefix_rule": []any{1}}, {"sandbox_permissions": "unknown"}, {"undeclared": true},
	} {
		raw, _ := json.Marshal(extra)
		call, err := bridge.translateCall(functionCmdTestNative(t, "exec_command", cmd, string(raw)))
		want := object{"cmd": cmd}
		for key, value := range extra {
			want[key] = value
		}
		gotRaw, _ := json.Marshal(functionCmdTestArguments(t, call))
		wantRaw, _ := json.Marshal(want)
		if err != nil || !bytes.Equal(gotRaw, wantRaw) {
			t.Fatal("client-validated metadata was not passed through exactly")
		}
	}
}

func TestFunctionCmdTransportAcceptsRedundantEqualDuplicate(t *testing.T) {
	source := testSource()
	source["tools"] = []any{functionCmdTestTool("exec_command")}
	_, bridge := mustPrepare(t, source, "scope", nil)
	const cmd = "printf \"duplicate-safe\""
	metadata, _ := json.Marshal(object{"cmd": cmd, "description": "Keep the one exact command"})
	call, err := bridge.translateCall(functionCmdTestNative(t, "exec_command", cmd, string(metadata)))
	if err != nil {
		t.Fatal(err)
	}
	want := object{"cmd": cmd, "description": "Keep the one exact command"}
	if got := functionCmdTestArguments(t, call); !reflect.DeepEqual(got, want) {
		t.Fatal("identical duplicate cmd was not collapsed exactly")
	}
}

func TestFunctionCmdTransportRejectsConflictingDuplicate(t *testing.T) {
	source := testSource()
	source["tools"] = []any{functionCmdTestTool("exec_command")}
	_, bridge := mustPrepare(t, source, "scope", nil)
	const cmd = "private-command-never-in-errors"
	for _, duplicate := range []any{"different-command", nil, 7, object{}} {
		metadata, _ := json.Marshal(object{"cmd": duplicate, "description": "Reject conflict"})
		_, err := bridge.translateCall(functionCmdTestNative(t, "exec_command", cmd, string(metadata)))
		if err == nil || strings.Contains(err.Error(), cmd) || strings.Contains(err.Error(), "different-command") {
			t.Fatal("conflicting duplicate cmd was accepted or leaked")
		}
	}
}

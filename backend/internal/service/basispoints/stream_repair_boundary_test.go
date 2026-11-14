package basispoints

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUnknownToolContinuationBoundaries(t *testing.T) {
	for _, scenario := range []string{"success", "tool_with_text", "retired_function", "retired_custom", "changed_schema", "visible_text", "visible_summary", "pending_history", "duplicate_item", "unverifiable_history", "reused_call", "reused_item", "missing_item", "repeated_operation", "second_unknown", "unsafe_second_unknown", "schema_error", "invalid_transport", "expanded_batch", "cancelled", "repair_failure"} {
		t.Run(scenario, func(t *testing.T) {
			source := testSource()
			source["stream"] = true
			source["tools"] = []any{object{"type": "function", "name": "shell", "parameters": object{"type": "object", "required": []any{"cmd"}}}}
			history := []any{message("user", "inspect"), object{"type": "function_call", "id": "fc_old", "call_id": "old", "name": "shell", "arguments": `{"cmd":"pwd"}`}, object{"type": "function_call_output", "call_id": "old", "output": "/tmp"}}
			if scenario == "retired_function" {
				history[1].(object)["name"] = "retired_shell"
			}
			if scenario == "retired_custom" {
				history[1] = object{"type": "custom_tool_call", "id": "ctc_old", "call_id": "old", "name": "retired_exec", "input": "recorded operation"}
				history[2].(object)["type"] = "custom_tool_call_output"
			}
			if scenario == "changed_schema" {
				source["tools"].([]any)[0].(object)["parameters"].(object)["required"] = []any{"cmd", "approved"}
			}
			if scenario == "pending_history" {
				history = history[:2]
			}
			source["input"] = append(history, object{"type": "compaction_trigger"})
			cache := new(ReplayCache)
			prepared, bridge := mustPrepare(t, source, "continuation", cache)
			if scenario == "duplicate_item" || scenario == "unverifiable_history" {
				items := []any{repairCall("old", "Run", `{"name":"shell","arguments":{"cmd":"pwd"}}`), object{"type": "function_call_output", "call_id": "old", "output": "ok"}}
				if scenario == "duplicate_item" {
					other := repairCall("other", "Run", `{"name":"shell","arguments":{"cmd":"ls"}}`)
					other["id"] = "fc_old"
					items = append(items, other, object{"type": "function_call_output", "call_id": "other", "output": "ok"})
				} else {
					items[0].(object)["arguments"] = `{"code":"unverifiable"}`
				}
				bridge.completedToolHistory = bridge.completeToolHistory(items)
				require.Nil(t, bridge.completedToolHistory)
			}
			bad := repairCall("bad", "Run", `{"name":"unknown","arguments":{}}`)
			original := repairResponse("resp_original", 10, 2, bad)
			prefix := ""
			if scenario == "visible_text" || scenario == "visible_summary" {
				kind := "response.output_text.delta"
				if scenario == "visible_summary" {
					kind = "response.reasoning_summary_text.delta"
				}
				prefix = sse(object{"type": kind, "delta": "Working"})
			}
			calls := 0
			var repairBody []byte
			body := driveRepairFixture(context.Background(), bridge, io.NopCloser(strings.NewReader(prefix+sse(object{"type": "response.completed", "response": original}))), func(ctx context.Context, failed object, validation error) (object, error) {
				calls++
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				wire, err := BuildToolRepairRequest([]byte(quoted(prepared)), failed, validation)
				if err != nil {
					return nil, err
				}
				repairBody = wire
				good := repairCall("new", "Run", `{"name":"shell","arguments":{"cmd":"ls"}}`)
				if scenario == "changed_schema" {
					good = repairCall("new", "Run", `{"name":"shell","arguments":{"cmd":"ls","approved":true}}`)
				}
				switch scenario {
				case "reused_call":
					good["call_id"] = "old"
				case "reused_item":
					good["id"] = "fc_old"
				case "missing_item":
					delete(good, "id")
				case "repeated_operation":
					good = repairCall("new", "Run", `{"name":"shell","arguments":{"cmd":"pwd"}}`)
				case "second_unknown":
					good = repairCall("new", "Run", `{"name":"another_unknown","arguments":{}}`)
				case "unsafe_second_unknown":
					good = repairCall("new", "Run", quoted(object{"name": "PRIVATE_PAYLOAD secret", "arguments": object{}}))
				case "schema_error":
					good = repairCall("new", "Run", `{"name":"shell","arguments":{}}`)
				case "invalid_transport":
					good = repairCall("new", "Run", "PRIVATE_PAYLOAD unframed code")
				}
				fixed := repairResponse("resp_fixed", 7, 1, good)
				if scenario == "tool_with_text" {
					fixed["output"] = append([]any{object{"type": "message", "id": "msg_commentary", "role": "assistant", "phase": "commentary", "content": []any{object{"type": "output_text", "text": "Continuing with the current tools.", "annotations": []any{object{"type": "url_citation", "url": "https://example.invalid"}}}}}}, good)
				}
				if scenario == "expanded_batch" {
					fixed["output"] = append(fixed["output"].([]any), repairCall("extra", "Run", `{"name":"shell","arguments":{"cmd":"date"}}`))
				}
				if scenario == "cancelled" {
					return fixed, context.Canceled
				}
				if scenario == "repair_failure" {
					return fixed, errors.New("PRIVATE_PAYLOAD correction connection failed")
				}
				return fixed, nil
			}, nil)
			if scenario == "cancelled" {
				_, err := io.ReadAll(body)
				require.ErrorIs(t, err, context.Canceled)
				_ = body.Close()
			} else {
				events := repairEvents(t, body)
				last := events[len(events)-1]
				if scenario == "success" || scenario == "tool_with_text" || scenario == "retired_function" || scenario == "retired_custom" || scenario == "changed_schema" {
					require.Equal(t, "response.completed", last["type"])
					require.NotNil(t, cache.get("continuation", "new"))
				} else {
					require.Equal(t, "response.failed", last["type"])
					require.NotNil(t, ObservedStreamFailure(body))
					require.True(t, ObservedStreamFailure(body).Local)
					require.Nil(t, cache.get("continuation", "new"))
					for _, event := range events {
						require.False(t, isToolEvent(text(event["type"])))
						item, _ := event["item"].(object)
						require.False(t, isTool(item))
					}
					reasons := map[string]string{
						"reused_call": "reused_call_id", "reused_item": "reused_item_id", "missing_item": "missing_item_id",
						"repeated_operation": "repeated_operation", "second_unknown": "unknown_tool", "unsafe_second_unknown": "unknown_tool",
						"schema_error": "schema_error", "invalid_transport": "invalid_transport", "expanded_batch": "invalid_batch", "repair_failure": "request_failed",
					}
					if reason := reasons[scenario]; reason != "" {
						var diagnostic, detail object
						failure := ObservedStreamFailure(body)
						require.NoError(t, decode(failure.Diagnostic, &diagnostic))
						require.NoError(t, decode([]byte(text(diagnostic["error"].(object)["param"])), &detail))
						require.Equal(t, "unknown", detail["rejected_tool"])
						require.Equal(t, reason, detail["correction_reason"])
						require.Equal(t, "failed", detail["correction_outcome"])
						require.Contains(t, failure.Message, reason)
						require.Equal(t, true, detail["regeneration_attempted"])
						require.NotContains(t, string(failure.Diagnostic), "PRIVATE_PAYLOAD")
						require.NotContains(t, quoted(events), "PRIVATE_PAYLOAD")
						if scenario == "second_unknown" {
							require.Equal(t, "another_unknown", detail["correction_rejected_tool"])
						} else if scenario == "unsafe_second_unknown" {
							require.Equal(t, "[invalid identifier]", detail["correction_rejected_tool"])
						} else {
							require.NotContains(t, detail, "correction_rejected_tool")
						}
					}
				}
				require.Equal(t, "resp_original", last["response"].(object)["id"])
			}
			want := 1
			switch scenario {
			case "visible_text", "visible_summary", "pending_history", "duplicate_item", "unverifiable_history":
				want = 0
			}
			require.Equal(t, want, calls)
			if calls > 0 {
				var request object
				require.NoError(t, decode(repairBody, &request))
				input := request["input"].([]any)
				require.Equal(t, "compaction_trigger", input[len(input)-1].(object)["type"])
				require.Contains(t, string(repairBody), `\"executed\":false`)
				require.Contains(t, string(repairBody), "unknown_client_tool")
			}
			require.Nil(t, cache.get("continuation", "bad"))
			usage := ObservedStreamUsage(body)
			require.Equal(t, fmt.Sprint(10+calls*7), fmt.Sprint(usage["input_tokens"]))
		})
	}
}

func TestUnknownToolContinuationExplanation(t *testing.T) {
	for _, scenario := range []string{
		"text", "refusal", "empty_text", "missing_id", "wrong_role", "incomplete_message", "commentary",
		"unknown_content", "nonstring_text", "nonarray_annotations", "annotations", "reasoning", "null_encrypted_reasoning", "malformed_encrypted_reasoning", "reasoning_only", "malformed_reasoning", "hosted_tool",
		"unknown_tool", "multi_tool", "malformed_output", "malformed_item", "non_completed",
		"error_payload", "empty_output", "duplicate_message", "omitted_tool",
	} {
		t.Run(scenario, func(t *testing.T) {
			source := testSource()
			source["stream"] = true
			source["tools"] = []any{object{"type": "function", "name": "shell"}}
			source["input"] = []any{
				object{"type": "function_call", "id": "fc_old", "call_id": "old", "name": "shell", "arguments": `{"cmd":"pwd"}`},
				object{"type": "function_call_output", "call_id": "old", "output": "recorded result"},
			}
			cache := new(ReplayCache)
			_, bridge := mustPrepare(t, source, "explanation", cache)
			history := quoted(cache.get("explanation", "old"))
			bad := repairCall("bad", "Run", `{"name":"unknown","arguments":{}}`)
			original := repairResponse("resp_original", 10, 2, bad)
			upstream := &repairTrackedBody{Reader: strings.NewReader(sse(object{"type": "response.completed", "response": original}))}
			calls, regenerations := 0, 0
			wantReason := ""
			body := driveRepairFixture(context.Background(), bridge, upstream, func(ctx context.Context, failed object, validation error) (object, error) {
				calls++
				require.Equal(t, int32(1), upstream.closed.Load())
				part := object{"type": "output_text", "text": "The current tools cannot perform that operation.", "annotations": []any{}}
				message := object{"type": "message", "id": "msg_explanation", "role": "assistant", "status": "completed", "content": []any{part}}
				fixed := repairResponse("resp_correction", 7, 1, message)
				wantReason = "invalid_explanation"
				switch scenario {
				case "text":
					wantReason = ""
				case "refusal":
					wantReason = ""
					message["content"] = []any{object{"type": "refusal", "refusal": "I cannot perform that operation with the current tools."}}
				case "empty_text":
					part["text"] = " \n\t"
				case "missing_id":
					delete(message, "id")
				case "wrong_role":
					message["role"] = "developer"
				case "incomplete_message":
					message["status"] = "in_progress"
				case "commentary":
					wantReason = ""
					message["phase"] = "commentary"
				case "unknown_content":
					message["content"] = append(message["content"].([]any), object{"type": "function_call", "name": "shell", "arguments": "{}"})
				case "nonstring_text":
					part["text"] = object{"type": "function_call"}
				case "nonarray_annotations":
					part["annotations"] = object{"type": "function_call"}
				case "annotations":
					wantReason = ""
					part["annotations"] = []any{object{"type": "url_citation", "url": "https://example.invalid", "title": "Example", "start_index": 0, "end_index": 3}}
					part["logprobs"] = []any{object{"token": "The", "logprob": -0.1, "bytes": []any{84, 104, 101}, "top_logprobs": []any{}}}
				case "reasoning", "null_encrypted_reasoning", "malformed_encrypted_reasoning", "reasoning_only", "malformed_reasoning":
					reasoning := object{"type": "reasoning", "id": "rs_explanation", "summary": []any{object{"type": "summary_text", "text": "The catalog does not cover the task."}}, "encrypted_content": "opaque-reasoning-data"}
					fixed["output"] = []any{reasoning, message}
					if scenario == "reasoning" {
						wantReason = ""
					} else if scenario == "null_encrypted_reasoning" {
						wantReason = ""
						reasoning["encrypted_content"] = nil
					} else if scenario == "malformed_encrypted_reasoning" {
						reasoning["encrypted_content"] = object{"type": "function_call"}
					} else if scenario == "reasoning_only" {
						fixed["output"] = []any{reasoning}
					} else {
						reasoning["summary"] = []any{object{"type": "function_call", "name": "shell"}}
					}
				case "hosted_tool":
					wantReason = "invalid_output"
					fixed["output"] = append(fixed["output"].([]any), object{"type": "web_search_call", "id": "hidden"})
				case "unknown_tool":
					wantReason = "unknown_tool"
					fixed["output"] = append(fixed["output"].([]any), repairCall("new", "Run", `{"name":"another_unknown","arguments":{}}`))
				case "multi_tool":
					wantReason = "invalid_batch"
					fixed["output"] = append(fixed["output"].([]any), repairCall("new", "Run", `{"name":"shell","arguments":{}}`), repairCall("extra", "Run", `{"name":"shell","arguments":{}}`))
				case "malformed_output":
					wantReason = "invalid_output"
					fixed["output"] = object{"message": message}
				case "malformed_item":
					wantReason = "invalid_output"
					fixed["output"] = append(fixed["output"].([]any), "PRIVATE_PAYLOAD")
				case "non_completed":
					wantReason = "non_completed"
					fixed["status"] = "incomplete"
				case "error_payload":
					wantReason = "invalid_output"
					fixed["error"] = object{"message": "PRIVATE_PAYLOAD"}
				case "empty_output":
					wantReason = "invalid_output"
					fixed["output"] = []any{}
				case "duplicate_message":
					fixed["output"] = append(fixed["output"].([]any), message)
				case "omitted_tool":
					wantReason = "request_failed"
					hidden := repairCall("new", "Run", `{"name":"shell","arguments":{}}`)
					wire := sse(object{"type": "response.output_item.done", "item": hidden}) + sse(object{"type": "response.completed", "response": fixed})
					return ReadToolRepairResponse(strings.NewReader(wire))
				}
				return fixed, nil
			}, func(context.Context) (io.ReadCloser, error) {
				regenerations++
				return nil, errors.New("unexpected regeneration")
			})
			events := repairEvents(t, body)
			last := events[len(events)-1]
			response := last["response"].(object)
			require.Equal(t, "resp_original", response["id"])
			require.Equal(t, 1, calls)
			require.Zero(t, regenerations)
			for _, event := range events {
				require.False(t, isToolEvent(text(event["type"])))
				item, _ := event["item"].(object)
				require.False(t, isTool(item))
			}
			if wantReason == "" {
				require.Equal(t, "response.completed", last["type"])
				require.Nil(t, ObservedStreamFailure(body))
				output := response["output"].([]any)
				wantItems := 1
				if scenario == "reasoning" || scenario == "null_encrypted_reasoning" {
					wantItems++
				}
				require.Len(t, output, wantItems)
				require.Equal(t, "msg_explanation", output[len(output)-1].(object)["id"])
				require.Contains(t, quoted(events), "response.content_part.done")
			} else {
				require.Equal(t, "response.failed", last["type"])
				require.Empty(t, response["output"])
				var diagnostic, detail object
				failure := ObservedStreamFailure(body)
				require.NotNil(t, failure)
				require.NoError(t, decode(failure.Diagnostic, &diagnostic))
				require.NoError(t, decode([]byte(text(diagnostic["error"].(object)["param"])), &detail))
				require.Equal(t, "unknown", detail["rejected_tool"])
				require.Equal(t, wantReason, detail["correction_reason"])
				require.Equal(t, "failed", detail["correction_outcome"])
				require.NotContains(t, string(failure.Diagnostic), "PRIVATE_PAYLOAD")
				require.NotContains(t, quoted(events), "response.content_part.done")
			}
			require.NotContains(t, quoted(events), "PRIVATE_PAYLOAD")
			for _, call := range []string{"bad", "new", "extra"} {
				require.Nil(t, cache.get("explanation", call))
			}
			require.Equal(t, history, quoted(cache.get("explanation", "old")))
			require.Equal(t, "17", fmt.Sprint(ObservedStreamUsage(body)["input_tokens"]))
			require.Equal(t, "3", fmt.Sprint(ObservedStreamUsage(body)["output_tokens"]))
		})
	}
}

func TestUnknownToolRegenerationStopsAfterVisibleContent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		event   object
		visible bool
	}{
		{"text_delta", object{"type": "response.output_text.delta", "delta": "Working."}, true},
		{"summary_delta", object{"type": "response.reasoning_summary_text.delta", "delta": "Checking tools."}, true},
		{"whitespace_delta", object{"type": "response.output_text.delta", "delta": " "}, true},
		{"empty_summary_delta", object{"type": "response.reasoning_summary_text.delta", "delta": ""}, false},
		{"text_done", object{"type": "response.output_text.done", "text": "Working."}, true},
		{"summary_done", object{"type": "response.reasoning_summary_text.done", "text": "Checking tools."}, true},
		{"summary_part", object{"type": "response.reasoning_summary_part.done", "part": object{"type": "summary_text", "text": "Checking tools."}}, true},
		{"message_item", object{"type": "response.output_item.done", "item": object{"type": "message", "content": []any{object{"type": "output_text", "text": "Working."}}}}, true},
		{"reasoning_item", object{"type": "response.output_item.done", "item": object{"type": "reasoning", "summary": []any{object{"type": "summary_text", "text": "Checking tools."}}}}, true},
		{"empty_reasoning_item", object{"type": "response.output_item.added", "item": object{"type": "reasoning", "summary": []any{}, "encrypted_content": "opaque-test-value"}}, false},
		{"empty_content_part", object{"type": "response.content_part.added", "part": object{"type": "output_text", "text": ""}}, false},
		{"refusal_delta", object{"type": "response.refusal.delta", "delta": "Cannot do that."}, true},
		{"refusal_part", object{"type": "response.content_part.done", "part": object{"type": "refusal", "refusal": "Cannot do that."}}, true},
		{"metadata", object{"type": "response.created", "response": object{"id": "resp_original", "output": []any{}}}, false},
	} {
		for _, stream := range []bool{true, false} {
			name := tc.name + "/buffered"
			if stream {
				name = tc.name + "/stream"
			}
			t.Run(name, func(t *testing.T) {
				source := testSource()
				source["stream"] = stream
				source["tools"] = []any{object{"type": "function", "name": "shell"}}
				cache := new(ReplayCache)
				_, bridge := mustPrepare(t, source, "boundary", cache)
				bad := nativeCall(object{"name": "unknown", "arguments": object{}})
				original := repairResponse("resp_original", 10, 2, bad)
				wire := sse(tc.event) + sse(object{"type": "response.completed", "response": original})
				regenerations, corrections := 0, 0
				body := driveRepairFixture(context.Background(), bridge, io.NopCloser(strings.NewReader(wire)), func(context.Context, object, error) (object, error) {
					corrections++
					return nil, nil
				}, func(context.Context) (io.ReadCloser, error) {
					regenerations++
					good := nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}})
					good["id"], good["call_id"] = "fc_fixed", "call_fixed"
					return io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": repairResponse("resp_fixed", 7, 1, good)}))), nil
				})
				events := repairEvents(t, body)
				require.Equal(t, tc.event["type"], events[0]["type"])
				for key, value := range tc.event {
					if key == "response" { // Response metadata is normalized by the bridge.
						continue
					}
					require.Equal(t, value, events[0][key], "forwarded event changed: %s", key)
				}
				require.Zero(t, corrections, "must not fall through to the formatting correction path")
				last := events[len(events)-1]
				response := repairValue[object](t, last["response"])
				require.Equal(t, "resp_original", response["id"])
				if stream && tc.visible {
					require.Zero(t, regenerations)
					require.Equal(t, "response.failed", last["type"])
					require.Equal(t, "basispoints_protocol_error", repairValue[object](t, response["error"])["code"])
					require.Equal(t, `{"input_tokens":10,"input_tokens_details":{"cached_tokens":2},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":12}`, quoted(response["usage"]))
					for _, event := range events {
						require.NotEqual(t, "response.completed", event["type"])
						require.False(t, isToolEvent(text(event["type"])))
						item, _ := event["item"].(object)
						require.False(t, isTool(item))
					}
					require.Nil(t, cache.get("boundary", "call_fixed"))
					failure := ObservedStreamFailure(body)
					require.NotNil(t, failure, "catalog failures need a diagnostic snapshot before Ops freezes")
					var diagnostic object
					require.NoError(t, decode(failure.Diagnostic, &diagnostic))
					fields := repairValue[object](t, diagnostic["error"])
					var detail object
					require.NoError(t, decode([]byte(text(fields["param"])), &detail))
					require.Equal(t, "unknown", detail["rejected_tool"])
					require.Equal(t, true, detail["visible_output"])
					require.Equal(t, false, detail["regeneration_attempted"])
					require.NotContains(t, quoted(events), "rejected_tool")
				} else {
					require.Equal(t, 1, regenerations)
					require.Equal(t, "response.completed", last["type"])
					require.NotNil(t, cache.get("boundary", "call_fixed"))
				}
				require.Nil(t, cache.get("boundary", "call_native"))
			})
		}
	}
}

func TestReasoningSummaryArrivesBeforeTerminal(t *testing.T) {
	source := testSource()
	source["stream"] = true
	_, bridge := mustPrepare(t, source, "", nil)
	upstream, producer := io.Pipe()
	body := bridge.Stream(upstream)
	t.Cleanup(func() { _ = body.Close(); _ = producer.Close() })
	written := make(chan error, 1)
	go func() {
		_, err := io.WriteString(producer, sse(object{"type": "response.reasoning_summary_text.delta", "item_id": "rs_test", "output_index": 0, "summary_index": 0, "delta": "Checking tools."}))
		written <- err
	}()
	// The producer remains open without a terminal event. The summary must
	// already be readable, not buffered until the upstream finishes.
	read := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(body).ReadString('\n')
		read <- line
	}()
	select {
	case line := <-read:
		require.Equal(t, "event: response.reasoning_summary_text.delta\n", line)
	case <-time.After(3 * time.Second):
		t.Fatal("reasoning summary was withheld until the terminal event")
	}
	require.NoError(t, <-written)
}

func TestUpstreamEventObservationPrecedesWithheldContent(t *testing.T) {
	for _, mode := range []string{"tool", "structured"} {
		t.Run(mode, func(t *testing.T) {
			source := testSource()
			source["stream"] = true
			first := object{"type": "response.function_call_arguments.delta", "delta": "UNVALIDATED_NATIVE"}
			var terminal string
			if mode == "tool" {
				source["tools"] = []any{object{"type": "function", "name": "shell", "parameters": object{"type": "object"}}}
				terminal = sse(object{"type": "response.completed", "response": object{"id": "resp_observed", "status": "completed", "output": []any{nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}})}}})
			} else {
				source = testStructuredSource(testStructuredFormat())
				source["stream"] = true
				first["type"] = "response.output_text.delta"
				terminal = structuredTerminal("response.completed", structuredMessage(`{"answer":9007199254740993}`))
			}
			_, bridge := mustPrepare(t, source, "observe", nil)
			checkpoint, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var observed []object
			bridge.OnUpstreamEvent = func(raw []byte) {
				var event object
				if err := decode(raw, &event); err != nil {
					panic(err)
				}
				observed = append(observed, event)
				if len(observed) == 2 {
					close(checkpoint)
					<-release
				}
			}
			upstream, producer := io.Pipe()
			defer upstream.Close()
			defer producer.Close()
			var downstream bytes.Buffer
			done, written := make(chan error, 1), make(chan error, 1)
			go func() {
				conversion := bridge.NewStreamConversion(writeSSEFixture(&downstream), RepairPolicy{})
				_, err := conversion.Convert(upstream)
				if err == nil {
					err = conversion.Deliver(bridge.CommitReplays(conversion.Replays()))
				}
				done <- err
			}()
			go func() {
				// The second event is a synchronization barrier after the hidden
				// event was fully processed. Its SSE type is absent from JSON.
				_, err := io.WriteString(producer, sse(first)+"event: response.queued\ndata: {}\n\n"+terminal)
				_ = producer.Close()
				written <- err
			}()
			select {
			case <-checkpoint:
			case <-time.After(2 * time.Second):
				t.Fatal("converter did not reach the post-content observation boundary")
			}
			// The worker is paused at the next upstream event, so this read is
			// synchronized and cannot race a downstream write.
			require.Zero(t, downstream.Len(), "raw native/structured content must remain withheld")
			require.Equal(t, "UNVALIDATED_NATIVE", observed[0]["delta"])
			require.Equal(t, "response.queued", observed[1]["type"], "observer includes the SSE event name")
			unblock()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("converter did not finish after releasing the observation barrier")
			}
			require.NoError(t, <-written)
			require.NotContains(t, downstream.String(), "UNVALIDATED_NATIVE")
			require.Contains(t, downstream.String(), "response.completed")
			require.Equal(t, "response.completed", observed[len(observed)-1]["type"])
		})
	}
}

func TestReadEventsAcceptsAllSSELineEndings(t *testing.T) {
	type event struct{ name, data string }
	collect := func(t *testing.T, wire string) []event {
		t.Helper()
		var got []event
		require.NoError(t, readEvents(strings.NewReader(wire), func(name string, data []byte) error {
			got = append(got, event{name, string(data)})
			return nil
		}))
		return got
	}
	lf := "event: response.created\ndata: {\"a\":1}\n\n: comment\ndata: {\"b\":\ndata: 2}\n\ndata: {\"tail\":true}"
	want := collect(t, lf)
	require.Equal(t, []event{{"response.created", `{"a":1}`}, {"", "{\"b\":\n2}"}, {"", `{"tail":true}`}}, want)
	require.Equal(t, want, collect(t, strings.ReplaceAll(lf, "\n", "\r\n")))
	require.Equal(t, want, collect(t, strings.ReplaceAll(lf, "\n", "\r")))
	require.Equal(t, want, collect(t, "\ufeff"+lf))

	// A CR-only live stream must dispatch an event without waiting for EOF.
	reader, writer := io.Pipe()
	seen := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- readEvents(reader, func(_ string, data []byte) error {
			seen <- string(data)
			return nil
		})
	}()
	_, err := io.WriteString(writer, "data: {\"live\":1}\r\r")
	require.NoError(t, err)
	select {
	case data := <-seen:
		require.Equal(t, `{"live":1}`, data)
	case <-time.After(2 * time.Second):
		t.Fatal("CR-terminated event was not dispatched before EOF")
	}
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
}

func TestUnknownToolContinuationBlockReasonDiagnostics(t *testing.T) {
	for _, reason := range []string{
		"visible_output", "structured_output", "incomplete_history", "empty_catalog", "non_completed",
		"invalid_batch", "tool_not_last", "reused_call_id", "missing_item_id", "reused_item_id",
	} {
		t.Run(reason, func(t *testing.T) {
			source := testSource()
			source["stream"] = true
			source["tools"] = []any{object{"type": "function", "name": "shell"}}
			source["input"] = []any{
				object{"type": "function_call", "id": "fc_old", "call_id": "old", "name": "shell", "arguments": `{"cmd":"PRIVATE_HISTORY"}`},
				object{"type": "function_call_output", "call_id": "old", "output": "PRIVATE_RESULT"},
			}
			_, bridge := mustPrepare(t, source, "blocker-diagnostic", new(ReplayCache))
			bad := repairCall("bad", "Run", `{"name":"unknown","arguments":{"cmd":"PRIVATE_COMMAND"}}`)
			original := repairResponse("resp_original", 10, 2, bad)
			visible := reason == "visible_output"
			switch reason {
			case "structured_output":
				var err error
				bridge.structured, err = prepareStructuredOutput(object{"format": object{"type": "json_object"}})
				require.NoError(t, err)
			case "incomplete_history":
				bridge.completedToolHistory = nil
			case "empty_catalog":
				bridge.tools = nil
			case "non_completed":
				original["status"] = "incomplete"
			case "invalid_batch":
				original["output"] = append(original["output"].([]any), repairCall("second", "Run", `{"name":"unknown","arguments":{}}`))
			case "tool_not_last":
				original["output"] = append(original["output"].([]any), message("assistant", ""))
			case "reused_call_id":
				bad["call_id"] = "old"
			case "missing_item_id":
				delete(bad, "id")
			case "reused_item_id":
				bad["id"] = "fc_old"
			}
			unknown := unknownClientToolError{target: "unknown"}
			require.Equal(t, reason, bridge.unknownContinuationBlockReason(original, unknown, visible))
			if !visible {
				require.False(t, bridge.unknownContinuationBlockReason(original, unknown, false) == "")
			}
			direct := bridge.unknownToolFailure(unknown, original, visible, false)
			var diagnostic, detail object
			require.NoError(t, decode(direct.Diagnostic, &diagnostic))
			require.NoError(t, decode([]byte(text(diagnostic["error"].(object)["param"])), &detail))
			require.Equal(t, reason, detail["correction_blocker"])
			require.NotContains(t, string(direct.Diagnostic), "PRIVATE_")
			require.NotContains(t, string(bridge.unknownToolFailure(unknown, original, visible, true).Diagnostic), "correction_blocker", "an attempted correction must not be diagnosed as blocked before it ran")

			prefix := ""
			if visible {
				prefix = sse(object{"type": "response.output_text.delta", "delta": "Checking."})
			}
			corrections, regenerations := 0, 0
			body := driveRepairFixture(t.Context(), bridge, io.NopCloser(strings.NewReader(prefix+sse(object{"type": "response.completed", "response": original}))), func(context.Context, object, error) (object, error) {
				corrections++
				return nil, errors.New("unexpected correction")
			}, func(context.Context) (io.ReadCloser, error) {
				regenerations++
				return nil, errors.New("unexpected regeneration")
			})
			events := repairEvents(t, body)
			require.Zero(t, corrections)
			require.Zero(t, regenerations)
			require.Equal(t, "response.failed", events[len(events)-1]["type"])
			require.NotContains(t, quoted(events), "PRIVATE_")
			require.Equal(t, "10", fmt.Sprint(ObservedStreamUsage(body)["input_tokens"]))
			failure := ObservedStreamFailure(body)
			require.NotNil(t, failure)
			require.NoError(t, decode(failure.Diagnostic, &diagnostic))
			require.NoError(t, decode([]byte(text(diagnostic["error"].(object)["param"])), &detail))
			require.Equal(t, reason, detail["correction_blocker"])
			require.NotContains(t, string(failure.Diagnostic), "PRIVATE_")
		})
	}
}

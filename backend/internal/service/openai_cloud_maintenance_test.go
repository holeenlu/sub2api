//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudCatalogPreflightPreservesPendingOwnerAndAllowsRetry(t *testing.T) {
	// This test processes a genuine >4 MiB catalog under the race detector.
	// Its owner must outlive that CPU work so the retry checks the protocol.
	a, store, wire := cloudHostedActorForTest(t, 30*time.Second)
	a.service.validate = func(context.Context, OpenAICloudOwner) error { return nil }
	a.requestID, a.lastResponseID = "", "resp_cloud_previous"
	a.record.Status = "awaiting_tools"
	a.history = []any{cloudUserMessage("go"), map[string]any{"type": "function_call", "call_id": "call_cloud_pending", "name": "echo", "arguments": "{}"}}
	a.tools = map[string]*cloudPendingTool{
		"call_cloud_pending":     {Published: true, Name: "echo", RPCID: json.RawMessage(`"pending-rpc"`)},
		"call_cloud_unpublished": {Name: "echo", RPCID: json.RawMessage(`"unpublished-rpc"`)},
	}
	tools := cloneCloudValue(a.protocol.source["tools"]).([]any)
	// Exceed the real wire limit, independent of JSON HTML escaping.
	added := map[string]any{"type": "function", "name": "discovered", "parameters": map[string]any{"type": "object"}, "description": strings.Repeat("x", OpenAICloudMaxRPCBytes)}
	body := map[string]any{"model": "gpt-test", "previous_response_id": a.lastResponseID, "tools": append(tools, added), "input": []any{map[string]any{"type": "function_call_output", "call_id": "call_cloud_pending", "output": "ok"}}}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	oldProtocol, oldUsage, oldResponse := a.protocol, a.record.Usage, a.responseID
	err = a.handleCommand(OpenAICloudCommand{RequestID: "next", Request: OpenAICloudRequest{Body: raw}})
	status, code, _, known := cloudPublicError(err)
	require.True(t, known, "%v", err)
	require.Equal(t, 413, status)
	require.Equal(t, "cloud_message_too_large", code)
	require.Empty(t, wire.writes, "no interrupt, reply or archive before complete catalog preflight")
	require.Empty(t, store.results)
	require.Empty(t, store.settlements)
	require.Equal(t, "awaiting_tools", a.record.Status)
	require.Equal(t, "thread", a.record.ThreadID)
	require.Equal(t, "turn", a.record.TurnID)
	require.Equal(t, "root", a.record.RootRequestID)
	require.Same(t, oldUsage, a.record.Usage)
	require.Same(t, oldProtocol, a.protocol)
	require.Equal(t, oldResponse, a.responseID)
	require.Empty(t, a.requestID)
	require.Len(t, a.history, 2)
	require.Len(t, a.tools, 2)

	// Retry on the same owner with a small catalog. Unpublished RPCs have no
	// client result; they must be canceled rather than blocking this handoff.
	added["description"] = "small catalog"
	raw, err = json.Marshal(body)
	require.NoError(t, err)
	events := make(chan cloudRPCDelivery, 1)
	a.upstream = events
	script := runCloudScript(func() error {
		if _, err := cloudExpectReply(a.ctx, wire, "turn/interrupt", map[string]any{}); err != nil {
			return err
		}
		events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}}
		for _, step := range []struct {
			method string
			result any
		}{
			{"thread/archive", map[string]any{}},
			{"thread/start", map[string]any{"model": "gpt-test", "thread": map[string]any{"id": "replacement"}}},
			{"thread/inject_items", map[string]any{}},
			{"turn/start", map[string]any{"turn": map[string]any{"id": "continued"}}},
		} {
			if _, err := cloudExpectReply(a.ctx, wire, step.method, step.result); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "retry", Request: OpenAICloudRequest{Body: raw}}))
	script.join(t)
	require.Equal(t, "replacement", a.record.ThreadID)
	require.Equal(t, "root", a.record.RootRequestID)
	require.Equal(t, "continued", a.record.TurnID)
	require.Equal(t, "running", a.record.Status)
	require.Empty(t, a.tools)
	require.Empty(t, wire.writes)
	require.Empty(t, store.settlements, "handoff cannot settle a new root or replay tools")
}

func TestCloudHandoffRechecksLateHistoryBeforeArchive(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	a.service.validate = func(context.Context, OpenAICloudOwner) error { return nil }
	events := make(chan cloudRPCDelivery, 2)
	a.upstream = events
	script := runCloudScript(func() error {
		if _, err := cloudExpectReply(a.ctx, wire, "turn/interrupt", map[string]any{}); err != nil {
			return err
		}
		raw, err := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"type": "reasoning", "id": "late", "summary": []any{}, "encrypted_content": strings.Repeat("x", cloudInjectionMaxBytes)}})
		if err != nil {
			return err
		}
		events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "rawResponseItem/completed", Params: raw}}
		events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}}
		return nil
	})
	require.NoError(t, a.restartPendingTurn(OpenAICloudCommand{RequestID: "http"}, a.protocol, []any{cloudUserMessage("result")}))
	script.join(t)
	require.Empty(t, wire.writes, "late oversized history cannot archive the only retained upstream thread")
	require.Equal(t, "thread", a.record.ThreadID)
	require.Equal(t, "root", a.record.RootRequestID)
	require.Len(t, store.settlements, 1)
	require.True(t, store.settlements[0].LowerBound)
	require.False(t, store.settlements[0].Unknown)
	require.Equal(t, 12, store.settlements[0].Usage.TotalTokens)
	require.Equal(t, "cloud_history_item_too_large", gjson.GetBytes(store.record.Result.Response, "error.code").String())
}

func TestCloudTerminalStatusAndLocalStopAttribution(t *testing.T) {
	for _, status := range []string{"failed", "interrupted"} {
		for _, local := range []bool{false, true} {
			t.Run(status+"/local="+map[bool]string{true: "true", false: "false"}[local], func(t *testing.T) {
				a, store, wire := cloudHostedActorForTest(t)
				terminal := json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"` + status + `"}}`)
				if local {
					events := make(chan cloudRPCDelivery, 1)
					a.upstream = events
					script := runCloudScript(func() error {
						if _, err := cloudExpectReply(a.ctx, wire, "turn/interrupt", map[string]any{}); err != nil {
							return err
						}
						events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "turn/completed", Params: terminal}}
						return nil
					})
					a.stop("Cloud client cancelled the request")
					script.join(t)
				} else {
					require.NoError(t, a.handleEvent(cloudRPCMessage{Method: "turn/completed", Params: terminal}))
				}
				result := store.record.Result
				failure := gjson.GetBytes(result.Response, "error")
				require.Equal(t, "turn/completed", failure.Get("upstream_method").String())
				require.Equal(t, status, failure.Get("upstream_turn_status").String())
				require.True(t, failure.Get("usage_observed").Bool())
				require.Equal(t, "cloud_execution_failed", failure.Get("code").String())
				payload, httpStatus := cloudTerminalFailureFacts(result)
				if local {
					require.Equal(t, "client_cancelled", failure.Get("gateway_stop_reason").String())
					require.Empty(t, payload)
					require.Zero(t, httpStatus)
				} else {
					require.False(t, failure.Get("gateway_stop_reason").Exists())
					require.NotEmpty(t, payload)
					require.Equal(t, 502, httpStatus)
				}
				require.Len(t, store.settlements, 1)
				require.True(t, store.settlements[0].LowerBound)
				require.Equal(t, 12, store.settlements[0].Usage.TotalTokens)
			})
		}
	}
	// A real provider failure is still actionable when observed during local
	// cancellation; only the gateway's generic stop loses account attribution.
	a, store, _ := cloudHostedActorForTest(t)
	a.publicFailure = map[string]any{"gateway_stop_reason": "client_cancelled"}
	require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"codexErrorInfo": "unauthorized", "gateway_stop_reason": "forged"}}}, ""))
	require.Equal(t, "client_cancelled", gjson.GetBytes(store.record.Result.Response, "error.gateway_stop_reason").String())
	_, status := cloudTerminalFailureFacts(store.record.Result)
	require.Equal(t, 401, status)
}

func TestCloudCompactionStartDoesNotClaimCompletion(t *testing.T) {
	for _, completed := range []bool{false, true} {
		a, store, _ := cloudHostedActorForTest(t)
		a.record.Usage = nil
		item := map[string]any{"item": map[string]any{"id": "compact", "type": "contextCompaction"}}
		require.NoError(t, cloudHostedEventForTest(t, a, "item/started", item, ""))
		require.False(t, a.threadCompacted)
		require.False(t, a.encryptedHistoryRejectedBeforeInference(&cloudUpstreamError{Code: "invalid_compaction"}, "turn/completed"), "started compaction already prevents pre-inference replay")
		if completed {
			require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", item, ""))
		}
		a.record.Usage = &OpenAICloudUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120}
		a.lastStep = &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
		require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, ""))
		require.Equal(t, completed, a.threadCompacted)
		require.Equal(t, int64(12), gjson.GetBytes(store.record.Result.Response, "usage.total_tokens").Int(), "neither started nor completed compaction exposes cumulative root usage as context size")
		require.Equal(t, 120, store.settlements[0].Usage.TotalTokens, "public compaction projection never changes the root's measured usage")
	}
}

func TestCloudPendingHandoffPreflightsTurnSchemaBeforeInterrupt(t *testing.T) {
	for _, strict := range []bool{true, false} {
		for _, handoff := range []string{"catalog", "original_image"} {
			t.Run(fmt.Sprintf("strict=%v/%s", strict, handoff), func(t *testing.T) {
				// Encoding the full 64 MiB boundary under -race can exceed the
				// ordinary fixture's five seconds before the retry assertion.
				a, store, wire := cloudHostedActorForTest(t, 30*time.Second)
				// Model an existing pending owner with inherited output settings.
				// The schema fits the gateway request budget, but not turn/start.
				oldSource := cloneCloudValue(a.protocol.source).(map[string]any)
				oldSource["text"] = map[string]any{"format": map[string]any{
					"type": "json_schema", "name": "answer", "strict": strict,
					"schema": map[string]any{"type": "object", "description": strings.Repeat("\x00", OpenAICloudMaxRPCBytes/6+1)},
				}}
				var err error
				a.protocol, err = compileCloudProtocolRequest(oldSource, nil)
				require.NoError(t, err)
				a.requestID, a.lastResponseID = "", "resp_cloud_previous"
				a.record.Status = "awaiting_tools"
				a.history = []any{cloudUserMessage("go"), map[string]any{"type": "function_call", "call_id": "call_cloud_pending", "name": "echo", "arguments": "{}"}}
				a.tools["call_cloud_pending"] = &cloudPendingTool{Published: true, CallID: "call_cloud_pending", Name: "echo", RPCID: json.RawMessage(`"pending-rpc"`)}
				output := map[string]any{"type": "function_call_output", "call_id": "call_cloud_pending", "output": "ok"}
				body := map[string]any{"model": "gpt-test", "previous_response_id": a.lastResponseID, "input": []any{output}}
				if handoff == "catalog" {
					tools := cloneCloudValue(a.protocol.source["tools"]).([]any)
					body["tools"] = append(tools, map[string]any{"type": "function", "name": "discovered", "parameters": map[string]any{"type": "object"}})
				} else {
					output["output"] = []any{map[string]any{"type": "input_image", "detail": "original", "image_url": "data:image/png;base64,aW1hZ2U="}}
				}
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				oldProtocol, oldUsage, oldTelemetry, oldResponse := a.protocol, a.record.Usage, a.record.Telemetry, a.responseID
				err = a.handleCommand(OpenAICloudCommand{RequestID: "oversized", Request: OpenAICloudRequest{Body: raw}})
				status, code, _, known := cloudPublicError(err)
				require.True(t, known, "%v", err)
				require.Equal(t, 413, status)
				require.Equal(t, "cloud_message_too_large", code)
				require.Empty(t, wire.writes, "no interrupt, reply, archive or new turn before complete preflight")
				require.Empty(t, store.results)
				require.Empty(t, store.settlements)
				require.Nil(t, store.record, "preflight cannot write the owner record")
				require.Equal(t, "awaiting_tools", a.record.Status)
				require.Equal(t, "thread", a.record.ThreadID)
				require.Equal(t, "turn", a.record.TurnID)
				require.Equal(t, "root", a.record.RootRequestID)
				require.Same(t, oldProtocol, a.protocol)
				require.Same(t, oldUsage, a.record.Usage)
				require.Same(t, oldTelemetry, a.record.Telemetry)
				require.Equal(t, oldResponse, a.responseID)
				require.Empty(t, a.requestID)
				require.Len(t, a.history, 2)
				require.Len(t, a.tools, 1)

				// A normal result still resumes the original pending RPC. Rejection
				// must not consume its ownership or force a replacement generation.
				delete(body, "tools")
				output["output"] = "ok"
				raw, err = json.Marshal(body)
				require.NoError(t, err)
				require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "retry", Request: OpenAICloudRequest{Body: raw}}))
				reply := cloudTestMessage(t, a.ctx, wire)
				require.JSONEq(t, `"pending-rpc"`, string(reply.ID))
				require.Empty(t, reply.Method)
				require.Empty(t, wire.writes)
				require.Empty(t, a.tools)
				require.Equal(t, "running", a.record.Status)
				require.Equal(t, "root", a.record.RootRequestID)
				require.Empty(t, store.settlements)
			})
		}
	}
}

func TestCloudPendingHandoffPreflightMeasuresCompleteTurnEnvelope(t *testing.T) {
	for _, strict := range []bool{true, false} {
		t.Run(fmt.Sprintf("strict=%v", strict), func(t *testing.T) {
			p, err := parseCloudProtocolRequest([]byte(fmt.Sprintf(`{"model":"gpt-test","input":"go","reasoning":{"effort":"high","summary":"auto"},"text":{"format":{"type":"json_schema","name":"answer","strict":%v,"schema":{"type":"object","description":""}}}}`, strict)))
			require.NoError(t, err)
			threadID, messageID := strings.Repeat("\x00", 512), strings.Repeat("m", 36)
			turn, err := cloudTurnParams(p, threadID, messageID)
			require.NoError(t, err)
			encoded, err := cloudEncodeJSON(map[string]any{"id": ^uint64(0), "method": "turn/start", "params": turn})
			require.NoError(t, err)
			padding := strings.Repeat("x", OpenAICloudMaxRPCBytes-len(encoded))
			p.outputSchema["description"] = padding
			require.NoError(t, cloudPreflightPendingHandoff(p, nil), "complete envelope at the boundary is accepted")
			p.outputSchema["description"] = padding + "x"
			_, code, _, known := cloudPublicError(cloudPreflightPendingHandoff(p, nil))
			require.True(t, known)
			require.Equal(t, "cloud_message_too_large", code, "one extra encoded byte must reject the whole handoff")
		})
	}
}

//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudToolArgumentsExactIntegerProjection(t *testing.T) {
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"yield_time_ms":{"type":"integer"},"max_output_tokens":{"type":"integer"}}}}]}`))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"observed optional parameters", `{"yield_time_ms":7500.0,"max_output_tokens":1000.0}`, `{"yield_time_ms":7500,"max_output_tokens":1000}`},
		{"nested decimals and exponents", `{"a":[7.5e3,{"n":1000e-3},-12.000,0.01e2,1E+3]}`, `{"a":[7500,{"n":1},-12,1,1000]}`},
		{"exact large integers", `{"a":9007199254740993.0,"b":9.007199254740993e15,"c":18446744073709551615.0,"d":1844674407370955161600000.0}`, `{"a":9007199254740993,"b":9007199254740993,"c":18446744073709551615,"d":1844674407370955161600000}`},
		{"fractions never round", `[-7500.001,1.2300,1001e-3,9007199254740993.0000001,1e-99999]`, `[-7500.001,1.2300,1001e-3,9007199254740993.0000001,1e-99999]`},
		{"strings keys whitespace and escapes", " {\"7500.0\":\"7.5e3\",\"s\":\"\\u003c 7500.0 \\\"x\\\"\\n\",\"n\": 7500.0 } ", " {\"7500.0\":\"7.5e3\",\"s\":\"\\u003c 7500.0 \\\"x\\\"\\n\",\"n\": 7500 } "},
		{"duplicate keys are not collapsed", `{"n":7500.0,"n":1000.0}`, `{"n":7500,"n":1000}`},
		{"string root", `"{\"yield_time_ms\":7500.0}"`, `"{\"yield_time_ms\":7500.0}"`},
		{"non numeric values", `[null,true,false,"7500.0"]`, `[null,true,false,"7500.0"]`},
		{"zero with huge exponent", `[-0.000e999999999999999999999,0e-999999999999999999999]`, `[0,0]`},
		{"huge positive exponents", `[1e9223372036854775807,1e9223372036854775808,1e999999999999999999999]`, `[1e9223372036854775807,1e9223372036854775808,1e999999999999999999999]`},
		{"huge negative exponents", `[1e-9223372036854775808,1e-9223372036854775809]`, `[1e-9223372036854775808,1e-9223372036854775809]`},
		{"leading exponent zeros", `[7.5e+00003,1000e-00003]`, `[7500,1]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(tc.raw)
			require.True(t, json.Valid(raw))
			projected, err := p.projectToolArguments("exec_command", raw)
			require.NoError(t, err)
			require.Equal(t, tc.want, projected)
			require.Equal(t, tc.raw, string(raw), "the RPC bytes remain immutable")
			require.True(t, json.Valid([]byte(projected)))
			again, err := p.projectToolArguments("exec_command", json.RawMessage(projected))
			require.NoError(t, err)
			require.Equal(t, projected, again)
		})
	}
}

func TestCloudToolArgumentsStrictIntegerClient(t *testing.T) {
	// Typed decoding models the observed usize/u64 client failure, unlike
	// JSONEq or decoding into any, both of which hide numeric token differences.
	var client struct {
		YieldTimeMS     uint64 `json:"yield_time_ms"`
		MaxOutputTokens uint   `json:"max_output_tokens"`
		Nested          struct {
			IDs []uint64 `json:"ids"`
		} `json:"nested"`
	}
	raw := json.RawMessage(`{"yield_time_ms":7500.0,"max_output_tokens":1e3,"nested":{"ids":[9007199254740993.0,18446744073709551615.0]}}`)
	require.Error(t, json.Unmarshal(raw, &client), "unprojected arguments reproduce the strict integer failure")
	p := &cloudProtocolRequest{}
	projected, err := p.projectToolArguments("exec_command", raw)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(projected), &client))
	require.Equal(t, uint64(7500), client.YieldTimeMS)
	require.Equal(t, uint(1000), client.MaxOutputTokens)
	require.Equal(t, []uint64{9007199254740993, 18446744073709551615}, client.Nested.IDs)
	for _, invalidInteger := range []string{`7500.01`, `-1.0`, `18446744073709551616.0`, `"7500.0"`} {
		arguments, err := p.projectToolArguments("exec_command", json.RawMessage(`{"yield_time_ms":`+invalidInteger+`}`))
		require.NoError(t, err)
		require.Error(t, json.Unmarshal([]byte(arguments), &client), "fractions, negatives, overflow and strings must not be coerced into uint64")
	}
}

func TestCloudToolArgumentsExponentExpansionBounded(t *testing.T) {
	for _, tc := range []struct {
		number, want string
	}{
		{"1e3", "1000"},
		{"-1e3", "-1000"},
		{"123000e-3", "123"},
		{"1e19", "10000000000000000000"},
		{"2e19", "2e19"},
		{"-9e18", "-9000000000000000000"},
		{"-1e19", "-1e19"},
		{"1e20", "1e20"},
		{"1e16777209", "1e16777209"},
		{"1e9223372036854775807", "1e9223372036854775807"},
		{"1e-9223372036854775808", "1e-9223372036854775808"},
		{"1e" + strings.Repeat("9", 4096), "1e" + strings.Repeat("9", 4096)},
	} {
		require.Equal(t, tc.want, cloudIntegralToolNumber(tc.number))
	}
	// Even when the entire result allowance is available, neither token may
	// turn a short exponent into megabytes of integer text.
	exponent := OpenAICloudMaxResultBytes / 2
	raw := json.RawMessage(fmt.Sprintf(`[1e%d,1e%d]`, exponent, exponent))
	projected, err := (&cloudProtocolRequest{}).projectToolArguments("exec_command", raw)
	require.NoError(t, err)
	require.Equal(t, string(raw), projected)
	require.True(t, json.Valid([]byte(projected)))
}

func TestCloudToolArgumentsHugeExponentsReachTerminalAndReplay(t *testing.T) {
	for _, number := range []string{"1e16777209", "1e8388608", "1e20", "1e9223372036854775807", "1e-9223372036854775808", "1e" + strings.Repeat("9", 64)} {
		t.Run(number, func(t *testing.T) {
			a, store, wire := cloudHostedActorForTest(t)
			a.history = []any{cloudUserMessage("go")}
			require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "prior", "type": "agentMessage", "text": "already delivered <>&\"\n"}}, ""))
			arguments := `{"n":` + number + `}`
			require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "upstream-call", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": json.RawMessage(arguments)}, `"rpc"`))
			result := a.record.Result
			require.NotNil(t, result, "the real publication must reach response.completed")
			require.True(t, result.AwaitingTools)
			require.False(t, result.Terminal)
			require.Less(t, len(result.Response), 4096, "a tiny RPC cannot consume the response budget")
			call := gjson.GetBytes(result.Response, "output.1")
			callID := call.Get("call_id").String()
			require.Equal(t, arguments, call.Get("arguments").String())
			require.Equal(t, arguments, a.tools[callID].Arguments)
			require.Equal(t, arguments, a.history[len(a.history)-1].(map[string]any)["arguments"])
			require.NoError(t, a.replayCompletedResponse("retry-huge", result))
			counts := map[string]int{}
			for _, event := range store.results {
				kind := gjson.GetBytes(event.Event, "type").String()
				path := ""
				switch kind {
				case "response.function_call_arguments.delta":
					path = "delta"
				case "response.function_call_arguments.done":
					path = "arguments"
				case "response.output_item.done":
					if gjson.GetBytes(event.Event, "item.call_id").String() == callID {
						path = "item.arguments"
					}
				case "response.completed":
					path = "response.output.1.arguments"
				}
				if path != "" {
					counts[kind]++
					require.Equal(t, arguments, gjson.GetBytes(event.Event, path).String())
				}
				envelope, err := json.Marshal(event)
				require.NoError(t, err)
				require.Less(t, len(envelope), 8192)
			}
			require.Equal(t, map[string]int{"response.function_call_arguments.delta": 2, "response.function_call_arguments.done": 2, "response.output_item.done": 2, "response.completed": 2}, counts)
			require.Empty(t, wire.writes)
			require.Empty(t, store.settlements, "tool waiting/replay does not settle the root")
		})
	}
}

func TestCloudToolArgumentsPublicationResultBoundary(t *testing.T) {
	text, err := json.Marshal(strings.Repeat("<>&\"\\\n\u2028", 2048))
	require.NoError(t, err)
	raw := `{"n":1e3,"s":` + string(text) + `}`
	want := strings.Replace(raw, "1e3", "1000", 1)
	prepare := func(t *testing.T) (*cloudSessionActor, *cloudActorTestStore, *cloudRPCFakeConn) {
		t.Helper()
		a, store, wire := cloudHostedActorForTest(t, 30*time.Second)
		p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"namespace","name":"local","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}]}`))
		require.NoError(t, err)
		a.protocol = p
		a.history = []any{cloudUserMessage("go")}
		a.metadata = map[string]any{"pad": "", "escaped": "<>&\"\n"}
		a.lastStep = &OpenAICloudUsage{InputTokens: 7, OutputTokens: 2, TotalTokens: 9}
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "prior", "type": "agentMessage", "text": "already delivered <>&\"\n"}}, ""))
		require.NoError(t, a.flushPendingMessages())
		// Real RPCs can queue between HTTP tool-continuation requests. Publish
		// the complete batch together so its combined cost is exercised.
		a.requestID = ""
		name := p.tools[0].(map[string]any)["name"].(string)
		for i := range 2 {
			require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": fmt.Sprintf("up-%d", i), "namespace": cloudClientToolNamespace, "tool": name, "arguments": json.RawMessage(raw)}, fmt.Sprintf(`"rpc-%d"`, i)))
		}
		a.requestID = "http"
		return a, store, wire
	}
	// Calibrate from an actual completed publication, not a copy of the size
	// checker. UUID contents vary but their encoded lengths are fixed.
	reference, _, _ := prepare(t)
	require.NoError(t, reference.finishAwaiting())
	baseBytes := len(reference.record.Result.Response)
	for _, tc := range []struct {
		name  string
		extra int
	}{
		{"one_byte_below", -1},
		{"exact_limit", 0},
		{"one_byte_over", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, store, wire := prepare(t)
			a.metadata.(map[string]any)["pad"] = strings.Repeat("x", OpenAICloudMaxResultBytes-baseBytes+tc.extra)
			eventsBefore, historyBefore, sequenceBefore := len(store.results), len(a.history), a.sequence
			err := a.finishAwaiting()
			if tc.extra > 0 {
				require.EqualError(t, err, "Cloud response exceeded its size limit")
				require.Len(t, store.results, eventsBefore, "reject before publishing any tool identity or arguments")
				require.Len(t, a.output, 1, "previously delivered output remains intact")
				require.Len(t, a.history, historyBefore)
				require.Equal(t, sequenceBefore, a.sequence)
				require.Nil(t, a.record.Result)
				require.False(t, a.lastStepPublished)
				require.Empty(t, a.record.TurnCallIDs)
				for _, tool := range a.tools {
					require.False(t, tool.Published)
					require.Equal(t, want, tool.Arguments)
				}
			} else {
				require.NoError(t, err)
				result := a.record.Result
				require.NotNil(t, result)
				require.True(t, result.AwaitingTools)
				require.Equal(t, OpenAICloudMaxResultBytes+tc.extra, len(result.Response))
				require.Equal(t, "completed", gjson.GetBytes(result.Response, "status").String())
				require.Equal(t, int64(9), gjson.GetBytes(result.Response, "usage.total_tokens").Int())
				require.True(t, a.lastStepPublished)
				require.Len(t, a.history, historyBefore+2)
				for i := 1; i <= 2; i++ {
					call := gjson.GetBytes(result.Response, fmt.Sprintf("output.%d", i))
					require.Equal(t, "local", call.Get("namespace").String())
					require.Equal(t, want, call.Get("arguments").String())
					require.Equal(t, want, a.tools[call.Get("call_id").String()].Arguments)
					require.Equal(t, want, a.history[historyBefore+i-1].(map[string]any)["arguments"])
				}
				terminal, streamBytes := 0, 0
				for _, event := range store.results {
					envelope, err := json.Marshal(event)
					require.NoError(t, err)
					require.LessOrEqual(t, len(envelope), OpenAICloudMaxEnvelopeBytes)
					streamBytes += len(envelope)
					if gjson.GetBytes(event.Event, "type").String() == "response.completed" {
						terminal++
						require.Equal(t, want, gjson.GetBytes(event.Event, "response.output.1.arguments").String())
						require.Greater(t, len(event.Event), len(result.Response), "the complete public event needs envelope headroom")
					}
				}
				require.Equal(t, 1, terminal)
				require.Less(t, streamBytes, OpenAICloudMaxEventStreamBytes)
			}
			require.Equal(t, "root", a.record.RootRequestID)
			require.Equal(t, 12, a.record.Usage.TotalTokens)
			require.Empty(t, wire.writes)
			require.Empty(t, store.settlements)
		})
	}
}

func TestCloudToolArgumentsPublicationEnvelopeEscaping(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t, 30*time.Second)
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"namespace","name":"local","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}]}`))
	require.NoError(t, err)
	a.protocol = p
	// Namespace restoration emits literal '<' in the public body, but the
	// store's JSON envelope must escape it again. The body alone fits easily.
	a.metadata = map[string]any{"pad": strings.Repeat("<", OpenAICloudMaxEnvelopeBytes/6+1)}
	name := p.tools[0].(map[string]any)["name"].(string)
	err = cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "up", "namespace": cloudClientToolNamespace, "tool": name, "arguments": json.RawMessage(`{"n":1e3}`)}, `"rpc"`)
	require.EqualError(t, err, "Cloud response event exceeded its size limit")
	require.Empty(t, store.results)
	require.Empty(t, a.output)
	require.Empty(t, a.history)
	require.Nil(t, a.record.Result)
	require.Len(t, a.tools, 1)
	for _, tool := range a.tools {
		require.False(t, tool.Published)
		require.Equal(t, `{"n":1000}`, tool.Arguments)
	}
	require.Empty(t, wire.writes)
	require.Empty(t, store.settlements)
}

func TestCloudToolArgumentsPublicationReplayAndContinuation(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"namespace","name":"local","tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"yield_time_ms":{"type":"integer"},"max_output_tokens":{"type":"integer"}}}}]}]}`))
	require.NoError(t, err)
	a.protocol = p
	a.history = []any{cloudUserMessage("go")}
	a.record.BillingSnapshot = json.RawMessage(`{"root":"original"}`)
	name := p.tools[0].(map[string]any)["name"].(string)
	const raw = `{"yield_time_ms":7500.0,"max_output_tokens":1e3,"id":9007199254740993.0,"fraction":1.125}`
	const want = `{"yield_time_ms":7500,"max_output_tokens":1000,"id":9007199254740993,"fraction":1.125}`
	const rpcID = `9007199254740993`
	require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{
		"callId": "upstream-call", "namespace": cloudClientToolNamespace, "tool": name, "arguments": json.RawMessage(raw),
	}, rpcID))
	result := a.record.Result
	require.NotNil(t, result)
	require.True(t, result.AwaitingTools)
	call := gjson.GetBytes(result.Response, "output.0")
	callID := call.Get("call_id").String()
	require.Equal(t, "local", call.Get("namespace").String())
	require.Equal(t, "exec_command", call.Get("name").String())
	require.Equal(t, want, call.Get("arguments").String())
	require.True(t, strings.HasPrefix(callID, "call_cloud_"))
	require.Equal(t, rpcID, string(a.tools[callID].RPCID))
	require.Equal(t, want, a.tools[callID].Arguments)
	require.Equal(t, want, a.history[1].(map[string]any)["arguments"])
	require.True(t, store.refs[callID])
	require.Empty(t, store.settlements)
	assertEvents := func(events []OpenAICloudStreamEvent) {
		t.Helper()
		counts := map[string]int{}
		for _, event := range events {
			kind := gjson.GetBytes(event.Event, "type").String()
			path := ""
			switch kind {
			case "response.function_call_arguments.delta":
				path = "delta"
			case "response.function_call_arguments.done":
				path = "arguments"
			case "response.output_item.done":
				path = "item.arguments"
				require.Equal(t, callID, gjson.GetBytes(event.Event, "item.call_id").String())
			case "response.completed":
				path = "response.output.0.arguments"
			}
			if path != "" {
				counts[kind]++
				arguments := gjson.GetBytes(event.Event, path).String()
				require.Equal(t, want, arguments)
				var client struct {
					YieldTimeMS     uint64 `json:"yield_time_ms"`
					MaxOutputTokens uint   `json:"max_output_tokens"`
					ID              uint64 `json:"id"`
				}
				require.NoError(t, json.Unmarshal([]byte(arguments), &client))
				require.Equal(t, uint64(7500), client.YieldTimeMS)
				require.Equal(t, uint(1000), client.MaxOutputTokens)
				require.Equal(t, uint64(9007199254740993), client.ID)
			}
		}
		require.Equal(t, map[string]int{"response.function_call_arguments.delta": 1, "response.function_call_arguments.done": 1, "response.output_item.done": 1, "response.completed": 1}, counts)
	}
	assertEvents(store.results)
	for range 2 {
		before := len(store.results)
		require.NoError(t, a.replayCompletedResponse("cached-retry", result))
		assertEvents(store.results[before:])
		require.Empty(t, wire.writes, "replay does not send RPCs")
		require.Empty(t, store.settlements)
	}
	// A client may retain the pre-projection spelling from an earlier version.
	// Full-history comparison must still accept exactly the same numeric values.
	var publicCall map[string]any
	require.NoError(t, json.Unmarshal([]byte(call.Raw), &publicCall))
	publicCall["arguments"] = raw
	body, err := json.Marshal(map[string]any{"model": "gpt-test", "previous_response_id": a.lastResponseID, "input": []any{
		cloudUserMessage("go"), publicCall, map[string]any{"type": "function_call_output", "call_id": callID, "output": "accepted"},
	}})
	require.NoError(t, err)
	request := OpenAICloudRequest{Owner: a.record.OpenAICloudOwner, Body: body, RootRequestID: "must-not-replace-root", BillingSnapshot: json.RawMessage(`{"root":"other"}`)}
	require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "continued", Request: request}))
	var reply struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
	}
	select {
	case raw := <-wire.writes:
		require.NoError(t, json.Unmarshal(raw, &reply))
	case <-a.ctx.Done():
		t.Fatal("tool result reply did not reach the upstream")
	}
	require.Equal(t, rpcID, string(reply.ID))
	require.Empty(t, reply.Method)
	require.Equal(t, "accepted", gjson.GetBytes(reply.Result, "contentItems.0.text").String())
	require.Equal(t, "root", a.record.RootRequestID)
	require.Equal(t, `{"root":"original"}`, string(a.record.BillingSnapshot))
	require.Empty(t, a.tools)
	require.Empty(t, store.settlements)
	require.NoError(t, cloudHostedEventForTest(t, a, "thread/tokenUsage/updated", map[string]any{"tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 12}}}, ""))
	require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, ""))
	require.Len(t, store.settlements, 1)
	require.Equal(t, "root", store.settlements[0].RootRequestID)
	require.False(t, store.settlements[0].Unknown)
	require.Equal(t, 12, store.settlements[0].Usage.TotalTokens)
	require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "continued-retry", Request: request}))
	require.Empty(t, wire.writes, "cached continuation cannot answer the RPC twice")
	require.Len(t, store.settlements, 1)
}

func TestCloudToolArgumentsCustomInputUnchanged(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"namespace","name":"local","tools":[{"type":"custom","name":"apply_patch"}]}]}`))
	require.NoError(t, err)
	a.protocol = p
	name := p.tools[0].(map[string]any)["name"].(string)
	require.True(t, p.customTools[name])
	const input = "*** Begin Patch\n+{\"yield_time_ms\":7500.0,\"x\":1e999999}\n*** End Patch\n"
	raw, err := json.Marshal(map[string]any{"input": input})
	require.NoError(t, err)
	projected, err := p.projectToolArguments(name, raw)
	require.NoError(t, err)
	require.Equal(t, string(raw), projected)
	// Even a custom tool's noncanonical JSON fallback is opaque input text.
	fallback := json.RawMessage(`{"n":7500.0,"x":1e999999}`)
	projected, err = p.projectToolArguments(name, fallback)
	require.NoError(t, err)
	require.Equal(t, string(fallback), projected)
	require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "up-custom", "namespace": cloudClientToolNamespace, "tool": name, "arguments": json.RawMessage(raw)}, `"custom-rpc"`))
	result := a.record.Result
	require.Equal(t, input, gjson.GetBytes(result.Response, "output.0.input").String())
	require.Equal(t, "local", gjson.GetBytes(result.Response, "output.0.namespace").String())
	require.NoError(t, a.replayCompletedResponse("retry-custom", result))
	counts := map[string]int{}
	for _, event := range store.results {
		kind := gjson.GetBytes(event.Event, "type").String()
		path := ""
		switch kind {
		case "response.custom_tool_call_input.delta":
			path = "delta"
		case "response.custom_tool_call_input.done":
			path = "input"
		case "response.output_item.done":
			path = "item.input"
		case "response.completed":
			path = "response.output.0.input"
		}
		if path != "" {
			counts[kind]++
			require.Equal(t, input, gjson.GetBytes(event.Event, path).String())
		}
	}
	require.Equal(t, map[string]int{"response.custom_tool_call_input.delta": 2, "response.custom_tool_call_input.done": 2, "response.output_item.done": 2, "response.completed": 2}, counts)
}

func TestCloudToolSearchArgumentsPublicationAndReplay(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"tool_search","execution":"client","parameters":{"type":"object","properties":{"limit":{"type":"integer"},"id":{"type":"integer"}}}}]}`))
	require.NoError(t, err)
	a.protocol = p
	require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{
		"callId": "up-search", "namespace": cloudClientToolNamespace, "tool": "tool_search",
		"arguments": json.RawMessage(`{"limit":7500.0,"id":9007199254740993.0,"fraction":1.125}`),
	}, `"search-rpc"`))
	result := a.record.Result
	require.NotNil(t, result)
	require.True(t, result.AwaitingTools)
	require.Equal(t, "tool_search_call", gjson.GetBytes(result.Response, "output.0.type").String())
	require.NoError(t, a.replayCompletedResponse("retry-search", result))
	seen := 0
	for _, event := range store.results {
		kind := gjson.GetBytes(event.Event, "type").String()
		require.NotEqual(t, "response.function_call_arguments.delta", kind)
		require.NotEqual(t, "response.function_call_arguments.done", kind)
		path := ""
		if kind == "response.output_item.done" {
			path = "item.arguments"
		} else if kind == "response.completed" {
			path = "response.output.0.arguments"
		}
		if path != "" {
			seen++
			arguments := gjson.GetBytes(event.Event, path)
			require.Equal(t, "7500", arguments.Get("limit").Raw)
			require.Equal(t, "9007199254740993", arguments.Get("id").Raw)
			require.Equal(t, "1.125", arguments.Get("fraction").Raw)
		}
	}
	require.Equal(t, 4, seen)
	require.Empty(t, wire.writes)
	require.Empty(t, store.settlements)
}

func TestCloudCachedToolSearchArgumentsPreserveNumbers(t *testing.T) {
	store := &cloudActorTestStore{}
	a := &cloudSessionActor{service: &OpenAICloudSessions{store: store}, ctx: t.Context()}
	const response = `{"id":"resp_cloud_cached","status":"completed","output":[{"id":"tsc_cached","type":"tool_search_call","call_id":"call_cloud_search","execution":"client","status":"completed","arguments":{"limit":7500,"ids":[9007199254740993,18446744073709551615],"fraction":9007199254740993.0000001,"huge":1e999999,"text":"7500.0"}}]}`
	result := &OpenAICloudResult{Response: json.RawMessage(response), RootRequestID: "root", AwaitingTools: true}
	for range 2 {
		before := len(store.results)
		require.NoError(t, a.replayCompletedResponse("retry-search", result))
		seen := 0
		for _, event := range store.results[before:] {
			kind := gjson.GetBytes(event.Event, "type").String()
			prefix := ""
			switch kind {
			case "response.output_item.done":
				prefix = "item.arguments"
			case "response.completed":
				prefix = "response.output.0.arguments"
			}
			if prefix != "" {
				seen++
				arguments := gjson.GetBytes(event.Event, prefix)
				require.Equal(t, "9007199254740993", arguments.Get("ids.0").Raw)
				require.Equal(t, "18446744073709551615", arguments.Get("ids.1").Raw)
				require.Equal(t, "9007199254740993.0000001", arguments.Get("fraction").Raw)
				require.Equal(t, "1e999999", arguments.Get("huge").Raw)
				require.Equal(t, "7500.0", arguments.Get("text").String())
				var client struct {
					Limit uint64   `json:"limit"`
					IDs   []uint64 `json:"ids"`
				}
				require.NoError(t, json.Unmarshal([]byte(arguments.Raw), &client))
				require.Equal(t, uint64(7500), client.Limit)
				require.Equal(t, []uint64{9007199254740993, 18446744073709551615}, client.IDs)
			}
		}
		require.Equal(t, 2, seen)
		require.Equal(t, response, string(result.Response))
		require.Empty(t, store.settlements)
		require.Nil(t, store.record)
	}
	before := len(store.results)
	err := a.replayCompletedResponse("corrupt", &OpenAICloudResult{Response: json.RawMessage(response + `{}`)})
	require.ErrorIs(t, err, errCloudCachedResponseInvalid, "lossless decoding must not accept trailing JSON")
	require.Len(t, store.results, before)
}

//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloudParserPlanNonExecutionFields(t *testing.T) {
	cases := map[string]string{
		"sampling":       `"temperature":0.7,"top_p":0.9,"max_output_tokens":99,"max_tool_calls":4`,
		"new_client":     `"access_programs":["future"],"future_client_hint":{"version":2}`,
		"reasoning":      `"reasoning":{"effort":"high","context":"all_turns","generate_summary":true}`,
		"format":         `"text":{"format":{"type":"json_schema","name":"answer","strict":true,"description":"hint","schema":{"type":"object","additionalProperties":false}}}`,
		"include":        `"include":["reasoning.encrypted_content","future.telemetry"]`,
		"stream_options": `"stream_options":{"future":true,"reasoning_summary_delivery":"future"}`,
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"go",` + fields + `}`))
			require.NoError(t, err)
			require.NotEmpty(t, p.input)
		})
	}
}

func TestCloudParserPlanPastCountLimits(t *testing.T) {
	tools := make([]any, 257)
	for i := range tools {
		tools[i] = map[string]any{"type": "function", "name": fmt.Sprintf("f%d", i), "parameters": map[string]any{"type": "object"}}
	}
	items := make([]any, 8193)
	for i := range items {
		items[i] = map[string]any{"role": "user", "content": "go"}
	}
	raw, err := json.Marshal(map[string]any{"model": "m", "tools": tools, "input": items})
	require.NoError(t, err)
	p, err := parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	require.Len(t, p.tools, 257)
	require.Len(t, p.history, 8192)
}

func TestCloudParserPlanNoCipherReasoningNotInjected(t *testing.T) {
	p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":[{"type":"reasoning","id":"rs_old","summary":[]},{"role":"user","content":"go"}]}`))
	require.NoError(t, err)
	require.Len(t, p.history, 1, "public history retained")
	require.Empty(t, cloudInjectionHistory(p))
}

func TestCloudParserPlanIncludeDigestAndTrigger(t *testing.T) {
	a, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"go"}`))
	require.NoError(t, err)
	b, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"go","include":["web_search_call.action.sources"]}`))
	require.NoError(t, err)
	require.NotEqual(t, a.settings, b.settings)
	require.Equal(t, a.threadSettings, b.threadSettings)
	c, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":[{"type":"compaction_trigger"},{"role":"user","content":"go"},{"type":"compaction_trigger"}]}`))
	require.NoError(t, err)
	require.True(t, c.compaction)
}

func TestCloudParserPlanReusesNativeSchemaRepairs(t *testing.T) {
	raw := []byte(`{"model":"m","input":"go","tools":[{"type":"function","name":"echo","parameters":{"oneOf":[{"type":"object","properties":{"x":{"type":"string","pattern":"(?=a)a"}},"required":null},{"anyOf":[{"type":"object"}]}],"default":{"pattern":"(?=literal)","required":null},"$defs":{"inner":{"type":"object","required":null,"properties":{"x":{"type":"string","pattern":"(?<!a)b"}}}}}}]}`)
	native, _, err := sanitizeOpenAIResponsesToolSchemasForPlatform(raw, PlatformOpenAI)
	require.NoError(t, err)
	var expected map[string]any
	require.NoError(t, decodeOpenAIJSONUseNumber(native, &expected))
	p, err := parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	require.Equal(t, expected["tools"].([]any)[0].(map[string]any)["parameters"], p.source["tools"].([]any)[0].(map[string]any)["parameters"])
}

func TestCloudParserPlanDigestUsesEffectiveSettings(t *testing.T) {
	base, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"go"}`))
	require.NoError(t, err)
	for _, extra := range []string{`"text":{"format":{"type":"text"}}`, `"include":["future.hint"]`, `"include":[]`, `"reasoning":null`, `"temperature":0.1`} {
		other, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"go",` + extra + `}`))
		require.NoError(t, err)
		require.Equal(t, base.settings, other.settings, extra)
		require.Equal(t, base.threadSettings, other.threadSettings, extra)
	}
}

func TestCloudParserPlanNeverDropsMisplacedExecutionFields(t *testing.T) {
	for _, raw := range []string{
		`{"model":"m","input":[{"role":"user","content":"go","encrypted_function_args":"opaque"}]}`,
		`{"model":"m","input":"go","tools":[{"type":"function","name":"echo","parameters":{"type":"object"},"input_schema":{"type":"string"}}]}`,
		`{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"go","encrypted_content":"opaque"}]}]}`,
	} {
		_, err := parseCloudProtocolRequest([]byte(raw))
		require.Error(t, err, "execution fields cannot be silently reduced to hints")
	}
}

func TestCloudParserPlanLimitErrorsAreSpecific(t *testing.T) {
	_, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"go","metadata":` + strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34) + `}`))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "duplicate")
	require.Contains(t, err.Error(), "depth")
}

func TestCloudParserPlanCompleteCodexFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/cloud_codex_0_164_catalog_shapes.json")
	require.NoError(t, err)
	var fixture struct {
		Requests map[string]json.RawMessage `json:"requests"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	for _, kind := range []string{"lite", "standard"} {
		t.Run(kind, func(t *testing.T) {
			p, err := parseCloudProtocolRequest(fixture.Requests[kind])
			require.NoError(t, err)
			require.NotEmpty(t, p.tools)
		})
	}
}

func TestCloudParserPlanResolvePreservesPendingOwnerAfterFiltering(t *testing.T) {
	store := newCloudLiveRoutingStore()
	store.record = &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{
		OpenAICloudOwner: OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4},
		SessionID:        "s", Status: "awaiting_tools", ExpiresAt: time.Now().Add(time.Hour)}, LeaseUntil: time.Now().Add(time.Hour)}
	store.refs["call_cloud_past"] = true
	sessions := &OpenAICloudSessions{store: store}
	binding, err := sessions.Resolve(context.Background(), 1, 2, 3, []byte(`{"model":"m","input":[{"type":"function_call","call_id":"call_cloud_past","name":"echo","arguments":"{}"},{"type":"function_call_output","call_id":"call_cloud_past","output":"ok"},{"type":"tool_search_output","call_id":"ignored","status":"completed"}]}`))
	require.NoError(t, err)
	require.NotNil(t, binding, "filtered foreign activity must not detach a live tool reply from its pending owner")
}

func TestCloudParserPlanToolReplyDoesNotPinHistoricalCloudSession(t *testing.T) {
	for _, tc := range []struct {
		name        string
		pastLive    bool
		currentCall string
		currentLive bool
	}{
		{"expired history followed by native tool", false, "call_native_current", false},
		{"completed history followed by native tool", true, "call_native_current", false},
		{"completed history followed by another Cloud owner", true, "call_cloud_current", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newCloudMultiRoutingStore()
			addSession := func(id, callID, status string) {
				record := &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{
					OpenAICloudOwner: OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4},
					SessionID:        id, Status: status, ExpiresAt: time.Now().Add(time.Hour)}, LeaseUntil: time.Now().Add(time.Hour)}
				require.NoError(t, store.CreateCloudSession(t.Context(), record))
				store.session(id).refs[callID] = true
			}
			if tc.pastLive {
				addSession("past", "call_cloud_past", "completed")
			}
			if tc.currentLive {
				addSession("current", tc.currentCall, "awaiting_tools")
			}
			raw := []byte(fmt.Sprintf(`{"model":"m","input":[
				{"role":"user","content":"first turn"},
				{"type":"function_call","call_id":"call_cloud_past","name":"echo","arguments":"{}"},
				{"type":"function_call_output","call_id":"call_cloud_past","output":"old result"},
				{"role":"assistant","content":"first turn complete"},
				{"role":"user","content":"next turn"},
				{"type":"function_call","call_id":%q,"name":"echo","arguments":"{}"},
				{"type":"function_call_output","call_id":%q,"output":"current result"}
			]}`, tc.currentCall, tc.currentCall))
			sessions := &OpenAICloudSessions{store: store}
			binding, err := sessions.Resolve(t.Context(), 1, 2, 3, raw)
			require.NoError(t, err)
			if tc.currentLive {
				require.NotNil(t, binding)
				require.Equal(t, "current", binding.SessionID)
			} else {
				require.Nil(t, binding, "a paired call in old history cannot own the current tool result")
			}
		})
	}
}

func TestCloudParserPlanParallelToolReplyDistinguishesPortableHistory(t *testing.T) {
	for _, cloudLast := range []bool{false, true} {
		for _, paired := range []bool{false, true} {
			t.Run(fmt.Sprintf("cloud_last_%t_paired_%t", cloudLast, paired), func(t *testing.T) {
				outputs := []any{
					map[string]any{"type": "function_call_output", "call_id": "call_cloud_current", "output": "cloud result"},
					map[string]any{"type": "function_call_output", "call_id": "call_native_current", "output": "native result"},
				}
				if cloudLast {
					outputs[0], outputs[1] = outputs[1], outputs[0]
				}
				items := []any{cloudUserMessage("parallel tools")}
				if paired {
					items = append(items, map[string]any{"type": "function_call", "call_id": "call_cloud_current", "name": "echo", "arguments": "{}"})
				}
				items = append(items, map[string]any{"type": "function_call", "call_id": "call_native_current", "name": "echo", "arguments": "{}"})
				raw, err := json.Marshal(map[string]any{"model": "m", "input": append(items, outputs...)})
				require.NoError(t, err)
				p, err := parseCloudProtocolRequest(raw)
				require.NoError(t, err)
				sessions := &OpenAICloudSessions{store: newCloudLiveRoutingStore()}
				binding, err := sessions.resolveProtocol(t.Context(), 1, 2, 3, p)
				if paired {
					require.NoError(t, err, "full paired history can migrate without an extra user message")
					require.Nil(t, binding)
				} else {
					require.ErrorIs(t, err, errCloudToolCallExpired, "a parallel result batch cannot hide an orphan Cloud result")
				}
			})
		}
	}
}

func TestCloudParserPlanDiagnosticDecisions(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"decisions": []any{map[string]any{"path": "reasoning.context", "action": "drop", "matrix_id": "CLOUD-REQ"}}})
	events, _ := json.Marshal([]any{map[string]any{"kind": "cloud_adaptation", "detail": string(payload)}})
	diagnostics, err := ParseOpsUpstreamErrors(string(events))
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	require.Equal(t, "cloud_adaptation", diagnostics[0].Kind)
	exported, _ := json.Marshal(diagnostics)
	require.Contains(t, string(exported), "reasoning.context")
	require.Contains(t, string(exported), "CLOUD-REQ")
}

func TestCloudEarlyProjectionDefersTargetValidation(t *testing.T) {
	source, err := decodeCloudProtocolBody([]byte(`{"model":"m","previous_response_id":"resp_cloud_old","reasoning":{"context":"all_turns"},"tools":[{"type":"function","name":"echo","parameters":{"type":"object"},"input_schema":{}}],"input":"go"}`))
	require.NoError(t, err)
	projection, err := projectCloudIngressSource(source)
	require.NoError(t, err, "owner lookup must not enforce Cloud target capabilities")
	require.Equal(t, "resp_cloud_old", projection.previous)
	raw, err := json.Marshal(source)
	require.NoError(t, err)
	_, err = parseCloudProtocolRequest(raw)
	var rejected *cloudRejection
	require.ErrorAs(t, err, &rejected)
	found := false
	for _, decision := range rejected.Decisions {
		found = found || decision.Path == "reasoning.context"
	}
	require.True(t, found, "selected Cloud compilation must still validate and retain its adaptation decisions")
}

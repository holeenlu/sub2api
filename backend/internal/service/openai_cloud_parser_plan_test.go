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

func TestCloudParserPlanResolveUsesFilteredHistory(t *testing.T) {
	store := newCloudLiveRoutingStore()
	store.record = &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{
		OpenAICloudOwner: OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4},
		SessionID:        "s", Status: "completed", ExpiresAt: time.Now().Add(time.Hour)}, LeaseUntil: time.Now().Add(time.Hour)}
	store.refs["call_cloud_past"] = true
	sessions := &OpenAICloudSessions{store: store}
	binding, err := sessions.Resolve(context.Background(), 1, 2, 3, []byte(`{"model":"m","input":[{"type":"function_call","call_id":"call_cloud_past","name":"echo","arguments":"{}"},{"type":"function_call_output","call_id":"call_cloud_past","output":"ok"},{"type":"tool_search_output","call_id":"ignored","status":"completed"}]}`))
	require.NoError(t, err)
	require.NotNil(t, binding, "filtered foreign activity must not turn a tool reply into an independent new user turn")
}

func TestCloudParserPlanDiagnosticDecisions(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"decisions": []any{map[string]any{"path": "reasoning.context", "action": "drop", "matrix_id": "CLOUD-REQ"}}})
	events, _ := json.Marshal([]any{map[string]any{"kind": "cloud_adaptation", "detail": string(payload)}})
	diagnostics := BuildOpsRequestDiagnostics(&OpsErrorLogDetail{UpstreamErrors: string(events)})
	require.Len(t, diagnostics.Events, 1)
	require.Equal(t, "cloud_adaptation", diagnostics.Events[0].Kind)
	exported, _ := json.Marshal(diagnostics)
	require.Contains(t, string(exported), "reasoning.context")
	require.Contains(t, string(exported), "CLOUD-REQ")
}

func TestCloudEarlyProjectionRetainsPriorAdaptationDecisions(t *testing.T) {
	source, err := decodeCloudProtocolBody([]byte(`{"model":"m","previous_response_id":"resp_cloud_old","reasoning":{"context":"all_turns"},"tools":[{"type":"function","name":"echo","parameters":{"type":"object"},"input_schema":{}}],"input":"go"}`))
	require.NoError(t, err)
	_, err = projectCloudIngressSource(source)
	var rejected *cloudRejection
	require.ErrorAs(t, err, &rejected)
	found := false
	for _, decision := range rejected.Decisions {
		found = found || decision.Path == "reasoning.context"
	}
	require.True(t, found, "an early owner-projection error must retain decisions already applied")
}

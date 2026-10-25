//go:build unit

package service

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCloudCapabilityMatrixFixtures(t *testing.T) {
	document, err := os.ReadFile("../../../docs/cloud-parser/implementation-20261025.md")
	require.NoError(t, err)
	fixtures := map[string]string{
		"CLOUD-REQ":       `{"model":"m","input":"go","future_hint":1}`,
		"CLOUD-TOOL":      `{"model":"m","input":"go","tools":[{"type":"function","name":"echo","parameters":null}]}`,
		"CLOUD-CHOICE":    `{"model":"m","input":"go","tool_choice":"required"}`,
		"CLOUD-STORE":     `{"model":"m","input":"go","store":true}`,
		"CLOUD-FORMAT":    `{"model":"m","input":"go","text":{"format":{"type":"text","description":"hint"}}}`,
		"CLOUD-TIER":      `{"model":"m","input":"go","service_tier":"priority"}`,
		"CLOUD-SEARCH":    `{"model":"m","input":"go","tools":[{"type":"tool_search"}]}`,
		"CLOUD-LITE":      `{"model":"m","input":[{"type":"additional_tools","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]},{"role":"user","content":"go"}]}`,
		"CLOUD-CONFIG":    `{"model":"m","input":[{"type":"configuration_update","reasoning":{"effort":"low"}},{"role":"user","content":"go"}]}`,
		"CLOUD-AGENT":     `{"model":"m","input":[{"type":"agent_message","author":"worker","recipient":"main","content":[{"type":"input_text","text":"ready"}]},{"role":"user","content":"go"}]}`,
		"CLOUD-CODE":      `{"model":"m","input":"go","tools":[{"type":"code_mode"}]}`,
		"CLOUD-HISTORY":   `{"model":"m","input":[{"type":"future_activity"},{"role":"user","content":"go"}]}`,
		"CLOUD-CIPHER":    `{"model":"m","input":[{"type":"reasoning","summary":[]},{"role":"user","content":"go"}]}`,
		"CLOUD-IMAGE":     `{"model":"m","input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,"},{"type":"input_text","text":"go"}]}]}`,
		"CLOUD-PART":      `{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":{"count":2}}]}]}`,
		"CLOUD-META":      `{"model":"m","input":"go","prompt_cache_key":"cache-hint"}`,
		"CLOUD-REASONING": `{"model":"m","input":"go","reasoning":{"context":"all_turns"}}`,
		"CLOUD-INCLUDE":   `{"model":"m","input":"go","include":["future.telemetry"]}`,
		"CLOUD-CUSTOM":    `{"model":"m","input":"go","tools":[{"type":"custom","name":"patch","format":{"type":"grammar","syntax":"lark","definition":"start: /x/"}}]}`,
		"CLOUD-JSON":      `{"model":"m","model":"duplicate","input":"go"}`,
		"CLOUD-RECOVERY":  `[{"type":"reasoning","summary":[],"encrypted_content":"opaque-history"}]`,
	}
	ids := map[string]bool{}
	codes := map[string]bool{}
	for _, rule := range cloudCapabilities {
		require.Contains(t, string(document), "| "+rule.ID+" | "+rule.Action+" | "+rule.Category+" |", "matrix and implementation documentation must agree")
		require.False(t, ids[rule.ID], "duplicate matrix ID")
		ids[rule.ID] = true
		require.NotEmpty(t, rule.Scope)
		require.NotEmpty(t, rule.Path)
		require.NotEmpty(t, rule.Action)
		require.Contains(t, []string{"protocol-fact", "observed", "policy"}, rule.Category)
		require.NotEmpty(t, rule.Remedy)
		require.NotEmpty(t, rule.Evidence)
		require.NotEmpty(t, rule.ErrorCode)
		codes[rule.ErrorCode] = true
		raw, exists := fixtures[rule.ID]
		require.True(t, exists, "every matrix record needs a matching fixture: "+rule.ID)
		t.Run(rule.ID, func(t *testing.T) {
			if rule.ID == "CLOUD-RECOVERY" {
				var history []any
				require.NoError(t, json.Unmarshal([]byte(raw), &history))
				reduced, stage, changed := cloudReduceRejectedHistory(history, "invalid_encrypted_content")
				require.True(t, changed)
				require.Equal(t, "reasoning", stage)
				require.Empty(t, reduced)
				return
			}
			p, err := parseCloudProtocolRequest([]byte(raw))
			matched := false
			if err != nil {
				var rejected *cloudRejection
				require.ErrorAs(t, err, &rejected)
				matched = rejected.MatrixID == rule.ID
				for _, d := range rejected.Decisions {
					matched = matched || d.MatrixID == rule.ID
				}
				require.Contains(t, codes, rejected.Code)
			} else {
				for _, d := range p.decisions {
					matched = matched || d.MatrixID == rule.ID
				}
			}
			require.True(t, matched, "fixture must exercise its matrix record")
		})
	}
	require.Len(t, fixtures, len(cloudCapabilities))
	for _, value := range deriveCloudCodexToolCapabilities() {
		require.True(t, json.Valid(value))
	}
	require.Equal(t, "false", string(cloudCodexToolCapabilities["supports_search_tool"]))
	require.Equal(t, "false", string(cloudCodexToolCapabilities["use_responses_lite"]))
	require.Equal(t, "null", string(cloudCodexToolCapabilities["multi_agent_reasoning_effort"]))
}

func TestCloudCompilerDoesNotCallNativeTargetTransforms(t *testing.T) {
	forbidden := []string{"applyCodexOAuthTransformWithOptions", "filterCodexInputWithOptions", "applyCodexClientMetadata", "applyCodexAccountIdentity", "applyCodexFingerprint", "applyCodexRequestMetadata", "ensureOpenAIOAuthWebSearchToolForHistory", "normalizeCodexToolChoice"}
	files, err := filepath.Glob("openai_cloud*.go")
	require.NoError(t, err)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		require.NoError(t, err)
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch expr := call.Fun.(type) {
			case *ast.Ident:
				name = expr.Name
			case *ast.SelectorExpr:
				name = expr.Sel.Name
			}
			for _, prefix := range forbidden {
				require.False(t, strings.HasPrefix(name, prefix), file+" calls forbidden target transform "+name)
			}
			return true
		})
	}
}

func TestCloudProtocolIsDecodedOncePerOwnerProcess(t *testing.T) {
	raw := []byte(`{"model":"m","input":"go","prompt_cache_key":"hint"}`)
	ctx, state, err := cloudIngress(context.Background(), raw)
	require.NoError(t, err)
	require.NoError(t, validateCloudIngress(state))
	_, same, err := cloudIngress(ctx, raw)
	require.NoError(t, err)
	require.Same(t, state, same)
	p, err := parseCloudIngressSource(state.source)
	require.NoError(t, err)
	local := OpenAICloudRequest{Body: raw, protocol: p}
	first, err := cloudRequestProtocol(&local)
	require.NoError(t, err)
	second, err := cloudRequestProtocol(&local)
	require.NoError(t, err)
	require.Same(t, first, second)
	encoded, err := json.Marshal(OpenAICloudCommand{Request: local})
	require.NoError(t, err)
	var remote OpenAICloudCommand
	require.NoError(t, json.Unmarshal(encoded, &remote))
	require.Nil(t, remote.Request.protocol, "executable plans must never cross the owner transport")
	ownerFirst, err := cloudRequestProtocol(&remote.Request)
	require.NoError(t, err)
	ownerSecond, err := cloudRequestProtocol(&remote.Request)
	require.NoError(t, err)
	require.Same(t, ownerFirst, ownerSecond)
	require.NotSame(t, first, ownerFirst)
}

func TestCloudInjectionViewsHaveSameBytesAcrossIngress(t *testing.T) {
	raw, err := os.ReadFile("testdata/cloud_codex_0_164_catalog_shapes.json")
	require.NoError(t, err)
	var f struct {
		Requests map[string]map[string]any `json:"requests"`
	}
	require.NoError(t, json.Unmarshal(raw, &f))
	for _, shape := range []string{"standard", "lite"} {
		t.Run(shape, func(t *testing.T) {
			original := f.Requests[shape]
			input := original["input"].([]any)
			original["input"] = append(input[:len(input)-1], map[string]any{"type": "function_call_output", "name": "worker", "output": "delivered task"}, input[len(input)-1])
			raw, err := json.Marshal(original)
			require.NoError(t, err)
			ctx := PreserveCloudResponsesInput(context.Background(), raw, false)
			// Simulate the legacy handler bootstrap; Cloud must use the saved client
			// source rather than this already lowered native body.
			nativeBody := []byte(`{"model":"m","input":"different bootstrap representation"}`)
			_, httpState, err := cloudIngress(ctx, nativeBody)
			require.NoError(t, err)
			hp, err := parseCloudIngressSource(httpState.source)
			require.NoError(t, err)
			wsEnvelope := cloneCloudValue(original).(map[string]any)
			wsEnvelope["type"] = "response.create"
			wire, err := json.Marshal(wsEnvelope)
			require.NoError(t, err)
			_, wsState, err := cloudIngress(context.Background(), wire)
			require.NoError(t, err)
			wp, err := parseCloudIngressSource(wsState.source)
			require.NoError(t, err)
			hi, err := json.Marshal(cloudInjectionHistory(hp))
			require.NoError(t, err)
			wi, err := json.Marshal(cloudInjectionHistory(wp))
			require.NoError(t, err)
			require.Equal(t, string(hi), string(wi))
		})
	}
}

func TestCloudAdaptationDiagnosticsOmitContentAndBoundSize(t *testing.T) {
	payload := `{"decisions":[{"path":"reasoning.context","action":"drop","matrix_id":"CLOUD-REQ"},{"path":"private prompt with spaces","action":"drop","matrix_id":"CLOUD-REQ"},{"path":"input","action":"secret body","matrix_id":"CLOUD-REQ"},{"path":"input","action":"drop","matrix_id":"unknown"}]}`
	safe := cloudSafeAdaptationDetail(payload)
	require.NotNil(t, safe)
	require.Len(t, safe.Decisions, 1)
	require.Equal(t, 3, safe.Omitted)
	raw, err := json.Marshal(safe)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private prompt")
	require.NotContains(t, string(raw), "secret body")
	var decisions []cloudAdaptationDecision
	for i := 0; i < 1000; i++ {
		cloudDecision(&decisions, strings.Repeat("x", i+1), "drop", "CLOUD-REQ")
	}
	require.LessOrEqual(t, len(decisions), cloudAdaptationMaxDecisions)
	require.Positive(t, decisions[len(decisions)-1].Omitted)
}

func TestCloudAdaptationUsesExistingOpsAndRequestLogger(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "parser-contract")))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	_, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"private prompt","reasoning":{"context":"all_turns"},"tool_choice":"required"}`))
	require.Error(t, err)
	recordCloudAdaptation(c, nil, err)
	events := c.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Nil(t, LastOpsUpstreamAttempt(events), "local adaptation is not an upstream health attempt")
	for i := 0; i < opsUpstreamErrorsBodyWindow+1; i++ {
		events = append(events, &OpsUpstreamErrorEvent{Kind: "http_error", Message: "request failed", UpstreamStatusCode: 502})
	}
	entry := &OpsInsertErrorLogInput{UpstreamErrors: events}
	require.NoError(t, sanitizeOpsUpstreamErrors(entry))
	require.NotNil(t, entry.UpstreamErrorsJSON)
	diagnostics, err := ParseOpsUpstreamErrors(*entry.UpstreamErrorsJSON)
	require.NoError(t, err)
	require.Equal(t, cloudAdaptationKind, diagnostics[0].Kind)
	raw, err := json.Marshal(diagnostics[0])
	require.NoError(t, err)
	require.Contains(t, string(raw), "reasoning.context")
	require.Contains(t, string(raw), "CLOUD-CHOICE")
	require.NotContains(t, string(raw), "private prompt")
	recordCloudAdaptation(c, []cloudAdaptationDecision{{Path: "reasoning.context", Action: "drop", MatrixID: "CLOUD-REASONING"}}, nil)
	require.Equal(t, 1, logs.Len())
	require.Equal(t, "parser-contract", logs.All()[0].ContextMap()["request_id"])
}

func BenchmarkCloudRequestPlan(b *testing.B) {
	raw, err := os.ReadFile("testdata/cloud_codex_0_159_3.json")
	if err != nil {
		b.Fatal(err)
	}
	var fixture struct {
		Tools     json.RawMessage
		Scenarios map[string]struct {
			Frames []struct{ Request map[string]json.RawMessage }
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		b.Fatal(err)
	}
	request := fixture.Scenarios["http_tools"].Frames[0].Request
	request["tools"] = fixture.Tools
	raw, err = json.Marshal(request)
	if err != nil {
		b.Fatal(err)
	}
	p, err := parseCloudProtocolRequest(raw)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("compile", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := parseCloudProtocolRequest(raw); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("local_projection", func(b *testing.B) {
		r := OpenAICloudRequest{protocol: p}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := cloudRequestProtocol(&r); err != nil {
				b.Fatal(err)
			}
		}
	})
}

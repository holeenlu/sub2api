package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the real Forward -> repair driver -> pure conversion path. Harmless
// framing and unprovable metadata both avoid a second upstream generation.
func TestExcelBPSRawFunctionMetadataDriver(t *testing.T) {
	raw := `{"description":"PRIVATE_DESCRIPTION","timeoutMs":9007199254740993}`
	encoded, err := json.Marshal(raw)
	require.NoError(t, err)
	fence := strings.Repeat(string(rune(96)), 3)
	cases := []struct {
		name     string
		metadata any
		valid    bool
	}{
		{"object", raw, true},
		{"json_string", string(encoded), true},
		{"fence", fence + "json\n" + raw + "\n" + fence, true},
		{"label", "JSON: " + raw, true},
		{"missing", nil, false},
		{"non_string", map[string]any{"description": "PRIVATE_DESCRIPTION"}, false},
		{"empty", "", false},
		{"null", "null", false},
		{"array", "[]", false},
		{"description", "PRIVATE_HUMAN_DESCRIPTION", false},
		{"trailing", raw + " trailing", false},
		{"two_objects", raw + raw, false},
		{"duplicate_key", `{"description":"PRIVATE_A","description":"PRIVATE_B"}`, false},
		{"missing_separator", `{"description" "PRIVATE_MISSING_COLON"}`, false},
		{"raw_newline", "{\"description\":\"PRIVATE_RAW\nLINE\"}", false},
	}
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, field := range []string{"code", "cmd"} {
			for _, tc := range cases {
				t.Run(protocol+"/"+field+"/"+tc.name, func(t *testing.T) {
					name := "run_code"
					if field == "cmd" {
						name = "exec_command"
					}
					code := "  printf 'PRIVATE_UNCHANGED';\r\n"
					arguments, err := json.Marshal(map[string]any{
						"summary": "codex2api.function_" + field + "/" + name, "code": code,
						"extended_summary": tc.metadata, "references": []any{}, "destructive": false,
					})
					require.NoError(t, err)
					initial, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{
						"id": "resp_metadata", "status": "completed", "model": "gpt-6-astra",
						"usage": map[string]int{"input_tokens": 10, "output_tokens": 2},
						"output": []any{map[string]any{
							"type": "function_call", "name": "run_officejs", "id": "fc_metadata", "call_id": "call_metadata",
							"arguments": string(arguments), "status": "completed",
						}},
					}})
					require.NoError(t, err)
					first := bpsCompletionResponse(http.StatusOK, "data: "+string(initial)+"\n\n")
					first.Header.Set("X-Request-Id", "req_metadata_original")
					upstream := &httpUpstreamRecorder{resp: first}
					svc := excelBPSGatewayTestService(upstream)
					svc.cfg.Gateway.LogUpstreamErrorBody = true
					tool := map[string]any{"type": "function", "name": name, "parameters": map[string]any{
						"type": "object", "required": []string{field, "description"},
						"properties": map[string]any{field: map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "timeoutMs": map[string]any{"type": "number"}},
					}}
					body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "calculate", "stream": protocol != "json", "tools": []any{tool}})
					require.NoError(t, err)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					var result *OpenAIForwardResult
					var events []gjson.Result
					if protocol == "ws" {
						SetOpenAIClientTransport(c, OpenAIClientTransportWS)
						result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(event []byte) error {
							events = append(events, gjson.ParseBytes(event))
							return nil
						})
					} else {
						result, err = svc.Forward(t.Context(), c, excelAccount(), body)
						if protocol == "sse" {
							for _, line := range strings.Split(rec.Body.String(), "\n") {
								if strings.HasPrefix(line, "data: ") {
									events = append(events, gjson.Parse(strings.TrimPrefix(line, "data: ")))
								}
							}
						}
					}
					require.Len(t, upstream.requests, 1, "metadata framing must not spend a repair attempt")
					require.NotNil(t, result)
					require.Equal(t, 10, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					if protocol != "ws" {
						require.Equal(t, "req_metadata_original", result.RequestID)
					}
					response := gjson.Parse(rec.Body.String())
					if protocol != "json" {
						require.NotEmpty(t, events)
						response = events[len(events)-1].Get("response")
						terminals := 0
						for _, event := range events {
							if kind := event.Get("type").String(); kind == "response.completed" || kind == "response.failed" {
								terminals++
							}
							if !tc.valid {
								require.NotEqual(t, "response.output_item.added", event.Get("type").String())
								require.NotContains(t, event.Raw, "PRIVATE_")
							}
						}
						require.Equal(t, 1, terminals)
					}
					if tc.valid {
						require.NoError(t, err)
						require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
						require.Equal(t, "resp_metadata", response.Get("id").String())
						require.Len(t, response.Get("output").Array(), 1)
						call := response.Get("output.0")
						require.Equal(t, name, call.Get("name").String())
						require.Equal(t, "call_metadata", call.Get("call_id").String())
						args := gjson.Parse(call.Get("arguments").String())
						require.Equal(t, code, args.Get(field).String())
						require.Equal(t, "PRIVATE_DESCRIPTION", args.Get("description").String())
						require.Equal(t, "9007199254740993", args.Get("timeoutMs").Raw)
					} else {
						require.Error(t, err)
						require.Equal(t, "response.failed", result.UpstreamTerminalEvent)
						require.Empty(t, response.Get("output").Array())
						require.NotContains(t, rec.Body.String(), "PRIVATE_")
						value, ok := c.Get(OpsUpstreamErrorsKey)
						require.True(t, ok)
						items := value.([]*OpsUpstreamErrorEvent)
						require.Len(t, items, 1)
						require.Equal(t, "bridge_error", items[0].Kind)
						require.NotContains(t, fmt.Sprint(items[0]), "PRIVATE_")
						detail := gjson.Parse(gjson.Get(items[0].Detail, "error.param").String())
						require.Equal(t, "original_metadata_unverifiable", detail.Get("reason").String())
						require.True(t, detail.Get("original_metadata_shape").Exists())
						require.True(t, detail.Get("correction_attempt").Exists())
						require.Zero(t, detail.Get("correction_attempt").Int())
						require.False(t, detail.Get("executed").Bool())
					}
				})
			}
		}
	}
}

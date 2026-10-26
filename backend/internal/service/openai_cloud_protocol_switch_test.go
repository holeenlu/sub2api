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

// These are gateway round trips against protocol-shaped upstream fixtures.
// Source responses pass through their real adapter before becoming target input.
func TestCloudHistoricalConversationSwitchFourDirections(t *testing.T) {
	for _, direction := range [][2]string{{"native", "cloud"}, {"bps", "cloud"}, {"cloud", "native"}, {"cloud", "bps"}} {
		for _, mode := range []string{"http", "sse", "ws"} {
			t.Run(direction[0]+"_to_"+direction[1]+"/"+mode, func(t *testing.T) {
				h := newCloudHistoryRoutingHarness(t)
				var history []any
				if direction[0] == "cloud" {
					_, history, _ = h.establish()
				} else {
					call := map[string]any{"type": "function_call", "id": "fc_source", "call_id": "call_" + direction[0] + "_source", "name": "echo", "arguments": "{}", "status": "completed"}
					if direction[0] == "bps" {
						envelope, _ := json.Marshal(map[string]any{"name": "echo", "arguments": map[string]any{}})
						wrapped, _ := json.Marshal(map[string]any{"code": string(envelope), "summary": "Recorded call", "extended_summary": "A previously requested client tool", "destructive": false, "references": []any{}})
						call["name"], call["arguments"] = "run_officejs", string(wrapped)
					}
					event, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_source", "status": "completed", "output": []any{call}, "usage": map[string]any{"input_tokens": 5, "output_tokens": 2}}})
					upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, "data: "+string(event)+"\n\n")}
					svc, account := cloudSwitchTarget(t, h, direction[0], upstream)
					history = []any{cloudUserMessage("original question")}
					result := cloudSwitchInvoke(t, h, svc, account, map[string]any{"model": "gpt-5.1", "input": history, "tools": cloudHistoryTools}, "http")
					tool := result.Get("output.#(type==\"function_call\")")
					require.Equal(t, "echo", tool.Get("name").String(), result.Raw)
					for _, item := range result.Get("output").Array() {
						history = append(history, item.Value())
					}
					history = append(history, map[string]any{"type": "function_call_output", "call_id": tool.Get("call_id").String(), "output": "preserved tool result"})
				}
				history = append(history, cloudUserMessage("continue the same conversation"))
				request := map[string]any{"model": "gpt-5.1", "input": history, "tools": cloudHistoryTools}
				before, _ := json.Marshal(request)
				settlements := h.store.settlementCount()
				if direction[1] == "cloud" {
					h.in.Account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-5.1"}
					finished := make(chan struct{})
					go func() {
						defer close(finished)
						wire := h.nextWire()
						if wire == nil {
							return
						}
						cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "imported"}})
						injected := cloudTestMessage(t, h.ctx, wire)
						require.Equal(t, "thread/inject_items", injected.Method)
						require.Contains(t, string(injected.Params), "original question")
						require.Contains(t, string(injected.Params), "preserved tool result")
						require.Contains(t, string(injected.Params), "call_"+direction[0]+"_source")
						require.NotContains(t, string(injected.Params), "run_officejs", "BPS wire tools must already have become client Responses history")
						cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
						cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "imported-turn"}})
						cloudWireCompleteTurn(wire, "imported", "imported-turn", 12)
					}()
					result, err := cloudMigrationInvoker(t, h, mode)(request)
					require.NoError(t, err, result.Raw)
					require.Equal(t, "completed", result.Get("status").String(), result.Raw)
					<-finished
					require.Equal(t, settlements+1, h.store.settlementCount())
				} else {
					upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_target", "continued"))}
					svc, account := cloudSwitchTarget(t, h, direction[1], upstream)
					result := cloudSwitchInvoke(t, h, svc, account, request, mode)
					require.Equal(t, "completed", result.Get("status").String(), result.Raw)
					require.Len(t, upstream.requests, 1, "migration is one new target request, not a replay of old execution")
					coverage := AnalyzeToolCallOutputContextCoverageBytes(upstream.lastBody)
					require.True(t, coverage.ContextCoversAllCallIDs)
					require.Contains(t, string(upstream.lastBody), "call_cloud_")
					require.Contains(t, string(upstream.lastBody), "answer")
					require.Contains(t, string(upstream.lastBody), "continue the same conversation")
					require.Equal(t, settlements, h.store.settlementCount(), "exporting history must not settle the old Cloud root again")
					require.Empty(t, h.wires, "export must not reconnect to Cloud")
				}
				after, _ := json.Marshal(request)
				require.JSONEq(t, string(before), string(after), "source history stays canonical and unchanged")
			})
		}
	}
}

func cloudSwitchTarget(t *testing.T, h *cloudHistoryRoutingHarness, protocol string, upstream *httpUpstreamRecorder) (*OpenAIGatewayService, *Account) {
	t.Helper()
	svc := excelBPSGatewayTestService(upstream)
	svc.cloudSessions = h.svc.cloudSessions
	svc.cache = h.svc.cache
	account := newOpenAIOAuthNamespaceTestAccount()
	if protocol == "bps" {
		account = excelAccount()
		account.Extra["openai_excel_bps_models"] = []string{"gpt-5.1"}
	}
	account.ID = h.in.Account.ID
	return svc, account
}

func cloudSwitchInvoke(t *testing.T, h *cloudHistoryRoutingHarness, svc *OpenAIGatewayService, account *Account, request map[string]any, mode string) gjson.Result {
	t.Helper()
	body := cloneCloudValue(request).(map[string]any)
	body["stream"] = mode != "http"
	if mode == "ws" {
		body["type"] = "response.create"
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	ctx, err := svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw)
	require.NoError(t, err)
	require.Nil(t, cloudContinuationFromContext(ctx), "completed Cloud history does not select the old protocol")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(raw))).WithContext(ctx)
	c.Set("api_key", h.in.APIKey)
	SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
	var result *OpenAIForwardResult
	if mode == "ws" {
		result, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "unused", raw, len(raw), "gpt-5.1", "", "", "", "", 1, func(event []byte) error { _, e := fmt.Fprintf(recorder, "data: %s\n\n", event); return e })
	} else {
		result, err = svc.Forward(ctx, c, account, raw)
	}
	require.NoError(t, err, recorder.Body.String())
	require.NotNil(t, result)
	return cloudContractResponse(recorder.Body.String())
}

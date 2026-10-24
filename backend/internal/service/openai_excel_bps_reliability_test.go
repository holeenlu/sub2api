package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIWSBPSCancelBeforeResponseIDKeepsConfirmedHistory(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(200, bpsCompletedStream("resp_confirmed", "confirmed answer")),
		{StatusCode: 200, Header: http.Header{"X-Request-Id": {"req_http_only"}}, Body: reader},
		bpsCompletionResponse(200, bpsCompletedStream("resp_after_cancel", "done")),
	}}
	svc := excelBPSGatewayTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	turns := make(chan bpsWSTestTurn, 3)
	client, done := startBPSWSMemorySession(t, svc, excelAccount(), `{"type":"response.create","model":"gpt-6-astra","stream_id":"lane","input":"original context"}`, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
	defer client.CloseNow()
	readBPSWSTestTurn(t, client)
	require.NoError(t, (<-turns).err)
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","previous_response_id":"resp_confirmed","stream_id":"lane","event_id":"create_cancelled","input":"cancelled input"}`)))
	observed := make(chan struct{})
	go func() {
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.in_progress","response":{"usage":{"input_tokens":3}}}`+"\n\n")
		close(observed)
	}()
	<-observed // lifecycle metadata is staged; it is not client output
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.cancel","stream_id":"lane","event_id":"cancel_event"}`)))
	events := readBPSWSTestTurn(t, client)
	last := events[len(events)-1]
	require.Equal(t, "error", gjson.GetBytes(last, "type").String())
	require.Equal(t, "request_cancelled", gjson.GetBytes(last, "error.code").String())
	require.Equal(t, "cancel_event", gjson.GetBytes(last, "event_id").String())
	require.Equal(t, "lane", gjson.GetBytes(last, "stream_id").String())
	require.NotContains(t, string(last), "req_http_only")
	turn := <-turns
	require.NoError(t, turn.err)
	require.Equal(t, 3, turn.result.Usage.InputTokens)
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","previous_response_id":"resp_confirmed","stream_id":"lane","input":"after cancellation"}`)))
	readBPSWSTestTurn(t, client)
	require.NoError(t, (<-turns).err)
	require.Contains(t, string(upstream.bodies[2]), "original context")
	require.Contains(t, string(upstream.bodies[2]), "confirmed answer")
	require.NotContains(t, string(upstream.bodies[2]), "cancelled input")
	requireBPSWSTestServerExit(t, client, done)
}

func TestExcelBPSRepairInterruptionPreservesMeteredUsage(t *testing.T) {
	for _, variant := range []string{"json", "sse", "ws", "unknown-json", "unknown-sse", "unknown-ws"} {
		protocol := strings.TrimPrefix(variant, "unknown-")
		unknownContinuation := protocol != variant
		for _, interrupted := range []string{"eof", "cancel", "closed_pipe"} {
			t.Run(variant+"/"+interrupted, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				reader, writer := io.Pipe()
				defer writer.Close()
				observed := make(chan struct{})
				calls := 0
				upstream := &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
					calls++
					if calls == 1 {
						if unknownContinuation {
							return bpsCompletionResponse(200, excelBPSRepairWire(t, "metered", "Run client tool", `{"name":"unknown","arguments":{}}`)), nil
						}
						return bpsCompletionResponse(200, excelBPSRepairWire(t, "metered", "Run client tool", "text(42);")), nil
					}
					go func() {
						_, _ = io.WriteString(writer, "data: "+`{"type":"response.in_progress","response":{"usage":{"input_tokens":7,"output_tokens":3}}}`+"\n\n")
						close(observed)
						if interrupted == "eof" {
							_ = writer.Close()
						} else if interrupted == "closed_pipe" {
							_ = writer.CloseWithError(io.ErrClosedPipe)
						} else {
							<-req.Context().Done()
							_ = writer.CloseWithError(req.Context().Err())
						}
					}()
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}, nil
				}}
				svc := excelBPSGatewayTestService(&upstream.httpUpstreamRecorder)
				svc.httpUpstream = upstream
				svc.cfg = passthroughLifecycleConfig()
				input := `"calculate"`
				if unknownContinuation {
					input = `[{"type":"custom_tool_call","id":"fc_old","call_id":"old","name":"exec","input":"text(1);"},{"type":"custom_tool_call_output","call_id":"old","output":"1"},{"role":"user","content":"continue"}]`
				}
				body := []byte(fmt.Sprintf(`{"type":"response.create","model":"gpt-6-astra","stream":%t,"input":%s,"tools":[{"type":"custom","name":"exec"}]}`, protocol != "json", input))
				var result *OpenAIForwardResult
				if protocol == "ws" {
					turns := make(chan bpsWSTestTurn, 1)
					client, done := startBPSWSMemorySession(t, svc, excelAccount(), string(body), &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
					defer client.CloseNow()
					<-observed
					if interrupted == "cancel" {
						require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.cancel"}`)))
					}
					readBPSWSTestTurn(t, client)
					result = (<-turns).result
					requireBPSWSTestServerExit(t, client, done)
				} else {
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
					if interrupted == "cancel" {
						go func() { <-observed; cancel() }()
					}
					var err error
					result, err = svc.Forward(ctx, c, excelAccount(), body)
					require.Error(t, err)
					var failover *UpstreamFailoverError
					if errors.As(err, &failover) {
						require.False(t, failover.RetryableOnSameAccount || failover.ShouldRetryNextAccount(), "metered correction cannot trigger another generation")
					}
				}
				require.Equal(t, 2, calls, "a failed correction must not replay generation")
				require.NotNil(t, result)
				require.NotEmpty(t, result.RequestID, "metered work needs a billing identity even if its response was withheld")
				require.Equal(t, 17, result.Usage.InputTokens)
				require.Equal(t, 5, result.Usage.OutputTokens)
			})
		}
	}
}

func TestOpenAIWSBPSRefreshHonorsIndependentRouting(t *testing.T) {
	selected := excelAccount()
	selected.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeOff
	latest := *selected
	latest.Extra = maps.Clone(selected.Extra)
	svc := excelBPSGatewayTestService(&httpUpstreamRecorder{})
	svc.cfg = passthroughLifecycleConfig()
	svc.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	svc.accountRepo = &codexAccountIdentityRepoStub{account: &latest}
	_, err := svc.RefreshOpenAIWSAccount(t.Context(), nil, selected, nil, "gpt-6-astra")
	require.NoError(t, err, "BPS follow-up turns do not require a native upstream websocket")
	latest.Extra[ExcelBPSOmitUnsupportedToolsKey] = true
	_, err = svc.RefreshOpenAIWSAccount(t.Context(), nil, selected, nil, "gpt-6-astra")
	var changed *OpenAIWSAccountChangedError
	require.ErrorAs(t, err, &changed, "later turns must use the new BPS options")
	latest.Extra["openai_excel_bps"] = false
	_, err = svc.RefreshOpenAIWSAccount(t.Context(), nil, selected, nil, "gpt-6-astra")
	require.ErrorIs(t, err, ErrOpenAIWSModelSwitchRequiresReconnect)
}

func TestExcelBPSFirstOutputDeadlineStopsAtSemanticOutput(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	upstream := &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
		go func() {
			defer writer.Close()
			_, _ = io.WriteString(writer, "data: "+`{"type":"response.output_text.delta","delta":"answer"}`+"\n\n")
			select {
			case <-req.Context().Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			_, _ = io.WriteString(writer, bpsCompletedStream("resp_delayed", "answer"))
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}, nil
	}}
	svc := excelBPSGatewayTestService(&upstream.httpUpstreamRecorder)
	svc.httpUpstream = upstream
	svc.cfg.Gateway.OpenAITurnMaxAttempts = 2
	svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	budget := NewOpenAITurnBudget(svc.cfg)
	budget.started = time.Now().Add(-900 * time.Millisecond)
	ctx := WithOpenAITurnBudget(t.Context(), budget)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	result, err := svc.Forward(ctx, c, excelAccount(), []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`))
	require.NoError(t, err)
	require.Equal(t, "resp_delayed", result.ResponseID)
	require.Contains(t, rec.Body.String(), "response.completed")
}

func TestOpenAIWSBPSCancelOwnershipAndNextTurn(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	upstreamCanceled := make(chan struct{})
	calls := 0
	upstream := &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
		calls++
		if calls > 1 {
			return bpsCompletionResponse(200, bpsCompletedStream("resp_next", "done")), nil
		}
		go func() {
			defer writer.Close()
			_, _ = io.WriteString(writer, "data: "+`{"type":"response.created","response":{"id":"resp_owned","usage":{"input_tokens":5}}}`+"\n\n"+
				"data: "+`{"type":"response.output_text.delta","delta":"visible"}`+"\n\n")
			<-req.Context().Done()
			close(upstreamCanceled)
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}, nil
	}}
	svc := excelBPSGatewayTestService(&upstream.httpUpstreamRecorder)
	svc.httpUpstream = upstream
	require.NoError(t, svc.settingService.settingRepo.SetMultiple(t.Context(), map[string]string{SettingKeyExcelBPSImageRelayEnabled: "true", SettingKeyExcelBPSImageMaxRequests: "1"}))
	t.Cleanup(func() { require.NoError(t, svc.CloseExcelBPSImages()) })
	svc.cfg = passthroughLifecycleConfig()
	turns := make(chan bpsWSTestTurn, 2)
	client, done := startBPSWSMemorySession(t, svc, excelAccount(), `{"type":"response.create","model":"gpt-6-astra","stream_id":"owner","input":"cancel me"}`, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
	defer client.CloseNow()
	created, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "resp_owned", gjson.GetBytes(created, "response.id").String())
	delta, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.output_text.delta", gjson.GetBytes(delta, "type").String())
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.cancel","response_id":"wrong","stream_id":"owner"}`)))
	rejected, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(rejected, "type").String())
	select {
	case <-upstreamCanceled:
		t.Fatal("a foreign cancellation stopped the active BPS request")
	default:
	}
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.interrupt","response_id":"resp_owned","mode":"discard_partial_items","stream_id":"owner"}`)))
	failedInterrupt, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.interrupt.failed", gjson.GetBytes(failedInterrupt, "type").String())
	require.Equal(t, "unsupported_operation", gjson.GetBytes(failedInterrupt, "error.code").String())
	select {
	case <-upstreamCanceled:
		t.Fatal("unsupported interrupt must not cancel the BPS stream")
	default:
	}
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.cancel","response_id":"resp_owned","stream_id":"owner"}`)))
	events := readBPSWSTestTurn(t, client)
	require.Equal(t, "response.cancelled", gjson.GetBytes(events[len(events)-1], "type").String())
	turn := <-turns
	require.NoError(t, turn.err, "the ingress owner treats an explicit cancellation as a completed lifecycle")
	require.Equal(t, "response.cancelled", turn.result.UpstreamTerminalEvent)
	require.NotNil(t, turn.result)
	require.Equal(t, 5, turn.result.Usage.InputTokens)
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("owned cancellation did not reach the HTTP request")
	}
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","stream_id":"owner","input":"new turn"}`)))
	readBPSWSTestTurn(t, client)
	require.NoError(t, (<-turns).err)
	require.Equal(t, 2, calls)
	requireBPSWSTestServerExit(t, client, done)
}

func TestExcelBPSFirstOutputDeadlineCoversHeadersAndMetadata(t *testing.T) {
	for _, headers := range []bool{true, false} {
		for _, protocol := range []string{"json", "sse", "ws", "chat", "chat-stream"} {
			t.Run(fmt.Sprintf("headers=%t/protocol=%s", headers, protocol), func(t *testing.T) {
				stream := protocol == "sse" || protocol == "ws" || protocol == "chat-stream"
				reader, writer := io.Pipe()
				defer writer.Close()
				calls := 0
				upstream := &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
					calls++
					if headers {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					go func() {
						defer writer.Close()
						for {
							select {
							case <-req.Context().Done():
								return
							case <-time.After(5 * time.Millisecond):
								_, err := io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_deadline\",\"usage\":{\"input_tokens\":7}}}\n\n")
								if err != nil {
									return
								}
							}
						}
					}()
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}, nil
				}}
				svc := excelBPSGatewayTestService(&upstream.httpUpstreamRecorder)
				svc.httpUpstream = upstream
				svc.cfg.Gateway.OpenAITurnMaxAttempts = 2
				svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
				budget := NewOpenAITurnBudget(svc.cfg)
				budget.started = time.Now().Add(-950 * time.Millisecond)
				ctx := WithOpenAITurnBudget(t.Context(), budget)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
				start := time.Now()
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"hello","stream":%t}`, stream))
				var result *OpenAIForwardResult
				var err error
				var events strings.Builder
				switch {
				case protocol == "ws":
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					result, err = svc.proxyOpenAIWSExcelBPSTurn(ctx, c, excelAccount(), body, "gpt-6-astra", func(raw []byte) error { events.Write(raw); return nil })
				case strings.HasPrefix(protocol, "chat"):
					body = []byte(fmt.Sprintf(`{"model":"gpt-6-astra","messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream))
					result, err = svc.forwardOpenAIGrantCompatibility(ctx, c, excelAccount(), body, "", "gpt-6-astra", false)
				default:
					result, err = svc.Forward(ctx, c, excelAccount(), body)
				}
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover, "an expired turn must not acquire a new retry deadline")
				require.Equal(t, 1, calls)
				require.Less(t, time.Since(start), 500*time.Millisecond)
				require.Contains(t, rec.Body.String()+events.String(), "first_output_timeout")
				value, ok := c.Get(OpsUpstreamErrorsKey)
				require.True(t, ok)
				diagnostics := value.([]*OpsUpstreamErrorEvent)
				require.Equal(t, OpsUpstreamRetryStopped, diagnostics[len(diagnostics)-1].Kind)
				require.Equal(t, string(OpenAIRetryStopTurnBudget), diagnostics[len(diagnostics)-1].Reason)
				require.True(t, gjson.Get(diagnostics[len(diagnostics)-1].Detail, "budget_remaining_ms").Exists())
				require.Zero(t, gjson.Get(diagnostics[len(diagnostics)-1].Detail, "budget_remaining_ms").Int())
				if !headers {
					require.NotNil(t, result)
					require.NotEmpty(t, result.RequestID, "metered work needs a billing identity even if its response was withheld")
					require.Equal(t, 7, result.Usage.InputTokens)
					require.False(t, result.ClientDisconnect)
					snapshot, ok := GetOpsStreamError(c)
					require.True(t, ok)
					require.Equal(t, http.StatusGatewayTimeout, snapshot.IntendedStatus)
					require.Equal(t, "first_output_timeout", snapshot.Code)
					require.True(t, snapshot.CountTowardsSLA)
					require.Equal(t, http.StatusOK, LastOpsUpstreamAttempt(snapshot.UpstreamErrors).UpstreamStatusCode)
					require.True(t, c.GetBool(OpsGatewayResponsesFailedKey))
					if protocol == "ws" {
						BeginOpsStreamTurn(c, 2)
						require.False(t, c.GetBool(OpsGatewayResponsesFailedKey), "gateway provenance is scoped to one WS turn")
					}
				}
			})
		}
	}
}

func TestExcelBPSRepairsShareAttemptBudget(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, excelBPSRepairWire(t, "bad", "ambiguous tool", "text(24);"))}
	svc := excelBPSGatewayTestService(upstream)
	svc.cfg.Gateway.OpenAITurnMaxAttempts = 1
	body := []byte(`{"model":"gpt-6-astra","input":"calculate","tools":[{"type":"custom","name":"exec","format":{"type":"grammar","syntax":"regex","definition":"^text\\(42\\);$"}}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	result, err := svc.Forward(t.Context(), c, excelAccount(), body)
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
}

func TestExcelBPSRetryableFailurePreservesTurnDeadline(t *testing.T) {
	for _, code := range []string{"server_error", "basispoints_model_access_changed", "model_not_allowed", "server_is_overloaded"} {
		t.Run(code, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200,
				"data: "+`{"type":"response.created","response":{"id":"resp_not_delivered"}}`+"\n\n"+
					"data: "+fmt.Sprintf(`{"type":"error","error":{"code":%q}}`, code)+"\n\n")}
			svc := excelBPSGatewayTestService(upstream)
			svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 2
			budget := NewOpenAITurnBudget(svc.cfg)
			deadline := time.Now().Add(time.Second)
			budget.deadline = deadline
			ctx := WithOpenAITurnBudget(t.Context(), budget)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			_, err := svc.Forward(ctx, c, excelAccount(), []byte(`{"model":"gpt-6-astra","input":"continue","stream":true}`))
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, deadline, budget.deadline, "a failure before semantic output must not reset the logical turn's deadline")
		})
	}
}

func TestOpenAIWSBPSFailedTurnRetainsConfirmedHistoryAndEnvelope(t *testing.T) {
	first := strings.Replace(bpsCompletedStream("resp_good", "confirmed answer"), `"output":[`, `"output":[{"type":"reasoning","id":"rs_good","encrypted_content":"opaque-history","summary":[]},`, 1)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(200, first), bpsCompletionResponse(400, excelBPSInvalidCiphertext), bpsCompletionResponse(200, bpsCompletedStream("resp_next", "done")),
	}}
	svc := excelBPSGatewayTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	turns := make(chan bpsWSTestTurn, 3)
	client, serverErr := startBPSWSMemorySession(t, svc, excelAccount(), `{"type":"response.create","model":"gpt-6-astra","stream_id":"lane","input":"first context"}`, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
	defer client.CloseNow()
	readBPSWSTestTurn(t, client)
	require.NoError(t, (<-turns).err)
	for _, event := range []string{
		`{"type":"response.create","previous_response_id":"resp_good","stream_id":"lane","event_id":"bad-event","input":"rejected-turn"}`,
		`{"type":"response.create","previous_response_id":"resp_good","stream_id":"lane","event_id":"next-event","input":"next-turn"}`,
	} {
		require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(event)))
		events := readBPSWSTestTurn(t, client)
		terminal := events[len(events)-1]
		turn := <-turns
		require.Equal(t, "lane", gjson.GetBytes(terminal, "stream_id").String())
		if strings.Contains(event, "bad-event") {
			require.Error(t, turn.err)
			require.Equal(t, "error", gjson.GetBytes(terminal, "type").String())
			require.Equal(t, "bad-event", gjson.GetBytes(terminal, "event_id").String())
			require.False(t, gjson.GetBytes(terminal, "response.id").Exists())
		} else {
			require.NoError(t, turn.err)
		}
	}
	require.Len(t, upstream.requests, 3)
	wire := string(upstream.bodies[2])
	require.Contains(t, wire, "first context")
	require.Contains(t, wire, "confirmed answer")
	require.Contains(t, wire, "opaque-history")
	require.Contains(t, wire, "next-turn")
	require.NotContains(t, wire, "rejected-turn")
	var sent map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(upstream.bodies[2], &sent))
	require.NotContains(t, sent, "stream_id")
	require.NotContains(t, sent, "event_id")
	requireBPSWSTestServerExit(t, client, serverErr)
}

func TestExcelBPSDisabledGloballyDoesNotBlockNativeModels(t *testing.T) {
	svc := excelBPSGatewayTestService(&httpUpstreamRecorder{})
	a := excelAccount()
	delete(a.Credentials, "client_id")
	svc.coolDownExcelBPS(t.Context(), a, "30")
	svc.coolDownExcelBPSModel(a, "gpt-6-astra")
	require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(a, "gpt-6-astra"))
	require.NoError(t, svc.settingService.settingRepo.SetMultiple(t.Context(), map[string]string{SettingKeyExcelBPSEnabled: "false"}))
	svc.settingService.invalidateExcelBPSEnabledCache()
	require.False(t, svc.isExcelBPSCoolingDown(a, "gpt-6-astra"))
	require.False(t, svc.isExcelBPSModelCoolingDown(a, "gpt-6-astra"))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(a, "gpt-6-astra"))
}

func TestExcelBPSDeliveredTerminalWinsLateCancellation(t *testing.T) {
	baseFrameBytes := 0
	for _, boundary := range []int{0, 32 * 1024, 64 * 1024} {
		name := "small"
		if boundary > 0 {
			name = fmt.Sprintf("data_line_ends_at_%d_bytes", boundary)
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			text := "done"
			if boundary > 0 {
				require.Positive(t, baseFrameBytes)
				text += strings.Repeat("x", boundary+1-baseFrameBytes)
			}
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_terminal", text))}
			svc := excelBPSGatewayTestService(upstream)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
			var events [][]byte
			result, err := svc.proxyOpenAIWSExcelBPSTurn(ctx, c, excelAccount(), []byte(`{"model":"gpt-6-astra","stream":true,"input":"hello"}`), "gpt-6-astra", func(event []byte) error {
				events = append(events, append([]byte(nil), event...))
				if gjson.GetBytes(event, "type").String() == "response.completed" {
					frameBytes := len("event: response.completed\ndata: ") + len(event) + len("\n\n")
					if boundary == 0 {
						baseFrameBytes = frameBytes
					} else {
						require.Equal(t, boundary+1, frameBytes, "last empty-line byte must be in the next bounded-writer chunk")
					}
					cancel()
				}
				return nil
			})
			require.NoError(t, err)
			require.True(t, result.SucceededForScheduling())
			require.False(t, result.ClientDisconnect)
			require.Equal(t, "resp_terminal", result.ResponseID)
			require.Len(t, events, 1)
			require.Equal(t, "response.completed", gjson.GetBytes(events[0], "type").String())
		})
	}
}

func TestExcelBPSResponseWriterWaitsForCompleteFrame(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	var events [][]byte
	writer := &excelBPSResponseWriter{
		ResponseWriter: c.Writer, header: http.Header{"Content-Type": {"text/event-stream"}},
		size: -1, startDrain: func() {}, context: c,
		write: func(event []byte) error { events = append(events, append([]byte(nil), event...)); return nil },
	}
	_, err := writer.WriteString("event: response.completed\nid: keep\ndata: {\ndata: \"response\":{\"id\":\"resp_frame\"}}\n")
	require.NoError(t, err)
	writer.Flush()
	require.Empty(t, events, "an unclosed EOF fragment is not a delivered terminal")
	require.False(t, writer.terminal)
	_, err = writer.WriteString("\n")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.JSONEq(t, `{"type":"response.completed","response":{"id":"resp_frame"}}`, string(events[0]))
	require.True(t, writer.terminal)
}

type excelBPSTerminalFlushWriter struct {
	gin.ResponseWriter
	recorder        *httptest.ResponseRecorder
	cancel          context.CancelFunc
	flushErr        error
	terminalFlushed bool
}

func (w *excelBPSTerminalFlushWriter) Flush() { _ = w.FlushError() }
func (w *excelBPSTerminalFlushWriter) FlushError() error {
	if w.flushErr != nil {
		return w.flushErr
	}
	w.ResponseWriter.Flush()
	if strings.Contains(w.recorder.Body.String(), `"type":"response.completed"`) && strings.HasSuffix(w.recorder.Body.String(), "\n\n") {
		w.terminalFlushed = true
		w.cancel()
	}
	return nil
}

func TestExcelBPSHTTPTerminalFlushOutcome(t *testing.T) {
	for _, failFlush := range []bool{false, true} {
		name := "delivered_before_cancel"
		if failFlush {
			name = "flush_failure"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_http_terminal", "done"))}
			svc := excelBPSGatewayTestService(upstream)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			writer := &excelBPSTerminalFlushWriter{ResponseWriter: c.Writer, recorder: recorder, cancel: cancel}
			if failFlush {
				writer.flushErr = errors.New("terminal flush failed")
			}
			c.Writer = writer
			result, err := svc.forwardExcelBPS(ctx, c, excelAccount(), []byte(`{"model":"gpt-6-astra","stream":true,"input":"hello"}`), time.Now(), "gpt-6-astra")
			require.NotNil(t, result)
			require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			if failFlush {
				require.ErrorIs(t, err, writer.flushErr)
				require.False(t, writer.terminalFlushed)
				require.True(t, result.ClientDisconnect)
			} else {
				require.NoError(t, err)
				require.True(t, writer.terminalFlushed)
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				require.False(t, result.ClientDisconnect)
				require.True(t, result.SucceededForScheduling())
			}
		})
	}
}

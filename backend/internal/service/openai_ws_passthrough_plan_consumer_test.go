package service

import (
	"context"
	"errors"
	"fmt"
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

// A successful Native turn is already on this relay. The next public alias is
// selected for BPS only after channel AND account mapping. The old coordinator
// hands original wire back before either mapping or admission of the new turn.
func TestPassthroughFollowupChannelAliasUsesSharedBPSPlan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	frames := make(chan []byte, 4)
	upstream := httptest.NewServer(nativeHTTPBridgeTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, payload, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			frames <- append([]byte(nil), payload...)
			response := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_plan_native","status":"completed","model":%q,"output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`, gjson.GetBytes(payload, "model").String())
			if conn.Write(r.Context(), coderws.MessageText, []byte(response)) != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
	peerHTTP := &httpUpstreamRecorder{err: errors.New("the old coordinator must hand off before HTTP dispatch")}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: peerHTTP, cache: &stubGatewayCache{},
		settingService: excelBPSSettingsTestService(), openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector()}
	account := &Account{ID: 903, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"access_token": "native-plan-token", "chatgpt_account_id": "native-plan-owner",
			"model_mapping": map[string]any{"channel-a": "gpt-6.1-sol", "channel-b": "gpt-6-astra"}},
		Extra: map[string]any{"openai_oauth_responses_websockets_v2_enabled": true,
			"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough,
			"openai_excel_bps":                          true, "openai_excel_bps_models": []string{"gpt-6-astra"}}}
	fixture := newNativeHTTPBridgeFixture(t, svc, account)
	// Keep the real dialer, successful handshake observation, provider writer
	// and permits. Only the OAuth network destination is replaced by a local peer.
	dialer := newDefaultOpenAIWSClientDialer()
	svc.openaiWSPassthroughDialer = openAIWSExecutionScopeDialFunc(func(ctx context.Context, _ string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
		return dialer.Dial(ctx, "ws"+strings.TrimPrefix(upstream.URL, "http"), headers, proxy)
	})
	type turnObservation struct {
		ctx    context.Context
		result *OpenAIForwardResult
		err    error
	}
	turns, stopped := make(chan turnObservation, 2), make(chan turnObservation, 1)
	downstream := httptest.NewServer(nativeHTTPBridgeTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			stopped <- turnObservation{err: err}
			return
		}
		defer conn.CloseNow()
		parent := openAIWSTestAdmissionContext(r.Context())
		_, first, err := ReadOpenAIWSClientMessage(parent, conn, 3*time.Second, coderws.StatusPolicyViolation, "missing first create")
		if err != nil {
			stopped <- turnObservation{err: err}
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r.Clone(parent)
		hooks := &OpenAIWSIngressHooks{
			MapRequestModel: func(_ int, model string) (string, error) {
				switch model {
				case "gpt-6.1-sol":
					return "channel-a", nil
				case "public-b":
					return "channel-b", nil
				default:
					return "", fmt.Errorf("unexpected public model %q", model)
				}
			},
			AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
				turns <- turnObservation{c.Request.Context(), result, err}
			},
		}
		err = fixture.proxy(parent, c, conn, account, first, hooks)
		err = errors.Join(err, FinishNativeWSOperation(c.Request.Context(), err))
		stopped <- turnObservation{ctx: c.Request.Context(), err: err}
	}))
	defer downstream.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6.1-sol","input":"first Native turn"}`)))
	firstWire := wsLifecycleReceive(t, frames)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(firstWire, "model").String())
	firstEvents := readNativeWSTestTurn(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(firstEvents[len(firstEvents)-1], "type").String())
	var first turnObservation
	select {
	case first = <-turns:
	case <-ctx.Done():
		t.Fatal("Native first turn did not finalize")
	}
	require.NoError(t, first.err)
	require.NotNil(t, first.result)
	require.Equal(t, 2, first.result.Usage.InputTokens)
	require.Equal(t, 1, first.result.Usage.OutputTokens)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"public-b","input":"independent next request"}`)))
	var stop turnObservation
	select {
	case stop = <-stopped:
	case <-ctx.Done():
		t.Fatal("passthrough did not join after the mode switch")
	}
	var next *OpenAIWSNextExecution
	require.ErrorAs(t, stop.err, &next)
	require.Equal(t, "public-b", next.originalModel)
	require.Equal(t, "public-b", gjson.GetBytes(next.payload, "model").String(), "handoff preserves the original alias")
	require.Empty(t, frames, "channel-selected BPS must emit zero frames to the held Native upstream")
	require.Empty(t, peerHTTP.requests, "the old relay must not dispatch BPS itself")
	require.Empty(t, turns, "the unreadmitted submission must not create usage or repeat first-turn settlement")
	firstOp, nextOp := nativeDispatchFromContext(first.ctx), nativeDispatchFromContext(stop.ctx)
	require.NotNil(t, firstOp)
	require.NotNil(t, nextOp)
	require.Same(t, firstOp, nextOp, "only the handler may initialize the next operation after handoff")
	require.False(t, excelBPSExecutionPinned(first.ctx), "next-turn selection must not pin the completed Native operation")
	require.False(t, excelBPSExecutionPinned(stop.ctx), "the old coordinator has not selected or admitted the new BPS operation")
	require.Equal(t, 1, nextOp.budget.attemptBudget.Snapshot().Attempts)
	require.Len(t, nativeHTTPFromContext(stop.ctx).core.Snapshot().Attempts, 1)
}

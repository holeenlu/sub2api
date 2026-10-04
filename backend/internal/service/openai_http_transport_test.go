package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type rejectHTTPIngressWSDialer struct{ calls atomic.Int32 }

func (d *rejectHTTPIngressWSDialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	d.calls.Add(1)
	return nil, 0, nil, fmt.Errorf("HTTP ingress must not dial upstream WS")
}

func TestOpenAIHTTPIngressIgnoresRetiredAcceleration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
			t.Run(fmt.Sprintf("stream=%v/mode=%s", stream, mode), func(t *testing.T) {
				cfg := newOpenAIWSV2TestConfig()
				cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
				dialer := &rejectHTTPIngressWSDialer{}
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(dialer)
				t.Cleanup(pool.Close)
				contentType := "application/json"
				responseBody := `{"id":"resp_http","model":"gpt-5.4","usage":{"input_tokens":7,"output_tokens":2}}`
				if stream {
					contentType = "text/event-stream"
					responseBody = "data: {\"type\":\"response.completed\",\"response\":" + responseBody + "}\n\n"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}},
					Body: io.NopCloser(strings.NewReader(responseBody)),
				}}
				svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{},
					openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), openaiWSPool: pool, toolCorrector: NewCodexToolCorrector()}
				account := &Account{ID: 99101, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
					Status: StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"access_token": "test-token"},
					Extra: map[string]any{
						"openai_oauth_ws_sse_acceleration":             true,
						"openai_oauth_responses_websockets_v2_enabled": true,
						"openai_oauth_responses_websockets_v2_mode":    mode,
					}}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				result, err := svc.Forward(context.Background(), c, account,
					[]byte(fmt.Sprintf(`{"model":"gpt-5.4","stream":%v,"input":"hello"}`, stream)))
				require.NoError(t, err)
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, http.MethodPost, upstream.lastReq.Method)
				require.Zero(t, dialer.calls.Load())
				require.False(t, result.OpenAIWSMode)
				require.Equal(t, 7, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
			})
		}
	}
}

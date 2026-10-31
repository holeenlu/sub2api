package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Adapt the maintenance diagnostic cases to the real rebuild root, leases,
// dispatcher and local WS peer. A metadata-only read loss stays Unknown/Sent;
// diagnostic inventory safety cannot authorize another generation.
func TestNativeHTTPToWSRetryStopDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, input, event, reason string
		proposal                   bool
	}{
		{"opaque_compaction", `[{"type":"compaction","encrypted_content":"private-cipher"}]`, `{"type":"response.created","response":{"id":"resp_started"}}`, "replay_unsafe", false},
		{"metered", `"hello"`, `{"type":"response.in_progress","response":{"id":"resp_metered","usage":{"input_tokens":3,"output_tokens":1}}}`, "replay_unsafe", false},
		{"output_committed", `"hello"`, `{"type":"response.output_text.delta","response_id":"resp_started","delta":"private-text"}`, "downstream_committed", false},
		{"metadata_only_unknown", `"hello"`, `{"type":"codex.response.metadata","headers":{"x-codex-turn-state":"private-state"}}`, "replay_unsafe", false},
		{"explicit_pre_generation_proposal", `"hello"`, `{"type":"error","error":{"code":"websocket_connection_limit_reached","type":"websocket_connection_limit_reached","message":"explicit connection limit"}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var creates, dials atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				dials.Add(1)
				defer conn.CloseNow()
				if _, _, err = conn.Read(r.Context()); err != nil {
					return
				}
				creates.Add(1)
				if conn.Write(r.Context(), coderws.MessageText, []byte(tc.event)) != nil {
					return
				}
				_ = conn.Close(coderws.StatusServiceRestart, "private-close")
			}))
			defer upstream.Close()
			f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, upstream.URL)
			user, err := routing.AcquireCapacity(f.ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
			require.NoError(t, err)
			defer user.ReleaseFunc()
			account, err := routing.AcquireCapacity(f.ctx, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: tc.name, Limit: 2}, nil)
			require.NoError(t, err)
			require.True(t, account.Acquired)
			defer account.ReleaseFunc()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(f.ctx)
			c.Set("api_key", f.key)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			body := []byte(`{"model":"client-model","store":false,"stream":true,"input":` + tc.input + `,"tools":[{"type":"function","name":"local_read","parameters":{"type":"object"}}]}`)
			var request map[string]any
			require.NoError(t, decodeOpenAIJSONUseNumber(body, &request))
			owner := nativeHTTPFromContext(f.ctx)
			var result *OpenAIForwardResult
			err = owner.core.Execute(f.ctx, func() NativeOperationStep {
				bound, bindErr := f.service.BindNativeWSOperation(f.ctx, body)
				require.NoError(t, bindErr)
				require.Same(t, owner, nativeHTTPFromContext(bound))
				var forwardErr error
				result, forwardErr = f.service.forwardOpenAIWSV2(bound, c, f.resource, request, "", "diagnostic-native-root", "synthetic-upstream-key", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, true, "client-model", "client-model", time.Now(), 1, time.Time{}, "", nil)
				return NativeOperationStep{Err: forwardErr}
			})
			require.Error(t, err)
			var fallback *openAIWSFallbackError
			require.Equal(t, tc.proposal, errors.As(err, &fallback), "diagnostics cannot expand recovery eligibility")
			require.EqualValues(t, 1, creates.Load())
			require.EqualValues(t, 1, dials.Load())
			require.Zero(t, f.transport.sends.Load(), "no replacement HTTP generation")
			require.Zero(t, f.transport.legacy.Load())
			facts := owner.core.Snapshot()
			require.Len(t, facts.Attempts, 1)
			require.Equal(t, model.SendSent, facts.Attempts[0].Send)
			require.Equal(t, model.TerminalUnknown, facts.Attempts[0].Terminal)
			var stops []*OpsUpstreamErrorEvent
			if raw, ok := c.Get(OpsUpstreamErrorsKey); ok {
				if events, ok := raw.([]*OpsUpstreamErrorEvent); ok {
					for _, event := range events {
						if event.Kind == OpsUpstreamRetryStopped {
							stops = append(stops, event)
						}
					}
				}
			}
			if tc.proposal {
				require.Empty(t, stops, "an explicit repair proposal is not a final stop")
				return
			}
			require.Len(t, stops, 1)
			require.Equal(t, tc.reason, stops[0].Reason)
			detail := gjson.Parse(stops[0].Detail)
			require.True(t, detail.IsObject())
			require.Equal(t, "http", detail.Get("ingress_transport").String())
			require.Equal(t, "websocket", detail.Get("upstream_protocol").String())
			require.Equal(t, "read_upstream", detail.Get("failure_stage").String())
			require.EqualValues(t, 1012, detail.Get("upstream_close_code").Int())
			for _, secret := range []string{"private-", "resp_started", "resp_metered", "synthetic-upstream-key"} {
				require.NotContains(t, stops[0].Detail, secret)
			}
			if tc.name == "opaque_compaction" {
				require.Equal(t, "compaction", detail.Get("unresolved_context").String())
				require.False(t, detail.Get("usage_observed").Bool())
				require.False(t, detail.Get("downstream_committed").Bool())
			}
			if tc.name == "metered" {
				require.True(t, detail.Get("usage_observed").Bool())
				require.NotNil(t, result)
				require.Equal(t, 3, result.Usage.InputTokens)
			}
			if tc.name == "metadata_only_unknown" {
				require.False(t, detail.Get("usage_observed").Bool())
				require.False(t, detail.Get("downstream_committed").Bool())
			}
		})
	}
}

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNativeHTTPCompatibilityRecoveryUsesOneOperationAndRealPermits(t *testing.T) {
	for _, messages := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat_to_raw_chat", true: "messages_continuation"}[messages], func(t *testing.T) {
			body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hello"}],"max_tokens":16,"stream":false}`)
			protocol := model.ProtocolChatCompletions
			path := "/v1/chat/completions"
			if messages {
				protocol, path = model.ProtocolMessages, "/v1/messages"
			}
			f := newNativeOperationFixtureWithInput(t, true, protocol, body, 3)
			f.resource.Credentials["base_url"] = "https://api.openai.com/v1"
			if messages {
				// Continuation is enabled only for models covered by the actual
				// compatibility policy, not the generic transport fixture model.
				f.resource.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.3-codex"}
			}
			var err error
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body))).WithContext(f.ctx)
			c.Set("api_key", f.key)
			if messages {
				f.service.bindOpenAICompatSessionResponseID(f.ctx, c, f.resource, "stable-cache-key", "resp_missing")
			}
			var paths []string
			var sent [][]byte
			f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
				paths = append(paths, request.URL.Path)
				expectedFamily := nativeUpstreamResponses
				if !messages && len(paths) > 1 {
					expectedFamily = nativeUpstreamChat
				}
				require.Equal(t, expectedFamily, nativeUpstreamFamilyFromContext(request.Context()), "the real encoder supplies each attempt's family, independently of client protocol")
				data, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				sent = append(sent, data)
				if len(paths) == 1 {
					message := `{"error":{"message":"not found"}}`
					status := http.StatusNotFound
					if messages {
						status = http.StatusBadRequest
						message = `{"error":{"message":"previous_response_id is not available for this user","type":"invalid_request_error"}}`
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(message))}, nil
				}
				if messages {
					return openAICompatSSECompletedResponse("resp_recovered", "gpt-5.3-codex"), nil
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat-recovered","object":"chat.completion","model":"mapped-provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`))}, nil
			}
			var result *OpenAIForwardResult
			RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
				result, err = TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
					if messages {
						return f.service.ForwardAsAnthropic(f.ctx, c, f.resource, body, "stable-cache-key", "")
					}
					return f.service.ForwardAsChatCompletions(f.ctx, c, f.resource, body, "", "")
				})
				return NativeOperationStep{Err: err}
			})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, paths, 2)
			require.Equal(t, "/v1/responses", paths[0])
			if messages {
				require.Equal(t, "/v1/responses", paths[1])
				require.Equal(t, "resp_missing", gjson.GetBytes(sent[0], "previous_response_id").String())
				require.False(t, gjson.GetBytes(sent[1], "previous_response_id").Exists())
			} else {
				require.Equal(t, "/v1/chat/completions", paths[1])
			}
			operation := nativeHTTPFromContext(f.ctx).core.Snapshot()
			require.Len(t, operation.Attempts, 2)
			require.NotEqual(t, operation.Attempts[0].Identity.WireDigest, operation.Attempts[1].Identity.WireDigest)
			require.Equal(t, operation.Attempts[0].Identity.OperationID, operation.Attempts[1].Identity.OperationID)
			require.EqualValues(t, 2, f.transport.sends.Load())
			require.Zero(t, f.transport.legacy.Load())
			require.Equal(t, 2, f.op.budget.attemptBudget.Snapshot().Attempts)
			require.True(t, operation.Delivery.BusinessCommitted)
			require.Equal(t, model.UsageFinal, operation.Attempts[1].Usage.Confidence)
		})
	}
}

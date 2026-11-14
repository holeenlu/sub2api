package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeHTTPReviewFinalSnapshotsReachExistingJournal(t *testing.T) {
	for _, protocol := range []model.ClientProtocol{model.ProtocolResponses, model.ProtocolMessages, model.ProtocolChatCompletions} {
		for _, kind := range []string{"failed", "incomplete", "canceled", "error", "cache_only"} {
			t.Run(string(protocol)+"/"+kind, func(t *testing.T) {
				body := []byte("{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":true}")
				if protocol != model.ProtocolResponses {
					body = []byte("{\"model\":\"client-model\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}],\"stream\":true}")
				}
				f := newNativeOperationFixtureWithInput(t, true, protocol, body, 3)
				f.resource.Credentials["base_url"] = "https://api.openai.com/v1"
				f.resource.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.1"}
				f.resource.Extra = map[string]any{"openai_responses_supported": true}
				f.key.Group.Platform, f.key.Group.RateMultiplier = PlatformOpenAI, 1
				f.service.cfg.Default.RateMultiplier = 1
				f.service.billingService = NewBillingService(f.service.cfg, nil)
				journal := &nativeApplicationJournal{t: t, effects: &UsageBillingApplyResult{Applied: true}}
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				f.service.usageBillingRepo, f.service.usageLogRepo = journal, logs
				bindNativeRuntimeFixture(t, f)
				event := fmt.Sprintf("{\"type\":\"response.%s\",\"response\":{\"id\":\"resp-review\",\"model\":\"gpt-5.1\",\"status\":\"%s\",\"error\":{\"code\":\"server_error\",\"message\":\"provider stopped\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}", kind, kind)
				if kind == "error" {
					event = "{\"type\":\"error\",\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"provider stopped\"},\"usage\":{\"cached_tokens\":3}}"
				} else if kind == "cache_only" {
					event = "{\"type\":\"response.completed\",\"response\":{\"id\":\"resp-review\",\"model\":\"gpt-5.1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"cached_tokens\":3}}}"
				}
				f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + event + "\n\n"))}, nil
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+string(protocol), strings.NewReader(string(body))).WithContext(f.ctx)
				c.Set("api_key", f.key)
				cycles := 0
				err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
					cycles++
					result, forwardErr := TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
						switch protocol {
						case model.ProtocolResponses:
							return f.service.Forward(f.ctx, c, f.resource, body)
						case model.ProtocolMessages:
							return f.service.ForwardAsAnthropic(f.ctx, c, f.resource, body, "", "")
						default:
							return f.service.ForwardAsChatCompletions(f.ctx, c, f.resource, body, "", "")
						}
					})
					handled, finalErr := f.service.FinalizeNativeHTTPUsage(f.ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource,
						PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), RequestPayloadHash: HashUsageRequestPayload(body)})
					require.True(t, handled)
					require.NoError(t, finalErr)
					return NativeOperationStep{Err: errors.Join(forwardErr, finalErr)}
				})
				require.Equal(t, 1, cycles)
				require.EqualValues(t, 1, f.transport.sends.Load())
				require.Zero(t, f.transport.legacy.Load())
				require.Equal(t, 1, journal.records)
				require.Zero(t, journal.old)
				require.Zero(t, logs.calls)
				require.Len(t, journal.input.Observations, 1)
				evidence := journal.input.Evidence
				if kind == "error" {
					require.Error(t, err)
					require.Equal(t, billing.NativeUsagePartial, evidence.Confidence)
					require.False(t, evidence.TerminalConfirmed)
					require.Nil(t, journal.input.Settlement)
					require.EqualValues(t, 3, *evidence.Usage.CacheReadTokens)
					return
				}
				require.Equal(t, billing.NativeUsageFinal, evidence.Confidence)
				require.True(t, evidence.TerminalConfirmed)
				require.NotNil(t, journal.input.Settlement, "explicit terminal consumption must still reach the original builder")
				if kind == "cache_only" {
					require.NoError(t, err)
					require.EqualValues(t, 3, *evidence.Usage.CacheReadTokens)
				} else {
					// Existing HTTP converters can return a complete protocol reply
					// with nil Go error for incomplete/canceled. Preserve that
					// contract; the independently observed terminal must not
					// become Completed merely because usage became Final.
					if kind == "failed" {
						require.Error(t, err)
					}
					require.EqualValues(t, 2, *evidence.Usage.OutputTokens)
					expected := map[string]model.TerminalState{"failed": model.TerminalFailed, "incomplete": model.TerminalIncomplete, "canceled": model.TerminalCanceled}[kind]
					require.Equal(t, expected, nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts[0].Terminal)
				}
			})
		}
	}
}

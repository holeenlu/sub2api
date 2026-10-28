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

// This follows the production application route, real sealing/admission,
// observer and original pricing adapter. Only transport and repository ports
// are local substitutes; it does not call a normalizer in isolation.
func TestNativeHTTPReviewReasoningNormalizationReachesActualFinalizer(t *testing.T) {
	for _, route := range []struct {
		name         string
		client       model.ClientProtocol
		chatUpstream bool
	}{
		{"responses", model.ProtocolResponses, false},
		{"messages_to_responses", model.ProtocolMessages, false},
		{"chat_to_responses", model.ProtocolChatCompletions, false},
		{"responses_to_chat", model.ProtocolResponses, true},
	} {
		for _, sample := range []struct {
			name, details    string
			raw, normalized  int
			total, reasoning *int64
			auxiliaryOnly    bool
		}{
			{"independent", ",\"total_tokens\":10,\"output_tokens_details\":{\"reasoning_tokens\":3}", 2, 5, nativeImageCount(10), nativeImageCount(3), false},
			{"already_included", ",\"total_tokens\":10,\"output_tokens_details\":{\"reasoning_tokens\":3}", 5, 5, nativeImageCount(10), nativeImageCount(3), false},
			{"measured_zero_reasoning", ",\"total_tokens\":7,\"output_tokens_details\":{\"reasoning_tokens\":0}", 2, 2, nativeImageCount(7), nativeImageCount(0), false},
			{"missing_total", ",\"output_tokens_details\":{\"reasoning_tokens\":3}", 2, 2, nil, nativeImageCount(3), false},
			{"missing_reasoning", ",\"total_tokens\":10", 2, 2, nativeImageCount(10), nil, false},
			{"only_total", ",\"total_tokens\":10", 0, 0, nil, nil, true},
			{"only_reasoning", ",\"output_tokens_details\":{\"reasoning_tokens\":3}", 0, 0, nil, nil, true},
			{"only_auxiliary_pair", ",\"total_tokens\":10,\"output_tokens_details\":{\"reasoning_tokens\":3}", 0, 3, nil, nil, true},
		} {
			t.Run(route.name+"/"+sample.name, func(t *testing.T) {
				body := []byte("{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":false}")
				if route.client != model.ProtocolResponses {
					body = []byte("{\"model\":\"client-model\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}],\"stream\":false}")
				}
				f := newNativeOperationFixtureWithInput(t, true, route.client, body, 3)
				f.resource.Credentials["base_url"] = "https://api.openai.com/v1"
				f.resource.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.1"}
				f.resource.Extra = map[string]any{"openai_responses_supported": !route.chatUpstream}
				f.key.Group.Platform, f.key.Group.RateMultiplier = PlatformOpenAI, 1
				f.service.cfg.Default.RateMultiplier = 1
				f.service.billingService = NewBillingService(f.service.cfg, nil)
				journal := &nativeApplicationJournal{t: t, effects: &UsageBillingApplyResult{Applied: true}}
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				f.service.usageBillingRepo, f.service.usageLogRepo = journal, logs
				bindNativeRuntimeFixture(t, f)
				usage := fmt.Sprintf("{\"input_tokens\":5,\"output_tokens\":%d%s}", sample.raw, sample.details)
				if sample.auxiliaryOnly {
					usage = "{" + strings.TrimPrefix(sample.details, ",") + "}"
				}
				response := "{\"id\":\"resp-reasoning\",\"object\":\"response\",\"model\":\"gpt-5.1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":" + usage + "}"
				wire := "data: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
				contentType, expectedFamily := "text/event-stream", nativeUpstreamResponses
				if route.chatUpstream {
					wire = "{\"id\":\"chat-reasoning\",\"object\":\"chat.completion\",\"model\":\"gpt-5.1\",\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":" + usage + "}"
					contentType, expectedFamily = "application/json", nativeUpstreamChat
				}
				f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
					require.Equal(t, expectedFamily, nativeUpstreamFamilyFromContext(req.Context()))
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+string(route.client), strings.NewReader(string(body))).WithContext(f.ctx)
				c.Set("api_key", f.key)
				var finalErr error
				cycles := 0
				runErr := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
					cycles++
					result, forwardErr := TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
						switch route.client {
						case model.ProtocolResponses:
							return f.service.Forward(f.ctx, c, f.resource, body)
						case model.ProtocolMessages:
							return f.service.ForwardAsAnthropic(f.ctx, c, f.resource, body, "", "")
						default:
							return f.service.ForwardAsChatCompletions(f.ctx, c, f.resource, body, "", "")
						}
					})
					require.NoError(t, forwardErr)
					require.Equal(t, sample.normalized, result.Usage.OutputTokens, "the original production parser still owns normalization")
					attempt := nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts[0]
					if sample.auxiliaryOnly {
						require.Nil(t, attempt.Usage, "auxiliary proof cannot manufacture primary counters from the legacy normalized result")
					} else {
						require.EqualValues(t, sample.raw, attempt.Usage.Output.Value, "raw observation must not be rewritten to satisfy billing")
						require.Equal(t, model.UsageFinal, attempt.Usage.Confidence)
						for _, count := range []struct {
							want *int64
							got  *model.TokenMeasurement
						}{{sample.total, attempt.Usage.Total}, {sample.reasoning, attempt.Usage.Reasoning}} {
							if count.want == nil {
								require.Nil(t, count.got)
							} else {
								require.NotNil(t, count.got)
								require.Equal(t, *count.want, count.got.Value)
							}
						}
					}
					handled, err := f.service.FinalizeNativeHTTPUsage(f.ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource,
						PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), RequestPayloadHash: HashUsageRequestPayload(body)})
					require.True(t, handled)
					finalErr = err
					if finalErr != nil {
						return NativeOperationContinue() // Persistence failure must stop this proposed retry.
					}
					return NativeOperationStep{Err: forwardErr}
				})
				require.Equal(t, 1, cycles)
				require.EqualValues(t, 1, f.transport.sends.Load())
				require.Zero(t, f.transport.legacy.Load())
				require.Zero(t, journal.old)
				require.Zero(t, logs.calls)
				if finalErr != nil {
					require.ErrorIs(t, finalErr, billing.ErrNativeUsageConflict)
					require.True(t, errors.Is(runErr, billing.ErrNativeUsageConflict))
					require.False(t, nativeHTTPFromContext(f.ctx).core.Snapshot().Billing.Durable)
					t.Logf("actual route reached mapping conflict: raw_output=%d normalized_output=%d journal_calls=%d sends=%d", sample.raw, sample.normalized, journal.records, f.transport.sends.Load())
				}
				require.NoError(t, finalErr, "proved raw-to-normalized usage must be validated using its real source measurements")
				require.NoError(t, runErr)
				require.Equal(t, 1, journal.records)
				if sample.auxiliaryOnly {
					require.Nil(t, journal.input.Evidence.Usage)
					require.Equal(t, billing.NativeUsageUnknown, journal.input.Evidence.Confidence)
					require.Nil(t, journal.input.Settlement)
					require.Nil(t, journal.input.SettlementUsage)
					require.Equal(t, model.BillingPendingUsage, nativeHTTPFromContext(f.ctx).core.Snapshot().Billing.Disposition)
					return
				}
				require.EqualValues(t, sample.raw, *journal.input.Evidence.Usage.OutputTokens)
				require.Equal(t, sample.total, journal.input.Evidence.Usage.TotalTokens)
				require.Equal(t, sample.reasoning, journal.input.Evidence.Usage.ReasoningTokens)
				require.Equal(t, sample.normalized, journal.input.SettlementUsage.OpenAI.Usage.OutputTokens)
			})
		}
	}
}

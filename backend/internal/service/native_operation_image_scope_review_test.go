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

// Exercise the actual HTTP application, sealed transport, observation, original
// usage builder and Finalizer. The transport and journal ports are local
// substitutes. This is not a repository transaction or live-provider test.
func TestNativeHTTPImageScopesReachActualFinalizer(t *testing.T) {
	for _, route := range []struct {
		name   string
		client model.ClientProtocol
	}{
		{"responses", model.ProtocolResponses},
		{"messages_to_responses", model.ProtocolMessages},
		{"chat_to_responses", model.ProtocolChatCompletions},
	} {
		for _, sample := range []struct {
			name                        string
			primaryInput, primaryOutput *int64
			toolInput, toolOutput       int
			toolJSON                    string
			invalidHostedEvidence       bool
		}{
			{"main_zero_hosted_positive", nativeImageCount(0), nativeImageCount(0), 4, 8, "", false},
			{"main_input_zero_hosted_positive", nativeImageCount(0), nativeImageCount(3), 4, 8, "", false},
			{"main_output_zero_hosted_positive", nativeImageCount(2), nativeImageCount(0), 4, 8, "", false},
			{"main_positive_keeps_precedence", nativeImageCount(2), nativeImageCount(3), 4, 8, "", false},
			{"main_absent_uses_existing_fallback", nil, nil, 4, 8, "", false},
			{"both_scopes_measured_zero", nativeImageCount(0), nativeImageCount(0), 0, 0, "", false},
			{"hosted_strings_are_not_measurements", nativeImageCount(0), nativeImageCount(0), 4, 8, "{\"input_tokens_details\":{\"image_tokens\":\"4\"},\"output_tokens_details\":{\"image_tokens\":\"8\"}}", true},
			{"hosted_fractions_are_not_measurements", nativeImageCount(0), nativeImageCount(0), 4, 8, "{\"input_tokens_details\":{\"image_tokens\":4.5},\"output_tokens_details\":{\"image_tokens\":8.5}}", true},
			{"hosted_totals_are_not_image_components", nativeImageCount(0), nativeImageCount(0), 0, 0, "{\"input_tokens\":12,\"output_tokens\":8,\"total_tokens\":20}", false},
		} {
			t.Run(route.name+"/"+sample.name, func(t *testing.T) {
				body := []byte("{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":false}")
				if route.client != model.ProtocolResponses {
					body = []byte("{\"model\":\"client-model\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}],\"stream\":false}")
				}
				f := newNativeOperationFixtureWithInput(t, true, route.client, body, 3)
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

				// These are two independently reported objects, with different
				// totals. Image tokens must not be added to the main totals.
				usage := "{\"input_tokens\":100,\"output_tokens\":50,\"total_tokens\":150"
				for _, component := range []struct {
					name  string
					value *int64
				}{{"input_tokens_details", sample.primaryInput}, {"output_tokens_details", sample.primaryOutput}} {
					if component.value != nil {
						usage += fmt.Sprintf(",%q:{\"image_tokens\":%d}", component.name, *component.value)
					}
				}
				usage += "}"
				tool := fmt.Sprintf("{\"input_tokens\":12,\"output_tokens\":8,\"total_tokens\":20,\"input_tokens_details\":{\"image_tokens\":%d},\"output_tokens_details\":{\"image_tokens\":%d}}", sample.toolInput, sample.toolOutput)
				if sample.toolJSON != "" {
					tool = sample.toolJSON
				}
				response := "{\"id\":\"resp-image-scopes\",\"object\":\"response\",\"model\":\"gpt-5.1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":" + usage + ",\"tool_usage\":{\"image_gen\":" + tool + "}}"
				wire := "data: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
				f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
					require.Equal(t, nativeUpstreamResponses, nativeUpstreamFamilyFromContext(req.Context()))
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+string(route.client), strings.NewReader(string(body))).WithContext(f.ctx)
				c.Set("api_key", f.key)
				var finalErr error
				cycles := 0
				runErr := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
					cycles++
					if cycles > 1 {
						return NativeOperationStep{Err: errors.New("Finalizer failure allowed another cycle")}
					}
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
					require.NotNil(t, result)
					attempt := nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts[0]
					require.NotNil(t, attempt.Usage)
					require.Equal(t, model.UsageFinal, attempt.Usage.Confidence)
					require.True(t, attempt.Usage.TerminalConfirmed)
					for _, component := range []struct {
						name     string
						primary  *int64
						tool     int
						observed *model.TokenMeasurement
						hosted   *model.TokenMeasurement
						mapped   int
					}{
						{"input", sample.primaryInput, sample.toolInput, attempt.Usage.ImageInput, attempt.Usage.HostedImageInput, result.Usage.ImageInputTokens},
						{"output", sample.primaryOutput, sample.toolOutput, attempt.Usage.ImageOutput, attempt.Usage.HostedImageOutput, result.Usage.ImageOutputTokens},
					} {
						canonical := component.tool
						if component.primary == nil {
							require.Nil(t, component.observed, "%s hosted evidence cannot fill an absent main measurement", component.name)
						} else {
							require.NotNil(t, component.observed)
							require.Equal(t, *component.primary, component.observed.Value, "%s raw measurement must retain an explicit main zero", component.name)
							if *component.primary > 0 {
								canonical = int(*component.primary)
							}
						}
						if sample.toolJSON == "" {
							require.NotNil(t, component.hosted)
							require.EqualValues(t, component.tool, component.hosted.Value)
						} else {
							require.Nil(t, component.hosted, "invalid or absent image components do not become measured zero")
						}
						require.Equal(t, canonical, component.mapped, "%s must follow the existing helper's precedence", component.name)
					}
					require.EqualValues(t, 100, attempt.Usage.Input.Value)
					require.EqualValues(t, 50, attempt.Usage.Output.Value)
					require.EqualValues(t, 150, attempt.Usage.Total.Value)
					require.Equal(t, 100, result.Usage.InputTokens)
					require.Equal(t, 50, result.Usage.OutputTokens)
					require.Zero(t, result.ImageCount, "tool token measurements do not prove completed images")
					handled, err := f.service.FinalizeNativeHTTPUsage(f.ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource,
						PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), RequestPayloadHash: HashUsageRequestPayload(body)})
					require.True(t, handled)
					finalErr = err
					if err != nil {
						return NativeOperationContinue()
					}
					return NativeOperationStep{Err: forwardErr}
				})
				snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
				require.Equal(t, 1, cycles)
				require.Len(t, snapshot.Attempts, 1)
				for _, component := range []struct {
					primary  *int64
					observed *model.TokenMeasurement
				}{
					{sample.primaryInput, snapshot.Attempts[0].Usage.ImageInput},
					{sample.primaryOutput, snapshot.Attempts[0].Usage.ImageOutput},
				} {
					if component.primary == nil {
						require.Nil(t, component.observed)
					} else {
						require.NotNil(t, component.observed)
						require.Equal(t, *component.primary, component.observed.Value, "Finalizer must not rewrite the original measurement")
					}
				}
				require.EqualValues(t, 1, f.transport.sends.Load())
				require.Zero(t, f.transport.legacy.Load())
				require.Zero(t, journal.old)
				require.Zero(t, logs.calls)
				if finalErr != nil {
					require.ErrorIs(t, finalErr, billing.ErrNativeUsageConflict)
					require.ErrorIs(t, runErr, billing.ErrNativeUsageConflict)
					require.False(t, snapshot.Billing.Durable)
					require.Zero(t, journal.records, "the mapping validator rejected before persistence")
					t.Logf("image scope conflict reached real Finalizer: case=%s legacy_image_input=%d legacy_image_output=%d sends=%d journal_records=%d error=%v", sample.name, sample.toolInput, sample.toolOutput, f.transport.sends.Load(), journal.records, finalErr)
				}
				if sample.invalidHostedEvidence {
					// Invalid legacy projection must not discard valid parser facts.
					// The technical journal ACK retains a typed preparation failure;
					// it is not a monetary settlement or measured hosted zero.
					require.NoError(t, finalErr)
					require.NoError(t, runErr)
					require.Equal(t, 1, journal.records)
					require.True(t, snapshot.Billing.Durable)
					require.Equal(t, model.BillingRecoveryPending, snapshot.Billing.Disposition)
					require.Nil(t, journal.input.Settlement)
					require.Nil(t, journal.input.SettlementUsage)
					require.NotNil(t, journal.final)
					require.Equal(t, billing.NativeUsagePreparationUnavailable, journal.final.Reason)
					require.Nil(t, journal.input.Evidence.Usage.HostedImageInputTokens)
					require.Nil(t, journal.input.Evidence.Usage.HostedImageOutputTokens)
					require.Equal(t, sample.primaryInput, journal.input.Evidence.Usage.ImageInputTokens)
					require.Equal(t, sample.primaryOutput, journal.input.Evidence.Usage.ImageOutputTokens)
					require.EqualValues(t, 100, *journal.input.Evidence.Usage.InputTokens)
					require.EqualValues(t, 50, *journal.input.Evidence.Usage.OutputTokens)
					return
				}
				require.NoError(t, finalErr, "retain both image scopes and validate the existing projection without replacing raw zero")
				require.NoError(t, runErr)
				require.Equal(t, 1, journal.records)
				require.True(t, snapshot.Billing.Durable)
				require.NotNil(t, journal.input.Evidence.Usage)
				require.NotNil(t, journal.input.SettlementUsage)
				require.NotNil(t, journal.input.SettlementUsage.OpenAI)
				for _, component := range []struct {
					primary *int64
					raw     *int64
					hosted  *int64
					tool    int
					mapped  int
				}{
					{sample.primaryInput, journal.input.Evidence.Usage.ImageInputTokens, journal.input.Evidence.Usage.HostedImageInputTokens, sample.toolInput, journal.input.SettlementUsage.OpenAI.Usage.ImageInputTokens},
					{sample.primaryOutput, journal.input.Evidence.Usage.ImageOutputTokens, journal.input.Evidence.Usage.HostedImageOutputTokens, sample.toolOutput, journal.input.SettlementUsage.OpenAI.Usage.ImageOutputTokens},
				} {
					require.Equal(t, component.primary, component.raw, "persist the real main counter, including nil and zero")
					if sample.toolJSON == "" {
						require.NotNil(t, component.hosted)
						require.EqualValues(t, component.tool, *component.hosted)
					} else {
						require.Nil(t, component.hosted)
					}
					want := component.tool
					if component.primary != nil && *component.primary > 0 {
						want = int(*component.primary)
					}
					require.Equal(t, want, component.mapped)
				}
				require.Equal(t, 100, journal.input.SettlementUsage.OpenAI.Usage.InputTokens)
				require.Equal(t, 50, journal.input.SettlementUsage.OpenAI.Usage.OutputTokens)
				require.Zero(t, journal.input.SettlementUsage.OpenAI.ImageCount)
			})
		}
	}
}

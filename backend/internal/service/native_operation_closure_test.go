package service

import (
	"context"
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
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The existing journal fixture remains the only persistence substitute. This
// wrapper checks the real cleanup context and owned slots at that boundary.
type nativeClosureJournal struct {
	*nativeApplicationJournal
	beforeFinalize func(context.Context, *NativeIntentFinalizationInput)
}

func (r *nativeClosureJournal) FinalizeNativeUsageIntent(ctx context.Context, input *NativeIntentFinalizationInput) (NativeIntentFinalizationResult, error) {
	r.beforeFinalize(ctx, input)
	return r.nativeApplicationJournal.FinalizeNativeUsageIntent(ctx, input)
}

func TestNativeHTTPThreeProtocolsReachRealFinalizerBeforeRelease(t *testing.T) {
	for _, protocol := range []model.ClientProtocol{model.ProtocolResponses, model.ProtocolMessages, model.ProtocolChatCompletions} {
		for _, stream := range []bool{false, true} {
			for _, outcome := range []string{"final", "unknown_eof", "journal_failure"} {
				t.Run(fmt.Sprintf("%s/stream_%t/%s", protocol, stream, outcome), func(t *testing.T) {
					body := []byte(fmt.Sprintf("{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":%t}", stream))
					if protocol != model.ProtocolResponses {
						body = []byte(fmt.Sprintf("{\"model\":\"client-model\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}],\"stream\":%t}", stream))
					}
					f := newNativeOperationFixtureWithInput(t, true, protocol, body, 3)
					f.resource.Credentials["base_url"] = "https://api.openai.com/v1"
					f.resource.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.1"}
					f.resource.Extra = map[string]any{"openai_responses_supported": true}
					f.key.Group.Platform, f.key.Group.RateMultiplier = PlatformOpenAI, 1
					f.service.cfg.Default.RateMultiplier = 1
					f.service.billingService = NewBillingService(f.service.cfg, nil)
					logs := &openAIRecordUsageLogRepoStub{inserted: true}
					f.service.usageLogRepo = logs
					journal := &nativeApplicationJournal{t: t, effects: &UsageBillingApplyResult{Applied: true}}
					if outcome == "journal_failure" {
						journal.fail = errors.New("journal acknowledgement unavailable")
					}
					cache := &nativeFinalizationBalance{}
					hold := newInflightReservation(cache, f.key.UserID, "admitted-hold", 3, time.Minute)
					t.Cleanup(hold.Release)
					requestID := "closure-" + string(protocol)
					f.ctx = WithInflightReservation(context.WithValue(f.ctx, ctxkey.RequestID, requestID), hold)
					recordBoundaryCalls := 0
					f.service.usageBillingRepo = &nativeClosureJournal{nativeApplicationJournal: journal, beforeFinalize: func(cleanup context.Context, input *NativeIntentFinalizationInput) {
						recordBoundaryCalls++
						require.NoError(t, cleanup.Err())
						_, bounded := cleanup.Deadline()
						require.True(t, bounded)
						scope := routing.CapacityScopeFrom(cleanup)
						require.NotNil(t, scope)
						for _, policy := range []routing.CapacityPolicy{{ResourceID: f.key.UserID, User: true, Limit: 0}, {ResourceID: f.resource.ID, Limit: 0}} {
							_, err := scope.Lookup(policy.User, policy.ResourceID).Verify(cleanup, policy)
							require.NoError(t, err, "actual slots must remain held until journal completion")
						}
						require.Zero(t, cache.releases.Load())
						require.Equal(t, 1, journal.stores, "the actual before-send intent must precede finalization")
						require.Contains(t, journal.intents, input.Intent.Key)
						require.Equal(t, "local:"+requestID, input.Evidence.Evidence.Identity.RequestID)
					}}
					bindNativeRuntimeFixture(t, f)
					f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
						if outcome == "unknown_eof" {
							return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_unknown\",\"model\":\"gpt-5.1\"}}\n\n"))}, nil
						}
						response := openAICompatSSECompletedResponse("resp_closure", "gpt-5.1")
						wire, err := io.ReadAll(response.Body)
						require.NoError(t, err)
						require.NoError(t, response.Body.Close())
						wire = []byte(strings.Replace(string(wire), "\"input_tokens\":5,", "\"input_tokens\":5,\"input_tokens_details\":{\"cached_tokens\":2},", 1))
						response.Body = io.NopCloser(strings.NewReader(string(wire)))
						return response, nil
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+string(protocol), strings.NewReader(string(body))).WithContext(f.ctx)
					c.Set("api_key", f.key)
					lease := RetainNativeHTTPRelease(f.accountSlot.ReleaseFunc)
					cycles := 0
					err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
						cycles++
						defer lease.Release()
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
						TrackNativeHTTPAuthorization(f.ctx, ActualOpenAIUsageAuthorizationMode(c))
						handled, finalErr := f.service.FinalizeNativeHTTPUsage(f.ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource,
							PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), RequestPayloadHash: HashUsageRequestPayload(body)})
						require.True(t, handled)
						if outcome == "journal_failure" {
							require.ErrorIs(t, finalErr, journal.fail)
							return NativeOperationContinue()
						}
						return NativeOperationStep{Err: errors.Join(forwardErr, finalErr)}
					})
					hold.HandlerDone()
					require.Equal(t, 1, cycles)
					require.EqualValues(t, 1, f.transport.sends.Load())
					require.Zero(t, f.transport.legacy.Load())
					require.Equal(t, 1, recordBoundaryCalls)
					require.Equal(t, 1, journal.records)
					require.Zero(t, journal.old, "three public forwarding paths must not invoke legacy Apply")
					require.Zero(t, logs.calls, "no duplicate or zero-valued usage row")
					snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
					_, verifyErr := f.accountSlot.Verify(t.Context(), routing.CapacityPolicy{ResourceID: f.resource.ID, Limit: 0})
					require.Error(t, verifyErr)
					if outcome == "journal_failure" {
						require.ErrorIs(t, err, journal.fail)
						require.False(t, snapshot.Billing.Durable)
						require.Zero(t, cache.releases.Load())
						return
					}
					require.True(t, snapshot.Billing.Durable)
					require.EqualValues(t, 1, cache.releases.Load())
					require.Len(t, journal.input.Observations, 1)
					if outcome == "unknown_eof" {
						require.Error(t, err)
						require.Nil(t, journal.input.Settlement)
						require.Nil(t, journal.input.Evidence.Usage)
						require.Equal(t, model.BillingPendingUsage, snapshot.Billing.Disposition)
						return
					}
					require.NoError(t, err)
					require.Equal(t, billing.NativeUsageFinal, journal.input.Evidence.Confidence)
					require.NotNil(t, journal.input.Settlement)
					require.EqualValues(t, 5, *journal.input.Evidence.Usage.InputTokens)
					require.Equal(t, 3, journal.input.Settlement.InputTokens)
					require.Equal(t, 2, journal.input.Settlement.CacheReadTokens)
					require.Equal(t, 5, journal.input.SettlementUsage.OpenAI.Usage.InputTokens)
					require.Equal(t, 3, journal.input.SettlementUsage.Tokens.InputTokens)
					require.Equal(t, model.BillingSettled, snapshot.Billing.Disposition)
				})
			}
		}
	}
}

func TestNativeHTTPWSAttemptUsesHTTPFinalizerOnSharedCore(t *testing.T) {
	for _, outcome := range []string{"final", "unknown_usage", "journal_failure"} {
		t.Run(outcome, func(t *testing.T) {
			f, owner, c := nativeHTTPObservedFixture(t, "", "application/json")
			f.key.Group.Platform, f.key.Group.RateMultiplier = PlatformOpenAI, 1
			f.service.cfg.Default.RateMultiplier = 1
			f.service.billingService = NewBillingService(f.service.cfg, nil)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			f.service.usageLogRepo = logs
			journal := &nativeApplicationJournal{t: t, effects: &UsageBillingApplyResult{Applied: true}}
			if outcome == "journal_failure" {
				journal.fail = errors.New("HTTP-to-WS journal unavailable")
			}
			f.service.usageBillingRepo = &nativeClosureJournal{nativeApplicationJournal: journal, beforeFinalize: func(ctx context.Context, input *NativeIntentFinalizationInput) {
				require.NoError(t, ctx.Err())
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				_, err := f.accountSlot.Verify(ctx, routing.CapacityPolicy{ResourceID: f.resource.ID, Limit: 0})
				require.NoError(t, err, "HTTP slot must outlive the real journal acknowledgement")
				require.Equal(t, 1, journal.stores)
				require.Contains(t, journal.intents, input.Intent.Key)
				require.Equal(t, "ws", input.Evidence.Evidence.Prepared.Transport)
				require.Len(t, input.Evidence.Observations, 1)
			}}
			bindNativeRuntimeFixture(t, f)
			f.ctx = WithNativeIntentModelFacts(f.ctx, NativeIntentModelFacts{Model: "gpt-5.1", UpstreamModel: "gpt-5.1"})
			c.Request = c.Request.WithContext(f.ctx)
			handshake, err := nativeWSHandshakeSnapshot(f.resource, f.resource, "wss://api.openai.com/v1/responses", "", http.Header{"Authorization": []string{"Bearer synthetic-upstream-key"}})
			require.NoError(t, err)
			handshake.succeeded = true
			conn := &nativeWSRecordedConn{handshake: handshake, operation: f.op}
			payload := []byte("{\"type\":\"response.create\",\"model\":\"gpt-5.1\",\"input\":\"hello\"}")
			original := owner.core.Snapshot()
			lease := RetainNativeHTTPRelease(f.accountSlot.ReleaseFunc)
			cycles, afterTurns := 0, 0
			err = RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
				cycles++
				defer lease.Release()
				result, forwardErr := TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
					bound, bindErr := f.service.BindNativeWSOperation(f.ctx, payload)
					require.NoError(t, bindErr)
					require.Same(t, owner, nativeHTTPFromContext(bound))
					require.Equal(t, "http", f.op.request.Transport)
					require.Equal(t, original.OriginalWireDigest, owner.core.Snapshot().OriginalWireDigest)
					require.NoError(t, f.service.sendNativeWS(bound, f.resource, conn, coderws.MessageText, payload))
					result := &OpenAIForwardResult{OpenAIWSMode: true, AuthorizationMode: UsageAuthorizationModeNative, Model: "gpt-5.1", Stream: true}
					var cause error
					if outcome == "unknown_usage" {
						require.ErrorIs(t, conn.attempt.observe(nil, io.EOF), io.EOF)
						cause = io.ErrUnexpectedEOF
					} else {
						require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_http_ws\",\"model\":\"gpt-5.1\",\"usage\":{\"input_tokens\":5,\"output_tokens\":2,\"input_tokens_details\":{\"cached_tokens\":2}}}}"), nil))
						result.Usage = OpenAIUsage{InputTokens: 5, OutputTokens: 2, CacheReadInputTokens: 2}
					}
					conn.attempt.close(nil)
					cause = completeOpenAIWSTurn(bound, &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, cause error) {
						afterTurns++
						handled, finalErr := f.service.FinalizeNativeWSUsage(bound, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource}, cause)
						require.False(t, handled, "only client-WS roots may finalize in AfterTurn")
						require.NoError(t, finalErr)
					}}, 1, result, cause)
					require.Zero(t, journal.records)
					require.False(t, owner.finalized)
					require.Equal(t, model.OperationReceiving, owner.core.Snapshot().State)
					return result, cause
				})
				TrackNativeHTTPAuthorization(f.ctx, UsageAuthorizationModeNative)
				require.True(t, NativeHTTPFinalizesUsage(f.ctx))
				handled, finalErr := f.service.FinalizeNativeHTTPUsage(f.ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource,
					PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)})
				require.True(t, handled)
				if outcome == "journal_failure" {
					require.ErrorIs(t, finalErr, journal.fail)
					return NativeOperationContinue()
				}
				return NativeOperationStep{Err: errors.Join(forwardErr, finalErr)}
			})
			require.Equal(t, 1, cycles)
			require.Equal(t, 1, afterTurns)
			require.Equal(t, 1, conn.frames)
			require.Equal(t, 1, journal.records)
			require.Zero(t, journal.old)
			require.Zero(t, logs.calls, "no legacy zero row or second settlement")
			require.Zero(t, f.transport.sends.Load())
			snapshot := owner.core.Snapshot()
			require.True(t, snapshot.Attempts[0].Closed)
			require.Equal(t, "ws", snapshot.Attempts[0].Identity.Transport)
			_, verifyErr := f.accountSlot.Verify(t.Context(), routing.CapacityPolicy{ResourceID: f.resource.ID, Limit: 0})
			require.Error(t, verifyErr)
			switch outcome {
			case "journal_failure":
				require.ErrorIs(t, err, journal.fail)
				require.False(t, snapshot.Billing.Durable)
			case "unknown_usage":
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				require.True(t, snapshot.Billing.Durable)
				require.Equal(t, model.BillingPendingUsage, snapshot.Billing.Disposition)
				require.Nil(t, journal.input.Evidence.Usage)
				require.Nil(t, journal.input.Settlement)
			default:
				require.NoError(t, err)
				require.True(t, snapshot.Billing.Durable)
				require.Equal(t, model.BillingSettled, snapshot.Billing.Disposition)
				require.EqualValues(t, 5, *journal.input.Evidence.Usage.InputTokens)
				require.Equal(t, 3, journal.input.Settlement.InputTokens)
			}
		})
	}
}

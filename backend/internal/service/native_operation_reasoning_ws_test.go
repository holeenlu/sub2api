package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Real local sockets exercise the actual three WS ingress consumers, their
// usage parser, Native observation and real application finalizer. The existing
// in-memory journal port is the sole persistence substitute, not a PG claim.
func TestNativeWSReasoningActualConsumersKeepRawAndCanonicalSeparate(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
		for _, sample := range []struct {
			name          string
			raw           int
			auxiliaryOnly bool
		}{
			{"independent", 2, false},
			{"already_included", 5, false},
			{"auxiliary_only", 0, true},
		} {
			t.Run(mode+"/"+sample.name, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				usage := fmt.Sprintf("{\"input_tokens\":5,\"output_tokens\":%d,\"total_tokens\":10,\"output_tokens_details\":{\"reasoning_tokens\":3}}", sample.raw)
				if sample.auxiliaryOnly {
					usage = "{\"total_tokens\":10,\"output_tokens_details\":{\"reasoning_tokens\":3}}"
				}
				terminal := []byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_reasoning_ws\",\"status\":\"completed\",\"model\":\"gpt-5.1\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":" + usage + "}}")
				var generations atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if mode == OpenAIWSIngressModeHTTPBridge {
						if _, err := io.ReadAll(req.Body); err != nil {
							return
						}
						generations.Add(1)
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: %s\n\n", terminal)
						return
					}
					socket, err := coderws.Accept(w, req, nil)
					if err != nil {
						return
					}
					defer socket.CloseNow()
					for {
						_, payload, err := socket.Read(req.Context())
						if err != nil {
							return
						}
						if gjson.GetBytes(payload, "generate").Exists() && !gjson.GetBytes(payload, "generate").Bool() {
							warmup := []byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"warmup\",\"status\":\"completed\",\"output\":[]}}")
							if socket.Write(req.Context(), coderws.MessageText, warmup) != nil {
								return
							}
							continue
						}
						generations.Add(1)
						if socket.Write(req.Context(), coderws.MessageText, terminal) != nil {
							return
						}
					}
				}))
				t.Cleanup(upstream.Close)
				f := nativeWSConsumerService(t, mode, upstream.URL)
				f.resource.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.1"}
				if mode == OpenAIWSIngressModePassthrough {
					f.resource.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-5.1"}
					f.key.Group.ModelAllowlist.Models = []string{"gpt-5.1"}
				}
				f.key.Group.Platform, f.key.Group.RateMultiplier = PlatformOpenAI, 1
				f.service.cfg.Default.RateMultiplier = 1
				f.service.billingService = NewBillingService(f.service.cfg, nil)
				journal := &nativeApplicationJournal{t: t, effects: &UsageBillingApplyResult{Applied: true}}
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				f.service.usageBillingRepo, f.service.usageLogRepo = journal, logs
				bindNativeRuntimeFixture(t, f.nativeOperationFixture)
				if mode == OpenAIWSIngressModeHTTPBridge {
					transport := &http.Transport{}
					t.Cleanup(transport.CloseIdleConnections)
					f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
						return transport.RoundTrip(req)
					}
				}
				keys := &APIKeyService{apiKeyRepo: &nativeWSConcurrentKeys{nativeAuthorityKeys: f.keys}}
				type outcome struct {
					result *OpenAIForwardResult
					facts  model.OperationResult
					err    error
				}
				completed := make(chan outcome, 1)
				failures := make(chan error, 4)
				done := make(chan struct{})
				report := func(err error) {
					select {
					case failures <- err:
					default:
					}
				}
				downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					defer close(done)
					socket, err := coderws.Accept(w, req, nil)
					if err != nil {
						report(err)
						return
					}
					defer socket.CloseNow()
					parent := openAIWSTestAdmissionContext(req.Context())
					err = RunOpenAIWSMultiplexer(parent, socket, f.service.cfg, func(lane context.Context, ds *OpenAIWSDownstream) {
						defer ds.CloseNow()
						_, first, err := ds.Read(lane)
						if err != nil {
							report(err)
							return
						}
						request := f.op.request
						request.Transport, request.OperationID = "ws", "reasoning-"+mode+"-"+sample.name
						request.RequestedModel = gjson.GetBytes(first, "model").String()
						request.APIKeyDigest = sha256.Sum256([]byte(f.key.Key))
						ctx, err := f.service.BindNativeWSDispatch(ds.TurnBudgetContext(lane, f.service.cfg, first), keys, request)
						if err != nil {
							report(err)
							return
						}
						ctx, err = f.service.BindNativeWSOperation(ctx, first)
						if err != nil {
							report(err)
							return
						}
						if err = f.service.BindNativeIntentPricing(ctx, NativeIntentPricingInput{
							APIKey: f.key, User: f.key.User, APIKeyService: keys,
							PricingAt:          time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
							ChannelUsageFields: ChannelUsageFields{OriginalModel: request.RequestedModel},
						}); err != nil {
							report(err)
							return
						}
						// Each real consumer must project its own mapped model facts.
						ctx = WithNativeIntentModelFacts(ctx, NativeIntentModelFacts{})
						if err = ds.PublishNativeWSTurn(lane, ctx); err != nil {
							report(err)
							return
						}
						user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
						if err != nil {
							report(err)
							return
						}
						defer user.ReleaseFunc()
						account, err := routing.AcquireCapacity(ctx, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: request.OperationID, Limit: f.resource.Concurrency}, nil)
						if err != nil {
							report(err)
							return
						}
						if !account.Acquired {
							report(errors.New("fixture capacity unavailable"))
							return
						}
						defer account.ReleaseFunc()
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = req.Clone(ctx)
						c.Set("api_key", f.key)
						var result *OpenAIForwardResult
						var finalErr error
						hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, observed *OpenAIForwardResult, cause error) {
							result = observed
							handled, err := f.service.FinalizeNativeWSUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource,
								PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)}, cause)
							finalErr = errors.Join(cause, err)
							if !handled {
								finalErr = errors.Join(finalErr, errors.New("actual WS finalizer did not own this turn"))
							}
						}, ReleaseTurn: func(int) {
							facts := nativeHTTPFromContext(ctx).core.Snapshot()
							account.ReleaseFunc()
							user.ReleaseFunc()
							select {
							case completed <- outcome{result: result, facts: facts, err: finalErr}:
							default:
							}
						}}
						err = f.service.ProxyResponsesWebSocketFromClient(ctx, c, ds, f.resource, "synthetic-upstream-key", first, hooks)
						if err != nil && !errors.Is(err, context.Canceled) && coderws.CloseStatus(err) < 0 {
							report(err)
						}
					})
					if err != nil && !errors.Is(err, context.Canceled) && coderws.CloseStatus(err) < 0 {
						report(err)
					}
				}))
				t.Cleanup(downstream.Close)
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http"), nil)
				require.NoError(t, err)
				t.Cleanup(func() {
					_ = client.CloseNow()
					select {
					case <-done:
					case <-time.After(2 * time.Second):
						t.Error("actual WS consumer did not join after client close")
					}
				})
				payload := []byte("{\"type\":\"response.create\",\"model\":\"client-model\",\"stream_id\":\"reasoning\",\"event_id\":\"one\",\"store\":false,\"input\":[{\"role\":\"user\",\"content\":\"hello\"}]}")
				if mode == OpenAIWSIngressModePassthrough {
					// Passthrough deliberately ignores account model_mapping. This
					// quantity-normalization test requires an actually priced wire
					// model; an unknown alias correctly remains pending pricing.
					payload = []byte(strings.Replace(string(payload), "\"client-model\"", "\"gpt-5.1\"", 1))
				}
				require.NoError(t, client.Write(ctx, coderws.MessageText, payload))
				for {
					_, message, err := client.Read(ctx)
					require.NoError(t, err)
					if gjson.GetBytes(message, "type").String() == "response.completed" {
						break
					}
				}
				var got outcome
				select {
				case got = <-completed:
				case err := <-failures:
					t.Fatal(err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				require.NoError(t, got.err)
				require.EqualValues(t, 1, generations.Load())
				require.Equal(t, 1, journal.stores)
				require.Equal(t, 1, journal.records)
				require.Zero(t, journal.old)
				require.Zero(t, logs.calls)
				require.Zero(t, f.transport.legacy.Load())
				require.Equal(t, model.OperationCompleted, got.facts.State)
				require.True(t, got.facts.Billing.Durable)
				require.NotNil(t, journal.final)
				require.Contains(t, journal.intents, journal.final.Intent.Key)
				if sample.auxiliaryOnly {
					require.Nil(t, journal.input.Evidence.Usage)
					require.Nil(t, journal.input.Settlement)
					require.Nil(t, journal.input.SettlementUsage)
					require.Equal(t, model.BillingPendingUsage, got.facts.Billing.Disposition)
				} else {
					require.Equal(t, model.BillingSettled, got.facts.Billing.Disposition)
					require.NotNil(t, journal.input.Settlement)
					require.NotNil(t, journal.input.SettlementUsage)
					require.EqualValues(t, sample.raw, *journal.input.Evidence.Usage.OutputTokens)
					require.EqualValues(t, 10, *journal.input.Evidence.Usage.TotalTokens)
					require.EqualValues(t, 3, *journal.input.Evidence.Usage.ReasoningTokens)
					require.Equal(t, billing.NativeUsageFinal, journal.input.Evidence.Confidence)
					require.Equal(t, 5, got.result.Usage.OutputTokens)
					require.Equal(t, 5, journal.input.SettlementUsage.OpenAI.Usage.OutputTokens)
					require.Equal(t, 5, journal.input.Settlement.OutputTokens)
				}
			})
		}
	}
}

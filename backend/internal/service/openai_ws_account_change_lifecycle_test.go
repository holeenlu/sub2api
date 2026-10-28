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
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Real account snapshots, actual local upstream sockets/HTTP and the production
// ingress/refresh/replay builder are used here. The harness only performs the
// handler's existing authenticated bind, slot acquisition and retry handoff.
func TestNativeWSAccountChangeReprepareKeepsOriginalRoot(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
		for _, change := range []string{"credential_rotation", "endpoint_rotation"} {
			t.Run(mode+"/"+change, func(t *testing.T) {
				type wireObservation struct{ endpoint, tag, authorization string }
				observed := make(chan wireObservation, 8)
				var handshakes atomic.Int32
				upstream := func(endpoint string) *httptest.Server {
					return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						respond := func(body []byte) []byte {
							items := gjson.GetBytes(body, "input").Array()
							tag := ""
							if len(items) > 0 {
								content := items[len(items)-1].Get("content")
								tag = content.String()
								if content.IsArray() {
									tag = content.Get("0.text").String()
								}
							}
							observed <- wireObservation{endpoint, tag, r.Header.Get("Authorization")}
							output := []any{map[string]any{"type": "message", "id": "msg_" + tag, "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "done"}}}}
							result, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_" + tag, "status": "completed", "model": "client-model", "output": output, "usage": map[string]int{"input_tokens": 2, "output_tokens": 1}}})
							return result
						}
						if mode == OpenAIWSIngressModeHTTPBridge {
							body, err := io.ReadAll(r.Body)
							if err != nil {
								return
							}
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = fmt.Fprintf(w, "data: %s\n\n", respond(body))
							return
						}
						conn, err := coderws.Accept(w, r, nil)
						if err != nil {
							return
						}
						handshakes.Add(1)
						defer conn.CloseNow()
						for {
							_, body, err := conn.Read(r.Context())
							if err != nil {
								return
							}
							if conn.Write(r.Context(), coderws.MessageText, respond(body)) != nil {
								return
							}
						}
					}))
				}
				oldPeer, nextPeer := upstream("old"), upstream("next")
				t.Cleanup(oldPeer.Close)
				t.Cleanup(nextPeer.Close)
				f := nativeWSConsumerService(t, mode, oldPeer.URL)
				if mode == OpenAIWSIngressModeHTTPBridge {
					transport := &http.Transport{}
					t.Cleanup(transport.CloseIdleConnections)
					f.transport.reply = func(r *http.Request, _ native.TransportOptions) (*http.Response, error) {
						return transport.RoundTrip(r)
					}
				}
				parent, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				admission := &openAIWSTestAdmission{}
				parent = admission.context(parent)
				conn := newQueuedWSDownstreamTestConn()
				conn.writes = make(chan []byte, 32)
				ds := NewOpenAIWSDownstream(conn, 4096, parent)
				defer ds.finishAdmissions()
				defer ds.CloseNow()
				first := []byte("{\"type\":\"response.create\",\"event_id\":\"first\",\"model\":\"client-model\",\"store\":false,\"input\":[{\"role\":\"user\",\"content\":\"first\"}]}")
				second := []byte("{\"type\":\"response.create\",\"event_id\":\"second\",\"model\":\"client-model\",\"store\":false,\"input\":[{\"role\":\"user\",\"content\":\"first\"},{\"role\":\"assistant\",\"content\":\"done\"},{\"role\":\"user\",\"content\":\"second\"}]}")
				released := make(chan model.OperationResult, 4)
				type retryCheckpoint struct {
					owner     *nativeHTTPOperation
					budget    *OpenAITurnBudget
					before    model.OperationResult
					callbacks int
					payload   []byte
				}
				reprepared := make(chan retryCheckpoint, 1)
				done := make(chan error, 1)
				go func() {
					_, payload, err := ds.Read(parent)
					if err != nil {
						done <- err
						return
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(parent)
					c.Set("api_key", f.key)
					selected := f.resource
					var current context.Context
					var slots []func()
					callbacks := 0
					release := func() {
						for _, end := range slots {
							end()
						}
						slots = nil
					}
					defer release()
					bind := func(body []byte) error {
						request := f.op.request
						request.Transport = "ws"
						request.OperationID = "rotation-" + gjson.GetBytes(body, "event_id").String()
						current, err = f.service.BindNativeWSDispatch(ds.TurnBudgetContext(parent, f.service.cfg, body), &APIKeyService{apiKeyRepo: f.keys}, request)
						if err != nil {
							return err
						}
						current, err = f.service.BindNativeWSOperation(current, body)
						if err != nil {
							return err
						}
						current = bindNativeWSConsumerPricing(t, f, current)
						if err = ds.PublishNativeWSTurn(parent, current); err != nil {
							return err
						}
						c.Request = c.Request.WithContext(current)
						return nil
					}
					acquire := func() error {
						if len(slots) > 0 {
							return nil
						}
						user, e := routing.AcquireCapacity(current, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
						if e != nil {
							return e
						}
						slots = append(slots, user.ReleaseFunc)
						account, e := routing.AcquireCapacity(current, f.capacity, routing.CapacityRequest{ID: selected.ID, Token: nativeDispatchFromContext(current).request.OperationID, Limit: selected.Concurrency}, nil)
						if e != nil {
							return e
						}
						if !account.Acquired {
							return errors.New("rotation fixture capacity denied")
						}
						slots = append(slots, account.ReleaseFunc)
						return nil
					}
					if err = bind(payload); err != nil {
						done <- err
						return
					}
					if err = acquire(); err != nil {
						done <- err
						return
					}
					token, _, err := f.service.GetAccessToken(current, selected)
					if err != nil {
						done <- err
						return
					}
					hooks := &OpenAIWSIngressHooks{
						BeforeRequest: func(_ int, body []byte, model string) error {
							if e := bind(body); e != nil {
								return e
							}
							latest, e := f.service.RefreshOpenAIWSAccount(current, c, selected, f.key.GroupID, model)
							var changed *OpenAIWSAccountChangedError
							if e != nil && !errors.As(e, &changed) {
								return e
							}
							selected = latest
							if changed != nil {
								var credentialErr error
								token, _, credentialErr = f.service.GetAccessToken(current, selected)
								if credentialErr != nil {
									return credentialErr
								}
							}
							return e
						},
						BeforeTurn: func(int) error { return acquire() },
						AfterTurn: func(_ int, result *OpenAIForwardResult, cause error) {
							callbacks++
							_, _ = finalizeNativeWSUsage(current, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: selected}, cause, nativeWSFixtureJournal(), time.Second)
						},
						ReleaseTurn: func(int) { release(); released <- nativeHTTPFromContext(current).core.Snapshot() },
					}
					err = f.service.ProxyResponsesWebSocketFromClient(current, c, ds, selected, token, payload, hooks)
					// Independent frames now return to ingress BEFORE new-turn
					// admission. Reproduce the real handoff; never let a service
					// reuse the preceding turn's operation or preparation hook.
					var next *OpenAIWSNextExecution
					if errors.As(err, &next) {
						_, nextPayload, nextModel, acceptErr := next.Accept(parent, f.service.cfg)
						if acceptErr != nil {
							done <- acceptErr
							return
						}
						err = hooks.BeforeRequest(2, nextPayload, nextModel)
						if err != nil {
							err = newOpenAIWSCurrentTurnFailoverError(err, nextPayload)
						}
					}
					var changed *OpenAIWSAccountChangedError
					if !errors.As(err, &changed) {
						done <- fmt.Errorf("missing actual account-change result: %w", err)
						return
					}
					retry, ok := OpenAIWSCurrentTurnRetryPayload(err)
					if !ok || len(retry) == 0 {
						done <- fmt.Errorf("missing approved current-turn payload: %w", err)
						return
					}
					owner := nativeHTTPFromContext(current)
					budget := nativeDispatchFromContext(current).budget
					reprepared <- retryCheckpoint{owner, budget, owner.core.Snapshot(), callbacks, retry}
					if err = acquire(); err != nil {
						done <- err
						return
					}
					err = f.service.ProxyResponsesWebSocketFromClient(current, c, ds, selected, token, retry, hooks)
					if owner != nativeHTTPFromContext(current) || budget != nativeDispatchFromContext(current).budget {
						done <- errors.New("reprepare replaced original core/budget")
						return
					}
					done <- err
				}()
				readTerminal := func(id string) {
					t.Helper()
					for {
						select {
						case b := <-conn.writes:
							if gjson.GetBytes(b, "type").String() == "response.completed" {
								require.Equal(t, id, gjson.GetBytes(b, "response.id").String())
								return
							}
						case e := <-done:
							t.Fatalf("ingress ended before %s: %v", id, e)
						case <-parent.Done():
							t.Fatal(parent.Err())
						}
					}
				}
				conn.input <- first
				readTerminal("resp_first")
				firstFacts := wsLifecycleReceive(t, released)
				require.Equal(t, model.OperationCompleted, firstFacts.State)
				require.True(t, firstFacts.Billing.Durable)
				oldWire := wsLifecycleReceive(t, observed)
				require.Equal(t, "old", oldWire.endpoint)
				require.Equal(t, "first", oldWire.tag)
				latest := *f.resource
				latest.Credentials = maps.Clone(f.resource.Credentials)
				if change == "credential_rotation" {
					latest.Credentials["api_key"] = "rotated-unit-key"
				} else {
					latest.Credentials["base_url"] = nextPeer.URL
				}
				repo := f.service.accountRepo.(*nativeWSConcurrentAccounts)
				repo.mu.Lock()
				repo.accounts[latest.ID] = &latest
				repo.mu.Unlock()
				conn.input <- second
				var checkpoint retryCheckpoint
				select {
				case checkpoint = <-reprepared:
				case e := <-done:
					t.Fatalf("rotation did not reach reprepare: %v", e)
				case <-parent.Done():
					t.Fatal(parent.Err())
				}
				require.Equal(t, "rotation-second", checkpoint.before.OperationID)
				require.Equal(t, model.OperationWaiting, checkpoint.before.State, "pre-send account change must not finish the root")
				require.Empty(t, checkpoint.before.Attempts)
				require.Equal(t, 1, checkpoint.callbacks, "unsent second turn never entered AfterTurn")
				require.Equal(t, "second", gjson.GetBytes(checkpoint.payload, "event_id").String())
				readTerminal("resp_second")
				secondFacts := wsLifecycleReceive(t, released)
				require.Equal(t, model.OperationCompleted, secondFacts.State)
				require.True(t, secondFacts.Billing.Durable)
				require.Equal(t, checkpoint.before.OperationID, secondFacts.OperationID)
				require.Len(t, secondFacts.Attempts, 1)
				require.Equal(t, model.SendSent, secondFacts.Attempts[0].Send)
				require.Equal(t, 1, checkpoint.budget.attemptBudget.Snapshot().Attempts)
				newWire := wsLifecycleReceive(t, observed)
				require.Equal(t, "second", newWire.tag)
				if change == "credential_rotation" {
					require.Equal(t, "Bearer rotated-unit-key", newWire.authorization)
					require.Equal(t, "old", newWire.endpoint)
				} else {
					require.Equal(t, oldWire.authorization, newWire.authorization)
					require.Equal(t, "next", newWire.endpoint)
				}
				require.NoError(t, ds.CloseNow())
				endErr := wsLifecycleReceive(t, done)
				require.False(t, errors.Is(endErr, execution.ErrOperationConsumed), "%v", endErr)
				ds.finishAdmissions()
				waitOpenAIWSAdmissions(t, admission, 2, 2, 0)
				require.Zero(t, f.transport.legacy.Load())
				if mode == OpenAIWSIngressModeHTTPBridge {
					require.EqualValues(t, 2, f.transport.sends.Load())
				} else {
					require.EqualValues(t, 2, handshakes.Load())
				}
				select {
				case extra := <-observed:
					t.Fatalf("unexpected regeneration: %+v", extra)
				default:
				}
			})
		}
	}
}

func TestNativeWSAccountChangeCannotReviveObservedAttempt(t *testing.T) {
	for _, progress := range []string{"unknown", "output", "metered"} {
		t.Run(progress, func(t *testing.T) {
			f, conn := nativeWSLifecycleFixture(t, t.Context())
			body := []byte("{\"type\":\"response.create\",\"model\":\"client-model\",\"input\":[{\"role\":\"user\",\"content\":\"hi\"}]}")
			require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, body))
			if progress == "output" {
				frame := []byte("{\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}")
				require.NoError(t, conn.attempt.observe(frame, nil))
				conn.attempt.delivered(frame, nil)
			}
			if progress == "metered" {
				require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"error\",\"usage\":{\"input_tokens\":2}}"), nil))
			}
			conn.attempt.close(io.ErrUnexpectedEOF)
			latest := *f.resource
			latest.Credentials = maps.Clone(f.resource.Credentials)
			latest.Credentials["api_key"] = "rotated-unit-key"
			f.accounts.accounts[latest.ID] = &latest
			_, changed := f.service.RefreshOpenAIWSAccount(f.ctx, nil, f.resource, f.key.GroupID, "client-model")
			var changedType *OpenAIWSAccountChangedError
			require.ErrorAs(t, changed, &changedType)
			input, exists, err := openAIWSExtractNormalizedInputSequence(body)
			require.NoError(t, err)
			require.True(t, exists)
			proposal := f.service.openAIWSAccountRefreshRetry(f.resource, changed, body, input, true, "client-model")
			hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) {
				_, _ = finalizeNativeWSUsage(f.ctx, &OpenAIRecordUsageInput{Result: r, APIKey: f.key, User: f.key.User, Account: f.resource}, e, nativeWSFixtureJournal(), time.Second)
			}}
			require.Error(t, completeOpenAIWSTurn(f.ctx, hooks, 1, nil, proposal))
			require.Equal(t, model.OperationFailed, nativeHTTPFromContext(f.ctx).core.Snapshot().State)
			require.Error(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, body))
			require.Equal(t, 1, conn.frames, "changed credentials do not prove an observed attempt was never sent")
		})
	}
}

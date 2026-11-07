package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Reuse both existing transport fixtures: BPS uses HTTPUpstream.Do, while an
// accidental Native bridge must reach the real permit-consuming transport.
// A missing Native dispatcher must not make the no-Native-send assertion pass.
type bpsRouteReviewUpstream struct {
	*nativeOperationTransport
	bps *httpUpstreamRecorder
}

func (u *bpsRouteReviewUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	if req.URL.Path == "/basispoints/api/responses" {
		return u.bps.Do(req, proxy, id, concurrency)
	}
	return u.nativeOperationTransport.Do(req, proxy, id, concurrency)
}

func TestBPSRouteReviewWSContinuationGlobalDisableDuringHook(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		toolOutput, previous bool
	}{
		{name: "previous", previous: true},
		{name: "previous_with_tool_output", toolOutput: true, previous: true},
		{name: "implicit_tool_output", toolOutput: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNativeOperationFixture(t, false, 3)
			svc := f.service
			svc.cfg = passthroughLifecycleConfig()
			svc.cfg.Gateway.MaxBodySize = 1 << 20
			svc.cfg.Gateway.OpenAITurnMaxAttempts = 3
			svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
			svc.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
			svc.settingService = excelBPSSettingsTestService()
			settings := svc.settingService.settingRepo.(*excelBPSImageSettingsRepo)
			svc.cache = &nativeWSConcurrentCache{}
			account := excelBPSDualGrantTestAccount(t, svc)
			account.GroupIDs = []int64{*f.key.GroupID}
			account.Concurrency = 0 // Real unlimited-capacity receipts, no synthetic lease.
			f.accounts.accounts[account.ID] = account
			f.key.Group.ModelAllowlist.Models = []string{"gpt-5.1"}
			require.False(t, account.IsExcelOAuth(), "Native must remain independently authorized")
			first := []byte(`{"type":"response.create","event_id":"review-first","model":"gpt-5.1","input":"first question"}`)
			wire := bpsCompletedStream("resp_review_first", "first answer")
			if tc.toolOutput {
				first = []byte(`{"type":"response.create","event_id":"review-first","model":"gpt-5.1","input":"run the tool","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`)
				wire = excelBPSRepairWire(t, "review_tool", "codex2api.custom/functions.exec", "text(42);")
			}
			bps := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, wire)}
			svc.httpUpstream = &bpsRouteReviewUpstream{nativeOperationTransport: f.transport, bps: bps}
			f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
				return bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_review_wrong_native", "unexpected Native generation")), nil
			}

			parent, cancel := context.WithTimeout(openAIWSTestAdmissionContext(t.Context()), 8*time.Second)
			conn := newQueuedWSDownstreamTestConn()
			conn.writes = make(chan []byte, 64)
			ds := NewOpenAIWSDownstream(conn, 1<<20, parent)
			conn.input <- first
			_, payload, err := ds.Read(parent)
			require.NoError(t, err)
			initial := ds.TurnBudgetContext(parent, svc.cfg, payload)
			firstBudget := ds.budget.Load()
			c, _ := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(initial), f.key)
			SetOpenAIClientTransport(c, OpenAIClientTransportWS)
			BeginOpsStreamTurn(c, 1)
			entered, resume, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var resumeOnce sync.Once
			unblock := func() { resumeOnce.Do(func() { close(resume) }) }
			done := make(chan error, 1)
			turns := make(chan bpsWSTestTurn, 2)
			var secondBudget *OpenAITurnBudget
			t.Cleanup(func() {
				unblock()
				cancel()
				_ = ds.CloseNow()
				select {
				case <-exited:
				case <-time.After(time.Second):
					t.Error("review WS coordinator did not join")
				}
				ds.finishAdmissions()
				// Keep the zero-send invariant on early parse/admission failures,
				// too; those failures never reach the assertions after the hook.
				require.Zero(t, f.transport.sends.Load(), "a BPS predecessor/tool result must never reach the Native transport")
				require.Zero(t, f.transport.legacy.Load())
			})
			go func() {
				defer close(exited)
				var releases []func()
				defer func() {
					for _, release := range releases {
						release()
					}
				}()
				hooks := &OpenAIWSIngressHooks{
					BeforeRequest: func(turn int, body []byte, _ string) error {
						if turn != 2 {
							return fmt.Errorf("unexpected follow-up turn %d", turn)
						}
						// Mirror the handler's new-turn binding. The preceding BPS
						// producer must constrain this root even after history expansion.
						base := ds.TurnBudgetContext(parent, svc.cfg, body)
						secondBudget = ds.budget.Load()
						request := f.op.request
						request.Transport, request.OperationID, request.RequestedModel = "ws", "bps-route-review-second", "gpt-5.1"
						keys := &APIKeyService{apiKeyRepo: f.keys}
						current, bindErr := svc.BindNativeWSDispatch(base, keys, request)
						if bindErr != nil {
							return bindErr
						}
						current, bindErr = svc.BindNativeWSOperation(current, body)
						if bindErr != nil {
							return bindErr
						}
						if bindErr = svc.BindNativeIntentPricing(current, NativeIntentPricingInput{APIKey: f.key, User: f.key.User,
							APIKeyService: keys, PricingAt: time.Now(), ChannelUsageFields: ChannelUsageFields{OriginalModel: "gpt-5.1"}}); bindErr != nil {
							return bindErr
						}
						if bindErr = ds.PublishNativeWSTurn(parent, current); bindErr != nil {
							return bindErr
						}
						c.Request = c.Request.WithContext(current)
						for _, capacity := range []routing.CapacityRequest{{ID: f.key.UserID, User: true}, {ID: account.ID}} {
							lease, leaseErr := routing.AcquireCapacity(current, nil, capacity, nil)
							if leaseErr != nil {
								return leaseErr
							}
							releases = append(releases, lease.ReleaseFunc)
						}
						return nil
					},
					BeforeTurn: func(turn int) error {
						if turn != 2 {
							return nil
						}
						close(entered)
						select {
						case <-resume:
							return nil
						case <-parent.Done():
							return parent.Err()
						}
					},
					AfterTurn: func(_ int, result *OpenAIForwardResult, cause error) { turns <- bpsWSTestTurn{result, cause} },
				}
				done <- svc.ProxyResponsesWebSocketFromClient(initial, c, ds, account, account.GetOpenAIAccessToken(), payload, hooks)
			}()

			var completed []byte
			for {
				var event []byte
				select {
				case event = <-conn.writes:
				case failure := <-done:
					t.Fatalf("first BPS turn ended before a response: %v", failure)
				case <-parent.Done():
					t.Fatal("first BPS response was not delivered")
				}
				if gjson.GetBytes(event, "type").String() == "response.completed" {
					completed = event
					break
				}
				require.NotEqual(t, "error", gjson.GetBytes(event, "type").String())
				require.NotEqual(t, "response.failed", gjson.GetBytes(event, "type").String())
			}
			firstResult := requireBPSContinuationIntegrationTurn(t, turns)
			require.Equal(t, UsageAuthorizationModeBPS, firstResult.AuthorizationMode)
			require.Equal(t, "gpt-5.1", firstResult.UpstreamModel, "BPS must not apply a Native-only retirement alias after route selection")
			require.True(t, firstResult.Usage.HasTokens(), "the producer must finish with real fixture usage")
			id := gjson.GetBytes(completed, "response.id").String()
			require.NotEmpty(t, id)
			input := `[{"role":"user","content":"continue the same answer"}]`
			toolsField := ""
			if tc.toolOutput {
				callID := gjson.GetBytes(completed, "response.output.0.call_id").String()
				require.NotEmpty(t, callID)
				require.Equal(t, "custom_tool_call", gjson.GetBytes(completed, "response.output.0.type").String())
				require.True(t, firstResult.wsReplayInputExists)
				require.False(t, firstResult.wsReplayIncomplete, "implicit continuation requires complete retained history")
				knownCall := false
				for _, item := range firstResult.wsReplayInput {
					if gjson.GetBytes(item, "type").String() == "custom_tool_call" && gjson.GetBytes(item, "call_id").String() == callID {
						knownCall = true
					}
				}
				require.True(t, knownCall, "the reply must match the call actually delivered and retained on this socket")
				input = fmt.Sprintf(`[{"type":"custom_tool_call_output","call_id":%q,"output":"tool returned 42"}]`, callID)
				if !tc.previous {
					// Supply the already accepted catalog explicitly, so this case
					// isolates history inference from omitted-tools inheritance.
					// Do not add a call item or previous_response_id: either would
					// bypass the implicit continuation path being reviewed.
					toolsField = `,"tools":` + gjson.GetBytes(first, "tools").Raw
				}
			}
			previousField := ""
			if tc.previous {
				previousField = fmt.Sprintf(`,"previous_response_id":%q`, id)
			}
			next := []byte(fmt.Sprintf(`{"type":"response.create","event_id":"review-second","model":"gpt-5.1"%s%s,"input":%s}`, previousField, toolsField, input))
			if tc.toolOutput && !tc.previous {
				coverage := AnalyzeToolCallOutputContextCoverageBytes(next)
				require.True(t, coverage.HasFunctionCallOutput)
				require.False(t, coverage.ContextCoversAllCallIDs, "only the proven preceding turn supplies the call")
				mode, modeErr := svc.getOpenAIWSStateStore().GetResponseMode(parent, *f.key.GroupID, id)
				require.NoError(t, modeErr)
				require.Equal(t, session.ResponseModeBPS, mode)
				owned, ownerErr := svc.ValidateOpenAIHTTPResponseOwner(parent, *f.key.GroupID, id, f.key.UserID, f.key.ID)
				require.NoError(t, ownerErr)
				require.True(t, owned)
			}
			conn.input <- next
		awaitAdmission:
			for {
				select {
				case <-entered:
					break awaitAdmission
				case event := <-conn.writes:
					if gjson.GetBytes(event, "type").String() == "keepalive" {
						continue
					}
					// parseNextValidPayload reports a request error and then waits
					// for another frame. Observe that error immediately; otherwise
					// its later idle timeout conceals the admission failure.
					t.Fatalf("second frame rejected/emitted before global-disable hook: type=%q event_id=%q code=%q message=%q producer=%q; the mode-switch assertion was not reached",
						gjson.GetBytes(event, "type").String(), gjson.GetBytes(event, "event_id").String(),
						gjson.GetBytes(event, "error.code").String(), gjson.GetBytes(event, "error.message").String(), id)
				case err := <-done:
					t.Fatalf("continuation failed before the selected admission hook (global BPS still enabled): %v", err)
				case <-parent.Done():
					t.Fatal("second turn never reached the admission hook")
				}
			}
			require.NotSame(t, firstBudget, secondBudget, "a new turn gets a fresh allowance, but must retain its producer mode")
			require.NoError(t, settings.SetMultiple(parent, map[string]string{SettingKeyExcelBPSEnabled: "false"}))
			svc.settingService.invalidateExcelBPSEnabledCache()
			unblock()

			var stopped error
			select {
			case stopped = <-done:
			case turn := <-turns:
				stopped = turn.err
				_ = ds.CloseNow()
				select {
				case finalErr := <-done:
					stopped = errors.Join(stopped, finalErr)
				case <-parent.Done():
					t.Fatal("second-turn coordinator did not stop")
				}
			case <-parent.Done():
				t.Fatal("global disable did not stop the BPS continuation")
			}
			require.Zero(t, f.transport.sends.Load(), "a BPS predecessor/tool result must never reach the Native transport")
			require.Zero(t, f.transport.legacy.Load())
			require.Len(t, bps.requests, 1, "the disabled route must not generate another BPS response either")
			require.Error(t, stopped)
			var failure *UpstreamFailoverError
			require.ErrorAs(t, stopped, &failure, "a missing dispatcher or billing fixture is not a routing rejection")
			require.EqualValues(t, "selection", failure.Stage)
			require.True(t, failure.RequestNotSent)
			require.False(t, failure.ShouldReportAccountScheduleFailure())
		})
	}
}

func TestBPSRouteReviewOwnerSurvivesModeAndAffinityEviction(t *testing.T) {
	cache := &stubGatewayCache{}
	writer := NewOpenAIWSStateStore(cache)
	ctx := t.Context()
	const groupID, userID, keyID, accountID = int64(23), int64(19), int64(17), int64(300)
	const previous = "resp_review_evicted_bps"
	require.NoError(t, writer.BindResponseMode(ctx, groupID, previous, session.ResponseModeBPS, time.Hour))
	require.NoError(t, writer.BindResponseAccount(ctx, groupID, previous, accountID, time.Hour))
	require.NoError(t, writer.BindHTTPResponseOwner(ctx, groupID, previous, userID, keyID, time.Hour))
	// Only routing keys are evicted. A fresh gateway must not rely on the
	// producer's process-local account affinity to recognize an owned response.
	cache.sessionBindings = nil
	reader := NewOpenAIWSStateStore(cache)
	svc := excelBPSGatewayTestService(nil)
	svc.cache, svc.openaiWSStateStore = cache, reader
	owned, err := svc.ValidateOpenAIHTTPResponseOwner(ctx, groupID, previous, userID, keyID)
	require.NoError(t, err)
	require.True(t, owned)
	account, err := reader.GetResponseAccount(ctx, groupID, previous)
	require.NoError(t, err)
	require.Zero(t, account)
	t.Run("owned_bps_is_not_external_unknown", func(t *testing.T) {
		_, err := reader.GetResponseMode(ctx, groupID, previous)
		require.ErrorIs(t, err, session.ErrResponseModeUnavailable)
		err = svc.checkResponseContinuationMode(EnsureOpenAITurnBudget(ctx, svc.cfg), groupID, previous, session.ResponseModeNative)
		require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
	})
	t.Run("external_persisted_id_remains_unknown", func(t *testing.T) {
		mode, err := reader.GetResponseMode(ctx, groupID, "resp_external_persisted")
		require.NoError(t, err)
		require.Equal(t, session.ResponseModeUnknown, mode)
		require.NoError(t, svc.checkResponseContinuationMode(ctx, groupID, "resp_external_persisted", session.ResponseModeNative))
	})
}

// Fault only the mode lookup: successful owner/affinity reads cannot disguise
// a dependency failure as an absent predecessor or unknown external response.
type bpsRouteReviewModeFailureCache struct {
	stubGatewayCache
	modeErr error
}

func (c *bpsRouteReviewModeFailureCache) GetSessionAccountID(ctx context.Context, groupID int64, key string) (int64, error) {
	if strings.HasPrefix(key, "mode:v1:") {
		return 0, c.modeErr
	}
	return c.stubGatewayCache.GetSessionAccountID(ctx, groupID, key)
}

func TestBPSRouteReviewModeDependencyStays503AtConsumers(t *testing.T) {
	for _, consumer := range []string{"forward", "scheduler"} {
		t.Run(consumer, func(t *testing.T) {
			dependency := errors.New("review mode-cache dependency unavailable")
			cache := &bpsRouteReviewModeFailureCache{modeErr: dependency}
			store := NewOpenAIWSStateStore(cache)
			key := newBPSContinuationIntegrationKey()
			account := Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
				Schedulable: true, Concurrency: 2, GroupIDs: []int64{*key.GroupID}, Credentials: map[string]any{"api_key": "review-native-key"}}
			upstream := &httpUpstreamRecorder{}
			capacity := &openAIWSStickyLeaseCache{}
			svc := excelBPSGatewayTestService(upstream)
			svc.cfg = newOpenAIWSV2TestConfig()
			svc.accountRepo = stubOpenAIAccountRepo{accounts: []Account{account}}
			svc.cache, svc.openaiWSStateStore = cache, store
			svc.concurrencyService = NewConcurrencyService(capacity)
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			const previous = "resp_review_persisted_native"
			require.NoError(t, store.BindResponseAccount(ctx, *key.GroupID, previous, account.ID, time.Hour))
			require.NoError(t, store.BindResponseMode(ctx, *key.GroupID, previous, session.ResponseModeNative, time.Hour))
			require.NoError(t, store.BindHTTPResponseOwner(ctx, *key.GroupID, previous, key.UserID, key.ID, time.Hour))
			owned, err := svc.ValidateOpenAIHTTPResponseOwner(ctx, *key.GroupID, previous, key.UserID, key.ID)
			require.NoError(t, err)
			require.True(t, owned)
			if consumer == "scheduler" {
				var selection *AccountSelectionResult
				selection, _, err = newDefaultOpenAIAccountScheduler(svc, nil).Select(ctx, OpenAIAccountScheduleRequest{
					GroupID: key.GroupID, PreviousResponseID: previous, RequestedModel: "gpt-5.1",
					RequiredTransport: OpenAIUpstreamTransportAny, RequiredCapability: OpenAIEndpointCapabilityResponses, Platform: PlatformOpenAI,
				})
				if selection != nil && selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				require.Nil(t, selection)
			} else {
				c, _ := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx), key)
				var result *OpenAIForwardResult
				result, err = svc.Forward(ctx, c, &account, []byte(fmt.Sprintf(`{"model":"gpt-5.1","previous_response_id":%q,"input":"continue"}`, previous)))
				require.Nil(t, result)
			}
			require.ErrorIs(t, err, dependency, "the scheduler must preserve the authoritative dependency error")
			require.NotErrorIs(t, err, ErrOpenAIContinuationUnavailable, "HTTP/WS consumers map this sentinel to 400 and advise deleting previous_response_id")
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, http.StatusServiceUnavailable, failure.ClientStatusCode)
			require.Equal(t, http.StatusServiceUnavailable, failure.StatusCode)
			require.True(t, failure.RequestNotSent)
			require.False(t, failure.ShouldRetryNextAccount())
			require.False(t, failure.ShouldReportAccountScheduleFailure())
			for _, forbidden := range []string{"previous_response_not_found", "without previous_response_id", "resend the complete conversation", "remove previous_response_id"} {
				require.NotContains(t, strings.ToLower(failure.ClientMessage+" "+err.Error()), forbidden)
			}
			require.Empty(t, capacity.attempts, "a mode-state outage must stop before acquiring or switching accounts")
			require.Empty(t, upstream.requests)
		})
	}
}

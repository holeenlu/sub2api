package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type cloudBillingInheritanceStore struct {
	OpenAICloudSessionStore
	record *OpenAICloudSessionRecord
	err    error
}

func (s *cloudBillingInheritanceStore) LoadCloudSession(context.Context, string) (*OpenAICloudSessionRecord, error) {
	return s.record, s.err
}

func TestCloudNewTurnFreezesInheritedReasoningEffort(t *testing.T) {
	svc, input, _ := cloudBillingFixture(t)
	input.APIKey.Group.ModelPricing[0].ReasoningEffortMultipliers = map[string]float64{"high": 2}
	previousCtx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	previousCtx, err = svc.FinalizeCloudBillingSnapshot(previousCtx, "gpt-5.1", "", "high")
	require.NoError(t, err)
	owner := OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}
	binding := OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "session", RootRequestID: "previous-root", Status: "completed"}
	store := &cloudBillingInheritanceStore{record: &OpenAICloudSessionRecord{OpenAICloudBinding: binding, BillingSnapshot: CloudBillingSnapshotFromContext(previousCtx)}}
	svc.cloudSessions = &OpenAICloudSessions{store: store}
	body := []byte(`{"model":"gpt-5.1","previous_response_id":"resp_cloud_old","input":"next"}`)
	effort, err := svc.resolveCloudBillingEffort(t.Context(), &binding, owner, body)
	require.NoError(t, err)
	require.Equal(t, "high", effort)
	protocol, err := parseCloudProtocolRequest(body)
	require.NoError(t, err)
	require.Empty(t, protocol.effort, "billing must not synthesize a partial reasoning object")
	nextCtx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	nextCtx, err = svc.FinalizeCloudBillingSnapshot(nextCtx, "gpt-5.1", "", effort)
	require.NoError(t, err)
	snapshot := cloudBillingContext(nextCtx)
	require.Equal(t, "high", snapshot.ReasoningEffort)
	result := &OpenAIForwardResult{Model: "gpt-5.1", ReasoningEffort: &effort}
	cost, err := svc.cloudFrozenCost(nextCtx, snapshot, result, []string{"gpt-5.1"}, UsageTokens{InputTokens: 1_000_000}, snapshot.TokenMultiplier, "", false)
	require.NoError(t, err)
	require.InDelta(t, 8, cost.ActualCost, 1e-9, "2 input price × 2 group rate × 2 inherited reasoning multiplier")
	// Explicit new fields are never silently rewritten; the actor still checks
	// whether changing those settings on this existing thread is supported.
	explicit := []byte(`{"model":"gpt-5.1","input":"next","reasoning":{"effort":"low"}}`)
	effort, err = svc.resolveCloudBillingEffort(t.Context(), &binding, owner, explicit)
	require.NoError(t, err)
	require.Equal(t, "low", effort)
}

func TestCloudInheritedBillingRejectsChangedBindingOrSnapshot(t *testing.T) {
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "model", SettingsDigest: "settings"}
	binding := OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "session", RootRequestID: "root", Status: "completed"}
	group := int64(3)
	snapshot := OpenAICloudBillingSnapshot{Version: 1, UserID: 1, APIKeyID: 2, GroupID: &group, AccountID: 4, UpstreamModel: "model", ReasoningEffort: "high"}
	for _, scenario := range []string{"user", "key", "group", "generation", "settings", "root", "active turn", "snapshot model", "lookup failure"} {
		t.Run(scenario, func(t *testing.T) {
			record := &OpenAICloudSessionRecord{OpenAICloudBinding: binding}
			copy := snapshot
			store := &cloudBillingInheritanceStore{record: record}
			gateway := &OpenAIGatewayService{cloudSessions: &OpenAICloudSessions{store: store}}
			switch scenario {
			case "user":
				record.UserID++
			case "key":
				record.APIKeyID++
			case "group":
				record.GroupID++
			case "generation":
				record.GrantGeneration = "new"
			case "settings":
				record.SettingsDigest = "new"
			case "root":
				record.RootRequestID = "other"
			case "active turn":
				record.Status = "running"
			case "snapshot model":
				copy.UpstreamModel = "other"
			case "lookup failure":
				store.err = errors.New("unavailable")
			}
			record.BillingSnapshot, _ = json.Marshal(copy)
			_, err := gateway.resolveCloudBillingEffort(t.Context(), &binding, owner, []byte(`{"model":"model","input":"next"}`))
			require.Error(t, err)
		})
	}
}

func TestCloudContinuationAdmissionLeavesNativeRequestsAlone(t *testing.T) {
	s := &OpenAIGatewayService{}
	ctx := context.Background()
	for _, body := range []string{
		`{"model":"native","input":"` + strings.Repeat("x", OpenAICloudMaxRequestBytes) + `"}`,
		`{"model":"native","input":[{"type":"function_call_output","output":"legacy bootstrap"}]}`,
		`{"model":"native","previous_response_id":"resp_native","input":"next"}`,
	} {
		got, err := s.PrepareCloudContinuation(ctx, 1, 2, 3, []byte(body))
		require.NoError(t, err)
		require.Equal(t, ctx, got)
	}
	_, err := s.PrepareCloudContinuation(ctx, 1, 2, 3, []byte(`{"model":"cloud","previous_response_id":"resp_cloud_missing","input":"next"}`))
	require.Error(t, err)
}

func TestCloudConfiguredAccountsCannotFallBackToOtherModes(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICloudEnabledKey: true}, Credentials: map[string]any{"model_mapping": map[string]any{"model": "upstream-model", "wild-*": "upstream-model"}}}
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponsesCompatible))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.True(t, a.IsModelSupported("model"))
	require.False(t, a.IsModelSupported("other"))
	require.True(t, a.IsModelSupported("wild-small"))
	require.False(t, a.IsModelSupported("upstream-model"), "mapping targets do not become public whitelist aliases")
	require.False(t, supportsOpenAIRequestModel(a, "model", true))
	a.Extra["openai_excel_bps"] = true
	require.True(t, a.IsOpenAICloudConfigured())
	require.True(t, a.IsOpenAICloudEnabled())
	require.False(t, a.IsExcelBPSEnabled(), "Cloud takes precedence without clearing the BPS switch")
	a.Credentials["client_id"] = "unsupported-client"
	require.False(t, a.IsOpenAICloudEnabled())
	require.False(t, a.IsExcelBPSEnabled(), "an invalid Cloud selection must not enable BPS")
	// An invalid cached configuration stays on the rejecting Cloud route.
	require.False(t, (&OpenAIGatewayService{}).isOpenAIAccountTransportCompatible(context.Background(), a, OpenAIUpstreamTransportResponsesWebsocketV2Ingress))
}

func TestCloudForwardSchedulingRequiresCredibleTerminal(t *testing.T) {
	for _, tc := range []struct {
		event          string
		disconnected   bool
		known, success bool
	}{
		{"response.completed", false, true, true},
		{"response.completed", true, false, false},
		{"response.failed", false, true, false},
		{"response.failed", true, false, false},
		{"", false, false, false},
		{"response.cancelled", true, false, false},
	} {
		result := &OpenAIForwardResult{CloudManagedBilling: true, UpstreamTerminalEvent: tc.event, ClientDisconnect: tc.disconnected}
		require.Equal(t, tc.known, result.SchedulingOutcomeKnown())
		require.Equal(t, tc.success, result.SucceededForScheduling())
	}
}

func TestCloudSchedulerAcceptsResponsesIngressOnly(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICloudEnabledKey: true}}
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{cloudSessions: &OpenAICloudSessions{}}}
	for _, cap := range []OpenAIEndpointCapability{OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityResponsesCompatible} {
		_, reason := scheduler.isAccountRequestCompatibleReason(t.Context(), a, OpenAIAccountScheduleRequest{RequiredCapability: cap})
		require.NotEqual(t, "cloud_endpoint_unsupported", reason)
	}
	_, reason := scheduler.isAccountRequestCompatibleReason(t.Context(), a, OpenAIAccountScheduleRequest{RequiredCapability: OpenAIEndpointCapabilityChatCompletions})
	require.Equal(t, "cloud_endpoint_unsupported", reason)
	require.True(t, scheduler.isAccountTransportCompatible(t.Context(), a, OpenAIUpstreamTransportResponsesWebsocketV2Ingress))
}

func TestCloudIndependentTurnClearsInheritedContinuation(t *testing.T) {
	svc := &OpenAIGatewayService{}
	ctx := context.WithValue(t.Context(), cloudContinuationKey{}, &OpenAICloudBinding{RootRequestID: "old-root", Status: "awaiting_tool_outputs"})
	for _, body := range []string{`{"model":"model","input":"new independent turn"}`, `{"type":"response.create","model":"model","input":"new independent turn"}`} {
		fresh, err := svc.PrepareCloudContinuation(ctx, 1, 2, 3, []byte(body))
		require.NoError(t, err)
		require.False(t, HasOpenAICloudContinuation(fresh))
		require.True(t, HasOpenAICloudContinuation(ctx))
	}
}

func TestCloudRecoverySelectsNextAccountUsingItsConfiguredProtocol(t *testing.T) {
	for _, mode := range []string{"cloud", "bps", "native"} {
		t.Run(mode, func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			a := Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 2, Extra: map[string]any{OpenAICloudEnabledKey: true}}
			b := a
			b.ID, b.Priority = 12, 10
			b.Extra = map[string]any{OpenAICloudEnabledKey: mode == "cloud", "openai_excel_bps": mode == "bps"}
			var acquired, released []int64
			svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{a, b}},
				cfg: newSchedulerTestOpenAIWSV2Config(), cloudSessions: &OpenAICloudSessions{}, cache: &schedulerTestGatewayCache{},
				settingService:     excelBPSSettingsTestService(),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, releasedIDs: &released})}
			ctx := WithSchedulingQuotaRequest(EnsureOpenAITurnBudget(t.Context(), svc.cfg), []byte(`{"model":"gpt-5.1","input":"hi"}`), ContentModerationProtocolOpenAIResponses)
			pick := func(excluded map[int64]struct{}) *AccountSelectionResult {
				result, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-5.1", excluded, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityResponses, false, false, false)
				require.NoError(t, err)
				require.True(t, result.Acquired)
				return result
			}
			first := pick(nil)
			require.Equal(t, a.ID, first.Account.ID)
			first.ReleaseFunc()
			failure := cloudPreInferenceFailover(ctx, []byte(`{"model":"gpt-5.1","input":"hi"}`), 429, "cloud_quota_exhausted", "quota", nil, true)
			ExcludeTurnRecoveryAccount(ctx, first.Account, failure)
			next := pick(map[int64]struct{}{a.ID: {}})
			require.Equal(t, b.ID, next.Account.ID)
			require.Equal(t, mode == "cloud", next.Account.IsOpenAICloudConfigured())
			require.Equal(t, mode == "bps", next.Account.IsExcelBPSEnabledForModel("gpt-5.1"))
			next.ReleaseFunc()
			require.Equal(t, acquired, released)
			budget := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
			require.Empty(t, budget.bpsRecovery.rejected, "Cloud failure cannot start a BPS-only recovery phase")
			scheduler := svc.getOpenAIAccountScheduler(ctx).(*defaultOpenAIAccountScheduler)
			req := OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.1", RequiredTransport: OpenAIUpstreamTransportHTTPSSE}
			require.Equal(t, "cloud:small:medium", scheduler.schedulingHealthCohort(ctx, &a, req))
			require.Equal(t, "cloud", schedulingHealthMode(UsageAuthorizationModeCloud))
			require.Equal(t, "cloud:small:medium", reportedSchedulingHealthCohort(ctx, &a), "preparation failures have no send target but still belong to Cloud")
		})
	}
}

func TestCloudFinalFailureUsesExistingOpsOutcome(t *testing.T) {
	for _, scenario := range []struct {
		name                            string
		stream, websocket, preInference bool
		cyber                           bool
	}{
		{name: "HTTP JSON"},
		{name: "HTTP SSE", stream: true},
		{name: "WS bridge", stream: true, websocket: true},
		{name: "safe failover", stream: true, preInference: true},
		{name: "HTTP cyber policy", stream: true, cyber: true},
		{name: "WS cyber policy", stream: true, websocket: true, cyber: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			svc, in, _, wire := cloudForwardFixture(t, nil)
			info, code, status := "serverOverloaded", "server_is_overloaded", http.StatusServiceUnavailable
			if scenario.cyber {
				info, code, status = "cyberPolicy", "cyber_policy", http.StatusForbidden
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
			require.NoError(t, err)
			done := make(chan struct{})
			go func() {
				defer close(done)
				cloudWireStart(t, ctx, wire)
				if !scenario.preInference {
					cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 0, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 10}}}})
				}
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"codexErrorInfo": info}}}})
			}()
			defer func() { cancel(); <-done }()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
			c.Set("api_key", in.APIKey)
			SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
			if scenario.websocket {
				SetOpenAIClientTransport(c, OpenAIClientTransportWS)
				BeginOpsStreamTurn(c, 1)
				_, err = svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, []byte(`{"model":"gpt-5.1","input":"hi","stream":true}`), "client-model", func([]byte) error { return nil })
			} else {
				_, err = svc.Forward(ctx, c, in.Account, []byte(fmt.Sprintf(`{"model":"client-model","input":"hi","stream":%t}`, scenario.stream)))
			}
			if scenario.preInference {
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.Empty(t, GetOpsStreamErrors(c), "an attempt that can fail over is not a final request failure")
				return
			}
			require.NoError(t, err)
			if scenario.cyber {
				mark := GetOpsCyberPolicy(c)
				require.NotNil(t, mark)
				require.Equal(t, "cyber_policy", mark.Code)
				require.EqualValues(t, 10, mark.UpstreamInTok)
				var fields map[string]any
				require.NoError(t, json.Unmarshal([]byte(mark.Body), &fields))
				require.Len(t, fields, 1)
				require.Contains(t, fields, "error", "audit evidence must not include output or encrypted history")
			}
			failures := GetOpsStreamErrors(c)
			require.Len(t, failures, 1)
			require.Equal(t, status, failures[0].IntendedStatus)
			require.Equal(t, code, failures[0].Code)
			require.True(t, failures[0].CountTowardsSLA)
			require.Equal(t, !scenario.stream, failures[0].NonStream)
			if !scenario.websocket {
				require.Equal(t, status, failures[0].UpstreamStatus)
			}
		})
	}
}

type cloudStreamFailureStore struct {
	OpenAICloudSessionStore
	fail bool
}

func (s *cloudStreamFailureStore) ReadCloudEvents(ctx context.Context, id, cursor string) ([]OpenAICloudStreamMessage, error) {
	if s.fail {
		return nil, errors.New("fixture event delivery failure")
	}
	messages, err := s.OpenAICloudSessionStore.ReadCloudEvents(ctx, id, cursor)
	for _, message := range messages {
		if gjson.GetBytes(message.Payload, "event.type").String() == "response.output_text.delta" {
			s.fail = true
		}
	}
	return messages, err
}

func TestCloudHTTPStreamDeliveryFailureKeepsFailureOutcome(t *testing.T) {
	svc, in, store, wire := cloudForwardFixture(t, nil)
	svc.cloudSessions.store = &cloudStreamFailureStore{OpenAICloudSessionStore: store}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireStart(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "text", "delta": "partial"}})
	}()
	defer func() { cancel(); <-done }()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	c.Set("api_key", in.APIKey)
	SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
	result, err := svc.Forward(ctx, c, in.Account, []byte(`{"model":"client-model","input":"hi","stream":true}`))
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "partial")
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	failures := GetOpsStreamErrors(c)
	require.Len(t, failures, 1)
	require.Equal(t, http.StatusBadGateway, failures[0].IntendedStatus)
	require.True(t, failures[0].CountTowardsSLA)
	require.False(t, failures[0].NonStream)
	require.Zero(t, failures[0].UpstreamStatus, "event-store delivery failure is not an upstream HTTP response")
}

func TestCloudForwardChecksFreshClientRestriction(t *testing.T) {
	for _, websocket := range []bool{false, true} {
		t.Run(fmt.Sprint(websocket), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			stale := *in.Account
			stale.Extra = map[string]any{OpenAICloudEnabledKey: true}
			in.Account.Extra["codex_cli_only"] = true
			ctx := t.Context()
			if websocket {
				ctx = context.WithValue(ctx, cloudWebsocketModelKey{}, "client-model")
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			result, err := svc.forwardOpenAICloud(ctx, c, &stale, []byte(`{"model":"client-model","input":"hi"}`))
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Contains(t, recorder.Body.String(), "client_restricted")
			require.True(t, HasOpsClientBusinessLimited(c))
			require.Nil(t, store.snapshot(), "latest restriction must reject before creating a Cloud session")
			require.Empty(t, wire.writes)
		})
	}
}

func TestCloudForwardReusesNativeIngressRepairs(t *testing.T) {
	for _, websocket := range []bool{false, true} {
		for _, alias := range []string{`"prompt":"hello"`, `"messages":[{"role":"user","content":"hello"}]`} {
			t.Run(fmt.Sprintf("ws=%t/%s", websocket, alias), func(t *testing.T) {
				svc, in, _, wire := cloudForwardFixture(t, nil)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
				require.NoError(t, err)
				done := make(chan struct{})
				go func() {
					defer close(done)
					cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
					require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
					thread := cloudTestMessage(t, ctx, wire)
					require.Equal(t, "thread/start", thread.Method)
					require.Equal(t, "object", gjson.GetBytes(thread.Params, "dynamicTools.0.tools.0.inputSchema.type").String())
					cloudTestRead(wire, map[string]any{"id": thread.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
					turn := cloudTestMessage(t, ctx, wire)
					require.Equal(t, "turn/start", turn.Method)
					require.Equal(t, "max", gjson.GetBytes(turn.Params, "effort").String())
					require.Equal(t, "hello", gjson.GetBytes(turn.Params, "input.0.text").String())
					cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
					cloudWireComplete(wire)
				}()
				defer func() { cancel(); <-done }()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
				c.Set("api_key", in.APIKey)
				SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
				model := "client-model"
				if websocket {
					model = "gpt-5.1"
				}
				body := []byte(fmt.Sprintf(`{"model":%q,%s,"stream":true,"reasoning":{"mode":"pro"},"tools":[{"type":"function","name":"echo","parameters":{"type":null}}]}`, model, alias))
				var result *OpenAIForwardResult
				if websocket {
					result, err = svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, body, "client-model", func([]byte) error { return nil })
				} else {
					result, err = svc.Forward(ctx, c, in.Account, body)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.SucceededForScheduling())
			})
		}
	}
}

func TestCloudNativeIngressRepairsPreserveValidationAndContinuationFences(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate before legacy map": `{"model":"client-model","model":"other","messages":[{"role":"user","content":"hello"}]}`,
		"unknown previous remains":    `{"model":"client-model","previous_response_id":"resp_cloud_missing","messages":[{"role":"user","content":"hello"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), in)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
			c.Set("api_key", in.APIKey)
			SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
			result, err := svc.Forward(ctx, c, in.Account, []byte(body))
			require.Error(t, err)
			require.Nil(t, result)
			require.Nil(t, store.snapshot())
			require.Empty(t, wire.writes, "must not dispatch malformed or unknown continuation as a fresh turn")
		})
	}
}

type cloudHeartbeatSignalWriter struct {
	gin.ResponseWriter
	ready chan struct{}
}

func (w *cloudHeartbeatSignalWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}
func (w *cloudHeartbeatSignalWriter) Write(value []byte) (int, error) {
	n, err := w.ResponseWriter.Write(value)
	if strings.HasPrefix(string(value), ": keepalive") {
		select {
		case w.ready <- struct{}{}:
		default:
		}
	}
	return n, err
}

func TestCloudHTTPHeartbeatPreservesNativeFailoverOutputBoundary(t *testing.T) {
	svc, in, _, _ := cloudForwardFixture(t, nil)
	svc.cfg.Gateway.StreamKeepaliveInterval = 1
	ready := make(chan struct{}, 1)
	svc.cloudSessions.connect = func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
		select {
		case <-ready:
			return nil, &cloudGatewayError{429, "cloud_quota_exhausted", "Cloud quota is exhausted"}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	c.Set("api_key", in.APIKey)
	SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
	c.Writer = &cloudHeartbeatSignalWriter{ResponseWriter: c.Writer, ready: ready}
	before := OpenAICompactKeepaliveAdjustedWrittenSize(c)
	result, err := svc.Forward(ctx, c, in.Account, []byte(`{"model":"client-model","input":"hello","stream":true}`))
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, CloudPreInferenceFailureReason, failure.Reason)
	require.Nil(t, result)
	require.Contains(t, recorder.Body.String(), ": keepalive")
	require.NotContains(t, recorder.Body.String(), "data:")
	require.Equal(t, before, OpenAICompactKeepaliveAdjustedWrittenSize(c), "the existing HTTP scheduler must see no committed model output")
	require.Empty(t, GetOpsStreamErrors(c))
	require.Empty(t, svc.cloudBillingWorker.holds)
}

// Cloud follows the native public keepalive setting, including explicit zero.
func TestCloudWSKeepaliveUsesNativeConfiguration(t *testing.T) {
	for _, seconds := range []int{0, 1} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			svc, in, _, _ := cloudForwardFixture(t, nil)
			svc.cfg.Gateway.StreamKeepaliveInterval = seconds
			svc.cloudSessions.connect = func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
				select {
				case <-time.After(1250 * time.Millisecond):
					return nil, &cloudGatewayError{429, "cloud_quota_exhausted", "Cloud quota is exhausted"}
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			body := []byte(`{"model":"gpt-5.1","input":"hello","stream":true}`)
			ctx, c, _ := cloudContractContext(t, svc, in, ctx, body)
			var events []string
			_, err := svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, body, "client-model", func(raw []byte) error {
				events = append(events, gjson.GetBytes(raw, "type").String())
				return nil
			})
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			if seconds == 0 {
				require.Empty(t, events, "native keepalive disable must also disable Cloud bridge keepalive")
			} else {
				require.Contains(t, events, "keepalive")
			}
			require.NotContains(t, events, "response.created", "keepalive must not commit the failed attempt")
		})
	}
}

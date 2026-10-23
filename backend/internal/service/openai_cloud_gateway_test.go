package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	body, effort, err := svc.inheritCloudBillingEffort(t.Context(), &binding, owner, []byte(`{"model":"gpt-5.1","previous_response_id":"resp_cloud_old","input":"next"}`))
	require.NoError(t, err)
	require.Equal(t, "high", effort)
	protocol, err := parseCloudProtocolRequest(body)
	require.NoError(t, err)
	require.Equal(t, "high", protocol.effort)
	nextCtx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	nextCtx, err = svc.FinalizeCloudBillingSnapshot(nextCtx, "gpt-5.1", "", effort)
	require.NoError(t, err)
	snapshot := cloudBillingContext(nextCtx)
	require.Equal(t, protocol.effort, snapshot.ReasoningEffort)
	result := &OpenAIForwardResult{Model: "gpt-5.1", ReasoningEffort: &effort}
	cost, err := svc.cloudFrozenCost(nextCtx, snapshot, result, []string{"gpt-5.1"}, UsageTokens{InputTokens: 1_000_000}, snapshot.TokenMultiplier, "", false)
	require.NoError(t, err)
	require.InDelta(t, 8, cost.ActualCost, 1e-9, "2 input price × 2 group rate × 2 inherited reasoning multiplier")
	// Explicit new fields are never silently rewritten; the actor still checks
	// whether changing those settings on this existing thread is supported.
	explicit := []byte(`{"model":"gpt-5.1","input":"next","reasoning":{"effort":"low"}}`)
	got, effort, err := svc.inheritCloudBillingEffort(t.Context(), &binding, owner, explicit)
	require.NoError(t, err)
	require.Equal(t, explicit, got)
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
			_, _, err := gateway.inheritCloudBillingEffort(t.Context(), &binding, owner, []byte(`{"model":"model","input":"next"}`))
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
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICloudEnabledKey: true, OpenAICloudModelsKey: []string{"model"}}}
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.True(t, a.IsModelSupported("model"))
	require.False(t, a.IsModelSupported("other"))
	require.False(t, supportsOpenAIRequestModel(a, "model", true))
	a.Extra["openai_excel_bps"] = true
	require.True(t, a.IsOpenAICloudConfigured())
	require.False(t, a.IsOpenAICloudEnabled())
	// An invalid cached configuration stays on the rejecting Cloud route.
	require.False(t, (&OpenAIGatewayService{}).isOpenAIAccountTransportCompatible(context.Background(), a, OpenAIUpstreamTransportResponsesWebsocketV2Ingress))
}

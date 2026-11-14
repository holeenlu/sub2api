package service

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func systemOneFinalizationFixture(t *testing.T) (*GatewayService, NativeUsageIntent, NativeIntentFinalizationInput) {
	t.Helper()
	scope := nativeI2ServiceIntent()
	groupID := int64(5)
	scope.Request.Protocol = NativeIntentProtocolSystemOne
	scope.Request.GroupID = &groupID
	scope.Request.OriginalModel = "jev-latest"
	scope.Request.Billing.QuotaPlatform = PlatformTypeSafe
	scope.Attempt.Prepared.GroupID, scope.Attempt.Prepared.GroupBound = groupID, true
	scope.Attempt.Billing.AccountPlatform = PlatformTypeSafe
	scope.Attempt.BaselineModel = "jev-latest"
	scope.Attempt.CandidateModels = []string{"jev-latest"}
	scope.Attempt.FrozenPricing.Policy.Model = "jev-latest"
	ref, err := scope.NativeIntentReference()
	require.NoError(t, err)
	in, out := int64(10), int64(2)
	fact := model.AttemptFact{Sequence: 1, Identity: scope.Attempt.Prepared, Send: model.SendSent,
		Terminal: model.TerminalCompleted, ReturnedModel: "jev-latest",
		Usage: &model.UsageFact{Confidence: model.UsageFinal, TerminalConfirmed: true, ReturnedModel: "jev-latest",
			Input: &model.TokenMeasurement{Value: in}, Output: &model.TokenMeasurement{Value: out}}}
	p := &SystemOneUsagePreparation{Version: 1, Intent: ref, Scope: scope,
		Identity:   billing.Identity{RequestID: "local:systemone-fixture", APIKeyID: scope.Key.APIKeyID},
		ObservedAt: scope.Attempt.CapturedAt, StatusCode: 200, InputTokens: &in, OutputTokens: &out,
		ReturnedModel: "jev-latest", FinalModel: "jev-latest",
		Operation: model.OperationResult{Protocol: model.ProtocolSystemOne, OperationID: scope.Key.OperationID,
			UserID: scope.UserID, APIKeyID: scope.Key.APIKeyID, GroupID: groupID, GroupBound: true,
			OriginalWireDigest: scope.OriginalWireDigest, Attempts: []model.AttemptFact{fact}}}
	e, err := NativeUsageEvidenceFromAttempt(billing.NativeUsageEvidence{Version: 2, Identity: p.Identity,
		OperationID: scope.Key.OperationID, UserID: scope.UserID, GroupID: groupID, GroupBound: true,
		OriginalWireDigest: scope.OriginalWireDigest}, fact)
	require.NoError(t, err)
	digest, err := hex.DecodeString(ref.ScopeDigest)
	require.NoError(t, err)
	e.AttemptScopeDigest = [32]byte(digest)
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	return svc, scope, NativeIntentFinalizationInput{Intent: ref, SystemOne: p,
		Reason: billing.NativeBuilderUnavailable, Evidence: NativeUsageEvidenceInput{
			Protocol: NativeIntentProtocolSystemOne, Intent: &ref, Evidence: e, FrozenPricing: scope.Attempt.FrozenPricing}}
}

func TestSystemOneFinalizationActualBuilderRoundTrip(t *testing.T) {
	svc, scope, input := systemOneFinalizationFixture(t)
	command, mapping, err := svc.PrepareSystemOneUsage(t.Context(), *input.SystemOne)
	require.NoError(t, err)
	require.NotNil(t, mapping.Gateway)
	require.Nil(t, mapping.OpenAI)
	input.Evidence.Settlement, input.Evidence.SettlementUsage = command, mapping
	input.Reason = billing.NativePreparationReady
	final, err := PrepareNativeIntentFinalization(input)
	require.NoError(t, err)
	stored, err := MergeNativeUsageIntent(nil, scope)
	require.NoError(t, err)
	require.NoError(t, ValidateNativeIntentFinalization(stored, final))
	record, err := MergeNativeIntentFinalization(nil, final)
	require.NoError(t, err)
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	hash, err := record.Fingerprint()
	require.NoError(t, err)
	decoded, err := DecodeNativeIntentFinalization(raw, scope.Key, hash)
	require.NoError(t, err)
	recovered, prices, err := svc.PrepareSystemOneUsage(t.Context(), *decoded.SystemOne)
	require.NoError(t, err)
	require.Equal(t, command.RequestFingerprint, recovered.RequestFingerprint)
	require.Equal(t, mapping, prices)
	require.Nil(t, decoded.SystemOne.Settlement, "money payload stays in canonical ledger only")
}

func TestSystemOneFinalizationRejectsCrossProtocolAndUnknownUsage(t *testing.T) {
	for _, kind := range []string{"missing_protocol", "unknown_protocol", "wrong_scope_protocol", "openai_projection", "missing_input", "wrong_count", "media", "foreign_scope", "changed_funding"} {
		t.Run(kind, func(t *testing.T) {
			svc, scope, in := systemOneFinalizationFixture(t)
			cmd, mapping, err := svc.PrepareSystemOneUsage(t.Context(), *in.SystemOne)
			require.NoError(t, err)
			in.Evidence.Settlement, in.Evidence.SettlementUsage, in.Reason = cmd, mapping, billing.NativePreparationReady
			switch kind {
			case "missing_protocol":
				in.Evidence.Protocol = ""
			case "unknown_protocol":
				in.Evidence.Protocol = "anything"
			case "wrong_scope_protocol":
				in.SystemOne.Scope.Request.Protocol = ""
			case "openai_projection":
				in.Evidence.SettlementUsage.OpenAI = &OpenAIForwardResult{}
			case "missing_input":
				in.Evidence.Evidence.Usage.InputTokens = nil
			case "wrong_count":
				mapping.Gateway.Usage.OutputTokens++
			case "media":
				mapping.Gateway.ImageCount = 1
			case "foreign_scope":
				in.SystemOne.Scope.Attempt.Prepared.ResourceID++
			case "changed_funding":
				in.Evidence.Settlement.AccountQuotaCost = 4
			}
			checked, err := PrepareNativeIntentFinalization(in)
			if err == nil {
				stored, storeErr := MergeNativeUsageIntent(nil, scope)
				require.NoError(t, storeErr)
				err = ValidateNativeIntentFinalization(stored, checked)
			}
			require.Error(t, err)
		})
	}
}

func TestSystemOneFinalizationPendingPromotesThroughSharedMerge(t *testing.T) {
	svc, scope, in := systemOneFinalizationFixture(t)
	pending, err := PrepareNativeIntentFinalization(in)
	require.NoError(t, err)
	record, err := MergeNativeIntentFinalization(nil, pending)
	require.NoError(t, err)
	require.Equal(t, billing.NativeBuilderUnavailable, record.Reason)
	command, mapping, err := svc.PrepareSystemOneUsage(t.Context(), *record.SystemOne)
	require.NoError(t, err)
	in.Evidence.Settlement, in.Evidence.SettlementUsage, in.Reason = command, mapping, billing.NativePreparationReady
	final, err := PrepareNativeIntentFinalization(in)
	require.NoError(t, err)
	record, err = MergeNativeIntentFinalization(record, final)
	require.NoError(t, err)
	_, err = DecodeNativeIntentFinalization(mustSystemOneJSON(t, record), scope.Key, mustSystemOneFingerprint(t, record))
	require.NoError(t, err)
}

func mustSystemOneJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func mustSystemOneFingerprint(t *testing.T, value *NativeIntentFinalizationRecord) string {
	t.Helper()
	hash, err := value.Fingerprint()
	require.NoError(t, err)
	return hash
}

func TestNativeFinalizationJSONOmitsSystemOneForHistoricalDigest(t *testing.T) {
	in := nativeI2ServiceInput(t, nativeI2ServiceIntent(), true)
	r, err := MergeNativeIntentFinalization(nil, &in)
	require.NoError(t, err)
	raw := mustSystemOneJSON(t, r)
	require.NotContains(t, string(raw), `"SystemOne"`)
	require.NotContains(t, string(raw), `"Protocol"`)
}

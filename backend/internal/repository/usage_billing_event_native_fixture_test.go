//go:build unit || integration

package repository

import (
	"crypto/sha256"
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func nativeRepositoryInput(id string, user, key, account int64) service.NativeUsageEvidenceInput {
	h := sha256.Sum256([]byte(id))
	exempt := false
	return service.NativeUsageEvidenceInput{Evidence: billing.NativeUsageEvidence{Identity: billing.Identity{RequestID: id, APIKeyID: key}, OperationID: "operation:" + id, UserID: user, OriginalWireDigest: h, SendState: model.SendAmbiguous, Prepared: &model.PreparedIdentity{OperationID: "operation:" + id, ResourceID: account, Owner: model.ResourceOwner{AccountID: account}, UserID: user, APIKeyID: key, Mode: "native", Transport: "http", GrantDigest: h, TargetDigest: h, WireDigest: h, BodyKind: model.BodyReplayable, BodyDigest: h}}, FrozenPricing: &service.NativeFrozenPricing{BalanceExempt: &exempt, Resolved: &service.NativeResolvedPricingSnapshot{Mode: service.BillingModeToken, Source: service.PricingSourceGroup, BasePricing: &service.ModelPricing{InputPricePerToken: 0.125}}}}
}
func nativeRepositoryFinal(t *testing.T, input service.NativeUsageEvidenceInput) service.NativeUsageEvidenceInput {
	t.Helper()
	input.Evidence.Confidence = billing.NativeUsageFinal
	input.Evidence.TerminalConfirmed = true
	input.Evidence.SendState = model.SendSent
	in, out := int64(10), int64(0)
	input.Evidence.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, Source: "response.usage", ReturnedModel: "native-test", InputTokens: &in, OutputTokens: &out}
	input.Evidence.UpstreamResponseID = "fixture-observed-response"
	id := input.Evidence.Identity
	input.Settlement = &service.UsageBillingCommand{RequestID: id.RequestID, APIKeyID: id.APIKeyID, UserID: input.Evidence.UserID, AccountID: input.Evidence.Prepared.ResourceID, Model: "native-test", Platform: service.PlatformOpenAI, InputTokens: 10, BalanceCost: 1.25, UsageLog: &service.UsageLog{RequestID: id.RequestID, APIKeyID: id.APIKeyID, UserID: input.Evidence.UserID, AccountID: input.Evidence.Prepared.ResourceID, Model: "native-test", InputTokens: 10, TotalCost: 1.25, ActualCost: 1.25, CreatedAt: time.Now()}}
	input.SettlementUsage = &service.UsagePricingRequest{OpenAI: &service.OpenAIForwardResult{Model: "native-test", Usage: service.OpenAIUsage{InputTokens: 10}}, Tokens: service.UsageTokens{InputTokens: 10}}
	normalized, err := service.PrepareNativeUsageEvidence(input)
	require.NoError(t, err)
	return *normalized
}

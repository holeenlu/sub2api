//go:build unit || integration

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Journal fixture: amount comes from the existing transaction fixture. The
// actual System One calculator is exercised in service's finalization tests.
func systemOneJournalFixture(t *testing.T, scope service.NativeUsageIntent, identity billing.Identity) service.SystemOneUsagePreparation {
	t.Helper()
	scope.Request.Protocol = service.NativeIntentProtocolSystemOne
	no := false
	scope.Request.Billing = &service.NativeIntentRequestBillingPolicy{Version: 1,
		QuotaPlatform: service.PlatformTypeSafe, GroupImageCountBilling: &no}
	scope.Attempt.Billing = &service.NativeIntentAttemptBillingPolicy{Version: 1, AccountType: service.AccountTypeAPIKey,
		AccountPlatform: service.PlatformTypeSafe, FreeFastEligible: &no, Stats: &service.NativeIntentStatsPricing{State: service.NativeIntentStatsNotApplicable}}
	ref, err := scope.NativeIntentReference()
	require.NoError(t, err)
	in, out := int64(10), int64(0)
	fact := model.AttemptFact{Sequence: scope.Attempt.Sequence, Identity: scope.Attempt.Prepared, Send: model.SendSent,
		ReturnedModel: "native-test", Terminal: model.TerminalCompleted,
		Usage: &model.UsageFact{Confidence: model.UsageFinal, TerminalConfirmed: true, ReturnedModel: "native-test",
			Input: &model.TokenMeasurement{Value: in}, Output: &model.TokenMeasurement{Value: out}}}
	return service.SystemOneUsagePreparation{Version: 1, Intent: ref, Scope: scope,
		Identity: identity, ObservedAt: scope.Attempt.CapturedAt, StatusCode: 200, InputTokens: &in, OutputTokens: &out,
		ReturnedModel: "native-test", FinalModel: scope.Attempt.BaselineModel,
		Operation: model.OperationResult{Protocol: model.ProtocolSystemOne, OperationID: scope.Key.OperationID,
			UserID: scope.UserID, APIKeyID: scope.Key.APIKeyID, OriginalWireDigest: scope.OriginalWireDigest,
			Attempts: []model.AttemptFact{fact}}}
}

func systemOneJournalCommand(p service.SystemOneUsagePreparation) (*service.UsageBillingCommand, *service.UsagePricingRequest) {
	id := p.Identity
	command := &service.UsageBillingCommand{RequestID: id.RequestID, APIKeyID: id.APIKeyID, UserID: p.Scope.UserID,
		AccountID: p.Scope.Attempt.Prepared.ResourceID, AccountType: service.AccountTypeAPIKey,
		Model: "native-test", Platform: service.PlatformTypeSafe, InputTokens: 10, BalanceCost: 1.25,
		UsageLog: &service.UsageLog{RequestID: id.RequestID, APIKeyID: id.APIKeyID, UserID: p.Scope.UserID,
			AccountID: p.Scope.Attempt.Prepared.ResourceID, Model: "native-test", InputTokens: 10,
			TotalCost: 1.25, ActualCost: 1.25, CreatedAt: p.ObservedAt}}
	return command, &service.UsagePricingRequest{Gateway: &service.ForwardResult{
		Model: "native-test", Usage: service.ClaudeUsage{InputTokens: 10}}}
}

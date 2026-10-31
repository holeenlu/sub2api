//go:build unit || integration

package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"testing"
	"time"
)

func nativeIntentRepositoryInput(t *testing.T) service.NativeUsageIntent {
	t.Helper()
	evidence := nativeRepositoryInput("intent-sql", 3, 2, 7).Evidence
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	exempt := false
	return service.NativeUsageIntent{Key: billing.NativeIntentKey{OperationID: evidence.OperationID, APIKeyID: evidence.Identity.APIKeyID}, UserID: evidence.UserID, OriginalWireDigest: evidence.OriginalWireDigest,
		Request: service.NativeIntentRequestScope{Mode: service.UsageBillingModeFull, PricingAt: at, UserRole: "user", BaseMultiplier: 1, TokenMultiplier: 1, ImageMultiplier: 1, VideoMultiplier: 1},
		Attempt: service.NativeIntentAttemptScope{Sequence: 1, Prepared: *evidence.Prepared, CapturedAt: at.Add(time.Second), BaselineModel: "native-test", CandidateModels: []string{"native-test"},
			FrozenPricing: &service.NativeFrozenPricing{BalanceExempt: &exempt, Policy: &service.UsageBillingPricingSnapshot{Model: "native-test", PricingAt: at, Balance: true, AccountRateMultiplier: 1}, Resolved: &service.NativeResolvedPricingSnapshot{Mode: service.BillingModeToken, Source: service.PricingSourceGroup, BasePricing: &service.ModelPricing{InputPricePerToken: 0.125}}}}}
}

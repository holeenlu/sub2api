package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func TestNativeApplicationImageScopesUseOriginalCanonicalProjection(t *testing.T) {
	for _, tc := range []struct {
		name            string
		primary, hosted *model.TokenMeasurement
		billed          int
		conflict        bool
	}{
		{"primary_zero_hosted_positive", &model.TokenMeasurement{Value: 0}, &model.TokenMeasurement{Value: 8}, 8, false},
		{"primary_absent_hosted_positive", nil, &model.TokenMeasurement{Value: 8}, 8, false},
		{"primary_positive_wins", &model.TokenMeasurement{Value: 4}, &model.TokenMeasurement{Value: 8}, 4, false},
		{"no_hosted_proof", &model.TokenMeasurement{Value: 0}, nil, 8, true},
		{"wrong_canonical", &model.TokenMeasurement{Value: 4}, &model.TokenMeasurement{Value: 8}, 8, true},
		{"negative_hosted", &model.TokenMeasurement{Value: 0}, &model.TokenMeasurement{Value: -1}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway, repo, logs, input := nativeApplicationFixture(t, func(_ *OpenAIGatewayService, input *NativeOperationFinalization) {
				input.UsageInput.Result.Model = "gpt-image-2"
			})
			input.Operation.Attempts[0].Usage = &model.UsageFact{ImageOutput: tc.primary, HostedImageOutput: tc.hosted, Confidence: model.UsageFinal, TerminalConfirmed: true}
			input.UsageInput.Result.Usage = OpenAIUsage{ImageOutputTokens: tc.billed}
			receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
			if tc.hosted != nil && tc.hosted.Value < 0 {
				require.Error(t, err)
				require.False(t, receipt.Intent.Durable())
				require.Nil(t, receipt.Canonical)
				require.Zero(t, repo.records)
				require.Zero(t, repo.old)
				require.Zero(t, logs.calls)
				return
			}
			require.NoError(t, err)
			// This fixture intentionally has no image-token catalog card.
			// Successful quantity validation must preserve pending pricing,
			// not fabricate a settled/free image bill.
			require.True(t, receipt.Intent.Durable())
			require.Equal(t, billing.NativeIntentPendingPreparation, receipt.Intent.Disposition)
			require.Nil(t, receipt.Canonical)
			require.Nil(t, repo.input.Settlement)
			require.Nil(t, repo.input.SettlementUsage)
			require.Equal(t, 1, repo.stores)
			require.Equal(t, 1, repo.records)
			require.Equal(t, billing.NativeUsageFinal, repo.input.Evidence.Confidence)
			require.True(t, repo.input.Evidence.TerminalConfirmed)
			usage := repo.input.Evidence.Usage
			if tc.primary == nil {
				require.Nil(t, usage.ImageOutputTokens)
			} else {
				require.Equal(t, tc.primary.Value, *usage.ImageOutputTokens)
			}
			if tc.hosted == nil {
				require.Nil(t, usage.HostedImageOutputTokens)
			} else {
				require.Equal(t, tc.hosted.Value, *usage.HostedImageOutputTokens)
			}
			require.Nil(t, usage.InputTokens)
			require.Nil(t, usage.OutputTokens)
			if tc.conflict {
				require.Equal(t, billing.NativeUsagePreparationUnavailable, repo.final.Reason)
				require.Nil(t, repo.final.Preparation)
				require.ErrorIs(t, validateNativeObservedUsageMapping(repo.input.Evidence, &UsagePricingRequest{OpenAI: input.UsageInput.Result}), billing.ErrNativeUsageConflict)
			} else {
				require.Equal(t, billing.NativeBuilderUnavailable, repo.final.Reason)
				require.NotNil(t, repo.final.Preparation)
				require.Equal(t, tc.billed, repo.final.Preparation.Result.Usage.ImageOutputTokens)
			}
			require.Zero(t, logs.calls)
			require.Zero(t, repo.old)
		})
	}
}

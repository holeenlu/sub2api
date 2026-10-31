package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imagePolicyRecoveryGroups struct {
	GroupRepository
	group *Group
}

func (r imagePolicyRecoveryGroups) GetByIDLite(context.Context, int64) (*Group, error) {
	return r.group, nil
}

func TestUsagePricingRecoveryPreservesDedicatedImageCountPolicy(t *testing.T) {
	for _, perImage := range []bool{false, true} {
		group := &Group{ID: 41, Platform: PlatformOpenAI, ImageCountBilling: perImage, ImagePrice1K: float64Ptr(0.4),
			ModelPricing: []ChannelModelPricing{{Models: []string{"gpt-image-2"}, BillingMode: BillingModeToken,
				InputPrice: float64Ptr(1e-6), OutputPrice: float64Ptr(2e-6)}}}
		key := &APIKey{ID: 2, GroupID: &group.ID, Group: group}
		result := &OpenAIForwardResult{Model: "gpt-image-2", ImageEndpoint: true, ImageCount: 2, ImageSize: "1K",
			Usage: OpenAIUsage{InputTokens: 1_000_000, OutputTokens: 10_000}}
		tokens := UsageTokens{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens}
		request := openAIPricingRequest(result, key, []string{result.Model}, tokens, 1, 1, 1, 1, "", nil, false, false, time.Now())
		// Recover from its actual durable representation after the live group
		// policy changed. Both the source endpoint and old switch must survive.
		raw, err := json.Marshal(request)
		require.NoError(t, err)
		var stored UsagePricingRequest
		require.NoError(t, json.Unmarshal(raw, &stored))
		group.ImageCountBilling = !perImage
		billing := NewBillingService(nil, nil)
		command := &UsageBillingCommand{RequestID: "image-policy", APIKeyID: key.ID, UsageLog: &UsageLog{Model: result.Model, ImageCount: 2},
			Pricing: &UsageBillingPricingSnapshot{Pending: true, Balance: true, Request: &stored}}
		require.NoError(t, usageBillingRepricer(billing, NewModelPricingResolver(nil, billing), imagePolicyRecoveryGroups{group: group})(t.Context(), command))
		want := 1.02
		if perImage {
			want = 0.8
		}
		require.InDelta(t, want, command.BalanceCost, 1e-9)
		require.False(t, command.Pricing.Pending)
		require.True(t, stored.OpenAI.ImageEndpoint)
		require.Equal(t, perImage, stored.GroupImageCountBilling)
	}
}

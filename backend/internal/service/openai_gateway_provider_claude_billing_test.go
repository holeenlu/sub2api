//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceRecordUsage_AggregatorClaudeUsesDefaultPricing(t *testing.T) {
	for _, platform := range []string{PlatformCommandCode, PlatformCline} {
		for _, model := range []string{"claude-sonnet-4-6", "anthropic/claude-sonnet-4-6"} {
			t.Run(platform+"/"+model, func(t *testing.T) {
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				group := &Group{ID: 1, Platform: platform, RateMultiplier: 1.1}
				usage := OpenAIUsage{InputTokens: 1000, OutputTokens: 200}
				expected := expectedOpenAICost(t, svc, model, usage, group.RateMultiplier)
				require.Greater(t, expected.ActualCost, 0.0)

				err := svc.RecordUsage(context.Background(), providerClaudeRecordUsageInput(platform, model, group, usage))

				require.NoError(t, err)
				require.Equal(t, 1, usageRepo.calls)
				require.NotNil(t, usageRepo.lastLog)
				require.Equal(t, usage.InputTokens, usageRepo.lastLog.InputTokens)
				require.Equal(t, usage.OutputTokens, usageRepo.lastLog.OutputTokens)
				require.InDelta(t, expected.TotalCost, usageRepo.lastLog.TotalCost, 1e-12)
				require.InDelta(t, expected.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
				require.Equal(t, 1, userRepo.deductCalls)
				require.InDelta(t, expected.ActualCost, userRepo.lastAmount, 1e-12)
			})
		}
	}
}

func TestOpenAIGatewayServiceRecordUsage_CNAndOpenCodeClaudeRequireExplicitPricing(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo} {
		t.Run(platform, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			group := &Group{ID: 1, Platform: platform, RateMultiplier: 1.1}
			usage := OpenAIUsage{InputTokens: 1000, OutputTokens: 200}

			err := svc.RecordUsage(context.Background(), providerClaudeRecordUsageInput(platform, "claude-sonnet-4-6", group, usage))

			require.ErrorIs(t, err, ErrUsagePricingPending)
			require.Equal(t, 1, usageRepo.calls)
			require.NotNil(t, usageRepo.lastLog)
			require.Equal(t, usage.InputTokens, usageRepo.lastLog.InputTokens)
			require.Equal(t, usage.OutputTokens, usageRepo.lastLog.OutputTokens)
			require.Zero(t, usageRepo.lastLog.TotalCost)
			require.Zero(t, usageRepo.lastLog.ActualCost)
			require.Zero(t, userRepo.deductCalls)
		})
	}
}

func TestOpenAIGatewayServiceRecordUsage_CNAndOpenCodeClaudeHonorExplicitPricing(t *testing.T) {
	const model = "claude-sonnet-4-6"
	for _, platform := range []string{PlatformKimi, PlatformOpenCodeGo} {
		for _, source := range []string{PricingSourceGroup, PricingSourceChannel} {
			t.Run(platform+"/"+source, func(t *testing.T) {
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
				group := &Group{ID: 1, Platform: platform, RateMultiplier: 1.25}
				inputPrice, outputPrice := 1e-6, 2e-6
				pricing := ChannelModelPricing{
					Models: []string{model}, BillingMode: BillingModeToken,
					InputPrice: &inputPrice, OutputPrice: &outputPrice,
				}
				if source == PricingSourceGroup {
					group.ModelPricing = []ChannelModelPricing{pricing}
				} else {
					cache := newEmptyChannelCache()
					cache.pricingByGroupModel[channelModelKey{groupID: group.ID, platform: platform, model: model}] = &pricing
					cache.channelByGroupID[group.ID] = &Channel{ID: 1, Status: StatusActive}
					cache.groupPlatform[group.ID] = platform
					cache.loadedAt = time.Now()
					svc.channelService = &ChannelService{}
					svc.channelService.cache.Store(cache)
				}
				svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
				usage := OpenAIUsage{InputTokens: 1000, OutputTokens: 200}
				expectedTotal := float64(usage.InputTokens)*inputPrice + float64(usage.OutputTokens)*outputPrice
				expectedActual := expectedTotal * group.RateMultiplier

				err := svc.RecordUsage(context.Background(), providerClaudeRecordUsageInput(platform, model, group, usage))

				require.NoError(t, err)
				require.NotNil(t, usageRepo.lastLog)
				require.InDelta(t, expectedTotal, usageRepo.lastLog.TotalCost, 1e-12)
				require.InDelta(t, expectedActual, usageRepo.lastLog.ActualCost, 1e-12)
				require.Equal(t, 1, userRepo.deductCalls)
				require.InDelta(t, expectedActual, userRepo.lastAmount, 1e-12)
			})
		}
	}
}

func TestProviderClaudeRecordUsagePreservesTTLAcrossSSELayouts(t *testing.T) {
	for _, route := range []string{"native_chat_json", "native_chat_sse", "native_responses_json", "native_responses_sse"} {
		for _, multiline := range []bool{false, true} {
			t.Run(route+map[bool]string{false: "/single", true: "/multiline"}[multiline], func(t *testing.T) {
				wireUsage, err := json.Marshal(map[string]any{"input_tokens": 10, "output_tokens": 1,
					"cache_creation_input_tokens": 1000, "cache_read_input_tokens": 3,
					"cache_creation": map[string]int{"ephemeral_5m_input_tokens": 250, "ephemeral_1h_input_tokens": 750}})
				require.NoError(t, err)
				fixture := strings.Replace(miniAnthropicSSEStream(), `{"input_tokens":10,"output_tokens":1}`, string(wireUsage), 1)
				if multiline {
					fixture = strings.Replace(fixture, ",", ",\ndata: ", 1)
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				result, err := runAnthropicCompatRoute(&GatewayService{}, route, c, &http.Response{Body: io.NopCloser(strings.NewReader(fixture))})
				require.NoError(t, err)
				require.Equal(t, 250, result.Usage.CacheCreation5mTokens)
				require.Equal(t, 750, result.Usage.CacheCreation1hTokens)
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				group := &Group{ID: 1, Platform: PlatformCommandCode, RateMultiplier: 1.1}
				input := providerClaudeRecordUsageInput(PlatformCommandCode, "claude-sonnet-4-6", group, claudeUsageToOpenAIUsage(&result.Usage))
				require.NoError(t, svc.RecordUsage(context.Background(), input))
				log := usageRepo.lastLog
				require.NotNil(t, log)
				require.Equal(t, 250, log.CacheCreation5mTokens)
				require.Equal(t, 750, log.CacheCreation1hTokens)
				require.Equal(t, 10, log.InputTokens)
				wantTotal := 10*3e-6 + 5*15e-6 + 3*0.3e-6 + 250*3.75e-6 + 750*6e-6
				require.InDelta(t, wantTotal, log.TotalCost, 1e-12)
				require.Equal(t, QuantizeUsageBillingAmount(wantTotal*1.1), log.ActualCost)
				require.Equal(t, log.ActualCost, userRepo.lastAmount)
			})
		}
	}
}

func providerClaudeRecordUsageInput(platform, model string, group *Group, usage OpenAIUsage) *OpenAIRecordUsageInput {
	return &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "provider_claude_" + platform,
			Model:     model, BillingModel: model, UpstreamModel: model,
			Usage: usage, Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 10, GroupID: &group.ID, Group: group},
		User:    &User{ID: 20},
		Account: &Account{ID: 30, Platform: platform, Type: AccountTypeAPIKey},
	}
}

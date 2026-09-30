package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func securityMediaPricingResolver(svc *OpenAIGatewayService, groupID int64, model string, mode BillingMode, price float64) *ModelPricingResolver {
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}] = &ChannelModelPricing{BillingMode: mode, PerRequestPrice: &price, OutputPrice: &price}
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = ""
	cache.loadedAt = time.Now()
	channels := &ChannelService{}
	channels.cache.Store(cache)
	return NewModelPricingResolver(channels, svc.billingService)
}

func TestSecurityVideoPricingUnitsSurviveRestart(t *testing.T) {
	cases := []struct {
		name                string
		endpoint            GrokMediaEndpoint
		mode                BillingMode
		price, groupSeconds float64
		groupCard           bool
		wantHold, wantCost  float64
		actualDuration      int
		missingUnit         bool
	}{
		{name: "flat_request_generation", endpoint: GrokMediaEndpointVideosGenerations, mode: BillingModePerRequest, price: 1, wantHold: 1, wantCost: 1},
		{name: "flat_request_edit", endpoint: GrokMediaEndpointVideosEdits, mode: BillingModePerRequest, price: 1, wantHold: 1, wantCost: 1},
		{name: "flat_request_extend", endpoint: GrokMediaEndpointVideosExtensions, mode: BillingModePerRequest, price: 1, wantHold: 1, wantCost: 1},
		{name: "flat_image_generation", endpoint: GrokMediaEndpointVideosGenerations, mode: BillingModeImage, price: 1, wantHold: 1, wantCost: 1},
		{name: "flat_image_edit", endpoint: GrokMediaEndpointVideosEdits, mode: BillingModeImage, price: 1, wantHold: 1, wantCost: 1},
		{name: "flat_image_extend", endpoint: GrokMediaEndpointVideosExtensions, mode: BillingModeImage, price: 1, wantHold: 1, wantCost: 1},
		{name: "channel_seconds", endpoint: GrokMediaEndpointVideosGenerations, mode: BillingModeVideo, price: 1, wantHold: 8, wantCost: 8},
		{name: "group_seconds_override_flat", endpoint: GrokMediaEndpointVideosGenerations, mode: BillingModePerRequest, price: 1, groupSeconds: .25, wantHold: 2, wantCost: 2},
		{name: "group_card_seconds_override_flat", endpoint: GrokMediaEndpointVideosGenerations, mode: BillingModeImage, price: 1, groupSeconds: .4, groupCard: true, wantHold: 3.2, wantCost: 3.2},
		{name: "seedance_tokens", endpoint: SeedanceEndpointCreate, mode: BillingModeToken, price: .001, wantHold: .001, wantCost: 1.2},
		{name: "seedance_flat_request", endpoint: SeedanceEndpointCreate, mode: BillingModePerRequest, price: 1, wantHold: 1, wantCost: 1},
		{name: "flat_request_actual_duration_changes", endpoint: GrokMediaEndpointVideosGenerations, mode: BillingModePerRequest, price: 1, actualDuration: 12, wantHold: 1, wantCost: 1},
		{name: "channel_seconds_actual_duration_changes", endpoint: GrokMediaEndpointVideosGenerations, mode: BillingModeVideo, price: 1, actualDuration: 12, wantHold: 8, wantCost: 12},
		{name: "seedance_flat_image", endpoint: SeedanceEndpointCreate, mode: BillingModeImage, price: 1, wantHold: 1, wantCost: 1},
		{name: "seedance_video_label_is_flat", endpoint: SeedanceEndpointCreate, mode: BillingModeVideo, price: 1, wantHold: 1, wantCost: 1},
		{name: "missing_persisted_unit_is_not_guessed", endpoint: GrokMediaEndpointVideosGenerations, mode: BillingModePerRequest, price: 1, missingUnit: true, wantHold: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := newSecurityMediaRepo()
			billing := &securityMediaBilling{seen: map[string]string{}}
			usage := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(usage, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			groupID := int64(987)
			model := "grok-imagine-video"
			group := &Group{ID: groupID, Platform: PlatformGrok, RateMultiplier: 1, VideoRateIndependent: true, VideoRateMultiplier: 1}
			if tc.groupSeconds > 0 {
				if tc.groupCard {
					group.ModelPricing = []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeVideo, PerRequestPrice: &tc.groupSeconds}}
				} else {
					group.VideoPrice480P = &tc.groupSeconds
				}
			}
			account := &Account{ID: 77, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "upstream"}}
			duration := tc.actualDuration
			if duration == 0 {
				duration = 8
			}
			upstream := &securityMediaHTTP{response: fmt.Sprintf(`{"status":"done","video":{"url":"https://cdn.example/video.mp4","duration":%d}}`, duration)}
			if tc.endpoint.IsSeedance() {
				model = "seedance-priced"
				account.Platform = PlatformOpenAI
				group.Platform = PlatformOpenAI
				account.Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/v3"
				account.Credentials["openai_capabilities"] = []string{"seedance"}
				upstream.response = `{"status":"succeeded","usage":{"completion_tokens":1200}}`
			}
			svc.accountRepo = &openAIRecordUsageAccountRepoStub{account: account}
			svc.mediaRepo = repo
			svc.usageBillingRepo = billing
			svc.httpUpstream = upstream
			svc.resolver = securityMediaPricingResolver(svc, groupID, model, tc.mode, tc.price)
			key := &APIKey{ID: 9, UserID: 1, User: &User{ID: 1}, GroupID: &groupID, Group: group}
			job, err := svc.PrepareGatewayVideoJob(ctx, key, nil, account, tc.endpoint, GrokMediaRequestInfo{Model: model, Resolution: VideoBillingResolution480P, DurationSeconds: 8}, model, "/videos", account.Platform)
			require.NoError(t, err)
			assert.InDelta(t, tc.wantHold, job.HoldAmount, 1e-12, "admission reserves the actual pricing unit, not the display billing mode")
			assert.InDelta(t, tc.wantHold, billing.held, 1e-12)
			if !tc.endpoint.IsSeedance() {
				require.Equal(t, string(BillingModeVideo), job.UnitCost.BillingMode, "video metadata remains video even for fixed channel pricing")
			}
			task := "priced-task"
			if tc.endpoint.IsSeedance() {
				task = SeedanceTaskKey(task)
			}
			require.NoError(t, svc.acceptGatewayVideo(WithGatewayMediaJob(ctx, job), task))
			// A fresh worker sees only serialized state. Remove the pricing resolver so
			// settlement cannot accidentally infer the old unit from today's settings.
			raw := append([]byte(nil), repo.jobs[job.ID]...)
			var restored GatewayMediaJob
			require.NoError(t, json.Unmarshal(raw, &restored))
			require.Equal(t, job.PricingUnit, restored.PricingUnit)
			expectedUnit := GatewayMediaPricingPerRequest
			if tc.mode == BillingModeToken {
				expectedUnit = GatewayMediaPricingPerOutputToken
			} else if !tc.endpoint.IsSeedance() && (tc.groupSeconds > 0 || tc.mode == BillingModeVideo) {
				expectedUnit = GatewayMediaPricingPerSecond
			}
			require.Equal(t, expectedUnit, restored.PricingUnit)
			if tc.missingUnit {
				restored.PricingUnit = ""
			}
			restarted := newOpenAIRecordUsageServiceForTest(usage, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			restarted.accountRepo = svc.accountRepo
			restarted.mediaRepo = repo
			restarted.usageBillingRepo = billing
			restarted.httpUpstream = upstream
			if tc.missingUnit {
				require.ErrorContains(t, restarted.settleGatewayMediaJob(ctx, &restored), "durable video pricing unit")
				require.Zero(t, billing.captured+billing.charged)
				persisted, err := repo.GetJob(ctx, groupID, 1, task)
				require.NoError(t, err)
				require.Equal(t, "pending", persisted.State)
				return
			}
			require.NoError(t, restarted.settleGatewayMediaJob(ctx, &restored))
			assert.InDelta(t, tc.wantCost, restored.Cost.ActualCost, 1e-12, "restart preserves the admission pricing unit")
			assert.InDelta(t, tc.wantCost, billing.captured+billing.charged, 1e-12, "one request must not become eight units")
			assert.InDelta(t, tc.wantCost, usage.lastLog.ActualCost, 1e-12)
			require.Equal(t, "settled", restored.State)
		})
	}
}

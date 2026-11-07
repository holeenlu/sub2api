package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSAccountUsesBPSHonorsConfiguredRoute(t *testing.T) {
	for _, mode := range []string{"bps", "bps_pinned", "account_disabled", "excel_only", "cloud", "model_native", "global_disabled"} {
		t.Run(mode, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			account := excelBPSDualGrantTestAccount(t, svc)
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg, "low")
			c, _ := gin.CreateTestContext(nil)
			want := true
			switch mode {
			case "bps_pinned":
				pinExcelBPSExecution(ctx)
			case "account_disabled":
				account.Extra["openai_excel_bps"] = false
				want = false
			case "excel_only":
				account = excelAccount()
			case "cloud":
				account.Extra[OpenAICloudEnabledKey] = true
				want = false
			case "model_native":
				account.Extra["openai_excel_bps_models"] = []string{"another-model"}
				want = false
			case "global_disabled":
				svc.settingService = nil
				want = false
			}
			require.Equal(t, want, svc.OpenAIWSAccountUsesBPS(ctx, c, account, "gpt-5.1"))
		})
	}
}

func TestOpenAIAccountContinuationWaitPlan(t *testing.T) {
	account := &Account{ID: 71, Concurrency: 8}
	for _, tc := range []struct {
		name    string
		cfg     *config.Config
		timeout time.Duration
		waiting int
	}{
		{name: "defaults", timeout: 45 * time.Second, waiting: 3},
		{name: "configured", cfg: &config.Config{Gateway: config.GatewayConfig{Scheduling: config.GatewaySchedulingConfig{
			StickySessionWaitTimeout: 17 * time.Millisecond, StickySessionMaxWaiting: 2,
			FallbackWaitTimeout: time.Minute, FallbackMaxWaiting: 99,
		}}}, timeout: 17 * time.Millisecond, waiting: 2},
		{name: "disabled", cfg: &config.Config{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: tc.cfg}
			plan := svc.OpenAIAccountContinuationWaitPlan(context.Background(), account)
			require.Equal(t, account.ID, plan.AccountID)
			require.Equal(t, account.Concurrency, plan.MaxConcurrency)
			require.Equal(t, tc.timeout, plan.Timeout)
			require.Equal(t, tc.waiting, plan.MaxWaiting)
			limit, err := plan.RefreshLimit(context.Background())
			require.NoError(t, err)
			require.Equal(t, account.Concurrency, limit)
		})
	}
	require.Nil(t, (&OpenAIGatewayService{}).OpenAIAccountContinuationWaitPlan(context.Background(), nil))
}

func TestOpenAIAccountContinuationWaitPlanRefreshesEffectiveLimit(t *testing.T) {
	account := quotaGuardOpenAIAccount(time.Now().Add(-time.Second))
	account.Concurrency = 8
	ctx := WithSchedulingQuotaRequest(context.Background(), []byte(`{"model":"gpt-5.1","input":"next"}`), ContentModerationProtocolOpenAIResponses)
	svc := &OpenAIGatewayService{}
	plan := svc.OpenAIAccountContinuationWaitPlan(ctx, account)
	require.Equal(t, 1, plan.MaxConcurrency, "reuse existing critical-window capacity")
	account.Extra = nil
	limit, err := plan.RefreshLimit(ctx)
	require.NoError(t, err)
	require.Equal(t, 8, limit, "the queue must refresh effective capacity rather than freeze its first value")
	require.Equal(t, 8, account.Concurrency, "the plan must not rewrite configured capacity")
}

package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExcelBPSPolicyLookupFailureCannotSelectNative(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	account := excelBPSDualGrantTestAccount(t, svc)
	svc.settingService = &SettingService{settingRepo: &excelBPSImageSettingsRepo{err: errors.New("database unavailable")}}
	ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	err := svc.checkExcelBPSExecutionRoute(ctx, account, "gpt-5.1")
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.False(t, failure.ShouldRetryNextAccount())
	require.True(t, failure.RequestNotSent)
	require.True(t, account.IsExcelBPSEnabled(), "a settings outage cannot disable the account")
}

func TestExcelBPSExecutionPinSurvivesRouteChanges(t *testing.T) {
	for _, change := range []string{"disabled", "other_model", "cloud", "global_disabled"} {
		t.Run(change, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			account := excelBPSDualGrantTestAccount(t, svc)
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			require.NoError(t, svc.checkExcelBPSExecutionRoute(ctx, account, "gpt-5.1"))
			require.True(t, excelBPSExecutionPinned(ctx))
			switch change {
			case "disabled":
				account.Extra["openai_excel_bps"] = false
			case "other_model":
				account.Extra["openai_excel_bps_models"] = []string{"other"}
			case "cloud":
				account.Extra[OpenAICloudEnabledKey] = true
			case "global_disabled":
				svc.settingService = nil
			}
			err := svc.checkExcelBPSExecutionRoute(ctx, account, "gpt-5.1")
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.True(t, failure.RequestNotSent)
			require.False(t, failure.ShouldReportAccountScheduleFailure())
			require.False(t, turnAccountEligible(ctx, account, UsageAuthorizationModeNative))
			require.False(t, turnAccountEligible(ctx, account, UsageAuthorizationModeCloud))
			// A new independent submission receives its own budget and can use
			// current settings. A retry reuses the original pinned budget.
			fresh := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			require.NoError(t, svc.checkExcelBPSExecutionRoute(fresh, account, "gpt-5.1"))
			require.False(t, excelBPSExecutionPinned(fresh))
		})
	}
}

func TestExcelBPSExecutionPinUsesMappedModel(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	account := excelBPSDualGrantTestAccount(t, svc)
	account.Credentials["model_mapping"] = map[string]any{"public": "selected"}
	account.Extra["openai_excel_bps_models"] = []string{"selected"}
	ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	require.NoError(t, svc.checkExcelBPSExecutionRoute(ctx, account, "public"))
	require.True(t, excelBPSExecutionPinned(ctx))
	require.True(t, turnAccountEligible(ctx, account, UsageAuthorizationModeBPS))
}

func TestExcelBPSExecutionPinConcurrentReaders(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			pinExcelBPSExecution(ctx)
			_ = excelBPSExecutionPinned(ctx)
		}()
	}
	group.Wait()
	require.True(t, excelBPSExecutionPinned(ctx))
}

func TestExcelBPSCooldownCannotBlockIndependentNativeSearch(t *testing.T) {
	for _, scope := range []string{"account", "model", "model_permission", "endpoint"} {
		t.Run(scope, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			account := excelBPSDualGrantTestAccount(t, svc)
			account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra", "other": "gpt-5.1", "native": "gpt-5.4"}
			account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra", "gpt-5.1"}
			cooldown := excelBPSRateLimitScope{}
			switch scope {
			case "model":
				cooldown.model = "gpt-6-astra"
			case "endpoint":
				cooldown.endpoint = "/basispoints/api/responses"
			}
			if scope == "model_permission" {
				svc.coolDownExcelBPSModel(account, "gpt-6-astra")
			} else {
				_, err := svc.coolDownExcelBPSScope(t.Context(), account, cooldown, "86400", time.Time{})
				require.NoError(t, err)
			}
			scheduler := &defaultOpenAIAccountScheduler{service: svc}
			req := OpenAIAccountScheduleRequest{
				Platform: PlatformOpenAI, RequestedModel: "alias",
				RequiredCapability: OpenAIEndpointCapabilityAlphaSearch,
			}
			ok, reason := scheduler.isAccountRequestCompatibleReason(t.Context(), account, req)
			require.True(t, ok, "BPS cooldown must not affect independent native search: %s", reason)
			require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias", req.RequiredCapability))
			// Callers without an explicit capability remain guarded as before.
			// Endpoint-only state is enforced at dispatch, never widened to all
			// candidate capabilities or unrelated BPS endpoints.
			require.Equal(t, scope != "endpoint", svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias"))
			require.Equal(t, scope == "account", svc.isOpenAIAccountRequestRuntimeBlocked(account, "other"))
			require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "native"))
			for _, capability := range []OpenAIEndpointCapability{"", OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityResponsesCompatible, OpenAIEndpointCapabilityChatCompletions} {
				req.RequiredCapability = capability
				ok, reason = scheduler.isAccountRequestCompatibleReason(t.Context(), account, req)
				require.Equal(t, scope == "endpoint", ok, "capability=%s reason=%s", capability, reason)
				if scope == "model_permission" {
					require.Equal(t, excelBPSModelPermissionFilterReason, reason)
				} else if scope != "endpoint" {
					require.Equal(t, excelBPSRateLimitedFilterReason, reason)
				}
			}
			if scope == "endpoint" {
				until, err := svc.excelBPSRateLimitDeadline(t.Context(), account.ID, excelBPSRateLimitScope{model: "gpt-6-astra", endpoint: "/basispoints/api/responses"})
				require.NoError(t, err)
				require.True(t, until.After(time.Now()), "real BPS dispatch remains cooled")
				until, err = svc.excelBPSRateLimitDeadline(t.Context(), account.ID, excelBPSRateLimitScope{model: "gpt-6-astra", endpoint: "/basispoints/api/attachments"})
				require.NoError(t, err)
				require.True(t, until.IsZero())
			}
			// A real account-wide scheduling block still applies to Native.
			until := time.Now().Add(time.Minute)
			account.TempUnschedulableUntil = &until
			svc.BlockAccountScheduling(account, until, "test account block")
			require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias", OpenAIEndpointCapabilityAlphaSearch))
		})
	}
}

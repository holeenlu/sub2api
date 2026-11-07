package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSModelPermissionForward(t *testing.T) {
	for _, code := range []string{"basispoints_model_access_changed", "model_not_allowed"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", code, stream), func(t *testing.T) {
				account := excelAccount()
				account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra", "second-alias": "gpt-6-astra", "gpt-5.6-sol": "gpt-5.6-sol"}
				account.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra", "gpt-5.6-sol"}
				account.Extra["openai_excel_bps_auto_disable_on_403"] = true
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"` + code + `"}}`))}}
				svc := excelBPSGatewayTestService(upstream)
				svc.accountRepo = &excelBPSAutoDisableRepo{disable: func(context.Context, *Account) (bool, error) {
					t.Fatal("model rejection must not disable the account")
					return false, nil
				}}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				_, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"alias","input":"test","stream":%t}`, stream)))
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover, "a rejected model must try an eligible account before replying")
				require.False(t, failover.ShouldReportAccountScheduleFailure())
				require.False(t, c.Writer.Written(), "do not commit the rejected account's error")
				require.True(t, svc.isExcelBPSModelCoolingDown(account, "alias"))
				require.True(t, svc.isExcelBPSModelCoolingDown(account, "second-alias"))
				require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias"))
				require.False(t, svc.isExcelBPSModelCoolingDown(account, "gpt-5.6-sol"))
				require.Len(t, upstream.requests, 1)
				account.Extra["openai_excel_bps"] = false
				require.False(t, svc.isExcelBPSModelCoolingDown(account, "alias"))
			})
		}
	}
}

func TestExcelBPSModelPermissionPersistsWithoutBlockingOtherRoutes(t *testing.T) {
	repo := &excelBPSCooldownRepoStub{}
	first := &OpenAIGatewayService{accountRepo: repo}
	account := excelAccount()
	account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra", "second-alias": "gpt-6-astra", "other": "gpt-5.6-sol"}
	account.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra", "gpt-5.6-sol"}
	// A later stored deadline may come from another instance. The fixed local
	// model-permission duration cannot replace it with a shorter value.
	want := time.Now().Add(2 * time.Hour)
	_, err := repo.ExtendExcelBPSCooldown(t.Context(), account.ID, excelBPSModelPermissionScope("gpt-6-astra"), want)
	require.NoError(t, err)
	until, err := first.coolDownExcelBPSModelWithContext(t.Context(), account, "gpt-6-astra")
	require.NoError(t, err)
	require.Equal(t, want, until)

	restarted := &OpenAIGatewayService{accountRepo: repo, settingService: excelBPSSettingsTestService()}
	require.False(t, restarted.isExcelBPSModelCoolingDown(account, "alias"), "a stale candidate snapshot cannot load shared state")
	require.Zero(t, repo.readCalls)
	until, err = restarted.excelBPSModelCooldownDeadline(t.Context(), account.ID, "gpt-6-astra")
	require.NoError(t, err)
	require.Equal(t, want, until, "the sending owner sees durable model state before the snapshot refresh")
	setAccountModelRateLimitSnapshot(account, excelBPSModelPermissionScope("gpt-6-astra"), want, string(ExcelBPSModelPermissionReason), time.Now())
	require.True(t, restarted.isExcelBPSModelCoolingDown(account, "alias"))
	require.True(t, restarted.isExcelBPSModelCoolingDown(account, "second-alias"))
	require.False(t, restarted.isExcelBPSModelCoolingDown(account, "other"))
	require.False(t, restarted.isExcelBPSCoolingDown(account, "alias"), "permission state is not a 429")
	require.False(t, restarted.isExcelBPSUpstreamModelCoolingDown(account.ID+1, "gpt-6-astra"))
	require.True(t, account.IsExcelBPSEnabled())
	account.Extra["openai_cloud"] = true
	require.False(t, restarted.isExcelBPSModelCoolingDown(account, "alias"))
	delete(account.Extra, "openai_cloud")
	account.Extra["openai_excel_bps"] = false
	require.False(t, restarted.isExcelBPSModelCoolingDown(account, "alias"))
	require.Nil(t, account.RateLimitResetAt)
	require.Equal(t, 1, repo.readCalls, "only the explicit context-aware read reaches shared state")
}

func TestExcelBPSModelCooldownExpiryKeepsOtherPersistedScopes(t *testing.T) {
	account := excelAccount()
	activeScope := excelBPSModelPermissionScope("gpt-5.6-sol")
	expiredScope := excelBPSModelPermissionScope("gpt-6-astra")
	until := time.Now().Add(time.Hour)
	repo := &excelBPSCooldownRepoStub{deadlines: map[excelBPSScopedCooldownKey]time.Time{
		{accountID: account.ID, scope: activeScope}:                  until,
		{accountID: account.ID, scope: expiredScope}:                 time.Now().Add(-time.Minute),
		{accountID: account.ID, scope: excelBPSAccountCooldownScope}: until,
	}}
	svc := &OpenAIGatewayService{accountRepo: repo}
	expired, err := svc.excelBPSModelCooldownDeadline(t.Context(), account.ID, "gpt-6-astra")
	require.NoError(t, err)
	require.True(t, expired.IsZero())
	active, err := svc.excelBPSModelCooldownDeadline(t.Context(), account.ID, "gpt-5.6-sol")
	require.NoError(t, err)
	require.Equal(t, until, active)
	rateLimited, err := svc.excelBPSRateLimitDeadline(t.Context(), account.ID, excelBPSRateLimitScope{})
	require.NoError(t, err)
	require.Equal(t, until, rateLimited)
	require.Len(t, repo.deadlines, 3, "checking expiry never clears other scopes or declares recovery")
}

func TestExcelBPSModelCooldownDependencyFailuresStayLocal(t *testing.T) {
	dependencyErr := fmt.Errorf("cooldown store unavailable")
	repo := &excelBPSCooldownRepoStub{writeErr: dependencyErr, readErr: dependencyErr}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: excelBPSSettingsTestService()}
	account := excelAccount()
	until, err := svc.coolDownExcelBPSModelWithContext(t.Context(), account, "gpt-6-astra")
	require.ErrorIs(t, err, dependencyErr)
	require.True(t, until.After(time.Now()))
	_, err = svc.excelBPSModelCooldownDeadline(t.Context(), account.ID, "gpt-6-astra")
	require.ErrorIs(t, err, dependencyErr)
	require.True(t, account.IsExcelBPSEnabled())
	require.Nil(t, repo.deadlines)
	account.Extra["openai_excel_bps"] = false
	require.False(t, svc.isExcelBPSModelCoolingDown(account, "gpt-6-astra"), "local BPS dependency failure cannot disable native")
}

func TestExcelBPSModelCooldownExpiryAndBound(t *testing.T) {
	svc := &OpenAIGatewayService{settingService: excelBPSSettingsTestService()}
	account := excelAccount()
	svc.coolDownExcelBPSModel(account, "gpt-6-astra")
	svc.excelBPSModelCooldowns.until[excelBPSModelKey{account.ID, "gpt-6-astra"}] = time.Now().Add(-time.Second)
	require.False(t, svc.isExcelBPSModelCoolingDown(account, "gpt-6-astra"))
	for i := 0; i < excelBPSModelPermissionMaxEntries+10; i++ {
		svc.coolDownExcelBPSModel(account, fmt.Sprint(i))
	}
	require.Len(t, svc.excelBPSModelCooldowns.until, excelBPSModelPermissionMaxEntries)
	require.False(t, isExcelBPSModelPermissionError(401, "model_not_allowed"))
	require.False(t, isExcelBPSModelPermissionError(403, "permission_denied"))
}

func TestExcelBPSModelPermissionSchedulers(t *testing.T) {
	for _, advanced := range []bool{true, false} {
		t.Run(fmt.Sprintf("advanced=%t", advanced), func(t *testing.T) {
			ctx := context.Background()
			groupID := int64(101301)
			newAccount := func(id int64) Account {
				return Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "chatgpt"},
					Extra:       map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []any{"gpt-6-astra"}}}
			}
			accounts := []Account{newAccount(38301), newAccount(38302)}
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			svc := &OpenAIGatewayService{
				settingService:     excelBPSSettingsTestService(),
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
				cache:              &schedulerTestGatewayCache{},
				cfg:                cfg,
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(strconv.FormatBool(advanced)),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}
			require.NotNil(t, svc.getOpenAIAccountScheduler(ctx))
			selectModel := func(model string) (*AccountSelectionResult, error) {
				selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", model, nil, OpenAIUpstreamTransportAny, false)
				if selection != nil && selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				return selection, err
			}

			svc.coolDownExcelBPSModel(&accounts[0], "gpt-6-astra")
			for range 10 {
				selection, err := selectModel("gpt-6-astra")
				require.NoError(t, err)
				require.Equal(t, int64(38302), selection.Account.ID)
			}

			svc.coolDownExcelBPSModel(&accounts[1], "gpt-6-astra")
			_, err := selectModel("gpt-6-astra")
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Contains(t, err.Error(), excelBPSModelPermissionFilterReason+"=2")
			selection, err := selectModel("gpt-5.1")
			require.NoError(t, err, "models these accounts forward natively stay schedulable")
			require.NotNil(t, selection.Account)
		})
	}
}

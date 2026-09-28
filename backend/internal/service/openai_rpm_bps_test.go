package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSRPMCountsRetriesAndStopsBeforeSend(t *testing.T) {
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				bpsCompletionResponse(http.StatusOK, bpsStartRateLimitedStream("limited", bpsOrganizationTPMError)),
				bpsCompletionResponse(http.StatusOK, bpsCompletedStream("completed", "ok")),
			}}
			svc := openAIClientToolsTestService(upstream)
			cache := &openAIRPMTestCache{counts: map[int64]int{}}
			svc.rpmCache = cache
			account := excelAccount()
			account.Extra["base_rpm"] = limit
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-6-astra","input":"test"}`))
			if limit == 1 {
				require.ErrorIs(t, err, ErrOpenAIRPMExhausted)
				require.False(t, IsResponseCommitted(c), "handler must still be able to return local 429")
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}
			require.Len(t, upstream.requests, limit)
			require.Equal(t, limit, cache.counts[account.ID])
			_, cooling := svc.excelBPSCooldownUntil.Load(account.ID)
			require.False(t, cooling, "local RPM does not poison upstream account health")
		})
	}
}

func TestExcelBPSRPMDoesNotChargeUnsentRequests(t *testing.T) {
	for _, reason := range []string{"invalid_image", "canceled", "ineligible", "cache_down"} {
		t.Run(reason, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{}
			svc := openAIClientToolsTestService(upstream)
			account := excelAccount()
			account.Extra["base_rpm"] = 2
			cache := &openAIRPMTestCache{counts: map[int64]int{}}
			svc.rpmCache = cache
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx := context.Background()
			body := []byte(`{"model":"gpt-6-astra","input":"test"}`)
			switch reason {
			case "invalid_image":
				body = []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,broken"}]}]}`)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				c.Request = c.Request.WithContext(ctx)
			case "ineligible":
				latest := *account
				latest.Status = "disabled"
				svc.accountRepo = &turnAdmissionRepo{account: &latest}
			case "cache_down":
				cache.err = errors.New("redis down")
			}
			_, err := svc.Forward(ctx, c, account, body)
			require.Error(t, err)
			require.Empty(t, upstream.requests)
			require.Zero(t, cache.counts[account.ID])
			if reason == "cache_down" {
				require.ErrorIs(t, err, ErrOpenAIRPMUnavailable)
				require.False(t, IsResponseCommitted(c))
			}
		})
	}
}

func TestExcelBPSRPMUsesLatestLimit(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	svc := openAIClientToolsTestService(upstream)
	selected := excelAccount()
	latest := *selected
	latest.Status = StatusActive
	latest.Schedulable = true
	latest.Extra = maps.Clone(selected.Extra)
	latest.Extra["base_rpm"] = 1
	svc.accountRepo = &turnAdmissionRepo{account: &latest}
	cache := &openAIRPMTestCache{counts: map[int64]int{selected.ID: 1}}
	svc.rpmCache = cache
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req, err := newExcelBPSRequest(context.Background(), nil, "test-token", "test-account")
	require.NoError(t, err)
	_, err = svc.doExcelBPSSend(context.Background(), c, selected, "gpt-6-astra", req, "")
	require.ErrorIs(t, err, ErrOpenAIRPMExhausted)
	require.Empty(t, upstream.requests)
	require.Zero(t, selected.GetBaseRPM(), "never mutate a scheduler snapshot")
}

func TestExcelBPSRPMProbeCannotBypassCeiling(t *testing.T) {
	upstream := &bpsProbeUpstream{}
	svc := bpsProbeTestService(upstream)
	cache := &openAIRPMTestCache{counts: map[int64]int{}}
	svc.openaiGatewayService.rpmCache = cache
	account := excelAccount()
	account.Extra["base_rpm"] = 2
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil)
	require.Error(t, svc.testExcelBPSToolRoundtrip(c, account, "gpt-6-astra"))
	require.Len(t, upstream.bodies, 2, "the third probe step must not reach upstream")
	require.Equal(t, 2, cache.counts[account.ID])
}

func TestOpenAIRPMMixedPoolSurvivesCounterFailure(t *testing.T) {
	for _, mode := range []string{"legacy", "legacy_batch", "advanced"} {
		t.Run(mode, func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
			accounts := []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 10, Extra: map[string]any{"base_rpm": 1}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 10},
			}
			svc := openAIClientToolsTestService(&httpUpstreamRecorder{})
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: accounts}
			svc.cache = &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:rpm_session": 1}}
			svc.rpmCache = &openAIRPMTestCache{counts: map[int64]int{}, err: errors.New("redis down")}
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
			svc.cfg.Gateway.Scheduling.LoadBatchEnabled = mode == "legacy_batch"
			if mode == "advanced" {
				svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
			}
			selection, _, err := svc.SelectAccountWithScheduler(context.Background(), nil, "", "rpm_session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.Equal(t, int64(2), selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			_, _, err = svc.SelectAccountWithScheduler(context.Background(), nil, "", "", "gpt-5.1", map[int64]struct{}{2: {}}, OpenAIUpstreamTransportAny, false)
			require.Error(t, err, "an excluded fallback must not make the protected account usable")
		})
	}
}

func TestOpenAIRPMShadowCannotRaiseOrDisableParentLimit(t *testing.T) {
	parent := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"base_rpm": 2}}
	shadow := &Account{ID: 2, ParentAccountID: &parent.ID, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	cache := &openAIRPMTestCache{counts: map[int64]int{}}
	svc := &OpenAIGatewayService{accountRepo: newStubCredRepo(parent), rpmCache: cache}
	for i := 0; i < 2; i++ {
		ok, _, err := svc.TryAcquireOpenAIOAuthRPM(context.Background(), shadow)
		require.True(t, ok)
		require.NoError(t, err)
	}
	shadow.Extra = map[string]any{"base_rpm": 100}
	_, _, err := svc.TryAcquireOpenAIOAuthRPM(context.Background(), shadow)
	require.ErrorIs(t, err, ErrOpenAIRPMExhausted)
	require.Equal(t, 2, cache.counts[parent.ID])
	require.NotContains(t, cache.counts, shadow.ID)
	require.Equal(t, 100, shadow.GetBaseRPM(), "inherited limit must not overwrite stored settings")
	parent.Extra["base_rpm"] = 3
	allowed, _, err := svc.TryAcquireOpenAIOAuthRPM(context.Background(), shadow)
	require.NoError(t, err)
	require.True(t, allowed, "a changed parent policy is re-read")
}

func TestExcelBPSRPMAttachmentCacheAndGenerationShareCounter(t *testing.T) {
	body, _ := nativeGatewayBody(t)
	account := excelAccount()
	account.Extra["base_rpm"] = 2
	uploads, generations := 0, 0
	svc := openAIClientToolsTestService(nil)
	cache := &openAIRPMTestCache{counts: map[int64]int{}}
	svc.rpmCache = cache
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.Path == "/basispoints/api/attachments" {
			uploads++
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-native123"}`))}, nil
		}
		generations++
		return bpsCompletionResponse(200, bpsCompletedStream("done", "ok")), nil
	}}
	for i := 0; i < 2; i++ {
		plan, err := basispoints.PrepareNativeImages(body)
		require.NoError(t, err)
		_, err = plan.Upload(context.Background(), &svc.excelBPSAttachments, "same-session", func(ctx context.Context, img basispoints.InlineAttachment) (string, error) {
			return svc.uploadExcelBPSAttachment(ctx, account, "token", "id", img)
		})
		require.NoError(t, err)
	}
	require.Equal(t, 1, uploads, "duplicate images and the next cache hit must not upload again")
	require.Equal(t, 1, cache.counts[account.ID])
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req, err := newExcelBPSRequest(context.Background(), nil, "token", "id")
	require.NoError(t, err)
	resp, err := svc.doExcelBPSSend(context.Background(), c, account, "gpt-6-astra", req, "")
	require.NoError(t, err)
	resp.Body.Close()
	_, err = svc.doExcelBPSSend(context.Background(), c, account, "gpt-6-astra", req, "")
	require.ErrorIs(t, err, ErrOpenAIRPMExhausted)
	require.Equal(t, 1, generations)
	require.Equal(t, 2, cache.counts[account.ID])
	// A new scope must not bypass the exhausted limit through attachment upload.
	plan, err := basispoints.PrepareNativeImages(body)
	require.NoError(t, err)
	_, err = plan.Upload(context.Background(), &svc.excelBPSAttachments, "different-session", func(ctx context.Context, img basispoints.InlineAttachment) (string, error) {
		return svc.uploadExcelBPSAttachment(ctx, account, "token", "id", img)
	})
	require.ErrorIs(t, err, ErrOpenAIRPMExhausted)
	require.Equal(t, 1, uploads)
}

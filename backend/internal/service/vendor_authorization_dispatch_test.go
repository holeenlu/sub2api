package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type vendorAuthorizationDispatchRepo struct {
	AccountRepository
	native, bps      bool
	modes            []string
	group            int64
	denyNativeAt     int
	denyBPSAt        int
	credentialParent *Account
}

type vendorGenericOnlyDispatchRepo struct{ AccountRepository }

func (*vendorGenericOnlyDispatchRepo) ValidateVendorSupply(context.Context, *Account, *int64) error {
	return nil
}

func (r *vendorAuthorizationDispatchRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.credentialParent, nil
}

type vendorDispatchTokenCache struct {
	OpenAITokenCache
	reads int
}

func (c *vendorDispatchTokenCache) GetAccessToken(context.Context, string) (string, error) {
	c.reads++
	return "cached-token", nil
}

func (r *vendorAuthorizationDispatchRepo) ValidateVendorSupply(context.Context, *Account, *int64) error {
	if !r.native && !r.bps {
		return ErrVendorSupplyUnavailable
	}
	return nil
}

func (r *vendorAuthorizationDispatchRepo) ValidateVendorAuthorizationMode(_ context.Context, _ *Account, group *int64, mode string) error {
	r.modes = append(r.modes, mode)
	if group != nil {
		r.group = *group
	}
	if mode == VendorOpenAIModeNative && r.native && (r.denyNativeAt == 0 || len(r.modes) < r.denyNativeAt) {
		return nil
	}
	if mode == VendorOpenAIModeBPS && r.bps && (r.denyBPSAt == 0 || len(r.modes) < r.denyBPSAt) {
		return nil
	}
	return ErrVendorSupplyUnavailable
}

func (r *vendorAuthorizationDispatchRepo) FilterVendorSupply(_ context.Context, accounts []Account, _ *int64) ([]Account, error) {
	if !r.native && !r.bps {
		return nil, nil
	}
	out := append([]Account(nil), accounts...)
	for i := range out {
		out[i].VendorAuthorizationModesLoaded = true
		out[i].VendorNativeAllowed, out[i].VendorBPSAllowed = r.native, r.bps
	}
	return out, nil
}

func vendorAuthorizationDispatchContext() (*gin.Context, *Account, int64) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	owner, group := int64(7), int64(13)
	c.Set("api_key", &APIKey{ID: 2, GroupID: &group})
	a := &Account{ID: 1, VendorID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "test-native", "plan_type": "plus"},
		Extra:       map[string]any{"openai_excel_bps": true}, Concurrency: 1,
		VendorAuthorizationModesLoaded: true, VendorNativeAllowed: true, VendorBPSAllowed: true}
	return c, a, group
}

func TestVendorAuthorizationDispatchChecksActualHTTPModeAndRevocation(t *testing.T) {
	c, account, group := vendorAuthorizationDispatchContext()
	repo := &vendorAuthorizationDispatchRepo{native: true, bps: true}
	transport := &supplyValidationHTTP{}
	s := &OpenAIGatewayService{accountRepo: repo, httpUpstream: transport}
	// A BPS marker cannot turn the native shared/plugin transport into a BPS call.
	req, _ := http.NewRequestWithContext(context.WithValue(t.Context(), vendorAuthorizationModeKey{}, VendorOpenAIModeBPS), http.MethodPost, "https://provider.invalid/v1/responses", nil)
	response, err := s.doOpenAIUpstream(req, "", account, &group)
	require.NoError(t, err)
	require.Equal(t, []string{"native"}, repo.modes)
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.invalid/basispoints/api/responses", nil)
	}
	bps, _, err := s.doExcelBPSRequestTo(t.Context(), c, account, "test", "https://provider.invalid/basispoints/api/responses", build)
	require.NoError(t, err)
	require.NoError(t, bps.Body.Close())
	require.Equal(t, []string{"native", "bps"}, repo.modes)
	require.Equal(t, group, repo.group)
	repo.native = false
	_, err = s.doOpenAIUpstream(req, "", account, &group)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	repo.bps = false
	_, _, err = s.doExcelBPSRequestTo(t.Context(), c, account, "test", "https://provider.invalid/basispoints/api/responses", build)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	require.Equal(t, 2, transport.calls, "neither revoked mode may reach the upstream")
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "existing stream", string(body), "revocation blocks new dispatch, not the accepted response")
	require.NoError(t, response.Body.Close())
	// A repository with only the old generic qualification cannot permit OpenAI modes.
	require.ErrorIs(t, validateVendorNativeDispatch(t.Context(), &vendorGenericOnlyDispatchRepo{}, account, &group), ErrVendorSupplyUnavailable)
}

func TestVendorAuthorizationDispatchStickySnapshotAndBPSOnlyFallback(t *testing.T) {
	_, account, group := vendorAuthorizationDispatchContext()
	repo := &vendorAuthorizationDispatchRepo{native: true, bps: true}
	cache := &supplyValidationCache{accounts: []*Account{account}}
	s := NewSchedulerSnapshotService(cache, nil, repo, nil, nil)
	rows, _, err := s.ListSchedulableAccounts(t.Context(), &group, PlatformOpenAI, false)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, excelBPSHasNativeFallback(&rows[0]))
	repo.native = false
	rows, _, err = s.ListSchedulableAccounts(t.Context(), &group, PlatformOpenAI, false)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.False(t, rows[0].VendorOpenAIModeAllowed(VendorOpenAIModeNative))
	require.False(t, excelBPSHasNativeFallback(&rows[0]), "a remaining native token is not native-mode permission")
	require.True(t, account.VendorNativeAllowed, "do not mutate shared cached pointers")
	// Even a stale sticky account must pass the live mode guard before forwarding.
	require.ErrorIs(t, validateVendorNativeDispatch(t.Context(), repo, account, &group), ErrVendorSupplyUnavailable)
	repo.bps = false
	sticky, err := s.GetAccount(t.Context(), account.ID)
	require.Nil(t, sticky)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
}

func TestVendorAuthorizationDispatchWSChecksBeforeDialAndBeforeWrite(t *testing.T) {
	for _, revokeAt := range []int{1, 2} {
		t.Run(map[int]string{1: "before_dial", 2: "after_dial_before_write"}[revokeAt], func(t *testing.T) {
			c, account, group := vendorAuthorizationDispatchContext()
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			conn := &openAIWSCaptureConn{}
			dialer := &openAIWSCaptureDialer{conn: conn}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(dialer)
			t.Cleanup(pool.Close)
			repo := &vendorAuthorizationDispatchRepo{native: true, bps: true, denyNativeAt: revokeAt}
			s := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, openaiWSPool: pool, cache: &stubGatewayCache{}, toolCorrector: NewCodexToolCorrector()}
			_, err := s.forwardOpenAIWSV2(t.Context(), c, account, map[string]any{"model": "gpt-6-astra", "input": []any{}}, "", "test-execution", "test-native", OpenAIWSProtocolDecision{}, false, false, "gpt-6-astra", "gpt-6-astra", time.Now(), 0, time.Time{}, "", nil, nil)
			require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
			require.Equal(t, revokeAt-1, dialer.DialCount())
			require.Equal(t, group, repo.group)
			conn.mu.Lock()
			writes := len(conn.writes)
			conn.mu.Unlock()
			require.Zero(t, writes, "revocation must stop response.create even after acquiring a WS connection")
		})
	}
}

func TestVendorAuthorizationDispatchBPSOnly429NeverSendsNative(t *testing.T) {
	for _, staleNative := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh_permissions", true: "stale_sticky_permissions"}[staleNative], func(t *testing.T) {
			s := excelBPSGatewayTestService(nil)
			f := newVendorExcelOAuthFixture(t)
			f.storeVerified(t, f.identity.tokens(t, f.identity.claims(true), nil))
			s.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: f.oauth}
			account := f.account
			account.Proxy.ID = 17
			account.ProxyID = &account.Proxy.ID
			c, _, _ := vendorAuthorizationDispatchContext()
			account.VendorAuthorizationModesLoaded = true
			account.VendorNativeAllowed, account.VendorBPSAllowed = staleNative, true
			repo := &vendorAuthorizationDispatchRepo{bps: true}
			s.accountRepo = repo
			bpsCalls, nativeCalls := 0, 0
			s.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				if req.URL.Path == "/basispoints/api/responses" {
					bpsCalls++
					return bpsCompletionResponse(429, `{}`), nil
				}
				nativeCalls++
				return bpsCompletionResponse(200, bpsCompletedStream("should-not-execute", "native")), nil
			}}
			_, err := s.Forward(t.Context(), c, account, []byte(`{"model":"gpt-6-astra","input":"hello"}`))
			require.Error(t, err)
			require.Equal(t, 1, bpsCalls, "expected BPS upstream rejection before fallback: %v", err)
			require.Zero(t, nativeCalls, "a stale cached grant must not bypass live native-mode revocation")
		})
	}
}

func TestVendorAuthorizationDispatchRevokedTokenCannotReadCache(t *testing.T) {
	for _, mode := range []string{VendorOpenAIModeNative, VendorOpenAIModeBPS} {
		t.Run(mode, func(t *testing.T) {
			_, account, _ := vendorAuthorizationDispatchContext()
			if mode == VendorOpenAIModeBPS {
				account.Credentials["client_id"] = openai.ExcelClientID
			}
			repo := &vendorAuthorizationDispatchRepo{native: true, bps: true, credentialParent: account}
			cache := &vendorDispatchTokenCache{}
			s := &OpenAIGatewayService{accountRepo: repo, openAITokenProvider: NewOpenAITokenProvider(repo, cache, nil)}
			token, _, err := s.GetAccessToken(t.Context(), account)
			require.NoError(t, err)
			require.Equal(t, "cached-token", token)
			require.Equal(t, []string{mode}, repo.modes)
			repo.native, repo.bps = mode != VendorOpenAIModeNative, mode != VendorOpenAIModeBPS
			shadow := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &account.ID}
			for _, candidate := range []*Account{account, shadow} {
				token, _, err = s.GetAccessToken(t.Context(), candidate)
				require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
				require.Empty(t, token)
			}
			require.Equal(t, 1, cache.reads, "check the resolved credential owner's actual primary mode before reading cached credentials")
			require.Zero(t, s.openAITokenProvider.SnapshotRuntimeMetrics().RefreshRequests)
			require.Equal(t, []string{mode, mode, mode}, repo.modes)
		})
	}
}

func TestVendorBPSAttachmentRechecksModeWithoutSpendingGenerationAttempt(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(fmt.Sprint(revoke), func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			f := newVendorExcelOAuthFixture(t)
			f.storeVerified(t, f.identity.tokens(t, f.identity.claims(true), nil))
			svc.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: f.oauth}
			account := f.account
			account.Proxy.ID = 17
			account.ProxyID = &account.Proxy.ID
			repo := &vendorAuthorizationDispatchRepo{bps: true}
			if revoke {
				repo.denyBPSAt = 2
			}
			svc.accountRepo = repo
			c, _, group := vendorAuthorizationDispatchContext()
			calls := 0
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				if req.URL.Path == "/basispoints/api/attachments" {
					return bpsCompletionResponse(200, `{"openai_file_id":"file-approved"}`), nil
				}
				return bpsCompletionResponse(200, bpsCompletedStream("approved", "ok")), nil
			}}
			body, _ := nativeGatewayBody(t)
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
			budget.limit = 1
			result, err := svc.Forward(ctx, c, account, body)
			require.Equal(t, group, repo.group, "forward error: %v; dispatch checks: %v", err, repo.modes)
			if revoke {
				require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.EqualValues(t, "selection", failure.Stage)
				require.True(t, failure.RequestNotSent)
				require.False(t, failure.ShouldReportAccountScheduleFailure())
				require.Zero(t, calls, "revocation after token acquisition must stop image upload")
				require.Nil(t, result)
				require.False(t, c.Writer.Written())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 2, calls, "one deduplicated attachment plus one generation")
			require.Equal(t, []string{VendorOpenAIModeBPS, VendorOpenAIModeBPS, VendorOpenAIModeBPS}, repo.modes)
			require.Equal(t, 1, budget.attempts, "uploads must not consume the generation allowance")
		})
	}
}

func TestVendorBPSImagesRechecksModeWithoutTransportFailure(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	f := newVendorExcelOAuthFixture(t)
	f.storeVerified(t, f.identity.tokens(t, f.identity.claims(true), nil))
	svc.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: f.oauth}
	account := f.account
	account.Proxy.ID = 17
	account.ProxyID = &account.Proxy.ID
	repo := &vendorAuthorizationDispatchRepo{bps: true, denyBPSAt: 2}
	svc.accountRepo = repo
	upstream := &httpUpstreamRecorder{}
	svc.httpUpstream = upstream
	c, _, group := vendorAuthorizationDispatchContext()
	parsed := &OpenAIImagesRequest{Endpoint: openAIImagesGenerationsEndpoint, Model: "gpt-image-2", Prompt: "draw", N: 1}
	result, fallback, err := svc.forwardExcelBPSImages(t.Context(), c, account, parsed, "gpt-image-2", "gpt-image-2", time.Now())
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.EqualValues(t, "selection", failure.Stage)
	require.True(t, failure.RequestNotSent)
	require.False(t, failure.ShouldReportAccountScheduleFailure())
	require.Equal(t, group, repo.group)
	require.Nil(t, result)
	require.False(t, fallback)
	require.Empty(t, upstream.requests)
	require.False(t, c.Writer.Written(), "the handler may choose another authorized account")
	require.Empty(t, c.GetString(OpsUpstreamErrorMessageKey))
	events, _ := c.Get(OpsUpstreamErrorsKey)
	require.Empty(t, events, "local revocation is not a provider HTTP attempt")
}

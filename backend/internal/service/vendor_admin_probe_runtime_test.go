package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminProbeDispatchRepo struct {
	*supplyValidationRepo
	bpsGroup *int64
	bpsCalls int
	bpsErr   error
}

func (r *adminProbeDispatchRepo) ValidateExcelBPSDispatch(_ context.Context, _ *Account, group *int64, _ string) error {
	r.bpsGroup = group
	r.bpsCalls++
	return r.bpsErr
}
func TestVendorAdminProbeMarkerPropagatesToBPSDispatchAndRechecksGrant(t *testing.T) {
	owner := int64(7)
	account := excelAccount()
	account.VendorID = &owner
	// The marker is issued by the actual account-test authorization path only.
	synthetic := *account
	synthetic.Extra = map[string]any{"synthetic_ui_test": true}
	synthetic.Platform = PlatformAnthropic
	repo := &adminProbeDispatchRepo{supplyValidationRepo: &supplyValidationRepo{account: &synthetic, allowed: true}}
	probe := &AccountTestService{accountRepo: repo}
	actor := authz.Subject{UserID: 2, Role: authz.SuperAdmin, ManagementRequest: true}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil).WithContext(authz.WithSubject(context.Background(), actor))
	require.NoError(t, probe.TestAccountConnection(c, account.ID, "gpt-5.6-sol", "", ""))
	require.Nil(t, vendorDispatchGroup(c), "verified interactive diagnostics may ask repository for any valid supplier group")
	repo.account = account
	sends := 0
	gateway := excelBPSImagesTestService(&codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}})
	gateway.accountRepo = repo
	ctx := context.WithValue(c.Request.Context(), excelBPSUpstreamModelKey{}, "gpt-5.6-sol")
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "https://bps.openai.com/basispoints/api/responses", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	}
	response, _, err := gateway.doExcelBPSRequestTo(ctx, c, account, "probe", "https://bps.openai.com/basispoints/api/responses", build)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, sends)
	require.Equal(t, 1, repo.bpsCalls)
	require.Nil(t, repo.bpsGroup)
	repo.bpsErr = errors.New("grant revoked")
	_, _, err = gateway.doExcelBPSRequestTo(ctx, c, account, "probe", "https://bps.openai.com/basispoints/api/responses", build)
	require.ErrorContains(t, err, "503")
	require.Equal(t, 2, repo.bpsCalls)
	require.Equal(t, 1, sends, "grant readback must stop the second upstream send")
	// A diagnostics marker does not bypass the live group's supply policy.
	repo.bpsErr = nil
	repo.err = ErrVendorSupplyUnavailable
	_, _, err = gateway.doExcelBPSRequestTo(ctx, c, account, "probe", "https://bps.openai.com/basispoints/api/responses", build)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	require.Equal(t, 1, sends, "inactive/unauthorized supplier group must stop dispatch")
	repo.err = nil
	proxyID := int64(9)
	proxied := *account
	proxied.ProxyID = &proxyID
	proxied.Proxy = &Proxy{ID: proxyID, VendorID: &owner, Protocol: "http", Host: "approved.invalid", Port: 8080}
	qualified := &qualifiedAccountHTTPUpstream{HTTPUpstream: gateway.httpUpstream, repo: repo, account: &proxied, groupID: vendorDispatchGroup(c), mode: VendorOpenAIModeBPS}
	request, err := build(ctx)
	require.NoError(t, err)
	_, err = qualified.Do(request, "http://unapproved.invalid:8080", proxied.ID, proxied.Concurrency)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	require.Equal(t, 1, sends, "diagnostics nil group must not permit proxy replacement")
	unmarked, _ := gin.CreateTestContext(httptest.NewRecorder())
	unmarked.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	group := vendorDispatchGroup(unmarked)
	require.NotNil(t, group)
	require.Zero(t, *group, "ordinary missing group never acquires diagnostics authority")
}

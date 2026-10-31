package service

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

type bpsAuditDispatchRepo struct {
	vendorAuthorizationDispatchRepo
	denyBPSAt int
}

func (r *bpsAuditDispatchRepo) ValidateVendorAuthorizationMode(ctx context.Context, a *Account, group *int64, mode string) error {
	if mode == VendorOpenAIModeBPS && r.denyBPSAt > 0 && len(r.modes)+1 >= r.denyBPSAt {
		r.modes = append(r.modes, mode)
		if group != nil {
			r.group = *group
		}
		return ErrVendorSupplyUnavailable
	}
	return r.vendorAuthorizationDispatchRepo.ValidateVendorAuthorizationMode(ctx, a, group, mode)
}
func TestExcelBPSVendorAttachmentRechecksModeWithoutSpendingGenerationAttempt(t *testing.T) {
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
			repo := &bpsAuditDispatchRepo{vendorAuthorizationDispatchRepo: vendorAuthorizationDispatchRepo{bps: true}}
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
			svc.cfg.Gateway.OpenAITurnMaxAttempts = 1
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
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
			require.Equal(t, 1, budget.attemptBudget.Snapshot().Attempts, "uploads must not consume the generation allowance")
		})
	}
}

func TestExcelBPSVendorImagesRechecksModeWithoutTransportFailure(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	f := newVendorExcelOAuthFixture(t)
	f.storeVerified(t, f.identity.tokens(t, f.identity.claims(true), nil))
	svc.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: f.oauth}
	account := f.account
	account.Proxy.ID = 17
	account.ProxyID = &account.Proxy.ID
	repo := &bpsAuditDispatchRepo{vendorAuthorizationDispatchRepo: vendorAuthorizationDispatchRepo{bps: true}, denyBPSAt: 2}
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

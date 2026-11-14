package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type excelPrimaryDispatchTestRepo struct {
	AccountRepository
	current   *Account
	mutate    string
	committed bool
}

func (r *excelPrimaryDispatchTestRepo) GetByID(context.Context, int64) (*Account, error) {
	if r.committed && r.mutate == "reload fails" {
		return nil, errors.New("durable reload unavailable")
	}
	if r.committed && r.mutate == "reauthorize before reload" {
		r.current.Credentials["access_token"] = "reauthorized-after-CAS"
		r.current.Credentials["refresh_token"] = "reauthorized-refresh-after-CAS"
	}
	return snapshotOAuthRefreshAccount(r.current), nil
}

func (r *excelPrimaryDispatchTestRepo) UpdateExcelPrimaryCredentialsIfUnchanged(_ context.Context, before *Account, credentials map[string]any) (bool, error) {
	if r.mutate == "reauthorize" {
		r.current.Credentials["refresh_token"] = "admin-new-grant"
		r.current.Credentials["access_token"] = "admin-new-access"
	}
	if r.mutate == "manual disable during refresh" {
		r.current.Extra["openai_excel_bps"] = false
	}
	if !reflect.DeepEqual(r.current.Credentials, before.Credentials) {
		return false, nil
	}
	r.current.Credentials = shallowCopyMap(credentials)
	r.committed = true
	return true, nil
}

func (r *excelPrimaryDispatchTestRepo) ValidateExcelBPSDispatch(ctx context.Context, snapshot *Account, _ *int64, model string) error {
	_, bound := ExcelBPSDispatchFingerprint(ctx, snapshot.ID)
	if !bound || !r.current.IsExcelBPSEnabled() || !reflect.DeepEqual(snapshot.Credentials, r.current.Credentials) ||
		!excelPrimaryRefreshPolicyEqual(snapshot, r.current) || model != "gpt-6-astra" {
		return errors.New("dispatch authorization changed")
	}
	return nil
}

func (*excelPrimaryDispatchTestRepo) ValidateVendorSupply(context.Context, *Account, *int64) error {
	return nil
}

type excelPrimaryDispatchRefresh struct{}

func (*excelPrimaryDispatchRefresh) CacheKey(*Account) string                  { return "excel-primary-send-test" }
func (*excelPrimaryDispatchRefresh) CanRefresh(*Account) bool                  { return true }
func (*excelPrimaryDispatchRefresh) NeedsRefresh(*Account, time.Duration) bool { return true }
func (*excelPrimaryDispatchRefresh) Refresh(_ context.Context, account *Account) (map[string]any, error) {
	credentials := shallowCopyMap(account.Credentials)
	credentials["access_token"] = "refreshed-bps-access"
	credentials["refresh_token"] = "rotated-bps-refresh"
	credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	return credentials, nil
}

func TestExcelPrimaryRefreshActualSenderUsesConfirmedSnapshot(t *testing.T) {
	for _, mutation := range []string{"", "reauthorize", "manual disable during refresh", "manual disable before send", "reload fails", "reauthorize before reload"} {
		t.Run(mutation, func(t *testing.T) {
			account := excelAccount()
			account.Credentials["refresh_token"] = "original-refresh"
			account.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
			repo := &excelPrimaryDispatchTestRepo{current: snapshotOAuthRefreshAccount(account), mutate: mutation}
			requests := 0
			upstream := &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
				requests++
				require.Equal(t, "Bearer refreshed-bps-access", req.Header.Get("Authorization"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
			}}
			svc := excelBPSGatewayTestService(nil)
			svc.httpUpstream = upstream
			svc.accountRepo = repo
			provider := NewOpenAITokenProvider(repo, nil, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), &excelPrimaryDispatchRefresh{})
			svc.openAITokenProvider = provider
			ctx := withExcelBPSUpstreamModel(EnsureOpenAITurnBudget(t.Context(), svc.cfg), "gpt-6-astra")
			token, prepared, err := svc.getExcelBPSAccessTokenSnapshot(ctx, account)
			if mutation == "reauthorize" || mutation == "manual disable during refresh" || mutation == "reload fails" || mutation == "reauthorize before reload" {
				require.Error(t, err)
				require.Zero(t, requests)
				return
			}
			require.NoError(t, err)
			require.NotSame(t, account, prepared)
			require.Equal(t, "test-token", account.GetCredential("access_token"), "never mutate the scheduler snapshot")
			ctx, err = svc.ExcelBPSDispatchContext(ctx, prepared, token)
			require.NoError(t, err)
			if mutation == "manual disable before send" {
				repo.current.Extra["openai_excel_bps"] = false
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp, _, err := svc.doExcelBPSRequestTo(ctx, c, prepared, "test", basispoints.ResponsesURL, func(ctx context.Context) (*http.Request, error) {
				return newExcelBPSRequest(ctx, []byte(`{"model":"gpt-6-astra"}`), token, prepared.GetChatGPTAccountID())
			})
			if mutation == "" {
				require.NoError(t, err)
				require.Equal(t, 1, requests)
				require.NoError(t, resp.Body.Close())
			} else {
				require.Error(t, err)
				require.Zero(t, requests)
			}
		})
	}
}

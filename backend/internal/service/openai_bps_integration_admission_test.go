package service

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBPSAdmissionRejectsDisabledAccountsBeforeSending(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformOpenAIBPS} {
		for _, unavailable := range []string{"disabled", "database"} {
			t.Run(platform+"/"+unavailable, func(t *testing.T) {
				svc, selected := bpsFixture()
				if platform == PlatformOpenAI {
					selected = excelAccount()
				}
				latest := *selected
				repo := &turnAdmissionRepo{account: &latest}
				if unavailable == "disabled" {
					latest.Schedulable = false
				} else {
					repo.err = errors.New("database unavailable")
				}
				svc.accountRepo = repo
				svc.requireLatestTurnAdmission = true
				upstream := &httpUpstreamRecorder{}
				svc.httpUpstream = upstream
				c, _ := bpsContext(1, "/v1/responses")
				_, err := svc.Forward(c.Request.Context(), c, selected, bpsRequestBody("hello", false))
				require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
				require.Empty(t, upstream.requests)
			})
		}
	}
}

func TestExcelBPSRechecksAdmissionBeforeEncryptedRecovery(t *testing.T) {
	for _, change := range []string{"disabled", "bps_disabled", "model_scope", "proxy", "database"} {
		t.Run(change, func(t *testing.T) {
			account := excelAccount()
			repo := &turnAdmissionRepo{account: account}
			svc := openAIClientToolsTestService(nil)
			svc.accountRepo = repo
			svc.requireLatestTurnAdmission = true
			calls := 0
			svc.httpUpstream = &bpsTestUpstream{send: func(_ *http.Request, _ string) (*http.Response, error) {
				calls++
				latest := *account
				latest.Extra = maps.Clone(account.Extra)
				switch change {
				case "disabled":
					latest.Schedulable = false
				case "bps_disabled":
					latest.Extra["openai_excel_bps"] = false
				case "model_scope":
					latest.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
				case "proxy":
					latest.Proxy = &Proxy{Protocol: "http", Host: "changed.invalid", Port: 8080}
				case "database":
					repo.err = errors.New("database unavailable")
				}
				repo.account = &latest
				return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))}, nil
			}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := svc.Forward(context.Background(), c, account, excelBPSEncryptedHistoryRequest(false))
			require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
			require.Equal(t, 1, calls, "a rejected repair must never reach the upstream")
			require.Equal(t, 3, repo.reads)
		})
	}
}

func TestExcelBPSSelectedModelsDoNotHarvestCodexTickets(t *testing.T) {
	account := excelAccount()
	account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	require.False(t, CodexTicketHarvestEnabled(account, "gpt-6-astra"))
	require.True(t, CodexTicketHarvestEnabled(account, "gpt-6-sol"))
	bound := bindOpenAIWSTicket(account, "gpt-6-sol", nil, nil)
	changed := *account
	changed.Extra = maps.Clone(account.Extra)
	changed.Extra["openai_excel_bps_models"] = []string{"gpt-6-sol"}
	require.True(t, IsOpenAITurnAdmissionError((&OpenAIGatewayService{}).checkOpenAIWSBinding(&changed, "gpt-6-sol", bound)))
}

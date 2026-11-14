package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSRepairCauseSurvivesProtocolAndApplication(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, mode := range []string{"known", "unknown"} {
			for _, stop := range []string{"budget", "revoked", "transport"} {
				t.Run(protocol+"/"+mode+"/"+stop, func(t *testing.T) {
					code, tool := "text(42);", `[{"type":"custom","name":"exec"}]`
					if mode == "unknown" {
						code, tool = `{"name":"missing","arguments":{}}`, `[{"type":"function","name":"inspect","parameters":{"type":"object"}}]`
					}
					first := bpsCompletionResponse(200, excelBPSRepairWire(t, "original", "Run client tool", code))
					first.Header.Set("X-Request-Id", "req_original")
					privateErr := errors.New("PRIVATE_TRANSPORT_SECRET")
					calls := 0
					upstream := &nativeAttachmentUpstream{do: func(*http.Request, string, int64, int) (*http.Response, error) {
						calls++
						if calls == 1 {
							return first, nil
						}
						return nil, privateErr
					}}
					svc := excelBPSGatewayTestService(nil)
					svc.httpUpstream = upstream
					account := excelAccount()
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					if stop == "budget" {
						svc.cfg.Gateway.OpenAITurnMaxAttempts = 1
					}
					if stop == "revoked" {
						fixture := newVendorExcelOAuthFixture(t)
						fixture.storeVerified(t, fixture.identity.tokens(t, fixture.identity.claims(true), nil))
						svc.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: fixture.oauth}
						account = fixture.account
						account.Proxy.ID = 17
						account.ProxyID = &account.Proxy.ID
						svc.accountRepo = &bpsAuditDispatchRepo{vendorAuthorizationDispatchRepo: vendorAuthorizationDispatchRepo{bps: true}, denyBPSAt: 3}
						c, _, _ = vendorAuthorizationDispatchContext()
					}
					body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"calculate","stream":%t,"tools":%s}`, protocol != "json", tool))
					var output strings.Builder
					var result *OpenAIForwardResult
					var err error
					if protocol == "ws" {
						SetOpenAIClientTransport(c, OpenAIClientTransportWS)
						result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, account, body, "gpt-6-astra", func(event []byte) error { output.Write(event); return nil })
					} else {
						result, err = svc.Forward(t.Context(), c, account, body)
						output.Write(rec.Body.Bytes())
					}
					require.Error(t, err)
					require.NotNil(t, result)
					require.Equal(t, 10, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					require.NotContains(t, output.String(), "PRIVATE_")
					require.NotContains(t, err.Error(), "PRIVATE_")
					require.NotContains(t, output.String(), "response.output_item.added")
					if protocol != "ws" {
						require.Equal(t, "req_original", result.RequestID)
					}
					switch stop {
					case "budget":
						require.ErrorIs(t, err, ErrOpenAITurnAttemptBudgetExhausted)
						require.Equal(t, 1, calls)
						require.Equal(t, "turn_budget", classifyOpenAIAccountScheduleFailure(t.Context(), err).reason)
					case "revoked":
						require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
						var failure *UpstreamFailoverError
						require.ErrorAs(t, err, &failure)
						require.EqualValues(t, "selection", failure.Stage)
						require.False(t, failure.ShouldReportAccountScheduleFailure())
						require.NotEmpty(t, classifyOpenAIAccountScheduleFailure(t.Context(), err).reason)
						require.Equal(t, 1, calls, "authorization is checked before repair network dispatch")
					case "transport":
						require.ErrorIs(t, err, privateErr)
						var failure *OpenAITransportAttemptError
						require.ErrorAs(t, err, &failure)
						require.Equal(t, 2, calls, "an accepted original generation cannot be replayed")
					}
				})
			}
		}
	}
}

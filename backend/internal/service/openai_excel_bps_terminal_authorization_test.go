package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSInBandAccountRejectionUsesSameDisablePolicy(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, code := range []string{"account_deactivated", "permission_denied"} {
			t.Run(fmt.Sprintf("%s/stream=%t", code, stream), func(t *testing.T) {
				body := fmt.Sprintf("data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_denied\",\"status\":\"failed\",\"error\":{\"code\":%q,\"status\":403},\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n", code)
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, body)}
				svc := excelBPSGatewayTestService(upstream)
				account := excelAccount()
				account.Extra["openai_excel_bps_auto_disable_on_403"] = true
				account.Extra[ExcelBPSAutoMoveOn403Key] = true
				account.Extra[ExcelBPS403TargetGroupIDKey] = 0
				var actions []string
				svc.accountRepo = &excelBPSGroupActionRepo{
					excelBPSAutoDisableRepo: excelBPSAutoDisableRepo{disable: func(context.Context, *Account) (bool, error) {
						actions = append(actions, "disable")
						return true, nil
					}},
					move: func(context.Context, *Account) (bool, error) {
						actions = append(actions, "move")
						return true, nil
					},
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				result, err := svc.Forward(t.Context(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"hello","stream":%t}`, stream)))
				require.Error(t, err)
				require.NotNil(t, result)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				require.Len(t, upstream.requests, 1, "terminal state observation cannot start another generation")
				if code == "account_deactivated" {
					require.Equal(t, []string{"move", "disable"}, actions)
				} else {
					require.Empty(t, actions)
				}
			})
		}
	}
}

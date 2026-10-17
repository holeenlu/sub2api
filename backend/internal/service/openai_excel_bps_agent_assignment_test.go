package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSRejectsNewEncryptedAssignmentBeforeAnySubmission(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		for _, top := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("ignore=%t/top=%t/stream=%t", ignore, top, stream), func(t *testing.T) {
					upstream := &httpUpstreamRecorder{}
					svc := excelBPSGatewayTestService(upstream)
					account := excelAccount()
					account.Extra[ExcelBPSIgnoreEncryptedContentKey] = ignore
					extra := ""
					content := `[{"type":"input_text","text":"Header"},{"type":"encrypted_content","data":"SYNTHETIC_PRIVATE"}]`
					param := "input[105].content[1]"
					if top {
						extra = `,"encrypted_content":"SYNTHETIC_PRIVATE"`
						content = `[{"type":"input_text","text":"Header"}]`
						param = "input[105].encrypted_content"
					}
					history := strings.Repeat(`{"role":"user","content":"Synthetic history"},`, 105)
					body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"input":[%s{"type":"agent_message","content":%s%s},{"type":"additional_tools","tools":[]}]}`, stream, history, content, extra))
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
					_, err := svc.Forward(context.Background(), c, account, body)
					require.Error(t, err)
					require.Equal(t, 400, rec.Code)
					require.Empty(t, upstream.requests)
					require.Equal(t, param, gjson.Get(rec.Body.String(), "error.param").String())
					require.Contains(t, rec.Body.String(), "new encrypted agent assignment")
					require.Contains(t, rec.Body.String(), "original plaintext task")
					require.Contains(t, rec.Body.String(), "history omission and compaction cannot recover this task")
					require.NotContains(t, rec.Body.String(), "SYNTHETIC_PRIVATE")
				})
			}
		}
	}
}

func TestExcelBPSWSIgnoreEncryptedHistoryPreservesAssignmentGuard(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		for _, current := range []bool{false, true} {
			t.Run(fmt.Sprintf("ignore=%t/current=%t", ignore, current), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_plain\",\"status\":\"completed\",\"output\":[]}}\n\n")}
				svc := excelBPSGatewayTestService(upstream)
				account := excelAccount()
				account.Extra[ExcelBPSIgnoreEncryptedContentKey] = ignore
				tail := ""
				if !current {
					tail = `,{"role":"user","content":"continue with plaintext"}`
				}
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":true,"input":[{"type":"agent_message","content":[{"type":"input_text","text":"task heading"},{"type":"encrypted_content","data":"PRIVATE_CIPHERTEXT"}]}%s]}`, tail))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
				SetOpenAIClientTransport(c, OpenAIClientTransportWS)
				var output strings.Builder
				_, err := svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, account, body, "gpt-6-astra", func(event []byte) error {
					output.Write(event)
					return nil
				})
				require.NotContains(t, output.String(), "PRIVATE_CIPHERTEXT")
				if ignore && !current {
					require.NoError(t, err)
					require.Len(t, upstream.requests, 1)
					require.Contains(t, string(upstream.bodies[0]), "Encrypted content omitted")
					require.Contains(t, string(upstream.bodies[0]), "continue with plaintext")
					require.NotContains(t, string(upstream.bodies[0]), "PRIVATE_CIPHERTEXT")
				} else {
					require.Error(t, err)
					require.Empty(t, upstream.requests)
					require.Contains(t, output.String(), "basispoints_request_invalid")
					if current {
						require.Contains(t, output.String(), "new encrypted agent assignment")
						require.Equal(t, "input[0].content[1]", gjson.Get(output.String(), "error.param").String())
						require.Equal(t, int64(400), gjson.Get(output.String(), "status").Int())
					}
				}
			})
		}
	}
}

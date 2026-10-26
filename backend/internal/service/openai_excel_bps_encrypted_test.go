package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const excelBPSInvalidCiphertext = `{"error":{"code":"invalid_encrypted_content","message":"private upstream diagnostic"}}`
const excelBPSInvalidCiphertextNoCode = `{"error":{"message":"The encrypted content fixture...end could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`

func TestExcelBPSInvalidEncryptedContentClassification(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      bool
	}{
		{"code", excelBPSInvalidCiphertext, true},
		{"diagnostic without code", excelBPSInvalidCiphertextNoCode, true},
		{"SSE rejection", `{"type":"response.failed","response":{"error":{"code":"invalid_encrypted_content"}}}`, true},
		{"bare SSE rejection", `{"type":"error","code":"invalid_encrypted_content","message":"Please retry"}`, true},
		{"not an error event", `{"type":"response.output_text.delta","code":"invalid_encrypted_content","delta":"example"}`, false},
		{"signature diagnostic", `{"error":{"code":"thinking_signature_invalid","message":"The encrypted content fixture could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`, true},
		{"unrelated signature", `{"error":{"code":"thinking_signature_invalid","message":"Invalid thinking signature"}}`, false},
		{"unrelated code", `{"error":{"code":"invalid_value","message":"The encrypted content could not be verified and could not be decrypted or parsed"}}`, false},
		{"echoed text", `{"error":{"message":"Invalid input. User said: the encrypted content could not be verified and could not be decrypted or parsed"}}`, false},
		{"unrelated request", `{"error":{"message":"Unsupported parameter: encrypted_content"}}`, false},
		{"invalid JSON", `{"error":{"code":"invalid_encrypted_content"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isOpenAIEncryptedContextRejection([]byte(tc.raw)))
		})
	}
}

func excelBPSEncryptedHistoryRequest(stream bool) []byte {
	return []byte(fmt.Sprintf(`{"model":"gpt-5.4","stream":%t,"reasoning":{"effort":"max"},"prompt_cache_key":"encrypted-recovery-session","input":[{"role":"user","content":"continue original task"},{"type":"reasoning","encrypted_content":"private-ciphertext"},{"type":"function_call","call_id":"call_old","name":"inspect","arguments":"{}"},{"type":"function_call_output","call_id":"call_old","output":"original tool result"}]}`, stream))
}

// Since 2026-10-24 unreadable history is recovered before generation (see
// openai_encrypted_history_recovery_test.go). When the history is still refused
// after every reduction, the client receives the safe 400 without diagnostics.
func TestExcelBPSInvalidEncryptedContentAfterRecoveryKeepsSafeRejection(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, rejection := range []string{excelBPSInvalidCiphertext, excelBPSInvalidCiphertextNoCode} {
			t.Run(fmt.Sprintf("stream=%t/code=%t", stream, rejection == excelBPSInvalidCiphertext), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(rejection))},
					{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(rejection))},
				}}
				svc := excelBPSGatewayTestService(upstream)
				body := excelBPSEncryptedHistoryRequest(stream)
				original := bytes.Clone(body)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				result, err := svc.Forward(context.Background(), c, excelAccount(), body)
				require.Error(t, err)
				require.Nil(t, result)
				require.Len(t, upstream.requests, 2, "the reasoning reduction is tried once; nothing else is removable")
				require.Equal(t, original, body)
				require.Contains(t, string(upstream.bodies[0]), "private-ciphertext")
				require.NotContains(t, string(upstream.bodies[1]), "private-ciphertext")
				require.Contains(t, string(upstream.bodies[1]), "original tool result")
				require.Equal(t, 400, rec.Code)
				require.Equal(t, "invalid_encrypted_content", gjson.Get(rec.Body.String(), "error.code").String())
				require.NotContains(t, rec.Body.String(), "private")
			})
		}
	}
}

func TestExcelBPSInvalidEncryptedContentDoesNotRetryOtherFailures(t *testing.T) {
	for _, tc := range []struct {
		name, rejection, body string
		status                int
	}{
		{"ordinary 400", `{"error":{"code":"invalid_value"}}`, string(excelBPSEncryptedHistoryRequest(false)), 400},
		{"no removable reasoning", excelBPSInvalidCiphertext, `{"model":"gpt-5.6-sol","input":"continue"}`, 400},
		{"non-400", excelBPSInvalidCiphertext, string(excelBPSEncryptedHistoryRequest(false)), 500},
		{"signature rejection with 503", `{"error":{"code":"thinking_signature_invalid","message":"The encrypted content fixture could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`, string(excelBPSEncryptedHistoryRequest(false)), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.rejection))}}
			svc := excelBPSGatewayTestService(upstream)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := svc.Forward(context.Background(), c, excelAccount(), []byte(tc.body))
			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, tc.status, rec.Code)
		})
	}
}

// A local format rejection must use the available native grant without sending
// ciphertext to BPS, dropping it, or treating the account as unhealthy.
func TestExcelBPSEncryptedMessageUsesSameAccountNativeBeforeSend(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, assignment := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/assignment=%t", protocol, assignment), func(t *testing.T) {
				svc := excelBPSGatewayTestService(nil)
				account := excelBPSNativeFallbackTestAccount(t, svc)
				itemType := "message"
				if assignment {
					itemType = "agent_message"
					account.Extra[ExcelBPSIgnoreEncryptedContentKey] = true
				}
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"input":[{"type":"%s","role":"user","content":[{"type":"input_text","text":"task"},{"type":"encrypted_content","encrypted_content":"PRIVATE_CIPHERTEXT"}]}]}`, protocol != "json", itemType))
				calls := 0
				svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
					calls++
					require.Equal(t, account.ID, id)
					require.Equal(t, "/backend-api/codex/responses", req.URL.Path)
					require.Equal(t, "Bearer native-codex-token", req.Header.Get("Authorization"))
					raw, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Contains(t, string(raw), "PRIVATE_CIPHERTEXT")
					require.NotContains(t, string(raw), "Encrypted content omitted")
					return bpsCompletionResponse(200, bpsCompletedStream("resp_native_encrypted", "native answer")), nil
				}}
				ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
				// Pool-managed requests still use the same owner for this format decision.
				ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget).bpsRecovery.managed = true
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
				var err error
				if protocol == "ws" {
					_, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "native-codex-token", body, len(body), "gpt-6-astra", "", "", "", "", 1, func(event []byte) error { _, e := rec.Write(append(event, byte(10))); return e })
				} else {
					_, err = svc.Forward(ctx, c, account, body)
				}
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				require.Contains(t, rec.Body.String(), "native answer")
				require.NotContains(t, rec.Body.String(), "PRIVATE_CIPHERTEXT")
				require.True(t, account.IsExcelBPSEnabled())
				require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
			})
		}
	}
}

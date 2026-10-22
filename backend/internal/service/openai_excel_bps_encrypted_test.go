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

func TestExcelBPSInvalidEncryptedContentPreservesHistoryWithoutReplay(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, rejection := range []string{excelBPSInvalidCiphertext, excelBPSInvalidCiphertextNoCode} {
			t.Run(fmt.Sprintf("stream=%t/code=%t", stream, rejection == excelBPSInvalidCiphertext), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(rejection))}}
				svc := excelBPSGatewayTestService(upstream)
				body := excelBPSEncryptedHistoryRequest(stream)
				original := bytes.Clone(body)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				result, err := svc.Forward(context.Background(), c, excelAccount(), body)
				require.Error(t, err)
				require.Nil(t, result)
				require.Len(t, upstream.requests, 1, "never replay by removing encrypted reasoning")
				require.Equal(t, original, body)
				require.Contains(t, string(upstream.lastBody), "private-ciphertext")
				require.Contains(t, string(upstream.lastBody), "original tool result")
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
		{"encrypted compaction", excelBPSInvalidCiphertext, `{"model":"gpt-5.6-sol","input":[{"role":"user","content":"continue"},{"type":"reasoning","encrypted_content":"opaque"},{"type":"compaction","encrypted_content":"context"}]}`, 400},
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

//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 上游 401/400 的响应体常把 key 片段原样回显，这段文本会经
// extractUpstreamErrorMessage → sanitizeUpstreamErrorMessage 同时进入客户端响应
// 与 ops 落库，必须在这一层擦掉。
func TestSanitizeUpstreamErrorMessageRedactsCredentialLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		leaked   string
		wantMark string
	}{
		{
			name:     "anthropic key in upstream 401 body",
			in:       "Incorrect API key provided: sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789",
			leaked:   "sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789",
			wantMark: "sk-ant-***REDACTED***",
		},
		{
			name:     "openai style key",
			in:       "invalid api key sk-proj-AbCdEfGhIjKlMnOpQrStUvWxYz01",
			leaked:   "sk-proj-AbCdEfGhIjKlMnOpQrStUvWxYz01",
			wantMark: "sk-***REDACTED***",
		},
		{
			name:     "google key",
			in:       "API key not valid: AIzaSyA1234567890abcdefghijklmnopqrstuv",
			leaked:   "AIzaSyA1234567890abcdefghijklmnopqrstuv",
			wantMark: "AIza***REDACTED***",
		},
		{
			name:     "bearer jwt",
			in:       "token rejected: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk",
			leaked:   "dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk",
			wantMark: "eyJ***REDACTED.JWT***",
		},
		{
			name:     "query param still masked",
			in:       `Get "https://upstream.example/v1?key=super-secret-value": timeout`,
			leaked:   "super-secret-value",
			wantMark: "key=***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeUpstreamErrorMessage(tt.in)
			require.NotContains(t, got, tt.leaked, "凭据必须被擦除")
			require.Contains(t, got, tt.wantMark)
		})
	}
}

// 不含凭据的文本必须原样保留，否则会削弱排障信息。
func TestSanitizeUpstreamErrorMessageKeepsOrdinaryText(t *testing.T) {
	t.Parallel()

	msg := "upstream returned 529: overloaded_error, please retry later"
	require.Equal(t, msg, sanitizeUpstreamErrorMessage(msg))
	require.Equal(t, "", sanitizeUpstreamErrorMessage(""))
}

// 错误透传规则打开 PassthroughBody 时会把上游原文写给客户端，凭据必须在
// ExtractUpstreamErrorMessage 这个对客边界就被擦掉——ops 落库前的脱敏补不了
// 已经发出去的响应。
func TestExtractUpstreamErrorMessageSanitizesForClient(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":{"type":"authentication_error","message":"Incorrect API key provided: sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"}}`)

	got := ExtractUpstreamErrorMessage(body)
	require.NotContains(t, got, "sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789")
	require.Contains(t, got, "sk-ant-***REDACTED***")
	require.Contains(t, got, "Incorrect API key provided", "非凭据部分要保留，否则排障无从下手")

	// 服务内部判定用的未导出版本保持原文，供上游错误类型识别使用。
	require.Contains(t, extractUpstreamErrorMessage(body), "sk-ant-api03-")
}

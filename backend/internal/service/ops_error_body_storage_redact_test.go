//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 上游 401 常把被拒的 key 原样回显在 message 这种"非敏感键"的字符串值里，按键名擦
// 不到；落库前必须再过一遍密钥字面量模式，否则 upstream_error_detail 会存明文。
func TestSanitizeErrorBodyForStorageRedactsCredentialLiterals(t *testing.T) {
	t.Parallel()

	const leaked = "sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"

	jsonBody := `{"error":{"type":"authentication_error","message":"Incorrect API key provided: ` + leaked + `"}}`
	got, truncated := sanitizeErrorBodyForStorage(jsonBody, 4096)
	require.False(t, truncated)
	require.NotContains(t, got, leaked)
	require.Contains(t, got, "sk-ant-***REDACTED***")
	require.Contains(t, got, "authentication_error", "非敏感内容原样保留")

	plainBody := "upstream said: invalid key " + leaked + " (rejected)"
	got, truncated = sanitizeErrorBodyForStorage(plainBody, 4096)
	require.False(t, truncated)
	require.NotContains(t, got, leaked)
	require.Contains(t, got, "sk-ant-***REDACTED***")

	// 截断路径同样擦除。
	got, truncated = sanitizeErrorBodyForStorage(plainBody, 40)
	require.True(t, truncated)
	require.NotContains(t, got, leaked)
}

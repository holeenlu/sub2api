package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

// Classify only explicit ciphertext rejections. Keep the original history intact;
// ciphertext is never removed to manufacture a retry.
func isExcelBPSInvalidEncryptedContent(raw []byte) bool {
	if !gjson.ValidBytes(raw) {
		return false
	}
	if code := extractUpstreamErrorCode(raw); code != "" {
		return code == "invalid_encrypted_content"
	}
	// Accept the same diagnostic when a provider omits its error code. Do not classify
	// arbitrary mentions of encryption (or echoed request text) as this error.
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(raw)))
	return strings.HasPrefix(message, "the encrypted content ") &&
		strings.Contains(message, "could not be verified") &&
		strings.Contains(message, "could not be decrypted or parsed")
}

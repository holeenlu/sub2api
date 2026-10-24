package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// isOpenAIRequestScoped403 excludes explicit relay request failures from account
// penalties. Some relays append SSE after their JSON error; trust only the first
// complete object, never echoed input or a later event. Refusal policy remains
// owned by the shared classifier.
func isOpenAIRequestScoped403(account *Account, statusCode int, body []byte) bool {
	if account == nil || account.Platform != PlatformOpenAI || statusCode != http.StatusForbidden {
		return false
	}
	var payload json.RawMessage
	if json.NewDecoder(bytes.NewReader(body)).Decode(&payload) != nil {
		return false
	}
	if IsUpstreamContentRefusal(payload) {
		return true
	}
	code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.code").String()))
	errorType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.type").String()))
	return errorType == "upstream_error" && (code == "" || code == "upstream_error") &&
		strings.EqualFold(strings.TrimSpace(gjson.GetBytes(payload, "error.message").String()),
			"failed to WebSocket dial: expected handshake response status code 101 but got 403")
}

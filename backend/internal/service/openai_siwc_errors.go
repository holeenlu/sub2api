package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Semantic statuses apply only inside an accepted HTTP stream. A real HTTP
// response retains its status, including direct-admission {"detail":...} bodies.
func siwcErrorStatus(payload []byte) int {
	code := openAIStreamFailedEventErrorCode(payload)
	if code == "" {
		code = gjson.GetBytes(payload, "error.code").String()
	}
	switch code {
	case "subscription_sharing_usage_limit_exceeded":
		return http.StatusTooManyRequests
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		return http.StatusServiceUnavailable
	case "subscription_sharing_invalid_user":
		return http.StatusUnauthorized
	case "subscription_sharing_unsupported_capability":
		return http.StatusBadRequest
	case "subscription_sharing_user_not_eligible", "subscription_sharing_route_not_supported", "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context":
		return http.StatusForbidden
	}
	return 0
}

func writeSIWCError(c *gin.Context, status int, headers http.Header, payload []byte) error {
	code := openAIStreamFailedEventErrorCode(payload)
	if code == "" {
		code = gjson.GetBytes(payload, "error.code").String()
	}
	message := sanitizeUpstreamErrorMessage(extractOpenAISSEErrorMessage(payload))
	if message == "" {
		message = sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(payload))
	}
	if message == "" {
		message = sanitizeUpstreamErrorMessage(gjson.GetBytes(payload, "detail").String())
	}
	if message == "" {
		message = "The SIWC request could not be completed"
	}
	if status < 400 {
		status = http.StatusBadGateway
	}
	body := payload
	if nested := gjson.GetBytes(payload, "response.error"); nested.IsObject() {
		body = []byte("{\"error\":" + nested.Raw + "}")
	}
	if !json.Valid(body) {
		body, _ = json.Marshal(gin.H{"detail": message})
	}
	if c != nil {
		if id := headers.Get("x-request-id"); id != "" {
			c.Header("x-request-id", id)
		}
		if retry := headers.Get("Retry-After"); retry != "" {
			c.Header("Retry-After", retry)
		}
		setOpsUpstreamError(c, status, message, "")
		// The existing writer owns queue keepalives and committed-SSE failures.
		var envelope gin.H
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		_ = decoder.Decode(&envelope)
		if envelope == nil {
			envelope = gin.H{"detail": message}
		}
		writeOpenAIResponsesErrorJSON(c, status, envelope)
	}
	return fmt.Errorf("SIWC upstream request failed (status %d, code %s)", status, code)
}

func (s *OpenAIGatewayService) rejectSIWCResponseToken(ctx context.Context, resp *http.Response, account *Account) {
	if resp == nil || resp.Request == nil {
		return
	}
	token := strings.TrimPrefix(resp.Request.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		return
	}
	if oauth := s.siwcOAuthService(); oauth != nil {
		_ = oauth.rejectSIWCAccessToken(ctx, account, token, s.openAITokenProvider.tokenCache)
	}
}

func (s *OpenAIGatewayService) handleSIWCHTTPError(ctx context.Context, resp *http.Response, c *gin.Context, account *Account, token string) error {
	body := s.readUpstreamErrorBody(resp)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		if oauth := s.siwcOAuthService(); oauth != nil {
			_ = oauth.rejectSIWCAccessToken(ctx, account, token, s.openAITokenProvider.tokenCache)
		}
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account), Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"), Passthrough: true, Kind: "http_error", Message: sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(body))})
	return writeSIWCError(c, resp.StatusCode, resp.Header, body)
}

// Compatibility clients have a different envelope, but retain SIWC's exact
// machine code and parameter instead of applying BPS-specific normalization.
func siwcCompatibilityErrorDetails(payload []byte) gin.H {
	source := gjson.GetBytes(payload, "response.error")
	if !source.IsObject() {
		source = gjson.GetBytes(payload, "error")
	}
	detail := gin.H{}
	if source.IsObject() {
		decoder := json.NewDecoder(strings.NewReader(source.Raw))
		decoder.UseNumber()
		_ = decoder.Decode(&detail)
	}
	message, _ := detail["message"].(string)
	if message == "" {
		message = gjson.GetBytes(payload, "detail").String()
	}
	if message == "" {
		message = "SIWC request could not be completed"
	}
	if gjson.GetBytes(payload, "type").String() == "response.incomplete" {
		message = "SIWC response was incomplete"
		detail["code"] = "OPENAI_SIWC_RESPONSE_INCOMPLETE"
	}
	detail["message"] = sanitizeUpstreamErrorMessage(message)
	if kind, _ := detail["type"].(string); kind == "" {
		detail["type"] = "api_error"
	}
	return detail
}

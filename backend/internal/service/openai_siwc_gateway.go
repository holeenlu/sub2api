package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Adapted from ranxi2001 e48c0a5b0; public SIWC transport remains independent of Codex/BPS.
func normalizeSIWCBody(body []byte) ([]byte, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&payload) != nil || payload == nil {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "Invalid Responses request")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "Invalid Responses request")
	}
	// Public SIWC HTTP has no persistent previous-response store. The client WS
	// owner expands its own accepted history before reaching this boundary.
	if previous, exists := payload["previous_response_id"]; exists && previous != nil && previous != "" {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_FULL_CONTEXT_REQUIRED", "SIWC HTTP requires complete input context; previous_response_id cannot be resolved on this request")
	}
	for _, key := range []string{"conversation", "prompt"} {
		if value, exists := payload[key]; exists && value != nil && value != "" {
			return nil, infraerrors.BadRequest("OPENAI_SIWC_FULL_CONTEXT_REQUIRED", "SIWC HTTP requires complete input context; stored conversations and prompt references cannot be resolved")
		}
	}
	for _, key := range []string{"background", "conversation", "previous_response_id", "max_output_tokens", "max_completion_tokens", "max_tool_calls", "metadata", "moderation", "multi_agent", "prompt", "prompt_cache_retention", "prompt_cache_options", "safety_identifier", "temperature", "top_logprobs", "top_p", "truncation", "user", "client_metadata"} {
		delete(payload, key)
	}
	payload["store"] = false
	payload["stream"] = true
	if text, ok := payload["input"].(string); ok {
		payload["input"] = []any{map[string]any{"type": "message", "role": "user", "content": text}}
	}
	if payload["input"] == nil {
		payload["input"] = []any{}
	}
	if items, ok := payload["input"].([]any); ok {
		kept := make([]any, 0, len(items))
		for _, raw := range items {
			if item, ok := raw.(map[string]any); ok {
				if item["type"] == "configuration_update" {
					if update, ok := item["reasoning"].(map[string]any); ok {
						if effort, ok := update["effort"].(string); ok && effort != "" {
							reasoning, _ := payload["reasoning"].(map[string]any)
							if reasoning == nil {
								reasoning = map[string]any{}
							}
							reasoning["effort"] = effort
							payload["reasoning"] = reasoning
						}
					}
					continue
				}
				if item["role"] == "system" {
					item["role"] = "developer"
				}
			}
			kept = append(kept, raw)
		}
		payload["input"] = kept
	}
	items, ok := payload["input"].([]any)
	if !ok {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "SIWC input must be a string or array")
	}
	for itemIndex, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "Invalid SIWC input item")
		}
		switch item["type"] {
		case "tool_search_call", "tool_search_output", "input_audio", "input_video":
			return nil, infraerrors.BadRequest("OPENAI_SIWC_UNSUPPORTED_CAPABILITY", "SIWC does not support tool search, audio or video input")
		case "additional_tools":
			if item["role"] != "developer" {
				return nil, infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "Additional tools require the developer role")
			}
			tools, ok := item["tools"].([]any)
			if !ok {
				return nil, infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "Invalid additional tools")
			}
			for toolIndex, tool := range tools {
				if err := validateSIWCTool(tool, false, fmt.Sprintf("input[%d].tools[%d]", itemIndex, toolIndex)); err != nil {
					return nil, err
				}
			}
		}
		for _, key := range []string{"content", "output"} {
			if parts, ok := item[key].([]any); ok {
				for _, raw := range parts {
					if part, ok := raw.(map[string]any); ok {
						switch part["type"] {
						case "input_audio", "input_video", "audio", "video":
							return nil, infraerrors.BadRequest("OPENAI_SIWC_UNSUPPORTED_CAPABILITY", "SIWC does not support audio or video input")
						}
					}
				}
			}
		}
	}
	if raw, exists := payload["tools"]; exists {
		tools, ok := raw.([]any)
		if !ok {
			return nil, infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "SIWC tools must be an array")
		}
		local, hosted := make([]any, 0, len(tools)), make([]any, 0, len(tools))
		for toolIndex, raw := range tools {
			if err := validateSIWCTool(raw, true, fmt.Sprintf("tools[%d]", toolIndex)); err != nil {
				return nil, err
			}
			tool := raw.(map[string]any)
			if tool["type"] == "function" || tool["type"] == "custom" {
				local = append(local, tool)
			} else {
				hosted = append(hosted, tool)
			}
		}
		// additional_tools keeps existing tool names and choices stable. Inventing
		// a namespace here would also require rewriting calls and every history item.
		if len(local) > 0 {
			payload["input"] = append([]any{map[string]any{"type": "additional_tools", "role": "developer", "tools": local}}, items...)
		}
		payload["tools"] = hosted
	}
	return marshalOpenAIUpstreamJSON(payload)
}

func validateSIWCTool(raw any, topLevel bool, path string) error {
	tool, ok := raw.(map[string]any)
	if !ok {
		return infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "Invalid SIWC tool declaration")
	}
	switch tool["type"] {
	case "function", "custom":
		return nil
	case "web_search", "web_search_preview":
		if topLevel {
			return nil
		}
	case "namespace":
		if !topLevel {
			break
		}
		name, _ := tool["name"].(string)
		children, ok := tool["tools"].([]any)
		if strings.TrimSpace(name) == "" || !ok {
			return infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "Invalid SIWC tool namespace")
		}
		for index, child := range children {
			if err := validateSIWCTool(child, false, fmt.Sprintf("%s.tools[%d]", path, index)); err != nil {
				return err
			}
		}
		return nil
	}
	kind, _ := tool["type"].(string)
	if kind == "" || len(kind) > 64 {
		kind = "<invalid>"
	}
	return infraerrors.BadRequest("OPENAI_SIWC_UNSUPPORTED_TOOL", fmt.Sprintf("SIWC does not support tool type %q at %s; use function/custom tools", kind, path))
}

func (s *OpenAIGatewayService) buildSIWCRequest(ctx context.Context, c *gin.Context, body []byte, token string) (*http.Request, error) {
	if suffix := openAIResponsesRequestPathSuffix(c); suffix != "" {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_UNSUPPORTED_ENDPOINT", "SIWC supports /v1/responses; compact and other endpoints are unavailable")
	}
	if token == "" {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_AUTH_REQUIRED", "Authorize SIWC for this account first")
	}
	normalized, err := normalizeSIWCBody(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(normalized))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "Sub2API")
	return req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI)), nil
}

func (s *OpenAIGatewayService) forwardSIWC(ctx context.Context, c *gin.Context, account *Account, body []byte, start time.Time) (result *OpenAIForwardResult, err error) {
	defer func() {
		if err == nil {
			return
		}
		// This opt-in grant is a distinct billing/permission path. A failed
		// request must not silently replay against Codex, BPS or another user.
		var failover *UpstreamFailoverError
		if errors.As(err, &failover) {
			status, code, message := infraerrors.Code(failover.Cause), infraerrors.Reason(failover.Cause), infraerrors.Message(failover.Cause)
			if failover.Cause == nil || status == http.StatusInternalServerError {
				status = failover.ClientStatusCode
				code = "OPENAI_SIWC_REQUEST_STOPPED"
				message = failover.ClientMessage
			}
			if status < 400 {
				status = http.StatusBadGateway
			}
			if message == "" || message == "internal error" {
				message = "SIWC request stopped; no alternate authorization was used"
			}
			err = infraerrors.New(status, code, message)
		}
		if c != nil && !c.Writer.Written() {
			status, code, message := infraerrors.Code(err), infraerrors.Reason(err), infraerrors.Message(err)
			if status < 400 {
				status = http.StatusBadGateway
			}
			if message == "internal error" {
				message = "SIWC request could not be completed; no alternate authorization was used"
			}
			writeOpenAIResponsesErrorJSON(c, status, gin.H{"error": gin.H{"type": "invalid_request_error", "code": code, "message": message}})
		}
	}()
	if !account.IsSIWCEligible() {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_ACCOUNT_REQUIRED", "SIWC is unavailable for this account type")
	}
	if suffix := openAIResponsesRequestPathSuffix(c); suffix != "" {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_UNSUPPORTED_ENDPOINT", "SIWC supports /v1/responses only")
	}
	reqModel := gjson.GetBytes(body, "model").String()
	mapped := strings.TrimSpace(account.GetMappedModel(reqModel))
	if mapped == "" {
		mapped = reqModel
	}
	upstream, err := sjson.SetBytes(body, "model", mapped)
	if err != nil {
		return nil, err
	}
	if cacheKey := gjson.GetBytes(upstream, "prompt_cache_key").String(); cacheKey != "" {
		upstream, err = sjson.SetBytes(upstream, "prompt_cache_key", isolateOpenAIUpstreamSessionID(getAPIKeyIDFromContext(c), account, cacheKey))
		if err != nil {
			return nil, err
		}
	}
	// Validate before acquiring tokens. Preserve the downstream stream preference;
	// the shared parser converts the upstream SSE terminal response to JSON.
	if _, err = normalizeSIWCBody(upstream); err != nil {
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, openAIResponsesUpstreamEndpoint)
	setActualOpenAIUsageAuthorizationMode(c, UsageAuthorizationModeSIWC)
	result, err = s.forwardOpenAIPassthrough(ctx, c, account, upstream, body, reqModel, false, extractOpenAIReasoningEffortFromBody(body, reqModel), gjson.GetBytes(body, "stream").Bool(), start)
	if result != nil {
		result.AuthorizationMode = UsageAuthorizationModeSIWC
		result.UpstreamModel = mapped
	}
	return result, err
}

// SIWC shares the existing OAuth service and its private storage, not a second
// credential manager or a fallback to the primary Codex token.
func (s *OpenAIGatewayService) siwcOAuthService() *OpenAIOAuthService {
	if s == nil || s.openAITokenProvider == nil {
		return nil
	}
	return s.openAITokenProvider.openAIOAuthService
}

// Public SIWC does not authorize the separate Images, Embeddings or alpha-search
// routes. Reject before obtaining credentials rather than falling back to Codex.
func rejectSIWCUnsupportedEndpoint(c *gin.Context) error {
	message := "SIWC supports Responses requests only; this endpoint is unavailable for the selected authorization"
	MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalFeatureGate)
	writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "OPENAI_SIWC_UNSUPPORTED_ENDPOINT", "message": message}})
	return infraerrors.BadRequest("OPENAI_SIWC_UNSUPPORTED_ENDPOINT", message)
}

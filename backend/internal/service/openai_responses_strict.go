package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// This is a loss detector for the selected native account, not a JSON Schema
// validator. Relay, Cloud and BPS contracts are checked by their own adapters.
type openAIResponsesCompatibilityError struct{ Fields []string }

func (e *openAIResponsesCompatibilityError) Error() string {
	return "native Codex cannot preserve fields: " + strings.Join(e.Fields, ", ")
}
func (e *openAIResponsesCompatibilityError) Is(target error) bool {
	return target == ErrOpenAIWSLocalClientPolicy
}
func (e *openAIResponsesCompatibilityError) response() gin.H {
	return gin.H{"error": gin.H{"type": "invalid_request_error", "code": "unsupported_feature", "message": e.Error(), "details": gin.H{"fields": e.Fields}}}
}
func (e *openAIResponsesCompatibilityError) websocketEvent() []byte {
	event := e.response()
	event["type"] = "error"
	event["status"] = http.StatusBadRequest
	payload, _ := json.Marshal(event)
	return payload
}

func (s *OpenAIGatewayService) strictResponsesError(c *gin.Context, account *Account, body []byte, wsMode ...string) *openAIResponsesCompatibilityError {
	enabled := s != nil && s.cfg != nil && s.cfg.Gateway.StrictResponsesCompatibility
	if c != nil && strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Codex2API-Strict-Compatibility")), "true") {
		enabled = true
	}
	if !enabled || account == nil || !account.UsesOpenAICodexProtocol() || account.IsOpenAICloudConfigured() {
		return nil
	}
	passthrough := account.IsOpenAIPassthroughEnabled()
	normalizeMinimal := !passthrough
	if len(wsMode) > 0 && wsMode[0] != OpenAIWSIngressModeHTTPBridge {
		// Native WS uses raw compatibility in both ctx_pool and passthrough,
		// not the HTTP Codex transform that strips generation parameters.
		passthrough = true
		normalizeMinimal = false
	}
	return detectOpenAIResponsesCompatibilityLoss(account, body, passthrough, normalizeMinimal)
}

func detectOpenAIResponsesCompatibilityLoss(account *Account, body []byte, passthrough, normalizeMinimal bool) *openAIResponsesCompatibilityError {
	var request map[string]any
	if decodeOpenAIJSONUseNumber(body, &request) != nil {
		return nil
	}
	normalizeOpenAIResponsesNativeAliases(request)
	fields := map[string]bool{}
	unsupported := openAICodexOAuthUnsupportedFields
	if passthrough {
		unsupported = openAIChatGPTInternalUnsupportedFields
	}
	for _, field := range unsupported {
		if value, ok := request[field]; ok && value != nil {
			fields[field] = true
		}
	}
	if value, ok := request["store"]; ok && value != nil && value != false {
		fields["store"] = true
	}
	reasoning, _ := request["reasoning"].(map[string]any)
	if reasoning["effort"] == "minimal" && normalizeMinimal {
		fields["reasoning.effort"] = true
	}
	// Compare only constraint-bearing nodes using the existing compatibility
	// implementation. Additions such as inferred schema types are visible too.
	original, _ := json.Marshal(request)
	var normalized map[string]any
	_ = decodeOpenAIJSONUseNumber(original, &normalized)
	normalizeOpenAIResponseFormatSchemas(normalized)
	collectOpenAICompatibilityDifferences("text", request["text"], normalized["text"], fields)
	if sanitized, changed, err := sanitizeOpenAIResponsesToolSchemasForPlatform(original, account.Platform); err == nil && changed {
		var toolNormalized map[string]any
		if decodeOpenAIJSONUseNumber(sanitized, &toolNormalized) == nil {
			collectOpenAICompatibilityDifferences("tools", request["tools"], toolNormalized["tools"], fields)
		}
	}

	// Use the same normalizers as forwarding, including dropped malformed tools
	// and image-tool option rewrites; do not maintain a second schema rule list.
	var normalizedTools map[string]any
	_ = decodeOpenAIJSONUseNumber(original, &normalizedTools)
	if !passthrough {
		normalizeCodexTools(normalizedTools)
	}
	normalizeOpenAIResponsesImageGenerationTools(normalizedTools)
	collectOpenAICompatibilityDifferences("tools", request["tools"], normalizedTools["tools"], fields)
	if prompt, exists := request["prompt"]; exists && prompt != nil {
		if _, legacy := prompt.(string); !legacy {
			fields["prompt"] = true
		}
	}

	if next, changed, err := normalizeOpenAIResponsesReasoningMode(original, account.GetMappedModel(gjson.GetBytes(body, "model").String())); err == nil && changed {
		var after map[string]any
		if decodeOpenAIJSONUseNumber(next, &after) == nil {
			for _, field := range []string{"reasoning", "temperature", "top_p"} {
				collectOpenAICompatibilityDifferences(field, request[field], after[field], fields)
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	result := &openAIResponsesCompatibilityError{Fields: make([]string, 0, len(fields))}
	for field := range fields {
		result.Fields = append(result.Fields, field)
	}
	sort.Strings(result.Fields)
	return result
}
func collectOpenAICompatibilityDifferences(path string, before, after any, fields map[string]bool) {
	if reflect.DeepEqual(before, after) {
		return
	}
	b, bok := before.(map[string]any)
	a, aok := after.(map[string]any)
	if bok && aok {
		for key, value := range b {
			collectOpenAICompatibilityDifferences(path+"."+key, value, a[key], fields)
		}
		for key, value := range a {
			if _, exists := b[key]; !exists && value != nil {
				fields[path+"."+key] = true
			}
		}
		return
	}
	bs, bok := before.([]any)
	as, aok := after.([]any)
	if bok && aok && len(bs) == len(as) {
		for i := range bs {
			collectOpenAICompatibilityDifferences(fmt.Sprintf("%s[%d]", path, i), bs[i], as[i], fields)
		}
		return
	}
	fields[path] = true
}

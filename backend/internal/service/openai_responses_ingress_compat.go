package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

func normalizeOpenAIResponsesLegacyIngressWithConfig(cfg *config.Config, body []byte) ([]byte, bool, error) {
	if len(body) == 0 {
		return body, false, nil
	}
	// Native requests can contain large image data URLs. Decode the full object
	// only when a legacy top-level field needs compatibility handling.
	view := parseRawJSONView(body)
	if view.IsObject() && gjson.ValidBytes(body) {
		hasLegacyField := false
		view.ForEach(func(key, _ gjson.Result) bool {
			switch key.Str {
			case "messages", "prompt", "commands", "reasoning_effort", "response_format", "functions", "function_call":
				hasLegacyField = true
			}
			return !hasLegacyField
		})
		if !hasLegacyField {
			return body, false, nil
		}
	}

	var request map[string]any
	if err := decodeOpenAIJSONUseNumber(body, &request); err != nil {
		return body, false, fmt.Errorf("normalize legacy Responses ingress: %w", err)
	}

	changed := false
	messagesValue, hasMessages := request["messages"]
	if hasMessages {
		messages, messagesAreArray := messagesValue.([]any)
		_, hasNativeInput := request["input"]
		if !hasNativeInput && messagesAreArray && len(messages) > 0 {
			legacy, err := convertLegacyResponsesMessages(cfg, body)
			if err != nil {
				return body, false, err
			}

			request["input"] = legacy.input
			applyLegacyResponsesTopLevelFields(request, legacy)
		}
		// messages is not a Responses field. When native input is present but the
		// two histories are ambiguous, retain input and drop only the legacy alias.
		delete(request, "messages")
		changed = true
	}

	changed = normalizeOpenAIResponsesNativeAliases(request) || changed
	changed = normalizeOpenAIResponsesLegacyAliases(request) || changed

	if !changed {
		return body, false, nil
	}
	normalized, err := marshalOpenAIUpstreamJSON(request)
	if err != nil {
		return body, false, fmt.Errorf("serialize legacy Responses ingress: %w", err)
	}
	return restoreOpenAIUpstreamJSONTopLevelOrder(body, normalized), true, nil
}

type convertedLegacyResponsesMessages struct {
	input              any
	instructions       string
	maxOutputTokens    *int
	temperature        *float64
	topP               *float64
	tools              any
	reasoning          any
	toolChoice         any
	serviceTier        string
	hasChatTools       bool
	hasChatToolChoice  bool
	hasReasoningEffort bool
}

func convertLegacyResponsesMessages(cfg *config.Config, body []byte) (convertedLegacyResponsesMessages, error) {
	var converted convertedLegacyResponsesMessages
	var chatRequest apicompat.ChatCompletionsRequest
	if err := decodeOpenAIJSONUseNumber(body, &chatRequest); err != nil {
		return converted, fmt.Errorf("normalize legacy Responses messages: %w", err)
	}
	responsesRequest, err := chatCompletionsToResponses(cfg, &chatRequest)
	if err != nil {
		return converted, fmt.Errorf("normalize legacy Responses messages: %w", err)
	}
	if len(responsesRequest.Input) == 0 {
		return converted, fmt.Errorf("normalize legacy Responses messages: empty input")
	}
	if err := decodeOpenAIJSONUseNumber(responsesRequest.Input, &converted.input); err != nil {
		return converted, fmt.Errorf("decode normalized legacy Responses input: %w", err)
	}

	converted.instructions = strings.TrimSpace(responsesRequest.Instructions)
	converted.maxOutputTokens = responsesRequest.MaxOutputTokens
	converted.temperature = responsesRequest.Temperature
	converted.topP = responsesRequest.TopP
	converted.serviceTier = strings.TrimSpace(responsesRequest.ServiceTier)
	converted.hasReasoningEffort = strings.TrimSpace(chatRequest.ReasoningEffort) != ""
	if responsesRequest.Reasoning != nil {
		converted.reasoning = responsesRequest.Reasoning
	}

	var rawRequest map[string]any
	if err := decodeOpenAIJSONUseNumber(body, &rawRequest); err != nil {
		return converted, fmt.Errorf("inspect legacy Responses fields: %w", err)
	}
	converted.hasChatTools = hasChatStyleTools(rawRequest)
	if converted.hasChatTools && len(responsesRequest.Tools) > 0 {
		converted.tools = responsesRequest.Tools
	}
	converted.hasChatToolChoice = hasChatStyleToolChoice(rawRequest)
	if converted.hasChatToolChoice && len(responsesRequest.ToolChoice) > 0 {
		choice, err := normalizeLegacyResponsesToolChoice(responsesRequest.ToolChoice)
		if err != nil {
			return converted, err
		}
		converted.toolChoice = choice
	}
	return converted, nil
}

func hasChatStyleTools(request map[string]any) bool {
	tools, ok := request["tools"].([]any)
	if !ok {
		return false
	}
	for _, rawTool := range tools {
		tool, _ := rawTool.(map[string]any)
		if _, ok := tool["function"].(map[string]any); ok {
			return true
		}
	}
	return false
}

func hasChatStyleToolChoice(request map[string]any) bool {
	choice, _ := request["tool_choice"].(map[string]any)
	_, ok := choice["function"].(map[string]any)
	return ok
}

func normalizeLegacyResponsesToolChoice(raw json.RawMessage) (any, error) {
	var choice any
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil, fmt.Errorf("normalize legacy Responses tool_choice: %w", err)
	}
	object, ok := choice.(map[string]any)
	if !ok || strings.TrimSpace(firstNonEmptyString(object["type"])) != "function" {
		return choice, nil
	}
	function, ok := object["function"].(map[string]any)
	if !ok {
		return choice, nil
	}
	name := strings.TrimSpace(firstNonEmptyString(function["name"]))
	if name == "" {
		return choice, nil
	}
	return map[string]any{"type": "function", "name": name}, nil
}

func applyLegacyResponsesTopLevelFields(request map[string]any, legacy convertedLegacyResponsesMessages) {
	if legacy.instructions != "" && strings.TrimSpace(firstNonEmptyString(request["instructions"])) == "" {
		request["instructions"] = legacy.instructions
	}
	if _, native := request["max_output_tokens"]; !native && legacy.maxOutputTokens != nil {
		request["max_output_tokens"] = *legacy.maxOutputTokens
	}
	if legacy.temperature != nil {
		request["temperature"] = *legacy.temperature
	}
	if legacy.topP != nil {
		request["top_p"] = *legacy.topP
	}
	if legacy.hasChatTools && legacy.tools != nil {
		request["tools"] = legacy.tools
	}
	if _, native := request["reasoning"]; !native && legacy.hasReasoningEffort && legacy.reasoning != nil {
		request["reasoning"] = legacy.reasoning
	}
	if legacy.hasChatToolChoice && legacy.toolChoice != nil {
		request["tool_choice"] = legacy.toolChoice
	}
	if legacy.serviceTier != "" {
		request["service_tier"] = legacy.serviceTier
	}
	for _, key := range []string{"max_tokens", "max_completion_tokens"} {
		delete(request, key)
	}
}

// Aliases fill only absent native fields. Keep explicit native null/invalid
// values for provider validation instead of silently changing their meaning.
func normalizeOpenAIResponsesNativeAliases(request map[string]any) bool {
	changed := false
	if effort, alias := request["reasoning_effort"]; alias {
		if raw, present := request["reasoning"]; !present {
			request["reasoning"] = map[string]any{"effort": effort}
		} else if reasoning, ok := raw.(map[string]any); ok {
			if _, native := reasoning["effort"]; !native {
				reasoning["effort"] = effort
			}
		}
		delete(request, "reasoning_effort")
		changed = true
	}
	if format, alias := request["response_format"]; alias {
		text, present := request["text"]
		object, valid := text.(map[string]any)
		if !present {
			object = map[string]any{}
			request["text"] = object
			valid = true
		}
		if valid {
			if _, native := object["format"]; !native {
				if f, ok := format.(map[string]any); ok && f["type"] == "json_schema" {
					if schema, ok := f["json_schema"].(map[string]any); ok {
						nativeFormat := make(map[string]any, len(schema)+1)
						for k, v := range schema {
							nativeFormat[k] = v
						}
						nativeFormat["type"] = "json_schema"
						format = nativeFormat
					}
				}
				object["format"] = format
			}
		}
		delete(request, "response_format")
		changed = true
	}
	if functions, alias := request["functions"]; alias {
		if _, native := request["tools"]; !native {
			if list, ok := functions.([]any); ok {
				tools := make([]any, 0, len(list))
				for _, function := range list {
					if definition, ok := function.(map[string]any); ok {
						tool := make(map[string]any, len(definition)+1)
						for key, value := range definition {
							tool[key] = value
						}
						tool["type"] = "function"
						tools = append(tools, tool)
					}
				}
				request["tools"] = tools
			}
		}
		delete(request, "functions")
		changed = true
	}
	if choice, alias := request["function_call"]; alias {
		if _, native := request["tool_choice"]; !native {
			if name, ok := choice.(string); ok {
				request["tool_choice"] = name
			} else if object, ok := choice.(map[string]any); ok {
				if name, ok := object["name"].(string); ok && strings.TrimSpace(name) != "" {
					request["tool_choice"] = map[string]any{"type": "function", "name": name}
				}
			}
		}
		delete(request, "function_call")
		changed = true
	}

	return changed
}

// Standard relay WS keeps its existing prompt/messages contract; only the
// shared native aliases are normalized, without decoding untouched large bodies.
func normalizeOpenAIResponsesNativeAliasesBody(body []byte) ([]byte, bool, error) {
	for _, key := range []string{"reasoning_effort", "response_format", "functions", "function_call"} {
		if !gjson.GetBytes(body, key).Exists() {
			continue
		}
		var request map[string]any
		if err := decodeOpenAIJSONUseNumber(body, &request); err != nil {
			return body, false, err
		}
		if !normalizeOpenAIResponsesNativeAliases(request) {
			return body, false, nil
		}
		normalized, err := marshalOpenAIUpstreamJSON(request)
		if err != nil {
			return body, false, err
		}
		return restoreOpenAIUpstreamJSONTopLevelOrder(body, normalized), true, nil
	}
	return body, false, nil
}

// normalizeOpenAIResponsesLegacyMap adapts the decoded envelope to the existing
// apicompat message converter. Cloud needs its map form to retain call IDs and
// avoid re-decoding the entire request during routing and owner dispatch.
func normalizeOpenAIResponsesLegacyMap(body map[string]any) error {
	if raw, exists := body["messages"]; exists {
		if body["input"] == nil {
			messages, ok := raw.([]any)
			if !ok || len(messages) == 0 {
				return fmt.Errorf("legacy messages must be a nonempty compatible array")
			}
			input, err := apicompat.ChatMessagesMapToResponsesInput(messages)
			if err != nil {
				return fmt.Errorf("legacy messages must be a nonempty compatible array")
			}
			body["input"] = input
		}
		delete(body, "messages")
	}
	normalizeOpenAIResponsesLegacyAliases(body)
	if body["reasoning"] == nil {
		if effort, ok := body["reasoning_effort"].(string); ok && effort != "" {
			body["reasoning"] = map[string]any{"effort": effort, "summary": "auto"}
		}
	}
	delete(body, "reasoning_effort")
	if body["tools"] == nil {
		if functions, ok := body["functions"].([]any); ok {
			tools := make([]any, 0, len(functions))
			for _, raw := range functions {
				f, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("legacy function declaration must be an object")
				}
				tool := make(map[string]any, len(f)+1)
				for key, value := range f {
					tool[key] = value
				}
				tool["type"] = "function"
				tools = append(tools, tool)
			}
			body["tools"] = tools
		}
	}
	delete(body, "functions")
	if body["tool_choice"] == nil {
		if choice, exists := body["function_call"]; exists {
			body["tool_choice"] = choice
		}
	}
	delete(body, "function_call")
	if choice, ok := body["tool_choice"].(map[string]any); ok {
		if f, ok := choice["function"].(map[string]any); ok {
			body["tool_choice"] = map[string]any{"type": "function", "name": f["name"]}
		}
	}
	return nil
}
func normalizeOpenAIResponsesLegacyAliases(request map[string]any) bool {
	changed := false
	if prompt, hasPrompt := request["prompt"]; hasPrompt {
		if text, ok := prompt.(string); ok {
			if _, present := request["input"]; !present {
				request["input"] = text
			}
			delete(request, "prompt")
			changed = true
		}
	}
	if _, exists := request["commands"]; exists {
		delete(request, "commands")
		changed = true
	}
	return changed
}

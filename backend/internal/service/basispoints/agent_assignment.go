package basispoints

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

func encryptedAssignmentPath(item object, index int) string {
	if value, exists := item["encrypted_content"]; exists && value != nil && value != "" {
		return fmt.Sprintf("input[%d].encrypted_content", index)
	}
	field := "content"
	if openai.IsNamedStandaloneToolOutput(item) {
		field = "output"
	}
	parts, _ := item[field].([]any)
	for partIndex, raw := range parts {
		part, _ := raw.(object)
		if text(part["type"]) == "encrypted_content" {
			return fmt.Sprintf("input[%d].%s[%d]", index, field, partIndex)
		}
	}
	return ""
}

// A trailing assignment is the current task. Historical omission is not a
// substitute for its unreadable payload, even if a readable header is present.
func validateNewAgentMessage(input any) error {
	items, _ := input.([]any)
	for index := len(items) - 1; index >= 0; index-- {
		item, _ := items[index].(object)
		switch text(item["type"]) {
		case "additional_tools", "tool_search_call", "tool_search_output", "compaction_trigger":
			continue
		}
		if text(item["type"]) == "agent_message" || openai.IsNamedStandaloneToolOutput(item) {
			if path := encryptedAssignmentPath(item, index); path != "" {
				return &ContentValidationError{Path: path, ContentType: "encrypted_content", message: "basispoints cannot read a new encrypted agent assignment; refresh the model catalog, remove any multi-agent v2 override, and resend the original plaintext task in a new conversation; history omission and compaction cannot recover this task"}
			}
		}
		break
	}
	return nil
}

// ValidateNewAgentMessage must run before history omission and image compaction
// can consume or transform the original task.
func ValidateNewAgentMessage(raw []byte) error {
	var source object
	if decode(raw, &source) != nil || source == nil {
		return fmt.Errorf("invalid Basispoints request JSON")
	}
	return validateNewAgentMessage(source["input"])
}

// Remove only the display annotation for plaintext collaboration. Keep the
// original schema in tool.Parameters/Catalog for validation and cache identity.
func plaintextPromptParameters(name, namespace string, parameters object) object {
	if namespace != "collaboration" || (name != "spawn_agent" && name != "send_message" && name != "followup_task") || parameters == nil {
		return parameters
	}
	properties, _ := parameters["properties"].(object)
	message, _ := properties["message"].(object)
	if message == nil {
		return parameters
	}
	result := make(object, len(parameters))
	for k, v := range parameters {
		result[k] = v
	}
	props := make(object, len(properties))
	for k, v := range properties {
		props[k] = v
	}
	schema := make(object, len(message))
	for k, v := range message {
		if k != "encrypted" {
			schema[k] = v
		}
	}
	props["message"] = schema
	result["properties"] = props
	return result
}

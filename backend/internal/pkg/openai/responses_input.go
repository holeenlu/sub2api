package openai

import (
	"fmt"
	"maps"
	"strings"
)

// IsNamedStandaloneToolOutput recognizes the existing Codex external-input form.
// It describes supplied context, not proof that a tool call was executed. Native
// forwarding and protocol adapters share this classification; adapters still
// validate field types and supported content before lowering it.
func IsNamedStandaloneToolOutput(item map[string]any) bool {
	kind, _ := item["type"].(string)
	id, _ := item["call_id"].(string)
	name, _ := item["name"].(string)
	return strings.TrimSpace(kind) == "function_call_output" && strings.TrimSpace(id) == "" && strings.TrimSpace(name) != ""
}

// StandaloneToolTextError identifies an invalid textual value without retaining
// its content. Index -1 denotes the output container itself.
type StandaloneToolTextError struct {
	Index int
	Field string
}

func (e *StandaloneToolTextError) Error() string {
	if e.Index < 0 {
		return "standalone tool output must be text or a content array"
	}
	return fmt.Sprintf("standalone tool output[%d].%s must be a string", e.Index, e.Field)
}

// NormalizeStandaloneToolOutputContent lowers textual variants in externally
// supplied tool context to user input_text. Callers must first classify the
// item with IsNamedStandaloneToolOutput and retain their own content validation.
// It never creates a call or execution identity. Nontext parts and extra fields
// remain available to the target's policy; only copied maps are changed.
func NormalizeStandaloneToolOutputContent(value any) ([]any, error) {
	if text, ok := value.(string); ok {
		return []any{map[string]any{"type": "input_text", "text": text}}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, &StandaloneToolTextError{Index: -1}
	}
	normalized := make([]any, len(items))
	for index, raw := range items {
		part, ok := raw.(map[string]any)
		if !ok {
			normalized[index] = raw // The protocol's content validator owns this case.
			continue
		}
		copy := maps.Clone(part)
		normalized[index] = copy
		field := "text"
		switch part["type"] {
		case "input_text", "text", "output_text":
		case "refusal":
			field = "refusal"
		default:
			continue
		}
		text, ok := part[field].(string)
		if !ok {
			return nil, &StandaloneToolTextError{Index: index, Field: field}
		}
		copy["type"], copy["text"] = "input_text", text
		if field == "refusal" {
			delete(copy, field)
		}
	}
	return normalized, nil
}

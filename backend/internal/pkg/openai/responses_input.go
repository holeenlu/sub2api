package openai

import "strings"

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

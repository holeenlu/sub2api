package claude

import (
	_ "embed"
	"encoding/json"
	"maps"
)

// One catalog supplies both the editor's presets and the wire renderer.
//
//go:embed prompts/catalog.json
var promptTemplateJSON []byte

var promptTemplates = func() struct {
	Variables    map[string]string          `json:"variables"`
	Presets      map[string]json.RawMessage `json:"presets"`
	Continuation string                     `json:"continuation"`
	Environment  string                     `json:"official_environment"`
} {
	var catalog struct {
		Variables    map[string]string          `json:"variables"`
		Presets      map[string]json.RawMessage `json:"presets"`
		Continuation string                     `json:"continuation"`
		Environment  string                     `json:"official_environment"`
	}
	if err := json.Unmarshal(promptTemplateJSON, &catalog); err != nil {
		panic(err)
	}
	return catalog
}()

func PromptTemplateVariables() map[string]string { return maps.Clone(promptTemplates.Variables) }
func IsPromptTemplateVariable(name string) bool  { _, ok := promptTemplates.Variables[name]; return ok }
func PromptTemplatePreset(name string) json.RawMessage {
	return append(json.RawMessage(nil), promptTemplates.Presets[name]...)
}
func PromptTemplateContinuation() string { return promptTemplates.Continuation }
func PromptTemplateEnvironment() string  { return promptTemplates.Environment }

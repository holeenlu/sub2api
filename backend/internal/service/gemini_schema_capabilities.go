package service

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// The local AI Studio builder always uses v1beta; Vertex's v1 API also
// supports JSON Schema. Code Assist / Antigravity's private v1internal dialect
// has no such public contract and retains the existing typed-schema adapter.
func geminiSupportsJSONSchema(account *Account) bool {
	if account == nil || account.Platform == PlatformAntigravity {
		return false
	}
	if account.Type == AccountTypeOAuth {
		return strings.TrimSpace(account.GetCredential("project_id")) == ""
	}
	return account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount
}

func convertClaudeForGeminiAccount(c *gin.Context, account *Account, body []byte, model string) ([]byte, error) {
	var diagnostics apicompat.ConversionDiagnostics
	result, err := convertClaudeMessagesToGeminiWithSchema(body, geminiSupportsJSONSchema(account), &diagnostics)
	recordConversionDiagnostics(c, "anthropic", "gemini", model, diagnostics)
	return result, err
}

func schemaDepthAllowed(value any, depth int) bool {
	if depth > 64 {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if !schemaDepthAllowed(child, depth+1) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !schemaDepthAllowed(child, depth+1) {
				return false
			}
		}
	}
	return true
}

func applyGeminiOutputFormat(req, generation map[string]any, rawSchema bool, d *apicompat.ConversionDiagnostics) error {
	config, _ := req["output_config"].(map[string]any)
	format, _ := config["format"].(map[string]any)
	if format == nil {
		format, _ = req["output_format"].(map[string]any)
	}
	if format == nil {
		return nil
	}
	kind, _ := format["type"].(string)
	if kind == "text" || kind == "" {
		return nil
	}
	if kind != "json_object" && kind != "json_schema" {
		return fmt.Errorf("unsupported output format for Gemini")
	}
	generation["responseMimeType"] = "application/json"
	if kind == "json_object" {
		return nil
	}
	schema, ok := format["schema"].(map[string]any)
	if !ok {
		return fmt.Errorf("output format requires an object JSON Schema")
	}
	if !schemaDepthAllowed(schema, 0) {
		return fmt.Errorf("output schema exceeds maximum depth of 64")
	}
	if rawSchema {
		generation["responseJsonSchema"] = schema
	} else {
		d.Add("schema_legacy_conversion", "output_config.format.schema")
		generation["responseSchema"] = cleanToolSchema(schema)
	}
	return nil
}

// Marshal/unmarshal makes the raw schema independent from the caller's maps.
// The conversion's input body is immutable across account attempts.
func validateGeminiToolSchemaDepth(tools any) error {
	arr, _ := tools.([]any)
	for _, item := range arr {
		tool, _ := item.(map[string]any)
		schema := tool["input_schema"]
		if custom, ok := tool["custom"].(map[string]any); ok {
			schema = custom["input_schema"]
		}
		if !schemaDepthAllowed(schema, 0) {
			return fmt.Errorf("tool schema exceeds maximum depth of 64")
		}
	}
	return nil
}

func rejectAntigravityClaudeOutputFormat(c *gin.Context, body []byte, model string) error {
	if gjson.GetBytes(body, "output_config.format").Exists() || gjson.GetBytes(body, "output_format").Exists() {
		var d apicompat.ConversionDiagnostics
		d.Add("structured_output_unsupported", "output_config.format")
		recordConversionDiagnostics(c, "anthropic", "antigravity_claude", model, d)
		return fmt.Errorf("structured output is not supported by the Antigravity Claude transport")
	}
	return nil
}
func recordAntigravitySchemaConversion(c *gin.Context, body []byte, model string) {
	var d apicompat.ConversionDiagnostics
	if len(gjson.GetBytes(body, "tools").Array()) > 0 {
		d.Add("schema_legacy_conversion", "tools[].input_schema")
	}
	if gjson.GetBytes(body, "output_config.format.schema").Exists() {
		d.Add("schema_legacy_conversion", "output_config.format.schema")
	}
	recordConversionDiagnostics(c, "anthropic", "antigravity", model, d)
}

package apicompat

import (
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

func preserveResponsesOutputFormat(req *ResponsesRequest, out *AnthropicRequest, native bool) error {
	if req.Text == nil || len(req.Text.Format) == 0 {
		return nil
	}
	var format struct {
		Type   string
		Schema json.RawMessage
	}
	if json.Unmarshal(req.Text.Format, &format) != nil {
		return fmt.Errorf("invalid text.format")
	}
	if format.Type == "text" || format.Type == "" {
		return nil
	}
	if format.Type != "json_schema" && format.Type != "json_object" {
		return fmt.Errorf("unsupported text.format.type")
	}
	if native && (format.Type == "json_object" || !claude.SupportsStructuredOutput(req.Model)) {
		out.Diagnostics.Add("structured_output_unsupported", "text.format")
		return fmt.Errorf("target Messages model requires a supported json_schema output format")
	}
	if format.Type == "json_schema" && (len(format.Schema) == 0 || string(format.Schema) == "null") {
		return fmt.Errorf("text.format.schema is required")
	}
	// name/description/strict are OpenAI envelope metadata, not Anthropic fields.
	mapped, err := json.Marshal(struct {
		Type   string          `json:"type"`
		Schema json.RawMessage `json:"schema,omitempty"`
	}{format.Type, format.Schema})
	if err != nil {
		return err
	}
	if out.OutputConfig == nil {
		out.OutputConfig = &AnthropicOutputConfig{}
	}
	out.OutputConfig.Format = mapped
	return nil
}

func omitCompletedResponsesTail(input json.RawMessage, d *ConversionDiagnostics) json.RawMessage {
	var items []json.RawMessage
	if json.Unmarshal(input, &items) != nil || len(items) < 2 {
		return input
	}
	last := len(items) - 1
	for last > 0 {
		var item ResponsesInputItem
		if json.Unmarshal(items[last], &item) != nil || item.Role != "assistant" || (item.Type != "" && item.Type != "message") || item.Status != "completed" {
			break
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(item.Content, &parts) != nil || len(parts) == 0 {
			break
		}
		pure := true
		var envelope map[string]json.RawMessage
		_ = json.Unmarshal(items[last], &envelope)
		for key := range envelope {
			switch key {
			case "type", "id", "role", "status", "content":
			default:
				pure = false
			}
		}
		for _, part := range parts {
			var kind string
			if json.Unmarshal(part["type"], &kind) != nil || kind != "output_text" {
				pure = false
				break
			}
			// Unknown extensions, annotations and signatures are not safe to discard.
			for key, value := range part {
				if key != "type" && key != "text" && (key != "annotations" || string(value) != "[]") {
					pure = false
				}
			}
		}
		if !pure {
			break
		}
		last--
	}
	if last == len(items)-1 {
		return input
	}
	// Do not expose another assistant / unfinished tool as a new prefill.
	var preceding ResponsesInputItem
	if json.Unmarshal(items[last], &preceding) != nil || (preceding.Role != "user" && preceding.Type != "function_call_output") {
		return input
	}
	trimmed, err := json.Marshal(items[:last+1])
	if err != nil {
		return input
	}
	d.Add("trailing_assistant_omitted", "input[-1]")
	return trimmed
}

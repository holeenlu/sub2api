package basispoints

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestToolSchemaDiagnosticsKeepOriginalLocationAndPrivacy(t *testing.T) {
	for _, replay := range []bool{false, true} {
		for _, tc := range []struct {
			schema object
			reason string
		}{
			{object{"properties": object{"x": object{"$ref": "https://private.invalid/?token=SECRET"}}}, "external_reference_blocked"},
			{object{"$ref": "#/SECRET_MISSING"}, "local_reference_not_found"},
			{object{"type": "SECRET_INVALID_TYPE"}, "invalid_schema"},
			{object{"description": strings.Repeat("x", 1<<20)}, "resource limits"},
		} {
			source := testSource()
			tools := []any{object{"type": "namespace", "name": "catalog", "tools": []any{object{"type": "function", "name": "inspect", "input_schema": tc.schema}}}}
			source["tools"] = tools
			prefix := "tools[0].tools[0].input_schema"
			if replay {
				delete(source, "tools")
				source["input"] = []any{message("user", "inspect"), object{"type": "tool_search_output", "status": "completed", "tools": tools}}
				prefix = "input[1].tools[0].tools[0].input_schema"
			}
			raw, err := json.Marshal(source)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = Prepare(raw, "scope", nil)
			var diagnostic *ContentValidationError
			if !errors.As(err, &diagnostic) || !strings.HasPrefix(diagnostic.Path, prefix) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("missing schema location/reason: %v", err)
			}
			if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "private.invalid") || len(err.Error()) > 2048 {
				t.Fatalf("unsafe schema diagnostic: %v", err)
			}
		}
	}
}

func TestContentErrorsIdentifyPathAndKnownType(t *testing.T) {
	for _, field := range []string{"content", "function_call_output", "custom_tool_call_output"} {
		for _, kind := range []string{"input_file", "input_audio", "image_url", "image", "encrypted_content"} {
			t.Run(field+"/"+kind, func(t *testing.T) {
				source := testSource()
				parts := []any{object{"type": "input_text", "text": "private-text"}, object{"type": kind, "data": "private-payload", "url": "https://private-url.example/image"}}
				input := []any{message("user", "inspect")}
				path := "input[1].content[1]"
				if field != "content" {
					call := object{"type": "function_call", "call_id": "call_diagnostic", "name": "inspect", "arguments": `{}`}
					if field == "custom_tool_call_output" {
						call = object{"type": "custom_tool_call", "call_id": "call_diagnostic", "name": "inspect", "input": "diagnostic"}
					}
					input = append(input, call, object{"type": field, "call_id": "call_diagnostic", "output": parts})
					path = "input[2].output[1]"
				} else {
					input = append(input, object{"role": "user", "content": parts})
				}
				source["input"] = input
				raw, err := json.Marshal(source)
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = Prepare(raw, "scope", nil)
				if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "type="+kind) || strings.Contains(err.Error(), "private-") {
					t.Fatalf("content error lacks safe structural diagnostics: %v", err)
				}
			})
		}
	}
}

func TestContentDiagnosticsDoNotEchoUntrustedTypeValues(t *testing.T) {
	for _, tc := range []struct {
		part any
		kind string
	}{
		{nil, "non_object"},
		{"private-payload", "non_object"},
		{object{}, "missing"},
		{object{"type": "private-type"}, "unknown"},
		{object{"type": "input_file\nprivate-log-injection"}, "unknown"},
		{object{"type": object{"private-key": "private-value"}}, "non_string"},
	} {
		source := testSource()
		source["input"] = []any{object{"role": "user", "content": []any{tc.part}}}
		raw, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = Prepare(raw, "scope", nil)
		if err == nil || !strings.Contains(err.Error(), "input[0].content[0]") || !strings.Contains(err.Error(), "type="+tc.kind) || strings.Contains(err.Error(), "private-") {
			t.Fatalf("missing path or untrusted data echoed: %v", err)
		}
	}
}

func TestImageValidationErrorIncludesPathWithoutURL(t *testing.T) {
	source := testSource()
	source["input"] = []any{message("user", "diagnostic"), object{"role": "user", "content": []any{object{"type": "input_image", "image_url": "http://private-url.example/image?private-token"}}}}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Prepare(raw, "scope", nil)
	if err == nil || !strings.Contains(err.Error(), "path=input[1].content[0]") || !strings.Contains(err.Error(), "HTTPS") || strings.Contains(err.Error(), "private-") {
		t.Fatalf("image error lacks a safe path: %v", err)
	}
}

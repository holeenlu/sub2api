package basispoints

import (
	"strings"
	"testing"
)

func TestCatalogExamplesFollowDeclaredTools(t *testing.T) {
	source := testSource()
	source["tools"] = []any{
		object{"type": "function", "name": "client.run_code", "parameters": object{
			"type": "object", "required": []any{"code", "description"},
			"properties": object{"code": object{"type": "string"}, "description": object{"type": "string"}},
		}},
		object{"type": "custom", "name": "client.apply_patch"},
		object{"type": "function", "name": "ordinary", "parameters": object{"type": "object", "properties": object{"value": object{"type": "string"}}}},
	}
	body, _ := mustPrepare(t, source, "examples", nil)
	items := mustTestValue[[]any](t, body["input"])
	protocolMessage := mustTestValue[object](t, items[1])
	protocol := text(mustTestValue[object](t, mustTestValue[[]any](t, protocolMessage["content"])[0])["text"])
	if !strings.Contains(protocol, "Example outer arguments for client.run_code (function)") ||
		!strings.Contains(protocol, functionCodeTransportPrefix+"client.run_code") {
		t.Fatal("missing schema-valid example for the declared code transport")
	}
	if !strings.Contains(protocol, "Example outer arguments for client.apply_patch (custom)") ||
		!strings.Contains(protocol, customTransportPrefix+"client.apply_patch") {
		t.Fatal("missing example for the declared custom transport")
	}
	if strings.Contains(protocol, "functions.exec") || strings.Contains(protocol, "functions.apply_patch") {
		t.Fatal("protocol taught a hardcoded tool that was not declared")
	}
}

func TestCatalogExamplesSkipInvalidRequiredSchema(t *testing.T) {
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "exec_command", "parameters": object{
		"type": "object", "required": []any{"cmd", "must_be_enum"},
		"properties": object{
			"cmd":          object{"type": "string"},
			"must_be_enum": object{"type": "string", "pattern": "^only-this-value$"},
		},
	}}}
	body, _ := mustPrepare(t, source, "invalid-example", nil)
	items := mustTestValue[[]any](t, body["input"])
	protocolMessage := mustTestValue[object](t, items[1])
	protocol := text(mustTestValue[object](t, mustTestValue[[]any](t, protocolMessage["content"])[0])["text"])
	if strings.Contains(protocol, "Example outer arguments for exec_command") {
		t.Fatal("an invalid schema example was advertised")
	}
}

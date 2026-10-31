package basispoints

import (
	"errors"
	"fmt"
	"testing"
)

func TestPrepareClientToolChoicePolicy(t *testing.T) {
	const clients = `[{"type":"function","name":"inspect","parameters":{"type":"object"}},{"type":"custom","name":"exec"}]`
	cases := []struct {
		name, choice, tools, input string
		supported                  bool
	}{
		{name: "required", choice: `"required"`},
		{name: "function", choice: `{"type":"function","name":"inspect"}`},
		{name: "custom", choice: `{"type":"custom","name":"exec"}`},
		{name: "allowed auto", choice: `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"inspect"}]}`},
		{name: "allowed required", choice: `{"type":"allowed_tools","mode":"required","tools":[{"type":"custom","name":"exec"}]}`},
		{name: "auto remains BPS", choice: `"auto"`, supported: true},
		{name: "none remains BPS", choice: `"none"`, supported: true},
		{name: "null remains BPS", choice: `null`, supported: true},
		{name: "unknown string", choice: `"any"`},
		{name: "numeric choice", choice: `7`},
		{name: "missing name", choice: `{"type":"function"}`},
		{name: "wrong name type", choice: `{"type":"function","name":7}`},
		{name: "unknown name", choice: `{"type":"function","name":"missing"}`},
		{name: "wrong kind", choice: `{"type":"custom","name":"inspect"}`},
		{name: "name whitespace", choice: `{"type":"function","name":" inspect"}`},
		{name: "extra choice field", choice: `{"type":"function","name":"inspect","extra":true}`},
		{name: "legacy choice must be normalized at ingress", choice: `{"type":"function","function":{"name":"inspect"}}`},
		{name: "allowed missing mode", choice: `{"type":"allowed_tools","tools":[{"type":"function","name":"inspect"}]}`},
		{name: "allowed bad mode", choice: `{"type":"allowed_tools","mode":"none","tools":[{"type":"function","name":"inspect"}]}`},
		{name: "allowed empty", choice: `{"type":"allowed_tools","mode":"required","tools":[]}`},
		{name: "allowed bad tools shape", choice: `{"type":"allowed_tools","mode":"auto","tools":{}}`},
		{name: "allowed unknown name", choice: `{"type":"allowed_tools","mode":"required","tools":[{"type":"function","name":"missing"}]}`},
		{name: "allowed hosted reference", choice: `{"type":"allowed_tools","mode":"required","tools":[{"type":"web_search"}]}`},
		{name: "allowed extra field", choice: `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"inspect"}],"extra":true}`},
		{name: "allowed extra reference field", choice: `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"inspect","extra":true}]}`},
		{name: "inherited-only catalog", choice: `"required"`, tools: `null`},
		{name: "empty catalog", choice: `"required"`, tools: `[]`},
		{name: "bad declaration shape", choice: `"required"`, tools: `[7]`},
		{name: "bad schema", choice: `"required"`, tools: `[{"type":"function","name":"inspect","parameters":{"type":"invalid"}}]`},
		{name: "conflicting definitions", choice: `"required"`, tools: `[{"type":"function","name":"inspect"},{"type":"custom","name":"inspect"}]`},
		{name: "declaration whitespace", choice: `"required"`, tools: `[{"type":"function","name":" inspect"}]`},
		{name: "additional tools", choice: `"required"`, input: `[{"type":"additional_tools","tools":[]}]`},
		{name: "tool search call", choice: `"required"`, input: `[{"type":"tool_search_call","call_id":"search"}]`},
		{name: "tool search output", choice: `"required"`, input: `[{"type":"tool_search_output","tools":[]}]`},
		{name: "native hosted history injection", choice: `"required"`, input: `[{"type":"web_search_call","id":"search"}]`},
	}
	for _, kind := range []string{"web_search", "web_search_preview", "web_search_preview_2025_03_11", "web_search_2025_08_26", "tool_search", "image_generation", "file_search", "code_interpreter", "computer", "computer_use_preview", "mcp"} {
		cases = append(cases, struct {
			name, choice, tools, input string
			supported                  bool
		}{name: "mixed hosted " + kind, choice: `"required"`, tools: fmt.Sprintf(`[{"type":"function","name":"inspect"},{"type":%q}]`, kind)})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tools == "" {
				tc.tools = clients
			}
			if tc.input == "" {
				tc.input = `"hello"`
			}
			body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","tools":%s,"tool_choice":%s,"input":%s}`, tc.tools, tc.choice, tc.input))
			before := string(body)
			prepared, bridge, err := Prepare(body, "", nil)
			if tc.supported {
				if err != nil || len(prepared) == 0 || bridge == nil {
					t.Fatalf("supported choice failed: %v", err)
				}
			} else if !errors.Is(err, ErrUnsupportedToolChoice) || prepared != nil || bridge != nil {
				t.Fatalf("unsupported choice must fail without a prepared request: %v", err)
			}
			if string(body) != before {
				t.Fatal("preparation changed the client's request")
			}
		})
	}
}

func TestPrepareUnsupportedToolChoiceUsesCapabilityError(t *testing.T) {
	_, _, err := Prepare([]byte(`{"model":"gpt-6-astra","input":"hello","tool_choice":"required"}`), "", nil)
	if !errors.Is(err, ErrUnsupportedToolChoice) || err.Error() != "basispoints supports tool_choice auto or none only" {
		t.Fatalf("unexpected tool-choice error: %v", err)
	}
}

func TestUnsupportedHostedToolsReason(t *testing.T) {
	cases := []struct {
		body, want string
	}{
		{`{"tools":[{"type":"image_generation"}]}`, "image_generation"},
		{`{"tools":[{"type":"web_search","external_web_access":true}]}`, "web_search"},
		{`{"tools":[{"type":"function","name":"exec"}]}`, ""},
		{`{"tools":[{"type":"web_search","external_web_access":true}],"tool_choice":"none"}`, ""},
		{`{"tool_choice":{"type":"web_search"}}`, "tool_choice"},
		{`{"tool_choice":{"type":"image_generation"}}`, "tool_choice"},
		{`{"tool_choice":{"type":"function","name":"web_search_client"}}`, ""},
		{`{"tool_choice":{"type":"custom","name":"image_generation_client"}}`, ""},
	}
	for _, tc := range cases {
		if got := UnsupportedHostedToolsReason([]byte(tc.body)); got != tc.want {
			t.Errorf("reason=%q want %q for %s", got, tc.want, tc.body)
		}
	}
}

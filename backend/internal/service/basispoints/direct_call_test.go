package basispoints

import (
	"reflect"
	"strings"
	"testing"
)

// The model sometimes calls a declared client tool by its own name instead of
// wrapping it in run_officejs. The bridge recovers this into the client tool call
// rather than failing the whole response, and never executes anything.
func TestDirectFunctionCallRecoveredIntoClientTool(t *testing.T) {
	cache := new(ReplayCache)
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "shell", "parameters": object{"type": "object"}}}
	_, bridge := mustPrepare(t, source, "scope", cache)
	for _, name := range []string{"shell", "functions.shell"} {
		native := object{"type": "function_call", "id": "fc_" + name, "call_id": "call_" + name, "name": name, "arguments": `{"command":["ls","-la"]}`}
		call, err := bridge.translateCall(native)
		if err != nil {
			t.Fatalf("%s: direct call not recovered: %v", name, err)
		}
		if call["type"] != "function_call" || call["name"] != "shell" || call["call_id"] != "call_"+name {
			t.Fatalf("%s: wrong client tool item: %+v", name, call)
		}
		var args object
		if decode([]byte(text(call["arguments"])), &args) != nil {
			t.Fatalf("%s: arguments not valid JSON object: %v", name, call["arguments"])
		}
		if cmd, _ := args["command"].([]any); len(cmd) != 2 {
			t.Fatalf("%s: arguments lost: %+v", name, args)
		}
	}
}

// The same declared target must resolve identically with or without the BPS
// wrapper. A host display prefix does not select a different client tool.
func TestWrappedCatalogCallAcceptsHostDisplayPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, kind, marker, field, value string
	}{
		{"shell", "function", "", "cmd", "pwd"},
		{"exec", "custom", customTransportPrefix, "input", "text(42);"},
		{"js", "function", functionCodeTransportPrefix, "code", "text(42);"},
		{"exec_command", "function", functionCmdTransportPrefix, "cmd", "printf 'hello'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := testSource()
			source["tools"] = []any{object{"type": tc.kind, "name": tc.name,
				"parameters": object{"type": "object", "properties": object{tc.field: object{"type": "string"}}}}}
			_, bridge := mustPrepare(t, source, "", nil)
			native := nativeCall(object{"name": "functions." + tc.name, "arguments": object{tc.field: tc.value}})
			if tc.marker != "" {
				native["arguments"] = quoted(object{"summary": tc.marker + "functions." + tc.name, "code": tc.value, "extended_summary": "{}"})
			}
			call, err := bridge.translateCall(native)
			if err != nil {
				t.Fatalf("declared target rejected through wrapper: %v", err)
			}
			if call["name"] != tc.name || text(call["namespace"]) != "" {
				t.Fatalf("wrong target: %+v", call)
			}
			if tc.kind == "custom" {
				if call["input"] != tc.value {
					t.Fatal("custom input changed")
				}
			} else {
				var args object
				if decode([]byte(text(call["arguments"])), &args) != nil || args[tc.field] != tc.value {
					t.Fatal("function arguments changed")
				}
			}
		})
	}
}

func TestCatalogDisplayPrefixPreservesNamespaceAuthority(t *testing.T) {
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "shell"},
		object{"type": "namespace", "name": "functions", "tools": []any{object{"type": "function", "name": "shell"}}},
		object{"type": "namespace", "name": "admin", "tools": []any{object{"type": "function", "name": "shell"}}}}
	_, bridge := mustPrepare(t, source, "", nil)
	for _, tc := range []struct{ name, namespace string }{
		{"shell", ""}, {"functions.shell", "functions"}, {"functions.functions.shell", "functions"},
		{"admin.shell", "admin"}, {"functions.admin.shell", "admin"},
	} {
		call, err := bridge.translateCall(nativeCall(object{"name": tc.name, "arguments": object{"cmd": "pwd"}}))
		if err != nil || call["name"] != "shell" || text(call["namespace"]) != tc.namespace {
			t.Fatalf("%s resolved to wrong authority: %v %v", tc.name, call, err)
		}
	}
	for _, name := range []string{"other.shell", "functions.functions.admin.shell", "functions.missing"} {
		if _, err := bridge.translateCall(nativeCall(object{"name": name, "arguments": object{}})); err == nil {
			t.Fatalf("undeclared target accepted: %s", name)
		}
	}
	delete(bridge.tools, "shell")
	if _, err := bridge.translateCall(nativeCall(object{"name": "shell", "arguments": object{}})); err == nil {
		t.Fatal("missing namespace was guessed")
	}
}

// A direct custom tool call is relayed as custom_tool_call with its exact input.
func TestDirectCustomCallRecoveredIntoClientTool(t *testing.T) {
	cache := new(ReplayCache)
	source := testSource()
	source["tools"] = []any{object{"type": "namespace", "name": "functions", "tools": []any{object{"type": "custom", "name": "apply_patch"}}}}
	_, bridge := mustPrepare(t, source, "scope", cache)
	patch := "*** Begin Patch\n*** Add File: a.txt\n+hi\n*** End Patch"
	native := object{"type": "custom_tool_call", "id": "ctc_a", "call_id": "call_a", "name": "functions.apply_patch", "input": patch}
	call, err := bridge.translateCall(native)
	if err != nil {
		t.Fatalf("direct custom call not recovered: %v", err)
	}
	if call["type"] != "custom_tool_call" || call["name"] != "apply_patch" || call["namespace"] != "functions" || call["input"] != patch {
		t.Fatalf("wrong custom tool item: %+v", call)
	}
}

// A direct call still refuses tools outside the catalog and genuine native tools.
func TestDirectCallRejectsUnknownAndMismatchedTools(t *testing.T) {
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "shell"}, object{"type": "custom", "name": "patch"}}
	_, bridge := mustPrepare(t, source, "scope", nil)
	cases := []object{
		{"type": "function_call", "id": "1", "call_id": "c1", "name": "read_ranges", "arguments": "{}"},
		{"type": "function_call", "id": "2", "call_id": "c2", "name": "search_workbook", "arguments": "{}"},
		{"type": "custom_tool_call", "id": "3", "call_id": "c3", "name": "shell", "input": "ls"},  // function tool arriving as custom
		{"type": "function_call", "id": "4", "call_id": "c4", "name": "patch", "arguments": "{}"}, // custom tool arriving as function
	}
	for i, native := range cases {
		if _, err := bridge.translateCall(native); err == nil {
			t.Fatalf("case %d must be rejected: %+v", i, native)
		}
	}
}

// A direct call cached for replay must reappear as a BPS-known run_officejs wrapper
// on the next turn, and the recorded tool output must round-trip under that identity.
func TestDirectCallReplaysAsTransportWrapper(t *testing.T) {
	cache := new(ReplayCache)
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "shell", "parameters": object{"type": "object"}}}
	_, bridge := mustPrepare(t, source, "account/key", cache)
	native := object{"type": "function_call", "id": "fc_d", "call_id": "call_d", "name": "shell", "arguments": `{"command":["pwd"]}`}
	call, err := bridge.translateCall(native)
	if err != nil {
		t.Fatal(err)
	}
	source["input"] = []any{message("user", "run"), call, object{"type": "function_call_output", "call_id": call["call_id"], "output": "/root"}}
	replayed, _ := mustPrepare(t, source, "account/key", cache)
	items := mustTestValue[[]any](t, replayed["input"])
	wrapper, _ := items[len(items)-2].(object)
	if text(wrapper["type"]) != "function_call" || text(wrapper["name"]) != "run_officejs" {
		t.Fatalf("direct call did not replay as a run_officejs transport item: %+v", wrapper)
	}
	if !strings.Contains(text(wrapper["arguments"]), `\"command\"`) {
		t.Fatalf("replayed transport lost the client arguments: %+v", wrapper["arguments"])
	}
	output, _ := items[len(items)-1].(object)
	if output["call_id"] != call["call_id"] || output["output"] != "/root" {
		t.Fatalf("tool output identity broken: %+v", output)
	}
	// Determinism: an identical follow-up request must serialize identically.
	again, _ := mustPrepare(t, source, "account/key", cache)
	if !reflect.DeepEqual(replayed, again) {
		t.Fatal("direct-call replay is not deterministic")
	}
}

func TestDirectCallPreservesExplicitNamespace(t *testing.T) {
	source := testSource()
	source["tools"] = []any{
		object{"type": "function", "name": "shell"},
		object{"type": "function", "name": "root.only"},
		object{"type": "namespace", "name": "admin", "tools": []any{object{"type": "function", "name": "shell"}}},
		object{"type": "namespace", "name": "client", "tools": []any{object{"type": "function", "name": "run_officejs"}}},
		object{"type": "namespace", "name": "custom", "tools": []any{object{"type": "custom", "name": "patch"}}},
	}
	for _, tc := range []struct {
		label, name, kind, wantNamespace string
		namespace                        any
		ok                               bool
	}{
		{"namespaced", "shell", "function_call", "admin", "admin", true},
		{"host_prefix_is_not_namespace", "shell", "function_call", "", "functions", false},
		{"root_dotted_name_is_not_namespace", "only", "function_call", "", "root", false},
		{"other_wrapper_name", "run_officejs", "function_call", "client", "client", true},
		{"unknown_wrapper_namespace", "run_officejs", "function_call", "", "unknown", false},
		{"custom", "patch", "custom_tool_call", "custom", "custom", true},
		{"unknown", "shell", "function_call", "", "missing", false},
		{"invalid_type", "shell", "function_call", "", 7, false},
		{"whitespace", "shell", "function_call", "", " admin", false},
		{"conflicting_qualified_name", "admin.shell", "function_call", "", "other", false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			cache := new(ReplayCache)
			_, bridge := mustPrepare(t, source, "scope", cache)
			native := object{"type": tc.kind, "id": "item_" + tc.label, "call_id": "call_" + tc.label, "name": tc.name, "namespace": tc.namespace, "arguments": `{"cmd":"pwd"}`, "input": "patch bytes"}
			call, err := bridge.translateCall(native)
			if !tc.ok {
				if err == nil || cache.get("scope", text(native["call_id"])) != nil {
					t.Fatalf("invalid explicit authority was accepted or cached: %v %v", call, err)
				}
				return
			}
			if err != nil || call["namespace"] != tc.wantNamespace || call["name"] != tc.name {
				t.Fatalf("explicit identity changed: %v %v", call, err)
			}
			wrapped := cache.get("scope", text(native["call_id"]))
			if wrapped == nil || !strings.Contains(quoted(wrapped), tc.wantNamespace+"."+tc.name) {
				t.Fatalf("replay lost namespace: %v", wrapped)
			}
		})
	}
}

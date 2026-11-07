package basispoints

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolSearchHistoryConsumesCompletedDiscoveryPairs(t *testing.T) {
	for _, execution := range []string{"client", "server"} {
		for _, choice := range []string{"auto", "none"} {
			t.Run(execution+"/"+choice, func(t *testing.T) {
				var callID any = "call_search"
				if execution == "server" {
					callID = nil
				}
				source := testSource()
				source["tool_choice"] = choice
				source["input"] = []any{
					message("user", "Find a file reader"),
					object{"type": "tool_search_call", "call_id": callID, "execution": execution, "status": "completed", "arguments": object{"query": "read files"}},
					object{"type": "tool_search_output", "call_id": callID, "execution": execution, "status": "completed", "tools": []any{catalogReadTool("string")}},
					object{"type": "function_call", "name": "read_file", "call_id": "call_read", "arguments": `{"path":"AGENTS.md"}`},
					object{"type": "function_call_output", "call_id": "call_read", "output": "Keep these file contents"},
					message("user", "Continue"),
				}
				body, bridge := mustPrepare(t, source, "search-history", nil)
				require.True(t, bridge.hasToolHistory)
				items := mustTestValue[[]any](t, body["input"])
				require.Len(t, items, 6) // Two developer messages plus four conversation items.
				for _, raw := range items {
					item := mustTestValue[object](t, raw)
					require.NotEqual(t, "tool_search_call", item["type"])
					require.NotEqual(t, "tool_search_output", item["type"])
				}
				require.Equal(t, "call_read", mustTestValue[object](t, items[3])["call_id"])
				require.Equal(t, "Keep these file contents", mustTestValue[object](t, items[4])["output"])
				_, err := bridge.translateCall(nativeCall(object{"name": "read_file", "arguments": object{"path": "next.go"}}))
				if choice == "none" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestToolSearchHistoryRequiresCompletedMatchingOutput(t *testing.T) {
	for _, output := range []object{
		nil,
		{"type": "tool_search_output", "call_id": "another", "tools": []any{}},
		{"type": "tool_search_output", "call_id": "search", "status": "in_progress", "tools": []any{}},
		{"type": "tool_search_output", "call_id": "search", "status": "incomplete", "tools": []any{}},
		{"type": "tool_search_output", "call_id": "search", "execution": "server", "tools": []any{}},
	} {
		t.Run(fmt.Sprint(output), func(t *testing.T) {
			source := testSource()
			items := []any{message("user", "Find tools"), object{"type": "tool_search_call", "call_id": "search", "execution": "client", "status": "completed", "arguments": object{"query": "files"}}}
			if output != nil {
				items = append(items, output)
			}
			source["input"] = items
			raw, err := json.Marshal(source)
			require.NoError(t, err)
			_, _, err = Prepare(raw, "search", nil)
			require.ErrorContains(t, err, "tool_search")
			require.ErrorContains(t, err, "path=input[1]")
		})
	}
}

func TestToolSearchIncompleteDiscoveryDoesNotAuthorizeTools(t *testing.T) {
	for _, status := range []string{"in_progress", "incomplete", "failed"} {
		t.Run(status, func(t *testing.T) {
			source := testSource()
			source["input"] = []any{object{"type": "tool_search_output", "status": status, "tools": []any{catalogReadTool("string")}}, message("user", "Continue")}
			_, bridge := mustPrepare(t, source, "scope", nil)
			require.Empty(t, bridge.tools)
			_, err := bridge.translateCall(nativeCall(object{"name": "read_file", "arguments": object{"path": "file.go"}}))
			require.ErrorContains(t, err, "outside the client's catalog")
		})
	}
}

func TestToolSearchHistoryPairingAndIdentity(t *testing.T) {
	call := func(id any, execution string) object {
		return object{"type": "tool_search_call", "call_id": id, "execution": execution, "arguments": object{"query": "files"}}
	}
	output := func(id any, execution string) object {
		return object{"type": "tool_search_output", "call_id": id, "execution": execution, "tools": []any{}}
	}
	for _, tc := range []struct {
		name  string
		items []any
		valid bool
	}{
		{"out_of_order_client_results", []any{call("a", "client"), call("b", "client"), output("b", "client"), output("a", "client")}, true},
		{"successive_hosted_searches", []any{call(nil, "server"), output(nil, "server"), call(nil, "server"), output(nil, "server")}, true},
		{"legacy_execution_omitted", []any{call("a", ""), output("a", "")}, true},
		{"result_precedes_call", []any{output("a", "client"), call("a", "client")}, false},
		{"duplicate_pending_id", []any{call("a", "client"), call("a", "client"), output("a", "client")}, false},
		{"duplicate_completed_id", []any{call("a", "client"), output("a", "client"), call("a", "client"), output("a", "client")}, false},
		{"client_id_missing", []any{call(nil, "client"), output(nil, "client")}, false},
		{"malformed_hosted_id", []any{call(23, "server"), output(nil, "server")}, false},
		{"malformed_output_id", []any{call(nil, "server"), output(23, "server")}, false},
		{"whitespace_id", []any{call(" a", "client"), output(" a", "client")}, false},
		{"unknown_execution", []any{call("a", "unknown"), output("a", "unknown")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := testSource()
			source["input"] = append(tc.items, message("user", "Continue"))
			source["tool_choice"] = "none"
			raw, err := json.Marshal(source)
			require.NoError(t, err)
			_, _, err = Prepare(raw, "search", nil)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "tool_search")
			}
		})
	}
}

func catalogReadTool(kind string) object {
	return object{"type": "function", "name": "read_file", "parameters": object{"type": "object", "properties": object{"path": object{"type": kind}}, "required": []any{"path"}}}
}

func TestLoadedCatalogToolsAndSchemaPrecedence(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		source := testSource()
		old, current := catalogReadTool("number"), catalogReadTool("string")
		source["input"] = []any{object{"type": "additional_tools", "tools": []any{old}}, object{"type": "tool_search_output", "tools": []any{current}}, message("user", "continue")}
		if explicit {
			source["tools"] = []any{current}
			mustTestValue[[]any](t, source["input"])[1] = object{"type": "tool_search_output", "tools": []any{old}}
		}
		body, bridge := mustPrepare(t, source, "scope", nil)
		if len(bridge.tools) != 1 {
			t.Fatal("loaded declaration missing")
		}
		for _, raw := range mustTestValue[[]any](t, body["input"]) {
			item, _ := raw.(object)
			if isCatalogItem(text(item["type"])) {
				t.Fatal("catalog leaked into history")
			}
		}
		if _, err := bridge.translateCall(nativeCall(object{"name": "read_file", "arguments": object{"path": "file.go"}})); err != nil {
			t.Fatal(err)
		}
		if _, err := bridge.translateCall(nativeCall(object{"name": "read_file", "arguments": object{"path": 3}})); err == nil {
			t.Fatal("selected schema was not enforced")
		}
	}
}

func TestLoadedCatalogPersistsAndUpdatesScopedCache(t *testing.T) {
	cache := new(CatalogCache)
	source := testSource()
	source["input"] = []any{object{"type": "tool_search_output", "tools": []any{object{"type": "namespace", "name": "outer.inner", "tools": []any{catalogReadTool("number")}}}}, message("user", "continue")}
	prepareCatalogTest(t, cache, source, "scope")
	source["input"] = []any{object{"type": "tool_search_output", "tools": []any{object{"type": "namespace", "name": "outer.inner", "tools": []any{catalogReadTool("string")}}}}, message("user", "continue")}
	bridge := prepareCatalogTest(t, cache, source, "scope")
	call, err := bridge.translateCall(nativeCall(object{"name": "outer.inner.read_file", "arguments": object{"path": "file.go"}}))
	if err != nil || call["namespace"] != "outer.inner" || call["name"] != "read_file" {
		t.Fatalf("namespace/schema lost: %v %v", call, err)
	}
	source["input"] = "continue"
	if _, err := prepareCatalogTest(t, cache, source, "scope").translateCall(nativeCall(object{"name": "outer.inner.read_file", "arguments": object{"path": "file.go"}})); err != nil {
		t.Fatal(err)
	}
	if len(prepareCatalogTest(t, cache, source, "other").tools) != 0 {
		t.Fatal("catalog crossed scopes")
	}
}

func TestLoadedCatalogChoiceNoneAndInvalidSelectedSchema(t *testing.T) {
	source := testSource()
	source["tool_choice"] = "none"
	source["input"] = []any{object{"type": "tool_search_output", "tools": []any{catalogReadTool("string")}}, message("user", "continue")}
	body, bridge := mustPrepare(t, source, "scope", nil)
	if len(bridge.tools) != 0 {
		t.Fatal("choice none retained tools")
	}
	encoded, _ := json.Marshal(body)
	if strings.Contains(string(encoded), "tool_search_output") {
		t.Fatal("catalog leaked")
	}
	delete(source, "tool_choice")
	mustTestValue[object](t, mustTestValue[[]any](t, source["input"])[0])["tools"] = []any{object{"type": "function", "name": "read_file", "parameters": "invalid"}}
	raw, _ := json.Marshal(source)
	if _, _, err := Prepare(raw, "scope", nil); err == nil {
		t.Fatal("invalid selected schema accepted")
	}
}

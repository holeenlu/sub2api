package basispoints

import (
	"fmt"
	"strings"
)

func isCatalogItem(kind string) bool {
	return kind == "additional_tools" || kind == "tool_search_output"
}

func completedToolSearchItem(item object) bool {
	status, exists := item["status"]
	return !exists || status == "completed"
}

// Search results become catalog declarations rather than native BPS tool
// outputs. Consume their calls too, but never hide a pending client search.
// Output-only discoveries remain supported for expanded/compacted catalogs.
func validateToolSearchHistory(input []any) error {
	pending := make(map[string]int)
	seen := make(map[string]bool)
	invalid := func(index int) error {
		return fmt.Errorf("basispoints requires a completed tool_search_output matching each tool_search_call (path=input[%d]); resend the matching tool search result", index)
	}
	for index, raw := range input {
		item, _ := raw.(object)
		kind := text(item["type"])
		if kind != "tool_search_call" && kind != "tool_search_output" {
			continue
		}
		id, execution := text(item["call_id"]), text(item["execution"])
		_, stringID := item["call_id"].(string)
		validID := item["call_id"] == nil || stringID
		_, stringExecution := item["execution"].(string)
		validExecution := item["execution"] == nil || stringExecution
		if kind == "tool_search_call" {
			_, waiting := pending[id]
			if !validID || !validExecution || !completedToolSearchItem(item) || waiting || (id != "" && seen[id]) || strings.TrimSpace(id) != id ||
				(id == "" && execution != "server") || (execution != "" && execution != "client" && execution != "server") {
				return invalid(index)
			}
			pending[id], seen[id] = index, true
			continue
		}
		callIndex, waiting := pending[id]
		if !waiting {
			continue
		}
		call, _ := input[callIndex].(object)
		callExecution := text(call["execution"])
		if callExecution == "" {
			callExecution = "client"
		}
		_, toolsArray := item["tools"].([]any)
		if !validID || !validExecution || !completedToolSearchItem(item) || !toolsArray || (execution != "" && execution != callExecution) {
			return invalid(callIndex)
		}
		delete(pending, id)
	}
	first := len(input)
	for _, index := range pending {
		if index < first {
			first = index
		}
	}
	if first < len(input) {
		return invalid(first)
	}
	return nil
}

type catalogDeclaration struct {
	item           object
	namespace, key string
	path           string
}

// Flatten only declaration identity here. Validate the selected schema later,
// so a superseded historical schema cannot reject a valid current declaration.
func flattenCatalog(value any, namespace, location string) ([]catalogDeclaration, error) {
	items, ok := value.([]any)
	if value != nil && !ok {
		return nil, fmt.Errorf("basispoints client tools must be an array")
	}
	var result []catalogDeclaration
	for index, raw := range items {
		itemPath := fmt.Sprintf("%s[%d]", location, index)
		item, ok := raw.(object)
		if !ok {
			return nil, fmt.Errorf("invalid Basispoints client tool")
		}
		kind, name := text(item["type"]), text(item["name"])
		if kind == "namespace" {
			if name == "" {
				return nil, fmt.Errorf("basispoints client namespaces require a name")
			}
			path := name
			if namespace != "" {
				path = namespace + "." + name
			}
			nested, err := flattenCatalog(item["tools"], path, itemPath+".tools")
			if err != nil {
				return nil, err
			}
			result = append(result, nested...)
			continue
		}
		key := name
		if namespace != "" {
			key = namespace + "." + name
		}
		if isUnsupportedHostedTool(kind) {
			key = "hosted:" + kind
		}
		result = append(result, catalogDeclaration{item: item, namespace: namespace, key: key, path: itemPath})
	}
	return result, nil
}

// Current explicit declarations win; without them, newer replay declarations
// replace inherited entries. Preserve first-seen order while choosing the last
// replayed definition. Cache publication still follows the existing CAS boundary.
func (b *Bridge) collectClientCatalog(source object, inherited bool) ([]any, error) {
	catalog, err := b.collectTools(source["tools"], "", "tools", false)
	if err != nil {
		return nil, err
	}
	var order []string
	replayed := make(map[string]catalogDeclaration)
	items, _ := source["input"].([]any)
	for index, raw := range items {
		item, _ := raw.(object)
		if !isCatalogItem(text(item["type"])) {
			continue
		}
		if text(item["type"]) == "tool_search_output" && !completedToolSearchItem(item) {
			continue
		}
		declarations, err := flattenCatalog(item["tools"], "", fmt.Sprintf("input[%d].tools", index))
		if err != nil {
			return nil, err
		}
		for _, declaration := range declarations {
			if _, exists := b.tools[declaration.key]; exists && !inherited {
				continue
			}
			if _, exists := replayed[declaration.key]; !exists {
				order = append(order, declaration.key)
			}
			replayed[declaration.key] = declaration
		}
	}
	if inherited {
		kept := catalog[:0]
		for _, raw := range catalog {
			entry, _ := raw.(object)
			if _, replaced := replayed[text(entry["name"])]; !replaced {
				kept = append(kept, raw)
			}
		}
		catalog = kept
	}
	for _, key := range order {
		declaration := replayed[key]
		if inherited {
			delete(b.tools, key)
		}
		additional, err := b.collectTools([]any{declaration.item}, declaration.namespace, declaration.path, true)
		if err != nil {
			return nil, err
		}
		catalog = append(catalog, additional...)
	}
	return catalog, nil
}

package service

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"mime"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// Cloud's programmatic exec wrapper can serialize dynamic-tool results as text.
// This private direct-only namespace uses the verified multimodal tool-result
// protocol; public names and namespaces still use the existing mapping.
const cloudClientToolNamespace = "sub2api_client"

var errCloudOriginalToolImage = errors.New("Cloud dynamic tool replies cannot preserve image detail original; use detail high or the native protocol")

type cloudProtocolRequest struct {
	body                 map[string]any
	source               map[string]any
	decisions            []cloudAdaptationDecision
	continuation         cloudContinuationProjection
	history              []any
	input                []any
	inputMessage         map[string]any
	outputs              map[string]string // JSON-encoded original outputs for idempotency only.
	outputItems          map[string][]any
	outputHistory        map[string]any
	originalToolImages   map[string]bool
	tools                []any
	toolNames            map[string]bool
	customTools          map[string]bool // free-form tools mapped to one string input
	names                map[string]apicompat.ResponsesNamespaceName
	previous             string
	model                string
	instructions         string
	effort               string
	summary              string
	verbosity            string
	settings             string
	threadSettings       string
	serialTools          bool
	outputSchema         map[string]any
	webSearch            bool
	webSearchMode        string // App Server web_search mode: cached, indexed or live
	imageGeneration      bool
	includeSearchSources bool
	nonToolSettings      string
	toolSearch           bool
	// compaction marks a Codex remote compaction request. Its final
	// compaction_trigger is removed: every item is history and the turn input
	// asks for the summary its compaction item will carry.
	compaction bool
}

func cloudBadRequest(field string) error {
	if len(field) > 64 || strings.IndexFunc(field, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		field = "request field"
	}
	return cloudRejectionFor(field)
}

func parseCloudProtocolRequest(raw []byte) (*cloudProtocolRequest, error) {
	body, err := decodeCloudProtocolBody(raw)
	if err != nil {
		return nil, err
	}
	return parseCloudIngressSource(body)
}

func compileCloudProtocolRequest(source map[string]any, decisions []cloudAdaptationDecision) (p *cloudProtocolRequest, err error) {
	body := cloneCloudValue(source).(map[string]any)
	defer func() {
		if err == nil {
			return
		}
		var rejected *cloudRejection
		if !errors.As(err, &rejected) {
			id, param := "CLOUD-HISTORY", "input"
			if strings.Contains(strings.ToLower(err.Error()), "image") {
				id, param = "CLOUD-IMAGE", "input[].content[]"
			}
			rejected = &cloudRejection{Code: "cloud_unsupported_parameter", Param: param, MatrixID: id, Remedy: err.Error(), reason: param}
			err = rejected
		}
		rejected.Decisions = append([]cloudAdaptationDecision(nil), decisions...)
		cloudDecision(&rejected.Decisions, rejected.Param, "reject", rejected.MatrixID)
	}()
	if items, ok := body["input"].([]any); ok {
		for _, raw := range items {
			if item, ok := raw.(map[string]any); ok {
				if typ, exists := item["type"]; exists {
					if _, ok := typ.(string); !ok {
						return nil, cloudBadRequest("input item type")
					}
				} else if item["role"] != nil {
					item["type"] = "message"
				}
			}
		}
		if next, changed := normalizeCodexToolRoleMessages(items); changed {
			items = next
			cloudDecision(&decisions, "input[]", "map", "CLOUD-HISTORY")
		}
		if next, changed := normalizeCodexMessageContentText(items); changed {
			items = next
			cloudDecision(&decisions, "input[].content[]", "map", "CLOUD-PART")
		}
		body["input"] = items
	}
	if sanitizeEmptyBase64InputImagesInOpenAIRequestBodyMap(body) {
		cloudDecision(&decisions, "input[].content[]", "drop", "CLOUD-IMAGE")
	}
	if err := cloudNormalizeRequest(body, &decisions); err != nil {
		return nil, err
	}
	if err := cloudDropNeutralFields(body); err != nil {
		return nil, err
	}
	// The common ingress already consumes this optional cache-affinity hint.
	// Cloud has no equivalent thread/turn parameter: its response/call IDs and
	// authenticated owner remain the only continuation authority. Do not use the
	// hint as a thread ID or inherit it as a generation setting across turns.
	if value := body["prompt_cache_key"]; value != nil {
		if _, ok := value.(string); !ok {
			return nil, cloudBadRequest("prompt_cache_key")
		}
	}
	delete(body, "prompt_cache_key")
	// Codex HTTP/WS attaches a string map for client affinity/diagnostics.
	// Ingress may read it before adaptation. Cloud owns its upstream identities;
	// never use a client value as a Cloud owner, credential, tool, or config override.
	if metadata, exists := body["client_metadata"]; exists && metadata != nil {
		values, ok := metadata.(map[string]any)
		if !ok || len(values) > 64 {
			return nil, cloudBadRequest("client_metadata")
		}
		encoded, _ := json.Marshal(values)
		if len(encoded) > 64<<10 {
			return nil, cloudBadRequest("client_metadata size")
		}
		for key, value := range values {
			if _, ok := value.(string); !ok || len(key) == 0 || len(key) > 128 {
				return nil, cloudBadRequest("client_metadata")
			}
		}
	}
	// Application metadata is still returned unchanged. Transport metadata is
	// local to the incoming request and is not part of generation settings.
	delete(body, "client_metadata")
	p = &cloudProtocolRequest{source: source, body: body, decisions: decisions, outputs: make(map[string]string), outputItems: make(map[string][]any), outputHistory: make(map[string]any), toolNames: make(map[string]bool)}
	if metadata, exists := body["metadata"]; exists && metadata != nil {
		values, ok := metadata.(map[string]any)
		if !ok || len(values) > 16 {
			return nil, cloudBadRequest("metadata")
		}
		for key, value := range values {
			text, ok := value.(string)
			if !ok || len(key) > 64 || len(text) > 512 {
				return nil, cloudBadRequest("metadata")
			}
		}
	}
	p.model, _ = body["model"].(string)
	if p.model == "" {
		return nil, cloudBadRequest("model")
	}
	if value, exists := body["previous_response_id"]; exists && value != nil {
		var ok bool
		p.previous, ok = value.(string)
		if !ok {
			return nil, cloudBadRequest("previous_response_id")
		}
	}
	if value, exists := body["instructions"]; exists && value != nil {
		var ok bool
		p.instructions, ok = value.(string)
		if !ok {
			return nil, cloudBadRequest("instructions")
		}
	}
	for _, key := range []string{"stream", "store", "parallel_tool_calls"} {
		if value, ok := body[key]; ok && value != nil {
			b, valid := value.(bool)
			if !valid || (key == "store" && b) {
				return nil, cloudBadRequest(key)
			}
		}
	}
	p.serialTools = body["parallel_tool_calls"] == false
	if include, ok := body["include"]; ok && include != nil {
		values, valid := include.([]any)
		if !valid {
			return nil, cloudBadRequest("include")
		}
		for _, value := range values {
			switch value {
			case "reasoning.encrypted_content":
				// Raw reasoning projection preserves upstream ciphertext, as before.
			case "web_search_call.action.sources":
				p.includeSearchSources = true
			case "output.encrypted_content":
				// Optional ciphertext on output items for token replay. Cloud
				// produces none; clients treat the field as absent, as natively.
			default:
				return nil, cloudBadRequest("include")
			}
		}
	}
	if value, ok := body["tool_choice"]; ok && value != nil && value != "auto" && value != "none" {
		return nil, cloudBadRequest("tool_choice")
	}
	if text, ok := body["text"]; ok && text != nil {
		m, valid := text.(map[string]any)
		if !valid {
			return nil, cloudBadRequest("text")
		}
		for key, v := range m {
			if key == "verbosity" {
				if v == nil {
					continue
				}
				p.verbosity, _ = v.(string)
				switch p.verbosity {
				case "low", "medium", "high":
					continue
				default:
					return nil, cloudBadRequest("text.verbosity")
				}
			}
			if key != "format" {
				return nil, cloudBadRequest("text." + key)
			}
			if v == nil {
				continue
			}
			format, valid := v.(map[string]any)
			if !valid {
				return nil, cloudBadRequest("text.format")
			}
			switch format["type"] {
			case "text":
				if len(format) != 1 {
					return nil, cloudBadRequest("text.format")
				}
			case "json_schema":
				// App-server outputSchema uses strict validation and its own schema
				// name. Preserve the client descriptor publicly; never pretend that
				// non-strict generation or an extra prompt description was honored.
				for field := range format {
					if field != "type" && field != "name" && field != "schema" && field != "strict" {
						return nil, cloudBadRequest("text.format." + field)
					}
				}
				name, ok := format["name"].(string)
				if !ok || name == "" || len(name) > 64 || format["strict"] != true {
					return nil, cloudBadRequest("text.format strict schema")
				}
				p.outputSchema, ok = format["schema"].(map[string]any)
				if !ok || len(p.outputSchema) == 0 {
					return nil, cloudBadRequest("text.format.schema")
				}
			default:
				return nil, cloudBadRequest("text.format")
			}
		}
	}
	if reasoning, ok := body["reasoning"]; ok && reasoning != nil {
		m, valid := reasoning.(map[string]any)
		if !valid {
			return nil, cloudBadRequest("reasoning")
		}
		for key, value := range m {
			if key == "summary" {
				if value == nil {
					continue
				}
				p.summary, _ = value.(string)
				switch p.summary {
				case "auto", "concise", "detailed", "none":
					continue
				default:
					return nil, cloudBadRequest("reasoning.summary")
				}
			}
			if key != "effort" {
				return nil, cloudBadRequest("reasoning." + key)
			}
			p.effort, _ = value.(string)
			switch p.effort {
			case "none", "minimal", "low", "medium", "high", "xhigh", "max":
			default:
				return nil, cloudBadRequest("reasoning.effort")
			}
		}
	}
	// Codex can carry its complete catalog in input[].additional_tools. Move
	// those declarations into the same catalog before validation and lowering;
	// they are configuration, not messages to inject into the Cloud thread.
	if err := cloudPromoteAdditionalTools(body); err != nil {
		return nil, err
	}
	if _, err := apicompat.PromoteResponsesToolSearchDiscoveries(body); err != nil {
		return nil, err
	}
	if err := cloudNormalizeTools(body["tools"], "tools", &decisions); err != nil {
		return nil, err
	}
	// Freeze the effective, unlowered request for field-level inheritance.
	// Pure Lite declarations must become the inherited catalog, not stay hidden
	// in a previous turn's input carrier.
	p.source = cloneCloudValue(body).(map[string]any)
	// Validate before the shared namespace lowering, which intentionally skips
	// unsupported child types for other adapters. Cloud must never drop one.
	if rawTools, ok := body["tools"]; ok && rawTools != nil {
		tools, valid := rawTools.([]any)
		if !valid {
			return nil, cloudBadRequest("tools")
		}
		for _, rawTool := range tools {
			tool, valid := rawTool.(map[string]any)
			if !valid {
				return nil, cloudBadRequest("tools")
			}
			switch tool["type"] {
			case "namespace":
				children, valid := tool["tools"].([]any)
				if !valid || len(children) == 0 {
					return nil, cloudBadRequest("namespace tools")
				}
				for _, child := range children {
					m, ok := child.(map[string]any)
					if !ok || (m["type"] != "function" && m["type"] != "custom") {
						return nil, cloudBadRequest("namespace child")
					}
					if m["type"] == "custom" {
						if err := cloudValidateCustomTool(m); err != nil {
							return nil, err
						}
					}
				}
			case "web_search", "image_generation":
				// Bare declarations are the verified Cloud contract. Codex search
				// mode controls map to the thread's web_search mode; other provider
				// controls (filters, location, context size) are never discarded.
				if tool["type"] == "web_search" {
					if p.webSearch {
						return nil, cloudBadRequest("duplicate hosted tool")
					}
					mode, err := cloudWebSearchMode(tool)
					if err != nil {
						return nil, err
					}
					p.webSearch, p.webSearchMode = true, mode
				} else if len(tool) != 1 {
					return nil, cloudBadRequest("hosted tool controls")
				} else {
					if p.imageGeneration {
						return nil, cloudBadRequest("duplicate hosted tool")
					}
					p.imageGeneration = true
				}
			case "custom":
				if err := cloudValidateCustomTool(tool); err != nil {
					return nil, err
				}
			case "tool_search":
				if execution, exists := tool["execution"]; exists && execution != "client" {
					return nil, cloudBadRequest("tool_search.execution")
				}
				p.toolSearch = true
			case "function":
			default:
				if tool["type"] == "code_mode" {
					rule := cloudCapabilityByID("CLOUD-CODE")
					return nil, &cloudRejection{Code: rule.ErrorCode, Param: "tools[].type", MatrixID: rule.ID, Remedy: rule.Remedy, reason: "tool type"}
				}
				return nil, cloudBadRequest("tool type")
			}
		}
	}
	body, err = cloudAdaptSearchTools(p)
	if err != nil {
		return nil, err
	}
	p.body = body
	names, _, err := apicompat.FlattenResponsesNamespaces(body)
	if err != nil {
		return nil, cloudBadRequest("namespace collision")
	}
	if len(names) > 0 {
		p.names = names
	}
	if tools, ok := body["tools"].([]any); ok {
		for _, rawTool := range tools {
			tool := rawTool.(map[string]any)
			if tool["type"] == "web_search" || tool["type"] == "image_generation" {
				continue
			}
			if tool["type"] == "custom" {
				name, _ := tool["name"].(string)
				if p.toolNames[name] {
					return nil, cloudBadRequest("tool name")
				}
				description, _ := tool["description"].(string)
				format, _ := tool["format"].(map[string]any)
				if p.customTools == nil {
					p.customTools = make(map[string]bool)
				}
				p.toolNames[name], p.customTools[name] = true, true
				p.tools = append(p.tools, map[string]any{"type": "function", "name": name, "description": cloudCustomToolDescription(description, format), "inputSchema": cloudCustomToolSchema()})
				continue
			}
			name, _ := tool["name"].(string)
			if name == "" || len(name) > 64 || p.toolNames[name] {
				return nil, cloudBadRequest("tool name")
			}
			if strict, ok := tool["strict"]; ok && strict != nil && strict != false {
				return nil, cloudBadRequest("strict tool")
			}
			schema, ok := tool["parameters"].(map[string]any)
			if !ok {
				return nil, cloudBadRequest("tool parameters")
			}
			description, _ := tool["description"].(string)
			p.toolNames[name] = true
			p.tools = append(p.tools, map[string]any{"type": "function", "name": name, "description": description, "inputSchema": schema})
		}
	}
	if body["tool_choice"] == "none" {
		p.tools = nil
		p.toolNames = map[string]bool{}
		p.webSearch = false
		p.webSearchMode = ""
		p.imageGeneration = false
	}
	var items []any
	switch input := body["input"].(type) {
	case string:
		items = []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": input}}}}
	case []any:
		items = input
	default:
		return nil, cloudBadRequest("input")
	}
	if len(items) == 0 {
		return nil, cloudBadRequest("input")
	}
	items, _ = normalizeCodexToolRoleMessages(items)
	items, _ = normalizeCodexMessageContentText(items)
	body["input"] = items
	items, _ = normalizeOpenAICompactionTriggerItems(items, true)
	// Records of tool activity from turns served before Cloud (native or BPS)
	// carry nothing a Cloud thread can use: client tool search loaded tools that
	// this request now declares directly, and a hosted search or image Cloud did
	// not run cannot be presented as its own. Omit them, as BPS omits tool
	// search, rather than fail every later turn of a migrated conversation; the
	// answers that followed them remain in the history.
	items, err = normalizeCloudHistoryItems(items, &decisions)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, cloudBadRequest("input")
	}
	// Remote compaction v2 places one bare trigger after the history it asks to
	// summarize. Over a hot WebSocket the history is the previous response's.
	if last, ok := items[len(items)-1].(map[string]any); ok && last["type"] == "compaction_trigger" {
		if len(last) != 1 {
			return nil, cloudBadRequest("compaction_trigger")
		}
		if items = items[:len(items)-1]; len(items) == 0 && p.previous == "" {
			return nil, cloudBadRequest("compaction history")
		}
		p.compaction = true
	}
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, cloudBadRequest("input item")
		}
		typ, _ := item["type"].(string)
		if typ == "compaction_trigger" {
			return nil, cloudBadRequest("compaction_trigger position")
		}
		if typ == "reasoning" || typ == "compaction" {
			if typ == "reasoning" && (item["encrypted_content"] == nil || item["encrypted_content"] == "") {
				cloudDecision(&decisions, "input[].reasoning", "drop", "CLOUD-CIPHER")
			}

			if err := cloudEncryptedHistory(item, typ); err != nil {
				return nil, err
			}
			if _, _, err := cloudCompactionSummary(item); typ == "compaction" && err != nil {
				return nil, err
			}
			continue
		}
		if typ == "web_search_call" || typ == "image_generation_call" {
			if err := cloudHostedHistory(item, typ); err != nil {
				return nil, err
			}
			continue
		}
		if typ == "custom_tool_call" || typ == "custom_tool_call_output" {
			// Same internal form as the published call: one canonical JSON input.
			if err := cloudLowerCustomToolItem(item); err != nil {
				return nil, err
			}
			typ, _ = item["type"].(string)
		}
		for key, value := range item {
			switch key {
			case "type", "role", "content", "id", "status", "call_id", "name", "namespace", "arguments", "output", "phase":
			case cloudMessageMetadataKey:
				if err := cloudValidMessageMetadata(value); err != nil {
					return nil, err
				}
			case "encrypted_content":
				// Output token replay ciphertext from an earlier turn: opaque and
				// passed through unchanged, like reasoning ciphertext. Only model
				// output (assistant messages and calls) can carry it.
				if text, ok := value.(string); !ok || text == "" || (typ != "function_call" && !(typ == "message" && item["role"] == "assistant")) {
					return nil, cloudBadRequest("input." + key)
				}
			default:
				return nil, cloudBadRequest("input." + key)
			}
		}
		if typ == "" {
			typ = "message"
			item["type"] = typ
		}
		if phase, exists := item["phase"]; exists && (typ != "message" || item["role"] != "assistant" || !cloudValidMessagePhase(phase)) {
			return nil, cloudBadRequest("message phase")
		}
		switch typ {
		case "message":
			role, _ := item["role"].(string)
			switch role {
			case "system", "developer", "user", "assistant":
			default:
				return nil, cloudBadRequest("message role")
			}
			content, err := cloudMessageContent(item["content"], role)
			if err != nil {
				return nil, err
			}
			item["content"] = content
		case "function_call":
			callID, _ := item["call_id"].(string)
			name, _ := item["name"].(string)
			if callID == "" || name == "" {
				return nil, cloudBadRequest("function history")
			}
			if args, ok := item["arguments"].(string); !ok || !json.Valid([]byte(args)) {
				return nil, cloudBadRequest("function arguments")
			}
		case "function_call_output":
			callID, _ := item["call_id"].(string)
			if callID == "" {
				return nil, cloudBadRequest("tool call_id")
			}
			var outputItems []any
			switch output := item["output"].(type) {
			case string:
				outputItems = []any{map[string]any{"type": "inputText", "text": output}}
			case []any:
				parts, err := cloudMessageContent(output, "user")
				if err != nil {
					return nil, err
				}
				for _, rawPart := range parts {
					part := rawPart.(map[string]any)
					if part["type"] == "input_image" {
						if part["detail"] == "original" {
							// Raw historical outputs retain detail through inject_items.
							// A pending dynamic-tool reply cannot carry this field.
							if p.originalToolImages == nil {
								p.originalToolImages = make(map[string]bool)
							}
							p.originalToolImages[callID] = true
							continue
						}
						// DynamicToolCallOutputContentItem has no detail field;
						// Codex converts inputImage to the default High detail.
						outputItems = append(outputItems, map[string]any{"type": "inputImage", "imageUrl": part["image_url"]})
					} else {
						outputItems = append(outputItems, map[string]any{"type": "inputText", "text": part["text"]})
					}
				}
			default:
				return nil, cloudBadRequest("tool output content")
			}
			encoded, _ := json.Marshal(item["output"])
			output := string(encoded)
			if old, exists := p.outputs[callID]; exists && old != output {
				return nil, cloudBadRequest("conflicting tool output")
			}
			p.outputs[callID] = output
			p.outputItems[callID] = outputItems
			p.outputHistory[callID] = item["output"]
		default:
			return nil, cloudBadRequest("input item type")
		}
	}
	body["input"] = items
	p.continuation = projectCloudContinuation(body, items)
	p.history = items
	var last map[string]any
	if len(items) > 0 {
		last, _ = items[len(items)-1].(map[string]any)
	}
	summary, summarized, _ := cloudCompactionSummary(last)
	switch {
	case p.compaction:
		p.inputMessage = map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": cloudCompactionPrompt}}}
		p.input = []any{map[string]any{"type": "text", "text": cloudCompactionPrompt, "text_elements": []any{}}}
	case summarized && last["type"] == "compaction":
		// Codex resumes a turn it compacted midway from the compaction itself:
		// the summary is the turn input and the item stays thread history.
		p.inputMessage = last
		p.input = []any{map[string]any{"type": "text", "text": cloudCompactionText(summary), "text_elements": []any{}}}
		p.history = items[:len(items)-1]
	case last["type"] == "message" && last["role"] == "user":
		p.inputMessage = last
		for _, entry := range last["content"].([]any) {
			part := entry.(map[string]any)
			if part["type"] == "input_image" {
				detail := part["detail"]
				if detail == nil {
					detail = "auto"
				}
				p.input = append(p.input, map[string]any{"type": "image", "url": part["image_url"], "detail": detail})
			} else {
				p.input = append(p.input, map[string]any{"type": "text", "text": part["text"], "text_elements": []any{}})
			}
		}
		p.history = items[:len(items)-1]
	}
	thread := map[string]any{
		"model": p.model, "instructions": p.instructions, "tools": p.tools, "tool_search": p.toolSearch,
		"web_search": p.webSearch, "web_search_mode": p.webSearchMode,
		"image_generation": p.imageGeneration, "verbosity": p.verbosity,
	}
	encoded, _ := json.Marshal(thread)
	p.threadSettings = cloudDigest(encoded)
	thread["effort"], thread["summary"], thread["serial_tools"], thread["output_schema"] = p.effort, p.summary, p.serialTools, p.outputSchema
	includeSet := map[string]bool{}
	if values, ok := body["include"].([]any); ok {
		for _, value := range values {
			if name, ok := value.(string); ok {
				includeSet[name] = true
			}
		}
	}
	include := make([]string, 0, len(includeSet))
	for name := range includeSet {
		include = append(include, name)
	}
	sort.Strings(include)
	thread["include"] = include
	encoded, _ = json.Marshal(thread)
	p.settings = cloudDigest(encoded)
	withoutTools := maps.Clone(thread)
	for _, key := range []string{"tools", "tool_search", "web_search", "web_search_mode", "image_generation"} {
		delete(withoutTools, key)
	}
	encoded, _ = json.Marshal(withoutTools)
	p.nonToolSettings = cloudDigest(encoded)
	p.decisions = decisions
	return p, nil
}

// Translate only client history forms that App Server cannot inject directly.
// The normal parser still validates the resulting messages and tool calls.
func cloudLowerMigratedInput(item map[string]any) (map[string]any, error) {
	if item["type"] == "function_call" {
		if value, exists := item["encrypted_function_args"]; exists {
			if _, ok := value.([]any); value != nil && !ok {
				return nil, cloudBadRequest("encrypted_function_args")
			}
			// Like Codex's non-OpenAI provider request, use the explicit plaintext
			// arguments. They still must be valid JSON; ciphertext cannot replace them.
			delete(item, "encrypted_function_args")
		}
		return item, nil
	}
	standalone := openai.IsNamedStandaloneToolOutput(item)
	if item["type"] != "agent_message" && item["type"] != "configuration_update" && !standalone {
		return item, nil
	}
	for key, value := range item {
		allowed := false
		switch key {
		case "type":
			allowed = true
		case "id":
			_, valid := value.(string)
			allowed = value == nil || valid
		case cloudMessageMetadataKey:
			if err := cloudValidMessageMetadata(value); err != nil {
				return nil, err
			}
			allowed = true
		default:
			switch item["type"] {
			case "agent_message":
				allowed = key == "author" || key == "recipient" || key == "content"
			case "configuration_update":
				allowed = key == "reasoning"
			case "function_call_output":
				allowed = key == "name" || key == "namespace" || key == "call_id" || key == "output" || key == "status"
			}
		}
		if !allowed {
			return nil, cloudBadRequest("input." + key)
		}
	}
	if item["type"] == "configuration_update" {
		reasoning, ok := item["reasoning"].(map[string]any)
		if !ok || len(reasoning) != 1 {
			return nil, cloudBadRequest("configuration_update.reasoning")
		}
		if effort, ok := reasoning["effort"].(string); !ok || effort == "" {
			return nil, cloudBadRequest("configuration_update.reasoning.effort")
		}
		// This records a past turn's setting. The current request's reasoning
		// object is authoritative; replay must not reset it to an old effort.
		return nil, nil
	}
	var content []any
	if standalone {
		for _, key := range []string{"name", "namespace", "call_id", "status"} {
			if value, exists := item[key]; exists && value != nil {
				if _, ok := value.(string); !ok {
					return nil, cloudBadRequest("input." + key)
				}
			}
		}
		parts, err := cloudMessageContent(item["output"], "user")
		if err != nil {
			return nil, err
		}
		name, _ := item["name"].(string)
		if namespace, _ := item["namespace"].(string); namespace != "" {
			name = namespace + "." + name
		}
		content = append([]any{map[string]any{"type": "input_text", "text": "Tool message from " + name + ":\n"}}, parts...)
	} else {
		author, authorOK := item["author"].(string)
		recipient, recipientOK := item["recipient"].(string)
		if !authorOK || !recipientOK || author == "" || recipient == "" {
			return nil, cloudBadRequest("agent_message attribution")
		}
		parts, ok := item["content"].([]any)
		if !ok {
			return nil, cloudBadRequest("agent_message.content")
		}
		var texts []string
		for _, raw := range parts {
			part, ok := raw.(map[string]any)
			if !ok || len(part) != 2 {
				return nil, cloudBadRequest("agent_message.content")
			}
			switch part["type"] {
			case "input_text":
				value, ok := part["text"].(string)
				if !ok {
					return nil, cloudBadRequest("agent_message.text")
				}
				texts = append(texts, value)
			case "encrypted_content":
				if _, ok := part["encrypted_content"].(string); !ok {
					return nil, cloudBadRequest("agent_message.encrypted_content")
				}
				// The sibling-agent cipher is not Cloud reasoning and cannot be
				// injected or represented as plaintext. Keep the readable message.
			default:
				return nil, cloudBadRequest("agent_message.content type")
			}
		}
		text := strings.Join(texts, "\n")
		if strings.TrimSpace(text) == "" {
			// Like Codex's realtime transcript, skip unreadable agent history.
			// Rejecting it would poison every later full-history replay.
			return nil, nil
		}
		content = []any{map[string]any{"type": "input_text", "text": "Agent message from " + author + " to " + recipient + ":\n" + text}}
	}
	// A task delivered to this thread is its next user input, never proof that
	// a pending tool ran. It cannot satisfy or bypass call_id/owner matching.
	message := map[string]any{"type": "message", "role": "user", "content": content}
	if id, exists := item["id"]; exists {
		message["id"] = id
	}
	return message, nil
}

// Extract declaration carriers without injecting them as conversation content.
func cloudPromoteAdditionalTools(body map[string]any) error {
	items, ok := body["input"].([]any)
	if !ok {
		return nil
	}
	var declarations, history []any
	found := false
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || item["type"] != "additional_tools" {
			history = append(history, raw)
			continue
		}
		found = true
		for key, value := range item {
			if key == "id" {
				if _, ok := value.(string); value != nil && !ok {
					return cloudBadRequest("additional_tools.id")
				}
				continue
			}
			if key != "type" && key != "role" && key != "tools" {
				return cloudBadRequest("additional_tools." + key)
			}
		}
		if role, exists := item["role"]; exists && role != "developer" && role != "system" {
			return cloudBadRequest("additional_tools.role")
		}
		tools, ok := item["tools"].([]any)
		if !ok {
			return cloudBadRequest("additional_tools.tools")
		}
		declarations = append(declarations, tools...)
	}
	if !found {
		return nil
	}
	var tools []any
	if raw := body["tools"]; raw != nil {
		var ok bool
		tools, ok = raw.([]any)
		if !ok {
			return cloudBadRequest("tools")
		}
	}
	// A standard request declares the current catalog explicitly. Older Lite
	// declarations in replayed history must not restore retired tools or collide
	// with their new definitions. Pure Lite requests still merge all carriers.
	if tools != nil {
		body["input"] = history
		return nil
	}
	// Codex IncrementalTools writes a baseline once, then appends deltas.
	// Later top-level definitions replace their type/name identity; namespace
	// deltas replace only named members, retaining members they do not mention.
	// Removal notices remain conversation content, not a second control parser.
	merged := make([]any, 0, len(declarations))
	positions := make(map[string]int)
	members := make(map[string]map[string]int)
	for _, raw := range declarations {
		tool, ok := raw.(map[string]any)
		if !ok {
			return cloudBadRequest("tools")
		}
		typ, typeOK := tool["type"].(string)
		name, _ := tool["name"].(string)
		if !typeOK || typ == "" {
			return cloudBadRequest("tool type")
		}
		identity := typ + "\x00" + name
		index, exists := positions[identity]
		if !exists {
			index = len(merged)
			positions[identity] = index
			merged = append(merged, tool)
		}
		if typ == "namespace" {
			children, ok := tool["tools"].([]any)
			if !ok || len(children) == 0 || name == "" {
				return cloudBadRequest("namespace tools")
			}
			var combined []any
			if exists {
				combined = merged[index].(map[string]any)["tools"].([]any)
			} else {
				members[identity] = make(map[string]int)
			}
			for _, rawChild := range children {
				child, ok := rawChild.(map[string]any)
				childName, _ := child["name"].(string)
				if !ok || childName == "" || (child["type"] != "function" && child["type"] != "custom") {
					return cloudBadRequest("namespace child")
				}
				if position, exists := members[identity][childName]; exists {
					combined[position] = child
				} else {
					members[identity][childName] = len(combined)
					combined = append(combined, child)
				}
			}
			tool = maps.Clone(tool)
			tool["tools"] = combined
		}
		merged[index] = tool
	}
	body["tools"], body["input"] = merged, history
	return nil
}

// The same verified phase vocabulary governs incoming history and raw output.
// Missing upstream phases remain unknown; callers must not invent a final phase.
func cloudValidMessagePhase(value any) bool {
	phase, ok := value.(string)
	if !ok {
		return false
	}
	switch phase {
	case "commentary", "partial_answer", "final_answer":
		return true
	default:
		return false
	}
}

func cloudMessageContent(raw any, role string) ([]any, error) {
	if text, ok := raw.(string); ok {
		typ := "input_text"
		if role == "assistant" {
			typ = "output_text"
		}
		return []any{map[string]any{"type": typ, "text": text}}, nil
	}
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, cloudBadRequest("message content")
	}
	for _, v := range items {
		part, ok := v.(map[string]any)
		if !ok {
			return nil, cloudBadRequest("message content")
		}
		typ, _ := part["type"].(string)
		if typ == "input_image" {
			if role != "user" {
				return nil, cloudBadRequest("image role")
			}
			if err := cloudValidateInlineImage(part); err != nil {
				return nil, err
			}
			continue
		}
		if typ != "input_text" && !(role == "assistant" && typ == "output_text") {
			return nil, cloudBadRequest("message content type")
		}
		if _, ok := part["text"].(string); !ok {
			return nil, cloudBadRequest("message text")
		}

	}
	return items, nil
}

// Encrypted history is an opaque upstream value. Validate its public envelope
// without decrypting it, normalizing it, or accepting private reasoning fields.
func cloudEncryptedHistory(item map[string]any, typ string) error {
	for key, value := range item {
		switch key {
		case "type":
		case "id":
			if value != nil {
				if _, ok := value.(string); !ok {
					return cloudBadRequest("encrypted history id")
				}
			}
		case "encrypted_content":
			if value != nil {
				if text, ok := value.(string); !ok || text == "" {
					return cloudBadRequest("encrypted history content")
				}
			}
		case cloudMessageMetadataKey:
			if err := cloudValidMessageMetadata(value); err != nil {
				return err
			}
		case "summary":
			if typ != "reasoning" {
				return cloudBadRequest("compaction summary")
			}
		case "content":
			// Codex replays an absent raw reasoning content as null. Only
			// that empty form is accepted; raw reasoning text is never.
			if parts, isList := value.([]any); typ != "reasoning" || (value != nil && (!isList || len(parts) != 0)) {
				return cloudBadRequest("encrypted history.content")
			}
		case "status":
			if typ == "compaction" && value != "completed" || typ == "reasoning" && value != "completed" && value != "in_progress" && value != "incomplete" {
				return cloudBadRequest("encrypted history status")
			}
		default:
			return cloudBadRequest("encrypted history." + key)
		}
	}
	if typ == "compaction" {
		if content, ok := item["encrypted_content"].(string); !ok || content == "" {
			return cloudBadRequest("compaction encrypted_content")
		}
		return nil
	}
	delete(item, "content")
	summary, ok := item["summary"].([]any)
	if !ok {
		return cloudBadRequest("reasoning summary")
	}
	for _, raw := range summary {
		part, ok := raw.(map[string]any)
		if !ok || len(part) != 2 || part["type"] != "summary_text" {
			return cloudBadRequest("reasoning summary")
		}
		if _, ok := part["text"].(string); !ok {
			return cloudBadRequest("reasoning summary text")
		}
	}
	return nil
}

// cloudValidateInlineImage checks one input_image part against the Cloud
// protocol: inline base64 data only (no file_id or remote URL), and no detail
// low. Like native /responses there is no gateway per-image byte or count cap:
// the official client already resizes images, and the whole request is bounded
// by the ingress body limit.
func cloudValidateInlineImage(part map[string]any) error {
	if _, exists := part["file_id"]; exists {
		return errors.New("Cloud cannot resolve client image file_id ownership; send an inline base64 image instead")
	}
	if detail, exists := part["detail"]; exists {
		switch detail {
		case "auto", "high", "original":
		case "low":
			return errors.New("Cloud upstream does not support image detail low; use auto, high or the native protocol")
		default:
			return cloudBadRequest("image detail")
		}
	}
	url, ok := part["image_url"].(string)
	if !ok || len(url) < 5 || !strings.EqualFold(url[:5], "data:") {
		return errors.New("Cloud requires an inline base64 image; remote image URLs are not supported by its upstream protocol")
	}
	// Native Responses forwards image content to its upstream. Cloud likewise
	// preserves the original MIME and bytes: codec support, orientation, animation
	// and dimensions belong to the upstream image processor, not a gateway list.
	// Validate only the inline transport encoding; never rasterize or fetch.
	header, payload, found := strings.Cut(url[5:], ",")
	if !found || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return errors.New("Cloud requires an inline base64 image data URL")
	}
	mediaType, _, err := mime.ParseMediaType(header[:len(header)-len(";base64")])
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return cloudBadRequest("image media type")
	}
	// The base64 decoder skips CR/LF even in strict mode; reject them here.
	if len(payload) == 0 || strings.ContainsAny(payload, "\r\n") {
		return cloudBadRequest("image encoding")
	}
	// Streaming validation avoids allocating a second copy of every image.
	if _, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(payload))); err != nil {
		return cloudBadRequest("image encoding")
	}
	return nil
}

func cloudDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// cloudWebSearchMode maps Codex's web_search controls (see codex-rs
// core/src/tools/hosted_spec.rs) to the App Server WebSearchMode it came from.
// search_content_types follows the model's own search tool type upstream.
func cloudWebSearchMode(tool map[string]any) (string, error) {
	for key := range tool {
		switch key {
		case "type", "external_web_access", "indexed_web_access", "search_content_types":
		default:
			return "", cloudBadRequest("web_search." + key)
		}
	}
	external, hasExternal := tool["external_web_access"]
	indexed, hasIndexed := tool["indexed_web_access"]
	if (hasExternal && external != true && external != false) || (hasIndexed && indexed != true && indexed != false) {
		return "", cloudBadRequest("web_search access")
	}
	if raw, exists := tool["search_content_types"]; exists {
		types, ok := raw.([]any)
		if !ok || len(types) == 0 {
			return "", cloudBadRequest("web_search.search_content_types")
		}
		for _, value := range types {
			if value != "text" && value != "image" {
				return "", cloudBadRequest("web_search.search_content_types")
			}
		}
	}
	switch {
	case indexed == true && external != true:
		return "", cloudBadRequest("web_search access")
	case indexed == true:
		return "indexed", nil
	case hasExternal && external == false:
		return "cached", nil
	default:
		return "live", nil
	}
}

func cloudThreadParams(p *cloudProtocolRequest) map[string]any {
	search := "disabled"
	if p.webSearch {
		search = p.webSearchMode
		if search == "" {
			search = "live"
		}
	}
	config := map[string]any{"features.shell_tool": false, "features.unified_exec": false, "features.apply_patch": false, "features.js_repl": false, "features.plugins": false, "features.apps": false, "features.multi_agent": false, "web_search": search, "features.imagegenext": p.imageGeneration, "features.image_generation": p.imageGeneration, "features.omit_app_server_notification_media": false, "mcp_servers": map[string]any{}}
	// App-server's model_verbosity feeds the same Responses text.verbosity
	// option as the native client. It is fixed for this thread; the existing
	// settings digest rejects incompatible continuation changes.
	if p.verbosity != "" {
		config["model_verbosity"] = p.verbosity
	}
	dynamicTools := []any{}
	if len(p.tools) > 0 {
		dynamicTools = append(dynamicTools, map[string]any{"type": "namespace", "name": cloudClientToolNamespace, "description": "Functions executed by the requesting client.", "tools": p.tools})
		config["features.code_mode.direct_only_tool_namespaces"] = []string{cloudClientToolNamespace}
	}
	return map[string]any{
		"model": p.model, "allowProviderModelFallback": false, "ephemeral": true, "experimentalRawEvents": true, "serviceName": "codex_cloud", "approvalPolicy": "never", "permissions": ":read-only", "dynamicTools": dynamicTools, "environments": []any{}, "baseInstructions": p.instructions,
		"config": config,
	}
}

// Adapt only known client function calls at the injection boundary. The public
// history and continuation comparisons must never gain the private namespace;
// historical calls outside the current client catalog keep their own identity.
// cloudMessageMetadataKey is Codex's attribution metadata (turn ID, creation
// time, content classification) that the Responses API copies into the
// underlying chat messages. It has no execution effect. It is accepted and
// validated but never injected: production showed Cloud discarding a new
// thread whose opening messages carried it (turn IDs of the client's own
// turns), and the thread never needs it. History comparison ignores it.
const cloudMessageMetadataKey = "internal_chat_message_metadata_passthrough"

func cloudValidMessageMetadata(value any) error {
	if value == nil {
		return nil
	}
	metadata, ok := value.(map[string]any)
	encoded, _ := json.Marshal(metadata)
	if !ok || len(encoded) > 16<<10 {
		return cloudBadRequest(cloudMessageMetadataKey)
	}
	return nil
}

// cloudDropNeutralFields validates bounded identifiers and consumes explicit
// defaults after matrix classification. Cloud has no RPC carrier for these
// identifiers; their loss is observable, not an upstream-equivalence claim.
func cloudDropNeutralFields(body map[string]any) error {
	for _, key := range []string{"user", "safety_identifier"} {
		if value, exists := body[key]; exists {
			if text, ok := value.(string); value != nil && (!ok || len(text) > 512) {
				return cloudBadRequest(key)
			}
			delete(body, key)
		}
	}
	defaults := map[string]func(any) bool{
		"service_tier": func(v any) bool { return v == "auto" || v == "default" },
		"background":   func(v any) bool { return v == false },
	}
	for key, isDefault := range defaults {
		if value, exists := body[key]; exists {
			if value != nil && !isDefault(value) {
				return cloudBadRequest(key)
			}
			delete(body, key)
		}
	}
	return nil
}

// Custom (free-form) tools take one text input. Cloud dynamic tools take a JSON
// object, so the input becomes the single string property "input". Grammar
// constraints cannot be enforced upstream; they are stated in the description.
func cloudValidateCustomTool(tool map[string]any) error {
	if name, _ := tool["name"].(string); name == "" || len(name) > 64 {
		return cloudBadRequest("custom tool name")
	}
	if description, exists := tool["description"]; exists && description != nil {
		if _, ok := description.(string); !ok {
			return cloudBadRequest("custom tool description")
		}
	}
	raw, exists := tool["format"]
	if !exists || raw == nil {
		return nil
	}
	format, ok := raw.(map[string]any)
	if !ok {
		return cloudBadRequest("custom tool format")
	}
	switch format["type"] {
	case "text":
		if len(format) != 1 {
			return cloudBadRequest("custom tool format")
		}
	case "grammar":
		definition, _ := format["definition"].(string)
		if len(format) != 3 || (format["syntax"] != "lark" && format["syntax"] != "regex") || definition == "" || len(definition) > 64<<10 {
			return cloudBadRequest("custom tool grammar")
		}
	default:
		return cloudBadRequest("custom tool format")
	}
	return nil
}

func cloudCustomToolSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"input": map[string]any{"type": "string"}}, "required": []any{"input"}, "additionalProperties": false}
}

func cloudCustomToolDescription(description string, format map[string]any) string {
	note := "Call this tool with its complete free-form text in the `input` argument."
	if format["type"] == "grammar" {
		note += fmt.Sprintf(" The `input` must match this %s grammar exactly:\n%s", format["syntax"], format["definition"])
	}
	if description == "" {
		return note
	}
	return description + "\n\n" + note
}

// cloudCustomToolArguments is the one canonical internal arguments encoding of
// a custom tool input, shared by published calls and replayed history.

func cloudCustomToolInput(arguments string) string {
	var value map[string]json.RawMessage
	var input string
	if json.Unmarshal([]byte(arguments), &value) == nil && len(value) == 1 && json.Unmarshal(value["input"], &input) == nil {
		return input
	}
	return arguments
}

func cloudLowerCustomToolItem(item map[string]any) error {
	if item["type"] == "custom_tool_call" {
		for key := range item {
			switch key {
			// namespace and attribution metadata stay on the lowered call and are
			// validated with every other function_call.
			case "type", "id", "status", "call_id", "name", "input", "encrypted_content", "namespace", cloudMessageMetadataKey:
			default:
				return cloudBadRequest("custom_tool_call." + key)
			}
		}
		input, ok := item["input"].(string)
		if !ok {
			return cloudBadRequest("custom_tool_call.input")
		}
		delete(item, "input")
		delete(item, "id")
		delete(item, "status")
		item["type"], item["arguments"] = "function_call", apicompat.CustomToolCallArguments(input)
		return nil
	}
	for key := range item {
		switch key {
		case "type", "id", "status", "call_id", "output", "name", cloudMessageMetadataKey:
		default:
			return cloudBadRequest("custom_tool_call_output." + key)
		}
	}
	delete(item, "id")
	delete(item, "status")
	item["type"] = "function_call_output"
	return nil
}

func cloudForeignActivityRecord(item map[string]any) bool {
	id, _ := item["id"].(string)
	switch item["type"] {
	case "tool_search_call", "tool_search_output":
		callID, _ := item["call_id"].(string)
		return !strings.HasPrefix(callID, "call_cloud_")
	case "web_search_call":
		return !strings.HasPrefix(id, "ws_cloud_")
	case "image_generation_call":
		return !strings.HasPrefix(id, "ig_cloud_")
	}
	return false
}

// cloudHostedHistory accepts a hosted result exactly as this gateway published
// it (ws_cloud_/ig_cloud_ IDs, completed, public fields only). It is completed
// history: Cloud never re-runs it, and replaying it adds no hosted usage.
func cloudHostedHistory(item map[string]any, typ string) error {
	prefix, allowed := "ws_cloud_", map[string]bool{"type": true, "id": true, "status": true, "action": true}
	if typ == "image_generation_call" {
		prefix, allowed = "ig_cloud_", map[string]bool{"type": true, "id": true, "status": true, "result": true, "revised_prompt": true, "output_format": true, "size": true}
	}
	for key, value := range item {
		if key == cloudMessageMetadataKey {
			// Codex attaches attribution to every replayed item; it is not
			// part of the hosted result and is not injected.
			if err := cloudValidMessageMetadata(value); err != nil {
				return err
			}
			continue
		}
		if !allowed[key] {
			return cloudBadRequest(typ + "." + key)
		}
	}
	id, _ := item["id"].(string)
	if !strings.HasPrefix(id, prefix) || len(id) > 128 || item["status"] != "completed" {
		return cloudBadRequest(typ)
	}
	if typ == "image_generation_call" {
		// Apply the same checks as a published result: it is injected as an
		// image, so it must be the complete PNG this gateway could have emitted.
		result, ok := item["result"].(string)
		if !ok {
			return cloudBadRequest(typ + ".result")
		}
		if _, err := cloudGeneratedPNGSize(result); err != nil {
			return cloudBadRequest(typ + ".result")
		}
		for _, key := range []string{"revised_prompt", "output_format", "size"} {
			if value, exists := item[key]; exists {
				if _, ok := value.(string); !ok {
					return cloudBadRequest(typ + "." + key)
				}
			}
		}
		return nil
	}
	action, ok := item["action"].(map[string]any)
	if !ok {
		return cloudBadRequest(typ + ".action")
	}
	canonical := make(map[string]any, len(action))
	for key, value := range action {
		if key == "sources" {
			sources, ok := value.([]any)
			if !ok {
				return cloudBadRequest(typ + ".action.sources")
			}
			for _, raw := range sources {
				source, ok := raw.(map[string]any)
				link, _ := source["url"].(string)
				if !ok || source["type"] != "url" || !cloudPublicURL(link) {
					return cloudBadRequest(typ + ".action.sources")
				}
			}
			continue
		}
		canonical[key] = value
	}
	normalized, err := cloudSearchAction(canonical)
	if err != nil {
		return cloudBadRequest(typ + ".action")
	}
	a, _ := json.Marshal(normalized)
	b, _ := json.Marshal(canonical)
	if string(a) != string(b) {
		return cloudBadRequest(typ + ".action")
	}
	return nil
}

// cloudInjectedHostedItem keeps the upstream ResponseItem fields of a replayed
// hosted result. Include-only search sources and output metadata are public
// projections, not thread history.
func cloudInjectedHostedItem(item map[string]any) map[string]any {
	out := map[string]any{"type": item["type"], "id": item["id"], "status": item["status"]}
	if item["type"] == "image_generation_call" {
		out["result"] = item["result"]
		if prompt, ok := item["revised_prompt"]; ok {
			out["revised_prompt"] = prompt
		}
		return out
	}
	action := make(map[string]any)
	for key, value := range item["action"].(map[string]any) {
		if key != "sources" {
			action[key] = value
		}
	}
	out["action"] = action
	return out
}

func cloudInjectionHistory(p *cloudProtocolRequest) []any {
	items := make([]any, 0, len(p.history))
	for _, raw := range p.history {
		candidate, _ := raw.(map[string]any)
		if candidate != nil && candidate["type"] == "reasoning" && (candidate["encrypted_content"] == nil || candidate["encrypted_content"] == "") {
			continue
		}
		items = append(items, raw)
		index := len(items) - 1
		item, ok := raw.(map[string]any)
		if ok && (item["type"] == "web_search_call" || item["type"] == "image_generation_call") {
			items[index] = cloudInjectedHostedItem(item)
			continue
		}
		if !ok {
			continue
		}
		if summary, summarized, _ := cloudCompactionSummary(item); summarized && item["type"] == "compaction" {
			items[index] = cloudCompactionMessage(summary)
			continue
		}
		copy := make(map[string]any, len(item)+1)
		for key, value := range item {
			if key != cloudMessageMetadataKey {
				copy[key] = value
			}
		}
		// Cloud treats client-supplied item IDs as persisted-item lookups even
		// in inject_items. Real Codex histories fail with "unknown thread"
		// when these IDs are retained. Reuse native's self-contained-item rule
		// on the injection copy only: routing/history IDs, call_id and opaque
		// ciphertext in the public request remain unchanged. Incomplete items
		// are never silently converted into inline content.
		delete(copy, "id")
		if item["type"] == "function_call" {
			name, _ := item["name"].(string)
			namespace, _ := item["namespace"].(string)
			if p.toolNames[name] && namespace == "" {
				copy["namespace"] = cloudClientToolNamespace
			}
		}
		items[index] = copy
	}
	// App-server drops raw system messages from model history. Use the native
	// Responses normalization on this injection-only copy: system guidance stays
	// in input as developer guidance, while the public history keeps its role.
	// The thread's base instructions are fixed; mid-turn guidance belongs here.
	extractSystemMessagesFromInput(map[string]any{"input": items}, false)
	return items
}

func cloudParseUsage(raw json.RawMessage) (*OpenAICloudUsage, error) {
	var value struct {
		Input  *json.Number `json:"inputTokens"`
		Output *json.Number `json:"outputTokens"`
		Cached *json.Number `json:"cachedInputTokens"`
		Total  *json.Number `json:"totalTokens"`
		Write  *json.Number `json:"-"`
		Reason *json.Number `json:"-"`
	}
	var optional struct {
		Write  json.RawMessage `json:"cacheWriteInputTokens"`
		Reason json.RawMessage `json:"reasoningOutputTokens"`
	}
	if json.Unmarshal(raw, &value) != nil || json.Unmarshal(raw, &optional) != nil || value.Input == nil || value.Output == nil || value.Cached == nil || value.Total == nil {
		return nil, errors.New("Cloud token usage is incomplete")
	}
	// App Server declares cacheWriteInputTokens with serde(default): only an
	// absent field is zero by protocol. An explicit null is not a valid count.
	defaulted := func(field json.RawMessage) (*json.Number, error) {
		if len(field) == 0 {
			zero := json.Number("0")
			return &zero, nil
		}
		var number json.Number
		if string(field) == "null" || json.Unmarshal(field, &number) != nil {
			return nil, errors.New("Cloud token usage is incomplete")
		}
		return &number, nil
	}
	write, err := defaulted(optional.Write)
	if err != nil {
		return nil, err
	}
	reason, err := defaulted(optional.Reason)
	if err != nil {
		return nil, err
	}
	value.Write, value.Reason = write, reason
	counts := make([]int, 0, 6)
	for _, number := range []*json.Number{value.Input, value.Output, value.Cached, value.Write, value.Total, value.Reason} {
		text := number.String()
		if len(text) > 64 {
			return nil, errors.New("Cloud token usage number is too large")
		}
		if index := strings.IndexAny(text, "eE"); index >= 0 {
			exponent, err := strconv.Atoi(text[index+1:])
			if err != nil || exponent > 18 || exponent < -18 {
				return nil, errors.New("Cloud token usage exponent is invalid")
			}
		}
		rational, ok := new(big.Rat).SetString(text)
		if !ok || rational.Sign() < 0 || !rational.IsInt() || rational.Num().BitLen() > 53 {
			return nil, errors.New("Cloud token usage is not an exact nonnegative integer")
		}
		counts = append(counts, int(rational.Num().Int64()))
	}
	u := &OpenAICloudUsage{InputTokens: counts[0], OutputTokens: counts[1], CachedInputTokens: counts[2], CacheWriteInputTokens: counts[3], TotalTokens: counts[4], ReasoningOutputTokens: counts[5]}
	if u.ReasoningOutputTokens > u.OutputTokens {
		return nil, errors.New("Cloud token usage is inconsistent")
	}
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CachedInputTokens < 0 || u.CacheWriteInputTokens < 0 || u.TotalTokens < 0 || u.CachedInputTokens > u.InputTokens || u.CacheWriteInputTokens > u.InputTokens-u.CachedInputTokens || u.InputTokens > int(^uint(0)>>1)-u.OutputTokens || u.TotalTokens != u.InputTokens+u.OutputTokens {
		return nil, errors.New("Cloud token usage is inconsistent")
	}
	return u, nil
}

func cloudUsageDifference(total, base *OpenAICloudUsage) (*OpenAICloudUsage, error) {
	if total == nil {
		return nil, nil
	}
	if base == nil {
		return total, nil
	}
	u := &OpenAICloudUsage{InputTokens: total.InputTokens - base.InputTokens, OutputTokens: total.OutputTokens - base.OutputTokens, CachedInputTokens: total.CachedInputTokens - base.CachedInputTokens, CacheWriteInputTokens: total.CacheWriteInputTokens - base.CacheWriteInputTokens, TotalTokens: total.TotalTokens - base.TotalTokens, ReasoningOutputTokens: total.ReasoningOutputTokens - base.ReasoningOutputTokens}
	if u.ReasoningOutputTokens < 0 || u.ReasoningOutputTokens > u.OutputTokens {
		return nil, errors.New("Cloud token usage moved backwards")
	}
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CachedInputTokens < 0 || u.CacheWriteInputTokens < 0 || u.TotalTokens < 0 || u.CachedInputTokens > u.InputTokens || u.CacheWriteInputTokens > u.InputTokens-u.CachedInputTokens || u.TotalTokens != u.InputTokens+u.OutputTokens {
		return nil, errors.New("Cloud token usage moved backwards")
	}
	return u, nil
}

func cloudResponseUsage(u *OpenAICloudUsage) any {
	if u == nil {
		return nil
	}
	return map[string]any{"input_tokens": u.InputTokens, "output_tokens": u.OutputTokens, "total_tokens": u.TotalTokens,
		"input_tokens_details":  map[string]any{"cached_tokens": u.CachedInputTokens, "cache_creation_tokens": u.CacheWriteInputTokens},
		"output_tokens_details": map[string]any{"reasoning_tokens": u.ReasoningOutputTokens}}
}

func cloudRestorePayload(raw []byte, names map[string]apicompat.ResponsesNamespaceName, toolSearch ...bool) json.RawMessage {
	if len(toolSearch) > 0 && toolSearch[0] {
		restored, _, err := apicompat.RestoreResponsesClientToolPayload(raw, apicompat.ResponsesClientToolMapping{ToolSearch: true, NamespaceTools: names})
		if err == nil {
			return restored
		}
	}
	out, err := restoreOpenAIResponsesNamespacePayloadWithNames(raw, names)
	if err != nil {
		return raw
	}
	return out
}

func cloudIsResponseID(id string) bool { return strings.HasPrefix(id, "resp_cloud_") }

func cloudUniqueJSON(raw []byte) bool { _, why := validateOpenAIJSONObject(raw); return why == "" }

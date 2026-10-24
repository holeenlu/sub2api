package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

// Cloud's programmatic exec wrapper can serialize dynamic-tool results as text.
// This private direct-only namespace uses the verified multimodal tool-result
// protocol; public names and namespaces still use the existing mapping.
const cloudClientToolNamespace = "sub2api_client"

var errCloudOriginalToolImage = errors.New("Cloud dynamic tool replies cannot preserve image detail original; use detail high or the native protocol")

type cloudProtocolRequest struct {
	body                      map[string]any
	history                   []any
	input                     []any
	inputMessage              map[string]any
	outputs                   map[string]string // JSON-encoded original outputs for idempotency only.
	outputItems               map[string][]any
	outputHistory             map[string]any
	originalToolImages        map[string]bool
	tools                     []any
	toolNames                 map[string]bool
	customTools               map[string]bool // free-form tools mapped to one string input
	names                     map[string]apicompat.ResponsesNamespaceName
	previous                  string
	model                     string
	instructions              string
	effort                    string
	summary                   string
	verbosity                 string
	settings                  string
	threadSettings            string
	serialTools               bool
	outputSchema              map[string]any
	webSearch                 bool
	webSearchMode             string // App Server web_search mode: cached, indexed or live
	imageGeneration           bool
	includeEncryptedReasoning bool
	includeSearchSources      bool
}

func cloudBadRequest(field string) error {
	if len(field) > 64 || strings.IndexFunc(field, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		field = "request field"
	}
	return fmt.Errorf("Cloud does not support the supplied %s semantics", field)
}

func parseCloudProtocolRequest(raw []byte) (*cloudProtocolRequest, error) {
	if len(raw) == 0 || len(raw) > OpenAICloudMaxRequestBytes {
		return nil, cloudBadRequest("request size")
	}
	if !cloudUniqueJSON(raw) {
		return nil, cloudBadRequest("duplicate or invalid JSON")
	}
	var body map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&body) != nil || body == nil {
		return nil, cloudBadRequest("request")
	}
	if err := cloudDropNeutralFields(body); err != nil {
		return nil, err
	}
	allowed := map[string]bool{"model": true, "input": true, "instructions": true, "tools": true, "tool_choice": true, "parallel_tool_calls": true, "reasoning": true, "stream": true, "store": true, "previous_response_id": true, "metadata": true, "client_metadata": true, "prompt_cache_key": true, "text": true, "include": true}
	for key := range body {
		if !allowed[key] {
			return nil, cloudBadRequest(key)
		}
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
	p := &cloudProtocolRequest{body: body, outputs: make(map[string]string), outputItems: make(map[string][]any), outputHistory: make(map[string]any), toolNames: make(map[string]bool)}
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
				p.includeEncryptedReasoning = true
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
	// Validate before the shared namespace lowering, which intentionally skips
	// unsupported child types for other adapters. Cloud must never drop one.
	if rawTools, ok := body["tools"]; ok && rawTools != nil {
		tools, valid := rawTools.([]any)
		if !valid || len(tools) > 128 {
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
					if !ok || m["type"] != "function" {
						return nil, cloudBadRequest("namespace child")
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
				// Client tool search adds tool definitions during a turn. Cloud
				// registers dynamic tools once per thread, so it cannot honor them.
				return nil, errors.New("Cloud cannot load tools during a turn: tool_search is not supported; send the tools directly or use the native protocol")
			case "function":
			default:
				return nil, cloudBadRequest("tool type")
			}
		}
	}
	names, _, err := apicompat.FlattenResponsesNamespaces(body)
	if err != nil {
		return nil, cloudBadRequest("namespace collision")
	}
	p.names = names
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
			for key := range tool {
				switch key {
				case "type", "name", "description", "parameters", "strict":
				default:
					return nil, cloudBadRequest("tool." + key)
				}
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
	if len(items) == 0 || len(items) > 4096 {
		return nil, cloudBadRequest("input")
	}
	for _, rawItem := range items {
		// The image bound applies per message or tool output: store:false
		// clients resend every earlier image, which must not exhaust it.
		imageBudget := cloudImageBudget{}
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, cloudBadRequest("input item")
		}
		typ, _ := item["type"].(string)
		if typ == "reasoning" || typ == "compaction" {
			if err := cloudEncryptedHistory(item, typ); err != nil {
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
			content, err := cloudMessageContent(item["content"], role, &imageBudget)
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
				parts, err := cloudMessageContent(output, "user", &imageBudget)
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
	p.history = items
	if last, ok := items[len(items)-1].(map[string]any); ok && last["type"] == "message" && last["role"] == "user" {
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
	settings := make(map[string]any)
	for _, key := range []string{"model", "instructions", "tools", "tool_choice", "parallel_tool_calls", "reasoning", "text"} {
		if v, ok := body[key]; ok {
			settings[key] = v
		}
	}
	encoded, _ := json.Marshal(settings)
	p.settings = cloudDigest(encoded)
	delete(settings, "reasoning")           // effort/summary are turn-level overrides.
	delete(settings, "parallel_tool_calls") // Local delivery policy may change between turns.
	if text, ok := body["text"].(map[string]any); ok {
		threadText := make(map[string]any)
		if verbosity, ok := text["verbosity"]; ok {
			threadText["verbosity"] = verbosity
		}
		if len(threadText) == 0 {
			delete(settings, "text")
		} else {
			settings["text"] = threadText
		}
	}
	encoded, _ = json.Marshal(settings)
	p.threadSettings = cloudDigest(encoded)
	return p, nil
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

func cloudMessageContent(raw any, role string, images *cloudImageBudget) ([]any, error) {
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
			if err := images.validate(part); err != nil {
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
		for key := range part {
			if key != "type" && key != "text" && !(key == "annotations" && role == "assistant" && typ == "output_text") {
				return nil, cloudBadRequest("message content." + key)
			}
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

type cloudImageBudget struct {
	count int
}

func (b *cloudImageBudget) validate(part map[string]any) error {
	if _, exists := part["file_id"]; exists {
		return errors.New("Cloud cannot resolve client image file_id ownership; send an inline base64 image instead")
	}
	for key := range part {
		if key != "type" && key != "image_url" && key != "detail" {
			return cloudBadRequest("image." + key)
		}
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
	// Validate only the bounded inline transport envelope; never rasterize or fetch.
	if b.count >= 8 {
		return cloudBadRequest("image count")
	}
	header, payload, found := strings.Cut(url[5:], ",")
	if !found || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return errors.New("Cloud requires an inline base64 image data URL")
	}
	mediaType, _, err := mime.ParseMediaType(header[:len(header)-len(";base64")])
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return cloudBadRequest("image media type")
	}
	const maxBytes = 3 << 20
	if len(payload) == 0 || len(payload) > base64.StdEncoding.EncodedLen(maxBytes) || strings.ContainsAny(payload, "\r\n") {
		return cloudBadRequest("image size or encoding")
	}
	// Streaming validation avoids allocating a second copy of every image.
	size, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(payload)))
	if err != nil || size > maxBytes {
		return cloudBadRequest("image encoding")
	}
	b.count++
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
// underlying chat messages. It has no execution effect. App Server items carry
// the same field, so history passes it through; the final user message has no
// turn/start field for it, and history comparison ignores it.
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

// cloudDropNeutralFields removes client-attached identifiers and explicit
// Responses defaults. They have no execution effect, so accepting them changes
// nothing; any non-default value is execution semantics Cloud cannot honor.
func cloudDropNeutralFields(body map[string]any) error {
	for _, key := range []string{"user", "safety_identifier"} {
		if value, exists := body[key]; exists {
			if text, ok := value.(string); value != nil && (!ok || len(text) > 512) {
				return cloudBadRequest(key)
			}
			delete(body, key)
		}
	}
	if value, exists := body["stream_options"]; exists {
		if value != nil {
			options, ok := value.(map[string]any)
			if !ok {
				return cloudBadRequest("stream_options")
			}
			for key, option := range options {
				switch key {
				case "include_obfuscation":
					if _, isBool := option.(bool); option != nil && !isBool {
						return cloudBadRequest("stream_options." + key)
					}
				case "reasoning_summary_delivery":
					// Codex concurrent reasoning summaries ask that summaries be
					// delivered as completed parts, never as interleaved deltas.
					// Cloud publishes each summary only when its item completes.
					if option != nil && option != "sequential_cutoff" {
						return cloudBadRequest("stream_options." + key)
					}
				default:
					return cloudBadRequest("stream_options." + key)
				}
			}
		}
		delete(body, "stream_options")
	}
	defaults := map[string]func(any) bool{
		"service_tier": func(v any) bool { return v == "auto" || v == "default" },
		"truncation":   func(v any) bool { return v == "disabled" },
		"background":   func(v any) bool { return v == false },
		"top_logprobs": func(v any) bool { n, ok := v.(json.Number); return ok && n.String() == "0" },
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
	for key := range tool {
		switch key {
		case "type", "name", "description", "format":
		default:
			return cloudBadRequest("custom tool." + key)
		}
	}
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
func cloudCustomToolArguments(input string) string {
	encoded, _ := json.Marshal(map[string]string{"input": input})
	return string(encoded)
}

// cloudCustomToolInput recovers the public input from a dynamic tool call. A
// call that does not follow the declared schema is returned unchanged.
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
			case "type", "id", "status", "call_id", "name", "input", "encrypted_content":
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
		item["type"], item["arguments"] = "function_call", cloudCustomToolArguments(input)
		return nil
	}
	for key := range item {
		switch key {
		case "type", "id", "status", "call_id", "output":
		default:
			return cloudBadRequest("custom_tool_call_output." + key)
		}
	}
	delete(item, "id")
	delete(item, "status")
	item["type"] = "function_call_output"
	return nil
}

// cloudHostedHistory accepts a hosted result exactly as this gateway published
// it (ws_cloud_/ig_cloud_ IDs, completed, public fields only). It is completed
// history: Cloud never re-runs it, and replaying it adds no hosted usage.
func cloudHostedHistory(item map[string]any, typ string) error {
	prefix, allowed := "ws_cloud_", map[string]bool{"type": true, "id": true, "status": true, "action": true}
	if typ == "image_generation_call" {
		prefix, allowed = "ig_cloud_", map[string]bool{"type": true, "id": true, "status": true, "result": true, "revised_prompt": true, "output_format": true, "size": true}
	}
	for key := range item {
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
	items := make([]any, len(p.history))
	for index, raw := range p.history {
		items[index] = raw
		item, ok := raw.(map[string]any)
		if ok && (item["type"] == "web_search_call" || item["type"] == "image_generation_call") {
			items[index] = cloudInjectedHostedItem(item)
			continue
		}
		if !ok || item["type"] != "function_call" {
			continue
		}
		name, _ := item["name"].(string)
		namespace, _ := item["namespace"].(string)
		if !p.toolNames[name] || namespace != "" {
			continue
		}
		copy := make(map[string]any, len(item)+1)
		for key, value := range item {
			copy[key] = value
		}
		copy["namespace"] = cloudClientToolNamespace
		items[index] = copy
	}
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

func cloudRestorePayload(raw []byte, names map[string]apicompat.ResponsesNamespaceName) json.RawMessage {
	out, _, err := apicompat.RestoreResponsesNamespaceCalls(raw, names)
	if err != nil {
		return raw
	}
	return out
}

func cloudResponseReferences(raw []byte) (string, []string, error) {
	var body map[string]json.RawMessage
	if len(raw) > OpenAICloudMaxRequestBytes || !cloudUniqueJSON(raw) || json.Unmarshal(raw, &body) != nil {
		return "", nil, cloudBadRequest("request")
	}
	var previous string
	if v := body["previous_response_id"]; len(v) > 0 && string(v) != "null" {
		if json.Unmarshal(v, &previous) != nil {
			return "", nil, cloudBadRequest("previous_response_id")
		}
	}
	var input []struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
	}
	_ = json.Unmarshal(body["input"], &input)
	var calls []string
	for _, item := range input {
		if item.Type == "function_call_output" || item.Type == "custom_tool_call_output" {
			if item.CallID == "" {
				return "", nil, cloudBadRequest("call_id")
			}
			calls = append(calls, item.CallID)
		}
	}
	return previous, calls, nil
}

func cloudIsResponseID(id string) bool { return strings.HasPrefix(id, "resp_cloud_") }

func cloudUniqueJSON(raw []byte) bool {
	if !gjson.ValidBytes(raw) || !openAIIncrementalUnicode(raw) {
		return false
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() {
		return false
	}
	budget := 131072
	var walk func(gjson.Result, int) bool
	walk = func(node gjson.Result, depth int) bool {
		budget--
		if depth > 32 || budget < 0 || (node.IsObject() && hasDuplicateJSONObjectKeys(node)) {
			return false
		}
		valid := true
		if node.IsObject() || node.IsArray() {
			node.ForEach(func(_, v gjson.Result) bool { valid = walk(v, depth+1); return valid })
		}
		return valid
	}
	return walk(root, 0)
}

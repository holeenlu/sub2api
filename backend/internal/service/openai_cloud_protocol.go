package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"math/big"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

// Cloud's programmatic exec wrapper can serialize dynamic-tool results as text.
// This private direct-only namespace uses the verified multimodal tool-result
// protocol; public names and namespaces still use the existing mapping.
const cloudClientToolNamespace = "sub2api_client"

type cloudProtocolRequest struct {
	body                      map[string]any
	history                   []any
	input                     []any
	inputMessage              map[string]any
	outputs                   map[string]string // JSON-encoded original outputs for idempotency only.
	outputItems               map[string][]any
	outputHistory             map[string]any
	tools                     []any
	toolNames                 map[string]bool
	names                     map[string]apicompat.ResponsesNamespaceName
	previous                  string
	model                     string
	instructions              string
	effort                    string
	summary                   string
	verbosity                 string
	settings                  string
	webSearch                 bool
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
			if !valid || (key == "store" && b) || (key == "parallel_tool_calls" && !b) {
				return nil, cloudBadRequest(key)
			}
		}
	}
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
			if !valid || len(format) != 1 || format["type"] != "text" {
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
			case "low", "medium", "high", "xhigh", "max":
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
				// These bare declarations are the verified Cloud contract. Do
				// not silently discard provider-specific hosted tool controls.
				if len(tool) != 1 {
					return nil, cloudBadRequest("hosted tool controls")
				}
				if tool["type"] == "web_search" {
					if p.webSearch {
						return nil, cloudBadRequest("duplicate hosted tool")
					}
					p.webSearch = true
				} else {
					if p.imageGeneration {
						return nil, cloudBadRequest("duplicate hosted tool")
					}
					p.imageGeneration = true
				}
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
	imageBudget := cloudImageBudget{}
	for _, rawItem := range items {
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
		for key := range item {
			switch key {
			case "type", "role", "content", "id", "status", "call_id", "name", "namespace", "arguments", "output", "phase":
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
				p.input = append(p.input, map[string]any{"type": "image", "url": part["image_url"], "detail": "auto"})
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
	count  int
	pixels int64
}

func (b *cloudImageBudget) validate(part map[string]any) error {
	for key := range part {
		if key != "type" && key != "image_url" && key != "detail" {
			return cloudBadRequest("image." + key)
		}
	}
	if detail, exists := part["detail"]; exists && detail != "auto" {
		return cloudBadRequest("image detail")
	}
	url, ok := part["image_url"].(string)
	const prefix = "data:image/png;base64,"
	if !ok || !strings.HasPrefix(url, prefix) {
		return cloudBadRequest("inline PNG image")
	}
	encoded := url[len(prefix):]
	const maxBytes = 3 << 20
	if len(encoded) == 0 || len(encoded) > base64.StdEncoding.EncodedLen(maxBytes) || strings.ContainsAny(encoded, "\r\n") {
		return cloudBadRequest("image size or encoding")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > maxBytes {
		return cloudBadRequest("image encoding")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 {
		return cloudBadRequest("image dimensions or format")
	}
	pixels := int64(config.Width) * int64(config.Height)
	// Bound decompression work over all current and historical user/tool images,
	// including very small PNGs whose decoded bitmaps are much larger.
	if b.count >= 8 || pixels > (16<<20)-b.pixels {
		return cloudBadRequest("image count or pixels")
	}
	reader := bytes.NewReader(data)
	if _, err := png.Decode(reader); err != nil || reader.Len() != 0 {
		return cloudBadRequest("incomplete or invalid PNG image")
	}
	b.count++
	b.pixels += pixels
	return nil
}

func cloudDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func cloudThreadParams(p *cloudProtocolRequest) map[string]any {
	search := "disabled"
	if p.webSearch {
		search = "live"
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
		"model": p.model, "allowProviderModelFallback": false, "ephemeral": false, "experimentalRawEvents": true, "serviceName": "codex_cloud", "approvalPolicy": "never", "permissions": ":read-only", "dynamicTools": dynamicTools, "environments": []any{}, "baseInstructions": p.instructions,
		"config": config,
	}
}

// Adapt only known client function calls at the injection boundary. The public
// history and continuation comparisons must never gain the private namespace;
// historical calls outside the current client catalog keep their own identity.
func cloudInjectionHistory(p *cloudProtocolRequest) []any {
	items := make([]any, len(p.history))
	for index, raw := range p.history {
		items[index] = raw
		item, ok := raw.(map[string]any)
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
		Write  *json.Number `json:"cacheWriteInputTokens"`
		Total  *json.Number `json:"totalTokens"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Input == nil || value.Output == nil || value.Cached == nil || value.Write == nil || value.Total == nil {
		return nil, errors.New("Cloud token usage is incomplete")
	}
	counts := make([]int, 0, 5)
	for _, number := range []*json.Number{value.Input, value.Output, value.Cached, value.Write, value.Total} {
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
	u := &OpenAICloudUsage{InputTokens: counts[0], OutputTokens: counts[1], CachedInputTokens: counts[2], CacheWriteInputTokens: counts[3], TotalTokens: counts[4]}
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
	u := &OpenAICloudUsage{InputTokens: total.InputTokens - base.InputTokens, OutputTokens: total.OutputTokens - base.OutputTokens, CachedInputTokens: total.CachedInputTokens - base.CachedInputTokens, CacheWriteInputTokens: total.CacheWriteInputTokens - base.CacheWriteInputTokens, TotalTokens: total.TotalTokens - base.TotalTokens}
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CachedInputTokens < 0 || u.CacheWriteInputTokens < 0 || u.TotalTokens < 0 || u.CachedInputTokens > u.InputTokens || u.CacheWriteInputTokens > u.InputTokens-u.CachedInputTokens || u.TotalTokens != u.InputTokens+u.OutputTokens {
		return nil, errors.New("Cloud token usage moved backwards")
	}
	return u, nil
}

func cloudResponseUsage(u *OpenAICloudUsage) any {
	if u == nil {
		return nil
	}
	return map[string]any{"input_tokens": u.InputTokens, "output_tokens": u.OutputTokens, "total_tokens": u.TotalTokens, "input_tokens_details": map[string]any{"cached_tokens": u.CachedInputTokens, "cache_creation_tokens": u.CacheWriteInputTokens}}
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
		if item.Type == "function_call_output" {
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

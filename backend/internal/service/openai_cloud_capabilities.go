package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// cloudCapability is the single protocol policy source. It is package-private,
// immutable after initialization, and shared by normalization and catalog projection.
type cloudCapability struct {
	ID, Scope, Path, Action, Category, Catalog, Remedy, Evidence string
	Executive                                                    bool
	ErrorCode                                                    string
}

var cloudCapabilities = [...]cloudCapability{
	{"CLOUD-REQ", "request", "*", "drop", "policy", "", "Remove unsupported execution controls or use the native protocol.", "OPENAI_CLOUD_PARSER_DESIGN D1", false, "cloud_invalid_request"},
	{"CLOUD-TOOL", "tool", "tools[]", "map", "protocol-fact", "experimental_supported_tools=[]", "Use function/custom tools declared in full.", "cloudThreadParams/dynamicTools", true, "cloud_unsupported_parameter"},
	{"CLOUD-CHOICE", "request", "tool_choice", "map", "policy", "", "Use auto or none.", "turn/start has no forced-tool mapping", true, "cloud_unsupported_parameter"},
	{"CLOUD-STORE", "request", "store", "default-only", "policy", "", "Use store:false and resend complete history.", "ephemeral Cloud threads", true, "cloud_unsupported_parameter"},
	{"CLOUD-FORMAT", "format", "text.format", "map", "protocol-fact", "", "Use text or json_schema; non-strict schemas are best-effort per-turn guidance.", "turn/start outputSchema or per-turn schema guidance", true, "cloud_unsupported_parameter"},
	{"CLOUD-TIER", "request", "service_tier", "default-only", "policy", "service_tiers=[]", "Use auto/default until service tier billing is mapped.", "OPENAI_CLOUD_PARSER_DESIGN D1", true, "cloud_unsupported_parameter"},
	{"CLOUD-SEARCH", "tool", "tools[].type=tool_search", "map", "observed", "supports_search_tool=false", "Use client execution; catalog changes need all pending results and confirmed interruption/usage.", "2026-10-25 migrated discovery live verification", true, "cloud_unsupported_parameter"},
	{"CLOUD-LITE", "item", "input[].type=additional_tools", "map", "protocol-fact", "use_responses_lite=false", "Send tools directly.", "Codex IncrementalTools", true, "cloud_invalid_request"},
	{"CLOUD-CONFIG", "item", "input[].type=configuration_update", "drop", "policy", "supports_reasoning_effort_updates=false", "Supply reasoning on the new request.", "history is not current configuration", false, "cloud_invalid_request"},
	{"CLOUD-AGENT", "item", "input[].type=agent_message", "map", "policy", "multi_agent_version=null;multi_agent_reasoning_effort=null", "Supply readable messages.", "Codex plaintext_agent_message_content", true, "cloud_invalid_request"},
	{"CLOUD-CODE", "tool", "tools[].type=code_mode", "reject", "policy", "tool_mode=null", "Use directly declared function/custom tools.", "Cloud direct tool namespace", true, "cloud_unsupported_parameter"},
	{"CLOUD-HISTORY", "item", "input[]", "map", "policy", "", "Resend complete, readable history with matching call IDs.", "Cloud owner/pending RPC contract", true, "cloud_invalid_request"},
	{"CLOUD-CIPHER", "item", "input[].encrypted_content", "map", "observed", "", "Resend readable complete history if the cipher is rejected.", "OC-1/OC-2", true, "cloud_invalid_request"},
	{"CLOUD-IMAGE", "part", "input[].content[].type=input_image", "map", "observed", "", "Use an inline base64 image; original tool results require a confirmed history handoff.", "OC-5/OC-6 and same-root tool history handoff", true, "cloud_unsupported_parameter"},
	{"CLOUD-PART", "part", "input[].content[]", "map", "policy", "", "Use text or supported inline image content.", "Cloud turn and dynamic reply mapping", true, "cloud_invalid_request"},
	{"CLOUD-META", "request", "client_metadata,prompt_cache_key,user,safety_identifier", "drop", "policy", "", "These client hints have no Cloud RPC carrier.", "OPENAI_CLOUD_PARSER_DESIGN D1", false, "cloud_invalid_request"},
	{"CLOUD-REASONING", "reasoning", "reasoning", "map", "protocol-fact", "", "Use a supported effort/summary.", "turn/start effort and summary", true, "cloud_invalid_request"},
	{"CLOUD-INCLUDE", "request", "include", "map", "policy", "", "Only available response projections are returned.", "Cloud response projection", false, "cloud_invalid_request"},
	{"CLOUD-CUSTOM", "custom-format", "tools[].format", "map", "policy", "", "Grammar is a description hint; the client validates its output.", "OPENAI_CLOUD_PARSER_DESIGN D5", true, "cloud_invalid_request"},
	{"CLOUD-JSON", "envelope", "$", "reject", "policy", "", "Send an unambiguous bounded JSON object.", "shared JSON envelope validation", true, "cloud_invalid_request"},
	{"CLOUD-RECOVERY", "continuation", "history.inject", "map", "observed", "", "Resend readable complete history.", "OPENAI_CLOUD_PARSER_DESIGN D6/OC-2", true, "cloud_history_rejected"},
}

// Field definitions are part of the matrix, not a second ingress allow-list.
// Unknown non-execution fields are removed with a Decision. Known execution
// shapes still undergo structural and ownership validation in the compiler/actor.
var cloudScopeFields = map[string]string{
	"request":                 "model input instructions tools tool_choice parallel_tool_calls reasoning stream store previous_response_id metadata client_metadata prompt_cache_key user safety_identifier text include service_tier background conversation prompt",
	"reasoning":               "effort summary",
	"text":                    "verbosity format",
	"format":                  "type name schema strict",
	"function":                "type name description parameters strict defer_loading",
	"custom":                  "type name description format defer_loading",
	"namespace":               "type name description tools defer_loading",
	"web_search":              "type external_web_access indexed_web_access search_content_types filters user_location search_context_size",
	"image_generation":        "type size quality background output_format output_compression moderation partial_images input_fidelity",
	"tool_search":             "type description execution parameters",
	"custom-format":           "type syntax definition",
	"message":                 "type role content id status call_id encrypted_content phase " + cloudMessageMetadataKey,
	"function_call":           "type id status call_id name namespace arguments encrypted_content encrypted_function_args " + cloudMessageMetadataKey,
	"function_call_output":    "type id status call_id name namespace output " + cloudMessageMetadataKey,
	"custom_tool_call":        "type id status call_id name namespace input encrypted_content encrypted_function_args " + cloudMessageMetadataKey,
	"custom_tool_call_output": "type id status call_id name namespace output " + cloudMessageMetadataKey,
	"reasoning-item":          "type id status encrypted_content summary content " + cloudMessageMetadataKey,
	"compaction":              "type id status encrypted_content " + cloudMessageMetadataKey,
	"agent_message":           "type id author recipient content " + cloudMessageMetadataKey,
	"configuration_update":    "type id reasoning " + cloudMessageMetadataKey,
	"additional_tools":        "type id role tools content",
	"compaction_trigger":      "type mode",
	"input_text":              "type text",
	"output_text":             "type text annotations",
	"input_image":             "type image_url file_id detail",
	"summary_text":            "type text",
	"encrypted_content":       "type encrypted_content",
}

type cloudAdaptationDecision struct {
	Path     string `json:"path"`
	Action   string `json:"action"`
	MatrixID string `json:"matrix_id"`
	Omitted  int    `json:"omitted,omitempty"`
}
type cloudRejection struct {
	Code      string
	Param     string
	MatrixID  string
	Remedy    string
	reason    string
	Decisions []cloudAdaptationDecision
}

func (e *cloudRejection) Error() string {
	if e.reason == "" {
		return fmt.Sprintf("Cloud does not support the supplied %s semantics: %s", e.Param, e.Remedy)
	}
	return fmt.Sprintf("Cloud does not support the supplied %s semantics: %s", e.reason, e.Remedy)
}

func (e *cloudRejection) RequestScoped() bool { return true }
func cloudCapabilityByID(id string) cloudCapability {
	for _, rule := range cloudCapabilities {
		if rule.ID == id {
			return rule
		}
	}
	panic("unregistered Cloud capability: " + id)
}
func cloudRuleForScope(scope string) string {
	switch scope {
	case "function", "namespace", "web_search", "image_generation":
		return "CLOUD-TOOL"
	case "custom", "custom-format":
		return "CLOUD-CUSTOM"
	case "tool_search":
		return "CLOUD-SEARCH"
	case "format", "text":
		return "CLOUD-FORMAT"
	case "reasoning":
		return "CLOUD-REASONING"
	case "input_image":
		return "CLOUD-IMAGE"
	case "input_text", "output_text", "summary_text", "encrypted_content":
		return "CLOUD-PART"
	case "request":
		return "CLOUD-REQ"
	default:
		return "CLOUD-HISTORY"
	}
}
func cloudDecision(out *[]cloudAdaptationDecision, path, action, id string) {
	// Paths are schema locations only: arbitrary client keys never enter telemetry.
	decision := cloudAdaptationDecision{Path: path, Action: action, MatrixID: id}
	for _, old := range *out {
		if old == decision {
			return
		}
	}
	if len(*out) >= cloudAdaptationMaxDecisions {
		last := &(*out)[len(*out)-1]
		if last.Omitted == 0 {
			*last = cloudAdaptationDecision{Path: "$", Action: "drop", MatrixID: "CLOUD-REQ", Omitted: 2}
		} else {
			last.Omitted++
		}
		return
	}
	*out = append(*out, decision)
}
func cloudNormalizeFields(m map[string]any, scope, path string, out *[]cloudAdaptationDecision) error {
	fields, known := cloudScopeFields[scope]
	if !known {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.Contains(" "+fields+" ", " "+key+" ") {
			continue
		}
		if scope != "configuration_update" && strings.Contains(" encrypted_content encrypted_function_args parameters input_schema tools tool_choice format schema output arguments call_id input function ", " "+key+" ") {
			rule := cloudCapabilityByID(cloudRuleForScope(scope))
			param := path + "." + key
			return &cloudRejection{Code: rule.ErrorCode, Param: param, MatrixID: rule.ID, Remedy: rule.Remedy, Decisions: []cloudAdaptationDecision{{Path: param, Action: "reject", MatrixID: rule.ID}}}
		}
		delete(m, key)
		cloudDecision(out, path+".*", "drop", cloudRuleForScope(scope))
	}
	return nil
}
func cloudNormalizeContent(raw any, path string, out *[]cloudAdaptationDecision) error {
	parts, _ := raw.([]any)
	for i, v := range parts {
		if part, ok := v.(map[string]any); ok {
			typ, _ := part["type"].(string)
			if err := cloudNormalizeFields(part, typ, fmt.Sprintf("%s[%d]", path, i), out); err != nil {
				return err
			}
		}
	}
	return nil
}
func cloudNormalizeTools(raw any, path string, out *[]cloudAdaptationDecision) error {
	tools, _ := raw.([]any)
	for i, v := range tools {
		if tool, ok := v.(map[string]any); ok {
			at := fmt.Sprintf("%s[%d]", path, i)
			typ, _ := tool["type"].(string)
			// Shared Chat Completions function promotion; unlike native's permissive
			// wrapper, never discard an invalid executable declaration.
			if typ == "function" {
				if f, ok := tool["function"].(map[string]any); ok {
					for _, key := range []string{"name", "description", "parameters", "strict"} {
						if value, present := f[key]; present {
							tool[key] = value
						}
					}
					delete(tool, "function")
					cloudDecision(out, at, "map", "CLOUD-TOOL")
				}
			}
			if typ != "web_search" && typ != "image_generation" {
				if err := cloudNormalizeFields(tool, typ, at, out); err != nil {
					return err
				}
			}
			if typ == "tool_search" {
				if schema, exists := tool["parameters"]; exists && schema != nil {
					object, valid := schema.(map[string]any)
					if !valid {
						return cloudBadRequest("tool_search.parameters")
					}
					normalizeOpenAIResponsesToolSchemaMap(object)
				}
				cloudDecision(out, at, "map", "CLOUD-SEARCH")
			}
			if typ == "namespace" {
				if err := cloudNormalizeTools(tool["tools"], at+".tools", out); err != nil {
					return err
				}
			}
			if typ == "function" {
				if schema, exists := tool["parameters"]; exists && schema == nil {
					tool["parameters"] = map[string]any{"type": "object"}
					cloudDecision(out, at+".parameters", "map", "CLOUD-TOOL")
				}
				if schema, ok := tool["parameters"].(map[string]any); ok {
					if normalizeOpenAIResponsesToolSchemaMap(schema) {
						cloudDecision(out, at+".parameters", "map", "CLOUD-TOOL")
					}
				}
			}
			if typ == "custom" {
				if format, ok := tool["format"].(map[string]any); ok {
					if err := cloudNormalizeFields(format, "custom-format", at+".format", out); err != nil {
						return err
					}
					if format["type"] == "grammar" {
						cloudDecision(out, at+".format", "map", "CLOUD-CUSTOM")
					}
				}
			}
		}
	}
	return nil
}
func cloudNormalizeRequest(body map[string]any, out *[]cloudAdaptationDecision) (err error) {
	defer func() { err = cloudErrorWithDecisions(err, *out) }()
	if body["conversation"] != nil || body["prompt"] != nil {
		return cloudBadRequest("persisted conversation or prompt")
	}

	for scope, keys := range map[string][]string{
		"request":     {"stream_options", "truncation", "top_logprobs", "max_output_tokens", "max_completion_tokens", "temperature", "top_p", "max_tool_calls", "prompt_cache_retention", "access_programs"},
		"reasoning":   {"context", "mode", "generate_summary"},
		"text.format": {"description"},
	} {
		target := body
		for _, key := range strings.Split(scope, ".") {
			if key == "request" {
				continue
			}
			m, ok := target[key].(map[string]any)
			if !ok {
				target = nil
				break
			}
			target = m
		}
		for _, key := range keys {
			if _, exists := target[key]; exists {
				delete(target, key)
				path := key
				if scope != "request" {
					path = scope + "." + key
				}
				cloudDecision(out, path, "drop", cloudRuleForScope(strings.Split(scope, ".")[0]))
			}
		}
	}
	if err := cloudNormalizeFields(body, "request", "$", out); err != nil {
		return err
	}
	for _, key := range []string{"client_metadata", "prompt_cache_key", "user", "safety_identifier"} {
		if _, ok := body[key]; ok {
			cloudDecision(out, key, "drop", "CLOUD-META")
		}
	}
	for key, scope := range map[string]string{"reasoning": "reasoning", "text": "text"} {
		if m, ok := body[key].(map[string]any); ok {
			if err := cloudNormalizeFields(m, scope, key, out); err != nil {
				return err
			}
		}
	}
	if m, ok := body["text"].(map[string]any); ok {
		if format, ok := m["format"].(map[string]any); ok {
			if err := cloudNormalizeFields(format, "format", "text.format", out); err != nil {
				return err
			}
		}
	}
	if includes, ok := body["include"].([]any); ok {
		kept := make([]any, 0, len(includes))
		for _, v := range includes {
			switch v {
			case "reasoning.encrypted_content", "web_search_call.action.sources", "output.encrypted_content":
				kept = append(kept, v)
			default:
				cloudDecision(out, "include[]", "drop", "CLOUD-INCLUDE")
			}
		}
		if len(kept) == 0 && len(includes) > 0 {
			delete(body, "include")
		} else {
			body["include"] = kept
		}
	}
	if err := cloudNormalizeTools(body["tools"], "tools", out); err != nil {
		return err
	}
	items, _ := body["input"].([]any)
	for i, v := range items {
		if item, ok := v.(map[string]any); ok {
			at := fmt.Sprintf("input[%d]", i)
			typ, _ := item["type"].(string)
			if typ == "" {
				typ = "message"
			}
			if typ == "compaction_summary" || typ == "context_compaction" {
				typ = "compaction"
				item["type"] = typ
				cloudDecision(out, at, "map", "CLOUD-CIPHER")
			}
			if _, exists := item["phase"]; exists && typ != "message" {
				return cloudBadRequest("message phase")
			}
			scope := typ
			if scope == "reasoning" {
				scope = "reasoning-item"
			}
			if err := cloudNormalizeFields(item, scope, at, out); err != nil {
				return err
			}
			switch typ {
			case "additional_tools":
				if err := cloudNormalizeTools(item["tools"], at+".tools", out); err != nil {
					return err
				}
				cloudDecision(out, at, "map", "CLOUD-LITE")
			case "agent_message":
				if err := cloudNormalizeContent(item["content"], at+".content", out); err != nil {
					return err
				}
				cloudDecision(out, at, "map", "CLOUD-AGENT")
			case "configuration_update":
				cloudDecision(out, at, "drop", "CLOUD-CONFIG")
			case "function_call_output", "custom_tool_call_output":
				if openai.IsNamedStandaloneToolOutput(item) {
					// Lower legacy text variants before content policy: type=text
					// is a content alias here, not the top-level text format object.
					parts, err := openai.NormalizeStandaloneToolOutputContent(item["output"])
					if err != nil {
						return cloudBadRequest("standalone tool output text")
					}
					item["output"] = parts
				}
				if err := cloudNormalizeContent(item["output"], at+".output", out); err != nil {
					return err
				}
			case "message":
				if err := cloudNormalizeContent(item["content"], at+".content", out); err != nil {
					return err
				}
			case "reasoning":
				if err := cloudNormalizeContent(item["summary"], at+".summary", out); err != nil {
					return err
				}
				if _, exists := item["content"]; exists {
					delete(item, "content")
					cloudDecision(out, at+".content", "drop", "CLOUD-CIPHER")
				}
				if _, exists := item["summary"]; !exists {
					item["summary"] = []any{}
				}
			}
		}
	}
	return nil
}

func cloudErrorWithDecisions(err error, prior []cloudAdaptationDecision) error {
	var rejected *cloudRejection
	if !errors.As(err, &rejected) {
		return err
	}
	copy := *rejected
	copy.Decisions = append([]cloudAdaptationDecision(nil), prior...)
	for _, decision := range rejected.Decisions {
		cloudDecision(&copy.Decisions, decision.Path, decision.Action, decision.MatrixID)
	}
	cloudDecision(&copy.Decisions, copy.Param, "reject", copy.MatrixID)
	return &copy
}

func cloudJSONError(reason string) error {
	field := reason
	switch reason {
	case "duplicate_json":
		field = "duplicate JSON key"
	case "depth_limit":
		field = "JSON depth limit"
	case "node_limit":
		field = "JSON node limit"
	default:
		field = "invalid JSON"
	}
	return &cloudRejection{Code: "cloud_invalid_request", Param: "$", MatrixID: "CLOUD-JSON", Remedy: "Send an unambiguous bounded JSON object.", reason: field, Decisions: []cloudAdaptationDecision{{Path: "$", Action: "reject", MatrixID: "CLOUD-JSON"}}}
}

// cloneCloudValue copies only container nodes; strings (including large images,
// ciphertext and argument JSON) stay shared and immutable across derived views.
func cloneCloudValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneCloudValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneCloudValue(item)
		}
		return out
	default:
		return value
	}
}
func cloudRejectionFor(field string) *cloudRejection {
	id, param, code := "CLOUD-HISTORY", field, ""
	switch {
	case field == "tool_choice":
		id, param, code = "CLOUD-CHOICE", "tool_choice", "cloud_unsupported_parameter"
	case field == "store":
		id, param, code = "CLOUD-STORE", "store", "cloud_unsupported_parameter"
	case field == "service_tier":
		id, param, code = "CLOUD-TIER", "service_tier", "cloud_unsupported_parameter"
	case field == "strict tool":
		id, param, code = "CLOUD-TOOL", "tools[].strict", "cloud_unsupported_parameter"
	case field == "tool type" || field == "namespace child":
		id, param, code = "CLOUD-TOOL", "tools[].type", "cloud_unsupported_parameter"
	case strings.HasPrefix(field, "tool_search."):
		id, param, code = "CLOUD-SEARCH", "tools[].execution", "cloud_unsupported_parameter"
		if field == "tool_search.parameters" {
			param = "tools[].parameters"
		}
	case strings.HasPrefix(field, "text."):
		id = "CLOUD-FORMAT"
	case strings.HasPrefix(field, "reasoning."):
		id = "CLOUD-REASONING"
	case strings.HasPrefix(field, "tool") || strings.HasPrefix(field, "namespace") || strings.HasPrefix(field, "hosted"):
		id = "CLOUD-TOOL"
		param = "tools"
	case strings.HasPrefix(field, "custom"):
		id = "CLOUD-CUSTOM"
		param = "tools[].format"
	case strings.HasPrefix(field, "image"):
		id = "CLOUD-IMAGE"
		param = "input[].content[]"
	case field == "model" || field == "metadata" || field == "client_metadata" || field == "prompt_cache_key" || field == "instructions" || field == "stream" || field == "background" || field == "previous_response_id":
		id = "CLOUD-REQ"
	}
	rule := cloudCapabilityByID(id)
	if code == "" {
		code = rule.ErrorCode
	}
	if strings.ContainsAny(param, " \t\n") {
		switch id {
		case "CLOUD-FORMAT":
			param = "text.format"
		case "CLOUD-TOOL", "CLOUD-CUSTOM":
			param = "tools"
		default:
			param = "input"
		}
	}
	return &cloudRejection{Code: code, Param: param, MatrixID: id, Remedy: rule.Remedy, reason: field, Decisions: []cloudAdaptationDecision{{Path: param, Action: "reject", MatrixID: id}}}
}

type cloudContinuationProjection struct {
	CallIDs     []string
	PairedCalls map[string]bool
	HistoryRefs []string
}

func projectCloudContinuation(body map[string]any, items []any) cloudContinuationProjection {
	out := cloudContinuationProjection{PairedCalls: map[string]bool{}}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		typ, _ := item["type"].(string)
		id, _ := item["call_id"].(string)
		switch typ {
		case "function_call", "custom_tool_call", "tool_search_call":
			if id != "" {
				out.PairedCalls[id] = true
			}
		case "function_call_output", "custom_tool_call_output", "tool_search_output":
			if id != "" {
				out.CallIDs = append(out.CallIDs, id)
			}
			if strings.HasPrefix(id, "call_cloud_") {
				out.HistoryRefs = append(out.HistoryRefs, id)
				continue
			}
		}
		if id, _ := item["id"].(string); cloudHistoryItemRef(id) {
			out.HistoryRefs = append(out.HistoryRefs, id)
		}
	}
	return out
}
func cloudRequestProtocol(request *OpenAICloudRequest) (*cloudProtocolRequest, error) {
	if request.protocol != nil {
		return request.protocol, nil
	}
	p, err := parseCloudProtocolRequest(request.Body)
	if err == nil {
		request.protocol = p
	}
	return p, err
}

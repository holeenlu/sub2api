package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// Client conversation affinity partitions full-history forks; it never grants
// authority to resume a response/tool. User/key/group/account fences still own
// authorization. Use only the native stable headers, never cache keys or model
// text, and keep legacy records without this optional value compatible.
type cloudConversationKey struct{}

func cloudConversationHash(headers http.Header) string {
	for _, name := range explicitOpenAIHeaderSessionNames {
		if id := sanitizeSessionID(headers.Get(name)); id != "" {
			return DeriveSessionHashFromSeed(id)
		}
	}
	return ""
}
func cloudConversationFromContext(ctx context.Context) string {
	value, _ := ctx.Value(cloudConversationKey{}).(string)
	return value
}
func cloudDifferentConversation(ctx context.Context, binding OpenAICloudBinding) bool {
	hash := cloudConversationFromContext(ctx)
	return hash != "" && binding.ConversationHash != "" && hash != binding.ConversationHash
}

type cloudIngressKey struct{}

// Set only by the Cloud WS ingress after validating this frame, never serialized.
type cloudEnvelopeCheckedKey struct{}
type cloudIngressSourceKey struct{}
type cloudIngressSource struct {
	body                []byte
	responsesCompaction bool
	effort              *string
}
type cloudIngressState struct {
	source    map[string]any
	protocol  *cloudProtocolRequest
	binding   *OpenAICloudBinding
	resolved  bool
	raw       []byte
	validated bool
}

func cloudIngressAdaptations(ctx context.Context) []cloudAdaptationDecision {
	if ctx == nil {
		return nil
	}
	if state, _ := ctx.Value(cloudIngressKey{}).(*cloudIngressState); state != nil && state.protocol != nil {
		return state.protocol.decisions
	}
	return nil
}

// PreserveCloudResponsesInput retains the pre-bootstrap/pre-compact input for a
// later Cloud account selection. Native dispatch still uses its normal body.
func PreserveCloudResponsesInput(ctx context.Context, body []byte, compaction bool) context.Context {
	return context.WithValue(ctx, cloudIngressSourceKey{}, &cloudIngressSource{body: body, responsesCompaction: compaction})
}
func cloudIngressSourceBody(ctx context.Context, body []byte) []byte {
	if source, ok := ctx.Value(cloudIngressSourceKey{}).(*cloudIngressSource); ok {
		return source.body
	}
	return body
}
func decodeCloudProtocolBody(raw []byte) (map[string]any, error) {
	if _, why := validateOpenAIJSONObject(raw); why != "" {
		return nil, cloudJSONError(why)
	}
	return decodeCloudProtocolEnvelope(raw)
}

func cloudIngress(ctx context.Context, body []byte) (context.Context, *cloudIngressState, error) {
	if state, ok := ctx.Value(cloudIngressKey{}).(*cloudIngressState); ok && state != nil {
		return ctx, state, nil
	}
	raw := cloudIngressSourceBody(ctx, body)
	source, err := decodeCloudProtocolEnvelope(raw)
	if err != nil {
		return ctx, nil, err
	}
	if original, ok := ctx.Value(cloudIngressSourceKey{}).(*cloudIngressSource); ok && original.effort != nil {
		setCloudReasoningEffort(source, *original.effort)
	}
	state := &cloudIngressState{source: source, raw: raw}
	state.validated, _ = ctx.Value(cloudEnvelopeCheckedKey{}).(bool)
	return context.WithValue(ctx, cloudIngressKey{}, state), state, nil
}

// SyncCloudReasoningEffort applies the result of the existing public policy at
// its mutation boundary. It never reparses the body or changes other settings.
func SyncCloudReasoningEffort(ctx context.Context, effort string) {
	if original, ok := ctx.Value(cloudIngressSourceKey{}).(*cloudIngressSource); ok {
		original.effort = &effort
	}
	if state, ok := ctx.Value(cloudIngressKey{}).(*cloudIngressState); ok && state != nil {
		setCloudReasoningEffort(state.source, effort)
		state.protocol = nil
	}
}
func setCloudReasoningEffort(body map[string]any, effort string) {
	reasoning, _ := cloneCloudValue(body["reasoning"]).(map[string]any)
	if reasoning == nil {
		reasoning = map[string]any{}
	}
	if effort == "" {
		delete(reasoning, "effort")
	} else {
		reasoning["effort"] = effort
	}
	body["reasoning"] = reasoning
}
func cloudOriginalResponsesCompaction(ctx context.Context) bool {
	original, ok := ctx.Value(cloudIngressSourceKey{}).(*cloudIngressSource)
	return ok && original.responsesCompaction
}
func parseCloudIngressSource(source map[string]any) (*cloudProtocolRequest, error) {
	normalized := cloneCloudValue(source).(map[string]any)
	decisions := cloudLegacyAdaptations(source)
	if err := normalizeOpenAIResponsesLegacyMap(normalized); err != nil {
		return nil, cloudBadRequest("messages")
	}
	return compileCloudProtocolRequest(normalized, decisions)
}

func cloudLegacyAdaptations(source map[string]any) []cloudAdaptationDecision {
	var decisions []cloudAdaptationDecision
	for _, field := range []string{"messages", "functions", "function_call", "reasoning_effort", "commands"} {
		if _, exists := source[field]; exists {
			action := "map"
			if field == "commands" || field == "messages" && source["input"] != nil {
				action = "drop"
			}
			cloudDecision(&decisions, field, action, "CLOUD-REQ")
		}
	}
	if _, alias := source["prompt"].(string); alias {
		cloudDecision(&decisions, "prompt", "map", "CLOUD-REQ")
	}
	return decisions
}
func normalizeCloudReasoningMode(body map[string]any, model string) {
	reasoning, _ := body["reasoning"].(map[string]any)
	if !isOpenAIGPT6Model(model) && reasoning["mode"] == "pro" && reasoning["effort"] == nil {
		reasoning["effort"] = "max"
	}
}
func cloudHasRawMarker(raw []byte) bool {
	return bytes.Contains(raw, []byte("resp_cloud_")) || bytes.Contains(raw, []byte("call_cloud_"))
}

func decodeCloudProtocolEnvelope(raw []byte) (map[string]any, error) {
	var body map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&body) != nil || body == nil {
		return nil, cloudJSONError("invalid_json")
	}
	return body, nil
}
func validateCloudIngress(state *cloudIngressState) error {
	if state.validated {
		return nil
	}
	if _, why := validateOpenAIJSONObject(state.raw); why != "" {
		return cloudJSONError(why)
	}
	state.validated = true
	return nil
}
func normalizeCloudHistoryItems(items []any, decisions *[]cloudAdaptationDecision) ([]any, error) {
	kept := make([]any, 0, len(items))
	for _, rawItem := range items {
		if item, ok := rawItem.(map[string]any); ok {
			if cloudForeignActivityRecord(item) {
				cloudDecision(decisions, "input[]", "drop", "CLOUD-HISTORY")
				continue
			}
			normalized, err := cloudLowerMigratedInput(item)
			if err != nil {
				return nil, err
			}
			if normalized == nil {
				cloudDecision(decisions, "input[]", "drop", "CLOUD-HISTORY")
				continue
			}
			// Keep the parsed body consistent for settings inheritance and the
			// mid-turn input guard; retain configuration/compaction markers there.
			rawItem = normalized
		}
		if item, ok := rawItem.(map[string]any); ok {
			typ, _ := item["type"].(string)
			switch typ {
			case "", "message", "reasoning", "compaction", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "tool_search_call", "tool_search_output", "web_search_call", "image_generation_call", "compaction_trigger":
			default:
				for _, key := range []string{"call_id", "name", "tools", "encrypted_content", "content", "arguments", "output"} {
					if item[key] != nil {
						return nil, cloudBadRequest("input item type")
					}
				}
				cloudDecision(decisions, "input[]", "drop", "CLOUD-HISTORY")
				continue
			}
		}
		kept = append(kept, rawItem)
	}

	return kept, nil
}

// Project only public continuation identity before target selection. A Cloud ID
// in complete history does not select Cloud or impose its capability matrix on
// the next native/BPS request. The selected Cloud adapter compiles that matrix.
func projectCloudIngressSource(source map[string]any) (p *cloudProtocolRequest, err error) {
	body := cloneCloudValue(source).(map[string]any)
	if err := normalizeOpenAIResponsesLegacyMap(body); err != nil {
		return nil, cloudBadRequest("messages")
	}
	decisions := cloudLegacyAdaptations(source)
	defer func() { err = cloudErrorWithDecisions(err, decisions) }()
	if items, ok := body["input"].([]any); ok {
		items, _ = normalizeCodexToolRoleMessages(items)
		items, _ = normalizeCodexMessageContentText(items)
		body["input"] = items
	}
	// Declarations are not turn input or continuation authority. Do not inspect
	// their schemas, or reject another target's history content during lookup.
	items, _ := body["input"].([]any)
	filtered := make([]any, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["type"] != "additional_tools" {
			filtered = append(filtered, raw)
		}
	}
	previous := ""
	if value, exists := source["previous_response_id"]; exists && value != nil {
		var ok bool
		previous, ok = value.(string)
		if !ok {
			return nil, cloudBadRequest("previous_response_id")
		}
	}
	items, _ = normalizeOpenAICompactionTriggerItems(filtered, true)
	return &cloudProtocolRequest{source: body, body: body, previous: previous, continuation: projectCloudContinuation(body, items), decisions: decisions}, nil
}

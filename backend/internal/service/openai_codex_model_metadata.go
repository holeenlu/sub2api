package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

var codexToolCapabilityFields = []string{
	"service_tiers", "experimental_supported_tools",
	"supports_search_tool", "apply_patch_tool_type", "comp_hash", "tool_mode", "use_responses_lite",
	"multi_agent_reasoning_effort", "multi_agent_version",
	"supports_reasoning_effort_updates",
}

// One capability declaration for locally generated and proxied directories.
// The group directory must describe every route that can serve a model.
var cloudCodexToolCapabilities = deriveCloudCodexToolCapabilities()

func deriveCloudCodexToolCapabilities() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage)
	for _, rule := range cloudCapabilities {
		for _, projection := range strings.Split(rule.Catalog, ";") {
			key, value, ok := strings.Cut(projection, "=")
			if ok {
				out[key] = json.RawMessage(value)
			}
		}
	}
	return out
}

func applyCodexToolCapabilities(dst, src map[string]json.RawMessage, overwrite bool) bool {
	changed := false
	for _, field := range codexToolCapabilityFields {
		value := bytes.TrimSpace(src[field])
		if len(value) == 0 {
			continue
		}
		if (field == "service_tiers" || field == "experimental_supported_tools") && bytes.Equal(value, []byte("null")) {
			value = []byte("[]")
		}
		// These Codex fields are nullable booleans or strings, never arbitrary objects.
		if !bytes.Equal(value, []byte("null")) {
			if field == "experimental_supported_tools" {
				tools, valid := decodeCodexExperimentalTools(value)
				if !valid {
					continue
				}
				value, _ = json.Marshal(tools)
			} else if field == "service_tiers" {
				var tiers []configuredCodexServiceTier
				if json.Unmarshal(value, &tiers) != nil {
					continue
				}
			} else if field == "supports_search_tool" || field == "use_responses_lite" || field == "supports_reasoning_effort_updates" {
				if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
					continue
				}
			} else {
				var text string
				if json.Unmarshal(value, &text) != nil {
					continue
				}
			}
		}
		current, exists := dst[field]
		if (exists && !overwrite) || bytes.Equal(current, value) {
			continue
		}
		dst[field] = append(json.RawMessage(nil), value...)
		changed = true
	}
	return changed
}

func accountCodexToolCapabilities(account *Account, modelID string) map[string]json.RawMessage {
	capabilities := make(map[string]json.RawMessage)
	if account == nil {
		return capabilities
	}
	if metadata, ok := account.GetUpstreamModelMetadata(modelID); ok {
		snapshot := account.GetUpstreamModelMetadataSnapshot()
		if account.IsOpenAIApiKey() {
			// All API-key catalog consumers use the same scoped tri-state
			// evidence. Stale credential metadata cannot supply capabilities.
			applyCodexToolCapabilities(capabilities, codexCatalogScopedCapabilities(account, snapshot, metadata.CodexToolCapabilities), true)
		} else {
			applyCodexToolCapabilities(capabilities, metadata.CodexToolCapabilities, true)
			if snapshot == nil || snapshot.CapabilityScope != codexCapabilityScope(account) {
				delete(capabilities, "experimental_supported_tools")
			}
		}
	}
	if account.IsOpenAI() && shouldForwardOpenAIResponsesViaRawChatCompletions(account) {
		// This bridge implements client-side tool discovery, even without a native manifest.
		applyCodexToolCapabilities(capabilities, map[string]json.RawMessage{"supports_search_tool": json.RawMessage("true")}, false)
	}
	// Codex 0.153's bundled Astra catalog verifies these values. API-key routes
	// use standard Responses, not the ChatGPT-only Responses Lite wire.
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		baseURL = account.GetOpenAIBaseURL()
	}
	parsed, err := url.Parse(baseURL)
	official := err == nil && (strings.EqualFold(parsed.Hostname(), "api.openai.com") ||
		(account.IsOpenAIOAuth() && strings.EqualFold(parsed.Hostname(), "chatgpt.com")))
	if account.IsOpenAI() && (isOpenAIGPT6AstraModel(modelID) || openai.IsGPT61SolModelSpelling(modelID)) && official {
		defaults := map[string]json.RawMessage{
			"supports_search_tool":  json.RawMessage("true"),
			"apply_patch_tool_type": json.RawMessage(`"freeform"`),
			"comp_hash":             json.RawMessage(`"3000"`),
			"tool_mode":             json.RawMessage("null"),
			"use_responses_lite":    json.RawMessage("false"),
		}
		if account.IsOpenAIOAuth() {
			defaults["tool_mode"] = json.RawMessage(`"code_mode_only"`)
			defaults["use_responses_lite"] = json.RawMessage("true")
		}
		applyCodexToolCapabilities(capabilities, defaults, false)
	}
	if account.IsOpenAIApiKey() {
		target := modelID
		if isOpenAIGPT6AstraModel(target) {
			target = "gpt-6-astra"
		}
		_, disabled := apiKeyCodexModelsWithoutResponsesLite[target]
		if disabled && bytes.Equal(capabilities["use_responses_lite"], []byte("true")) {
			capabilities["use_responses_lite"] = json.RawMessage("false")
		}
	}
	// This function receives the mapped upstream model. A BPS relay does not
	// implement Codex's native encrypted multi-agent message contract, even
	// when the underlying ChatGPT model advertises it. Explicit nulls override
	// synced/bundled declarations without disabling ordinary plaintext tools.
	if account.isExcelBPSUpstreamModelEnabled(modelID) {
		capabilities["multi_agent_version"] = json.RawMessage("null")
		capabilities["multi_agent_reasoning_effort"] = json.RawMessage("null")
		capabilities["use_responses_lite"] = json.RawMessage("false")
	}
	// Cloud adapts standard Responses to App Server dynamic tools registered at
	// thread start. It cannot load client tools mid-turn (tool_search), speak
	// Responses Lite, host client code mode, carry encrypted multi-agent
	// messages or honor a non-default service tier. Declaring that makes Codex
	// send deferred tools directly in the standard shape instead of failing.
	if account.IsOpenAICloudConfigured() {
		applyCodexToolCapabilities(capabilities, cloudCodexToolCapabilities, true)
		return capabilities
	}
	// API Astra publicly supports Ultrafast. OAuth must advertise it in its
	// account manifest; a subscription label alone does not grant the capability.
	if account.IsOpenAIApiKey() && isOfficialOpenAIModelsBaseURL(baseURL) && isOpenAIGPT6AstraModel(modelID) {
		tiers := configuredCodexServiceTiersForModel(modelID)
		tiers = append(tiers, configuredCodexServiceTier{ID: OpenAIFastTierUltrafast, Name: "Ultrafast", Description: "Lowest latency; 6x Standard token pricing."})
		encoded, err := json.Marshal(tiers)
		if err != nil {
			panic(err)
		}
		applyCodexToolCapabilities(capabilities, map[string]json.RawMessage{"service_tiers": encoded}, false)
	}
	return capabilities
}

// codexModelRoutingAccountIDs 返回分组显式为该公开别名声明的账号集合。
//
// 返回非空表示运营者已经用 model_routing 指明“这个别名由这些账号服务”，此时别名的
// 归属不再是需要推断的未知量：能力声明只看这些账号，且不再因为它们映射到不同上游而
// 判定为冲突。返回空表示没有相关规则，保持原有的全量推断与失败即关闭行为。
func codexModelRoutingAccountIDs(group *Group, modelID string) []int64 {
	if group == nil {
		return nil
	}
	return group.GetRoutingAccountIDs(strings.TrimSpace(modelID))
}

func codexAccountServesPlatformModel(account *Account, platform, modelID string) bool {
	return account.Platform == platform ||
		(account.Platform == PlatformAntigravity && mixedListingAccountAllowed(platform, account) &&
			mixedListingModelAllowed(platform, modelID))
}

func groupCodexModelMetadata(
	platform string,
	modelID string,
	accounts []Account,
	group *Group,
	compositeRoutes []CompositeModelRoute,
	compositeRoutesAvailable bool,
) (codexModelMetadataOverride, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return codexModelMetadataOverride{}, false
	}
	// 显式路由规则本身与平台无关，先取出来供后续两处判定共用。
	routedAccountIDs := codexModelRoutingAccountIDs(group, modelID)
	routed := len(routedAccountIDs) > 0
	upstreamModel := modelID
	if platform == PlatformComposite {
		var resolved bool
		platform, upstreamModel, resolved = resolveCodexCompositeModelTarget(
			modelID,
			accounts,
			compositeRoutes,
			compositeRoutesAvailable,
		)
		if !resolved {
			if !routed && codexExplicitModelTargetsConflict(accounts, modelID) {
				return codexModelMetadataOverride{
					reasoningConflict:       true,
					inputModalitiesConflict: true,
				}, true
			}
			return codexModelMetadataOverride{}, false
		}
	}
	if !isConcreteRequestPlatform(platform) {
		return codexModelMetadataOverride{}, false
	}

	explicitClaims := false
	if upstreamModel == modelID {
		for i := range accounts {
			account := &accounts[i]
			if routed && !containsInt64(routedAccountIDs, account.ID) {
				continue
			}
			if codexAccountServesPlatformModel(account, platform, modelID) && codexExplicitModelMappingClaims(*account, modelID) {
				explicitClaims = true
				break
			}
		}
	}
	// 别名已被显式路由声明时，"多个账号映射到不同上游"是故障转移的正常写法，不是歧义，
	// 因此不再失败即关闭；能力回落到与单账号无快照时相同的按名推导。
	explicitTargetsConflict := explicitClaims && !routed &&
		codexExplicitModelTargetsConflictForPlatform(accounts, platform, modelID)
	publicAlias := upstreamModel != modelID
	candidates := make([]UpstreamModelMetadata, 0)
	missingMetadata := false
	for i := range accounts {
		account := &accounts[i]
		if !codexAccountServesPlatformModel(account, platform, modelID) {
			continue
		}
		if routed && !containsInt64(routedAccountIDs, account.ID) {
			continue
		}
		var lookupModel string
		if explicitClaims {
			if !codexExplicitModelMappingClaims(*account, modelID) {
				continue
			}
			lookupModel = account.GetMappedModel(modelID)
		} else {
			if !account.IsModelSupported(upstreamModel) {
				continue
			}
			lookupModel = account.GetMappedModel(upstreamModel)
		}
		if strings.TrimSpace(lookupModel) != modelID {
			publicAlias = true
		}
		metadata, ok := account.GetUpstreamModelMetadata(lookupModel)
		if !ok {
			if explicitTargetsConflict {
				return codexModelMetadataOverride{
					reasoningConflict:       true,
					inputModalitiesConflict: true,
				}, true
			}
			missingMetadata = true
		}
		metadata.CodexToolCapabilities = accountCodexToolCapabilities(account, lookupModel)
		candidates = append(candidates, metadata)
	}
	if len(candidates) == 0 {
		return codexModelMetadataOverride{}, false
	}
	metadata := intersectUpstreamModelMetadata(modelID, candidates)
	if missingMetadata {
		metadata = codexModelMetadataOverride{UpstreamModelMetadata: UpstreamModelMetadata{
			CodexToolCapabilities: metadata.CodexToolCapabilities,
		}}
	}
	if publicAlias {
		metadata.DisplayName = modelID
		metadata.Description = configuredCodexCustomDescription
	}
	return metadata, true
}

func codexExplicitModelTargetsConflict(accounts []Account, modelID string) bool {
	targets := make(map[string]struct{})
	for i := range accounts {
		account := &accounts[i]
		mappedModel, matched := account.ResolveMappedModel(modelID)
		mappedModel = strings.TrimSpace(mappedModel)
		if !matched || mappedModel == "" {
			continue
		}
		targets[strings.TrimSpace(account.Platform)+"\x00"+mappedModel] = struct{}{}
	}
	return len(targets) > 1
}

func codexExplicitModelTargetsConflictForPlatform(accounts []Account, platform, modelID string) bool {
	targets := make(map[string]struct{})
	for i := range accounts {
		account := &accounts[i]
		if !codexAccountServesPlatformModel(account, platform, modelID) {
			continue
		}
		mappedModel, matched := account.ResolveMappedModel(modelID)
		mappedModel = strings.TrimSpace(mappedModel)
		if !matched || mappedModel == "" {
			continue
		}
		targets[mappedModel] = struct{}{}
	}
	return len(targets) > 1
}

func intersectUpstreamModelMetadata(modelID string, candidates []UpstreamModelMetadata) codexModelMetadataOverride {
	result := codexModelMetadataOverride{UpstreamModelMetadata: UpstreamModelMetadata{ID: strings.TrimSpace(modelID)}}
	result.CodexToolCapabilities = make(map[string]json.RawMessage)
	for _, field := range codexToolCapabilityFields {
		if field == "experimental_supported_tools" {
			shared, _ := decodeCodexExperimentalTools(candidates[0].CodexToolCapabilities[field])
			for _, candidate := range candidates[1:] {
				values, valid := decodeCodexExperimentalTools(candidate.CodexToolCapabilities[field])
				if !valid {
					shared = nil
					break
				}
				shared = intersectOrderedStrings(shared, values)
			}
			if shared == nil {
				shared = []string{}
			}
			result.CodexToolCapabilities[field], _ = json.Marshal(shared)
			continue
		}
		value := bytes.TrimSpace(candidates[0].CodexToolCapabilities[field])
		shared := len(value) > 0
		declared := shared
		for _, candidate := range candidates[1:] {
			declared = declared || len(candidate.CodexToolCapabilities[field]) > 0
			if !bytes.Equal(value, bytes.TrimSpace(candidate.CodexToolCapabilities[field])) {
				shared = false
			}
		}
		if shared {
			result.CodexToolCapabilities[field] = value
		} else if declared {
			fallback := json.RawMessage("null")
			switch field {
			case "supports_search_tool", "use_responses_lite", "supports_reasoning_effort_updates":
				fallback = json.RawMessage("false")
			case "service_tiers":
				fallback = json.RawMessage("[]")
			}
			result.CodexToolCapabilities[field] = fallback
		}
	}
	for _, candidate := range candidates {
		if result.DisplayName == "" && strings.TrimSpace(candidate.DisplayName) != "" {
			result.DisplayName = strings.TrimSpace(candidate.DisplayName)
		}
		if result.Description == "" && strings.TrimSpace(candidate.Description) != "" {
			result.Description = strings.TrimSpace(candidate.Description)
		}
	}

	reasoningKnown := true
	reasoningValue := false
	for i, candidate := range candidates {
		if candidate.Reasoning == nil {
			reasoningKnown = false
			break
		}
		if i == 0 {
			reasoningValue = *candidate.Reasoning
			continue
		}
		if reasoningValue != *candidate.Reasoning {
			reasoningKnown = false
			result.reasoningConflict = true
			break
		}
	}
	if reasoningKnown {
		result.Reasoning = &reasoningValue
		if reasoningValue {
			levels := normalizeReasoningLevels(candidates[0].SupportedReasoningLevels)
			for _, candidate := range candidates[1:] {
				levels = intersectOrderedStrings(levels, normalizeReasoningLevels(candidate.SupportedReasoningLevels))
			}
			result.SupportedReasoningLevels = levels
			if len(levels) == 0 {
				result.reasoningConflict = true
			} else {
				sharedDefault := normalizeReasoningLevel(candidates[0].DefaultReasoningLevel)
				for _, candidate := range candidates[1:] {
					if normalizeReasoningLevel(candidate.DefaultReasoningLevel) != sharedDefault {
						sharedDefault = ""
						break
					}
				}
				if !stringSliceContains(levels, sharedDefault) {
					sharedDefault = levels[0]
				}
				result.DefaultReasoningLevel = sharedDefault
			}
		}
	}

	modalitiesKnown := true
	modalities := normalizeCodexInputModalities(candidates[0].InputModalities)
	if len(modalities) == 0 {
		modalitiesKnown = false
	}
	for _, candidate := range candidates[1:] {
		candidateModalities := normalizeCodexInputModalities(candidate.InputModalities)
		if len(candidateModalities) == 0 {
			modalitiesKnown = false
			break
		}
		modalities = intersectOrderedStrings(modalities, candidateModalities)
	}
	if modalitiesKnown && len(modalities) > 0 {
		result.InputModalities = modalities
	} else if modalitiesKnown {
		result.inputModalitiesConflict = true
	}

	contextKnown := true
	for i, candidate := range candidates {
		if candidate.ContextWindow <= 0 {
			contextKnown = false
			break
		}
		if i == 0 || candidate.ContextWindow < result.ContextWindow {
			result.ContextWindow = candidate.ContextWindow
		}
	}
	if !contextKnown {
		result.ContextWindow = 0
	}
	for i, candidate := range candidates {
		maxContextWindow := candidate.MaxContextWindow
		if maxContextWindow <= 0 {
			// Older snapshots only stored the default window. Keep that bound
			// until a sync supplies the upstream's explicit maximum.
			maxContextWindow = candidate.ContextWindow
		}
		if maxContextWindow <= 0 {
			result.MaxContextWindow = 0
			break
		}
		if i == 0 || maxContextWindow < result.MaxContextWindow {
			result.MaxContextWindow = maxContextWindow
		}
	}
	return result
}

func applyUpstreamModelMetadataToCodexDescriptor(
	descriptor *configuredCodexModelDescriptor,
	metadata codexModelMetadataOverride,
) {
	if descriptor == nil {
		return
	}
	if strings.TrimSpace(metadata.DisplayName) != "" {
		descriptor.DisplayName = strings.TrimSpace(metadata.DisplayName)
	}
	if strings.TrimSpace(metadata.Description) != "" {
		descriptor.Description = strings.TrimSpace(metadata.Description)
	}
	if metadata.reasoningConflict {
		descriptor.DefaultReasoningLevel = nil
		descriptor.SupportedReasoningLevels = []configuredCodexReasoningLevel{}
	} else if metadata.Reasoning != nil && !*metadata.Reasoning {
		none := "none"
		descriptor.DefaultReasoningLevel = &none
		descriptor.SupportedReasoningLevels = []configuredCodexReasoningLevel{{
			Effort:      "none",
			Description: configuredCodexReasoningLevelDescription("none"),
		}}
	} else if metadata.Reasoning != nil && *metadata.Reasoning {
		levels := normalizeReasoningLevels(metadata.SupportedReasoningLevels)
		if len(levels) == 0 {
			descriptor.DefaultReasoningLevel = nil
			descriptor.SupportedReasoningLevels = []configuredCodexReasoningLevel{}
		} else {
			defaultLevel := normalizeReasoningLevel(metadata.DefaultReasoningLevel)
			if !stringSliceContains(levels, defaultLevel) {
				defaultLevel = levels[0]
			}
			descriptor.DefaultReasoningLevel = &defaultLevel
			descriptor.SupportedReasoningLevels = make([]configuredCodexReasoningLevel, 0, len(levels))
			for _, level := range levels {
				descriptor.SupportedReasoningLevels = append(descriptor.SupportedReasoningLevels, configuredCodexReasoningLevel{
					Effort:      level,
					Description: configuredCodexReasoningLevelDescription(level),
				})
			}
		}
	}
	if metadata.inputModalitiesConflict {
		descriptor.InputModalities = []string{"text"}
	} else if modalities := normalizeCodexInputModalities(metadata.InputModalities); len(modalities) > 0 {
		descriptor.InputModalities = modalities
	}
	if metadata.ContextWindow > 0 {
		descriptor.ContextWindow = metadata.ContextWindow
		descriptor.MaxContextWindow = metadata.ContextWindow
	}
	if metadata.MaxContextWindow > 0 {
		descriptor.MaxContextWindow = metadata.MaxContextWindow
		descriptor.ContextWindow = min(descriptor.ContextWindow, metadata.MaxContextWindow)
	}
}

func configuredCodexReasoningLevelDescription(level string) string {
	switch level {
	case "none":
		return "Use the model's default behavior without configurable reasoning"
	case "minimal":
		return "Minimal reasoning for the fastest responses"
	case "low":
		return "Fast responses with lighter reasoning"
	case "medium":
		return "Balanced reasoning for most coding tasks"
	case "high":
		return "Greater reasoning depth for coding and agent tasks"
	case "xhigh":
		return "Extra-high reasoning depth for difficult tasks"
	case "max":
		return "Maximum reasoning depth for complex tasks"
	case "ultra":
		return "Maximum reasoning with automatic task delegation"
	default:
		return "Reasoning effort supported by the upstream model"
	}
}

func intersectOrderedStrings(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	intersection := make([]string, 0, len(left))
	for _, value := range left {
		if _, ok := rightSet[value]; ok {
			intersection = append(intersection, value)
		}
	}
	return intersection
}

func stringSliceContains(values []string, target string) bool {
	if target == "" {
		return false
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func decodeCodexExperimentalTools(raw json.RawMessage) ([]string, bool) {
	var names []string
	if len(raw) > 32768 || len(raw) == 0 || json.Unmarshal(raw, &names) != nil {
		return nil, false
	}
	if len(names) > 128 {
		return nil, false
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || len(name) > 256 || strings.TrimSpace(name) != name {
			return nil, false
		}
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result, true
}

// A refreshed token for the same OAuth identity does not invalidate discovery.
// Different providers, identities and relay keys cannot inherit old capabilities.
func codexCapabilityScope(account *Account) string {
	if account == nil {
		return ""
	}
	identity := codexAccountIdentityNamespace(account)
	if identity == "" {
		identity = account.GetOpenAIAccessToken() + account.GetCredential("api_key")
	}
	value := fmt.Sprintf("%d|%s|%s|%s|%s", account.ID, account.Platform, account.Type, account.GetOpenAIBaseURL(), identity)
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}

// Apply account intersection to every entry, including models fetched from one
// account's live directory; the final body's ETag is computed by the caller.
func restrictCodexExperimentalToolsManifest(body []byte, accounts []Account, group *Group) ([]byte, bool, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return body, false, err
	}
	var models []json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		return body, false, err
	}
	changed := false
	for i, raw := range models {
		var model map[string]json.RawMessage
		if json.Unmarshal(raw, &model) != nil {
			continue
		}
		var slug string
		if json.Unmarshal(model["slug"], &slug) != nil {
			continue
		}
		metadata, ok := groupCodexModelMetadata(group.Platform, slug, accounts, group, nil, false)
		value := json.RawMessage("[]")
		if ok && len(metadata.CodexToolCapabilities["experimental_supported_tools"]) > 0 {
			value = metadata.CodexToolCapabilities["experimental_supported_tools"]
		}
		if _, present := model["experimental_supported_tools"]; !present && bytes.Equal(value, []byte("[]")) {
			continue
		}
		if !bytes.Equal(bytes.TrimSpace(model["experimental_supported_tools"]), value) {
			model["experimental_supported_tools"] = value
			models[i], _ = json.Marshal(model)
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	envelope["models"], _ = json.Marshal(models)
	next, err := json.Marshal(envelope)
	return next, err == nil, err
}

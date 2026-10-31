// Package catalog owns model identity and model policy interpretation.
package catalog

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

var codexModelMap = map[string]string{
	"gpt-6.1-sol":          "gpt-6.1-sol",
	"gpt-6-sol":            "gpt-6-sol",
	"gpt-6-luna":           "gpt-6-luna",
	"gpt-6-astra":          "gpt-6-astra",
	"gpt-5.6-sol":          "gpt-5.6-sol",
	"gpt-5.6-terra":        "gpt-5.6-terra",
	"gpt-5.6-luna":         "gpt-5.6-luna",
	"gpt-5.5":              "gpt-5.5",
	"gpt-5.5-pro":          "gpt-5.5-pro",
	"codex-auto-review":    "codex-auto-review",
	"gpt-5.4":              "gpt-5.4",
	"gpt-5.4-mini":         "gpt-5.4-mini",
	"gpt-5.4-none":         "gpt-5.4",
	"gpt-5.4-low":          "gpt-5.4",
	"gpt-5.4-medium":       "gpt-5.4",
	"gpt-5.4-high":         "gpt-5.4",
	"gpt-5.4-xhigh":        "gpt-5.4",
	"gpt-5.4-chat-latest":  "gpt-5.4",
	"gpt-5.3":              "gpt-5.3-codex",
	"gpt-5.3-none":         "gpt-5.3-codex",
	"gpt-5.3-low":          "gpt-5.3-codex",
	"gpt-5.3-medium":       "gpt-5.3-codex",
	"gpt-5.3-high":         "gpt-5.3-codex",
	"gpt-5.3-xhigh":        "gpt-5.3-codex",
	"gpt-5.3-codex":        "gpt-5.3-codex",
	"gpt-5.3-codex-spark":  "gpt-5.3-codex-spark",
	"gpt-5.3-codex-low":    "gpt-5.3-codex",
	"gpt-5.3-codex-medium": "gpt-5.3-codex",
	"gpt-5.3-codex-high":   "gpt-5.3-codex",
	"gpt-5.3-codex-xhigh":  "gpt-5.3-codex",
	"gpt-5.2":              "gpt-5.2",
	"gpt-5.2-none":         "gpt-5.2",
	"gpt-5.2-low":          "gpt-5.2",
	"gpt-5.2-medium":       "gpt-5.2",
	"gpt-5.2-high":         "gpt-5.2",
	"gpt-5.2-xhigh":        "gpt-5.2",
	"gpt-5":                "gpt-5.4",
	"gpt-5-mini":           "gpt-5.4",
	"gpt-5-nano":           "gpt-5.4",
	"gpt-5.1":              "gpt-5.4",
	"gpt-5.1-codex":        "gpt-5.3-codex",
	"gpt-5.1-codex-max":    "gpt-5.3-codex",
	"gpt-5.1-codex-mini":   "gpt-5.3-codex",
	"gpt-5.2-codex":        "gpt-5.2",
	"codex-mini-latest":    "gpt-5.3-codex",
	"gpt-5-codex":          "gpt-5.3-codex",
}

var codexVersionModelPrefixes = []struct {
	prefix string
	target string
}{
	{prefix: "gpt-6.1-sol", target: "gpt-6.1-sol"},
	{prefix: "gpt-6-sol", target: "gpt-6-sol"},
	{prefix: "gpt-6-luna", target: "gpt-6-luna"},
	{prefix: "gpt-5.6-sol", target: "gpt-5.6-sol"},
	{prefix: "gpt-5.6-terra", target: "gpt-5.6-terra"},
	{prefix: "gpt-5.6-luna", target: "gpt-5.6-luna"},
	{prefix: "gpt-5.3-codex-spark", target: "gpt-5.3-codex-spark"},
	{prefix: "gpt-5.3-codex", target: "gpt-5.3-codex"},
	{prefix: "gpt-5.4-mini", target: "gpt-5.4-mini"},
	{prefix: "gpt-5.4-nano", target: "gpt-5.4-nano"},
	{prefix: "gpt-5.5-pro", target: "gpt-5.5-pro"},
	{prefix: "gpt-5.5", target: "gpt-5.5"},
	{prefix: "gpt-5.4", target: "gpt-5.4"},
	{prefix: "gpt-5.2", target: "gpt-5.2"},
}

// NormalizeCodexModel preserves existing Codex mappings; blank input uses the existing default and unknown names remain unchanged.
func NormalizeCodexModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "gpt-5.4"
	}
	if mapped, ok := normalizeKnownCodexModel(model); ok {
		return mapped
	}
	return model
}

func normalizeKnownCodexModel(model string) (string, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}
	if IsOpenAIImageGenerationModel(model) {
		return model, true
	}

	modelID := LastOpenAIModelSegment(model)

	if normalized := openai.CanonicalizeOpenAIModelAliasSpelling(modelID); normalized != "" {
		modelID = normalized
	}
	if mapped := NormalizeKnownOpenAICodexModel(modelID); mapped != "" {
		return mapped, true
	}
	key := codexModelLookupKey(modelID)
	if key == "" {
		return "", false
	}
	if mapped := LookupCodexModel(key); mapped != "" {
		return mapped, true
	}
	for _, item := range codexVersionModelPrefixes {
		if key == item.prefix {
			return item.target, true
		}
		suffix, ok := strings.CutPrefix(key, item.prefix+"-")
		if ok && IsKnownCodexModelSuffix(suffix) {
			return item.target, true
		}
	}
	return "", false
}

func codexModelLookupKey(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if strings.Contains(modelID, "/") {
		parts := strings.Split(modelID, "/")
		modelID = parts[len(parts)-1]
	}
	return strings.ToLower(strings.Join(strings.Fields(modelID), "-"))
}

// IsKnownCodexModelSuffix recognizes existing effort/date syntax, without asserting upstream capability.
func IsKnownCodexModelSuffix(suffix string) bool {
	switch suffix {
	case "none", "minimal", "low", "medium", "high", "xhigh":
		return true
	}
	return isCodexDateSuffix(suffix)
}

func isCodexDateSuffix(suffix string) bool {
	parts := strings.Split(suffix, "-")
	if len(parts) != 3 || len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return false
	}
	for _, part := range parts {
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// LookupCodexModel resolves the explicit alias table, returning empty for unknown entries.
func LookupCodexModel(modelID string) string {
	key := codexModelLookupKey(modelID)
	if key == "" {
		return ""
	}
	if mapped, ok := codexModelMap[key]; ok {
		return mapped
	}
	return ""
}

// LastOpenAIModelSegment trims a model and removes its optional provider path prefix.
func LastOpenAIModelSegment(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if strings.Contains(model, "/") {
		parts := strings.Split(model, "/")
		model = parts[len(parts)-1]
	}
	return strings.TrimSpace(model)
}

// NormalizeKnownOpenAICodexModel preserves known alias rules, returning empty if no rule matches; it does not authorize requests.
func NormalizeKnownOpenAICodexModel(model string) string {
	normalized := openai.CanonicalizeOpenAIModelAliasSpelling(model)
	if normalized == "" {
		return ""
	}

	if mapped := LookupCodexModel(normalized); mapped != "" {
		return mapped
	}
	if strings.HasSuffix(normalized, "-openai-compact") {
		if mapped := LookupCodexModel(strings.TrimSuffix(normalized, "-openai-compact")); mapped != "" {
			return mapped
		}
	}

	if openai.IsGPT61SolModelSpelling(normalized) {
		return "gpt-6.1-sol"
	}
	if openai.IsGPT6SolOrLunaModelSpelling(normalized) {
		if strings.HasPrefix(normalized, "gpt-6-sol") {
			return "gpt-6-sol"
		}
		return "gpt-6-luna"
	}

	switch {
	case normalized == "gpt-6" || normalized == "gpt-6-astra":
		return "gpt-6-astra"
	case strings.Contains(normalized, "gpt-5.6-sol"):
		return "gpt-5.6-sol"
	case strings.Contains(normalized, "gpt-5.6-terra"):
		return "gpt-5.6-terra"
	case strings.Contains(normalized, "gpt-5.6-luna"):
		return "gpt-5.6-luna"
	case normalized == "gpt-5.6":
		return "gpt-5.6-sol"
	case strings.HasPrefix(normalized, "gpt-5.6-"):
		suffix := strings.TrimPrefix(normalized, "gpt-5.6-")
		if suffix == "max" || IsKnownCodexModelSuffix(suffix) {
			return "gpt-5.6-sol"
		}
		return ""
	case strings.Contains(normalized, "gpt-5.5-pro"):
		return "gpt-5.5-pro"
	case strings.Contains(normalized, "gpt-5.5"):
		return "gpt-5.5"
	case strings.Contains(normalized, "gpt-5.4-mini"):
		return "gpt-5.4-mini"
	case strings.Contains(normalized, "gpt-5.4-nano"):
		return "gpt-5.4-nano"
	case strings.Contains(normalized, "gpt-5.4"):
		return "gpt-5.4"
	case strings.Contains(normalized, "gpt-5.2"):
		return "gpt-5.2"
	case strings.Contains(normalized, "gpt-5.3-codex-spark"):
		return "gpt-5.3-codex-spark"
	case strings.Contains(normalized, "gpt-5.3-codex"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "gpt-5.3"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "codex"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "gpt-5"):
		return "gpt-5.4"
	default:
		return ""
	}
}

// NormalizeOpenAICompatRequestedModel derives routing/allowlist model identity, preserving unknown names.
func NormalizeOpenAICompatRequestedModel(model string) string {
	if openai.IsGPT61SolModelSpelling(model) {
		return "gpt-6.1-sol"
	}
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}

	normalized, _, ok := SplitOpenAICompatReasoningModel(trimmed)
	if !ok || normalized == "" {
		return trimmed
	}
	return normalized
}

// SplitOpenAICompatReasoningModel separates existing GPT effort suffixes; false means unrecognized suffix, not unsupported model.
func SplitOpenAICompatReasoningModel(model string) (normalizedModel string, reasoningEffort string, ok bool) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return "", "", false
	}

	modelID := trimmed
	if strings.Contains(modelID, "/") {
		parts := strings.Split(modelID, "/")
		modelID = parts[len(parts)-1]
	}
	modelID = strings.TrimSpace(modelID)
	if !strings.HasPrefix(strings.ToLower(modelID), "gpt-") {
		return trimmed, "", false
	}

	parts := strings.FieldsFunc(strings.ToLower(modelID), func(r rune) bool {
		switch r {
		case '-', '_', ' ':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return trimmed, "", false
	}

	last := strings.NewReplacer("-", "", "_", "", " ", "").Replace(parts[len(parts)-1])
	switch last {
	case "none", "minimal":
	case "low", "medium", "high":
		reasoningEffort = last
	case "xhigh", "extrahigh":
		reasoningEffort = "xhigh"
	default:
		return trimmed, "", false
	}

	return NormalizeCodexModel(modelID), reasoningEffort, true
}

// IsOpenAIImageGenerationModel identifies the existing GPT and Grok image families.
func IsOpenAIImageGenerationModel(model string) bool {
	return IsGPTImageGenerationModel(model) || IsGrokImageGenerationModel(model)
}

// IsGPTImageGenerationModel identifies the existing GPT image prefix.
func IsGPTImageGenerationModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-image-")
}

// IsGrokImageGenerationModel identifies the existing Grok image names.
func IsGrokImageGenerationModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "grok-imagine" ||
		model == "grok-imagine-edit" ||
		strings.HasPrefix(model, "grok-imagine-image")
}

// CodexAliasesForModel returns a detached list from the single alias table.
// Order is unspecified; unknown targets return an empty, non-nil list.
func CodexAliasesForModel(model string) []string {
	out := make([]string, 0, 1)
	for alias, target := range codexModelMap {
		if target == model {
			out = append(out, alias)
		}
	}
	return out
}

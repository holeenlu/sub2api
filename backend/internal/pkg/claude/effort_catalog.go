package claude

import (
	"strings"
	"unicode"
)

var (
	effortLowMediumHigh         = []string{"low", "medium", "high"}
	effortLowMediumHighMax      = []string{"low", "medium", "high", "max"}
	effortLowMediumHighXHighMax = []string{"low", "medium", "high", "xhigh", "max"}
)

// ThinkingCapabilities describes known Messages API models only. The separate
// lookup boolean distinguishes an unknown model from a model without effort.
// Keep model facts here so discovery and outbound adaptation cannot diverge.
type ThinkingCapabilities struct {
	EffortLevels    []string
	Adaptive        bool
	Manual          bool
	Disable         bool
	BetweenTools    bool
	DisableHighOnly bool
	// DisableFixedEffort forbids historical effort changes independently of
	// the request-level ceiling (Haiku 5.5, whose default effort is medium).
	DisableFixedEffort bool
	DefaultThinking    bool
}

var thinkingFamilies = map[string]ThinkingCapabilities{
	"claude-mythos-preview": {EffortLevels: effortLowMediumHighMax, Adaptive: true, Manual: true, DefaultThinking: true},
	"claude-mythos-5":       {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, DefaultThinking: true},
	"claude-mythos-5-1":     {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, DefaultThinking: true},
	"claude-fable-5":        {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, DefaultThinking: true},
	"claude-fable-5-1":      {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, DefaultThinking: true},
	"claude-sonnet-4-6":     {EffortLevels: effortLowMediumHighMax, Adaptive: true, Manual: true, Disable: true},
	"claude-sonnet-5-5":     {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, BetweenTools: true, DefaultThinking: true},
	"claude-sonnet-5":       {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, Disable: true, DefaultThinking: true},
	"claude-opus-4-8":       {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, Disable: true},
	"claude-opus-4-7":       {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, Disable: true},
	"claude-opus-4-6":       {EffortLevels: effortLowMediumHighMax, Adaptive: true, Manual: true, Disable: true},
	"claude-opus-4-5":       {EffortLevels: effortLowMediumHigh, Manual: true, Disable: true},
	"claude-opus-5-5":       {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, DefaultThinking: true},
	"claude-opus-5":         {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, Disable: true, DisableHighOnly: true, DefaultThinking: true},
	"claude-haiku-4-5":      {Manual: true, Disable: true},
	"claude-haiku-5-5":      {EffortLevels: effortLowMediumHighXHighMax, Adaptive: true, Disable: true, DisableHighOnly: true, DisableFixedEffort: true, DefaultThinking: true},
	"claude-sonnet-4-5":     {Manual: true, Disable: true},
	"claude-opus-4-1":       {Manual: true, Disable: true},
	"claude-opus-4":         {Manual: true, Disable: true},
	"claude-sonnet-4":       {Manual: true, Disable: true},
}

// ThinkingCapabilitiesForModel never infers the capabilities of future minor
// versions or arbitrary suffixes. Provider prefixes and dated IDs are aliases.
func ThinkingCapabilitiesForModel(model string) (ThinkingCapabilities, bool) {
	capabilities, known := thinkingFamilies[normalizeEffortModelID(model)]
	capabilities.EffortLevels = append([]string(nil), capabilities.EffortLevels...)
	return capabilities, known
}

// EffortLevelsForModel returns the output_config.effort values accepted by a
// Claude model, ordered from the lightest to the deepest reasoning level.
func EffortLevelsForModel(model string) []string {
	capabilities, _ := ThinkingCapabilitiesForModel(model)
	return capabilities.EffortLevels
}

// IsOpus55 identifies the fixed Opus 5.5 ID after provider/local suffix normalization.
func IsOpus55(model string) bool {
	return normalizeEffortModelID(model) == "claude-opus-5-5"
}

// IsSonnet55 identifies the fixed Sonnet 5.5 ID after provider/local suffix normalization.
func IsSonnet55(model string) bool {
	return normalizeEffortModelID(model) == "claude-sonnet-5-5"
}

// IsHaiku55 identifies the fixed Haiku 5.5 ID after provider/local suffix normalization.
func IsHaiku55(model string) bool {
	return normalizeEffortModelID(model) == "claude-haiku-5-5"
}

func normalizeEffortModelID(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	id = strings.TrimPrefix(id, "models/")
	if slash := strings.IndexByte(id, '/'); slash >= 0 {
		id = strings.TrimPrefix(strings.TrimSpace(id[slash+1:]), "models/")
	}
	for _, prefix := range []string{"us.", "eu.", "apac.", "jp.", "au.", "us-gov.", "global."} {
		id = strings.TrimPrefix(id, prefix)
	}
	id = strings.TrimPrefix(id, "anthropic.")
	// Bedrock version qualifiers and Vertex date qualifiers are not model
	// families. Strip them before matching exact, known model IDs.
	if version := strings.IndexByte(id, ':'); version >= 0 {
		id = id[:version]
	}
	id = strings.ReplaceAll(id, "@", "-")
	id = strings.TrimSuffix(id, "-v1")
	id = strings.TrimSuffix(id, "-thinking")
	// OpenRouter uses dotted minor versions for some models. Normalize them
	// before effort, thinking, and billing family lookups.
	id = strings.ReplaceAll(id, ".", "-")
	if mapped, ok := ModelIDReverseOverrides[id]; ok {
		id = mapped
	}
	if len(id) >= 9 {
		suffix := id[len(id)-9:]
		if suffix[0] == '-' {
			digits := true
			for _, r := range suffix[1:] {
				if !unicode.IsDigit(r) {
					digits = false
					break
				}
			}
			if digits {
				id = id[:len(id)-9]
			}
		}
	}
	return id
}

// RejectsAssistantPrefill applies only to known adaptive-era Messages models.
func RejectsAssistantPrefill(model string) bool {
	cap, known := ThinkingCapabilitiesForModel(model)
	return known && cap.Adaptive
}

// SupportsStructuredOutput uses the same normalized family catalog as thinking.
// Unknown model versions remain the upstream's decision, not a local rejection.
func SupportsStructuredOutput(model string) bool {
	id := normalizeEffortModelID(model)
	_, known := thinkingFamilies[id]
	return !known || (id != "claude-opus-4" && id != "claude-sonnet-4" && id != "claude-opus-4-1")
}

// ModelSupportsMidConversationOutputConfig is the verified thread capability
// matrix; future model families must not inherit an experimental beta by prefix.
func ModelSupportsMidConversationOutputConfig(model string) bool {
	switch normalizeEffortModelID(model) {
	case "claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5", "claude-opus-5-5", "claude-fable-5-1", "claude-mythos-5-1":
		return true
	}
	return false
}

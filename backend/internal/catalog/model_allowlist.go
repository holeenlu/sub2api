package catalog

import (
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

// ErrEmptyModelAllowlist rejects enabled policies without nonblank models.
var ErrEmptyModelAllowlist = errors.New("model allowlist cannot be enabled with an empty model list")

// ModelAllowlist governs requested names before account or channel mapping.
// The zero value is disabled. Models is caller-owned and must not be mutated
// during use; normalization returns a detached list. JSON remains unchanged.
type ModelAllowlist struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models,omitempty"`
}

// NormalizeModelAllowlist trims and deduplicates in input order, preserving case.
// Enabled empty input is invalid; nil/empty disabled input stays disabled.
func NormalizeModelAllowlist(cfg ModelAllowlist) (ModelAllowlist, error) {
	out := ModelAllowlist{Enabled: cfg.Enabled}
	if len(cfg.Models) == 0 {
		if out.Enabled {
			return out, ErrEmptyModelAllowlist
		}
		return out, nil
	}

	seen := make(map[string]struct{}, len(cfg.Models))
	out.Models = make([]string, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.Models = append(out.Models, model)
	}
	if len(out.Models) == 0 {
		if out.Enabled {
			return out, ErrEmptyModelAllowlist
		}
		out.Models = nil
	}
	return out, nil
}

// Allows matches existing client aliases without granting account capability.
// Blank input is left to the ingress required-field validator.
func (a ModelAllowlist) Allows(model string) bool {
	if !a.Enabled {
		return true
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return true
	}
	candidates := ModelAllowlistCandidates(model)
	for _, entry := range a.Models {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		for _, candidate := range candidates {
			if ModelPatternMatches(entry, candidate) {
				return true
			}
		}
	}
	return false
}

// ModelPatternMatches treats only * as a wildcard, including in the middle
// or at either end. Matching is anchored and case-insensitive.
func ModelPatternMatches(pattern, model string) bool {
	pattern = strings.ToLower(pattern)
	model = strings.ToLower(model)
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == model
	}
	if !strings.HasPrefix(model, parts[0]) {
		return false
	}
	model = model[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		index := strings.Index(model, part)
		if index < 0 {
			return false
		}
		model = model[index+len(part):]
	}
	return strings.HasSuffix(model, parts[len(parts)-1])
}

// ModelAllowlistCandidates 返回客户端模型名在白名单匹配中的候选形式（均已小写）。
func ModelAllowlistCandidates(model string) []string {
	candidates := make([]string, 0, 4)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	add(model)
	add(strings.TrimPrefix(model, "models/"))
	add(claude.NormalizeModelID(strings.TrimSuffix(model, "-thinking")))
	add(NormalizeOpenAICompatRequestedModel(model))
	return candidates
}

// FilterForListing 按白名单条目顺序生成模型列表输出。
// 精确条目沿用既有规则：只有出现在 source（账号映射键 ∪ 平台默认列表）的模式
// 集合中才输出；通配条目展开为 source 中所有匹配项并保持 source 顺序；全局去重。
// 当 source 是前缀通配模式且白名单模式的固定前缀位于该前缀之内时，
// 可直接输出白名单模式：它的所有实例都属于 source。其他未处理的无限
// 交集不会被枚举为模型 ID，列表可能省略仍可请求的模型。
func (a ModelAllowlist) FilterForListing(source []string) []string {
	if !a.Enabled {
		return source
	}
	if len(a.Models) == 0 {
		// 开启但为空的配置在管理端已被拒绝；对遗留脏数据保持“看到什么 = 能调什么”，
		// 列表与准入同时返回空。
		return nil
	}

	patterns := make([]string, 0, len(source))
	for _, pattern := range source {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	if len(patterns) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(a.Models)+len(patterns))
	filtered := make([]string, 0, len(patterns))
	add := func(model string) {
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		filtered = append(filtered, model)
	}

	for _, entry := range a.Models {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "*") {
			for _, pattern := range patterns {
				if ModelPatternMatches(entry, pattern) {
					add(pattern)
				} else if strings.HasSuffix(pattern, "*") &&
					!strings.Contains(strings.TrimSuffix(pattern, "*"), "*") &&
					strings.HasPrefix(strings.ToLower(entry), strings.ToLower(strings.TrimSuffix(pattern, "*"))) {
					add(entry)
				}
			}
			continue
		}
		if allowlistSourcePatternAllowsModel(patterns, entry) {
			add(entry)
		}
	}
	return filtered
}

// allowlistSourcePatternAllowsModel 沿用 filterModelsByCustomList 时代的匹配规则：
// 精确相等、source 通配模式的前缀匹配，以及 Claude 归一化（-thinking 后缀）
// 后的精确匹配。比较与 Allows 一律大小写不敏感，保证「准入允许 ⇒ 列表可见」。
func allowlistSourcePatternAllowsModel(patterns []string, model string) bool {
	for _, pattern := range patterns {
		if strings.EqualFold(pattern, model) {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(strings.ToLower(model), strings.ToLower(strings.TrimSuffix(pattern, "*"))) {
			return true
		}
	}
	normalizedClaudeModel := claude.NormalizeModelID(strings.TrimSuffix(model, "-thinking"))
	if !strings.EqualFold(normalizedClaudeModel, model) {
		for _, pattern := range patterns {
			if strings.EqualFold(pattern, normalizedClaudeModel) {
				return true
			}
		}
	}
	return false
}

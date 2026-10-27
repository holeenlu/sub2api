package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// CheckManagedModel adds policy restrictions without replacing group, channel,
// account, entitlement or protocol checks. Unknown models retain the configured
// fallback semantics; a known disabled alias cannot evade its target's policy.
func (s *SettingService) CheckManagedModel(ctx context.Context, platform, model string) error {
	if s == nil || model == "" {
		return nil
	}
	if platform == PlatformComposite || platform == "" {
		switch {
		case strings.HasPrefix(model, "claude-"):
			platform = PlatformAnthropic
		case strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "codex"):
			platform = PlatformOpenAI
		default:
			return nil
		}
	}
	p, err := s.GetModelPolicy(ctx, platform, false)
	if err != nil {
		return err
	}
	if !p.Allows(model) {
		return fmt.Errorf("model %q is disabled", model)
	}
	if platform == PlatformOpenAI {
		mappings, err := s.GetModelMappings(ctx, false)
		if err != nil {
			return err
		}
		if target, ok := mappings.Mappings[model]; ok && !p.Allows(target) {
			return fmt.Errorf("model %q maps to a disabled model", model)
		}
	}
	return nil
}
func (s *SettingService) ResolveManagedModel(ctx context.Context, platform, model string) (string, error) {
	if s == nil {
		return model, nil
	}
	if err := s.CheckManagedModel(ctx, platform, model); err != nil {
		return "", err
	}
	if platform == PlatformOpenAI {
		mappings, err := s.GetModelMappings(ctx, false)
		if err != nil {
			return "", err
		}
		if target, ok := mappings.Mappings[model]; ok {
			return target, nil
		}
	}
	p, err := s.GetModelPolicy(ctx, platform, false)
	if err != nil {
		return "", err
	}
	target, _, _ := p.Resolve(model)
	return target, nil
}

func (s *SettingService) ApplyClaudeModelParams(ctx context.Context, body []byte, header string) ([]byte, string, error) {
	p, err := s.GetModelPolicy(ctx, PlatformAnthropic, false)
	if err != nil {
		return nil, header, err
	}
	// Existing installations retain request behavior until the operator explicitly
	// saves/syncs/resets this platform. Catalog reads alone do not alter requests.
	if p.Version == 0 {
		return body, header, nil
	}
	model := gjson.GetBytes(body, "model").String()
	_, row, known := p.Resolve(model)
	params := row.Params
	if !known {
		params = ManagedModelParams{MaxTokensDefault: p.Defaults.MaxTokens, MaxTokensCap: 64000, ThinkingFallbackBudget: p.Defaults.ThinkingFallbackBudget, OnAdaptive: "passthrough", OnEnabled: "passthrough"}
	}
	set := func(path string, value any) {
		if err == nil {
			body, err = sjson.SetBytes(body, path, value)
		}
	}
	del := func(path string) {
		if err == nil {
			body, err = sjson.DeleteBytes(body, path)
		}
	}
	maxTokens := gjson.GetBytes(body, "max_tokens").Int()
	if maxTokens <= 0 && params.MaxTokensDefault > 0 {
		maxTokens = params.MaxTokensDefault
		set("max_tokens", maxTokens)
	}
	if params.MaxTokensCap > 0 && maxTokens > params.MaxTokensCap {
		maxTokens = params.MaxTokensCap
		set("max_tokens", maxTokens)
	}
	if p.Defaults.NormalizeThinking {
		thinking := gjson.GetBytes(body, "thinking.type").String()
		action := ""
		if thinking == "adaptive" {
			action = params.OnAdaptive
		}
		if thinking == "enabled" || thinking == "disabled" {
			action = params.OnEnabled
		}
		switch action {
		case "strip":
			del("thinking")
		case "convert_to_enabled":
			set("thinking", map[string]any{"type": "enabled", "budget_tokens": params.ThinkingFallbackBudget})
		case "convert_to_adaptive":
			set("thinking", map[string]any{"type": "adaptive"})
		}
		if gjson.GetBytes(body, "thinking.type").String() == "enabled" {
			budget := gjson.GetBytes(body, "thinking.budget_tokens").Int()
			if budget <= 0 {
				budget = params.ThinkingFallbackBudget
			}
			if budget <= 0 {
				budget = p.Defaults.ThinkingFallbackBudget
			}
			if budget < 1024 {
				budget = 1024
			}
			if maxTokens > 1024 && budget >= maxTokens {
				budget = maxTokens - 1
			}
			set("thinking.budget_tokens", budget)
		}
	}
	if params.DefaultEffort != "" && !gjson.GetBytes(body, "output_config.effort").Exists() {
		set("output_config.effort", params.DefaultEffort)
	}
	const beta = "context-1m-2025-08-07"
	pass := !p.Defaults.StripContext1M
	for _, pattern := range p.Defaults.Context1MWhitelist {
		if groupAllowlistPatternMatches(pattern, model) {
			pass = true
		}
	}
	if known && row.Betas.PassContext1M != nil {
		pass = *row.Betas.PassContext1M
	}
	parts := []string{}
	for _, item := range strings.Split(header, ",") {
		item = strings.TrimSpace(item)
		if item == "" || (!pass && item == beta) {
			continue
		}
		if !slices.Contains(parts, item) {
			parts = append(parts, item)
		}
	}
	return body, strings.Join(parts, ","), err
}

// FilterManagedManifest retains upstream metadata verbatim and only removes
// entries rejected by the central policy. ETags are recomputed from final bytes.
func (s *SettingService) FilterManagedManifest(ctx context.Context, platform string, body []byte) ([]byte, error) {
	if s == nil {
		return body, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	for _, field := range []string{"models", "data"} {
		raw, ok := root[field]
		if !ok {
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, err
		}
		out := make([]json.RawMessage, 0, len(entries))
		for _, entry := range entries {
			var identity struct {
				ID   string `json:"id"`
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(entry, &identity); err != nil {
				return nil, err
			}
			id := identity.ID
			if id == "" {
				id = identity.Slug
			}
			if err := s.CheckManagedModel(ctx, platform, id); err != nil {
				if errors.Is(err, ErrModelPolicyUnavailable) {
					return nil, err
				}
				continue
			}
			out = append(out, entry)
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		root[field] = encoded
	}
	return json.Marshal(root)
}

func (s *GatewayService) managedAvailableModels(ctx context.Context, groupID *int64, platform string) ([]string, error) {
	var err error
	var accounts []Account
	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
	}
	if err != nil {
		return nil, err
	}
	return s.settingService.modelIDsForAccounts(ctx, platform, accounts)
}

func (s *SettingService) modelIDsForAccounts(ctx context.Context, platform string, accounts []Account) ([]string, error) {
	rows, err := s.ModelReferenceCatalog(ctx, platform)
	if err != nil {
		return nil, err
	}
	models := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			models = append(models, id)
			seen[id] = true
		}
	}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		target, err := s.ResolveManagedModel(ctx, platform, row.ID)
		if err != nil {
			continue
		}
		for i := range accounts {
			a := &accounts[i]
			if a.Platform != platform && !mixedListingAccountAllowed(platform, a) {
				continue
			}
			if !a.IsModelSupported(target) {
				continue
			}
			if len(row.SourceAccountIDs) > 0 && !slices.Contains(row.SourceAccountIDs, a.ID) && a.Type == AccountTypeAPIKey && !slices.Contains(modelPolicySeed(platform).IDs(false), target) && !slices.Contains(a.ModelReferences(), target) {
				continue
			}
			add(row.ID)
			break
		}
	}
	// Existing account-specific names remain removable references; their route
	// scope is never turned into a global alias by the migration.
	for i := range accounts {
		a := &accounts[i]
		if a.Platform != platform {
			continue
		}
		refs := append([]string{}, a.ModelReferences()...)
		for id := range a.GetModelMapping() {
			refs = append(refs, id)
		}
		for _, id := range dedupeAndSortModelIDs(refs) {
			if id == "" || strings.Contains(id, "*") {
				continue
			}
			if a.IsModelSupported(id) && s.CheckManagedModel(ctx, platform, id) == nil && s.CheckManagedModel(ctx, platform, a.GetMappedModel(id)) == nil {
				add(id)
			}
		}
	}
	return models, nil
}

type modelManagementContextKey struct{}

func WithModelManagementContext(ctx context.Context, settings *SettingService) context.Context {
	return context.WithValue(ctx, modelManagementContextKey{}, settings)
}
func ModelManagementFromContext(ctx context.Context) *SettingService {
	s, _ := ctx.Value(modelManagementContextKey{}).(*SettingService)
	return s
}
func (s *OpenAIGatewayService) CheckManagedModel(ctx context.Context, platform, model string) error {
	return s.settingService.CheckManagedModel(ctx, platform, model)
}

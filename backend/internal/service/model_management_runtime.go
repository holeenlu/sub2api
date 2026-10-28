package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// CheckManagedModel adds policy restrictions without replacing group, channel,
// account, entitlement or protocol checks. Unknown models retain the configured
// fallback semantics; a known disabled alias cannot evade its target's policy.
var ErrManagedModelNotFound = errors.New("managed model is unavailable")

// managedModelSnapshot is owned by one planning or authorization stage. Names
// share this read; the next send gate loads another fresh snapshot. It carries
// values only and is never installed as a request-context service or grant.
type managedModelSnapshot struct {
	policy   *ModelPolicy
	mappings *ModelMappings
}

func (s *SettingService) managedModelSnapshot(ctx context.Context, platform, name string) (managedModelSnapshot, error) {
	var snapshot managedModelSnapshot
	if s == nil {
		return snapshot, nil
	}
	if platform == "" || platform == PlatformComposite {
		switch {
		case strings.HasPrefix(name, "claude-"):
			platform = PlatformAnthropic
		case strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "codex"):
			platform = PlatformOpenAI
		default:
			mappings, err := s.GetModelMappings(ctx, true)
			if err != nil {
				return snapshot, err
			}
			if _, ok := mappings.Mappings[name]; !ok {
				return snapshot, nil
			}
			snapshot.mappings = mappings
			platform = PlatformOpenAI
		}
	}
	policy, err := s.GetModelPolicy(ctx, platform, true)
	if err != nil {
		return snapshot, err
	}
	snapshot.policy = policy
	if platform == PlatformOpenAI && snapshot.mappings == nil {
		snapshot.mappings, err = s.GetModelMappings(ctx, true)
		if err != nil {
			return managedModelSnapshot{}, err
		}
	}
	return snapshot, nil
}
func (s managedModelSnapshot) check(name string) error {
	if name == "" || s.policy == nil {
		return nil
	}
	if !s.policy.Allows(name) {
		return fmt.Errorf("%w: model %q is disabled", ErrManagedModelNotFound, name)
	}
	if s.mappings != nil {
		if target, ok := s.mappings.Mappings[name]; ok && !s.policy.Allows(target) {
			return fmt.Errorf("%w: model %q maps to a disabled model", ErrManagedModelNotFound, name)
		}
	}
	return nil
}
func (s managedModelSnapshot) resolve(name string) (string, error) {
	if err := s.check(name); err != nil {
		return "", err
	}
	if s.mappings != nil {
		if target, ok := s.mappings.Mappings[name]; ok {
			return target, nil
		}
	}
	if s.policy == nil {
		return name, nil
	}
	target, _, _ := s.policy.Resolve(name)
	return target, nil
}
func (s *SettingService) CheckManagedModel(ctx context.Context, platform, name string) error {
	if s == nil || name == "" {
		return nil
	}
	snapshot, err := s.managedModelSnapshot(ctx, platform, name)
	if err != nil {
		return err
	}
	return snapshot.check(name)
}
func (s *SettingService) ResolveManagedModel(ctx context.Context, platform, name string) (string, error) {
	if s == nil {
		return name, nil
	}
	snapshot, err := s.managedModelSnapshot(ctx, platform, name)
	if err != nil {
		return "", err
	}
	return snapshot.resolve(name)
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
		if catalog.ModelPatternMatches(pattern, model) {
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

// managedModelBatch owns a finite policy set for one manifest/catalog pass.
// Composite names retain the existing Claude/OpenAI inference contract.
type managedModelBatch struct {
	platform  string
	snapshots map[string]managedModelSnapshot
}

func (s *SettingService) managedModelBatch(ctx context.Context, platform string) (managedModelBatch, error) {
	batch := managedModelBatch{platform: platform, snapshots: map[string]managedModelSnapshot{}}
	platforms := []string{platform}
	if platform == "" || platform == PlatformComposite {
		platforms = []string{PlatformOpenAI, PlatformAnthropic}
	}
	for _, provider := range platforms {
		snapshot, err := s.managedModelSnapshot(ctx, provider, "")
		if err != nil {
			return managedModelBatch{}, err
		}
		batch.snapshots[provider] = snapshot
	}
	return batch, nil
}
func (b managedModelBatch) forModel(name string) managedModelSnapshot {
	if b.platform != "" && b.platform != PlatformComposite {
		return b.snapshots[b.platform]
	}
	switch {
	case strings.HasPrefix(name, "claude-"):
		return b.snapshots[PlatformAnthropic]
	case strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "codex"):
		return b.snapshots[PlatformOpenAI]
	}
	if mappings := b.snapshots[PlatformOpenAI].mappings; mappings != nil {
		if _, ok := mappings.Mappings[name]; ok {
			return b.snapshots[PlatformOpenAI]
		}
	}
	return managedModelSnapshot{}
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
	batch, err := s.managedModelBatch(ctx, platform)
	if err != nil {
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
			if err := batch.forModel(id).check(id); err != nil {
				if !errors.Is(err, ErrManagedModelNotFound) {
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
	if platform == "" {
		platform = PlatformComposite
	}

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
	batch, err := s.managedModelBatch(ctx, platform)
	if err != nil {
		return nil, err
	}
	rows, err := s.ModelReferenceCatalog(ctx, platform)
	if err != nil {
		return nil, err
	}
	seedIDs := modelPolicySeed(platform).IDs(false)
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
		target, err := batch.forModel(row.ID).resolve(row.ID)
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
			if len(row.SourceAccountIDs) > 0 && !slices.Contains(row.SourceAccountIDs, a.ID) && a.Type == AccountTypeAPIKey && !slices.Contains(seedIDs, target) && !slices.Contains(a.ModelReferences(), target) {
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
			if a.IsModelSupported(id) && batch.forModel(id).check(id) == nil && batch.forModel(a.GetMappedModel(id)).check(a.GetMappedModel(id)) == nil {
				add(id)
			}
		}
	}
	return models, nil
}

func (s *OpenAIGatewayService) CheckManagedModel(ctx context.Context, platform, model string) error {
	return s.settingService.CheckManagedModel(ctx, platform, model)
}

// checkManagedAccountModels only narrows the existing resource/mode grant. It
// checks every name in the mapping chain, including the final sealed wire name.
func (s *SettingService) checkManagedAccountModels(ctx context.Context, a *Account, requested, upstream string) error {
	if a == nil {
		return ErrAccountNotFound
	}
	if s == nil {
		return nil
	}
	snapshot, err := s.managedModelSnapshot(ctx, a.Platform, requested)
	if err != nil {
		return err
	}
	return snapshot.checkAccount(a, requested, upstream)
}

func (snapshot managedModelSnapshot) checkAccount(a *Account, requested, upstream string) error {
	resolved, err := snapshot.resolve(requested)
	if err != nil {
		return err
	}
	if !a.IsModelSupported(resolved) {
		return fmt.Errorf("%w: account model reference denied: %q", ErrManagedModelNotFound, resolved)
	}
	seen := make(map[string]struct{}, 5)
	for _, name := range []string{requested, resolved, a.GetMappedModel(requested), a.GetMappedModel(resolved), upstream} {
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		if err := snapshot.check(name); err != nil {
			return err
		}
	}
	return nil
}

// Existing legacy transport qualification also consumes the same policy, without
// owning execution or putting mutable services into request context.
func (s *SettingService) validateManagedUpstreamRequest(req *http.Request, repo AccountRepository, account *Account) error {
	if s == nil || repo == nil || account == nil {
		return nil
	}
	current, err := repo.GetByID(req.Context(), account.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrAccountNotFound
	}
	name := ""
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return err
		}
		limit := int64(64 << 20)
		if s.cfg != nil && s.cfg.Gateway.MaxBodySize > 0 {
			limit = s.cfg.Gateway.MaxBodySize
		}
		wire, readErr := io.ReadAll(io.LimitReader(body, limit+1))
		closeErr := body.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		if int64(len(wire)) > limit {
			return fmt.Errorf("managed policy request exceeds body limit")
		}
		name = gjson.GetBytes(wire, "model").String()
		if name == "" {
			name = gjson.GetBytes(wire, "request.model").String()
		}
	}
	if name == "" && req.URL != nil {
		if _, rest, ok := strings.Cut(req.URL.Path, "/models/"); ok {
			name = strings.SplitN(rest, ":", 2)[0]
		}
	}
	if name == "" {
		return nil
	} // Non-generation metadata/credential requests carry no model.
	if err := s.CheckManagedModel(req.Context(), current.Platform, name); err != nil {
		return err
	}
	return nil
}
func (s *GeminiMessagesCompatService) managedModelSettings() *SettingService {
	if s == nil || s.antigravityGatewayService == nil {
		return nil
	}
	return s.antigravityGatewayService.settingService
}

// SetModelSettings connects the central control policy to existing consumers.
func (s *ChannelService) SetModelSettings(settings *SettingService) { s.modelSettings = settings }
func (s *PricingService) SetModelSettings(settings *SettingService) { s.modelSettings = settings }

// GetManagedModelCatalog projects the central reference catalog for control readers.
func (s *SettingService) GetManagedModelCatalog(ctx context.Context, platform string) ([]ManagedModel, error) {
	return s.ModelReferenceCatalog(ctx, platform)
}

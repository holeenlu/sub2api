package service

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const ModelManagementKeyPrefix = "unified_models."
const ModelAutoReviewAlias = "codex-auto-review"
const ModelAutoReviewDefault = "gpt-6-luna"

// Seeds are data from the authorized customized vm2api b1eafd20. They do not
// authorize an account, replace upstream facts, or override operator policies.
//
//go:embed modelmanagement/anthropic.json
var anthropicModelSeed []byte

//go:embed modelmanagement/openai.json
var openAIModelSeed []byte

var (
	ErrModelPolicyConflict    = infraerrors.Conflict("MODEL_POLICY_CONFLICT", "Model configuration changed; reload before saving")
	ErrModelPolicyUnavailable = infraerrors.ServiceUnavailable("MODEL_POLICY_UNAVAILABLE", "Model configuration is unavailable")
)

type ManagedModelParams struct {
	MaxTokensDefault       int64  `json:"max_tokens_default"`
	MaxTokensCap           int64  `json:"max_tokens_cap"`
	ThinkingFallbackBudget int64  `json:"thinking_fallback_budget"`
	OnAdaptive             string `json:"on_adaptive"`
	OnEnabled              string `json:"on_enabled"`
	DefaultEffort          string `json:"default_effort,omitempty"`
}
type ManagedModelBetas struct {
	Required      []string `json:"required,omitempty"`
	Drop          []string `json:"drop,omitempty"`
	AllowClient   bool     `json:"allow_client"`
	PassContext1M *bool    `json:"pass_context_1m,omitempty"`
}
type ManagedModel struct {
	ID               string                 `json:"id"`
	DisplayName      string                 `json:"display_name"`
	Kind             string                 `json:"kind"`
	Family           string                 `json:"family,omitempty"`
	Enabled          bool                   `json:"enabled"`
	Sort             int                    `json:"sort"`
	Aliases          []string               `json:"aliases"`
	Capabilities     map[string]any         `json:"capabilities,omitempty"`
	Params           ManagedModelParams     `json:"params"`
	Betas            ManagedModelBetas      `json:"betas"`
	Catalog          *UpstreamModelMetadata `json:"catalog,omitempty"`
	SourceAccountIDs []int64                `json:"source_account_ids,omitempty"`
}
type ModelPolicyDefaults struct {
	Enabled                bool     `json:"enabled"`
	MaxTokens              int64    `json:"max_tokens"`
	ThinkingFallbackBudget int64    `json:"thinking_fallback_budget"`
	StripContext1M         bool     `json:"strip_context_1m"`
	Context1MWhitelist     []string `json:"context_1m_whitelist"`
	NormalizeThinking      bool     `json:"normalize_thinking"`
}
type ModelPolicy struct {
	Version     int64                   `json:"version"`
	Platform    string                  `json:"platform"`
	Source      string                  `json:"source"`
	UpdatedAt   string                  `json:"updated_at,omitempty"`
	CatalogMode string                  `json:"catalog_mode"`
	Defaults    ModelPolicyDefaults     `json:"defaults"`
	Models      map[string]ManagedModel `json:"models"`
	Aliases     map[string]string       `json:"aliases,omitempty"`
}
type ModelMappings struct {
	Version   int64             `json:"version"`
	UpdatedAt string            `json:"updated_at,omitempty"`
	Mappings  map[string]string `json:"mappings"`
}

// The repository serializes each section in the existing settings table. A
// policy, mapping, or price save cannot overwrite a different section's draft.
type ModelSettingsUpdater interface {
	CompareAndSwapModelSettings(context.Context, string, int64, string) error
}
type modelSettingsCacheEntry struct {
	Raw     string
	Expires time.Time
}
type modelSettingsCache struct {
	sync.Mutex
	Entries map[string]modelSettingsCacheEntry
}

func validModelPlatform(p string) bool {
	return isConcreteRequestPlatform(p) || p == PlatformAntigravity
}
func modelPolicySeed(platform string) *ModelPolicy {
	p := &ModelPolicy{Platform: platform, Source: "builtin", CatalogMode: "policy_only", Models: map[string]ManagedModel{}, Defaults: ModelPolicyDefaults{Enabled: true}}
	switch platform {
	case PlatformAnthropic:
		_ = json.Unmarshal(anthropicModelSeed, p)
	case PlatformOpenAI:
		_ = json.Unmarshal(openAIModelSeed, p)
		p.Defaults.Enabled = true
	default:
		for i, id := range modelSeedIDs(platform) {
			p.Models[id] = ManagedModel{ID: id, DisplayName: id, Kind: "chat", Enabled: true, Sort: i, Aliases: []string{}}
		}
	}
	p.Version = 0
	normalizeModelPolicy(p)
	return p
}

// normalizeModelPolicy keeps the JSON DTO collection fields usable by the
// browser even when a seed or an upstream response omitted them. This is a
// representation rule only; it does not change authorization or unknown-model
// behavior.
func normalizeModelPolicy(p *ModelPolicy) {
	if p == nil {
		return
	}
	if p.Models == nil {
		p.Models = map[string]ManagedModel{}
	}
	if p.Aliases == nil {
		p.Aliases = map[string]string{}
	}
	if p.Defaults.Context1MWhitelist == nil {
		p.Defaults.Context1MWhitelist = []string{}
	}
	for id, row := range p.Models {
		if row.Aliases == nil {
			row.Aliases = []string{}
		}
		if row.SourceAccountIDs == nil {
			row.SourceAccountIDs = []int64{}
		}
		if row.Betas.Required == nil {
			row.Betas.Required = []string{}
		}
		if row.Betas.Drop == nil {
			row.Betas.Drop = []string{}
		}
		p.Models[id] = row
	}
}

func (s *SettingService) readModelSetting(ctx context.Context, section string, fresh bool) (string, error) {
	if s == nil || s.settingRepo == nil {
		return "", ErrModelPolicyUnavailable
	}
	if !fresh {
		s.modelSettings.Lock()
		e, ok := s.modelSettings.Entries[section]
		s.modelSettings.Unlock()
		if ok && time.Now().Before(e.Expires) {
			return e.Raw, nil
		}
	}
	raw, err := s.settingRepo.GetValue(ctx, ModelManagementKeyPrefix+section)
	if errors.Is(err, ErrSettingNotFound) {
		raw = ""
		err = nil
	}
	if err != nil {
		return "", ErrModelPolicyUnavailable
	}
	s.modelSettings.Lock()
	if s.modelSettings.Entries == nil {
		s.modelSettings.Entries = map[string]modelSettingsCacheEntry{}
	}
	s.modelSettings.Entries[section] = modelSettingsCacheEntry{raw, time.Now().Add(2 * time.Second)}
	s.modelSettings.Unlock()
	return raw, nil
}
func (s *SettingService) saveModelSetting(ctx context.Context, section string, version int64, data any) error {
	updater, ok := s.settingRepo.(ModelSettingsUpdater)
	if !ok {
		return ErrModelPolicyUnavailable
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err = updater.CompareAndSwapModelSettings(ctx, ModelManagementKeyPrefix+section, version, string(raw)); err != nil {
		return err
	}
	s.modelSettings.Lock()
	delete(s.modelSettings.Entries, section)
	s.modelSettings.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return nil
}
func (s *SettingService) GetModelPolicy(ctx context.Context, platform string, fresh bool) (*ModelPolicy, error) {
	if !validModelPlatform(platform) {
		return nil, infraerrors.BadRequest("INVALID_MODEL_PLATFORM", "Unknown model platform")
	}
	raw, err := s.readModelSetting(ctx, platform, fresh)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return modelPolicySeed(platform), nil
	}
	var p ModelPolicy
	if json.Unmarshal([]byte(raw), &p) != nil || p.Version < 0 || p.Platform != platform || p.Models == nil {
		return nil, ErrModelPolicyUnavailable
	}
	normalizeModelPolicy(&p)
	return &p, nil
}
func (p *ModelPolicy) Resolve(model string) (string, ManagedModel, bool) {
	key := strings.TrimSpace(model)
	// Match only existing equivalent spellings. Do not strip arbitrary upstream
	// namespaces: those can identify independent API-supply models.
	for _, candidate := range catalog.ModelAllowlistCandidates(key) {
		if target, ok := p.Aliases[candidate]; ok {
			if row, ok := p.Models[target]; ok {
				return target, row, true
			}
		}
		if row, ok := p.Models[candidate]; ok {
			return candidate, row, true
		}
		for id, row := range p.Models {
			for _, alias := range row.Aliases {
				if strings.EqualFold(alias, candidate) {
					return id, row, true
				}
			}
		}
	}
	return model, ManagedModel{}, false
}
func (p *ModelPolicy) Allows(model string) bool {
	_, row, ok := p.Resolve(model)
	if ok {
		return row.Enabled
	}
	return p.Defaults.Enabled
}
func (p *ModelPolicy) IDs(enabledOnly bool) []string {
	rows := make([]ManagedModel, 0, len(p.Models))
	for _, row := range p.Models {
		if !enabledOnly || row.Enabled {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Sort == rows[j].Sort {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Sort < rows[j].Sort
	})
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}
func validateModelID(id string) bool {
	return id != "" && len(id) <= 256 && !strings.ContainsAny(id, "*?\\") && strings.IndexFunc(id, unicode.IsSpace) < 0
}
func invalidModelPolicy(message string) error {
	return infraerrors.BadRequest("INVALID_MODEL_POLICY", message)
}
func validateModelPolicy(p *ModelPolicy) error {
	if p.Version < 0 || len(p.Models) > 2000 {
		return invalidModelPolicy("Invalid model policy size or version")
	}
	if p.CatalogMode != "" && p.CatalogMode != "policy_only" && p.CatalogMode != "worker_intersect_policy" && p.CatalogMode != "worker_only" {
		return invalidModelPolicy("Invalid catalog mode")
	}
	if p.Defaults.MaxTokens < 0 || p.Defaults.MaxTokens > 10000000 || p.Defaults.ThinkingFallbackBudget < 0 {
		return invalidModelPolicy("Invalid default token budget")
	}
	aliases := map[string]string{}
	for id, row := range p.Models {
		if !validateModelID(id) || row.ID != id || (row.Kind != "chat" && row.Kind != "image") {
			return invalidModelPolicy("Invalid model ID or kind")
		}
		if len(row.Aliases) > 100 {
			return invalidModelPolicy("Too many model aliases")
		}
		for _, alias := range row.Aliases {
			key := strings.ToLower(alias)
			if !validateModelID(alias) {
				return invalidModelPolicy("Invalid alias")
			}
			if other, ok := aliases[key]; ok && other != id {
				return invalidModelPolicy("Duplicate model alias")
			}
			// Resolve gives an explicit policy alias precedence over a model ID.
			// The built-in Haiku catalog deliberately keeps both spellings, so
			// that declared redirect is valid; an undeclared shadow is not.
			if _, exists := p.Models[key]; exists && key != id && p.Aliases[key] != id {
				return invalidModelPolicy("Alias shadows another model")
			}
			aliases[key] = id
		}
		params := row.Params
		if params.MaxTokensDefault < 0 || params.MaxTokensCap < 0 || params.MaxTokensCap > 10000000 || (params.MaxTokensCap > 0 && params.MaxTokensDefault > params.MaxTokensCap) || params.ThinkingFallbackBudget < 0 {
			return invalidModelPolicy("Invalid model token limits")
		}
		for _, action := range []string{params.OnAdaptive, params.OnEnabled} {
			if action != "" && action != "passthrough" && action != "strip" && action != "convert_to_enabled" && action != "convert_to_adaptive" {
				return invalidModelPolicy("Invalid thinking action")
			}
		}
		if params.DefaultEffort != "" && params.DefaultEffort != "low" && params.DefaultEffort != "medium" && params.DefaultEffort != "high" && params.DefaultEffort != "xhigh" && params.DefaultEffort != "max" {
			return invalidModelPolicy("Invalid default effort")
		}
	}
	for alias, target := range p.Aliases {
		if !validateModelID(alias) {
			return invalidModelPolicy("Invalid alias")
		}
		if _, ok := p.Models[target]; !ok {
			return invalidModelPolicy("Unknown alias target")
		}
	}
	return nil
}
func (s *SettingService) SaveModelPolicy(ctx context.Context, platform string, next ModelPolicy) (*ModelPolicy, error) {
	current, err := s.GetModelPolicy(ctx, platform, true)
	if err != nil {
		return nil, err
	}
	if current.Version != next.Version {
		return nil, ErrModelPolicyConflict
	}
	next.Platform = platform
	// Upstream metadata and source scope are never editable by the browser.
	for id, row := range next.Models {
		old, exists := current.Models[id]
		if !exists {
			return nil, invalidModelPolicy("Sync the model catalog before configuring new models")
		}
		row.Catalog = old.Catalog
		row.SourceAccountIDs = old.SourceAccountIDs
		if platform != PlatformAnthropic {
			row.Capabilities = old.Capabilities
			row.Params = old.Params
			row.Betas = old.Betas
			row.Aliases = old.Aliases
			row.Kind = old.Kind
		}
		next.Models[id] = row
	}
	// Disabling has a visible meaning; deletion from a submitted snapshot is not
	// a second way to silently re-enable a model through unknown-model fallback.
	for id := range current.Models {
		if _, ok := next.Models[id]; !ok {
			return nil, invalidModelPolicy("Models must be disabled, not removed")
		}
	}
	if err = validateModelPolicy(&next); err != nil {
		return nil, err
	}
	next.Version++
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	next.Source = "settings"
	normalizeModelPolicy(&next)
	if err = s.saveModelSetting(ctx, platform, current.Version, &next); err != nil {
		return nil, err
	}
	return &next, nil
}
func (s *SettingService) ResetModelPolicy(ctx context.Context, platform string, expected int64) (*ModelPolicy, error) {
	current, err := s.GetModelPolicy(ctx, platform, true)
	if err != nil {
		return nil, err
	}
	if current.Version != expected {
		return nil, ErrModelPolicyConflict
	}
	seed := modelPolicySeed(platform)
	seed.Version = expected + 1
	seed.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	normalizeModelPolicy(seed)
	if err = s.saveModelSetting(ctx, platform, expected, seed); err != nil {
		return nil, err
	}
	return seed, nil
}
func (s *SettingService) GetModelMappings(ctx context.Context, fresh bool) (*ModelMappings, error) {
	raw, err := s.readModelSetting(ctx, "mappings", fresh)
	if err != nil {
		return nil, err
	}
	p := &ModelMappings{Mappings: map[string]string{ModelAutoReviewAlias: ModelAutoReviewDefault}}
	if raw != "" && (json.Unmarshal([]byte(raw), p) != nil || p.Mappings == nil || p.Version < 0) {
		return nil, ErrModelPolicyUnavailable
	}
	if _, ok := p.Mappings[ModelAutoReviewAlias]; !ok {
		p.Mappings[ModelAutoReviewAlias] = ModelAutoReviewDefault
	}
	return p, nil
}
func (s *SettingService) SaveModelMappings(ctx context.Context, next ModelMappings) (*ModelMappings, error) {
	p, err := s.GetModelPolicy(ctx, PlatformOpenAI, true)
	if err != nil {
		return nil, err
	}
	if next.Version < 0 || len(next.Mappings) > 500 {
		return nil, invalidModelPolicy("Invalid mapping table")
	}
	seen := map[string]bool{}
	if next.Mappings == nil {
		next.Mappings = map[string]string{}
	}
	if _, ok := next.Mappings[ModelAutoReviewAlias]; !ok {
		next.Mappings[ModelAutoReviewAlias] = ModelAutoReviewDefault
	}
	for alias, target := range next.Mappings {
		key := strings.ToLower(alias)
		parts := strings.Split(key, "/")
		bare := parts[len(parts)-1]
		if !validateModelID(alias) || seen[key] {
			return nil, invalidModelPolicy("Blank, whitespace or duplicate alias")
		}
		seen[key] = true
		if strings.HasPrefix(bare, "claude-") || strings.HasPrefix(bare, "anthropic.claude-") || bare == "sonnet" || bare == "opus" || bare == "haiku" || bare == "fable" {
			return nil, invalidModelPolicy("Claude aliases cannot be mapped to GPT")
		}
		if strings.Contains(bare, "image") {
			return nil, invalidModelPolicy("Image models cannot be mapped as chat aliases")
		}
		if target == "" {
			target = "gpt-6.1-sol"
			next.Mappings[alias] = target
		}
		row, ok := p.Models[target]
		if !ok || !row.Enabled || row.Kind != "chat" {
			return nil, invalidModelPolicy(fmt.Sprintf("Unavailable mapping target: %s", target))
		}
	}
	expected := next.Version
	next.Version++
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err = s.saveModelSetting(ctx, "mappings", expected, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

// SyncModelPolicy publishes a successfully collected snapshot. Collection is
// performed before this method, so failures never erase the saved policy.
func (s *SettingService) SyncModelPolicy(ctx context.Context, platform string, expected int64, catalogs map[int64]*UpstreamModelCatalog) (*ModelPolicy, error) {
	current, err := s.GetModelPolicy(ctx, platform, true)
	if err != nil {
		return nil, err
	}
	if current.Version != expected {
		return nil, ErrModelPolicyConflict
	}
	next := *current
	next.Models = map[string]ManagedModel{}
	for id, row := range current.Models {
		next.Models[id] = row
	}
	count := 0
	collected := map[string][]int64{}
	accountIDs := make([]int64, 0, len(catalogs))
	for id := range catalogs {
		accountIDs = append(accountIDs, id)
	}
	sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
	for _, accountID := range accountIDs {
		catalog := catalogs[accountID]
		if catalog == nil {
			continue
		}
		for _, id := range catalog.Models {
			if !validateModelID(id) {
				continue
			}
			row, exists := current.Models[id]
			if !exists {
				row = ManagedModel{ID: id, DisplayName: id, Kind: "chat", Enabled: platform == PlatformAnthropic && current.CatalogMode == "worker_only", Aliases: []string{}, Sort: 100}
			}
			if strings.Contains(strings.ToLower(id), "image") {
				row.Kind = "image"
			}
			if metadata, ok := catalog.Metadata[id]; ok {
				m := metadata
				row.Catalog = &m
				if row.DisplayName == id && m.DisplayName != "" {
					row.DisplayName = m.DisplayName
				}
			}
			if ids := collected[id]; len(ids) == 0 || ids[len(ids)-1] != accountID {
				collected[id] = append(ids, accountID)
			}
			// A partial source refresh must not erase provenance from sources
			// that did not participate in this refresh.
			row.SourceAccountIDs = append([]int64{}, collected[id]...)
			for _, source := range current.Models[id].SourceAccountIDs {
				if _, refreshed := catalogs[source]; !refreshed {
					row.SourceAccountIDs = append(row.SourceAccountIDs, source)
				}
			}
			sort.Slice(row.SourceAccountIDs, func(i, j int) bool { return row.SourceAccountIDs[i] < row.SourceAccountIDs[j] })
			next.Models[id] = row
			count++
		}
	}
	if count == 0 {
		return nil, infraerrors.BadRequest("EMPTY_MODEL_CATALOG", "Upstream returned no usable models; saved catalog is unchanged")
	}
	// An alias whose live target disappeared must not refer to a phantom row.
	next.Aliases = map[string]string{}
	for alias, target := range current.Aliases {
		if _, ok := next.Models[target]; ok {
			next.Aliases[alias] = target
		}
	}
	next.Version++
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	next.Source = "upstream"
	normalizeModelPolicy(&next)
	if err = s.saveModelSetting(ctx, platform, expected, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *SettingService) ModelReferenceCatalog(ctx context.Context, platform string) ([]ManagedModel, error) {
	platforms := []string{platform}
	if platform == PlatformComposite {
		platforms = []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformZhipu, PlatformMiniMax, PlatformOpenCodeGo, PlatformTypeSafe, PlatformCommandCode, PlatformCline}
	}
	rows := []ManagedModel{}
	seen := map[string]bool{}
	for _, provider := range platforms {
		p, err := s.GetModelPolicy(ctx, provider, false)
		if err != nil {
			return nil, err
		}
		for _, id := range p.IDs(false) {
			if !seen[id] {
				rows = append(rows, p.Models[id])
				seen[id] = true
			}
		}
	}
	if platform == PlatformOpenAI || platform == PlatformComposite {
		mappings, err := s.GetModelMappings(ctx, false)
		if err != nil {
			return nil, err
		}
		p, err := s.GetModelPolicy(ctx, PlatformOpenAI, false)
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(mappings.Mappings))
		for k := range mappings.Mappings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, alias := range keys {
			if seen[alias] {
				continue
			}
			target := mappings.Mappings[alias]
			row, ok := p.Models[target]
			if !ok {
				continue
			}
			row.ID = alias
			row.DisplayName = alias
			rows = append(rows, row)
		}
	}
	return rows, nil
}

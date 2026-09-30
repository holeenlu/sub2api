package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const ModelCatalogPolicyExtraKey = "model_catalog_policy"
const modelCatalogVisibilityExtraKey = "model_catalog_visibility"

type ModelCatalogPolicy struct {
	Mode     string   `json:"mode"`
	Models   []string `json:"models"`
	Excluded []string `json:"excluded"`
}

func accountModelCatalogPolicy(a *Account) ModelCatalogPolicy {
	p := ModelCatalogPolicy{Mode: "legacy", Models: []string{}, Excluded: []string{}}
	if a == nil {
		return p
	}
	if raw, err := json.Marshal(a.Extra[ModelCatalogPolicyExtraKey]); err == nil {
		_ = json.Unmarshal(raw, &p)
	}
	if p.Models == nil {
		p.Models = []string{}
	}
	if p.Excluded == nil {
		p.Excluded = []string{}
	}
	return p
}

func catalogPolicyMatch(patterns []string, model string) bool {
	for _, pattern := range patterns {
		if groupAllowlistPatternMatches(pattern, model) {
			return true
		}
	}
	return false
}

// Passthrough forwards the original model; a dormant mapping must not grant
// access to an alias or make a permitted request depend on its old target.
func accountCatalogPolicyModel(a *Account, model string) string {
	if a.IsOpenAIPassthroughEnabled() {
		return model
	}
	return a.GetMappedModel(model)
}

func accountCatalogPolicyAllows(a *Account, model string) (bool, bool) {
	p := accountModelCatalogPolicy(a)
	if p.Mode == "" || p.Mode == "legacy" {
		return false, false
	}
	if catalogPolicyMatch(p.Excluded, model) {
		return false, true
	}
	if p.Mode == "fixed" {
		actual := accountCatalogPolicyModel(a, model)
		if catalogPolicyMatch(p.Excluded, actual) {
			return false, true
		}
		// The policy owns access. Alias mappings never grant additional access,
		// and a fixed empty list must stay empty even on platforms with defaults.
		return catalogPolicyMatch(p.Models, model) || catalogPolicyMatch(p.Models, actual), true
	}
	if p.Mode != "follow" {
		return false, true
	}
	actual := accountCatalogPolicyModel(a, model)
	if catalogPolicyMatch(p.Excluded, actual) {
		return false, true
	}
	var visibility struct {
		ExpiresAt time.Time       `json:"expires_at"`
		Scope     string          `json:"scope"`
		Models    map[string]bool `json:"models"`
	}
	raw, err := json.Marshal(a.Extra[modelCatalogVisibilityExtraKey])
	if err != nil || json.Unmarshal(raw, &visibility) != nil {
		return false, true
	}
	// Parent credentials are rechecked by the registry at the request boundary.
	if visibility.Scope != modelCatalogScope(a, a) || !visibility.ExpiresAt.After(time.Now()) {
		return false, true
	}
	return visibility.Models[actual], true
}

func validateModelCatalogPolicy(p ModelCatalogPolicy) error {
	if p.Mode != "legacy" && p.Mode != "fixed" && p.Mode != "follow" {
		return fmt.Errorf("invalid catalog policy mode")
	}
	if len(p.Models) > 5000 || len(p.Excluded) > 5000 {
		return fmt.Errorf("too many model patterns")
	}
	for _, list := range [][]string{p.Models, p.Excluded} {
		for _, id := range list {
			if strings.TrimSpace(id) == "" || len(id) > 256 || strings.ContainsAny(id, "\r\n") {
				return fmt.Errorf("invalid model pattern")
			}
		}
	}
	return nil
}

// Validate against the proposed credentials as well as the saved visibility,
// so a credential change cannot activate follow mode using another scope.
func validateAccountCatalogPolicy(a *Account, p ModelCatalogPolicy) error {
	if err := validateModelCatalogPolicy(p); err != nil {
		return err
	}
	if p.Mode == "follow" {
		var visibility struct {
			ExpiresAt time.Time `json:"expires_at"`
			Scope     string    `json:"scope"`
		}
		raw, err := json.Marshal(a.Extra[modelCatalogVisibilityExtraKey])
		if err != nil || json.Unmarshal(raw, &visibility) != nil ||
			visibility.Scope != modelCatalogScope(a, a) || !visibility.ExpiresAt.After(time.Now()) {
			return fmt.Errorf("refresh the account catalog before enabling follow mode")
		}
	}
	return nil
}

func applyAccountCatalogPolicy(a *Account, p *ModelCatalogPolicy) error {
	if p == nil {
		return nil // Unrelated edits must not migrate or broaden legacy permissions.
	}
	if err := validateAccountCatalogPolicy(a, *p); err != nil {
		return infraerrors.BadRequest("INVALID_MODEL_CATALOG_POLICY", err.Error())
	}
	if a.Extra == nil {
		a.Extra = make(map[string]any)
	}
	a.Extra[ModelCatalogPolicyExtraKey] = *p
	return nil
}

func (s *ModelCatalogService) SaveAccountPolicy(ctx context.Context, id int64, p ModelCatalogPolicy) error {
	if err := validateModelCatalogPolicy(p); err != nil {
		return err
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := validateAccountCatalogPolicy(a, p); err != nil {
		return err
	}
	return s.accounts.UpdateExtra(ctx, id, map[string]any{ModelCatalogPolicyExtraKey: p})
}

func (s *ModelCatalogService) saveVisibility(ctx context.Context, a *Account, snapshot *ModelCatalogSnapshot) error {
	visible := map[string]bool{}
	for _, entry := range snapshot.Models {
		if (entry.Access == "listed" || entry.Access == "observed") && entry.Lifecycle != "retired" {
			visible[entry.ID] = true
		}
	}
	return s.accounts.UpdateExtra(ctx, a.ID, map[string]any{modelCatalogVisibilityExtraKey: map[string]any{"scope": modelCatalogScope(a, a), "models": visible, "revision": snapshot.Revision, "expires_at": snapshot.CheckedAt.Add(time.Duration(s.Settings(ctx).StaleSeconds) * time.Second)}})
}

func (s *ModelCatalogService) ModelIsPublished(ctx context.Context, a *Account, model string) (bool, bool) {
	snapshot, err := s.Account(ctx, a)
	if err != nil || snapshot.Status == "unavailable" {
		return false, false
	}
	for _, entry := range snapshot.Models {
		if entry.ID == model {
			if entry.Lifecycle == "retired" || entry.Access == "unlisted" {
				return false, true
			}
			if entry.Access == "listed" || entry.Access == "observed" {
				return true, true
			}
			// Explicit administrator aliases are retained in legacy/fixed mode.
			return entry.Access == "configured" && accountModelCatalogPolicy(a).Mode != "follow", true
		}
	}
	return false, true
}

// Follow/fixed policies must recheck the credential parent, expiration and
// explicit revocations even when used by a legacy group or a sticky session.
func (s *ModelCatalogService) accountRouteAllowed(ctx context.Context, a *Account, requested string) bool {
	mode := accountModelCatalogPolicy(a).Mode
	if mode != "follow" && mode != "fixed" {
		return true
	}
	if !a.IsModelSupported(requested) {
		return false
	}
	allowed, known := s.ModelIsPublished(ctx, a, accountCatalogPolicyModel(a, requested))
	return known && allowed
}

func catalogRegistryApplies(e ModelCatalogEntry, a, source *Account) bool {
	if a == nil || e.Platform != a.Platform {
		return false
	}
	if e.SourceAccountID != 0 && (source == nil || source.ID != e.SourceAccountID) {
		return false
	}
	if e.UpstreamNamespace == "" {
		return true
	}
	if source == nil {
		return false
	}
	namespace := upstreamModelRegistryBaseURL(source)
	if source.IsOpenAIOAuth() {
		namespace = "https://chatgpt.com/backend-api/codex"
	}
	return normalizeModelRegistryBaseURL(namespace) == normalizeModelRegistryBaseURL(e.UpstreamNamespace)
}

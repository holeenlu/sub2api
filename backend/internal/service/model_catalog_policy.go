package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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

func accountCatalogPolicyAllows(a *Account, model string) (bool, bool) {
	p := accountModelCatalogPolicy(a)
	if p.Mode == "" || p.Mode == "legacy" {
		return false, false
	}
	if catalogPolicyMatch(p.Excluded, model) {
		return false, true
	}
	if p.Mode == "fixed" {
		actual := a.GetMappedModel(model)
		if catalogPolicyMatch(p.Excluded, actual) {
			return false, true
		}
		return catalogPolicyMatch(p.Models, model) || catalogPolicyMatch(p.Models, actual), true
	}
	if p.Mode != "follow" {
		return false, false
	}
	actual := a.GetMappedModel(model)
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
	if visibility.Scope != modelCatalogScope(a, a) || (!visibility.ExpiresAt.IsZero() && !visibility.ExpiresAt.After(time.Now())) {
		return false, true
	}
	return visibility.Models[actual], true
}

func (s *ModelCatalogService) SaveAccountPolicy(ctx context.Context, id int64, p ModelCatalogPolicy) error {
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
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if p.Mode == "follow" {
		snapshot, err := s.Account(ctx, a)
		if err != nil || snapshot.Status == "unavailable" {
			return fmt.Errorf("refresh the account catalog before enabling follow mode")
		}
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

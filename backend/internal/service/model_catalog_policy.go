package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const ModelCatalogPolicyExtraKey = "model_catalog_policy"

// Models is the only editable access configuration. An empty account list
// means unrestricted supply; aliases continue to describe routing only.
type ModelCatalogPolicy struct {
	Models []string `json:"models"`
}

func accountModelCatalogPolicy(a *Account) ModelCatalogPolicy {
	p, _ := readAccountModelSelection(a)
	return p
}

func readAccountModelSelection(a *Account) (ModelCatalogPolicy, bool) {
	p := ModelCatalogPolicy{Models: []string{}}
	if a != nil {
		raw, err := json.Marshal(a.Extra[ModelCatalogPolicyExtraKey])
		if err != nil || json.Unmarshal(raw, &p) != nil {
			return p, false
		}
	}
	if p.Models == nil {
		p.Models = []string{}
	}
	return p, true
}

// Pre-catalog accounts have no separate selection and retain their original
// mapping semantics until saved. Old fixed/follow records both use their saved
// selections; discovery never expands an administrator's selection.
func accountHasModelSelection(a *Account) bool {
	if a == nil {
		return false
	}
	value, exists := a.Extra[ModelCatalogPolicyExtraKey]
	if !exists || value == nil {
		return false
	}
	raw, _ := json.Marshal(value)
	var old struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(raw, &old)
	return old.Mode != "legacy"
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
	if !accountHasModelSelection(a) {
		return false, false
	}
	p, valid := readAccountModelSelection(a)
	if !valid {
		return false, true
	}
	actual := accountCatalogPolicyModel(a, model)
	return len(p.Models) == 0 || catalogPolicyMatch(p.Models, model) || catalogPolicyMatch(p.Models, actual), true
}

func validateAccountCatalogPolicy(_ *Account, p ModelCatalogPolicy) error {
	if len(p.Models) > 5000 {
		return fmt.Errorf("too many model patterns")
	}
	for _, id := range p.Models {
		if strings.TrimSpace(id) == "" || len(id) > 256 || strings.ContainsAny(id, "\r\n") {
			return fmt.Errorf("invalid model pattern")
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

func (s *ModelCatalogService) ModelIsPublished(ctx context.Context, a *Account, model string) (bool, bool) {
	snapshot, err := s.Account(ctx, a)
	if err != nil {
		return false, false
	}
	// An explicit credential rejection is different from missing/expired
	// discovery. A fixed administrator declaration cannot restore revoked access.
	if snapshot.LastError == "authentication_unavailable" {
		return false, true
	}
	// A saved selection is an explicit administrator declaration of account supply.
	// A catalog candidate (or an OAuth manifest omitting media) must not negate
	// that declaration. Authentication failures and explicit retirement still deny.
	declared := accountDeclaresModelSupply(a, model)
	if declared && snapshot.Status == "unavailable" {
		// A fresh/stale snapshot has already checked both account principals in
		// Account(). Only a missing/expired snapshot needs this extra check.
		source, err := resolveCredentialAccount(ctx, s.accounts, a)
		if err != nil || !a.IsActive() || !source.IsActive() {
			return false, false
		}
	}
	for _, entry := range snapshot.Models {
		if entry.ID == model {
			if entry.Lifecycle == "retired" || entry.Access == "unlisted" {
				return false, true
			}
			if declared {
				return true, true
			}
			if snapshot.Status == "unavailable" {
				return false, false
			}
			if entry.Access == "listed" || entry.Access == "observed" {
				return true, true
			}
			// Explicit administrator aliases are retained in legacy/fixed mode.
			return entry.Access == "configured", true
		}
	}
	if declared {
		return true, true
	}
	if snapshot.Status == "unavailable" {
		return false, false
	}
	return false, true
}

func accountDeclaresModelSupply(a *Account, native string) bool {
	if a == nil {
		return false
	}
	if !accountHasModelSelection(a) {
		// Legacy empty mappings already mean unrestricted supply. Existing
		// aliases also declare their targets without a discovery prerequisite.
		if a.IsModelSupported(native) {
			return true
		}
		if !a.IsOpenAIPassthroughEnabled() {
			for alias := range a.GetModelMapping() {
				if a.IsModelSupported(alias) && groupAllowlistPatternMatches(accountCatalogPolicyModel(a, alias), native) {
					return true
				}
			}
		}
		return false
	}
	p, valid := readAccountModelSelection(a)
	if !valid {
		return false
	}
	if len(p.Models) == 0 {
		return true
	}
	for _, selected := range p.Models {
		if groupAllowlistPatternMatches(selected, native) || groupAllowlistPatternMatches(accountCatalogPolicyModel(a, selected), native) {
			return true
		}
	}
	return false
}

// Recheck the credential parent and explicit revocations for saved selections,
// including sticky sessions.
func (s *ModelCatalogService) accountRouteAllowed(ctx context.Context, a *Account, requested string) bool {
	if !accountHasModelSelection(a) {
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

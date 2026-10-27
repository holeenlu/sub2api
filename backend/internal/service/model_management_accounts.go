package service

import (
	"context"
	"slices"
	"sort"
	"strings"
)

const AccountModelReferencesKey = "model_allowlist"

func (a *Account) ModelReferences() []string {
	if a == nil {
		return nil
	}
	if raw, exists := a.Credentials[AccountModelReferencesKey]; exists {
		switch list := raw.(type) {
		case []string:
			return slices.Clone(list)
		case []any:
			out := []string{}
			for _, v := range list {
				if id, ok := v.(string); ok {
					out = append(out, id)
				}
			}
			return out
		}
		return nil
	}
	// The legacy import credential contract is normalized on its next write.
	// Applied migration 298 converts persisted Anthropic/OpenAI records in bulk.
	mapping, _ := a.Credentials["model_mapping"].(map[string]any)
	refs := []string{}
	for id, v := range mapping {
		if _, ok := v.(string); ok {
			refs = append(refs, id)
		}
	}
	sort.Strings(refs)
	return refs
}
func (a *Account) allowsModelReference(model string) bool {
	refs := a.ModelReferences()
	if len(refs) == 0 {
		return true
	}
	return (GroupModelAllowlist{Enabled: true, Models: refs}).Allows(model)
}

// NormalizeAccountModelReferences converts the supported legacy import format
// at the write boundary. It does not promote account routes into global aliases.
func NormalizeAccountModelReferences(platform string, credentials map[string]any) {
	if credentials == nil || (platform != PlatformAnthropic && platform != PlatformOpenAI) {
		return
	}
	if _, exists := credentials[AccountModelReferencesKey]; exists {
		return
	}
	raw, _ := credentials["model_mapping"].(map[string]any)
	if len(raw) == 0 {
		return
	}
	refs := []string{}
	rewrites := map[string]any{}
	for id, v := range raw {
		target, ok := v.(string)
		if !ok {
			continue
		}
		refs = append(refs, id)
		if id != target {
			rewrites[id] = target
		}
	}
	sort.Strings(refs)
	credentials[AccountModelReferencesKey] = refs
	credentials["model_mapping"] = rewrites
}

type accountModelReferencesUpdater interface {
	UpdateModelReferences(context.Context, *Account, []string, map[string]any) error
}

func (s *SettingService) UpdateAccountModelReferences(ctx context.Context, repo AccountRepository, a *Account, expected, models []string) error {
	if a == nil {
		return ErrAccountNotFound
	}
	before := a.ModelReferences()
	sort.Strings(before)
	expected = slices.Clone(expected)
	sort.Strings(expected)
	if !slices.Equal(before, expected) {
		return ErrModelPolicyConflict
	}
	if len(models) > 2000 {
		return invalidModelPolicy("Too many account model references")
	}
	catalog, err := s.ModelReferenceCatalog(ctx, a.Platform)
	if err != nil {
		return err
	}
	enabled := map[string]bool{}
	for _, row := range catalog {
		enabled[row.ID] = row.Enabled
	}
	result := []string{}
	seen := map[string]bool{}
	for _, id := range models {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if !enabled[id] && !slices.Contains(before, id) {
			return invalidModelPolicy("New references must select an enabled catalog model")
		}
		result = append(result, id)
	}
	all := len(result) > 0
	for _, row := range catalog {
		if row.Enabled && !seen[row.ID] {
			all = false
			break
		}
	}
	// All selected is inheritance, so newly enabled catalog entries follow policy.
	if all || len(result) == 0 {
		result = nil
	}
	rewrites := map[string]any{}
	if raw, ok := a.Credentials["model_mapping"].(map[string]any); ok {
		for from, value := range raw {
			if target, ok := value.(string); ok && from != target {
				rewrites[from] = target
			}
		}
	}
	updater, ok := repo.(accountModelReferencesUpdater)
	if !ok {
		return ErrModelPolicyUnavailable
	}
	if err = updater.UpdateModelReferences(ctx, a, result, rewrites); err != nil {
		return err
	}
	a.Credentials[AccountModelReferencesKey] = result
	a.Credentials["model_mapping"] = rewrites
	return nil
}

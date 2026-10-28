package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"slices"
	"sort"
	"strings"
	"unicode"
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
	// Applied migration 306 converts persisted Anthropic/OpenAI records in bulk.
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

// GetAccountModelReferences keeps disabled and historical references visible for removal.
func (s *SettingService) GetAccountModelReferences(ctx context.Context, account *Account) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	return account.ModelReferences(), nil
}

func (a *Account) allowsModelReference(model string) bool {
	if a == nil || ValidateAccountModelReferences(a.Platform, a.Credentials) != nil {
		return false
	}

	refs := a.ModelReferences()
	if len(refs) == 0 {
		return true
	}
	return (catalog.ModelAllowlist{Enabled: true, Models: refs}).Allows(model)
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
	if a.Credentials == nil {
		a.Credentials = map[string]any{}
	}
	a.Credentials[AccountModelReferencesKey] = result
	a.Credentials["model_mapping"] = rewrites
	return nil
}

// ValidateAccountModelReferences distinguishes explicit inheritance (missing,
// null or an empty string array) from corrupt data. Discovery may still expose
// historical values for repair, but corrupt values never grant runtime access.
func ValidateAccountModelReferences(platform string, credentials map[string]any) error {
	if platform != PlatformAnthropic && platform != PlatformOpenAI {
		return nil
	}
	raw, exists := credentials[AccountModelReferencesKey]
	if !exists || raw == nil {
		return nil
	}
	validID := func(id string) bool {
		return id != "" && len(id) <= 256 && !strings.ContainsAny(id, "?\\") && strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
	}
	invalid := func() error {
		return invalidModelPolicy("Account model_allowlist must be null or an array of valid model references")
	}
	switch values := raw.(type) {
	case []string:
		if len(values) > 2000 {
			return invalid()
		}
		for _, id := range values {
			if !validID(id) {
				return invalid()
			}
		}
	case []any:
		if len(values) > 2000 {
			return invalid()
		}
		for _, value := range values {
			id, ok := value.(string)
			if !ok || !validID(id) {
				return invalid()
			}
		}
	default:
		return invalid()
	}
	return nil
}

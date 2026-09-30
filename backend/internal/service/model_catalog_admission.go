package service

import (
	"context"
	"fmt"
	"strings"
)

type catalogPreviewKey struct{}
type catalogAdmissionKey struct{}
type CatalogAdmission struct {
	Models   []GroupCatalogModel
	Required []string
}

// Existing groups keep their compatibility semantics until an administrator
// explicitly saves one of the new access modes. This is the rollout boundary.
func CatalogEnforced(group *Group) bool {
	return group != nil && (group.ModelAllowlist.Mode == "follow" || group.ModelAllowlist.Mode == "fixed")
}

func (s *GroupModelCatalogService) Preview(ctx context.Context, g *Group) (*GroupModelCatalog, error) {
	return s.Resolve(context.WithValue(ctx, catalogPreviewKey{}, true), g)
}

func (s *GroupModelCatalogService) Admit(ctx context.Context, g *Group, ids []string) (context.Context, error) {
	if !CatalogEnforced(g) {
		return ctx, nil
	}
	view, err := s.Resolve(ctx, g)
	if err != nil {
		return ctx, err
	}
	admission := &CatalogAdmission{Required: append([]string(nil), ids...)}
	for _, id := range ids {
		found := false
		for _, m := range view.Models {
			if m.Name == id && m.PricingStatus != "unavailable" {
				admission.Models = append(admission.Models, m)
				found = true
			}
		}
		if !found {
			return ctx, fmt.Errorf("model %q has no published priced route", id)
		}
	}
	return context.WithValue(ctx, catalogAdmissionKey{}, admission), nil
}

func CatalogAccountAllowed(ctx context.Context, a *Account) bool {
	if ctx == nil {
		return false
	}
	admission, _ := ctx.Value(catalogAdmissionKey{}).(*CatalogAdmission)
	if admission == nil {
		return true
	}
	if a == nil {
		return false
	}
	for _, id := range admission.Required {
		found := false
		for _, m := range admission.Models {
			if m.Name != id {
				continue
			}
			if _, ok := m.AccountModels[a.ID]; ok {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func normalizeCatalogModalities(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range in {
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "text", "image", "audio", "video":
			if !seen[value] {
				out = append(out, value)
				seen[value] = true
			}
		}
	}
	return out
}

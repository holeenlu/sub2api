package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

type catalogAdmissionKey struct{}
type CatalogAdmission struct {
	Models   []GroupCatalogModel
	Required []string
}

// CatalogAdmissionError separates publication and pricing failures for
// operators while public responses retain the protocol's generic 503 message.
type CatalogAdmissionError struct {
	Reason, Model, Revision string
	Cause                   error
}

func (e *CatalogAdmissionError) Error() string {
	return fmt.Sprintf("model catalog admission: %s (model %q)", e.Reason, e.Model)
}
func (e *CatalogAdmissionError) Unwrap() error { return e.Cause }

func catalogAdmissionFailure(ctx context.Context, g *Group, model, reason, revision string, cause error) error {
	logger.FromContext(ctx).Warn("model_catalog_admission_rejected",
		zap.Int64("group_id", g.ID),
		zap.String("model", model), zap.String("reason", reason), zap.String("catalog_revision", revision))
	return &CatalogAdmissionError{Reason: reason, Model: model, Revision: revision, Cause: cause}
}

// Every enabled group whitelist uses the same admission path.
func CatalogEnforced(group *Group) bool {
	return group != nil && group.ModelAllowlist.Enabled
}

func (s *GroupModelCatalogService) Admit(ctx context.Context, g *Group, ids []string) (context.Context, error) {
	// Each turn starts with its own admission, including compatibility mode.
	ctx = context.WithValue(ctx, catalogAdmissionKey{}, (*CatalogAdmission)(nil))
	if !CatalogEnforced(g) {
		return ctx, nil
	}
	view, err := s.Resolve(ctx, g)
	if err != nil {
		return ctx, catalogAdmissionFailure(ctx, g, "", "catalog_lookup_failed", "", err)
	}
	admission := &CatalogAdmission{Required: append([]string(nil), ids...)}
	for _, id := range ids {
		found := false
		reason := "no_published_route"
		for _, m := range view.Models {
			if m.Name != id || len(m.AccountModels) == 0 {
				continue
			}
			if m.PricingStatus == "unavailable" {
				reason = "pricing_unavailable"
			} else {
				admission.Models = append(admission.Models, m)
				found = true
			}
		}
		if !found {
			return ctx, catalogAdmissionFailure(ctx, g, id, reason, view.Revision, nil)
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

package service

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// CatalogModelInput edits candidate identities only. Credentials, permissions,
// upstream capability evidence and prices have separate owners.
type CatalogModelInput struct {
	ID          string `json:"id"`
	Platform    string `json:"platform"`
	DisplayName string `json:"display_name"`
	Kind        string `json:"kind"`
	Disabled    bool   `json:"disabled"`
}

func (s *ModelCatalogService) SaveInventoryModel(ctx context.Context, input CatalogModelInput) error {
	input.ID = strings.TrimSpace(input.ID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.ID == "" || len(input.ID) > 256 || strings.ContainsAny(input.ID, "*\r\n\t ") ||
		!isConcreteRequestPlatform(input.Platform) || len(input.DisplayName) > 256 {
		return infraerrors.BadRequest("INVALID_CATALOG_MODEL", "invalid model identity")
	}
	switch input.Kind {
	case "chat", "image", "video", "audio", "embedding", "other":
	default:
		return infraerrors.BadRequest("INVALID_CATALOG_MODEL", "invalid model kind")
	}
	if s.repo == nil {
		return ErrModelCatalogUnavailable
	}
	return s.repo.UpdateRegistry(ctx, func(entries []ModelCatalogEntry) ([]ModelCatalogEntry, error) {
		index := -1
		for i, entry := range entries {
			if entry.ID == input.ID && entry.Platform == input.Platform && entry.SourceAccountID == 0 && entry.UpstreamNamespace == "" {
				index = i
				break
			}
		}
		if index < 0 {
			if len(entries) >= 5000 {
				return nil, infraerrors.BadRequest("CATALOG_LIMIT", "model registry exceeds 5000 entries")
			}
			index = len(entries)
			entries = append(entries, ModelCatalogEntry{ID: input.ID, Platform: input.Platform, Lifecycle: "unknown", Access: "candidate", Source: "registry"})
		}
		entry := &entries[index]
		entry.DisplayName, entry.Kind, entry.Disabled = input.DisplayName, input.Kind, input.Disabled
		entry.Metadata.ID, entry.Metadata.ModelKind = input.ID, input.Kind
		entry.Missing = modelCatalogMissing(input.Kind, entry.Metadata)
		return entries, nil
	})
}

// PricingAudits is retained for billing reconciliation, independently of the
// model inventory UI.
func (s *ModelCatalogService) PricingAudits(ctx context.Context, pending bool) ([]map[string]any, error) {
	return s.repo.PricingAudits(ctx, pending)
}

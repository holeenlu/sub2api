package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type catalogReadContextKey struct{}
type catalogReadCache struct {
	settingsOnce sync.Once
	settings     ModelCatalogSettings
	registryOnce sync.Once
	registry     []ModelCatalogEntry
	mu           sync.Mutex
	snapshots    map[int64]*ModelCatalogSnapshot
}

func withCatalogReadCache(ctx context.Context) context.Context {
	if ctx.Value(catalogReadContextKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, catalogReadContextKey{}, &catalogReadCache{snapshots: map[int64]*ModelCatalogSnapshot{}})
}

type CodexSetupProfile struct {
	Model           string `json:"model"`
	ReviewModel     string `json:"review_model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	ContextWindow   int64  `json:"context_window,omitempty"`
	CatalogRevision string `json:"catalog_revision"`
	Status          string `json:"status"`
}

func (s *ModelCatalogService) SetupProfile(ctx context.Context, key *APIKey) (*CodexSetupProfile, error) {
	out := &CodexSetupProfile{Status: "unavailable"}
	if key == nil || key.Group == nil || key.Group.Platform == PlatformTypeSafe {
		return out, nil
	}
	gateway := &GatewayService{accountRepo: s.accounts, groupCatalog: s.groupCatalog}
	ids := gateway.GetAvailableModels(ctx, &key.Group.ID, key.Group.Platform)
	if len(ids) == 0 {
		ids = DefaultModelsListCandidateIDs(key.Group.Platform)
	}
	ids = FilterCodexModelIDsForGroup(key.Group.ModelAllowlist.FilterForListing(ids), key.Group)
	body, err := gateway.BuildCodexModelsManifestForGroup(ctx, key.Group, "", ids)
	if err != nil {
		return out, err
	}
	out.CatalogRevision = codexModelsManifestBodyETag(body)
	var envelope struct {
		Models []struct {
			Slug             string `json:"slug"`
			Visibility       string `json:"visibility"`
			DefaultReasoning string `json:"default_reasoning_level"`
			Priority         int    `json:"priority"`
			ContextWindow    int64  `json:"context_window"`
		} `json:"models"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil {
		return out, err
	}
	preferred := ""
	preferenceKey := fmt.Sprintf("model_catalog_default_group_%d", key.Group.ID)
	if s.settings != nil && s.settings.settingRepo != nil {
		preferred, _ = s.settings.settingRepo.GetValue(ctx, preferenceKey)
	}
	selected := -1
	for i, m := range envelope.Models {
		if m.Visibility == "hide" || isBackgroundCodexModel(m.Slug) {
			continue
		}
		if selected < 0 {
			selected = i
		}
		if m.Slug == preferred && m.Priority == envelope.Models[selected].Priority {
			selected = i
			break
		}
	}
	for i, m := range envelope.Models {
		if i != selected {
			continue
		}
		if m.Visibility == "hide" || isBackgroundCodexModel(m.Slug) {
			continue
		}
		out.Model = m.Slug
		out.ReviewModel = m.Slug
		out.ContextWindow = m.ContextWindow
		out.Status = "ready"
		if out.Model != preferred && s.settings != nil && s.settings.settingRepo != nil {
			_ = s.settings.settingRepo.Set(ctx, preferenceKey, out.Model)
		}
		if m.DefaultReasoning != "none" {
			out.ReasoningEffort = m.DefaultReasoning
		}
		break
	}
	return out, nil
}

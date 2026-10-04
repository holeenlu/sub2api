package service

import (
	"context"
	"sort"
)

// ModelListCapabilities is the public, optional capability extension to a
// standard model-list entry. Never serialize snapshots or upstream descriptors:
// those also contain account identities, routing details and instructions.
type ModelListCapabilities struct {
	Visibility               string   `json:"visibility,omitempty"`
	ModelPurpose             string   `json:"model_purpose,omitempty"`
	ModelKind                string   `json:"model_kind,omitempty"`
	Reasoning                *bool    `json:"reasoning,omitempty"`
	SupportedReasoningLevels []string `json:"supported_reasoning_levels,omitempty"`
	DefaultReasoningLevel    string   `json:"default_reasoning_level,omitempty"`
	InputModalities          []string `json:"input_modalities,omitempty"`
	OutputModalities         []string `json:"output_modalities,omitempty"`
	ContextWindow            int64    `json:"context_window,omitempty"`
	MaxContextWindow         int64    `json:"max_context_window,omitempty"`
	MaxOutputTokens          int64    `json:"max_output_tokens,omitempty"`
}

// ResolveForListing keeps the ordinary model inventory intact, including media
// and entries without capability evidence. Only persisted, credential-scoped
// facts shared by every eligible supplier are added; no upstream calls occur.
func (s *GroupModelCatalogService) ResolveForListing(ctx context.Context, group *Group) (*GroupModelCatalog, map[string]ModelListCapabilities, error) {
	ctx = withCatalogReadCache(ctx)
	view, err := s.Resolve(ctx, group)
	if err != nil || s.registry == nil {
		return view, nil, err
	}
	known := map[int64]map[string]UpstreamModelMetadata{}
	for _, account := range view.accounts {
		a := &account
		if !isCatalogAccountActive(a) {
			continue
		}
		snapshot, err := s.registry.Account(ctx, a)
		if err != nil || snapshot == nil || snapshot.Status == "unavailable" {
			continue
		}
		known[a.ID] = map[string]UpstreamModelMetadata{}
		for _, entry := range snapshot.Models {
			if entry.Lifecycle == "retired" || entry.Disabled {
				continue
			}
			metadata := entry.Metadata
			metadata.ModelKind = entry.Kind
			known[a.ID][entry.ID] = metadata
		}
	}
	// The same public name can have several endpoint/platform routes. Include
	// all suppliers so one route cannot advertise another route's larger limits.
	candidates := map[string][]UpstreamModelMetadata{}
	for _, model := range view.Models {
		ids := make([]int64, 0, len(model.AccountModels))
		for id := range model.AccountModels {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		if len(ids) == 0 {
			candidates[model.Name] = append(candidates[model.Name], UpstreamModelMetadata{})
		}
		for _, id := range ids {
			// A missing supplier contributes unknown facts, not permission to use
			// another account's capabilities as a substitute.
			candidates[model.Name] = append(candidates[model.Name], known[id][model.AccountModels[id]])
		}
	}
	capabilities := make(map[string]ModelListCapabilities, len(candidates))
	for name, entries := range candidates {
		capability := intersectModelListCapabilities(entries)
		capability.Visibility, capability.ModelPurpose = ModelPresentation(name, capability.Visibility, capability.ModelPurpose)
		capabilities[name] = capability
	}
	return view, capabilities, nil
}

func intersectModelListCapabilities(entries []UpstreamModelMetadata) ModelListCapabilities {
	if len(entries) == 0 {
		return ModelListCapabilities{}
	}
	shared := intersectUpstreamModelMetadata("", entries)
	out := ModelListCapabilities{
		Visibility: shared.Visibility, ModelPurpose: shared.ModelPurpose,
		Reasoning: shared.Reasoning, SupportedReasoningLevels: shared.SupportedReasoningLevels,
		DefaultReasoningLevel: shared.DefaultReasoningLevel,
		ContextWindow:         shared.ContextWindow, MaxContextWindow: shared.MaxContextWindow,
		ModelKind: entries[0].ModelKind, MaxOutputTokens: entries[0].MaxOutputTokens,
		InputModalities:  normalizeCatalogModalities(entries[0].InputModalities),
		OutputModalities: normalizeCatalogModalities(entries[0].OutputModalities),
	}
	if shared.reasoningConflict {
		out.Reasoning, out.SupportedReasoningLevels, out.DefaultReasoningLevel = nil, nil, ""
	}
	for _, entry := range entries[1:] {
		if entry.ModelKind != out.ModelKind {
			out.ModelKind = ""
		}
		out.InputModalities = intersectOrderedStrings(out.InputModalities, normalizeCatalogModalities(entry.InputModalities))
		out.OutputModalities = intersectOrderedStrings(out.OutputModalities, normalizeCatalogModalities(entry.OutputModalities))
		if entry.MaxOutputTokens < out.MaxOutputTokens {
			out.MaxOutputTokens = entry.MaxOutputTokens
		}
	}
	if out.MaxOutputTokens < 0 {
		out.MaxOutputTokens = 0
	}
	return out
}

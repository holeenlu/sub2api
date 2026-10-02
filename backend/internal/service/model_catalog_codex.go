package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

func (s *ModelCatalogService) CodexManifest(ctx context.Context, group *Group) ([]byte, error) {
	ctx = withCatalogReadCache(ctx)
	if s.groupCatalog == nil || group == nil {
		return nil, ErrModelCatalogUnavailable
	}
	view, err := s.groupCatalog.Resolve(ctx, group)
	if err != nil {
		return nil, err
	}
	accounts, err := s.accounts.ListByGroup(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	known := map[int64]map[string]ModelCatalogEntry{}
	for i := range accounts {
		a := &accounts[i]
		if !isCatalogAccountActive(a) {
			continue
		}
		snapshot, err := s.Account(ctx, a)
		if err != nil {
			continue
		}
		known[a.ID] = map[string]ModelCatalogEntry{}
		for _, entry := range snapshot.Models {
			allowed, _ := s.ModelIsPublished(ctx, a, entry.ID)
			if allowed && entry.Lifecycle != "retired" {
				known[a.ID][entry.ID] = entry
			}
		}
	}
	ids := []string{}
	metadata := map[string]codexModelMetadataOverride{}
	targets := map[string]string{}
	images := map[string]bool{}
	search := map[string]bool{}
	nativeMessages := map[string]json.RawMessage{}
	nativeContracts := map[string]map[string]json.RawMessage{}
	priorities := map[string]int{}
	for _, model := range view.Models {
		if model.PricingStatus == "unavailable" {
			continue
		}
		if model.Endpoint != "" && model.Endpoint != "any" && model.Endpoint != "responses" {
			continue
		}
		candidates := []UpstreamModelMetadata{}
		ready := len(model.AccountModels) > 0
		target := ""
		sameTarget := true
		var commonMessages json.RawMessage
		messagesMatch := true
		priority := 50
		hasPriority := false
		commonContract := map[string]json.RawMessage{}
		routeIDs := make([]int64, 0, len(model.AccountModels))
		for id := range model.AccountModels {
			routeIDs = append(routeIDs, id)
		}
		sort.Slice(routeIDs, func(i, j int) bool { return routeIDs[i] < routeIDs[j] })
		for _, accountID := range routeIDs {
			native := model.AccountModels[accountID]
			entry, exists := known[accountID][native]
			if !exists || entry.Kind != "chat" || len(entry.Missing) > 0 {
				ready = false
				break
			}
			if target == "" {
				target = native
			} else if target != native {
				sameTarget = false
			}
			candidates = append(candidates, entry.Metadata)
			if entry.Metadata.RecommendedPriority != nil && (!hasPriority || *entry.Metadata.RecommendedPriority < priority) {
				priority = *entry.Metadata.RecommendedPriority
				hasPriority = true
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(entry.CodexModel, &fields)
			if len(candidates) == 1 {
				for key, value := range fields {
					commonContract[key] = value
				}
			} else {
				for key, value := range commonContract {
					if string(value) != string(fields[key]) {
						delete(commonContract, key)
					}
				}
			}
			raw := fields["model_messages"]
			if len(candidates) == 1 {
				commonMessages = raw
			} else if string(commonMessages) != string(raw) {
				messagesMatch = false
			}
		}
		if !ready || len(candidates) == 0 || isCodexDedicatedMediaModel(model.Name) {
			continue
		}
		merged := intersectUpstreamModelMetadata(model.Name, candidates)
		if merged.reasoningConflict || merged.inputModalitiesConflict {
			continue
		}
		ids = append(ids, model.Name)
		metadata[model.Name] = merged
		if sameTarget {
			targets[model.Name] = target
		}
		images[model.Name] = stringSliceContains(merged.InputModalities, "image")
		search[model.Name] = string(merged.CodexToolCapabilities["supports_search_tool"]) == "true"
		priorities[model.Name] = priority
		nativeContracts[model.Name] = commonContract
		if messagesMatch && len(commonMessages) > 0 {
			nativeMessages[model.Name] = commonMessages
		}
	}
	body, err := buildCodexModelsManifest(ids, images, search, targets, metadata)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	for _, model := range envelope.Models {
		var slug string
		_ = json.Unmarshal(model["slug"], &slug)
		for field, value := range nativeContracts[slug] {
			switch field {
			case "slug", "display_name", "description", "priority", "input_modalities", "context_window", "max_context_window", "max_output_tokens", "supported_reasoning_levels", "default_reasoning_level":
				continue
			}
			model[field] = value
		}
		if value := nativeMessages[slug]; len(value) > 0 {
			model["model_messages"] = value
		}
		model["priority"], _ = json.Marshal(priorities[slug])
	}
	sort.SliceStable(envelope.Models, func(i, j int) bool {
		var left, right int
		_ = json.Unmarshal(envelope.Models[i]["priority"], &left)
		_ = json.Unmarshal(envelope.Models[j]["priority"], &right)
		return left < right
	})
	return json.Marshal(envelope)
}

func (s *ModelCatalogService) SetupProfile(ctx context.Context, key *APIKey) (*CodexSetupProfile, error) {
	out := &CodexSetupProfile{Status: "unavailable"}
	if key == nil || key.Group == nil {
		return out, nil
	}
	var body []byte
	var err error
	if !CatalogEnforced(key.Group) && s.syncer != nil && s.syncer.openaiGatewayService != nil {
		var manifest *OpenAIModelsResponse
		manifest, _, err = s.syncer.openaiGatewayService.BuildGroupConfiguredCodexModelsManifest(ctx, key.Group, "")
		if manifest != nil {
			body = manifest.Body
		}
		if len(body) == 0 {
			body, err = s.CodexManifest(ctx, key.Group)
		}
	} else {
		body, err = s.CodexManifest(ctx, key.Group)
	}
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
		if m.Visibility == "hide" || strings.HasPrefix(m.Slug, "codex-auto-") {
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
		if m.Visibility == "hide" || strings.HasPrefix(m.Slug, "codex-auto-") {
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

func (s *OpenAIGatewayService) PublishedCodexCatalog(ctx context.Context, group *Group) (*OpenAIModelsResponse, bool, error) {
	if s.modelCatalog == nil || !CatalogEnforced(group) {
		return nil, false, nil
	}
	body, err := s.modelCatalog.CodexManifest(ctx, group)
	if err != nil {
		return nil, true, err
	}
	return &OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}, true, nil
}

// ApplyPinnedCodexCatalogPolicy keeps the selected live source as the
// discovery boundary while using the persisted model catalog as the
// authoritative descriptor and capability boundary for enforced groups. This
// prevents a pinned upstream response from advertising capabilities that the
// routed account intersection has not published.
func (s *OpenAIGatewayService) ApplyPinnedCodexCatalogPolicy(ctx context.Context, group *Group, source *OpenAIModelsResponse, ifNoneMatch string) error {
	if source == nil || source.NotModified || group == nil {
		return nil
	}
	if s.modelCatalog == nil || !CatalogEnforced(group) {
		return s.MergeGroupConfiguredCodexModels(ctx, group, source, ifNoneMatch)
	}
	governed, err := s.modelCatalog.CodexManifest(ctx, group)
	if err != nil {
		return err
	}
	body, err := restrictCodexManifestToSource(governed, source.Body)
	if err != nil {
		return err
	}
	source.Body = body
	source.ETag = codexModelsManifestBodyETag(body)
	if codexModelsManifestETagMatches(ifNoneMatch, source.ETag) {
		source.Body = nil
		source.NotModified = true
	}
	return nil
}

// restrictCodexManifestToSource intersects a governed manifest with the live
// selected-source slugs. The governed descriptors and order are retained; the
// source only decides which of those published models may be returned.
func restrictCodexManifestToSource(governed, source []byte) ([]byte, error) {
	governedEnvelope, governedEntries, err := modelCatalogEntries(governed, "models")
	if err != nil {
		return nil, err
	}
	_, sourceEntries, err := modelCatalogEntries(source, "models")
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(sourceEntries))
	for _, raw := range sourceEntries {
		var model struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, err
		}
		if slug := strings.TrimSpace(model.Slug); slug != "" {
			allowed[slug] = struct{}{}
		}
	}
	filtered := make([]json.RawMessage, 0, len(governedEntries))
	for _, raw := range governedEntries {
		var model struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, err
		}
		if _, ok := allowed[strings.TrimSpace(model.Slug)]; ok {
			filtered = append(filtered, raw)
		}
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	governedEnvelope["models"] = encoded
	return json.Marshal(governedEnvelope)
}

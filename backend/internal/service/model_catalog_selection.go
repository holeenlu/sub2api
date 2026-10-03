package service

import (
	"context"
	"strings"
	"time"
)

// SelectionCatalog is an administrator's inventory for configuring supply,
// never evidence that an account can serve a model.
func (s *ModelCatalogService) SelectionCatalog(ctx context.Context, account *Account, platform string) (*ModelCatalogSnapshot, error) {
	return s.selectionCatalog(ctx, account, platform, false)
}

// Inventory includes disabled entries so an administrator can restore them.
// Disabling a candidate never silently changes existing group/account grants.
func (s *ModelCatalogService) Inventory(ctx context.Context, platform string) (*ModelCatalogSnapshot, error) {
	return s.selectionCatalog(ctx, nil, platform, true)
}

func (s *ModelCatalogService) selectionCatalog(ctx context.Context, account *Account, platform string, includeDisabled bool) (*ModelCatalogSnapshot, error) {
	if account != nil {
		platform = account.Platform
	}
	snapshot, err := s.Platform(ctx, platform)
	if err != nil {
		return nil, err
	}
	out := *snapshot
	out.Models = append([]ModelCatalogEntry(nil), snapshot.Models...)
	known := map[string]bool{}
	for _, entry := range out.Models {
		known[entry.Platform+"\x00"+entry.ID] = true
	}
	add := func(entry ModelCatalogEntry) {
		key := entry.Platform + "\x00" + entry.ID
		if entry.ID == "" || known[key] || (platform != "" && entry.Platform != platform) {
			return
		}
		known[key] = true
		entry.Access = "candidate"
		out.Models = append(out.Models, entry)
	}
	accounts, err := s.accounts.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		a := &accounts[i]
		if platform != "" && a.Platform != platform {
			continue
		}
		ids := configuredUpstreamModelsForCapabilitySync(a)
		for alias := range a.GetModelMapping() {
			if !strings.Contains(alias, "*") {
				ids = append(ids, alias)
			}
		}
		for _, id := range ids {
			m, _ := a.GetUpstreamModelMetadata(id)
			kind := modelCatalogEntryKind(id, m)
			add(ModelCatalogEntry{ID: id, Platform: a.Platform, Kind: kind, Metadata: m, Lifecycle: modelCatalogLifecycle(m, time.Now()), Source: "administrator", Missing: modelCatalogMissing(kind, m)})
		}
	}
	// Reuse the upstream group presets; administrators extend this inventory at runtime.
	platforms := []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformOpenCodeGo, PlatformTypeSafe}
	for _, p := range platforms {
		if platform != "" && platform != p {
			continue
		}
		for _, id := range defaultModelsListCandidateIDs(p) {
			m := UpstreamModelMetadata{ID: id}
			add(ModelCatalogEntry{ID: id, Platform: p, Kind: modelCatalogEntryKind(id, m), Lifecycle: "unknown", Source: "preset", Metadata: m})
		}
	}
	curated := map[string]ModelCatalogEntry{}
	for _, entry := range s.Registry(ctx) {
		if entry.SourceAccountID == 0 && entry.UpstreamNamespace == "" {
			curated[entry.Platform+"\x00"+entry.ID] = entry
		}
	}
	models := make([]ModelCatalogEntry, 0, len(out.Models))
	for _, entry := range out.Models {
		if override, ok := curated[entry.Platform+"\x00"+entry.ID]; ok {
			entry.Disabled = override.Disabled
			if override.DisplayName != "" {
				entry.DisplayName = override.DisplayName
			}
			if override.Kind != "" {
				entry.Kind = override.Kind
			}
		}
		if !includeDisabled && entry.Disabled {
			continue
		}
		models = append(models, entry)
	}
	out.Models = modelCatalogNormalizeEntries(models)
	out.Status = "ready"
	out.Revision = modelCatalogHash(out.Models)
	return &out, nil
}

package service

import (
	"context"
	"strings"
	"time"
)

// SelectionCatalog is an administrator's inventory for configuring supply,
// never evidence that an account can serve a model. Keep it separate from the
// account discovery snapshot consumed by routing and public catalogs.
func (s *ModelCatalogService) SelectionCatalog(ctx context.Context, account *Account, platform string) (*ModelCatalogSnapshot, error) {
	return s.selectionCatalog(ctx, account, platform, false)
}

// Inventory includes disabled entries so an administrator can restore them.
// Disabling a candidate never silently changes existing group/account grants.
func (s *ModelCatalogService) Inventory(ctx context.Context, platform string) (*ModelCatalogSnapshot, error) {
	return s.selectionCatalog(ctx, nil, platform, true)
}

func (s *ModelCatalogService) selectionCatalog(ctx context.Context, account *Account, platform string, includeDisabled bool) (*ModelCatalogSnapshot, error) {
	var snapshot *ModelCatalogSnapshot
	var err error
	if account != nil {
		platform = account.Platform
		snapshot, err = s.Account(ctx, account)
	} else {
		snapshot, err = s.Platform(ctx, platform)
	}
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
	// A single-account editor can also choose platform inventory. This avoids
	// hiding models omitted by the OAuth Codex manifest (notably media models).
	if account != nil {
		inventory, err := s.Platform(ctx, platform)
		if err != nil {
			return nil, err
		}
		for _, entry := range inventory.Models {
			add(entry)
		}
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
		for _, id := range configuredUpstreamModelsForCapabilitySync(a) {
			m, _ := a.GetUpstreamModelMetadata(id)
			kind := modelCatalogEntryKind(id, m)
			add(ModelCatalogEntry{ID: id, Platform: a.Platform, Kind: kind, Metadata: m, Lifecycle: modelCatalogLifecycle(m, time.Now()), Source: "administrator", Missing: modelCatalogMissing(kind, m)})
		}
	}
	// Price identities can fill holes in upstream discovery, but only in this
	// admin inventory. Neither prices nor another account grant access.
	if s.prices != nil {
		s.prices.mu.RLock()
		for id, price := range s.prices.pricingData {
			if price == nil {
				continue
			}
			provider := strings.ToLower(strings.TrimSpace(price.LiteLLMProvider))
			switch provider {
			case "vertex_ai", "vertex_ai-image-models", "gemini":
				provider = PlatformGemini
			case "xai":
				provider = PlatformGrok
			case PlatformOpenAI, PlatformGrok, PlatformAnthropic:
			default:
				continue // Do not guess ownership from an opaque model name.
			}
			kind := modelCatalogEntryKind(codexProviderQualifiedModelID(id), UpstreamModelMetadata{})
			if strings.Contains(price.Mode, "image") {
				kind = "image"
			} else if strings.Contains(price.Mode, "video") {
				kind = "video"
			}
			if kind != "image" && kind != "video" {
				continue
			}
			m := UpstreamModelMetadata{ID: id, ModelKind: kind, OutputModalities: []string{kind}}
			add(ModelCatalogEntry{ID: id, Platform: provider, Kind: kind, Metadata: m, Lifecycle: "unknown", Source: "pricing_catalog", Missing: []string{}})
		}
		s.prices.mu.RUnlock()
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
	out.Revision = modelCatalogHash(out.Models)
	return &out, nil
}

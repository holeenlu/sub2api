package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// GroupCatalogModel is a public request identity. BillingModels and account
// details never cross the public HTTP boundary.
type GroupCatalogModel struct {
	PricingStatus     string           `json:"pricing_status,omitempty"`
	AccountModels     map[int64]string `json:"-"`
	Name              string           `json:"name"`
	Platform          string           `json:"platform"`
	Endpoint          string           `json:"endpoint"`
	Source            string           `json:"source"`
	BillingModels     []string         `json:"-"`
	ResponseDependent bool             `json:"-"`
	ChannelName       string           `json:"-"`
}

type GroupCatalogIssue struct {
	Model  string `json:"model"`
	Reason string `json:"reason"`
}

type GroupModelCatalog struct {
	Revision  string              `json:"revision"`
	Models    []GroupCatalogModel `json:"models"`
	Issues    []GroupCatalogIssue `json:"issues"`
	Status    string              `json:"status"`
	UpdatedAt time.Time           `json:"updated_at"`
}

type groupCatalogAccounts interface {
	ListByGroup(context.Context, int64) ([]Account, error)
}
type groupCatalogSnapshots interface {
	ModelCatalogSnapshot(context.Context, *Account) ([]string, time.Time, bool)
}

// GroupModelCatalogService reads configuration and existing discovery snapshots.
// Public page loads never fetch upstream catalogs or probe inference endpoints.
type GroupModelCatalogService struct {
	legacySnapshots groupCatalogSnapshots
	registry        *ModelCatalogService
	accounts        groupCatalogAccounts
	channels        ChannelRepository
	routes          CompositeModelRouteRepository
	snapshots       groupCatalogSnapshots
}

func NewGroupModelCatalogService(accounts AccountRepository, channels ChannelRepository, routes CompositeModelRouteRepository, upstream *OpenAIGatewayService) *GroupModelCatalogService {
	catalog := &GroupModelCatalogService{accounts: accounts, channels: channels, routes: routes, snapshots: upstream, legacySnapshots: upstream}
	if upstream != nil {
		upstream.groupModelCatalog = catalog
	}
	return catalog
}

func (s *GroupModelCatalogService) Resolve(ctx context.Context, group *Group) (*GroupModelCatalog, error) {
	channels, err := s.channels.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog channels: %w", err)
	}
	return s.resolve(ctx, group, channels)
}

func (s *GroupModelCatalogService) resolve(ctx context.Context, group *Group, channels []Channel) (*GroupModelCatalog, error) {
	if s.registry != nil && !CatalogEnforced(group) && ctx.Value(catalogPreviewKey{}) != true {
		legacy := *s
		legacy.registry = nil
		legacy.snapshots = s.legacySnapshots
		return legacy.resolve(ctx, group, channels)
	}
	ctx = withCatalogReadCache(ctx)
	if group == nil {
		return nil, fmt.Errorf("catalog group is required")
	}
	out := &GroupModelCatalog{Models: []GroupCatalogModel{}, Issues: []GroupCatalogIssue{}, Status: "ready", UpdatedAt: group.UpdatedAt}
	accounts, err := s.accounts.ListByGroup(ctx, group.ID)
	if err != nil {
		return nil, fmt.Errorf("catalog accounts: %w", err)
	}
	active := make([]Account, 0, len(accounts))
	platforms := map[string]bool{}
	for _, a := range accounts {
		if !isPinnedCodexModelsAccountUsable(&a) || IsRetiredPlatform(a.Platform) || (group.RequireOAuthOnly && a.Type == AccountTypeAPIKey) {
			continue
		}
		if group.Platform != PlatformComposite && a.Platform != group.Platform && !mixedListingAccountAllowed(group.Platform, &a) {
			continue
		}
		active = append(active, a)
		platforms[a.Platform] = true
		if a.UpdatedAt.After(out.UpdatedAt) {
			out.UpdatedAt = a.UpdatedAt
		}
	}
	var channel *Channel
	for i := range channels {
		ch := &channels[i]
		if !ch.IsActive() {
			continue
		}
		for _, id := range ch.GroupIDs {
			if id == group.ID {
				if channel != nil {
					return nil, fmt.Errorf("multiple active channels for group %d", group.ID)
				}
				cp := *ch
				cp.normalizeBillingModelSource()
				channel = &cp
				if cp.UpdatedAt.After(out.UpdatedAt) {
					out.UpdatedAt = cp.UpdatedAt
				}
			}
		}
	}
	var channelIndex *channelCache
	if channel != nil {
		channelIndex = populateChannelCache([]Channel{*channel}, map[int64]string{group.ID: group.Platform})
	}
	var routes []CompositeModelRoute
	if group.Platform == PlatformComposite && s.routes != nil {
		routes, err = s.routes.ListByGroup(ctx, group.ID, false)
		if err != nil {
			return nil, fmt.Errorf("catalog routes: %w", err)
		}
		for _, route := range routes {
			if route.UpdatedAt.After(out.UpdatedAt) {
				out.UpdatedAt = route.UpdatedAt
			}
		}
	}
	type candidate struct{ name, platform, source string }
	candidates := map[string]candidate{}
	issueSeen := map[string]bool{}
	issue := func(model, reason string) {
		key := model + "\x00" + reason
		if !issueSeen[key] {
			out.Issues = append(out.Issues, GroupCatalogIssue{model, reason})
			issueSeen[key] = true
		}
	}
	add := func(name, platform, source string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if strings.Contains(name, "*") {
			issue(name, "wildcard_requires_concrete_models")
			return
		}
		key := platform + "\x00" + name
		if s.registry == nil {
			key = strings.ToLower(key)
		}
		if _, ok := candidates[key]; !ok {
			candidates[key] = candidate{name, platform, source}
		}
	}
	pinned := group.Platform == PlatformOpenAI && group.CodexModelsManifestConfig.Enabled
	pinnedIDs := map[int64]bool{}
	for _, id := range group.CodexModelsManifestConfig.AccountIDs {
		pinnedIDs[id] = true
	}
	if pinned && s.registry != nil && group.CodexModelsManifestConfig.FallbackToScheduler {
		usable := false
		for _, a := range active {
			if pinnedIDs[a.ID] {
				usable = true
				break
			}
		}
		if !usable {
			pinned = false
			issue("", "discovery_fallback_used")
		}
	}
	snapshotModels := map[string]bool{}
	foundSnapshots := 0
	var oldestSnapshot time.Time
	for i := range active {
		a := &active[i]
		if pinned && !pinnedIDs[a.ID] {
			continue
		}
		platform := a.Platform
		if group.Platform != PlatformComposite {
			platform = group.Platform
		}
		var ids []string
		var at time.Time
		var found bool
		if s.snapshots != nil {
			ids, at, found = s.snapshots.ModelCatalogSnapshot(ctx, a)
		}
		if pinned && !found {
			out.Status = "unavailable"
			issue("", "discovery_snapshot_missing")
			continue
		}
		if found {
			foundSnapshots++
			if time.Since(at) > 15*time.Minute && out.Status == "ready" {
				out.Status = "stale"
			}
			// For discovered catalogs report the oldest snapshot, not response time.
			if oldestSnapshot.IsZero() || at.Before(oldestSnapshot) {
				oldestSnapshot = at
			}
			for _, id := range ids {
				if group.Platform != PlatformComposite && a.Platform != group.Platform && !mixedListingModelAllowed(group.Platform, id) {
					continue
				}
				add(id, platform, "discovery")
				snapshotModels[strings.ToLower(platform+"\x00"+id)] = true
			}
		}
		if pinned {
			continue
		}
		if !a.IsOpenAIPassthroughEnabled() {
			for id, target := range stringMappingFromRaw(a.Credentials["model_mapping"]) {
				if strings.TrimSpace(target) == "" {
					continue
				}
				if a.Platform != platform && !mixedListingModelAllowed(platform, id) {
					continue
				}
				add(id, platform, "account_mapping")
			}
		}
	}
	if !oldestSnapshot.IsZero() {
		out.UpdatedAt = oldestSnapshot
	}
	if pinned && foundSnapshots == 0 {
		out.Status = "unavailable"
		issue("", "discovery_snapshot_missing")
	}
	if !pinned {
		if channel != nil {
			for platform, mapping := range channel.ModelMapping {
				if group.Platform != PlatformComposite && platform != group.Platform {
					continue
				}
				for id := range mapping {
					add(id, platform, "channel_mapping")
				}
			}
		}
		for _, route := range routes {
			if route.Enabled && route.MatchType == CompositeRouteMatchExact {
				add(route.PublicModel, route.TargetPlatform, "route")
			}
		}
		if group.ModelAllowlistEnabled() {
			for _, id := range group.ModelAllowlist.Models {
				if group.Platform == PlatformComposite {
					for p := range platforms {
						add(id, p, "group_selection")
					}
				} else {
					add(id, group.Platform, "group_selection")
				}
			}
		}
	}
	ordered := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		ordered = append(ordered, c)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].name != ordered[j].name {
			return ordered[i].name < ordered[j].name
		}
		return ordered[i].platform < ordered[j].platform
	})
	seen := map[string]bool{}
	for _, c := range ordered {
		if !group.ModelAllowlist.Allows(c.name) || (!GroupAllowsImageGeneration(group) && IsImageGenerationIntent("", c.name, nil)) {
			issue(c.name, "group_policy_excluded")
			continue
		}
		endpoints := []string{CompositeRouteEndpointAny}
		if group.Platform == PlatformComposite {
			endpointSet := map[string]bool{CompositeRouteEndpointAny: true}
			for _, r := range routes {
				if r.Enabled && r.Endpoint != CompositeRouteEndpointAny && (r.PublicModel == c.name || (r.MatchType == CompositeRouteMatchPrefix && strings.HasPrefix(c.name, r.PublicModel))) {
					endpointSet[r.Endpoint] = true
				}
			}
			endpoints = nil
			for e := range endpointSet {
				endpoints = append(endpoints, e)
			}
			sort.Strings(endpoints)
		}
		accepted := false
		for _, endpoint := range endpoints {
			platform, requestModel := c.platform, c.name
			if group.Platform == PlatformComposite {
				if route, ok := matchCompositeRoute(routes, c.name, endpoint); ok {
					platform = route.TargetPlatform
					requestModel = route.UpstreamModel
					if requestModel == "" {
						requestModel = c.name
					}
				} else {
					ownership := map[string]bool{}
					for _, a := range active {
						if explicitModelMappingClaims(a, c.name) {
							ownership[a.Platform] = true
						}
					}
					if len(ownership) > 1 {
						issue(c.name, "ambiguous_route")
						continue
					}
					if len(ownership) == 1 {
						for p := range ownership {
							platform = p
						}
					} else if p, ok := DetectModelPlatform(c.name); ok {
						platform = p
					} else {
						continue
					}
				}
			}
			if !isConcreteRequestPlatform(platform) {
				continue
			}
			mapped := requestModel
			if channel != nil {
				if m := lookupMappingAcrossPlatforms(channelIndex, group.ID, platform, strings.ToLower(requestModel)); m != "" {
					mapped = m
				}
			}
			billingSet := map[string]bool{}
			accountModels := map[int64]string{}
			responseDependent := channel != nil && channel.BillingModelSource == BillingModelSourceResponse
			for i := range active {
				a := &active[i]
				if a.Platform != platform && !(mixedListingAccountAllowed(platform, a) && mixedListingModelAllowed(platform, mapped)) {
					continue
				}
				if pinned && !pinnedIDs[a.ID] {
					continue
				}
				if !a.IsModelSupported(mapped) {
					continue
				}
				billing := resolveAccountUpstreamModel(a, mapped)
				if platform == PlatformOpenAI {
					billing = resolveOpenAIAccountUpstreamModelForRequest(a, mapped, false)
				}
				if s.registry != nil {
					allowed, known := s.registry.ModelIsPublished(ctx, a, billing)
					if !known || !allowed {
						continue
					}
				}
				if a.IsOpenAIPassthroughEnabled() {
					billing = mapped
				}
				actualModel := billing
				if channel != nil {
					switch channel.BillingModelSource {
					case BillingModelSourceRequested:
						billing = requestModel
					case BillingModelSourceChannelMapped:
						if mapped != requestModel {
							billing = mapped
						}
					case BillingModelSourceResponse:
						billing = mapped
					}
					restrictionModel := billingModelForRestriction(channel.BillingModelSource, requestModel, mapped)
					if restrictionModel == "" {
						restrictionModel = billing
					}
					if channel.RestrictModels && lookupPricingAcrossPlatforms(channelIndex, group.ID, platform, strings.ToLower(restrictionModel)) == nil {
						continue
					}
				}
				if strings.TrimSpace(billing) != "" {
					billingSet[billing] = true
					accountModels[a.ID] = actualModel
				}
			}
			if !GroupAllowsImageGeneration(group) {
				for billing := range billingSet {
					if IsImageGenerationIntent("", billing, nil) {
						delete(billingSet, billing)
					}
				}
			}
			if len(billingSet) == 0 {
				continue
			}
			if pinned && !snapshotModels[strings.ToLower(platform+"\x00"+c.name)] {
				continue
			}
			key := c.name + "\x00" + platform + "\x00" + endpoint
			if s.registry == nil {
				key = strings.ToLower(key)
			}
			accepted = true
			if seen[key] {
				continue
			}
			seen[key] = true
			m := GroupCatalogModel{AccountModels: accountModels, Name: c.name, Platform: platform, Endpoint: endpoint, Source: c.source, ResponseDependent: responseDependent}
			for name := range billingSet {
				m.BillingModels = append(m.BillingModels, name)
			}
			sort.Strings(m.BillingModels)
			if channel != nil {
				m.ChannelName = channel.Name
			}
			if s.registry != nil {
				m.PricingStatus = s.registry.quoteStatus(ctx, &m, group)
				if m.PricingStatus == "unavailable" {
					issue(m.Name, "pricing_unavailable")
				}
			}
			out.Models = append(out.Models, m)
		}
		if !accepted {
			issue(c.name, "no_configured_route")
		}
	}
	if channel != nil {
		for _, m := range channel.SupportedModels() {
			found := false
			for _, included := range out.Models {
				if strings.EqualFold(included.Name, m.Name) {
					found = true
					break
				}
			}
			if !found {
				issue(m.Name, "pricing_only_or_not_allowed")
			}
		}
	}
	sort.Slice(out.Issues, func(i, j int) bool {
		if out.Issues[i].Model != out.Issues[j].Model {
			return out.Issues[i].Model < out.Issues[j].Model
		}
		return out.Issues[i].Reason < out.Issues[j].Reason
	})
	if !pinned && len(candidates) == 0 && len(active) > 0 && foundSnapshots == 0 {
		out.Status = "unavailable"
		issue("", "discovery_snapshot_missing")
	}
	if pinned && out.Status == "unavailable" {
		// Never publish an incomplete pinned union as a complete directory.
		out.Models = []GroupCatalogModel{}
	}
	if group.ModelAllowlistEnabled() {
		// Keep the configured selection order, expanding patterns in place.
		rank := func(name string) int {
			for i, pattern := range group.ModelAllowlist.Models {
				if (GroupModelAllowlist{Enabled: true, Models: []string{pattern}}).Allows(name) {
					return i
				}
			}
			return len(group.ModelAllowlist.Models)
		}
		sort.SliceStable(out.Models, func(i, j int) bool { return rank(out.Models[i].Name) < rank(out.Models[j].Name) })
	}
	priceRevision := ""
	if s.registry != nil && s.registry.prices != nil {
		priceRevision = s.registry.prices.PriceRevision()
	}
	out.Revision = modelCatalogHash(struct {
		Models []GroupCatalogModel
		Issues []GroupCatalogIssue
		Policy GroupModelAllowlist
		Price  string
	}{out.Models, out.Issues, group.ModelAllowlist, priceRevision})
	return out, nil
}

// ModelIDs deduplicates endpoint variants without losing group selection order.
func (c *GroupModelCatalog) ModelIDs() []string {
	ids := make([]string, 0, len(c.Models))
	seen := map[string]bool{}
	for _, m := range c.Models {
		if m.PricingStatus == "unavailable" {
			continue
		}
		if !seen[m.Name] {
			seen[m.Name] = true
			ids = append(ids, m.Name)
		}
	}
	return ids
}

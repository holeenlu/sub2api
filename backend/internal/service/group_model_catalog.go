package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
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
	accounts  map[int64]Account
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
	groups    GroupRepository
	registry  *ModelCatalogService
	accounts  groupCatalogAccounts
	channels  ChannelRepository
	routes    CompositeModelRouteRepository
	snapshots groupCatalogSnapshots
}

func NewGroupModelCatalogService(accounts AccountRepository, channels ChannelRepository, routes CompositeModelRouteRepository, upstream *OpenAIGatewayService) *GroupModelCatalogService {
	catalog := &GroupModelCatalogService{accounts: accounts, channels: channels, routes: routes, snapshots: upstream}
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
	ctx = withCatalogReadCache(ctx)
	if group == nil {
		return nil, fmt.Errorf("catalog group is required")
	}
	out := &GroupModelCatalog{accounts: map[int64]Account{}, Models: []GroupCatalogModel{}, Issues: []GroupCatalogIssue{}, Status: "ready", UpdatedAt: group.UpdatedAt}
	accounts, err := s.accounts.ListByGroup(ctx, group.ID)
	if err != nil {
		return nil, fmt.Errorf("catalog accounts: %w", err)
	}
	active := make([]Account, 0, len(accounts))
	platforms := map[string]bool{}
	for _, a := range accounts {
		if !isCatalogAccountActive(&a) || IsUnsupportedPlatform(a.Platform) || (group.RequireOAuthOnly && a.Type == AccountTypeAPIKey) {
			continue
		}
		if group.Platform != PlatformComposite && a.Platform != group.Platform && !mixedListingAccountAllowed(group.Platform, &a) {
			continue
		}
		active = append(active, a)
		out.accounts[a.ID] = a
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
	// Every active member contributes its persisted snapshot. A fixed Codex
	// source only chooses the live manifest fetch accounts; it never narrows
	// this shared inventory or changes the inference account pool.
	foundSnapshots := 0
	var oldestSnapshot time.Time
	for i := range active {
		a := &active[i]
		platform := a.Platform
		if group.Platform != PlatformComposite {
			platform = group.Platform
		}
		if platform == PlatformTypeSafe && a.Type == AccountTypeAPIKey {
			add(typesafe.JevLatestModel, platform, "native_protocol")
		}
		var ids []string
		var at time.Time
		var found bool
		if s.snapshots != nil {
			ids, at, found = s.snapshots.ModelCatalogSnapshot(ctx, a)
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
			}
		}
		// Use native mappings, including platform defaults, independently of discovery.
		if !a.IsOpenAIPassthroughEnabled() {
			for id, target := range a.GetModelMapping() {
				if strings.TrimSpace(target) == "" {
					continue
				}
				if a.Platform != platform && !mixedListingModelAllowed(platform, id) {
					continue
				}
				add(id, platform, "account_mapping")
			}
		}
		if !found && (a.IsOpenAIPassthroughEnabled() || len(a.GetModelMapping()) == 0) {
			for _, id := range DefaultModelsListCandidateIDs(platform) {
				add(id, platform, "platform_default")
			}
		}
	}
	if !oldestSnapshot.IsZero() {
		out.UpdatedAt = oldestSnapshot
	}
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
			if platform == PlatformTypeSafe {
				if requestModel != typesafe.JevLatestModel || c.name != typesafe.JevLatestModel {
					continue
				}
				endpoint = "systemone"
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
				if platform == PlatformTypeSafe && a.Type != AccountTypeAPIKey {
					continue
				}
				if a.Platform != platform && !(mixedListingAccountAllowed(platform, a) && mixedListingModelAllowed(platform, mapped)) {
					continue
				}
				// Every active group member that supplies the selected model
				// remains eligible, independently of discovery source selection.
				if !a.IsModelSupported(mapped) {
					continue
				}
				billing := resolveAccountUpstreamModel(a, mapped)
				if platform == PlatformOpenAI {
					billing = resolveOpenAIAccountUpstreamModelForRequest(a, mapped, false)
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
	if group.FallbackGroupIDOnNoAccount != nil && s.groups != nil {
		chain := newNoAccountFallbackChain(&group.ID, s.groups.GetByIDLite, nil)
		chain.source = group
		for target := chain.next(ctx); target != nil; target = chain.next(ctx) {
			if target.ClaudeCodeOnly && !IsClaudeCodeClient(ctx) {
				continue
			}
			borrowed := *target
			borrowed.FallbackGroupIDOnNoAccount = nil
			// The target supplies capacity; the requesting key owns model access.
			borrowed.ModelAllowlist = group.ModelAllowlist
			borrowed.RequireOAuthOnly = group.RequireOAuthOnly || target.RequireOAuthOnly
			view, err := s.resolve(ctx, &borrowed, channels)
			if err != nil {
				return nil, err
			}
			for id, account := range view.accounts {
				out.accounts[id] = account
			}
			for _, entry := range view.Models {
				if !group.ModelAllowlist.Allows(entry.Name) || (!GroupAllowsImageGeneration(group) && IsImageGenerationIntent("", entry.Name, nil)) {
					continue
				}
				mapped := entry.Name
				if channel != nil {
					if value := lookupMappingAcrossPlatforms(channelIndex, group.ID, entry.Platform, strings.ToLower(entry.Name)); value != "" {
						mapped = value
					}
					restricted := billingModelForRestriction(channel.BillingModelSource, entry.Name, mapped)
					if restricted != "" && channel.RestrictModels && lookupPricingAcrossPlatforms(channelIndex, group.ID, entry.Platform, strings.ToLower(restricted)) == nil {
						continue
					}
				}
				// Reprice with the origin group. Never advertise the target group's tariff.
				billing := make([]string, 0, len(entry.AccountModels))
				for _, model := range entry.AccountModels {
					billing = append(billing, model)
				}
				entry.BillingModels = dedupeAndSortModelIDs(billing)
				entry.ChannelName, entry.ResponseDependent = "", false
				if channel != nil {
					entry.ChannelName = channel.Name
					switch channel.BillingModelSource {
					case BillingModelSourceRequested:
						entry.BillingModels = []string{entry.Name}
					case BillingModelSourceChannelMapped, BillingModelSourceResponse:
						entry.BillingModels = []string{mapped}
					}
					entry.ResponseDependent = channel.BillingModelSource == BillingModelSourceResponse
				}
				entry.Source = "fallback"
				if s.registry != nil {
					entry.PricingStatus = s.registry.quoteStatus(ctx, &entry, group)
				}
				merged := false
				for i := range out.Models {
					current := &out.Models[i]
					if current.Name != entry.Name || current.Platform != entry.Platform || current.Endpoint != entry.Endpoint {
						continue
					}
					for id, model := range entry.AccountModels {
						current.AccountModels[id] = model
					}
					current.BillingModels = dedupeAndSortModelIDs(append(current.BillingModels, entry.BillingModels...))
					current.ResponseDependent = current.ResponseDependent || entry.ResponseDependent
					if s.registry != nil {
						current.PricingStatus = s.registry.quoteStatus(ctx, current, group)
					}
					merged = true
					break
				}
				if !merged {
					out.Models = append(out.Models, entry)
				}
			}
			if view.UpdatedAt.After(out.UpdatedAt) {
				out.UpdatedAt = view.UpdatedAt
			}
		}
		if chain.loadFailed {
			out.Status = "stale"
			issue("", "fallback_group_unavailable")
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
	if !group.ModelAllowlistEnabled() && len(out.Models) == 0 && len(candidates) == 0 && len(active) > 0 && foundSnapshots == 0 {
		out.Status = "unavailable"
		issue("", "discovery_snapshot_missing")
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

func (s *GroupModelCatalogService) fallbackCodexInputs(ctx context.Context, group *Group, ids []string, accounts []Account) ([]string, []Account) {
	if s == nil || group == nil || group.FallbackGroupIDOnNoAccount == nil {
		return ids, accounts
	}
	view, err := s.Resolve(ctx, group)
	if err != nil {
		return ids, accounts
	}
	seen := map[int64]bool{}
	for _, account := range accounts {
		seen[account.ID] = true
	}
	for _, model := range view.Models {
		if model.Platform == PlatformTypeSafe {
			continue
		}
		if model.Source == "fallback" {
			ids = append(ids, model.Name)
		}
		for id := range model.AccountModels {
			if !seen[id] {
				accounts = append(accounts, view.accounts[id])
				seen[id] = true
			}
		}
	}
	return FilterCodexModelIDsForGroup(ids, group), accounts
}

// ModelIDs deduplicates endpoint variants without losing group selection order.
func (c *GroupModelCatalog) ModelIDs() []string {
	ids := make([]string, 0, len(c.Models))
	seen := map[string]bool{}
	for _, m := range c.Models {
		if !seen[m.Name] {
			seen[m.Name] = true
			ids = append(ids, m.Name)
		}
	}
	return ids
}

// Catalog visibility ignores transient load/rate limits, but respects persistent
// account status, scheduling opt-out and auto-paused subscription expiry.
func isCatalogAccountActive(account *Account) bool {
	if account == nil || !account.IsActive() || !account.Schedulable {
		return false
	}
	return !account.AutoPauseOnExpired || account.ExpiresAt == nil || account.ExpiresAt.After(time.Now())
}

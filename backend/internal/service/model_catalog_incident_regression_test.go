//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestModelCatalogFixedSupplyCannotOverrideAuthenticationRevocation(t *testing.T) {
	ctx := context.Background()
	s, repo, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"gpt-image-listed"}]}`))
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"gpt-image-listed", "gpt-image-manual"}}}
	_, err := s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	stored := repo.snapshots[modelCatalogSourceKey(a.ID)]
	for _, state := range []struct {
		status, reason string
		allowed        bool
	}{
		{"stale", "upstream_unavailable", true},
		{"unavailable", "catalog_expired", true},
		{"unavailable", "authentication_unavailable", false},
		{"ready", "", true},
	} {
		t.Run(state.reason+state.status, func(t *testing.T) {
			stored.Status, stored.LastError = state.status, state.reason
			for _, id := range []string{"gpt-image-listed", "gpt-image-manual"} {
				allowed, _ := s.ModelIsPublished(ctx, a, id)
				require.Equal(t, state.allowed, allowed, id)
			}
		})
	}
}

func TestModelCatalogLegacyOpenAISchedulerRechecksPublication(t *testing.T) {
	ctx := context.Background()
	s, _, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"gpt-image-selected"}]}`), catalogResponse(200, `{"data":[]}`))
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"gpt-image-selected"}}}
	_, err := s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	svc := &OpenAIGatewayService{modelCatalog: s, accountRepo: s.accounts}
	require.NotNil(t, svc.resolveFreshSchedulableOpenAIAccountBeforeProfit(ctx, a, PlatformOpenAI, "gpt-image-selected", false, ""))
	require.NotNil(t, svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(ctx, a, nil, PlatformOpenAI, "gpt-image-selected", false, ""))
	_, err = s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	require.True(t, a.IsModelSupported("gpt-image-selected"), "the local fixed policy alone does not know about explicit upstream removal")
	require.Nil(t, svc.resolveFreshSchedulableOpenAIAccountBeforeProfit(ctx, a, PlatformOpenAI, "gpt-image-selected", false, ""))
	require.Nil(t, svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(ctx, a, nil, PlatformOpenAI, "gpt-image-selected", false, ""))
}

func TestModelCatalogReadinessUsesRequestPriceGeneration(t *testing.T) {
	id := "incident-price-generation"
	priced := map[string]*LiteLLMModelPricing{id: {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, ProvidedFields: map[string]bool{"input_cost_per_token": true, "output_cost_per_token": true}}}
	for _, initiallyPriced := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-price-cannot-admit-old-request", true: "price-removal-cannot-reject-old-request"}[initiallyPriced], func(t *testing.T) {
			prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
			if initiallyPriced {
				prices.pricingData = priced
			}
			billing := NewBillingService(&config.Config{}, prices)
			group := &Group{ID: 1, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{id}}}
			gateway := &GatewayService{billingService: billing}
			ctx := gateway.PinRequestPricing(context.Background(), &APIKey{GroupID: &group.ID, Group: group})
			prices.pricingData = priced
			if initiallyPriced {
				prices.pricingData = map[string]*LiteLLMModelPricing{}
			}
			catalog := &ModelCatalogService{prices: prices, pricingResolver: NewModelPricingResolver(nil, billing)}
			expected := "unavailable"
			if initiallyPriced {
				expected = "ready"
			}
			require.Equal(t, expected, catalog.quoteStatus(ctx, &GroupCatalogModel{BillingModels: []string{id}}, group))
		})
	}
}

func TestModelCatalogCompatibilityAdmissionClearsPreviousTurn(t *testing.T) {
	a := &Account{ID: 17}
	ctx := context.WithValue(context.Background(), catalogAdmissionKey{}, &CatalogAdmission{Required: []string{"previous"}})
	require.False(t, CatalogAccountAllowed(ctx, a))
	catalog := &GroupModelCatalogService{}
	next, err := catalog.Admit(ctx, &Group{ModelAllowlist: GroupModelAllowlist{Enabled: false}}, []string{"allowed"})
	require.NoError(t, err)
	require.True(t, CatalogAccountAllowed(next, a))
}

func TestModelCatalogConfiguredRetirementStillAppliesWithoutSnapshot(t *testing.T) {
	s, _, a, _ := newCatalogTestService()
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"gpt-image-retired"}}}
	a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"gpt-image-retired": {ID: "gpt-image-retired", ShutdownDate: "2000-01-01"},
	}})
	allowed, known := s.ModelIsPublished(context.Background(), a, "gpt-image-retired")
	require.True(t, known)
	require.False(t, allowed)
}

func TestModelCatalogAdmissionDiagnosesRoutePriceAndStorageFailures(t *testing.T) {
	id := "gpt-image-incident"
	s, _, a, _ := newCatalogTestService()
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{id}}}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	// The explicit token card below prevents per-image defaults from satisfying admission.
	s.prices = prices
	s.pricingResolver = &ModelPricingResolver{billingService: NewBillingService(&config.Config{}, prices)}
	group := &Group{ID: 101, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{id}}}
	channels := &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, nil }}
	catalog := &GroupModelCatalogService{accounts: s.accounts, channels: channels, registry: s, snapshots: s}
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "incident-request")))
	assertFailure := func(model, reason string) {
		t.Helper()
		_, err := catalog.Admit(ctx, group, []string{model})
		var rejected *CatalogAdmissionError
		require.ErrorAs(t, err, &rejected)
		require.Equal(t, reason, rejected.Reason)
		entry := logs.All()[logs.Len()-1]
		require.Equal(t, "incident-request", entry.ContextMap()["request_id"])
		require.Equal(t, reason, entry.ContextMap()["reason"])
		require.NotContains(t, entry.ContextMap(), "credentials")
	}
	assertFailure("not-supplied", "no_published_route")
	// Token pricing override suppresses the image fallback, and only complete
	// prices may release the fixed supply into a priced group route.
	group.AllowImageGeneration = true
	zero := 0.0
	group.ModelPricing = []ChannelModelPricing{{Models: []string{id}, BillingMode: BillingModeToken, InputPrice: &zero}}
	assertFailure(id, "pricing_unavailable")
	group.ModelPricing[0].OutputPrice = &zero
	admitted, err := catalog.Admit(ctx, group, []string{id})
	require.NoError(t, err)
	require.True(t, CatalogAccountAllowed(admitted, a))
	require.False(t, CatalogAccountAllowed(admitted, &Account{ID: 99}))
	storageErr := errors.New("storage unavailable")
	channels.listAllFn = func(context.Context) ([]Channel, error) { return nil, storageErr }
	assertFailure(id, "catalog_lookup_failed")
	_, err = catalog.Admit(ctx, group, []string{id})
	require.ErrorIs(t, err, storageErr)
}

//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func managedConsumerSettings(t *testing.T) (*SettingService, *settingRepoStub) {
	t.Helper()
	repo := &settingRepoStub{values: map[string]string{}}
	return NewSettingService(repo, nil), repo
}
func setConsumerPolicy(t *testing.T, repo *settingRepoStub, p *ModelPolicy) {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	repo.mu.Lock()
	repo.values[ModelManagementKeyPrefix+p.Platform] = string(raw)
	repo.mu.Unlock()
}
func TestManagedConsumerRechecksDisabledAliasAndAccountRewrite(t *testing.T) {
	ctx := context.Background()
	settings, repo := managedConsumerSettings(t)
	p := modelPolicySeed(PlatformOpenAI)
	p.Platform = PlatformOpenAI
	p.Models["blocked"] = ManagedModel{ID: "blocked", Enabled: true}
	p.Models["allowed"] = ManagedModel{ID: "allowed", Enabled: true}
	p.Aliases = map[string]string{"alias": "blocked"}
	setConsumerPolicy(t, repo, p)
	require.NoError(t, settings.CheckManagedModel(ctx, PlatformOpenAI, "alias"))
	row := p.Models["blocked"]
	row.Enabled = false
	p.Models["blocked"] = row
	setConsumerPolicy(t, repo, p)
	require.ErrorIs(t, settings.CheckManagedModel(ctx, PlatformOpenAI, "alias"), ErrManagedModelNotFound, "live dispatch must ignore cached enabled policy")
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{AccountModelReferencesKey: []string{"allowed"}, "model_mapping": map[string]any{"allowed": "blocked"}}}
	require.ErrorIs(t, settings.checkManagedAccountModels(ctx, account, "allowed", "blocked"), ErrManagedModelNotFound)
	require.False(t, account.IsModelSupported("blocked"))
}
func TestManagedConsumerGlobalMappingCannotBypassTargetPolicy(t *testing.T) {
	ctx := context.Background()
	settings, repo := managedConsumerSettings(t)
	p := modelPolicySeed(PlatformOpenAI)
	p.Platform = PlatformOpenAI
	p.Models["disabled"] = ManagedModel{ID: "disabled", Enabled: false}
	setConsumerPolicy(t, repo, p)
	raw, err := json.Marshal(ModelMappings{Mappings: map[string]string{"custom": "disabled"}})
	require.NoError(t, err)
	repo.mu.Lock()
	repo.values[ModelManagementKeyPrefix+"mappings"] = string(raw)
	repo.mu.Unlock()
	require.ErrorIs(t, settings.CheckManagedModel(ctx, PlatformOpenAI, "custom"), ErrManagedModelNotFound)
}
func TestManagedAccountReferencesSeparateAdmissionAndRewrite(t *testing.T) {
	credentials := map[string]any{"api_key": "secret", "model_mapping": map[string]any{"same": "same", "alias": "target"}}
	NormalizeAccountModelReferences(PlatformOpenAI, credentials)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: credentials}
	require.Equal(t, []string{"alias", "same"}, account.ModelReferences())
	require.Equal(t, map[string]any{"alias": "target"}, credentials["model_mapping"])
	require.True(t, account.IsModelSupported("same"))
	require.False(t, account.IsModelSupported("target"))
	require.Equal(t, "target", account.GetMappedModel("alias"))
	account.Credentials[AccountModelReferencesKey] = []string{}
	require.True(t, account.IsModelSupported("new-inherited"))
	require.Equal(t, "target", account.GetMappedModel("alias"))
	refs, err := (&SettingService{}).GetAccountModelReferences(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, refs)
}
func TestManagedPricingPinnedAndDurableCardRetainsModesAndTiers(t *testing.T) {
	ctx := context.Background()
	settings, repo := managedConsumerSettings(t)
	price := ManagedPrice{ManagedTokenPrice: ManagedTokenPrice{Input: managedConsumerFloat(1), Output: managedConsumerFloat(2), CacheRead: managedConsumerFloat(0)}, Modes: map[string]*ManagedTokenPrice{"priority": {Input: managedConsumerFloat(9), Output: managedConsumerFloat(10), CacheRead: managedConsumerFloat(0)}}}
	raw, err := json.Marshal(ManagedPrices{Overrides: map[string]ManagedPrice{"managed-custom": price}})
	require.NoError(t, err)
	repo.mu.Lock()
	repo.values[ModelManagementKeyPrefix+"pricing"] = string(raw)
	repo.mu.Unlock()
	prices := &PricingService{modelSettings: settings}
	calculator := NewBillingService(nil, prices)
	resolver := NewModelPricingResolver(nil, calculator)
	pinned, err := resolver.Pin(ctx, time.Now())
	require.NoError(t, err)
	require.True(t, pinned.HasIdentifiedTokenPricing("managed-custom"))
	resolved := pinned.Resolve(ctx, PricingInput{Model: "managed-custom"})
	require.NotNil(t, resolved.BasePricing)
	frozen, err := FreezeNativeUsagePricing(resolved, nil, false)
	require.NoError(t, err)
	restored, err := frozen.Resolved.Restore()
	require.NoError(t, err)
	restoredCard, tier, err := prepareManagedPricing(restored.BasePricing, UsageTokens{InputTokens: 1}, "priority", true)
	require.NoError(t, err)
	require.Empty(t, tier)
	require.Equal(t, 9.0/1e6, restoredCard.InputPricePerToken)
	// A later publication must not alter the request's quote or recovery card.
	price.Input = managedConsumerFloat(99)
	raw, err = json.Marshal(ManagedPrices{Overrides: map[string]ManagedPrice{"managed-custom": price}})
	require.NoError(t, err)
	repo.mu.Lock()
	repo.values[ModelManagementKeyPrefix+"pricing"] = string(raw)
	repo.mu.Unlock()
	again := pinned.Resolve(ctx, PricingInput{Model: "managed-custom"})
	require.Equal(t, 1.0/1e6, again.BasePricing.InputPricePerToken)
	_, err = calculator.getModelPricingFrom("unknown-unpriced", time.Now(), pinned.resolver.pricingSnapshot)
	require.True(t, errors.Is(err, ErrModelPricingUnavailable))
}

func managedConsumerFloat(v float64) *float64 { return &v }

func TestManagedAliasPlanUsesCanonicalAccountReferencesAndDisableRevokes(t *testing.T) {
	ctx := context.Background()
	settings, repo := managedConsumerSettings(t)
	p := modelPolicySeed(PlatformOpenAI)
	p.Platform = PlatformOpenAI
	setConsumerPolicy(t, repo, p)
	canonical := "gpt-6.1-sol"
	raw, err := json.Marshal(ModelMappings{Mappings: map[string]string{"globalalias": canonical}})
	require.NoError(t, err)
	repo.mu.Lock()
	repo.values[ModelManagementKeyPrefix+"mappings"] = string(raw)
	repo.mu.Unlock()
	channels := &ChannelService{modelSettings: settings}
	mapping, restricted := channels.ResolveChannelMappingAndRestrict(ctx, nil, "globalalias")
	require.False(t, restricted)
	require.True(t, mapping.Mapped)
	require.Equal(t, canonical, mapping.MappedModel)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{AccountModelReferencesKey: []string{canonical}, "model_mapping": map[string]any{canonical: "native-target"}}}
	require.NoError(t, settings.checkManagedAccountModels(ctx, account, "globalalias", "native-target"))
	service := &OpenAIGatewayService{settingService: settings}
	plan, err := service.planResponseExecution(ctx, account, mapping.MappedModel)
	require.NoError(t, err)
	require.Equal(t, "native-target", plan.upstreamModel)
	row := p.Models[canonical]
	row.Enabled = false
	p.Models[canonical] = row
	setConsumerPolicy(t, repo, p)
	require.ErrorIs(t, settings.checkManagedAccountModels(ctx, account, "globalalias", "native-target"), ErrManagedModelNotFound)
}
func TestManagedManualPriceOverlayFlowsThroughBillingAndRecovery(t *testing.T) {
	ctx := context.Background()
	settings, repo := managedConsumerSettings(t)
	price := ManagedPrice{ManagedTokenPrice: ManagedTokenPrice{Input: managedConsumerFloat(10), Output: managedConsumerFloat(2), CacheRead: managedConsumerFloat(0)}}
	raw, err := json.Marshal(ManagedPrices{Overrides: map[string]ManagedPrice{"gpt-6.1-sol": price}})
	require.NoError(t, err)
	repo.mu.Lock()
	repo.values[ModelManagementKeyPrefix+"pricing"] = string(raw)
	repo.mu.Unlock()
	calculator := NewBillingService(nil, &PricingService{modelSettings: settings})
	resolver := NewModelPricingResolver(nil, calculator)
	pinned, err := resolver.Pin(ctx, time.Now())
	require.NoError(t, err)
	resolved := pinned.Resolve(ctx, PricingInput{Model: "gpt-6.1-sol"})
	frozen, err := FreezeNativeUsagePricing(resolved, nil, false)
	require.NoError(t, err)
	restored, err := frozen.Resolved.Restore()
	require.NoError(t, err)
	input := CostInput{Ctx: ctx, Model: "gpt-6.1-sol", Tokens: UsageTokens{InputTokens: 10, OutputTokens: 2}, RateMultiplier: 1, Resolver: resolver, Resolved: restored}
	quote, err := calculator.CalculateCostUnified(input)
	require.NoError(t, err)
	require.InDelta(t, 0.000104, quote.TotalCost, 1e-12)
	// The durable card is the sole authority even after the backing policy changes.
	repo.mu.Lock()
	repo.values[ModelManagementKeyPrefix+"pricing"] = `{"overrides":{}}`
	repo.mu.Unlock()
	repeat, err := calculator.CalculateCostUnified(input)
	require.NoError(t, err)
	require.Equal(t, quote.TotalCost, repeat.TotalCost)
}

func TestManagedAccountReferencesRejectMalformedShape(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		for _, tc := range []struct {
			name  string
			raw   any
			valid bool
		}{
			{"string", "gpt-6.1-sol", false}, {"number", float64(1), false},
			{"nonstring_array", []any{float64(1)}, false}, {"mixed_array", []any{"gpt-6.1-sol", true}, false},
			{"blank_reference", []string{""}, false}, {"whitespace_reference", []string{"bad model"}, false},
			{"null", nil, true}, {"empty_string_array", []string{}, true}, {"empty_json_array", []any{}, true},
			{"historical_wildcard", []string{"historical-*"}, true},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				credentials := map[string]any{AccountModelReferencesKey: tc.raw}
				a := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: credentials}
				err := ValidateAccountModelReferences(platform, credentials)
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					require.False(t, a.allowsModelReference("gpt-6.1-sol"))
					require.False(t, a.IsModelSupported("gpt-6.1-sol"))
					// Invalid create input is rejected before any repository side effect.
					_, err := (&AccountService{}).Create(context.Background(), CreateAccountRequest{Platform: platform, Credentials: credentials})
					require.Error(t, err)
					NormalizeAccountModelReferences(platform, credentials)
					require.Equal(t, tc.raw, credentials[AccountModelReferencesKey], "normalization must not convert corrupt configuration into inheritance")
				}
			})
		}
	}
	// This check does not reinterpret another platform's credential contract.
	require.NoError(t, ValidateAccountModelReferences(PlatformGemini, map[string]any{AccountModelReferencesKey: "legacy"}))
}

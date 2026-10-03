//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type manualCatalogRepo struct{ *catalogMemoryRepo }

func (r *manualCatalogRepo) SaveJob(context.Context, ModelCatalogJob) error { return nil }

func TestModelCatalogManualSyncWorksWithoutAutomaticSchedule(t *testing.T) {
	s, repo, a, _ := newCatalogTestService(catalogResponse(http.StatusOK, `{"data":[{"id":"gpt-image-future"}]}`))
	s.repo = &manualCatalogRepo{repo}
	// Model discovery must finish even when the independent pricing client is unavailable.
	s.prices = &PricingService{cfg: &config.Config{Pricing: config.PricingConfig{RemoteURL: "https://pricing.invalid"}}}
	s.settings = &SettingService{settingRepo: &catalogSettingsMemory{values: map[string]string{ModelCatalogSettingsKey: `{"enabled":false}`}}}
	require.False(t, s.Settings(context.Background()).Enabled)
	// Manual sync must bypass due-date scheduling, and does not need Start().
	job := s.QueueSync()
	require.Equal(t, "running", job.Status)
	require.Eventually(t, func() bool {
		current, ok := s.Job(context.Background(), job.ID)
		return ok && current.Status == "complete"
	}, time.Second, 5*time.Millisecond)
	current, ok := s.Job(context.Background(), job.ID)
	require.True(t, ok)
	require.Equal(t, 1, current.Succeeded)
	require.Zero(t, current.Failed)
	stored, err := repo.Current(context.Background(), modelCatalogSourceKey(a.ID))
	require.NoError(t, err)
	require.Equal(t, "gpt-image-future", stored.Models[0].ID)
	s.Stop()
}

func TestModelCatalogRefreshPreservesFailureReasonForJob(t *testing.T) {
	s, _, a, _ := newCatalogTestService(catalogResponse(http.StatusUnauthorized, `{}`))
	defer s.Stop()
	_, err := s.Refresh(context.Background(), a.ID, true)
	require.ErrorIs(t, err, ErrModelCatalogUnavailable)
	require.Equal(t, "authentication_unavailable", catalogSyncErrorCode(err))
}

// Building a discovery request can fall back to the stored token. This does
// not prove that upstream will accept it; HTTP authentication still decides.
func TestModelCatalogOpenAIDiscoveryFallsBackWhenTokenRefreshFails(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	account := &Account{ID: 18, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "stale-db-token",
		"expires_at":   expired.Format(time.RFC3339),
	}}
	provider := NewOpenAITokenProvider(nil, newOpenAITokenCacheStub(), nil)
	_, tokenErr := provider.GetAccessToken(context.Background(), account)
	require.Error(t, tokenErr, "test needs a provider that actually fails")

	syncer := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{openAITokenProvider: provider}}
	req, err := syncer.buildOpenAIOAuthUpstreamModelsRequest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "Bearer stale-db-token", req.Header.Get("Authorization"))
}

func TestModelCatalogOpenAIDiscoveryUsesTokenProvider(t *testing.T) {
	account := &Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "stale-db-token"}}
	cache := newOpenAITokenCacheStub()
	cache.tokens[OpenAITokenCacheKey(account)] = "current-cached-token"
	syncer := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{openAITokenProvider: NewOpenAITokenProvider(nil, cache, nil)}}
	req, err := syncer.buildOpenAIOAuthUpstreamModelsRequest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "Bearer current-cached-token", req.Header.Get("Authorization"))
}

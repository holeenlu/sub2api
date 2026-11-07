//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type modelControlCASRepo struct{ settingRepoStub }

func (r *modelControlCASRepo) CompareAndSwapModelSettings(ctx context.Context, key string, expected int64, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var current, next struct {
		Version int64 `json:"version"`
	}
	if raw := r.values[key]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &current); err != nil {
			return err
		}
	}
	if err := json.Unmarshal([]byte(value), &next); err != nil {
		return err
	}
	if current.Version != expected || next.Version != expected+1 {
		return ErrModelPolicyConflict
	}
	r.values[key] = value
	return nil
}
func newModelControlSettings() (*SettingService, *modelControlCASRepo) {
	r := &modelControlCASRepo{settingRepoStub: settingRepoStub{values: map[string]string{}}}
	return NewSettingService(r, nil), r
}
func TestModelControlSeedAndJSONArrays(t *testing.T) {
	s, r := newModelControlSettings()
	ctx := context.Background()
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformGrok, PlatformCline} {
		p, err := s.GetModelPolicy(ctx, platform, true)
		require.NoError(t, err)
		require.Equal(t, int64(0), p.Version)
		require.NoError(t, validateModelPolicy(p))
		raw, err := json.Marshal(p)
		require.NoError(t, err)
		require.NotContains(t, string(raw), `"aliases":null`)
		require.NotContains(t, string(raw), `"context_1m_whitelist":null`)
	}
	p := modelPolicySeed(PlatformGemini)
	p.Defaults.Context1MWhitelist = nil
	row := p.Models["gemini-3.8-flash"]
	row.Aliases = nil
	p.Models[row.ID] = row
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	r.values[ModelManagementKeyPrefix+PlatformGemini] = string(raw)
	p, err = s.GetModelPolicy(ctx, PlatformGemini, true)
	require.NoError(t, err)
	require.NotNil(t, p.Defaults.Context1MWhitelist)
	require.NotNil(t, p.Models[row.ID].Aliases)
}
func TestModelControlCASConcurrentAndIndependentSections(t *testing.T) {
	s, _ := newModelControlSettings()
	ctx := context.Background()
	seed, err := s.GetModelPolicy(ctx, PlatformOpenAI, true)
	require.NoError(t, err)
	raw, err := json.Marshal(seed)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var p ModelPolicy
			_ = json.Unmarshal(raw, &p)
			_, err := s.SaveModelPolicy(ctx, PlatformOpenAI, p)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	passed, conflicted := 0, 0
	for err := range results {
		if err == nil {
			passed++
		} else if errors.Is(err, ErrModelPolicyConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	require.Equal(t, 1, passed)
	require.Equal(t, 1, conflicted)
	p, err := s.GetModelPolicy(ctx, PlatformOpenAI, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), p.Version)
	m, err := s.SaveModelMappings(ctx, ModelMappings{Version: 0, Mappings: map[string]string{ModelAutoReviewAlias: ModelAutoReviewDefault}})
	require.NoError(t, err)
	require.Equal(t, int64(1), m.Version)
	zero := 0.0
	price := &ManagedPrice{ManagedTokenPrice: ManagedTokenPrice{Input: &zero, Output: &zero, CacheRead: &zero}}
	prices, err := s.SaveManagedPrice(ctx, PlatformOpenAI, "gpt-6.1-sol", 0, price)
	require.NoError(t, err)
	require.Equal(t, int64(1), prices.Version)
	_, err = s.SaveManagedPrice(ctx, PlatformAnthropic, "claude-haiku-4-5-20251001", 0, price)
	require.ErrorIs(t, err, ErrModelPolicyConflict)
	_, err = s.ResetModelPolicy(ctx, PlatformOpenAI, 0)
	require.ErrorIs(t, err, ErrModelPolicyConflict)
	prices, err = s.SaveManagedPrice(ctx, PlatformOpenAI, "gpt-6.1-sol", 1, nil)
	require.NoError(t, err)
	require.Empty(t, prices.Overrides)
}
func TestModelControlSyncRetainsDisabledAndUnparticipatingSources(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			s, _ := newModelControlSettings()
			ctx := context.Background()
			p, err := s.SyncModelPolicy(ctx, platform, 0, map[int64]*UpstreamModelCatalog{9: {Models: []string{"source-model", "disabled-model"}}, 10: {Models: []string{"other-model", "source-model"}}})
			require.NoError(t, err)
			row := p.Models["disabled-model"]
			row.Enabled = false
			p.Models[row.ID] = row
			p, err = s.SaveModelPolicy(ctx, platform, *p)
			require.NoError(t, err)
			p, err = s.SyncModelPolicy(ctx, platform, p.Version, map[int64]*UpstreamModelCatalog{9: {Models: []string{"source-model"}}})
			require.NoError(t, err)
			require.False(t, p.Models["disabled-model"].Enabled)
			require.Equal(t, []int64{9}, p.Models["disabled-model"].SourceAccountIDs)
			require.Equal(t, []int64{10}, p.Models["other-model"].SourceAccountIDs)
			require.Equal(t, []int64{9, 10}, p.Models["source-model"].SourceAccountIDs)
			p, err = s.SyncModelPolicy(ctx, platform, p.Version, map[int64]*UpstreamModelCatalog{9: {Models: []string{"disabled-model", "new-model"}}})
			require.NoError(t, err)
			require.False(t, p.Models["disabled-model"].Enabled)
			require.NotNil(t, p.Models["new-model"].Aliases)
			require.NotNil(t, p.Defaults.Context1MWhitelist)
			rows, err := s.ModelReferenceCatalog(ctx, platform)
			require.NoError(t, err)
			found := false
			for _, row := range rows {
				if row.ID == "disabled-model" {
					found = true
					require.False(t, row.Enabled)
				}
			}
			require.True(t, found)
			_, err = s.SyncModelPolicy(ctx, platform, p.Version, map[int64]*UpstreamModelCatalog{9: {}})
			require.Error(t, err)
			after, err := s.GetModelPolicy(ctx, platform, true)
			require.NoError(t, err)
			require.Equal(t, p, after)
		})
	}
}
func TestModelControlPriceValidationAndUnpricedCards(t *testing.T) {
	zero := 0.0
	bad := -1.0
	for _, card := range []ManagedPrice{{}, {ManagedTokenPrice: ManagedTokenPrice{Input: &zero, Output: &zero, CacheRead: &bad}}} {
		require.Error(t, validateManagedPrice(&card, PlatformOpenAI))
	}
	p := ManagedPrice{ManagedTokenPrice: ManagedTokenPrice{Input: &zero, Output: &zero, CacheRead: &zero}, Modes: map[string]*ManagedTokenPrice{"priority": nil}, Tiers: []ManagedPriceTier{{Above: 100, Unpriced: true}}}
	require.NoError(t, validateManagedPrice(&p, PlatformAnthropic))
	require.NotNil(t, p.Cache5m)
	require.Equal(t, 0.0, *p.Cache5m)
}

// The registry transport must never receive a supplier account identity or
// credentials. A race at publication is still a CAS conflict, not a source 502.
type modelControlPriceHTTP struct {
	serviceHTTP HTTPUpstream
	t           *testing.T
	during      func()
	calls       int
}

func (h *modelControlPriceHTTP) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	return h.DoWithTLS(req, proxy, id, concurrency, nil)
}
func (h *modelControlPriceHTTP) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	h.calls++
	require.Equal(h.t, modelsDevRegistryURL, req.URL.String())
	require.Zero(h.t, id)
	require.Empty(h.t, proxy)
	require.Empty(h.t, req.Header.Get("Authorization"))
	require.Nil(h.t, profile)
	deadline, ok := req.Context().Deadline()
	require.True(h.t, ok)
	require.LessOrEqual(h.t, time.Until(deadline), 60*time.Second)
	if h.during != nil {
		h.during()
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"openai":{"models":{"gpt-6.1-sol":{"cost":{"input":0,"output":2,"cache_read":0}}}}}`)), Header: http.Header{}}, nil
}
func TestModelControlPublicPriceSyncCASAndRevocation(t *testing.T) {
	for _, scenario := range []string{"success", "CAS during fetch", "revoked during fetch"} {
		t.Run(scenario, func(t *testing.T) {
			s, r := newModelControlSettings()
			httpClient := &modelControlPriceHTTP{t: t}
			collector := &AccountTestService{httpUpstream: httpClient}
			ctx := context.Background()
			checks := 0
			if scenario == "CAS during fetch" {
				httpClient.during = func() {
					r.mu.Lock()
					r.values[ModelManagementKeyPrefix+"pricing"] = `{"version":1,"overrides":{},"external":{}}`
					r.mu.Unlock()
				}
			}
			if scenario == "revoked during fetch" {
				ctx = authz.WithRevalidation(ctx, func(context.Context) error {
					checks++
					if checks > 1 {
						return authz.ErrAuthorizationExpired
					}
					return nil
				})
			}
			// Even an accidentally supplied vendor account cannot become the network
			// identity for this public-only operation.
			vendor := int64(9)
			prices, err := collector.SyncManagedPrices(ctx, s, PlatformOpenAI, 0, &Account{ID: 99, VendorID: &vendor, Credentials: map[string]any{"api_key": "must-not-send"}})
			switch scenario {
			case "success":
				require.NoError(t, err)
				require.Equal(t, int64(1), prices.Version)
				require.Equal(t, 0.0, *prices.External["gpt-6.1-sol"].Input)
			case "CAS during fetch":
				require.ErrorIs(t, err, ErrModelPolicyConflict)
			case "revoked during fetch":
				require.ErrorContains(t, err, "authorization expired")
				stored, e := s.GetManagedPrices(context.Background(), true)
				require.NoError(t, e)
				require.Equal(t, int64(0), stored.Version)
			}
			require.Equal(t, 1, httpClient.calls)
		})
	}
}

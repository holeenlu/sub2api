package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestGetStickySessionAccountID_FallbackToLegacyKey(t *testing.T) {
	beforeFallbackTotal, beforeFallbackHit, _ := openAIStickyCompatStats()

	cache := &stubGatewayCache{
		sessionBindings: map[string]int64{
			"openai:legacy-hash": 42,
		},
	}
	svc := &OpenAIGatewayService{
		cache: cache,
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				OpenAIWS: config.GatewayOpenAIWSConfig{
					SessionHashReadOldFallback: true,
				},
			},
		},
	}

	ctx := withOpenAILegacySessionHash(context.Background(), "legacy-hash")
	accountID, err := svc.getStickySessionAccountID(ctx, nil, "new-hash")
	require.NoError(t, err)
	require.Equal(t, int64(42), accountID)
	require.NotContains(t, cache.sessionBindings, "openai:new-hash", "read fallback must not bind an unvalidated account")

	afterFallbackTotal, afterFallbackHit, _ := openAIStickyCompatStats()
	require.Equal(t, beforeFallbackTotal+1, afterFallbackTotal)
	require.Equal(t, beforeFallbackHit+1, afterFallbackHit)
}

func TestSetStickySessionAccountID_DualWriteOldEnabled(t *testing.T) {
	_, _, beforeDualWriteTotal := openAIStickyCompatStats()

	cache := &stubGatewayCache{sessionBindings: map[string]int64{}}
	svc := &OpenAIGatewayService{
		cache: cache,
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				OpenAIWS: config.GatewayOpenAIWSConfig{
					SessionHashDualWriteOld: true,
				},
			},
		},
	}

	ctx := withOpenAILegacySessionHash(context.Background(), "legacy-hash")
	err := svc.setStickySessionAccountID(ctx, nil, "new-hash", 9, openaiStickySessionTTL)
	require.NoError(t, err)
	require.Equal(t, int64(9), cache.sessionBindings["openai:new-hash"])
	require.Equal(t, int64(9), cache.sessionBindings["openai:legacy-hash"])

	_, _, afterDualWriteTotal := openAIStickyCompatStats()
	require.Equal(t, beforeDualWriteTotal+1, afterDualWriteTotal)
}

func TestSetStickySessionAccountID_DualWriteOldDisabled(t *testing.T) {
	cache := &stubGatewayCache{sessionBindings: map[string]int64{}}
	svc := &OpenAIGatewayService{
		cache: cache,
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				OpenAIWS: config.GatewayOpenAIWSConfig{
					SessionHashDualWriteOld: false,
				},
			},
		},
	}

	ctx := withOpenAILegacySessionHash(context.Background(), "legacy-hash")
	err := svc.setStickySessionAccountID(ctx, nil, "new-hash", 9, openaiStickySessionTTL)
	require.NoError(t, err)
	require.Equal(t, int64(9), cache.sessionBindings["openai:new-hash"])
	_, exists := cache.sessionBindings["openai:legacy-hash"]
	require.False(t, exists)
}

func TestOpenAIStickySession_RefreshUsesConfiguredTTL(t *testing.T) {
	for _, ttlSeconds := range []int{0, 1800, 7200} {
		for _, binding := range []string{"primary", "legacy_only", "none"} {
			for _, transport := range []string{"http", "ws", "token_count"} {
				t.Run((time.Duration(ttlSeconds)*time.Second).String()+"/"+binding+"/"+transport, func(t *testing.T) {
					groupID := int64(10)
					account := Account{
						ID: 2001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
						Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true},
					}
					const primaryKey = "openai:new-hash"
					const legacyKey = "openai:legacy-hash"
					cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}
					if binding == "primary" {
						cache.sessionBindings[primaryKey] = account.ID
					}
					if binding != "none" {
						cache.sessionBindings[legacyKey] = account.ID
					}
					cfg := newSchedulerTestOpenAIWSV2Config()
					cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = ttlSeconds
					cfg.Gateway.OpenAIWS.SessionHashReadOldFallback = true
					svc := &OpenAIGatewayService{
						accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
						cache:              cache,
						cfg:                cfg,
						concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
					}
					ctx := withOpenAILegacySessionHash(context.Background(), "legacy-hash")
					if transport == "token_count" {
						selected, err := svc.SelectAccountForTokenCount(ctx, &groupID, "new-hash", "gpt-5.1", OpenAIEndpointCapabilityChatCompletions, PlatformOpenAI)
						require.NoError(t, err)
						require.NotNil(t, selected)
						require.Equal(t, account.ID, selected.ID)
					} else {
						requiredTransport := OpenAIUpstreamTransportHTTPSSE
						if transport == "ws" {
							requiredTransport = OpenAIUpstreamTransportResponsesWebsocketV2
						}
						selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "new-hash", "gpt-5.1", nil, requiredTransport, false)
						require.NoError(t, err)
						require.NotNil(t, selection)
						require.Equal(t, account.ID, selection.Account.ID)
						require.Equal(t, binding != "none", decision.StickySessionHit)
						if selection.ReleaseFunc != nil {
							selection.ReleaseFunc()
						}
					}
					wantTTL := time.Duration(ttlSeconds) * time.Second
					if ttlSeconds == 0 {
						wantTTL = openaiStickySessionTTL
					}
					require.Equal(t, account.ID, cache.sessionBindings[primaryKey])
					require.Equal(t, wantTTL, cache.sessionTTLs[primaryKey])
					if binding != "none" {
						require.Equal(t, 10*time.Minute, cache.sessionTTLs[legacyKey])
					}
					// Migration completion must not lose a successfully admitted session.
					cfg.Gateway.OpenAIWS.SessionHashReadOldFallback = false
					accountID, err := svc.getStickySessionAccountID(ctx, &groupID, "new-hash")
					require.NoError(t, err)
					require.Equal(t, account.ID, accountID)
				})
			}
		}
	}
}

func TestRefreshStickySessionTTL_PreservesConcurrentOwner(t *testing.T) {
	for _, primary := range []int64{0, 9, 42} {
		for _, legacy := range []int64{0, 9, 42} {
			cache := &schedulerTestGatewayCache{
				sessionBindings: map[string]int64{},
				sessionTTLs:     map[string]time.Duration{},
			}
			if primary > 0 {
				cache.sessionBindings["openai:current"] = primary
				cache.sessionTTLs["openai:current"] = 7 * time.Minute
			}
			if legacy > 0 {
				cache.sessionBindings["openai:legacy"] = legacy
				cache.sessionTTLs["openai:legacy"] = 3 * time.Minute
			}
			svc := &OpenAIGatewayService{cache: cache, cfg: &config.Config{}}
			svc.cfg.Gateway.OpenAIWS.SessionHashReadOldFallback = true
			ctx := withOpenAILegacySessionHash(context.Background(), "legacy")
			require.NoError(t, svc.refreshStickySessionTTL(ctx, nil, "current", 9, time.Hour))
			if primary == 42 {
				require.Equal(t, int64(42), cache.sessionBindings["openai:current"])
				require.Equal(t, 7*time.Minute, cache.sessionTTLs["openai:current"])
			} else {
				require.Equal(t, int64(9), cache.sessionBindings["openai:current"])
				require.Equal(t, time.Hour, cache.sessionTTLs["openai:current"])
			}
			if legacy == 0 {
				require.NotContains(t, cache.sessionBindings, "openai:legacy", "read fallback alone must not recreate a legacy key")
			} else {
				require.Equal(t, legacy, cache.sessionBindings["openai:legacy"])
				wantTTL := 3 * time.Minute
				if primary != 42 && legacy == 9 {
					wantTTL = 10 * time.Minute
				}
				require.Equal(t, wantTTL, cache.sessionTTLs["openai:legacy"])
			}
		}
	}
}

func TestOpenAIStickySession_DeferredUntilAdmission(t *testing.T) {
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}
	svc := &OpenAIGatewayService{cache: cache, cfg: &config.Config{}}
	ctx := WithDeferredOpenAIStickySession(context.Background())
	require.NoError(t, svc.BindStickySession(ctx, nil, "s", 9))
	require.NoError(t, svc.refreshStickySessionTTL(ctx, nil, "s", 9, time.Hour))
	require.Empty(t, cache.sessionBindings, "selection alone must not establish or renew WS affinity")
	require.NoError(t, svc.RefreshStickySessionAfterAdmission(ctx, nil, "s", 9))
	require.Equal(t, int64(9), cache.sessionBindings["openai:s"])
	require.Equal(t, time.Hour, cache.sessionTTLs["openai:s"])

	cache.sessionBindings["openai:s"] = 42
	cache.sessionTTLs["openai:s"] = 7 * time.Minute
	require.NoError(t, svc.RefreshStickySessionAfterAdmission(ctx, nil, "s", 9))
	require.Equal(t, int64(42), cache.sessionBindings["openai:s"])
	require.Equal(t, 7*time.Minute, cache.sessionTTLs["openai:s"])

	borrowed := withSchedulingAffinity(ctx)
	rememberBusyStickyHome(borrowed, "borrowed", &Account{ID: 42}, &AccountWaitPlan{})
	require.NoError(t, svc.RefreshStickySessionAfterAdmission(borrowed, nil, "borrowed", 9))
	require.NotContains(t, cache.sessionBindings, "openai:borrowed", "a borrowed connection must not steal an expired home")
}

type openAIStickyAdmissionRaceCache struct {
	schedulerTestGatewayCache
}

func (c *openAIStickyAdmissionRaceCache) GetSessionAccountID(ctx context.Context, groupID int64, hash string) (int64, error) {
	owner, err := c.schedulerTestGatewayCache.GetSessionAccountID(ctx, groupID, hash)
	// Another request takes ownership after this caller's snapshot read.
	c.sessionBindings[hash] = 42
	c.sessionTTLs[hash] = 7 * time.Minute
	return owner, err
}

func TestBindStickySessionAfterProfitAdmission_ConcurrentOwnerWins(t *testing.T) {
	cache := &openAIStickyAdmissionRaceCache{schedulerTestGatewayCache{
		sessionBindings: map[string]int64{"openai:s": 9},
		sessionTTLs:     map[string]time.Duration{"openai:s": time.Hour},
	}}
	svc := &OpenAIGatewayService{cache: cache, cfg: &config.Config{}}
	ctx := context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{groupID: 1, platform: PlatformOpenAI, threshold: 0.5})
	groupID := int64(1)
	require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, &groupID, "s", 9))
	require.Equal(t, int64(42), cache.sessionBindings["openai:s"])
	require.Equal(t, 7*time.Minute, cache.sessionTTLs["openai:s"])
}

func TestSnapshotOpenAICompatibilityFallbackMetrics(t *testing.T) {
	before := SnapshotOpenAICompatibilityFallbackMetrics()

	ctx := context.WithValue(context.Background(), ctxkey.ThinkingEnabled, true)
	_, _ = ThinkingEnabledFromContext(ctx)

	after := SnapshotOpenAICompatibilityFallbackMetrics()
	require.GreaterOrEqual(t, after.MetadataLegacyFallbackTotal, before.MetadataLegacyFallbackTotal+1)
	require.GreaterOrEqual(t, after.MetadataLegacyFallbackThinkingEnabledTotal, before.MetadataLegacyFallbackThinkingEnabledTotal+1)
}

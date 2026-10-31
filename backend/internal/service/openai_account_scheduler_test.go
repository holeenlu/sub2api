package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	gatewaymodel "github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	gatewaysession "github.com/Wei-Shaw/sub2api/internal/gateway/session"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
)

type openAISnapshotCacheStub struct {
	SchedulerCache
	snapshotAccounts []*Account
	accountsByID     map[int64]*Account
}

type schedulerTestOpenAIAccountRepo struct {
	AccountRepository
	accounts   []Account
	getByIDErr error
}

func (r schedulerTestOpenAIAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, errors.New("account not found")
}

func (r schedulerTestOpenAIAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range r.accounts {
		if acc.Platform == platform {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (r schedulerTestOpenAIAccountRepo) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

func (r schedulerTestOpenAIAccountRepo) ListModelAvailabilityCandidates(ctx context.Context, groupID *int64, _ []string, _ bool) ([]Account, error) {
	return r.ListSchedulableByGroupID(ctx, *groupID)
}

func (r schedulerTestOpenAIAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range r.accounts {
		if acc.Platform == platform {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (r schedulerTestOpenAIAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

type schedulerGroupAwareOpenAIAccountRepo struct {
	schedulerTestOpenAIAccountRepo
}

func (r schedulerGroupAwareOpenAIAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range r.accounts {
		if acc.Platform == platform && openAIStickyAccountMatchesGroup(&acc, &groupID) {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (r schedulerGroupAwareOpenAIAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range r.accounts {
		if acc.Platform == platform && openAIStickyAccountMatchesGroup(&acc, nil) {
			result = append(result, acc)
		}
	}
	return result, nil
}

type schedulerTestConcurrencyCache struct {
	ConcurrencyCache
	loadBatchErr    error
	loadMap         map[int64]*AccountLoadInfo
	acquireResults  map[int64]bool
	waitCounts      map[int64]int
	skipDefaultLoad bool
	acquiredIDs     *[]int64
	releasedIDs     *[]int64
}

func (c schedulerTestConcurrencyCache) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	if c.acquiredIDs != nil {
		*c.acquiredIDs = append(*c.acquiredIDs, accountID)
	}
	if c.acquireResults != nil {
		if result, ok := c.acquireResults[accountID]; ok {
			return result, nil
		}
	}
	return true, nil
}

func (c schedulerTestConcurrencyCache) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	if c.releasedIDs != nil {
		*c.releasedIDs = append(*c.releasedIDs, accountID)
	}
	return nil
}

func (c schedulerTestConcurrencyCache) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	if c.loadBatchErr != nil {
		return nil, c.loadBatchErr
	}
	out := make(map[int64]*AccountLoadInfo, len(accounts))
	if c.skipDefaultLoad && c.loadMap != nil {
		for _, acc := range accounts {
			if load, ok := c.loadMap[acc.ID]; ok {
				out[acc.ID] = load
			}
		}
		return out, nil
	}
	for _, acc := range accounts {
		if c.loadMap != nil {
			if load, ok := c.loadMap[acc.ID]; ok {
				out[acc.ID] = load
				continue
			}
		}
		out[acc.ID] = &AccountLoadInfo{AccountID: acc.ID, LoadRate: 0}
	}
	return out, nil
}

func (c schedulerTestConcurrencyCache) GetAccountWaitingCount(ctx context.Context, accountID int64) (int, error) {
	if c.waitCounts != nil {
		if count, ok := c.waitCounts[accountID]; ok {
			return count, nil
		}
	}
	return 0, nil
}

type schedulerTestGatewayCache struct {
	sessionBindings map[string]int64
	deletedSessions map[string]int
	sessionTTLs     map[string]time.Duration
}

func (c *schedulerTestGatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	if id, ok := c.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, ErrStickySessionNotFound
}

func (c *schedulerTestGatewayCache) CompareAndRefreshSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration, createIfAbsent bool) (bool, error) {
	current, exists := c.sessionBindings[sessionHash]
	if (exists && current != accountID) || (!exists && !createIfAbsent) {
		return false, nil
	}
	if err := c.SetSessionAccountID(ctx, groupID, sessionHash, accountID, ttl); err != nil {
		return false, err
	}
	return true, nil
}

func (c *schedulerTestGatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if c.sessionBindings == nil {
		c.sessionBindings = make(map[string]int64)
	}
	c.sessionBindings[sessionHash] = accountID
	if c.sessionTTLs == nil {
		c.sessionTTLs = make(map[string]time.Duration)
	}
	c.sessionTTLs[sessionHash] = ttl
	return nil
}

func (c *schedulerTestGatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	if _, exists := c.sessionBindings[sessionHash]; exists {
		if c.sessionTTLs == nil {
			c.sessionTTLs = make(map[string]time.Duration)
		}
		c.sessionTTLs[sessionHash] = ttl
	}
	return nil
}

func (c *schedulerTestGatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	if c.sessionBindings == nil {
		return nil
	}
	if c.deletedSessions == nil {
		c.deletedSessions = make(map[string]int)
	}
	c.deletedSessions[sessionHash]++
	delete(c.sessionBindings, sessionHash)
	return nil
}

// 长周期亲和键：该 mock 不参与历史键行为，给出中性实现以满足接口。
func (c *schedulerTestGatewayCache) GetSessionAccountHistory(_ context.Context, _ int64, _ string) (int64, error) {
	return 0, ErrStickySessionNotFound
}
func (c *schedulerTestGatewayCache) SetSessionAccountHistoryIfAbsentOrSame(_ context.Context, _ int64, _ string, _ int64, _ time.Duration) (bool, error) {
	return true, nil
}
func (c *schedulerTestGatewayCache) DeleteSessionAccountHistory(_ context.Context, _ int64, _ string) error {
	return nil
}

func (c *schedulerTestGatewayCache) SetGrokVideoPendingBilling(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}
func (c *schedulerTestGatewayCache) GetGrokVideoPendingBilling(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}
func (c *schedulerTestGatewayCache) ClaimGrokVideoBilled(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (c *schedulerTestGatewayCache) ReleaseGrokVideoBilled(_ context.Context, _ string) error {
	return nil
}

func (c *schedulerTestGatewayCache) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (c *schedulerTestGatewayCache) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", ErrReasoningContentNotFound
}

func newSchedulerTestOpenAIWSV2Config() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	return cfg
}

func newSchedulerTestSubscriptionPriorityConfig() *config.Config {
	cfg := &config.Config{}
	return cfg
}

type openAIAdvancedSchedulerSettingRepoStub struct {
	values map[string]string
}

func (s *openAIAdvancedSchedulerSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := s.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *openAIAdvancedSchedulerSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s == nil || s.values == nil {
		return "", ErrSettingNotFound
	}
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *openAIAdvancedSchedulerSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected call to Set")
}

func (s *openAIAdvancedSchedulerSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, err := s.GetValue(context.Background(), key); err == nil {
			result[key] = value
		}
	}
	return result, nil
}

func (s *openAIAdvancedSchedulerSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected call to SetMultiple")
}

func (s *openAIAdvancedSchedulerSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected call to GetAll")
}

func (s *openAIAdvancedSchedulerSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected call to Delete")
}

func newOpenAIAdvancedSchedulerRateLimitService(enabled string, values ...string) *RateLimitService {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	repo := &openAIAdvancedSchedulerSettingRepoStub{
		values: map[string]string{},
	}
	if enabled != "" {
		repo.values["openai_advanced_scheduler_enabled"] = enabled
	}
	if len(values) > 0 && values[0] != "" {
		repo.values["openai_advanced_scheduler_sticky_weighted_enabled"] = values[0]
	}
	if len(values) > 1 && values[1] != "" {
		repo.values[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled] = values[1]
	}
	return &RateLimitService{
		settingService: NewSettingService(repo, &config.Config{}),
	}
}

func (s *openAISnapshotCacheStub) GetSnapshot(ctx context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	if len(s.snapshotAccounts) == 0 {
		return nil, false, nil
	}
	out := make([]*Account, 0, len(s.snapshotAccounts))
	for _, account := range s.snapshotAccounts {
		if account == nil {
			continue
		}
		cloned := *account
		out = append(out, &cloned)
	}
	return out, true, nil
}

func (s *openAISnapshotCacheStub) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	if s.accountsByID == nil {
		return nil, nil
	}
	account := s.accountsByID[accountID]
	if account == nil {
		return nil, nil
	}
	cloned := *account
	return &cloned, nil
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_AlwaysHonorsPreviousResponse(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10106)
	accounts := []Account{
		{
			ID:          36001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
		},
		{
			ID:          36002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cache := &schedulerTestGatewayCache{}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_disabled_001", 36001, time.Hour))
	require.NoError(t, store.BindResponseMode(ctx, groupID, "resp_disabled_001", gatewaysession.ResponseModeNative, time.Hour))
	require.NotNil(t, svc.getOpenAIAccountScheduler(ctx))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_disabled_001",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(36001), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.True(t, decision.StickyPreviousHit)
}

// Regression: the legacy load-batch path had two bare ErrNoAvailableAccounts
// exits that bypassed the diagnostics added for both the advanced scheduler and
// the non-batched legacy selector. This is the default path when load batching
// is enabled, so quota auto-pause could still surface as an opaque 503.
func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabled_LoadBatchReportsFilterReasons(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := withOpenAIQuotaAutoPauseSettings(context.Background(), OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold7d: 0.9})
	groupID := int64(10107)
	quotaPaused := Account{
		ID:          36003,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"codex_7d_used_percent":  95.0,
			"codex_7d_reset_at":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
		},
	}
	mappingMiss := Account{
		ID:          36004,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-4o": "gpt-4o"},
		},
	}
	excluded := Account{
		ID:          36005,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{quotaPaused, mappingMiss, excluded}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	require.NotNil(t, svc.getOpenAIAccountScheduler(ctx))
	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4-mini",
		map[int64]struct{}{excluded.ID: {}},
		OpenAIUpstreamTransportAny,
		false,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.EqualError(t, err, "no available OpenAI accounts supporting model: gpt-5.4-mini (pool=3, filtered: excluded=1 model_not_supported=1 quota_auto_pause_7d=1)")
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabled_RequiredWSV2_SkipsHTTPOnlyAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10108)
	accounts := []Account{
		{
			ID:          36011,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          36012,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(36012), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabled_RequiredWSV2_NoAvailableAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10109)
	accounts := []Account{
		{
			ID:          36021,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.ErrorContains(t, err, "no available OpenAI accounts")
	require.Nil(t, selection)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabled_EmbeddingsSkipsChatOnlyAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10110)
	accounts := []Account{
		{
			ID:          36031,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions"},
			},
		},
		{
			ID:          36032,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions", "embeddings"},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithSchedulerForCapability(
		ctx,
		&groupID,
		"",
		"",
		"text-embedding-3-small",
		nil,
		OpenAIUpstreamTransportHTTPSSE,
		OpenAIEndpointCapabilityEmbeddings,
		false,
		false,
		true,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(36032), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountForTokenCount_DoesNotAcquireGenerationSlot(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10115)
	acquiredIDs := make([]int64, 0)
	accounts := []Account{
		{
			ID: 36501, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
			Credentials: map[string]any{"openai_capabilities": []any{"chat_completions"}},
		},
		{
			ID: 36502, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5,
			Credentials: map[string]any{"openai_capabilities": []any{"embeddings"}},
		},
		{
			ID: 36503, Platform: PlatformGrok, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
			Credentials: map[string]any{"openai_capabilities": []any{"chat_completions"}},
		},
		{
			ID: 36504, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 15,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions"},
				"model_mapping":       map[string]any{"gpt-4o": "gpt-4o"},
			},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:       &schedulerTestGatewayCache{},
		cfg:         &config.Config{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{36501: false},
			acquiredIDs:    &acquiredIDs,
		}),
	}

	account, err := svc.SelectAccountForTokenCount(
		ctx,
		&groupID,
		"",
		"gpt-5.1",
		OpenAIEndpointCapabilityChatCompletions,
		PlatformOpenAI,
	)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(36501), account.ID)
	require.Empty(t, acquiredIDs, "token counting must not acquire a generation slot")
}

// 生图意图的 /v1/responses 请求要求 OpenAIEndpointCapabilityResponses：探测确认
// 不支持 Responses API 的 APIKey 账号必须被排除，避免 forward 阶段降级为无法生图
// 的 Chat Completions 直转（#4417）。
func TestOpenAIGatewayService_SelectAccountWithScheduler_ResponsesCapabilityExcludesUnsupportedAPIKey(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10120)

	newSvc := func(accounts []Account) *OpenAIGatewayService {
		cfg := &config.Config{}
		cfg.Gateway.Scheduling.LoadBatchEnabled = false
		return &OpenAIGatewayService{
			accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
			cache:              &schedulerTestGatewayCache{},
			cfg:                cfg,
			concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		}
	}

	supported := Account{
		ID: 37001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
	}
	// 更高优先级但探测确认不支持 Responses——若门控失效会被优先选中。
	unsupported := Account{
		ID: 37002, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5,
		Extra: map[string]any{"openai_responses_supported": false},
	}

	t.Run("生图意图仅选中支持 responses 的账号", func(t *testing.T) {
		svc := newSvc([]Account{supported, unsupported})
		selection, _, err := svc.SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "", "", "gpt-image-2", nil,
			OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses,
			false, false, false,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, int64(37001), selection.Account.ID)
	})

	t.Run("仅有不支持 responses 的账号时生图意图无可用账号", func(t *testing.T) {
		svc := newSvc([]Account{unsupported})
		selection, _, err := svc.SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "", "", "gpt-image-2", nil,
			OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses,
			false, false, false,
		)
		require.Error(t, err)
		require.Nil(t, selection)
	})

	t.Run("非生图路径仍可选中不支持 responses 的账号", func(t *testing.T) {
		svc := newSvc([]Account{unsupported})
		selection, _, err := svc.SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "", "", "gpt-5.1", nil,
			OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions,
			false, false, true,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, int64(37002), selection.Account.ID)
	})
}

// alpha/search 调度必须同时放行 OAuth 与 APIKey 账号：v0.1.157 曾因 OAuth-only
// 门控把 APIKey 账号从候选池剔除，纯 APIKey 分组的独立搜索请求在选号阶段就
// 报无可用账号，Codex 网页搜索整体失效（转发层其实一直支持 APIKey 路径）。
func TestOpenAIGatewayService_SelectAccountWithScheduler_AlphaSearchAllowsAPIKeyAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10125)
	accounts := []Account{
		{
			ID:          38001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithSchedulerForCapability(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.6-sol",
		nil,
		OpenAIUpstreamTransportHTTPSSE,
		OpenAIEndpointCapabilityAlphaSearch,
		false,
		false,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(38001), selection.Account.ID)
}

func TestOpenAIAccountSchedulerCloudConfigurationUsesCapabilityExecutionMode(t *testing.T) {
	for _, tc := range []struct {
		name       string
		capability OpenAIEndpointCapability
		image      OpenAIImagesCapability
		mode       string
	}{
		{"responses", OpenAIEndpointCapabilityResponses, "", UsageAuthorizationModeCloud},
		{"responses compatible", OpenAIEndpointCapabilityResponsesCompatible, "", UsageAuthorizationModeCloud},
		{"alpha search", OpenAIEndpointCapabilityAlphaSearch, "", UsageAuthorizationModeNative},
		{"chat", OpenAIEndpointCapabilityChatCompletions, "", UsageAuthorizationModeNative},
		{"live", OpenAIEndpointCapabilityLive, "", UsageAuthorizationModeNative},
		{"direct images", "", OpenAIImagesCapabilityBasic, UsageAuthorizationModeNative},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 38011, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
				Extra: map[string]any{OpenAICloudEnabledKey: true}, Credentials: map[string]any{"access_token": "native-token", "chatgpt_account_id": "owner"}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, cloudSessions: &OpenAICloudSessions{}}
			scheduler := &defaultOpenAIAccountScheduler{service: svc}
			req := OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-5.1", RequiredCapability: tc.capability, RequiredImageCapability: tc.image}
			for _, blocked := range []string{UsageAuthorizationModeNative, UsageAuthorizationModeCloud} {
				budget := NewOpenAITurnBudget(nil)
				budget.excludedModes = map[gatewaymodel.ResourceOwner]map[string]bool{recoveryOwner(account): {blocked: true}}
				ctx := WithOpenAITurnBudget(t.Context(), budget)
				before := budget.attemptBudget.Snapshot()
				compatible, reason := scheduler.isAccountRequestCompatibleReason(ctx, account, req)
				require.Equal(t, blocked != tc.mode, compatible, "capability %s must consume the %s eligibility scope", tc.name, tc.mode)
				if !compatible {
					require.Equal(t, "turn_recovery_target", reason)
				}
				require.Equal(t, before, budget.attemptBudget.Snapshot())
			}
			// Selecting Cloud for Responses must not make independent native
			// capabilities depend on an assembled Cloud executor.
			svc.cloudSessions = nil
			compatible, reason := scheduler.isAccountRequestCompatibleReason(t.Context(), account, req)
			require.Equal(t, tc.mode != UsageAuthorizationModeCloud, compatible)
			if !compatible {
				require.Equal(t, "cloud_gateway_unavailable", reason)
			}
			if tc.mode == UsageAuthorizationModeCloud {
				req.RequireCompact = true
				compatible, reason = scheduler.isAccountRequestCompatibleReason(t.Context(), account, req)
				require.False(t, compatible)
				require.Equal(t, "cloud_endpoint_unsupported", reason)
			}
		})
	}
}

func TestOpenAIAccountSchedulerAlphaSearchDoesNotBorrowBPSMode(t *testing.T) {
	account := &Account{ID: 38012, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Extra: map[string]any{"openai_excel_bps": true}, Credentials: map[string]any{"access_token": "native-token", "chatgpt_account_id": "owner"}}
	require.True(t, account.IsExcelBPSEnabledForModel("gpt-5.1"))
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{cfg: &config.Config{}}}
	for _, tc := range []struct {
		capability OpenAIEndpointCapability
		mode       string
	}{
		{OpenAIEndpointCapabilityAlphaSearch, UsageAuthorizationModeNative},
		{OpenAIEndpointCapabilityResponses, UsageAuthorizationModeBPS},
	} {
		for _, blocked := range []string{UsageAuthorizationModeNative, UsageAuthorizationModeBPS} {
			t.Run(string(tc.capability)+"/blocked="+blocked, func(t *testing.T) {
				budget := NewOpenAITurnBudget(nil)
				budget.excludedModes = map[gatewaymodel.ResourceOwner]map[string]bool{recoveryOwner(account): {blocked: true}}
				ctx := WithOpenAITurnBudget(t.Context(), budget)
				compatible, reason := scheduler.isAccountRequestCompatibleReason(ctx, account, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-5.1", RequiredCapability: tc.capability})
				require.Equal(t, blocked != tc.mode, compatible)
				if !compatible {
					require.Equal(t, "turn_recovery_target", reason)
				}
			})
		}
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabled_AllowsGrokChatAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10113)
	accounts := []Account{
		{
			ID:          36041,
			Platform:    PlatformGrok,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithSchedulerForCapability(
		ctx,
		&groupID,
		"",
		"",
		"grok-4.3",
		nil,
		OpenAIUpstreamTransportAny,
		OpenAIEndpointCapabilityChatCompletions,
		false,
		false,
		false,
		PlatformGrok,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(36041), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_GrokMediaCapabilityFiltersIneligibleAccounts(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10114)
	ineligible := Account{
		ID: 36051, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5,
		Extra: map[string]any{GrokMediaEligibleExtraKey: false},
	}
	eligible := Account{
		ID: 36052, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
		Extra: map[string]any{GrokMediaEligibleExtraKey: true},
	}
	newService := func(accounts []Account) *OpenAIGatewayService {
		cfg := &config.Config{}
		cfg.Gateway.Scheduling.LoadBatchEnabled = false
		return &OpenAIGatewayService{
			accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
			cache:              &schedulerTestGatewayCache{},
			cfg:                cfg,
			concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		}
	}

	t.Run("media generation skips higher priority ineligible account", func(t *testing.T) {
		selection, _, err := newService([]Account{ineligible, eligible}).SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "", "", "grok-imagine-video", nil,
			OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityGrokMediaGeneration,
			false, false, false, PlatformGrok,
		)

		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, eligible.ID, selection.Account.ID)
	})

	t.Run("media generation fails closed when all accounts are ineligible", func(t *testing.T) {
		selection, _, err := newService([]Account{ineligible}).SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "", "", "grok-imagine-video", nil,
			OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityGrokMediaGeneration,
			false, false, false, PlatformGrok,
		)

		require.Error(t, err)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.Nil(t, selection)
	})

	t.Run("chat remains routable on media-ineligible account", func(t *testing.T) {
		selection, _, err := newService([]Account{ineligible}).SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "", "", "grok-4.3", nil,
			OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions,
			false, false, false, PlatformGrok,
		)

		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, ineligible.ID, selection.Account.ID)
	})
}

// Regression #4599: when the advanced scheduler's load-balance initial filter
// drops every candidate, the resulting "no available OpenAI accounts" error must
// carry per-reason exclusion counts. Previously all filter branches were silent
// (debug logs at best), so a 503 with excluded_account_count=0 could not be
// diagnosed from the error alone.
func TestOpenAIGatewayService_SelectAccountWithScheduler_NoAvailableErrorReportsQuotaAutoPauseExclusion(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := withOpenAIQuotaAutoPauseSettings(context.Background(), OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold7d: 0.9})
	groupID := int64(101201)
	accounts := []Account{
		{
			ID:          38101,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Extra: map[string]any{
				"codex_7d_used_percent":  95.0,
				"codex_7d_reset_at":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				"codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
			},
		},
	}
	cfg := &config.Config{}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "", "", "gpt-5.4-mini", nil, OpenAIUpstreamTransportAny, false,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.EqualError(t, err, "no available OpenAI accounts supporting model: gpt-5.4-mini (pool=1, filtered: quota_auto_pause_7d=1)")
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_NoAvailableErrorReportsModelNotSupported(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(101202)
	// OAuth account with empty model_mapping: foreign-family models (grok-*)
	// are not servable by Codex upstream (#3662) and are dropped by the filter.
	accounts := []Account{
		{
			ID:          38111,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "", "", "grok-4.5", nil, OpenAIUpstreamTransportAny, false,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	require.EqualError(t, err, "no available OpenAI accounts supporting model: grok-4.5 (pool=1, filtered: model_not_supported=1)")
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_NoAvailableErrorAggregatesReasonsDeterministically(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := withOpenAIQuotaAutoPauseSettings(context.Background(), OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold7d: 0.9})
	groupID := int64(101203)
	quotaPaused := Account{
		ID:          38121,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"codex_7d_used_percent":  95.0,
			"codex_7d_reset_at":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
		},
	}
	mappingMiss := Account{
		ID:          38122,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-4o": "gpt-4o"},
		},
	}
	excluded := Account{
		ID:          38123,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{quotaPaused, mappingMiss, excluded}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "", "", "gpt-5.4-mini", map[int64]struct{}{38123: {}}, OpenAIUpstreamTransportAny, false,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	// Reasons are sorted lexicographically, so the message is deterministic.
	require.EqualError(t, err, "no available OpenAI accounts supporting model: gpt-5.4-mini (pool=3, filtered: excluded=1 model_not_supported=1 quota_auto_pause_7d=1)")
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_NoAvailableErrorReportsEmptyPool(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(101204)
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{},
		cache:              &schedulerTestGatewayCache{},
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	require.EqualError(t, err, "no available OpenAI accounts supporting model: gpt-5.1 (pool=0)")
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_EnabledUsesAdvancedPreviousResponseRouting(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10107)
	accounts := []Account{
		{
			ID:          37001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
		{
			ID:          37002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_enabled_001", 37001, time.Hour))
	require.NoError(t, store.BindResponseMode(ctx, groupID, "resp_enabled_001", gatewaysession.ResponseModeNative, time.Hour))
	require.NotNil(t, svc.getOpenAIAccountScheduler(ctx))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_enabled_001",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(37001), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.True(t, decision.StickyPreviousHit)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_HealthySessionUsesAffinity(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(101071)
	accounts := []Account{
		{
			ID:          37101,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    100,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          37102,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
	}
	cfg := &config.Config{}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{
		"openai:session_hash_weighted_topk": 37101,
	}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true", "true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_weighted_topk",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(37101), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
	require.Zero(t, decision.TopK)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_AccountModelRouteOnlyUsesClaimingAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(1010711)
	owner := Account{
		ID: 371011, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
		GroupIDs:    []int64{groupID},
		Credentials: map[string]any{"model_mapping": map[string]any{"team-alias": "gpt-5.1"}},
	}
	nonOwner := Account{
		ID: 371012, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
		GroupIDs: []int64{groupID},
	}
	repo := schedulerTestOpenAIAccountRepo{accounts: []Account{owner, nonOwner}}
	ownership, err := (&GatewayService{accountRepo: repo}).resolveCompositeModelOwnership(ctx, groupID, "team-alias")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{TargetPlatform: PlatformOpenAI, Matched: true}, ownership)
	ctx = WithCompositeRouteDecision(ctx, CompositeRouteDecision{
		Matched: true, Source: CompositeRouteSourceAccount, GroupID: groupID,
		PublicModel: "team-alias", TargetPlatform: ownership.TargetPlatform, UpstreamModel: "team-alias",
	})

	for _, tt := range []struct {
		name            string
		sessionHash     string
		sessionBindings map[string]int64
	}{
		{name: "initial filter excludes non owner"},
		{name: "non owner sticky hit is rejected", sessionHash: "non-owner", sessionBindings: map[string]int64{"openai:non-owner": nonOwner.ID}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = false
			svc := &OpenAIGatewayService{
				accountRepo:        repo,
				cache:              &schedulerTestGatewayCache{sessionBindings: tt.sessionBindings},
				cfg:                cfg,
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}

			selection, _, selectErr := svc.SelectAccountWithScheduler(
				ctx, &groupID, "", tt.sessionHash, "team-alias", nil, OpenAIUpstreamTransportAny, false,
			)
			require.NoError(t, selectErr)
			require.NotNil(t, selection)
			require.NotNil(t, selection.Account)
			require.Equal(t, owner.ID, selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_AccountModelRouteDBRecheckFailsOverToOwner(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1010712)
	mapping := map[string]any{"model_mapping": map[string]any{"team-alias": "gpt-5.1"}}
	stalePrimary := &Account{
		ID: 371021, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
		GroupIDs: []int64{groupID}, Credentials: mapping,
	}
	staleBackup := &Account{
		ID: 371022, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
		GroupIDs: []int64{groupID}, Credentials: mapping,
	}
	dbPrimary := *stalePrimary
	dbPrimary.Credentials = nil
	dbBackup := *staleBackup
	repo := schedulerTestOpenAIAccountRepo{accounts: []Account{dbPrimary, dbBackup}}
	ctx = WithCompositeRouteDecision(ctx, CompositeRouteDecision{
		Matched: true, Source: CompositeRouteSourceAccount, GroupID: groupID,
		PublicModel: "team-alias", TargetPlatform: PlatformOpenAI, UpstreamModel: "team-alias",
	})
	acquiredIDs, releasedIDs := []int64{}, []int64{}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		cfg:              &config.Config{RunMode: config.RunModeStandard},
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			snapshotAccounts: []*Account{stalePrimary, staleBackup},
			accountsByID:     map[int64]*Account{stalePrimary.ID: stalePrimary, staleBackup.ID: staleBackup},
		}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquiredIDs: &acquiredIDs, releasedIDs: &releasedIDs,
		}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "", "", "team-alias", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, dbBackup.ID, selection.Account.ID)
	if containsInt64(acquiredIDs, dbPrimary.ID) {
		require.Contains(t, releasedIDs, dbPrimary.ID)
	}
	require.Contains(t, acquiredIDs, dbBackup.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ContinuationOwnerUnavailable(t *testing.T) {
	for _, reason := range []string{"disabled", "excluded", "wrong_group", "missing", "wrong_transport"} {
		t.Run(reason, func(t *testing.T) {
			budget := NewOpenAITurnBudget(nil)
			require.NoError(t, budget.attemptBudget.Take(nil))
			require.NoError(t, budget.attemptBudget.TakePreparation())
			_, reserveErr := budget.attemptBudget.ReserveWait(time.Millisecond, 0)
			require.NoError(t, reserveErr)
			spent := budget.attemptBudget.Snapshot()
			ctx := WithOpenAITurnBudget(t.Context(), budget)
			groupID := int64(101074)
			owner := Account{ID: 37131, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10, GroupIDs: []int64{groupID}}
			backup := Account{ID: 37132, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
			excluded := map[int64]struct{}{}
			requiredTransport := OpenAIUpstreamTransportAny
			switch reason {
			case "disabled":
				owner.Schedulable = false
			case "excluded":
				excluded[owner.ID] = struct{}{}
			case "wrong_group":
				owner.GroupIDs = []int64{groupID + 1}
			case "wrong_transport":
				requiredTransport = OpenAIUpstreamTransportResponsesWebsocketV2
				owner.Extra = map[string]any{"openai_apikey_responses_websockets_v2_enabled": false}
				backup.Extra = map[string]any{"openai_apikey_responses_websockets_v2_enabled": true}
			}
			accounts := []Account{owner, backup}
			if reason == "missing" {
				accounts = accounts[1:]
			}
			var acquired, released []int64
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
				cache:       &schedulerTestGatewayCache{}, cfg: newSchedulerTestOpenAIWSV2Config(),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, releasedIDs: &released}),
			}
			store := svc.getOpenAIWSStateStore()
			require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_owner_unavailable", owner.ID, time.Hour))
			if reason == "wrong_group" || reason == "wrong_transport" {
				// Reach the later compatibility check so slot release is tested.
				require.NoError(t, store.BindResponseMode(ctx, groupID, "resp_owner_unavailable", gatewaysession.ResponseModeNative, time.Hour))
			}
			for attempt := 0; attempt < 2; attempt++ {
				selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "resp_owner_unavailable", "", "gpt-5.1", excluded, requiredTransport, false)
				require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable, "an opaque predecessor must not be sent to the backup account")
				require.Nil(t, selection)
				require.False(t, decision.StickyPreviousHit)
				require.Equal(t, spent, budget.attemptBudget.Snapshot(), "owner rejection cannot reset or spend the operation attempt/preparation/wait budget")
				bound, lookupErr := store.GetResponseAccount(ctx, groupID, "resp_owner_unavailable")
				require.NoError(t, lookupErr)
				require.Equal(t, owner.ID, bound, "failed selection must not erase continuation ownership")
			}
			require.NotContains(t, acquired, backup.ID)
			if reason == "wrong_transport" {
				require.Equal(t, []int64{owner.ID, owner.ID}, acquired, "each incompatible owner acquisition must be released without probing the backup")
			}
			if reason == "wrong_group" || reason == "wrong_transport" {
				require.Equal(t, acquired, released, "rejecting an acquired incompatible owner must release its slots")
			}
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ContinuationUnavailableDoesNotRetryCapabilitySelection(t *testing.T) {
	groupID := int64(101078)
	owner := Account{ID: 37171, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}, Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": false}}
	backup := owner
	backup.ID++
	backup.Extra = map[string]any{"openai_apikey_responses_websockets_v2_enabled": true}
	var acquired, released []int64
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{owner, backup}},
		cache:       &schedulerTestGatewayCache{}, cfg: newSchedulerTestOpenAIWSV2Config(),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, releasedIDs: &released}),
	}
	budget := NewOpenAITurnBudget(nil)
	require.NoError(t, budget.attemptBudget.Take(nil))
	ctx := WithOpenAITurnBudget(t.Context(), budget)
	before := budget.attemptBudget.Snapshot()
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_capability_owner", owner.ID, time.Hour))
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseMode(ctx, groupID, "resp_capability_owner", gatewaysession.ResponseModeNative, time.Hour))

	selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "resp_capability_owner", "", "gpt-5.1", nil,
		OpenAIUpstreamTransportResponsesWebsocketV2, OpenAIEndpointCapabilityResponses, false, false, true)

	require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
	require.False(t, errors.Is(err, ErrSelectionDependency), "a known unavailable parent is not a storage outage")
	require.Nil(t, selection)
	require.Equal(t, before, budget.attemptBudget.Snapshot(), "local continuation refusal cannot spend dependency-retry preparation or wait budget")
	require.Equal(t, []int64{owner.ID}, acquired, "do not retry the local refusal or acquire an unrelated backup")
	require.Equal(t, acquired, released)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ContinuationOwnerBusyWaits(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101075)
	accounts := []Account{
		{ID: 37141, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
		{ID: 37142, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
	}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: &schedulerTestGatewayCache{}, cfg: &config.Config{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{37141: false}})}
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_busy_owner", 37141, time.Hour))
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseMode(ctx, groupID, "resp_busy_owner", gatewaysession.ResponseModeNative, time.Hour))
	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "resp_busy_owner", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, int64(37141), selection.Account.ID)
	require.NotNil(t, selection.WaitPlan)
	require.True(t, decision.StickyPreviousHit)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_UnknownPreviousPreservesProviderRouting(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101076)
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 37151, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}}},
		cache:       &schedulerTestGatewayCache{}, cfg: &config.Config{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	for _, previous := range []string{"resp_external_provider", ""} {
		selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, previous, "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
		require.NoError(t, err, "unknown provider IDs and expanded history retain ordinary routing")
		require.Equal(t, int64(37151), selection.Account.ID)
		require.False(t, decision.StickyPreviousHit)
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_CloudContinuationUsesVerifiedOwner(t *testing.T) {
	groupID := int64(101077)
	owner := Account{ID: 37161, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}, Extra: map[string]any{OpenAICloudEnabledKey: true}}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{owner}}, cache: &schedulerTestGatewayCache{}, cfg: newSchedulerTestOpenAIWSV2Config(), cloudSessions: &OpenAICloudSessions{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
	ctx := context.WithValue(t.Context(), cloudContinuationKey{}, &OpenAICloudBinding{AccountID: owner.ID})
	// Native affinity is not authoritative for a verified Cloud continuation.
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_cloud_owned", 999, time.Hour))
	selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "resp_cloud_owned", "", "gpt-5.1", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityResponses, false, false, true)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, owner.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ExplicitPreviousOwnerOverridesPriority(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(101072)
	accounts := []Account{
		{
			ID:          37111,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    100,
			GroupIDs:    []int64{groupID},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
		{
			ID:          37112,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true", "true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_weighted_unmovable", 37111, time.Hour))
	require.NoError(t, store.BindResponseMode(ctx, groupID, "resp_weighted_unmovable", gatewaysession.ResponseModeNative, time.Hour))

	selection, decision, err := svc.SelectAccountWithSchedulerForCapability(
		ctx,
		&groupID,
		"resp_weighted_unmovable",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		OpenAIEndpointCapabilityChatCompletions,
		false,
		false,
		true,
		PlatformOpenAI,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(37111), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.True(t, decision.StickyPreviousHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	selection, decision, err = svc.SelectAccountWithSchedulerForCapability(
		ctx,
		&groupID,
		"resp_weighted_unmovable",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		OpenAIEndpointCapabilityChatCompletions,
		false,
		true,
		true,
		PlatformOpenAI,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(37111), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.True(t, decision.StickyPreviousHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_PreviousResponseCompactUnsupportedPreservesOwner(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(101073)
	accounts := []Account{
		{
			ID:          37121,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
				"openai_compact_mode":                           OpenAICompactModeForceOff,
			},
		},
		{
			ID:          37122,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    10,
			GroupIDs:    []int64{groupID},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
				"openai_compact_mode":                           OpenAICompactModeForceOn,
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_compact_unsupported", 37121, time.Hour))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_compact_unsupported",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		true,
	)
	require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
	require.Nil(t, selection)
	require.False(t, decision.StickyPreviousHit)

	accountID, err := store.GetResponseAccount(ctx, groupID, "resp_compact_unsupported")
	require.NoError(t, err)
	require.Equal(t, int64(37121), accountID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_Enabled_EmbeddingsSkipsChatOnlyAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10111)
	accounts := []Account{
		{
			ID:          37011,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions"},
			},
		},
		{
			ID:          37012,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions", "embeddings"},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithSchedulerForCapability(
		ctx,
		&groupID,
		"",
		"",
		"text-embedding-3-small",
		nil,
		OpenAIUpstreamTransportHTTPSSE,
		OpenAIEndpointCapabilityEmbeddings,
		false,
		false,
		true,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(37012), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 1, decision.CandidateCount)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_Enabled_EmbeddingsSkipsChatOnlyStickyBindings(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10112)
	accounts := []Account{
		{
			ID:          37021,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions"},
			},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
		{
			ID:          37022,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions", "embeddings"},
			},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_embeddings": 37021,
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_embeddings_chat_only", 37021, time.Hour))

	selection, decision, err := svc.SelectAccountWithSchedulerForCapability(
		ctx,
		&groupID,
		"resp_embeddings_chat_only",
		"session_hash_embeddings",
		"text-embedding-3-small",
		nil,
		OpenAIUpstreamTransportHTTPSSE,
		OpenAIEndpointCapabilityEmbeddings,
		false,
		false,
		true,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(37022), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickyPreviousHit)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(37022), cache.sessionBindings["openai:session_hash_embeddings"])
}

func TestOpenAIGatewayService_OpenAIAccountSchedulerMetrics_AlwaysCollects(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	svc := &OpenAIGatewayService{}
	ttft := 120
	svc.ReportOpenAIAccountScheduleSuccess(context.Background(), &Account{ID: 10}, "", &ttft)
	svc.RecordOpenAIAccountSwitch()

	snapshot := svc.SnapshotOpenAIAccountSchedulerMetrics()
	require.Equal(t, int64(1), snapshot.AccountSwitchTotal)
	require.Equal(t, 1, snapshot.RuntimeStatsEntryCount)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SkipsQuarantinedSharedProxy(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	proxyA := int64(4698)
	proxyB := int64(4699)
	accounts := []Account{
		{ID: 469801, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, ProxyID: &proxyA},
		{ID: 469802, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, ProxyID: &proxyA},
		{ID: 469803, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, ProxyID: &proxyB},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		openaiProxyStreamCircuit: newOpenAIProxyStreamCircuit(openAIProxyStreamCircuitSettings{
			failureThreshold: 1,
			failureWindow:    time.Minute,
			quarantineTTL:    10 * time.Minute,
			maxEntries:       16,
		}),
	}
	svc.openaiProxyStreamCircuit.recordFailure(proxyA, time.Now())

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), nil, "", "", "gpt-5.6-sol", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(469803), selection.Account.ID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_QuarantineDoesNotBypassCooldown(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	proxyA := int64(5056)
	accounts := []Account{
		{ID: 505601, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, ProxyID: &proxyA},
		{ID: 505602, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, ProxyID: &proxyA},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		openaiProxyStreamCircuit: newOpenAIProxyStreamCircuit(openAIProxyStreamCircuitSettings{
			failureThreshold: 1,
			failureWindow:    time.Minute,
			quarantineTTL:    10 * time.Minute,
			maxEntries:       16,
		}),
	}
	tripped, _ := svc.openaiProxyStreamCircuit.recordFailure(proxyA, time.Now())
	require.True(t, tripped)

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), nil, "", "", "gpt-5.6-sol", nil, OpenAIUpstreamTransportAny, false,
	)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	require.True(t, svc.openaiProxyStreamCircuit.isBlocked(proxyA, time.Now()))
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyRateLimitedAccountFallsBackToFreshCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10101)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	staleSticky := &Account{ID: 31001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}
	staleBackup := &Account{ID: 31002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	freshSticky := &Account{ID: 31001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}, RateLimitResetAt: &rateLimitedUntil}
	freshBackup := &Account{ID: 31002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_rate_limited": 31001}}
	snapshotCache := &openAISnapshotCacheStub{snapshotAccounts: []*Account{staleSticky, staleBackup}, accountsByID: map[int64]*Account{31001: freshSticky, 31002: freshBackup}}
	snapshotService := &SchedulerSnapshotService{cache: snapshotCache}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{*freshSticky, *freshBackup}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot:  snapshotService,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_rate_limited", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(31002), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_AutoPauseBy5hThreshold(t *testing.T) {
	ctx := context.Background()
	primary := Account{
		ID:          35001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent":   95.0,
			"auto_pause_5h_threshold": 0.95,
		},
	}
	secondary := Account{ID: 35002, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35002), account.ID)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_AllowsBelow5hThreshold(t *testing.T) {
	ctx := context.Background()
	primary := Account{
		ID:          35101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent":   80.0,
			"auto_pause_5h_threshold": 0.95,
		},
	}
	secondary := Account{ID: 35102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35101), account.ID)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_AutoPauseBy7dThreshold(t *testing.T) {
	ctx := context.Background()
	primary := Account{
		ID:          35201,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_7d_used_percent":   95.0,
			"auto_pause_7d_threshold": 0.95,
		},
	}
	secondary := Account{ID: 35202, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35202), account.ID)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_UnconfiguredThresholdKeepsLegacyBehavior(t *testing.T) {
	ctx := context.Background()
	primary := Account{ID: 35301, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, Extra: map[string]any{"codex_5h_used_percent": 99.0, "codex_7d_used_percent": 99.0}}
	secondary := Account{ID: 35302, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35301), account.ID)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_UsesGlobalDefaultThreshold(t *testing.T) {
	ctx := withOpenAIQuotaAutoPauseSettings(context.Background(), OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold5h: 0.95})
	primary := Account{
		ID:          35401,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent": 95.0,
		},
	}
	secondary := Account{ID: 35402, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35402), account.ID)
}

// Regression: a per-account explicit-disable flag exempts the account from auto-pause
// even when a global default threshold is set. Without this, "leave threshold blank"
// silently falls back to global default and admins have no way to whitelist a single
// account.
func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_PerAccountDisableOverridesGlobalDefault(t *testing.T) {
	ctx := withOpenAIQuotaAutoPauseSettings(context.Background(), OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold5h: 0.95})
	// Account has high usage AND no per-account threshold (would normally fall back to
	// the global default and get paused), but the explicit disable flag is set.
	primary := Account{
		ID:          35701,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent":  99.0,
			"auto_pause_5h_disabled": true,
		},
	}
	secondary := Account{ID: 35702, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35701), account.ID)
}

// Disable is per-window: disabling only 5h must still allow 7d auto-pause to fire.
func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_PerWindowDisableScoped(t *testing.T) {
	ctx := context.Background()
	primary := Account{
		ID:          35801,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent":   99.0,
			"codex_7d_used_percent":   99.0,
			"auto_pause_5h_disabled":  true,
			"auto_pause_7d_threshold": 0.95,
		},
	}
	secondary := Account{ID: 35802, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35802), account.ID, "7d auto-pause must still fire even though 5h is disabled")
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_StaleUsageWindowResetSkipsPause(t *testing.T) {
	ctx := context.Background()
	// Usage is over threshold but the window's reset time has already passed, so the
	// cached percentage is stale (the real window rolled over) and the account must NOT
	// stay paused — otherwise it could be skipped forever with no traffic to refresh it.
	primary := Account{
		ID:          35501,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent":   99.0,
			"auto_pause_5h_threshold": 0.95,
			"codex_5h_reset_at":       time.Now().Add(-time.Minute).Format(time.RFC3339),
		},
	}
	secondary := Account{ID: 35502, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35501), account.ID)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_FreshUsageWindowStillPauses(t *testing.T) {
	ctx := context.Background()
	// Same as above but the window has not reset yet, so the account stays paused.
	primary := Account{
		ID:          35601,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent":   99.0,
			"auto_pause_5h_threshold": 0.95,
			"codex_5h_reset_at":       time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
	secondary := Account{ID: 35602, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35602), account.ID)
}

// Issue #2994: an account poisoned with an inflated used% (e.g. from the reverted #2918
// inversion) gets excluded from scheduling, and a paused account never receives traffic to
// refresh its snapshot. When the snapshot is stale (codex_usage_updated_at older than the
// staleness bound) the account must be allowed a request so it can self-heal from the real
// response headers when no valid future window reset is known.
func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_StaleUsageSnapshotSkipsPause_Issue2994(t *testing.T) {
	ctx := context.Background()
	primary := Account{
		ID:          35701,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent":   99.0,
			"auto_pause_5h_threshold": 0.95,
			// No reset time is known, so stale snapshot self-heal applies.
			// Snapshot is stale: older than openAICodexAutoPauseStaleAfter (2h).
			"codex_usage_updated_at": time.Now().Add(-3 * time.Hour).Format(time.RFC3339),
		},
	}
	secondary := Account{ID: 35702, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35701), account.ID)
}

// Issue #2994 guardrail: a genuinely-exhausted account whose snapshot was refreshed recently
// (codex_usage_updated_at fresh) must STILL be auto-paused. The stale self-heal must not let a
// real 99%-used account escape pause.
func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_FreshExhaustedSnapshotStillPauses_Issue2994(t *testing.T) {
	ctx := context.Background()
	primary := Account{
		ID:          35801,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"codex_5h_used_percent":   99.0,
			"auto_pause_5h_threshold": 0.95,
			"codex_5h_reset_at":       time.Now().Add(time.Hour).Format(time.RFC3339),
			// Snapshot refreshed 1 minute ago: not stale, so the account stays paused.
			"codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
		},
	}
	secondary := Account{ID: 35802, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}, cfg: &config.Config{}}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(35802), account.ID)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_SkipsFreshlyRateLimitedSnapshotCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10102)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	stalePrimary := &Account{ID: 32001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}
	staleSecondary := &Account{ID: 32002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	freshPrimary := &Account{ID: 32001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}, RateLimitResetAt: &rateLimitedUntil}
	freshSecondary := &Account{ID: 32002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	snapshotCache := &openAISnapshotCacheStub{snapshotAccounts: []*Account{stalePrimary, staleSecondary}, accountsByID: map[int64]*Account{32001: freshPrimary, 32002: freshSecondary}}
	snapshotService := &SchedulerSnapshotService{cache: snapshotCache}
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{*freshPrimary, *freshSecondary}},
		cfg:               &config.Config{},
		rateLimitService:  newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot: snapshotService,
	}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(32002), account.ID)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_ModelRateLimitOnlySkipsThatModel(t *testing.T) {
	ctx := context.Background()
	resetAt := time.Now().Add(30 * time.Minute).Format(time.RFC3339)
	primary := Account{
		ID:          32101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				"gpt-5.4": map[string]any{
					"rate_limit_reset_at": resetAt,
				},
			},
		},
	}
	secondary := Account{
		ID:          32102,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    5,
	}
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}},
		cfg:         &config.Config{},
	}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.4", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(32102), account.ID)

	account, err = svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.3", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(32101), account.ID)

	// Both engines and sticky hits must filter model cooldown before Top-K,
	// without disabling the account for a different model.
	for _, enabled := range []string{"false", "true"} {
		for _, session := range []string{"", "model-scope"} {
			for _, model := range []string{"gpt-5.4", "gpt-5.3"} {
				t.Run(enabled+"/"+session+"/"+model, func(t *testing.T) {
					traceCtx := WithSchedulingTrace(context.Background(), nil)
					schedulerSvc := &OpenAIGatewayService{
						accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}},
						cfg:                newSchedulerTestSubscriptionPriorityConfig(),
						cache:              &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:model-scope": primary.ID}},
						rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(enabled),
						concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
					}
					selection, _, err := schedulerSvc.SelectAccountWithScheduler(traceCtx, nil, "", session, model, nil, OpenAIUpstreamTransportAny, false)
					require.NoError(t, err)
					require.NotNil(t, selection)
					want := primary.ID
					if model == "gpt-5.4" {
						want = secondary.ID
					}
					require.Equal(t, want, selection.Account.ID)
					if selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
					policy := "openai_unified_v2"
					trace := WithOpsSchedulingTrace(traceCtx, nil)
					require.Contains(t, trace[0].Detail, policy)
				})
			}
		}
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyDBRuntimeRecheckSkipsStaleCachedAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10103)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	staleSticky := &Account{ID: 33001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}
	staleBackup := &Account{ID: 33002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	dbSticky := Account{ID: 33001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}, RateLimitResetAt: &rateLimitedUntil}
	dbBackup := Account{ID: 33002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_db_runtime_recheck": 33001}}
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{staleSticky, staleBackup},
		accountsByID:     map[int64]*Account{33001: staleSticky, 33002: staleBackup},
	}
	snapshotService := &SchedulerSnapshotService{cache: snapshotCache}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{dbSticky, dbBackup}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot:  snapshotService,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_db_runtime_recheck", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(33002), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_DBRuntimeRecheckSkipsStaleCachedCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10104)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	stalePrimary := &Account{ID: 34001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}
	staleSecondary := &Account{ID: 34002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	dbPrimary := Account{ID: 34001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}, RateLimitResetAt: &rateLimitedUntil}
	dbSecondary := Account{ID: 34002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{stalePrimary, staleSecondary},
		accountsByID:     map[int64]*Account{34001: stalePrimary, 34002: staleSecondary},
	}
	snapshotService := &SchedulerSnapshotService{cache: snapshotCache}
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{dbPrimary, dbSecondary}},
		cfg:               &config.Config{},
		rateLimitService:  newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot: snapshotService,
	}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(34002), account.ID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DBFreshGroupRecheckReleasesMovedAccount(t *testing.T) {
	ctx := context.Background()
	groupID, otherGroupID := int64(10105), int64(10106)
	stalePrimary := &Account{ID: 34101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}
	staleBackup := &Account{ID: 34102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10, GroupIDs: []int64{groupID}}
	dbPrimary := *stalePrimary
	dbPrimary.GroupIDs = []int64{otherGroupID}
	dbBackup := *staleBackup
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{stalePrimary, staleBackup},
		accountsByID:     map[int64]*Account{stalePrimary.ID: stalePrimary, staleBackup.ID: staleBackup},
	}
	acquiredIDs, releasedIDs := []int64{}, []int64{}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{dbPrimary, dbBackup}},
		cfg:               cfg,
		schedulerSnapshot: &SchedulerSnapshotService{cache: snapshotCache},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquiredIDs: &acquiredIDs,
			releasedIDs: &releasedIDs,
		}),
	}
	scheduler := &defaultOpenAIAccountScheduler{service: svc}

	selection, _, err := scheduler.tryAcquireOpenAISelectionOrder(ctx, OpenAIAccountScheduleRequest{
		GroupID: &groupID, Platform: PlatformOpenAI, RequestedModel: "gpt-5.1",
	}, []openAIAccountCandidateScore{
		{account: stalePrimary, loadInfo: &AccountLoadInfo{AccountID: stalePrimary.ID}},
		{account: staleBackup, loadInfo: &AccountLoadInfo{AccountID: staleBackup.ID}},
	})
	require.NoError(t, err)
	require.Equal(t, staleBackup.ID, selection.Account.ID)
	require.Equal(t, []int64{stalePrimary.ID, staleBackup.ID}, acquiredIDs)
	require.Equal(t, []int64{stalePrimary.ID}, releasedIDs)
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_SelectAccountWithLoadAwareness_DBFreshGroupRecheckWaitsOnValidAccount(t *testing.T) {
	ctx := context.Background()
	groupID, otherGroupID := int64(10107), int64(10108)
	stalePrimary := &Account{ID: 34201, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}
	staleBackup := &Account{ID: 34202, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10, GroupIDs: []int64{groupID}}
	dbPrimary := *stalePrimary
	dbPrimary.GroupIDs = []int64{otherGroupID}
	dbBackup := *staleBackup
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{stalePrimary, staleBackup},
		accountsByID:     map[int64]*Account{stalePrimary.ID: stalePrimary, staleBackup.ID: staleBackup},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{dbPrimary, dbBackup}},
		cfg:               cfg,
		schedulerSnapshot: &SchedulerSnapshotService{cache: snapshotCache},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{staleBackup.ID: false},
		}),
	}

	selection, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, staleBackup.ID, selection.Account.ID)
	require.Equal(t, staleBackup.ID, selection.WaitPlan.AccountID)
}

func TestOpenAIGatewayService_RecheckSelectedOpenAIAccountFromDB_SimpleModeUsesFullPool(t *testing.T) {
	grouped := Account{ID: 34301, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{99}}
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{grouped}},
		cfg:               &config.Config{RunMode: config.RunModeSimple},
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{}},
	}
	requestedGroupID := int64(100)

	for _, groupID := range []*int64{nil, &requestedGroupID} {
		fresh := svc.recheckSelectedOpenAIAccountFromDB(context.Background(), &grouped, groupID, PlatformOpenAI, "gpt-5.1", false, "")
		require.NotNil(t, fresh)
		require.Equal(t, grouped.ID, fresh.ID)
	}

	ungrouped := grouped
	ungrouped.ID++
	ungrouped.GroupIDs = nil
	standardSvc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{grouped, ungrouped}},
		cfg:               &config.Config{RunMode: config.RunModeStandard},
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{}},
	}
	require.Nil(t, standardSvc.recheckSelectedOpenAIAccountFromDB(context.Background(), &grouped, nil, PlatformOpenAI, "gpt-5.1", false, ""))
	require.NotNil(t, standardSvc.recheckSelectedOpenAIAccountFromDB(context.Background(), &ungrouped, nil, PlatformOpenAI, "gpt-5.1", false, ""))
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_PreviousResponseSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(9)
	account := Account{
		ID:          1001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	cache := &schedulerTestGatewayCache{}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 1800
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_001", account.ID, time.Hour))
	require.NoError(t, store.BindResponseMode(ctx, groupID, "resp_prev_001", gatewaysession.ResponseModeNative, time.Hour))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_prev_001",
		"session_hash_001",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.True(t, decision.StickyPreviousHit)
	require.Equal(t, account.ID, cache.sessionBindings["openai:session_hash_001"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIStickySession_LookupFailurePreservesBinding(t *testing.T) {
	for _, engine := range []string{"scheduler", "legacy"} {
		for _, phase := range []string{"lookup", "db_recheck"} {
			for _, tc := range []struct {
				name string
				err  error
			}{
				{"repository_unavailable", errors.New("repository unavailable")},
				{"deadline", context.DeadlineExceeded},
				{"canceled", context.Canceled},
				{"account_deleted", fmt.Errorf("lookup: %w", ErrAccountNotFound)},
			} {
				t.Run(engine+"/"+phase+"/"+tc.name, func(t *testing.T) {
					ctx := context.Background()
					groupID := int64(10)
					account := Account{ID: 2001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
					const sessionHash = "lookup_failure"
					const cacheKey = "openai:" + sessionHash
					cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{cacheKey: account.ID}}
					svc := &OpenAIGatewayService{
						accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}, getByIDErr: tc.err},
						cache:              cache,
						cfg:                &config.Config{},
						concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
					}
					if phase == "db_recheck" {
						svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{accountsByID: map[int64]*Account{account.ID: &account}}}
					}
					selectSticky := func() *Account {
						if engine == "legacy" {
							return svc.tryStickySessionHit(ctx, &groupID, PlatformOpenAI, sessionHash, "gpt-5.1", nil, false, 0, "")
						}
						selection, escaped, err := (&defaultOpenAIAccountScheduler{service: svc}).selectBySessionHash(ctx, OpenAIAccountScheduleRequest{
							GroupID: &groupID, Platform: PlatformOpenAI, SessionHash: sessionHash, RequestedModel: "gpt-5.1", RequiredTransport: OpenAIUpstreamTransportAny,
						})
						require.NoError(t, err)
						require.False(t, escaped)
						if selection == nil {
							return nil
						}
						if selection.ReleaseFunc != nil {
							selection.ReleaseFunc()
						}
						return selection.Account
					}
					require.Nil(t, selectSticky(), "a failed lookup must not admit an account")
					if errors.Is(tc.err, ErrAccountNotFound) {
						require.Equal(t, 1, cache.deletedSessions[cacheKey])
						require.NotContains(t, cache.sessionBindings, cacheKey)
						return
					}
					require.Zero(t, cache.deletedSessions[cacheKey])
					require.Equal(t, account.ID, cache.sessionBindings[cacheKey])
					svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{account}}
					recovered := selectSticky()
					require.NotNil(t, recovered)
					require.Equal(t, account.ID, recovered.ID)
				})
			}
		}
	}
}

func TestOpenAIStickySession_DBFreshRequestVetoPreservesBinding(t *testing.T) {
	for _, engine := range []string{"scheduler", "legacy"} {
		for _, veto := range []string{"profit", "model"} {
			t.Run(engine+"/"+veto, func(t *testing.T) {
				groupID := int64(10)
				ctx := context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{groupID: groupID, platform: PlatformOpenAI, threshold: 0.5})
				stale := Account{ID: 2001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
				profitControlTestAccountWithRate(&stale, 0.3)
				fresh := stale
				if veto == "profit" {
					profitControlTestAccountWithRate(&fresh, 0.8)
				} else {
					fresh.Credentials = map[string]any{"model_mapping": map[string]any{"another-model": "another-model"}}
				}
				cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:profit_recheck": stale.ID}}
				svc := &OpenAIGatewayService{
					accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{fresh}},
					cache:             cache,
					cfg:               &config.Config{},
					schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{accountsByID: map[int64]*Account{stale.ID: &stale}}},
				}
				if engine == "legacy" {
					require.Nil(t, svc.tryStickySessionHit(ctx, &groupID, PlatformOpenAI, "profit_recheck", "gpt-5.1", nil, false, 0, ""))
				} else {
					selection, escaped, err := (&defaultOpenAIAccountScheduler{service: svc}).selectBySessionHash(ctx, OpenAIAccountScheduleRequest{
						GroupID: &groupID, Platform: PlatformOpenAI, SessionHash: "profit_recheck", RequestedModel: "gpt-5.1", RequiredTransport: OpenAIUpstreamTransportAny,
					})
					require.NoError(t, err)
					require.Nil(t, selection)
					require.False(t, escaped)
				}
				require.Zero(t, cache.deletedSessions["openai:profit_recheck"])
				require.Equal(t, stale.ID, cache.sessionBindings["openai:profit_recheck"])
			})
		}
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10)
	account := Account{
		ID:          2001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_abc": account.ID,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_abc",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_BusySessionUsesAvailableCapacity(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10100)
	accounts := []Account{
		{
			ID:          21001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          21002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_sticky_busy": 21001,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true

	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{
			21001: false, // sticky 账号已满
			21002: true,  // 普通软亲和不应阻塞可用容量
		},
		waitCounts: map[int64]int{
			21001: 999,
		},
		loadMap: map[int64]*AccountLoadInfo{
			21001: {AccountID: 21001, LoadRate: 90, WaitingCount: 9},
			21002: {AccountID: 21002, LoadRate: 1, WaitingCount: 0},
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_sticky_busy",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21002), selection.Account.ID)
	require.True(t, selection.Acquired)
	require.Nil(t, selection.WaitPlan)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LongTTFTAloneDoesNotEvictSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10101)
	accounts := []Account{
		{
			ID:          21101,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          21102,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			GroupIDs:    []int64{groupID},
		},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_ttft": 21101}}
	cfg := &config.Config{}
	concurrencyCache := schedulerTestConcurrencyCache{acquireResults: map[int64]bool{21102: true}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
		openaiAccountStats: newOpenAIAccountRuntimeStats(),
	}
	fastTTFT := 14999
	svc.openaiAccountStats.report(21101, true, &fastTTFT, "gpt-5.1")
	stableTTFT := 14999
	svc.openaiAccountStats.report(21101, true, &stableTTFT, "gpt-5.1")

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_sticky_ttft", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21101), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	slowTTFT := 20000
	for i := 0; i < openAIHealthMinSamples; i++ {
		svc.openaiAccountStats.report(21101, true, &slowTTFT, "gpt-5.1")
	}

	selection, decision, err = svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_sticky_ttft", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21101), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
	require.Equal(t, int64(21101), cache.sessionBindings["openai:session_hash_sticky_ttft"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyEscapeByErrorRate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10102)
	accounts := []Account{
		{ID: 21201, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}},
		{ID: 21202, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_error_rate": 21201}}
	cfg := &config.Config{}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{21202: true}}),
		openaiAccountStats: newOpenAIAccountRuntimeStats(),
	}
	for i := 0; i < 3; i++ {
		svc.openaiAccountStats.report(21201, false, nil, "gpt-5.1")
	}
	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_sticky_error_rate", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21201), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	for i := 0; i < openAIHealthMinSamples-3; i++ {
		svc.openaiAccountStats.report(21201, false, nil, "gpt-5.1")
	}

	selection, decision, err = svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_sticky_error_rate", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21202), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(21202), cache.sessionBindings["openai:session_hash_sticky_error_rate"], "confirmed unhealthy affinity migrates; temporary busy borrowing preserves home")
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyBusyEscapes(t *testing.T) {
	ctx := WithSchedulingTrace(context.Background(), nil)
	groupID := int64(10103)
	accounts := []Account{
		{ID: 21301, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}},
		{ID: 21302, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_busy_escape": 21301}}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21301: false, 21302: true},
		waitCounts:     map[int64]int{21301: 999},
		loadMap: map[int64]*AccountLoadInfo{
			21301: {AccountID: 21301, LoadRate: 95, WaitingCount: 9},
			21302: {AccountID: 21302, LoadRate: 1, WaitingCount: 0},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_sticky_busy_escape", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21302), selection.Account.ID)
	require.Nil(t, selection.WaitPlan)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	trace := WithOpsSchedulingTrace(ctx, nil)
	require.Len(t, trace, 1)
	require.Contains(t, trace[0].Detail, `"reason":"concurrency_full"`)
	require.Contains(t, trace[0].Detail, `"policy":"openai_unified_v2"`)
	require.Contains(t, trace[0].Detail, `"account_id":21302`)
	require.Nil(t, LastOpsUpstreamAttempt(trace), "slot attempts are not upstream calls")
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_TaskOwnerDoesNotEscape(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10104)
	accounts := []Account{
		{ID: 21401, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}},
		{ID: 21402, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_disabled": 21401}}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21401: false, 21402: true},
		waitCounts:     map[int64]int{21401: 999},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
		openaiAccountStats: newOpenAIAccountRuntimeStats(),
	}
	slowTTFT := 20000
	svc.openaiAccountStats.report(21401, true, &slowTTFT)
	for i := 0; i < 5; i++ {
		svc.openaiAccountStats.report(21401, false, nil)
	}

	selection, decision, err := svc.getOpenAIAccountScheduler(ctx).Select(ctx, OpenAIAccountScheduleRequest{GroupID: &groupID, Platform: PlatformOpenAI, SessionHash: "session_hash_sticky_disabled", RequestedModel: "gpt-5.1", DisableStickyEscape: true})
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21401), selection.Account.ID)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(21401), selection.WaitPlan.AccountID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SubscriptionRespectsAccountPriority(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10120)
	cheapAPIKey := upstreamCostTestAccount(21602, UpstreamBillingProbeStatusOK, 0.01, time.Now().Add(-time.Minute), 30*time.Minute)
	cheapAPIKey.Status = StatusActive
	cheapAPIKey.Schedulable = true
	cheapAPIKey.Concurrency = 1
	cheapAPIKey.Priority = 0
	cheapAPIKey.GroupIDs = []int64{groupID}
	accounts := []Account{
		{
			ID:          21601,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    10,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"plan_type": "plus"},
		},
		*cheapAPIKey,
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21601: true, 21602: true},
		loadMap: map[int64]*AccountLoadInfo{
			21601: {AccountID: 21601, LoadRate: 90, WaitingCount: 1},
			21602: {AccountID: 21602, LoadRate: 0, WaitingCount: 0},
		},
	}
	cfg := newSchedulerTestSubscriptionPriorityConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true", "", "true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_subscription_first", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21602), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 2, decision.TopK)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SubscriptionPriorityFallsBackWhenSubscriptionFull(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10121)
	accounts := []Account{
		{
			ID:          21611,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"plan_type": "team"},
		},
		{
			ID:          21612,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
			GroupIDs:    []int64{groupID},
		},
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21611: false, 21612: true},
		loadMap: map[int64]*AccountLoadInfo{
			21611: {AccountID: 21611, LoadRate: 0, WaitingCount: 0},
			21612: {AccountID: 21612, LoadRate: 90, WaitingCount: 1},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                newSchedulerTestSubscriptionPriorityConfig(),
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true", "", "true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_subscription_fallback", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21612), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.True(t, selection.Acquired)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SubscriptionPriorityDisabledUsesScore(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10122)
	accounts := []Account{
		{
			ID:          21621,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    10,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"plan_type": "pro"},
		},
		{
			ID:          21622,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21621: true, 21622: true},
		loadMap: map[int64]*AccountLoadInfo{
			21621: {AccountID: 21621, LoadRate: 90, WaitingCount: 1},
			21622: {AccountID: 21622, LoadRate: 0, WaitingCount: 0},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                newSchedulerTestSubscriptionPriorityConfig(),
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true", "", "false"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_subscription_disabled", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21622), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_UsesAccountPriorityWithinGroupPool(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10123)
	accounts := []Account{
		{
			ID:          21631,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			AccountGroups: []AccountGroup{
				{AccountID: 21631, GroupID: groupID, Priority: 100},
			},
			GroupIDs: []int64{groupID},
		},
		{
			ID:          21632,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    100000,
			AccountGroups: []AccountGroup{
				{AccountID: 21632, GroupID: groupID, Priority: 1},
			},
			GroupIDs: []int64{groupID},
		},
	}
	cfg := newSchedulerTestSubscriptionPriorityConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_group_priority", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21631), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIAccountScheduler_SkipsAccountBlockedForRequestedModel(t *testing.T) {
	now := time.Now()
	account := &Account{ID: 21633, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	svc := &OpenAIGatewayService{openaiModelTransient: newOpenAIAccountModelTransientState(128)}
	svc.openaiModelTransient.recordFailure(account.ID, "gpt-5.5", now)
	svc.openaiModelTransient.recordFailure(account.ID, "gpt-5.5", now.Add(time.Millisecond))
	scheduler := &defaultOpenAIAccountScheduler{service: svc}

	require.False(t, scheduler.isAccountRequestCompatible(context.Background(), account, OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.5"}))
	require.True(t, scheduler.isAccountRequestCompatible(context.Background(), account, OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.6-sol"}))
}

func TestReportOpenAIAccountScheduleResult_SuccessClearsModelTransientState(t *testing.T) {
	svc := &OpenAIGatewayService{openaiModelTransient: newOpenAIAccountModelTransientState(128)}
	now := time.Now()
	svc.openaiModelTransient.recordFailure(21636, "gpt-5.5", now)
	svc.openaiModelTransient.recordFailure(21636, "gpt-5.5", now.Add(time.Millisecond))
	require.True(t, svc.openaiModelTransient.isBlocked(21636, "gpt-5.5", now.Add(2*time.Millisecond)))

	svc.ReportOpenAIAccountScheduleSuccess(context.Background(), &Account{ID: 21636}, "gpt-5.5", nil)

	require.False(t, svc.openaiModelTransient.isBlocked(21636, "gpt-5.5", now.Add(2*time.Millisecond)))
}

func TestDefaultOpenAIAccountScheduler_ShouldEscapeStickyAccount_ThresholdBoundary(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	const accountID int64 = 21501
	for i := 0; i < openAIHealthMinSamples; i++ {
		stats.report(accountID, true, nil, "model")
	}
	stat := stats.loadOrCreate(accountID, "model")
	scheduler := &defaultOpenAIAccountScheduler{stats: stats}
	stat.errorRateEWMABits.Store(math.Float64bits(openAIHealthDegradedErrorRate))
	reason, _, _, escape := scheduler.shouldEscapeStickyAccount(accountID, "model")
	require.False(t, escape)
	require.Empty(t, reason)
	// A wall-clock read decays evidence; use a meaningful margin above the threshold.
	stat.errorRateEWMABits.Store(math.Float64bits(openAIHealthDegradedErrorRate + 0.1))
	reason, _, _, escape = scheduler.shouldEscapeStickyAccount(accountID, "model")
	require.True(t, escape)
	require.Equal(t, "error_rate", reason)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionSticky_ForceHTTP(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1010)
	account := Account{
		ID:          2101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Extra: map[string]any{
			"openai_ws_force_http": true,
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_force_http": account.ID,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_force_http",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_RequiredWSV2_SkipsStickyHTTPAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1011)
	accounts := []Account{
		{
			ID:          2201,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          2202,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			GroupIDs:    []int64{groupID},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_ws_only": 2201,
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()

	// 构造“HTTP-only 账号负载更低”的场景，验证 required transport 会强制过滤。
	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*AccountLoadInfo{
			2201: {AccountID: 2201, LoadRate: 0, WaitingCount: 0},
			2202: {AccountID: 2202, LoadRate: 90, WaitingCount: 5},
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_ws_only",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(2202), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, 1, decision.CandidateCount)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_RequiredWSV2_PreservesHTTPStickyWithoutReplacement(t *testing.T) {
	for _, staleWS := range []bool{false, true} {
		t.Run(fmt.Sprintf("stale_ws_snapshot_%t", staleWS), func(t *testing.T) {
			ctx := context.Background()
			groupID := int64(10)
			account := Account{ID: 2001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
			const sessionHash = "http_home"
			const cacheKey = "openai:" + sessionHash
			cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{cacheKey: account.ID}}
			svc := &OpenAIGatewayService{
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
				cache:              cache,
				cfg:                newSchedulerTestOpenAIWSV2Config(),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}
			if staleWS {
				stale := account
				stale.Extra = map[string]any{"openai_apikey_responses_websockets_v2_enabled": true}
				svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
					snapshotAccounts: []*Account{&stale}, accountsByID: map[int64]*Account{stale.ID: &stale},
				}}
			}
			selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", sessionHash, "gpt-5.1", nil, OpenAIUpstreamTransportResponsesWebsocketV2, false)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selection)
			require.Zero(t, cache.deletedSessions[cacheKey])
			require.Equal(t, account.ID, cache.sessionBindings[cacheKey])
			selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", sessionHash, "gpt-5.1", nil, OpenAIUpstreamTransportHTTPSSE, false)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, account.ID, selection.Account.ID)
			require.True(t, decision.StickySessionHit)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ClearsStickyAccountOutsideGroup(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1013)
	accounts := []Account{
		{
			ID:          2401,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          2402,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			AccountGroups: []AccountGroup{
				{AccountID: 2402, GroupID: groupID},
			},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_removed_group": 2401,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_removed_group",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(2402), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, 1, cache.deletedSessions["openai:session_hash_removed_group"])
	require.Equal(t, int64(2402), cache.sessionBindings["openai:session_hash_removed_group"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_RequiredWSV2_NoAvailableAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1012)
	accounts := []Account{
		{
			ID:          2301,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                newSchedulerTestOpenAIWSV2Config(),
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.Error(t, err)
	require.Nil(t, selection)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 0, decision.CandidateCount)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LoadBalanceTopKFallback(t *testing.T) {
	ctx := context.Background()
	groupID := int64(11)
	accounts := []Account{
		{
			ID:          3001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          3002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          3003,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}

	accounts[0].Priority = 2
	accounts[1].Priority = 1
	cfg := &config.Config{}

	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*AccountLoadInfo{
			3001: {AccountID: 3001, LoadRate: 95, WaitingCount: 8},
			3002: {AccountID: 3002, LoadRate: 20, WaitingCount: 1},
			3003: {AccountID: 3003, LoadRate: 10, WaitingCount: 0},
		},
		acquireResults: map[int64]bool{
			3001: false,
			3003: false, // 优先层占位失败，使用下一层可用账号
			3002: true,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3002), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 3, decision.CandidateCount)
	require.Equal(t, 3, decision.TopK)
	require.Greater(t, decision.LoadSkew, 0.0)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

// Regression: TopK initial filter must drop quota-auto-paused accounts. Otherwise
// the candidate pool is filled with paused accounts, healthy accounts fall outside
// TopK, and the scheduler returns "no available accounts" even though healthy ones
// exist.
func TestOpenAIGatewayService_SelectAccountWithScheduler_LoadBalanceTopKExcludesQuotaPaused(t *testing.T) {
	ctx := context.Background()
	groupID := int64(110)
	accounts := []Account{
		{
			ID:          37001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":   96.0,
				"auto_pause_5h_threshold": 0.95,
			},
		},
		{
			ID:          37002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
		},
	}

	cfg := &config.Config{}

	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*AccountLoadInfo{
			37001: {AccountID: 37001, LoadRate: 5, WaitingCount: 0},
			37002: {AccountID: 37002, LoadRate: 5, WaitingCount: 0},
		},
		acquireResults: map[int64]bool{
			37002: true,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(37002), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	// Only the healthy account should ever enter the candidate pool; the paused one
	// must be filtered out at the initial-filter stage.
	require.Equal(t, 1, decision.CandidateCount)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_OpenAIAccountSchedulerMetrics(t *testing.T) {
	ctx := context.Background()
	groupID := int64(12)
	account := Account{
		ID:          4001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_metrics": account.ID,
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_metrics", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	svc.ReportOpenAIAccountScheduleSuccess(context.Background(), &account, "", intPtrForTest(120))
	svc.RecordOpenAIAccountSwitch()

	snapshot := svc.SnapshotOpenAIAccountSchedulerMetrics()
	require.GreaterOrEqual(t, snapshot.SelectTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.StickySessionHitTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.AccountSwitchTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.SchedulerLatencyMsAvg, float64(0))
	require.GreaterOrEqual(t, snapshot.StickyHitRatio, 0.0)
	require.GreaterOrEqual(t, snapshot.RuntimeStatsEntryCount, 1)
}

func intPtrForTest(v int) *int {
	return &v
}

func TestOpenAIAccountRuntimeStats_ReportAndSnapshot(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	firstTTFT := 100
	for i := 0; i < openAIHealthMinSamples; i++ {
		stats.report(1001, true, &firstTTFT)
	}
	secondTTFT := 200
	stats.report(1001, true, &secondTTFT)
	failureTTFT := 9000
	stats.report(1001, false, &failureTTFT)
	stats.report(1001, false, &failureTTFT)
	errorRate, ttft, hasTTFT := stats.snapshot(1001)
	require.True(t, hasTTFT)
	require.InDelta(t, 0.36, errorRate, 1e-9)
	require.InDelta(t, 120.0, ttft, 1e-9, "failed requests must not become latency samples")
	require.Equal(t, 1, stats.size())
}

func TestOpenAIAccountRuntimeStats_ReportConcurrent(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()

	const (
		accountCount = 4
		workers      = 16
		iterations   = 800
	)
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				accountID := int64(i%accountCount + 1)
				success := (i+worker)%3 != 0
				ttft := 80 + (i+worker)%40
				stats.report(accountID, success, &ttft)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, accountCount, stats.size())
	for accountID := int64(1); accountID <= accountCount; accountID++ {
		errorRate, ttft, hasTTFT := stats.snapshot(accountID)
		require.GreaterOrEqual(t, errorRate, 0.0)
		require.LessOrEqual(t, errorRate, 1.0)
		require.True(t, hasTTFT)
		require.Greater(t, ttft, 0.0)
	}
}

func TestBuildOpenAISelectionOrder_DeterministicBySessionSeed(t *testing.T) {
	candidates := []openAIAccountCandidateScore{
		{
			account:  &Account{ID: 101},
			loadInfo: &AccountLoadInfo{LoadRate: 10, WaitingCount: 0},
			score:    4.2,
		},
		{
			account:  &Account{ID: 102},
			loadInfo: &AccountLoadInfo{LoadRate: 30, WaitingCount: 1},
			score:    3.5,
		},
		{
			account:  &Account{ID: 103},
			loadInfo: &AccountLoadInfo{LoadRate: 50, WaitingCount: 2},
			score:    2.1,
		},
	}
	req := OpenAIAccountScheduleRequest{
		GroupID:        int64PtrForTest(99),
		SessionHash:    "session_seed_fixed",
		RequestedModel: "gpt-5.1",
	}

	first := (&defaultOpenAIAccountScheduler{}).buildOpenAISelectionOrder(req, openAIAccountLoadPlan{candidates: candidates})
	second := (&defaultOpenAIAccountScheduler{}).buildOpenAISelectionOrder(req, openAIAccountLoadPlan{candidates: candidates})
	require.Len(t, first, len(candidates))
	require.Len(t, second, len(candidates))
	for i := range first {
		require.Equal(t, first[i].account.ID, second[i].account.ID)
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LoadBalanceDistributesAcrossSessions(t *testing.T) {
	ctx := context.Background()
	groupID := int64(15)
	accounts := []Account{
		{
			ID:          5101,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 3,
			Priority:    0,
		},
		{
			ID:          5102,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 3,
			Priority:    0,
		},
		{
			ID:          5103,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 3,
			Priority:    0,
		},
	}
	cfg := &config.Config{}

	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*AccountLoadInfo{
			5101: {AccountID: 5101, LoadRate: 20, WaitingCount: 1},
			5102: {AccountID: 5102, LoadRate: 20, WaitingCount: 1},
			5103: {AccountID: 5103, LoadRate: 20, WaitingCount: 1},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{sessionBindings: map[string]int64{}},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selected := make(map[int64]int, len(accounts))
	for i := 0; i < 60; i++ {
		sessionHash := fmt.Sprintf("session_hash_lb_%d", i)
		selection, decision, err := svc.SelectAccountWithScheduler(
			ctx,
			&groupID,
			"",
			sessionHash,
			"gpt-5.1",
			nil,
			OpenAIUpstreamTransportAny,
			false,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
		selected[selection.Account.ID]++
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	}

	// 多 session 应该能打散到多个账号，避免“恒定单账号命中”。
	require.GreaterOrEqual(t, len(selected), 2)
}

func TestDeriveOpenAISelectionSeed_NoAffinityAddsEntropy(t *testing.T) {
	req := OpenAIAccountScheduleRequest{
		RequestedModel: "gpt-5.1",
	}
	seed1 := deriveOpenAISelectionSeed(req)
	time.Sleep(1 * time.Millisecond)
	seed2 := deriveOpenAISelectionSeed(req)
	require.NotZero(t, seed1)
	require.NotZero(t, seed2)
	require.NotEqual(t, seed1, seed2)
}

func TestBuildOpenAISelectionOrder_HandlesInvalidScores(t *testing.T) {
	candidates := []openAIAccountCandidateScore{
		{
			account:  &Account{ID: 901},
			loadInfo: &AccountLoadInfo{LoadRate: 5, WaitingCount: 0},
			score:    math.NaN(),
		},
		{
			account:  &Account{ID: 902},
			loadInfo: &AccountLoadInfo{LoadRate: 5, WaitingCount: 0},
			score:    math.Inf(1),
		},
		{
			account:  &Account{ID: 903},
			loadInfo: &AccountLoadInfo{LoadRate: 5, WaitingCount: 0},
			score:    -1,
		},
	}
	req := OpenAIAccountScheduleRequest{
		SessionHash: "seed_invalid_scores",
	}

	order := (&defaultOpenAIAccountScheduler{}).buildOpenAISelectionOrder(req, openAIAccountLoadPlan{candidates: candidates})
	require.Len(t, order, len(candidates))
	seen := map[int64]struct{}{}
	for _, item := range order {
		seen[item.account.ID] = struct{}{}
	}
	require.Len(t, seen, len(candidates))
}

func TestClamp01_AllBranches(t *testing.T) {
	require.Equal(t, 0.0, clamp01(-0.2))
	require.Equal(t, 1.0, clamp01(1.3))
	require.Equal(t, 0.5, clamp01(0.5))
}

func TestDefaultOpenAIAccountScheduler_ReportSwitchAndSnapshot(t *testing.T) {
	schedulerAny := newDefaultOpenAIAccountScheduler(&OpenAIGatewayService{}, nil)
	scheduler, ok := schedulerAny.(*defaultOpenAIAccountScheduler)
	require.True(t, ok)

	ttft := 100
	scheduler.ReportResult(1001, true, &ttft)
	scheduler.ReportSwitch()
	scheduler.metrics.recordSelect(OpenAIAccountScheduleDecision{
		Layer:             openAIAccountScheduleLayerLoadBalance,
		LatencyMs:         8,
		LoadSkew:          0.5,
		StickyPreviousHit: true,
	})
	scheduler.metrics.recordSelect(OpenAIAccountScheduleDecision{
		Layer:            openAIAccountScheduleLayerSessionSticky,
		LatencyMs:        6,
		LoadSkew:         0.2,
		StickySessionHit: true,
	})

	snapshot := scheduler.SnapshotMetrics()
	require.Equal(t, int64(2), snapshot.SelectTotal)
	require.Equal(t, int64(1), snapshot.StickyPreviousHitTotal)
	require.Equal(t, int64(1), snapshot.StickySessionHitTotal)
	require.Equal(t, int64(1), snapshot.LoadBalanceSelectTotal)
	require.Equal(t, int64(1), snapshot.AccountSwitchTotal)
	require.Greater(t, snapshot.SchedulerLatencyMsAvg, 0.0)
	require.Greater(t, snapshot.StickyHitRatio, 0.0)
	require.Greater(t, snapshot.LoadSkewAvg, 0.0)
}

func TestOpenAIGatewayService_SchedulerWrappersAndDefaults(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	svc := &OpenAIGatewayService{}
	ttft := 120
	svc.ReportOpenAIAccountScheduleSuccess(context.Background(), &Account{ID: 10}, "", &ttft)
	svc.RecordOpenAIAccountSwitch()
	snapshot := svc.SnapshotOpenAIAccountSchedulerMetrics()
	require.Equal(t, int64(1), snapshot.AccountSwitchTotal)
	require.Equal(t, 1, snapshot.RuntimeStatsEntryCount)
	require.Equal(t, openaiStickySessionTTL, svc.openAIWSSessionStickyTTL())

	defaultWeights := svc.openAIWSSchedulerWeights()
	require.Equal(t, 1.0, defaultWeights.Load)
	require.Equal(t, 0.7, defaultWeights.Queue)
	require.Equal(t, 0.8, defaultWeights.ErrorRate)
	require.Equal(t, 0.5, defaultWeights.TTFT)

	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 180
	svcWithCfg := &OpenAIGatewayService{cfg: cfg}

	require.Equal(t, 180*time.Second, svcWithCfg.openAIWSSessionStickyTTL())
	customWeights := svcWithCfg.openAIWSSchedulerWeights()
	require.Equal(t, defaultWeights, customWeights)
}

func TestDefaultOpenAIAccountScheduler_IsAccountTransportCompatible_Branches(t *testing.T) {
	scheduler := &defaultOpenAIAccountScheduler{}
	require.True(t, scheduler.isAccountTransportCompatible(context.Background(), nil, OpenAIUpstreamTransportAny))
	require.True(t, scheduler.isAccountTransportCompatible(context.Background(), nil, OpenAIUpstreamTransportHTTPSSE))
	require.False(t, scheduler.isAccountTransportCompatible(context.Background(), nil, OpenAIUpstreamTransportResponsesWebsocketV2))
	require.False(t, scheduler.isAccountTransportCompatible(context.Background(), nil, OpenAIUpstreamTransportResponsesWebsocketV2Ingress))

	cfg := newSchedulerTestOpenAIWSV2Config()
	scheduler.service = &OpenAIGatewayService{cfg: cfg}
	account := &Account{
		ID:          8801,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	require.True(t, scheduler.isAccountTransportCompatible(context.Background(), account, OpenAIUpstreamTransportResponsesWebsocketV2))
	require.True(t, scheduler.isAccountTransportCompatible(context.Background(), account, OpenAIUpstreamTransportResponsesWebsocketV2Ingress))

	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeHTTPBridge
	require.False(t, scheduler.isAccountTransportCompatible(context.Background(), account, OpenAIUpstreamTransportResponsesWebsocketV2))
	require.True(t, scheduler.isAccountTransportCompatible(context.Background(), account, OpenAIUpstreamTransportResponsesWebsocketV2Ingress))

	account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeOff
	require.False(t, scheduler.isAccountTransportCompatible(context.Background(), account, OpenAIUpstreamTransportResponsesWebsocketV2Ingress))
}

func int64PtrForTest(v int64) *int64 {
	return &v
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_StickyWeightedFallbackSkipsOutOfGroupStickyAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(101081)
	otherGroupID := int64(101082)
	accounts := []Account{
		{
			ID:          38001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    10,
			GroupIDs:    []int64{groupID},
		},
		{
			// 会话粘连绑定指向的账号已被移出请求分组（绑定 TTL 内账号改组的场景）。
			ID:          38002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{otherGroupID},
		},
	}
	cfg := &config.Config{}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{
		"openai:session_weighted_out_of_group": 38002,
	}}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{38001: false, 38002: true},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true", "true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_weighted_out_of_group",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	// 组内唯一候选 38001 满并发：必须返回其等待计划，绝不能把请求泄漏到组外的粘连账号 38002。
	require.Equal(t, int64(38001), selection.Account.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(38001), selection.WaitPlan.AccountID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	// 失效的粘连绑定应被清理，避免后续请求反复走同一条泄漏路径。
	require.Positive(t, cache.deletedSessions["openai:session_weighted_out_of_group"])
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SubscriptionPriorityWaitsOnBusySubscriptionWhenRegularUnusable(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(101091)
	accounts := []Account{
		{
			// 订阅账号：支持 compact，但并发已满（busy-but-waitable）。
			ID:          38011,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"plan_type": "team"},
			Extra:       map[string]any{"openai_compact_supported": true},
		},
		{
			// 常规账号：明确不支持 compact，无法服务本次请求。
			ID:          38012,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
			GroupIDs:    []int64{groupID},
			Extra:       map[string]any{"openai_compact_supported": false},
		},
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{38011: false, 38012: true},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                newSchedulerTestSubscriptionPriorityConfig(),
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true", "", "true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_subscription_wait",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		true,
	)
	// 常规池无可用候选时，忙碌的订阅账号应产生等待计划，而不是直接返回 no available accounts。
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(38011), selection.Account.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(38011), selection.WaitPlan.AccountID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LegacyAccountModelRouteExcludesNonOwner(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(1010713)
	owner := Account{
		ID: 371031, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
		GroupIDs:    []int64{groupID},
		Credentials: map[string]any{"model_mapping": map[string]any{"team-alias": "gpt-5.1"}},
		// WS transport must be resolvable for the websocket sub-case so the
		// ownership filter, not transport admission, is the behavior under test.
		Extra: map[string]any{"responses_websockets_v2_enabled": true},
	}
	nonOwner := Account{
		ID: 371032, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
		GroupIDs: []int64{groupID},
		Extra:    map[string]any{"responses_websockets_v2_enabled": true},
	}
	repo := schedulerTestOpenAIAccountRepo{accounts: []Account{owner, nonOwner}}
	ownership, err := (&GatewayService{accountRepo: repo}).resolveCompositeModelOwnership(ctx, groupID, "team-alias")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{TargetPlatform: PlatformOpenAI, Matched: true}, ownership)
	ctx = WithCompositeRouteDecision(ctx, CompositeRouteDecision{
		Matched: true, Source: CompositeRouteSourceAccount, GroupID: groupID,
		PublicModel: "team-alias", TargetPlatform: ownership.TargetPlatform, UpstreamModel: "team-alias",
	})

	for _, tt := range []struct {
		name              string
		requiredTransport OpenAIUpstreamTransport
		sessionHash       string
		sessionBindings   map[string]int64
	}{
		{name: "initial filter excludes non owner", requiredTransport: OpenAIUpstreamTransportAny},
		{name: "initial filter excludes non owner for websocket transport", requiredTransport: OpenAIUpstreamTransportResponsesWebsocket},
		{name: "non owner sticky hit is rejected", requiredTransport: OpenAIUpstreamTransportAny, sessionHash: "non-owner", sessionBindings: map[string]int64{"openai:non-owner": nonOwner.ID}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.Enabled = true
			cfg.Gateway.OpenAIWS.APIKeyEnabled = true
			cfg.Gateway.OpenAIWS.ResponsesWebsockets = true
			svc := &OpenAIGatewayService{
				accountRepo:        repo,
				cache:              &schedulerTestGatewayCache{sessionBindings: tt.sessionBindings},
				cfg:                cfg,
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("false"),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}
			require.NotNil(t, svc.getOpenAIAccountScheduler(ctx), "a single scheduler is always available")

			selection, _, selectErr := svc.SelectAccountWithScheduler(
				ctx, &groupID, "", tt.sessionHash, "team-alias", nil, tt.requiredTransport, false,
			)
			require.NoError(t, selectErr)
			require.NotNil(t, selection)
			require.NotNil(t, selection.Account)
			require.Equal(t, owner.ID, selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LegacyAccountModelRouteDBRecheckFailsOverToOwner(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(1010714)
	mapping := map[string]any{"model_mapping": map[string]any{"team-alias": "gpt-5.1"}}
	stalePrimary := &Account{
		ID: 371041, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
		GroupIDs: []int64{groupID}, Credentials: mapping,
	}
	staleBackup := &Account{
		ID: 371042, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
		GroupIDs: []int64{groupID}, Credentials: mapping,
	}
	dbPrimary := *stalePrimary
	dbPrimary.Credentials = nil
	dbBackup := *staleBackup
	repo := schedulerTestOpenAIAccountRepo{accounts: []Account{dbPrimary, dbBackup}}
	ctx = WithCompositeRouteDecision(ctx, CompositeRouteDecision{
		Matched: true, Source: CompositeRouteSourceAccount, GroupID: groupID,
		PublicModel: "team-alias", TargetPlatform: PlatformOpenAI, UpstreamModel: "team-alias",
	})
	acquiredIDs, releasedIDs := []int64{}, []int64{}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		cfg:              cfg,
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("false"),
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			snapshotAccounts: []*Account{stalePrimary, staleBackup},
			accountsByID:     map[int64]*Account{stalePrimary.ID: stalePrimary, staleBackup.ID: staleBackup},
		}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquiredIDs: &acquiredIDs, releasedIDs: &releasedIDs,
		}),
	}
	require.NotNil(t, svc.getOpenAIAccountScheduler(ctx), "a single scheduler is always available")

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "", "", "team-alias", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, dbBackup.ID, selection.Account.ID)
	if containsInt64(acquiredIDs, dbPrimary.ID) {
		require.Contains(t, releasedIDs, dbPrimary.ID)
	}
	require.Contains(t, acquiredIDs, dbBackup.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIAccountScheduleFailureAttribution(t *testing.T) {
	rejected := validateOpenAIResponsesInput([]byte("{\"input\":[]}"))
	require.Error(t, rejected)
	for _, tc := range []struct {
		name     string
		err      error
		penalize bool
	}{
		{"local input", rejected, false},
		{"closed dispatcher operation", execution.ErrOperationConsumed, false},
		{"BPS request dependency 503", &excelBPSForwardError{code: "basispoints_state_unavailable", status: 503, dependency: true}, false},
		{"BPS upstream cannot assert local origin", &excelBPSForwardError{code: "basispoints_state_unavailable", status: 503}, true},
		{"request transient without scope", &UpstreamFailoverError{StatusCode: 503, RequestScopedTransient: true}, false},
		{"plain upstream 429 keeps scheduler policy", &UpstreamFailoverError{StatusCode: 429}, false},
		{"native authority denial", nativeDispatchError("key scope", ErrNativeDispatchScopeChanged), false},
		{"native credential denial", nativeDispatchError("credential", ErrNativeDispatchGrantUnproven), false},
		{"capacity dependency", routing.ErrCapacityDependencyUnavailable, false},
		{"capacity policy changed", routing.ErrCapacityPolicyChanged, false},
		{"capacity lease lost", routing.ErrCapacityLeaseLost, false},
		{"continuation unavailable", ErrOpenAIContinuationUnavailable, false},
		{"selection dependency", fmt.Errorf("%w: redis unavailable", ErrSelectionDependency), false},
		{"local resource unavailable", ErrNoAvailableAccounts, false},
		{"local compact resource unavailable", ErrNoAvailableCompactAccounts, false},
		{"local group withdrawn", ErrGroupNotAllowed, false},
		{"local recovery probe unavailable", routing.ErrRecoveryProbeUnavailable, false},
		{"cancelled", context.Canceled, false},
		{"upstream deadline with live client", context.DeadlineExceeded, true},
		{"replay unavailable", ErrOpenAIWSReplayUnavailable, false},
		{"request transient", &UpstreamFailoverError{StatusCode: 503, RequestScopedTransient: true, Scope: GatewayFailureScopeRequest}, false},
		{"request parameter", &UpstreamFailoverError{StatusCode: 400, Scope: GatewayFailureScopeRequest}, false},
		{"provider credentials", &UpstreamFailoverError{StatusCode: 503, Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeProvider}, false},
		{"provider inference retains existing contract", &UpstreamFailoverError{StatusCode: 503, Scope: GatewayFailureScopeProvider}, true},
		{"provider body cannot assert local origin", &UpstreamFailoverError{StatusCode: 503, ResponseBody: []byte(`{"error":{"code":"native_dispatch_denied","message":"capacity lease authority unavailable"}}`)}, true},
		{"matching text cannot assert local origin", errors.New(ErrNativeDispatchDenied.Error()), true},
		{"account upstream failure", &UpstreamFailoverError{StatusCode: 502, Scope: GatewayFailureScopeAccount}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := newOpenAIAccountRuntimeStats()
			account := &Account{ID: 21637}
			const model = "gpt-5.1"
			for i := 0; i < openAIHealthMinSamples; i++ {
				stats.report(account.ID, true, nil, model)
			}
			svc := &OpenAIGatewayService{openaiScheduler: &defaultOpenAIAccountScheduler{stats: stats}}
			for i := 0; i < openAIHealthMinSamples; i++ {
				svc.ReportOpenAIAccountScheduleFailure(context.Background(), account, model, fmt.Errorf("forward: %w", tc.err))
			}
			rate, _, _ := stats.snapshot(account.ID, model)
			require.Equal(t, 1, stats.size())
			stat := stats.loadOrCreate(account.ID, model)
			stat.mu.Lock()
			samples := stat.samples
			stat.mu.Unlock()
			if tc.penalize {
				require.Greater(t, rate, 0.5)
				require.EqualValues(t, 2*openAIHealthMinSamples, samples)
			} else {
				require.Zero(t, rate)
				require.EqualValues(t, openAIHealthMinSamples, samples, "unattributable errors must not add samples")
			}
		})
	}
}

// Produce the trusted RequestScoped error through the existing BPS replay
// persistence path; a provider payload with the same code is not that proof.
func schedulerBPSLocalReplayFailure(t *testing.T) (error, func()) {
	t.Helper()
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, excelBPSRepairWire(t, "store", "Run", `{"name":"shell","arguments":{"cmd":"pwd"}}`))}
	svc := newBPSContinuationIntegrationService(upstream, &bpsContinuationIntegrationCache{})
	svc.cache = &bpsLocalReplayFailureCache{bpsContinuationIntegrationCache: &bpsContinuationIntegrationCache{}, err: basispoints.ErrStateUnavailable}
	c, rec := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", nil), newBPSContinuationIntegrationKey())
	c.Request.Header.Set("session_id", "scheduler-local-replay-outage")
	prior := &OpsUpstreamErrorEvent{Kind: "http_error", Platform: PlatformOpenAI, AccountID: excelAccount().ID, UpstreamStatusCode: 502}
	c.Set(OpsUpstreamErrorsKey, []*OpsUpstreamErrorEvent{prior})
	result, err := svc.Forward(t.Context(), c, excelAccount(), []byte(`{"model":"gpt-6-astra","input":"test","tools":[{"type":"function","name":"shell"}]}`))
	require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
	var local *basispoints.UpstreamFailure
	require.ErrorAs(t, err, &local)
	require.True(t, local.RequestScoped())
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Len(t, upstream.requests, 1, "the metered BPS generation cannot be replayed after local persistence fails")
	events, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	require.Contains(t, events.([]*OpsUpstreamErrorEvent), prior)
	before, marshalErr := json.Marshal(events)
	require.NoError(t, marshalErr)
	return err, func() {
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Equal(t, http.StatusServiceUnavailable, local.Status)
		require.True(t, c.GetBool(OpsLocalDependencyFailureKey), "local dependency 503 stays visible to SLA accounting")
		current, _ := c.Get(OpsUpstreamErrorsKey)
		after, marshalErr := json.Marshal(current)
		require.NoError(t, marshalErr)
		require.Equal(t, string(before), string(after), "health attribution must not erase historical provider events or the current local failure")
		require.Len(t, upstream.requests, 1)
	}
}

func TestOpenAIAccountLocalFailureReleasesProbeWithoutProviderPenalty(t *testing.T) {
	for _, entry := range []string{"report", "observe"} {
		for _, tc := range []struct {
			name         string
			cause        error
			penalize     bool
			provider429  bool
			cancelReport bool
			makeFailure  func(*testing.T) (error, func())
		}{
			{name: "native denial inside local 503", cause: &UpstreamFailoverError{StatusCode: 503, Cause: nativeDispatchError("capacity", routing.ErrCapacityDependencyUnavailable)}},
			{name: "withdrawn principal", cause: nativeDispatchError("key scope", ErrNativeDispatchScopeChanged)},
			{name: "capacity policy changed", cause: routing.ErrCapacityPolicyChanged},
			{name: "lease lost", cause: routing.ErrCapacityLeaseLost},
			{name: "continuation unavailable", cause: ErrOpenAIContinuationUnavailable},
			{name: "selection dependency", cause: fmt.Errorf("%w: storage unavailable", ErrSelectionDependency)},
			{name: "request local 503 after provider failure", cause: &UpstreamFailoverError{StatusCode: 503, Scope: GatewayFailureScopeRequest, Cause: &UpstreamFailoverError{StatusCode: 502}}},
			{name: "request transient without scope", cause: &UpstreamFailoverError{StatusCode: 503, RequestScopedTransient: true}},
			{name: "selection policy after provider failure", cause: &UpstreamFailoverError{StatusCode: 503, Stage: "selection", Cause: &UpstreamFailoverError{StatusCode: 502}}},
			{name: "credential preparation policy", cause: &UpstreamFailoverError{StatusCode: 503, Reason: "basispoints_credentials_unavailable"}},
			{name: "provider credentials", cause: &UpstreamFailoverError{StatusCode: 503, Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeProvider}},
			{name: "legacy BPS request dependency", cause: &excelBPSForwardError{code: "basispoints_state_unavailable", status: 503, dependency: true}},
			{name: "actual BPS local replay store", makeFailure: schedulerBPSLocalReplayFailure},
			{name: "operation closed", cause: execution.ErrOperationConsumed},
			{name: "attempt budget", cause: ErrOpenAITurnAttemptBudgetExhausted},
			{name: "preparation budget", cause: ErrTurnPreparationExhausted},
			{name: "wait budget", cause: ErrTurnRetryWaitExhausted},
			{name: "target budget", cause: ErrTurnTargetExhausted},
			{name: "first output budget", cause: ErrOpenAIWSFirstOutputBudgetExhausted},
			{name: "selection budget", cause: selectionbudget.ErrExhausted},
			{name: "explicit cancellation", cause: context.Canceled},
			{name: "request context cancellation after provider failure", cause: &UpstreamFailoverError{StatusCode: 503}, cancelReport: true},
			{name: "instance overload", cause: ErrGatewayInstanceOverloaded},
			{name: "missing cause"},
			{name: "provider 503 with local-looking code", cause: &UpstreamFailoverError{StatusCode: 503, ResponseBody: []byte(`{"error":{"code":"native_dispatch_denied"}}`)}, penalize: true},
			{name: "plain upstream 429 keeps entry policy", cause: &UpstreamFailoverError{StatusCode: 429}, penalize: true, provider429: true},
		} {
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				cause := tc.cause
				verifyFailure := func() {}
				if tc.makeFailure != nil {
					cause, verifyFailure = tc.makeFailure(t)
				}
				var originalStatus int
				if failure, ok := cause.(*UpstreamFailoverError); ok {
					originalStatus = failure.StatusCode
				}
				account := openAIHealthPoolAccount()
				const model = "gpt-5.1"
				scope := modelRecoveryScope(account.ID, model)
				cache := &recoveryWiringCache{scopes: map[string]string{scope: ""}}
				encoded, err := json.Marshal(OpenAIAPIKeyHealthBreakerSettings{Enabled: true, WindowMinutes: 1, FailureThreshold: 3, CooldownMinutes: 5})
				require.NoError(t, err)
				health := &openAIAPIKeyHealthCacheStub{tripped: true}
				accountRepo := &openAIAPIKeyHealthAccountRepo{}
				rateLimits := NewRateLimitService(accountRepo, nil, &config.Config{}, nil, health)
				rateLimits.SetSettingService(NewSettingService(&openAIAPIKeyHealthSettingRepo{value: string(encoded)}, &config.Config{}))
				rateLimits.SetOpenAIAPIKeyHealthCache(health)
				stats := newOpenAIAccountRuntimeStats()
				for range openAIHealthMinSamples {
					stats.report(account.ID, true, nil, model)
				}
				svc := &OpenAIGatewayService{concurrencyService: NewConcurrencyService(cache), rateLimitService: rateLimits,
					openaiScheduler: &defaultOpenAIAccountScheduler{stats: stats}}
				budget := NewOpenAITurnBudget(nil)
				ctx := WithOpenAITurnBudget(t.Context(), budget)
				svc.installSchedulingRecovery(ctx, model)
				probes := budget.recovery.leases
				qualified := []routing.RecoveryScope{{Key: scope, FailureCooldown: 13 * time.Second}}
				require.NoError(t, probes.Acquire(ctx, account.ID, "original-probe-owner", qualified, true))
				t.Cleanup(func() { probes.Finish(account.ID, false, 0) })
				before := budget.attemptBudget.Snapshot()
				reportCtx, cancelReport := context.WithCancel(ctx)
				defer cancelReport()
				if tc.cancelReport {
					cancelReport()
				}
				report := func() bool {
					wrapped := cause
					if cause != nil {
						wrapped = fmt.Errorf("forward: %w", cause)
					}
					if entry == "observe" {
						return svc.ObserveOpenAIAccountHealthFailure(reportCtx, account, wrapped)
					}
					return svc.ReportOpenAIAccountScheduleFailure(reportCtx, account, model, wrapped)
				}

				wantBreaker := tc.penalize && (!tc.provider429 || entry == "observe")
				require.Equal(t, wantBreaker, report())
				verifyFailure()
				require.Equal(t, before, budget.attemptBudget.Snapshot())
				cache.mu.Lock()
				cooldown, finished := cache.cooldowns[scope]
				owner, stillBlocked := cache.scopes[scope]
				cache.mu.Unlock()
				require.True(t, finished, "the actual routing probe must be joined and released, not leaked")
				require.True(t, stillBlocked, "local failure must not mark the provider healthy and clear its existing breaker")
				require.Empty(t, owner)
				if tc.penalize {
					if tc.provider429 {
						require.Zero(t, cooldown)
						stat := stats.loadOrCreate(account.ID, model)
						stat.mu.Lock()
						samples := stat.samples
						stat.mu.Unlock()
						require.EqualValues(t, openAIHealthMinSamples, samples, "upstream 429 keeps its existing scheduler-sample policy")
					} else {
						require.Equal(t, 13*time.Second, cooldown)
					}
					wantWrites := 0
					if wantBreaker {
						wantWrites = 1
					}
					require.Equal(t, wantWrites, health.recordCalls)
					require.Equal(t, wantWrites, accountRepo.setCalls)
					return
				}
				require.Zero(t, cooldown)
				require.Zero(t, health.recordCalls)
				require.Zero(t, accountRepo.setCalls)
				require.Nil(t, account.TempUnschedulableUntil)
				rate, _, _ := stats.snapshot(account.ID, model)
				require.Zero(t, rate)
				stat := stats.loadOrCreate(account.ID, model)
				stat.mu.Lock()
				samples := stat.samples
				stat.mu.Unlock()
				require.EqualValues(t, openAIHealthMinSamples, samples)
				if failure, ok := cause.(*UpstreamFailoverError); ok {
					require.Equal(t, originalStatus, failure.StatusCode, "provider health filtering must not rewrite the local SLA/client status")
				}

				// A repeat notification from the old operation must not clear the
				// next gateway's real claim, even though it uses the same resource.
				next := routing.NewRecovery(t.Context(), cache, nil, nil)
				require.NoError(t, next.Acquire(t.Context(), account.ID, "next-probe-owner", qualified, true))
				t.Cleanup(func() { next.Finish(account.ID, false, 0) })
				require.False(t, report())
				verifyFailure()
				cache.mu.Lock()
				nextOwner := cache.scopes[scope]
				cache.mu.Unlock()
				require.Equal(t, "next-probe-owner", nextOwner)
			})
		}
	}
}

// Replay the previous and target priority outcomes. Production must now match
// the independently filtered best business tier with available capacity.
func TestOpenAIAccountScheduler_PriorityMigrationReplay(t *testing.T) {
	for _, tc := range []struct {
		name            string
		priorities      []int
		loads           []int
		waiting         []int
		current, target int64
	}{
		{"priority_can_be_outweighed", []int{1, 10}, []int{90, 0}, []int{1, 0}, 2, 1},
		{"best_tier_full", []int{1, 10}, []int{100, 0}, []int{1, 0}, 2, 2},
		{"same_tier_load", []int{1, 1}, []int{90, 0}, []int{1, 0}, 2, 2},
		{"single_account", []int{1}, []int{0}, []int{0}, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newSchedulerTestSubscriptionPriorityConfig()
			scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{cfg: cfg}}
			accounts := make([]*Account, 0, len(tc.priorities))
			loads := make(map[int64]*AccountLoadInfo)
			for i, priority := range tc.priorities {
				id := int64(i + 1)
				accounts = append(accounts, &Account{ID: id, Priority: priority, Concurrency: 10})
				loads[id] = &AccountLoadInfo{AccountID: id, LoadRate: tc.loads[i], CurrentConcurrency: tc.loads[i] / 10, WaitingCount: tc.waiting[i]}
			}
			req := OpenAIAccountScheduleRequest{SessionHash: "priority-migration-fixed-seed"}
			current := scheduler.buildOpenAIAccountLoadPlan(context.Background(), req, accounts, loads)
			require.NotEmpty(t, current.selectionOrder)
			require.Equal(t, tc.target, current.selectionOrder[0].account.ID)
			var tier []*Account
			for _, account := range accounts {
				if loads[account.ID].LoadRate >= 100 {
					continue
				}
				if len(tier) == 0 || account.Priority < tier[0].Priority {
					tier = []*Account{account}
				} else if account.Priority == tier[0].Priority {
					tier = append(tier, account)
				}
			}
			target := scheduler.buildOpenAIAccountLoadPlan(context.Background(), req, tier, loads)
			require.NotEmpty(t, target.selectionOrder)
			require.Equal(t, tc.target, target.selectionOrder[0].account.ID)
			t.Logf("current=%d target_strict_tier=%d", tc.current, tc.target)
		})
	}
}

func (c *schedulerTestGatewayCache) GetClaudeConversationState(context.Context, string, []string) (map[string]string, error) {
	return nil, nil
}
func (c *schedulerTestGatewayCache) StoreClaudeConversationState(context.Context, string, map[string]string) error {
	return nil
}

func (s schedulerTestConcurrencyCache) CheckCapacityLease(ctx context.Context, _ routing.CapacityRequest) (time.Duration, error) {
	return time.Hour, ctx.Err()
}
func (s schedulerTestConcurrencyCache) RefreshCapacityLease(ctx context.Context, _ routing.CapacityRequest) (time.Duration, error) {
	return time.Hour, ctx.Err()
}

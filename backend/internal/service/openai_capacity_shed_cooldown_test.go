package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const capacityShedTestMessage = "Our servers are currently overloaded. Please try again later."

func capacityShedHTTPBody() []byte {
	return []byte(`{"error":{"type":"server_error","message":"` + capacityShedTestMessage + `"}}`)
}

func capacityShedStreamPayload() []byte {
	return []byte(`{"type":"error","error":{"type":"server_error","code":"server_is_overloaded","message":"` + capacityShedTestMessage + `"}}`)
}

func (s *openAICapacityShedState) forceCooldownUntil(t *testing.T, accountID int64, until time.Time) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[accountID]
	require.True(t, ok, "account %d has no capacity shed entry", accountID)
	entry.cooldownUntil = until
}

// --- 滑动窗口状态本身 ---

func TestOpenAICapacityShedState_WindowThresholdAndExpiry(t *testing.T) {
	state := newOpenAICapacityShedState(16)
	base := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	first := state.record(7, base)
	second := state.record(7, base.Add(10*time.Second))
	require.Equal(t, 1, first.Hits)
	require.Equal(t, 2, second.Hits)
	require.False(t, second.Cooling, "two hits inside the window stay below the threshold")
	require.False(t, state.isCooling(7, base.Add(11*time.Second)))

	third := state.record(7, base.Add(20*time.Second))
	require.Equal(t, 3, third.Hits)
	require.True(t, third.Tripped)
	require.True(t, third.Cooling)
	require.Equal(t, base.Add(20*time.Second).Add(openAICapacityShedCooldown), third.CooldownUntil)

	// 冷却期内再次命中不延长冷却，也不重复 Tripped。
	fourth := state.record(7, base.Add(30*time.Second))
	require.False(t, fourth.Tripped)
	require.True(t, fourth.Cooling)
	require.Equal(t, third.CooldownUntil, fourth.CooldownUntil)

	require.True(t, state.isCooling(7, third.CooldownUntil.Add(-time.Second)))
	require.False(t, state.isCooling(7, third.CooldownUntil), "cooldown ends exactly at the deadline")
	require.False(t, state.isCooling(7, third.CooldownUntil.Add(time.Minute)))
	require.Zero(t, state.size(), "an expired entry without window hits is reclaimed lazily")
}

func TestOpenAICapacityShedState_HitsOutsideWindowDoNotCount(t *testing.T) {
	state := newOpenAICapacityShedState(16)
	base := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	state.record(9, base)
	state.record(9, base.Add(20*time.Second))
	// 第三次命中时第一次已滑出 60s 窗口：窗口内只有 2 次，不触发冷却。
	decision := state.record(9, base.Add(openAICapacityShedWindow+time.Second))
	require.Equal(t, 2, decision.Hits)
	require.False(t, decision.Cooling)

	// 紧接着再来一次即达到阈值。
	decision = state.record(9, base.Add(openAICapacityShedWindow+2*time.Second))
	require.Equal(t, 3, decision.Hits)
	require.True(t, decision.Tripped)
}

func TestOpenAICapacityShedState_SuccessResetsHitsButKeepsArmedCooldown(t *testing.T) {
	state := newOpenAICapacityShedState(16)
	base := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	state.record(11, base)
	state.record(11, base.Add(time.Second))
	state.resetHits(11, base.Add(2*time.Second))
	require.Zero(t, state.size(), "a reset entry with no cooldown is dropped")

	// 成功之后重新计数：再来两次仍不触发。
	state.record(11, base.Add(3*time.Second))
	decision := state.record(11, base.Add(4*time.Second))
	require.Equal(t, 2, decision.Hits)
	require.False(t, decision.Cooling)

	// 已进入的冷却不因成功而提前解除（成功流来自冷却前的请求）。
	decision = state.record(11, base.Add(5*time.Second))
	require.True(t, decision.Tripped)
	state.resetHits(11, base.Add(6*time.Second))
	require.True(t, state.isCooling(11, base.Add(7*time.Second)))
	require.False(t, state.isCooling(11, decision.CooldownUntil))
}

func TestOpenAICapacityShedState_EvictsWhenFull(t *testing.T) {
	state := newOpenAICapacityShedState(2)
	base := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	state.record(1, base)
	state.record(2, base.Add(time.Second))
	// 容量已满但无过期条目：淘汰最久未触达的账号 1。
	state.record(3, base.Add(2*time.Second))
	require.Equal(t, 2, state.size())
	state.mu.Lock()
	_, hasOne := state.entries[1]
	_, hasTwo := state.entries[2]
	_, hasThree := state.entries[3]
	state.mu.Unlock()
	require.False(t, hasOne)
	require.True(t, hasTwo)
	require.True(t, hasThree)

	// 过期条目优先于最久未触达者被回收。
	state.record(4, base.Add(openAICapacityShedWindow+10*time.Second))
	require.Equal(t, 1, state.size())
	require.Equal(t, 2, state.record(4, base.Add(openAICapacityShedWindow+11*time.Second)).Hits)
}

// --- 服务级：HTTP 识别路径、阈值后直接换号、调度过滤与恢复 ---

func TestOpenAICapacityShedCooldown_HTTPPathTripsAfterThreshold(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 9101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "shed-a"}
	other := &Account{ID: 9102, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	headers := http.Header{"Retry-After": []string{"2"}}

	newErr := func() *UpstreamFailoverError {
		return svc.newOpenAIAccountFailoverError(account, http.StatusServiceUnavailable, headers, capacityShedHTTPBody(), capacityShedTestMessage, false, false)
	}

	for i := 1; i <= openAICapacityShedThreshold-1; i++ {
		failoverErr := newErr()
		require.True(t, failoverErr.RetryableOnSameAccount, "hit %d keeps the same-account backoff", i)
		require.True(t, failoverErr.RequestScopedTransient, "hit %d", i)
		require.Equal(t, 2*time.Second, failoverErr.SameAccountRetryDelay, "hit %d", i)
		require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5"), "hit %d must not cool the account", i)
	}

	tripped := newErr()
	require.False(t, tripped.RetryableOnSameAccount, "the threshold hit switches accounts directly")
	require.Zero(t, tripped.SameAccountRetryDelay)
	require.True(t, tripped.RequestScopedTransient, "request-scoped semantics stay for health/TempUnschedule/client message")
	require.True(t, tripped.IsOpenAICapacityShed())
	require.Equal(t, http.StatusServiceUnavailable, tripped.ClientStatusCode)
	require.Equal(t, capacityShedTestMessage, tripped.ClientMessage)

	require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5"))
	require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.5-other"), "the cooldown is account-level, not model-level")
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(other, "gpt-5"), "other accounts are unaffected")
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "no persisted-style account runtime block is installed")
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5"))

	// 健康评分与临时摘号维持原状：阈值后的 503 仍不计入健康熔断，也不写账号状态。
	_, _, eligible := classifyOpenAIAPIKeyHealthFailure(tripped)
	require.False(t, eligible)
	repo := &capacityShedAccountRepoStub{}
	(&GatewayService{accountRepo: repo}).TempUnscheduleRetryableError(context.Background(), account.ID, tripped)
	require.Zero(t, repo.tempUnschedCalls)

	// 冷却到期自动恢复，并且不会残留阻断。
	svc.getOpenAICapacityShedState().forceCooldownUntil(t, account.ID, time.Now().Add(-time.Millisecond))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5"))
	require.False(t, svc.isOpenAICapacityShedCooling(account))
}

func TestOpenAICapacityShedCooldown_StreamErrorFrameCounts(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 9111, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	streamErr := func() *UpstreamFailoverError {
		return svc.newOpenAIStreamFailoverErrorWithModel(nil, account, true, "rid-ws", capacityShedStreamPayload(), capacityShedTestMessage, "gpt-5")
	}

	// HTTP 与 WS/SSE 流内 error 帧共用同一个窗口：1 次 HTTP + 2 次流内即达到阈值。
	httpErr := svc.newOpenAIAccountFailoverError(account, http.StatusServiceUnavailable, nil, capacityShedHTTPBody(), capacityShedTestMessage, false, false)
	require.True(t, httpErr.RetryableOnSameAccount)

	first := streamErr()
	require.Equal(t, http.StatusServiceUnavailable, first.StatusCode)
	require.True(t, first.RetryableOnSameAccount)
	require.True(t, first.RequestScopedTransient)
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5"))

	second := streamErr()
	require.False(t, second.RetryableOnSameAccount)
	require.True(t, second.RequestScopedTransient)
	require.Equal(t, capacityShedStreamPayload(), second.ResponseBody, "the upstream overload payload is still preserved for the client")
	require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5"))
}

func TestOpenAICapacityShedCooldown_SuccessResetsWindow(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 9121, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	for range openAICapacityShedThreshold - 1 {
		svc.newOpenAIAccountFailoverError(account, http.StatusServiceUnavailable, nil, capacityShedHTTPBody(), capacityShedTestMessage, false, false)
	}
	svc.ReportOpenAIAccountScheduleSuccess(context.Background(), account, "gpt-5", nil)

	for range openAICapacityShedThreshold - 1 {
		failoverErr := svc.newOpenAIAccountFailoverError(account, http.StatusServiceUnavailable, nil, capacityShedHTTPBody(), capacityShedTestMessage, false, false)
		require.True(t, failoverErr.RetryableOnSameAccount, "the window restarted after a success")
	}
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5"))
}

func TestOpenAICapacityShedCooldown_IgnoresOtherPlatformsAndNonShedErrors(t *testing.T) {
	svc := &OpenAIGatewayService{}
	grok := &Account{ID: 9131, Platform: PlatformGrok, Type: AccountTypeAPIKey}
	openai := &Account{ID: 9132, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	for range openAICapacityShedThreshold + 1 {
		svc.newOpenAIAccountFailoverError(grok, http.StatusServiceUnavailable, nil, capacityShedHTTPBody(), capacityShedTestMessage, false, false)
		svc.newOpenAIAccountFailoverError(openai, http.StatusServiceUnavailable, nil, []byte(`{"error":{"message":"temporary upstream failure"}}`), "temporary upstream failure", false, false)
	}
	require.False(t, svc.isOpenAICapacityShedCooling(grok))
	require.False(t, svc.isOpenAICapacityShedCooling(openai))
	require.Zero(t, svc.getOpenAICapacityShedState().size())
}

func TestOpenAIAccountScheduler_SkipsAccountInCapacityShedCooldown(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 9141, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	scheduler := &defaultOpenAIAccountScheduler{service: svc}
	req := OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.5"}

	compatible, reason := scheduler.isAccountRequestCompatibleReason(context.Background(), account, req)
	require.True(t, compatible, "unexpected veto before any capacity shed: %s", reason)
	for range openAICapacityShedThreshold {
		svc.newOpenAIAccountFailoverError(account, http.StatusServiceUnavailable, nil, capacityShedHTTPBody(), capacityShedTestMessage, false, false)
	}

	compatible, reason = scheduler.isAccountRequestCompatibleReason(context.Background(), account, req)
	require.False(t, compatible)
	require.Equal(t, "runtime_blocked", reason)
	require.False(t, scheduler.isAccountRequestCompatible(context.Background(), account, OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.6-sol"}))

	svc.getOpenAICapacityShedState().forceCooldownUntil(t, account.ID, time.Now().Add(-time.Millisecond))
	require.True(t, scheduler.isAccountRequestCompatible(context.Background(), account, req), "the account returns to the candidate pool once the cooldown expires")
}

func TestOpenAIStickySession_CapacityShedCooldownReleasesBinding(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10)
	account := Account{ID: 9151, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
	const sessionHash = "capacity_shed_sticky"
	const cacheKey = "openai:" + sessionHash
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{cacheKey: account.ID}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                &config.Config{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	hit := svc.tryStickySessionHit(ctx, &groupID, PlatformOpenAI, sessionHash, "gpt-5.1", nil, false, 0, "")
	require.NotNil(t, hit)
	require.Equal(t, account.ID, hit.ID)

	for range openAICapacityShedThreshold {
		svc.newOpenAIAccountFailoverError(&account, http.StatusServiceUnavailable, nil, capacityShedHTTPBody(), capacityShedTestMessage, false, false)
	}

	// 粘性路径复查运行时阻断：冷却中的账号不再命中，并删除绑定以便改选其他账号。
	require.Nil(t, svc.tryStickySessionHit(ctx, &groupID, PlatformOpenAI, sessionHash, "gpt-5.1", nil, false, 0, ""))
	require.Equal(t, 1, cache.deletedSessions[cacheKey])
	require.NotContains(t, cache.sessionBindings, cacheKey)
}

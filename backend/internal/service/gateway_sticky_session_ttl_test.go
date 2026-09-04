//go:build unit

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// stickyTTLCacheStub 是一个记录 TTL 的粘性绑定缓存：SetSessionAccountID 建立绑定，
// RefreshSessionTTL 续期，expire 模拟 Redis 键自然过期。
type stickyTTLCacheStub struct {
	GatewayCache

	bindings map[string]int64

	lastSetTTL     time.Duration
	lastRefreshTTL time.Duration
	setCalls       int
	refreshCalls   int
	deleteCalls    int

	// 按键分桶，用于区分短期粘性键与长周期亲和键各自被写/删了几次。
	setTTLByHash   map[string]time.Duration
	setCallsByHash map[string]int
	deletedHashes  []string

	// stickyReads 统计短期粘性键被读了几次，用来钉住「短期一次、历史一次」——
	// 少了这个计数，历史补读重复读一遍短期键这种回归没人会发现。
	stickyReads int

	// 长周期亲和键的独立计数：读了几次（用于验证非 Anthropic 一次都不读）、
	// 尝试写几次、真正写成功几次（被「不覆盖别的账号」挡下的不计入 writes）。
	historyReads         int
	historyWriteAttempts int
	historyWrites        int
	historyTTL           time.Duration
}

func newStickyTTLCacheStub() *stickyTTLCacheStub {
	return &stickyTTLCacheStub{
		bindings:       map[string]int64{},
		setTTLByHash:   map[string]time.Duration{},
		setCallsByHash: map[string]int{},
	}
}

func (c *stickyTTLCacheStub) key(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, sessionHash)
}

func (c *stickyTTLCacheStub) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	c.stickyReads++
	accountID, ok := c.bindings[c.key(groupID, sessionHash)]
	if !ok {
		return 0, ErrStickySessionNotFound
	}
	return accountID, nil
}

func (c *stickyTTLCacheStub) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	c.bindings[c.key(groupID, sessionHash)] = accountID
	c.setTTLByHash[sessionHash] = ttl
	c.setCallsByHash[sessionHash]++
	c.setCalls++
	c.lastSetTTL = ttl
	return nil
}

// ---- 长周期亲和键：独立命名空间，独立计数 ----

func (c *stickyTTLCacheStub) historyKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("history/%d:%s", groupID, sessionHash)
}

func (c *stickyTTLCacheStub) GetSessionAccountHistory(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	c.historyReads++
	accountID, ok := c.bindings[c.historyKey(groupID, sessionHash)]
	if !ok {
		return 0, ErrStickySessionNotFound
	}
	return accountID, nil
}

func (c *stickyTTLCacheStub) SetSessionAccountHistoryIfAbsentOrSame(_ context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) (bool, error) {
	c.historyWriteAttempts++
	key := c.historyKey(groupID, sessionHash)
	if current, ok := c.bindings[key]; ok && current != accountID {
		return false, nil
	}
	c.bindings[key] = accountID
	c.historyTTL = ttl
	c.historyWrites++
	return true, nil
}

func (c *stickyTTLCacheStub) DeleteSessionAccountHistory(_ context.Context, groupID int64, sessionHash string) error {
	delete(c.bindings, c.historyKey(groupID, sessionHash))
	c.deletedHashes = append(c.deletedHashes, "history/"+sessionHash)
	return nil
}

func (c *stickyTTLCacheStub) RefreshSessionTTL(_ context.Context, _ int64, _ string, ttl time.Duration) error {
	c.lastRefreshTTL = ttl
	c.refreshCalls++
	return nil
}

func (c *stickyTTLCacheStub) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	delete(c.bindings, c.key(groupID, sessionHash))
	c.deletedHashes = append(c.deletedHashes, sessionHash)
	c.deleteCalls++
	return nil
}

// expire 模拟 Redis 键到期后消失。
func (c *stickyTTLCacheStub) expire(groupID int64, sessionHash string) {
	delete(c.bindings, c.key(groupID, sessionHash))
}

func stickyTTLAccount(id int64, groupID int64, priority int) Account {
	return Account{
		ID:            id,
		Name:          fmt.Sprintf("claude-cc-%d", id),
		Platform:      PlatformAnthropic,
		Type:          AccountTypeAPIKey,
		Priority:      priority,
		Status:        StatusActive,
		Schedulable:   true,
		AccountGroups: []AccountGroup{{GroupID: groupID}},
	}
}

type stickyTTLFixture struct {
	svc   *GatewayService
	cache *stickyTTLCacheStub
	group *Group
}

func (f *stickyTTLFixture) bind(sessionHash string, accountID int64) {
	f.cache.bindings[f.cache.key(1, sessionHash)] = accountID
}

// bindHistory 只写长周期亲和键，模拟短期粘性键已经过期、历史还在的状态。
func (f *stickyTTLFixture) bindHistory(sessionHash string, accountID int64) {
	f.cache.bindings[f.cache.historyKey(1, sessionHash)] = accountID
}

func (f *stickyTTLFixture) boundAccountID(sessionHash string) int64 {
	return f.cache.bindings[f.cache.key(1, sessionHash)]
}

func (f *stickyTTLFixture) historyAccountID(sessionHash string) int64 {
	return f.cache.bindings[f.cache.historyKey(1, sessionHash)]
}

// withHistoryTTL 打开长周期亲和键。
func (f *stickyTTLFixture) withHistoryTTL(seconds int) *stickyTTLFixture {
	f.svc.cfg.Gateway.Scheduling.SessionAccountHistoryTTLSeconds = seconds
	return f
}

// withModelRouting 给分组打开模型路由，把请求钉在给定账号集合上。
func (f *stickyTTLFixture) withModelRouting(model string, accountIDs ...int64) *stickyTTLFixture {
	f.group.ModelRoutingEnabled = true
	f.group.ModelRouting = map[string][]int64{model: accountIDs}
	return f
}

// newStickyTTLFixture 复刻线上那组账号：同一分组内 cc-5 优先级 1、cc-2 优先级 2，
// 没有粘性绑定时自由选号必然落到 cc-5。
func newStickyTTLFixture(ttlSeconds int, accounts ...Account) *stickyTTLFixture {
	group := noAccountFallbackGroup(1, PlatformAnthropic, nil)
	accountsByID := map[int64]*Account{}
	for i := range accounts {
		acc := accounts[i]
		accountsByID[acc.ID] = &acc
	}
	repo := &noAccountFallbackAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: accountsByID},
		byGroup:                    map[int64][]Account{1: accounts},
	}

	cfg := testConfig()
	cfg.Gateway.Scheduling = config.GatewaySchedulingConfig{StickySessionTTLSeconds: ttlSeconds}

	cache := newStickyTTLCacheStub()
	return &stickyTTLFixture{
		svc: &GatewayService{
			accountRepo: repo,
			groupRepo:   &mockGroupRepoForGateway{groups: map[int64]*Group{1: group}},
			cache:       cache,
			cfg:         cfg,
		},
		cache: cache,
		group: group,
	}
}

func (f *stickyTTLFixture) ctx() context.Context {
	return context.WithValue(context.Background(), ctxkey.Group, f.group)
}

func (f *stickyTTLFixture) selectAccount(t *testing.T, sessionHash string) *Account {
	t.Helper()
	groupID := int64(1)
	selection, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(), &groupID, sessionHash, "claude-opus-5", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	return selection.Account
}

// 复刻生产事故：晚间会话绑在低优先级的 cc-2 上，隔夜粘性键过期后，早晨第一条请求
// 自由选号落到优先级更高的 cc-5，于是整份 prompt cache 在新账号上重建一次。
func TestStickySessionTTL_ExpiryReselectsHigherPriorityAccount(t *testing.T) {
	const session = "ff6e6a62-26a3-4bb4-b5d0-1baa34feff64"

	// cc-5 优先级 1，cc-2 优先级 2。
	f := newStickyTTLFixture(0, stickyTTLAccount(5, 1, 1), stickyTTLAccount(2, 1, 2))

	// 昨晚：绑在 cc-2 上，命中时仍然返回 cc-2。
	f.bind(session, 2)
	require.Equal(t, int64(2), f.selectAccount(t, session).ID)

	// 隔夜：粘性键过期。
	f.cache.expire(1, session)

	// 今早：同一个 session_id，自由选号落到优先级更高的 cc-5。
	require.Equal(t, int64(5), f.selectAccount(t, session).ID)
}

// 绑定还在时，同一个 session_id 必须继续落在原账号上，哪怕存在优先级更高的账号。
func TestStickySessionTTL_HitKeepsBoundAccountWithinTTL(t *testing.T) {
	const session = "session-within-ttl"

	f := newStickyTTLFixture(259200, stickyTTLAccount(5, 1, 1), stickyTTLAccount(2, 1, 2))
	f.bind(session, 2)

	for i := range 3 {
		require.Equalf(t, int64(2), f.selectAccount(t, session).ID, "第 %d 次请求应仍然命中 cc-2", i+1)
	}
}

// 滑动窗口：每次命中都要按配置的 TTL 重新计时，否则活跃会话也会在绑定建立 TTL
// 后到期。Anthropic 走重写而不是 EXPIRE——命中可能来自长周期亲和键，此时短期键
// 并不存在，EXPIRE 会是空操作。
func TestStickySessionTTL_HitRefreshesWithConfiguredTTL(t *testing.T) {
	const session = "session-refresh"

	f := newStickyTTLFixture(259200, stickyTTLAccount(2, 1, 1))
	f.bind(session, 2)

	require.Equal(t, int64(2), f.selectAccount(t, session).ID)
	require.Equal(t, 1, f.cache.setCallsByHash[session])
	require.Equal(t, 72*time.Hour, f.cache.setTTLByHash[session])

	require.Equal(t, int64(2), f.selectAccount(t, session).ID)
	require.Equal(t, 2, f.cache.setCallsByHash[session])
	require.Equal(t, 72*time.Hour, f.cache.setTTLByHash[session])
}

// 未配置时保持历史行为：绑定与续期都用 1 小时。
func TestStickySessionTTL_DefaultsToOneHourWhenUnset(t *testing.T) {
	const session = "session-default-ttl"

	f := newStickyTTLFixture(0, stickyTTLAccount(2, 1, 1))
	f.bind(session, 2)

	require.Equal(t, int64(2), f.selectAccount(t, session).ID)
	require.Equal(t, time.Hour, f.cache.setTTLByHash[session])

	// 无绑定时新建绑定也必须是 1 小时。
	f.cache.expire(1, session)
	require.Equal(t, int64(2), f.selectAccount(t, session).ID)
	require.Equal(t, time.Hour, f.cache.lastSetTTL)
}

// 非 Anthropic 保持原有的纯续期语义：EXPIRE，不重写键，也没有长周期亲和键。
func TestStickySessionTTL_NonAnthropicKeepsRefreshSemantics(t *testing.T) {
	cfg := testConfig()
	cfg.Gateway.Scheduling = config.GatewaySchedulingConfig{
		StickySessionTTLSeconds:         259200,
		SessionAccountHistoryTTLSeconds: 604800,
	}
	cache := newStickyTTLCacheStub()
	svc := &GatewayService{cache: cache, cfg: cfg}
	groupID := int64(1)

	svc.refreshStickySessionOnHit(context.Background(), &groupID, "gemini:session", &Account{ID: 7, Platform: PlatformGemini})

	require.Equal(t, 1, cache.refreshCalls)
	require.Equal(t, time.Hour, cache.lastRefreshTTL)
	require.Equal(t, 0, cache.setCalls, "非 Anthropic 不得改写键，也不得写长周期亲和键")
}

// 延长 TTL 只延长「记忆」，不越过调度闸门：绑定未过期但账号已停调时照常换号。
func TestStickySessionTTL_UnschedulableBoundAccountStillSwitches(t *testing.T) {
	const session = "session-unschedulable"

	unschedulable := stickyTTLAccount(2, 1, 2)
	unschedulable.Schedulable = false
	f := newStickyTTLFixture(259200, stickyTTLAccount(5, 1, 1), unschedulable)

	f.bind(session, 2)

	require.Equal(t, int64(5), f.selectAccount(t, session).ID)
}

// 模型路由命中同样要滑动续期。这条路径原本一次都不续期，只靠 handler 转发成功后
// 的终局绑定——而那次绑定挂在会被客户端断连取消的请求 context 上。
func TestStickySessionTTL_ModelRoutingHitRenewsBinding(t *testing.T) {
	const session = "session-model-routing"

	f := newStickyTTLFixture(259200, stickyTTLAccount(5, 1, 1), stickyTTLAccount(2, 1, 2)).
		withModelRouting("claude-opus-5", 2)
	f.bind(session, 2)

	groupID := int64(1)
	account, err := f.svc.selectAccountForModelWithPlatform(f.ctx(), &groupID, session, "claude-opus-5", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID, "路由集合内的粘性账号应命中")

	require.Equal(t, 1, f.cache.setCallsByHash[session], "路由命中必须续期短期粘性键")
	require.Equal(t, 72*time.Hour, f.cache.setTTLByHash[session])
}

// legacy mixed（原生平台 + antigravity 混合调度）命中路径同样要续期。
func TestStickySessionTTL_MixedSchedulingHitRenewsBinding(t *testing.T) {
	const session = "session-mixed"

	f := newStickyTTLFixture(259200, stickyTTLAccount(5, 1, 1), stickyTTLAccount(2, 1, 2))
	f.bind(session, 2)

	groupID := int64(1)
	account, err := f.svc.selectAccountWithMixedScheduling(f.ctx(), &groupID, session, "claude-opus-5", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID)

	require.Equal(t, 1, f.cache.setCallsByHash[session], "混合调度命中必须续期短期粘性键")
	require.Equal(t, 72*time.Hour, f.cache.setTTLByHash[session])
}

// ---- 第二阶段：长周期「会话账号历史」亲和键 ----

// 默认关闭：不写历史键，也不会为了读它多打一次 Redis。
func TestSessionAccountHistory_DisabledByDefault(t *testing.T) {
	const session = "session-history-off"

	f := newStickyTTLFixture(3600, stickyTTLAccount(2, 1, 1))
	require.Equal(t, int64(2), f.selectAccount(t, session).ID)

	require.Zero(t, f.historyAccountID(session), "未开启时不得写长周期亲和键")
	require.Equal(t, 0, f.cache.historyWrites)

	// 即使历史键此刻存在（例如刚关闭开关），也不该被读回来。
	f.bindHistory(session, 999)
	f.cache.expire(1, session)
	require.Equal(t, int64(2), f.selectAccount(t, session).ID, "关闭后应忽略历史键，走自由选号")
}

// 开启后，绑定会同时写两级键，各用各的 TTL。
func TestSessionAccountHistory_BindWritesBothKeys(t *testing.T) {
	const session = "session-history-write"

	f := newStickyTTLFixture(259200, stickyTTLAccount(2, 1, 1)).withHistoryTTL(604800)
	require.Equal(t, int64(2), f.selectAccount(t, session).ID)

	require.Equal(t, int64(2), f.boundAccountID(session))
	require.Equal(t, int64(2), f.historyAccountID(session))
	require.Equal(t, 72*time.Hour, f.cache.setTTLByHash[session])
	require.Equal(t, 168*time.Hour, f.cache.historyTTL)
}

// 核心收益：短期粘性键过期后，会话仍回到历史账号，而不是按优先级换到 cc-5。
func TestSessionAccountHistory_PromotesAfterStickyExpiry(t *testing.T) {
	const session = "session-history-promote"

	f := newStickyTTLFixture(3600, stickyTTLAccount(5, 1, 1), stickyTTLAccount(2, 1, 2)).
		withHistoryTTL(604800)

	// 昨晚绑到 cc-2，两级键都写上。
	require.Equal(t, int64(2), func() int64 {
		f.bind(session, 2)
		return f.selectAccount(t, session).ID
	}())
	require.Equal(t, int64(2), f.historyAccountID(session))

	// 隔夜：短期键过期，历史键还在。
	f.cache.expire(1, session)
	require.Zero(t, f.boundAccountID(session))

	// 今早：回到 cc-2，而不是优先级更高的 cc-5；短期键被重新写回。
	require.Equal(t, int64(2), f.selectAccount(t, session).ID)
	require.Equal(t, int64(2), f.boundAccountID(session), "历史提升后必须重建短期键，否则每次请求都要多读一次历史")
}

// 历史只是优先建议，不是豁免：历史账号不可调度时照常换号。关键是换号之后历史
// **不能**被备用账号覆盖——否则原账号一恢复，会话再也回不去，这个键就白加了。
func TestSessionAccountHistory_TemporaryBypassDoesNotOverwriteHistory(t *testing.T) {
	const session = "session-history-gated"

	// cc-2 是历史账号，此刻临时停调；cc-5 是备用。
	unschedulable := stickyTTLAccount(2, 1, 2)
	unschedulable.Schedulable = false
	f := newStickyTTLFixture(3600, stickyTTLAccount(5, 1, 1), unschedulable).withHistoryTTL(604800)
	f.bindHistory(session, 2)

	require.Equal(t, int64(5), f.selectAccount(t, session).ID, "历史账号停调时必须换号")
	require.Equal(t, int64(2), f.historyAccountID(session), "临时绕行不得改写历史账号")
	require.Positive(t, f.cache.historyWriteAttempts, "备用账号仍会尝试写，靠 IfAbsentOrSame 挡下")

	// cc-2 恢复后，会话必须回得去。
	f.svc.accountRepo.(*noAccountFallbackAccountRepo).accountsByID[2].Schedulable = true
	f.svc.accountRepo.(*noAccountFallbackAccountRepo).byGroup[1][1].Schedulable = true
	f.cache.expire(1, session)

	require.Equal(t, int64(2), f.selectAccount(t, session).ID, "原账号恢复后必须能回到它")
}

// 非 Anthropic 请求不得读长周期亲和键：这个键只为 Anthropic 写，复合分组里同一个
// session 解析到别的平台时读它既是白打一次 Redis，也多一条没必要的失败路径。
func TestSessionAccountHistory_NotReadForNonAnthropicPlatforms(t *testing.T) {
	const session = "session-history-platform"

	cfg := testConfig()
	cfg.Gateway.Scheduling = config.GatewaySchedulingConfig{
		StickySessionTTLSeconds:         3600,
		SessionAccountHistoryTTLSeconds: 604800,
	}
	cache := newStickyTTLCacheStub()
	svc := &GatewayService{cache: cache, cfg: cfg}
	groupID := int64(1)

	for _, platform := range []string{PlatformGemini, PlatformAntigravity, PlatformOpenAI, ""} {
		accountID, source := svc.resolveStickySessionAccountID(context.Background(), &groupID, session, platform)
		require.Zero(t, accountID, platform)
		require.Empty(t, source, platform)
	}
	require.Zero(t, cache.historyReads, "非 Anthropic 平台一次都不该读历史键")

	// Anthropic 才读。
	_, _ = svc.resolveStickySessionAccountID(context.Background(), &groupID, session, PlatformAnthropic)
	require.Equal(t, 1, cache.historyReads)
}

// 读放大：每次解析最多「短期一次 + 历史一次」。调度入口自己已经读过短期键，补读
// 历史必须走 history-only 的 helper，否则同一个短期键会被读两遍。
func TestSessionAccountHistory_ReadAmplification(t *testing.T) {
	const session = "session-read-amplification"

	newSvc := func() (*GatewayService, *stickyTTLCacheStub) {
		cfg := testConfig()
		cfg.Gateway.Scheduling = config.GatewaySchedulingConfig{
			StickySessionTTLSeconds:         3600,
			SessionAccountHistoryTTLSeconds: 604800,
		}
		cache := newStickyTTLCacheStub()
		return &GatewayService{cache: cache, cfg: cfg}, cache
	}
	groupID := int64(1)

	t.Run("完整 resolver：短期命中就不读历史", func(t *testing.T) {
		svc, cache := newSvc()
		cache.bindings[cache.key(1, session)] = 2

		accountID, source := svc.resolveStickySessionAccountID(context.Background(), &groupID, session, PlatformAnthropic)
		require.Equal(t, int64(2), accountID)
		require.Equal(t, "cache", source)
		require.Equal(t, 1, cache.stickyReads)
		require.Zero(t, cache.historyReads)
	})

	t.Run("完整 resolver：短期 miss 时各读一次", func(t *testing.T) {
		svc, cache := newSvc()
		cache.bindings[cache.historyKey(1, session)] = 2

		accountID, source := svc.resolveStickySessionAccountID(context.Background(), &groupID, session, PlatformAnthropic)
		require.Equal(t, int64(2), accountID)
		require.Equal(t, "history", source)
		require.Equal(t, 1, cache.stickyReads)
		require.Equal(t, 1, cache.historyReads)
	})

	t.Run("history-only helper：一次短期键都不读", func(t *testing.T) {
		svc, cache := newSvc()
		cache.bindings[cache.historyKey(1, session)] = 2

		accountID, source := svc.resolveStickySessionHistoryAccountID(context.Background(), &groupID, session, PlatformAnthropic)
		require.Equal(t, int64(2), accountID)
		require.Equal(t, "history", source)
		require.Zero(t, cache.stickyReads, "调用方已经确认短期键 miss，不该再读一遍")
		require.Equal(t, 1, cache.historyReads)
	})
}

// 历史键必须有自己的 Redis 命名空间。sessionHash 来自客户端（新版 metadata.user_id
// 的 session_id 不做 UUID 校验），一旦历史键实现成 sessionHash 前拼字符串，客户端
// 只要把自己的会话命名成那个前缀开头，就能和别人的历史键撞进同一个 key。
func TestSessionAccountHistory_NamespaceIsolatedFromClientSessionHash(t *testing.T) {
	cfg := testConfig()
	cfg.Gateway.Scheduling = config.GatewaySchedulingConfig{
		StickySessionTTLSeconds:         3600,
		SessionAccountHistoryTTLSeconds: 604800,
	}
	cache := newStickyTTLCacheStub()
	svc := &GatewayService{cache: cache, cfg: cfg}
	groupID := int64(1)

	// 受害者会话 abc 的历史账号是 2。
	require.NoError(t, svc.bindStickySessionWithTTL(context.Background(), &groupID, "abc", 2,
		stickySessionBinding{ttl: time.Hour, historyTTL: 168 * time.Hour}))
	require.Equal(t, int64(2), cache.bindings[cache.historyKey(1, "abc")])

	// 攻击者把自己的会话命名成带前缀的形式，试图撞进受害者的历史键。
	for _, hostile := range []string{"history:abc", "history/abc", "sticky_session_history:1:abc"} {
		require.NoError(t, svc.bindStickySessionWithTTL(context.Background(), &groupID, hostile, 999,
			stickySessionBinding{ttl: time.Hour, historyTTL: 168 * time.Hour}))
	}

	require.Equal(t, int64(2), cache.bindings[cache.historyKey(1, "abc")],
		"客户端可控的 sessionHash 不得污染别的会话的历史键")

	accountID, source := svc.resolveStickySessionAccountID(context.Background(), &groupID, "abc", PlatformAnthropic)
	require.Equal(t, int64(2), accountID)
	require.Equal(t, "cache", source)
}

// 绑定指向的账号已经不在账号池里时，选号照常进行，短期键改写到新账号。
//
// 历史键这里既不会被改写、也不会被删除：调度层拿不到「账号消失」这个信号——可调度
// 快照（listSchedulableAccounts）会过滤临时限流/过载/临时停调，账号不在里面只说明
// 此刻不可调度。而误把临时绕行写进历史的代价（原账号恢复后再也回不去）远大于留一个
// 陈旧条目的代价（每次请求多一次 Redis GET，且 TTL 到期自然消失）。
// 见 TestSessionAccountHistory_NeverDeletedBySchedulingPaths。
func TestSessionAccountHistory_StaleAccountNeverYieldsWrongAccount(t *testing.T) {
	const session = "session-history-account-gone"

	f := newStickyTTLFixture(3600, stickyTTLAccount(5, 1, 1)).withHistoryTTL(604800)

	// 两级键都指向一个账号池里已经不存在的账号。
	f.bind(session, 404)
	f.bindHistory(session, 404)

	require.Equal(t, int64(5), f.selectAccount(t, session).ID, "消失的账号不得被选中")
	require.Equal(t, int64(5), f.boundAccountID(session), "短期键改写到实际选中的账号")
	require.Equal(t, int64(404), f.historyAccountID(session), "历史键保持不动，等 account_cleared 或 TTL 处理")
}

// 调度链路上没有任何一处删除长周期亲和键。
//
// 唯一看起来像「账号消失」的信号是负载感知选号里 accountByID 查不到粘性账号，但
// accountByID 来自 listSchedulableAccounts()，那个列表会过滤临时限流/过载/临时停调
// 等瞬时状态——把它当成「账号已删除」去清历史键，等于给临时绕行开了后门：原账号
// 一限流历史就没了，备用账号顺势接管，Lua 的「不覆盖」保护被整个绕过。
//
// 陈旧条目由 TTL 兜底，这个测试锁住「不删」这个决定，防止有人再把它加回来。
func TestSessionAccountHistory_NeverDeletedBySchedulingPaths(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		require.NoError(t, err)
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// 只找调用点（带接收者的 `.Method(`），跳过 GatewayCache 里的接口声明。
			if strings.Contains(line, ".DeleteSessionAccountHistory(") {
				offenders = append(offenders, fmt.Sprintf("%s:%d", name, i+1))
			}
		}
	}

	require.Emptyf(t, offenders, "调度链路不得删除长周期亲和键（%v）。\n"+
		"「不在可调度快照里」只代表此刻不可调度，不代表账号已删除或永久移出分组；"+
		"误删会让临时限流的原账号被备用账号顶掉且再也回不来。确实需要清理时，请在"+
		"能拿到持久层删除/移组事件的地方做，并连同这条测试一起更新。", offenders)
}

// 历史键与可配置 TTL 同样只覆盖 Anthropic。
func TestSessionAccountHistory_ScopedToAnthropic(t *testing.T) {
	cfg := testConfig()
	cfg.Gateway.Scheduling = config.GatewaySchedulingConfig{
		StickySessionTTLSeconds:         259200,
		SessionAccountHistoryTTLSeconds: 604800,
	}
	svc := &GatewayService{cfg: cfg}

	anthropic := svc.stickyBindingForAccount(&Account{Platform: PlatformAnthropic})
	require.Equal(t, 72*time.Hour, anthropic.ttl)
	require.Equal(t, 168*time.Hour, anthropic.historyTTL)

	for _, platform := range []string{PlatformGemini, PlatformAntigravity} {
		binding := svc.stickyBindingForAccount(&Account{Platform: platform})
		require.Equal(t, time.Hour, binding.ttl, platform)
		require.Zero(t, binding.historyTTL, platform)
	}

	require.Zero(t, svc.stickyBindingForAccount(nil).historyTTL)
}

// TTL 只对 Anthropic 路径可调；GatewayService 同时服务的 Gemini / Antigravity
// 保持历史的 1 小时，本次改动不外溢到别的协议。
func TestStickySessionTTLForPlatform_ScopedToAnthropic(t *testing.T) {
	cfg := testConfig()
	cfg.Gateway.Scheduling = config.GatewaySchedulingConfig{StickySessionTTLSeconds: 259200}
	svc := &GatewayService{cfg: cfg}

	require.Equal(t, 72*time.Hour, svc.stickySessionTTLForPlatform(PlatformAnthropic))
	require.Equal(t, time.Hour, svc.stickySessionTTLForPlatform(PlatformGemini))
	require.Equal(t, time.Hour, svc.stickySessionTTLForPlatform(PlatformAntigravity))

	// 账号缺失或平台读不到时退回兜底 TTL，绝不让绑定意外获得更长寿命。
	require.Equal(t, time.Hour, svc.stickyBindingForAccount(nil).ttl)
	require.Equal(t, 72*time.Hour, svc.stickyBindingForAccount(&Account{Platform: PlatformAnthropic}).ttl)

	// 未配置时退回 1 小时。
	unset := &GatewayService{cfg: testConfig()}
	require.Equal(t, time.Hour, unset.stickySessionTTLForPlatform(PlatformAnthropic))
	require.Equal(t, time.Hour, (*GatewayService)(nil).stickySessionTTLForPlatform(PlatformAnthropic))
}

// 绑定点用选号结果里的账号平台解析 TTL：Anthropic 走配置，其余协议保持 1 小时。
func TestBindSelectionStickySessionUsesPlatformTTL(t *testing.T) {
	cfg := testConfig()
	cfg.Gateway.Scheduling = config.GatewaySchedulingConfig{StickySessionTTLSeconds: 259200}

	cases := []struct {
		name    string
		account *Account
		wantTTL time.Duration
	}{
		{"anthropic 用配置 TTL", &Account{ID: 2, Platform: PlatformAnthropic}, 72 * time.Hour},
		{"gemini 保持 1 小时", &Account{ID: 2, Platform: PlatformGemini}, time.Hour},
		{"选号结果缺账号时退回 1 小时", nil, time.Hour},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(1)

			cache := newStickyTTLCacheStub()
			svc := &GatewayService{cache: cache, cfg: cfg}
			require.NoError(t, svc.BindSelectionStickySession(context.Background(),
				&AccountSelectionResult{Account: tc.account}, &groupID, "session", 2))
			require.Equal(t, tc.wantTTL, cache.lastSetTTL)

			cache = newStickyTTLCacheStub()
			svc = &GatewayService{cache: cache, cfg: cfg}
			require.NoError(t, svc.BindSelectionStickySessionAfterProfitAdmission(context.Background(),
				&AccountSelectionResult{Account: tc.account}, &groupID, "session", 2))
			require.Equal(t, tc.wantTTL, cache.lastSetTTL)
		})
	}

	// 没有选号结果的裸绑定点（Gemini v1beta 摘要复用）保持 1 小时。
	cache := newStickyTTLCacheStub()
	svc := &GatewayService{cache: cache, cfg: cfg}
	groupID := int64(1)
	require.NoError(t, svc.BindStickySession(context.Background(), &groupID, "session", 2))
	require.Equal(t, time.Hour, cache.lastSetTTL)
}

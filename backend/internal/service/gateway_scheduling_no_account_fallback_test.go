//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// noAccountFallbackAccountRepo 按分组分发账号池，并记录选号扫过了哪些分组。
type noAccountFallbackAccountRepo struct {
	*mockAccountRepoForPlatform
	byGroup      map[int64][]Account
	listedGroups []int64
}

func newNoAccountFallbackAccountRepo(byGroup map[int64][]Account) *noAccountFallbackAccountRepo {
	base := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{}}
	for _, accounts := range byGroup {
		for i := range accounts {
			acc := accounts[i]
			base.accountsByID[acc.ID] = &acc
		}
	}
	return &noAccountFallbackAccountRepo{mockAccountRepoForPlatform: base, byGroup: byGroup}
}

func (m *noAccountFallbackAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, groupID int64, _ []string) ([]Account, error) {
	m.listedGroups = append(m.listedGroups, groupID)
	return m.byGroup[groupID], nil
}

func (m *noAccountFallbackAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	return m.ListSchedulableByGroupIDAndPlatforms(ctx, groupID, []string{platform})
}

func noAccountFallbackAnthropicAccount(id int64) Account {
	return Account{ID: id, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true}
}

type noAccountFallbackGatewayFixture struct {
	svc         *GatewayService
	groupRepo   *mockGroupRepoForGateway
	accountRepo *noAccountFallbackAccountRepo
}

// 分组 1 -> 2 -> 3；账号池按 byGroup 给定。
func newNoAccountFallbackGatewayFixture(byGroup map[int64][]Account) *noAccountFallbackGatewayFixture {
	groupRepo := &mockGroupRepoForGateway{groups: noAccountFallbackLinearChain(3, PlatformAnthropic)}
	accountRepo := newNoAccountFallbackAccountRepo(byGroup)
	return &noAccountFallbackGatewayFixture{
		svc:         &GatewayService{accountRepo: accountRepo, groupRepo: groupRepo, cfg: testConfig()},
		groupRepo:   groupRepo,
		accountRepo: accountRepo,
	}
}

func (f *noAccountFallbackGatewayFixture) ctx(originID int64) context.Context {
	return context.WithValue(context.Background(), ctxkey.Group, f.groupRepo.groups[originID])
}

func TestGatewaySelectAccountWithLoadAwareness_NoAccountFallback(t *testing.T) {
	t.Run("borrows the fallback pool when the origin is empty", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{2: {noAccountFallbackAnthropicAccount(20)}})
		origin := int64(1)

		selection, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.Equal(t, int64(20), selection.Account.ID)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, int64(2), *selection.SchedulingGroupID)
		require.Equal(t, []int64{1, 2}, f.accountRepo.listedGroups)
		// 起点命中 ctx 快照，目标读一次；跳内的分组解析走 ctx，不再回源。
		require.Equal(t, 1, f.groupRepo.getByIDLiteCalls)
	})

	t.Run("stamps the origin when it serves the request itself", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{1: {noAccountFallbackAnthropicAccount(10)}, 2: {noAccountFallbackAnthropicAccount(20)}})
		origin := int64(1)

		selection, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(10), selection.Account.ID)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, origin, *selection.SchedulingGroupID)
		require.Equal(t, []int64{1}, f.accountRepo.listedGroups)
	})

	t.Run("walks the whole chain and returns the last hop's error", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{})
		origin := int64(1)

		_, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.Equal(t, []int64{1, 2, 3}, f.accountRepo.listedGroups)
	})

	// 请求内 failover：原组账号失败被排除后池子见底，下一轮选号自动跳到兜底组。
	t.Run("excluded origin accounts drain the pool into the fallback", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{1: {noAccountFallbackAnthropicAccount(10)}, 2: {noAccountFallbackAnthropicAccount(20)}})
		origin := int64(1)

		selection, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", map[int64]struct{}{10: {}}, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(20), selection.Account.ID)
		require.Equal(t, int64(2), *selection.SchedulingGroupID)
	})

	t.Run("claude code downgrade is recorded as the scheduling group", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{1: {noAccountFallbackAnthropicAccount(10)}, 2: {noAccountFallbackAnthropicAccount(20)}})
		origin := int64(1)
		ccFallback := int64(2)
		f.groupRepo.groups[origin].ClaudeCodeOnly = true
		f.groupRepo.groups[origin].FallbackGroupID = &ccFallback

		selection, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(20), selection.Account.ID)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, ccFallback, *selection.SchedulingGroupID)
		require.Equal(t, []int64{2}, f.accountRepo.listedGroups)
	})

	t.Run("non-trigger errors never start the chain", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{2: {noAccountFallbackAnthropicAccount(20)}})
		origin := int64(1)
		f.groupRepo.groups[origin].ClaudeCodeOnly = true

		_, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.ErrorIs(t, err, ErrClaudeCodeOnly)
		require.Empty(t, f.accountRepo.listedGroups)
	})

	// 兜底目标是 claude_code_only（保存后才改的）：那一跳被跳过，链继续，
	// 最终答复仍是原组的「无可用账号」而不是 ErrClaudeCodeOnly。
	t.Run("claude code only hop is skipped", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{2: {noAccountFallbackAnthropicAccount(20)}, 3: {noAccountFallbackAnthropicAccount(30)}})
		origin := int64(1)
		f.groupRepo.groups[2].ClaudeCodeOnly = true

		selection, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(30), selection.Account.ID)
		require.Equal(t, int64(3), *selection.SchedulingGroupID)
		require.Equal(t, []int64{1, 3}, f.accountRepo.listedGroups)

		f.groupRepo.groups[3].ClaudeCodeOnly = true
		f.accountRepo.listedGroups = nil
		_, err = f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.NotErrorIs(t, err, ErrClaudeCodeOnly)
	})

	t.Run("nested calls do not open a second chain", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{2: {noAccountFallbackAnthropicAccount(20)}})
		origin := int64(1)

		_, err := f.svc.SelectAccountWithLoadAwareness(withNoAccountFallbackActive(f.ctx(origin)), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.Equal(t, []int64{1}, f.accountRepo.listedGroups)
	})

	// 分组的渠道定价限制是管理员对本分组的显式模型禁令，不是容量问题：
	// 换到没设限的兜底分组等于绕过禁令，把请求按原分组计费打到被禁模型上。
	t.Run("origin channel model restriction never starts the chain", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{
			1: {noAccountFallbackAnthropicAccount(10)}, 2: {noAccountFallbackAnthropicAccount(20)},
		})
		f.svc.channelService = noAccountFallbackRestrictedChannelService(1)
		origin := int64(1)

		_, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.ErrorIs(t, err, ErrSchedulingPolicyRejected)
		require.Empty(t, f.accountRepo.listedGroups)
	})

	// 目标分组禁用了该模型：这一跳不可用，但不能替换掉「无可用账号」成为最终
	// 答复，链继续往下走。
	t.Run("restricted hop is skipped", func(t *testing.T) {
		f := newNoAccountFallbackGatewayFixture(map[int64][]Account{
			2: {noAccountFallbackAnthropicAccount(20)}, 3: {noAccountFallbackAnthropicAccount(30)},
		})
		f.svc.channelService = noAccountFallbackRestrictedChannelService(2)
		origin := int64(1)

		selection, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(30), selection.Account.ID)
		require.Equal(t, []int64{1, 3}, f.accountRepo.listedGroups)
	})
}

// 需求 D「只借账号，不改计费」与 D13 验收项：兜底换的是调度分组，计费分组必须
// 原地不动。利润门是这条边界上唯一在跳内读分组算钱的地方——门配置取被调度分组，
// 下游倍率 D 取计费分组——所以直接断言它。
func TestGatewayNoAccountFallbackKeepsBillingGroup(t *testing.T) {
	// 账号上游倍率 2：起点分组（下游倍率 3）的门放行，兜底分组（下游倍率 1）
	// 的门会否决——计费分组一旦跟着跳走，这个账号就选不出来。
	borrowed := noAccountFallbackAnthropicAccount(20)
	borrowed.RateMultiplier = floatPtr(2)
	f := newNoAccountFallbackGatewayFixture(map[int64][]Account{2: {borrowed}})
	origin := int64(1)
	f.groupRepo.groups[1].RateMultiplier = 3
	f.groupRepo.groups[1].ProfitControlEnabled = true
	f.groupRepo.groups[2].RateMultiplier = 1
	f.groupRepo.groups[2].ProfitControlEnabled = true

	ctx, _ := WithGatewayTokenRequestPricing(f.ctx(origin))
	selection, err := f.svc.SelectAccountWithLoadAwareness(ctx, &origin, "", "claude-sonnet-4-6", nil, "", 0)
	require.NoError(t, err)
	require.Equal(t, int64(20), selection.Account.ID)
	require.Equal(t, int64(2), *selection.SchedulingGroupID)

	require.NotNil(t, selection.profitGate)
	require.Equal(t, int64(2), selection.profitGate.groupID, "门配置取被调度分组")
	require.InDelta(t, 3.0, selection.profitGate.threshold, 1e-9,
		"下游倍率 D 必须仍按 API Key 自己的分组（倍率 3）算，跳到倍率 1 的兜底分组不影响计费")
	require.Equal(t, origin, gatewayTokenRequestBillingGroupFromContext(ctx).ID)
}

// 跳内的 ctxkey.Group 换成兜底分组是有意的（分组解析零回源），前提是计费分组
// 另有一把 key 钉住；这个断言守住那个前提。
func TestGatewayNoAccountFallbackHopContextKeepsBillingGroup(t *testing.T) {
	f := newNoAccountFallbackGatewayFixture(nil)
	origin := int64(1)

	ctx, _ := WithGatewayTokenRequestPricing(f.ctx(origin))
	hopCtx := f.svc.noAccountFallbackChain(&origin).hopContext(ctx, f.groupRepo.groups[2])

	hopGroup, ok := hopCtx.Value(ctxkey.Group).(*Group)
	require.True(t, ok)
	require.Equal(t, int64(2), hopGroup.ID, "调度分组跟着跳")
	require.Equal(t, origin, gatewayTokenRequestBillingGroupFromContext(hopCtx).ID, "计费分组不跟着跳")
}

// noAccountFallbackRestrictedChannelService 返回一个只覆盖 restrictedGroupID
// 的渠道：该分组开启模型限制且定价表里没有测试用的模型，其余分组无渠道配置。
func noAccountFallbackRestrictedChannelService(restrictedGroupID int64) *ChannelService {
	channel := Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{restrictedGroupID},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing:       []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"claude-opus-4-6"}}},
	}
	return newTestChannelService(makeStandardRepo(channel, map[int64]string{restrictedGroupID: PlatformAnthropic}))
}

func TestGatewaySelectAccountForModelWithExclusions_NoAccountFallback(t *testing.T) {
	f := newNoAccountFallbackGatewayFixture(map[int64][]Account{2: {noAccountFallbackAnthropicAccount(20)}})
	origin := int64(1)

	account, err := f.svc.SelectAccountForModelWithExclusions(f.ctx(origin), &origin, "", "claude-sonnet-4-6", nil)
	require.NoError(t, err)
	require.Equal(t, int64(20), account.ID)
	require.Equal(t, []int64{1, 2}, f.accountRepo.listedGroups)
}

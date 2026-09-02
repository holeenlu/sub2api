//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// noAccountFallbackOpenAIAccountRepo 按分组分发账号池，并记录扫过了哪些分组。
type noAccountFallbackOpenAIAccountRepo struct {
	AccountRepository
	byGroup      map[int64][]Account
	listedGroups []int64
}

func (r *noAccountFallbackOpenAIAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for _, accounts := range r.byGroup {
		for i := range accounts {
			if accounts[i].ID == id {
				return &accounts[i], nil
			}
		}
	}
	return nil, ErrAccountNotFound
}

func (r *noAccountFallbackOpenAIAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, _ string) ([]Account, error) {
	r.listedGroups = append(r.listedGroups, groupID)
	return r.byGroup[groupID], nil
}

func noAccountFallbackOpenAIAccount(id int64) Account {
	return Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
	}
}

// 账号池按分组给定；账号的 GroupIDs 随之补齐，调度的分组归属复检才能通过。
func noAccountFallbackOpenAIPools(byGroup map[int64][]Account) map[int64][]Account {
	for groupID, accounts := range byGroup {
		for i := range accounts {
			accounts[i].GroupIDs = []int64{groupID}
		}
	}
	return byGroup
}

type noAccountFallbackOpenAIFixture struct {
	svc         *OpenAIGatewayService
	groupRepo   *mockGroupRepoForGateway
	accountRepo *noAccountFallbackOpenAIAccountRepo
}

// 分组 1 -> 2 -> 3；账号池按 byGroup 给定。
func newNoAccountFallbackOpenAIFixture(byGroup map[int64][]Account) *noAccountFallbackOpenAIFixture {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	groupRepo := &mockGroupRepoForGateway{groups: noAccountFallbackLinearChain(3, PlatformOpenAI)}
	accountRepo := &noAccountFallbackOpenAIAccountRepo{byGroup: noAccountFallbackOpenAIPools(byGroup)}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	return &noAccountFallbackOpenAIFixture{
		svc: &OpenAIGatewayService{
			accountRepo:        accountRepo,
			cache:              &schedulerTestGatewayCache{},
			cfg:                cfg,
			concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			schedulerSnapshot:  &SchedulerSnapshotService{accountRepo: accountRepo, groupRepo: groupRepo},
		},
		groupRepo:   groupRepo,
		accountRepo: accountRepo,
	}
}

func (f *noAccountFallbackOpenAIFixture) ctx(originID int64) context.Context {
	return context.WithValue(context.Background(), ctxkey.Group, f.groupRepo.groups[originID])
}

// enableAdvancedScheduler 打开高级调度器：previous_response_id 粘连只在这条路径上生效。
func (f *noAccountFallbackOpenAIFixture) enableAdvancedScheduler() {
	f.svc.cfg.Gateway.OpenAIWS.LBTopK = 1
	f.svc.rateLimitService = &RateLimitService{settingService: NewSettingService(
		&openAIAdvancedSchedulerSettingRepoStub{values: map[string]string{openAIAdvancedSchedulerSettingKey: "true"}},
		f.svc.cfg,
	)}
}

// responseStore 返回进程内的响应绑定表（不接 Redis），按分组分命名空间。
func (f *noAccountFallbackOpenAIFixture) responseStore() OpenAIWSStateStore {
	if f.svc.openaiWSStateStore == nil {
		f.svc.openaiWSStateStore = NewOpenAIWSStateStore(nil)
	}
	return f.svc.openaiWSStateStore
}

func TestOpenAISelectAccountWithScheduler_NoAccountFallback(t *testing.T) {
	t.Run("borrows the fallback pool when the origin is empty", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{2: {noAccountFallbackOpenAIAccount(20)}})
		origin := int64(1)

		selection, _, err := f.svc.SelectAccountWithScheduler(f.ctx(origin), &origin, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
		require.NoError(t, err)
		require.NotNil(t, selection.Account)
		require.Equal(t, int64(20), selection.Account.ID)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, int64(2), *selection.SchedulingGroupID)
		require.Equal(t, []int64{1, 2}, f.accountRepo.listedGroups)
	})

	t.Run("stamps the origin when it serves the request itself", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{
			1: {noAccountFallbackOpenAIAccount(10)}, 2: {noAccountFallbackOpenAIAccount(20)},
		})
		origin := int64(1)

		selection, _, err := f.svc.SelectAccountWithScheduler(f.ctx(origin), &origin, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
		require.NoError(t, err)
		require.Equal(t, int64(10), selection.Account.ID)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, origin, *selection.SchedulingGroupID)
		require.Equal(t, []int64{1}, f.accountRepo.listedGroups)
	})

	// previous_response_id 粘连只在高级调度器里生效（legacy 路径根本不读它），下面
	// 三个用例都开高级调度器，并给兜底分组放两个账号：只有真的按绑定命中才能选到
	// 指定的那一个，单账号池会被普通选号碰巧选中，测不出粘连。
	t.Run("previous response keeps fallback affinity after origin recovers", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{
			1: {noAccountFallbackOpenAIAccount(10)},
			2: {noAccountFallbackOpenAIAccount(20), noAccountFallbackOpenAIAccount(21)},
		})
		f.enableAdvancedScheduler()
		origin := int64(1)
		const responseID = "resp_fallback_affinity"
		require.NoError(t, f.responseStore().BindResponseAccount(context.Background(), 2, responseID, 20, time.Hour))

		selection, decision, err := f.svc.SelectAccountWithScheduler(
			f.ctx(origin), &origin, responseID, "", "gpt-5.4", nil,
			OpenAIUpstreamTransportAny, false,
		)

		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, int64(20), selection.Account.ID,
			"the recovered origin account must not steal a continuation owned by the fallback account")
		require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, int64(2), *selection.SchedulingGroupID)
	})

	// 绑定还在、账号却已停调：兜底分组那一跳拿不到绑定的账号，就必须回到原分组正常
	// 起链。若把整条链从兜底分组起跑，这里会借走兜底分组的 21，而原分组的 10 明明
	// 可用；兜底分组池空时更会沿 2 -> 3 走到底报无账号。
	t.Run("stale fallback binding does not pin the conversation to the fallback group", func(t *testing.T) {
		stale := noAccountFallbackOpenAIAccount(20)
		stale.Schedulable = false
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{
			1: {noAccountFallbackOpenAIAccount(10)},
			2: {stale, noAccountFallbackOpenAIAccount(21)},
		})
		f.enableAdvancedScheduler()
		origin := int64(1)
		const responseID = "resp_fallback_stale"
		require.NoError(t, f.responseStore().BindResponseAccount(context.Background(), 2, responseID, 20, time.Hour))

		selection, _, err := f.svc.SelectAccountWithScheduler(
			f.ctx(origin), &origin, responseID, "", "gpt-5.4", nil,
			OpenAIUpstreamTransportAny, false,
		)

		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, int64(10), selection.Account.ID)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, origin, *selection.SchedulingGroupID)
	})

	// legacy 调度器不读 previous_response_id，跨组找绑定那一跳只会白跑：不开高级调度器
	// 时必须直接走常规链，不能先去兜底分组扫一遍账号池。
	t.Run("legacy scheduler skips the cross-group binding hop", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{
			1: {noAccountFallbackOpenAIAccount(10)},
			2: {noAccountFallbackOpenAIAccount(20)},
		})
		origin := int64(1)
		const responseID = "resp_legacy_bound_in_fallback"
		require.NoError(t, f.responseStore().BindResponseAccount(context.Background(), 2, responseID, 20, time.Hour))

		selection, _, err := f.svc.SelectAccountWithScheduler(
			f.ctx(origin), &origin, responseID, "", "gpt-5.4", nil,
			OpenAIUpstreamTransportAny, false,
		)

		require.NoError(t, err)
		require.Equal(t, int64(10), selection.Account.ID)
		require.Equal(t, []int64{1}, f.accountRepo.listedGroups, "不该先扫兜底分组的账号池")
	})

	// 绑定在原分组自己名下时不走特殊路径，常规链照旧按绑定命中。
	t.Run("previous response bound in the origin resolves through the ordinary chain", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{
			1: {noAccountFallbackOpenAIAccount(10), noAccountFallbackOpenAIAccount(11)},
			2: {noAccountFallbackOpenAIAccount(20)},
		})
		f.enableAdvancedScheduler()
		origin := int64(1)
		const responseID = "resp_origin_bound"
		require.NoError(t, f.responseStore().BindResponseAccount(context.Background(), 1, responseID, 11, time.Hour))

		selection, decision, err := f.svc.SelectAccountWithScheduler(
			f.ctx(origin), &origin, responseID, "", "gpt-5.4", nil,
			OpenAIUpstreamTransportAny, false,
		)

		require.NoError(t, err)
		require.Equal(t, int64(11), selection.Account.ID)
		require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
		require.Equal(t, origin, *selection.SchedulingGroupID)
		require.Equal(t, []int64(nil), f.accountRepo.listedGroups, "绑定命中不扫任何账号池")
	})

	t.Run("walks the whole chain before giving up", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{})
		origin := int64(1)

		_, _, err := f.svc.SelectAccountWithScheduler(f.ctx(origin), &origin, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.Equal(t, []int64{1, 2, 3}, f.accountRepo.listedGroups)
	})

	t.Run("excluded origin accounts drain the pool into the fallback", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{
			1: {noAccountFallbackOpenAIAccount(10)}, 2: {noAccountFallbackOpenAIAccount(20)},
		})
		origin := int64(1)

		selection, _, err := f.svc.SelectAccountWithScheduler(f.ctx(origin), &origin, "", "", "gpt-5.4", map[int64]struct{}{10: {}}, OpenAIUpstreamTransportAny, false)
		require.NoError(t, err)
		require.Equal(t, int64(20), selection.Account.ID)
		require.Equal(t, int64(2), *selection.SchedulingGroupID)
	})

	t.Run("nested calls do not open a second chain", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{2: {noAccountFallbackOpenAIAccount(20)}})
		origin := int64(1)

		_, _, err := f.svc.SelectAccountWithScheduler(withNoAccountFallbackActive(f.ctx(origin)), &origin, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.Equal(t, []int64{1}, f.accountRepo.listedGroups)
	})

	// 渠道定价限制是分组的显式模型禁令，兜底链不得绕过它，理由见
	// scheduling_group_fallback.go 的文件头。
	t.Run("origin channel model restriction never starts the chain", func(t *testing.T) {
		f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{
			1: {noAccountFallbackOpenAIAccount(10)}, 2: {noAccountFallbackOpenAIAccount(20)},
		})
		f.svc.channelService = newTestChannelService(makeStandardRepo(Channel{
			ID:                 1,
			Status:             StatusActive,
			GroupIDs:           []int64{1},
			RestrictModels:     true,
			BillingModelSource: BillingModelSourceChannelMapped,
			ModelPricing:       []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-4o"}}},
		}, map[int64]string{1: PlatformOpenAI}))
		origin := int64(1)

		_, _, err := f.svc.SelectAccountWithScheduler(f.ctx(origin), &origin, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.ErrorIs(t, err, ErrSchedulingPolicyRejected)
		require.Empty(t, f.accountRepo.listedGroups)
	})
}

// 需求 D「只借账号，不改计费」与 D13 验收项，OpenAI 侧。这一族的利润门直接把
// ctxkey.Group 当计费分组（resolveOpenAIProfitControlGate），所以兜底链刻意不装
// hopContext：跳内一旦换掉 ctx 分组，借来的请求就会按兜底分组的倍率算利润门。
// 两条断言一起守住这个约定——约定本身与彻底的修法见 scheduling_group_fallback.go。
func TestOpenAINoAccountFallbackKeepsBillingGroup(t *testing.T) {
	borrowed := noAccountFallbackOpenAIAccount(20)
	borrowed.RateMultiplier = floatPtr(2)
	f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{2: {borrowed}})
	origin := int64(1)
	require.Nil(t, f.svc.noAccountFallbackChain(&origin).hopContext,
		"OpenAI 族不得给兜底跳换 ctxkey.Group：利润门拿它当计费分组")

	f.groupRepo.groups[1].RateMultiplier = 3
	f.groupRepo.groups[1].ProfitControlEnabled = true
	f.groupRepo.groups[2].RateMultiplier = 1
	f.groupRepo.groups[2].ProfitControlEnabled = true

	selection, _, err := f.svc.SelectAccountWithScheduler(f.ctx(origin), &origin, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, int64(20), selection.Account.ID)
	require.Equal(t, int64(2), *selection.SchedulingGroupID)

	require.NotNil(t, selection.profitGate)
	require.Equal(t, int64(2), selection.profitGate.groupID, "门配置取被调度分组")
	require.InDelta(t, 3.0, selection.profitGate.threshold, 1e-9,
		"下游倍率 D 必须仍按 API Key 自己的分组（倍率 3）算")
}

func TestOpenAINoAccountFallbackContinuationHonorsOriginModelRestriction(t *testing.T) {
	f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{
		1: {noAccountFallbackOpenAIAccount(10)},
		2: {noAccountFallbackOpenAIAccount(20), noAccountFallbackOpenAIAccount(21)},
	})
	f.enableAdvancedScheduler()
	f.svc.channelService = newTestChannelService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{1},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing:       []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-4o"}}},
	}, map[int64]string{1: PlatformOpenAI}))
	origin := int64(1)
	const responseID = "resp_fallback_before_model_ban"
	require.NoError(t, f.responseStore().BindResponseAccount(context.Background(), 2, responseID, 20, time.Hour))

	selection, _, err := f.svc.SelectAccountWithScheduler(
		f.ctx(origin), &origin, responseID, "", "gpt-5.4", nil,
		OpenAIUpstreamTransportAny, false,
	)
	if selection != nil && selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}
	require.ErrorIs(t, err, ErrSchedulingPolicyRejected)
	require.Nil(t, selection)
	require.Empty(t, f.accountRepo.listedGroups)
}

// Images 请求先在本分组做能力降级（native -> basic），再考虑换分组；非 native
// 请求没有降级可走，从第一趟的结果接着走链，本分组不会被重复扫描。
func TestOpenAISelectAccountWithSchedulerForImages_NoAccountFallback(t *testing.T) {
	f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{2: {noAccountFallbackOpenAIAccount(20)}})
	origin := int64(1)

	selection, _, err := f.svc.SelectAccountWithSchedulerForImages(f.ctx(origin), &origin, "", "gpt-image-1", nil, OpenAIImagesCapabilityBasic)
	require.NoError(t, err)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(20), selection.Account.ID)
	require.NotNil(t, selection.SchedulingGroupID)
	require.Equal(t, int64(2), *selection.SchedulingGroupID)
	require.Equal(t, []int64{1, 2}, f.accountRepo.listedGroups)
}

func TestOpenAISelectAccountWithLoadAwareness_NoAccountFallback(t *testing.T) {
	f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{2: {noAccountFallbackOpenAIAccount(20)}})
	origin := int64(1)

	selection, err := f.svc.SelectAccountWithLoadAwareness(f.ctx(origin), &origin, "", "gpt-5.4", nil)
	require.NoError(t, err)
	require.Equal(t, int64(20), selection.Account.ID)
	require.NotNil(t, selection.SchedulingGroupID)
	require.Equal(t, int64(2), *selection.SchedulingGroupID)
	require.Equal(t, []int64{1, 2}, f.accountRepo.listedGroups)
}

func TestOpenAISelectAccountForModelWithExclusions_NoAccountFallback(t *testing.T) {
	f := newNoAccountFallbackOpenAIFixture(map[int64][]Account{2: {noAccountFallbackOpenAIAccount(20)}})
	origin := int64(1)

	account, err := f.svc.SelectAccountForModelWithExclusions(f.ctx(origin), &origin, "", "gpt-5.4", nil)
	require.NoError(t, err)
	require.Equal(t, int64(20), account.ID)
	require.Equal(t, []int64{1, 2}, f.accountRepo.listedGroups)
}

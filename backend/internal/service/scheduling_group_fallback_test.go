//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

func noAccountFallbackTestLoader(groups map[int64]*Group, calls *int) groupLiteLoader {
	return func(_ context.Context, groupID int64) (*Group, error) {
		if calls != nil {
			*calls++
		}
		group, ok := groups[groupID]
		if !ok {
			return nil, fmt.Errorf("group %d not found", groupID)
		}
		return group, nil
	}
}

func noAccountFallbackGroup(id int64, platform string, fallbackTo *int64) *Group {
	return &Group{
		ID:                         id,
		Name:                       fmt.Sprintf("group-%d", id),
		Platform:                   platform,
		Status:                     StatusActive,
		Hydrated:                   true,
		FallbackGroupIDOnNoAccount: fallbackTo,
	}
}

// noAccountFallbackLinearChain 构造 1 -> 2 -> ... -> n。
func noAccountFallbackLinearChain(n int64, platform string) map[int64]*Group {
	groups := map[int64]*Group{}
	for id := int64(1); id <= n; id++ {
		var next *int64
		if id < n {
			next = int64Ptr(id + 1)
		}
		groups[id] = noAccountFallbackGroup(id, platform, next)
	}
	return groups
}

func TestIsNoAccountFallbackTriggerError(t *testing.T) {
	require.False(t, isNoAccountFallbackTriggerError(nil))
	require.False(t, isNoAccountFallbackTriggerError(errors.New("boom")))
	require.False(t, isNoAccountFallbackTriggerError(context.Canceled))
	require.False(t, isNoAccountFallbackTriggerError(ErrGroupNotFound))
	require.False(t, isNoAccountFallbackTriggerError(ErrClaudeCodeOnly))

	require.True(t, isNoAccountFallbackTriggerError(ErrNoAvailableAccounts))
	require.True(t, isNoAccountFallbackTriggerError(ErrNoAvailableCompactAccounts))
	require.True(t, isNoAccountFallbackTriggerError(
		fmt.Errorf("%w supporting model: %s", ErrNoAvailableAccounts, "claude-opus-4")))
}

func TestNoAccountFallbackActiveSentinel(t *testing.T) {
	ctx := context.Background()
	require.False(t, noAccountFallbackActive(ctx))
	require.True(t, noAccountFallbackActive(withNoAccountFallbackActive(ctx)))
	// 嵌套设置是幂等的，不会翻转回 false。
	require.True(t, noAccountFallbackActive(withNoAccountFallbackActive(withNoAccountFallbackActive(ctx))))
}

func TestNoAccountFallbackChainNext(t *testing.T) {
	t.Run("hop then end of chain", func(t *testing.T) {
		chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(noAccountFallbackLinearChain(2, PlatformAnthropic), nil), nil)
		next := chain.next(context.Background())
		require.NotNil(t, next)
		require.Equal(t, int64(2), next.ID)
		require.Nil(t, chain.next(context.Background()))
	})

	t.Run("not configured", func(t *testing.T) {
		chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(noAccountFallbackLinearChain(1, PlatformAnthropic), nil), nil)
		require.Nil(t, chain.next(context.Background()))
	})

	t.Run("no origin group", func(t *testing.T) {
		chain := newNoAccountFallbackChain(nil, noAccountFallbackTestLoader(noAccountFallbackLinearChain(2, PlatformAnthropic), nil), nil)
		require.Nil(t, chain.next(context.Background()))
	})

	t.Run("inactive target", func(t *testing.T) {
		groups := noAccountFallbackLinearChain(2, PlatformAnthropic)
		groups[2].Status = StatusDisabled
		chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), nil)
		require.Nil(t, chain.next(context.Background()))
	})

	t.Run("cross-platform target", func(t *testing.T) {
		groups := noAccountFallbackLinearChain(2, PlatformAnthropic)
		groups[2].Platform = PlatformOpenAI
		chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), nil)
		// 选号按平台过滤，异平台分组永远选不出账号，直接不跳。
		require.Nil(t, chain.next(context.Background()))
	})

	t.Run("missing target", func(t *testing.T) {
		groups := map[int64]*Group{1: noAccountFallbackGroup(1, PlatformAnthropic, int64Ptr(99))}
		chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), nil)
		require.Nil(t, chain.next(context.Background()))
	})

	t.Run("cycle", func(t *testing.T) {
		groups := map[int64]*Group{
			1: noAccountFallbackGroup(1, PlatformAnthropic, int64Ptr(2)),
			2: noAccountFallbackGroup(2, PlatformAnthropic, int64Ptr(1)),
		}
		chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), nil)
		next := chain.next(context.Background())
		require.NotNil(t, next)
		require.Equal(t, int64(2), next.ID)
		// 2 又指回 1，已访问过，必须停下而不是无限绕圈。
		require.Nil(t, chain.next(context.Background()))
	})

	t.Run("hop limit", func(t *testing.T) {
		chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(noAccountFallbackLinearChain(6, PlatformAnthropic), nil), nil)
		hops := 0
		for chain.next(context.Background()) != nil {
			hops++
		}
		require.Equal(t, MaxNoAccountFallbackHops, hops)
	})
}

// 每跳只回源一次：起点命中 ctx 快照零查询，之后每个目标读一次，作为下一跳的
// 来源时不再重读。
func TestNoAccountFallbackChainLoadsEachGroupOnce(t *testing.T) {
	groups := noAccountFallbackLinearChain(3, PlatformAnthropic)
	ctx := context.WithValue(context.Background(), ctxkey.Group, groups[1])

	calls := 0
	repoLoader := noAccountFallbackTestLoader(groups, &calls)
	loader := func(ctx context.Context, groupID int64) (*Group, error) {
		if ctxGroup, ok := ctx.Value(ctxkey.Group).(*Group); ok && ctxGroup.ID == groupID {
			return ctxGroup, nil
		}
		return repoLoader(ctx, groupID)
	}
	chain := newNoAccountFallbackChain(int64Ptr(1), loader, nil)

	require.Equal(t, int64(2), chain.next(ctx).ID)
	require.Equal(t, 1, calls)
	require.Equal(t, int64(3), chain.next(ctx).ID)
	require.Equal(t, 2, calls)
	require.Nil(t, chain.next(ctx))
	require.Equal(t, 2, calls)
}

type noAccountFallbackAttemptLog struct {
	groups []int64
	ctxs   []context.Context
}

func (l *noAccountFallbackAttemptLog) attempt(results map[int64]error) noAccountFallbackAttempt[string] {
	return func(ctx context.Context, groupID *int64) (string, error) {
		l.groups = append(l.groups, derefGroupID(groupID))
		l.ctxs = append(l.ctxs, ctx)
		if err, ok := results[derefGroupID(groupID)]; ok && err != nil {
			return "", err
		}
		return fmt.Sprintf("account-of-%d", derefGroupID(groupID)), nil
	}
}

func TestRunWithNoAccountFallback(t *testing.T) {
	groups := noAccountFallbackLinearChain(3, PlatformAnthropic)
	newChain := func() *noAccountFallbackChain {
		return newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), nil)
	}

	t.Run("origin succeeds without touching the chain", func(t *testing.T) {
		log := &noAccountFallbackAttemptLog{}
		got, err := runWithNoAccountFallback(context.Background(), newChain(), log.attempt(nil))
		require.NoError(t, err)
		require.Equal(t, "account-of-1", got)
		require.Equal(t, []int64{1}, log.groups)
	})

	t.Run("falls back along the chain until an account is found", func(t *testing.T) {
		log := &noAccountFallbackAttemptLog{}
		got, err := runWithNoAccountFallback(context.Background(), newChain(), log.attempt(map[int64]error{
			1: ErrNoAvailableAccounts,
			2: fmt.Errorf("%w supporting model: x", ErrNoAvailableAccounts),
		}))
		require.NoError(t, err)
		require.Equal(t, "account-of-3", got)
		require.Equal(t, []int64{1, 2, 3}, log.groups)
		// 链内的每一跳都带着标记，嵌套的选号入口不会再各自开一条链。
		for _, ctx := range log.ctxs {
			require.True(t, noAccountFallbackActive(ctx))
		}
	})

	t.Run("returns the last hop's error when the whole chain is exhausted", func(t *testing.T) {
		log := &noAccountFallbackAttemptLog{}
		lastErr := fmt.Errorf("%w supporting model: last", ErrNoAvailableAccounts)
		_, err := runWithNoAccountFallback(context.Background(), newChain(), log.attempt(map[int64]error{
			1: ErrNoAvailableAccounts, 2: ErrNoAvailableAccounts, 3: lastErr,
		}))
		require.ErrorIs(t, err, lastErr)
		require.Equal(t, []int64{1, 2, 3}, log.groups)
	})

	t.Run("non-trigger errors are returned as-is", func(t *testing.T) {
		log := &noAccountFallbackAttemptLog{}
		boom := errors.New("boom")
		_, err := runWithNoAccountFallback(context.Background(), newChain(), log.attempt(map[int64]error{1: boom}))
		require.ErrorIs(t, err, boom)
		require.Equal(t, []int64{1}, log.groups)

		log = &noAccountFallbackAttemptLog{}
		_, err = runWithNoAccountFallback(context.Background(), newChain(), log.attempt(map[int64]error{
			1: ErrNoAvailableAccounts, 2: context.Canceled,
		}))
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, []int64{1, 2}, log.groups)
	})

	t.Run("nested call runs the origin only", func(t *testing.T) {
		log := &noAccountFallbackAttemptLog{}
		_, err := runWithNoAccountFallback(withNoAccountFallbackActive(context.Background()), newChain(), log.attempt(map[int64]error{1: ErrNoAvailableAccounts}))
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.Equal(t, []int64{1}, log.groups)
	})

	// 目标分组是 claude_code_only 而请求不是 Claude Code 客户端：那一跳不可用，
	// 但既不能让 ErrClaudeCodeOnly 成为最终答复（误导：用户并没有请求那个分组），
	// 也不能就此终止，链上后面的分组还要试。
	t.Run("claude_code_only hop is skipped and does not replace the error", func(t *testing.T) {
		log := &noAccountFallbackAttemptLog{}
		got, err := runWithNoAccountFallback(context.Background(), newChain(), log.attempt(map[int64]error{
			1: ErrNoAvailableAccounts, 2: ErrClaudeCodeOnly,
		}))
		require.NoError(t, err)
		require.Equal(t, "account-of-3", got)
		require.Equal(t, []int64{1, 2, 3}, log.groups)

		log = &noAccountFallbackAttemptLog{}
		originErr := fmt.Errorf("%w supporting model: origin", ErrNoAvailableAccounts)
		_, err = runWithNoAccountFallback(context.Background(), newChain(), log.attempt(map[int64]error{
			1: originErr, 2: ErrClaudeCodeOnly, 3: ErrClaudeCodeOnly,
		}))
		require.ErrorIs(t, err, originErr)
		require.NotErrorIs(t, err, ErrClaudeCodeOnly)
	})

	t.Run("hopContext prepares each hop", func(t *testing.T) {
		chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), func(ctx context.Context, target *Group) context.Context {
			return context.WithValue(ctx, ctxkey.Group, target)
		})
		log := &noAccountFallbackAttemptLog{}
		_, err := runWithNoAccountFallback(context.Background(), chain, log.attempt(map[int64]error{1: ErrNoAvailableAccounts}))
		require.NoError(t, err)
		require.Len(t, log.ctxs, 2)
		require.Nil(t, log.ctxs[0].Value(ctxkey.Group))
		require.Same(t, groups[2], log.ctxs[1].Value(ctxkey.Group))
	})
}

// selection 结果的 SchedulingGroupID 由兜底链自己补记：新增一个选号入口时忘了
// 写这一行也不会把借来的账号绑进原分组的粘性命名空间。
func TestRunWithNoAccountFallbackStampsSchedulingGroup(t *testing.T) {
	groups := noAccountFallbackLinearChain(3, PlatformAnthropic)
	newChain := func() *noAccountFallbackChain {
		return newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), nil)
	}
	// attempt 完全不碰 SchedulingGroupID，模拟「新入口忘了赋值」。
	attempt := func(failing map[int64]error) noAccountFallbackAttempt[*AccountSelectionResult] {
		return func(_ context.Context, groupID *int64) (*AccountSelectionResult, error) {
			if err, ok := failing[derefGroupID(groupID)]; ok && err != nil {
				return nil, err
			}
			return &AccountSelectionResult{Account: &Account{ID: derefGroupID(groupID) * 10}}, nil
		}
	}

	t.Run("stamps the hop that actually served the request", func(t *testing.T) {
		selection, err := runWithNoAccountFallback(context.Background(), newChain(), attempt(map[int64]error{
			1: ErrNoAvailableAccounts,
		}))
		require.NoError(t, err)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, int64(2), *selection.SchedulingGroupID)
	})

	t.Run("stamps the origin when it serves the request itself", func(t *testing.T) {
		selection, err := runWithNoAccountFallback(context.Background(), newChain(), attempt(nil))
		require.NoError(t, err)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, int64(1), *selection.SchedulingGroupID)
	})

	t.Run("nested calls stamp the origin too", func(t *testing.T) {
		selection, err := runWithNoAccountFallback(withNoAccountFallbackActive(context.Background()), newChain(), attempt(nil))
		require.NoError(t, err)
		require.NotNil(t, selection.SchedulingGroupID)
		require.Equal(t, int64(1), *selection.SchedulingGroupID)
	})

	// attempt 内部把分组解析成了别的分组（Claude Code 降级）：那才是账号真正的
	// 来源，helper 不能拿入参分组盖掉它。
	t.Run("keeps the group the attempt resolved for itself", func(t *testing.T) {
		downgraded := int64(7)
		selection, err := runWithNoAccountFallback(context.Background(), newChain(),
			func(_ context.Context, _ *int64) (*AccountSelectionResult, error) {
				return &AccountSelectionResult{Account: &Account{ID: 70}, SchedulingGroupID: &downgraded}, nil
			})
		require.NoError(t, err)
		require.Equal(t, downgraded, *selection.SchedulingGroupID)
	})

	t.Run("failures are left unstamped", func(t *testing.T) {
		selection, err := runWithNoAccountFallback(context.Background(), newChain(), attempt(map[int64]error{
			1: ErrNoAvailableAccounts, 2: ErrNoAvailableAccounts, 3: ErrNoAvailableAccounts,
		}))
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.Nil(t, selection)
	})
}

// 起点已在别处扫过（Images 的能力降级先在本分组找 native 账号）时，从它的结果
// 接着走链，不再重复扫描起点。
func TestContinueNoAccountFallbackSkipsOrigin(t *testing.T) {
	groups := noAccountFallbackLinearChain(2, PlatformAnthropic)
	chain := newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), nil)
	log := &noAccountFallbackAttemptLog{}

	got, err := continueNoAccountFallback(withNoAccountFallbackActive(context.Background()), chain, "", ErrNoAvailableAccounts, log.attempt(nil))
	require.NoError(t, err)
	require.Equal(t, "account-of-2", got)
	require.Equal(t, []int64{2}, log.groups)

	// 起点的错误不是触发错误时原样返回，不走链。
	log = &noAccountFallbackAttemptLog{}
	boom := errors.New("boom")
	_, err = continueNoAccountFallback(withNoAccountFallbackActive(context.Background()), newNoAccountFallbackChain(int64Ptr(1), noAccountFallbackTestLoader(groups, nil), nil), "", boom, log.attempt(nil))
	require.ErrorIs(t, err, boom)
	require.Empty(t, log.groups)
}

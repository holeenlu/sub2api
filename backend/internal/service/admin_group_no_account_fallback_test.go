//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func noAccountFallbackAdminGroup(id int64, platform string, next *int64) *Group {
	return &Group{
		ID:                         id,
		Name:                       "g",
		Platform:                   platform,
		Status:                     StatusActive,
		SubscriptionType:           SubscriptionTypeStandard,
		RateMultiplier:             1,
		FallbackGroupIDOnNoAccount: next,
	}
}

func noAccountFallbackCreateInput(fallbackID int64) *CreateGroupInput {
	return &CreateGroupInput{
		Name:                       "openai-primary",
		Platform:                   PlatformOpenAI,
		RateMultiplier:             1,
		FallbackGroupIDOnNoAccount: &fallbackID,
	}
}

func TestAdminService_CreateGroup_AcceptsSamePlatformNoAccountFallback(t *testing.T) {
	target := noAccountFallbackAdminGroup(7, PlatformOpenAI, nil)
	repo := &groupRepoStubForAdmin{createID: 1, getByIDByID: map[int64]*Group{7: target}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), noAccountFallbackCreateInput(7))

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.NotNil(t, repo.created.FallbackGroupIDOnNoAccount)
	require.Equal(t, int64(7), *repo.created.FallbackGroupIDOnNoAccount)
}

func TestAdminService_CreateGroup_TreatsNonPositiveNoAccountFallbackAsUnset(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 1, getByIDByID: map[int64]*Group{}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), noAccountFallbackCreateInput(0))

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.Nil(t, repo.created.FallbackGroupIDOnNoAccount)
}

func TestAdminService_CreateGroup_RejectsCrossPlatformNoAccountFallback(t *testing.T) {
	target := noAccountFallbackAdminGroup(7, PlatformAnthropic, nil)
	repo := &groupRepoStubForAdmin{createID: 1, getByIDByID: map[int64]*Group{7: target}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), noAccountFallbackCreateInput(7))

	require.Error(t, err)
	require.Contains(t, err.Error(), "same platform")
	require.Nil(t, repo.created)
}

func TestAdminService_CreateGroup_RejectsInactiveNoAccountFallback(t *testing.T) {
	target := noAccountFallbackAdminGroup(7, PlatformOpenAI, nil)
	target.Status = StatusDisabled
	repo := &groupRepoStubForAdmin{createID: 1, getByIDByID: map[int64]*Group{7: target}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), noAccountFallbackCreateInput(7))

	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
	require.Nil(t, repo.created)
}

func TestAdminService_CreateGroup_RejectsMissingNoAccountFallback(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 1, getByIDByID: map[int64]*Group{}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), noAccountFallbackCreateInput(7))

	require.Error(t, err)
	require.ErrorIs(t, err, ErrGroupNotFound)
	require.Nil(t, repo.created)
}

// 非 Claude Code 客户端到了 claude_code_only 分组那一跳会被 ErrClaudeCodeOnly
// 挡住，这样的兜底等于没配，保存时就拒绝。
func TestAdminService_CreateGroup_RejectsClaudeCodeOnlyNoAccountFallback(t *testing.T) {
	target := noAccountFallbackAdminGroup(7, PlatformOpenAI, nil)
	target.ClaudeCodeOnly = true
	repo := &groupRepoStubForAdmin{createID: 1, getByIDByID: map[int64]*Group{7: target}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), noAccountFallbackCreateInput(7))

	require.Error(t, err)
	require.Contains(t, err.Error(), "claude_code_only")
	require.Nil(t, repo.created)
}

// 运行时最多走 MaxNoAccountFallbackHops 跳；保存时就按同一上限拒绝，
// 否则链尾的分组配了却永远轮不到。
func TestAdminService_CreateGroup_RejectsNoAccountFallbackChainBeyondHopLimit(t *testing.T) {
	// 新分组 -> 7 -> 8 -> 9 -> 10：四跳，超过上限。
	groups := map[int64]*Group{}
	for id := int64(7); id <= 10; id++ {
		var next *int64
		if id < 10 {
			nextID := id + 1
			next = &nextID
		}
		groups[id] = noAccountFallbackAdminGroup(id, PlatformOpenAI, next)
	}
	repo := &groupRepoStubForAdmin{createID: 1, getByIDByID: groups}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), noAccountFallbackCreateInput(7))
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds 3 hops")
	require.Nil(t, repo.created)

	// 去掉链尾，正好三跳，允许。
	groups[9].FallbackGroupIDOnNoAccount = nil
	_, err = svc.CreateGroup(context.Background(), noAccountFallbackCreateInput(7))
	require.NoError(t, err)
	require.NotNil(t, repo.created)
}

func TestAdminService_UpdateGroup_RejectsSelfNoAccountFallback(t *testing.T) {
	existing := noAccountFallbackAdminGroup(3, PlatformAnthropic, nil)
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{3: existing}}
	svc := &adminServiceImpl{groupRepo: repo}

	self := int64(3)
	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		FallbackGroupIDOnNoAccount: &self,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot set self")
}

func TestAdminService_UpdateGroup_RejectsNoAccountFallbackCycle(t *testing.T) {
	// 3 -> 4 -> 3
	backToThree := int64(3)
	four := noAccountFallbackAdminGroup(4, PlatformAnthropic, &backToThree)
	three := noAccountFallbackAdminGroup(3, PlatformAnthropic, nil)
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{3: three, 4: four}}
	svc := &adminServiceImpl{groupRepo: repo}

	target := int64(4)
	_, err := svc.UpdateGroup(context.Background(), three.ID, &UpdateGroupInput{
		FallbackGroupIDOnNoAccount: &target,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle detected")
}

func TestAdminService_UpdateGroup_ClearsNoAccountFallbackWithZero(t *testing.T) {
	configured := int64(4)
	existing := noAccountFallbackAdminGroup(3, PlatformAnthropic, &configured)
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{3: existing}}
	svc := &adminServiceImpl{groupRepo: repo}

	clear := int64(0)
	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		FallbackGroupIDOnNoAccount: &clear,
	})

	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Nil(t, repo.updated.FallbackGroupIDOnNoAccount)
}

// 目标分组事后被停用不该让本分组的无关更新保存失败：未传字段时沿用旧值且不复验。
func TestAdminService_UpdateGroup_KeepsStaleNoAccountFallbackWhenInputOmitsIt(t *testing.T) {
	configured := int64(4)
	existing := noAccountFallbackAdminGroup(3, PlatformAnthropic, &configured)
	target := noAccountFallbackAdminGroup(4, PlatformAnthropic, nil)
	target.Status = StatusDisabled
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{3: existing, 4: target}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{Name: "renamed"})

	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Equal(t, "renamed", repo.updated.Name)
	require.NotNil(t, repo.updated.FallbackGroupIDOnNoAccount)
	require.Equal(t, configured, *repo.updated.FallbackGroupIDOnNoAccount)
}

// 管理端的编辑弹窗会回填兜底分组并无条件把它序列化进 PUT 载荷，所以「不改这一项」
// 在 HTTP 上表现为「重新提交同一个值」。它必须与不传字段等价，否则目标分组被停用
// 之后，改名字、改价格这些无关编辑全都保存不了。
func TestAdminService_UpdateGroup_KeepsStaleNoAccountFallbackWhenResubmittedUnchanged(t *testing.T) {
	configured := int64(4)
	existing := noAccountFallbackAdminGroup(3, PlatformAnthropic, &configured)
	target := noAccountFallbackAdminGroup(4, PlatformAnthropic, nil)
	target.Status = StatusDisabled
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{3: existing, 4: target}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		Name:                       "renamed",
		FallbackGroupIDOnNoAccount: &configured,
	})

	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Equal(t, "renamed", repo.updated.Name)
	require.NotNil(t, repo.updated.FallbackGroupIDOnNoAccount)
	require.Equal(t, configured, *repo.updated.FallbackGroupIDOnNoAccount)
}

// 分组被软删后 GetByIDLite 直接报 not found，同样不该拖累无关编辑。
func TestAdminService_UpdateGroup_KeepsDeletedNoAccountFallbackWhenResubmittedUnchanged(t *testing.T) {
	configured := int64(4)
	existing := noAccountFallbackAdminGroup(3, PlatformAnthropic, &configured)
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{3: existing}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		FallbackGroupIDOnNoAccount: &configured,
	})

	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.NotNil(t, repo.updated.FallbackGroupIDOnNoAccount)
	require.Equal(t, configured, *repo.updated.FallbackGroupIDOnNoAccount)
}

// 换成另一个目标才是真的「变更」，此时照常要求目标 active。
func TestAdminService_UpdateGroup_ValidatesNoAccountFallbackWhenValueChanges(t *testing.T) {
	configured := int64(4)
	existing := noAccountFallbackAdminGroup(3, PlatformAnthropic, &configured)
	target := noAccountFallbackAdminGroup(5, PlatformAnthropic, nil)
	target.Status = StatusDisabled
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{3: existing, 5: target}}
	svc := &adminServiceImpl{groupRepo: repo}

	changed := int64(5)
	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		FallbackGroupIDOnNoAccount: &changed,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
}

// 沿用旧值但改了本分组的平台：兜底目标从此永远选不出账号，仍要拦下来。
func TestAdminService_UpdateGroup_RevalidatesUnchangedNoAccountFallbackOnPlatformChange(t *testing.T) {
	configured := int64(4)
	existing := noAccountFallbackAdminGroup(3, PlatformAnthropic, &configured)
	target := noAccountFallbackAdminGroup(4, PlatformAnthropic, nil)
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{3: existing, 4: target}}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		Platform:                   PlatformOpenAI,
		FallbackGroupIDOnNoAccount: &configured,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "same platform")
}

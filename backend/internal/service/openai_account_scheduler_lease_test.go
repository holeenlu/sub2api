package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type acquireMutationCache struct {
	schedulerTestConcurrencyCache
	afterAcquire func(int)
	count        int
}

func (c *acquireMutationCache) AcquireAccountSlot(ctx context.Context, id int64, limit int, token string) (bool, error) {
	acquired, err := c.schedulerTestConcurrencyCache.AcquireAccountSlot(ctx, id, limit, token)
	c.count++
	if acquired && c.afterAcquire != nil {
		c.afterAcquire(c.count)
	}
	return acquired, err
}

func TestStickyAcquisitionRevalidatesMovedDisabledAndReducedCapacity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Account)
	}{
		{"group moved", func(a *Account) { a.GroupIDs = []int64{8} }},
		{"account disabled", func(a *Account) { a.Schedulable = false }},
		{"capacity reduced", func(a *Account) { a.Concurrency = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := int64(7)
			account := Account{ID: 7001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{group}}
			repo := &schedulerTestOpenAIAccountRepo{accounts: []Account{account}}
			released := []int64{}
			cache := &acquireMutationCache{schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{releasedIDs: &released}, afterAcquire: func(int) { fresh := repo.accounts[0]; tc.mutate(&fresh); repo.accounts = []Account{fresh} }}
			svc := &OpenAIGatewayService{cfg: &config.Config{RunMode: config.RunModeStandard}, accountRepo: repo, cache: &schedulerTestGatewayCache{},
				schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{accountsByID: map[int64]*Account{account.ID: &account}}}, concurrencyService: NewConcurrencyService(cache)}
			scheduler := &defaultOpenAIAccountScheduler{service: svc}
			selected, _, err := scheduler.selectBySessionHash(t.Context(), OpenAIAccountScheduleRequest{GroupID: &group, Platform: PlatformOpenAI, RequestedModel: "gpt-5.1", SessionHash: "session", StickyAccountID: account.ID})
			require.NoError(t, err)
			require.Nil(t, selected)
			require.Equal(t, []int64{account.ID}, released)
		})
	}
}

func TestCapacityReacquisitionRevalidatesRevokedGroup(t *testing.T) {
	group := int64(7)
	stale := Account{ID: 7001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{group}}
	fresh := stale
	fresh.Concurrency = 2
	repo := &schedulerTestOpenAIAccountRepo{accounts: []Account{fresh}}
	released := []int64{}
	cache := &acquireMutationCache{schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{releasedIDs: &released}, afterAcquire: func(count int) {
		if count == 2 {
			moved := repo.accounts[0]
			moved.GroupIDs = []int64{8}
			repo.accounts = []Account{moved}
		}
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{RunMode: config.RunModeStandard}, accountRepo: repo,
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{accountsByID: map[int64]*Account{stale.ID: &stale}}}, concurrencyService: NewConcurrencyService(cache)}
	scheduler := &defaultOpenAIAccountScheduler{service: svc}
	selected, _, err := scheduler.tryAcquireOpenAISelectionOrder(t.Context(), OpenAIAccountScheduleRequest{GroupID: &group, Platform: PlatformOpenAI, RequestedModel: "gpt-5.1"}, []openAIAccountCandidateScore{{account: &stale, loadInfo: &AccountLoadInfo{}}})
	require.NoError(t, err)
	require.Nil(t, selected)
	require.Equal(t, 2, cache.count)
	require.Equal(t, []int64{stale.ID, stale.ID}, released)
}

//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGatewayLargePoolBoundsAndCancellation(t *testing.T) {
	for _, routed := range []bool{false, true} {
		t.Run(fmt.Sprint(routed), func(t *testing.T) {
			f := newLoadAwareRestrictionFixture(t, false, nil, nil)
			repo := f.svc.accountRepo.(*mockAccountRepoForPlatform)
			repo.accounts = make([]Account, 1000)
			repo.accountsByID = map[int64]*Account{}
			ids := []int64{}
			reject := map[int64]bool{}
			for i := range repo.accounts {
				id := int64(i + 1)
				repo.accounts[i] = Account{ID: id, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}
				repo.accountsByID[id] = &repo.accounts[i]
				reject[id] = false
				ids = append(ids, id)
			}
			if routed {
				group := f.svc.groupRepo.(*mockGroupRepoForGateway).groups[f.groupID]
				group.ModelRoutingEnabled = true
				group.ModelRouting = map[string][]int64{"test-model": ids}
			}
			acquired := []int64{}
			f.svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: reject, acquiredIDs: &acquired})
			result, err := f.svc.SelectAccountWithLoadAwareness(f.ctx, &f.groupID, "", "test-model", nil, "", 0)
			require.NoError(t, err)
			require.NotNil(t, result.WaitPlan)
			require.Len(t, acquired, openAIAccountSelectionProbeLimit)
			ctx, cancel := context.WithCancel(f.ctx)
			cancel()
			before := len(acquired)
			_, err = f.svc.SelectAccountWithLoadAwareness(ctx, &f.groupID, "", "test-model", nil, "", 0)
			require.ErrorIs(t, err, context.Canceled)
			require.Len(t, acquired, before)
		})
	}
}

func TestGatewayOneSortPreservesLayeredOrder(t *testing.T) {
	for _, reset := range []bool{false, true} {
		pool := make([]accountWithLoad, 100)
		now := time.Now()
		for i := range pool {
			used := now.Add(-time.Duration(i+1) * time.Minute)
			end := now.Add(time.Duration(i%5+1) * time.Hour)
			pool[i] = accountWithLoad{account: &Account{ID: int64(i + 1), Priority: i % 3, LastUsedAt: &used, SessionWindowEnd: &end}, loadInfo: &AccountLoadInfo{LoadRate: (i * 7) % 90}}
		}
		ordered := append([]accountWithLoad(nil), pool...)
		orderGatewayLoadCandidates(ordered, false, reset)
		for _, actual := range ordered {
			tier := filterByMinPriority(pool)
			if reset {
				tier = filterBySoonestReset(tier)
			}
			expected := selectByLRU(filterByMinLoadRate(tier), false)
			require.Equal(t, expected.account.ID, actual.account.ID)
			for i := range pool {
				if pool[i].account.ID == actual.account.ID {
					pool = append(pool[:i], pool[i+1:]...)
					break
				}
			}
		}
	}
}

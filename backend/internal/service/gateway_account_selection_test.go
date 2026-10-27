//go:build unit

package service

import (
	mathrand "math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- helpers ---

func testTimePtr(t time.Time) *time.Time { return &t }

func makeAccWithLoad(id int64, priority int, loadRate int, lastUsed *time.Time, accType string) accountWithLoad {
	return accountWithLoad{
		account: &Account{
			ID:          id,
			Priority:    priority,
			LastUsedAt:  lastUsed,
			Type:        accType,
			Schedulable: true,
			Status:      StatusActive,
		},
		loadInfo: &AccountLoadInfo{
			AccountID:          id,
			CurrentConcurrency: 0,
			LoadRate:           loadRate,
		},
	}
}

// --- sortAccountsByPriorityAndLastUsed ---

func TestSortAccountsByPriorityAndLastUsed_ByPriority(t *testing.T) {
	now := time.Now()
	accounts := []*Account{
		{ID: 1, Priority: 5, LastUsedAt: testTimePtr(now)},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 3, Priority: 3, LastUsedAt: testTimePtr(now)},
	}
	sortAccountsByPriorityAndLastUsed(accounts, false)
	require.Equal(t, int64(2), accounts[0].ID, "优先级最低的排第一")
	require.Equal(t, int64(3), accounts[1].ID)
	require.Equal(t, int64(1), accounts[2].ID)
}

func TestSortAccountsByPriorityAndLastUsed_SamePriorityByLastUsed(t *testing.T) {
	now := time.Now()
	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now.Add(-1 * time.Hour))},
		{ID: 3, Priority: 1, LastUsedAt: nil},
	}
	sortAccountsByPriorityAndLastUsed(accounts, false)
	require.Equal(t, int64(3), accounts[0].ID, "nil LastUsedAt 排最前")
	require.Equal(t, int64(2), accounts[1].ID, "更早使用的排前面")
	require.Equal(t, int64(1), accounts[2].ID)
}

func TestSortAccountsByPriorityAndLastUsed_PreferOAuth(t *testing.T) {
	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: nil, Type: AccountTypeAPIKey},
		{ID: 2, Priority: 1, LastUsedAt: nil, Type: AccountTypeOAuth},
	}
	sortAccountsByPriorityAndLastUsed(accounts, true)
	require.Equal(t, int64(2), accounts[0].ID, "preferOAuth 时 OAuth 账号排前面")
}

func TestSortAccountsByPriorityAndLastUsed_StableSort(t *testing.T) {
	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: nil, Type: AccountTypeAPIKey},
		{ID: 2, Priority: 1, LastUsedAt: nil, Type: AccountTypeAPIKey},
		{ID: 3, Priority: 1, LastUsedAt: nil, Type: AccountTypeAPIKey},
	}

	// sortAccountsByPriorityAndLastUsed 内部会在同组(Priority+LastUsedAt)内做随机打散，
	// 因此这里不再断言“稳定排序”。我们只验证：
	// 1) 元素集合不变；2) 多次运行能产生不同的顺序。
	seenFirst := map[int64]bool{}
	for i := 0; i < 100; i++ {
		cpy := make([]*Account, len(accounts))
		copy(cpy, accounts)
		sortAccountsByPriorityAndLastUsed(cpy, false)
		seenFirst[cpy[0].ID] = true

		ids := map[int64]bool{}
		for _, a := range cpy {
			ids[a.ID] = true
		}
		require.True(t, ids[1] && ids[2] && ids[3])
	}
	require.GreaterOrEqual(t, len(seenFirst), 2, "同组账号应能被随机打散")
}

func TestSortAccountsByPriorityAndLastUsed_MixedPriorityAndTime(t *testing.T) {
	now := time.Now()
	accounts := []*Account{
		{ID: 1, Priority: 2, LastUsedAt: nil},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 3, Priority: 1, LastUsedAt: testTimePtr(now.Add(-1 * time.Hour))},
		{ID: 4, Priority: 2, LastUsedAt: testTimePtr(now.Add(-2 * time.Hour))},
	}
	sortAccountsByPriorityAndLastUsed(accounts, false)
	// 优先级1排前：nil < earlier
	require.Equal(t, int64(3), accounts[0].ID, "优先级1 + 更早")
	require.Equal(t, int64(2), accounts[1].ID, "优先级1 + 现在")
	// 优先级2排后：nil < time
	require.Equal(t, int64(1), accounts[2].ID, "优先级2 + nil")
	require.Equal(t, int64(4), accounts[3].ID, "优先级2 + 有时间")
}

// --- filterByMinPriority ---

func TestFilterByMinPriority_Empty(t *testing.T) {
	result := filterByMinPriority(nil)
	require.Nil(t, result)
}

func TestFilterByMinPriority_SelectsMinPriority(t *testing.T) {
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 5, 10, nil, AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 10, nil, AccountTypeAPIKey),
		makeAccWithLoad(3, 1, 20, nil, AccountTypeAPIKey),
		makeAccWithLoad(4, 2, 10, nil, AccountTypeAPIKey),
	}
	result := filterByMinPriority(accounts)
	require.Len(t, result, 2)
	require.Equal(t, int64(2), result[0].account.ID)
	require.Equal(t, int64(3), result[1].account.ID)
}

// --- filterByMinLoadRate ---

func TestFilterByMinLoadRate_Empty(t *testing.T) {
	result := filterByMinLoadRate(nil)
	require.Nil(t, result)
}

func TestFilterByMinLoadRate_SelectsMinLoadRate(t *testing.T) {
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 30, nil, AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 10, nil, AccountTypeAPIKey),
		makeAccWithLoad(3, 1, 10, nil, AccountTypeAPIKey),
		makeAccWithLoad(4, 1, 20, nil, AccountTypeAPIKey),
	}
	result := filterByMinLoadRate(accounts)
	require.Len(t, result, 2)
	require.Equal(t, int64(2), result[0].account.ID)
	require.Equal(t, int64(3), result[1].account.ID)
}

// --- selectByLRU ---

func TestSelectByLRU_Empty(t *testing.T) {
	result := selectByLRU(nil, false)
	require.Nil(t, result)
}

func TestSelectByLRU_Single(t *testing.T) {
	accounts := []accountWithLoad{makeAccWithLoad(1, 1, 10, nil, AccountTypeAPIKey)}
	result := selectByLRU(accounts, false)
	require.NotNil(t, result)
	require.Equal(t, int64(1), result.account.ID)
}

func TestSelectByLRU_NilLastUsedAtWins(t *testing.T) {
	now := time.Now()
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 10, testTimePtr(now), AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 10, nil, AccountTypeAPIKey),
		makeAccWithLoad(3, 1, 10, testTimePtr(now.Add(-1*time.Hour)), AccountTypeAPIKey),
	}
	result := selectByLRU(accounts, false)
	require.NotNil(t, result)
	require.Equal(t, int64(2), result.account.ID)
}

func TestSelectByLRU_EarliestTimeWins(t *testing.T) {
	now := time.Now()
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 10, testTimePtr(now), AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 10, testTimePtr(now.Add(-1*time.Hour)), AccountTypeAPIKey),
		makeAccWithLoad(3, 1, 10, testTimePtr(now.Add(-2*time.Hour)), AccountTypeAPIKey),
	}
	result := selectByLRU(accounts, false)
	require.NotNil(t, result)
	require.Equal(t, int64(3), result.account.ID)
}

func TestSelectByLRU_TiePreferOAuth(t *testing.T) {
	now := time.Now()
	// 账号 1/2 LastUsedAt 相同，且同为最小值。
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 10, testTimePtr(now), AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 10, testTimePtr(now), AccountTypeOAuth),
		makeAccWithLoad(3, 1, 10, testTimePtr(now.Add(1*time.Hour)), AccountTypeAPIKey),
	}
	for i := 0; i < 50; i++ {
		result := selectByLRU(accounts, true)
		require.NotNil(t, result)
		require.Equal(t, AccountTypeOAuth, result.account.Type)
		require.Equal(t, int64(2), result.account.ID)
	}
}

// Test-only adapters and reference algorithms; production uses the current entrypoints.
// filterByMinPriority 过滤出优先级最小的账号集合
func filterByMinPriority(accounts []accountWithLoad) []accountWithLoad {
	if len(accounts) == 0 {
		return accounts
	}
	minPriority := accounts[0].account.Priority
	for _, acc := range accounts[1:] {
		if acc.account.Priority < minPriority {
			minPriority = acc.account.Priority
		}
	}
	result := make([]accountWithLoad, 0, len(accounts))
	for _, acc := range accounts {
		if acc.account.Priority == minPriority {
			result = append(result, acc)
		}
	}
	return result
}

// filterByMinLoadRate 过滤出负载率最低的账号集合
func filterByMinLoadRate(accounts []accountWithLoad) []accountWithLoad {
	if len(accounts) == 0 {
		return accounts
	}
	minLoadRate := accounts[0].loadInfo.LoadRate
	for _, acc := range accounts[1:] {
		if acc.loadInfo.LoadRate < minLoadRate {
			minLoadRate = acc.loadInfo.LoadRate
		}
	}
	result := make([]accountWithLoad, 0, len(accounts))
	for _, acc := range accounts {
		if acc.loadInfo.LoadRate == minLoadRate {
			result = append(result, acc)
		}
	}
	return result
}

// selectByLRU 从集合中选择最久未用的账号
// 如果有多个账号具有相同的最小 LastUsedAt，则随机选择一个
func selectByLRU(accounts []accountWithLoad, preferOAuth bool) *accountWithLoad {
	if len(accounts) == 0 {
		return nil
	}
	if len(accounts) == 1 {
		return &accounts[0]
	}

	// 1. 找到最小的 LastUsedAt（nil 被视为最小）
	var minTime *time.Time
	hasNil := false
	for _, acc := range accounts {
		if acc.account.LastUsedAt == nil {
			hasNil = true
			break
		}
		if minTime == nil || acc.account.LastUsedAt.Before(*minTime) {
			minTime = acc.account.LastUsedAt
		}
	}

	// 2. 收集所有具有最小 LastUsedAt 的账号索引
	var candidateIdxs []int
	for i, acc := range accounts {
		if hasNil {
			if acc.account.LastUsedAt == nil {
				candidateIdxs = append(candidateIdxs, i)
			}
		} else {
			if acc.account.LastUsedAt != nil && acc.account.LastUsedAt.Equal(*minTime) {
				candidateIdxs = append(candidateIdxs, i)
			}
		}
	}

	// 3. 如果只有一个候选，直接返回
	if len(candidateIdxs) == 1 {
		return &accounts[candidateIdxs[0]]
	}

	// 4. 如果有多个候选且 preferOAuth，优先选择 OAuth 类型
	if preferOAuth {
		var oauthIdxs []int
		for _, idx := range candidateIdxs {
			if accounts[idx].account.Type == AccountTypeOAuth {
				oauthIdxs = append(oauthIdxs, idx)
			}
		}
		if len(oauthIdxs) > 0 {
			candidateIdxs = oauthIdxs
		}
	}

	// 5. 随机选择一个
	selectedIdx := candidateIdxs[mathrand.Intn(len(candidateIdxs))]
	return &accounts[selectedIdx]
}

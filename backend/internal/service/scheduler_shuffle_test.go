//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShuffleWithinPriorityAndLastUsed_Empty(t *testing.T) {
	shuffleWithinPriorityAndLastUsed(nil, false)
	shuffleWithinPriorityAndLastUsed([]*Account{}, false)
}

func TestShuffleWithinPriorityAndLastUsed_SingleElement(t *testing.T) {
	accounts := []*Account{{ID: 1, Priority: 1}}
	shuffleWithinPriorityAndLastUsed(accounts, false)
	require.Equal(t, int64(1), accounts[0].ID)
}

func TestShuffleWithinPriorityAndLastUsed_SameGroup_Shuffled(t *testing.T) {
	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: nil},
		{ID: 2, Priority: 1, LastUsedAt: nil},
		{ID: 3, Priority: 1, LastUsedAt: nil},
	}

	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		cpy := make([]*Account, len(accounts))
		copy(cpy, accounts)
		shuffleWithinPriorityAndLastUsed(cpy, false)
		seen[cpy[0].ID] = true
	}
	require.GreaterOrEqual(t, len(seen), 2, "same group should be shuffled")
}

func TestShuffleWithinPriorityAndLastUsed_DifferentPriority_OrderPreserved(t *testing.T) {
	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: nil},
		{ID: 2, Priority: 2, LastUsedAt: nil},
		{ID: 3, Priority: 3, LastUsedAt: nil},
	}

	for i := 0; i < 20; i++ {
		cpy := make([]*Account, len(accounts))
		copy(cpy, accounts)
		shuffleWithinPriorityAndLastUsed(cpy, false)
		require.Equal(t, int64(1), cpy[0].ID)
		require.Equal(t, int64(2), cpy[1].ID)
		require.Equal(t, int64(3), cpy[2].ID)
	}
}

func TestShuffleWithinPriorityAndLastUsed_DifferentLastUsedAt_OrderPreserved(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: nil},
		{ID: 2, Priority: 1, LastUsedAt: &earlier},
		{ID: 3, Priority: 1, LastUsedAt: &now},
	}

	for i := 0; i < 20; i++ {
		cpy := make([]*Account, len(accounts))
		copy(cpy, accounts)
		shuffleWithinPriorityAndLastUsed(cpy, false)
		require.Equal(t, int64(1), cpy[0].ID)
		require.Equal(t, int64(2), cpy[1].ID)
		require.Equal(t, int64(3), cpy[2].ID)
	}
}

// ============ sameLastUsedAt 测试 ============

func TestSameLastUsedAt(t *testing.T) {
	now := time.Now()
	sameSecond := time.Unix(now.Unix(), 0)
	sameSecondDiffNano := time.Unix(now.Unix(), 999_999_999)
	differentSecond := now.Add(1 * time.Second)

	t.Run("both nil", func(t *testing.T) {
		require.True(t, sameLastUsedAt(nil, nil))
	})

	t.Run("one nil one not", func(t *testing.T) {
		require.False(t, sameLastUsedAt(nil, &now))
		require.False(t, sameLastUsedAt(&now, nil))
	})

	t.Run("same second different nanoseconds", func(t *testing.T) {
		require.True(t, sameLastUsedAt(&sameSecond, &sameSecondDiffNano))
	})

	t.Run("different seconds", func(t *testing.T) {
		require.False(t, sameLastUsedAt(&now, &differentSecond))
	})

	t.Run("exact same time", func(t *testing.T) {
		require.True(t, sameLastUsedAt(&now, &now))
	})
}

func TestSameAccountGroup(t *testing.T) {
	now := time.Now()

	t.Run("same group", func(t *testing.T) {
		a := &Account{Priority: 1, LastUsedAt: nil}
		b := &Account{Priority: 1, LastUsedAt: nil}
		require.True(t, sameAccountGroup(a, b))
	})

	t.Run("different priority", func(t *testing.T) {
		a := &Account{Priority: 1, LastUsedAt: nil}
		b := &Account{Priority: 2, LastUsedAt: nil}
		require.False(t, sameAccountGroup(a, b))
	})

	t.Run("different LastUsedAt", func(t *testing.T) {
		later := now.Add(1 * time.Second)
		a := &Account{Priority: 1, LastUsedAt: &now}
		b := &Account{Priority: 1, LastUsedAt: &later}
		require.False(t, sameAccountGroup(a, b))
	})
}

// ============ sortAccountsByPriorityAndLastUsed 集成随机化测试 ============

func TestSortAccountsByPriorityAndLastUsed_WithShuffle(t *testing.T) {
	t.Run("same priority and nil LastUsedAt are shuffled", func(t *testing.T) {
		accounts := []*Account{
			{ID: 1, Priority: 1, LastUsedAt: nil},
			{ID: 2, Priority: 1, LastUsedAt: nil},
			{ID: 3, Priority: 1, LastUsedAt: nil},
		}

		seen := map[int64]bool{}
		for i := 0; i < 100; i++ {
			cpy := make([]*Account, len(accounts))
			copy(cpy, accounts)
			sortAccountsByPriorityAndLastUsed(cpy, false)
			seen[cpy[0].ID] = true
		}
		require.GreaterOrEqual(t, len(seen), 2, "identical sort keys should produce different orderings after shuffle")
	})

	t.Run("different priorities still sorted correctly", func(t *testing.T) {
		now := time.Now()
		accounts := []*Account{
			{ID: 3, Priority: 3, LastUsedAt: &now},
			{ID: 1, Priority: 1, LastUsedAt: &now},
			{ID: 2, Priority: 2, LastUsedAt: &now},
		}

		sortAccountsByPriorityAndLastUsed(accounts, false)
		require.Equal(t, int64(1), accounts[0].ID)
		require.Equal(t, int64(2), accounts[1].ID)
		require.Equal(t, int64(3), accounts[2].ID)
	})
}

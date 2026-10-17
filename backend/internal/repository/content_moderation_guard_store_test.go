package repository

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedModerationGuardStoreTTLBoundsAndClear(t *testing.T) {
	cache, _ := newStickyHistoryTestCache(t)
	first := &contentModerationHashCache{rdb: cache.rdb}
	second := &contentModerationHashCache{rdb: cache.rdb}
	expiry := time.Now().Add(time.Minute).UnixMilli()
	refusal := service.ContentModerationGuardRecord{Kind: "refusal", Key: strings.Repeat("a", 64), ExpiresMS: expiry, Sketch: []uint64{9007199254740993}}
	device := service.ContentModerationGuardRecord{Kind: "device", Key: strings.Repeat("b", 64), ExpiresMS: expiry}
	require.NoError(t, first.StoreGuardEntries(t.Context(), []service.ContentModerationGuardRecord{refusal, device}, 1, 1))
	revision, rows, changed, err := second.LoadGuardState(t.Context(), "")
	require.NoError(t, err)
	require.True(t, changed)
	require.Len(t, rows, 2)
	_, empty, changed, err := second.LoadGuardState(t.Context(), revision)
	require.NoError(t, err)
	require.False(t, changed)
	require.Empty(t, empty)
	device.ExpiresMS += 100000
	require.NoError(t, first.StoreGuardEntries(t.Context(), []service.ContentModerationGuardRecord{device}, 1, 1))
	_, rows, _, err = second.LoadGuardState(t.Context(), "")
	require.NoError(t, err)
	for _, row := range rows {
		if row.Kind == "device" {
			require.Equal(t, expiry, row.ExpiresMS)
		} else {
			require.Equal(t, refusal.Sketch, row.Sketch)
		}
	}
	replacement := refusal
	replacement.Key = strings.Repeat("c", 64)
	replacement.ExpiresMS++
	require.NoError(t, first.StoreGuardEntries(t.Context(), []service.ContentModerationGuardRecord{replacement}, 1, 1))
	_, rows, _, err = second.LoadGuardState(t.Context(), "")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.NotEqual(t, refusal.Key, row.Key)
	}
	deleted, err := first.ClearGuardEntries(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	_, rows, changed, err = second.LoadGuardState(t.Context(), revision)
	require.NoError(t, err)
	require.True(t, changed)
	require.Empty(t, rows)
	expired := refusal
	expired.ExpiresMS = time.Now().Add(-time.Second).UnixMilli()
	require.NoError(t, first.StoreGuardEntries(t.Context(), []service.ContentModerationGuardRecord{expired}, 1, 1))
	_, rows, _, err = second.LoadGuardState(t.Context(), "")
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestTelemetryFingerprintInitializationSelectsOneConcurrentIdentity(t *testing.T) {
	cache, _ := newStickyHistoryTestCache(t)
	identity := &identityCache{rdb: cache.rdb}
	results := make(chan *service.Fingerprint, 16)
	errors := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fp, err := identity.GetOrCreateFingerprint(t.Context(), 42, &service.Fingerprint{ClientID: strings.Repeat(string(rune('a'+i)), 64)})
			results <- fp
			errors <- err
		}(i)
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	chosen := ""
	for fp := range results {
		require.NotNil(t, fp)
		if chosen == "" {
			chosen = fp.ClientID
		}
		require.Equal(t, chosen, fp.ClientID)
	}
}

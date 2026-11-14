package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsExcelBPSEnabledCachesBrieflyAndInvalidatesOnUpdate(t *testing.T) {
	repo := &excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSEnabled: "true"}}
	svc := &SettingService{settingRepo: repo}
	ctx := context.Background()
	require.True(t, svc.IsExcelBPSEnabled(ctx))

	// A store change is not observed until the TTL elapses or the service
	// invalidates after its own update.
	repo.mu.Lock()
	repo.values[SettingKeyExcelBPSEnabled] = "false"
	repo.mu.Unlock()
	require.True(t, svc.IsExcelBPSEnabled(ctx), "cached within the TTL")
	svc.invalidateExcelBPSEnabledCache()
	require.False(t, svc.IsExcelBPSEnabled(ctx), "re-read after invalidation")

	// Transient store errors are not cached as a decision: the previous
	// cached answer expires normally and the next read tries the store again.
	old := excelBPSEnabledCacheTTL
	excelBPSEnabledCacheTTL = 10 * time.Millisecond
	t.Cleanup(func() { excelBPSEnabledCacheTTL = old })
	svc.invalidateExcelBPSEnabledCache()
	repo.mu.Lock()
	repo.values[SettingKeyExcelBPSEnabled] = "true"
	repo.mu.Unlock()
	require.True(t, svc.IsExcelBPSEnabled(ctx))
	time.Sleep(20 * time.Millisecond)
	repo.mu.Lock()
	repo.err = errors.New("store down")
	repo.mu.Unlock()
	require.False(t, svc.IsExcelBPSEnabled(ctx), "fail closed on a store error")
	repo.mu.Lock()
	repo.err = nil
	repo.mu.Unlock()
	require.True(t, svc.IsExcelBPSEnabled(ctx), "the error was not cached")

	var nilService *SettingService
	require.False(t, nilService.IsExcelBPSEnabled(ctx))
	nilService.invalidateExcelBPSEnabledCache()
}

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func stopHandlerBillingCache(t testing.TB, cache *service.BillingCacheService) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, cache.Stop(ctx))
}

func cleanupHandlerBillingCache(t testing.TB, cache *service.BillingCacheService) {
	t.Helper()
	t.Cleanup(func() { stopHandlerBillingCache(t, cache) })
}

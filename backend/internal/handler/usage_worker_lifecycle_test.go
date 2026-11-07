package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/host"
)

func stopHandlerUsageWorker(t *testing.T, worker *host.UsageWorker) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, worker.Stop(ctx), "all accepted usage work must be joined")
}

func startHandlerUsageWorker(t *testing.T, worker *host.UsageWorker) {
	t.Helper()
	require.NoError(t, worker.Start(t.Context()))
	t.Cleanup(func() { stopHandlerUsageWorker(t, worker) })
}

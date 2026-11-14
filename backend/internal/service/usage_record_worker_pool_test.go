package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/stretchr/testify/require"
)

func TestUsageRecordWorkerPool_OptionsFromConfig_AutoScaleDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.UsageRecord.WorkerCount = 64
	cfg.Gateway.UsageRecord.QueueSize = 128
	cfg.Gateway.UsageRecord.TaskTimeoutSeconds = 7
	cfg.Gateway.UsageRecord.OverflowPolicy = config.UsageRecordOverflowPolicyDrop
	cfg.Gateway.UsageRecord.OverflowSamplePercent = 0
	cfg.Gateway.UsageRecord.AutoScaleEnabled = false
	cfg.Gateway.UsageRecord.AutoScaleMinWorkers = 1
	cfg.Gateway.UsageRecord.AutoScaleMaxWorkers = 512

	opts := usageRecordPoolOptionsFromConfig(cfg)
	require.False(t, opts.AutoScaleEnabled)
	require.Equal(t, 64, opts.WorkerCount)
	require.Equal(t, 64, opts.AutoScaleMinWorkers)
	require.Equal(t, 64, opts.AutoScaleMaxWorkers)
	require.Equal(t, 7*time.Second, opts.TaskTimeout)
}

func TestProvideUsageRecordWorkerPool_FromConfig(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		workers, databaseLimit int
		wantConcurrency        int
	}{
		{name: "configured_workers", workers: 3, wantConcurrency: 3},
		{name: "database_execution_budget", workers: 8, databaseLimit: 6, wantConcurrency: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.UsageRecord.WorkerCount = tc.workers
			cfg.Gateway.UsageRecord.QueueSize = 16
			cfg.Gateway.UsageRecord.TaskTimeoutSeconds = 2
			cfg.Gateway.UsageRecord.OverflowPolicy = config.UsageRecordOverflowPolicyDrop
			cfg.Gateway.UsageRecord.AutoScaleEnabled = false
			cfg.Database.MaxOpenConns = tc.databaseLimit

			pool := ProvideUsageRecordWorkerPool(cfg, nil, nil, nil, nil, nil, nil, nil)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, pool.Stop(ctx))
			})
			require.NoError(t, pool.Start(t.Context()))
			stats := pool.Stats()
			require.Equal(t, tc.wantConcurrency, stats.MaxConcurrency)
			require.Equal(t, tc.wantConcurrency, stats.ExecutionLimit)

			// Use the production monetary-consumer entry point, with the configured
			// timeout. Queue scheduling policies are tested by host, not copied here.
			var deadline time.Time
			var hasDeadline bool
			var remaining time.Duration
			pool.Run(func(ctx context.Context) {
				deadline, hasDeadline = ctx.Deadline()
				remaining = time.Until(deadline)
			})
			require.True(t, hasDeadline)
			require.Positive(t, remaining)
			require.LessOrEqual(t, remaining, 2*time.Second)
			require.EqualValues(t, 1, pool.Stats().InlineCompleted)

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			require.NoError(t, pool.Drain(ctx))
			require.NoError(t, pool.Stop(ctx))
		})
	}
}

func TestUsageRecordWorkerPool_OptionsFromConfig_NilConfig(t *testing.T) {
	opts := usageRecordPoolOptionsFromConfig(nil)
	require.Equal(t, host.DefaultUsageWorkerOptions().WorkerCount, opts.WorkerCount)
	require.Equal(t, host.DefaultUsageWorkerOptions().QueueSize, opts.QueueSize)
	require.Equal(t, host.DefaultUsageWorkerOptions().TaskTimeout, opts.TaskTimeout)
	require.Equal(t, host.DefaultUsageWorkerOptions().OverflowPolicy, opts.OverflowPolicy)
	require.Equal(t, host.DefaultUsageWorkerOptions().OverflowSamplePercent, opts.OverflowSamplePercent)
	require.True(t, opts.AutoScaleEnabled)
	require.Equal(t, host.DefaultUsageWorkerOptions().AutoScaleMinWorkers, opts.AutoScaleMinWorkers)
	require.Equal(t, host.DefaultUsageWorkerOptions().AutoScaleMaxWorkers, opts.AutoScaleMaxWorkers)
}

//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func testNodeCollector(t *testing.T, server *miniredis.Miniredis, id string, maxConns, redisPool int) *OpsMetricsCollector {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	db.SetMaxOpenConns(maxConns)
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: redisPool, MaxRetries: -1, DialTimeout: 20 * time.Millisecond})
	t.Cleanup(func() { client.Close(); db.Close() })
	cfg := &config.Config{}
	cfg.Ops.NodeID = id
	cfg.Ops.NodeName = "Node " + id
	cfg.Database.MaxOpenConns = maxConns + 100
	c := NewOpsMetricsCollector(nil, nil, nil, nil, db, client, cfg)
	require.NoError(t, c.tasks.start(t.Context(), nil))
	t.Cleanup(func() { require.NoError(t, c.Stop(context.Background())) })
	c.publishNodeSnapshot(context.Background(), &opsCollectedSystemStats{cpuScope: "host", memoryScope: "host", cpuCapacity: 4, cpuUsagePercent: float64Ptr(80), memoryUsagePercent: float64Ptr(70)})
	require.NoError(t, mock.ExpectationsWereMet())
	return c
}

func TestOpsNodeMetricsKeepsAllNodesAndTheirPoolCapacities(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	server.SetTime(now)
	a := testNodeCollector(t, server, "A", 60, 64)
	testNodeCollector(t, server, "B", 20, 16)
	nodes, err := a.ListNodeMetrics(context.Background())
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, "A", nodes[0].NodeID)
	require.Equal(t, "B", nodes[1].NodeID)
	require.Equal(t, 60, *nodes[0].DBMaxOpenConns)
	require.Equal(t, 20, *nodes[1].DBMaxOpenConns)
	require.Equal(t, 64, *nodes[0].RedisPoolSize)
	require.Equal(t, 16, *nodes[1].RedisPoolSize)
	require.False(t, nodes[0].Stale)
	require.InDelta(t, 0, nodes[0].AgeSeconds, .01)
	require.WithinDuration(t, now, nodes[0].LastSeenAt, time.Millisecond)
	summary := summarizeOpsNodes(nodes)
	require.Equal(t, 80, *summary.DBMaxOpenConns)
	require.Equal(t, 80, *summary.RedisPoolSize)
	require.Nil(t, summary.MemoryUsedMB)
	require.Nil(t, summary.MemoryTotalMB)
}

func TestOpsNodeMetricsStaleClockSkewAndRetention(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	server.SetTime(now.Add(-time.Hour))
	c := testNodeCollector(t, server, "A", 60, 32)
	nodes, err := c.ListNodeMetrics(context.Background())
	require.NoError(t, err)
	require.False(t, nodes[0].Stale, "server timestamps must ignore host clock skew")
	server.SetTime(now.Add(-time.Hour + 31*time.Second))
	nodes, err = c.ListNodeMetrics(context.Background())
	require.NoError(t, err)
	require.True(t, nodes[0].Stale)
	require.Nil(t, summarizeOpsNodes(nodes))
	server.FastForward(opsNodeRetention + time.Second)
	nodes, err = c.ListNodeMetrics(context.Background())
	require.NoError(t, err)
	require.Empty(t, nodes)
}

func TestOpsNodeMetricsRestartReplacesSameIdentitySnapshot(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	server.SetTime(now)
	first := testNodeCollector(t, server, "stable-node", 60, 64)
	server.SetTime(now.Add(10 * time.Second))
	testNodeCollector(t, server, "stable-node", 20, 16)
	nodes, err := first.ListNodeMetrics(t.Context())
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "Node stable-node", nodes[0].NodeName)
	require.Equal(t, 20, *nodes[0].DBMaxOpenConns)
	require.Equal(t, 16, *nodes[0].RedisPoolSize)
	require.WithinDuration(t, now.Add(10*time.Second), nodes[0].LastSeenAt, time.Millisecond)
	require.Equal(t, opsNodeRetention, server.TTL(opsNodeKey("stable-node")))
}

func TestOpsNodeMetricsCorruptionAndRedisFailureAreNotHealthyCluster(t *testing.T) {
	server := miniredis.RunT(t)
	c := testNodeCollector(t, server, "A", 60, 32)
	server.Set(opsNodeKey("A"), "not-json")
	nodes, err := c.ListNodeMetrics(context.Background())
	require.Error(t, err)
	require.Len(t, nodes, 1)
	require.True(t, nodes[0].Stale)
	require.Equal(t, "unknown", nodes[0].CPUScope)
	require.Equal(t, "unknown", nodes[0].MemoryScope)
	server.Close()
	nodes, err = c.ListNodeMetrics(context.Background())
	require.Error(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "A", nodes[0].NodeID)
}

func TestOpsNodeSummaryDoesNotAverageAwayBusyNodesOrTreatMissingAsZero(t *testing.T) {
	a := &OpsNodeMetricsSnapshot{OpsSystemMetricsSnapshot: OpsSystemMetricsSnapshot{CPUUsagePercent: float64Ptr(10), MemoryUsagePercent: float64Ptr(20), DBConnActive: intPtr(2), DBMaxOpenConns: intPtr(60), DBOK: boolPtr(true)}, LastSeenAt: time.Now()}
	b := &OpsNodeMetricsSnapshot{OpsSystemMetricsSnapshot: OpsSystemMetricsSnapshot{CPUUsagePercent: float64Ptr(97), MemoryUsagePercent: float64Ptr(91), DBMaxOpenConns: intPtr(20), DBOK: boolPtr(false)}, LastSeenAt: time.Now()}
	out := summarizeOpsNodes([]*OpsNodeMetricsSnapshot{a, b})
	require.Equal(t, 97.0, *out.CPUUsagePercent)
	require.Equal(t, 91.0, *out.MemoryUsagePercent)
	require.False(t, *out.DBOK)
	require.Nil(t, out.DBConnActive)
	require.Equal(t, 80, *out.DBMaxOpenConns)
	b.Stale = true
	out = summarizeOpsNodes([]*OpsNodeMetricsSnapshot{a, b})
	require.Equal(t, 10.0, *out.CPUUsagePercent)
	overview := &OpsDashboardOverview{SystemMetrics: out, SystemMetricsNodes: []*OpsNodeMetricsSnapshot{a, b}}
	require.LessOrEqual(t, computeDashboardHealthScore(time.Now(), overview, nil), 89)
	overview.SystemMetricsNodes = nil
	overview.SystemMetricsUnavailable = true
	require.LessOrEqual(t, computeDashboardHealthScore(time.Now(), overview, nil), 89)
}

type opsNodeReaderFunc func(context.Context) ([]*OpsNodeMetricsSnapshot, error)

func (f opsNodeReaderFunc) ListNodeMetrics(ctx context.Context) ([]*OpsNodeMetricsSnapshot, error) {
	return f(ctx)
}

func TestOpsDashboardReturnsAllNodesAndNeverAttachesServingNodeLimits(t *testing.T) {
	now := time.Now().UTC()
	nodes := []*OpsNodeMetricsSnapshot{
		{NodeID: "A", LastSeenAt: now, OpsSystemMetricsSnapshot: OpsSystemMetricsSnapshot{CreatedAt: now, DBMaxOpenConns: intPtr(60), DBConnIdle: intPtr(8), DBOK: boolPtr(true), RedisOK: boolPtr(true), CPUUsagePercent: float64Ptr(10), MemoryUsagePercent: float64Ptr(20)}},
		{NodeID: "B", LastSeenAt: now, OpsSystemMetricsSnapshot: OpsSystemMetricsSnapshot{CreatedAt: now, DBMaxOpenConns: intPtr(20), DBConnIdle: intPtr(5), DBOK: boolPtr(true), RedisOK: boolPtr(true), CPUUsagePercent: float64Ptr(95), MemoryUsagePercent: float64Ptr(40)}},
	}
	repo := &opsHealthOverviewRepo{overview: &OpsDashboardOverview{}}
	cfg := &config.Config{}
	cfg.Ops.Enabled = true
	cfg.Database.MaxOpenConns = 999
	svc := &OpsService{opsRepo: repo, settingRepo: newStubSettingRepo(), cfg: cfg, nodeMetrics: opsNodeReaderFunc(func(context.Context) ([]*OpsNodeMetricsSnapshot, error) { return nodes, nil })}
	got, err := svc.GetDashboardOverview(context.Background(), &OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now})
	require.NoError(t, err)
	require.Len(t, got.SystemMetricsNodes, 2)
	require.Equal(t, 80, *got.SystemMetrics.DBMaxOpenConns)
	require.Equal(t, 95.0, *got.SystemMetrics.CPUUsagePercent)
	require.False(t, got.SystemMetricsUnavailable)
	svc.nodeMetrics = opsNodeReaderFunc(func(context.Context) ([]*OpsNodeMetricsSnapshot, error) { return nodes, context.DeadlineExceeded })
	got, err = svc.GetDashboardOverview(context.Background(), &OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now})
	require.NoError(t, err)
	require.True(t, got.SystemMetricsUnavailable)
	require.Len(t, got.SystemMetricsNodes, 2)
}

func TestOpsNodeIdentityConfigurationIsStableAcrossCollectors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ops.NodeID, cfg.Ops.NodeName = " rack-a/api-2 ", " API 2 "
	for range 2 {
		c := NewOpsMetricsCollector(nil, nil, nil, nil, nil, nil, cfg)
		id, name := c.nodeIdentity()
		require.Equal(t, "rack-a/api-2", id)
		require.Equal(t, "API 2", name)
		require.Nil(t, c.tasks.done, "construction must not start work")
	}
}

func TestOpsNodeMetricsWithoutRedisIsUnavailableAndLocalDataExpires(t *testing.T) {
	c := NewOpsMetricsCollector(nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, c.tasks.start(t.Context(), nil))
	t.Cleanup(func() { require.NoError(t, c.Stop(context.Background())) })
	sampledAt := time.Now().UTC().Add(-31 * time.Second)
	c.publishNodeSnapshot(t.Context(), &opsCollectedSystemStats{sampledAt: sampledAt, cpuUsagePercent: float64Ptr(4)})
	nodes, err := c.ListNodeMetrics(t.Context())
	require.Error(t, err, "local resources are not a successful cluster read")
	require.Len(t, nodes, 1)
	require.Equal(t, sampledAt, nodes[0].CreatedAt)
	require.True(t, nodes[0].Stale)
	require.Nil(t, nodes[0].DBOK)
	require.Nil(t, nodes[0].RedisOK)
	require.Nil(t, nodes[0].DBConnActive)
	require.True(t, opsNodesUnavailable(nodes, err))
	c.publishNodeSnapshot(t.Context(), &opsCollectedSystemStats{sampledAt: time.Now().Add(-opsNodeRetention - time.Second)})
	nodes, err = c.ListNodeMetrics(t.Context())
	require.Error(t, err)
	require.Empty(t, nodes)
}

func TestOpsNodeMetricsLifecycleFencesPublication(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	c := NewOpsMetricsCollector(nil, nil, nil, nil, nil, client, nil)
	stats := &opsCollectedSystemStats{cpuUsagePercent: float64Ptr(1)}
	c.publishNodeSnapshot(t.Context(), stats)
	require.Nil(t, c.nodeStats.Load())
	require.Empty(t, server.Keys())
	require.NoError(t, c.tasks.start(t.Context(), nil))
	c.publishNodeSnapshot(t.Context(), stats)
	first := c.nodeStats.Load()
	require.NotNil(t, first)
	require.NoError(t, c.Drain(t.Context()))
	c.publishNodeSnapshot(t.Context(), &opsCollectedSystemStats{cpuUsagePercent: float64Ptr(99)})
	require.Same(t, first, c.nodeStats.Load(), "drained publishers cannot replace the snapshot")
	_, err := c.ListNodeMetrics(t.Context())
	require.ErrorIs(t, err, errMaintenanceStopped)
	require.NoError(t, c.Stop(t.Context()))
	require.NoError(t, c.Stop(t.Context()))
}

type opsNodeBlockingPublishHook struct {
	entered    chan struct{}
	once       sync.Once
	canceled   chan struct{}
	cancelOnce sync.Once
	cancelErr  error
	release    <-chan struct{}
}

func (h *opsNodeBlockingPublishHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}
func (h *opsNodeBlockingPublishHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *opsNodeBlockingPublishHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "evalsha" || cmd.Name() == "eval" {
			h.once.Do(func() { close(h.entered) })
			<-ctx.Done()
			if h.canceled != nil {
				h.cancelOnce.Do(func() {
					h.cancelErr = ctx.Err()
					close(h.canceled)
				})
			}
			if h.release != nil {
				<-h.release
			}
			return ctx.Err()
		}
		return next(ctx, cmd)
	}
}

func TestOpsNodeMetricsStopCancelsPublicationAndJoinsIt(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	hook := &opsNodeBlockingPublishHook{entered: make(chan struct{})}
	client.AddHook(hook)
	c := NewOpsMetricsCollector(nil, nil, nil, nil, nil, client, nil)
	require.NoError(t, c.tasks.start(t.Context(), nil))
	t.Cleanup(func() { require.NoError(t, c.Stop(context.Background())) })
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		c.publishNodeSnapshot(t.Context(), &opsCollectedSystemStats{cpuUsagePercent: float64Ptr(1)})
	}()
	select {
	case <-hook.entered:
	case <-time.After(time.Second):
		t.Fatal("publication did not reach Redis")
	}
	drainCtx, cancelDrain := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelDrain()
	require.ErrorIs(t, c.Drain(drainCtx), context.DeadlineExceeded)
	stopCtx, cancelStop := context.WithTimeout(t.Context(), time.Second)
	defer cancelStop()
	require.NoError(t, c.Stop(stopCtx))
	select {
	case <-c.tasks.done:
	default:
		t.Fatal("stop returned before owned IO joined")
	}
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("publisher did not return")
	}
	require.Empty(t, server.Keys())
}

func TestOpsNodeSamplerPublishesEveryFiveSecondsThroughHostAndJoinsOnCleanup(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	life := host.NewLifetime()
	c := ProvideLifetimeOpsMetricsCollector(nil, nil, nil, nil, nil, client, nil, life)
	repo := &opsHealthOverviewRepo{overview: &OpsDashboardOverview{}}
	svc := ProvideLifetimeOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, c, life)
	require.Same(t, c, svc.nodeMetrics, "the root provider must inject the actual collector")
	require.Nil(t, c.tasks.done, "constructing providers must not launch sampling")
	require.Empty(t, server.Keys())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, life.Cleanup(ctx))
	})
	// Isolate the resource sampler from global SQL aggregation.
	c.nodesEnabled.Store(true)
	require.NoError(t, life.Start(t.Context()))
	require.Eventually(t, func() bool { return server.Exists(opsNodeRegistryKey) }, 3*time.Second, 10*time.Millisecond)
	readOverview := func() *OpsDashboardOverview {
		now := time.Now().UTC()
		got, err := svc.GetDashboardOverview(t.Context(), &OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now})
		require.NoError(t, err)
		require.Len(t, got.SystemMetricsNodes, 1)
		return got
	}
	first := readOverview().SystemMetricsNodes[0]
	require.False(t, first.Stale)
	require.NotNil(t, first.RedisOK)
	require.True(t, *first.RedisOK)
	// Wait for the real ticker, not another direct publish or synthetic clock.
	// Registry data is decoded through the same reader injected into dashboard.
	require.Eventually(t, func() bool {
		nodes, err := svc.nodeMetrics.ListNodeMetrics(t.Context())
		return err == nil && len(nodes) == 1 && nodes[0].CreatedAt.After(first.CreatedAt)
	}, gatewayResourceSampleInterval+3*time.Second, 20*time.Millisecond)
	second := readOverview().SystemMetricsNodes[0]
	require.Equal(t, first.NodeID, second.NodeID)
	require.GreaterOrEqual(t, second.CreatedAt.Sub(first.CreatedAt), 4*time.Second)
	cleanupCtx, cancelCleanup := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelCleanup()
	require.NoError(t, life.Cleanup(cleanupCtx))
	require.Empty(t, life.Pending())
	select {
	case <-c.tasks.done:
	default:
		t.Fatal("sampler was not joined")
	}
	last := c.nodeStats.Load()
	c.publishNodeSnapshot(t.Context(), &opsCollectedSystemStats{cpuUsagePercent: float64Ptr(99)})
	require.Same(t, last, c.nodeStats.Load())
	_, err := svc.nodeMetrics.ListNodeMetrics(t.Context())
	require.ErrorIs(t, err, errMaintenanceStopped)
}

func TestOpsNodeHostCleanupRetainsPublisherUntilJoinBeforeClosingRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	release := make(chan struct{})
	var releaseOnce sync.Once
	hook := &opsNodeBlockingPublishHook{entered: make(chan struct{}), canceled: make(chan struct{}), release: release}
	client.AddHook(hook)
	life := host.NewLifetime()
	c := ProvideLifetimeOpsMetricsCollector(nil, nil, nil, nil, nil, client, nil, life)
	parent, cancelParent := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancelParent()
		releaseOnce.Do(func() { close(release) })
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, life.Cleanup(ctx))
	})
	closed := 0
	life.Add(host.Component{Name: "ops-test-redis", Phase: host.Infrastructure, Constructed: true, Stop: func(context.Context) error {
		select {
		case <-c.tasks.done:
		default:
			return errors.New("Redis teardown preceded publisher join")
		}
		closed++
		return client.Close()
	}})
	c.nodesEnabled.Store(true)
	require.NoError(t, life.Start(parent))
	select {
	case <-hook.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("host-started sampler did not reach Redis publication")
	}
	// Parent cancellation must reach the actual publish context, before its
	// separate two-second timeout. Retain completion to exercise a late IO exit.
	cancelParent()
	select {
	case <-hook.canceled:
	case <-time.After(time.Second):
		t.Fatal("host cancellation did not reach the sampler's Redis operation")
	}
	require.ErrorIs(t, hook.cancelErr, context.Canceled, "publication must inherit host cancellation, not expire by its own timeout")
	short, cancelShort := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShort()
	require.ErrorIs(t, life.Cleanup(short), context.DeadlineExceeded)
	require.Zero(t, closed)
	require.Contains(t, life.Pending(), "OpsMetricsCollector")
	require.Contains(t, life.Pending(), "ops-test-redis")
	select {
	case <-c.tasks.done:
		t.Fatal("blocked publication was falsely joined")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	joined, cancelJoined := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelJoined()
	require.NoError(t, life.Cleanup(joined))
	require.Empty(t, life.Pending())
	require.Equal(t, 1, closed)
	require.NoError(t, life.Cleanup(joined))
	require.Equal(t, 1, closed, "completed dependencies must not close twice")
	require.Empty(t, server.Keys(), "canceled publication must not reach Redis")
}

func TestOpsDashboardUnknownNodesNeverReportHealthy(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name   string
		reader opsNodeMetricsReader
	}{
		{name: "unwired"},
		{name: "empty", reader: opsNodeReaderFunc(func(context.Context) ([]*OpsNodeMetricsSnapshot, error) { return nil, nil })},
		{name: "missing measurements", reader: opsNodeReaderFunc(func(context.Context) ([]*OpsNodeMetricsSnapshot, error) {
			return []*OpsNodeMetricsSnapshot{{NodeID: "A", LastSeenAt: now}}, nil
		})},
		{name: "redis unavailable", reader: opsNodeReaderFunc(func(context.Context) ([]*OpsNodeMetricsSnapshot, error) {
			return nil, errors.New("registry unavailable")
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &opsHealthOverviewRepo{overview: &OpsDashboardOverview{}}
			svc := &OpsService{opsRepo: repo, settingRepo: newStubSettingRepo(), nodeMetrics: tc.reader}
			got, err := svc.GetDashboardOverview(t.Context(), &OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now})
			require.NoError(t, err)
			require.True(t, got.SystemMetricsUnavailable)
			require.LessOrEqual(t, got.HealthScore, 89)
			data, err := json.Marshal(got)
			require.NoError(t, err)
			var dto map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &dto))
			require.Contains(t, dto, "system_metrics_nodes")
			require.JSONEq(t, "true", string(dto["system_metrics_unavailable"]))
		})
	}
}

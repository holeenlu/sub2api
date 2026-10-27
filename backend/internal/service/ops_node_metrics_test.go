package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
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

func TestOpsNodeMetricsCorruptionAndRedisFailureAreNotHealthyCluster(t *testing.T) {
	server := miniredis.RunT(t)
	c := testNodeCollector(t, server, "A", 60, 32)
	server.Set(opsNodeKey("A"), "not-json")
	nodes, err := c.ListNodeMetrics(context.Background())
	require.Error(t, err)
	require.Len(t, nodes, 1)
	require.True(t, nodes[0].Stale)
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
		{NodeID: "A", LastSeenAt: now, OpsSystemMetricsSnapshot: OpsSystemMetricsSnapshot{CreatedAt: now, DBMaxOpenConns: intPtr(60), DBConnIdle: intPtr(8)}},
		{NodeID: "B", LastSeenAt: now, OpsSystemMetricsSnapshot: OpsSystemMetricsSnapshot{CreatedAt: now, DBMaxOpenConns: intPtr(20), DBConnIdle: intPtr(5), CPUUsagePercent: float64Ptr(95)}},
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

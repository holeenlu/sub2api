package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWriteOpenAIFastPolicyBlockedResponseMarksBusinessLimited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	writeOpenAIFastPolicyBlockedResponse(c, &OpenAIFastBlockedError{Message: "custom fast policy block"})

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.True(t, HasOpsClientBusinessLimited(c))
	reason, ok := c.Get(OpsClientBusinessLimitedReasonKey)
	require.True(t, ok)
	require.Equal(t, OpsClientBusinessLimitedReasonLocalPolicyDenied, reason)
}

func TestOpsMetricsCollectorQueryErrorCountsExcludesCountTokens(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	collector := &OpsMetricsCollector{db: db}
	start := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectQuery(`(?s)FROM ops_error_logs\s+WHERE created_at >= \$1 AND created_at < \$2\s+AND is_count_tokens = FALSE`).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"error_total",
			"business_limited",
			"error_sla",
			"upstream_excl",
			"upstream_429",
			"upstream_529",
		}).AddRow(int64(5), int64(2), int64(3), int64(1), int64(1), int64(1)))

	errorTotal, businessLimited, errorSLA, upstreamExcl429529, upstream429, upstream529, err := collector.queryErrorCounts(context.Background(), start, end)
	require.NoError(t, err)
	require.Equal(t, int64(5), errorTotal)
	require.Equal(t, int64(2), businessLimited)
	require.Equal(t, int64(3), errorSLA)
	require.Equal(t, int64(1), upstreamExcl429529)
	require.Equal(t, int64(1), upstream429)
	require.Equal(t, int64(1), upstream529)
	require.NoError(t, mock.ExpectationsWereMet())
	mock.ExpectClose()
	require.NoError(t, db.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}

type opsMetricsBucketRepo struct {
	OpsRepository
	latestCalls int
	heartbeats  int
	input       *OpsInsertSystemMetricsInput
}

func (r *opsMetricsBucketRepo) GetLatestSystemMetrics(context.Context, int) (*OpsSystemMetricsSnapshot, error) {
	r.latestCalls++
	return &OpsSystemMetricsSnapshot{CreatedAt: time.Now().UTC().Truncate(time.Minute)}, nil
}
func (r *opsMetricsBucketRepo) UpsertJobHeartbeat(context.Context, *OpsUpsertJobHeartbeatInput) error {
	r.heartbeats++
	return nil
}
func (r *opsMetricsBucketRepo) InsertSystemMetrics(_ context.Context, input *OpsInsertSystemMetricsInput) error {
	r.input = input
	return nil
}

func TestOpsMetricsCollectorCompletedBucketSkipsAggregationAcrossReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	repo := &opsMetricsBucketRepo{}
	for range 2 {
		c := NewOpsMetricsCollector(repo, nil, nil, nil, db, client, nil)
		c.collectOnce(t.Context())
		require.True(t, c.nodesEnabled.Load(), "resource publication is enabled before leader selection")
	}
	require.Equal(t, 2, repo.latestCalls)
	require.Nil(t, repo.input)
	require.Zero(t, repo.heartbeats, "a skipped bucket is not a new successful job")
	require.False(t, server.Exists(opsMetricsCollectorLeaderLockKey))

	require.NoError(t, server.Set(opsMetricsCollectorLeaderLockKey, "other-owner"))
	c := NewOpsMetricsCollector(repo, nil, nil, nil, db, client, nil)
	c.collectOnce(t.Context())
	require.True(t, c.nodesEnabled.Load(), "a non-leader must still publish its resources")
	require.Equal(t, 2, repo.latestCalls, "non-leader must not aggregate global metrics")
}

func TestOpsMetricsCollectorUnavailableNodesPersistNullResources(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	repo := &opsMetricsBucketRepo{}
	c := NewOpsMetricsCollector(repo, nil, nil, nil, db, nil, nil)
	require.NoError(t, c.tasks.start(t.Context(), nil))
	t.Cleanup(func() { require.NoError(t, c.Stop(context.Background())) })
	// Even a healthy local diagnostic sample cannot stand in for the registry.
	c.nodeStats.Store(&OpsNodeMetricsSnapshot{OpsSystemMetricsSnapshot: OpsSystemMetricsSnapshot{
		CreatedAt: time.Now().UTC(), DBOK: boolPtr(true), RedisOK: boolPtr(true),
		CPUUsagePercent: float64Ptr(2), MemoryUsagePercent: float64Ptr(3), DBConnActive: intPtr(1),
	}})
	mock.ExpectQuery("(?s)SELECT.*FROM usage_logs").WillReturnRows(sqlmock.NewRows([]string{"success_count", "token_consumed"}).AddRow(3, 60))
	for range 2 {
		mock.ExpectQuery("(?s)SELECT.*percentile_cont.*FROM usage_logs").WillReturnRows(sqlmock.NewRows([]string{"p50", "p90", "p95", "p99", "avg_ms", "max_ms"}).AddRow(nil, nil, nil, nil, nil, nil))
	}
	mock.ExpectQuery("(?s)SELECT.*FROM ops_error_logs").WillReturnRows(sqlmock.NewRows([]string{"total", "business", "sla", "upstream", "429", "529"}).AddRow(0, 0, 0, 0, 0, 0))
	mock.ExpectQuery("(?s)SELECT.*AS switch_count.*FROM ops_error_logs").WillReturnRows(sqlmock.NewRows([]string{"switch_count"}).AddRow(0))
	require.NoError(t, c.collectAndPersist(t.Context()))
	require.NotNil(t, repo.input)
	require.EqualValues(t, 3, repo.input.SuccessCount)
	require.EqualValues(t, 60, repo.input.TokenConsumed)
	require.Equal(t, 1, repo.input.WindowMinutes)
	require.Equal(t, repo.input.CreatedAt.Truncate(time.Minute), repo.input.CreatedAt)
	require.Nil(t, repo.input.DBOK)
	require.Nil(t, repo.input.RedisOK)
	require.Nil(t, repo.input.CPUUsagePercent)
	require.Nil(t, repo.input.MemoryUsagePercent)
	require.Nil(t, repo.input.DBConnActive)
	require.Nil(t, repo.input.GoroutineCount)
}

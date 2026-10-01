//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Uses a disposable database only; the explicit variable is never sourced
// from the application's production database configuration.
func TestModelCatalogPostgresPublication(t *testing.T) {
	dsn := os.Getenv("CATALOG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CATALOG_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS accounts(id BIGINT PRIMARY KEY,status TEXT NOT NULL DEFAULT 'active',deleted_at TIMESTAMPTZ);INSERT INTO accounts(id) VALUES(1) ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	for _, file := range []string{"261_model_catalog_registry.sql", "262_model_catalog_global_sync_jobs.sql"} {
		migration, err := os.ReadFile("../../migrations/" + file)
		require.NoError(t, err)
		for i := 0; i < 2; i++ {
			_, err = db.ExecContext(ctx, string(migration))
			require.NoError(t, err, "migration is repeatable")
		}
	}
	// A separate connection represents another application process.
	db2, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db2.Close()
	r := NewModelCatalogRepository(db)
	other := NewModelCatalogRepository(db2)
	job := service.ModelCatalogJob{ID: "global-sync-test", Status: "complete", Succeeded: 1}
	require.NoError(t, r.SaveJob(ctx, job))
	savedJob, err := other.ReadJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, job.ID, savedJob.ID)
	require.Zero(t, savedJob.AccountID)
	require.Equal(t, 1, savedJob.Succeeded)

	source := "account:1"
	scope := "scope-a"
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, token := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			ok, err := r.Claim(ctx, source, 1, "openai", scope, token, time.Now().Add(time.Minute), true)
			if err != nil {
				results <- "error"
			} else if ok {
				results <- token
			}
		}(token)
	}
	wg.Wait()
	close(results)
	winner := ""
	for result := range results {
		require.Empty(t, winner, "only one distributed lease")
		require.NotEqual(t, "error", result)
		winner = result
	}
	require.NotEmpty(t, winner)
	snapshot := &service.ModelCatalogSnapshot{AccountID: 1, Platform: "openai", ScopeRevision: scope, Revision: "v1", Status: "ready", UpdatedAt: time.Now(), CheckedAt: time.Now(), Models: []service.ModelCatalogEntry{{ID: "future-a", Platform: "openai", Access: "listed", Lifecycle: "active"}}}
	require.NoError(t, r.Publish(ctx, source, winner, snapshot, time.Now()))
	current, err := other.Current(ctx, source)
	require.NoError(t, err)
	require.Equal(t, "v1", current.Revision)
	require.Equal(t, scope, current.ScopeRevision)
	ok, err := r.Claim(ctx, source, 1, "openai", scope, "worker-c", time.Now().Add(time.Minute), true)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, r.Fail(ctx, source, "worker-c", "upstream_rate_limited", time.Now().Add(time.Hour)))
	current, err = other.Current(ctx, source)
	require.NoError(t, err)
	require.Equal(t, "v1", current.Revision)
	require.Equal(t, "stale", current.Status)
	ok, err = r.Claim(ctx, source, 1, "openai", scope, "worker-d", time.Now().Add(time.Minute), false)
	require.NoError(t, err)
	require.False(t, ok, "backoff survives process boundaries")
	ok, err = r.Claim(ctx, source, 1, "openai", scope, "worker-d", time.Now().Add(time.Minute), true)
	require.NoError(t, err)
	require.True(t, ok)
	snapshot.Revision = "v2"
	snapshot.Models[0].Access = "unlisted"
	require.NoError(t, r.Publish(ctx, source, "worker-d", snapshot, time.Now()))
	require.NoError(t, r.SaveJob(ctx, service.ModelCatalogJob{ID: "job-1", AccountID: 1, Status: "complete", StartedAt: time.Now()}))
	storedJob, err := other.ReadJob(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, "complete", storedJob.Status)
	require.NoError(t, r.ObserveMedia(ctx, 1, scope, service.CatalogMediaObservation{Model: "picture-next", Operation: "images/generations", ObservedAt: time.Now()}))
	observations, err := other.MediaObservations(ctx, 1, scope)
	require.NoError(t, err)
	require.Len(t, observations, 1)
	require.Equal(t, "images/generations", observations[0].Operation)
	observations, err = other.MediaObservations(ctx, 1, "scope-b")
	require.NoError(t, err)
	require.Empty(t, observations)
	require.NoError(t, r.SavePrices(ctx, "price-v1", json.RawMessage(`{"future-a":{"input_cost_per_token":0}}`)))
	ok, err = r.Claim(ctx, source, 1, "openai", "scope-b", "worker-e", time.Now().Add(time.Minute), true)
	require.NoError(t, err)
	require.True(t, ok)
	current, err = other.Current(ctx, source)
	require.NoError(t, err)
	require.Nil(t, current, "new credential scope cannot read prior access")
	require.Error(t, other.Publish(ctx, source, "worker-d", snapshot, time.Now()), "stale publisher rejected")
}

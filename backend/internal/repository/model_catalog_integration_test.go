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

func TestModelCatalogPolicyMigration(t *testing.T) {
	dsn := os.Getenv("CATALOG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CATALOG_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	migration, err := os.ReadFile("../../migrations/263_model_catalog_candidates_only.sql")
	require.NoError(t, err)
	for _, tc := range []struct {
		name, policy, mapping, extra, mode, want string
		invalid                                  bool
	}{
		{name: "identities and authorized aliases", policy: `{"models":["selected"]}`, mapping: `{"alias":"selected","stale":"blocked"}`, want: `{"selected":"selected","alias":"selected"}`},
		{name: "selected alias retains target", policy: `{"models":["alias"]}`, mapping: `{"alias":"target"}`, want: `{"alias":"target"}`},
		{name: "empty unrestricted", policy: `{"models":[]}`, mapping: `{"stale":"stale"}`, want: `{}`},
		{name: "legacy", policy: `{"mode":"legacy"}`, mapping: `{"old":"target"}`, want: `{"old":"target"}`},
		{name: "null", policy: `null`, mapping: `{"old":"target"}`, want: `{"old":"target"}`},
		{name: "restricted explicit alias mode", policy: `{"models":["selected"]}`, mapping: `{"alias":"selected"}`, mode: "aliases", want: `{"selected":"selected","alias":"selected"}`},
		{name: "wildcard requires review", policy: `{"models":["gpt-*"]}`, mapping: `{}`, invalid: true},
		{name: "passthrough requires review", policy: `{"models":["selected"]}`, mapping: `{}`, extra: `"openai_oauth_passthrough":true`, invalid: true},
		{name: "unrestricted aliases require review", policy: `{"models":[]}`, mapping: `{"alias":"target"}`, invalid: true},
		{name: "malformed stays denied", policy: `{"models":42}`, mapping: `{}`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.Begin()
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.Exec(`CREATE TEMP TABLE accounts(id BIGINT PRIMARY KEY, platform TEXT, type TEXT, parent_account_id BIGINT, credentials JSONB, extra JSONB, updated_at TIMESTAMPTZ);
CREATE TEMP TABLE scheduler_outbox(event_type TEXT, account_id BIGINT);
INSERT INTO accounts VALUES(1,'openai','oauth',NULL,'{}','{"model_catalog_policy":{"models":["safe"]}}',NOW());`)
			require.NoError(t, err)
			extra := `{"model_catalog_policy":` + tc.policy
			if tc.extra != "" {
				extra += "," + tc.extra
			}
			extra += "}"
			credentials := `{"model_mapping":` + tc.mapping + `,"model_mapping_mode":"` + tc.mode + `"}`
			_, err = tx.Exec(`INSERT INTO accounts VALUES(2,'openai','oauth',NULL,$1,$2,NOW());`, credentials, extra)
			require.NoError(t, err)
			_, err = tx.Exec("SAVEPOINT migration_start")
			require.NoError(t, err)
			_, err = tx.Exec(string(migration))
			if tc.invalid {
				require.ErrorContains(t, err, "accounts {2}")
				_, err = tx.Exec("ROLLBACK TO SAVEPOINT migration_start")
				require.NoError(t, err)
				var unchanged, events int
				require.NoError(t, tx.QueryRow(`SELECT count(*) FROM accounts WHERE extra ? 'model_catalog_policy'`).Scan(&unchanged))
				require.NoError(t, tx.QueryRow(`SELECT count(*) FROM scheduler_outbox`).Scan(&events))
				require.Equal(t, 2, unchanged, "even the valid first row must roll back")
				require.Zero(t, events)
				return
			}
			require.NoError(t, err)
			var got string
			require.NoError(t, tx.QueryRow(`SELECT credentials->'model_mapping' FROM accounts WHERE id=2`).Scan(&got))
			require.JSONEq(t, tc.want, got)
			var remaining int
			require.NoError(t, tx.QueryRow(`SELECT count(*) FROM accounts WHERE extra ? 'model_catalog_policy'`).Scan(&remaining))
			require.Zero(t, remaining)
			_, err = tx.Exec(string(migration))
			require.NoError(t, err, "repeat execution is harmless")
		})
	}
}

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

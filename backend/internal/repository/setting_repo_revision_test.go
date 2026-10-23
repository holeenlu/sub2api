package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func settingsReadModelTestStore(t *testing.T) (*settingRepository, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("VENDOR_SUPPLY_TEST_DSN")
	if dsn == "" {
		t.Skip("VENDOR_SUPPLY_TEST_DSN not set")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/vendor_settlement_test", u.Path)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	schema := fmt.Sprintf("settings_revision_%d", time.Now().UnixNano())
	_, err = db.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, err := db.Exec("DROP SCHEMA " + schema + " CASCADE"); require.NoError(t, err) })
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	scoped, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, scoped)))
	t.Cleanup(func() { _ = client.Close() })
	_, err = scoped.Exec("CREATE TABLE settings(id BIGSERIAL PRIMARY KEY,key VARCHAR(100) UNIQUE NOT NULL,value TEXT NOT NULL,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())")
	require.NoError(t, err)
	return &settingRepository{client: client}, scoped
}

func TestSettingsRevisionPostgresConcurrentWriters(t *testing.T) {
	repo, _ := settingsReadModelTestStore(t)
	ctx := context.Background()
	require.NoError(t, repo.Set(ctx, "site_name", "before"))
	values, err := repo.GetAll(ctx)
	require.NoError(t, err)
	revision := service.SettingsRevision(values)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, value := range []string{"first", "second"} {
		go func(value string) {
			<-start
			_, err := repo.CompareAndSetMultiple(ctx, revision, map[string]string{"site_name": value, "payment_enabled": value})
			results <- err
		}(value)
	}
	close(start)
	succeeded, conflicted := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, service.ErrSettingsRevisionConflict):
			conflicted++
		default:
			t.Fatal(err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicted)
	values, err = repo.GetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, values["site_name"], values["payment_enabled"], "并发保存不能产生混合设置")
	revision = service.SettingsRevision(values)
	require.NoError(t, repo.Set(ctx, "new_setting", "another writer"))
	_, err = repo.CompareAndSetMultiple(ctx, revision, map[string]string{"site_name": "stale"})
	require.ErrorIs(t, err, service.ErrSettingsRevisionConflict)
	name, err := repo.GetValue(ctx, "site_name")
	require.NoError(t, err)
	require.NotEqual(t, "stale", name)
	values, err = repo.GetAll(ctx)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = repo.CompareAndSetMultiple(canceled, service.SettingsRevision(values), map[string]string{"site_name": "canceled"})
	require.Error(t, err)
	require.NoError(t, repo.Set(ctx, "after_cancel", "unblocked"))
}

func TestQuotaForecastPostgresScopesAndObservationCutoff(t *testing.T) {
	_, db := settingsReadModelTestStore(t)
	_, err := db.Exec(`CREATE TABLE usage_logs(account_id BIGINT,created_at TIMESTAMPTZ,authorization_mode TEXT,billing_status TEXT,input_tokens BIGINT,output_tokens BIGINT,cache_creation_tokens BIGINT,cache_read_tokens BIGINT,total_cost NUMERIC,actual_cost NUMERIC,account_stats_cost NUMERIC,account_rate_multiplier NUMERIC,upstream_model TEXT,model TEXT)`)
	require.NoError(t, err)
	observed := time.Now().UTC().Truncate(time.Second)
	for _, row := range []struct {
		mode, model, status string
		at                  time.Time
		tokens              int
	}{
		{"native", "gpt-5.1", "settled", observed.Add(-time.Minute), 100},
		{"bps", "gpt-5.1", "settled", observed.Add(-time.Minute), 700},
		{"siwc", "gpt-5.1", "settled", observed.Add(-time.Minute), 800},
		{"native", "gpt-5.1", "settled", observed.Add(time.Minute), 900},
		{"", "claude-fable", "settled", observed.Add(-time.Minute), 50},
	} {
		_, err = db.Exec(`INSERT INTO usage_logs VALUES(1,$1,$2,$3,$4,0,0,0,2,2,2,1,$5,$5)`, row.at, row.mode, row.status, row.tokens, row.model)
		require.NoError(t, err)
	}
	repo := &usageLogRepository{sql: db}
	sample, err := repo.GetAccountQuotaForecastSample(context.Background(), 1, observed.Add(-time.Hour), observed, service.PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, int64(1), sample.Requests)
	require.Equal(t, int64(100), sample.Tokens)
	require.Equal(t, 2.0, sample.Cost)
	sample, err = repo.GetAccountQuotaForecastSample(context.Background(), 1, observed.Add(-time.Hour), observed, "anthropic_fable")
	require.NoError(t, err)
	require.Equal(t, int64(50), sample.Tokens)
	_, err = db.Exec(`UPDATE usage_logs SET billing_status='pending_pricing' WHERE authorization_mode='native'`)
	require.NoError(t, err)
	sample, err = repo.GetAccountQuotaForecastSample(context.Background(), 1, observed.Add(-time.Hour), observed, service.PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, int64(1), sample.MissingCost)
}

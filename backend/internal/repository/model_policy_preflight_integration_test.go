//go:build integration

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestModelPolicyPreflightLeavesPersistentRowsUntouched(t *testing.T) {
	dsn := os.Getenv("CATALOG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an isolated disposable PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	for _, policy := range []string{`{"models":["allowed"]}`, `{"models":["gpt-*"]}`} {
		t.Run(policy, func(t *testing.T) {
			tx, err := db.Begin()
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.Exec(`CREATE TABLE public.accounts (id BIGINT, platform TEXT, type TEXT, parent_account_id BIGINT, credentials JSONB, extra JSONB, updated_at TIMESTAMPTZ);
CREATE TABLE public.scheduler_outbox(event_type TEXT, account_id BIGINT);`)
			require.NoError(t, err)
			_, err = tx.Exec(`INSERT INTO public.accounts VALUES(17, 'openai', 'apikey', NULL, '{}', jsonb_build_object('model_catalog_policy', $1::jsonb), NOW())`, policy)
			require.NoError(t, err)
			_, err = tx.Exec(`SAVEPOINT before_preflight`)
			require.NoError(t, err)
			err = checkModelPolicyMigration(context.Background(), tx)
			if policy == `{"models":["gpt-*"]}` {
				require.ErrorContains(t, err, "accounts {17}")
				_, err = tx.Exec(`ROLLBACK TO SAVEPOINT before_preflight`)
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
			}
			var current string
			require.NoError(t, tx.QueryRow(`SELECT extra->'model_catalog_policy' FROM public.accounts WHERE id=17`).Scan(&current))
			require.JSONEq(t, policy, current)
			var events int
			require.NoError(t, tx.QueryRow(`SELECT COUNT(*) FROM public.scheduler_outbox`).Scan(&events))
			require.Zero(t, events)
		})
	}
}

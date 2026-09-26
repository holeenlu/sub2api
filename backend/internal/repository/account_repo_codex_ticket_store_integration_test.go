//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newCodexTicketStoreTestAccount(t *testing.T, extra string) (*accountRepository, *service.Account) {
	t.Helper()
	ctx := context.Background()
	var id int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts(name, platform, type, status, credentials, extra)
		VALUES ('ticket-cas-test', 'openai', 'oauth', 'active',
			'{"access_token":"observed","refresh_token":"original","_token_version":7}'::jsonb, $1::jsonb)
		RETURNING id`, extra).Scan(&id))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id=$1`, id)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, id)
		require.NoError(t, err)
	})
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	observed, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	return repo, observed
}

func codexStoredGeneration(t *testing.T, id int64, model string) string {
	t.Helper()
	var generation string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(),
		`SELECT COALESCE(extra->$2->>'generation_id', '') FROM accounts WHERE id=$1`,
		id, "codex_turn_ticket:"+model).Scan(&generation))
	return generation
}

func codexStoreOutboxCount(t *testing.T, id int64) int {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1 AND event_type=$2`,
		id, service.SchedulerOutboxEventAccountChanged).Scan(&count))
	return count
}

func TestCodexTicketStorePreservesLatestExtraAndRefreshesScheduler(t *testing.T) {
	for _, initialTicket := range []string{"null", `{"generation_id":"previous","state":"old"}`} {
		t.Run(initialTicket, func(t *testing.T) {
			repo, observed := newCodexTicketStoreTestAccount(t,
				`{"codex_turn_ticket:gpt-6-astra":`+initialTicket+`,"admin_note":"original"}`)
			_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET extra=extra ||
				'{"admin_note":"concurrent edit","codex_turn_ticket:gpt-5.6-sol":{"generation_id":"other-model"}}'::jsonb WHERE id=$1`, observed.ID)
			require.NoError(t, err)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			ticket := json.RawMessage(`{"generation_id":"fresh","state":"verified","cookie":"__oailb=test"}`)

			applied, err := repo.StoreCodexTicketIfUnchanged(context.Background(), observed, "gpt-6-astra", ticket)

			require.NoError(t, err)
			require.True(t, applied)
			require.Equal(t, "fresh", codexStoredGeneration(t, observed.ID, "gpt-6-astra"))
			require.Equal(t, "other-model", codexStoredGeneration(t, observed.ID, "gpt-5.6-sol"))
			got, err := repo.GetByID(context.Background(), observed.ID)
			require.NoError(t, err)
			require.Equal(t, "concurrent edit", got.Extra["admin_note"])
			require.Equal(t, observed.Credentials, got.Credentials)
			require.Equal(t, 1, codexStoreOutboxCount(t, observed.ID))
			require.Len(t, cache.setAccounts, 1)
			require.Equal(t, got.Extra, cache.setAccounts[0].Extra)
			require.NoError(t, cache.setCtxErr)
		})
	}
}

func TestCodexTicketStoreRejectsStaleProbe(t *testing.T) {
	for _, test := range []struct {
		name, mutation string
	}{
		{"rotated access token", `credentials=jsonb_set(credentials, '{access_token}', '"replacement"')`},
		{"rotated refresh token", `credentials=jsonb_set(credentials, '{refresh_token}', '"replacement"')`},
		{"changed model whitelist", `credentials=credentials || '{"model_mapping":{"gpt-5.6-sol":"gpt-5.6-sol"}}'::jsonb`},
		{"changed OAuth-like type", `type='setup-token'`},
		{"new token version", `credentials=jsonb_set(credentials, '{_token_version}', '8')`},
		{"disabled account", `status='disabled'`},
		{"error account", `status='error'`},
		{"different platform", `platform='anthropic'`},
		{"different account type", `type='apikey'`},
		{"deleted account", `deleted_at=NOW()`},
		{"paused account participation", `extra=extra || '{"codex_ticket_harvest_enabled":false}'::jsonb`},
		{"paused model participation", `extra=extra || '{"codex_ticket_harvest_models":{"gpt-6-astra":false}}'::jsonb`},
		{"newer ticket generation", `extra=jsonb_set(extra, '{codex_turn_ticket:gpt-6-astra,generation_id}', '"newer"')`},
		{"invalidated previous generation", `extra=extra-'codex_turn_ticket:gpt-6-astra'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, observed := newCodexTicketStoreTestAccount(t,
				`{"codex_turn_ticket:gpt-6-astra":{"generation_id":"previous","state":"old"}}`)
			_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET `+test.mutation+` WHERE id=$1`, observed.ID)
			require.NoError(t, err)
			var before string
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT extra::text FROM accounts WHERE id=$1`, observed.ID).Scan(&before))
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache

			applied, err := repo.StoreCodexTicketIfUnchanged(context.Background(), observed, "gpt-6-astra",
				json.RawMessage(`{"generation_id":"stale-probe","state":"must-not-store"}`))

			require.NoError(t, err)
			require.False(t, applied)
			var after string
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT extra::text FROM accounts WHERE id=$1`, observed.ID).Scan(&after))
			require.JSONEq(t, before, after)
			require.Zero(t, codexStoreOutboxCount(t, observed.ID))
			require.Empty(t, cache.setAccounts)
		})
	}
}

func TestCodexTicketStoreConcurrentProbes(t *testing.T) {
	for _, sameModel := range []bool{true, false} {
		name := "different models preserve both tickets"
		if sameModel {
			name = "same generation has only one winner"
		}
		t.Run(name, func(t *testing.T) {
			repo, observed := newCodexTicketStoreTestAccount(t, `{}`)
			models := []string{"gpt-6-astra", "gpt-5.6-sol"}
			if sameModel {
				models[1] = models[0]
			}
			start := make(chan struct{})
			var workers sync.WaitGroup
			applied := make([]bool, 2)
			errors := make([]error, 2)
			for i := range models {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					ticket, _ := json.Marshal(map[string]string{"generation_id": []string{"first", "second"}[i]})
					applied[i], errors[i] = repo.StoreCodexTicketIfUnchanged(context.Background(), observed, models[i], ticket)
				}()
			}
			close(start)
			workers.Wait()
			require.NoError(t, errors[0])
			require.NoError(t, errors[1])
			if sameModel {
				require.NotEqual(t, applied[0], applied[1])
				require.Equal(t, 1, codexStoreOutboxCount(t, observed.ID))
				generation := "first"
				if applied[1] {
					generation = "second"
				}
				require.Equal(t, generation, codexStoredGeneration(t, observed.ID, models[0]))
			} else {
				require.True(t, applied[0])
				require.True(t, applied[1])
				require.Equal(t, "first", codexStoredGeneration(t, observed.ID, models[0]))
				require.Equal(t, "second", codexStoredGeneration(t, observed.ID, models[1]))
				require.Equal(t, 2, codexStoreOutboxCount(t, observed.ID))
			}
		})
	}
}

func TestCodexTicketStoreOutboxFailureRollsBackTicket(t *testing.T) {
	repo, observed := newCodexTicketStoreTestAccount(t, `{}`)
	repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
	cache := &schedulerCacheRecorder{}
	repo.schedulerCache = cache

	applied, err := repo.StoreCodexTicketIfUnchanged(context.Background(), observed, "gpt-6-astra",
		json.RawMessage(`{"generation_id":"fresh"}`))

	require.Error(t, err)
	require.False(t, applied)
	require.Empty(t, codexStoredGeneration(t, observed.ID, "gpt-6-astra"))
	require.Zero(t, codexStoreOutboxCount(t, observed.ID))
	require.Empty(t, cache.setAccounts)
}

func TestCodexTicketStoreRefreshesSchedulerAfterRequestCancellation(t *testing.T) {
	repo, observed := newCodexTicketStoreTestAccount(t, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo.sql = &cancelAfterAtomicMutationSQLExecutor{sqlExecutor: integrationDB, cancel: cancel}
	cache := &schedulerCacheRecorder{}
	repo.schedulerCache = cache

	applied, err := repo.StoreCodexTicketIfUnchanged(ctx, observed, "gpt-6-astra",
		json.RawMessage(`{"generation_id":"fresh"}`))

	require.NoError(t, err)
	require.True(t, applied)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Len(t, cache.setAccounts, 1)
	require.NoError(t, cache.setCtxErr)
	require.Equal(t, "fresh", cache.setAccounts[0].Extra["codex_turn_ticket:gpt-6-astra"].(map[string]any)["generation_id"])
}

func TestCodexTicketStoreSupportsSetupToken(t *testing.T) {
	repo, account := newCodexTicketStoreTestAccount(t, `{}`)
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET type='setup-token' WHERE id=$1`, account.ID)
	require.NoError(t, err)
	observed, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)

	applied, err := repo.StoreCodexTicketIfUnchanged(context.Background(), observed, "gpt-6-astra",
		json.RawMessage(`{"generation_id":"setup-ticket"}`))

	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, "setup-ticket", codexStoredGeneration(t, account.ID, "gpt-6-astra"))
	require.Equal(t, 1, codexStoreOutboxCount(t, account.ID))
}

func TestCodexTicketStoreRejectsShadowAndIneligibleSnapshots(t *testing.T) {
	for _, kind := range []string{"shadow", "apikey", "other-platform", "disabled"} {
		t.Run(kind, func(t *testing.T) {
			repo, observed := newCodexTicketStoreTestAccount(t, `{}`)
			switch kind {
			case "shadow":
				parentID := observed.ID + 1
				observed.ParentAccountID = &parentID
			case "apikey":
				observed.Type = service.AccountTypeAPIKey
			case "other-platform":
				observed.Platform = service.PlatformAnthropic
			case "disabled":
				observed.Status = "disabled"
			}

			applied, err := repo.StoreCodexTicketIfUnchanged(context.Background(), observed, "gpt-6-astra",
				json.RawMessage(`{"generation_id":"must-not-store"}`))

			require.NoError(t, err)
			require.False(t, applied)
			require.Empty(t, codexStoredGeneration(t, observed.ID, "gpt-6-astra"))
			require.Zero(t, codexStoreOutboxCount(t, observed.ID))
		})
	}
}

func TestCodexTicketStoreRejectsAccountThatBecameShadow(t *testing.T) {
	_, parent := newCodexTicketStoreTestAccount(t, `{}`)
	repo, observed := newCodexTicketStoreTestAccount(t, `{}`)
	_, err := integrationDB.ExecContext(context.Background(),
		`UPDATE accounts SET parent_account_id=$2, quota_dimension='spark' WHERE id=$1`, observed.ID, parent.ID)
	require.NoError(t, err)

	applied, err := repo.StoreCodexTicketIfUnchanged(context.Background(), observed, "gpt-6-astra",
		json.RawMessage(`{"generation_id":"must-not-store"}`))

	require.NoError(t, err)
	require.False(t, applied)
	require.Empty(t, codexStoredGeneration(t, observed.ID, "gpt-6-astra"))
	require.Zero(t, codexStoreOutboxCount(t, observed.ID))
}

func TestCodexTicketStoreComparesEffectivePassthroughMode(t *testing.T) {
	for _, test := range []struct {
		name, initial, changed string
		applied                bool
	}{
		{"enable canonical passthrough", `{}`, `{"openai_passthrough":true}`, false},
		{"enable legacy passthrough", `{}`, `{"openai_oauth_passthrough":true}`, false},
		{"disable canonical passthrough", `{"openai_passthrough":true}`, `{"openai_passthrough":false}`, false},
		{"disable legacy passthrough", `{"openai_oauth_passthrough":true}`, `{"openai_oauth_passthrough":false}`, false},
		{"canonical false overrides legacy true", `{"openai_oauth_passthrough":true}`, `{"openai_passthrough":false}`, false},
		{"invalid canonical falls back to legacy", `{"openai_passthrough":"invalid","openai_oauth_passthrough":true}`, `{"openai_oauth_passthrough":false}`, false},
		{"equivalent canonical upgrade", `{"openai_oauth_passthrough":true}`, `{"openai_passthrough":true}`, true},
		{"ignored legacy toggle", `{"openai_passthrough":false,"openai_oauth_passthrough":true}`, `{"openai_oauth_passthrough":false}`, true},
		{"nonboolean values stay disabled", `{}`, `{"openai_passthrough":"true","openai_oauth_passthrough":1}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, observed := newCodexTicketStoreTestAccount(t, test.initial)
			_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET extra=extra || $2::jsonb WHERE id=$1`, observed.ID, test.changed)
			require.NoError(t, err)

			applied, err := repo.StoreCodexTicketIfUnchanged(context.Background(), observed, "gpt-6-astra",
				json.RawMessage(`{"generation_id":"fresh"}`))

			require.NoError(t, err)
			require.Equal(t, test.applied, applied)
			if test.applied {
				require.Equal(t, "fresh", codexStoredGeneration(t, observed.ID, "gpt-6-astra"))
			} else {
				require.Empty(t, codexStoredGeneration(t, observed.ID, "gpt-6-astra"))
			}
		})
	}
}

//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func cloudAuthorizationRepositoryFixture(client *dbent.Client, sql sqlExecutor, cache service.SchedulerCache) *accountRepository {
	repo := newAccountRepositoryWithSQL(client, sql, cache)
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{
		verify: func(context.Context, *service.Account) (*service.VerifiedSupplyIdentity, error) {
			return new(service.VerifiedSupplyIdentity), nil
		},
		claim: func(context.Context, service.VendorProxySQL, *service.Account, *service.VerifiedSupplyIdentity) error {
			return nil
		},
	})
	return repo
}

func cloudAuthorizationAccountFixture() *service.Account {
	return &service.Account{Name: fmt.Sprintf("cloud-auth-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "native-access", "refresh_token": "native-refresh", "chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}, Extra: map[string]any{}}
}

func cloudAuthorizationBindingFixture() *service.OpenAICloudAuthorizationBinding {
	return &service.OpenAICloudAuthorizationBinding{Generation: "generation", AccountID: "workspace", UserID: "user", Subject: "subject", ProtocolVersion: service.OpenAICloudProtocolVersion, Models: map[string]service.OpenAICloudCapabilities{"verified-model": {Text: true, Tools: true, Usage: true, Settlement: true, ImageInput: true, EncryptedContext: true, WebSearch: true, ImageGeneration: true, VerifiedAt: time.Now().UTC()}}}
}

func TestCloudMigrationPreservesHistoricalSIWCUsageAndSettlement(t *testing.T) {
	ctx := t.Context()
	tx := testTx(t)
	// Reproduce the upgrade in a rollback-only schema, without dropping any
	// current integration tables or rewriting the applied-migration ledger.
	schema := fmt.Sprintf("cloud_siwc_upgrade_%d", time.Now().UnixNano())
	_, err := tx.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "SET LOCAL search_path="+schema+",pg_catalog")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
CREATE TABLE accounts (
 id BIGINT PRIMARY KEY,
 platform TEXT NOT NULL DEFAULT 'openai',
 type TEXT NOT NULL DEFAULT 'oauth',
 vendor_id BIGINT,
 parent_account_id BIGINT,
 deleted_at TIMESTAMPTZ,
 credentials JSONB NOT NULL DEFAULT '{}'::jsonb,
 extra JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE usage_logs (
 id BIGINT PRIMARY KEY,
 account_id BIGINT NOT NULL REFERENCES accounts(id),
 total_cost NUMERIC(20,8) NOT NULL,
 actual_cost NUMERIC(20,8) NOT NULL,
 billing_status TEXT NOT NULL DEFAULT 'pending'
);
INSERT INTO accounts(id,extra) VALUES(1,'{"openai_siwc":true}'::jsonb);
`)
	require.NoError(t, err)
	apply := func(name string) {
		t.Helper()
		raw, err := dbmigrations.FS.ReadFile(name)
		require.NoError(t, err, "read the actual migration, not a copied fixture")
		_, err = tx.ExecContext(ctx, string(raw))
		require.NoError(t, err, name)
	}
	apply("279_openai_usage_authorization_mode.sql")
	apply("284_openai_siwc_grants.sql")
	_, err = tx.ExecContext(ctx, `
INSERT INTO openai_siwc_installation(singleton,host_id) VALUES(TRUE,'historical-installation');
INSERT INTO openai_siwc_grants(account_id,registration_ciphertext,credentials_ciphertext)
 VALUES(1,'historical-registration','historical-credentials');
INSERT INTO usage_logs(id,account_id,authorization_mode,total_cost,actual_cost)
 VALUES(1,1,'siwc',0.23456789,0.12345678);
`)
	require.NoError(t, err)
	type usageFacts struct{ mode, total, actual, status string }
	readUsage := func() usageFacts {
		t.Helper()
		var facts usageFacts
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT authorization_mode,total_cost::text,actual_cost::text,billing_status FROM usage_logs WHERE id=1`).Scan(&facts.mode, &facts.total, &facts.actual, &facts.status))
		return facts
	}
	before := readUsage()
	require.Equal(t, usageFacts{"siwc", "0.23456789", "0.12345678", "pending"}, before)
	for _, name := range []string{"290_remove_openai_siwc_authorization.sql", "291_openai_cloud_authorization.sql"} {
		apply(name)
		require.Equal(t, before, readUsage(), "migration must preserve historical source and monetary evidence")
	}
	for _, name := range []string{"openai_siwc_grants", "openai_siwc_installation"} {
		var removed bool
		require.NoError(t, tx.QueryRowContext(ctx, "SELECT to_regclass($1) IS NULL", schema+"."+name).Scan(&removed))
		require.True(t, removed, "retired authorization storage must stay removed")
	}
	var stillConfigured bool
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT extra ? 'openai_siwc' FROM accounts WHERE id=1").Scan(&stillConfigured))
	require.False(t, stillConfigured)
	result, err := tx.ExecContext(ctx, "UPDATE usage_logs SET billing_status='settled' WHERE id=1")
	require.NoError(t, err, "historical SIWC rows must still satisfy the CHECK on later settlement updates")
	rows, err := result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	settled := before
	settled.status = "settled"
	require.Equal(t, settled, readUsage())
	// The Cloud addition must not weaken the original immutable-source guard.
	_, err = tx.ExecContext(ctx, "SAVEPOINT immutable_usage_source")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET authorization_mode='cloud' WHERE id=1")
	require.ErrorContains(t, err, "usage authorization mode is immutable")
	_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT immutable_usage_source")
	require.NoError(t, err)
	require.Equal(t, settled, readUsage())
	_, err = tx.ExecContext(ctx, "INSERT INTO usage_logs(id,account_id,authorization_mode,total_cost,actual_cost) VALUES(2,1,'cloud',0.1,0.1)")
	require.NoError(t, err, "new Cloud records are admitted without rewriting old SIWC records")
}

func TestCloudAuthorizationPostgresSharedSettingsBoundary(t *testing.T) {
	for _, path := range []string{"extra patch", "bulk patch", "account save", "raw SQL", "import create"} {
		t.Run(path, func(t *testing.T) {
			tx := testEntTx(t)
			ctx := dbent.NewTxContext(t.Context(), tx)
			repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
			a := cloudAuthorizationAccountFixture()
			require.NoError(t, repo.Create(ctx, a))
			patch := map[string]any{service.OpenAICloudEnabledKey: true, service.OpenAICloudModelsKey: []string{"unverified"}}
			var err error
			switch path {
			case "extra patch":
				err = repo.UpdateExtra(ctx, a.ID, patch)
			case "bulk patch":
				_, err = repo.BulkUpdate(ctx, []int64{a.ID}, service.AccountBulkUpdate{Extra: patch})
			case "account save":
				a.Extra = patch
				err = repo.Update(ctx, a)
			case "raw SQL":
				_, err = tx.Client().ExecContext(ctx, `UPDATE accounts SET extra='{"openai_cloud":true,"openai_cloud_models":["unverified"]}' WHERE id=$1`, a.ID)
				err = mapCloudSettingsError(err)
			case "import create":
				copy := cloudAuthorizationAccountFixture()
				copy.Extra = patch
				err = repo.Create(ctx, copy)
			}
			require.ErrorIs(t, err, service.ErrOpenAICloudSettings)
		})
	}
}

func TestCloudAuthorizationPostgresRequiresEveryCapability(t *testing.T) {
	for _, missing := range []string{"text", "tools", "usage", "settlement", "image_input", "encrypted_context", "web_search", "image_generation", "verified_at"} {
		t.Run(missing, func(t *testing.T) {
			tx := testEntTx(t)
			ctx := dbent.NewTxContext(t.Context(), tx)
			repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
			a := cloudAuthorizationAccountFixture()
			require.NoError(t, repo.Create(ctx, a))
			b := cloudAuthorizationBindingFixture()
			encoded, err := json.Marshal(b.Models["verified-model"])
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &fields))
			require.Contains(t, fields, missing)
			delete(fields, missing)
			encoded, err = json.Marshal(fields)
			require.NoError(t, err)
			var incomplete service.OpenAICloudCapabilities
			require.NoError(t, json.Unmarshal(encoded, &incomplete))
			require.False(t, incomplete.Complete())
			b.Models["verified-model"] = incomplete
			// Pending facts may be stored, but enabling the account must fail.
			// An omitted timestamp becomes Go's zero time when persisted; a
			// mere verified_at JSON key must not make that model eligible.
			ok, err := repo.StoreCloudAuthorization(ctx, a, "", b, nil)
			require.NoError(t, err)
			require.True(t, ok)
			err = repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true, service.OpenAICloudModelsKey: []string{"verified-model"}})
			require.ErrorIs(t, err, service.ErrOpenAICloudSettings)
		})
	}
}

func TestCloudAuthorizationPostgresVerifiedModelsAndIdentity(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true, service.OpenAICloudModelsKey: []string{"verified-model"}}))
	a, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, a.IsOpenAICloudEnabled())
	// Token rotation changes only the native authority and preserves Cloud.
	rotated := copyJSONMap(a.Credentials)
	rotated["access_token"] = "rotated-access"
	rotated["refresh_token"] = "rotated-refresh"
	require.NoError(t, repo.UpdateCredentials(ctx, a.ID, rotated))
	b, err = repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, b.Revoked)
	require.Equal(t, "generation", b.Generation)
	a, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, a.IsOpenAICloudEnabled())
	// A late callback cannot overwrite a newer native credential document.
	stale := *a
	stale.Credentials = copyJSONMap(a.Credentials)
	stale.Credentials["access_token"] = "native-access"
	ok, err = repo.StoreCloudAuthorization(ctx, &stale, b.Generation, b, nil)
	require.NoError(t, err)
	require.False(t, ok)
	// Identity replacement atomically disables Cloud and advances its tombstone.
	replaced := copyJSONMap(rotated)
	replaced["chatgpt_account_id"] = "new-workspace"
	require.NoError(t, repo.UpdateCredentials(ctx, a.ID, replaced))
	current, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, current.IsOpenAICloudEnabled())
	revoked, err := repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, revoked.Revoked)
	require.NotEqual(t, b.Generation, revoked.Generation)
	require.Empty(t, revoked.Models)
	ok, err = repo.StoreCloudAuthorization(ctx, a, b.Generation, b, nil)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCloudAuthorizationPostgresBPSConflictAndRevisionCAS(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b, nil)
	require.NoError(t, err)
	require.True(t, ok)
	b, err = repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	stale := *b
	ok, err = repo.StoreCloudAuthorization(ctx, a, b.Generation, b, nil)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.StoreCloudAuthorization(ctx, a, stale.Generation, &stale, nil)
	require.NoError(t, err)
	require.False(t, ok, "same-generation probe writes still require revision CAS")
	for _, patch := range []map[string]any{
		{service.OpenAICloudEnabledKey: true, service.OpenAICloudModelsKey: []string{"verified-model"}, "openai_excel_bps": true},
		{service.OpenAICloudEnabledKey: true, service.OpenAICloudModelsKey: []string{}},
		{service.OpenAICloudEnabledKey: "true"},
		{service.OpenAICloudModelsKey: nil},
	} {
		_, err = tx.Client().ExecContext(ctx, "SAVEPOINT invalid_cloud_settings")
		require.NoError(t, err)
		err = repo.UpdateExtra(ctx, a.ID, patch)
		require.ErrorIs(t, err, service.ErrOpenAICloudSettings)
		_, err = tx.Client().ExecContext(ctx, "ROLLBACK TO SAVEPOINT invalid_cloud_settings")
		require.NoError(t, err)
	}
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: false, service.OpenAICloudModelsKey: []string{}}))
}

func TestCloudAuthorizationPostgresPermissionRollback(t *testing.T) {
	client := testEntClient(t)
	repo := cloudAuthorizationRepositoryFixture(client, integrationDB, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(t.Context(), a))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", a.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", a.ID)
	})
	ctx := authz.WithRevalidation(t.Context(), func(context.Context) error { return authz.ErrAuthorizationExpired })
	credentials := copyJSONMap(a.Credentials)
	credentials["access_token"] = "replacement"
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", cloudAuthorizationBindingFixture(), credentials)
	require.ErrorIs(t, err, authz.ErrAuthorizationExpired)
	require.False(t, ok)
	b, err := repo.GetCloudAuthorization(t.Context(), a.ID)
	require.NoError(t, err)
	require.Nil(t, b)
	current, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, "native-access", current.GetCredential("access_token"))
	require.NotContains(t, current.Extra, service.OpenAICloudEnabledKey)
}

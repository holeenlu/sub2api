//go:build integration

package repository

import (
	"context"
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
	return &service.OpenAICloudAuthorizationBinding{Generation: "generation", AccountID: "workspace", UserID: "user", Subject: "subject"}
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

func TestCloudNativeAuthorizationPostgresSwitchNeedsNoSeparateLogin(t *testing.T) {
	for _, path := range []string{"extra patch", "bulk patch", "account save", "raw SQL", "import create"} {
		t.Run(path, func(t *testing.T) {
			tx := testEntTx(t)
			ctx := dbent.NewTxContext(t.Context(), tx)
			repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
			a := cloudAuthorizationAccountFixture()
			require.NoError(t, repo.Create(ctx, a))
			patch := map[string]any{service.OpenAICloudEnabledKey: true}
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
				_, err = tx.Client().ExecContext(ctx, `UPDATE accounts SET extra='{"openai_cloud":true}' WHERE id=$1`, a.ID)
				err = mapCloudSettingsError(err)
			case "import create":
				copy := cloudAuthorizationAccountFixture()
				copy.Extra = patch
				err = repo.Create(ctx, copy)
			}
			require.NoError(t, err)
		})
	}
}

func TestCloudAuthorizationPostgresEnablesWithoutVerification(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true}))
	a, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b)
	require.NoError(t, err)
	require.True(t, ok)
	current, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, current.IsOpenAICloudEnabled())
	// The ordinary model map remains an independent account policy. Changing
	// it needs neither per-model Cloud evidence nor another authorization.
	credentials := copyJSONMap(current.Credentials)
	credentials["model_mapping"] = map[string]any{"client-model": "new-model"}
	require.NoError(t, repo.UpdateCredentials(ctx, a.ID, credentials))
	current, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, current.IsOpenAICloudEnabled())
	grant, err := repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, b.Generation, grant.Generation)
}

func TestCloudAuthorizationPostgresAuthorizationAndIdentity(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true}))
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
	ok, err = repo.StoreCloudAuthorization(ctx, &stale, b.Generation, b)
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
	ok, err = repo.StoreCloudAuthorization(ctx, a, b.Generation, b)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCloudAuthorizationPostgresSettingsAndRevisionCAS(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b)
	require.NoError(t, err)
	require.True(t, ok)
	b, err = repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	stale := *b
	ok, err = repo.StoreCloudAuthorization(ctx, a, b.Generation, b)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.StoreCloudAuthorization(ctx, a, stale.Generation, &stale)
	require.NoError(t, err)
	require.False(t, ok, "same-generation authorization writes still require revision CAS")
	for _, patch := range []map[string]any{
		{service.OpenAICloudEnabledKey: "true"},
		{service.OpenAICloudEnabledKey: nil},
	} {
		_, err = tx.Client().ExecContext(ctx, "SAVEPOINT invalid_cloud_settings")
		require.NoError(t, err)
		err = repo.UpdateExtra(ctx, a.ID, patch)
		require.ErrorIs(t, err, service.ErrOpenAICloudSettings)
		_, err = tx.Client().ExecContext(ctx, "ROLLBACK TO SAVEPOINT invalid_cloud_settings")
		require.NoError(t, err)
	}
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: false}))
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
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", cloudAuthorizationBindingFixture())
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

func TestCloudAuthorizationPostgresModelAndProxySettingsPreserveAuthorization(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b)
	require.NoError(t, err)
	require.True(t, ok)
	enable := func() {
		require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true}))
	}
	assertPreserved := func() {
		t.Helper()
		current, err := repo.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.True(t, current.IsOpenAICloudEnabled())
		grant, err := repo.GetCloudAuthorization(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, b.Generation, grant.Generation)
		require.False(t, grant.Revoked)
	}
	enable()
	creds := copyJSONMap(a.Credentials)
	creds["model_mapping"] = map[string]any{"client": "verified-model"}
	require.NoError(t, repo.UpdateCredentials(ctx, a.ID, creds))
	assertPreserved()
	var proxyID int64
	rows, err := tx.Client().QueryContext(ctx, `INSERT INTO proxies(name,protocol,host,port,status) VALUES('cloud-verification','http','127.0.0.1',8080,'active') RETURNING id`)
	require.NoError(t, err)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&proxyID))
	require.NoError(t, rows.Close())
	_, err = tx.Client().ExecContext(ctx, `UPDATE accounts SET proxy_id=$1 WHERE id=$2`, proxyID, a.ID)
	require.NoError(t, err)
	assertPreserved()
	_, err = tx.Client().ExecContext(ctx, `UPDATE proxies SET host='127.0.0.2' WHERE id=$1`, proxyID)
	require.NoError(t, err)
	assertPreserved()
}

func TestCloudReconciliationPostgresRequiresAllPersistentProofs(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	complete, err := repo.CloudRuntimeReconciled(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, complete, "missing Redis/SQL evidence cannot unlock")
	root := fmt.Sprintf("cloud-root-%d", a.ID)
	require.NoError(t, repo.RecordCloudRuntimeBlock(ctx, root, a.ID))
	complete, err = repo.CloudRuntimeReconciled(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, complete)
	var actorID int64
	rows, err := tx.Client().QueryContext(ctx, `INSERT INTO users(email,password_hash,role,status) VALUES($1,'hash','super_admin','active') RETURNING id`, fmt.Sprintf("cloud-%d@example.test", a.ID))
	require.NoError(t, err)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&actorID))
	require.NoError(t, rows.Close())
	_, err = tx.Client().ExecContext(ctx, `INSERT INTO openai_cloud_reconciliations(root_id,account_id,actor_id,evidence,reason,settlement) VALUES($1,$2,$3,'verified external evidence','confirmed final usage','{}')`, root, a.ID, actorID)
	require.NoError(t, err)
	complete, err = repo.CloudRuntimeReconciled(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, complete, "an audit record is not a ledger ACK")
	require.NoError(t, repo.MarkCloudReconciliationSettled(ctx, root))
	complete, err = repo.CloudRuntimeReconciled(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, complete)
	require.NoError(t, repo.RecordCloudRuntimeBlock(ctx, root+"-second", a.ID))
	complete, err = repo.CloudRuntimeReconciled(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, complete, "every root needs independent proof")
}

func TestCloudPostgresAccountBlockSurvivesGrantReplacement(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true}))
	require.NoError(t, repo.RecordCloudRuntimeBlock(ctx, "old-generation-root", a.ID))
	a, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, a.IsOpenAICloudEnabled())
	b, err = repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, b.RuntimeBlocked)
	old := b.Generation
	b.Generation = "replacement-grant"
	b.RuntimeBlocked = false
	ok, err = repo.StoreCloudAuthorization(ctx, a, old, b)
	require.NoError(t, err)
	require.True(t, ok)
	b, err = repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, b.RuntimeBlocked)
	// Reading SQL alone (no Redis dependency) still exposes unresolved roots if
	// an old writer left the grant flag false.
	_, err = tx.Client().ExecContext(ctx, `UPDATE openai_cloud_grants SET runtime_blocked=FALSE WHERE account_id=$1`, a.ID)
	require.NoError(t, err)
	b, err = repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, b.RuntimeBlocked)
	err = repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true})
	require.ErrorIs(t, err, service.ErrOpenAICloudSettings, "a durable unresolved root still blocks direct re-enabling")
}

func TestCloudPostgresProtocolChoiceStillFencesStaleAuthorizationWrites(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true}))
	a, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	b, err = repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: false}))
	ok, err = repo.StoreCloudAuthorization(ctx, a, b.Generation, b)
	require.NoError(t, err)
	require.False(t, ok, "protocol selection changes fence the old account snapshot")
}

func TestCloudPostgresRetiredModelListRejected(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	err := repo.UpdateExtra(ctx, a.ID, map[string]any{"openai_cloud_models": []string{"model"}})
	require.ErrorIs(t, err, service.ErrUnsupportedAccountField)
	_, err = tx.Client().ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{openai_cloud_models}','[]') WHERE id=$1`, a.ID)
	require.ErrorIs(t, mapCloudSettingsError(err), service.ErrOpenAICloudSettings)
}

func TestCloudVerificationRemovalMigrationPreservesGrantAndProtocolSelection(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b)
	require.NoError(t, err)
	require.True(t, ok)
	before, err := repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	// Reconstruct only the retired storage in a rollback-only transaction. The
	// actual forward migration must remove it without changing the selected mode.
	_, err = tx.Client().ExecContext(ctx, `ALTER TABLE openai_cloud_grants ADD COLUMN models JSONB NOT NULL DEFAULT '{}'::jsonb, ADD COLUMN protocol_version TEXT NOT NULL DEFAULT 'old-version'`)
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, `ALTER TABLE accounts DISABLE TRIGGER accounts_openai_cloud_settings`)
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{openai_cloud}','true') || '{"openai_cloud_models":["old-model"]}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, `UPDATE openai_cloud_grants SET models='{"old-model":{"text":true,"verified_at":"2026-10-01T00:00:00Z"}}' WHERE account_id=$1`, a.ID)
	require.NoError(t, err)
	raw, err := dbmigrations.FS.ReadFile("293_remove_openai_cloud_verification.sql")
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, string(raw))
	require.NoError(t, err)
	current, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, current.IsOpenAICloudEnabled())
	require.NotContains(t, current.Extra, "openai_cloud_models")
	require.Equal(t, a.Credentials, current.Credentials)
	after, err := repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	rows, err := tx.Client().QueryContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='openai_cloud_grants' AND column_name IN ('models','protocol_version')`)
	require.NoError(t, err)
	require.True(t, rows.Next())
	var retired int
	require.NoError(t, rows.Scan(&retired))
	require.NoError(t, rows.Close())
	require.Zero(t, retired)
	// An ordinary account edit after the migration neither retests models nor
	// removes the user's chosen protocol.
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{"description": "unchanged route"}))
	current, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, current.IsOpenAICloudEnabled())
}

func TestCloudPostgresOverridePreservesBPSAuthorizationAndModelRouting(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	a := cloudAuthorizationAccountFixture()
	require.NoError(t, repo.Create(ctx, a))
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []string{"bps-model"}, "openai_excel_bps_cache_creation_as_input": true}))
	_, err := tx.Client().ExecContext(ctx, `INSERT INTO openai_excel_grants(account_id,ciphertext) VALUES($1,'independent-excel-ciphertext')`, a.ID)
	require.NoError(t, err)
	a, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	savedBPS := map[string]any{}
	for _, key := range []string{"openai_excel_bps", "openai_excel_bps_models", "openai_excel_bps_cache_creation_as_input"} {
		savedBPS[key] = a.Extra[key]
	}
	// Establishing a Cloud execution binding never changes native or Excel credentials.
	b := cloudAuthorizationBindingFixture()
	ok, err := repo.StoreCloudAuthorization(ctx, a, "", b)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true}))
	a, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, a.IsOpenAICloudEnabled())
	require.False(t, a.IsExcelBPSEnabled(), "Cloud is selected without erasing the saved BPS switch")
	for key, value := range savedBPS {
		require.Equal(t, value, a.Extra[key], key)
	}
	b, err = repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	// A concurrent BPS-only edit is independent of the selected Cloud grant.
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{"openai_excel_bps": false}))
	ok, err = repo.StoreCloudAuthorization(ctx, a, b.Generation, b)
	require.NoError(t, err)
	require.True(t, ok)
	current, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, current.IsOpenAICloudEnabled())
	require.NotContains(t, current.Extra, "openai_excel_bps", "explicit BPS disable retains its existing cleanup semantics")
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, savedBPS))
	require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: false}))
	current, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, current.IsOpenAICloudEnabled())
	require.True(t, current.IsExcelBPSEnabled())
	require.True(t, current.IsExcelBPSEnabledForModel("bps-model"))
	require.False(t, current.IsExcelBPSEnabledForModel("native-model"))
	for key, value := range savedBPS {
		require.Equal(t, value, current.Extra[key], key)
	}
	require.Equal(t, "native-access", current.GetCredential("access_token"))
	excel, err := repo.GetExcelCredentials(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, "independent-excel-ciphertext", excel)
	grant, err := repo.GetCloudAuthorization(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, grant.Revoked)
	require.Equal(t, b.Generation, grant.Generation)
}

func TestCloudPostgresLateBPS403CannotChangeAccount(t *testing.T) {
	for _, action := range []string{"disable", "move"} {
		t.Run(action, func(t *testing.T) {
			tx := testEntTx(t)
			ctx := dbent.NewTxContext(t.Context(), tx)
			repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
			a := cloudAuthorizationAccountFixture()
			a.Extra = map[string]any{"openai_excel_bps": true, "openai_excel_bps_auto_disable_on_403": true,
				service.ExcelBPSAutoMoveOn403Key: true, service.ExcelBPS403TargetGroupIDKey: 0}
			require.NoError(t, repo.Create(ctx, a))
			group := mustCreateGroup(t, tx.Client(), &service.Group{Name: "cloud-keep", Platform: service.PlatformOpenAI})
			require.NoError(t, repo.BindGroups(ctx, a.ID, []int64{group.ID}))
			snapshot, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			saved, err := repo.StoreCloudAuthorization(ctx, snapshot, "", cloudAuthorizationBindingFixture())
			require.NoError(t, err)
			require.True(t, saved)
			require.NoError(t, repo.UpdateExtra(ctx, a.ID, map[string]any{service.OpenAICloudEnabledKey: true}))
			before, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			var eventsBefore, eventsAfter int
			require.NoError(t, scanSingleRow(ctx, tx.Client(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", []any{a.ID}, &eventsBefore))
			var changed bool
			if action == "disable" {
				changed, err = repo.DisableExcelBPSOn403(ctx, snapshot)
			} else {
				changed, err = repo.MoveExcelBPSOn403(ctx, snapshot)
			}
			require.NoError(t, err)
			require.False(t, changed, "a late BPS response must honor the current Cloud selection")
			after, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			require.True(t, after.IsOpenAICloudEnabled())
			require.Equal(t, before.Extra, after.Extra)
			require.Equal(t, before.GroupIDs, after.GroupIDs)
			require.NoError(t, scanSingleRow(ctx, tx.Client(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", []any{a.ID}, &eventsAfter))
			require.Equal(t, eventsBefore, eventsAfter)
		})
	}
}

func TestCloudNativeBulkSwitchPreservesEachAccountCredentials(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(t.Context(), tx)
	repo := cloudAuthorizationRepositoryFixture(tx.Client(), tx, nil)
	var accounts []*service.Account
	var ids []int64
	for i := 0; i < 2; i++ {
		a := cloudAuthorizationAccountFixture()
		a.Credentials["access_token"] = fmt.Sprintf("native-%d", i)
		a.Extra["openai_excel_bps"] = true
		require.NoError(t, repo.Create(ctx, a))
		accounts = append(accounts, a)
		ids = append(ids, a.ID)
	}
	for _, enabled := range []bool{true, false} {
		_, err := repo.BulkUpdate(ctx, ids, service.AccountBulkUpdate{Extra: map[string]any{service.OpenAICloudEnabledKey: enabled}})
		require.NoError(t, err)
		for _, original := range accounts {
			current, err := repo.GetByID(ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, enabled, current.IsOpenAICloudEnabled())
			require.Equal(t, original.Credentials, current.Credentials)
			require.Equal(t, true, current.Extra["openai_excel_bps"])
			binding, err := repo.GetCloudAuthorization(ctx, current.ID)
			require.NoError(t, err)
			require.Nil(t, binding, "saving the switch does not require a separate login or issue credentials")
		}
	}
}

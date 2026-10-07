//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func rbacFixture(t *testing.T) (*userRepository, *service.SettingService, authz.Subject, authz.Subject) {
	t.Helper()
	ctx := context.Background()
	var old string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=$1", authz.PolicyKey).Scan(&old))
	_, err := integrationDB.ExecContext(ctx, `UPDATE settings SET value='{"version":1,"permissions":["staff.manage","users.read","users.create","users.update","users.delete","subscriptions.read","subscriptions.manage"]}' WHERE key=$1`, authz.PolicyKey)
	require.NoError(t, err)
	repo := newUserRepositoryWithSQL(testEntClient(t), integrationDB)
	actors := []authz.Subject{}
	for _, role := range []string{authz.SuperAdmin, authz.Admin} {
		u := &service.User{Email: fmt.Sprintf("rbac-%s-%d@example.com", role, time.Now().UnixNano()), PasswordHash: "hash", Role: role, Status: service.StatusActive}
		require.NoError(t, repo.Create(ctx, u))
		actors = append(actors, authz.Subject{UserID: u.ID, Role: role, ManagementRequest: true, PolicyVersion: 1, Permissions: []string{"staff.manage", "users.read", "users.create", "users.update", "users.delete", "subscriptions.read", "subscriptions.manage"}})
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "UPDATE settings SET value=$1 WHERE key=$2", old, authz.PolicyKey)
		for _, a := range actors {
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id=$1", a.UserID)
		}
	})
	return repo, service.NewSettingService(NewSettingRepository(testEntClient(t)), nil), actors[0], actors[1]
}
func TestRBACPolicyCASDoesNotInvalidateAdmittedRequests(t *testing.T) {
	repo, settings, root, admin := rbacFixture(t)
	ctx := authz.WithSubject(context.Background(), root)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := settings.UpdateAdminRolePolicy(ctx, 1, []string{"users.read"}, &service.AuditLog{ActorUserID: &root.UserID, StatusCode: 200})
			results <- err
		}()
	}
	close(start)
	a, b := <-results, <-results
	require.True(t, (a == nil) != (b == nil), "exactly one CAS writer must commit: %v / %v", a, b)
	if a != nil {
		require.ErrorIs(t, a, service.ErrAdminPolicyConflict)
	} else {
		require.ErrorIs(t, b, service.ErrAdminPolicyConflict)
	}
	policy, err := settings.GetAdminRolePolicy(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, policy.Version)
	var audits int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=$1 AND action='admin.roles.permissions.update' AND visibility='super_admin'", root.UserID).Scan(&audits))
	require.Equal(t, 1, audits)
	err = repo.Create(authz.WithSubject(context.Background(), admin), &service.User{Email: fmt.Sprintf("stale-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash", Role: authz.Admin, Status: service.StatusActive})
	require.NoError(t, err)
}
func TestRBACPolicyRecoveryPreservesIdentitySessions(t *testing.T) {
	repo, settings, root, admin := rbacFixture(t)
	ctx := authz.WithSubject(context.Background(), root)
	for _, raw := range []string{"broken", ""} {
		t.Run(raw, func(t *testing.T) {
			if raw == "" {
				_, err := integrationDB.ExecContext(ctx, "DELETE FROM settings WHERE key=$1", authz.PolicyKey)
				require.NoError(t, err)
			} else {
				_, err := integrationDB.ExecContext(ctx, "UPDATE settings SET value=$1 WHERE key=$2", raw, authz.PolicyKey)
				require.NoError(t, err)
			}
			_, err := settings.GetAdminRolePolicy(ctx)
			require.ErrorIs(t, err, service.ErrAdminPolicyUnavailable)
			recovery, err := settings.GetAdminRolePolicyForEdit(ctx)
			require.NoError(t, err)
			require.Zero(t, recovery.Version)
			before, err := repo.GetByID(ctx, admin.UserID)
			require.NoError(t, err)
			_, err = settings.UpdateAdminRolePolicy(ctx, 0, []string{}, &service.AuditLog{ActorUserID: &root.UserID, StatusCode: 200})
			require.NoError(t, err)
			after, err := repo.GetByID(ctx, admin.UserID)
			require.NoError(t, err)
			require.Equal(t, before.SessionGeneration, after.SessionGeneration)
		})
	}
	r := NewSettingRepository(testEntClient(t))
	require.ErrorIs(t, r.Set(ctx, authz.PolicyKey, "{}"), service.ErrAdminPermissionDenied)
	require.ErrorIs(t, r.SetMultiple(ctx, map[string]string{authz.PolicyKey: "{}"}), service.ErrAdminPermissionDenied)
	require.ErrorIs(t, r.Delete(ctx, authz.PolicyKey), service.ErrAdminPermissionDenied)
}
func TestRBACPeerManagementAndPromotionRace(t *testing.T) {
	repo, _, root, admin := rbacFixture(t)
	ctx := authz.WithSubject(context.Background(), admin)
	peer := &service.User{Email: fmt.Sprintf("peer-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash", Role: authz.Admin, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, peer))
	t.Cleanup(func() { _, _ = integrationDB.Exec("DELETE FROM users WHERE id=$1", peer.ID) })
	peer.Username = "managed by peer"
	require.NoError(t, repo.Update(ctx, peer, service.UserUpdateFields{Username: true}))
	// A stale form cannot edit a member after another transaction promotes them.
	_, err := integrationDB.Exec("UPDATE users SET role='super_admin',session_generation=session_generation+1 WHERE id=$1", peer.ID)
	require.NoError(t, err)
	peer.Username = "stale form"
	require.ErrorIs(t, repo.Update(ctx, peer, service.UserUpdateFields{Username: true}), service.ErrAdminPermissionDenied)
	require.ErrorIs(t, repo.Delete(ctx, root.UserID), service.ErrAdminPermissionDenied)
	require.ErrorIs(t, repo.Delete(ctx, admin.UserID), service.ErrAdminPermissionDenied)
	_, err = integrationDB.Exec("UPDATE users SET role='admin' WHERE id=$1", peer.ID)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, peer.ID))
}
func TestRBACConcurrentSuperAdminsCannotRemoveEachOther(t *testing.T) {
	repo, _, root, peer := rbacFixture(t)
	_, err := integrationDB.Exec("UPDATE users SET role='super_admin' WHERE id=$1", peer.UserID)
	require.NoError(t, err)
	peer.Role = authz.SuperAdmin
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pair := range [][2]authz.Subject{{root, peer}, {peer, root}} {
		wg.Add(1)
		go func(actor, target authz.Subject) {
			defer wg.Done()
			<-start
			results <- repo.Update(authz.WithSubject(context.Background(), actor), &service.User{ID: target.UserID, Role: authz.User}, service.UserUpdateFields{Role: true})
		}(pair[0], pair[1])
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
	var count int
	require.NoError(t, integrationDB.QueryRow("SELECT COUNT(*) FROM users WHERE id IN ($1,$2) AND role='super_admin' AND status='active'", root.UserID, peer.UserID).Scan(&count))
	require.Equal(t, 1, count)
}
func TestRBACMigrationPromotesLegacyOnlyOnce(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/270_three_tier_roles.sql")
	require.NoError(t, err)
	tx := testTx(t)
	schema := fmt.Sprintf("rbac_migration_%d", time.Now().UnixNano())
	_, err = tx.Exec("CREATE SCHEMA " + schema + "; SET LOCAL search_path TO " + schema)
	require.NoError(t, err)
	_, err = tx.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT,updated_at TIMESTAMPTZ);
 CREATE TABLE users(id BIGSERIAL PRIMARY KEY, role TEXT, session_generation BIGINT DEFAULT 0);
 CREATE TABLE audit_logs(id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ, extra JSONB);
 CREATE TABLE scheduled_test_plans(id BIGSERIAL PRIMARY KEY, diagnostic_config JSONB);
 CREATE TABLE scheduled_test_results(id BIGSERIAL PRIMARY KEY,plan_id BIGINT,diagnostic_run JSONB);
 INSERT INTO users(role) VALUES ('admin'),('user'); INSERT INTO audit_logs DEFAULT VALUES;
 INSERT INTO scheduled_test_plans DEFAULT VALUES;`)
	require.NoError(t, err)
	_, err = tx.Exec(string(raw))
	require.NoError(t, err)
	var role string
	var generation int
	require.NoError(t, tx.QueryRow("SELECT role,session_generation FROM users WHERE id=1").Scan(&role, &generation))
	require.Equal(t, authz.SuperAdmin, role)
	require.Equal(t, 1, generation)
	_, err = tx.Exec("INSERT INTO users(role) VALUES ('admin')")
	require.NoError(t, err)
	_, err = tx.Exec(string(raw))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow("SELECT role FROM users WHERE id=3").Scan(&role))
	require.Equal(t, authz.Admin, role)
	require.NoError(t, tx.QueryRow("SELECT visibility FROM audit_logs LIMIT 1").Scan(&role))
	require.Equal(t, authz.SuperAdmin, role)
	var policy string
	require.NoError(t, tx.QueryRow("SELECT value FROM settings WHERE key=$1", authz.PolicyKey).Scan(&policy))
	var value authz.Policy
	require.NoError(t, json.Unmarshal([]byte(policy), &value))
	require.NoError(t, value.Validate())
}

func TestRBACAuditScopeIsAppliedBeforePagination(t *testing.T) {
	_, _, root, admin := rbacFixture(t)
	ctx := context.Background()
	repo := NewAuditLogRepository(integrationDB)
	for _, row := range []*service.AuditLog{
		{ActorUserID: &root.UserID, ActorRole: authz.SuperAdmin, Action: "rbac.scope", Visibility: "staff"},
		{ActorUserID: &admin.UserID, ActorRole: authz.Admin, Action: "rbac.scope", Visibility: "staff"},
	} {
		require.NoError(t, repo.Insert(ctx, row))
	}
	list, err := repo.List(ctx, service.AuditScopeStaff, &service.AuditLogFilter{Action: "rbac.scope", Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, 1, list.Total)
	require.Len(t, list.Logs, 1)
	require.Equal(t, authz.Admin, list.Logs[0].ActorRole)
	roots, err := repo.List(ctx, service.AuditScopeAll, &service.AuditLogFilter{Action: "rbac.scope", ActorUserID: &root.UserID})
	require.NoError(t, err)
	require.Len(t, roots.Logs, 1)
	_, err = repo.GetByID(ctx, service.AuditScopeStaff, roots.Logs[0].ID)
	require.Error(t, err)
}
func TestRBACIndirectSubscriptionAndRateTargets(t *testing.T) {
	_, _, root, admin := rbacFixture(t)
	client := testEntClient(t)
	ctx := context.Background()
	group, err := client.Group.Create().SetName("rbac target group").SetPlatform("anthropic").SetSubscriptionType("subscription").Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM user_group_rate_multipliers WHERE group_id=$1", group.ID)
		_, _ = integrationDB.Exec("DELETE FROM groups WHERE id=$1", group.ID)
	})
	subRepo := NewUserSubscriptionRepository(client)
	err = subRepo.Create(authz.WithSubject(ctx, admin), &service.UserSubscription{UserID: root.UserID, GroupID: group.ID, ExpiresAt: time.Now().Add(time.Hour), Status: service.SubscriptionStatusActive})
	require.ErrorIs(t, err, service.ErrAdminPermissionDenied)
	_, err = integrationDB.Exec("INSERT INTO user_group_rate_multipliers(user_id,group_id,rate_multiplier) VALUES($1,$2,0.5)", root.UserID, group.ID)
	require.NoError(t, err)
	rates := NewUserGroupRateRepository(integrationDB)
	// Clearing the complete group also targets super-admin entries omitted by the caller.
	require.ErrorIs(t, rates.SyncGroupRateMultipliers(authz.WithSubject(ctx, admin), group.ID, nil), service.ErrAdminPermissionDenied)
	rate, err := rates.GetByUserAndGroup(ctx, root.UserID, group.ID)
	require.NoError(t, err)
	require.NotNil(t, rate)
	require.Equal(t, .5, *rate)
}
func TestRBACSavedAccountTransportCannotBeRedirected(t *testing.T) {
	_, _, _, admin := rbacFixture(t)
	client := testEntClient(t)
	ctx := context.Background()
	proxy, err := client.Proxy.Create().SetName("rbac proxy").SetProtocol("http").SetHost("127.0.0.1").SetPort(8080).Save(ctx)
	require.NoError(t, err)
	account, err := client.Account.Create().SetName("rbac account").SetPlatform("anthropic").SetType("oauth").SetCredentials(map[string]any{"access_token": "saved-token", "base_url": "https://api.anthropic.com"}).SetExtra(map[string]any{}).SetProxyID(proxy.ID).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM accounts WHERE id=$1", account.ID)
		_, _ = integrationDB.Exec("DELETE FROM proxies WHERE id=$1", proxy.ID)
	})
	repo := newUserRepositoryWithSQL(client, integrationDB)
	adminCtx := authz.WithSubject(ctx, admin)
	require.NoError(t, repo.CheckAdminProxyTransport(adminCtx, []int64{proxy.ID}, map[string]any{"name": "new label"}))
	require.ErrorIs(t, repo.CheckAdminProxyTransport(adminCtx, []int64{proxy.ID}, map[string]any{"host": "attacker.example"}), service.ErrAdminPermissionDenied)
	require.ErrorIs(t, repo.checkAdminAccountTransport(adminCtx, []int64{account.ID}, map[string]any{"credentials": map[string]any{"access_token": "replacement"}}, true), service.ErrAdminPermissionDenied)
	require.ErrorIs(t, repo.CheckAdminAccountTransport(adminCtx, []int64{account.ID}, map[string]any{"rate_multiplier": 2}), service.ErrAdminPermissionDenied)
	require.NoError(t, repo.CheckAdminAccountTransport(adminCtx, []int64{account.ID}, map[string]any{"credentials": map[string]any{"base_url": "https://api.anthropic.com"}}))
	require.ErrorIs(t, repo.CheckAdminAccountTransport(adminCtx, []int64{account.ID}, map[string]any{"credentials": map[string]any{"base_urls": map[string]any{"openai": "https://attacker.example"}}}), service.ErrAdminPermissionDenied)
	require.ErrorIs(t, repo.CheckAdminAccountTransport(adminCtx, []int64{account.ID}, map[string]any{"extra": map[string]any{"tls_insecure_skip_verify": true}}), service.ErrAdminPermissionDenied)
	_, err = client.Account.UpdateOneID(account.ID).ClearProxyID().SetProxyFallbackOriginID(proxy.ID).Save(ctx)
	require.NoError(t, err)
	require.ErrorIs(t, repo.CheckAdminProxyTransport(adminCtx, []int64{proxy.ID}, map[string]any{"host": "attacker.example"}), service.ErrAdminPermissionDenied)

}

func TestRBACRestorePsqlRollsBackOnError(t *testing.T) {
	// Exercise the production command builder using psql inside our existing
	// isolated PostgreSQL fixture; no system-wide client installation is needed.
	docker, err := exec.LookPath("docker")
	require.NoError(t, err)
	bin := t.TempDir()
	wrapper := filepath.Join(bin, "psql")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	require.NoError(t, os.WriteFile(wrapper, []byte("#!/bin/sh\nexec "+quote(docker)+" exec -i "+quote(integrationPgContainerID)+" psql \"$@\"\n"), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err = integrationDB.Exec("CREATE TABLE rbac_restore_probe(value INTEGER); INSERT INTO rbac_restore_probe VALUES(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = integrationDB.Exec("DROP TABLE rbac_restore_probe") })
	dumper := &PgDumper{cfg: &config.DatabaseConfig{Host: "", Port: 5432, User: "postgres", DBName: "sub2api_test", SSLMode: "disable"}}
	err = dumper.Restore(context.Background(), strings.NewReader("UPDATE rbac_restore_probe SET value=2; SELECT * FROM missing_rbac_restore_table;"))
	require.Error(t, err)
	var value int
	require.NoError(t, integrationDB.QueryRow("SELECT value FROM rbac_restore_probe").Scan(&value))
	require.Equal(t, 1, value)
	require.NoError(t, dumper.Restore(context.Background(), strings.NewReader("UPDATE rbac_restore_probe SET value=3;")))
	require.NoError(t, integrationDB.QueryRow("SELECT value FROM rbac_restore_probe").Scan(&value))
	require.Equal(t, 3, value)
}

func TestRBACTOTPResetRevokesTokensAndPendingAuthentication(t *testing.T) {
	repo, settings, _, actor := rbacFixture(t)
	ctx := context.Background()
	client := testEntClient(t)
	actor.Permissions = append(actor.Permissions, "users.security")
	_, err := integrationDB.Exec(`UPDATE settings SET value='{"version":1,"permissions":["staff.manage","users.read","users.update","users.security"]}' WHERE key=$1`, authz.PolicyKey)
	require.NoError(t, err)
	target, err := client.User.Create().SetEmail(fmt.Sprintf("mfa-%d@example.com", time.Now().UnixNano())).SetPasswordHash("hash").SetRole(authz.Admin).SetTotpEnabled(true).SetTotpSecretEncrypted("encrypted-test-secret").Save(ctx)
	require.NoError(t, err)
	pending, err := client.PendingAuthSession.Create().SetSessionToken(fmt.Sprintf("pending-%d", target.ID)).SetIntent("login").SetProviderType("oidc").SetProviderKey("fixture").SetProviderSubject("subject").SetTargetUserID(target.ID).SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM pending_auth_sessions WHERE target_user_id=$1", target.ID)
		_, _ = integrationDB.Exec("DELETE FROM users WHERE id=$1", target.ID)
	})
	before, err := repo.GetByID(ctx, target.ID)
	require.NoError(t, err)
	auth := service.NewAuthService(nil, repo, nil, nil, &config.Config{JWT: config.JWTConfig{Secret: "rbac-mfa-test", ExpireHour: 1}}, settings, nil, nil, nil, nil, nil, nil, nil)
	token, err := auth.GenerateToken(ctx, before)
	require.NoError(t, err)
	claims, err := auth.ValidateToken(token)
	require.NoError(t, err)
	svc := service.NewAdminService(nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, client, settings, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err = svc.UpdateUser(authz.WithSubject(ctx, actor), target.ID, &service.UpdateUserInput{ResetTOTP: true})
	require.NoError(t, err)
	after, err := repo.GetByID(ctx, target.ID)
	require.NoError(t, err)
	require.False(t, after.TotpEnabled)
	require.Nil(t, after.TotpSecretEncrypted)
	require.Greater(t, after.SessionGeneration, before.SessionGeneration)
	require.ErrorIs(t, auth.ValidateAccessSession(ctx, claims, after), service.ErrTokenRevoked)
	stored, err := client.PendingAuthSession.Get(ctx, pending.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.ConsumedAt)
	var auditCount int
	require.NoError(t, integrationDB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=$1 AND action='admin.user.mfa.reset' AND visibility='staff'", actor.UserID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestRBACBatchDeleteRejectsProtectedSelectionWithoutPartialWrites(t *testing.T) {
	repo, settings, root, actor := rbacFixture(t)
	client := testEntClient(t)
	ctx := context.Background()
	target, err := client.User.Create().SetEmail(fmt.Sprintf("batch-%d@example.com", time.Now().UnixNano())).SetPasswordHash("hash").SetRole(authz.User).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = integrationDB.Exec("DELETE FROM users WHERE id=$1", target.ID) })
	svc := service.NewAdminService(nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, client, settings, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	requestCtx := authz.WithSubject(ctx, actor)
	require.ErrorIs(t, svc.DeleteUsers(requestCtx, []int64{target.ID, root.UserID}), service.ErrAdminPermissionDenied)
	stillPresent, err := repo.GetByID(ctx, target.ID)
	require.NoError(t, err)
	require.Equal(t, target.ID, stillPresent.ID)
	require.ErrorIs(t, svc.DeleteUsers(requestCtx, []int64{target.ID, actor.UserID}), service.ErrAdminPermissionDenied)
	require.NoError(t, svc.DeleteUsers(requestCtx, []int64{target.ID, target.ID}))
	_, err = repo.GetByID(ctx, target.ID)
	require.ErrorIs(t, err, service.ErrUserNotFound)
}

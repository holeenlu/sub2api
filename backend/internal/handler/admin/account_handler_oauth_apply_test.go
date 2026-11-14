package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Preserve the existing request-validation/credential-merge tests' narrow
// service double. Transactional behavior below exercises the real admin service.
func (s *stubAdminService) ApplyAccountOAuthCredentials(ctx context.Context, id int64, input *service.ApplyAccountOAuthCredentialsInput) (*service.Account, error) {
	account, err := s.UpdateAccount(ctx, id, &service.UpdateAccountInput{
		Type: input.Type, Credentials: input.Credentials, ExpiresAt: input.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	if len(input.Extra) > 0 {
		if err := s.UpdateAccountExtra(ctx, id, input.Extra); err != nil {
			return nil, err
		}
	}
	return account, nil
}

const oauthApplyDisabledAt = "2026-10-27T01:02:03.000004Z"

type oauthApplyStage struct {
	account *service.Account
	writes  int
}

// This repository double stages state against real Ent transaction hooks.
// It is not PostgreSQL evidence: production row locking/outbox/identity claims
// remain covered by the repository integration selectors in the handoff.
type oauthApplyRepository struct {
	service.AdminAccountRepository
	t                 *testing.T
	account           *service.Account
	stages            map[*dbent.Tx]*oauthApplyStage
	writeErr          error
	extraErr          error
	readAfterExtraErr error
	clearErr          error
	committedWrites   int
	snapshotPublishes int
	clearSteps        []string
	extraCalls        int
}

func cloneOAuthApplyAccount(account *service.Account) *service.Account {
	cloned := *account
	cloned.Credentials = maps.Clone(account.Credentials)
	cloned.Extra = maps.Clone(account.Extra)
	return &cloned
}

func (r *oauthApplyRepository) stage(ctx context.Context) *oauthApplyStage {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return nil
	}
	if stage := r.stages[tx]; stage != nil {
		return stage
	}
	stage := &oauthApplyStage{account: cloneOAuthApplyAccount(r.account)}
	r.stages[tx] = stage
	tx.OnCommit(func(next dbent.Committer) dbent.Committer {
		return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
			if err := next.Commit(ctx, tx); err != nil {
				return err
			}
			r.account = cloneOAuthApplyAccount(stage.account)
			r.committedWrites += stage.writes
			delete(r.stages, tx)
			return nil
		})
	})
	tx.OnRollback(func(next dbent.Rollbacker) dbent.Rollbacker {
		return dbent.RollbackFunc(func(ctx context.Context, tx *dbent.Tx) error {
			delete(r.stages, tx)
			return next.Rollback(ctx, tx)
		})
	})
	return stage
}

func (r *oauthApplyRepository) current(ctx context.Context) *service.Account {
	if stage := r.stage(ctx); stage != nil {
		return stage.account
	}
	return r.account
}

func (r *oauthApplyRepository) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	require.Equal(r.t, r.account.ID, id)
	if r.extraCalls > 0 && r.readAfterExtraErr != nil {
		return nil, r.readAfterExtraErr
	}
	return cloneOAuthApplyAccount(r.current(ctx)), nil
}

func (r *oauthApplyRepository) UpdateWithAccountBillingSettings(ctx context.Context, account *service.Account, probe, rateSync *bool, rate *float64) error {
	require.Nil(r.t, probe)
	require.Nil(r.t, rateSync)
	require.Nil(r.t, rate)
	stage := r.stage(ctx)
	require.NotNil(r.t, stage, "credential writes must share the extra transaction")
	if r.writeErr != nil {
		return r.writeErr
	}
	extra, err := service.MergeExcelBPSSettingsUpdate(account.Extra, stage.account.Extra)
	if err != nil {
		return err
	}
	stage.account = cloneOAuthApplyAccount(account)
	stage.account.Extra = extra
	stage.writes++
	return nil
}

func (r *oauthApplyRepository) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	require.Equal(r.t, r.account.ID, id)
	r.extraCalls++
	if r.extraErr != nil {
		return r.extraErr
	}
	current := r.current(ctx)
	settings, err := service.MergeExcelBPSSettingsUpdate(updates, current.Extra)
	if err != nil {
		return err
	}
	merged := maps.Clone(current.Extra)
	for _, key := range []string{service.ExcelBPS403DisabledAtKey, service.ExcelBPS403LastProbeAtKey, service.ExcelBPS403LastProbeResultKey, service.ExcelBPSFailureKey} {
		delete(merged, key)
	}
	maps.Copy(merged, settings)
	current.Extra = merged
	if stage := r.stage(ctx); stage != nil {
		stage.writes++
	}
	return nil
}

func (r *oauthApplyRepository) ScopedAdminCommitted(ctx context.Context, id int64, deleted bool) {
	require.Nil(r.t, dbent.TxFromContext(ctx), "snapshot publication must not retain a transaction")
	require.Equal(r.t, r.account.ID, id)
	require.False(r.t, deleted)
	r.snapshotPublishes++
}

func (r *oauthApplyRepository) ClearError(ctx context.Context, _ int64) error {
	r.clearSteps = append(r.clearSteps, "error")
	if r.clearErr != nil {
		return r.clearErr
	}
	account := r.current(ctx)
	account.Status = service.StatusActive
	account.ErrorMessage = ""
	return nil
}
func (r *oauthApplyRepository) ClearRateLimit(context.Context, int64) error {
	r.clearSteps = append(r.clearSteps, "rate_limit")
	return nil
}
func (r *oauthApplyRepository) ClearAntigravityQuotaScopes(context.Context, int64) error {
	r.clearSteps = append(r.clearSteps, "antigravity_quota")
	return nil
}
func (r *oauthApplyRepository) ClearModelRateLimits(context.Context, int64) error {
	r.clearSteps = append(r.clearSteps, "model_rate_limits")
	return nil
}
func (r *oauthApplyRepository) ClearTempUnschedulable(context.Context, int64) error {
	r.clearSteps = append(r.clearSteps, "temp_unschedulable")
	return nil
}

type oauthApplyInvalidator struct {
	t        *testing.T
	accounts []*service.Account
	err      error
}

func (c *oauthApplyInvalidator) InvalidateToken(ctx context.Context, account *service.Account) error {
	require.Nil(c.t, dbent.TxFromContext(ctx), "token invalidation must run after commit")
	c.accounts = append(c.accounts, cloneOAuthApplyAccount(account))
	return c.err
}

func newOAuthApplyFixture(t *testing.T) (*AccountHandler, *oauthApplyRepository, *oauthApplyInvalidator, *dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := &oauthApplyRepository{
		t: t, stages: make(map[*dbent.Tx]*oauthApplyStage),
		account: &service.Account{
			ID: 7, Name: "reauthorize", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Status: service.StatusError, ErrorMessage: "reauthorization required",
			Credentials: map[string]any{"access_token": "old-token", "refresh_token": "old-refresh", "plan_type": "plus", "model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"}, "password": "removed", "sso_token": "removed"},
			Extra: map[string]any{
				"openai_excel_bps": false, "openai_excel_bps_models": []string{"gpt-6-astra"},
				service.ExcelBPS403DisabledAtKey: oauthApplyDisabledAt, service.ExcelBPS403LastProbeAtKey: oauthApplyDisabledAt,
				service.ExcelBPSFailureKey:    map[string]any{"source": "system", "state": "disabled", "occurred_at": oauthApplyDisabledAt},
				service.ExcelBPS403MovedAtKey: oauthApplyDisabledAt,
				"base_rpm":                    60, "window_cost_limit": 12, "max_sessions": 3, "quota_used": 5, "privacy_mode": "private",
			},
		},
	}
	svc := service.NewAdminService(
		nil, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		client, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	cache := &oauthApplyInvalidator{t: t}
	return &AccountHandler{adminService: svc, tokenCacheInvalidator: cache}, repo, cache, client, mock
}

func serveOAuthApply(t *testing.T, h *AccountHandler, ctx context.Context, extra map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(ApplyOAuthCredentialsRequest{
		Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "new-token", "refresh_token": "new-refresh"}, Extra: extra,
	})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/api/v1/admin/accounts/:id/apply-oauth-credentials", h.ApplyOAuthCredentials)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/7/apply-oauth-credentials", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestApplyOAuthCredentialsBPSConflictRollsBackCredentials(t *testing.T) {
	for _, expected := range []string{"missing", "", "2026-10-27T01:02:02Z"} {
		t.Run(expected, func(t *testing.T) {
			h, repo, cache, _, mock := newOAuthApplyFixture(t)
			before := cloneOAuthApplyAccount(repo.account)
			extra := map[string]any{"openai_excel_bps": true, "privacy_mode": "updated"}
			if expected != "missing" {
				extra[service.ExcelBPSExpectedDisabledAtKey] = expected
			}
			mock.ExpectBegin()
			mock.ExpectRollback()
			response := serveOAuthApply(t, h, t.Context(), extra)
			require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "OPENAI_EXCEL_BPS_STATE_CONFLICT")
			require.Equal(t, before, repo.account)
			require.Equal(t, 1, repo.extraCalls, "exercise CAS after the staged credential write")
			require.Zero(t, repo.committedWrites)
			require.Zero(t, repo.snapshotPublishes)
			require.Empty(t, repo.clearSteps)
			require.Empty(t, cache.accounts)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestApplyOAuthCredentialsBPSExpectedStateCommitsMergedExtra(t *testing.T) {
	h, repo, cache, _, mock := newOAuthApplyFixture(t)
	mock.ExpectBegin()
	mock.ExpectCommit()
	response := serveOAuthApply(t, h, t.Context(), map[string]any{
		"openai_excel_bps": true, service.ExcelBPSExpectedDisabledAtKey: oauthApplyDisabledAt, "privacy_mode": "updated",
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "new-token", repo.account.Credentials["access_token"])
	require.Equal(t, "new-refresh", repo.account.Credentials["refresh_token"])
	require.Contains(t, repo.account.Credentials, "model_mapping")
	require.NotContains(t, repo.account.Credentials, "password")
	require.NotContains(t, repo.account.Credentials, "sso_token")
	require.Equal(t, true, repo.account.Extra["openai_excel_bps"])
	require.Equal(t, "updated", repo.account.Extra["privacy_mode"])
	for key, value := range map[string]any{"base_rpm": 60, "window_cost_limit": 12, "max_sessions": 3, "quota_used": 5, service.ExcelBPS403MovedAtKey: oauthApplyDisabledAt} {
		require.Equal(t, value, repo.account.Extra[key], key)
	}
	for _, key := range []string{service.ExcelBPSExpectedDisabledAtKey, service.ExcelBPS403DisabledAtKey, service.ExcelBPS403LastProbeAtKey, service.ExcelBPSFailureKey} {
		require.NotContains(t, repo.account.Extra, key)
	}
	require.Equal(t, 2, repo.committedWrites)
	require.Equal(t, 1, repo.snapshotPublishes)
	require.Equal(t, []string{"error", "rate_limit", "antigravity_quota", "model_rate_limits", "temp_unschedulable"}, repo.clearSteps)
	require.Equal(t, service.StatusActive, repo.account.Status)
	require.Len(t, cache.accounts, 1)
	require.Equal(t, "new-token", cache.accounts[0].Credentials["access_token"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyOAuthCredentialsOmittedBPSSwitchPreservesRecovery(t *testing.T) {
	h, repo, cache, _, mock := newOAuthApplyFixture(t)
	markers := maps.Clone(repo.account.Extra)
	mock.ExpectBegin()
	mock.ExpectCommit()
	response := serveOAuthApply(t, h, t.Context(), map[string]any{"privacy_mode": "updated"})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	for _, key := range []string{"openai_excel_bps", "openai_excel_bps_models", service.ExcelBPS403DisabledAtKey, service.ExcelBPS403LastProbeAtKey, service.ExcelBPSFailureKey} {
		require.Equal(t, markers[key], repo.account.Extra[key], "reauth/clear-error must not authorize BPS recovery: "+key)
	}
	require.Equal(t, "new-token", repo.account.Credentials["access_token"])
	require.Equal(t, "updated", repo.account.Extra["privacy_mode"])
	require.Len(t, cache.accounts, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyOAuthCredentialsWriteFailuresDoNotClearOrPublish(t *testing.T) {
	for _, failure := range []string{"permission", "supply_identity", "extra_write", "readback", "invalid_extra"} {
		t.Run(failure, func(t *testing.T) {
			h, repo, cache, _, mock := newOAuthApplyFixture(t)
			extra := map[string]any{"privacy_mode": "updated"}
			status := http.StatusInternalServerError
			switch failure {
			case "permission":
				repo.writeErr = authz.ErrPermissionDenied
				status = http.StatusForbidden
			case "supply_identity":
				repo.writeErr = infraerrors.Conflict("SUPPLY_IDENTITY_CONFLICT", "identity already claimed")
				status = http.StatusConflict
			case "extra_write":
				repo.extraErr = errors.New("extra storage unavailable")
			case "readback":
				repo.readAfterExtraErr = errors.New("readback unavailable")
			case "invalid_extra":
				extra["openai_excel_bps"] = "true"
				status = http.StatusBadRequest
			}
			before := cloneOAuthApplyAccount(repo.account)
			mock.ExpectBegin()
			mock.ExpectRollback()
			response := serveOAuthApply(t, h, t.Context(), extra)
			require.Equal(t, status, response.Code, response.Body.String())
			require.Equal(t, before, repo.account)
			require.Zero(t, repo.committedWrites)
			require.Zero(t, repo.snapshotPublishes)
			require.Empty(t, repo.clearSteps)
			require.Empty(t, cache.accounts)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestApplyOAuthCredentialsCommittedWriteSurvivesCleanupFailure(t *testing.T) {
	for _, failure := range []string{"clear_error", "token_cache", "grok_flags"} {
		t.Run(failure, func(t *testing.T) {
			h, repo, cache, _, mock := newOAuthApplyFixture(t)
			extra := map[string]any{"privacy_mode": "updated"}
			writes := 2
			switch failure {
			case "clear_error":
				repo.clearErr = errors.New("clear unavailable")
			case "token_cache":
				cache.err = errors.New("cache unavailable")
			case "grok_flags":
				repo.account.Platform = service.PlatformGrok
				repo.account.Extra = map[string]any{"grok_needs_reauth": true}
				repo.extraErr = errors.New("flag clear unavailable")
				extra, writes = nil, 1
			}
			mock.ExpectBegin()
			mock.ExpectCommit()
			response := serveOAuthApply(t, h, t.Context(), extra)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, "new-token", repo.account.Credentials["access_token"])
			require.Equal(t, writes, repo.committedWrites)
			require.Equal(t, 1, repo.snapshotPublishes)
			require.Len(t, cache.accounts, 1, "cleanup failure must not skip cache eviction")
			require.Equal(t, "new-token", cache.accounts[0].Credentials["access_token"])
			if failure == "clear_error" {
				require.Equal(t, service.StatusError, repo.account.Status)
				require.Contains(t, response.Body.String(), "reauthorization required")
				require.Equal(t, "updated", repo.account.Extra["privacy_mode"])
				require.Contains(t, response.Body.String(), "updated", "return the persisted extra even when clear-error cannot read it back")
			}
			if failure == "grok_flags" {
				require.Equal(t, true, repo.account.Extra["grok_needs_reauth"])
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

type oauthApplyWithoutAtomicPort struct{ service.AdminService }

func TestApplyOAuthCredentialsRequiresAtomicPersistence(t *testing.T) {
	h, repo, cache, _, mock := newOAuthApplyFixture(t)
	// Wrapping only the broad interface intentionally hides the dedicated port.
	h.adminService = &oauthApplyWithoutAtomicPort{h.adminService}
	response := serveOAuthApply(t, h, t.Context(), map[string]any{"privacy_mode": "updated"})
	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "OAUTH_APPLY_UNAVAILABLE")
	require.Zero(t, repo.committedWrites)
	require.Zero(t, repo.extraCalls)
	require.Empty(t, repo.clearSteps)
	require.Empty(t, cache.accounts)
	require.NoError(t, mock.ExpectationsWereMet(), "missing atomic capability must fail before opening a transaction")
}

func TestApplyOAuthCredentialsCommitFailureEvictsWithoutSuccessOrClearing(t *testing.T) {
	h, repo, cache, _, mock := newOAuthApplyFixture(t)
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(errors.New("commit acknowledgement lost"))
	response := serveOAuthApply(t, h, t.Context(), nil)
	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	require.Empty(t, repo.clearSteps)
	require.Zero(t, repo.snapshotPublishes)
	require.Len(t, cache.accounts, 1, "an uncertain commit may already have saved the new token")
	require.Equal(t, "new-token", cache.accounts[0].Credentials["access_token"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyOAuthCredentialsOuterTransactionDefersSideEffects(t *testing.T) {
	for _, finish := range []string{"commit", "rollback", "commit_failure"} {
		t.Run(finish, func(t *testing.T) {
			h, repo, cache, client, mock := newOAuthApplyFixture(t)
			mock.ExpectBegin()
			tx, err := client.Tx(t.Context())
			require.NoError(t, err)
			response := serveOAuthApply(t, h, dbent.NewTxContext(t.Context(), tx), nil)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Empty(t, cache.accounts)
			require.Zero(t, repo.snapshotPublishes)
			require.Zero(t, repo.committedWrites)
			switch finish {
			case "commit":
				mock.ExpectCommit()
				require.NoError(t, tx.Commit())
				require.Len(t, cache.accounts, 1)
				require.Equal(t, "new-token", cache.accounts[0].Credentials["access_token"])
				require.Equal(t, 1, repo.snapshotPublishes)
				require.Equal(t, 1, repo.committedWrites)
			case "rollback":
				mock.ExpectRollback()
				require.NoError(t, tx.Rollback())
				require.Empty(t, cache.accounts)
				require.Zero(t, repo.snapshotPublishes)
				require.Equal(t, "old-token", repo.account.Credentials["access_token"])
			case "commit_failure":
				mock.ExpectCommit().WillReturnError(errors.New("outer commit failed"))
				require.Error(t, tx.Commit())
				require.Empty(t, cache.accounts)
				require.Zero(t, repo.snapshotPublishes)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

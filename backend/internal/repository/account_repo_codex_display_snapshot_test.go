package repository

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpdateExtraQuotaDisplaySnapshotsAvoidSchedulerOutbox(t *testing.T) {
	for _, key := range []string{
		"codex_credits_snapshot", "codex_referral_snapshot", "codex_reset_credit_snapshot", "codex_5h_used_percent",
		"session_window_utilization", "passive_usage_7d_utilization", "passive_usage_7d_oi_utilization",
		"claude_reset_credit_snapshot",
	} {
		for _, management := range []bool{false, true} {
			for _, tc := range []struct {
				name             string
				value            any
				schedulingChange bool
			}{
				{"refresh", map[string]any{"fetched_at": 1770000000}, false},
				{"invalidate", nil, false},
				{"mixed scheduling update", nil, true},
			} {
				mode := "background"
				if management {
					mode = "management"
				}
				t.Run(key+"/"+mode+"/"+tc.name, func(t *testing.T) {
					db, mock, err := sqlmock.New()
					require.NoError(t, err)
					client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
					t.Cleanup(func() { _ = client.Close() })
					repo := newAccountRepositoryWithSQL(client, db, nil)
					updates := map[string]any{key: tc.value}
					if tc.schedulingChange {
						updates["openai_compact_enabled"] = true
					}
					if tc.schedulingChange || management {
						mock.ExpectBegin()
					}
					payload, err := json.Marshal(updates)
					require.NoError(t, err)
					updateSQL := "UPDATE accounts SET extra = (COALESCE(extra, '{}'::jsonb) || $1::jsonb) - 'openai_excel_bps_config_mode', updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL AND COALESCE(extra->>'vendor_archived_at','')=''"
					if management {
						updateSQL += " AND (vendor_id IS NULL OR EXISTS (SELECT 1 FROM users va WHERE va.id=0 AND va.role='super_admin' AND va.status='active' AND va.deleted_at IS NULL AND va.session_generation=0))"
					}
					mock.ExpectExec("^"+regexp.QuoteMeta(updateSQL)+"$").
						WithArgs(string(payload), int64(27)).WillReturnResult(sqlmock.NewResult(0, 1))
					if tc.schedulingChange {
						mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
							WithArgs(service.SchedulerOutboxEventAccountChanged, int64(27), nil, nil, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
					}
					if tc.schedulingChange || management {
						mock.ExpectCommit()
					}
					// Background snapshots stay lightweight; management snapshots must
					// commit their transaction without rebuilding scheduler buckets.
					ctx := context.Background()
					if management {
						ctx = authz.WithSubject(ctx, authz.Subject{Role: authz.SuperAdmin, ManagementRequest: true})
					}
					require.NoError(t, repo.UpdateExtra(ctx, 27, updates))
					require.NoError(t, mock.ExpectationsWereMet())
				})
			}
		}
	}
}

func TestUpdateExtraDisplaySnapshotTransactionOwnershipAndFailures(t *testing.T) {
	for _, mode := range []string{"outer transaction", "write failure", "commit failure", "account missing"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newAccountRepositoryWithSQL(client, db, nil)
			ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.SuperAdmin, ManagementRequest: true})
			mock.ExpectBegin()
			var outer *dbent.Tx
			if mode == "outer transaction" {
				outer, err = client.Tx(ctx)
				require.NoError(t, err)
				ctx = dbent.NewTxContext(ctx, outer)
			}
			update := mock.ExpectExec("UPDATE accounts SET extra").WithArgs(sqlmock.AnyArg(), int64(27))
			sentinel := errors.New("database write/commit failed")
			switch mode {
			case "write failure":
				update.WillReturnError(sentinel)
				mock.ExpectRollback()
			case "account missing":
				update.WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectRollback()
			default:
				update.WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if mode == "commit failure" {
				mock.ExpectCommit().WillReturnError(sentinel)
			}
			updates := map[string]any{"codex_reset_credit_snapshot": map[string]any{"available_count": 3, "credits": []any{map[string]any{"expires_at": "2026-11-07T00:00:00Z"}}}}
			err = repo.UpdateExtra(ctx, 27, updates)
			switch mode {
			case "outer transaction":
				require.NoError(t, err)
				// The caller can still roll back; the repository must not commit its transaction.
				mock.ExpectRollback()
				require.NoError(t, outer.Rollback())
			case "account missing":
				require.ErrorIs(t, err, service.ErrAccountNotFound)
			default:
				require.ErrorIs(t, err, sentinel)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpdateExtraDisplaySnapshotScopesTheSingleWrite(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin})
	mock.ExpectExec("UPDATE accounts SET extra .* AND vendor_id IS NULL$").
		WithArgs(sqlmock.AnyArg(), int64(27)).WillReturnResult(sqlmock.NewResult(0, 0))
	require.ErrorIs(t, repo.UpdateExtra(ctx, 27, map[string]any{"codex_credits_snapshot": nil}), service.ErrAccountNotFound)
	// No preflight query, transaction, outbox write or cache publication is allowed.
}

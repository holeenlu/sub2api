package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSCooldownRepositoryMonotonicWriteAndOutbox(t *testing.T) {
	const accountID = int64(42)
	const scope = service.ExcelBPSCooldownScopePrefix + "429"
	until := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Microsecond)
	for _, tc := range []struct {
		name      string
		affected  int64
		outboxErr error
		commitErr error
	}{
		{name: "extended", affected: 1},
		{name: "shorter ignored", affected: 0},
		{name: "outbox failure rolls back", affected: 1, outboxErr: fmt.Errorf("outbox unavailable")},
		{name: "unknown commit is not success", affected: 1, commitErr: fmt.Errorf("commit acknowledgement lost")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE accounts SET.*model_rate_limits.*rate_limit_reset_at.*timestamptz <").
				WithArgs(scope, sqlmock.AnyArg(), accountID, until).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			want := until
			if tc.affected == 0 {
				want = until.Add(time.Hour)
				mock.ExpectQuery("SELECT \\(SELECT MAX").
					WithArgs(accountID, pq.Array([]string{scope})).
					WillReturnRows(sqlmock.NewRows([]string{"not_before"}).AddRow(want)).RowsWillBeClosed()
			} else {
				outbox := mock.ExpectExec("INSERT INTO scheduler_outbox").
					WithArgs(service.SchedulerOutboxEventAccountChanged, accountID, nil, nil, sqlmock.AnyArg())
				if tc.outboxErr != nil {
					outbox.WillReturnError(tc.outboxErr)
				} else {
					outbox.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			if tc.outboxErr != nil {
				mock.ExpectRollback()
			} else if tc.commitErr != nil {
				mock.ExpectCommit().WillReturnError(tc.commitErr)
			} else {
				mock.ExpectCommit()
			}
			got, err := repo.ExtendExcelBPSCooldown(t.Context(), accountID, scope, until)
			switch {
			case tc.outboxErr != nil:
				require.ErrorIs(t, err, tc.outboxErr)
				require.True(t, got.IsZero())
			case tc.commitErr != nil:
				require.ErrorIs(t, err, tc.commitErr)
				require.True(t, got.IsZero())
			default:
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		})
	}
}

func TestExcelBPSCooldownRepositoryReadDistinguishesMissingAndUnavailable(t *testing.T) {
	scope := service.ExcelBPSCooldownScopePrefix + "429"
	for _, mode := range []string{"absent scope", "absent account", "query failure", "scan failure"} {
		t.Run(mode, func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			query := mock.ExpectQuery("SELECT \\(SELECT MAX").WithArgs(int64(42), pq.Array([]string{scope}))
			switch mode {
			case "absent scope":
				query.WillReturnRows(sqlmock.NewRows([]string{"not_before"}).AddRow(nil)).RowsWillBeClosed()
			case "absent account":
				query.WillReturnRows(sqlmock.NewRows([]string{"not_before"})).RowsWillBeClosed()
			case "query failure":
				query.WillReturnError(fmt.Errorf("database unavailable"))
			case "scan failure":
				query.WillReturnRows(sqlmock.NewRows([]string{"not_before"}).AddRow("invalid timestamp")).RowsWillBeClosed()
			}
			until, err := repo.GetExcelBPSCooldown(t.Context(), 42, []string{scope})
			require.True(t, until.IsZero())
			if mode == "absent scope" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				if mode == "absent account" {
					require.ErrorIs(t, err, service.ErrAccountNotFound)
				}
			}
		})
	}
}

func TestExcelBPSCooldownRepositoryRejectsUnrelatedScope(t *testing.T) {
	repo, _ := supplyRepoMock(t)
	for _, scope := range []string{"", "gpt-6-astra", "openai:image_generation", "cloud:429"} {
		_, err := repo.ExtendExcelBPSCooldown(t.Context(), 42, scope, time.Now().Add(time.Hour))
		require.Error(t, err)
		_, err = repo.GetExcelBPSCooldown(t.Context(), 42, []string{scope})
		require.Error(t, err)
	}
	_, err := repo.GetExcelBPSCooldown(t.Context(), 42, nil)
	require.Error(t, err)
}

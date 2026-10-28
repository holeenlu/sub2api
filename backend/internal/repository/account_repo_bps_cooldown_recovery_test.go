package repository

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBPSCooldownRecoveryRepositoryPublishesOnlyCommittedCleanup(t *testing.T) {
	for _, mode := range []string{"committed", "update failure", "missing account", "outbox failure", "commit failure"} {
		t.Run(mode, func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			failure := errors.New(mode)
			mock.ExpectBegin()
			update := mock.ExpectExec("UPDATE accounts AS a SET.*jsonb_each.*").WithArgs(int64(42), service.ExcelBPSCooldownScopePrefix)
			switch mode {
			case "update failure":
				update.WillReturnError(failure)
			case "missing account":
				update.WillReturnResult(sqlmock.NewResult(0, 0))
			default:
				update.WillReturnResult(sqlmock.NewResult(0, 1))
				outbox := mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(service.SchedulerOutboxEventAccountChanged, int64(42), nil, nil, sqlmock.AnyArg())
				if mode == "outbox failure" {
					outbox.WillReturnError(failure)
				} else {
					outbox.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			switch mode {
			case "committed":
				mock.ExpectCommit()
			case "commit failure":
				mock.ExpectCommit().WillReturnError(failure)
			default:
				mock.ExpectRollback()
			}
			err := repo.ClearModelRateLimitsPreservingBPS(t.Context(), 42)
			if mode == "committed" {
				require.NoError(t, err)
			} else if mode == "missing account" {
				require.ErrorIs(t, err, service.ErrAccountNotFound)
			} else {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

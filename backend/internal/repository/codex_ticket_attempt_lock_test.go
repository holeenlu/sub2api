package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketTryLockDiscardsUncertainAcquisition(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).WillReturnError(errors.New("reply interrupted after possible lock"))
	mock.ExpectClose()
	repo := &codexTicketAttemptRepository{db: db}
	unlock, acquired, err := repo.TryLock(context.Background(), 41, "gpt-6-astra")
	require.Error(t, err)
	require.False(t, acquired)
	require.Nil(t, unlock)
	require.Zero(t, db.Stats().OpenConnections, "a possibly lock-owning session must leave the pool")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexTicketTryLockReleasePoolState(t *testing.T) {
	for _, outcome := range []string{"released", "unlock error", "ownership missing", "busy"} {
		t.Run(outcome, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectQuery(`SELECT pg_try_advisory_lock`).WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(outcome != "busy"))
			discarded := outcome == "unlock error" || outcome == "ownership missing"
			if outcome != "busy" {
				query := mock.ExpectQuery(`SELECT pg_advisory_unlock`)
				if outcome == "unlock error" {
					query.WillReturnError(errors.New("unlock reply lost"))
				} else {
					query.WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(outcome != "ownership missing"))
				}
			}
			if discarded {
				mock.ExpectClose()
			}
			repo := &codexTicketAttemptRepository{db: db}
			unlock, acquired, err := repo.TryLock(context.Background(), 41, "gpt-6-astra")
			require.NoError(t, err)
			require.Equal(t, outcome != "busy", acquired)
			if acquired {
				require.Equal(t, 1, db.Stats().InUse)
				unlock()
			} else {
				require.Nil(t, unlock)
			}
			require.Zero(t, db.Stats().InUse)
			if discarded {
				require.Zero(t, db.Stats().OpenConnections)
			} else {
				require.Equal(t, 1, db.Stats().Idle, "a proven clean session remains reusable")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCodexTicketCleanupDiscardsUncertainAcquisition(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).WithArgs(codexTicketMaintenanceLock).WillReturnError(errors.New("lock reply lost"))
	mock.ExpectClose()
	err = (&codexTicketAttemptRepository{db: db}).Cleanup(context.Background())
	require.Error(t, err)
	require.Zero(t, db.Stats().OpenConnections)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexTicketCleanupDoesNotReturnDirtySession(t *testing.T) {
	for _, outcome := range []string{"released", "reset error", "unlock error", "ownership missing"} {
		t.Run(outcome, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectQuery(`SELECT pg_try_advisory_lock`).WithArgs(codexTicketMaintenanceLock).WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(true))
			// Even a failed SET can have changed the server session before its
			// reply was interrupted. Cleanup must reset it on this early return.
			setupErr := errors.New("SET reply interrupted")
			mock.ExpectExec(`SET lock_timeout = '2s'`).WillReturnError(setupErr)
			reset := mock.ExpectExec(`RESET lock_timeout`)
			if outcome == "reset error" {
				reset.WillReturnError(errors.New("RESET reply interrupted"))
			} else {
				reset.WillReturnResult(sqlmock.NewResult(0, 0))
				unlock := mock.ExpectQuery(`SELECT pg_advisory_unlock`).WithArgs(codexTicketMaintenanceLock)
				if outcome == "unlock error" {
					unlock.WillReturnError(errors.New("unlock reply interrupted"))
				} else {
					unlock.WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(outcome != "ownership missing"))
				}
			}
			if outcome != "released" {
				mock.ExpectClose()
			}
			err = (&codexTicketAttemptRepository{db: db}).Cleanup(context.Background())
			require.ErrorIs(t, err, setupErr)
			require.Zero(t, db.Stats().InUse)
			if outcome == "released" {
				require.Equal(t, 1, db.Stats().Idle)
			} else {
				require.Zero(t, db.Stats().OpenConnections, "failed cleanup must close the driver connection")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

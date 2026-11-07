package repository

import (
	"context"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelSettingsRepositoryCASSeedConflictRollbackAndCommit(t *testing.T) {
	for _, tc := range []struct {
		name, old, next string
		expected        int64
		want            error
		writeFail       bool
	}{
		{name: "seed zero", old: `{"version":0}`, next: `{"version":1}`},
		{name: "ordinary save", old: `{"version":5}`, next: `{"version":6}`, expected: 5},
		{name: "concurrent conflict", old: `{"version":1}`, next: `{"version":1}`, want: service.ErrModelPolicyConflict},
		{name: "version skip", old: `{"version":0}`, next: `{"version":2}`, want: service.ErrModelPolicyConflict},
		{name: "corrupt stored data", old: `not json`, next: `{"version":1}`, want: service.ErrModelPolicyUnavailable},
		{name: "write error", old: `{"version":0}`, next: `{"version":1}`, writeFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, e := sqlmock.New()
			require.NoError(t, e)
			defer db.Close()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			defer client.Close()
			r := &settingRepository{client: client}
			key := service.ModelManagementKeyPrefix + "openai"
			mock.ExpectBegin()
			mock.ExpectExec(`INSERT INTO settings .*ON CONFLICT .*DO NOTHING`).WithArgs(key).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery(`SELECT value FROM settings WHERE key=\$1 FOR UPDATE`).WithArgs(key).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(tc.old))
			if tc.want == nil {
				q := mock.ExpectExec(`UPDATE settings SET value=\$2,updated_at=NOW\(\) WHERE key=\$1`).WithArgs(key, tc.next)
				if tc.writeFail {
					q.WillReturnError(errors.New("write failed"))
					mock.ExpectRollback()
				} else {
					q.WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
				}
			} else {
				mock.ExpectRollback()
			}
			err := r.CompareAndSwapModelSettings(context.Background(), key, tc.expected, tc.next)
			if tc.writeFail {
				require.ErrorContains(t, err, "write failed")
			} else {
				require.ErrorIs(t, err, tc.want)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func TestModelSettingsRepositoryRejectsUnownedKeysAndNegativeVersion(t *testing.T) {
	r := &settingRepository{}
	require.ErrorIs(t, r.CompareAndSwapModelSettings(context.Background(), "unrelated", 0, `{"version":1}`), service.ErrModelPolicyConflict)
	require.ErrorIs(t, r.CompareAndSwapModelSettings(context.Background(), service.ModelManagementKeyPrefix+"openai", -1, `{"version":0}`), service.ErrModelPolicyConflict)
}

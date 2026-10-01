//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModelCatalogInventoryUpdateIsAtomic(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve-other-models", true: "rollback-invalid-edit"}[reject], func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectBegin()
			mock.ExpectExec(`INSERT INTO settings(key,value,updated_at) VALUES($1,'[]',NOW()) ON CONFLICT(key) DO NOTHING`).
				WithArgs(service.ModelCatalogRegistryKey).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery(`SELECT value FROM settings WHERE key=$1 FOR UPDATE`).WithArgs(service.ModelCatalogRegistryKey).
				WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(`[{"id":"existing","platform":"openai","kind":"image","disabled":true}]`))
			validationErr := errors.New("invalid edit")
			if reject {
				mock.ExpectRollback()
			} else {
				mock.ExpectExec(`UPDATE settings SET value=$2,updated_at=NOW() WHERE key=$1`).WithArgs(service.ModelCatalogRegistryKey, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			err = NewModelCatalogRepository(db).UpdateRegistry(context.Background(), func(entries []service.ModelCatalogEntry) ([]service.ModelCatalogEntry, error) {
				require.Len(t, entries, 1)
				require.Equal(t, "existing", entries[0].ID)
				require.True(t, entries[0].Disabled, "edits must read the current locked row, not a stale page snapshot")
				if reject {
					return nil, validationErr
				}
				return append(entries, service.ModelCatalogEntry{ID: "new-model", Platform: "openai"}), nil
			})
			if reject {
				require.ErrorIs(t, err, validationErr)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

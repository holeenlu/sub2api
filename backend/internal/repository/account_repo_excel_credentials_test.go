package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestExcelCredentialCompareAndSwapGuardsPrimaryAndConcurrentGrant(t *testing.T) {
	for _, tc := range []struct {
		name, previous string
		exists         bool
		affected       int64
	}{
		{"import", "", true, 1}, {"refresh", "old", true, 1}, {"concurrent reauthorization", "old", true, 0}, {"primary changed", "old", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			defer client.Close()
			repo := newAccountRepositoryWithSQL(client, db, nil)
			a := &service.Account{ID: 23, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "native", "chatgpt_account_id": "workspace"}}
			credentials, _ := json.Marshal(a.Credentials)
			mock.ExpectBegin()
			rows := sqlmock.NewRows([]string{"id"})
			if tc.exists {
				rows.AddRow(a.ID)
			}
			mock.ExpectQuery(`SELECT id FROM accounts WHERE.*parent_account_id IS NULL.*credentials=\$2::jsonb FOR UPDATE`).WithArgs(a.ID, string(credentials)).WillReturnRows(rows)
			if tc.exists {
				query := `UPDATE openai_excel_grants SET ciphertext=$3,updated_at=NOW() WHERE account_id=$1 AND ciphertext=$2`
				if tc.previous == "" {
					query = `INSERT INTO openai_excel_grants(account_id,ciphertext) VALUES($1,$3) ON CONFLICT(account_id) DO UPDATE SET ciphertext=$3,updated_at=NOW() WHERE openai_excel_grants.ciphertext=$2`
				}
				mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(a.ID, tc.previous, "encrypted-new").WillReturnResult(sqlmock.NewResult(0, tc.affected))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			changed, err := repo.StoreExcelCredentials(context.Background(), a, tc.previous, "encrypted-new", nil)
			require.NoError(t, err)
			require.Equal(t, tc.affected == 1, changed)
			require.NoError(t, mock.ExpectationsWereMet())
			require.Equal(t, "native", a.Credentials["access_token"])
		})
	}
}

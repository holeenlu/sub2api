package service

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestLoadUserCreatorsNamesAndVisibility(t *testing.T) {
	for _, role := range []string{authz.SuperAdmin, authz.Admin} {
		t.Run(role, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { client.Close() })
			query := `SELECT target.id::text,target.created_by,COALESCE\(creator.username,''\) FROM users target LEFT JOIN users creator ON creator.id=target.created_by WHERE target.id::text=ANY\(\$1\) AND target.created_by IS NOT NULL`
			mock.ExpectQuery(query).WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"target", "actor", "username"}).AddRow("10", 4, " holeen ").AddRow("11", 5, "   ").AddRow("12", 6, ""))
			users := []User{{ID: 10}, {ID: 11}, {ID: 12}, {ID: 13}}
			(&adminServiceImpl{entClient: client}).loadUserCreators(authz.WithSubject(context.Background(), authz.Subject{Role: role}), users)
			require.Equal(t, "holeen", users[0].CreatedByName)
			require.EqualValues(t, 4, *users[0].CreatedBy)
			require.Empty(t, users[1].CreatedByName)
			require.EqualValues(t, 5, *users[1].CreatedBy)
			require.Empty(t, users[2].CreatedByName)
			require.EqualValues(t, 6, *users[2].CreatedBy)
			require.Nil(t, users[3].CreatedBy)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

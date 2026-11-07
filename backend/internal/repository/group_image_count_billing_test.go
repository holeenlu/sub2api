//go:build unit

package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/stretchr/testify/require"
)

func TestGroupImageCountBillingPostgresMigrationAndEntRoundTrip(t *testing.T) {
	db := scopedAdminKeyTestDB(t)
	_, err := db.Exec(`CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,updated_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ);
 INSERT INTO groups(id,name) VALUES(1,'existing group')`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "302_group_image_count_billing.sql"))
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = db.Exec(string(migration))
		require.NoError(t, err)
	}
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	ctx := context.Background()
	read := func() bool {
		row, err := client.Group.Query().Where(group.IDEQ(1)).Select(group.FieldID, group.FieldImageCountBilling).Only(ctx)
		require.NoError(t, err)
		return row.ImageCountBilling
	}
	require.False(t, read(), "migration must not enable per-image billing for existing groups")
	for _, enabled := range []bool{true, false} {
		_, err := client.Group.UpdateOneID(1).SetImageCountBilling(enabled).Select(group.FieldID).Save(ctx)
		require.NoError(t, err)
		require.Equal(t, enabled, read())
	}
	var name string
	require.NoError(t, db.QueryRow("SELECT name FROM groups WHERE id=1").Scan(&name))
	require.Equal(t, "existing group", name)
}

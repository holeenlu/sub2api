package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type captureEntQueryMatcher struct {
	actual  *string
	queries *[]string
}

func (m captureEntQueryMatcher) Match(_, actual string) error {
	if m.actual == nil {
		return fmt.Errorf("query capture target is nil")
	}
	if m.queries != nil {
		*m.queries = append(*m.queries, actual)
	}
	*m.actual = actual
	return nil
}

func TestListSchedulableAccountLoadsUsesSingleProjectionQuery(t *testing.T) {
	var capturedSQL string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &capturedSQL}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)

	mock.ExpectQuery("schedulable account load projection").
		WillReturnRows(sqlmock.NewRows([]string{"id", "concurrency", "load_factor"}).
			AddRow(int64(11), 3, nil).
			AddRow(int64(12), 2, 7))

	loads, err := repo.ListSchedulableAccountLoads(context.Background())
	require.NoError(t, err)
	require.Len(t, loads, 2)
	require.Equal(t, int64(11), loads[0].ID)
	require.Equal(t, 3, loads[0].MaxConcurrency)
	require.Equal(t, int64(12), loads[1].ID)
	require.Equal(t, 7, loads[1].MaxConcurrency)
	require.NoError(t, mock.ExpectationsWereMet(), "projection path must execute exactly one query")

	normalized := normalizeSQLWhitespace(capturedSQL)
	selectClause, _, found := strings.Cut(normalized, " FROM ")
	require.True(t, found, "unexpected projection SQL: %s", normalized)
	require.Equal(t, 2, strings.Count(selectClause, ","), "projection must select exactly three columns: %s", selectClause)
	require.Contains(t, selectClause, `"id"`)
	require.Contains(t, selectClause, `"concurrency"`)
	require.Contains(t, selectClause, `"load_factor"`)
	require.NotContains(t, selectClause, "credentials")
	require.NotContains(t, selectClause, "extra")
	require.NotContains(t, selectClause, "proxy_id")
	// Supplier qualification needs correlated EXISTS reads of grants/proxies,
	// but must not hydrate those relations, multiply account rows, or issue
	// additional queries. The outer projection stays on accounts alone.
	_, fromAndWhere, _ := strings.Cut(normalized, " FROM ")
	fromClause, _, hasWhere := strings.Cut(fromAndWhere, " WHERE ")
	require.True(t, hasWhere)
	require.Equal(t, `"accounts"`, fromClause)
	require.Contains(t, normalized, "accounts.vendor_id IS NULL OR")
	require.Contains(t, normalized, "EXISTS (SELECT 1 FROM proxies vp")
	require.Contains(t, normalized, "EXISTS (SELECT 1 FROM vendor_groups vg")
	require.Contains(t, normalized, "ag.account_id=accounts.id")
	for _, predicateColumn := range []string{
		"status",
		"schedulable",
		"temp_unschedulable_until",
		"expires_at",
		"auto_pause_on_expired",
		"overload_until",
		"rate_limit_reset_at",
		"deleted_at",
	} {
		require.Contains(t, normalized, predicateColumn)
	}
	_, orderClause, hasOrder := strings.Cut(normalized, " ORDER BY ")
	require.True(t, hasOrder, "projection query must preserve schedulable account order: %s", normalized)
	require.Contains(t, orderClause, `"priority" ASC`)
}

// 模拟请求失败/自动恢复晚于管理员停用：SQL 必须以数据库当前状态为准，不能读后覆盖。
func TestAccountErrorMutationHonorsManualDisableAtWrite(t *testing.T) {
	for _, tc := range []struct {
		name, condition string
		clear           bool
	}{
		{"request failure", `"status" <>`, false},
		{"automatic recovery", `"status" =`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var query string
			var queries []string
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &query, queries: &queries}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newAccountRepositoryWithSQL(client, db, nil)
			mock.ExpectExec("atomic account state update").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec("scheduler projection refresh").WillReturnResult(sqlmock.NewResult(0, 1))
			// Check the atomic UPDATE in its own matcher before the outbox statement replaces capture.
			mock.MatchExpectationsInOrder(true)
			if tc.clear {
				err = repo.ClearError(context.Background(), 44)
			} else {
				err = repo.SetError(context.Background(), 44, "late failure")
			}
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
			require.NotEmpty(t, queries)
			_, where, ok := strings.Cut(queries[0], " WHERE ")
			require.True(t, ok)
			require.Contains(t, where, tc.condition)
			require.NotContains(t, where, "vendor_id", "manual-disable protection also applies to administrator-owned accounts")
		})
	}
}

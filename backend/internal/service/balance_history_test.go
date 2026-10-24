package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestBalanceHistoryPostgres(t *testing.T) {
	dsn := os.Getenv("BALANCE_HISTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL DSN not supplied")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/balance_history_test", u.Path)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	// Session-local tables cannot change application data and disappear on close.
	_, err = db.Exec(`
 CREATE TEMP TABLE redeem_codes (id bigint, used_by bigint, type text, value numeric, validity_days int, status text, code text, notes text, used_at timestamptz, created_at timestamptz);
 CREATE TEMP TABLE payment_orders (user_id bigint, recharge_code text);
 CREATE TEMP TABLE user_affiliate_ledger (id bigint, user_id bigint, action text, amount numeric, created_at timestamptz);
 INSERT INTO redeem_codes VALUES
 (1,11,'balance',100,0,'used','private-payment-code',NULL,'2026-10-01','2026-09-01'),
 (2,11,'admin_balance',50,0,'used','private-admin-code','Adjustment','2026-10-02','2026-10-02'),
 (3,11,'admin_balance',-10,0,'used','private-deduct-code','Refund','2026-10-03','2026-10-03'),
 (4,11,'concurrency',5,0,'used','private-concurrency-code','','2026-10-04','2026-10-04'),
 (5,11,'subscription',0,30,'used','private-subscription-code','','2026-10-05','2026-10-05'),
 (6,22,'balance',900,0,'used','foreign-code','','2026-10-06','2026-10-06'),
 (7,11,'balance',999,0,'unused','unused-code','','2026-10-07','2026-10-07'),
 (8,11,'balance',20,0,'used','historical-code','','2026-10-02','2026-10-02');
 INSERT INTO payment_orders VALUES (11,'private-payment-code'), (22,'historical-code');
 INSERT INTO user_affiliate_ledger VALUES (1,11,'transfer',7,'2026-10-06'), (2,22,'transfer',888,'2026-10-07'), (3,11,'reward',999,'2026-10-08');
 `)
	require.NoError(t, err)
	s := &RedeemService{entClient: client}
	ctx := context.Background()
	first, err := s.GetBalanceHistory(ctx, 11, 1, 2, BalanceHistoryFilter{Category: "balance"})
	require.NoError(t, err)
	require.EqualValues(t, 5, first.Total)
	require.Equal(t, 170.0, first.TotalRecharged)
	require.Equal(t, "affiliate-1", first.Items[0].ID)
	require.Equal(t, "credit-3", first.Items[1].ID)
	second, err := s.GetBalanceHistory(ctx, 11, 2, 2, BalanceHistoryFilter{Category: "balance"})
	require.NoError(t, err)
	require.Equal(t, "credit-8", second.Items[0].ID)
	require.Equal(t, "redeem", second.Items[0].Source) // Another user's payment cannot label this record.
	require.Equal(t, "credit-2", second.Items[1].ID)
	for _, tc := range []struct {
		filter BalanceHistoryFilter
		ids    []string
	}{
		{BalanceHistoryFilter{Category: "balance", Direction: "decrease"}, []string{"credit-3"}},
		{BalanceHistoryFilter{Category: "balance", Source: "payment"}, []string{"credit-1"}},
		{BalanceHistoryFilter{Category: "balance", Source: "admin", Direction: "increase"}, []string{"credit-2"}},
		{BalanceHistoryFilter{Category: "other"}, []string{"credit-5", "credit-4"}},
	} {
		result, err := s.GetBalanceHistory(ctx, 11, 1, 15, tc.filter)
		require.NoError(t, err)
		ids := []string{}
		for _, item := range result.Items {
			ids = append(ids, item.ID)
		}
		require.Equal(t, tc.ids, ids)
		require.EqualValues(t, len(ids), result.Total)
		require.Equal(t, 170.0, result.TotalRecharged)
		if tc.filter.Category == "other" {
			require.Equal(t, 30.0, result.Items[0].Value)
		}
		raw, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "private-")
		require.NotContains(t, string(raw), "used_by")
	}
	empty, err := s.GetBalanceHistory(ctx, 33, 1, 15, BalanceHistoryFilter{Category: "balance"})
	require.NoError(t, err)
	require.Empty(t, empty.Items)
	require.Zero(t, empty.TotalRecharged)
	beyond, err := s.GetBalanceHistory(ctx, 11, 99, 15, BalanceHistoryFilter{Category: "balance"})
	require.NoError(t, err)
	require.Empty(t, beyond.Items)
	require.EqualValues(t, 5, beyond.Total)
	require.Equal(t, 170.0, beyond.TotalRecharged)
	_, err = s.GetBalanceHistory(ctx, 0, 1, 15, BalanceHistoryFilter{Category: "balance"})
	require.Error(t, err)
}

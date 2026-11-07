package service

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type vendorSettlementSettings struct{ SettingRepository }

func (vendorSettlementSettings) GetValue(context.Context, string) (string, error) { return "true", nil }

// Opt-in local database only. Each test gets a unique schema; no production
// schema or repository migration runner is ever used by this test.
func vendorSettlementPostgres(t *testing.T, beforeBilateral ...bool) (*VendorService, context.Context, context.Context) {
	t.Helper()
	dsn := os.Getenv("VENDOR_SETTLEMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("VENDOR_SETTLEMENT_TEST_DSN not set")
	}
	u, e := url.Parse(dsn)
	require.NoError(t, e)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/vendor_settlement_test", u.Path)
	db, e := sql.Open("postgres", dsn)
	require.NoError(t, e)
	require.NoError(t, db.Ping())
	schema := fmt.Sprintf("vendor_test_%d", time.Now().UnixNano())
	_, e = db.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, e)
	values := u.Query()
	values.Set("search_path", schema)
	u.RawQuery = values.Encode()
	scoped, e := sql.Open("postgres", u.String())
	require.NoError(t, e)
	scoped.SetMaxOpenConns(15)
	t.Cleanup(func() { scoped.Close(); db.Exec("DROP SCHEMA " + schema + " CASCADE"); db.Close() })
	core := `
CREATE TABLE users(id BIGINT PRIMARY KEY,role TEXT,status TEXT,session_generation BIGINT,deleted_at TIMESTAMPTZ);
CREATE TABLE vendors(id BIGINT PRIMARY KEY,user_id BIGINT,name TEXT,status TEXT,policy_version BIGINT,platforms JSONB,account_limit INT,proxy_limit INT,allow_direct BOOL,default_concurrency INT,default_priority INT,default_rate_multiplier NUMERIC,contact_name TEXT,settlement_email TEXT,binance_id TEXT,default_coefficient NUMERIC,group_coefficients JSONB,contract_version BIGINT,created_at TIMESTAMPTZ DEFAULT NOW(),updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,platform TEXT,status TEXT,deleted_at TIMESTAMPTZ);
CREATE TABLE vendor_groups(vendor_id BIGINT,group_id BIGINT,is_default BOOL);
CREATE TABLE accounts(id BIGINT PRIMARY KEY,vendor_id BIGINT,name TEXT,platform TEXT,status TEXT,deleted_at TIMESTAMPTZ,parent_account_id BIGINT);
CREATE TABLE proxies(id BIGINT PRIMARY KEY,vendor_id BIGINT,deleted_at TIMESTAMPTZ);
CREATE TABLE usage_logs(id BIGSERIAL PRIMARY KEY,request_id TEXT,api_key_id BIGINT,account_id BIGINT,group_id BIGINT,created_at TIMESTAMPTZ,requested_model TEXT,model TEXT,upstream_model TEXT,input_tokens BIGINT DEFAULT 0,output_tokens BIGINT DEFAULT 0,cache_creation_tokens BIGINT DEFAULT 0,cache_read_tokens BIGINT DEFAULT 0,account_stats_cost NUMERIC,total_cost NUMERIC DEFAULT 0,actual_cost NUMERIC DEFAULT 0,account_rate_multiplier NUMERIC,duration_ms INT,first_token_ms INT);
INSERT INTO users VALUES(9,'super_admin','active',1,NULL),(3,'vendor_level1','active',1,NULL),(4,'vendor_level1','active',1,NULL);
INSERT INTO vendors(id,user_id,name,status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,contract_version) VALUES(1,3,'A','active',1,'["openai"]',10,10,FALSE,1,1,1,'payee','settlement@example.invalid','00123456789012345678',1.8,'{"2":"1.6"}',1),(2,4,'B','active',1,'["openai"]',10,10,FALSE,1,1,1,'B','b@example.invalid','0002',1,'{}',1);
INSERT INTO groups VALUES(1,'group one','openai','active',NULL),(2,'group two','openai','active',NULL);
INSERT INTO vendor_groups VALUES(1,1,TRUE),(1,2,FALSE),(2,1,TRUE);
INSERT INTO accounts VALUES(11,1,'account A','openai','active',NULL,NULL),(12,1,'deleted A','openai','active',NOW(),NULL),(21,2,'account B','openai','active',NULL,NULL);
ALTER TABLE accounts ADD COLUMN extra JSONB NOT NULL DEFAULT '{}';
ALTER TABLE vendors ADD COLUMN openai_authorization_modes JSONB NOT NULL DEFAULT '["native"]';
ALTER TABLE vendors ADD COLUMN tier SMALLINT NOT NULL DEFAULT 1;
ALTER TABLE vendors ADD COLUMN parent_vendor_id BIGINT;
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT 'test@invalid';
ALTER TABLE users ADD COLUMN username TEXT NOT NULL DEFAULT 'test';
`
	_, e = scoped.Exec(core)
	require.NoError(t, e)
	migration, e := os.ReadFile(filepath.Join("..", "..", "migrations", "274_vendor_settlements.sql"))
	require.NoError(t, e)
	_, e = scoped.Exec(string(migration))
	require.NoError(t, e)
	_, e = scoped.Exec(string(migration))
	require.NoError(t, e, "migration must be re-entrant")
	if len(beforeBilateral) == 0 || !beforeBilateral[0] {
		migration, e = os.ReadFile(filepath.Join("..", "..", "migrations", "277_vendor_bilateral_settlements.sql"))
		require.NoError(t, e)
		_, e = scoped.Exec(string(migration))
		require.NoError(t, e)
		_, e = scoped.Exec(string(migration))
		require.NoError(t, e, "bilateral migration must be re-entrant")
	}
	migration, e = os.ReadFile(filepath.Join("..", "..", "migrations", "279_openai_usage_authorization_mode.sql"))
	require.NoError(t, e)
	_, e = scoped.Exec(string(migration))
	require.NoError(t, e)
	_, e = scoped.Exec(string(migration))
	require.NoError(t, e, "usage-mode migration must be re-entrant")
	migration, e = os.ReadFile(filepath.Join("..", "..", "migrations", "287_vendor_settlement_exchange_rate.sql"))
	require.NoError(t, e)
	_, e = scoped.Exec(string(migration))
	require.NoError(t, e)
	_, e = scoped.Exec(string(migration))
	require.NoError(t, e, "exchange-rate migration must be re-entrant")
	s := &VendorService{db: scoped, cfg: &config.Config{JWT: config.JWTConfig{Secret: "integration-test-not-production-key"}}, settings: &SettingService{settingRepo: vendorSettlementSettings{}}}
	admin := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	supplier := authz.WithSubject(context.Background(), authz.Subject{UserID: 3, Role: authz.VendorLevel1, SessionGeneration: 1})
	supplier = authz.WithVendorScope(supplier, authz.VendorScope{VendorID: 1, PolicyVersion: 1, ActorUserID: 3})
	return s, admin, supplier
}
func vendorSettlementSeedUsage(t *testing.T, s *VendorService) {
	t.Helper()
	_, e := s.db.Exec(`INSERT INTO usage_logs(vendor_id_snapshot,account_id,account_name_snapshot,group_id,group_name_snapshot,platform_snapshot,created_at,requested_model,model,upstream_model,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,account_stats_cost,total_cost,actual_cost,account_rate_multiplier,duration_ms,first_token_ms) VALUES
 (1,11,'original A',1,'original one','openai','2026-10-01T12:00:00Z','requested','model','upstream',100,40,20,200,6994.09,99999,77777,2,900,100),
 (1,12,'deleted A',2,'original two','openai','2026-10-02T12:00:00Z','requested','model','upstream',200,60,30,300,NULL,2036.31,66666,NULL,NULL,NULL),
 (2,21,'B',1,'one','openai','2026-10-01T12:00:00Z','requested','model','upstream',900,900,0,0,NULL,88888,88888,1,100,10),
 (NULL,11,'legacy',1,'one','openai','2026-10-01T12:00:00Z','requested','model','upstream',900,900,0,0,NULL,55555,55555,1,100,10)`)
	require.NoError(t, e)
	_, e = s.db.Exec("UPDATE usage_logs SET request_id='native-'||id::text,api_key_id=100")
	require.NoError(t, e)
}
func vendorSettlementPreviewCreate(t *testing.T, s *VendorService, ctx context.Context, key string) *VendorSettlement {
	t.Helper()
	_, in, _, _ := vendorSettlementFixture(t)
	in.VendorIDs = []int64{1}
	pre, e := s.PreviewVendorSettlements(ctx, in)
	require.NoError(t, e)
	require.Len(t, pre, 1)
	require.Empty(t, pre[0].Error)
	out, e := s.CreateVendorSettlements(ctx, []VendorSettlementDraftInput{{PreviewToken: pre[0].PreviewToken, IdempotencyKey: key}})
	require.NoError(t, e)
	require.Empty(t, out[0].Error)
	require.NotNil(t, out[0].Statement)
	return out[0].Statement
}
func TestVendorSettlementPostgresFrozenCleanupPayments(t *testing.T) {
	s, admin, supplier := vendorSettlementPostgres(t)
	vendorSettlementSeedUsage(t, s)
	draft := vendorSettlementPreviewCreate(t, s, admin, "draft-a")
	require.Equal(t, "2365.29", draft.Total)
	_, e := s.GetVendorSettlement(supplier, draft.ID, 1, 20, false)
	require.ErrorIs(t, e, ErrVendorSettlementNotFound)
	confirmed, e := s.ConfirmVendorSettlement(admin, draft.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, e)
	require.Equal(t, "pending_payment", confirmed.Status)
	before, e := s.VendorSettlementCSV(supplier, draft.ID, false)
	require.NoError(t, e)
	require.Contains(t, string(before), "'00123456789012345678")
	_, e = s.db.Exec("UPDATE vendors SET name='renamed',binance_id='new',default_coefficient=99,contract_version=2 WHERE id=1; UPDATE accounts SET name='new',deleted_at=NOW() WHERE vendor_id=1; UPDATE groups SET name='new'; DELETE FROM usage_logs WHERE vendor_id_snapshot=1")
	require.NoError(t, e)
	after, e := s.VendorSettlementCSV(supplier, draft.ID, false)
	require.NoError(t, e)
	require.Equal(t, before, after, "frozen CSV must survive account changes and raw cleanup")
	var active int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM vendor_settlement_allocations WHERE active").Scan(&active))
	require.Equal(t, 2, active)
	pay := VendorSettlementPaymentInput{Version: confirmed.Version, Amount: "1000.00", PaidAt: time.Now().UTC(), Channel: "OFFLINE", Reference: " Receipt 001 ", IdempotencyKey: "pay-1"}
	paid, e := s.RegisterVendorPayment(admin, draft.ID, pay)
	require.NoError(t, e)
	require.Equal(t, "partially_paid", paid.Status)
	require.Equal(t, "1000.00", paid.Paid)
	replay, e := s.RegisterVendorPayment(admin, draft.ID, pay)
	require.NoError(t, e)
	require.Equal(t, paid.Version, replay.Version)
	require.Len(t, replay.Payments, 1)
	pay.Version = paid.Version
	pay.IdempotencyKey = "pay-duplicate"
	_, e = s.RegisterVendorPayment(admin, draft.ID, pay)
	require.ErrorIs(t, e, ErrVendorSettlementConflict)
	reverse := VendorSettlementAdjustmentInput{Version: paid.Version, Amount: "400", Reason: "entry correction", IdempotencyKey: "reverse-1"}
	reversed, e := s.ReverseVendorPayment(admin, draft.ID, paid.Payments[0].ID, reverse)
	require.NoError(t, e)
	require.Equal(t, "600.00", reversed.Paid)
	require.Len(t, reversed.Payments, 2)
	_, e = s.ReverseVendorPayment(admin, draft.ID, paid.Payments[0].ID, reverse)
	require.NoError(t, e)
	reverse.Version = reversed.Version
	reverse.Amount = "600.01"
	reverse.IdempotencyKey = "reverse-over"
	_, e = s.ReverseVendorPayment(admin, draft.ID, paid.Payments[0].ID, reverse)
	require.ErrorIs(t, e, ErrVendorSettlementConflict)
	credit, e := s.AdjustVendorSettlement(admin, draft.ID, VendorSettlementAdjustmentInput{Version: reversed.Version, Amount: "-100", Reason: "contract correction", IdempotencyKey: "credit-1"})
	require.NoError(t, e)
	require.Equal(t, "credit", credit.Kind)
	_, e = s.ConfirmVendorSettlement(admin, credit.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, e)
	parent, e := s.GetVendorSettlement(admin, draft.ID, 1, 20, true)
	require.NoError(t, e)
	require.Equal(t, "100.00", parent.Credited)
	require.Equal(t, "1665.29", parent.Unpaid)
	require.Equal(t, draft.Total, parent.Total)
	_, e = s.AdjustVendorSettlement(admin, draft.ID, VendorSettlementAdjustmentInput{Version: parent.Version, Amount: "-1665.30", Reason: "too much", IdempotencyKey: "credit-over"})
	require.ErrorIs(t, e, ErrVendorSettlementConflict)
	_, e = s.VoidVendorSettlement(admin, draft.ID, VendorSettlementVersionInput{Version: parent.Version, Reason: "cannot void paid"})
	require.ErrorIs(t, e, ErrVendorSettlementConflict)
	// Retrying a native request after cleanup gets a new row ID, but its
	// durable allocation must still prevent a second settlement.
	_, e = s.db.Exec("INSERT INTO usage_logs(request_id,api_key_id,vendor_id_snapshot,account_id,account_name_snapshot,group_id,group_name_snapshot,platform_snapshot,created_at,model,total_cost) VALUES('native-1',100,1,11,'retry',1,'old','openai','2026-10-01T12:00:00Z','model',6994.09)")
	require.NoError(t, e)
	replayed, e := s.GetVendorSettlement(admin, draft.ID, 1, 20, true)
	require.NoError(t, e)
	require.Zero(t, replayed.LateUsage.Requests, "native replay must remain occupied after raw cleanup")
	_, replayInput, _, _ := vendorSettlementFixture(t)
	replayInput.VendorIDs = []int64{1}
	replayInput.ContractVersion = 2
	preview, e := s.PreviewVendorSettlements(admin, replayInput)
	require.NoError(t, e)
	require.Empty(t, preview[0].Error)
	require.Zero(t, preview[0].AvailableCount)
	require.Equal(t, int64(1), preview[0].OccupiedCount)
	// A late event remains unreserved and is reported without mutating the old bill.
	_, e = s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,account_name_snapshot,group_id,group_name_snapshot,platform_snapshot,created_at,model,total_cost) VALUES(1,11,'late',1,'old','openai','2026-10-01T13:00:00Z','model',1)")
	require.NoError(t, e)
	parent, e = s.GetVendorSettlement(admin, draft.ID, 1, 20, true)
	require.NoError(t, e)
	require.Equal(t, int64(1), parent.LateUsage.Requests)
	require.Equal(t, "1", parent.LateUsage.CostUSD)
	require.Equal(t, draft.SnapshotHash, parent.SnapshotHash)
	replayInput.ParentStatementID = &draft.ID
	preview, e = s.PreviewVendorSettlements(admin, replayInput)
	require.NoError(t, e)
	require.Empty(t, preview[0].Error)
	require.Equal(t, int64(1), preview[0].AvailableCount)
	require.Equal(t, int64(1), preview[0].OccupiedCount)
	supplementInput := []VendorSettlementDraftInput{{PreviewToken: preview[0].PreviewToken, IdempotencyKey: "late-supplement"}}
	supplement, e := s.CreateVendorSettlements(admin, supplementInput)
	require.NoError(t, e)
	require.Empty(t, supplement[0].Error)
	require.NotNil(t, supplement[0].Statement)
	require.Equal(t, draft.ID, *supplement[0].Statement.ParentStatementID)
	require.Equal(t, int64(1), supplement[0].Statement.UsageCount)
	repeated, e := s.CreateVendorSettlements(admin, supplementInput)
	require.NoError(t, e)
	require.Empty(t, repeated[0].Error)
	require.Equal(t, supplement[0].Statement.ID, repeated[0].Statement.ID)
	_, e = s.ConfirmVendorSettlement(admin, supplement[0].Statement.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, e)
	parent, e = s.GetVendorSettlement(admin, draft.ID, 1, 20, true)
	require.NoError(t, e)
	require.Zero(t, parent.LateUsage.Requests)
	require.Equal(t, draft.Total, parent.Total)
	require.Equal(t, draft.SnapshotHash, parent.SnapshotHash)
	other := authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel1, SessionGeneration: 1})
	_, e = s.GetVendorSettlement(other, draft.ID, 1, 20, false)
	require.ErrorIs(t, e, ErrVendorSettlementNotFound)
	_, e = s.db.Exec("UPDATE users SET role='admin' WHERE id=9")
	require.NoError(t, e)
	_, e = s.GetVendorSettlement(admin, draft.ID, 1, 20, true)
	require.ErrorIs(t, e, ErrVendorUsageForbidden)
}
func TestVendorSettlementPostgresOverlapVoidAndConcurrentPay(t *testing.T) {
	s, admin, _ := vendorSettlementPostgres(t)
	vendorSettlementSeedUsage(t, s)
	_, in, _, _ := vendorSettlementFixture(t)
	in.VendorIDs = []int64{1}
	p, e := s.PreviewVendorSettlements(admin, in)
	require.NoError(t, e)
	require.Empty(t, p[0].Error)
	var wg sync.WaitGroup
	results := make(chan []VendorSettlementDraftResult, 2)
	for _, key := range []string{"overlap-a", "overlap-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			out, err := s.CreateVendorSettlements(admin, []VendorSettlementDraftInput{{PreviewToken: p[0].PreviewToken, IdempotencyKey: key}})
			if err == nil {
				results <- out
			}
		}(key)
	}
	wg.Wait()
	close(results)
	var winner *VendorSettlement
	success := 0
	for r := range results {
		if r[0].Statement != nil {
			winner = r[0].Statement
			success++
		}
	}
	require.Equal(t, 1, success)
	void, e := s.VoidVendorSettlement(admin, winner.ID, VendorSettlementVersionInput{Version: 1, Reason: "rebuild"})
	require.NoError(t, e)
	require.Equal(t, "void", void.Status)
	var count int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM vendor_settlement_allocations WHERE active").Scan(&count))
	require.Zero(t, count)
	again := vendorSettlementPreviewCreate(t, s, admin, "after-void")
	confirmed, e := s.ConfirmVendorSettlement(admin, again.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, e)
	payments := make(chan error, 2)
	for _, key := range []string{"concurrent-a", "concurrent-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, e := s.RegisterVendorPayment(admin, again.ID, VendorSettlementPaymentInput{Version: confirmed.Version, Amount: "2000", PaidAt: time.Now(), Channel: "offline", Reference: key, IdempotencyKey: key})
			payments <- e
		}(key)
	}
	wg.Wait()
	close(payments)
	successes := 0
	for e := range payments {
		if e == nil {
			successes++
		} else {
			require.ErrorIs(t, e, ErrVendorSettlementConflict)
		}
	}
	require.Equal(t, 1, successes)
	var paid string
	require.NoError(t, s.db.QueryRow("SELECT paid::text FROM vendor_settlements WHERE id=$1", again.ID).Scan(&paid))
	require.Equal(t, "2000.00", paid)
}
func TestVendorUsagePostgresDimensionsHistoryCSV(t *testing.T) {
	s, admin, supplier := vendorSettlementPostgres(t)
	vendorSettlementSeedUsage(t, s)
	f := VendorUsageFilter{VendorID: 1, StartDate: "2026-10-01", EndDate: "2026-10-02", PageSize: 1}
	for _, dimension := range []string{"vendor", "account", "date", "group", "account_date"} {
		f.Dimension = dimension
		r, e := s.VendorUsage(admin, f, true)
		require.NoError(t, e, dimension)
		require.Equal(t, int64(2), r.Summary.Requests)
		require.Equal(t, "9030.4", r.Summary.CostUSD)
		require.Len(t, r.Items, 1)
		if dimension == "date" {
			require.Empty(t, r.Items[0].VendorName, "a date aggregate is not a vendor identity")
		} else {
			require.Equal(t, "A", r.Items[0].VendorName, dimension)
		}
		require.Nil(t, r.Summary.SuccessRate)
		f.SnapshotAt = r.SnapshotAt
		f.Watermark = r.Watermark
	}
	multi, e := s.VendorUsage(admin, VendorUsageFilter{StartDate: f.StartDate, EndDate: f.EndDate, Dimension: "vendor", PageSize: 100}, true)
	require.NoError(t, e)
	require.Equal(t, int64(3), multi.Summary.Requests)
	require.Equal(t, "97918.4", multi.Summary.CostUSD)
	require.Len(t, multi.Items, 2)
	require.Equal(t, []int64{2, 1}, []int64{multi.Items[0].VendorID, multi.Items[1].VendorID}, "numeric cost ordering is unchanged")
	require.Equal(t, []string{"B", "A"}, []string{multi.Items[0].VendorName, multi.Items[1].VendorName})
	f.AccountID = 12
	detail, e := s.VendorAccountUsage(supplier, f, false)
	require.NoError(t, e)
	require.Equal(t, int64(1), detail.Summary.Requests)
	require.Len(t, detail.FixedWindows, 3)
	require.Equal(t, "unknown", detail.Results[0].Result)
	f.AccountID = 0
	data, e := s.VendorUsageCSV(admin, f)
	require.NoError(t, e)
	csvRows, e := csv.NewReader(strings.NewReader(string(data[3:]))).ReadAll()
	require.NoError(t, e)
	require.Len(t, csvRows, 4)
	require.Equal(t, "total", csvRows[3][0])
	require.NotContains(t, string(data), "77777")
	require.NotContains(t, string(data), "88888")
	_, e = s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,created_at,model,total_cost) SELECT 1,11,'2026-10-01T12:00:00Z','model',0 FROM generate_series(1,10001)")
	require.NoError(t, e)
	f.Watermark = 0
	f.SnapshotAt = time.Time{}
	_, e = s.VendorUsageCSV(admin, f)
	require.Error(t, e, "overflow must fail, never truncate")
	// An explicit zero watermark is a real empty snapshot, not an omitted filter.
	emptyFilter := VendorUsageFilter{VendorID: 1, StartDate: "2020-01-01", EndDate: "2020-01-01", WatermarkSet: true}
	empty, e := s.VendorUsage(admin, emptyFilter, true)
	require.NoError(t, e)
	require.Zero(t, empty.Summary.Requests)
	_, e = s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,created_at,model,total_cost) VALUES(1,11,'2020-01-01T12:00:00Z','model',1)")
	require.NoError(t, e)
	emptyFilter.SnapshotAt = empty.SnapshotAt
	same, e := s.VendorUsage(admin, emptyFilter, true)
	require.NoError(t, e)
	require.Zero(t, same.Summary.Requests)
}

func TestVendorSettlementPostgresBatchUsesEachSupplierContract(t *testing.T) {
	s, admin, _ := vendorSettlementPostgres(t)
	vendorSettlementSeedUsage(t, s)
	_, err := s.db.Exec(`UPDATE vendors SET contract_version=2,default_coefficient=1.1,group_coefficients='{"1":"1.3"}' WHERE id=2`)
	require.NoError(t, err)
	_, in, _, _ := vendorSettlementFixture(t)
	in.VendorIDs, in.ContractVersion = []int64{1, 2}, 0
	in.DefaultCoefficient, in.GroupCoefficients = "", nil
	previews, err := s.PreviewVendorSettlements(admin, in)
	require.NoError(t, err)
	require.Len(t, previews, 2)
	for i, version := range []int64{1, 2} {
		require.Empty(t, previews[i].Error)
		require.Equal(t, version, previews[i].Statement.ContractVersion)
	}
	require.Equal(t, "2365.29", previews[0].Statement.Total)
	require.Equal(t, "1.3", previews[1].Statement.Lines[0].Coefficient)
	// The preview resolves the contract once. Saving still rejects a changed contract.
	_, err = s.db.Exec("UPDATE vendors SET contract_version=3 WHERE id=2")
	require.NoError(t, err)
	results, err := s.CreateVendorSettlements(admin, []VendorSettlementDraftInput{{PreviewToken: previews[1].PreviewToken, IdempotencyKey: "changed-batch-contract"}})
	require.NoError(t, err)
	require.NotEmpty(t, results[0].Error)
	require.Nil(t, results[0].Statement)
	in.ContractVersion = 1
	stale, err := s.PreviewVendorSettlements(admin, in)
	require.NoError(t, err)
	require.Empty(t, stale[0].Error)
	require.NotEmpty(t, stale[1].Error, "an explicit stale version must never be silently replaced")
}

func TestVendorSettlementPostgresPreviewLifecycleAndSupplierList(t *testing.T) {
	s, admin, supplier := vendorSettlementPostgres(t)
	vendorSettlementSeedUsage(t, s)
	_, in, _, _ := vendorSettlementFixture(t)
	in.VendorIDs = []int64{1}
	previews, e := s.PreviewVendorSettlements(admin, in)
	require.NoError(t, e)
	token, e := s.vendorPreviewParse(previews[0].PreviewToken)
	require.NoError(t, e)
	token.ExpiresAt = time.Now().Add(-time.Minute)
	expired, e := s.vendorPreviewSign(token)
	require.NoError(t, e)
	results, e := s.CreateVendorSettlements(admin, []VendorSettlementDraftInput{{PreviewToken: expired, IdempotencyKey: "expired"}})
	require.NoError(t, e)
	require.NotEmpty(t, results[0].Error)
	// Data changes between preview and save invalidate the preview rather than
	// silently saving a different amount.
	_, e = s.db.Exec("UPDATE usage_logs SET total_cost=total_cost+1 WHERE id=2")
	require.NoError(t, e)
	results, e = s.CreateVendorSettlements(admin, []VendorSettlementDraftInput{{PreviewToken: previews[0].PreviewToken, IdempotencyKey: "changed"}})
	require.NoError(t, e)
	require.NotEmpty(t, results[0].Error)
	draft := vendorSettlementPreviewCreate(t, s, admin, "fresh")
	list, e := s.ListVendorSettlements(supplier, VendorSettlementListFilter{}, false)
	require.NoError(t, e)
	require.Empty(t, list.Items)
	adminList, e := s.ListVendorSettlements(admin, VendorSettlementListFilter{VendorID: 1}, true)
	require.NoError(t, e)
	require.Equal(t, int64(1), adminList.Total)
	require.Equal(t, "draft", adminList.Items[0].Status)
	// Frozen allocations allow confirmation even after raw retention removes data.
	_, e = s.db.Exec("DELETE FROM usage_logs WHERE vendor_id_snapshot=1")
	require.NoError(t, e)
	confirmed, e := s.ConfirmVendorSettlement(admin, draft.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, e)
	list, e = s.ListVendorSettlements(supplier, VendorSettlementListFilter{Page: 1, PageSize: 1}, false)
	require.NoError(t, e)
	require.Equal(t, int64(1), list.Total)
	require.Equal(t, draft.SnapshotHash, list.Items[0].SnapshotHash)
	require.Equal(t, confirmed.Unpaid, list.Items[0].Unpaid)
	adjustment, e := s.AdjustVendorSettlement(admin, draft.ID, VendorSettlementAdjustmentInput{Version: confirmed.Version, Amount: "12.34", Reason: "supplemental agreed amount", IdempotencyKey: "positive-adjustment"})
	require.NoError(t, e)
	require.Equal(t, "adjustment", adjustment.Kind)
	adjustment, e = s.ConfirmVendorSettlement(admin, adjustment.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, e)
	require.Equal(t, "12.34", adjustment.Unpaid)
	list, e = s.ListVendorSettlements(supplier, VendorSettlementListFilter{}, false)
	require.NoError(t, e)
	require.Equal(t, int64(2), list.Total)
	// The current local day is an open accounting period.
	now := time.Now().UTC()
	_, e = s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,created_at,model,total_cost) VALUES(1,11,$1,'model',1)", now)
	require.NoError(t, e)
	in.StartDate = now.Format(time.DateOnly)
	in.EndDate = in.StartDate
	in.Timezone = "UTC"
	p, e := s.PreviewVendorSettlements(admin, in)
	require.NoError(t, e)
	require.Empty(t, p[0].Error)
	results, e = s.CreateVendorSettlements(admin, []VendorSettlementDraftInput{{PreviewToken: p[0].PreviewToken, IdempotencyKey: "open-period"}})
	require.NoError(t, e)
	require.Empty(t, results[0].Error)
	_, e = s.ConfirmVendorSettlement(admin, results[0].Statement.ID, VendorSettlementVersionInput{Version: 1})
	require.ErrorIs(t, e, ErrVendorSettlementConflict)
}

func TestVendorSettlementPostgresTenThousandRowsAndAllocationPlans(t *testing.T) {
	s, admin, _ := vendorSettlementPostgres(t)
	_, e := s.db.Exec("INSERT INTO usage_logs(request_id,api_key_id,vendor_id_snapshot,account_id,account_name_snapshot,group_id,group_name_snapshot,platform_snapshot,created_at,model,total_cost) SELECT 'scale-'||n::text,100,1,11,'A',1,'one','openai','2026-10-01T12:00:00Z','model',0.01 FROM generate_series(1,10000) n")
	require.NoError(t, e)
	started := time.Now()
	draft := vendorSettlementPreviewCreate(t, s, admin, "scale-draft")
	require.Equal(t, int64(10000), draft.UsageCount)
	require.Equal(t, "100", draft.CostUSD)
	_, e = s.ConfirmVendorSettlement(admin, draft.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, e)
	t.Logf("10,000 source rows preview/draft/confirm: %s", time.Since(started))
	_, e = s.db.Exec("ANALYZE vendor_settlement_allocations")
	require.NoError(t, e)
	plans := []struct{ query, index string }{
		{"EXPLAIN SELECT id FROM vendor_settlement_allocations WHERE active AND vendor_id=1 AND payer_vendor_id IS NULL AND usage_log_id=100", "vendor_allocations_platform_usage_idx"},
		{"EXPLAIN SELECT id FROM vendor_settlement_allocations WHERE active AND vendor_id=1 AND payer_vendor_id IS NULL AND source_fingerprint=encode(sha256(convert_to('scale-100:100','UTF8')),'hex')", "vendor_allocations_platform_source_idx"},
	}
	for _, p := range plans {
		rows, e := s.db.Query(p.query)
		require.NoError(t, e)
		var plan strings.Builder
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		require.NoError(t, rows.Err())
		rows.Close()
		require.Contains(t, plan.String(), p.index)
		t.Log(plan.String())
	}
}

func TestVendorSettlementPostgresDraftCreditReservesPaymentCapacity(t *testing.T) {
	s, admin, _ := vendorSettlementPostgres(t)
	vendorSettlementSeedUsage(t, s)
	draft := vendorSettlementPreviewCreate(t, s, admin, "credit-capacity")
	parent, err := s.ConfirmVendorSettlement(admin, draft.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, err)
	credit, err := s.AdjustVendorSettlement(admin, parent.ID, VendorSettlementAdjustmentInput{Version: parent.Version, Amount: "-500", Reason: "agreed credit awaiting review", IdempotencyKey: "capacity-credit"})
	require.NoError(t, err)
	require.Equal(t, "draft", credit.Status)
	parent, err = s.GetVendorSettlement(admin, parent.ID, 1, 20, true)
	require.NoError(t, err)
	require.Equal(t, "0.00", parent.Credited, "unconfirmed credit is not presented as confirmed")
	payment := VendorSettlementPaymentInput{Version: parent.Version, Amount: "2000", PaidAt: time.Now().UTC(), Channel: "offline", Reference: "capacity-receipt", IdempotencyKey: "capacity-payment"}
	_, err = s.RegisterVendorPayment(admin, parent.ID, payment)
	require.ErrorIs(t, err, ErrVendorSettlementConflict, "draft credit must reserve capacity before it is confirmed")
	payment.Amount = "1865.29"
	paid, err := s.RegisterVendorPayment(admin, parent.ID, payment)
	require.NoError(t, err)
	require.Equal(t, "partially_paid", paid.Status)
	require.Equal(t, "1865.29", paid.Paid)
	require.Equal(t, "500.00", paid.Unpaid)
	_, err = s.ConfirmVendorSettlement(admin, credit.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, err)
	paid, err = s.GetVendorSettlement(admin, parent.ID, 1, 20, true)
	require.NoError(t, err)
	require.Equal(t, "paid", paid.Status)
	require.Equal(t, "500.00", paid.Credited)
	require.Equal(t, "0.00", paid.Unpaid)
	require.Equal(t, draft.Total, paid.Total)
	require.Equal(t, draft.SnapshotHash, paid.SnapshotHash)
	reversed, err := s.ReverseVendorPayment(admin, parent.ID, paid.Payments[0].ID, VendorSettlementAdjustmentInput{Version: paid.Version, Amount: "65.29", Reason: "receipt correction", IdempotencyKey: "capacity-reversal"})
	require.NoError(t, err)
	require.Equal(t, "partially_paid", reversed.Status)
	require.Equal(t, "1800.00", reversed.Paid)
	require.Equal(t, "65.29", reversed.Unpaid)
	require.Equal(t, "500.00", reversed.Credited)
}

// Exercise the real SQL -> report/export -> preview -> persisted bill path.
func TestVendorProcurementPostgresIgnoresCommercialMultipliers(t *testing.T) {
	s, admin, supplier := vendorSettlementPostgres(t)
	_, err := s.db.Exec(`INSERT INTO usage_logs(vendor_id_snapshot,account_id,group_id,created_at,model,account_stats_cost,total_cost,actual_cost,account_rate_multiplier) VALUES
(1,11,1,'2026-10-01T12:00Z','claude-opus-4-6',10,99,999,0.5),
(1,11,1,'2026-10-01T13:00Z','claude-opus-4-6',10,99,999,2),
(1,11,1,'2026-10-01T14:00Z','claude-opus-4-6',10,99,999,0),
(1,11,1,'2026-10-01T15:00Z','claude-opus-4-6',NULL,10,999,NULL),
(1,12,2,'2026-10-01T16:00Z','claude-fable-5',10,99,999,0.5),
(1,12,2,'2026-10-01T17:00Z','claude-fable-5',0,99,999,2)`)
	require.NoError(t, err)
	f := VendorUsageFilter{VendorID: 1, StartDate: "2026-10-01", EndDate: "2026-10-02", Timezone: "UTC", PageSize: 100}
	for _, dimension := range []string{"vendor", "account", "group", "date", "request"} {
		f.Dimension = dimension
		r, err := s.VendorUsage(admin, f, true)
		require.NoError(t, err)
		require.Equal(t, "50", r.Summary.CostUSD, dimension)
		require.Equal(t, "40", r.Summary.OpusCostUSD)
		require.Equal(t, "10", r.Summary.FableCostUSD)
		require.Equal(t, vendorUsageCostBasis, r.Source)
	}
	f.Dimension = "account"
	report, err := s.VendorUsage(supplier, f, false)
	require.NoError(t, err)
	require.Equal(t, "50", report.Summary.CostUSD)
	csv, err := s.VendorUsageCSV(admin, f)
	require.NoError(t, err)
	require.Contains(t, string(csv), vendorUsageCostBasis)
	require.NotContains(t, string(csv), "account_rate_multiplier")
	require.NotContains(t, string(csv), "999")
	_, in, _, _ := vendorSettlementFixture(t)
	in.VendorIDs = []int64{1}
	_, err = s.db.Exec("UPDATE vendors SET default_exchange_rate=6.7 WHERE id=1")
	require.NoError(t, err)
	in.ExchangeRate = ""
	in.DefaultCoefficient = "1.6"
	in.GroupCoefficients = map[string]string{"2": "1.8"}
	previews, err := s.PreviewVendorSettlements(admin, in)
	require.NoError(t, err)
	require.Empty(t, previews[0].Error)
	snap := previews[0].Statement
	require.Equal(t, "50", snap.CostUSD)
	require.Equal(t, "9.55", snap.Lines[0].Amount) // 40 USD * 1.6 CNY/USD / 6.7 CNY/USDT
	require.Equal(t, "2.69", snap.Lines[1].Amount) // 10 USD * 1.8 CNY/USD / the same rate
	require.Equal(t, "12.24", snap.Total)
	require.Equal(t, vendorUsageCostBasis, snap.CostBasis)
	drafts, err := s.CreateVendorSettlements(admin, []VendorSettlementDraftInput{{PreviewToken: previews[0].PreviewToken, IdempotencyKey: "raw-procurement"}})
	require.NoError(t, err)
	require.Empty(t, drafts[0].Error)
	draft := drafts[0].Statement
	confirmed, err := s.ConfirmVendorSettlement(admin, draft.ID, VendorSettlementVersionInput{Version: draft.Version})
	require.NoError(t, err)
	// Late usage follows the new basis while the frozen bill does not change.
	_, err = s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,group_id,created_at,model,account_stats_cost,total_cost,account_rate_multiplier) VALUES(1,11,1,'2026-10-01T18:00Z','model',10,99,0.5)")
	require.NoError(t, err)
	loaded, err := s.GetVendorSettlement(admin, draft.ID, 1, 100, true)
	require.NoError(t, err)
	require.Equal(t, "10", loaded.LateUsage.CostUSD)
	require.Equal(t, draft.SnapshotHash, loaded.SnapshotHash)
	paid, err := s.RegisterVendorPayment(admin, draft.ID, VendorSettlementPaymentInput{Version: confirmed.Version, Amount: "12.24", PaidAt: time.Now(), Channel: "offline", Reference: "procurement-usdt", IdempotencyKey: "procurement-payment"})
	require.NoError(t, err)
	require.Equal(t, "paid", paid.Status)
	csv, err = s.VendorSettlementCSV(supplier, draft.ID, false)
	require.NoError(t, err)
	require.Contains(t, string(csv), vendorUsageCostBasis)
	require.Contains(t, string(csv), "12.24")
}

func TestVendorUsageModeAppliesToOpenAIOnly(t *testing.T) {
	s, admin, _ := vendorSettlementPostgres(t)
	f := VendorUsageFilter{VendorID: 1, StartDate: "2026-10-01", EndDate: "2026-10-02", Dimension: "vendor", PageSize: 100}
	for _, tc := range []struct{ platform, mode, want string }{{"anthropic", "", ""}, {"openai", "", "unknown"}, {"openai", "native", "native"}, {"openai", "bps", "bps"}} {
		_, err := s.db.Exec("DELETE FROM usage_logs")
		require.NoError(t, err)
		_, err = s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,platform_snapshot,authorization_mode,created_at,total_cost) VALUES (1,11,$1,NULLIF($2,''),'2026-10-01T12:00:00Z',1)", tc.platform, tc.mode)
		require.NoError(t, err)
		report, err := s.VendorUsage(admin, f, true)
		require.NoError(t, err)
		require.NotEmpty(t, report.Items)
		require.Equal(t, tc.want, report.Items[0].AuthorizationMode)
	}
}

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestVendorSettlementBilateralMigrationKeepsLegacyEvidence(t *testing.T) {
	s, root, _ := vendorSettlementPostgres(t, true)
	vendorSettlementSeedUsage(t, s)
	v, in, f, entries := vendorSettlementFixture(t)
	snap, err := buildVendorSettlement(v, in, f, entries)
	require.NoError(t, err)
	snap.CostBasis = "COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1)"
	raw, err := json.Marshal(snap)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "payer_vendor_id")
	hash, err := vendorHash(snap)
	require.NoError(t, err)
	var id int64
	require.NoError(t, s.db.QueryRow("INSERT INTO vendor_settlements(number,vendor_id,kind,status,start_at,end_at,timezone,snapshot_at,watermark,contract_version,policy_version,snapshot,snapshot_hash,total,idempotency_key,input_hash,created_by) VALUES('LEGACY',1,'usage','pending_payment',$1,$2,$3,$4,2,1,1,$5,$6,$7,'legacy','legacy',9) RETURNING id", snap.StartAt, snap.EndAt, snap.Timezone, snap.SnapshotAt, string(raw), hash, snap.Total).Scan(&id))
	for i, line := range snap.Lines {
		var lineID int64
		require.NoError(t, s.db.QueryRow("INSERT INTO vendor_settlement_lines(statement_id,group_id,group_name,contract_version,coefficient,cost_usd,amount,usage_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id", id, line.GroupID, line.GroupName, line.ContractVersion, line.Coefficient, line.CostUSD, line.Amount, line.UsageCount).Scan(&lineID))
		entry, err := json.Marshal(entries[i])
		require.NoError(t, err)
		require.NotContains(t, string(entry), "authorization_mode", "legacy allocations retain their frozen JSON shape")
		_, err = s.db.Exec("INSERT INTO vendor_settlement_allocations(statement_id,line_id,usage_log_id,vendor_id,snapshot) VALUES($1,$2,$3,1,$4)", id, lineID, entries[i].UsageLogID, string(entry))
		require.NoError(t, err)
	}
	var oldSnapshot, oldHash string
	require.NoError(t, s.db.QueryRow("SELECT snapshot::text,snapshot_hash FROM vendor_settlements WHERE id=$1", id).Scan(&oldSnapshot, &oldHash))
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "277_vendor_bilateral_settlements.sql"))
	require.NoError(t, err)
	_, err = s.db.Exec(string(migration))
	require.NoError(t, err)
	_, err = s.db.Exec(string(migration))
	require.NoError(t, err)
	var newSnapshot, newHash string
	require.NoError(t, s.db.QueryRow("SELECT snapshot::text,snapshot_hash FROM vendor_settlements WHERE id=$1", id).Scan(&newSnapshot, &newHash))
	require.Equal(t, oldSnapshot, newSnapshot)
	require.Equal(t, oldHash, newHash)
	legacy, err := s.GetVendorSettlement(root, id, 1, 20, true)
	require.NoError(t, err)
	require.Nil(t, legacy.PayerVendorID)
	require.Equal(t, snap.Total, legacy.Total)
	require.Equal(t, snap.CostBasis, legacy.CostBasis)
	require.Equal(t, oldHash, legacy.SnapshotHash)
	_, err = s.VendorSettlementCSV(root, id, true)
	require.NoError(t, err, "old allocation JSON must still satisfy its frozen data hash")
	var downstream int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM vendor_settlements WHERE payer_vendor_id IS NOT NULL").Scan(&downstream))
	require.Zero(t, downstream)
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE vendor_parent_id_snapshot IS NOT NULL").Scan(&downstream))
	require.Zero(t, downstream)
}

func vendorBilateralFixture(t *testing.T) (*VendorService, context.Context, context.Context, context.Context, context.Context) {
	t.Helper()
	s, root, l1 := vendorSettlementPostgres(t)
	_, err := s.db.Exec(
		"INSERT INTO users(id,role,status,session_generation) VALUES(5,'vendor_level2','active',1),(6,'vendor_level2','active',1),(7,'vendor_level2','active',1);" +
			"INSERT INTO vendors(id,user_id,name,status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,contract_version,tier,parent_vendor_id) " +
			"SELECT 3,5,'child A',status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,'child-only',0.9,jsonb_build_object('2','0.5'),3,2,1 FROM vendors WHERE id=1;" +
			"INSERT INTO vendors(id,user_id,name,status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,contract_version,created_at,updated_at,tier,parent_vendor_id) SELECT 4,6,'child B',status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,contract_version,created_at,updated_at,2,2 FROM vendors WHERE id=3;" +
			"INSERT INTO vendors(id,user_id,name,status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,contract_version,created_at,updated_at,tier,parent_vendor_id) SELECT 5,7,'sibling A',status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,contract_version,created_at,updated_at,2,1 FROM vendors WHERE id=3;" +
			"UPDATE vendors SET contract_version=7 WHERE id=1;" +
			"INSERT INTO accounts(id,vendor_id,name,platform,status) VALUES(31,3,'child original','openai','active'),(41,4,'foreign child','openai','active'),(51,5,'sibling','openai','active');" +
			"INSERT INTO usage_logs(request_id,api_key_id,vendor_id_snapshot,account_id,account_name_snapshot,group_id,group_name_snapshot,platform_snapshot,created_at,model,total_cost,actual_cost,authorization_mode) VALUES" +
			"('root',100,1,11,'root',1,'group one','openai','2026-10-01T12:00Z','model',10,99999,NULL)," +
			"('child-one',100,3,31,'child original',1,'group one','openai','2026-10-01T12:00Z','model',100,99999,'native')," +
			"('child-two',100,3,31,'child original',2,'group two','openai','2026-10-02T12:00Z','model',5,99999,'bps')," +
			"('foreign',100,4,41,'foreign child',1,'group one','openai','2026-10-01T12:00Z','model',777,99999,NULL)")
	require.NoError(t, err)
	l2 := authz.WithSubject(context.Background(), authz.Subject{UserID: 5, Role: authz.VendorLevel2, SessionGeneration: 1})
	other := authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel1, SessionGeneration: 1})
	return s, root, l1, l2, other
}

func bilateralInput(id int64) VendorSettlementInput {
	return VendorSettlementInput{VendorIDs: []int64{id}, StartDate: "2026-10-01", EndDate: "2026-10-02", Timezone: "UTC", ExchangeRate: "6.7", ExchangeRateSource: "agreed CNY/USDT", CoefficientUnit: "CNY/USD", SettlementCurrency: "USDT"}
}
func bilateralDraft(t *testing.T, s *VendorService, ctx context.Context, id int64, key string) *VendorSettlement {
	t.Helper()
	p, err := s.PreviewVendorSettlements(ctx, bilateralInput(id))
	require.NoError(t, err)
	require.Empty(t, p[0].Error)
	out, err := s.CreateVendorSettlements(ctx, []VendorSettlementDraftInput{{PreviewToken: p[0].PreviewToken, IdempotencyKey: key}})
	require.NoError(t, err)
	require.Empty(t, out[0].Error)
	require.NotNil(t, out[0].Statement)
	return out[0].Statement
}

func TestVendorSettlementBilateralIndependentBooksAndPermissions(t *testing.T) {
	s, root, l1, l2, other := vendorBilateralFixture(t)
	parent := bilateralDraft(t, s, root, 1, "same-key")
	child := bilateralDraft(t, s, l1, 3, "same-key")
	require.Nil(t, parent.PayerVendorID)
	require.Equal(t, int64(1), *child.PayerVendorID)
	require.Equal(t, "30.74", parent.Total)
	require.Equal(t, "13.80", child.Total)
	require.Equal(t, int64(7), parent.ContractVersion)
	require.Equal(t, int64(3), child.ContractVersion)
	require.Equal(t, int64(3), parent.UsageCount)
	require.Equal(t, int64(2), child.UsageCount)
	var allocations, consumerCharges int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM vendor_settlement_allocations WHERE active").Scan(&allocations))
	require.Equal(t, 5, allocations)
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE api_key_id=100 AND actual_cost=99999").Scan(&consumerCharges))
	require.Equal(t, 4, consumerCharges)
	for _, tc := range []struct {
		ctx context.Context
		id  int64
	}{{root, 1}, {l1, 3}} {
		preview, err := s.PreviewVendorSettlements(tc.ctx, bilateralInput(tc.id))
		require.NoError(t, err)
		require.Empty(t, preview[0].Error)
		require.Zero(t, preview[0].AvailableCount)
	}
	// Payees cannot see drafts; the payer can see and export its own draft.
	_, err := s.GetVendorSettlement(l2, child.ID, 1, 20, false)
	require.ErrorIs(t, err, ErrVendorSettlementNotFound)
	_, err = s.GetVendorSettlement(l1, child.ID, 1, 20, false, "payable")
	require.NoError(t, err)
	_, err = s.VendorSettlementCSV(l1, child.ID, false, "payable")
	require.NoError(t, err)
	_, err = s.GetVendorSettlement(l1, parent.ID, 1, 20, false, "payable")
	require.ErrorIs(t, err, ErrVendorSettlementNotFound)
	for _, tc := range []struct {
		ctx context.Context
		id  int64
	}{{root, 3}, {l1, 1}, {l1, 4}, {other, 3}} {
		p, err := s.PreviewVendorSettlements(tc.ctx, bilateralInput(tc.id))
		require.NoError(t, err)
		require.NotEmpty(t, p[0].Error)
		require.Nil(t, p[0].Statement)
	}
	_, err = s.PreviewVendorSettlements(l2, bilateralInput(3))
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	_, err = s.ConfirmVendorSettlement(l1, parent.ID, VendorSettlementVersionInput{Version: 1})
	require.ErrorIs(t, err, ErrVendorSettlementNotFound)
	_, err = s.ConfirmVendorSettlement(root, child.ID, VendorSettlementVersionInput{Version: 1})
	require.ErrorIs(t, err, ErrVendorSettlementNotFound)
	parent, err = s.ConfirmVendorSettlement(root, parent.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, err)
	child, err = s.ConfirmVendorSettlement(l1, child.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, err)
	receivable, err := s.ListVendorSettlements(l1, VendorSettlementListFilter{}, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), receivable.Total)
	require.Equal(t, parent.ID, receivable.Items[0].ID)
	payable, err := s.ListVendorSettlements(l1, VendorSettlementListFilter{Direction: "payable", VendorID: 3}, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), payable.Total)
	require.Equal(t, child.ID, payable.Items[0].ID)
	_, err = s.ListVendorSettlements(l2, VendorSettlementListFilter{Direction: "payable"}, false)
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	_, err = s.GetVendorSettlement(l2, parent.ID, 1, 20, false)
	require.ErrorIs(t, err, ErrVendorSettlementNotFound)
	_, err = s.VendorSettlementCSV(l2, parent.ID, false)
	require.ErrorIs(t, err, ErrVendorSettlementNotFound)
	own, err := s.GetVendorSettlement(l2, child.ID, 1, 20, false)
	require.NoError(t, err)
	require.Len(t, own.Allocations, 2)
	require.Equal(t, "native", own.Allocations[0].AuthorizationMode)
	require.Equal(t, "bps", own.Allocations[1].AuthorizationMode)
	raw, err := json.Marshal(own)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "1.8")
	require.NotContains(t, string(raw), "99999")
	_, err = s.GetVendorSettlement(other, child.ID, 1, 20, false)
	require.ErrorIs(t, err, ErrVendorSettlementNotFound)
	// Identical external references are independent between platform and L1 books.
	payment := VendorSettlementPaymentInput{Version: parent.Version, Amount: "5", PaidAt: time.Now(), Channel: "wire", Reference: "same-receipt", IdempotencyKey: "pay"}
	parent, err = s.RegisterVendorPayment(root, parent.ID, payment)
	require.NoError(t, err)
	payment.Version = child.Version
	child, err = s.RegisterVendorPayment(l1, child.ID, payment)
	require.NoError(t, err)
	_, err = s.RegisterVendorPayment(l2, child.ID, payment)
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	reverse := VendorSettlementAdjustmentInput{Version: child.Version, Amount: "2", Reason: "entry correction", IdempotencyKey: "reverse"}
	child, err = s.ReverseVendorPayment(l1, child.ID, child.Payments[0].ID, reverse)
	require.NoError(t, err)
	require.Equal(t, "3.00", child.Paid)
	stillParent, err := s.GetVendorSettlement(root, parent.ID, 1, 20, true)
	require.NoError(t, err)
	require.Equal(t, "5.00", stillParent.Paid)
	credit, err := s.AdjustVendorSettlement(l1, child.ID, VendorSettlementAdjustmentInput{Version: child.Version, Amount: "-1", Reason: "agreed correction", IdempotencyKey: "credit"})
	require.NoError(t, err)
	require.Equal(t, int64(1), *credit.PayerVendorID)
	_, err = s.ConfirmVendorSettlement(l1, credit.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, err)
	own, err = s.GetVendorSettlement(l2, child.ID, 1, 20, false)
	require.NoError(t, err)
	require.Equal(t, "1.00", own.Credited)
	require.Equal(t, "9.80", own.Unpaid)
}

func TestVendorSettlementBilateralConcurrentReservationsVoidAndReplay(t *testing.T) {
	s, root, l1, _, _ := vendorBilateralFixture(t)
	parent := bilateralDraft(t, s, root, 1, "platform")
	p, err := s.PreviewVendorSettlements(l1, bilateralInput(3))
	require.NoError(t, err)
	require.Empty(t, p[0].Error)
	var wg sync.WaitGroup
	results := make(chan *VendorSettlement, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := s.CreateVendorSettlements(l1, []VendorSettlementDraftInput{{PreviewToken: p[0].PreviewToken, IdempotencyKey: fmt.Sprintf("race-%d", i)}})
			if err != nil {
				errs <- err
				return
			}
			results <- out[0].Statement
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	successes := 0
	var child *VendorSettlement
	for st := range results {
		if st != nil {
			successes++
			child = st
		}
	}
	require.Equal(t, 1, successes)
	// Releasing one edge must not release the other's claim.
	_, err = s.VoidVendorSettlement(l1, child.ID, VendorSettlementVersionInput{Version: 1, Reason: "retry draft"})
	require.NoError(t, err)
	parentPreview, err := s.PreviewVendorSettlements(root, bilateralInput(1))
	require.NoError(t, err)
	require.Zero(t, parentPreview[0].AvailableCount)
	child = bilateralDraft(t, s, l1, 3, "replacement")
	_, err = s.ConfirmVendorSettlement(root, parent.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, err)
	_, err = s.ConfirmVendorSettlement(l1, child.ID, VendorSettlementVersionInput{Version: 1})
	require.NoError(t, err)
	before, err := s.VendorSettlementCSV(l1, child.ID, false)
	require.NoError(t, err)
	_, err = s.db.Exec("DELETE FROM usage_logs WHERE vendor_id_snapshot=3; UPDATE accounts SET name='changed',deleted_at=NOW() WHERE id=31; UPDATE vendors SET default_coefficient=99,contract_version=99,name='renamed' WHERE id=3")
	require.NoError(t, err)
	after, err := s.VendorSettlementCSV(l1, child.ID, false)
	require.NoError(t, err)
	require.Equal(t, before, after)
	// New usage IDs cannot bypass the durable source fingerprint on either edge.
	_, err = s.db.Exec("INSERT INTO usage_logs(request_id,api_key_id,vendor_id_snapshot,account_id,created_at,model,total_cost) VALUES('child-one',100,3,31,'2026-10-01T12:00Z','model',100)")
	require.NoError(t, err)
	for _, tc := range []struct {
		ctx context.Context
		id  int64
	}{{root, 1}, {l1, 3}} {
		p, err := s.PreviewVendorSettlements(tc.ctx, bilateralInput(tc.id))
		require.NoError(t, err)
		require.Empty(t, p[0].Error)
		require.Zero(t, p[0].AvailableCount)
	}
}

func TestVendorSettlementBilateralRootContractAttestations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		total   string
		groups  map[string]string
		periods []VendorSettlementContractPeriod
	}{
		{name: "custom default and group prices", total: "35.08", groups: map[string]string{"2": "3"}},
		{name: "explicitly clear group overrides", total: "34.33", groups: map[string]string{}},
		{name: "explicit historical periods", total: "18.66", periods: []VendorSettlementContractPeriod{
			{StartDate: "2026-10-01", EndDate: "2026-10-01", ContractVersion: 1, DefaultCoefficient: "1"},
			{StartDate: "2026-10-02", EndDate: "2026-10-02", ContractVersion: 6, DefaultCoefficient: "2", GroupCoefficients: map[string]string{"2": "3"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, root, _, _, _ := vendorBilateralFixture(t)
			in := bilateralInput(1)
			in.DefaultCoefficient, in.GroupCoefficients, in.ContractPeriods = "2", tc.groups, tc.periods
			in.ExchangeRateSource = "  agreed historical contract and CNY/USDT rate  "
			preview, err := s.PreviewVendorSettlements(root, in)
			require.NoError(t, err)
			require.Empty(t, preview[0].Error)
			require.Equal(t, tc.total, preview[0].Statement.Total)
			token, err := s.vendorPreviewParse(preview[0].PreviewToken)
			require.NoError(t, err)
			require.Nil(t, token.Input.PayerVendorID)
			require.Equal(t, int64(7), token.Input.ContractVersion)
			require.Equal(t, "2", token.Input.DefaultCoefficient)
			require.Equal(t, len(tc.periods), len(token.Input.ContractPeriods))
			drafts, err := s.CreateVendorSettlements(root, []VendorSettlementDraftInput{{PreviewToken: preview[0].PreviewToken, IdempotencyKey: "attested"}})
			require.NoError(t, err)
			require.Empty(t, drafts[0].Error)
			require.NotNil(t, drafts[0].Statement)
			detail, err := s.GetVendorSettlement(root, drafts[0].Statement.ID, 1, 20, true)
			require.NoError(t, err)
			require.Equal(t, token.SnapshotHash, detail.SnapshotHash)
			require.Equal(t, tc.total, detail.Total)
			require.Equal(t, "agreed historical contract and CNY/USDT rate", detail.ExchangeRateSource)
			if len(tc.periods) > 0 {
				require.Equal(t, tc.periods[0].ContractVersion, detail.Allocations[0].ContractVersion)
				require.Equal(t, tc.periods[1].ContractVersion, detail.Allocations[len(detail.Allocations)-1].ContractVersion)
			}
		})
	}
}

func TestVendorSettlementBilateralCurrentPricesAndActorRevocation(t *testing.T) {
	s, _, l1, l2, _ := vendorBilateralFixture(t)
	p, err := s.PreviewVendorSettlements(l1, bilateralInput(3))
	require.NoError(t, err)
	require.Empty(t, p[0].Error)
	require.Equal(t, int64(3), p[0].Statement.ContractVersion)
	require.Equal(t, "13.80", p[0].Statement.Total)
	token, err := s.vendorPreviewParse(p[0].PreviewToken)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		change func(*VendorSettlementInput)
	}{
		{"default price", func(in *VendorSettlementInput) { in.DefaultCoefficient = "1.8" }},
		{"group price", func(in *VendorSettlementInput) { in.GroupCoefficients = map[string]string{"2": "1.6"} }},
		{"version", func(in *VendorSettlementInput) { in.ContractVersion = 7 }},
		{"historical periods", func(in *VendorSettlementInput) {
			in.ContractPeriods = []VendorSettlementContractPeriod{{StartDate: in.StartDate, EndDate: in.EndDate, ContractVersion: 3, DefaultCoefficient: "0.9"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := bilateralInput(3)
			tc.change(&in)
			preview, err := s.PreviewVendorSettlements(l1, in)
			require.NoError(t, err)
			require.Equal(t, "invalid_or_unavailable", preview[0].Error)
			require.Empty(t, preview[0].PreviewToken)
			// Even a server-signed payload must pass the same current-contract
			// validation at create. Rejection cannot rely only on a hash mismatch.
			changed := token
			tc.change(&changed.Input)
			signed, err := s.vendorPreviewSign(changed)
			require.NoError(t, err)
			drafts, err := s.CreateVendorSettlements(l1, []VendorSettlementDraftInput{{PreviewToken: signed, IdempotencyKey: "forbidden-attestation"}})
			require.NoError(t, err)
			require.Equal(t, "invalid_or_unavailable", drafts[0].Error)
			require.Nil(t, drafts[0].Statement)
		})
	}
	in := bilateralInput(3)
	wrong := int64(2)
	in.PayerVendorID = &wrong
	_, err = s.PreviewVendorSettlements(l1, in)
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	p, err = s.PreviewVendorSettlements(l1, bilateralInput(3))
	require.NoError(t, err)
	require.Empty(t, p[0].Error)
	_, err = s.db.Exec("UPDATE vendors SET default_coefficient=0.8,contract_version=4 WHERE id=3")
	require.NoError(t, err)
	out, err := s.CreateVendorSettlements(l1, []VendorSettlementDraftInput{{PreviewToken: p[0].PreviewToken, IdempotencyKey: "stale"}})
	require.NoError(t, err)
	require.NotEmpty(t, out[0].Error)
	p, err = s.PreviewVendorSettlements(l1, bilateralInput(3))
	require.NoError(t, err)
	require.Empty(t, p[0].Error)
	readOnly := authz.WithVendorScope(l1, authz.VendorScope{VendorID: 3, ActorUserID: 3, PolicyVersion: 1, ReadOnly: true})
	_, err = s.CreateVendorSettlements(readOnly, []VendorSettlementDraftInput{{PreviewToken: p[0].PreviewToken, IdempotencyKey: "readonly"}})
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	_, err = s.db.Exec("UPDATE users SET session_generation=2 WHERE id=3")
	require.NoError(t, err)
	_, err = s.CreateVendorSettlements(l1, []VendorSettlementDraftInput{{PreviewToken: p[0].PreviewToken, IdempotencyKey: "revoked"}})
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	_, err = s.db.Exec("UPDATE vendors SET status='disabled' WHERE id=1")
	require.NoError(t, err)
	_, err = s.ListVendorSettlements(l2, VendorSettlementListFilter{}, false)
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
}

// Raw retention must not let a retry switch payees and charge the same payer
// twice. The durable native source identity survives both raw IDs and routing.
func TestVendorSettlementBilateralReplayCannotChangePayee(t *testing.T) {
	for _, tc := range []struct {
		name            string
		direct, account int64
		payee           int64
		platform        bool
	}{
		{"sibling payee", 5, 51, 5, false},
		{"another root payee", 4, 41, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, root, l1, _, _ := vendorBilateralFixture(t)
			platform := bilateralDraft(t, s, root, 1, "original-platform")
			supplier := bilateralDraft(t, s, l1, 3, "original-supplier")
			_, err := s.db.Exec("DELETE FROM usage_logs WHERE request_id IN ('child-one','foreign')")
			require.NoError(t, err)
			_, err = s.db.Exec("INSERT INTO usage_logs(request_id,api_key_id,vendor_id_snapshot,account_id,created_at,model,total_cost) VALUES('child-one',100,$1,$2,'2026-10-01T12:00Z','model',100)", tc.direct, tc.account)
			require.NoError(t, err)
			actor := l1
			original := supplier
			if tc.platform {
				actor = root
				original = platform
			}
			preview, err := s.PreviewVendorSettlements(actor, bilateralInput(tc.payee))
			require.NoError(t, err)
			require.Empty(t, preview[0].Error)
			require.Zero(t, preview[0].AvailableCount, "same payer must retain the original native source reservation across payees")
			// Database uniqueness must also reject an insert that bypasses the
			// preview filter, including a legacy row with no source fingerprint.
			_, err = s.db.Exec("INSERT INTO usage_logs(request_id,api_key_id,vendor_id_snapshot,account_id,created_at,model,total_cost) VALUES('fresh-target',100,$1,$2,'2026-10-01T12:00Z','model',1)", tc.direct, tc.account)
			require.NoError(t, err)
			target := bilateralDraft(t, s, actor, tc.payee, "replayed-payee")
			var lineID int64
			require.NoError(t, s.db.QueryRow("SELECT id FROM vendor_settlement_lines WHERE statement_id=$1 LIMIT 1", target.ID).Scan(&lineID))
			for _, source := range []bool{false, true} {
				usageID, fingerprint := "a.usage_log_id", "NULL"
				if source {
					usageID, fingerprint = "(SELECT id FROM usage_logs WHERE request_id='child-one')", "a.source_fingerprint"
				}
				_, err = s.db.Exec("INSERT INTO vendor_settlement_allocations(statement_id,line_id,usage_log_id,vendor_id,snapshot,source_fingerprint,payer_vendor_id) SELECT $1,$2,"+usageID+",$3,a.snapshot,"+fingerprint+",a.payer_vendor_id FROM vendor_settlement_allocations a WHERE a.statement_id=$4 AND a.snapshot->>'account_id'='31' ORDER BY a.usage_log_id LIMIT 1", target.ID, lineID, tc.payee, original.ID)
				var pgErr *pq.Error
				require.ErrorAs(t, err, &pgErr)
				require.Equal(t, pq.ErrorCode("23505"), pgErr.Code)
				book, key := "supplier", "usage"
				if tc.platform {
					book = "platform"
				}
				if source {
					key = "source"
				}
				require.Equal(t, "vendor_allocations_"+book+"_"+key+"_idx", pgErr.Constraint)
			}
		})
	}
}

func TestVendorUsageBilateralSnapshotsAndHistoricalMigration(t *testing.T) {
	s, _, l1, l2, other := vendorBilateralFixture(t)
	_, updateErr := s.db.Exec("UPDATE usage_logs SET account_rate_multiplier=0.5,actual_cost=99999")
	require.NoError(t, updateErr)
	var direct, root, parent int64
	require.NoError(t, s.db.QueryRow("SELECT vendor_id_snapshot,vendor_root_id_snapshot,vendor_parent_id_snapshot FROM usage_logs WHERE request_id='child-one'").Scan(&direct, &root, &parent))
	require.Equal(t, int64(3), direct)
	require.Equal(t, int64(1), root)
	require.Equal(t, int64(1), parent)
	_, err := s.db.Exec("UPDATE usage_logs SET vendor_parent_id_snapshot=2 WHERE request_id='child-one'")
	require.Error(t, err)
	// Current metadata/relations never supply historical filtering or ownership.
	_, err = s.db.Exec("UPDATE accounts SET name='renamed',vendor_id=2 WHERE id=31; UPDATE vendors SET name='renamed child' WHERE id=3")
	require.NoError(t, err)
	f := VendorUsageFilter{StartDate: "2026-10-01", EndDate: "2026-10-02", Timezone: "UTC"}
	tree, err := s.VendorUsage(l1, f, false)
	require.NoError(t, err)
	require.Equal(t, "115", tree.Summary.CostUSD)
	own, err := s.VendorUsage(l2, f, false)
	require.NoError(t, err)
	require.Equal(t, "105", own.Summary.CostUSD)
	foreign, err := s.VendorUsage(other, f, false)
	require.NoError(t, err)
	require.Equal(t, "777", foreign.Summary.CostUSD)
	f.VendorID = 3
	child, err := s.VendorUsage(l1, f, false)
	require.NoError(t, err)
	require.Equal(t, "105", child.Summary.CostUSD)
	f.VendorID = 4
	_, err = s.VendorUsage(l1, f, false)
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	f.VendorID = 1
	_, err = s.VendorUsage(l2, f, false)
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	// Simulate a pre-277 log lacking hierarchy. Reapplying 277 may preserve only
	// its known direct owner; it must not infer a second payable to the child.
	_, err = s.db.Exec("ALTER TABLE usage_logs DISABLE TRIGGER usage_logs_vendor_hierarchy; INSERT INTO usage_logs(vendor_id_snapshot,account_id,created_at,model,total_cost) VALUES(3,31,'2026-09-01','legacy',7),(NULL,11,'2026-09-01','unowned',7); ALTER TABLE usage_logs ENABLE TRIGGER usage_logs_vendor_hierarchy")
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "277_vendor_bilateral_settlements.sql"))
	require.NoError(t, err)
	// Migration's one-time backfill is not a runtime ownership update.
	_, err = s.db.Exec("ALTER TABLE usage_logs DISABLE TRIGGER usage_logs_vendor_hierarchy")
	require.NoError(t, err)
	_, err = s.db.Exec(string(migration))
	require.NoError(t, err)
	var oldRoot int64
	var oldParent *int64
	require.NoError(t, s.db.QueryRow("SELECT vendor_root_id_snapshot,vendor_parent_id_snapshot FROM usage_logs WHERE model='legacy'").Scan(&oldRoot, &oldParent))
	require.Equal(t, int64(3), oldRoot)
	require.Nil(t, oldParent)
	in := bilateralInput(3)
	in.StartDate, in.EndDate = "2026-09-01", "2026-09-01"
	p, err := s.PreviewVendorSettlements(l1, in)
	require.NoError(t, err)
	require.Empty(t, p[0].Error)
	require.Zero(t, p[0].AvailableCount)
	var unknown int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE model='unowned' AND vendor_id_snapshot IS NULL AND vendor_root_id_snapshot IS NULL AND vendor_parent_id_snapshot IS NULL").Scan(&unknown))
	require.Equal(t, 1, unknown)
}

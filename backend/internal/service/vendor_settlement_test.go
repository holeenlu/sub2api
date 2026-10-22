package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func vendorSettlementFixture(t *testing.T) (*Vendor, VendorSettlementInput, VendorUsageFilter, []VendorUsageEntry) {
	t.Helper()
	v := &Vendor{ID: 1, Tier: 1, Name: "supplier", PolicyVersion: 1, ContractVersion: 1, DefaultCoefficient: "1.8", GroupCoefficients: map[string]string{"2": "1.6"}, BinanceID: "00123456789012345678"}
	in := VendorSettlementInput{StartDate: "2026-10-01", EndDate: "2026-10-02", ExchangeRate: "6.7", ExchangeRateSource: "agreed contract", CoefficientUnit: "CNY/USD", SettlementCurrency: "USDT", ContractVersion: 1}
	require.NoError(t, normalizeVendorSettlementInput(&in, v))
	f, e := vendorSettlementFilter(in, 1, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	require.NoError(t, e)
	g1, g2 := int64(1), int64(2)
	entries := []VendorUsageEntry{{UsageLogID: 1, VendorID: 1, AccountID: 9, AccountName: "original", GroupID: &g1, GroupName: "first", CostUSD: "6994.09", CreatedAt: f.start}, {UsageLogID: 2, VendorID: 1, AccountID: 10, GroupID: &g2, GroupName: "second", CostUSD: "2036.31", CreatedAt: f.start}}
	return v, in, f, entries
}
func TestVendorSettlementDecimalContractAndFrozenHash(t *testing.T) {
	v, in, f, entries := vendorSettlementFixture(t)
	snap, e := buildVendorSettlement(v, in, f, entries)
	require.NoError(t, e)
	require.Equal(t, "1879.01", snap.Lines[0].Amount)
	require.Equal(t, "486.28", snap.Lines[1].Amount)
	require.Equal(t, "2365.29", snap.Total)
	hash, e := vendorHash(snap)
	require.NoError(t, e)
	v.BinanceID = "changed"
	v.DefaultCoefficient = "9"
	v.Name = "new name"
	require.Equal(t, "00123456789012345678", snap.Payee.BinanceID)
	require.Equal(t, "supplier", snap.Payee.VendorName)
	encoded, e := json.Marshal(snap)
	require.NoError(t, e)
	var restored VendorSettlementSnapshot
	require.NoError(t, json.Unmarshal(encoded, &restored))
	hash2, e := vendorHash(restored)
	require.NoError(t, e)
	require.Equal(t, hash, hash2)
	in.Adjustment = "-0.01"
	in.AdjustmentReason = "rounding difference"
	snap, e = buildVendorSettlement(v, in, f, entries)
	require.NoError(t, e)
	require.Equal(t, "2365.28", snap.Total)
}
func TestVendorSettlementHalfUpAtGroupNotRequest(t *testing.T) {
	v, in, f, entries := vendorSettlementFixture(t)
	in.ExchangeRate = "1"
	in.DefaultCoefficient = "1"
	in.GroupCoefficients = map[string]string{}
	entries[0].CostUSD = "0.005"
	entries[1].CostUSD = "0.005"
	snap, e := buildVendorSettlement(v, in, f, entries)
	require.NoError(t, e)
	require.Equal(t, "0.02", snap.Total)
	entries[1].GroupID = entries[0].GroupID
	snap, e = buildVendorSettlement(v, in, f, entries)
	require.NoError(t, e)
	require.Equal(t, "0.01", snap.Total)
	entries[1].VendorID = 2
	_, e = buildVendorSettlement(v, in, f, entries)
	require.ErrorIs(t, e, ErrVendorSettlementConflict)
}
func TestVendorSettlementDecimalValidation(t *testing.T) {
	for _, val := range []string{"NaN", "1e3", "-1", "1.001", "9999999999999999999", "+1", " 1", "0x10"} {
		_, e := vendorDecimal(val, 2, false)
		require.Error(t, e, val)
	}
	_, e := vendorDecimal("0.000001", 10, false)
	require.NoError(t, e)
	v, in, _, _ := vendorSettlementFixture(t)
	in.ContractVersion = 2
	require.Error(t, normalizeVendorSettlementInput(&in, v))
	in.ContractVersion = 1
	in.ExchangeRate = "0"
	require.Error(t, normalizeVendorSettlementInput(&in, v))
}
func TestVendorSettlementPreviewIntegrity(t *testing.T) {
	s := &VendorService{cfg: &config.Config{JWT: config.JWTConfig{Secret: "test-only-not-production-secret"}}}
	p := vendorSettlementToken{VendorID: 1, ActorID: 9, SnapshotHash: "hash", ExpiresAt: time.Now().Add(time.Minute)}
	token, e := s.vendorPreviewSign(p)
	require.NoError(t, e)
	got, e := s.vendorPreviewParse(token)
	require.NoError(t, e)
	require.Equal(t, p.VendorID, got.VendorID)
	_, e = s.vendorPreviewParse("A" + token[1:])
	require.Error(t, e)
	_, e = s.vendorPreviewParse(strings.Repeat("x", 32769))
	require.Error(t, e)
	s.cfg.JWT.Secret = "changed-server-signing-secret"
	_, e = s.vendorPreviewParse(token)
	require.Error(t, e)
}
func TestVendorSettlementReceiptNormalizationAndState(t *testing.T) {
	a, e := normalizeVendorReceipt(" Receipt 001 ", 128)
	require.NoError(t, e)
	b, e := normalizeVendorReceipt("receipt001", 128)
	require.NoError(t, e)
	require.Equal(t, a, b)
	require.Equal(t, "pending_payment", vendorSettlementState(decimal.NewFromInt(100), decimal.Zero, decimal.Zero))
	require.Equal(t, "partially_paid", vendorSettlementState(decimal.NewFromInt(100), decimal.NewFromInt(30), decimal.Zero))
	require.Equal(t, "paid", vendorSettlementState(decimal.NewFromInt(100), decimal.NewFromInt(30), decimal.NewFromInt(70)))
}
func vendorSettlementSQLFixture(t *testing.T, status string, version int64, paid string) (*VendorService, sqlmock.Sqlmock, context.Context, *VendorSettlement) {
	t.Helper()
	db, mock, e := sqlmock.New()
	require.NoError(t, e)
	t.Cleanup(func() { db.Close() })
	s := &VendorService{db: db}
	v, in, f, entries := vendorSettlementFixture(t)
	snap, e := buildVendorSettlement(v, in, f, entries)
	require.NoError(t, e)
	hash, e := vendorHash(snap)
	require.NoError(t, e)
	raw, e := json.Marshal(snap)
	require.NoError(t, e)
	ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	mock.ExpectQuery("SELECT role, status, session_generation FROM users").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"role", "status", "session_generation"}).AddRow("super_admin", "active", int64(1)))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT vendor_id,parent_statement_id").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"vendor_id", "parent_statement_id", "payer_vendor_id"}).AddRow(int64(1), nil, nil))
	mock.ExpectQuery("SELECT id FROM vendors.*FOR UPDATE").WithArgs(int64(1), nil).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectQuery("SELECT role, status, session_generation FROM users").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"role", "status", "session_generation"}).AddRow("super_admin", "active", int64(1)))
	mock.ExpectQuery("SELECT v.id,v.user_id,v.name").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "status", "policy_version", "platforms", "account_limit", "proxy_limit", "allow_direct", "contact", "email", "binance", "coefficient", "groups", "contract", "created", "updated", "tier", "parent", "login_email", "username", "openai_authorization_modes"}).AddRow(1, 3, "supplier", "active", 1, "[]", 10, 10, false, "", "", "", "1.8", "{}", 1, time.Now(), time.Now(), 1, nil, "test@invalid", "test", "[]"))
	mock.ExpectQuery("SELECT id,number,status,version,snapshot.*FOR UPDATE").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "number", "status", "version", "snapshot", "snapshot_hash", "total", "paid", "input_hash", "created_by", "confirmed_by", "created_at", "confirmed_at", "void_reason", "vendor_id", "payer_vendor_id"}).AddRow(int64(7), "VS-test", status, version, string(raw), hash, snap.Total, paid, "input", int64(9), int64(9), time.Now(), time.Now(), "", int64(1), nil))
	return s, mock, ctx, &VendorSettlement{VendorSettlementSnapshot: snap, ID: 7, Status: status, Version: version, Paid: paid}
}
func TestVendorSettlementStaleVersionCannotPay(t *testing.T) {
	s, mock, ctx, _ := vendorSettlementSQLFixture(t, "pending_payment", 2, "0.00")
	mock.ExpectQuery("SELECT input_hash FROM vendor_settlement_payments").WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	_, e := s.RegisterVendorPayment(ctx, 7, VendorSettlementPaymentInput{Version: 1, Amount: "1", PaidAt: time.Now(), Channel: "offline", Reference: "receipt", IdempotencyKey: "pay:1"})
	require.ErrorIs(t, e, ErrVendorSettlementConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestVendorSettlementOverpaymentAndPaidVoid(t *testing.T) {
	t.Run("overpayment", func(t *testing.T) {
		s, mock, ctx, _ := vendorSettlementSQLFixture(t, "partially_paid", 3, "2300.00")
		mock.ExpectQuery("SELECT input_hash FROM vendor_settlement_payments").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT COALESCE.*kind='credit'").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow("0"))
		mock.ExpectRollback()
		_, e := s.RegisterVendorPayment(ctx, 7, VendorSettlementPaymentInput{Version: 3, Amount: "65.30", PaidAt: time.Now(), Channel: "offline", Reference: "receipt", IdempotencyKey: "pay:1"})
		require.ErrorIs(t, e, ErrVendorSettlementConflict)
		require.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("paid_void", func(t *testing.T) {
		s, mock, ctx, _ := vendorSettlementSQLFixture(t, "partially_paid", 3, "1.00")
		mock.ExpectRollback()
		_, e := s.VoidVendorSettlement(ctx, 7, VendorSettlementVersionInput{Version: 3, Reason: "mistake"})
		require.ErrorIs(t, e, ErrVendorSettlementConflict)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestVendorSettlementHistoricalContractsSplitAndRequireCoverage(t *testing.T) {
	v, in, f, entries := vendorSettlementFixture(t)
	v.ContractVersion = 2
	in.ContractVersion = 2
	entries[1].GroupID = entries[0].GroupID
	entries[0].CostUSD = "10"
	entries[1].CostUSD = "10"
	entries[1].CreatedAt = f.start.AddDate(0, 0, 1)
	in.ExchangeRate = "1"
	in.ContractPeriods = []VendorSettlementContractPeriod{{StartDate: in.StartDate, EndDate: in.StartDate, ContractVersion: 1, DefaultCoefficient: "1"}, {StartDate: in.EndDate, EndDate: in.EndDate, ContractVersion: 2, DefaultCoefficient: "2"}}
	snap, e := buildVendorSettlement(v, in, f, entries)
	require.NoError(t, e)
	require.Len(t, snap.Lines, 2)
	require.Equal(t, "30.00", snap.Total)
	require.Equal(t, int64(1), entries[0].ContractVersion)
	require.Equal(t, int64(2), entries[1].ContractVersion)
	in.ContractPeriods[1].StartDate = in.StartDate
	_, e = buildVendorSettlement(v, in, f, entries)
	require.Error(t, e, "overlapping historical contracts must fail")
	in.ContractPeriods = in.ContractPeriods[:1]
	_, e = buildVendorSettlement(v, in, f, entries)
	require.Error(t, e, "gaps must fail")
	in.ContractPeriods[0].EndDate = in.EndDate
	in.ContractPeriods[0].DefaultCoefficient = ""
	_, e = buildVendorSettlement(v, in, f, entries)
	require.Error(t, e, "historical rates cannot inherit today's default")
}

func TestVendorSettlementPayableOnlyCurrentServerContract(t *testing.T) {
	v, in, f, entries := vendorSettlementFixture(t)
	payer := int64(2)
	v.Tier, v.ParentVendorID = 2, &payer
	in.PayerVendorID = &payer
	for i := range entries {
		entries[i].RootVendorID, entries[i].ParentVendorID = &payer, &payer
	}
	in.ContractPeriods = []VendorSettlementContractPeriod{{StartDate: in.StartDate, EndDate: in.EndDate, ContractVersion: 1, DefaultCoefficient: "0"}}
	require.ErrorIs(t, normalizeVendorSettlementInput(&in, v), ErrVendorUsageInput)
	_, err := buildVendorSettlement(v, in, f, entries)
	require.ErrorIs(t, err, ErrVendorUsageInput)
	in.ContractPeriods = nil
	in.DefaultCoefficient = "0"
	require.ErrorIs(t, normalizeVendorSettlementInput(&in, v), ErrVendorUsageInput)
	in.DefaultCoefficient = ""
	in.GroupCoefficients = map[string]string{"2": "0"}
	require.ErrorIs(t, normalizeVendorSettlementInput(&in, v), ErrVendorUsageInput)
	in.GroupCoefficients = nil
	require.NoError(t, normalizeVendorSettlementInput(&in, v))
	snap, err := buildVendorSettlement(v, in, f, entries)
	require.NoError(t, err)
	require.Len(t, snap.ContractPeriods, 1)
	require.Equal(t, v.ContractVersion, snap.ContractPeriods[0].ContractVersion)
	require.Equal(t, "2365.29", snap.Total)
}

package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const vendorPreviewTTL = 15 * time.Minute

var vendorDecimalPattern = regexp.MustCompile("^-?[0-9]{1,18}(\\.[0-9]{1,20})?$")
var vendorKeyPattern = regexp.MustCompile("^[a-zA-Z0-9_.:/-]{1,128}$")

type VendorSettlementInput struct {
	PayerVendorID      *int64                           `json:"payer_vendor_id,omitempty"`
	ContractPeriods    []VendorSettlementContractPeriod `json:"contract_periods,omitempty"`
	AccountID          int64                            `json:"account_id,omitempty"`
	GroupID            int64                            `json:"group_id,omitempty"`
	Platform           string                           `json:"platform,omitempty"`
	VendorIDs          []int64                          `json:"vendor_ids"`
	StartDate          string                           `json:"start_date"`
	EndDate            string                           `json:"end_date"`
	Timezone           string                           `json:"timezone"`
	ExchangeRate       string                           `json:"exchange_rate"`
	ExchangeRateSource string                           `json:"exchange_rate_source"`
	CoefficientUnit    string                           `json:"coefficient_unit"`
	SettlementCurrency string                           `json:"settlement_currency"`
	ContractVersion    int64                            `json:"contract_version"`
	DefaultCoefficient string                           `json:"default_coefficient"`
	GroupCoefficients  map[string]string                `json:"group_coefficients"`
	Adjustment         string                           `json:"adjustment"`
	AdjustmentReason   string                           `json:"adjustment_reason"`
	ParentStatementID  *int64                           `json:"parent_statement_id,omitempty"`
}
type VendorSettlementPayee struct {
	VendorName      string `json:"vendor_name"`
	ContactName     string `json:"contact_name"`
	SettlementEmail string `json:"settlement_email"`
	BinanceID       string `json:"binance_id"`
}
type VendorSettlementLine struct {
	ID              int64  `json:"id,omitempty"`
	GroupID         *int64 `json:"group_id"`
	GroupName       string `json:"group_name"`
	ContractVersion int64  `json:"contract_version"`
	Coefficient     string `json:"coefficient"`
	CostUSD         string `json:"cost_usd"`
	Amount          string `json:"amount"`
	UsageCount      int64  `json:"usage_count"`
	Kind            string `json:"kind"`
	Reason          string `json:"reason,omitempty"`
}
type VendorSettlementSnapshot struct {
	PayerVendorID      *int64                           `json:"payer_vendor_id,omitempty"`
	ContractPeriods    []VendorSettlementContractPeriod `json:"contract_periods"`
	AccountID          int64                            `json:"account_id,omitempty"`
	GroupID            int64                            `json:"group_id,omitempty"`
	Platform           string                           `json:"platform,omitempty"`
	VendorID           int64                            `json:"vendor_id"`
	Kind               string                           `json:"kind"`
	ParentStatementID  *int64                           `json:"parent_statement_id,omitempty"`
	StartAt            time.Time                        `json:"start_at"`
	EndAt              time.Time                        `json:"end_at"`
	Timezone           string                           `json:"timezone"`
	SnapshotAt         time.Time                        `json:"snapshot_at"`
	Watermark          int64                            `json:"watermark"`
	ContractVersion    int64                            `json:"contract_version"`
	PolicyVersion      int64                            `json:"policy_version"`
	Payee              VendorSettlementPayee            `json:"payee"`
	ExchangeRate       string                           `json:"exchange_rate"`
	ExchangeRateSource string                           `json:"exchange_rate_source"`
	CoefficientUnit    string                           `json:"coefficient_unit"`
	SettlementCurrency string                           `json:"settlement_currency"`
	CostUSD            string                           `json:"cost_usd"`
	Subtotal           string                           `json:"subtotal"`
	Adjustment         string                           `json:"adjustment"`
	AdjustmentReason   string                           `json:"adjustment_reason"`
	Total              string                           `json:"total"`
	Lines              []VendorSettlementLine           `json:"lines"`
	UsageCount         int64                            `json:"usage_count"`
	DataHash           string                           `json:"data_hash"`
	CostBasis          string                           `json:"cost_basis"`
	Rounding           string                           `json:"rounding"`
}
type VendorSettlement struct {
	PaymentTotal int64 `json:"payment_total"`
	VendorSettlementSnapshot
	ID              int64                     `json:"id"`
	Number          string                    `json:"number"`
	Status          string                    `json:"status"`
	Version         int64                     `json:"version"`
	Paid            string                    `json:"paid"`
	Credited        string                    `json:"credited"`
	Unpaid          string                    `json:"unpaid"`
	SnapshotHash    string                    `json:"snapshot_hash"`
	CreatedAt       time.Time                 `json:"created_at"`
	ConfirmedAt     *time.Time                `json:"confirmed_at"`
	VoidReason      string                    `json:"void_reason,omitempty"`
	Payments        []VendorSettlementPayment `json:"payments"`
	Allocations     []VendorUsageEntry        `json:"allocations"`
	AllocationTotal int64                     `json:"allocation_total"`
	Page            int                       `json:"page"`
	PageSize        int                       `json:"page_size"`
	LateUsage       VendorSettlementLateUsage `json:"late_usage"`
	CreatedBy       int64                     `json:"-"`
	ConfirmedBy     *int64                    `json:"-"`
	inputHash       string
}
type VendorSettlementLateUsage struct {
	Requests    int64     `json:"requests"`
	CostUSD     string    `json:"cost_usd"`
	CollectedAt time.Time `json:"collected_at"`
}
type VendorSettlementPayment struct {
	ID           int64     `json:"id"`
	Amount       string    `json:"amount"`
	Currency     string    `json:"currency"`
	PaidAt       time.Time `json:"paid_at"`
	Channel      string    `json:"channel"`
	Reference    string    `json:"reference"`
	ReversalOfID *int64    `json:"reversal_of_id,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
type VendorSettlementPreview struct {
	VendorID       int64                     `json:"vendor_id"`
	PreviewToken   string                    `json:"preview_token,omitempty"`
	ExpiresAt      time.Time                 `json:"expires_at"`
	Statement      *VendorSettlementSnapshot `json:"statement,omitempty"`
	AvailableCount int64                     `json:"available_count"`
	OccupiedCount  int64                     `json:"occupied_count"`
	Error          string                    `json:"error,omitempty"`
}
type vendorSettlementToken struct {
	Input        VendorSettlementInput `json:"input"`
	VendorID     int64                 `json:"vendor_id"`
	ActorID      int64                 `json:"actor_id"`
	SnapshotAt   time.Time             `json:"snapshot_at"`
	Watermark    int64                 `json:"watermark"`
	SnapshotHash string                `json:"snapshot_hash"`
	ExpiresAt    time.Time             `json:"expires_at"`
}
type VendorSettlementDraftInput struct {
	PreviewToken   string `json:"preview_token"`
	IdempotencyKey string `json:"idempotency_key"`
}
type VendorSettlementDraftResult struct {
	VendorID  int64             `json:"vendor_id"`
	Statement *VendorSettlement `json:"statement,omitempty"`
	Error     string            `json:"error,omitempty"`
}

func vendorDecimal(v string, scale int32, negative bool) (decimal.Decimal, error) {
	if !vendorDecimalPattern.MatchString(v) {
		return decimal.Zero, ErrVendorUsageInput
	}
	d, e := decimal.NewFromString(v)
	if e != nil || d.Exponent() < -scale || (!negative && d.IsNegative()) || d.Abs().GreaterThanOrEqual(decimal.New(1, 18)) {
		return decimal.Zero, ErrVendorUsageInput
	}
	return d, nil
}
func vendorHash(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func (s *VendorService) vendorPreviewSign(p vendorSettlementToken) (string, error) {
	if s.cfg == nil || len(s.cfg.JWT.Secret) < 16 {
		return "", fmt.Errorf("settlement signing configuration unavailable")
	}
	b, e := json.Marshal(p)
	if e != nil {
		return "", e
	}
	body := base64.RawURLEncoding.EncodeToString(b)
	h := hmac.New(sha256.New, []byte(s.cfg.JWT.Secret))
	h.Write([]byte("vendor-settlement-preview:v1:" + body))
	return body + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}
func (s *VendorService) vendorPreviewParse(token string) (vendorSettlementToken, error) {
	var p vendorSettlementToken
	if len(token) > 32768 || s.cfg == nil || len(s.cfg.JWT.Secret) < 16 {
		return p, ErrVendorUsageInput
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return p, ErrVendorUsageInput
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return p, ErrVendorUsageInput
	}
	h := hmac.New(sha256.New, []byte(s.cfg.JWT.Secret))
	h.Write([]byte("vendor-settlement-preview:v1:" + parts[0]))
	if !hmac.Equal(sig, h.Sum(nil)) {
		return p, ErrVendorUsageInput
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil || json.Unmarshal(b, &p) != nil {
		return p, ErrVendorUsageInput
	}
	return p, nil
}
func vendorSettlementFilter(in VendorSettlementInput, id int64, now time.Time) (VendorUsageFilter, error) {
	f := VendorUsageFilter{VendorID: id, AccountID: in.AccountID, GroupID: in.GroupID, Platform: in.Platform, StartDate: in.StartDate, EndDate: in.EndDate, Timezone: in.Timezone}
	// Settlement defaults end at yesterday, never a silently open billing period.
	if f.StartDate == "" && f.EndDate == "" {
		tz := f.Timezone
		if tz == "" {
			tz = "Asia/Shanghai"
		}
		loc, e := time.LoadLocation(tz)
		if e != nil {
			return f, ErrVendorUsageInput
		}
		end := now.In(loc).AddDate(0, 0, -1)
		f.EndDate = end.Format(time.DateOnly)
		f.StartDate = end.AddDate(0, 0, -6).Format(time.DateOnly)
	}
	err := normalizeVendorUsageFilter(&f, now)
	f.payerVendorID = in.PayerVendorID
	if in.PayerVendorID == nil {
		f.rootVendorID, f.VendorID = id, 0
	} else {
		f.parentVendorID = in.PayerVendorID
	}
	return f, err
}
func normalizeVendorSettlementInput(in *VendorSettlementInput, v *Vendor) error {
	// Preview derives the payer from the live actor; create verifies the signed
	// payer against that actor before reaching this shared validation. Only the
	// platform administrator can attest custom prices and historical periods.
	if (in.PayerVendorID != nil && len(in.ContractPeriods) != 0) || in.ContractVersion <= 0 || in.ContractVersion != v.ContractVersion || in.CoefficientUnit != "CNY/USD" || in.SettlementCurrency != "USDT" {
		return ErrVendorUsageInput
	}
	in.ExchangeRateSource = strings.TrimSpace(in.ExchangeRateSource)
	in.AdjustmentReason = strings.TrimSpace(in.AdjustmentReason)
	if len(in.ExchangeRateSource) == 0 || len(in.ExchangeRateSource) > 512 || len(in.AdjustmentReason) > 2000 {
		return ErrVendorUsageInput
	}
	if in.ExchangeRate == "" {
		in.ExchangeRate = v.DefaultExchangeRate
	}
	rate, e := vendorDecimal(in.ExchangeRate, 10, false)
	if e != nil || !rate.IsPositive() {
		return ErrVendorUsageInput
	}
	in.ExchangeRate = rate.String()
	if in.DefaultCoefficient == "" {
		in.DefaultCoefficient = v.DefaultCoefficient
	}
	coefficient, e := vendorDecimal(in.DefaultCoefficient, 10, false)
	if e != nil {
		return e
	}
	if in.PayerVendorID != nil {
		current, e := vendorDecimal(v.DefaultCoefficient, 10, false)
		if e != nil || !coefficient.Equal(current) {
			return ErrVendorUsageInput
		}
	}
	in.DefaultCoefficient = coefficient.String()
	coefficients := in.GroupCoefficients
	if coefficients == nil {
		coefficients = v.GroupCoefficients
	}
	if len(coefficients) > 1000 || (in.PayerVendorID != nil && len(coefficients) != len(v.GroupCoefficients)) {
		return ErrVendorUsageInput
	}
	in.GroupCoefficients = make(map[string]string, len(coefficients))
	for k, val := range coefficients {
		id, e := strconv.ParseInt(k, 10, 64)
		if e != nil || id <= 0 || k != strconv.FormatInt(id, 10) {
			return ErrVendorUsageInput
		}
		d, e := vendorDecimal(val, 10, false)
		if e != nil {
			return e
		}
		if in.PayerVendorID != nil {
			want, ok := v.GroupCoefficients[k]
			current, e := vendorDecimal(want, 10, false)
			if !ok || e != nil || !d.Equal(current) {
				return ErrVendorUsageInput
			}
		}
		in.GroupCoefficients[k] = d.String()
	}
	if in.Adjustment == "" {
		in.Adjustment = "0"
	}
	a, e := vendorDecimal(in.Adjustment, 2, true)
	if e != nil {
		return e
	}
	in.Adjustment = a.StringFixed(2)
	if !a.IsZero() && in.AdjustmentReason == "" {
		return ErrVendorUsageInput
	}
	if in.ParentStatementID != nil && *in.ParentStatementID <= 0 {
		return ErrVendorUsageInput
	}
	return nil
}
func buildVendorSettlement(v *Vendor, in VendorSettlementInput, f VendorUsageFilter, entries []VendorUsageEntry) (VendorSettlementSnapshot, error) {
	out := VendorSettlementSnapshot{PayerVendorID: in.PayerVendorID, AccountID: in.AccountID, GroupID: in.GroupID, Platform: in.Platform, VendorID: v.ID, Kind: "usage", ParentStatementID: in.ParentStatementID, StartAt: f.start.UTC(), EndAt: f.end.UTC(), Timezone: f.Timezone, SnapshotAt: f.SnapshotAt, Watermark: f.Watermark, ContractVersion: in.ContractVersion, PolicyVersion: v.PolicyVersion, Payee: VendorSettlementPayee{VendorName: v.Name, ContactName: v.ContactName, SettlementEmail: v.SettlementEmail, BinanceID: v.BinanceID}, ExchangeRate: in.ExchangeRate, ExchangeRateSource: in.ExchangeRateSource, CoefficientUnit: in.CoefficientUnit, SettlementCurrency: in.SettlementCurrency, Adjustment: in.Adjustment, AdjustmentReason: in.AdjustmentReason, UsageCount: int64(len(entries)), CostBasis: vendorUsageCostBasis, Rounding: "HALF_UP_2_PER_GROUP_CONTRACT", Lines: []VendorSettlementLine{}}
	periods, err := vendorSettlementContracts(in, f)
	if err != nil {
		return out, err
	}
	out.ContractPeriods = periods
	rate, _ := vendorDecimal(in.ExchangeRate, 10, false)
	adjust, _ := vendorDecimal(in.Adjustment, 2, true)
	costs := map[string]decimal.Decimal{}
	names := map[string]string{}
	counts := map[string]int64{}
	ids := map[string]*int64{}
	versions := map[string]int64{}
	coefficients := map[string]string{}
	totalCost := decimal.Zero
	for i := range entries {
		e := entries[i]
		if !vendorSettlementEntryBelongs(e, v.ID, in.PayerVendorID) {
			return out, ErrVendorSettlementConflict
		}
		cost, err := vendorDecimal(e.CostUSD, 20, false)
		if err != nil {
			return out, err
		}
		version, coefficient, err := vendorSettlementEntryRate(periods, e)
		if err != nil {
			return out, err
		}
		entries[i].ContractVersion = version
		key := vendorSettlementLineKey(e.GroupID, version)
		if previous, ok := coefficients[key]; ok && previous != coefficient {
			return out, ErrVendorUsageInput
		}
		coefficients[key] = coefficient
		versions[key] = version
		costs[key] = costs[key].Add(cost)
		counts[key]++
		ids[key] = e.GroupID
		// Entries are ID ordered, so the latest immutable name is deterministic.
		names[key] = e.GroupName
		totalCost = totalCost.Add(cost)
	}
	keys := make([]string, 0, len(costs))
	for key := range costs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	subtotal := decimal.Zero
	for _, key := range keys {
		coefficient := coefficients[key]
		c, _ := vendorDecimal(coefficient, 10, false)
		amount := costs[key].Mul(c).DivRound(rate, 2)
		if _, e := vendorDecimal(amount.StringFixed(2), 2, false); e != nil {
			return out, e
		}
		out.Lines = append(out.Lines, VendorSettlementLine{GroupID: ids[key], GroupName: names[key], ContractVersion: versions[key], Coefficient: c.String(), CostUSD: costs[key].String(), Amount: amount.StringFixed(2), UsageCount: counts[key], Kind: "usage"})
		subtotal = subtotal.Add(amount)
	}
	total := subtotal.Add(adjust)
	if _, e := vendorDecimal(total.StringFixed(2), 2, false); e != nil {
		return out, e
	}
	out.CostUSD = totalCost.String()
	out.Subtotal = subtotal.StringFixed(2)
	out.Total = total.StringFixed(2)
	hash, e := vendorHash(entries)
	out.DataHash = hash
	return out, e
}

func (s *VendorService) PreviewVendorSettlements(ctx context.Context, in VendorSettlementInput) ([]VendorSettlementPreview, error) {
	actor, payer, err := s.vendorSettlementPayer(ctx, s.db, false)
	if err != nil {
		return nil, err
	}
	if in.PayerVendorID != nil && !vendorSettlementSamePayer(in.PayerVendorID, payer) {
		return nil, ErrVendorUsageForbidden
	}
	in.PayerVendorID = payer
	if len(in.VendorIDs) == 0 || len(in.VendorIDs) > 50 {
		return nil, ErrVendorUsageInput
	}
	out := make([]VendorSettlementPreview, 0, len(in.VendorIDs))
	seen := map[int64]bool{}
	for _, id := range in.VendorIDs {
		if id <= 0 || seen[id] {
			return nil, ErrVendorUsageInput
		}
		seen[id] = true
		p, e := s.previewVendorSettlement(ctx, in, id, actor)
		if e != nil {
			out = append(out, VendorSettlementPreview{VendorID: id, Error: vendorSettlementPublicError(e)})
			continue
		}
		out = append(out, *p)
	}
	return out, nil
}
func (s *VendorService) previewVendorSettlement(ctx context.Context, in VendorSettlementInput, id, actor int64) (*VendorSettlementPreview, error) {
	v, err := vendorSettlementPayee(ctx, s.db, id, in.PayerVendorID)
	if err != nil {
		return nil, err
	}
	// A batch may span different current contracts. Resolve an omitted version
	// for each preview, then freeze it in the signed token; saving still checks
	// that exact version. Explicit versions and historical periods stay strict.
	if in.ContractVersion == 0 {
		in.ContractVersion = v.ContractVersion
	}
	if err = normalizeVendorSettlementInput(&in, v); err != nil {
		return nil, err
	}
	in.VendorIDs = []int64{id}
	f, err := vendorSettlementFilter(in, id, time.Now())
	if err != nil {
		return nil, err
	}
	in.StartDate = f.StartDate
	in.EndDate = f.EndDate
	in.Timezone = f.Timezone
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	v, err = vendorSettlementPayee(ctx, tx, id, in.PayerVendorID)
	if err != nil {
		return nil, err
	}
	if err = normalizeVendorSettlementInput(&in, v); err != nil {
		return nil, err
	}
	if err = vendorUsageWatermark(ctx, tx, &f); err != nil {
		return nil, err
	}
	entries, err := vendorUsageEntries(ctx, tx, f, true, false)
	if err != nil {
		return nil, err
	}
	where, args := vendorUsageWhere(f)
	var occupied int64
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs ul WHERE "+where+" AND "+vendorUsageAllocatedSQL("va", f), args...).Scan(&occupied)
	if err != nil {
		return nil, err
	}
	snap, err := buildVendorSettlement(v, in, f, entries)
	if err != nil {
		return nil, err
	}
	hash, err := vendorHash(snap)
	if err != nil {
		return nil, err
	}
	expiry := time.Now().UTC().Add(vendorPreviewTTL)
	token, err := s.vendorPreviewSign(vendorSettlementToken{Input: in, VendorID: id, ActorID: actor, SnapshotAt: f.SnapshotAt, Watermark: f.Watermark, SnapshotHash: hash, ExpiresAt: expiry})
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	liveActor, livePayer, err := s.vendorSettlementPayer(ctx, s.db, false)
	if err != nil {
		return nil, err
	}
	if liveActor != actor || !vendorSettlementSamePayer(livePayer, in.PayerVendorID) {
		return nil, ErrVendorUsageForbidden
	}
	return &VendorSettlementPreview{VendorID: id, PreviewToken: token, ExpiresAt: expiry, Statement: &snap, AvailableCount: int64(len(entries)), OccupiedCount: occupied}, nil
}
func vendorSettlementPublicError(err error) string {
	if err == nil {
		return ""
	}
	// Batch errors must not expose SQL, receipt details, other owners or payees.
	switch {
	case isVendorConflict(err):
		return "conflict"
	default:
		return "invalid_or_unavailable"
	}
}

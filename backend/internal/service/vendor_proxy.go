package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const VendorProxyMaxRows = 1000 // Native data import/export page cap; reject, never truncate.
const VendorProxyMaxBytes = 1 << 20
const VendorProxyMaxScopeRows = 10000

var ErrVendorProxyVersion = infraerrors.Conflict("PROXY_VERSION_CONFLICT", "proxy changed; reload and retry")
var ErrVendorProxyUnavailable = infraerrors.Conflict("PROXY_UNAVAILABLE", "proxy is disabled or expired")
var ErrVendorProxyWorkbenchRequired = infraerrors.Conflict("PROXY_WORKBENCH_REQUIRED", "use the supplier proxy workbench for this operation")

// VendorProxySQL accepts native Ent transactions and database/sql transactions.
// All callers MUST use a transaction, not a pool, for lock and binding changes.
type VendorProxySQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Optional native repository extension: keep the existing account probe and
// quota invalidation rules in their single repository implementation.
type VendorProxySnapshotInvalidator interface {
	InvalidateVendorProxySnapshots(context.Context, VendorProxySQL, int64) error
}

// AcquireVendorProxyLock serializes binding, OAuth references, policy changes,
// and proxy mutations across instances. Lock order: advisory, vendor, proxies
// ascending id, then accounts. No network operation belongs inside this lock.
func AcquireVendorProxyLock(ctx context.Context, tx VendorProxySQL, vendorID int64) error {
	if vendorID <= 0 {
		return ErrProxyNotFound
	}
	rows, err := tx.QueryContext(ctx, "SELECT COALESCE(parent_vendor_id,id) FROM vendors WHERE id=$1", vendorID)
	if err != nil {
		return err
	}
	var root int64
	if !rows.Next() {
		rows.Close()
		return ErrVendorNotFound
	}
	err = rows.Scan(&root)
	rows.Close()
	if err != nil {
		return err
	}
	if root != vendorID {
		if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('vendor-proxy:' || $1::text,0))", root); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('vendor-proxy:' || $1::text,0))", vendorID)
	return err
}

const vendorProxyColumns = `p.id,p.name,p.protocol,p.host,p.port,COALESCE(p.username,''),COALESCE(p.password,''),p.status,p.created_at,p.updated_at,p.expires_at,p.fallback_mode,p.backup_proxy_id,p.expiry_warn_days,p.vendor_id,p.created_by,p.vendor_notes,p.region_label_country_code,p.region_label_city,p.config_version`

type proxyScanner interface{ Scan(...any) error }

func scanVendorProxy(row proxyScanner) (*Proxy, error) {
	p := new(Proxy)
	err := row.Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.Port, &p.Username, &p.Password, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.ExpiresAt, &p.FallbackMode, &p.BackupProxyID, &p.ExpiryWarnDays, &p.VendorID, &p.CreatedBy, &p.VendorNotes, &p.RegionLabelCountryCode, &p.RegionLabelCity, &p.ConfigVersion)
	return p, err
}

// ValidateVendorProxyBinding must run after AcquireVendorProxyLock and before
// inserting bindings/OAuth references, in the same transaction. An unavailable
// existing binding may be displayed, but cannot be newly selected.
func ValidateVendorProxyBinding(ctx context.Context, tx VendorProxySQL, vendorID, proxyID int64) (*Proxy, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+vendorProxyColumns+" FROM proxies p WHERE p.id=$1 AND p.vendor_id=$2 AND p.deleted_at IS NULL FOR KEY SHARE", proxyID, vendorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrProxyNotFound
	}
	p, err := scanVendorProxy(rows)
	if err != nil {
		return nil, err
	}
	if !p.IsActive() || p.IsExpired(time.Now()) {
		return nil, ErrVendorProxyUnavailable
	}
	return p, rows.Err()
}

type VendorProxyInput struct {
	FallbackMode           string     `json:"fallback_mode"`
	BackupProxyID          *int64     `json:"backup_proxy_id"`
	ExpiryWarnDays         *int       `json:"expiry_warn_days,omitempty"`
	Name                   string     `json:"name"`
	Protocol               string     `json:"protocol"`
	Host                   string     `json:"host"`
	Port                   int        `json:"port"`
	Username               string     `json:"username,omitempty"`
	Password               string     `json:"password,omitempty"`
	VendorNotes            string     `json:"vendor_notes,omitempty"`
	RegionLabelCountryCode string     `json:"region_label_country_code,omitempty"`
	RegionLabelCity        string     `json:"region_label_city,omitempty"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
	Status                 string     `json:"status,omitempty"`
}
type VendorProxyPatch struct {
	FallbackMode           *string         `json:"fallback_mode"`
	BackupProxyID          json.RawMessage `json:"backup_proxy_id"`
	ExpiryWarnDays         *int            `json:"expiry_warn_days"`
	ConfigVersion          int64           `json:"config_version"`
	Name                   *string         `json:"name"`
	Protocol               *string         `json:"protocol"`
	Host                   *string         `json:"host"`
	Port                   *int            `json:"port"`
	Username               *string         `json:"username"`
	Password               *string         `json:"password"`
	CredentialAction       string          `json:"credential_action"`
	VendorNotes            *string         `json:"vendor_notes"`
	RegionLabelCountryCode *string         `json:"region_label_country_code"`
	RegionLabelCity        *string         `json:"region_label_city"`
	ExpiresAt              json.RawMessage `json:"expires_at"`
	Status                 *string         `json:"status"`
}
type VendorProxyDTO struct {
	FallbackMode           string                   `json:"fallback_mode"`
	BackupProxyID          *int64                   `json:"backup_proxy_id"`
	ExpiryWarnDays         int                      `json:"expiry_warn_days"`
	ID                     int64                    `json:"id"`
	Name                   string                   `json:"name"`
	Protocol               string                   `json:"protocol"`
	Host                   string                   `json:"host"`
	Port                   int                      `json:"port"`
	Endpoint               string                   `json:"endpoint"`
	HasUsername            bool                     `json:"has_username"`
	HasPassword            bool                     `json:"has_password"`
	Status                 string                   `json:"status"`
	EffectiveStatus        string                   `json:"effective_status"`
	VendorID               *int64                   `json:"vendor_id"`
	VendorName             string                   `json:"vendor_name"`
	CreatedBy              *int64                   `json:"created_by"`
	VendorNotes            string                   `json:"vendor_notes"`
	RegionLabelCountryCode string                   `json:"region_label_country_code"`
	RegionLabelCity        string                   `json:"region_label_city"`
	CountryCode            string                   `json:"country_code"`
	City                   string                   `json:"city"`
	RegionSource           string                   `json:"region_source"`
	ConfigVersion          int64                    `json:"config_version"`
	ExpiresAt              *time.Time               `json:"expires_at"`
	CreatedAt              time.Time                `json:"created_at"`
	UpdatedAt              time.Time                `json:"updated_at"`
	AccountCount           int64                    `json:"account_count"`
	AccountIDs             []int64                  `json:"account_ids"`
	LatencyMs              *int64                   `json:"latency_ms"`
	IPAddress              string                   `json:"ip_address,omitempty"`
	TestedAt               *time.Time               `json:"tested_at"`
	Quality                *ProxyQualityCheckResult `json:"quality"`
	References             *VendorProxyReferences   `json:"references,omitempty"`
}
type VendorProxyReferences struct {
	Accounts []int64 `json:"accounts"`
	Backups  []int64 `json:"backups"`
	OAuth    int64   `json:"oauth"`
	Inflight int64   `json:"inflight"`
	Total    int64   `json:"total"`
}
type VendorProxySummary struct {
	Total               int `json:"total"`
	Occupied            int `json:"occupied"`
	OccupiedUnavailable int `json:"occupied_unavailable"`
	Unoccupied          int `json:"unoccupied"`
}
type VendorProxyFilter struct {
	VendorID  int64  `json:"vendor_id" form:"vendor_id"`
	Page      int    `json:"page" form:"page"`
	PageSize  int    `json:"page_size" form:"page_size"`
	Search    string `json:"search" form:"search"`
	Protocol  string `json:"protocol" form:"protocol"`
	Status    string `json:"status" form:"status"`
	Country   string `json:"country" form:"country"`
	Occupancy string `json:"occupancy" form:"occupancy"`
	StartDate string `json:"start_date" form:"start_date"`
	EndDate   string `json:"end_date" form:"end_date"`
	SortBy    string `json:"sort_by" form:"sort_by"`
	SortOrder string `json:"sort_order" form:"sort_order"`
}
type VendorProxyList struct {
	Items        []VendorProxyDTO   `json:"items"`
	Total        int                `json:"total"`
	Page         int                `json:"page"`
	PageSize     int                `json:"page_size"`
	Summary      VendorProxySummary `json:"summary"`
	Capabilities map[string]any     `json:"capabilities"`
	SnapshotAt   time.Time          `json:"snapshot_at"`
	Timezone     string             `json:"timezone"`
}

func (s *VendorService) proxyScope(ctx context.Context) (int64, error) {
	if err := requireVendorWriteScope(ctx); err != nil {
		return 0, err
	}
	return s.proxyOwnedScope(ctx)
}
func (s *VendorService) proxyOwnedScope(ctx context.Context) (int64, error) {
	scope, ok := authz.VendorScopeFromContext(ctx)
	if !ok || scope.VendorID <= 0 {
		return 0, ErrProxyNotFound
	}
	if _, err := s.RequireVendor(ctx, scope.VendorID); err != nil {
		return 0, err
	}
	return scope.VendorID, nil
}

func (s *VendorService) proxyReadScope(ctx context.Context, requested int64) (int64, error) {
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.Children {
		if requested != 0 {
			return 0, ErrVendorDenied
		}
		_, err := s.vendorAccountListScope(ctx)
		return 0, err
	}
	if requested < 0 {
		return 0, ErrProxyNotFound
	}
	// A body filter may narrow an aggregate admin view, but cannot replace
	// the vendor already selected and authorized by the mounted middleware.
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.VendorID > 0 && requested != 0 && requested != scope.VendorID {
		return 0, ErrProxyNotFound
	}
	actor, ok := authz.FromContext(ctx)
	if ok && actor.Role == authz.SuperAdmin {
		if err := s.RequireSuperAdmin(ctx); err != nil {
			return 0, err
		}
		if requested > 0 {
			if _, err := s.RequireVendor(ctx, requested); err != nil {
				return 0, err
			}
			return requested, nil
		}
		if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.VendorID > 0 {
			return s.proxyOwnedScope(ctx)
		}
		return 0, nil
	}
	id, err := s.proxyOwnedScope(ctx)
	if err != nil {
		return 0, err
	}
	if requested != 0 && requested != id {
		return 0, ErrProxyNotFound
	}
	return id, nil
}

func normalizeVendorProxyEndpoint(in *VendorProxyInput) {
	in.Protocol = strings.ToLower(strings.TrimSpace(in.Protocol))
	if in.Protocol == "socks5" {
		in.Protocol = "socks5h"
	}
	in.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in.Host), "."))
	if ip, err := netip.ParseAddr(in.Host); err == nil {
		in.Host = ip.Unmap().String()
	}
}

func validateVendorProxyInput(in *VendorProxyInput) error {
	if in.FallbackMode == "" {
		in.FallbackMode = FallbackModeNone
	}
	// Native supplier fallback never authorizes implicit direct egress.
	if in.FallbackMode != FallbackModeNone && in.FallbackMode != FallbackModeProxy {
		return infraerrors.New(422, "PROXY_FALLBACK_INVALID", "supplier fallback must be none or proxy")
	}
	if in.BackupProxyID != nil && *in.BackupProxyID <= 0 {
		return infraerrors.New(422, "PROXY_BACKUP_REQUIRED", "backup proxy must be a positive id")
	}
	if in.FallbackMode == FallbackModeProxy && in.BackupProxyID == nil {
		return infraerrors.New(422, "PROXY_BACKUP_REQUIRED", "backup proxy required when fallback_mode=proxy")
	}
	if in.FallbackMode == FallbackModeNone && in.BackupProxyID != nil {
		return infraerrors.New(422, "PROXY_FALLBACK_INVALID", "backup proxy requires proxy fallback")
	}
	if in.ExpiryWarnDays == nil {
		days := 7
		in.ExpiryWarnDays = &days
	}
	if *in.ExpiryWarnDays < 0 || *in.ExpiryWarnDays > 2147483647 {
		return infraerrors.New(422, "PROXY_WARN_DAYS_INVALID", "expiry_warn_days must be a nonnegative integer")
	}
	in.Name = strings.TrimSpace(in.Name)
	normalizeVendorProxyEndpoint(in)
	in.RegionLabelCountryCode = strings.ToUpper(strings.TrimSpace(in.RegionLabelCountryCode))
	if in.Status == "" {
		in.Status = StatusActive
	}
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || len(in.Host) > 255 || in.Port < 1 || in.Port > 65535 || utf8.RuneCountInString(in.Username) > 100 || utf8.RuneCountInString(in.Password) > 100 || len(in.VendorNotes) > 4000 || utf8.RuneCountInString(in.RegionLabelCity) > 100 {
		return infraerrors.New(422, "PROXY_INPUT_INVALID", "invalid proxy fields or length")
	}
	if in.Status != StatusActive && in.Status != "inactive" {
		return infraerrors.New(422, "PROXY_STATUS_INVALID", "status must be active or inactive")
	}
	if in.RegionLabelCountryCode != "" && (len(in.RegionLabelCountryCode) != 2 || in.RegionLabelCountryCode[0] < 'A' || in.RegionLabelCountryCode[0] > 'Z' || in.RegionLabelCountryCode[1] < 'A' || in.RegionLabelCountryCode[1] > 'Z') {
		return infraerrors.New(422, "PROXY_COUNTRY_INVALID", "country must be a two-letter code")
	}
	if in.ExpiresAt != nil && (in.ExpiresAt.Year() < 1 || in.ExpiresAt.Year() > 9999) {
		return infraerrors.New(422, "PROXY_EXPIRY_INVALID", "invalid expiry")
	}
	if strings.ContainsAny(in.Host, "/@?#%\\ \t\r\n") || urlvalidator.IsBlockedHost(in.Host) {
		return infraerrors.New(422, "PROXY_HOST_INVALID", "proxy host must be public")
	}
	if ip, err := netip.ParseAddr(in.Host); err == nil {
		if !urlvalidator.IsPublicIP(ip) {
			return infraerrors.New(422, "PROXY_HOST_INVALID", "proxy host must be public")
		}
	}
	u := &url.URL{Scheme: in.Protocol, Host: net.JoinHostPort(in.Host, strconv.Itoa(in.Port))}
	_, parsed, err := proxyurl.Parse(u.String())
	if err != nil {
		return infraerrors.New(422, "PROXY_PROTOCOL_INVALID", "unsupported proxy protocol")
	}
	in.Protocol = parsed.Scheme
	return nil
}

func vendorProxyDTO(p *Proxy, info *ProxyLatencyInfo, now time.Time) VendorProxyDTO {
	d := VendorProxyDTO{FallbackMode: p.FallbackMode, BackupProxyID: p.BackupProxyID, ExpiryWarnDays: p.ExpiryWarnDays, ID: p.ID, Name: p.Name, Protocol: p.Protocol, Host: p.Host, Port: p.Port, Endpoint: (&url.URL{Scheme: p.Protocol, Host: net.JoinHostPort(p.Host, strconv.Itoa(p.Port))}).String(), HasUsername: p.Username != "", HasPassword: p.Password != "", Status: p.Status, EffectiveStatus: "untested", VendorID: p.VendorID, CreatedBy: p.CreatedBy, VendorNotes: p.VendorNotes, RegionLabelCountryCode: p.RegionLabelCountryCode, RegionLabelCity: p.RegionLabelCity, CountryCode: p.RegionLabelCountryCode, City: p.RegionLabelCity, RegionSource: "unknown", ConfigVersion: p.ConfigVersion, ExpiresAt: p.ExpiresAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, AccountIDs: []int64{}}
	if d.CountryCode != "" || d.City != "" {
		d.RegionSource = "label"
	}
	if info != nil && info.ConfigVersion == p.ConfigVersion && now.Sub(info.UpdatedAt) <= 24*time.Hour {
		d.TestedAt = &info.UpdatedAt
		d.LatencyMs = info.LatencyMs
		d.IPAddress = info.IPAddress
		if info.Quality != nil && now.Sub(time.Unix(info.Quality.CheckedAt, 0)) <= 24*time.Hour {
			d.Quality = sanitizeVendorProxyQuality(info.Quality)
		}
		if info.Success {
			d.EffectiveStatus = "active"
		} else {
			d.EffectiveStatus = "failed"
		}
		if info.CountryCode != "" {
			d.CountryCode = info.CountryCode
			d.City = info.City
			d.RegionSource = "detected"
		}
	}
	if p.IsExpired(now) {
		d.EffectiveStatus = "expired"
	}
	if p.Status == StatusExpired {
		d.EffectiveStatus = "expired"
	}
	if p.Status == "inactive" {
		d.EffectiveStatus = "inactive"
	}
	return d
}

func (s *VendorService) proxyCache() ProxyLatencyCache {
	if a, ok := s.admin.(*adminServiceImpl); ok {
		return a.proxyLatencyCache
	}
	return nil
}
func (s *VendorService) vendorProxyRows(ctx context.Context, vID int64) ([]VendorProxyDTO, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+vendorProxyColumns+" FROM proxies p WHERE "+vendorInventoryOwnerSQL(ctx, "p.vendor_id")+" AND p.deleted_at IS NULL ORDER BY p.id LIMIT $2", vID, VendorProxyMaxScopeRows+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var proxies []*Proxy
	var ids []int64
	for rows.Next() {
		p, e := scanVendorProxy(rows)
		if e != nil {
			return nil, e
		}
		proxies = append(proxies, p)
		ids = append(ids, p.ID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(proxies) > VendorProxyMaxScopeRows {
		return nil, infraerrors.New(422, "PROXY_SCOPE_TOO_LARGE", "proxy scope exceeds synchronous workbench limit")
	}
	rows.Close()
	names := map[int64]string{}
	ownerRows, err := s.db.QueryContext(ctx, "SELECT v.id,v.name FROM vendors v WHERE "+vendorInventoryOwnerSQL(ctx, "v.id"), vID)
	if err != nil {
		return nil, err
	}
	for ownerRows.Next() {
		var id int64
		var name string
		if err = ownerRows.Scan(&id, &name); err != nil {
			ownerRows.Close()
			return nil, err
		}
		names[id] = name
	}
	err = ownerRows.Err()
	ownerRows.Close()
	if err != nil {
		return nil, err
	}
	info := map[int64]*ProxyLatencyInfo{}
	if c := s.proxyCache(); c != nil {
		if values, e := c.GetProxyLatencies(ctx, ids); e == nil {
			info = values
		}
	}
	out := make([]VendorProxyDTO, 0, len(proxies))
	byID := map[int64]int{}
	now := time.Now()
	for _, p := range proxies {
		byID[p.ID] = len(out)
		out = append(out, vendorProxyDTO(p, info[p.ID], now))
		if p.VendorID != nil {
			out[len(out)-1].VendorName = names[*p.VendorID]
		}
	}
	ar, err := s.db.QueryContext(ctx, "SELECT a.proxy_id,a.id FROM accounts a JOIN proxies p ON p.id=a.proxy_id WHERE "+vendorInventoryOwnerSQL(ctx, "p.vendor_id")+" AND a.vendor_id=p.vendor_id AND a.deleted_at IS NULL AND p.deleted_at IS NULL ORDER BY a.id", vID)
	if err != nil {
		return nil, err
	}
	defer ar.Close()
	for ar.Next() {
		var pID, aID int64
		if err = ar.Scan(&pID, &aID); err != nil {
			return nil, err
		}
		if i, ok := byID[pID]; ok {
			out[i].AccountIDs = append(out[i].AccountIDs, aID)
			out[i].AccountCount++
		}
	}
	return out, ar.Err()
}

func filterVendorProxies(items []VendorProxyDTO, f VendorProxyFilter, loc *time.Location) ([]VendorProxyDTO, error) {
	var start, end time.Time
	var err error
	if f.StartDate != "" {
		start, err = time.ParseInLocation("2006-01-02", f.StartDate, loc)
		if err != nil {
			return nil, infraerrors.New(422, "PROXY_DATE_INVALID", "invalid start date")
		}
	}
	if f.EndDate != "" {
		end, err = time.ParseInLocation("2006-01-02", f.EndDate, loc)
		if err != nil {
			return nil, infraerrors.New(422, "PROXY_DATE_INVALID", "invalid end date")
		}
		end = end.AddDate(0, 0, 1)
	}
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return nil, infraerrors.New(422, "PROXY_DATE_INVALID", "start date is after end date")
	}
	if f.Occupancy != "" && f.Occupancy != "occupied" && f.Occupancy != "unoccupied" {
		return nil, infraerrors.New(422, "PROXY_FILTER_INVALID", "invalid occupancy")
	}
	if f.SortOrder != "" && f.SortOrder != "asc" && f.SortOrder != "desc" {
		return nil, infraerrors.New(422, "PROXY_FILTER_INVALID", "invalid sort order")
	}
	if f.SortBy == "" {
		f.SortBy = "created_at"
	}
	allowed := map[string]bool{"name": true, "protocol": true, "account_count": true, "latency": true, "expires_at": true, "created_at": true}
	if !allowed[f.SortBy] {
		return nil, infraerrors.New(422, "PROXY_FILTER_INVALID", "invalid sort field")
	}
	out := make([]VendorProxyDTO, 0, len(items))
	search := strings.ToLower(strings.TrimSpace(f.Search))
	for _, p := range items {
		if search != "" && !strings.Contains(strings.ToLower(p.Name+" "+p.Host+" "+p.IPAddress+" "+strconv.FormatInt(p.ID, 10)), search) {
			continue
		}
		if f.Protocol != "" && p.Protocol != f.Protocol && !(f.Protocol == "socks5" && p.Protocol == "socks5h") {
			continue
		}
		if f.Status != "" && p.EffectiveStatus != f.Status {
			continue
		}
		if f.Country == "unknown" && p.CountryCode != "" || f.Country != "" && f.Country != "unknown" && !strings.EqualFold(f.Country, p.CountryCode) {
			continue
		}
		if f.Occupancy == "occupied" && p.AccountCount == 0 || f.Occupancy == "unoccupied" && p.AccountCount > 0 {
			continue
		}
		if !start.IsZero() && p.CreatedAt.Before(start) || !end.IsZero() && !p.CreatedAt.Before(end) {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		cmp := 0
		switch f.SortBy {
		case "name":
			cmp = strings.Compare(a.Name, b.Name)
		case "protocol":
			cmp = strings.Compare(a.Protocol, b.Protocol)
		case "account_count":
			cmp = compareProxyInt(a.AccountCount, b.AccountCount)
		case "latency":
			if a.LatencyMs == nil || b.LatencyMs == nil {
				if a.LatencyMs == nil && b.LatencyMs == nil {
					return a.ID < b.ID
				}
				return a.LatencyMs != nil
			}
			cmp = compareProxyInt(*a.LatencyMs, *b.LatencyMs)
		case "expires_at":
			if a.ExpiresAt == nil || b.ExpiresAt == nil {
				if a.ExpiresAt == nil && b.ExpiresAt == nil {
					return a.ID < b.ID
				}
				return a.ExpiresAt != nil
			}
			cmp = a.ExpiresAt.Compare(*b.ExpiresAt)
		default:
			cmp = a.CreatedAt.Compare(b.CreatedAt)
		}
		if cmp == 0 {
			return a.ID < b.ID
		}
		if f.SortOrder == "asc" {
			return cmp < 0
		}
		return cmp > 0
	})
	return out, nil
}
func compareProxyInt(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func (s *VendorService) proxyLocation() *time.Location {
	if s.cfg != nil {
		if l, e := time.LoadLocation(s.cfg.Timezone); e == nil {
			return l
		}
	}
	return time.UTC
}

func (s *VendorService) ListVendorProxies(ctx context.Context, f VendorProxyFilter) (*VendorProxyList, error) {
	vID, err := s.proxyReadScope(ctx, f.VendorID)
	if err != nil {
		return nil, err
	}
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 20
	}
	if f.Page < 1 || f.Page > 1000000 || (f.PageSize != 10 && f.PageSize != 20 && f.PageSize != 50) {
		return nil, infraerrors.New(422, "PROXY_PAGE_INVALID", "page size must be 10, 20 or 50")
	}
	all, err := s.vendorProxyRows(ctx, vID)
	if err != nil {
		return nil, err
	}
	summary := VendorProxySummary{Total: len(all)}
	for _, p := range all {
		if p.AccountCount > 0 {
			summary.Occupied++
			if p.EffectiveStatus == "inactive" || p.EffectiveStatus == "expired" || p.EffectiveStatus == "failed" {
				summary.OccupiedUnavailable++
			}
		} else {
			summary.Unoccupied++
		}
	}
	filtered, err := filterVendorProxies(all, f, s.proxyLocation())
	if err != nil {
		return nil, err
	}
	total := len(filtered)
	start := (f.Page - 1) * f.PageSize
	if start > total {
		start = total
	}
	end := min(start+f.PageSize, total)
	if _, err = s.proxyReadScope(ctx, f.VendorID); err != nil {
		return nil, err
	}
	return &VendorProxyList{Items: filtered[start:end], Total: total, Page: f.Page, PageSize: f.PageSize, Summary: summary, Capabilities: map[string]any{"protocols": []string{"http", "https", "socks5", "socks5h"}, "max_import_rows": VendorProxyMaxRows, "max_import_bytes": VendorProxyMaxBytes, "max_batch_size": VendorProxyMaxRows, "max_scope_rows": VendorProxyMaxScopeRows, "probe_concurrency": 2, "probe_requests_per_minute": 20, "probe_timeout_seconds": 90, "snapshot_ttl_seconds": 86400}, SnapshotAt: time.Now(), Timezone: s.proxyLocation().String()}, nil
}

func (s *VendorService) ownedProxy(ctx context.Context, id, vID int64) (*Proxy, error) {
	p, err := scanVendorProxy(s.db.QueryRowContext(ctx, "SELECT "+vendorProxyColumns+" FROM proxies p WHERE p.id=$1 AND p.vendor_id=$2 AND p.deleted_at IS NULL", id, vID))
	if err == sql.ErrNoRows {
		return nil, ErrProxyNotFound
	}
	return p, err
}
func (s *VendorService) GetVendorProxy(ctx context.Context, id int64) (*VendorProxyDTO, error) {
	vID, err := s.proxyOwnedScope(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.ownedProxy(ctx, id, vID)
	if err != nil {
		return nil, err
	}
	var info *ProxyLatencyInfo
	if c := s.proxyCache(); c != nil {
		if v, e := c.GetProxyLatencies(ctx, []int64{id}); e == nil {
			info = v[id]
		}
	}
	d := vendorProxyDTO(p, info, time.Now())
	v, err := s.RequireVendor(ctx, vID)
	if err != nil {
		return nil, err
	}
	d.VendorName = v.Name
	refs, err := s.vendorProxyReferences(ctx, s.db, vID, id)
	if err != nil {
		return nil, err
	}
	d.References = refs
	bindings, e := s.db.QueryContext(ctx, "SELECT id FROM accounts WHERE proxy_id=$1 AND vendor_id=$2 AND deleted_at IS NULL ORDER BY id", id, vID)
	if e != nil {
		return nil, e
	}
	defer bindings.Close()
	for bindings.Next() {
		var accountID int64
		if e = bindings.Scan(&accountID); e != nil {
			return nil, e
		}
		d.AccountIDs = append(d.AccountIDs, accountID)
		d.AccountCount++
	}
	if e = bindings.Err(); e != nil {
		return nil, e
	}
	bindings.Close()
	if _, err = s.proxyOwnedScope(ctx); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *VendorService) beginProxyMutation(ctx context.Context, vID int64) (*sql.Tx, error) {
	if err := requireVendorWriteScope(ctx); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err = AcquireVendorProxyLock(ctx, tx, vID); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := s.lockVendorResourceActor(ctx, tx, vID); err != nil {
		tx.Rollback()
		return nil, err
	}
	var version int64
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT policy_version,status FROM vendors WHERE id=$1 FOR UPDATE", vID).Scan(&version, &status); err != nil {
		tx.Rollback()
		return nil, err
	}
	scope, _ := authz.VendorScopeFromContext(ctx)
	if version != scope.PolicyVersion || (!scope.Admin && status != StatusActive) {
		tx.Rollback()
		return nil, ErrVendorProxyVersion
	}
	return tx, nil
}

func commitVendorProxyMutation(ctx context.Context, tx *sql.Tx) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := authz.Revalidate(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// validateVendorProxyFallback runs under beginProxyMutation's native vendor
// advisory lock (also used by expiry, binding and deletion). Each chain node is
// locked and read through the direct owner predicate; no backup credentials are
// loaded. Availability remains the native resolver's responsibility: inactive
// or expired nodes may lead to a live backup but cannot themselves be selected.
func validateVendorProxyFallback(ctx context.Context, tx VendorProxySQL, vendorID, sourceID int64, in VendorProxyInput) error {
	if in.BackupProxyID == nil {
		return nil
	}
	visited := map[int64]bool{}
	if sourceID > 0 {
		visited[sourceID] = true
	}
	next := in.BackupProxyID
	for next != nil {
		if visited[*next] {
			return infraerrors.New(422, "PROXY_BACKUP_SELF", "backup proxy chain cannot contain a cycle")
		}
		if len(visited) >= VendorProxyMaxScopeRows {
			return infraerrors.New(422, "PROXY_SCOPE_TOO_LARGE", "proxy chain exceeds scope limit")
		}
		visited[*next] = true
		rows, err := tx.QueryContext(ctx, "SELECT fallback_mode,backup_proxy_id FROM proxies WHERE id=$1 AND vendor_id=$2 AND deleted_at IS NULL FOR KEY SHARE", *next, vendorID)
		if err != nil {
			return err
		}
		if !rows.Next() {
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			return ErrProxyNotFound
		}
		var mode string
		var backup *int64
		err = rows.Scan(&mode, &backup)
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if mode == FallbackModeDirect {
			return infraerrors.New(422, "PROXY_FALLBACK_INVALID", "supplier fallback cannot lead to direct egress")
		}
		if mode != FallbackModeProxy {
			return nil
		}
		next = backup
	}
	return nil
}

// Check only the direct owner's live inventory while holding its mutation lock.
// Compare canonical transport identities, never IP alone or another owner's
// credentials. The same key is used by import preview and confirmation.
func checkVendorProxyDuplicate(ctx context.Context, tx VendorProxySQL, vendorID, excludeID int64, in VendorProxyInput) error {
	rows, err := tx.QueryContext(ctx, "SELECT protocol,host,port,COALESCE(username,''),COALESCE(password,'') FROM proxies WHERE vendor_id=$1 AND deleted_at IS NULL AND id<>$2 AND port=$3 AND COALESCE(username,'')=$4 AND COALESCE(password,'')=$5", vendorID, excludeID, in.Port, in.Username, in.Password)
	if err != nil {
		return err
	}
	defer rows.Close()
	key := proxyEndpointKey(in)
	for rows.Next() {
		var existing VendorProxyInput
		if err := rows.Scan(&existing.Protocol, &existing.Host, &existing.Port, &existing.Username, &existing.Password); err != nil {
			return err
		}
		if proxyEndpointKey(existing) == key {
			return infraerrors.Conflict("PROXY_DUPLICATE", "proxy already exists")
		}
	}
	return rows.Err()
}

func (s *VendorService) CreateVendorProxy(ctx context.Context, in VendorProxyInput) (*VendorProxyDTO, error) {
	vID, err := s.proxyScope(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateVendorProxyInput(&in); err != nil {
		return nil, err
	}
	tx, err := s.beginProxyMutation(ctx, vID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = validateVendorProxyFallback(ctx, tx, vID, 0, in); err != nil {
		return nil, err
	}
	if err = checkVendorProxyDuplicate(ctx, tx, vID, 0, in); err != nil {
		return nil, err
	}
	if err = ValidateVendorResourceCapacity(ctx, tx, vID, "proxies"); err != nil {
		return nil, err
	}
	scope, _ := authz.VendorScopeFromContext(ctx)
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO proxies(name,protocol,host,port,username,password,status,vendor_id,created_by,vendor_notes,region_label_country_code,region_label_city,expires_at,fallback_mode,backup_proxy_id,expiry_warn_days,config_version,created_at,updated_at) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,1,NOW(),NOW()) RETURNING id`, in.Name, in.Protocol, in.Host, in.Port, in.Username, in.Password, in.Status, vID, scope.ActorUserID, in.VendorNotes, in.RegionLabelCountryCode, in.RegionLabelCity, in.ExpiresAt, in.FallbackMode, in.BackupProxyID, *in.ExpiryWarnDays).Scan(&id)
	if err != nil {
		return nil, err
	}
	if err = commitVendorProxyMutation(ctx, tx); err != nil {
		return nil, err
	}
	s.VendorAudit(ctx, vID, "proxy.create", id, "success")
	p, err := s.ownedProxy(ctx, id, vID)
	if err != nil {
		return nil, err
	}
	d := vendorProxyDTO(p, nil, time.Now())
	return &d, nil
}

func applyVendorProxyPatch(p *Proxy, patch VendorProxyPatch) (VendorProxyInput, error) {
	in := VendorProxyInput{FallbackMode: p.FallbackMode, BackupProxyID: p.BackupProxyID, ExpiryWarnDays: &p.ExpiryWarnDays, Name: p.Name, Protocol: p.Protocol, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password, VendorNotes: p.VendorNotes, RegionLabelCountryCode: p.RegionLabelCountryCode, RegionLabelCity: p.RegionLabelCity, ExpiresAt: p.ExpiresAt, Status: p.Status}
	// Expiry is independent from the user's enable switch. A native expiry
	// sweep stores expired; renewing it must not become an impossible edit.
	if in.Status == StatusExpired {
		in.Status = StatusActive
	}
	if patch.FallbackMode != nil {
		in.FallbackMode = *patch.FallbackMode
		if in.FallbackMode == FallbackModeNone {
			in.BackupProxyID = nil
		}
	}
	if patch.BackupProxyID != nil {
		var id *int64
		if err := json.Unmarshal(patch.BackupProxyID, &id); err != nil {
			return in, infraerrors.New(422, "PROXY_BACKUP_REQUIRED", "backup proxy must be a positive id or null")
		}
		in.BackupProxyID = id
	}
	if patch.ExpiryWarnDays != nil {
		in.ExpiryWarnDays = patch.ExpiryWarnDays
	}
	if patch.Name != nil {
		in.Name = *patch.Name
	}
	if patch.Protocol != nil {
		in.Protocol = *patch.Protocol
	}
	if patch.Host != nil {
		in.Host = *patch.Host
	}
	if patch.Port != nil {
		in.Port = *patch.Port
	}
	if patch.VendorNotes != nil {
		in.VendorNotes = *patch.VendorNotes
	}
	if patch.RegionLabelCountryCode != nil {
		in.RegionLabelCountryCode = *patch.RegionLabelCountryCode
	}
	if patch.RegionLabelCity != nil {
		in.RegionLabelCity = *patch.RegionLabelCity
	}
	if patch.Status != nil {
		in.Status = *patch.Status
	}
	switch patch.CredentialAction {
	case "", "preserve":
		if patch.Username != nil && *patch.Username != "" || patch.Password != nil && *patch.Password != "" {
			return in, infraerrors.New(422, "PROXY_AUTH_ACTION", "choose update to change authentication")
		}
	case "clear":
		in.Username = ""
		in.Password = ""
	case "update":
		if patch.Username != nil {
			in.Username = *patch.Username
		}
		if patch.Password != nil && *patch.Password != "" {
			in.Password = *patch.Password
		}
	default:
		return in, infraerrors.New(422, "PROXY_AUTH_ACTION", "invalid authentication action")
	}
	if patch.ExpiresAt != nil {
		if string(patch.ExpiresAt) == "null" {
			in.ExpiresAt = nil
		} else {
			var t time.Time
			if err := json.Unmarshal(patch.ExpiresAt, &t); err != nil {
				return in, infraerrors.New(422, "PROXY_EXPIRY_INVALID", "expiry must be an RFC3339 timestamp")
			}
			in.ExpiresAt = &t
		}
	}
	return in, validateVendorProxyInput(&in)
}

func (s *VendorService) UpdateVendorProxy(ctx context.Context, id int64, patch VendorProxyPatch) (*VendorProxyDTO, error) {
	vID, err := s.proxyScope(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := s.beginProxyMutation(ctx, vID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := scanVendorProxy(tx.QueryRowContext(ctx, "SELECT "+vendorProxyColumns+" FROM proxies p WHERE p.id=$1 AND p.vendor_id=$2 AND p.deleted_at IS NULL FOR UPDATE", id, vID))
	if err == sql.ErrNoRows {
		return nil, ErrProxyNotFound
	}
	if err != nil {
		return nil, err
	}
	if patch.ConfigVersion <= 0 || p.ConfigVersion != patch.ConfigVersion {
		return nil, ErrVendorProxyVersion
	}
	in, err := applyVendorProxyPatch(p, patch)
	if err != nil {
		return nil, err
	}
	if err = validateVendorProxyFallback(ctx, tx, vID, id, in); err != nil {
		return nil, err
	}
	previous := VendorProxyInput{Protocol: p.Protocol, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password}
	// Historical duplicates must remain editable; only a changed identity can
	// introduce a new collision. Metadata and explicit auth preservation work.
	if proxyEndpointKey(previous) != proxyEndpointKey(in) {
		if err = checkVendorProxyDuplicate(ctx, tx, vID, id, in); err != nil {
			return nil, err
		}
	}
	changed := p.Protocol != in.Protocol || p.Host != in.Host || p.Port != in.Port || p.Username != in.Username || p.Password != in.Password
	if changed {
		if s.rdb == nil {
			return nil, infraerrors.ServiceUnavailable("PROXY_REFERENCES_UNAVAILABLE", "proxy references unavailable")
		}
		claimed, e := s.rdb.ZCount(ctx, vendorProxyReferenceKey(vID, id, "inflight"), strconv.FormatInt(time.Now().UnixMilli()+1, 10), "+inf").Result()
		if e != nil {
			return nil, infraerrors.ServiceUnavailable("PROXY_REFERENCES_UNAVAILABLE", "proxy references unavailable")
		}
		if claimed > 0 {
			return nil, infraerrors.Conflict("PROXY_EXCHANGE_CLAIMED", "proxy is used by an authorization exchange")
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE proxies SET name=$3,protocol=$4,host=$5,port=$6,username=NULLIF($7,''),password=NULLIF($8,''),vendor_notes=$9,region_label_country_code=$10,region_label_city=$11,expires_at=$12,status=$13,fallback_mode=$14,backup_proxy_id=$15,expiry_warn_days=$16,config_version=config_version+1,updated_at=NOW() WHERE id=$1 AND vendor_id=$2`, id, vID, in.Name, in.Protocol, in.Host, in.Port, in.Username, in.Password, in.VendorNotes, in.RegionLabelCountryCode, in.RegionLabelCity, in.ExpiresAt, in.Status, in.FallbackMode, in.BackupProxyID, *in.ExpiryWarnDays)
	if err != nil {
		return nil, err
	}
	// Commit native scheduler invalidation with the endpoint mutation. Account
	// credentials, groups, pause flags, platform status and usage stay untouched.
	if changed || p.Status != in.Status {
		invalidator, ok := s.proxies.(VendorProxySnapshotInvalidator)
		if !ok {
			return nil, infraerrors.ServiceUnavailable("PROXY_INVALIDATION_UNAVAILABLE", "native proxy invalidation unavailable")
		}
		if err = invalidator.InvalidateVendorProxySnapshots(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO scheduler_outbox(event_type,account_id) SELECT 'account_changed',id FROM accounts WHERE proxy_id=$1 AND vendor_id=$2 AND deleted_at IS NULL", id, vID); err != nil {
		return nil, err
	}
	if err = commitVendorProxyMutation(ctx, tx); err != nil {
		return nil, err
	}
	// Preserve metadata-only detection snapshots; transport changes stay stale
	// under their old version even if Redis invalidation is unavailable.
	if !changed {
		if c := s.proxyCache(); c != nil {
			if values, e := c.GetProxyLatencies(ctx, []int64{id}); e == nil && values[id] != nil && values[id].ConfigVersion == p.ConfigVersion {
				v := *values[id]
				v.ConfigVersion++
				_ = c.SetProxyLatency(ctx, id, &v)
			}
		}
	}
	s.InvalidateVendorAccounts(ctx, vID)
	s.VendorAudit(ctx, vID, "proxy.update", id, "success")
	return s.GetVendorProxy(ctx, id)
}

// Reference key never contains credentials, authorization codes or state.
func vendorProxyReferenceKey(vID, pID int64, kind string) string {
	return fmt.Sprintf("vendor:proxy:refs:%d:%d:%s", vID, pID, kind)
}

func (s *VendorService) vendorProxyReferences(ctx context.Context, q VendorProxySQL, vID, id int64) (*VendorProxyReferences, error) {
	r := &VendorProxyReferences{Accounts: []int64{}, Backups: []int64{}}
	rows, err := q.QueryContext(ctx, `SELECT CASE WHEN vendor_id=$2 THEN id ELSE NULL END,'account' FROM accounts WHERE deleted_at IS NULL AND (proxy_id=$1 OR proxy_fallback_origin_id=$1) UNION ALL SELECT CASE WHEN vendor_id=$2 THEN id ELSE NULL END,'backup' FROM proxies WHERE deleted_at IS NULL AND backup_proxy_id=$1`, id, vID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var target sql.NullInt64
		var kind string
		if err = rows.Scan(&target, &kind); err != nil {
			return nil, err
		}
		r.Total++
		if target.Valid {
			if kind == "account" {
				r.Accounts = append(r.Accounts, target.Int64)
			} else {
				r.Backups = append(r.Backups, target.Int64)
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if s.rdb == nil {
		return nil, infraerrors.ServiceUnavailable("PROXY_REFERENCES_UNAVAILABLE", "proxy references unavailable")
	}
	for _, kind := range []string{"oauth", "inflight", "probe"} {
		key := vendorProxyReferenceKey(vID, id, kind)
		n, e := s.rdb.ZCount(ctx, key, strconv.FormatInt(time.Now().UnixMilli()+1, 10), "+inf").Result()
		if e != nil {
			return nil, infraerrors.ServiceUnavailable("PROXY_REFERENCES_UNAVAILABLE", "proxy references unavailable")
		}
		if kind == "oauth" {
			r.OAuth = n
		} else {
			r.Inflight += n
		}
	}
	r.Total += r.OAuth + r.Inflight
	return r, nil
}

// RegisterVendorProxyReference is called while holding AcquireVendorProxyLock,
// after validating the proxy. kind is oauth (unclaimed) or inflight (claimed).
func (s *VendorService) RegisterVendorProxyReference(ctx context.Context, vID, pID int64, kind, leaseID string, expires time.Time) error {
	if (kind != "oauth" && kind != "inflight" && kind != "probe") || leaseID == "" || !expires.After(time.Now()) || expires.After(time.Now().Add(24*time.Hour)) {
		return infraerrors.New(422, "PROXY_LEASE_INVALID", "invalid proxy lease")
	}
	if s.rdb == nil {
		return infraerrors.ServiceUnavailable("PROXY_REFERENCES_UNAVAILABLE", "proxy references unavailable")
	}
	return s.rdb.Eval(ctx, `redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ARGV[1]); redis.call('ZADD',KEYS[1],ARGV[2],ARGV[3]); redis.call('PEXPIRE',KEYS[1],86400000); return 1`, []string{vendorProxyReferenceKey(vID, pID, kind)}, time.Now().UnixMilli(), expires.UnixMilli(), leaseID).Err()
}
func (s *VendorService) ReleaseVendorProxyReference(ctx context.Context, vID, pID int64, kind, leaseID string) error {
	if s.rdb == nil {
		return infraerrors.ServiceUnavailable("PROXY_REFERENCES_UNAVAILABLE", "proxy references unavailable")
	}
	return s.rdb.ZRem(ctx, vendorProxyReferenceKey(vID, pID, kind), leaseID).Err()
}

func (s *VendorService) DeleteVendorProxy(ctx context.Context, id int64) error {
	vID, err := s.proxyScope(ctx)
	if err != nil {
		return err
	}
	tx, err := s.beginProxyMutation(ctx, vID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var deleted sql.NullTime
	if err = tx.QueryRowContext(ctx, "SELECT deleted_at FROM proxies WHERE id=$1 AND vendor_id=$2 FOR UPDATE", id, vID).Scan(&deleted); err == sql.ErrNoRows {
		return ErrProxyNotFound
	} else if err != nil {
		return err
	}
	if deleted.Valid {
		return nil
	}
	refs, err := s.vendorProxyReferences(ctx, tx, vID, id)
	if err != nil {
		return err
	}
	if refs.Total > 0 {
		return ErrProxyInUse
	}
	_, err = tx.ExecContext(ctx, "UPDATE proxies SET deleted_at=NOW(),updated_at=NOW(),config_version=config_version+1 WHERE id=$1 AND vendor_id=$2", id, vID)
	if err != nil {
		return err
	}
	if err = commitVendorProxyMutation(ctx, tx); err != nil {
		return err
	}
	s.VendorAudit(ctx, vID, "proxy.delete", id, "success")
	return nil
}

type VendorProxyBatchItem struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}
type VendorProxyBatchResult struct {
	Items        []VendorProxyBatchItem `json:"items"`
	SuccessCount int                    `json:"success_count"`
	FailedCount  int                    `json:"failed_count"`
}

func (s *VendorService) BatchDeleteVendorProxies(ctx context.Context, ids []int64) (*VendorProxyBatchResult, error) {
	if len(ids) == 0 || len(ids) > VendorProxyMaxRows {
		return nil, infraerrors.New(422, "PROXY_SELECTION_INVALID", "select between 1 and 1000 proxies")
	}
	scope, scoped := authz.VendorScopeFromContext(ctx)
	aggregate := scoped && scope.Admin && scope.VendorID == 0
	var vID int64
	var err error
	if aggregate {
		err = s.RequireSuperAdmin(ctx)
	} else {
		vID, err = s.proxyScope(ctx)
	}
	if err != nil {
		return nil, err
	}
	r := &VendorProxyBatchResult{Items: []VendorProxyBatchItem{}}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		item := VendorProxyBatchItem{ID: id, Status: "deleted"}
		itemCtx, itemVendor := ctx, vID
		var itemErr error
		if aggregate {
			// Resolve each selected object independently, then use the same
			// vendor/proxy locks and reference checks as an individual delete.
			// ResourceVendorID excludes platform objects and rechecks the actor.
			itemVendor, itemErr = s.ResourceVendorID(ctx, "proxies", id)
			if itemErr == nil {
				var v *Vendor
				v, itemErr = s.RequireVendor(ctx, itemVendor)
				if itemErr == nil {
					actor, _ := authz.FromContext(ctx)
					itemCtx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: itemVendor, PolicyVersion: v.PolicyVersion, ActorUserID: actor.UserID, Admin: true})
					itemCtx = authz.WithRevalidation(itemCtx, func(context.Context) error {
						fresh, e := s.RequireVendor(ctx, itemVendor)
						if e != nil {
							return e
						}
						if fresh.PolicyVersion != v.PolicyVersion {
							return ErrVendorProxyVersion
						}
						return nil
					})
				}
			}
		}
		if itemErr == nil {
			itemErr = s.DeleteVendorProxy(itemCtx, id)
		}
		if itemErr != nil {
			item.Status = "failed"
			item.Code = infraerrors.Reason(itemErr)
			if item.Code == "" {
				item.Code = "PROXY_OPERATION_FAILED"
			}
			r.FailedCount++
			if itemVendor > 0 {
				s.VendorAudit(itemCtx, itemVendor, "proxy.delete", id, item.Code)
			}
		} else {
			r.SuccessCount++
		}
		r.Items = append(r.Items, item)
	}
	return r, nil
}

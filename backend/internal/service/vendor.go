package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"hash/fnv"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

var (
	ErrVendorNotFound = infraerrors.NotFound("VENDOR_NOT_FOUND", "Supplier resource not found")
	ErrVendorDenied   = infraerrors.Forbidden("VENDOR_ACCESS_DENIED", "Supplier access is not permitted")
	ErrVendorConflict = infraerrors.Conflict("VENDOR_POLICY_CHANGED", "Supplier policy or resource changed; reload and retry")
	ErrVendorInput    = infraerrors.BadRequest("VENDOR_INVALID_INPUT", "Invalid supplier input")
)

// Vendor is a dedicated supply identity, independent from customer entitlements.
type Vendor struct {
	CreationDefaults         AccountCreationDefaults `json:"account_defaults"`
	Tier                     int                     `json:"tier"`
	ParentVendorID           *int64                  `json:"parent_vendor_id"`
	Email                    string                  `json:"email"`
	Username                 string                  `json:"username"`
	ID                       int64                   `json:"id"`
	UserID                   int64                   `json:"user_id"`
	Name                     string                  `json:"name"`
	Status                   string                  `json:"status"`
	PolicyVersion            int64                   `json:"policy_version"`
	Platforms                []string                `json:"platforms"`
	OpenAIAuthorizationModes []string                `json:"openai_authorization_modes"`
	GroupIDs                 []int64                 `json:"group_ids"`
	Groups                   []VendorGroup           `json:"groups"`
	AccountLimit             int                     `json:"account_limit"`
	ProxyLimit               int                     `json:"proxy_limit"`
	AllowDirect              bool                    `json:"allow_direct"`
	ContactName              string                  `json:"contact_name"`
	SettlementEmail          string                  `json:"settlement_email"`
	BinanceID                string                  `json:"binance_id"`
	DefaultExchangeRate      string                  `json:"default_exchange_rate"`
	DefaultCoefficient       string                  `json:"default_coefficient"`
	GroupCoefficients        map[string]string       `json:"group_coefficients"`
	ContractVersion          int64                   `json:"contract_version"`
	AccountCount             int64                   `json:"account_count"`
	ProxyCount               int64                   `json:"proxy_count"`
	AccountTreeCount         int64                   `json:"account_tree_count"`
	ProxyTreeCount           int64                   `json:"proxy_tree_count"`
	CreatedAt                time.Time               `json:"created_at"`
	UpdatedAt                time.Time               `json:"updated_at"`
}
type VendorGroup struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	IsDefault bool   `json:"is_default"`
}

// VendorService coordinates the existing resource services. SQL transactions are
// used only for the cross-resource supply/settlement invariants.
type VendorService struct {
	db           *sql.DB
	client       *dbent.Client
	accounts     AccountRepository
	proxies      ProxyRepository
	groups       GroupRepository
	users        UserRepository
	admin        AdminService
	settings     *SettingService
	oauth        *OAuthService
	openaiOAuth  *OpenAIOAuthService
	rdb          *redis.Client
	audit        *AuditLogService
	accountUsage *AccountUsageService
	cfg          *config.Config
}

func NewVendorService(db *sql.DB, client *dbent.Client, accounts AccountRepository, proxies ProxyRepository, groups GroupRepository, users UserRepository, admin AdminService, settings *SettingService, oauth *OAuthService, openaiOAuth *OpenAIOAuthService, rdb *redis.Client, audit *AuditLogService, accountUsage *AccountUsageService, cfg *config.Config) *VendorService {
	return &VendorService{db: db, client: client, accounts: accounts, proxies: proxies, groups: groups, users: users, admin: admin, settings: settings, oauth: oauth, openaiOAuth: openaiOAuth, rdb: rdb, audit: audit, accountUsage: accountUsage, cfg: cfg}
}

const vendorColumns = "v.id,v.user_id,v.name,v.status,v.policy_version,v.platforms,v.account_limit,v.proxy_limit,v.allow_direct,v.contact_name,v.settlement_email,v.binance_id,v.default_coefficient::text,v.group_coefficients,v.contract_version,v.created_at,v.updated_at,v.tier,v.parent_vendor_id,(SELECT email FROM users WHERE id=v.user_id),(SELECT username FROM users WHERE id=v.user_id),v.openai_authorization_modes,COALESCE(v.default_exchange_rate::text,'')"

type vendorScanner interface{ Scan(...any) error }

func scanVendor(row vendorScanner) (*Vendor, error) {
	v := &Vendor{CreationDefaults: NewAccountCreationDefaults(), GroupIDs: []int64{}, Groups: []VendorGroup{}}
	var platforms, coefficients, modes []byte
	err := row.Scan(&v.ID, &v.UserID, &v.Name, &v.Status, &v.PolicyVersion, &platforms, &v.AccountLimit, &v.ProxyLimit, &v.AllowDirect, &v.ContactName, &v.SettlementEmail, &v.BinanceID, &v.DefaultCoefficient, &coefficients, &v.ContractVersion, &v.CreatedAt, &v.UpdatedAt, &v.Tier, &v.ParentVendorID, &v.Email, &v.Username, &modes, &v.DefaultExchangeRate)
	if err == sql.ErrNoRows {
		return nil, ErrVendorNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(platforms, &v.Platforms); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(modes, &v.OpenAIAuthorizationModes); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(coefficients, &v.GroupCoefficients); err != nil {
		return nil, err
	}
	return v, nil
}
func (s *VendorService) Enabled(ctx context.Context) bool {
	if s == nil || s.settings == nil {
		return false
	}
	value, err := s.settings.settingRepo.GetValue(ctx, "vendor_console_enabled")
	return err == nil && value == "true"
}
func (s *VendorService) RequireSuperAdmin(ctx context.Context) error {
	actor, ok := authz.FromContext(ctx)
	if !ok || actor.Role != authz.SuperAdmin {
		return ErrVendorDenied
	}
	if err := authz.Revalidate(ctx); err != nil {
		return err
	}
	var valid bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='super_admin' AND status='active' AND session_generation=$2 AND deleted_at IS NULL)", actor.UserID, actor.SessionGeneration).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrVendorDenied
	}
	return nil
}
func (s *VendorService) loadVendor(ctx context.Context, id int64) (*Vendor, error) {
	v, err := scanVendor(s.db.QueryRowContext(ctx, "SELECT "+vendorColumns+" FROM vendors v WHERE v.id=$1", id))
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT g.id,g.name,g.platform,vg.is_default FROM vendor_groups vg JOIN groups g ON g.id=vg.group_id WHERE vg.vendor_id=$1 AND g.deleted_at IS NULL AND g.status='active' ORDER BY vg.is_default DESC,g.id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var g VendorGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Platform, &g.IsDefault); err != nil {
			return nil, err
		}
		v.Groups = append(v.Groups, g)
		v.GroupIDs = append(v.GroupIDs, g.ID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Direct counts match the resource lists and can be summed without counting
	// a child's resources twice. Tree counts measure the shared parent capacity.
	err = s.db.QueryRowContext(ctx, `WITH owners AS (SELECT id FROM vendors WHERE id=$1 OR parent_vendor_id=$1)
 SELECT (SELECT COUNT(*) FROM accounts WHERE vendor_id=$1 AND deleted_at IS NULL AND parent_account_id IS NULL),
 (SELECT COUNT(*) FROM proxies WHERE vendor_id=$1 AND deleted_at IS NULL),
 (SELECT COUNT(*) FROM accounts WHERE vendor_id IN(SELECT id FROM owners) AND deleted_at IS NULL AND parent_account_id IS NULL),
 (SELECT COUNT(*) FROM proxies WHERE vendor_id IN(SELECT id FROM owners) AND deleted_at IS NULL)`, id).Scan(&v.AccountCount, &v.ProxyCount, &v.AccountTreeCount, &v.ProxyTreeCount)
	return v, err
}
func (s *VendorService) CurrentVendor(ctx context.Context) (*Vendor, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return nil, ErrVendorDenied
	}
	if scope, ok := authz.VendorScopeFromContext(ctx); ok {
		return s.RequireVendor(ctx, scope.VendorID)
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM vendors WHERE user_id=$1", actor.UserID).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVendorDenied
		}
		return nil, err
	}
	return s.RequireVendor(ctx, id)
}
func (s *VendorService) RequireVendor(ctx context.Context, id int64) (*Vendor, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return nil, ErrVendorDenied
	}
	if err := authz.Revalidate(ctx); err != nil {
		return nil, err
	}
	v, err := s.loadVendor(ctx, id)
	if err != nil {
		return nil, err
	}
	if actor.Role == authz.SuperAdmin {
		if err := s.RequireSuperAdmin(ctx); err != nil {
			return nil, err
		}
		return v, nil
	}
	if !authz.IsVendorRole(actor.Role) || !s.Enabled(ctx) {
		return nil, ErrVendorDenied
	}
	var ownID int64
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM vendors WHERE user_id=$1", actor.UserID).Scan(&ownID); err != nil {
		return nil, ErrVendorDenied
	}
	if err := s.validateVendorActor(ctx, s.db, actor, ownID); err != nil {
		return nil, err
	}
	scope, scoped := authz.VendorScopeFromContext(ctx)
	if scoped && (scope.VendorID != id || scope.PolicyVersion != v.PolicyVersion || scope.ActorUserID != actor.UserID || scope.Admin) {
		return nil, ErrVendorConflict
	}
	if ownID != id && (!scoped || !scope.ReadOnly || actor.Role != authz.VendorLevel1 || v.Tier != 2 || v.ParentVendorID == nil || *v.ParentVendorID != ownID) {
		return nil, ErrVendorDenied
	}
	if ownID == id && v.Status != StatusActive {
		return nil, ErrVendorDenied
	}
	if v.ParentVendorID != nil {
		parent, err := s.loadVendor(ctx, *v.ParentVendorID)
		if err != nil {
			return nil, err
		}
		applyVendorParentPolicy(v, parent)
	}
	return v, nil
}
func (s *VendorService) ValidateVendorGroups(ctx context.Context, v *Vendor, platform string, ids []int64) error {
	if v == nil || len(ids) == 0 || len(ids) > 100 {
		return ErrVendorInput
	}
	allowed := false
	for _, p := range v.Platforms {
		if p == platform {
			allowed = true
		}
	}
	if !allowed {
		return ErrVendorDenied
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		found := false
		for _, g := range v.Groups {
			if g.ID == id && (g.Platform == platform || g.Platform == PlatformComposite) {
				found = true
			}
		}
		if !found {
			return ErrVendorDenied
		}
	}
	return nil
}
func validateVendorInput(v *Vendor) error {
	if err := normalizeVendorOpenAIModes(v); err != nil {
		return err
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || len(v.Name) > 100 || (v.Status != StatusActive && v.Status != StatusDisabled) || v.AccountLimit < 0 || v.AccountLimit > 10000 || v.ProxyLimit < 0 || v.ProxyLimit > 10000 {
		return ErrVendorInput
	}
	if len(v.Platforms) == 0 || len(v.Platforms) > 2 || len(v.GroupIDs) == 0 || len(v.GroupIDs) > 100 {
		return ErrVendorInput
	}
	seen := map[string]bool{}
	for _, p := range v.Platforms {
		if (p != "anthropic" && p != "openai") || seen[p] {
			return ErrVendorInput
		}
		seen[p] = true
	}
	if len(v.ContactName) > 100 || len(v.BinanceID) > 100 || len(v.SettlementEmail) > 254 {
		return ErrVendorInput
	}
	if v.SettlementEmail != "" {
		a, err := mail.ParseAddress(v.SettlementEmail)
		if err != nil || a.Address != v.SettlementEmail {
			return ErrVendorInput
		}
	}
	if v.DefaultExchangeRate != "" {
		rate, err := vendorDecimal(v.DefaultExchangeRate, 10, false)
		if err != nil || !rate.IsPositive() {
			return ErrVendorInput
		}
		v.DefaultExchangeRate = rate.String()
	}
	validCoefficient := func(raw string) bool {
		d, err := decimal.NewFromString(raw)
		return err == nil && d.IsPositive() && d.LessThanOrEqual(decimal.NewFromInt(100000)) && d.Exponent() >= -8
	}
	if !validCoefficient(v.DefaultCoefficient) {
		return ErrVendorInput
	}
	for k, c := range v.GroupCoefficients {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil || id <= 0 || !validCoefficient(c) {
			return ErrVendorInput
		}
		found := false
		for _, g := range v.GroupIDs {
			if g == id {
				found = true
			}
		}
		if !found {
			return ErrVendorInput
		}
	}
	return nil
}

// SaveVendor updates policy; creating or attaching an existing login is not supported.
func (s *VendorService) SaveVendor(ctx context.Context, id int64, input Vendor) (*Vendor, error) {
	if id <= 0 {
		return nil, ErrVendorInput
	}
	return s.saveVendor(ctx, id, input, nil)
}
func (s *VendorService) saveVendor(ctx context.Context, id int64, input Vendor, login *VendorCreateInput) (*Vendor, error) {
	manager, err := s.vendorManager(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateVendorInput(&input); err != nil {
		return nil, err
	}
	if manager != nil {
		if input.Tier != 2 || (input.ParentVendorID != nil && *input.ParentVendorID != manager.ID) {
			return nil, ErrVendorDenied
		}
		input.ParentVendorID = &manager.ID
	}
	if (input.Tier != 1 && input.Tier != 2) || (input.Tier == 1 && input.ParentVendorID != nil) || (input.Tier == 2 && (input.ParentVendorID == nil || *input.ParentVendorID <= 0)) {
		return nil, ErrVendorInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lockID := id
	if input.ParentVendorID != nil {
		lockID = *input.ParentVendorID
	}
	if lockID > 0 {
		if err := AcquireVendorProxyLock(ctx, tx, lockID); err != nil {
			return nil, err
		}
	}
	if id > 0 {
		current, err := scanVendor(tx.QueryRowContext(ctx, "SELECT "+vendorColumns+" FROM vendors v WHERE id=$1 FOR UPDATE", id))
		if err != nil {
			return nil, err
		}
		if current.UserID != input.UserID || current.Tier != input.Tier || !sameVendorParent(current.ParentVendorID, input.ParentVendorID) || current.PolicyVersion != input.PolicyVersion {
			return nil, ErrVendorConflict
		}
	}
	actor, _ := authz.FromContext(ctx)
	if err := s.lockVendorManager(ctx, tx, actor, manager); err != nil {
		return nil, err
	}
	if input.ParentVendorID != nil {
		if err := validateVendorParentGrant(ctx, tx, &input); err != nil {
			return nil, err
		}
	}
	if login != nil {
		emailLock := fnv.New64a()
		_, _ = emailLock.Write([]byte("users:normalized-email:" + login.Email))
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", int64(emailLock.Sum64())); err != nil {
			return nil, err
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE lower(trim(email))=$1)", login.Email).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrEmailExists
		}
		role := RoleVendorLevel1
		if input.Tier == 2 {
			role = RoleVendorLevel2
		}
		err = tx.QueryRowContext(ctx, "INSERT INTO users(email,password_hash,username,role,status,concurrency,restrict_public_groups) VALUES($1,$2,$3,$4,'active',0,TRUE) RETURNING id", login.Email, login.passwordHash, input.Name, role).Scan(&input.UserID)
		if err != nil {
			if e, ok := err.(*pq.Error); ok && e.Code == "23505" {
				return nil, ErrEmailExists
			}
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,platform FROM groups WHERE id=ANY($1) AND status='active' AND deleted_at IS NULL FOR SHARE", pq.Array(input.GroupIDs))
	if err != nil {
		return nil, err
	}
	validGroups := map[int64]bool{}
	for rows.Next() {
		var gid int64
		var platform string
		if err = rows.Scan(&gid, &platform); err != nil {
			rows.Close()
			return nil, err
		}
		ok := platform == PlatformComposite
		for _, p := range input.Platforms {
			if p == platform {
				ok = true
			}
		}
		if ok {
			validGroups[gid] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, g := range input.GroupIDs {
		if !validGroups[g] {
			return nil, ErrVendorInput
		}
	}
	platforms, _ := json.Marshal(input.Platforms)
	if input.GroupCoefficients == nil {
		input.GroupCoefficients = map[string]string{}
	}
	coeff, _ := json.Marshal(input.GroupCoefficients)
	modes, _ := json.Marshal(input.OpenAIAuthorizationModes)
	args := []any{input.UserID, input.Name, input.Status, string(platforms), input.AccountLimit, input.ProxyLimit, input.AllowDirect, input.ContactName, input.SettlementEmail, input.BinanceID, input.DefaultCoefficient, string(coeff)}
	if id == 0 {
		args = append(args, input.Tier, input.ParentVendorID, string(modes), input.DefaultExchangeRate)
		err = tx.QueryRowContext(ctx, "INSERT INTO vendors(user_id,name,status,platforms,account_limit,proxy_limit,allow_direct,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,tier,parent_vendor_id,openai_authorization_modes,default_exchange_rate) VALUES($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10,$11::numeric,$12::jsonb,$13,$14,$15::jsonb,NULLIF($16,'')::numeric) RETURNING id", args...).Scan(&id)
		if err == sql.ErrNoRows {
			return nil, ErrVendorConflict
		}
	} else {
		args = append(args, id, input.PolicyVersion, string(modes), input.DefaultExchangeRate)
		result, e := tx.ExecContext(ctx, "UPDATE vendors SET default_exchange_rate=NULLIF($16,'')::numeric,openai_authorization_modes=$15::jsonb,name=$2,status=$3,platforms=$4::jsonb,account_limit=$5,proxy_limit=$6,allow_direct=$7,contact_name=$8,settlement_email=$9,binance_id=$10,default_coefficient=$11::numeric,group_coefficients=$12::jsonb,policy_version=policy_version+1,contract_version=CASE WHEN default_coefficient IS DISTINCT FROM $11::numeric OR group_coefficients IS DISTINCT FROM $12::jsonb THEN contract_version+1 ELSE contract_version END,updated_at=NOW() WHERE id=$13 AND policy_version=$14 AND user_id=$1", args...)
		if e != nil {
			return nil, e
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return nil, ErrVendorConflict
		}
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM vendor_groups WHERE vendor_id=$1", id); err != nil {
		return nil, err
	}
	for i, gid := range input.GroupIDs {
		if _, err = tx.ExecContext(ctx, "INSERT INTO vendor_groups(vendor_id,group_id,is_default) VALUES($1,$2,$3) ON CONFLICT(vendor_id,group_id) DO NOTHING", id, gid, i == 0); err != nil {
			return nil, err
		}
	}
	if input.Tier == 1 && login == nil {
		if _, err = tx.ExecContext(ctx, "UPDATE vendors SET policy_version=policy_version+1,updated_at=NOW() WHERE parent_vendor_id=$1", id); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO scheduler_outbox(event_type,account_id) SELECT 'account_changed',id FROM accounts WHERE vendor_id IN (SELECT id FROM vendors WHERE id=$1 OR parent_vendor_id=$1) AND deleted_at IS NULL", id); err != nil {
		return nil, err
	}
	if err = authz.Revalidate(ctx); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, id)
	s.VendorAudit(ctx, id, "vendor.policy.update", id, "success")
	return s.loadVendor(ctx, id)
}

// VendorListFilter narrows inventory inside the manager's existing authority.
type VendorListFilter struct {
	Tier           int
	ParentVendorID *int64
}

func (s *VendorService) ListVendors(ctx context.Context, page, size int, search, status string, filters ...VendorListFilter) ([]*Vendor, int64, error) {
	manager, err := s.vendorManager(ctx)
	if err != nil {
		return nil, 0, err
	}
	filter := VendorListFilter{}
	if len(filters) > 0 {
		filter = filters[0]
	}
	if filter.Tier < 0 || filter.Tier > 2 || (filter.ParentVendorID != nil && *filter.ParentVendorID <= 0) {
		return nil, 0, ErrVendorInput
	}
	if manager != nil && filter.ParentVendorID != nil && *filter.ParentVendorID != manager.ID {
		return nil, 0, ErrVendorDenied
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	where := " WHERE ($1='' OR v.name ILIKE '%'||$1||'%' OR u.email ILIKE '%'||$1||'%') AND ($2='' OR v.status=$2) AND ($3::smallint=0 OR v.tier=$3) AND ($4::bigint IS NULL OR v.parent_vendor_id=$4)"
	if manager != nil {
		where += " AND v.tier=2 AND v.parent_vendor_id=" + strconv.FormatInt(manager.ID, 10)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vendors v JOIN users u ON u.id=v.user_id"+where, search, status, filter.Tier, filter.ParentVendorID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT v.id FROM vendors v JOIN users u ON u.id=v.user_id"+where+" ORDER BY v.id DESC LIMIT $5 OFFSET $6", search, status, filter.Tier, filter.ParentVendorID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	out := []*Vendor{}
	for _, id := range ids {
		v, err := s.loadVendor(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		if len(v.BinanceID) > 4 {
			v.BinanceID = "****" + v.BinanceID[len(v.BinanceID)-4:]
		}
		out = append(out, v)
	}
	return out, total, nil
}
func (s *VendorService) InvalidateVendorAccounts(ctx context.Context, id int64) {
	if refresher, ok := s.accounts.(interface {
		RefreshVendorSnapshots(context.Context, int64) error
	}); ok {
		_ = refresher.RefreshVendorSnapshots(ctx, id)
	}
}
func (s *VendorService) VendorAudit(ctx context.Context, id int64, action string, objectID int64, result string) {
	if s.audit == nil {
		return
	}
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return
	}
	s.audit.Record(&AuditLog{ActorUserID: &actor.UserID, ActorRole: actor.Role, Visibility: "super_admin", Action: action, StatusCode: 200, Extra: map[string]any{"vendor_id": id, "object_id": objectID, "result": result}})
}
func ValidateVendorOrigin(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == ""
}

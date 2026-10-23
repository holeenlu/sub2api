package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
)

var (
	ErrVendorAccountNotFound      = infraerrors.NotFound("VENDOR_ACCOUNT_NOT_FOUND", "Account not found")
	ErrVendorIdentityKeyMismatch  = infraerrors.ServiceUnavailable("VENDOR_IDENTITY_KEY_MISMATCH", "Supplier identity configuration does not match this server; contact an administrator before authorizing again")
	ErrVendorOAuthRestartRequired = infraerrors.Conflict("VENDOR_OAUTH_RESTART_REQUIRED", "This authorization session was already used; generate a new authorization link and sign in again")
	ErrVendorAccountConflict      = infraerrors.Conflict("VENDOR_ACCOUNT_CONFLICT", "Account or policy changed; reload and retry")
	ErrVendorAccountArchived      = infraerrors.Conflict("VENDOR_ACCOUNT_ARCHIVED", "Archived accounts are read-only")
	ErrVendorIdentityBound        = infraerrors.Conflict("VENDOR_IDENTITY_BOUND", "This resource is already bound; contact an administrator")
	ErrVendorIdentityUnverified   = infraerrors.New(http.StatusUnprocessableEntity, "VENDOR_IDENTITY_UNVERIFIED", "Upstream identity could not be verified; authorize again")
)

func vendorAccountInputError(message string) error {
	return infraerrors.New(http.StatusUnprocessableEntity, "VENDOR_ACCOUNT_INPUT", message)
}
func vendorAccountUnavailable() error {
	return infraerrors.ServiceUnavailable("VENDOR_ACCOUNT_UNAVAILABLE", "Account operation temporarily unavailable")
}

type VendorAccountGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type VendorAccountProxy struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	Endpoint     string `json:"endpoint"`
	CountryCode  string `json:"country_code"`
	City         string `json:"city"`
	RegionSource string `json:"region_source"`
}
type VendorModelLimit struct {
	Scope    string    `json:"scope"`
	ResetsAt time.Time `json:"resets_at"`
}
type VendorQuotaWindow struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}
type VendorAccountQuota struct {
	Source        string             `json:"source"`
	UpdatedAt     *time.Time         `json:"updated_at"`
	FiveHour      *VendorQuotaWindow `json:"five_hour"`
	SevenDay      *VendorQuotaWindow `json:"seven_day"`
	SevenDayFable *VendorQuotaWindow `json:"seven_day_fable,omitempty"`
}

// VendorAccountDTO is deliberately independent of the admin account DTO. In
// particular, neither credentials nor arbitrary extra/error/notes are exposed.
type VendorAccountDTO struct {
	PlatformSchedulable    bool                         `json:"platform_schedulable"`
	AnthropicStatus        *AnthropicAccountStatus      `json:"anthropic_status,omitempty"`
	Extra                  map[string]any               `json:"extra,omitempty"`
	AuthorizationModes     []string                     `json:"authorization_modes"`
	Authorizations         []VendorAccountAuthorization `json:"authorizations"`
	BPSModels              []string                     `json:"bps_models"`
	VendorID               int64                        `json:"vendor_id"`
	VendorName             string                       `json:"vendor_name"`
	ID                     int64                        `json:"id"`
	Name                   string                       `json:"name"`
	Platform               string                       `json:"platform"`
	Type                   string                       `json:"type"`
	Method                 string                       `json:"method"`
	VendorNotes            string                       `json:"vendor_notes"`
	Email                  string                       `json:"email"`
	ProxyID                *int64                       `json:"proxy_id"`
	Proxy                  *VendorAccountProxy          `json:"proxy"`
	GroupIDs               []int64                      `json:"group_ids"`
	Groups                 []VendorAccountGroup         `json:"groups"`
	Concurrency            int                          `json:"concurrency"`
	Priority               int                          `json:"priority"`
	FailureCategory        string                       `json:"failure_category"`
	Status                 string                       `json:"status"`
	Archived               bool                         `json:"archived"`
	ArchivedAt             *time.Time                   `json:"archived_at"`
	Schedulable            bool                         `json:"schedulable"`
	VendorPausedAt         *time.Time                   `json:"vendor_paused_at"`
	Blockers               []string                     `json:"blockers"`
	RateLimitResetAt       *time.Time                   `json:"rate_limit_reset_at"`
	OverloadUntil          *time.Time                   `json:"overload_until"`
	ExpiresAt              *time.Time                   `json:"expires_at"`
	CreatedAt              time.Time                    `json:"created_at"`
	UpdatedAt              time.Time                    `json:"updated_at"`
	CurrentConcurrency     *int                         `json:"current_concurrency"`
	RecentMinute           *VendorUsageMetrics          `json:"recent_minute"`
	RPM                    int64                        `json:"rpm"`
	TPM                    int64                        `json:"tpm"`
	TPMWithCache           int64                        `json:"tpm_with_cache"`
	ModelLimits            []VendorModelLimit           `json:"model_limits"`
	TempUnschedulableUntil *time.Time                   `json:"temp_unschedulable_until"`
	TodayUsage             *VendorUsageMetrics          `json:"today_usage"`
	AllTimeUsage           *VendorUsageMetrics          `json:"all_time_usage"`
	StatisticsAt           *time.Time                   `json:"statistics_at"`
	Timezone               string                       `json:"timezone"`
	PlanType               string                       `json:"plan_type"`
	SubscriptionType       string                       `json:"subscription_type"`
	SubscriptionExpiresAt  string                       `json:"subscription_expires_at"`
	AuthorizationExpiresAt string                       `json:"authorization_expires_at"`
	CanRefresh             bool                         `json:"can_refresh"`
	Quota                  VendorAccountQuota           `json:"quota"`
}

func vendorQuotaWindow(p *UsageProgress) *VendorQuotaWindow {
	if p == nil {
		return nil
	}
	return &VendorQuotaWindow{p.Utilization, p.ResetsAt}
}
func vendorAccountDTO(a *Account, v *Vendor) *VendorAccountDTO {
	d := &VendorAccountDTO{ID: a.ID, Name: a.Name, Platform: a.Platform, Type: a.Type, Method: a.GetExtraString("vendor_auth_method"), VendorNotes: a.VendorNotes,
		ProxyID: a.ProxyID, GroupIDs: append([]int64{}, a.GroupIDs...), Groups: []VendorAccountGroup{}, Concurrency: a.Concurrency, Priority: a.Priority,
		Status: a.Status, PlatformSchedulable: a.Schedulable, Schedulable: a.IsSchedulable(), VendorPausedAt: a.VendorPausedAt,
		Archived: a.IsVendorArchived(), ArchivedAt: a.VendorArchivedAt(),
		Blockers: []string{}, RateLimitResetAt: a.RateLimitResetAt, OverloadUntil: a.OverloadUntil, ExpiresAt: a.ExpiresAt, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		Quota: VendorAccountQuota{Source: "unknown"}, AnthropicStatus: AnthropicAccountStatusFromAccount(a)}
	if a.VendorID != nil {
		d.VendorID = *a.VendorID
	}
	if v != nil {
		d.VendorName = v.Name
	}
	// Only the verified vendor projection is displayable, never decoded JWT data.
	d.Email = a.GetExtraString("vendor_verified_email")
	d.FailureCategory = vendorAccountFailureCategory(a)
	d.PlanType = a.GetCredential("plan_type")
	d.SubscriptionType = a.GetCredential("subscription_type")
	d.SubscriptionExpiresAt = a.GetCredential("subscription_expires_at")
	d.AuthorizationExpiresAt = a.GetCredential("expires_at")
	d.CanRefresh = !d.Archived && a.GetCredential("refresh_token") != ""
	if a.Proxy != nil && (v == nil || (a.Proxy.VendorID != nil && *a.Proxy.VendorID == v.ID)) {
		proxy := vendorProxyDTO(a.Proxy, nil, time.Now())
		d.Proxy = &VendorAccountProxy{ID: proxy.ID, Name: proxy.Name, Protocol: proxy.Protocol, Endpoint: proxy.Endpoint, CountryCode: proxy.CountryCode, City: proxy.City, RegionSource: proxy.RegionSource}
	}
	for _, g := range a.Groups {
		if g != nil {
			d.Groups = append(d.Groups, VendorAccountGroup{g.ID, g.Name})
		}
	}
	now := time.Now()
	d.TempUnschedulableUntil = a.TempUnschedulableUntil
	d.ModelLimits = []VendorModelLimit{}
	if limits, ok := a.Extra[modelRateLimitsKey].(map[string]any); ok {
		scopes := make([]string, 0, len(limits))
		for scope := range limits {
			scopes = append(scopes, scope)
		}
		slices.Sort(scopes)
		for _, scope := range scopes {
			// Project native public model scopes and reset times, never raw reasons.
			if len(scope) > 128 || (!strings.HasPrefix(scope, "gpt-") && !strings.HasPrefix(scope, "claude-") && scope != openAIImageGenerationRateLimitKey && scope != "AICredits") {
				continue
			}
			if at := a.modelRateLimitResetAt(scope); at != nil && at.After(now) {
				d.ModelLimits = append(d.ModelLimits, VendorModelLimit{Scope: scope, ResetsAt: *at})
			}
		}
	}
	// Only public status fields reach the shared status component, never raw extra/credentials.
	limits := map[string]any{}
	for _, limit := range d.ModelLimits {
		limits[limit.Scope] = map[string]any{"rate_limit_reset_at": limit.ResetsAt}
	}
	d.Extra = map[string]any{"model_rate_limits": limits, "allow_overages": a.Extra["allow_overages"] == true}
	if d.Archived {
		d.Blockers = append(d.Blockers, "archived")
	}
	if a.VendorPausedAt != nil {
		d.Blockers = append(d.Blockers, "vendor_paused")
	}
	if a.Status == StatusError {
		d.Blockers = append(d.Blockers, "credential_error")
	} else if a.Status != StatusActive || !a.Schedulable {
		d.Blockers = append(d.Blockers, "platform_disabled")
	}
	if a.VendorSupplyBlocked {
		d.Blockers = append(d.Blockers, "supply_policy_blocked")
	}
	if v != nil && (v.Status != StatusActive || !stringsContain(v.Platforms, a.Platform)) {
		d.Blockers = append(d.Blockers, "supply_policy_blocked")
	}

	if a.RateLimitResetAt != nil && a.RateLimitResetAt.After(now) {
		d.Blockers = append(d.Blockers, "quota_cooldown")
	}
	if a.OverloadUntil != nil && a.OverloadUntil.After(now) {
		d.Blockers = append(d.Blockers, "upstream_cooldown")
	}
	if a.TempUnschedulableUntil != nil && a.TempUnschedulableUntil.After(now) {
		d.Blockers = append(d.Blockers, "temporarily_unavailable")
	}
	if a.AutoPauseOnExpired && a.ExpiresAt != nil && !a.ExpiresAt.After(now) {
		d.Blockers = append(d.Blockers, "expired")
	}
	if a.GetCredential("access_token") == "" {
		d.Blockers = append(d.Blockers, "authorization_required")
	}
	if a.ProxyID != nil && (a.Proxy == nil || (v != nil && (a.Proxy.VendorID == nil || *a.Proxy.VendorID != v.ID)) || !a.Proxy.IsActive() || a.Proxy.IsExpired(now)) {
		d.Blockers = append(d.Blockers, "proxy_unavailable")
	}
	if a.ProxyID == nil && v != nil && !v.AllowDirect {
		d.Blockers = append(d.Blockers, "direct_not_allowed")
	}
	if len(d.Blockers) > 0 {
		d.Schedulable = false
	}
	if a.Platform == PlatformOpenAI {
		d.Quota.FiveHour = vendorQuotaWindow(buildCodexUsageProgressFromExtra(a.Extra, "5h", now))
		d.Quota.SevenDay = vendorQuotaWindow(buildCodexUsageProgressFromExtra(a.Extra, "7d", now))
	} else {
		usage := &UsageInfo{SevenDay: buildPassiveUsageWindow(a.Extra, "passive_usage_7d_utilization", "passive_usage_7d_reset")}
		if utilization, ok := a.Extra["session_window_utilization"].(float64); ok {
			usage.FiveHour = &UsageProgress{Utilization: utilization * 100, ResetsAt: a.SessionWindowEnd}
		}
		usage.SevenDayFable = buildPassiveUsageWindow(a.Extra, "passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset")
		enrichAnthropicUsage(a, usage)
		d.Quota.FiveHour = vendorQuotaWindow(usage.FiveHour)
		d.Quota.SevenDay = vendorQuotaWindow(usage.SevenDay)
		d.Quota.SevenDayFable = vendorQuotaWindow(usage.SevenDayFable)
	}
	if d.Quota.FiveHour != nil || d.Quota.SevenDay != nil || d.Quota.SevenDayFable != nil {
		d.Quota.Source = "passive"
	}
	for _, key := range []string{"codex_usage_updated_at", "passive_usage_sampled_at"} {
		if raw := a.GetExtraString(key); raw != "" {
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				d.Quota.UpdatedAt = &t
				break
			}
		}
	}
	return d
}

type VendorAccountInput struct {
	AuthorizationMode   string  `json:"authorization_mode"`
	AuthorizationAction string  `json:"authorization_action"`
	Name                string  `json:"name"`
	VendorNotes         string  `json:"vendor_notes"`
	Platform            string  `json:"platform"`
	Method              string  `json:"method"`
	GroupIDs            []int64 `json:"group_ids"`
	ProxyID             *int64  `json:"proxy_id"`
	Direct              bool    `json:"direct"`
	Token               string  `json:"token,omitempty"`
	TargetAccountID     int64   `json:"target_account_id,omitempty"`
}
type VendorAccountUpdate struct {
	NativeEnabled *bool     `json:"native_enabled"`
	BPSEnabled    *bool     `json:"bps_enabled"`
	BPSAllModels  *bool     `json:"bps_all_models"`
	BPSModels     *[]string `json:"bps_models"`
	Name          *string   `json:"name"`
	VendorNotes   *string   `json:"vendor_notes"`
	GroupIDs      *[]int64  `json:"group_ids"`
	ProxyID       *int64    `json:"proxy_id"`
	Direct        bool      `json:"direct"`
}

func normalizeVendorAccountInput(in *VendorAccountInput) error {
	if in.ProxyID != nil && (*in.ProxyID <= 0 || in.Direct) {
		return vendorAccountInputError("Choose one valid proxy or explicit direct connection")
	}
	if in.ProxyID == nil && !in.Direct {
		return vendorAccountInputError("An explicit egress selection is required")
	}
	return normalizeVendorAccountFields(in)
}

// Import previews can use a validated bundled proxy before it has a local ID.
// They share all remaining account validation with ordinary authorization.
func normalizeVendorAccountFields(in *VendorAccountInput) error {
	if err := normalizeVendorAuthorizationInput(in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	in.VendorNotes = strings.TrimSpace(in.VendorNotes)
	if len(in.Name) > 100 || len(in.VendorNotes) > 4000 {
		return vendorAccountInputError("Name max 100 bytes; notes max 4000 bytes")
	}
	if in.Platform != PlatformAnthropic && in.Platform != PlatformOpenAI {
		return vendorAccountInputError("Unsupported platform")
	}
	in.GroupIDs = vendorUniqueIDs(in.GroupIDs)
	if len(in.GroupIDs) == 0 || len(in.GroupIDs) > 100 || in.GroupIDs[0] <= 0 {
		return vendorAccountInputError("At least one authorized group is required")
	}
	if in.TargetAccountID < 0 {
		return vendorAccountInputError("Invalid reauthorization target")
	}
	if len(in.Token) > 32768 {
		return vendorAccountInputError("Authorization material is too long")
	}
	return nil
}
func vendorUniqueIDs(ids []int64) []int64 {
	out := append([]int64{}, ids...)
	slices.Sort(out)
	return slices.Compact(out)
}
func (s *VendorService) vendorOwnedAccount(ctx context.Context, v *Vendor, id int64) (*Account, error) {
	if id <= 0 {
		return nil, ErrVendorAccountNotFound
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil || a == nil || a.VendorID == nil || *a.VendorID != v.ID || a.ParentAccountID != nil {
		return nil, ErrVendorAccountNotFound
	}
	return a, nil
}

func (s *VendorService) vendorEditableAccount(ctx context.Context, v *Vendor, id int64) (*Account, error) {
	if err := requireVendorWriteScope(ctx); err != nil {
		return nil, err
	}
	a, err := s.vendorOwnedAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if a.IsVendorArchived() {
		return nil, ErrVendorAccountArchived
	}
	return a, nil
}
func (s *VendorService) GetVendorAccount(ctx context.Context, id int64) (*VendorAccountDTO, error) {
	v, err := s.CurrentVendor(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.vendorOwnedAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	dto := vendorAccountDTO(a, v)
	if err = s.populateVendorAuthorizations(ctx, []*Account{a}, v, []*VendorAccountDTO{dto}); err != nil {
		return nil, err
	}
	s.populateVendorUsageSnapshots(ctx, []*VendorAccountDTO{dto})
	if err = s.vendorAccountStatistics(ctx, v, []*VendorAccountDTO{dto}, nil, time.Now()); err != nil {
		return nil, err
	}
	return dto, nil
}

type VendorAccountListInput struct {
	Page, PageSize                                                       int
	Search, Platform, Status, Method, SortBy, SortOrder, FailureCategory string
	AuthorizationMode                                                    string
	GroupID                                                              int64
	Cooldown, Schedulability                                             string
	CreatedFrom, CreatedTo                                               *time.Time
}
type VendorAccountOverview struct {
	Abnormal              int64                               `json:"abnormal"`
	HighQuota             int64                               `json:"high_quota"`
	QuotaNearLimitPercent int                                 `json:"quota_near_limit_percent"`
	QuotaDistribution     map[string]*VendorQuotaDistribution `json:"quota_distribution"`
	Failures              map[string]int64                    `json:"failures"`
	Total                 int64                               `json:"total"`
	Available             int64                               `json:"available"`
	Paused                int64                               `json:"paused"`
	Cooling               int64                               `json:"cooling"`
	Disabled              int64                               `json:"disabled"`
	CollectedAt           time.Time                           `json:"collected_at"`
	RecentMinute          *VendorUsageMetrics                 `json:"recent_minute"`
	TodayUsage            *VendorUsageMetrics                 `json:"today_usage"`
	AllTimeUsage          *VendorUsageMetrics                 `json:"all_time_usage"`
	RPM                   int64                               `json:"rpm"`
	TPM                   int64                               `json:"tpm"`
	TPMWithCache          int64                               `json:"tpm_with_cache"`
	CurrentConcurrency    *int64                              `json:"current_concurrency"`
	MaximumConcurrency    int64                               `json:"maximum_concurrency"`
	ConcurrencyAt         *time.Time                          `json:"concurrency_at"`
	Timezone              string                              `json:"timezone"`
	QuotaFullPercent      int                                 `json:"quota_full_percent"`
}
type VendorAccountList struct {
	Items    []*VendorAccountDTO   `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Overview VendorAccountOverview `json:"overview"`
}

// Aggregate inventory is either all suppliers for a live superadmin, or only
// direct children of the live authenticated tier-1 supplier.
func (s *VendorService) vendorAccountListScope(ctx context.Context) (*Vendor, error) {
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.Children {
		actor, present := authz.FromContext(ctx)
		if !present || actor.Role != authz.VendorLevel1 || scope.Admin || !scope.ReadOnly || scope.ActorUserID != actor.UserID {
			return nil, ErrVendorDenied
		}
		parent, err := s.CurrentVendor(ctx)
		if err != nil {
			return nil, err
		}
		if parent.Tier != 1 || parent.UserID != actor.UserID {
			return nil, ErrVendorDenied
		}
		return &Vendor{}, nil
	}
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.VendorID == 0 {
		actor, present := authz.FromContext(ctx)
		if !scope.Admin || !present || scope.ActorUserID != actor.UserID {
			return nil, ErrVendorDenied
		}
		if err := s.RequireSuperAdmin(ctx); err != nil {
			return nil, err
		}
		return &Vendor{}, nil
	}
	return s.CurrentVendor(ctx)
}

// Call only after validating the inventory scope. $1 is its selected owner,
// with zero reserved for aggregate reads. The parent is never client supplied.
func vendorInventoryOwnerSQL(ctx context.Context, column string) string {
	where := "(" + column + "=$1 OR ($1::bigint=0 AND " + column + " IS NOT NULL))"
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.Children {
		where += fmt.Sprintf(" AND %s IN (SELECT id FROM vendors WHERE tier=2 AND parent_vendor_id=%d)", column, scope.VendorID)
	}
	return where
}
func (s *VendorService) ListVendorAccounts(ctx context.Context, in VendorAccountListInput) (*VendorAccountList, error) {
	v, err := s.vendorAccountListScope(ctx)
	if err != nil {
		return nil, err
	}
	if s.db == nil {
		return nil, vendorAccountUnavailable()
	}
	if in.Page < 1 {
		in.Page = 1
	}
	if in.PageSize < 1 {
		in.PageSize = 20
	}
	if in.PageSize > 100 {
		in.PageSize = 100
	}
	if in.Page > 100000 || len(in.Search) > 200 {
		return nil, vendorAccountInputError("Invalid pagination or search")
	}
	where := vendorInventoryOwnerSQL(ctx, "a.vendor_id") + " AND a.deleted_at IS NULL AND a.parent_account_id IS NULL"
	args := []any{v.ID}
	add := func(expr string, val any) { args = append(args, val); where += " AND " + fmt.Sprintf(expr, len(args)) }
	if in.Platform != "" {
		add("a.platform=$%d", in.Platform)
	}
	if in.Search != "" {
		add("(a.name ILIKE $%[1]d OR a.id::text ILIKE $%[1]d OR a.extra->>'vendor_verified_email' ILIKE $%[1]d)", "%"+in.Search+"%")
	}
	if in.GroupID > 0 {
		add("EXISTS(SELECT 1 FROM account_groups ag WHERE ag.account_id=a.id AND ag.group_id=$%d)", in.GroupID)
	}
	if in.AuthorizationMode != "" {
		if !validVendorOpenAIMode(in.AuthorizationMode) {
			return nil, vendorAccountInputError("Invalid authorization mode")
		}
		where += " AND a.platform='openai'"
		if in.AuthorizationMode == VendorOpenAIModeBPS {
			where += " AND ((a.credentials->>'client_id'='app_fnr0pYvVwwFDocDumLG3H2Bp' AND COALESCE(a.credentials->>'access_token','')<>'') OR EXISTS(SELECT 1 FROM openai_excel_grants eg WHERE eg.account_id=a.id AND eg.ciphertext<>''))"
		} else {
			where += " AND COALESCE(a.credentials->>'client_id','')<>'app_fnr0pYvVwwFDocDumLG3H2Bp' AND COALESCE(a.credentials->>'access_token','')<>''"
		}
	}
	if in.Method != "" {
		add("a.extra->>'vendor_auth_method'=$%d", in.Method)
	}
	switch in.Status {
	case "":
	case "archived":
		where += " AND COALESCE(a.extra->>'vendor_archived_at','')<>''"
	case "paused":
		where += " AND a.vendor_paused_at IS NOT NULL"
	case "cooling":
		where += " AND (a.rate_limit_reset_at>NOW() OR a.overload_until>NOW())"
	case "active":
		where += " AND (" + vendorAccountAvailableSQL + ")"
	default:
		add("a.status=$%d", in.Status)
	}
	if in.FailureCategory != "" {
		valid := in.FailureCategory == "unknown"
		for _, r := range vendorAccountFailureRules {
			if in.FailureCategory == r.category {
				valid = true
			}
		}
		if !valid {
			return nil, vendorAccountInputError("Invalid failure category")
		}
		args = append(args, in.FailureCategory)
		where += " AND (" + vendorAccountFailureSQL() + ")=" + fmt.Sprintf("$%d", len(args))
	}
	if in.CreatedFrom != nil {
		add("a.created_at >= $%d", *in.CreatedFrom)
	}
	if in.CreatedTo != nil {
		add("a.created_at < $%d", *in.CreatedTo)
	}
	switch in.Cooldown {
	case "", "all":
	case "account":
		where += " AND (a.rate_limit_reset_at>NOW() OR a.overload_until>NOW())"
	case "available":
		where += " AND NOT (a.rate_limit_reset_at>NOW() OR a.overload_until>NOW())"
	case "five_hour":
		where += " AND a.session_window_end>NOW()"
	case "seven_day", "seven_day_fable":
		key := "passive_usage_7d_reset"
		if in.Cooldown == "seven_day_fable" {
			key = "passive_usage_7d_oi_reset"
		}
		where += fmt.Sprintf(" AND CASE WHEN COALESCE(a.extra->>'%s','') ~ '^[0-9]+(\\.[0-9]+)?$' THEN (a.extra->>'%s')::double precision > EXTRACT(EPOCH FROM NOW()) ELSE FALSE END", key, key)
	default:
		return nil, vendorAccountInputError("Invalid cooldown filter")
	}
	switch in.Schedulability {
	case "", "all":
	case "schedulable":
		where += " AND (" + vendorAccountAvailableSQL + ")"
	case "unschedulable":
		where += " AND NOT (" + vendorAccountAvailableSQL + ")"
	default:
		return nil, vendorAccountInputError("Invalid schedulability filter")
	}
	out := &VendorAccountList{Items: []*VendorAccountDTO{}, Page: in.Page, PageSize: in.PageSize}
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts a WHERE "+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	direction := "DESC"
	if in.SortOrder == "asc" {
		direction = "ASC"
	} else if in.SortOrder != "" && in.SortOrder != "desc" {
		return nil, vendorAccountInputError("Invalid sort direction")
	}
	columns := map[string]string{"": "a.id", "id": "a.id", "name": "a.name", "created_at": "a.created_at"}
	column, ok := columns[in.SortBy]
	costSort := in.SortBy == "today_cost" || in.SortBy == "total_cost" || in.SortBy == "cost" || in.SortBy == "fable_cost" || in.SortBy == "opus_cost"
	if !ok && !costSort {
		return nil, vendorAccountInputError("Unsupported sort field")
	}
	order := column + " " + direction + ",a.id " + direction
	if costSort {
		order = vendorAccountCostOrder(&args, in.SortBy, direction, time.Now())
	}
	args = append(args, in.PageSize, (in.Page-1)*in.PageSize)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT a.id,a.vendor_id FROM accounts a WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d", where, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	owners := map[int64][]int64{}
	for rows.Next() {
		var id, owner int64
		if err = rows.Scan(&id, &owner); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		owners[owner] = append(owners[owner], id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	byID := map[int64]*VendorAccountDTO{}
	for owner, ownerIDs := range owners {
		ownerVendor := v
		readCtx := ctx
		if v.ID == 0 {
			scope, _ := authz.VendorScopeFromContext(ctx)
			if scope.Children {
				ownerVendor, err = s.ReadVendorScope(ctx, owner)
			} else {
				ownerVendor, err = s.loadVendor(ctx, owner)
			}
			if err != nil {
				return nil, err
			}
			actor, _ := authz.FromContext(ctx)
			readCtx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: owner, PolicyVersion: ownerVendor.PolicyVersion, ActorUserID: actor.UserID, Admin: scope.Admin, ReadOnly: scope.Children})
		}
		accounts, e := s.accounts.GetByIDs(readCtx, ownerIDs)
		if e != nil {
			return nil, e
		}
		ownerDTOs := []*VendorAccountDTO{}
		for _, a := range accounts {
			if a.VendorID != nil && *a.VendorID == owner && a.ParentAccountID == nil {
				byID[a.ID] = vendorAccountDTO(a, ownerVendor)
				ownerDTOs = append(ownerDTOs, byID[a.ID])
			}
		}
		if err = s.populateVendorAuthorizations(readCtx, accounts, ownerVendor, ownerDTOs); err != nil {
			return nil, err
		}
		s.populateVendorUsageSnapshots(readCtx, ownerDTOs)
	}
	for _, id := range ids {
		if dto := byID[id]; dto != nil {
			out.Items = append(out.Items, dto)
		}
	}
	if err = s.vendorAccountPoolOverview(ctx, v, &out.Overview); err != nil {
		return nil, err
	}
	if err = s.vendorAccountStatistics(ctx, v, out.Items, &out.Overview, out.Overview.CollectedAt); err != nil {
		return nil, err
	}
	if _, err = s.vendorAccountListScope(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// Every mutation takes the vendor row lock before checking limits/ownership.
// Main's policy and proxy mutation services use the same lock order. SQL is
// necessary here because identity claims, limits and the native account/outbox
// must commit together, including when the repository's Create opens its own tx.
func (s *VendorService) vendorAccountTx(ctx context.Context, v *Vendor, fn func(*sql.Tx) error) error {
	if err := requireVendorWriteScope(ctx); err != nil {
		return err
	}
	if s.db == nil {
		return vendorAccountUnavailable()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := AcquireVendorProxyLock(ctx, tx, v.ID); err != nil {
		return err
	}
	if err := s.lockVendorResourceActor(ctx, tx, v.ID); err != nil {
		return err
	}
	var version int64
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT policy_version,status FROM vendors WHERE id=$1 FOR UPDATE", v.ID).Scan(&version, &status); err != nil {
		return ErrVendorAccountConflict
	}
	if version != v.PolicyVersion || status != StatusActive {
		return ErrVendorAccountConflict
	}
	if err = authz.Revalidate(ctx); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	if err = authz.Revalidate(ctx); err != nil {
		return err
	}
	return tx.Commit()
}
func vendorAccountOutbox(ctx context.Context, tx *sql.Tx, id int64, groups []int64) error {
	payload, err := json.Marshal(map[string]any{"group_ids": groups})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO scheduler_outbox(event_type,account_id,payload) VALUES($1,$2,$3::jsonb)", SchedulerOutboxEventAccountChanged, id, string(payload))
	return err
}
func (s *VendorService) vendorLockAccount(ctx context.Context, tx *sql.Tx, v *Vendor, id int64) error {
	var archived bool
	err := tx.QueryRowContext(ctx, "SELECT COALESCE(extra->>'vendor_archived_at','')<>'' FROM accounts WHERE id=$1 AND vendor_id=$2 AND deleted_at IS NULL AND parent_account_id IS NULL FOR UPDATE", id, v.ID).Scan(&archived)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrVendorAccountNotFound
	}
	if err == nil && archived {
		return ErrVendorAccountArchived
	}
	return err
}
func (s *VendorService) vendorBindGroups(ctx context.Context, tx *sql.Tx, v *Vendor, id int64, platform string, ids []int64) error {
	if err := s.ValidateVendorGroups(ctx, v, platform, ids); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM groups WHERE id=ANY($1) AND platform=$2 AND status='active' AND deleted_at IS NULL FOR SHARE", pq.Array(ids), platform)
	if err != nil {
		return err
	}
	n := 0
	for rows.Next() {
		n++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if n != len(ids) {
		return ErrVendorAccountConflict
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM account_groups WHERE account_id=$1", id); err != nil {
		return err
	}
	for i, g := range ids {
		if _, err = tx.ExecContext(ctx, "INSERT INTO account_groups(account_id,group_id,priority) VALUES($1,$2,$3)", id, g, i+1); err != nil {
			return err
		}
	}
	return nil
}
func (s *VendorService) UpdateVendorAccount(ctx context.Context, id int64, in VendorAccountUpdate) (*VendorAccountDTO, error) {
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.vendorEditableAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if *in.Name == "" || len(*in.Name) > 100 {
			return nil, vendorAccountInputError("Invalid name")
		}
	}
	if in.VendorNotes != nil && len(*in.VendorNotes) > 4000 {
		return nil, vendorAccountInputError("Notes too long")
	}
	if in.GroupIDs != nil {
		ids := vendorUniqueIDs(*in.GroupIDs)
		in.GroupIDs = &ids
		if len(ids) == 0 {
			return nil, vendorAccountInputError("At least one group is required")
		}
	}
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		if in.ProxyID != nil || in.Direct {
			if _, _, e := s.vendorAccountEgress(ctx, tx, v, in.ProxyID, in.Direct); e != nil {
				return e
			}
		}
		if e := s.vendorLockAccount(ctx, tx, v, id); e != nil {
			return e
		}
		if in.ProxyID != nil || in.Direct {
			if _, e := tx.ExecContext(ctx, "UPDATE accounts SET proxy_id=$1,proxy_fallback_origin_id=NULL WHERE id=$2", in.ProxyID, id); e != nil {
				return e
			}
		}
		if _, e := tx.ExecContext(ctx, "UPDATE accounts SET name=COALESCE($1,name),vendor_notes=COALESCE($2,vendor_notes),updated_at=NOW() WHERE id=$3", in.Name, in.VendorNotes, id); e != nil {
			return e
		}
		groups := a.GroupIDs
		if in.GroupIDs != nil {
			groups = *in.GroupIDs
			if e := s.vendorBindGroups(ctx, tx, v, id, a.Platform, groups); e != nil {
				return e
			}
		}
		if err := s.updateVendorAuthorizationSettings(ctx, tx, v, a, in); err != nil {
			return err
		}
		return vendorAccountOutbox(ctx, tx, id, append(append([]int64{}, a.GroupIDs...), groups...))
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, v.ID)
	s.VendorAudit(ctx, v.ID, "account.update", id, "success")
	return s.GetVendorAccount(ctx, id)
}
func (s *VendorService) SetVendorAccountPaused(ctx context.Context, id int64, paused bool) (*VendorAccountDTO, error) {
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.vendorEditableAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		if e := s.vendorLockAccount(ctx, tx, v, id); e != nil {
			return e
		}
		var at *time.Time
		if paused {
			now := time.Now()
			at = &now
		}
		if _, e := tx.ExecContext(ctx, "UPDATE accounts SET vendor_paused_at=$1,updated_at=NOW() WHERE id=$2", at, id); e != nil {
			return e
		}
		return vendorAccountOutbox(ctx, tx, id, a.GroupIDs)
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, v.ID)
	action := "account.resume"
	if paused {
		action = "account.pause"
	}
	s.VendorAudit(ctx, v.ID, action, id, "success")
	return s.GetVendorAccount(ctx, id)
}
func (s *VendorService) DeleteVendorAccount(ctx context.Context, id int64) error {
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return err
	}
	a, err := s.vendorOwnedAccount(ctx, v, id)
	if err != nil {
		return err
	}
	changed := false
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		if e := s.vendorLockAccount(ctx, tx, v, id); e != nil {
			if errors.Is(e, ErrVendorAccountArchived) {
				return nil
			}
			return e
		}
		// Preserve provider health, secrets, bindings and identity claims. The
		// marker is authoritative; native pause flags also stop older consumers.
		at := time.Now().UTC().Format(time.RFC3339Nano)
		if _, e := tx.ExecContext(ctx, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||jsonb_build_object('vendor_archived_at',$3::text),vendor_paused_at=COALESCE(vendor_paused_at,$3::timestamptz),schedulable=false,updated_at=NOW() WHERE vendor_id=$1 AND (id=$2 OR parent_account_id=$2) AND deleted_at IS NULL AND COALESCE(extra->>'vendor_archived_at','')=''`, v.ID, id, at); e != nil {
			return e
		}
		// Disable future work without cascading away plans or historical results.
		if _, e := tx.ExecContext(ctx, "UPDATE scheduled_test_plans SET enabled=false,next_run_at=NULL,updated_at=NOW() WHERE account_id IN(SELECT id FROM accounts WHERE vendor_id=$1 AND (id=$2 OR parent_account_id=$2))", v.ID, id); e != nil {
			return e
		}
		if e := vendorAccountOutbox(ctx, tx, id, a.GroupIDs); e != nil {
			return e
		}
		// Evict dependent shadow snapshots on every application node as well.
		_, e := tx.ExecContext(ctx, "INSERT INTO scheduler_outbox(event_type) VALUES($1)", SchedulerOutboxEventFullRebuild)
		changed = e == nil
		return e
	})
	if err != nil {
		return err
	}
	if changed {
		s.InvalidateVendorAccounts(ctx, v.ID)
		if s.rdb != nil {
			_ = s.rdb.Del(ctx, "oauth:token:"+ClaudeTokenCacheKey(a), "oauth:token:"+OpenAITokenCacheKey(a)).Err()
		}
		s.VendorAudit(ctx, v.ID, "account.archive", id, "success")
	}
	return nil
}

// Reuse the operations account-list service, including its cache and bounded
// concurrency. The supplier response projects only quota/status, never charges.
func (s *VendorService) populateVendorUsageSnapshots(ctx context.Context, rows []*VendorAccountDTO) {
	if s.accountUsage == nil {
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.Archived {
			continue
		}
		// Do not initiate native network probes for BPS-only or policy-blocked accounts.
		if row.Platform == PlatformOpenAI && (!stringsContain(row.AuthorizationModes, "native") || len(row.Blockers) > 0 && !onlyVendorQuotaBlockers(row.Blockers)) {
			continue
		}
		ids = append(ids, row.ID)
	}
	usage, _, err := s.accountUsage.GetUsageBatch(ctx, ids, false)
	if err != nil {
		return
	}
	for _, row := range rows {
		if u := usage[row.ID]; u != nil {
			row.Quota = VendorAccountQuota{FiveHour: vendorQuotaWindow(u.FiveHour), SevenDay: vendorQuotaWindow(u.SevenDay), SevenDayFable: vendorQuotaWindow(u.SevenDayFable), Source: u.Source, UpdatedAt: u.UpdatedAt}
			if row.Platform == PlatformAnthropic {
				row.AnthropicStatus = u.AnthropicStatus
			}
		}
	}
}
func onlyVendorQuotaBlockers(blockers []string) bool {
	for _, b := range blockers {
		if b != "quota_cooldown" {
			return false
		}
	}
	return true
}

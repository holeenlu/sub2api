package service

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"strings"
	"time"
)

// Display classification only; it never changes the native security state.
var vendorAccountFailureRules = []struct {
	category string
	terms    []string
}{
	{"authentication", []string{"401", "unauthenticated", "invalid_grant", "invalid token"}},
	{"kyc", []string{"kyc"}},
	{"terms", []string{"terms", "term accepted", "条款"}},
	{"tier", []string{"tier", "plan mismatch", "subscription"}},
	{"duplicate", []string{"duplicate", "already exists"}},
	{"email_mismatch", []string{"email mismatch", "邮箱不一致"}},
	{"rejected", []string{"400", "rejected", "account rejected"}},
	{"proxy", []string{"proxy", "dial tcp"}},
	{"connection", []string{"connection refused", "connection reset", "timeout"}},
	{"permission", []string{"403", "forbidden", "violation", "validation"}},
	{"upstream", []string{"500", "502", "503", "504", "overloaded"}},
}

func vendorAccountFailureCategory(a *Account) string {
	if a.Status != StatusError && (a.TempUnschedulableUntil == nil || !a.TempUnschedulableUntil.After(time.Now())) {
		return ""
	}
	text := strings.ToLower(a.ErrorMessage + " " + a.TempUnschedulableReason)
	for _, r := range vendorAccountFailureRules {
		for _, term := range r.terms {
			if strings.Contains(text, term) {
				return r.category
			}
		}
	}
	return "unknown"
}
func vendorAccountFailureSQL() string {
	raw := "LOWER(COALESCE(a.error_message,'')||' '||COALESCE(a.temp_unschedulable_reason,''))"
	expr := "CASE WHEN a.status<>'error' AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until<=NOW()) THEN '' "
	for _, r := range vendorAccountFailureRules {
		parts := make([]string, 0, len(r.terms))
		for _, term := range r.terms {
			parts = append(parts, raw+" LIKE '%"+term+"%'")
		}
		expr += "WHEN " + strings.Join(parts, " OR ") + " THEN '" + r.category + "' "
	}
	return expr + "ELSE 'unknown' END"
}

// Eligibility is distinct from load saturation reported by native concurrency.
var vendorAccountAvailableSQL = `a.status='active' AND a.schedulable AND a.vendor_paused_at IS NULL
 AND COALESCE(a.extra->>'vendor_archived_at','')=''
 AND (NOT a.auto_pause_on_expired OR a.expires_at IS NULL OR a.expires_at>NOW())
 AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at<=NOW())
 AND (a.overload_until IS NULL OR a.overload_until<=NOW())
 AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until<=NOW())
 AND (a.platform='openai' OR COALESCE(a.credentials->>'access_token','')<>'')
 AND EXISTS(SELECT 1 FROM vendors v JOIN users u ON u.id=v.user_id WHERE v.id=a.vendor_id AND ` + authz.ActiveVendorSQL("v", "u") + ` AND v.platforms ? a.platform
 AND (a.platform<>'openai' OR (` + authz.OpenAIAuthorizationModeSQL("a", "v", "native") + ` OR ` + authz.OpenAIAuthorizationModeSQL("a", "v", "bps") + `))
 AND (v.tier=1 OR EXISTS(SELECT 1 FROM vendors parent_v WHERE parent_v.id=v.parent_vendor_id AND parent_v.platforms ? a.platform AND (a.proxy_id IS NOT NULL OR parent_v.allow_direct)))
 AND ((a.proxy_id IS NULL AND v.allow_direct) OR EXISTS(SELECT 1 FROM proxies p WHERE p.id=a.proxy_id AND p.vendor_id=a.vendor_id AND p.deleted_at IS NULL AND p.status='active' AND (p.expires_at IS NULL OR p.expires_at>NOW()))))
 AND EXISTS(SELECT 1 FROM account_groups ag JOIN vendor_groups vg ON vg.group_id=ag.group_id AND vg.vendor_id=a.vendor_id JOIN groups g ON g.id=ag.group_id WHERE ag.account_id=a.id AND (g.platform=a.platform OR g.platform='composite') AND g.status='active' AND g.deleted_at IS NULL AND EXISTS(SELECT 1 FROM vendors child WHERE child.id=a.vendor_id AND (child.tier=1 OR EXISTS(SELECT 1 FROM vendor_groups parent_g WHERE parent_g.vendor_id=child.parent_vendor_id AND parent_g.group_id=g.id))))`

type VendorQuotaDistribution struct {
	Normal    int64 `json:"normal"`
	NearLimit int64 `json:"near_limit"`
	Full      int64 `json:"full"`
	Unknown   int64 `json:"unknown"`
}

const vendorQuotaNearLimitPercent = 90
const vendorQuotaFullPercent = 99

func vendorCountQuota(d *VendorQuotaDistribution, w *VendorQuotaWindow) bool {
	if w == nil {
		d.Unknown++
		return false
	}
	if w.Utilization >= vendorQuotaFullPercent {
		d.Full++
		return true
	}
	if w.Utilization >= vendorQuotaNearLimitPercent {
		d.NearLimit++
		return true
	}
	d.Normal++
	return false
}
func (s *VendorService) vendorAccountPoolOverview(ctx context.Context, v *Vendor, out *VendorAccountOverview) error {
	query := "SELECT a.platform,a.type,a.credentials,a.extra,a.session_window_end,a.concurrency,a.vendor_paused_at IS NOT NULL,COALESCE(a.rate_limit_reset_at>NOW() OR a.overload_until>NOW(),false),a.status,a.schedulable,(" + vendorAccountAvailableSQL + "),(" + vendorAccountFailureSQL() + ") FROM accounts a WHERE " + vendorInventoryOwnerSQL(ctx, "a.vendor_id") + " AND a.deleted_at IS NULL AND a.parent_account_id IS NULL"
	rows, err := s.db.QueryContext(ctx, query, v.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out.QuotaDistribution = map[string]*VendorQuotaDistribution{"five_hour": {}, "seven_day": {}, "seven_day_fable": {}}
	out.Failures = map[string]int64{}
	out.QuotaNearLimitPercent = vendorQuotaNearLimitPercent
	out.QuotaFullPercent = vendorQuotaFullPercent
	for rows.Next() {
		var a Account
		var raw, credentials []byte
		var paused, cooling, available bool
		var category string
		if err = rows.Scan(&a.Platform, &a.Type, &credentials, &raw, &a.SessionWindowEnd, &a.Concurrency, &paused, &cooling, &a.Status, &a.Schedulable, &available, &category); err != nil {
			return err
		}
		if err = json.Unmarshal(credentials, &a.Credentials); err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &a.Extra); err != nil {
			return err
		}
		out.Total++
		out.MaximumConcurrency += int64(a.Concurrency)
		if available {
			out.Available++
		}
		if paused {
			out.Paused++
		}
		if cooling {
			out.Cooling++
		}
		// SetError also disables scheduling. That is an abnormal account, not a
		// manual disable; keep the overview consistent with the status badge.
		if a.Status == StatusDisabled || (a.Status != StatusError && !a.Schedulable) {
			out.Disabled++
		}
		if a.Status == StatusError || (!available && !paused && !cooling && a.Status != StatusDisabled && a.Schedulable) {
			out.Abnormal++
		}
		if category != "" {
			out.Failures[category]++
		}
		q := vendorAccountDTO(&a, nil).Quota
		high := vendorCountQuota(out.QuotaDistribution["five_hour"], q.FiveHour)
		if vendorCountQuota(out.QuotaDistribution["seven_day"], q.SevenDay) {
			high = true
		}
		if a.Platform == PlatformAnthropic && vendorCountQuota(out.QuotaDistribution["seven_day_fable"], q.SevenDayFable) {
			high = true
		}
		if high {
			out.HighQuota++
		}
	}
	out.CollectedAt = time.Now()
	return rows.Err()
}

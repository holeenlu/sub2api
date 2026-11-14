package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/lib/pq"
	"slices"
	"time"
)

// Native supplier cost/token normalization, separate from upstream entitlement.
func (s *VendorService) vendorAccountStatistics(ctx context.Context, v *Vendor, items []*VendorAccountDTO, overview *VendorAccountOverview, snapshot time.Time) error {
	if s.db == nil {
		return vendorAccountUnavailable()
	}
	ids := make([]int64, 0, len(items))
	byID := map[int64]*VendorAccountDTO{}
	for _, item := range items {
		ids = append(ids, item.ID)
		byID[item.ID] = item
	}
	f := VendorUsageFilter{VendorID: v.ID, SnapshotAt: snapshot}
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.Children {
		f.parentVendorID = &scope.VendorID
	}
	if err := vendorUsageWatermark(ctx, s.db, &f); err != nil {
		return err
	}
	local := snapshot.In(timezone.Location())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	for _, window := range []string{"all", "today", "minute"} {
		start := time.Unix(0, 0)
		if window == "today" {
			start = today
		} else if window == "minute" {
			start = snapshot.Add(-time.Minute)
		}
		f.start, f.end = start, snapshot.Add(time.Nanosecond)
		where, args := vendorUsageWhere(f)
		args = append(args, pq.Array(ids))
		query := "SELECT ul.account_id," + vendorUsageAggregateSQL() + " FROM usage_logs ul WHERE " + where + fmt.Sprintf(" AND ul.account_id=ANY($%d) GROUP BY ul.account_id", len(args))
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		found := map[int64]VendorUsageMetrics{}
		for rows.Next() {
			var id int64
			var scan vendorMetricsScan
			dest := append([]any{&id}, scan.dest()...)
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return err
			}
			found[id] = scan.finish()
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for id, item := range byID {
			m, ok := found[id]
			if !ok {
				m = VendorUsageMetrics{CostUSD: "0", FableCostUSD: "0", OpusCostUSD: "0"}
			}
			switch window {
			case "today":
				item.TodayUsage = &m
			case "minute":
				item.RecentMinute = &m
				item.RPM = m.Requests
				item.TPM = m.TotalTokens
				item.TPMWithCache = m.TotalTokens + m.CacheTokens
			default:
				item.AllTimeUsage = &m
			}
			item.StatisticsAt = &snapshot
			item.Timezone = timezone.Name()
		}
	}
	if overview != nil {
		f.start = snapshot.Add(-time.Minute)
		f.end = snapshot
		recent, err := vendorUsageSummary(ctx, s.db, f)
		if err != nil {
			return err
		}
		overview.RecentMinute = &recent
		overview.RPM = recent.Requests
		overview.TPM = recent.TotalTokens
		overview.TPMWithCache = recent.TotalTokens + recent.CacheTokens
		overview.Timezone = timezone.Name()
		// Use the immutable supplier snapshot, including deleted accounts. Page
		// filters and pagination must not change the cumulative summary.
		for _, window := range []string{"all", "today"} {
			f.start = time.Unix(0, 0)
			if window == "today" {
				f.start = today
			}
			metrics, err := vendorUsageSummary(ctx, s.db, f)
			if err != nil {
				return err
			}
			if window == "today" {
				overview.TodayUsage = &metrics
			} else {
				overview.AllTimeUsage = &metrics
			}
		}
	}
	return nil
}

// Missing/failed native Redis reads remain null rather than a fabricated zero.
func (s *VendorService) PopulateVendorAccountConcurrency(ctx context.Context, items []*VendorAccountDTO, overview *VendorAccountOverview, native *ConcurrencyService) {
	if native == nil {
		return
	}
	accounts := make([]AccountWithConcurrency, 0, len(items))
	if overview != nil {
		v, err := s.vendorAccountListScope(ctx)
		if err != nil {
			return
		}
		rows, err := s.db.QueryContext(ctx, "SELECT id,concurrency FROM accounts WHERE "+vendorInventoryOwnerSQL(ctx, "vendor_id")+" AND parent_account_id IS NULL AND deleted_at IS NULL", v.ID)
		if err != nil {
			return
		}
		for rows.Next() {
			var a AccountWithConcurrency
			if rows.Scan(&a.ID, &a.MaxConcurrency) != nil {
				rows.Close()
				return
			}
			accounts = append(accounts, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return
		}
	} else {
		for _, item := range items {
			accounts = append(accounts, AccountWithConcurrency{ID: item.ID, MaxConcurrency: item.Concurrency})
		}
	}
	loads, err := native.GetAccountsLoadBatchFresh(ctx, accounts)
	if err != nil {
		return
	}
	var total int64
	for _, a := range accounts {
		l, ok := loads[a.ID]
		if !ok || l == nil {
			return
		}
		total += int64(l.CurrentConcurrency)
	}
	for _, item := range items {
		if l := loads[item.ID]; l != nil {
			n := l.CurrentConcurrency
			item.CurrentConcurrency = &n
		}
	}
	if overview != nil {
		overview.CurrentConcurrency = &total
		now := time.Now()
		overview.ConcurrencyAt = &now
	}
}

// Reject a queued request whose account binding/token changed before locking.
func vendorAccountEditFingerprint(a *Account) string {
	groups := vendorUniqueIDs(a.GroupIDs)
	raw, _ := json.Marshal([]any{a.Name, a.VendorNotes, a.Platform, a.ProxyID, groups, a.GetExtraString("vendor_authorization_version")})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func vendorAccountSnapshotMatches(ctx context.Context, tx *sql.Tx, a *Account) error {
	var proxy sql.NullInt64
	var access, refresh, name, notes, authorizationVersion string
	var groups pq.Int64Array
	err := tx.QueryRowContext(ctx, "SELECT proxy_id,COALESCE(credentials->>'access_token',''),COALESCE(credentials->>'refresh_token',''),name,vendor_notes,COALESCE(extra->>'vendor_authorization_version',''),ARRAY(SELECT group_id FROM account_groups WHERE account_id=accounts.id ORDER BY group_id) FROM accounts WHERE id=$1 AND deleted_at IS NULL", a.ID).Scan(&proxy, &access, &refresh, &name, &notes, &authorizationVersion, &groups)
	if err != nil {
		return ErrVendorAccountConflict
	}
	if proxy.Valid != (a.ProxyID != nil) || (proxy.Valid && proxy.Int64 != *a.ProxyID) || access != a.GetCredential("access_token") || refresh != a.GetCredential("refresh_token") || name != a.Name || notes != a.VendorNotes || authorizationVersion != a.GetExtraString("vendor_authorization_version") || !slices.Equal([]int64(groups), vendorUniqueIDs(a.GroupIDs)) {
		return ErrVendorAccountConflict
	}
	return nil
}

// Share the native background/request refresh lock; compare the nonce at release.
func (s *VendorService) vendorTokenLock(ctx context.Context, a *Account) (func(), error) {
	if s.rdb == nil {
		return nil, vendorAccountUnavailable()
	}
	key := ClaudeTokenCacheKey(a)
	if a.Platform == PlatformOpenAI {
		key = OpenAITokenCacheKey(a)
	}
	nonce, err := oauth.GenerateSessionID()
	if err != nil {
		return nil, err
	}
	keys := []string{"oauth:refresh_lock:" + key}
	if a.Platform == PlatformOpenAI {
		// Attachment, revocation and manual refresh also serialize against the
		// independent Excel refresher, whose rotating token uses this same key.
		keys = append(keys, fmt.Sprintf("oauth:refresh_lock:openai:excel:account:%d", a.ID))
	}
	locked := []string{}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for _, lock := range locked {
			_ = s.rdb.Eval(cleanup, "if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) else return 0 end", []string{lock}, nonce).Err()
		}
	}
	for _, lock := range keys {
		ok, err := s.rdb.SetNX(ctx, lock, nonce, 90*time.Second).Result()
		if err != nil || !ok {
			release()
			if err != nil {
				return nil, vendorAccountUnavailable()
			}
			return nil, ErrVendorAccountConflict
		}
		locked = append(locked, lock)
	}
	return release, nil
}

func vendorAccountCostOrder(args *[]any, which, direction string, snapshot time.Time) string {
	*args = append(*args, snapshot)
	end := len(*args)
	predicate := fmt.Sprintf("ul.vendor_id_snapshot=a.vendor_id AND ul.account_id=a.id AND ul.created_at<=$%d", end)
	if which == "today_cost" {
		local := snapshot.In(timezone.Location())
		*args = append(*args, time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()))
		predicate += fmt.Sprintf(" AND ul.created_at >= $%d", len(*args))
	}
	cost := vendorUsageCostSQL("ul")
	if which == "fable_cost" || which == "opus_cost" {
		family := "fable"
		if which == "opus_cost" {
			family = "opus"
		}
		cost = "CASE WHEN " + vendorUsageFamilySQL(family) + " THEN " + cost + " ELSE 0 END"
	}
	return "(SELECT COALESCE(SUM(" + cost + "),0) FROM usage_logs ul WHERE " + predicate + ") " + direction + ",a.id " + direction
}

// GlobalAccountPoolOverview is the intentionally public-to-suppliers fleet
// projection. It never contains account, owner, proxy, cost or credential data.
type GlobalAccountPoolOverview struct {
	Total              int64     `json:"total"`
	Available          int64     `json:"available"`
	Cooling            int64     `json:"cooling"`
	RPM                int64     `json:"rpm"`
	TPM                int64     `json:"tpm"`
	TPMWithCache       int64     `json:"tpm_with_cache"`
	CurrentConcurrency *int64    `json:"current_concurrency"`
	MaximumConcurrency int64     `json:"maximum_concurrency"`
	CollectedAt        time.Time `json:"collected_at"`
}

func (s *VendorService) GlobalAccountPoolOverview(ctx context.Context, native *ConcurrencyService) (*GlobalAccountPoolOverview, error) {
	if _, err := s.vendorAccountListScope(ctx); err != nil {
		return nil, err
	}
	reader, ok := s.accounts.(interface {
		GlobalAccountPoolInventory(context.Context) (*GlobalAccountPoolOverview, []AccountWithConcurrency, error)
	})
	if !ok {
		return nil, vendorAccountUnavailable()
	}
	out, accounts, err := reader.GlobalAccountPoolInventory(ctx)
	if err != nil {
		return nil, err
	}
	out.CollectedAt = time.Now()
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(input_tokens+output_tokens),0),COALESCE(SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),0) FROM usage_logs WHERE created_at >= $1 AND created_at < $2`, out.CollectedAt.Add(-time.Minute), out.CollectedAt).Scan(&out.RPM, &out.TPM, &out.TPMWithCache)
	if err != nil {
		return nil, err
	}
	if native != nil {
		loads, e := native.GetAccountsLoadBatchFresh(ctx, accounts)
		if e == nil {
			var total int64
			for _, a := range accounts {
				l := loads[a.ID]
				if l == nil {
					return out, nil
				}
				total += int64(l.CurrentConcurrency)
			}
			out.CurrentConcurrency = &total
		}
	}
	return out, nil
}

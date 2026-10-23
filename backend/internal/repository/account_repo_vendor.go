package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// The same visibility predicate protects native Ent and raw SQL entrypoints.
// Only trusted integer identifiers are interpolated; never request strings.
func accountVisibilitySQL(ctx context.Context, ownerColumn string) string {
	actor, hasActor := authz.FromContext(ctx)
	if hasActor && actor.Role == authz.Admin {
		return ownerColumn + " IS NULL"
	}
	if scope, ok := authz.VendorScopeFromContext(ctx); ok {
		// Aggregate inventory narrows each read to its verified owner before Ent.
		if scope.Children {
			return "FALSE"
		}
		if scope.Admin {
			if !hasActor || actor.Role != authz.SuperAdmin || actor.UserID != scope.ActorUserID || scope.VendorID < 0 {
				return "FALSE"
			}
			if scope.VendorID == 0 {
				return "(" + ownerColumn + " IS NOT NULL AND " + liveSupplyAdminSQL(actor) + ")"
			}
			if scope.PolicyVersion <= 0 {
				return "FALSE"
			}
			return fmt.Sprintf("(%s=%d AND %s AND EXISTS (SELECT 1 FROM vendors vs WHERE vs.id=%d AND vs.policy_version=%d))", ownerColumn, scope.VendorID, liveSupplyAdminSQL(actor), scope.VendorID, scope.PolicyVersion)
		}
		if scope.VendorID <= 0 || scope.ActorUserID <= 0 || scope.PolicyVersion <= 0 {
			return "FALSE"
		}
		if !hasActor || !authz.IsVendorRole(actor.Role) || actor.UserID != scope.ActorUserID {
			return "FALSE"
		}
		relation := fmt.Sprintf("vs.user_id=%d AND vu.role='%s' AND %s", actor.UserID, actor.Role, authz.ActiveVendorSQL("vs", "vu"))
		if scope.ReadOnly {
			if actor.Role != authz.VendorLevel1 {
				return "FALSE"
			}
			relation = fmt.Sprintf("vs.tier=2 AND EXISTS(SELECT 1 FROM vendors parent_v JOIN users parent_u ON parent_u.id=parent_v.user_id WHERE parent_v.id=vs.parent_vendor_id AND parent_u.id=%d AND parent_u.session_generation=%d AND %s)", actor.UserID, actor.SessionGeneration, authz.ActiveVendorSQL("parent_v", "parent_u"))
		} else {
			relation += fmt.Sprintf(" AND vu.session_generation=%d", actor.SessionGeneration)
		}
		return fmt.Sprintf("(%s=%d AND EXISTS(SELECT 1 FROM vendors vs JOIN users vu ON vu.id=vs.user_id WHERE vs.id=%d AND vs.policy_version=%d AND %s) AND EXISTS(SELECT 1 FROM settings WHERE key='vendor_console_enabled' AND value='true'))", ownerColumn, scope.VendorID, scope.VendorID, scope.PolicyVersion, relation)
	}

	if hasActor && authz.IsVendorRole(actor.Role) {
		return "FALSE"
	}
	if hasActor && actor.ManagementRequest {
		if actor.Role != authz.SuperAdmin {
			return ownerColumn + " IS NULL"
		}
		return "(" + ownerColumn + " IS NULL OR " + liveSupplyAdminSQL(actor) + ")"
	}
	return "TRUE"
}

func liveSupplyAdminSQL(actor authz.Subject) string {
	return fmt.Sprintf("EXISTS (SELECT 1 FROM users va WHERE va.id=%d AND va.role='super_admin' AND va.status='active' AND va.deleted_at IS NULL AND va.session_generation=%d)", actor.UserID, actor.SessionGeneration)
}

func accountScopePredicate(ctx context.Context) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		if condition := accountVisibilitySQL(ctx, s.C("vendor_id")); condition != "TRUE" {
			s.Where(entsql.ExprP(condition))
		}
	}
}

// Apply this inside the write statement: a preflight read alone can race archive.
const accountNotArchivedSQL = "COALESCE(extra->>'vendor_archived_at','')=''"

func accountNotArchivedPredicate(s *entsql.Selector) {
	s.Where(entsql.ExprP("COALESCE(" + s.C("extra") + "->>'vendor_archived_at','')=''"))
}

func (r *accountRepository) scopedAccountQuery(ctx context.Context) *dbent.AccountQuery {
	return clientFromContext(ctx, r.client).Account.Query().Where(accountScopePredicate(ctx))
}

// Ownership is immutable in PostgreSQL, so this raw-write guard cannot race
// with an account transfer. Caller transactions retain their own role locks.
func (r *accountRepository) guardAccountScope(ctx context.Context, ids ...int64) error {
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && (scope.ReadOnly || scope.Children) {
		return authz.ErrPermissionDenied
	}
	if accountVisibilitySQL(ctx, "vendor_id") == "TRUE" {
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	ids = uniquePositiveInt64s(ids)
	if len(ids) == 0 {
		return service.ErrAccountNotFound
	}
	count, err := r.scopedAccountQuery(ctx).Where(dbaccount.IDIn(ids...)).Count(ctx)
	if err != nil {
		return err
	}
	if count != len(ids) {
		return service.ErrAccountNotFound
	}
	return nil
}

// Generic edits cannot replace a supplier identity or resurrect a paused or
// platform-disabled account. The verified supplier flow owns these fields.
func preserveNativeVendorAccount(ctx context.Context, client *dbent.Client, a *service.Account) error {
	if a.VendorID == nil {
		return nil
	}
	credentials, err := json.Marshal(normalizeJSONMap(a.Credentials))
	if err != nil {
		return err
	}
	editableKeys := []string{}
	superAdmin := false
	if actor, ok := authz.FromContext(ctx); ok && actor.Role == authz.SuperAdmin {
		editableKeys = service.SupplierOperationalCredentialKeys()
		superAdmin = true
	}
	rows, err := client.QueryContext(ctx, "SELECT vendor_paused_at,vendor_notes,status,schedulable,platform=$3 AND type=$4 AND (credentials - $7::text[])=($5::jsonb - $7::text[]) AND (proxy_id IS NOT DISTINCT FROM $6::bigint OR ($8 AND (($6::bigint IS NULL AND EXISTS (SELECT 1 FROM vendors v LEFT JOIN vendors parent ON parent.id=v.parent_vendor_id WHERE v.id=$2 AND v.allow_direct AND (v.tier=1 OR parent.allow_direct))) OR EXISTS (SELECT 1 FROM proxies p WHERE p.id=$6 AND p.vendor_id=$2 AND p.status='active' AND p.deleted_at IS NULL AND (p.expires_at IS NULL OR p.expires_at>NOW()))))),COALESCE(extra->>'vendor_archived_at','')<>'' FROM accounts WHERE id=$1 AND vendor_id=$2 AND deleted_at IS NULL FOR UPDATE", a.ID, *a.VendorID, a.Platform, a.Type, string(credentials), a.ProxyID, pq.Array(editableKeys), superAdmin)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return service.ErrAccountNotFound
	}
	var sameIdentity, archived, currentSchedulable bool
	var currentStatus string
	if err := rows.Scan(&a.VendorPausedAt, &a.VendorNotes, &currentStatus, &currentSchedulable, &sameIdentity, &archived); err != nil {
		return err
	}
	statusEdit, schedulableEdit := service.AccountLifecycleEditFromContext(ctx)
	if !statusEdit {
		a.Status = currentStatus
	}
	if !schedulableEdit {
		a.Schedulable = currentSchedulable
	}
	if archived {
		return service.ErrVendorAccountArchived
	}
	if !sameIdentity {
		return authz.ErrPermissionDenied
	}
	return rows.Err()
}

func vendorOwnerUnchangedPredicate(owner *int64) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		if owner == nil {
			s.Where(entsql.IsNull(s.C("vendor_id")))
			return
		}
		s.Where(entsql.EQ(s.C("vendor_id"), *owner))
	}
}

// Bindings are bounded by the supplier's policy even for super administrators.
func validateVendorGroupBindings(ctx context.Context, exec sqlExecutor, accountIDs, groupIDs []int64) error {
	if len(accountIDs) == 0 {
		return nil
	}
	// Use a separate statement after locking so READ COMMITTED observes any
	// policy update that completed while we waited. Callers hold a transaction
	// and acquire these vendor locks before group/account locks.
	locked, err := exec.QueryContext(ctx, "SELECT id FROM vendors WHERE id IN (SELECT v.id FROM vendors v JOIN accounts a ON a.vendor_id=v.id WHERE a.id=ANY($1) AND a.deleted_at IS NULL UNION SELECT v.parent_vendor_id FROM vendors v JOIN accounts a ON a.vendor_id=v.id WHERE a.id=ANY($1) AND a.deleted_at IS NULL) ORDER BY tier,id FOR SHARE", pq.Array(accountIDs))
	if err != nil {
		return err
	}
	for locked.Next() {
	}
	err = locked.Err()
	locked.Close()
	if err != nil {
		return err
	}
	rows, err := exec.QueryContext(ctx, `SELECT EXISTS (SELECT 1 FROM accounts a WHERE a.id=ANY($1) AND a.vendor_id IS NOT NULL AND a.deleted_at IS NULL AND
 (COALESCE(a.extra->>'vendor_archived_at','')<>'' OR COALESCE(cardinality($2::bigint[]),0)=0 OR EXISTS (SELECT 1 FROM unnest($2::bigint[]) target(id) WHERE NOT EXISTS
 (SELECT 1 FROM vendor_groups vg JOIN groups g ON g.id=vg.group_id WHERE vg.vendor_id=a.vendor_id AND vg.group_id=target.id AND EXISTS(SELECT 1 FROM vendors v JOIN users u ON u.id=v.user_id WHERE v.id=a.vendor_id AND `+authz.ActiveVendorSQL("v", "u")+` AND v.platforms ? a.platform AND (v.tier=1 OR EXISTS(SELECT 1 FROM vendors parent_v JOIN vendor_groups parent_g ON parent_g.vendor_id=parent_v.id WHERE parent_v.id=v.parent_vendor_id AND parent_v.platforms ? a.platform AND parent_g.group_id=target.id))) AND g.deleted_at IS NULL AND g.status='active' AND g.platform=a.platform))))`, pq.Array(accountIDs), pq.Array(groupIDs))
	if err != nil {
		return err
	}
	defer rows.Close()
	var invalid bool
	if !rows.Next() {
		return authz.ErrPermissionDenied
	}
	if err := rows.Scan(&invalid); err != nil {
		return err
	}
	if invalid {
		return authz.ErrPermissionDenied
	}
	return rows.Err()
}

// Native group writes serialize with policy grants, which hold FOR SHARE on
// these same groups. Background/native consumer operations retain their path.
func beginVendorGroupWrite(ctx context.Context, client *dbent.Client, groupID int64) (*dbent.Client, *dbent.Tx, error) {
	if _, ok := authz.VendorScopeFromContext(ctx); ok {
		return nil, nil, authz.ErrPermissionDenied
	}
	actor, ok := authz.FromContext(ctx)
	if !ok || (!actor.ManagementRequest && actor.Role != authz.Admin) {
		return client, nil, nil
	}
	tx, err := client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return nil, nil, err
	}
	if tx != nil {
		client = tx.Client()
	}
	fail := func(err error) (*dbent.Client, *dbent.Tx, error) {
		if tx != nil {
			_ = tx.Rollback()
		}
		return nil, nil, err
	}
	rows, err := client.QueryContext(ctx, "SELECT id FROM groups WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", groupID)
	if err != nil {
		return fail(err)
	}
	found := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(err)
	}
	if !found {
		return fail(service.ErrGroupNotFound)
	}
	if err := guardVendorGroupWrite(ctx, client, []int64{groupID}); err != nil {
		return fail(err)
	}
	return client, tx, nil
}

// Groups remain shared consumer resources. Only mutations affecting supplier
// policy require root; this must never filter API-key owner eligibility.
func guardVendorGroupWrite(ctx context.Context, exec sqlExecutor, groupIDs []int64) error {
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return authz.ErrPermissionDenied
	}
	actor, ok := authz.FromContext(ctx)
	if !ok || (!actor.ManagementRequest && actor.Role != authz.Admin) {
		return nil
	}
	condition := "FALSE"
	if actor.Role == authz.SuperAdmin {
		condition = liveSupplyAdminSQL(actor)
	}
	rows, err := exec.QueryContext(ctx, "SELECT (EXISTS (SELECT 1 FROM vendor_groups vg WHERE vg.group_id=ANY($1)) OR EXISTS (SELECT 1 FROM account_groups ag JOIN accounts a ON a.id=ag.account_id WHERE ag.group_id=ANY($1) AND a.vendor_id IS NOT NULL AND a.deleted_at IS NULL)) AND NOT ("+condition+")", pq.Array(groupIDs))
	if err != nil {
		return err
	}
	defer rows.Close()
	var denied bool
	if !rows.Next() {
		return authz.ErrPermissionDenied
	}
	if err := rows.Scan(&denied); err != nil {
		return err
	}
	if denied {
		return authz.ErrPermissionDenied
	}
	return rows.Err()
}

// One supplier qualification rule for fresh queries and dispatch-time reads.
// groupExpr is an internal column, SQL parameter, or NULL (any eligible group).
func vendorSupplySQL(a, groupExpr string) string {
	return `(` + a + `.vendor_id IS NULL OR (` + a + `.vendor_paused_at IS NULL
 AND COALESCE(` + a + `.extra->>'vendor_archived_at','')=''
 AND EXISTS (SELECT 1 FROM settings WHERE key='vendor_console_enabled' AND value='true')
 AND EXISTS (SELECT 1 FROM vendors v JOIN users vu ON vu.id=v.user_id
 WHERE v.id=` + a + `.vendor_id AND ` + authz.ActiveVendorSQL("v", "vu") + `
 AND (` + a + `.platform<>'openai' OR (` + authz.OpenAIAuthorizationModeSQL(a, "v", "native") + ` OR ` + authz.OpenAIAuthorizationModeSQL(a, "v", "bps") + `))
 AND (` + a + `.type='oauth' OR (` + a + `.platform='anthropic' AND ` + a + `.type='setup-token'))
 AND (` + a + `.parent_account_id IS NULL OR EXISTS (SELECT 1 FROM accounts parent WHERE parent.id=` + a + `.parent_account_id AND parent.vendor_id=` + a + `.vendor_id AND parent.deleted_at IS NULL AND parent.status='active' AND parent.schedulable AND parent.vendor_paused_at IS NULL AND COALESCE(parent.extra->>'vendor_archived_at','')=''))
 AND ` + a + `.platform IN ('anthropic','openai') AND v.platforms @> jsonb_build_array(` + a + `.platform)
 AND (v.tier=1 OR EXISTS(SELECT 1 FROM vendors parent_v WHERE parent_v.id=v.parent_vendor_id AND parent_v.platforms @> jsonb_build_array(` + a + `.platform) AND (` + a + `.proxy_id IS NOT NULL OR parent_v.allow_direct)))
 AND ((` + a + `.proxy_id IS NULL AND v.allow_direct) OR EXISTS
 (SELECT 1 FROM proxies vp WHERE vp.id=` + a + `.proxy_id AND vp.vendor_id=v.id
 AND vp.deleted_at IS NULL AND vp.status='active' AND (vp.expires_at IS NULL OR vp.expires_at>NOW())))
 AND EXISTS (SELECT 1 FROM vendor_groups vg JOIN groups vsg ON vsg.id=vg.group_id
 JOIN account_groups ag ON ag.group_id=vsg.id AND ag.account_id=` + a + `.id
 WHERE vg.vendor_id=v.id AND vsg.status='active' AND vsg.deleted_at IS NULL AND vsg.platform=` + a + `.platform
 AND (v.tier=1 OR EXISTS(SELECT 1 FROM vendor_groups parent_g WHERE parent_g.vendor_id=v.parent_vendor_id AND parent_g.group_id=vsg.id))
 AND (` + groupExpr + ` IS NULL OR vsg.id=` + groupExpr + `)))))`
}

// Background refresh uses the primary credential, not whichever grant admits
// inference. Keep this separate from the scheduler's native-or-BPS predicate.
func vendorOAuthRefreshModeSQL(a string) string {
	return "(" + a + ".vendor_id IS NULL OR " + a + ".platform<>'openai' OR EXISTS (SELECT 1 FROM vendors refresh_v WHERE refresh_v.id=" + a + ".vendor_id AND CASE WHEN " + a + ".credentials->>'client_id'='app_fnr0pYvVwwFDocDumLG3H2Bp' THEN " + authz.OpenAIAuthorizationModeSQL(a, "refresh_v", "bps") + " ELSE " + authz.OpenAIAuthorizationModeSQL(a, "refresh_v", "native") + " END))"
}

type vendorOAuthRefreshSnapshotKey struct{}

var _ service.VendorOAuthRefreshRepository = (*accountRepository)(nil)

// Complete credentials and authorization version prevent an old refresh from
// overwriting a reauthorization or disabling a mode that was explicitly edited.
func vendorOAuthRefreshMatchSQL(ctx context.Context, table string, a *service.Account, offset int) (string, []any, error) {
	if a == nil || a.VendorID == nil || a.Platform != service.PlatformOpenAI || a.Type != service.AccountTypeOAuth || a.ParentAccountID != nil {
		return "", nil, service.ErrVendorSupplyUnavailable
	}
	credentials, err := json.Marshal(normalizeJSONMap(a.Credentials))
	if err != nil {
		return "", nil, err
	}
	var proxyVersion any
	if a.ProxyID != nil {
		if a.Proxy == nil || a.Proxy.ID != *a.ProxyID || !service.SameProxyOwner(a.VendorID, a.Proxy.VendorID) {
			return "", nil, service.ErrVendorSupplyUnavailable
		}
		proxyVersion = a.Proxy.ConfigVersion
	}
	args := []any{a.ID, *a.VendorID, string(credentials), a.ProxyID, proxyVersion, a.GetExtraString("vendor_authorization_version")}
	q := func(i int) string { return "$" + strconv.Itoa(offset+i) }
	predicate := table + ".id=" + q(1) + " AND " + table + ".vendor_id=" + q(2) +
		" AND " + table + ".credentials=" + q(3) + "::jsonb AND " + table + ".proxy_id IS NOT DISTINCT FROM " + q(4) + "::bigint" +
		" AND (" + table + ".proxy_id IS NULL OR EXISTS(SELECT 1 FROM proxies refresh_p WHERE refresh_p.id=" + table + ".proxy_id AND refresh_p.config_version=" + q(5) + "::bigint))" +
		" AND COALESCE(" + table + ".extra->>'vendor_authorization_version','')=" + q(6) + "::text" +
		" AND " + table + ".deleted_at IS NULL AND " + table + ".parent_account_id IS NULL AND " + table + ".platform='openai' AND " + table + ".type='oauth' AND " + table + ".status='active'" +
		" AND " + accountVisibilitySQL(ctx, table+".vendor_id") + " AND " + vendorSupplySQL(table, "NULL") + " AND " + vendorOAuthRefreshModeSQL(table)
	return predicate, args, nil
}

func (r *accountRepository) ValidateVendorOAuthRefresh(ctx context.Context, a *service.Account) error {
	predicate, args, err := vendorOAuthRefreshMatchSQL(ctx, "a", a, 0)
	if err != nil {
		return err
	}
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts a WHERE "+predicate+")", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var allowed bool
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return service.ErrVendorSupplyUnavailable
	}
	if err := rows.Scan(&allowed); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !allowed {
		return service.ErrVendorSupplyUnavailable
	}
	return nil
}

func (r *accountRepository) UpdateVendorOAuthCredentialsIfUnchanged(ctx context.Context, a *service.Account, credentials map[string]any) (bool, error) {
	if a == nil {
		return false, service.ErrAccountNotFound
	}
	if err := r.ValidateVendorOAuthRefresh(ctx, a); err != nil {
		if errors.Is(err, service.ErrVendorSupplyUnavailable) {
			return false, nil
		}
		return false, err
	}
	// Reuse the existing identity verifier, claim transaction and outbox path;
	// only add a conditional refresh predicate to its final UPDATE.
	ctx = context.WithValue(ctx, vendorOAuthRefreshSnapshotKey{}, a)
	err := r.UpdateCredentials(ctx, a.ID, credentials)
	if errors.Is(err, service.ErrAccountNotFound) || errors.Is(err, service.ErrVendorSupplyUnavailable) {
		return false, nil
	}
	return err == nil, err
}

func (r *accountRepository) DisableVendorOAuthAuthorizationIfUnchanged(ctx context.Context, a *service.Account) (bool, error) {
	if a == nil {
		return false, service.ErrAccountNotFound
	}
	if err := r.guardAccountScope(ctx, a.ID); err != nil {
		return false, err
	}
	predicate, args, err := vendorOAuthRefreshMatchSQL(ctx, "a", a, 0)
	if err != nil {
		return false, err
	}
	updates := map[string]any{"vendor_native_disabled": true, "vendor_authorization_version": strconv.FormatInt(time.Now().UnixNano(), 10)}
	if a.IsExcelOAuth() {
		delete(updates, "vendor_native_disabled")
		updates["openai_excel_bps"] = false
	}
	raw, err := json.Marshal(updates)
	if err != nil {
		return false, err
	}
	args = append(args, string(raw), service.SchedulerOutboxEventAccountChanged)
	result, err := txAwareSQLExecutor(ctx, r.sql, r.client).ExecContext(ctx, `WITH updated AS (
 UPDATE accounts a SET extra=COALESCE(extra,'{}'::jsonb)||$7::jsonb,updated_at=NOW()
 WHERE `+predicate+` RETURNING a.id)
 INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
 SELECT $8,id,NULL,NULL FROM updated`, args...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err == nil && n > 0 {
		r.syncSchedulerAccountSnapshotDetached(ctx, a.ID)
	}
	return n > 0, err
}

// VendorGroupAvailabilitySQL exposes the same live qualification used by
// native dispatch to the admin aggregate query. Aliases are fixed (a=account,
// g=group); no request-controlled identifier or SQL fragment is accepted.
func (r *accountRepository) VendorGroupAvailabilitySQL() string {
	return "(g.status='active' AND " + accountAvailabilitySQL("a") + " AND " + vendorSupplySQL("a", "g.id") + ")"
}

func accountAvailabilitySQL(a string) string {
	return "(" + a + ".status='active' AND " + a + ".schedulable AND (" + a + ".expires_at IS NULL OR " + a + ".expires_at>NOW() OR NOT " + a + ".auto_pause_on_expired) AND (" + a + ".temp_unschedulable_until IS NULL OR " + a + ".temp_unschedulable_until<=NOW()) AND (" + a + ".rate_limit_reset_at IS NULL OR " + a + ".rate_limit_reset_at<=NOW()) AND (" + a + ".overload_until IS NULL OR " + a + ".overload_until<=NOW()))"
}

func vendorSupplyPredicate(groupID *int64) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		group := "NULL"
		if groupID != nil {
			group = strconv.FormatInt(*groupID, 10)
		}
		s.Where(entsql.ExprP(vendorSupplySQL(s.TableName(), group)))
	}
}

// Native Redis metadata omits ownership and proxy credentials. Hydrate live
// qualification by ID in one query, also protecting pre-feature snapshots.
func (r *accountRepository) FilterVendorSupply(ctx context.Context, accounts []service.Account, groupID *int64) ([]service.Account, error) {
	if len(accounts) == 0 {
		return accounts, nil
	}
	ids := make([]int64, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	var group any
	if groupID != nil {
		group = *groupID
	}
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, `SELECT a.id,a.vendor_id,a.vendor_paused_at,
 (`+accountAvailabilitySQL("a")+` AND `+vendorSupplySQL("a", "$2::bigint")+`),
 EXISTS(SELECT 1 FROM vendors v WHERE v.id=a.vendor_id AND `+authz.OpenAIAuthorizationModeSQL("a", "v", "native")+`),
 EXISTS(SELECT 1 FROM vendors v WHERE v.id=a.vendor_id AND `+authz.OpenAIAuthorizationModeSQL("a", "v", "bps")+`)
 FROM accounts a WHERE a.id=ANY($1) AND a.deleted_at IS NULL`, pq.Array(ids), group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type state struct {
		vendorID    *int64
		paused      *time.Time
		allowed     bool
		native, bps bool
	}
	states := make(map[int64]state, len(accounts))
	for rows.Next() {
		var id int64
		var v state
		if err := rows.Scan(&id, &v.vendorID, &v.paused, &v.allowed, &v.native, &v.bps); err != nil {
			return nil, err
		}
		states[id] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]service.Account, 0, len(accounts))
	for _, a := range accounts {
		v, ok := states[a.ID]
		if !ok || (v.vendorID != nil && !v.allowed) {
			continue
		}
		a.VendorID = v.vendorID
		a.VendorPausedAt = v.paused
		a.VendorSupplyBlocked = false
		a.VendorAuthorizationModesLoaded = v.vendorID != nil
		a.VendorNativeAllowed, a.VendorBPSAllowed = v.native, v.bps
		out = append(out, a)
	}
	return out, nil
}

// Live DB failures deny new dispatch; this does not interrupt response streams.
func (r *accountRepository) ValidateVendorSupply(ctx context.Context, a *service.Account, groupID *int64) error {
	return r.validateVendorSupplyMode(ctx, a, groupID, "")
}
func (r *accountRepository) ValidateVendorAuthorizationMode(ctx context.Context, a *service.Account, groupID *int64, mode string) error {
	if mode != "native" && mode != "bps" {
		return service.ErrVendorSupplyUnavailable
	}
	return r.validateVendorSupplyMode(ctx, a, groupID, mode)
}
func (r *accountRepository) validateVendorSupplyMode(ctx context.Context, a *service.Account, groupID *int64, mode string) error {
	if a == nil {
		return service.ErrAccountNotFound
	}
	if r == nil || (r.sql == nil && r.client == nil) {
		return service.ErrVendorSupplyUnavailable
	}
	var proxyVersion any
	if a.VendorID != nil && a.ProxyID != nil {
		if a.Proxy == nil || a.Proxy.ID != *a.ProxyID || !service.SameProxyOwner(a.VendorID, a.Proxy.VendorID) {
			return service.ErrVendorSupplyUnavailable
		}
		proxyVersion = a.Proxy.ConfigVersion
	}
	var vendor any
	if a.VendorID != nil {
		vendor = *a.VendorID
	}
	var group any
	if groupID != nil {
		group = *groupID
	}
	modeCondition := "TRUE"
	if mode != "" {
		modeCondition = "(a.platform<>'openai' OR EXISTS(SELECT 1 FROM vendors v WHERE v.id=a.vendor_id AND " + authz.OpenAIAuthorizationModeSQL("a", "v", mode) + "))"
	}
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, `SELECT EXISTS (SELECT 1 FROM accounts a
 WHERE a.id=$1 AND a.deleted_at IS NULL AND (a.vendor_id IS NULL OR (a.vendor_id=$2 AND a.platform=$3 AND a.proxy_id IS NOT DISTINCT FROM $5::bigint
 AND `+accountAvailabilitySQL("a")+`
 AND (a.proxy_id IS NULL OR EXISTS (SELECT 1 FROM proxies p WHERE p.id=a.proxy_id AND p.config_version=$6::bigint))
 AND `+vendorSupplySQL("a", "$4::bigint")+` AND `+modeCondition+`)))`, a.ID, vendor, a.Platform, group, a.ProxyID, proxyVersion)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrVendorSupplyUnavailable, err)
	}
	defer rows.Close()
	var allowed bool
	if !rows.Next() {
		return service.ErrVendorSupplyUnavailable
	}
	if err := rows.Scan(&allowed); err != nil {
		return fmt.Errorf("%w: %v", service.ErrVendorSupplyUnavailable, err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: %v", service.ErrVendorSupplyUnavailable, err)
	}
	if !allowed {
		return service.ErrVendorSupplyUnavailable
	}
	return nil
}

// Policy changes durably queue native rebuilding and evict sticky snapshots;
// they never rewrite account disabled, paused, or credential state.
func (r *accountRepository) RefreshVendorSnapshots(ctx context.Context, vendorID int64) error {
	if err := enqueueSchedulerOutbox(ctx, txAwareSQLExecutor(ctx, r.sql, r.client), service.SchedulerOutboxEventFullRebuild, nil, nil, nil); err != nil {
		return err
	}
	if service.DeferManagementCommit(ctx, func(committed context.Context) { _ = r.evictVendorSnapshots(committed, vendorID) }) {
		return nil
	}
	return r.evictVendorSnapshots(ctx, vendorID)
}

func (r *accountRepository) evictVendorSnapshots(ctx context.Context, vendorID int64) error {
	if r.schedulerCache == nil {
		return nil
	}
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, "SELECT id FROM accounts WHERE vendor_id IN (SELECT id FROM vendors WHERE id=$1 OR parent_vendor_id=$1)", vendorID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.schedulerCache.DeleteAccount(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

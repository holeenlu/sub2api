package repository

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.AccountExcelBPSDispatchRepository = (*accountRepository)(nil)

// This is a send-time read, not a new selector, refresh owner or retry budget.
func (r *accountRepository) ValidateExcelBPSDispatch(ctx context.Context, a *service.Account, groupID *int64, upstreamModel string) error {
	if a == nil || !a.IsExcelBPSEligible() {
		return errors.New("Excel BPS account is unavailable")
	}
	if service.IsExcelBPSRecoveryProbe(ctx) {
		valid, err := r.ValidateExcelBPSRecovery(ctx, a)
		if err != nil {
			return err
		}
		if !valid {
			return errors.New("Excel BPS recovery claim changed")
		}
	} else if !a.IsExcelBPSEnabled() {
		return errors.New("Excel BPS disabled")
	}
	fingerprint, bound := service.ExcelBPSDispatchFingerprint(ctx, a.ID)
	if !bound || fingerprint == "" {
		return errors.New("Excel BPS dispatch grant is not bound")
	}
	if err := r.ValidateVendorAuthorizationMode(ctx, a, groupID, "bps"); err != nil {
		return err
	}
	credentials, err := json.Marshal(a.Credentials)
	if err != nil {
		return err
	}
	extra, err := json.Marshal(a.Extra)
	if err != nil {
		return err
	}
	groups := append([]int64{}, a.GroupIDs...)
	slices.Sort(groups)
	if groupID != nil && !slices.Contains(groups, *groupID) {
		return errors.New("Excel BPS group membership changed")
	}
	var proxy int64
	if a.ProxyID != nil {
		proxy = *a.ProxyID
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT id FROM accounts
 WHERE id=$1 AND deleted_at IS NULL AND parent_account_id IS NULL
 AND platform='openai' AND type='oauth' AND status='active' AND schedulable=true
 AND COALESCE(extra->>'vendor_archived_at','')=''
 AND (auto_pause_on_expired=false OR expires_at IS NULL OR expires_at>NOW())
 AND credentials=$2::jsonb AND COALESCE(proxy_id,0)=$4
 AND COALESCE(extra->'openai_cloud','false'::jsonb)<>'true'::jsonb
 AND (extra->'openai_excel_bps'='true'::jsonb OR $7::boolean)
 AND (`+excelBPSCurrentGrantSQL+`)=$6
 AND ARRAY(SELECT group_id FROM account_groups WHERE account_id=accounts.id ORDER BY group_id)=$5::bigint[]
 AND NOT EXISTS(SELECT 1 FROM jsonb_object_keys(COALESCE(extra,'{}'::jsonb)||$3::jsonb) AS o(key)
 WHERE key ~ '^openai_excel_bps' AND key<>'openai_excel_bps_failure'
 AND extra->key IS DISTINCT FROM $3::jsonb->key)
 AND ($8::text='' OR NOT (extra ? 'openai_excel_bps_models') OR
 (jsonb_typeof(extra->'openai_excel_bps_models')='array' AND extra->'openai_excel_bps_models' ? $8::text))`,
		a.ID, string(credentials), string(extra), proxy, pq.Array(groups), fingerprint, service.IsExcelBPSRecoveryProbe(ctx), strings.TrimSpace(upstreamModel))
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errors.New("Excel BPS dispatch authorization changed")
	}
	return rows.Err()
}

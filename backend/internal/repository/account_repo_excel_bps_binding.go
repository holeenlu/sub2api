package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// This snapshot contains policy, not tokens. Normal usage snapshots are omitted.
// Group row identities and settings updated_at also prevent off/on ABA edits
// from reviving an in-flight probe. Account writers serialize on the account row.
const excelBPSPolicySQL = `jsonb_build_object(
 'options', (SELECT COALESCE(jsonb_object_agg(k,v),'{}'::jsonb)
 FROM jsonb_each(COALESCE(accounts.extra,'{}'::jsonb)) o(k,v)
 WHERE k ~ '^openai_excel_bps' AND k NOT IN (
 'openai_excel_bps','openai_excel_bps_failure','openai_excel_bps_403_disabled_at',
 'openai_excel_bps_403_last_probe_at','openai_excel_bps_403_last_probe_result','openai_excel_bps_403_moved_at','openai_excel_bps_403_moved_group_id')),
 'cloud', COALESCE(accounts.extra->'openai_cloud','false'::jsonb),
 'primary', md5(accounts.credentials::text),
 'proxy', accounts.proxy_id,
 'groups', (SELECT COALESCE(jsonb_agg(jsonb_build_array(to_jsonb(ag),to_jsonb(g)) ORDER BY ag.group_id),'[]'::jsonb)
 FROM account_groups ag JOIN groups g ON g.id=ag.group_id WHERE ag.account_id=accounts.id),
 'global', (SELECT jsonb_build_array(s.value,s.updated_at) FROM settings s WHERE s.key='excel_bps_enabled'))`

const excelBPSCurrentGrantSQL = `CASE WHEN credentials->>'client_id'='` + openai.ExcelClientID + `' THEN 'primary'
 ELSE COALESCE((SELECT CASE WHEN g.ciphertext='' THEN '' ELSE md5(g.ciphertext) END
 FROM openai_excel_grants g WHERE g.account_id=accounts.id),'') END`

var excelBPSVendorRecoveryGuardSQL = ` AND (vendor_id IS NULL OR (` +
	vendorSupplySQL("accounts", "NULL") + ` AND EXISTS(SELECT 1 FROM vendors v WHERE v.id=accounts.vendor_id AND ` +
	authz.OpenAIAuthorizationModeSQL("accounts", "v", "bps") + `)))`

func lockExcelBPSAccount(ctx context.Context, exec sqlExecutor, id int64) (bool, error) {
	rows, err := exec.QueryContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

// Source provenance is preserved at the service boundary; this checks that its
// observed grant still exists at the conditional write.
func validExcelBPSFailure(f service.ExcelBPSFailure) bool {
	switch f.Reason {
	case "account_forbidden":
		return !f.ReauthRequired
	case "grant_missing", "grant_revoked", "refresh_grant_missing", "invalid_grant",
		"token_revoked", "refreshed_access_token_rejected":
		return f.ReauthRequired
	default:
		return false
	}
}

// Reconcile an uncertain commit without repeating it. The unique system action
// ID distinguishes this action from a subsequent shutdown or manual edit.
func (r *accountRepository) excelBPSActionCommitted(ctx context.Context, id int64, action, state string) (bool, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT extra->'openai_excel_bps_failure'->>'id',
 extra->'openai_excel_bps_failure'->>'state' FROM accounts WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var gotAction, gotState *string
	if err := rows.Scan(&gotAction, &gotState); err != nil {
		return false, err
	}
	return gotAction != nil && gotState != nil && *gotAction == action && *gotState == state, rows.Err()
}

func excelBPSActionID(a *service.Account) string {
	if a == nil {
		return ""
	}
	record, _ := a.Extra[service.ExcelBPSFailureKey].(map[string]any)
	id, _ := record["id"].(string)
	return id
}

func (r *accountRepository) ReadExcelBPSRecoverySnapshot(ctx context.Context, a *service.Account, claim string) (*service.Account, error) {
	fresh, err := r.GetByID(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if !fresh.IsExcelBPS403RecoveryPending() || fresh.Extra[service.ExcelBPS403LastProbeAtKey] != claim ||
		(excelBPSActionID(a) != "" && excelBPSActionID(a) != excelBPSActionID(fresh)) {
		return nil, errors.New("Excel BPS recovery claim changed")
	}
	ok, err := r.ValidateExcelBPSRecovery(ctx, fresh)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("Excel BPS recovery authorization changed")
	}
	return fresh, nil
}

func (r *accountRepository) ValidateExcelBPSRecovery(ctx context.Context, a *service.Account) (bool, error) {
	if !a.IsExcelBPS403RecoveryPending() {
		return false, nil
	}
	claim, _ := a.Extra[service.ExcelBPS403LastProbeAtKey].(string)
	if _, err := time.Parse(time.RFC3339Nano, claim); err != nil {
		return false, nil
	}
	args, err := excelBPSRecoveryArgs(a)
	if err != nil {
		return false, err
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT id FROM accounts `+excelBPSRecoveryGuardSQL, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func excelBPSFailureJSON(id, at string, fact service.ExcelBPSFailure, account *service.Account) string {
	var next any
	if !fact.ReauthRequired && account.Extra[service.ExcelBPSAutoRecoverOn403Key] == true {
		when, _ := time.Parse(time.RFC3339Nano, at)
		next = when.Add(account.ExcelBPS403RecoveryInterval()).Format(time.RFC3339Nano)
	}
	record, _ := json.Marshal(map[string]any{
		"id": id, "reason": fact.Reason, "scope": "account_bps", "occurred_at": at,
		"source": "system", "state": "disabled", "reauth_required": fact.ReauthRequired,
		"grant_fingerprint": fact.GrantFingerprint, "next_probe_at": next,
	})
	return string(record)
}

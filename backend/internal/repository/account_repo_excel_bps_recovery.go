package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

var _ service.AccountExcelBPSRecoveryRepository = (*accountRepository)(nil)

// Compare all BPS options (including the server-owned shutdown/claim markers)
// but allow unrelated usage snapshots to change during the probe.
var excelBPSRecoveryGuardSQL = `
WHERE id = $1 AND deleted_at IS NULL AND parent_account_id IS NULL
 AND COALESCE(extra->>'vendor_archived_at','')=''
 AND platform = 'openai' AND type = 'oauth' AND status = 'active' AND schedulable = true
 AND (auto_pause_on_expired = false OR expires_at IS NULL OR expires_at > NOW())
 AND credentials = $2::jsonb AND COALESCE(proxy_id, 0) = $4
 AND COALESCE(extra -> 'openai_cloud', 'false'::jsonb) <> 'true'::jsonb
 AND COALESCE(extra -> 'openai_excel_bps', 'false'::jsonb) <> 'true'::jsonb
 AND extra -> 'openai_excel_bps_auto_disable_on_403' = 'true'::jsonb
 AND extra -> 'openai_excel_bps_auto_recover_on_403' = 'true'::jsonb
 AND jsonb_typeof(extra -> 'openai_excel_bps_403_disabled_at') = 'string'
 AND EXISTS(SELECT 1 FROM settings s WHERE s.key='excel_bps_enabled' AND s.value='true')
 AND (` + excelBPSCurrentGrantSQL + `)<>''
 AND (NOT (extra ? 'openai_excel_bps_failure') OR (
  extra->'openai_excel_bps_failure'->>'state'='disabled'
  AND extra->'openai_excel_bps_failure'->>'source'='system'
  AND extra->'openai_excel_bps_failure'->>'reauth_required'='false'
  AND extra->'openai_excel_bps_failure'->>'grant_fingerprint'=(` + excelBPSCurrentGrantSQL + `)
  AND extra->'openai_excel_bps_failure'->'policy'=(` + excelBPSPolicySQL + `)))
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_object_keys(COALESCE(extra, '{}'::jsonb) || $3::jsonb) AS options(key)
  WHERE options.key ~ '^openai_excel_bps'
   AND (CASE WHEN options.key='openai_excel_bps_failure' THEN (extra->options.key) - 'grant_fingerprint' ELSE extra->options.key END)
    IS DISTINCT FROM
    (CASE WHEN options.key='openai_excel_bps_failure' THEN ($3::jsonb->options.key) - 'grant_fingerprint' ELSE $3::jsonb->options.key END)
 )` + excelBPSVendorRecoveryGuardSQL

func excelBPSRecoveryArgs(account *service.Account) ([]any, error) {
	credentials, err := json.Marshal(account.Credentials)
	if err != nil {
		return nil, err
	}
	extra, err := json.Marshal(account.Extra)
	if err != nil {
		return nil, err
	}
	var proxyID int64
	if account.ProxyID != nil {
		proxyID = *account.ProxyID
	}
	return []any{account.ID, string(credentials), string(extra), proxyID}, nil
}

func (r *accountRepository) ClaimExcelBPS403Probe(ctx context.Context, account *service.Account, now time.Time) (bool, error) {
	if !account.ExcelBPS403RecoveryDue(now) {
		return false, nil
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.claimExcelBPS403ProbeInTx(ctx, account, now)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	claimed, err := r.claimExcelBPS403ProbeInTx(dbent.NewTxContext(ctx, tx), account, now)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		fresh, readErr := r.GetByID(readCtx, account.ID)
		if readErr != nil || fresh == nil || fresh.Extra[service.ExcelBPS403LastProbeAtKey] != now.UTC().Format(time.RFC3339Nano) {
			return false, errors.Join(err, readErr)
		}
	}
	if claimed {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return claimed, nil
}

func (r *accountRepository) claimExcelBPS403ProbeInTx(ctx context.Context, account *service.Account, now time.Time) (bool, error) {
	client := clientFromContext(ctx, r.client)
	locked, err := lockExcelBPSAccount(ctx, client, account.ID)
	if err != nil || !locked {
		return false, err
	}
	args, err := excelBPSRecoveryArgs(account)
	if err != nil {
		return false, err
	}
	args = append(args, now.UTC().Format(time.RFC3339Nano))
	args = append(args, uuid.NewString(), now.Add(account.ExcelBPS403RecoveryInterval()).UTC().Format(time.RFC3339Nano))
	pending, err := json.Marshal(service.ExcelBPS403ProbeClaimResult(account, now))
	if err != nil {
		return false, err
	}
	args = append(args, string(pending))
	result, err := client.ExecContext(ctx, `UPDATE accounts
SET extra = jsonb_set(extra, '{openai_excel_bps_403_last_probe_at}', to_jsonb($5::text))
 || jsonb_build_object('openai_excel_bps_403_last_probe_result',$8::jsonb)
 || jsonb_build_object('openai_excel_bps_failure',
 COALESCE(extra->'openai_excel_bps_failure', jsonb_build_object(
 'id',$6::text,'reason','account_forbidden','scope','account_bps','source','system','state','disabled',
 'occurred_at',extra->'openai_excel_bps_403_disabled_at','reauth_required',false,
 'grant_fingerprint',`+excelBPSCurrentGrantSQL+`,'policy',`+excelBPSPolicySQL+`))
 || jsonb_build_object('next_probe_at',$7::text)), updated_at = NOW()
`+excelBPSRecoveryGuardSQL, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, nil); err != nil {
		return false, err
	}
	return true, nil
}

func (r *accountRepository) RestoreExcelBPSAfter403(ctx context.Context, account *service.Account, outcome service.ExcelBPS403ProbeResult) (bool, error) {
	if !account.IsExcelBPS403RecoveryPending() || !outcome.Usage.MatchesClaim(account) || outcome.Status != "restored" ||
		outcome.Usage.SendState != "sent" || outcome.Usage.Terminal != "completed" {
		return false, nil
	}
	claim, _ := account.Extra[service.ExcelBPS403LastProbeAtKey].(string)
	if _, err := time.Parse(time.RFC3339Nano, claim); err != nil {
		return false, nil
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	args, err := excelBPSRecoveryArgs(account)
	if err != nil {
		return false, err
	}
	raw, err := json.Marshal(outcome)
	if err != nil {
		return false, err
	}
	args = append(args, string(raw))
	client := clientFromContext(txCtx, r.client)
	locked, err := lockExcelBPSAccount(txCtx, client, account.ID)
	if err != nil || !locked {
		return false, err
	}
	result, err := client.ExecContext(txCtx, `UPDATE accounts
SET extra = jsonb_set(extra, '{openai_excel_bps}', 'true'::jsonb)
 - 'openai_excel_bps_403_disabled_at' - 'openai_excel_bps_403_last_probe_at'
 || jsonb_build_object('openai_excel_bps_failure', extra->'openai_excel_bps_failure'
 || '{"state":"recovered","next_probe_at":null}'::jsonb)
 || jsonb_build_object('openai_excel_bps_403_last_probe_result',$5::jsonb), updated_at = NOW()
`+excelBPSRecoveryGuardSQL, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		if err := enqueueSchedulerOutbox(txCtx, client, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, nil); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		confirmed, readErr := r.excelBPSActionCommitted(readCtx, account.ID, excelBPSActionID(account), "recovered")
		if !confirmed {
			return false, errors.Join(err, readErr)
		}
	}
	if affected > 0 {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return affected > 0, nil
}

// Recording an observation cannot authorize recovery. The durable probe identity
// fences this metadata write independently of live policy: revocation may stop
// restoration after consumption. A finalized result is immutable, and another
// claim cannot be overwritten. RestoreExcelBPSAfter403 retains the full guards.
func (r *accountRepository) RecordExcelBPS403ProbeResult(ctx context.Context, account *service.Account, outcome service.ExcelBPS403ProbeResult) (bool, error) {
	if !outcome.Usage.MatchesClaim(account) || outcome.Status != "failed" {
		return false, nil
	}
	claim, _ := account.Extra[service.ExcelBPS403LastProbeAtKey].(string)
	if _, err := time.Parse(time.RFC3339Nano, claim); err != nil {
		return false, nil
	}
	raw, err := json.Marshal(outcome)
	if err != nil {
		return false, err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	locked, err := lockExcelBPSAccount(txCtx, client, account.ID)
	if err != nil || !locked {
		return false, err
	}
	result, err := client.ExecContext(txCtx, `UPDATE accounts
 SET extra=jsonb_set(extra,'{openai_excel_bps_403_last_probe_result}',$3::jsonb), updated_at=NOW()
 WHERE id=$1 AND deleted_at IS NULL
 AND extra->'openai_excel_bps_403_last_probe_result'->'usage'->>'claim_at'=$2::text
 AND extra->'openai_excel_bps_403_last_probe_result'->>'status'='probing'
 AND extra->'openai_excel_bps_403_last_probe_result'->'usage'->>'probe_id'=$3::jsonb->'usage'->>'probe_id'
 AND extra->'openai_excel_bps_403_last_probe_result'->'usage'->>'scope'=$3::jsonb->'usage'->>'scope'
 AND extra->'openai_excel_bps_403_last_probe_result'->'usage'->>'account_id'=$1::text
`, account.ID, claim, string(raw))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		if err := enqueueSchedulerOutbox(txCtx, client, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, nil); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		fresh, readErr := r.GetByID(readCtx, account.ID)
		if readErr != nil || fresh == nil {
			return false, errors.Join(err, readErr)
		}
		stored, marshalErr := json.Marshal(fresh.Extra[service.ExcelBPS403LastProbeResultKey])
		var expected map[string]any
		_ = json.Unmarshal(raw, &expected)
		canonical, _ := json.Marshal(expected)
		if marshalErr != nil || string(stored) != string(canonical) {
			return false, errors.Join(err, marshalErr)
		}
	}
	if affected > 0 {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return affected > 0, nil
}

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

var _ service.AccountExcelBPSRepository = (*accountRepository)(nil)

// DisableExcelBPSOn403 changes only the protocol switch. A stale request cannot
// disable an account whose credentials or opt-in have since been changed.
func (r *accountRepository) DisableExcelBPSOn403(ctx context.Context, account *service.Account) (bool, error) {
	if !account.IsExcelBPSAutoDisableOn403Enabled() {
		return false, nil
	}
	fingerprint := ""
	if account.IsExcelOAuth() {
		fingerprint = "primary"
	}
	return r.DisableExcelBPSForFailure(ctx, account, service.ExcelBPSFailure{Reason: "account_forbidden", GrantFingerprint: fingerprint})
}

func (r *accountRepository) DisableExcelBPSForFailure(ctx context.Context, account *service.Account, fact service.ExcelBPSFailure) (bool, error) {
	if account == nil || !account.IsExcelBPSEligible() || !validExcelBPSFailure(fact) ||
		(fact.Reason == "account_forbidden" && !account.IsExcelBPSAutoDisableOn403Enabled()) {
		return false, nil
	}
	action := uuid.NewString()
	if dbent.TxFromContext(ctx) != nil {
		return r.disableExcelBPSFailureInTx(ctx, account, fact, action)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.disableExcelBPSFailureInTx(ctx, account, fact, action)
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := r.disableExcelBPSFailureInTx(dbent.NewTxContext(ctx, tx), account, fact, action)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		confirmed, readErr := r.excelBPSActionCommitted(readCtx, account.ID, action, "disabled")
		if !confirmed {
			return false, errors.Join(err, readErr)
		}
	}
	if changed {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return changed, nil
}

func (r *accountRepository) disableExcelBPSFailureInTx(ctx context.Context, account *service.Account, fact service.ExcelBPSFailure, action string) (bool, error) {
	credentials, err := json.Marshal(account.Credentials)
	if err != nil {
		return false, err
	}
	client := clientFromContext(ctx, r.client)
	locked, err := lockExcelBPSAccount(ctx, client, account.ID)
	if err != nil || !locked {
		return false, err
	}
	extra, err := json.Marshal(account.Extra)
	if err != nil {
		return false, err
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	// Only account-level 403 retains its historical marker and opt-in. Missing
	// or rejected grants have their own reason and never schedule a probe. Keep
	// the last probe result: authorization loss cannot erase its consumption.
	result, err := client.ExecContext(ctx, `
UPDATE accounts
SET extra = (jsonb_set(COALESCE(extra,'{}'::jsonb), '{openai_excel_bps}', 'false'::jsonb)
 - 'openai_excel_bps_403_last_probe_at' - 'openai_excel_bps_403_disabled_at')
 || CASE WHEN $4::jsonb->>'reason'='account_forbidden' THEN jsonb_build_object('openai_excel_bps_403_disabled_at', $3::text) ELSE '{}'::jsonb END
 || jsonb_build_object('openai_excel_bps_failure', $4::jsonb || jsonb_build_object('policy', `+excelBPSPolicySQL+`)),
 updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL AND parent_account_id IS NULL
 AND COALESCE(extra->>'vendor_archived_at','')=''
  AND platform = 'openai' AND type = 'oauth'
  AND credentials = $2::jsonb
  AND COALESCE(extra -> 'openai_cloud', 'false'::jsonb) <> 'true'::jsonb
  AND (extra -> 'openai_excel_bps' = 'true'::jsonb OR
    (extra->'openai_excel_bps_failure'->>'state'='disabled' AND extra->'openai_excel_bps_failure'->>'reauth_required'='false'
     AND $4::jsonb->>'reauth_required'='true'
     AND extra->'openai_excel_bps_failure'->>'id'=$5::jsonb->'openai_excel_bps_failure'->>'id'
     AND extra->'openai_excel_bps_403_last_probe_at' IS NOT DISTINCT FROM $5::jsonb->'openai_excel_bps_403_last_probe_at'))
  AND ($4::jsonb->>'reason'<>'account_forbidden' OR extra -> 'openai_excel_bps_auto_disable_on_403' = 'true'::jsonb)
  AND `+excelBPSCurrentGrantSQL+` = $4::jsonb->>'grant_fingerprint'`,
		account.ID, string(credentials), at, excelBPSFailureJSON(action, at, fact, account), string(extra))
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

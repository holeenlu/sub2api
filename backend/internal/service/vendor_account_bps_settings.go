package service

import (
	"context"
	"database/sql"
	"encoding/json"
)

// persistVendorAuthorizationSettings runs after vendorLockAccount, within the
// existing vendor/account/outbox transaction. Only the explicit switch command
// consumes recovery authority; ordinary supplier metadata/model edits do not.
func persistVendorAuthorizationSettings(ctx context.Context, tx *sql.Tx, a *Account, in VendorAccountUpdate, updates map[string]any) error {
	extraExpression := "extra||$1::jsonb"
	if in.BPSEnabled != nil {
		var raw []byte
		if err := tx.QueryRowContext(ctx, "SELECT extra FROM accounts WHERE id=$1", a.ID).Scan(&raw); err != nil {
			return err
		}
		var current map[string]any
		if err := json.Unmarshal(raw, &current); err != nil {
			return err
		}
		if in.BPSExpectedDisabledAt != nil {
			updates[ExcelBPSExpectedDisabledAtKey] = *in.BPSExpectedDisabledAt
		}
		var err error
		updates, err = MergeExcelBPSSettingsUpdate(updates, current)
		if err != nil {
			return err
		}
		// JSONB merge cannot delete a key missing from its RHS. Remove only the
		// old shutdown/claim; retain probe consumption and group-action history.
		extraExpression = "(extra - 'openai_excel_bps_403_disabled_at' - 'openai_excel_bps_403_last_probe_at' - 'openai_excel_bps_failure')||$1::jsonb"
	}
	raw, err := json.Marshal(updates)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE accounts SET extra="+extraExpression+",updated_at=NOW() WHERE id=$2", string(raw), a.ID); err != nil {
		return err
	}

	return err
}

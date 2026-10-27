package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) UpdateModelReferences(ctx context.Context, a *service.Account, models []string, rewrites map[string]any) error {
	if err := r.guardAccountScope(ctx, a.ID); err != nil {
		return err
	}
	refs, err := json.Marshal(models)
	if err != nil {
		return err
	}
	routes, err := json.Marshal(rewrites)
	if err != nil {
		return err
	}
	previous, err := json.Marshal(a.Credentials[service.AccountModelReferencesKey])
	if err != nil {
		return err
	}
	oldRoutes, err := json.Marshal(a.Credentials["model_mapping"])
	if err != nil {
		return err
	}
	result, err := txAwareSQLExecutor(ctx, r.sql, r.client).ExecContext(ctx, `WITH updated AS (
 UPDATE accounts SET credentials=jsonb_set(jsonb_set(COALESCE(credentials,'{}'::jsonb),'{model_allowlist}',$2::jsonb),'{model_mapping}',$3::jsonb),updated_at=NOW()
 WHERE id=$1 AND deleted_at IS NULL AND COALESCE(extra->>'vendor_archived_at','')=''
 AND COALESCE(credentials->'model_allowlist','null'::jsonb)=$4::jsonb
 AND COALESCE(credentials->'model_mapping','null'::jsonb)=$5::jsonb
 RETURNING id)
 INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload) SELECT $6,id,NULL,NULL FROM updated`, a.ID, string(refs), string(routes), string(previous), string(oldRoutes), service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrModelPolicyConflict
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, a.ID)
	return nil
}

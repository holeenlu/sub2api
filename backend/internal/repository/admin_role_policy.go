package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/ent/setting"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *settingRepository) UpdateAdminRolePolicy(ctx context.Context, expected int64, policy *authz.Policy, trace *service.AuditLog) error {
	actor, ok := authz.FromContext(ctx)
	if !ok || actor.Role != authz.SuperAdmin {
		return service.ErrAdminPermissionDenied
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	if expected == 0 {
		// Materialize a missing row before locking it. Only a corrupt/missing
		// policy accepts recovery; an ordinary concurrent update still conflicts.
		if _, err := client.ExecContext(ctx, "INSERT INTO settings (key,value,updated_at) VALUES ($1,'{}',NOW()) ON CONFLICT (key) DO NOTHING", authz.PolicyKey); err != nil {
			return err
		}
	}
	rows, err := client.QueryContext(ctx, "SELECT value FROM settings WHERE key = $1 FOR UPDATE", authz.PolicyKey)
	if err != nil {
		return err
	}
	var raw string
	if !rows.Next() {
		_ = rows.Close()
		return service.ErrAdminPolicyUnavailable
	}
	err = rows.Scan(&raw)
	_ = rows.Close()
	if err != nil {
		return err
	}
	var current authz.Policy
	valid := json.Unmarshal([]byte(raw), &current) == nil && current.Validate() == nil
	if !valid && expected != 0 {
		return service.ErrAdminPolicyUnavailable
	}
	if (valid && (expected == 0 || current.Version != expected)) || policy.Version != expected+1 {
		return service.ErrAdminPolicyConflict
	}
	// Revalidate the actor while serialized with other privilege changes.
	rows, err = client.QueryContext(ctx, "SELECT role, status, session_generation FROM users WHERE id=$1 AND deleted_at IS NULL FOR SHARE", actor.UserID)
	if err != nil {
		return err
	}
	var role, status string
	var generation int64
	if !rows.Next() {
		_ = rows.Close()
		return service.ErrAdminPermissionDenied
	}
	err = rows.Scan(&role, &status, &generation)
	_ = rows.Close()
	if err != nil {
		return err
	}
	if role != authz.SuperAdmin || status != service.StatusActive || generation != actor.SessionGeneration {
		return service.ErrAdminPermissionDenied
	}
	if expected == 0 {
		trace.Extra["recovered"] = true
	} else {
		trace.Extra["previous_permissions"] = current.Permissions
	}
	policy.LegacyAuditMaxID = current.LegacyAuditMaxID
	encoded, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	if _, err = client.Setting.Update().Where(setting.KeyEQ(authz.PolicyKey)).SetValue(string(encoded)).Save(ctx); err != nil {
		return err
	}
	if _, err = client.ExecContext(ctx, `INSERT INTO audit_logs (`+auditLogInsertColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, auditLogInsertValues(trace)...); err != nil {
		return err
	}
	return tx.Commit()
}

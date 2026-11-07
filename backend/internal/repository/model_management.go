package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// CompareAndSwapModelSettings guards concurrent editors, including replicas.
// The initial row and the read/check/write all belong to this transaction.
func (r *settingRepository) CompareAndSwapModelSettings(ctx context.Context, key string, expected int64, value string) error {
	if !strings.HasPrefix(key, service.ModelManagementKeyPrefix) || expected < 0 {
		return service.ErrModelPolicyConflict
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	if _, err = client.ExecContext(ctx, "INSERT INTO settings (key,value,updated_at) VALUES ($1,'{\"version\":0}',NOW()) ON CONFLICT (key) DO NOTHING", key); err != nil {
		return err
	}
	rows, err := client.QueryContext(ctx, "SELECT value FROM settings WHERE key=$1 FOR UPDATE", key)
	if err != nil {
		return err
	}
	var raw string
	if !rows.Next() {
		_ = rows.Close()
		return service.ErrModelPolicyUnavailable
	}
	err = rows.Scan(&raw)
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return err
	}
	var old, next struct {
		Version int64 `json:"version"`
	}
	if json.Unmarshal([]byte(raw), &old) != nil || json.Unmarshal([]byte(value), &next) != nil {
		return service.ErrModelPolicyUnavailable
	}
	if old.Version != expected || next.Version != expected+1 {
		return service.ErrModelPolicyConflict
	}
	result, err := client.ExecContext(ctx, "UPDATE settings SET value=$2,updated_at=NOW() WHERE key=$1", key, value)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return service.ErrModelPolicyUnavailable
	}
	return tx.Commit()
}

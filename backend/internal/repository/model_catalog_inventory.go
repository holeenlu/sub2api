package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Lock the shared settings row across server instances: editing one model must
// not replace another administrator's changes to a different model.
func (r *modelCatalogRepository) UpdateRegistry(ctx context.Context, update func([]service.ModelCatalogEntry) ([]service.ModelCatalogEntry, error)) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES($1,'[]',NOW()) ON CONFLICT(key) DO NOTHING`, service.ModelCatalogRegistryKey)
	if err != nil {
		return err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1 FOR UPDATE`, service.ModelCatalogRegistryKey).Scan(&raw); err != nil {
		return err
	}
	var entries []service.ModelCatalogEntry
	if err = json.Unmarshal([]byte(raw), &entries); err != nil {
		return err
	}
	entries, err = update(entries)
	if err != nil {
		return err
	}
	body, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE settings SET value=$2,updated_at=NOW() WHERE key=$1`, service.ModelCatalogRegistryKey, string(body)); err != nil {
		return err
	}
	return tx.Commit()
}

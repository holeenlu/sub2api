package repository

import (
	"context"
	"encoding/json"
	"time"
)

func (r *usageLogRepository) SaveUsagePricingAudit(ctx context.Context, keyID int64, requestID, revision string, payload json.RawMessage) error {
	_, err := r.sql.ExecContext(ctx, `INSERT INTO model_request_price_versions(api_key_id,request_id,price_revision,payload)
		VALUES($1,$2,$3,$4) ON CONFLICT(api_key_id,request_id) DO NOTHING`, keyID, requestID, revision, []byte(payload))
	return err
}

func (r *modelCatalogRepository) PricingAudits(ctx context.Context, pending bool) ([]map[string]any, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT api_key_id,request_id,price_revision,payload,created_at FROM model_request_price_versions WHERE NOT $1 OR payload->>'status'='pending' ORDER BY created_at DESC LIMIT 100`, pending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var key int64
		var request, revision string
		var raw []byte
		var at time.Time
		if err := rows.Scan(&key, &request, &revision, &raw, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"api_key_id": key, "request_id": request, "price_revision": revision, "payload": json.RawMessage(raw), "created_at": at})
	}
	return out, rows.Err()
}

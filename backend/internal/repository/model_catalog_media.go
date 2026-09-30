package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *modelCatalogRepository) ObserveMedia(ctx context.Context, id int64, scope string, o service.CatalogMediaObservation) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO model_catalog_observations(account_id,scope_revision,model_id,operation,driver_model,observed_at)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id,scope_revision,model_id,operation)
 DO UPDATE SET driver_model=EXCLUDED.driver_model,observed_at=EXCLUDED.observed_at`, id, scope, o.Model, o.Operation, o.DriverModel, o.ObservedAt)
	return err
}
func (r *modelCatalogRepository) MediaObservations(ctx context.Context, id int64, scope string) ([]service.CatalogMediaObservation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT model_id,operation,driver_model,observed_at FROM model_catalog_observations WHERE account_id=$1 AND scope_revision=$2 ORDER BY model_id,operation`, id, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.CatalogMediaObservation{}
	for rows.Next() {
		var o service.CatalogMediaObservation
		if err := rows.Scan(&o.Model, &o.Operation, &o.DriverModel, &o.ObservedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

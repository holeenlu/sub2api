package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type modelCatalogRepository struct{ db *sql.DB }

func NewModelCatalogRepository(db *sql.DB) service.ModelCatalogRepository {
	return &modelCatalogRepository{db: db}
}

func (r *modelCatalogRepository) Current(ctx context.Context, key string) (*service.ModelCatalogSnapshot, error) {
	var raw []byte
	var scope, lastError string
	var checked sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT s.payload,s.scope_revision,c.checked_at,c.last_error
		FROM model_catalog_sources c JOIN model_catalog_snapshots s
		ON s.source_key=c.source_key AND s.revision=c.current_revision AND s.scope_revision=c.scope_revision
		WHERE c.source_key=$1`, key).Scan(&raw, &scope, &checked, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out service.ModelCatalogSnapshot
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out.ScopeRevision = scope
	out.LastError = lastError
	if checked.Valid {
		out.CheckedAt = checked.Time
	}
	if lastError != "" {
		out.Status = "stale"
		if lastError == "authentication_unavailable" {
			out.Status = "unavailable"
		}
	}
	return &out, nil
}

func (r *modelCatalogRepository) Claim(ctx context.Context, key string, id int64, platform, scope, token string, until time.Time, force bool) (bool, error) {
	_, err := r.db.ExecContext(ctx, `INSERT INTO model_catalog_sources(source_key,account_id,platform,scope_revision)
		VALUES($1,$2,$3,$4) ON CONFLICT(source_key) DO NOTHING`, key, id, platform, scope)
	if err != nil {
		return false, err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE model_catalog_sources SET lease_token=$2,lease_until=$3,scope_revision=$4,
		current_revision=CASE WHEN scope_revision=$4 THEN current_revision ELSE NULL END
		WHERE source_key=$1 AND (lease_until IS NULL OR lease_until<NOW())
		AND ($5 OR next_sync_at<=NOW() OR scope_revision<>$4)`, key, token, until, scope, force)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (r *modelCatalogRepository) Publish(ctx context.Context, key, token string, snapshot *service.ModelCatalogSnapshot, next time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var previous sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT current_revision FROM model_catalog_sources
		WHERE source_key=$1 AND lease_token=$2 AND scope_revision=$3 FOR UPDATE`, key, token, snapshot.ScopeRevision).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrModelCatalogScopeChanged
	}
	if err != nil {
		return err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO model_catalog_snapshots(source_key,revision,scope_revision,observed_at,payload)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(source_key,revision) DO NOTHING`, key, snapshot.Revision, snapshot.ScopeRevision, snapshot.UpdatedAt, raw)
	if err != nil {
		return err
	}
	for _, entry := range snapshot.Models {
		encoded, e := json.Marshal(entry)
		if e != nil {
			return e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO model_catalog_entries(source_key,revision,model_id,payload)
			VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, key, snapshot.Revision, entry.ID, encoded)
		if err != nil {
			return err
		}
	}
	if !previous.Valid || previous.String != snapshot.Revision {
		_, err = tx.ExecContext(ctx, `INSERT INTO model_catalog_releases(source_key,revision,previous_revision,price_revision)
			VALUES($1,$2,$3,$4)`, key, snapshot.Revision, previous, snapshot.PriceRevision)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE model_catalog_sources SET current_revision=$2,checked_at=NOW(),next_sync_at=$3,
		failure_count=0,last_error='',lease_token=NULL,lease_until=NULL WHERE source_key=$1`, key, snapshot.Revision, next)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *modelCatalogRepository) Fail(ctx context.Context, key, token, code string, next time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE model_catalog_sources SET failure_count=failure_count+1,last_error=$3,
		next_sync_at=GREATEST($4, NOW() + (LEAST(3600,30*power(2,LEAST(failure_count,7))) * (0.8+random()*0.4)) * INTERVAL '1 second'),lease_token=NULL,lease_until=NULL WHERE source_key=$1 AND lease_token=$2`, key, token, code, next)
	return err
}

func (r *modelCatalogRepository) ListPlatform(ctx context.Context, platform string) ([]service.ModelCatalogSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT s.payload,s.scope_revision FROM model_catalog_sources c
		JOIN model_catalog_snapshots s ON s.source_key=c.source_key AND s.revision=c.current_revision AND s.scope_revision=c.scope_revision
		JOIN accounts a ON a.id=c.account_id AND a.deleted_at IS NULL AND a.status='active'
		WHERE ($1='' OR c.platform=$1) ORDER BY c.account_id`, platform)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.ModelCatalogSnapshot{}
	for rows.Next() {
		var raw []byte
		var scope string
		if err = rows.Scan(&raw, &scope); err != nil {
			return nil, err
		}
		var item service.ModelCatalogSnapshot
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		item.ScopeRevision = scope
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *modelCatalogRepository) History(ctx context.Context, key string, limit int) ([]service.ModelCatalogRelease, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,revision,COALESCE(previous_revision,''),price_revision,created_at,operation
		FROM model_catalog_releases WHERE source_key=$1 ORDER BY id DESC LIMIT $2`, key, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.ModelCatalogRelease{}
	for rows.Next() {
		var item service.ModelCatalogRelease
		if err = rows.Scan(&item.ID, &item.Revision, &item.PreviousRevision, &item.PriceRevision, &item.CreatedAt, &item.Operation); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *modelCatalogRepository) Rollback(ctx context.Context, key, scope, revision string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var previous sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT current_revision FROM model_catalog_sources WHERE source_key=$1 AND scope_revision=$2
		AND (lease_until IS NULL OR lease_until<NOW()) FOR UPDATE`, key, scope).Scan(&previous)
	if err != nil {
		return err
	}
	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM model_catalog_snapshots
		WHERE source_key=$1 AND scope_revision=$2 AND revision=$3)`, key, scope, revision).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("catalog revision not in current credential scope")
	}
	// Rollback cannot reintroduce models retired/revoked in the current revision.
	var expands bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM model_catalog_entries old
		WHERE old.source_key=$1 AND old.revision=$2 AND old.payload->>'access'='listed'
		AND NOT EXISTS(SELECT 1 FROM model_catalog_entries cur WHERE cur.source_key=$1 AND cur.revision=$3
		AND cur.model_id=old.model_id AND cur.payload->>'access'='listed' AND cur.payload->>'lifecycle'<>'retired'))`, key, revision, previous).Scan(&expands)
	if err != nil {
		return err
	}
	if expands {
		return fmt.Errorf("rollback would restore revoked or retired models")
	}
	_, err = tx.ExecContext(ctx, `UPDATE model_catalog_sources SET current_revision=$2,last_error='' WHERE source_key=$1`, key, revision)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO model_catalog_releases(source_key,revision,previous_revision,operation)
		VALUES($1,$2,$3,'rollback')`, key, revision, previous)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *modelCatalogRepository) SavePrices(ctx context.Context, revision string, payload json.RawMessage) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO official_price_versions(revision,payload) VALUES($1,$2) ON CONFLICT DO NOTHING`, revision, []byte(payload))
	return err
}

func (r *modelCatalogRepository) SaveJob(ctx context.Context, job service.ModelCatalogJob) error {
	_, _ = r.db.ExecContext(ctx, `DELETE FROM model_catalog_jobs WHERE updated_at<NOW()-INTERVAL '1 day'`)
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO model_catalog_jobs(id,account_id,payload) VALUES($1,$2,$3)
 ON CONFLICT(id) DO UPDATE SET payload=EXCLUDED.payload,updated_at=NOW()`, job.ID, job.AccountID, body)
	return err
}
func (r *modelCatalogRepository) ReadJob(ctx context.Context, id string) (*service.ModelCatalogJob, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM model_catalog_jobs WHERE id=$1 AND updated_at>NOW()-INTERVAL '1 day'`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var job service.ModelCatalogJob
	if err = json.Unmarshal(raw, &job); err != nil {
		return nil, err
	}
	if job.Status == "running" && time.Since(job.StartedAt) > 3*time.Minute {
		job.Status = "failed"
		job.Error = "refresh_interrupted"
	}
	return &job, nil
}

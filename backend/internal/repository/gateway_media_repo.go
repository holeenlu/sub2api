package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type gatewayMediaRepository struct{ db *sql.DB }

func NewGatewayMediaRepository(db *sql.DB) service.GatewayMediaRepository {
	return &gatewayMediaRepository{db: db}
}
func (r *gatewayMediaRepository) PutVoice(ctx context.Context, v *service.GatewayMediaVoice) error {
	res, err := r.db.ExecContext(ctx, `INSERT INTO gateway_media_voices(account_id,voice_id,user_id,group_id,metadata) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(account_id,voice_id) DO UPDATE SET metadata=EXCLUDED.metadata
 WHERE gateway_media_voices.user_id=EXCLUDED.user_id AND gateway_media_voices.group_id=EXCLUDED.group_id`, v.AccountID, v.ID, v.UserID, v.GroupID, []byte(v.Metadata))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return service.ErrMediaNotOwned
	}
	return err
}
func (r *gatewayMediaRepository) GetVoice(ctx context.Context, g, u int64, id string) (*service.GatewayMediaVoice, error) {
	v := &service.GatewayMediaVoice{ID: id, UserID: u, GroupID: g}
	err := r.db.QueryRowContext(ctx, `SELECT account_id,metadata FROM gateway_media_voices WHERE group_id=$1 AND user_id=$2 AND voice_id=$3`, g, u, id).Scan(&v.AccountID, &v.Metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrMediaNotOwned
	}
	return v, err
}
func (r *gatewayMediaRepository) ListVoices(ctx context.Context, g, u int64) ([]service.GatewayMediaVoice, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT voice_id,account_id,metadata FROM gateway_media_voices WHERE group_id=$1 AND user_id=$2 ORDER BY created_at,voice_id`, g, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.GatewayMediaVoice{}
	for rows.Next() {
		v := service.GatewayMediaVoice{GroupID: g, UserID: u}
		if err := rows.Scan(&v.ID, &v.AccountID, &v.Metadata); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *gatewayMediaRepository) DeleteVoice(ctx context.Context, g, u int64, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM gateway_media_voices WHERE group_id=$1 AND user_id=$2 AND voice_id=$3`, g, u, id)
	return err
}
func (r *gatewayMediaRepository) CreateJob(ctx context.Context, j *service.GatewayMediaJob) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO gateway_media_jobs(id,user_id,group_id,account_id,state,snapshot,next_attempt_at) VALUES($1,$2,$3,$4,'reserving',$5,now()+interval '2 minutes')`, j.ID, j.UserID, j.GroupID, j.AccountID, raw)
	var pg *pq.Error
	if errors.As(err, &pg) && pg.Code == "23505" {
		return service.ErrMediaPendingCapacity
	}
	return err
}
func (r *gatewayMediaRepository) SaveJob(ctx context.Context, j *service.GatewayMediaJob) error {
	next := *j
	next.Version++
	raw, err := json.Marshal(&next)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `UPDATE gateway_media_jobs SET task_id=NULLIF($2,''),state=$3,snapshot=$4,version=version+1,next_attempt_at=now()+CASE WHEN $3='pending' THEN interval '15 seconds' ELSE interval '2 minutes' END,updated_at=now()
 WHERE id=$1 AND version=$5 AND state NOT IN ('settled','failed')`, j.ID, j.TaskID, j.State, raw, j.Version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("media job changed, is already terminal or missing")
	}
	if err == nil {
		j.Version = next.Version
	}
	return err
}
func (r *gatewayMediaRepository) GetJob(ctx context.Context, g, u int64, id string) (*service.GatewayMediaJob, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT snapshot FROM gateway_media_jobs WHERE group_id=$1 AND user_id=$2 AND task_id=$3`, g, u, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrMediaNotOwned
	}
	if err != nil {
		return nil, err
	}
	j := &service.GatewayMediaJob{}
	err = json.Unmarshal(raw, j)
	return j, err
}
func (r *gatewayMediaRepository) ClaimJobs(ctx context.Context, n int) ([]service.GatewayMediaJob, error) {
	rows, err := r.db.QueryContext(ctx, `UPDATE gateway_media_jobs SET next_attempt_at=now()+interval '2 minutes',version=version+1,snapshot=jsonb_set(snapshot,'{Version}',to_jsonb(version+1))
 WHERE id IN (SELECT id FROM gateway_media_jobs WHERE state IN ('reserving','pending','billing','releasing') AND next_attempt_at<=now() ORDER BY next_attempt_at LIMIT $1 FOR UPDATE SKIP LOCKED) RETURNING snapshot`, n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.GatewayMediaJob{}
	for rows.Next() {
		var raw []byte
		var j service.GatewayMediaJob
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

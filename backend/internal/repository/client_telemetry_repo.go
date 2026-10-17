package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const insertClientTelemetrySQL = `INSERT INTO ops_client_telemetry_logs
 (user_id, api_key_id, group_id, client, client_key, payload_hash,
  request_path, event_type, session_id, device_id, payload_json, headers)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
 ON CONFLICT DO NOTHING`

// The unique indexes arbitrate concurrent uploads across all gateway instances.
// A second statement gets a fresh READ COMMITTED snapshot after a conflicting
// insert commits; a single insert/CTE/select can miss that winning row.
const touchClientTelemetrySQL = `UPDATE ops_client_telemetry_logs
 SET received_count = received_count + 1, last_seen_at = GREATEST(last_seen_at, clock_timestamp())
 WHERE id = (SELECT id FROM ops_client_telemetry_logs
  WHERE user_id = $1 AND client = $2
   AND (payload_hash = $3 OR ($4 <> '' AND client_key = $4))
  ORDER BY id LIMIT 1)`

func (r *opsRepository) CaptureClientTelemetry(ctx context.Context, samples []*service.ClientTelemetryLog) error {
	if r == nil || r.db == nil {
		return service.ErrClientTelemetryStorage
	}
	if len(samples) == 0 || len(samples) > 200 {
		return service.ErrInvalidClientTelemetry
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize each user's batches before unique-index locks, including batches
	// listing devices in opposite order. Other users remain independent.
	if samples[0] == nil || samples[0].UserID <= 0 {
		return service.ErrInvalidClientTelemetry
	}
	userID := samples[0].UserID
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('client-telemetry:' || $1::text, 0))`, userID); err != nil {
		return err
	}
	for _, row := range samples {
		if row == nil || row.UserID != userID || row.APIKeyID <= 0 {
			return service.ErrInvalidClientTelemetry
		}
		headers, err := json.Marshal(row.Headers)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, insertClientTelemetrySQL,
			row.UserID, row.APIKeyID, row.GroupID, row.Client, row.ClientKey, row.PayloadHash,
			row.RequestPath, row.EventType, row.SessionID, row.DeviceID, row.PayloadJSON, string(headers))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			result, err = tx.ExecContext(ctx, touchClientTelemetrySQL, row.UserID, row.Client, row.PayloadHash, row.ClientKey)
			if err != nil {
				return err
			}
			count, err = result.RowsAffected()
			if err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("telemetry sample expired during capture")
			}
		}
	}
	return tx.Commit()
}

const clientTelemetryColumns = `id, user_id, api_key_id, group_id,
 client, request_path, event_type, session_id, device_id, payload_hash,
 received_count, created_at, last_seen_at`

func scanClientTelemetry(scanner interface{ Scan(...any) error }, detail bool) (*service.ClientTelemetryLog, error) {
	row := &service.ClientTelemetryLog{}
	dest := []any{&row.ID, &row.UserID, &row.APIKeyID, &row.GroupID,
		&row.Client, &row.RequestPath, &row.EventType, &row.SessionID, &row.DeviceID, &row.PayloadHash,
		&row.ReceivedCount, &row.CreatedAt, &row.LastSeenAt}
	var headers []byte
	if detail {
		dest = append(dest, &row.PayloadJSON, &headers)
	}
	if err := scanner.Scan(dest...); err != nil {
		return nil, err
	}
	if detail {
		if err := json.Unmarshal(headers, &row.Headers); err != nil {
			return nil, err
		}
	}
	return row, nil
}

func clientTelemetryWhere(filter *service.ClientTelemetryLogFilter) (string, []any) {
	clauses := []string{"1=1"}
	args := []any{}
	add := func(column, operator string, value any) {
		args = append(args, value)
		clauses = append(clauses, column+operator+fmt.Sprintf("$%d", len(args)))
	}
	if filter.StartTime != nil {
		add("last_seen_at", ">=", *filter.StartTime)
	}
	if filter.EndTime != nil {
		add("last_seen_at", "<", *filter.EndTime)
	}
	if filter.UserID != nil {
		add("user_id", "=", *filter.UserID)
	}
	if filter.APIKeyID != nil {
		add("api_key_id", "=", *filter.APIKeyID)
	}
	if filter.SessionID != "" {
		add("session_id", "=", filter.SessionID)
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (r *opsRepository) ListClientTelemetry(ctx context.Context, filter *service.ClientTelemetryLogFilter) (*service.ClientTelemetryLogList, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrClientTelemetryStorage
	}
	if filter == nil {
		filter = &service.ClientTelemetryLogFilter{}
	}
	page, size := max(1, filter.Page), filter.PageSize
	if size <= 0 {
		size = 20
	}
	size = min(size, 100)
	page = min(page, 100000)
	where, args := clientTelemetryWhere(filter)
	out := &service.ClientTelemetryLogList{Items: []*service.ClientTelemetryLog{}, Page: page, PageSize: size}
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ops_client_telemetry_logs"+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	query := "SELECT " + clientTelemetryColumns + " FROM ops_client_telemetry_logs" + where +
		fmt.Sprintf(" ORDER BY last_seen_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	rows, err := r.db.QueryContext(ctx, query, append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		row, err := scanClientTelemetry(rows, false)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, row)
	}
	return out, rows.Err()
}

func (r *opsRepository) GetClientTelemetry(ctx context.Context, id int64) (*service.ClientTelemetryLog, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrClientTelemetryStorage
	}
	return scanClientTelemetry(r.db.QueryRowContext(ctx, "SELECT "+clientTelemetryColumns+
		", payload_json, headers FROM ops_client_telemetry_logs WHERE id=$1", id), true)
}

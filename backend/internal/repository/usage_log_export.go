package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// One bounded read-only snapshot prevents concurrent inserts (including
// backdated ones) from changing the export while each batch uses keyset paging.
func (r *usageLogRepository) StreamUsageLogs(ctx context.Context, filters UsageLogFilters, maxRows int, emit func([]service.UsageLog) error) error {
	if r.db == nil {
		return fmt.Errorf("usage export requires a database connection")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SET LOCAL statement_timeout = '10s'"); err != nil {
		return err
	}
	cutoff := time.Now()
	if filters.EndTime == nil || filters.EndTime.After(cutoff) {
		filters.EndTime = &cutoff
	}
	conditions, args := usageLogFilterConditions(filters)
	var cursorTime time.Time
	var cursorID int64
	emitted := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		where := append([]string(nil), conditions...)
		params := append([]any(nil), args...)
		if cursorID > 0 {
			where = append(where, fmt.Sprintf("(created_at,id) < ($%d,$%d)", len(params)+1, len(params)+2))
			params = append(params, cursorTime, cursorID)
		}
		// Fetch one extra row at the limit; never silently label a partial export complete.
		batchSize := 500
		if remaining := maxRows - emitted + 1; remaining < batchSize {
			batchSize = remaining
		}
		params = append(params, batchSize)
		query := fmt.Sprintf("SELECT %s FROM usage_logs %s ORDER BY created_at DESC,id DESC LIMIT $%d", usageLogSelectColumns, buildWhere(where), len(params))
		batchRepo := &usageLogRepository{sql: tx}
		logs, err := batchRepo.queryUsageLogs(ctx, query, params...)
		if err != nil {
			return err
		}
		if len(logs) == 0 {
			break
		}
		if emitted+len(logs) > maxRows {
			return service.ErrUsageExportTooLarge
		}
		if err = r.hydrateUsageLogAssociations(ctx, logs); err != nil {
			return err
		}
		if err = emit(logs); err != nil {
			return err
		}
		emitted += len(logs)
		last := logs[len(logs)-1]
		cursorTime, cursorID = last.CreatedAt, last.ID
		if len(logs) < batchSize {
			break
		}
	}
	return tx.Commit()
}

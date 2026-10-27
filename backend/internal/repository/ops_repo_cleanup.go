package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CleanupOpsTable 只执行既有 Ops retention 表；保留策略和轮转批次由服务决定。
func (r *opsRepository) CleanupOpsTable(ctx context.Context, table string, cutoff time.Time, truncate bool, batchSize int) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var timeColumn string
	var castDate bool
	switch table {
	case "ops_error_logs", "ops_alert_events", "ops_system_logs", "ops_system_log_cleanup_audits", "ops_system_metrics":
		timeColumn = "created_at"
	case "ops_ingress_reject_aggregates", "ops_metrics_hourly":
		timeColumn = "bucket_start"
	case "ops_metrics_daily":
		timeColumn, castDate = "bucket_date", true
	default:
		return 0, fmt.Errorf("unsupported Ops cleanup table")
	}
	if truncate {
		return r.truncateOpsTable(ctx, table)
	}
	if batchSize <= 0 {
		return 0, fmt.Errorf("cleanup batch size must be positive")
	}
	where := fmt.Sprintf("%s < $1", timeColumn)
	if castDate {
		where = fmt.Sprintf("%s < $1::date", timeColumn)
	}
	q := fmt.Sprintf(`
WITH batch AS (
  SELECT id FROM %s
  WHERE %s
  ORDER BY id
  LIMIT $2
)
DELETE FROM %s
WHERE id IN (SELECT id FROM batch)
`, table, where, table)
	res, err := r.execOpsQuery(ctx, q, cutoff, batchSize)
	if err != nil {
		if isMissingRelationError(err) {
			return 0, nil
		}
		return 0, err
	}
	return res.RowsAffected()
}

// 清空前的行数仍用于心跳；COUNT 和 TRUNCATE 各自完整归还连接后才推进下一条 SQL。
func (r *opsRepository) truncateOpsTable(ctx context.Context, table string) (int64, error) {
	var count int64
	if err := r.queryOpsRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count); err != nil {
		if isMissingRelationError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("count %s: %w", table, err)
	}
	if count == 0 {
		return 0, nil
	}
	if _, err := r.execOpsQuery(ctx, fmt.Sprintf("TRUNCATE TABLE %s", table)); err != nil {
		if isMissingRelationError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("truncate %s: %w", table, err)
	}
	return count, nil
}

func isMissingRelationError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "does not exist") && strings.Contains(s, "relation")
}

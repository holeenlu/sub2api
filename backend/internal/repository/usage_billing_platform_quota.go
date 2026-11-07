package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"time"
)

func incrementUsageBillingPlatformQuota(ctx context.Context, tx *sql.Tx, userID int64, platform string, cost float64) error {
	var id int64
	var daily, weekly, monthly float64
	var day, week, month *time.Time
	err := tx.QueryRowContext(ctx, `SELECT id,daily_usage_usd,weekly_usage_usd,monthly_usage_usd,daily_window_start,weekly_window_start,monthly_window_start
        FROM user_platform_quotas WHERE user_id=$1 AND platform=$2 AND deleted_at IS NULL FOR UPDATE`, userID, platform).Scan(&id, &daily, &weekly, &monthly, &day, &week, &month)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now()
	dayStart, weekStart := timezone.StartOfDay(now), timezone.StartOfWeek(now)
	daily, weekly = maybeReset(daily, day, dayStart, cost), maybeReset(weekly, week, weekStart, cost)
	monthly, monthStart := monthlyMaybeReset(monthly, month, cost, now)
	_, err = tx.ExecContext(ctx, `UPDATE user_platform_quotas SET daily_usage_usd=$2,weekly_usage_usd=$3,monthly_usage_usd=$4,
        daily_window_start=$5,weekly_window_start=$6,monthly_window_start=$7,updated_at=$8 WHERE id=$1`, id, daily, weekly, monthly, dayStart, weekStart, monthStart, now)
	return err
}

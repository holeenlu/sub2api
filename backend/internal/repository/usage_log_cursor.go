package repository

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"strings"
	"time"
)

var errUsageCursorRequired = infraerrors.BadRequest("USAGE_CURSOR_REQUIRED", "Use cursor pagination beyond 10000 skipped rows")
var errUsageCursorInvalid = infraerrors.BadRequest("USAGE_CURSOR_INVALID", "Invalid usage cursor or changed filters/sort order")

type usageLogCursor struct {
	ID     int64
	Time   time.Time
	Model  string
	Filter string
	Seen   int
}

func usageCursorFilter(params pagination.PaginationParams, filters UsageLogFilters) string {
	filters.ExactTotal = false
	data, _ := json.Marshal(struct {
		Filters UsageLogFilters
		Sort    string
	}{filters, usageLogOrderBy(params)})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func decodeUsageCursor(raw, filter string) (usageLogCursor, error) {
	var cursor usageLogCursor
	if raw == "start" {
		return cursor, nil
	}
	if len(raw) > 4096 {
		return cursor, errUsageCursorInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.ID <= 0 || cursor.Seen < 1 || cursor.Seen > 1000000000 || cursor.Filter != filter {
		return cursor, errUsageCursorInvalid
	}
	return cursor, nil
}
func (r *usageLogRepository) listUsageLogsWithCursor(ctx context.Context, conditions []string, args []any, params pagination.PaginationParams, filters UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	filter := usageCursorFilter(params, filters)
	cursor, err := decodeUsageCursor(params.Cursor, filter)
	if err != nil {
		return nil, nil, err
	}
	var exact int64
	if filters.ExactTotal {
		if err := scanSingleRow(ctx, r.sql, "SELECT COUNT(*) FROM usage_logs "+buildWhere(conditions), args, &exact); err != nil {
			return nil, nil, err
		}
	}
	conditions = append([]string(nil), conditions...)
	args = append([]any(nil), args...)
	op := "<"
	if params.NormalizedSortOrder(pagination.SortOrderDesc) == pagination.SortOrderAsc {
		op = ">"
	}
	if cursor.ID > 0 {
		n := len(args) + 1
		switch strings.ToLower(strings.TrimSpace(params.SortBy)) {
		case "created_at":
			if cursor.Time.IsZero() {
				return nil, nil, errUsageCursorInvalid
			}
			conditions = append(conditions, fmt.Sprintf("(created_at,id) %s ($%d,$%d)", op, n, n+1))
			args = append(args, cursor.Time, cursor.ID)
		case "model":
			conditions = append(conditions, fmt.Sprintf("(COALESCE(NULLIF(TRIM(requested_model), ''), model),id) %s ($%d,$%d)", op, n, n+1))
			args = append(args, cursor.Model, cursor.ID)
		default:
			conditions = append(conditions, fmt.Sprintf("id %s $%d", op, n))
			args = append(args, cursor.ID)
		}
	}
	query := fmt.Sprintf("SELECT %s FROM usage_logs %s ORDER BY %s LIMIT $%d", usageLogSelectColumns, buildWhere(conditions), usageLogOrderBy(params), len(args)+1)
	logs, err := r.queryUsageLogs(ctx, query, append(args, params.Limit()+1)...)
	if err != nil {
		return nil, nil, err
	}
	more := len(logs) > params.Limit()
	if more {
		logs = logs[:params.Limit()]
	}
	total := int64(cursor.Seen + len(logs))
	if more {
		total++
	}
	if filters.ExactTotal {
		total = exact
	}
	page := paginationResultFromTotal(total, params)
	page.HasMore = more
	page.TotalExact = filters.ExactTotal
	if more {
		last := logs[len(logs)-1]
		model := strings.TrimSpace(last.RequestedModel)
		if model == "" {
			model = last.Model
		}
		data, _ := json.Marshal(usageLogCursor{ID: last.ID, Time: last.CreatedAt, Model: model, Filter: filter, Seen: cursor.Seen + len(logs)})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return logs, page, nil
}

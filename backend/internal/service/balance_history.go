package service

import (
	"context"
	"encoding/json"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// BalanceHistoryFilter applies before pagination. Account ownership is supplied
// separately by the authenticated handler, never by a query parameter.
type BalanceHistoryFilter struct {
	Category  string `form:"category"`
	Direction string `form:"direction"`
	Source    string `form:"source"`
}

func (f BalanceHistoryFilter) Validate() error {
	if f.Category != "balance" && f.Category != "other" {
		return infraerrors.BadRequest("INVALID_HISTORY_FILTER", "invalid history category")
	}
	if f.Direction != "" && f.Direction != "increase" && f.Direction != "decrease" {
		return infraerrors.BadRequest("INVALID_HISTORY_FILTER", "invalid history direction")
	}
	switch f.Source {
	case "", "admin", "payment", "redeem", "affiliate":
		return nil
	default:
		return infraerrors.BadRequest("INVALID_HISTORY_FILTER", "invalid history source")
	}
}

// This public projection intentionally omits redemption codes, internal user
// metadata and management-only fields from the existing ledger.
type BalanceHistoryItem struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Value      float64   `json:"value"`
	Source     string    `json:"source"`
	Notes      string    `json:"notes"`
	OccurredAt time.Time `json:"occurred_at"`
}

type BalanceHistoryPage struct {
	Items          []BalanceHistoryItem `json:"items"`
	Total          int64                `json:"total"`
	TotalRecharged float64              `json:"total_recharged"`
}

// GetBalanceHistory reads the existing credit and affiliate ledgers in one
// snapshot. Unlike management history, this projection supports category and
// direction filters and does not expose credentials or management metadata.
func (s *RedeemService) GetBalanceHistory(ctx context.Context, userID int64, page, pageSize int, filter BalanceHistoryFilter) (*BalanceHistoryPage, error) {
	if userID <= 0 {
		return nil, ErrInsufficientPerms
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	rows, err := s.entClient.QueryContext(ctx, `
WITH history AS (
 SELECT 'credit-' || r.id AS id, r.id AS sequence, r.type,
        CASE WHEN r.type = 'subscription' THEN r.validity_days ELSE r.value END AS value,
        CASE WHEN r.type IN ('admin_balance', 'admin_concurrency') THEN 'admin'
             WHEN EXISTS (SELECT 1 FROM payment_orders p WHERE p.user_id = $1 AND p.recharge_code = r.code) THEN 'payment'
             ELSE 'redeem' END AS source,
        COALESCE(r.notes, '') AS notes, COALESCE(r.used_at, r.created_at) AS occurred_at
 FROM redeem_codes r
 WHERE r.used_by = $1 AND r.status = 'used'
   AND r.type IN ('balance', 'admin_balance', 'concurrency', 'admin_concurrency', 'subscription')
 UNION ALL
 SELECT 'affiliate-' || id, id, 'affiliate_balance', amount, 'affiliate', '', created_at
 FROM user_affiliate_ledger WHERE user_id = $1 AND action = 'transfer'
), filtered AS (
 SELECT * FROM history
 WHERE (($2 = 'balance' AND type IN ('balance', 'admin_balance', 'affiliate_balance'))
     OR ($2 = 'other' AND type IN ('concurrency', 'admin_concurrency', 'subscription')))
   AND ($3 = '' OR ($3 = 'increase' AND value > 0) OR ($3 = 'decrease' AND value < 0))
   AND ($4 = '' OR source = $4)
)
SELECT (SELECT COUNT(*) FROM filtered),
       COALESCE((SELECT SUM(value) FROM history WHERE type IN ('balance', 'admin_balance') AND value > 0), 0),
       COALESCE((SELECT jsonb_agg(entry ORDER BY occurred_at DESC, sequence DESC, id DESC)
                 FROM (SELECT id, type, value, source, notes, occurred_at, sequence FROM filtered
                       ORDER BY occurred_at DESC, sequence DESC, id DESC LIMIT $5 OFFSET $6) entry), '[]'::jsonb)
`, userID, filter.Category, filter.Direction, filter.Source, params.Limit(), params.Offset())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := &BalanceHistoryPage{Items: []BalanceHistoryItem{}}
	if rows.Next() {
		var raw []byte
		if err := rows.Scan(&result.Total, &result.TotalRecharged, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &result.Items); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

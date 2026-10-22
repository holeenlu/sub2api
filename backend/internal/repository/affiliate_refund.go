package repository

import (
	"context"
	"fmt"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"math"
)

func lockAffiliateProfile(ctx context.Context, client *dbent.Client, userID int64) error {
	rows, err := client.QueryContext(ctx, `SELECT user_id FROM user_affiliates WHERE user_id=$1 FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("affiliate profile missing")
	}
	return rows.Err()
}

// ReverseOrderRebate recovers a cumulative proportional refund. Original accruals
// stay immutable for the gross lifetime cap; reversed_amount makes recovery and
// thaw idempotent. Spendable quota is recovered first; already transferred or
// withdrawn rewards become balance debt (including an existing negative balance).
// Future top-ups repay that debt before enabling further usage.
func (r *affiliateRepository) ReverseOrderRebate(ctx context.Context, orderID int64, refunded, original float64) error {
	if original <= 0 || refunded < 0 || math.IsNaN(refunded) || math.IsInf(refunded, 0) {
		return fmt.Errorf("invalid rebate reversal amount")
	}
	return r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		rows, err := client.QueryContext(txCtx, `SELECT DISTINCT user_id FROM user_affiliate_ledger WHERE source_order_id=$1 AND action='accrue' ORDER BY user_id`, orderID)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := lockAffiliateProfile(txCtx, client, id); err != nil {
				return err
			}
			// All ledger writers lock profile before touching accrual rows.
			rows, err := client.QueryContext(txCtx, `SELECT id, amount, reversed_amount, frozen_until IS NOT NULL FROM user_affiliate_ledger WHERE source_order_id=$1 AND action='accrue' AND user_id=$2 FOR UPDATE`, orderID, id)
			if err != nil {
				return err
			}
			type entry struct {
				id               int64
				amount, reversed float64
				frozen           bool
			}
			var entries []entry
			for rows.Next() {
				var e entry
				if err := rows.Scan(&e.id, &e.amount, &e.reversed, &e.frozen); err != nil {
					_ = rows.Close()
					return err
				}
				entries = append(entries, e)
			}
			err = rows.Err()
			_ = rows.Close()
			if err != nil {
				return err
			}
			for _, e := range entries {
				target := math.Round(e.amount*math.Min(refunded/original, 1)*1e8) / 1e8
				delta := target - e.reversed
				if delta <= 0 {
					continue
				}
				if e.frozen {
					_, err = client.ExecContext(txCtx, `UPDATE user_affiliates SET aff_frozen_quota=aff_frozen_quota-$1, updated_at=NOW() WHERE user_id=$2`, delta, id)
				} else {
					// Profile lock prevents transfer/withdraw/thaw from racing this split.
					rows, queryErr := client.QueryContext(txCtx, `SELECT LEAST(GREATEST(aff_quota,0),$1::numeric) FROM user_affiliates WHERE user_id=$2`, delta, id)
					if queryErr != nil {
						return queryErr
					}
					var quota float64
					if !rows.Next() {
						_ = rows.Close()
						return fmt.Errorf("affiliate profile missing")
					}
					err = rows.Scan(&quota)
					_ = rows.Close()
					if err != nil {
						return err
					}
					_, err = client.ExecContext(txCtx, `UPDATE user_affiliates SET aff_quota=aff_quota-$1, updated_at=NOW() WHERE user_id=$2`, quota, id)
					if err == nil && delta > quota {
						_, err = client.ExecContext(txCtx, `UPDATE users SET balance=balance-$1, updated_at=NOW() WHERE id=$2`, delta-quota, id)
					}
				}
				if err != nil {
					return err
				}
				_, err = client.ExecContext(txCtx, `UPDATE user_affiliate_ledger SET reversed_amount=$1, updated_at=NOW() WHERE id=$2`, target, e.id)
				if err != nil {
					return err
				}
				_, err = client.ExecContext(txCtx, `INSERT INTO user_affiliate_ledger(user_id,action,amount,source_order_id,created_at,updated_at) VALUES($1,'refund_reversal',-$2::numeric,$3,NOW(),NOW())`, id, delta, orderID)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const paymentAllowanceGrace = 5 * time.Minute
const paymentAllowanceStateKey = "daily_allowance_state"
const paymentAllowanceDayKey = "daily_allowance_day"

func paymentAllowanceAmount(o *dbent.PaymentOrder) float64 {
	if amount, ok := o.ProviderSnapshot["daily_allowance_amount"].(float64); ok && isValidProviderAmount(amount) {
		return amount
	}
	if o.OrderType == payment.OrderTypeBalance {
		return o.PayAmount
	}
	return o.Amount
}

func paymentAllowanceCounted(o *dbent.PaymentOrder, now time.Time) bool {
	day := psStartOfDayUTC(now)
	state, _ := o.ProviderSnapshot[paymentAllowanceStateKey].(string)
	if state == "deferred" {
		return false
	}
	if state == "admitted" {
		// A captured fulfillment reservation survives a crash/day boundary until
		// entitlement accounting completes. Expiry can only release unpaid holds.
		if o.Status == OrderStatusPaid || o.Status == OrderStatusRecharging || o.Status == OrderStatusFailed {
			return true
		}
		bookedDay, _ := o.ProviderSnapshot[paymentAllowanceDayKey].(string)
		return bookedDay == day.Format("2006-01-02") || (o.CompletedAt != nil && !o.CompletedAt.Before(day))
	}
	if o.Status == OrderStatusCompleted || psIsRefundStatus(o.Status) {
		return (o.CompletedAt != nil && !o.CompletedAt.Before(day)) || (o.PaidAt != nil && !o.PaidAt.Before(day))
	}
	// Cancelled, expired and create-failed checkouts can still pay within grace.
	// After grace their money is accepted, but fulfillment must acquire a new hold.
	return o.ExpiresAt.Add(paymentAllowanceGrace).After(now)
}

// Caller must hold the stable user row lock; creation and fulfillment share it.
func (s *PaymentService) checkDailyAllowance(ctx context.Context, tx *dbent.Tx, userID, excludeOrderID int64, amount, limit float64, now time.Time) error {
	if limit <= 0 {
		return nil
	}
	orders, err := tx.PaymentOrder.Query().Where(paymentorder.UserIDEQ(userID), paymentorder.IDNEQ(excludeOrderID), paymentorder.Or(
		paymentorder.PaidAtGTE(psStartOfDayUTC(now)), paymentorder.CompletedAtGTE(psStartOfDayUTC(now)),
		paymentorder.ExpiresAtGT(now.Add(-paymentAllowanceGrace)),
		paymentorder.StatusIn(OrderStatusPaid, OrderStatusRecharging, OrderStatusFailed),
	)).All(ctx)
	if err != nil {
		return fmt.Errorf("query daily allowance: %w", err)
	}
	used, unconfirmed := decimal.Zero, decimal.Zero
	for _, o := range orders {
		if !paymentAllowanceCounted(o, now) {
			continue
		}
		reserved := paymentAllowanceAmount(o)
		if !isValidProviderAmount(reserved) {
			return fmt.Errorf("invalid allowance amount for order %d", o.ID)
		}
		value := decimal.NewFromFloat(reserved)
		used = used.Add(value)
		if o.PaidAt == nil {
			unconfirmed = unconfirmed.Add(value)
		}
	}
	if used.Add(decimal.NewFromFloat(amount)).GreaterThan(decimal.NewFromFloat(limit)) {
		usedFloat, _ := used.Float64()
		return infraerrors.TooManyRequests("DAILY_LIMIT_EXCEEDED", "daily_limit_exceeded").WithMetadata(map[string]string{
			"remaining": fmt.Sprintf("%.2f", math.Max(0, limit-usedFloat)), "unconfirmed_reserved": unconfirmed.StringFixed(2),
		})
	}
	return nil
}

func (s *PaymentService) fulfillmentDailyLimit(ctx context.Context, o *dbent.PaymentOrder) (float64, error) {
	if s.configService != nil {
		cfg, err := s.configService.GetPaymentConfig(ctx)
		if err != nil {
			return 0, err
		}
		return cfg.DailyLimit, nil
	}
	// Preserve the configured admission policy in explicitly constructed services.
	limit, _ := o.ProviderSnapshot["daily_allowance_limit"].(float64)
	return limit, nil
}

// Retry paid-but-uncredited orders without another provider call. A deferred
// attempt stays PAID and rotates updated_at so one blocked user cannot starve
// the bounded batch. Both direct fulfillment entry points enforce admission.
func (s *PaymentService) reconcileDeferredPayments(ctx context.Context) (int, error) {
	orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.StatusEQ(OrderStatusPaid)).
		Order(dbent.Asc(paymentorder.FieldUpdatedAt)).Limit(pendingPaymentReconcileLimit).All(ctx)
	if err != nil {
		return 0, err
	}
	completed := 0
	for _, o := range orders {
		if ctx.Err() != nil {
			return completed, ctx.Err()
		}
		if err := s.executeFulfillment(ctx, o.ID); err != nil {
			slog.Warn("retry paid order fulfillment", "orderID", o.ID, "error", err)
			_, updateErr := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(o.ID), paymentorder.StatusEQ(OrderStatusPaid), paymentorder.UpdatedAtEQ(o.UpdatedAt)).SetUpdatedAt(time.Now()).Save(ctx)
			if updateErr != nil {
				return completed, updateErr
			}
			continue
		}
		current, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
		if err != nil {
			return completed, err
		}
		if current.Status == OrderStatusCompleted {
			completed++
		}
	}
	return completed, nil
}

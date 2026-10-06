package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// REFUND_RESERVATION is the durable refund job. Its claimed order, captured
// entitlement and provider reference commit together. Neither a lost request nor
// a pending/unknown provider outcome releases collateral. The existing unique
// (order_id, action) index permits exactly one current reservation per order.
type refundReservation struct {
	Plan     RefundPlan `json:"plan"`
	State    string     `json:"state"` // held, consumed, released
	RefundID string     `json:"refund_id"`
	Outcome  string     `json:"outcome,omitempty"`
}

func loadRefundReservation(ctx context.Context, client *dbent.Client, order *dbent.PaymentOrder) (*refundReservation, error) {
	entry, err := client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("REFUND_RESERVATION")).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("refund reservation missing; reconcile legacy refund before retry: %w", err)
	}
	var record refundReservation
	if err := json.Unmarshal([]byte(entry.Detail), &record); err != nil {
		return nil, fmt.Errorf("read refund reservation: %w", err)
	}
	if record.Plan.OrderID != order.ID || record.State != "held" {
		return nil, infraerrors.Conflict("CONFLICT", "refund reservation is no longer held")
	}
	record.Plan.Order = order
	return &record, nil
}

func saveRefundReservation(ctx context.Context, client *dbent.Client, record *refundReservation) error {
	copy := *record
	copy.Plan.Order = nil // Never duplicate provider snapshots or user PII in job data.
	body, err := json.Marshal(copy)
	if err != nil {
		return err
	}
	oid := strconv.FormatInt(record.Plan.OrderID, 10)
	entry, err := client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(oid), paymentauditlog.ActionEQ("REFUND_RESERVATION")).Only(ctx)
	if dbent.IsNotFound(err) {
		_, err = client.PaymentAuditLog.Create().SetOrderID(oid).SetAction("REFUND_RESERVATION").SetDetail(string(body)).SetOperator("system").Save(ctx)
	} else if err == nil {
		_, err = client.PaymentAuditLog.UpdateOneID(entry.ID).SetDetail(string(body)).Save(ctx)
	}
	return err
}

func (s *PaymentService) reserveRefund(ctx context.Context, p *RefundPlan) error {
	if p == nil || p.Order == nil || p.OrderID != p.Order.ID || !isValidProviderAmount(p.RefundAmount) || p.RefundAmount > p.Order.Amount {
		return infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund plan")
	}
	if p.Order.OrderType != payment.OrderTypeBalance && p.Order.OrderType != payment.OrderTypeSubscription {
		return infraerrors.BadRequest("INVALID_ORDER_TYPE", "unknown order type")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	n, err := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusIn(OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed)).SetStatus(OrderStatusRefunding).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetForceRefund(p.Force).Save(txCtx)
	if err != nil {
		return err
	}
	if n != 1 {
		return infraerrors.Conflict("CONFLICT", "refund already claimed; reconcile pending refund")
	}
	// Only a definitively released job can be replaced. Corrupt or unknown
	// state is not evidence that collateral was released.
	prior, err := tx.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(p.OrderID, 10)), paymentauditlog.ActionEQ("REFUND_RESERVATION")).Only(txCtx)
	if err == nil {
		var record refundReservation
		if err := json.Unmarshal([]byte(prior.Detail), &record); err != nil {
			return err
		}
		if record.State != "released" {
			return infraerrors.Conflict("CONFLICT", "existing refund job requires reconciliation")
		}
	} else if !dbent.IsNotFound(err) {
		return err
	}
	legacy, err := tx.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(p.OrderID, 10)), paymentauditlog.ActionEQ("REFUND_ROLLBACK_FAILED")).Exist(txCtx)
	if err != nil {
		return err
	}
	if legacy {
		return infraerrors.Conflict("REFUND_RECONCILIATION_REQUIRED", "legacy rollback failure requires accounting reconciliation")
	}
	p.ReservationID = uuid.NewString()
	if err := s.applyRefundFinalDeduction(txCtx, p); err != nil {
		return err
	}
	if err := saveRefundReservation(txCtx, tx.Client(), &refundReservation{Plan: *p, State: "held"}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateRefundCaches(ctx, p)
	return nil
}

func (s *PaymentService) completeRefundReservation(ctx context.Context, supplied *RefundPlan, success bool, cause error) (*RefundResult, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	n, err := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(supplied.OrderID), paymentorder.StatusIn(OrderStatusRefundPending, OrderStatusRefunding)).SetStatus(OrderStatusRefunding).Save(txCtx)
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	order, err := tx.PaymentOrder.Get(txCtx, supplied.OrderID)
	if err != nil {
		return nil, err
	}
	record, err := loadRefundReservation(txCtx, tx.Client(), order)
	if err != nil {
		return nil, err
	}
	if record.Plan.ReservationID != supplied.ReservationID {
		return nil, infraerrors.Conflict("CONFLICT", "stale refund job")
	}
	p := &record.Plan
	var result *RefundResult
	if success {
		result, err = s.markRefundOkTx(txCtx, tx.Client(), p)
		record.State = "consumed"
	} else {
		// Release only on a definitive provider failure, atomically with job state.
		if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
			_, err = tx.User.UpdateOneID(order.UserID).AddBalance(p.BalanceToDeduct).Save(txCtx)
		}
		if err == nil && p.DeductionType == payment.DeductionTypeSubscription && p.SubscriptionID > 0 && p.SubDaysToDeduct > 0 {
			sub, subErr := tx.UserSubscription.UpdateOneID(p.SubscriptionID).SetUpdatedAt(time.Now()).Save(txCtx)
			if subErr != nil {
				err = subErr
			} else {
				expiresAt := sub.ExpiresAt.AddDate(0, 0, p.SubDaysToDeduct)
				update := tx.UserSubscription.UpdateOneID(sub.ID).SetExpiresAt(expiresAt)
				if sub.Status == SubscriptionStatusExpired && expiresAt.After(time.Now()) {
					update.SetStatus(SubscriptionStatusActive)
				}
				_, err = update.Save(txCtx)
			}
		}
		if err == nil {
			_, err = tx.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundFailed).SetFailedAt(time.Now()).SetFailedReason(psErrMsg(cause)).Save(txCtx)
		}
		record.State = "released"
		result = &RefundResult{Success: false, Warning: "provider confirmed refund failure; reservation released"}
	}
	if err != nil {
		return nil, err
	}
	var affectedInviters []int64
	if success && s.affiliateService != nil {
		rows, err := tx.Client().QueryContext(txCtx, `SELECT DISTINCT user_id FROM user_affiliate_ledger WHERE source_order_id=$1 AND action='accrue'`, order.ID)
		if err != nil {
			return nil, fmt.Errorf("load rebate cache recipients: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			affectedInviters = append(affectedInviters, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := saveRefundReservation(txCtx, tx.Client(), record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.invalidateRefundCaches(ctx, p)
	// A disconnected caller must not cancel post-commit eviction of balances
	// changed by clawback, including an inviter whose rewards were already spent.
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, id := range affectedInviters {
		s.affiliateService.invalidateAffiliateCaches(cacheCtx, id)
	}

	return result, nil
}

func (s *PaymentService) invalidateRefundCaches(ctx context.Context, p *RefundPlan) {
	if s.affiliateService != nil {
		s.affiliateService.invalidateAffiliateCaches(ctx, p.Order.UserID)
	}
	if s.redeemService != nil {
		if s.redeemService.authCacheInvalidator != nil {
			s.redeemService.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, p.Order.UserID)
		}
		if s.redeemService.billingCacheService != nil {
			_ = s.redeemService.billingCacheService.InvalidateUserBalance(ctx, p.Order.UserID)
		}
	}
	if p.Order.SubscriptionGroupID != nil && s.subscriptionSvc != nil {
		_ = s.subscriptionSvc.invalidateSubscriptionCaches(p.Order.UserID, *p.Order.SubscriptionGroupID)
	}
}

// The existing leader-owned sweep recovers both accepted pending jobs and a
// worker lost after capture. Queries never resubmit the provider refund request.
func (s *PaymentService) ReconcilePendingRefunds(ctx context.Context) error {
	orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.Or(paymentorder.StatusEQ(OrderStatusRefundPending), paymentorder.And(paymentorder.StatusEQ(OrderStatusRefunding), paymentorder.UpdatedAtLT(time.Now().Add(-paymentFulfillmentLeaseDuration))))).Order(dbent.Asc(paymentorder.FieldUpdatedAt)).Limit(50).All(ctx)
	if err != nil {
		return err
	}
	for _, order := range orders {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := s.QueryAndFinalizeRefund(ctx, order.ID); err != nil {
			s.writeAuditLog(ctx, order.ID, "REFUND_RECONCILIATION_REQUIRED", "system", map[string]any{"detail": err.Error()})
			// Rotate failed queries to avoid starving later jobs in the batch.
			_, _ = s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(order.ID), paymentorder.StatusEQ(order.Status), paymentorder.UpdatedAtEQ(order.UpdatedAt)).SetUpdatedAt(time.Now()).Save(ctx)
		}
	}
	return nil
}

// Persist the provider's definitive result before local accounting. If the
// following transaction fails, the worker retries accounting without refunding
// again or requiring query support from a synchronous-only provider.
func (s *PaymentService) recordRefundOutcome(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	n, err := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusIn(OrderStatusRefunding, OrderStatusRefundPending)).SetStatus(OrderStatusRefundPending).Save(txCtx)
	if err != nil {
		return err
	}
	if n != 1 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	record, err := loadRefundReservation(txCtx, tx.Client(), p.Order)
	if err != nil {
		return err
	}
	if record.Plan.ReservationID != p.ReservationID {
		return infraerrors.Conflict("CONFLICT", "stale refund job")
	}
	outcome := payment.ProviderStatusSuccess
	if resp.Status == payment.ProviderStatusFailed {
		outcome = payment.ProviderStatusFailed
	}
	if record.Outcome != "" && record.Outcome != outcome {
		return fmt.Errorf("conflicting provider refund outcome")
	}
	record.Outcome = outcome
	if resp.RefundID != "" {
		record.RefundID = resp.RefundID
	}
	if err := saveRefundReservation(txCtx, tx.Client(), record); err != nil {
		return err
	}
	return tx.Commit()
}

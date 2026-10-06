package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// --- Refund Flow ---

var createPaymentProviderFromInstance = provider.CreateProvider

// getOrderProviderInstance looks up the provider instance that processed this order.
// For legacy orders without provider_instance_id, it resolves only when the
// historical instance is uniquely identifiable from the stored order fields.
func (s *PaymentService) getOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return s.resolveUniqueLegacyOrderProviderInstance(ctx, o)
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, nil
	}
	return s.entClient.PaymentProviderInstance.Get(ctx, instID)
}

// getRefundOrderProviderInstance resolves the provider instance for refund paths.
// Refunds must be pinned to an explicit historical binding, so legacy
// "best-effort" provider guessing is intentionally not allowed here.
func (s *PaymentService) getRefundOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return nil, nil
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("order %d refund provider instance id is invalid: %s", o.ID, instIDStr)
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, instID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("order %d refund provider instance %s is missing", o.ID, instIDStr)
		}
		return nil, err
	}
	return inst, nil
}

func (s *PaymentService) resolveUniqueLegacyOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	paymentType := payment.GetBasePaymentType(strings.TrimSpace(o.PaymentType))
	providerKey := strings.TrimSpace(psStringValue(o.ProviderKey))
	if providerKey != "" {
		instances, err := s.entClient.PaymentProviderInstance.Query().
			Where(paymentproviderinstance.ProviderKeyEQ(providerKey)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
		if len(matched) == 1 {
			return matched[0], nil
		}
		return nil, nil
	}

	if paymentType == "" {
		return nil, nil
	}

	instances, err := s.entClient.PaymentProviderInstance.Query().
		All(ctx)
	if err != nil {
		return nil, err
	}

	matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
	if len(matched) == 1 {
		return matched[0], nil
	}
	return nil, nil
}

func psFilterLegacyOrderProviderInstances(orderPaymentType string, instances []*dbent.PaymentProviderInstance) []*dbent.PaymentProviderInstance {
	if len(instances) == 0 {
		return nil
	}
	if strings.TrimSpace(orderPaymentType) == "" {
		return instances
	}
	var matched []*dbent.PaymentProviderInstance
	for _, inst := range instances {
		if psLegacyOrderMatchesInstance(orderPaymentType, inst) {
			matched = append(matched, inst)
		}
	}
	return matched
}

func psLegacyOrderMatchesInstance(orderPaymentType string, inst *dbent.PaymentProviderInstance) bool {
	if inst == nil {
		return false
	}

	baseType := payment.GetBasePaymentType(strings.TrimSpace(orderPaymentType))
	instanceProviderKey := strings.TrimSpace(inst.ProviderKey)
	if baseType == "" {
		return false
	}

	if baseType == payment.TypeStripe {
		return instanceProviderKey == payment.TypeStripe
	}
	if instanceProviderKey == payment.TypeStripe {
		return false
	}
	if instanceProviderKey == baseType {
		return true
	}
	return payment.InstanceSupportsType(inst.SupportedTypes, baseType)
}

func (s *PaymentService) RequestRefund(ctx context.Context, oid, uid int64, reason string) error {
	o, err := s.validateRefundRequest(ctx, oid, uid)
	if err != nil {
		return err
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Balance < o.Amount {
		return infraerrors.BadRequest("BALANCE_NOT_ENOUGH", "refund amount exceeds balance")
	}
	nr := strings.TrimSpace(reason)
	now := time.Now()
	by := fmt.Sprintf("%d", uid)
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(oid), paymentorder.UserIDEQ(uid), paymentorder.StatusEQ(OrderStatusCompleted), paymentorder.OrderTypeEQ(payment.OrderTypeBalance)).SetStatus(OrderStatusRefundRequested).SetRefundRequestedAt(now).SetRefundRequestReason(nr).SetRefundRequestedBy(by).SetRefundAmount(o.Amount).Save(ctx)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if c == 0 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	s.writeAuditLog(ctx, oid, "REFUND_REQUESTED", fmt.Sprintf("user:%d", uid), map[string]any{"amount": o.Amount, "reason": nr})
	return nil
}

func (s *PaymentService) validateRefundRequest(ctx context.Context, oid, uid int64) (*dbent.PaymentOrder, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != uid {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission")
	}
	if o.OrderType != payment.OrderTypeBalance {
		return nil, infraerrors.BadRequest("INVALID_ORDER_TYPE", "only balance orders can request refund")
	}
	if o.Status != OrderStatusCompleted {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only completed orders can request refund")
	}
	// Check provider instance allows user refund
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil || inst == nil {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.AllowUserRefund {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "user refund is not enabled for this provider")
	}
	return o, nil
}

func (s *PaymentService) PrepareRefund(ctx context.Context, oid int64, amt float64, reason string, force, deduct bool) (*RefundPlan, *RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	ok := []string{OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed}
	if !psSliceContains(ok, o.Status) {
		return nil, nil, infraerrors.BadRequest("INVALID_STATUS", "order status does not allow refund")
	}
	// Check provider instance allows admin refund
	inst, instErr := s.getRefundOrderProviderInstance(ctx, o)
	if instErr != nil {
		slog.Warn("refund: provider instance lookup failed", "orderID", oid, "error", instErr)
		return nil, nil, infraerrors.InternalServer("PROVIDER_LOOKUP_FAILED", "failed to look up payment provider for this order")
	}
	if inst == nil {
		// Legacy order without provider_instance_id — block refund
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.RefundEnabled {
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not enabled for this provider")
	}
	if math.IsNaN(amt) || math.IsInf(amt, 0) {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund amount")
	}
	if amt <= 0 {
		amt = o.Amount
	}
	orderCurrency := PaymentOrderCurrency(o)
	if amt-o.Amount > paymentAmountToleranceForCurrency(orderCurrency) {
		return nil, nil, infraerrors.BadRequest("REFUND_AMOUNT_EXCEEDED", "refund amount exceeds recharge")
	}
	ga := calculateGatewayRefundAmount(o.Amount, o.PayAmount, amt, orderCurrency)
	rr := strings.TrimSpace(reason)
	if rr == "" && o.RefundRequestReason != nil {
		rr = *o.RefundRequestReason
	}
	if rr == "" {
		rr = fmt.Sprintf("refund order:%d", o.ID)
	}
	p := &RefundPlan{OrderID: oid, Order: o, RefundAmount: amt, GatewayAmount: ga, Reason: rr, Force: force, DeductBalance: deduct, DeductionType: payment.DeductionTypeNone}
	if deduct {
		if er := s.prepDeduct(ctx, o, p, force); er != nil {
			return nil, er, nil
		}
	}
	return p, nil, nil
}

func (s *PaymentService) prepDeduct(ctx context.Context, o *dbent.PaymentOrder, p *RefundPlan, force bool) *RefundResult {
	if o.OrderType == payment.OrderTypeSubscription {
		p.DeductionType = payment.DeductionTypeSubscription
		if o.SubscriptionGroupID != nil && o.SubscriptionDays != nil {
			p.SubDaysToDeduct = *o.SubscriptionDays
			sub, err := s.subscriptionSvc.GetActiveSubscription(ctx, o.UserID, *o.SubscriptionGroupID)
			if err == nil && sub != nil {
				p.SubscriptionID = sub.ID
			} else if !force {
				return &RefundResult{Success: false, Warning: "cannot find active subscription for deduction, use force", RequireForce: true}
			}
		}
		return nil
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		if !force {
			return &RefundResult{Success: false, Warning: "cannot fetch user balance, use force", RequireForce: true}
		}
		return nil
	}
	p.DeductionType = payment.DeductionTypeBalance
	if u.Balance < p.RefundAmount && !force {
		return &RefundResult{Success: false, Warning: "user balance is insufficient for deduction, use force", RequireForce: true}
	}
	p.BalanceToDeduct = math.Max(0, math.Min(p.RefundAmount, u.Balance))
	return nil
}

type availableBalanceDeductor interface {
	DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error)
}

func (s *PaymentService) deductAvailableBalance(ctx context.Context, userID int64, amount float64) (float64, error) {
	repo, ok := s.userRepo.(availableBalanceDeductor)
	if !ok {
		return 0, errors.New("user repository does not support available balance deduction")
	}
	return repo.DeductAvailableBalance(ctx, userID, amount)
}

func (s *PaymentService) ExecuteRefund(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	// Configuration/identity failures before submission have no uncertain
	// provider side effect, so detect them before capturing collateral.
	if p != nil && p.Order != nil && p.Order.PaymentTradeNo != "" {
		prov, err := s.getRefundProvider(ctx, p.Order)
		if err != nil {
			return nil, err
		}
		if err := validateProviderSnapshotMetadata(p.Order, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
			return nil, err
		}
	}
	if err := s.reserveRefund(ctx, p); err != nil {
		return nil, err
	}
	resp, err := s.gwRefund(ctx, p)
	if err != nil {
		// A timeout/error does not prove the provider rejected the refund.
		// The durable reservation is now owned by reconciliation, not this call.
		s.writeAuditLog(ctx, p.OrderID, "REFUND_OUTCOME_UNKNOWN", "admin", map[string]any{"detail": psErrMsg(err)})
		return s.markRefundPending(ctx, p, resp)
	}
	return s.finishRefund(ctx, p, resp)
}

func (s *PaymentService) gwRefund(ctx context.Context, p *RefundPlan) (*payment.RefundResponse, error) {
	if p.Order.PaymentTradeNo == "" {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_NO_TRADE_NO", "admin", map[string]any{"detail": "skipped"})
		return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
	}

	// Use the exact provider instance that created this order, not a random one
	// from the registry. Each instance has its own merchant credentials.
	prov, err := s.getRefundProvider(ctx, p.Order)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(p.Order, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_PROVIDER_METADATA_MISMATCH", "admin", map[string]any{
			"detail": err.Error(),
		})
		return nil, err
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.Refund(ctx, payment.RefundRequest{
		TradeNo: p.Order.PaymentTradeNo,
		OrderID: p.Order.OutTradeNo,
		Amount:  formatGatewayRefundAmount(p.GatewayAmount, p.Order),
		Reason:  p.Reason,
	})
	finishProviderCall()
	if err != nil {
		if resp != nil {
			switch strings.TrimSpace(resp.Status) {
			case payment.ProviderStatusPending, payment.ProviderStatusFailed:
				return resp, nil
			}
		}
		return resp, err
	}
	return resp, nil
}

func formatGatewayRefundAmount(amount float64, order *dbent.PaymentOrder) string {
	return payment.FormatAmountForCurrency(amount, PaymentOrderCurrency(order))
}

func (s *PaymentService) finishRefund(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	if resp == nil {
		return s.markRefundPending(ctx, p, resp)
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		if err := s.recordRefundOutcome(ctx, p, resp); err != nil {
			return nil, err
		}
		return s.markRefundOk(ctx, p)
	case payment.ProviderStatusFailed:
		if err := s.recordRefundOutcome(ctx, p, resp); err != nil {
			return nil, err
		}
		return s.completeRefundReservation(ctx, p, false, fmt.Errorf("provider confirmed refund failure"))
	default:
		return s.markRefundPending(ctx, p, resp)
	}
}

func (s *PaymentService) QueryAndFinalizeRefund(ctx context.Context, oid int64) (*RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if !refundReconciliationReady(o, time.Now()) {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "refund is not awaiting reconciliation")
	}
	record, err := loadRefundReservation(ctx, s.entClient, o)
	if err != nil {
		return nil, err
	}
	if record.Outcome == payment.ProviderStatusSuccess {
		return s.completeRefundReservation(ctx, &record.Plan, true, nil)
	}
	if record.Outcome == payment.ProviderStatusFailed {
		return s.completeRefundReservation(ctx, &record.Plan, false, fmt.Errorf("provider confirmed refund failure"))
	}
	prov, err := s.getRefundProvider(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(o, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		return nil, err
	}
	queryProvider, ok := prov.(payment.RefundQueryProvider)
	if !ok {
		return nil, infraerrors.BadRequest("REFUND_QUERY_UNSUPPORTED", "this payment provider requires manual refund reconciliation; reservation retained")
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
		TradeNo: o.PaymentTradeNo, OrderID: o.OutTradeNo, RefundID: record.RefundID,
		Amount: formatGatewayRefundAmount(record.Plan.GatewayAmount, o),
	})
	finishProviderCall()
	if err != nil {
		return nil, fmt.Errorf("query refund: %w", err)
	}
	if resp != nil && record.RefundID != "" && resp.RefundID != "" && record.RefundID != resp.RefundID {
		return nil, fmt.Errorf("refund query identity mismatch")
	}
	return s.finishRefund(ctx, &record.Plan, resp)
}

// ResolvePendingRefund applies an explicit administrator decision when the
// provider has no query API (for example legacy EasyPay). This is deliberately
// separate from retrying the provider request: resubmitting an unknown refund
// can double-refund an order, while a recorded decision gives the held balance
// a controlled accounting exit.
func (s *PaymentService) ResolvePendingRefund(ctx context.Context, oid int64, success bool, reason string) (*RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if !refundReconciliationReady(o, time.Now()) {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "order is not awaiting refund resolution")
	}
	record, err := loadRefundReservation(ctx, s.entClient, o)
	if err != nil {
		return nil, err
	}
	decision := strings.TrimSpace(reason)
	if decision == "" {
		return nil, infraerrors.BadRequest("REASON_REQUIRED", "manual refund resolution requires a reason")
	}
	result, err := s.completeRefundReservation(ctx, &record.Plan, success, fmt.Errorf("manual resolution: %s", decision))
	if err != nil {
		return nil, err
	}
	s.writeAuditLog(ctx, oid, "REFUND_MANUAL_RESOLUTION", "admin", map[string]any{
		"outcome": map[bool]string{true: "success", false: "failed"}[success],
		"reason":  decision,
	})
	return result, nil
}

// refundReconciliationReady is shared by provider queries and manual
// decisions. A REFUNDING order remains owned by the provider request until its
// fulfillment lease expires; resolving it earlier can release collateral while
// the provider is still able to complete the original refund.
func refundReconciliationReady(o *dbent.PaymentOrder, now time.Time) bool {
	if o == nil {
		return false
	}
	return o.Status == OrderStatusRefundPending ||
		(o.Status == OrderStatusRefunding && now.Sub(o.UpdatedAt) > paymentFulfillmentLeaseDuration)
}

func (s *PaymentService) applyRefundFinalDeduction(ctx context.Context, p *RefundPlan) error {
	if p.DeductionType == payment.DeductionTypeBalance {
		if p.Force {
			deducted, err := s.deductAvailableBalance(ctx, p.Order.UserID, p.RefundAmount)
			if err != nil {
				return fmt.Errorf("deduction: %w", err)
			}
			p.BalanceToDeduct = deducted
		} else {
			tx := dbent.TxFromContext(ctx)
			if tx == nil {
				return fmt.Errorf("refund deduction requires transaction")
			}
			n, err := tx.User.Update().Where(user.IDEQ(p.Order.UserID), user.DeletedAtIsNil(), user.BalanceGTE(p.RefundAmount)).AddBalance(-p.RefundAmount).Save(ctx)
			if err != nil {
				return fmt.Errorf("deduction: %w", err)
			}
			if n != 1 {
				return infraerrors.BadRequest("BALANCE_NOT_ENOUGH", "current balance is insufficient for refund; use explicit force")
			}
			p.BalanceToDeduct = p.RefundAmount
		}
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 {
		if _, err := s.subscriptionSvc.ExtendSubscription(ctx, p.SubscriptionID, -p.SubDaysToDeduct); err != nil {
			// Revocation deletes the entitlement and cannot be safely restored on
			// a pending failure. Reserve by shortening directly, even past now.
			if !errors.Is(err, ErrAdjustWouldExpire) {
				return fmt.Errorf("deduct subscription: %w", err)
			}
			tx := dbent.TxFromContext(ctx)
			sub, err := tx.UserSubscription.Get(ctx, p.SubscriptionID)
			if err != nil {
				return err
			}
			_, err = tx.UserSubscription.UpdateOneID(sub.ID).SetExpiresAt(sub.ExpiresAt.AddDate(0, 0, -p.SubDaysToDeduct)).Save(ctx)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *PaymentService) getRefundProvider(ctx context.Context, o *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("refund provider instance is unavailable for order %d", o.ID)
	}
	return s.createProviderFromInstance(ctx, inst)
}

func (s *PaymentService) markRefundOk(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	return s.completeRefundReservation(ctx, p, true, nil)
}

func (s *PaymentService) markRefundOkTx(ctx context.Context, client *dbent.Client, p *RefundPlan) (*RefundResult, error) {
	if s.affiliateService != nil {
		if err := s.affiliateService.ReverseOrderRebate(ctx, p.OrderID, p.RefundAmount, p.Order.Amount); err != nil {
			return nil, err
		}
	}
	fs := OrderStatusRefunded
	if p.RefundAmount < p.Order.Amount {
		fs = OrderStatusPartiallyRefunded
	}
	now := time.Now()
	_, err := client.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(fs).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetRefundAt(now).SetForceRefund(p.Force).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund: %w", err)
	}
	detail, err := json.Marshal(map[string]any{"refundAmount": p.RefundAmount, "reason": p.Reason, "balanceDeducted": p.BalanceToDeduct, "force": p.Force})
	if err != nil {
		return nil, fmt.Errorf("marshal refund audit: %w", err)
	}
	if _, err := client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(p.OrderID, 10)).
		SetAction("REFUND_SUCCESS").
		SetDetail(string(detail)).
		SetOperator("admin").
		Save(ctx); err != nil {
		return nil, fmt.Errorf("write refund audit: %w", err)
	}
	return &RefundResult{Success: true, BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct}, nil
}

func (s *PaymentService) markRefundPending(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	n, err := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusIn(OrderStatusRefunding, OrderStatusRefundPending)).
		SetStatus(OrderStatusRefundPending).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetForceRefund(p.Force).Save(txCtx)
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	record, err := loadRefundReservation(txCtx, tx.Client(), p.Order)
	if err != nil {
		return nil, err
	}
	if record.Plan.ReservationID != p.ReservationID {
		return nil, infraerrors.Conflict("CONFLICT", "stale refund job")
	}
	if id := refundResponseID(resp); id != "" {
		record.RefundID = id
	}
	if err := saveRefundReservation(txCtx, tx.Client(), record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.writeAuditLog(ctx, p.OrderID, "REFUND_PENDING", "admin", map[string]any{"refundID": record.RefundID, "balanceDeducted": record.Plan.BalanceToDeduct, "deductionRollbackOK": false})
	return &RefundResult{Success: false, Warning: "gateway refund is pending confirmation; reservation retained"}, nil
}

func refundResponseID(resp *payment.RefundResponse) string {
	if resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.RefundID)
}

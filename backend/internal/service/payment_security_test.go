//go:build unit

package service

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func securityRefundFixture(t *testing.T, client *dbent.Client, balance float64, suffix string) (*PaymentService, *RefundPlan) {
	t.Helper()
	ctx := context.Background()
	order := createPendingRefundOrderForTest(t, ctx, client, suffix)
	_, err := client.User.UpdateOneID(order.UserID).SetBalance(balance).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusCompleted).SetPaymentTradeNo("").Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, userRepo: &securityRefundUserRepo{client: client}}
	return svc, &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 100, GatewayAmount: 100, Reason: "security test", DeductBalance: true, DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: 100}
}

type securityRefundUserRepo struct {
	UserRepository
	client *dbent.Client
}

func (r *securityRefundUserRepo) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return 0, fmt.Errorf("missing transaction")
	}
	u, err := tx.User.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	if u.Balance < amount {
		amount = u.Balance
	}
	if amount < 0 {
		amount = 0
	}
	_, err = tx.User.UpdateOneID(id).AddBalance(-amount).Save(ctx)
	return amount, err
}

func TestPaymentSecurityRefundReservationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name            string
		balance         float64
		force, noDeduct bool
		want            float64
		reject          bool
	}{
		{name: "spent after prepare", balance: 10, reject: true},
		{name: "full collateral", balance: 100, want: 100},
		{name: "forced partial", balance: 25, force: true, want: 25},
		{name: "forced existing debt", balance: -5, force: true},
		{name: "authorized no deduction", balance: 10, noDeduct: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			svc, p := securityRefundFixture(t, client, tc.balance, "boundary")
			p.Force = tc.force
			if tc.noDeduct {
				p.DeductBalance = false
				p.DeductionType = payment.DeductionTypeNone
				p.BalanceToDeduct = 0
			}
			result, err := svc.ExecuteRefund(ctx, p)
			u, getErr := client.User.Get(ctx, p.Order.UserID)
			require.NoError(t, getErr)
			o, getErr := client.PaymentOrder.Get(ctx, p.OrderID)
			require.NoError(t, getErr)
			if tc.reject {
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, OrderStatusCompleted, o.Status)
				require.Equal(t, tc.balance, u.Balance)
				return
			}
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, tc.want, result.BalanceDeducted)
			require.Equal(t, tc.balance-tc.want, u.Balance)
		})
	}
}

func TestPaymentSecurityPendingRefundRetainsDurableCollateral(t *testing.T) {
	for _, terminal := range []string{payment.ProviderStatusSuccess, payment.ProviderStatusFailed} {
		t.Run(terminal, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			svc, p := securityRefundFixture(t, client, 100, "durable")
			require.NoError(t, svc.reserveRefund(ctx, p))
			result, err := svc.finishRefund(ctx, p, &payment.RefundResponse{Status: payment.ProviderStatusPending, RefundID: "refund-provider-id"})
			require.NoError(t, err)
			require.False(t, result.Success)
			require.Equal(t, float64(0), client.User.GetX(ctx, p.Order.UserID).Balance)
			// A fresh service instance loads the committed job, not a caller's plan.
			restarted := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
			record, err := loadRefundReservation(ctx, client, p.Order)
			require.NoError(t, err)
			require.Equal(t, "refund-provider-id", record.RefundID)
			// Unknown status must never be classified as definitive failure.
			_, err = restarted.finishRefund(ctx, &record.Plan, &payment.RefundResponse{Status: "unrecognized"})
			require.NoError(t, err)
			require.Equal(t, float64(0), client.User.GetX(ctx, p.Order.UserID).Balance)
			_, err = restarted.ExecuteRefund(ctx, p)
			require.Error(t, err)
			result, err = restarted.finishRefund(ctx, &record.Plan, &payment.RefundResponse{Status: terminal})
			require.NoError(t, err)
			require.Equal(t, terminal == payment.ProviderStatusSuccess, result.Success)
			wantBalance := float64(0)
			if terminal == payment.ProviderStatusFailed {
				wantBalance = 100
			}
			require.Equal(t, wantBalance, client.User.GetX(ctx, p.Order.UserID).Balance)
			_, err = restarted.finishRefund(ctx, &record.Plan, &payment.RefundResponse{Status: terminal})
			require.Error(t, err)
			require.Equal(t, wantBalance, client.User.GetX(ctx, p.Order.UserID).Balance)
			if terminal == payment.ProviderStatusFailed { // Definitively released attempts may retry.
				result, err = svc.ExecuteRefund(ctx, p)
				require.NoError(t, err)
				require.True(t, result.Success)
			}
		})
	}
}

func TestPaymentSecurityUnknownOrderTypes(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc, p := securityRefundFixture(t, client, 0, "unknown")
	for _, typ := range []string{"other", "BALANCE", " balance", "subscription ", "balance\x00"} {
		_, err := svc.validateOrderInput(ctx, CreateOrderRequest{OrderType: typ, Amount: 100}, &PaymentConfig{BalanceDisabled: true})
		require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(err))
		_, err = client.PaymentOrder.UpdateOneID(p.OrderID).SetOrderType(typ).SetStatus(OrderStatusPaid).Save(ctx)
		require.NoError(t, err)
		require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(svc.executeFulfillment(ctx, p.OrderID)))
		require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(svc.ExecuteBalanceFulfillment(ctx, p.OrderID)))
		require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(svc.ExecuteSubscriptionFulfillment(ctx, p.OrderID)))
	}
	_, err := svc.validateOrderInput(ctx, CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: 100}, &PaymentConfig{BalanceDisabled: true})
	require.Equal(t, "BALANCE_PAYMENT_DISABLED", infraerrors.Reason(err))
	_, err = svc.validateOrderInput(ctx, CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: 100}, &PaymentConfig{})
	require.NoError(t, err)
	require.Equal(t, float64(20), calculateCreditedBalance(100, .2))
}

func TestPaymentSecurityReturnURLDropsAuthorityFields(t *testing.T) {
	raw := "https://site/payment/result?trade_no=fake&trade_status=TRADE_SUCCESS&money=100&from=checkout"
	canonical, err := CanonicalizeReturnURL(raw, "site", "")
	require.NoError(t, err)
	require.Equal(t, "https://site/payment/result", canonical)
	built, err := buildPaymentReturnURL(raw, 42, "server-order", "server-resume")
	require.NoError(t, err)
	parsed, err := url.Parse(built)
	require.NoError(t, err)
	require.Len(t, parsed.Query(), 4)
	require.Empty(t, parsed.Query().Get("trade_no"))
	require.Equal(t, "server-order", parsed.Query().Get("out_trade_no"))
}

func TestPaymentSecurityDailyAllowanceIncludesUnsettledOrders(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc, p := securityRefundFixture(t, client, 0, "allowance")
	// A cancelled order from yesterday can still settle today, so must reserve.
	for _, status := range []string{OrderStatusPending, OrderStatusCancelled, OrderStatusExpired, OrderStatusFailed} {
		_, err := client.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(status).ClearPaidAt().SetProviderSnapshot(map[string]any{"daily_allowance_amount": 80.0}).Save(ctx)
		require.NoError(t, err)
		tx, err := client.Tx(ctx)
		require.NoError(t, err)
		require.Equal(t, "DAILY_LIMIT_EXCEEDED", infraerrors.Reason(svc.checkDailyLimit(ctx, tx, p.Order.UserID, 21, 100)))
		require.NoError(t, svc.checkDailyLimit(ctx, tx, p.Order.UserID, 20, 100))
		require.NoError(t, tx.Rollback())
	}
}

func TestPaymentSecurityStaleRefundJobCannotSettleRetry(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc, p := securityRefundFixture(t, client, 100, "stale-job")
	require.NoError(t, svc.reserveRefund(ctx, p))
	old := *p
	_, err := svc.finishRefund(ctx, p, &payment.RefundResponse{Status: payment.ProviderStatusFailed})
	require.NoError(t, err)
	require.NoError(t, svc.reserveRefund(ctx, p))
	require.NotEqual(t, old.ReservationID, p.ReservationID)
	_, err = svc.finishRefund(ctx, &old, &payment.RefundResponse{Status: payment.ProviderStatusSuccess})
	require.ErrorContains(t, err, "stale refund job")
	_, err = svc.finishRefund(ctx, &old, &payment.RefundResponse{Status: payment.ProviderStatusPending, RefundID: "wrong-attempt"})
	require.ErrorContains(t, err, "stale refund job")
	record, err := loadRefundReservation(ctx, client, p.Order)
	require.NoError(t, err)
	require.Empty(t, record.RefundID)
	require.Equal(t, p.ReservationID, record.Plan.ReservationID)
	require.Zero(t, client.User.GetX(ctx, p.Order.UserID).Balance)
}

func TestPaymentSecurityKnownOutcomeSurvivesAccountingFailure(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc, p := securityRefundFixture(t, client, 100, "known-outcome")
	require.NoError(t, svc.reserveRefund(ctx, p))
	_, err := client.ExecContext(ctx, `CREATE TRIGGER fail_security_success BEFORE INSERT ON payment_audit_logs WHEN NEW.action='REFUND_SUCCESS' BEGIN SELECT RAISE(ABORT,'accounting unavailable'); END`)
	require.NoError(t, err)
	_, err = svc.finishRefund(ctx, p, &payment.RefundResponse{Status: payment.ProviderStatusSuccess, RefundID: "provider-success"})
	require.ErrorContains(t, err, "accounting unavailable")
	record, err := loadRefundReservation(ctx, client, p.Order)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusSuccess, record.Outcome)
	require.Equal(t, "provider-success", record.RefundID)
	require.Zero(t, client.User.GetX(ctx, p.Order.UserID).Balance)
	_, err = client.ExecContext(ctx, `DROP TRIGGER fail_security_success`)
	require.NoError(t, err)
	// Deliberately no provider/load-balancer: the durable known result is enough.
	result, err := (&PaymentService{entClient: client}).QueryAndFinalizeRefund(ctx, p.OrderID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Zero(t, client.User.GetX(ctx, p.Order.UserID).Balance)
}

func TestPaymentSecuritySubscriptionPendingRestoresEntitlementOnlyOnFailure(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc, p := securityRefundFixture(t, client, 0, "subscription-hold")
	group := client.Group.Create().SetName("refund-subscription").SaveX(ctx)
	expires := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	sub := client.UserSubscription.Create().SetUserID(p.Order.UserID).SetGroupID(group.ID).SetStartsAt(time.Now().Add(-time.Hour)).SetExpiresAt(expires).SaveX(ctx)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID, ExpiresAt: expires, Status: SubscriptionStatusActive})
	svc.subscriptionSvc = NewSubscriptionService(nil, subRepo, nil, nil, nil)
	order, err := client.PaymentOrder.UpdateOneID(p.OrderID).SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(group.ID).SetSubscriptionDays(30).Save(ctx)
	require.NoError(t, err)
	p.Order = order
	p.DeductionType = payment.DeductionTypeSubscription
	p.BalanceToDeduct = 0
	p.SubDaysToDeduct = 30
	p.SubscriptionID = sub.ID
	require.NoError(t, svc.reserveRefund(ctx, p))
	_, err = svc.finishRefund(ctx, p, &payment.RefundResponse{Status: payment.ProviderStatusPending})
	require.NoError(t, err)
	held := client.UserSubscription.GetX(ctx, sub.ID)
	require.True(t, held.ExpiresAt.Before(time.Now()))
	require.Nil(t, held.DeletedAt)
	// Simulate the regular expiry sweep while the provider is pending.
	_, err = client.UserSubscription.UpdateOneID(sub.ID).SetStatus(SubscriptionStatusExpired).Save(ctx)
	require.NoError(t, err)
	_, err = svc.finishRefund(ctx, p, &payment.RefundResponse{Status: payment.ProviderStatusFailed})
	require.NoError(t, err)
	restored := client.UserSubscription.GetX(ctx, sub.ID)
	require.True(t, restored.ExpiresAt.Equal(expires))
	require.Equal(t, SubscriptionStatusActive, restored.Status)
}

func TestPaymentSecurityDailyAllowanceUsesSettlementDate(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc, p := securityRefundFixture(t, client, 0, "allowance-date")
	// Imported or repaired timestamps can put creation after settlement. Only
	// settlement determines a paid order's bucket; unpaid orders still reserve.
	_, err := client.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(OrderStatusCompleted).
		SetPaidAt(psStartOfDayUTC(time.Now()).Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.checkDailyLimit(ctx, tx, p.Order.UserID, 100, 100))
	require.NoError(t, tx.Rollback())
	_, err = client.PaymentOrder.UpdateOneID(p.OrderID).SetPaidAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	require.Equal(t, "DAILY_LIMIT_EXCEEDED", infraerrors.Reason(svc.checkDailyLimit(ctx, tx, p.Order.UserID, 1, 100)))
	require.NoError(t, tx.Rollback())
}

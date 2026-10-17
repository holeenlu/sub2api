//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

// Package-local database adapters keep the fulfillment test at the real balance
// transaction boundary without sharing or mutating other workers' fixtures.
type allowanceUserRepository struct {
	UserRepository
	client *dbent.Client
}

func (r *allowanceUserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
	u, err := r.client.User.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Balance: u.Balance}, nil
}
func (r *allowanceUserRepository) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("missing balance transaction")
	}
	_, err := tx.User.UpdateOneID(id).AddBalance(amount).Save(ctx)
	return err
}

type allowanceRedeemRepository struct {
	RedeemCodeRepository
	client *dbent.Client
}

func allowanceRedeemResult(c *dbent.RedeemCode, err error) (*RedeemCode, error) {
	if dbent.IsNotFound(err) {
		return nil, ErrRedeemCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	return &RedeemCode{ID: c.ID, Code: c.Code, Type: c.Type, Status: c.Status, Value: c.Value, UsedBy: c.UsedBy, UsedAt: c.UsedAt}, nil
}
func (r *allowanceRedeemRepository) GetByCode(ctx context.Context, code string) (*RedeemCode, error) {
	return allowanceRedeemResult(r.client.RedeemCode.Query().Where(redeemcode.CodeEQ(code)).Only(ctx))
}
func (r *allowanceRedeemRepository) GetByID(ctx context.Context, id int64) (*RedeemCode, error) {
	return allowanceRedeemResult(r.client.RedeemCode.Get(ctx, id))
}
func (r *allowanceRedeemRepository) Create(ctx context.Context, c *RedeemCode) error {
	saved, err := r.client.RedeemCode.Create().SetCode(c.Code).SetType(c.Type).SetValue(c.Value).SetStatus(c.Status).Save(ctx)
	if err == nil {
		c.ID = saved.ID
	}
	return err
}
func (r *allowanceRedeemRepository) Use(ctx context.Context, id, uid int64) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("missing redemption transaction")
	}
	n, err := tx.RedeemCode.Update().Where(redeemcode.IDEQ(id), redeemcode.StatusEQ(StatusUnused)).SetStatus(StatusUsed).SetUsedBy(uid).SetUsedAt(time.Now()).Save(ctx)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRedeemCodeUsed
	}
	return nil
}
func allowancePaymentService(client *dbent.Client) *PaymentService {
	users := &allowanceUserRepository{client: client}
	return &PaymentService{entClient: client, userRepo: users, redeemService: NewRedeemService(&allowanceRedeemRepository{client: client}, users, nil, nil, nil, client, nil, nil)}
}
func allowanceCreateOrder(ctx context.Context, s *PaymentService, u *dbent.User) (*dbent.PaymentOrder, error) {
	return s.createOrderInTx(ctx, CreateOrderRequest{UserID: u.ID, OrderType: payment.OrderTypeBalance, PaymentType: payment.TypeStripe}, &User{ID: u.ID, Email: u.Email}, nil, &PaymentConfig{DailyLimit: 100, MaxPendingOrders: 3}, 100, 100, 0, 100, 0, nil)
}

func TestPaymentAllowanceBoundsUnpaidReservations(t *testing.T) {
	now := time.Now().UTC()
	for _, status := range []string{OrderStatusPending, OrderStatusCancelled, OrderStatusExpired, OrderStatusFailed} {
		t.Run(status, func(t *testing.T) {
			o := &dbent.PaymentOrder{Status: status, ExpiresAt: now.Add(-paymentAllowanceGrace).Add(time.Second)}
			require.True(t, paymentAllowanceCounted(o, now))
			o.ExpiresAt = now.Add(-paymentAllowanceGrace)
			require.False(t, paymentAllowanceCounted(o, now))
			o.ExpiresAt = now.Add(-48 * time.Hour)
			require.False(t, paymentAllowanceCounted(o, now))
		})
	}
}

func TestPaymentAllowanceLatePaymentDeferredThenFulfilled(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	s := allowancePaymentService(client)
	u := client.User.Create().SetEmail("allowance-late@example.com").SetPasswordHash("hash").SaveX(ctx)
	abandoned, err := allowanceCreateOrder(ctx, s, u)
	require.NoError(t, err)
	abandoned, err = client.PaymentOrder.UpdateOneID(abandoned.ID).SetStatus(OrderStatusExpired).SetExpiresAt(time.Now().Add(-48 * time.Hour)).Save(ctx)
	require.NoError(t, err)
	fresh, err := allowanceCreateOrder(ctx, s, u)
	require.NoError(t, err, "abandoned checkout must not block later purchases")
	require.NoError(t, s.toPaid(ctx, fresh, "fresh-trade", 100, payment.TypeStripe))
	require.Equal(t, float64(100), client.User.GetX(ctx, u.ID).Balance)
	require.NoError(t, s.toPaid(ctx, abandoned, "late-trade", 100, payment.TypeStripe))
	held := client.PaymentOrder.GetX(ctx, abandoned.ID)
	require.Equal(t, OrderStatusPaid, held.Status)
	require.NotNil(t, held.PaidAt)
	require.Equal(t, "late-trade", held.PaymentTradeNo)
	require.Equal(t, "deferred", held.ProviderSnapshot[paymentAllowanceStateKey])
	require.Equal(t, float64(100), client.User.GetX(ctx, u.ID).Balance)
	require.NoError(t, s.ExecuteBalanceFulfillment(ctx, abandoned.ID)) // direct/admin path cannot bypass
	require.Equal(t, float64(100), client.User.GetX(ctx, u.ID).Balance)
	// Simulate the next UTC day's settled usage; the late payment itself may
	// have PaidAt yesterday, but admission must be booked to today's allowance.
	yesterday := psStartOfDayUTC(time.Now()).Add(-time.Hour)
	snapshot := client.PaymentOrder.GetX(ctx, fresh.ID).ProviderSnapshot
	snapshot[paymentAllowanceDayKey] = yesterday.Format("2006-01-02")
	_, err = client.PaymentOrder.UpdateOneID(fresh.ID).SetPaidAt(yesterday).SetCompletedAt(yesterday).SetProviderSnapshot(snapshot).Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.UpdateOneID(abandoned.ID).SetPaidAt(yesterday).Save(ctx)
	require.NoError(t, err)
	count, err := s.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, OrderStatusCompleted, client.PaymentOrder.GetX(ctx, abandoned.ID).Status)
	require.Equal(t, float64(200), client.User.GetX(ctx, u.ID).Balance)
	require.NoError(t, s.toPaid(ctx, abandoned, "late-trade", 100, payment.TypeStripe))
	require.Equal(t, float64(200), client.User.GetX(ctx, u.ID).Balance, "duplicate callback must not credit twice")
	_, err = allowanceCreateOrder(ctx, s, u)
	require.Error(t, err, "late fulfillment consumes today's allowance")
}

type clawbackCacheRecorder struct {
	APIKeyAuthCacheInvalidator
	BillingCache
	authIDs, balanceIDs []int64
}

func (c *clawbackCacheRecorder) InvalidateAuthCacheByUserID(ctx context.Context, id int64) {
	c.authIDs = append(c.authIDs, id)
}
func (c *clawbackCacheRecorder) InvalidateUserBalance(ctx context.Context, id int64) error {
	c.balanceIDs = append(c.balanceIDs, id)
	return ctx.Err()
}

type spentRebateTestRepository struct {
	AffiliateRepository
	inviterID int64
}

func (r *spentRebateTestRepository) ReverseOrderRebate(ctx context.Context, oid int64, refunded, original float64) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("missing refund transaction")
	}
	_, err := tx.User.UpdateOneID(r.inviterID).AddBalance(-20 * refunded / original).Save(ctx)
	return err
}
func TestPaymentRefundInvalidatesSpentInviterBalance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	s, p := securityRefundFixture(t, client, 100, "clawback-cache")
	inviter := client.User.Create().SetEmail("spent-inviter@example.com").SetPasswordHash("hash").SetBalance(0).SaveX(ctx)
	_, err := client.ExecContext(ctx, `CREATE TABLE user_affiliate_ledger(user_id bigint, source_order_id bigint, action text)`)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, `INSERT INTO user_affiliate_ledger VALUES ($1,$2,'accrue')`, inviter.ID, p.OrderID)
	require.NoError(t, err)
	cache := &clawbackCacheRecorder{}
	s.affiliateService = &AffiliateService{repo: &spentRebateTestRepository{inviterID: inviter.ID}, authCacheInvalidator: cache, billingCacheService: &BillingCacheService{cache: cache}}
	result, err := s.ExecuteRefund(ctx, p)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, float64(-20), client.User.GetX(ctx, inviter.ID).Balance)
	require.Contains(t, cache.authIDs, inviter.ID)
	require.Contains(t, cache.balanceIDs, inviter.ID)
	require.Contains(t, cache.authIDs, p.Order.UserID)
	require.Contains(t, cache.balanceIDs, p.Order.UserID)
}

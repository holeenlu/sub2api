//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func securityAffiliateUsers(t *testing.T) (*affiliateRepository, int64, int64) {
	t.Helper()
	ctx := context.Background()
	repo := &affiliateRepository{client: integrationEntClient}
	u := integrationEntClient.User.Create().SetEmail(fmt.Sprintf("aff-sec-inviter-%d@example.com", time.Now().UnixNano())).SetPasswordHash("hash").SaveX(ctx)
	v := integrationEntClient.User.Create().SetEmail(fmt.Sprintf("aff-sec-invitee-%d@example.com", time.Now().UnixNano())).SetPasswordHash("hash").SaveX(ctx)
	t.Cleanup(func() {
		_, _ = integrationEntClient.ExecContext(ctx, `DELETE FROM payment_orders WHERE user_id IN ($1,$2)`, u.ID, v.ID)
		_, _ = integrationEntClient.ExecContext(ctx, `DELETE FROM users WHERE id IN ($1,$2)`, u.ID, v.ID)
	})
	_, err := repo.EnsureUserAffiliate(ctx, u.ID)
	require.NoError(t, err)
	_, err = repo.EnsureUserAffiliate(ctx, v.ID)
	require.NoError(t, err)
	_, err = repo.BindInviter(ctx, v.ID, u.ID)
	require.NoError(t, err)
	return repo, u.ID, v.ID
}

func securityAffiliateOrder(t *testing.T, uid int64) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	key := fmt.Sprintf("aff-sec-%d", time.Now().UnixNano())
	return integrationEntClient.PaymentOrder.Create().SetUserID(uid).SetUserEmail("test@example.com").SetUserName("test").SetAmount(100).SetPayAmount(100).SetRechargeCode(key).SetOutTradeNo(key).SetPaymentType("stripe").SetPaymentTradeNo(key).SetOrderType("balance").SetStatus(service.OrderStatusCompleted).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("localhost").SaveX(ctx)
}

func TestAffiliateSecurityConcurrentCapDistinctSources(t *testing.T) {
	for _, source := range []string{"redemptions", "orders"} {
		t.Run(source, func(t *testing.T) {
			ctx := context.Background()
			repo, inviter, invitee := securityAffiliateUsers(t)
			var sources []*int64
			for i := 0; i < 16; i++ {
				var id *int64
				if source == "orders" {
					order := securityAffiliateOrder(t, invitee)
					id = &order.ID
				}
				sources = append(sources, id)
			}
			start := make(chan struct{})
			errs := make(chan error, 16)
			credits := make(chan float64, 16)
			var wg sync.WaitGroup
			for _, id := range sources {
				wg.Add(1)
				go func(id *int64) {
					defer wg.Done()
					<-start
					n, err := repo.AccrueQuotaCapped(ctx, inviter, invitee, 3, 0, id, 7)
					errs <- err
					credits <- n
				}(id)
			}
			close(start)
			wg.Wait()
			close(errs)
			close(credits)
			for err := range errs {
				require.NoError(t, err)
			}
			var total float64
			for n := range credits {
				total += n
			}
			require.Equal(t, float64(7), total)
			gross, err := repo.GetAccruedRebateFromInvitee(ctx, inviter, invitee)
			require.NoError(t, err)
			require.Equal(t, float64(7), gross)
			require.Equal(t, float64(7), querySingleFloat(t, ctx, integrationEntClient, `SELECT aff_quota FROM user_affiliates WHERE user_id=$1`, inviter))
		})
	}
}

func TestAffiliateSecurityRefundRecoveryStates(t *testing.T) {
	for _, state := range []string{"frozen", "available", "transferred-spent"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			repo, inviter, invitee := securityAffiliateUsers(t)
			order := securityAffiliateOrder(t, invitee)
			freeze := 0
			if state == "frozen" {
				freeze = 24
			}
			n, err := repo.AccrueQuotaCapped(ctx, inviter, invitee, 20, freeze, &order.ID, 100)
			require.NoError(t, err)
			require.Equal(t, float64(20), n)
			if state == "transferred-spent" {
				_, _, err = repo.TransferQuotaToBalance(ctx, inviter)
				require.NoError(t, err)
				_, err = integrationEntClient.User.UpdateOneID(inviter).SetBalance(0).Save(ctx)
				require.NoError(t, err)
			}
			for _, refunded := range []float64{50, 50, 100, 100} {
				require.NoError(t, repo.ReverseOrderRebate(ctx, order.ID, refunded, 100))
			}
			require.Equal(t, float64(20), querySingleFloat(t, ctx, integrationEntClient, `SELECT reversed_amount FROM user_affiliate_ledger WHERE source_order_id=$1 AND action='accrue'`, order.ID))
			require.Equal(t, float64(0), querySingleFloat(t, ctx, integrationEntClient, `SELECT aff_quota+aff_frozen_quota FROM user_affiliates WHERE user_id=$1`, inviter))
			wantBalance := float64(0)
			if state == "transferred-spent" {
				wantBalance = -20
			}
			require.Equal(t, wantBalance, integrationEntClient.User.GetX(ctx, inviter).Balance)
			_, err = integrationEntClient.ExecContext(ctx, `UPDATE user_affiliate_ledger SET frozen_until=NOW()-interval '1 hour' WHERE source_order_id=$1 AND action='accrue'`, order.ID)
			require.NoError(t, err)
			thawed, err := repo.ThawFrozenQuota(ctx, inviter)
			require.NoError(t, err)
			require.Zero(t, thawed)
			// Gross cap is deliberately not reset by refunds.
			gross, err := repo.GetAccruedRebateFromInvitee(ctx, inviter, invitee)
			require.NoError(t, err)
			require.Equal(t, float64(20), gross)
		})
	}
}

func TestAffiliateSecurityConcurrentRefundThawTransfer(t *testing.T) {
	ctx := context.Background()
	repo, inviter, invitee := securityAffiliateUsers(t)
	order := securityAffiliateOrder(t, invitee)
	_, err := repo.AccrueQuotaCapped(ctx, inviter, invitee, 20, 24, &order.ID, 100)
	require.NoError(t, err)
	_, err = integrationEntClient.ExecContext(ctx, `UPDATE user_affiliate_ledger SET frozen_until=NOW()-interval '1 hour' WHERE source_order_id=$1`, order.ID)
	require.NoError(t, err)
	start := make(chan struct{})
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	for _, fn := range []func() error{
		func() error { return repo.ReverseOrderRebate(ctx, order.ID, 100, 100) },
		func() error { _, err := repo.ThawFrozenQuota(ctx, inviter); return err },
		func() error {
			_, _, err := repo.TransferQuotaToBalance(ctx, inviter)
			if err == service.ErrAffiliateQuotaEmpty {
				return nil
			}
			return err
		},
	} {
		wg.Add(1)
		go func(fn func() error) { defer wg.Done(); <-start; errs <- fn() }(fn)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	value := querySingleFloat(t, ctx, integrationEntClient, `SELECT u.balance+a.aff_quota+a.aff_frozen_quota FROM users u JOIN user_affiliates a ON a.user_id=u.id WHERE u.id=$1`, inviter)
	require.Equal(t, float64(0), value, "refund cannot both release and escape recovery")
}

func TestAffiliateSecurityAccrualAfterRefundDoesNotEscape(t *testing.T) {
	ctx := context.Background()
	repo, inviter, invitee := securityAffiliateUsers(t)
	order := securityAffiliateOrder(t, invitee)
	_, err := integrationEntClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(service.OrderStatusRefunded).Save(ctx)
	require.NoError(t, err)
	amount, err := repo.AccrueQuotaCapped(ctx, inviter, invitee, 20, 0, &order.ID, 100)
	require.NoError(t, err)
	require.Zero(t, amount)
}

func TestAffiliateSecurityHistoricalRefundMigration(t *testing.T) {
	ctx := context.Background()
	repo, inviter, invitee := securityAffiliateUsers(t)
	order := securityAffiliateOrder(t, invitee)
	_, err := repo.AccrueQuotaCapped(ctx, inviter, invitee, 20, 0, &order.ID, 100)
	require.NoError(t, err)
	_, _, err = repo.TransferQuotaToBalance(ctx, inviter)
	require.NoError(t, err)
	_, err = integrationEntClient.User.UpdateOneID(inviter).SetBalance(0).Save(ctx)
	require.NoError(t, err)
	_, err = integrationEntClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(service.OrderStatusPartiallyRefunded).SetRefundAmount(50).Save(ctx)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/260_security_payment_invariants.sql")
	require.NoError(t, err)
	body := string(migration)
	offset := strings.Index(body, "-- Reconcile already-finalized refunds")
	require.Greater(t, offset, 0)
	for i := 0; i < 2; i++ {
		_, err = integrationEntClient.ExecContext(ctx, body[offset:])
		require.NoError(t, err)
	}
	require.Equal(t, float64(-10), integrationEntClient.User.GetX(ctx, inviter).Balance)
	require.Equal(t, float64(10), querySingleFloat(t, ctx, integrationEntClient, `SELECT reversed_amount FROM user_affiliate_ledger WHERE source_order_id=$1 AND action='accrue'`, order.ID))
}

func TestAffiliateSecurityLegacyPendingRefundMigration(t *testing.T) {
	ctx := context.Background()
	_, _, invitee := securityAffiliateUsers(t)
	order := securityAffiliateOrder(t, invitee)
	_, err := integrationEntClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(service.OrderStatusRefundPending).SetRefundAmount(100).SetForceRefund(false).Save(ctx)
	require.NoError(t, err)
	_, err = integrationEntClient.PaymentAuditLog.Create().SetOrderID(fmt.Sprint(order.ID)).SetAction("REFUND_PENDING").SetDetail(`{"refundAmount":100,"force":false,"refundID":"legacy-rf","balanceRolledBack":100,"balanceDeducted":0,"deductionRollbackOK":true}`).Save(ctx)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/260_security_payment_invariants.sql")
	require.NoError(t, err)
	body := string(migration)
	offset := strings.Index(body, "-- Upgrade identifiable legacy balance-refund jobs")
	require.Greater(t, offset, 0)
	for i := 0; i < 2; i++ {
		_, err = integrationEntClient.ExecContext(ctx, body[offset:])
		require.NoError(t, err)
	}
	require.Equal(t, float64(-100), integrationEntClient.User.GetX(ctx, invitee).Balance)
	require.Equal(t, 1, querySingleInt(t, ctx, integrationEntClient, `SELECT count(*) FROM payment_audit_logs WHERE order_id=$1 AND action='REFUND_RESERVATION' AND detail::jsonb->>'state'='held'`, fmt.Sprint(order.ID)))
}

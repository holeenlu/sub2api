//go:build unit && integration

package service

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPaymentSecurityPostgresConcurrency(t *testing.T) {
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:18.1-alpine3.23", tcpostgres.WithDatabase("payment_security_test"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("local-test-only"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Terminate(ctx) })
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	require.NoError(t, client.Schema.Create(ctx))
	svc := &PaymentService{entClient: client}

	t.Run("distinct pending orders share allowance", func(t *testing.T) {
		u := client.User.Create().SetEmail("daily@example.com").SetPasswordHash("hash").SaveX(ctx)
		start := make(chan struct{})
		results := make(chan error, 12)
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := svc.createOrderInTx(ctx, CreateOrderRequest{UserID: u.ID, OrderType: payment.OrderTypeBalance, PaymentType: "stripe"}, &User{ID: u.ID, Email: u.Email}, nil, &PaymentConfig{DailyLimit: 100, MaxPendingOrders: 100}, 30, 30, 0, 30, nil)
				results <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else {
				require.Equal(t, "DAILY_LIMIT_EXCEEDED", infraerrors.Reason(err))
			}
		}
		require.Equal(t, 3, successes)
		_, err := client.ExecContext(ctx, `UPDATE payment_orders SET status='CANCELLED', created_at=NOW()-interval '2 day' WHERE user_id=$1`, u.ID)
		require.NoError(t, err)
		_, err = svc.createOrderInTx(ctx, CreateOrderRequest{UserID: u.ID, OrderType: payment.OrderTypeBalance, PaymentType: "stripe"}, &User{ID: u.ID, Email: u.Email}, nil, &PaymentConfig{DailyLimit: 100, MaxPendingOrders: 100}, 30, 30, 0, 30, nil)
		require.Equal(t, "DAILY_LIMIT_EXCEEDED", infraerrors.Reason(err), "cancellation and crossing midnight cannot free still-payable reservations")
	})

	t.Run("distinct refunds cannot share collateral", func(t *testing.T) {
		svc, p := securityRefundFixture(t, client, 100, "pg-refund-one")
		second := client.PaymentOrder.Create().SetUserID(p.Order.UserID).SetUserEmail("test@example.com").SetUserName("test").SetAmount(100).SetPayAmount(100).SetRechargeCode("pg-second").SetOutTradeNo("pg-second").SetPaymentType("stripe").SetPaymentTradeNo("").SetOrderType("balance").SetStatus(OrderStatusCompleted).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("localhost").SaveX(ctx)
		other := *p
		other.OrderID = second.ID
		other.Order = second
		results := make(chan error, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, plan := range []*RefundPlan{p, &other} {
			wg.Add(1)
			go func(plan *RefundPlan) { defer wg.Done(); <-start; results <- svc.reserveRefund(ctx, plan) }(plan)
		}
		close(start)
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else {
				require.Equal(t, "BALANCE_NOT_ENOUGH", infraerrors.Reason(err))
			}
		}
		require.Equal(t, 1, successes)
		require.Zero(t, client.User.GetX(ctx, p.Order.UserID).Balance)
	})

	t.Run("capture rolls back if persistence fails", func(t *testing.T) {
		svc, p := securityRefundFixture(t, client, 100, "pg-refund-rollback")
		_, err := client.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION reject_security_reservation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.order_id='%d' AND NEW.action='REFUND_RESERVATION' THEN RAISE EXCEPTION 'injected reservation failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_security_reservation BEFORE INSERT ON payment_audit_logs FOR EACH ROW EXECUTE FUNCTION reject_security_reservation()`, p.OrderID))
		require.NoError(t, err)
		err = svc.reserveRefund(ctx, p)
		require.ErrorContains(t, err, "injected reservation failure")
		require.Equal(t, float64(100), client.User.GetX(ctx, p.Order.UserID).Balance)
		require.Equal(t, OrderStatusCompleted, client.PaymentOrder.GetX(ctx, p.OrderID).Status)
	})
	t.Run("late payment competes with new checkout", func(t *testing.T) {
		for i := 0; i < 12; i++ {
			s := allowancePaymentService(client)
			u := client.User.Create().SetEmail(fmt.Sprintf("late-race-%d@example.com", i)).SetPasswordHash("hash").SaveX(ctx)
			old, err := allowanceCreateOrder(ctx, s, u)
			require.NoError(t, err)
			old, err = client.PaymentOrder.UpdateOneID(old.ID).SetStatus(OrderStatusExpired).SetExpiresAt(time.Now().Add(-48 * time.Hour)).Save(ctx)
			require.NoError(t, err)
			start := make(chan struct{})
			paymentDone := make(chan error, 1)
			type createResult struct {
				o   *dbent.PaymentOrder
				err error
			}
			created := make(chan createResult, 1)
			go func() { <-start; paymentDone <- s.toPaid(ctx, old, "late-race-trade", 100, payment.TypeStripe) }()
			go func() { <-start; o, err := allowanceCreateOrder(ctx, s, u); created <- createResult{o, err} }()
			close(start)
			require.NoError(t, <-paymentDone)
			next := <-created
			paid := client.PaymentOrder.GetX(ctx, old.ID)
			if next.err == nil {
				require.Equal(t, OrderStatusPaid, paid.Status)
				require.Zero(t, client.User.GetX(ctx, u.ID).Balance)
				require.Equal(t, "deferred", paid.ProviderSnapshot[paymentAllowanceStateKey])
				_, err = client.PaymentOrder.UpdateOneID(next.o.ID).SetStatus(OrderStatusCancelled).SetExpiresAt(time.Now().Add(-48 * time.Hour)).Save(ctx)
				require.NoError(t, err)
				_, err = s.ReconcilePendingPaymentOrders(ctx)
				require.NoError(t, err)
			} else {
				require.Equal(t, "DAILY_LIMIT_EXCEEDED", infraerrors.Reason(next.err))
				require.Equal(t, OrderStatusCompleted, paid.Status)
			}
			require.Equal(t, float64(100), client.User.GetX(ctx, u.ID).Balance)
			require.NoError(t, s.toPaid(ctx, old, "late-race-trade", 100, payment.TypeStripe))
			require.Equal(t, float64(100), client.User.GetX(ctx, u.ID).Balance)
		}
	})

}

package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/stretchr/testify/require"
)

type paymentDeliveryGate struct {
	*notificationEmailMemorySettingRepo
	entered chan context.Context
	release chan struct{}
	saved   atomic.Bool
}

func (r *paymentDeliveryGate) Set(ctx context.Context, key, value string) error {
	if strings.HasPrefix(key, notificationEmailDeliveryKeyPrefix) {
		r.entered <- ctx
		// A delayed driver can finish after cancellation. The lifecycle must
		// retain the real task, not assume cancellation already joined it.
		<-r.release
		err := r.notificationEmailMemorySettingRepo.Set(ctx, key, value)
		if err == nil {
			r.saved.Store(true)
		}
		return err
	}
	return r.notificationEmailMemorySettingRepo.Set(ctx, key, value)
}

func TestRootPaymentNotificationJoinsBeforeSubscriptionAndStorage(t *testing.T) {
	for _, action := range []string{"RECHARGE_SUCCESS", "SUBSCRIPTION_SUCCESS"} {
		t.Run(action, func(t *testing.T) {
			repo := &paymentDeliveryGate{
				notificationEmailMemorySettingRepo: newNotificationEmailMemorySettingRepo(),
				entered:                            make(chan context.Context, 1), release: make(chan struct{}),
			}
			smtp := startNotificationEmailTestSMTPServer(t)
			require.NoError(t, repo.SetMultiple(t.Context(), smtp.settings()))
			notification := NewNotificationEmailService(repo, NewEmailService(repo, nil))
			life := host.NewLifetime()
			payments := ProvidePaymentService(nil, nil, nil, nil, nil, nil, nil, nil, notification, life)
			order := &dbent.PaymentOrder{ID: 71, UserID: 5, UserEmail: "fixture@example.invalid", Amount: 12.34}
			var fulfillmentStops atomic.Int32
			var subscriptionClosed, storageClosed atomic.Bool
			life.Add(host.Component{
				Name: "last-payment-fulfillment", Phase: host.Producers, ShutdownOrder: 30, Constructed: true,
				Stop: func(context.Context) error {
					fulfillmentStops.Add(1)
					payments.dispatchPaymentFulfillmentNotification(order, action)
					return nil
				},
			})
			life.Add(host.Component{
				Name: "subscription-dependency", Phase: host.Producers, ShutdownOrder: 50, Constructed: true,
				Stop: func(context.Context) error {
					if !repo.saved.Load() {
						return errors.New("subscription closed before accepted payment notification")
					}
					subscriptionClosed.Store(true)
					return nil
				},
			})
			life.Add(host.Component{
				Name: "payment-storage", Phase: host.Infrastructure, Constructed: true,
				Stop: func(context.Context) error {
					if !repo.saved.Load() {
						return errors.New("storage closed before delivery persistence")
					}
					storageClosed.Store(true)
					return nil
				},
			})
			var releaseOnce sync.Once
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(repo.release) })
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, life.Cleanup(ctx))
			})
			require.NoError(t, life.Start(t.Context()))
			cleanupCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- life.Cleanup(cleanupCtx) }()

			var notificationCtx context.Context
			select {
			case notificationCtx = <-repo.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("actual SMTP delivery did not reach the persistence boundary")
			}
			require.Equal(t, int64(1), smtp.messageCount())
			cancel()
			select {
			case err := <-stopped:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("cleanup ignored its cancellation")
			}
			require.ErrorIs(t, notificationCtx.Err(), context.Canceled)
			require.False(t, subscriptionClosed.Load())
			require.False(t, storageClosed.Load())
			require.Contains(t, life.Pending(), "PaymentNotifications")
			payments.notificationTasks.mu.Lock()
			firstDone := payments.notificationTasks.done
			payments.notificationTasks.mu.Unlock()
			payments.dispatchPaymentFulfillmentNotification(order, action)

			releaseOnce.Do(func() { close(repo.release) })
			joined, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			require.NoError(t, life.Cleanup(joined))
			payments.notificationTasks.mu.Lock()
			secondDone := payments.notificationTasks.done
			payments.notificationTasks.mu.Unlock()
			require.Equal(t, firstDone, secondDone, "retry must retain the same completion owner")
			require.True(t, subscriptionClosed.Load())
			require.True(t, storageClosed.Load())
			require.Equal(t, int32(1), fulfillmentStops.Load(), "retrying shutdown must not replay fulfillment")
			require.Equal(t, int64(1), smtp.messageCount(), "closed intake must not send a second notification")
			event := NotificationEmailEventBalanceRechargeSuccess
			if action == "SUBSCRIPTION_SUCCESS" {
				event = NotificationEmailEventSubscriptionPurchaseSuccess
			}
			key := notificationEmailDeliveryKey(event, "payment_order", strconv.FormatInt(order.ID, 10), order.UserEmail, "")
			_, err := repo.GetValue(t.Context(), key)
			require.NoError(t, err)
		})
	}
}

type paymentNotificationUserGate struct {
	UserRepository
	entered chan int64
	release chan struct{}
}

func (r *paymentNotificationUserGate) GetByID(_ context.Context, id int64) (*User, error) {
	r.entered <- id
	<-r.release
	return &User{ID: id, Balance: 12.34}, nil
}

func TestPaymentNotificationOwnsCommittedOrderSnapshot(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	smtp := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), smtp.settings()))
	users := &paymentNotificationUserGate{entered: make(chan int64, 1), release: make(chan struct{})}
	svc := NewPaymentService(nil, nil, nil, nil, nil, nil, users, nil)
	svc.SetNotificationEmailService(NewNotificationEmailService(repo, NewEmailService(repo, nil)))
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(users.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.StopNotifications(ctx))
	})
	require.NoError(t, svc.StartNotifications(t.Context()))
	order := &dbent.PaymentOrder{ID: 72, UserID: 6, UserName: "Original",
		UserEmail: "original@example.invalid", Amount: 12.34}
	svc.dispatchPaymentFulfillmentNotification(order, "RECHARGE_SUCCESS")
	select {
	case id := <-users.entered:
		require.Equal(t, order.UserID, id)
	case <-time.After(time.Second):
		t.Fatal("notification did not enter the owned user lookup")
	}
	order.UserEmail, order.UserName, order.Amount = "changed@example.invalid", "Changed", 999
	releaseOnce.Do(func() { close(users.release) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, svc.DrainNotifications(ctx))
	require.Equal(t, int64(1), smtp.messageCount())
	require.Contains(t, smtp.lastMessage(), "original@example.invalid")
	require.NotContains(t, smtp.lastMessage(), "changed@example.invalid")
	require.Contains(t, smtp.lastMessageBody(t), "Original")
	require.Contains(t, smtp.lastMessageBody(t), "12.34")
	require.NotContains(t, smtp.lastMessageBody(t), "999")
}

func TestPaymentNotificationConstructionDoesNotStartBackgroundWork(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	smtp := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), smtp.settings()))
	svc := NewPaymentService(nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetNotificationEmailService(NewNotificationEmailService(repo, NewEmailService(repo, nil)))
	order := &dbent.PaymentOrder{ID: 73, UserEmail: "fixture@example.invalid"}
	svc.dispatchPaymentFulfillmentNotification(order, "RECHARGE_SUCCESS")
	require.False(t, svc.notificationTasks.accepting())
	require.NoError(t, svc.StopNotifications(t.Context()))
	require.ErrorIs(t, svc.StartNotifications(t.Context()), errMaintenanceStopped)
	require.Equal(t, int64(0), smtp.messageCount())
}

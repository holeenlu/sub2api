package service

import "context"

// StartNotifications is called by the root before payment producers start.
// Construction and synchronous payment operations do not start background work.
func (s *PaymentService) StartNotifications(ctx context.Context) error {
	return s.notificationTasks.start(ctx, nil)
}

// DrainNotifications follows the payment sweep's join and precedes subscription
// and storage shutdown. Pending SMTP/delivery writes retain the same completion
// owner across a deadline; no detached waiter can outlive its dependencies.
func (s *PaymentService) DrainNotifications(ctx context.Context) error {
	return s.notificationTasks.drain(ctx)
}

func (s *PaymentService) StopNotifications(ctx context.Context) error {
	return s.notificationTasks.stop(ctx)
}

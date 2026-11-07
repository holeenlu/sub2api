//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lifecycleEmailSender struct {
	started, release chan struct{}
	once             sync.Once
	sent             atomic.Int32
}

func (s *lifecycleEmailSender) send(ctx context.Context) error {
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
		s.sent.Add(1)
		return nil
	}
}
func (s *lifecycleEmailSender) SendVerifyCode(ctx context.Context, _, _ string, _ ...string) error {
	return s.send(ctx)
}
func (s *lifecycleEmailSender) SendPasswordResetEmailWithCooldown(ctx context.Context, _, _, _ string, _ ...string) error {
	return s.send(ctx)
}
func (s *lifecycleEmailSender) SendManagementSecurityChange(ctx context.Context, _, _ string, _, _ int64) error {
	return s.send(ctx)
}

func TestEmailQueueLifecycleDrainsAcceptedMailAndRejectsNewMail(t *testing.T) {
	sender := &lifecycleEmailSender{started: make(chan struct{}), release: make(chan struct{})}
	s := NewEmailQueueService(nil, 1)
	s.emailService = sender
	require.False(t, s.started)
	require.Nil(t, s.done)
	require.Error(t, s.EnqueueVerifyCode("before", "site"))
	require.NoError(t, s.Start(context.Background()))
	require.NoError(t, s.Start(context.Background()))
	for i := 0; i < 3; i++ {
		require.NoError(t, s.EnqueueVerifyCode("test@example.invalid", "site"))
	}
	<-sender.started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Drain(ctx), context.DeadlineExceeded)
	require.Error(t, s.EnqueuePasswordReset("test@example.invalid", "site", "reset"))
	close(sender.release)
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, s.Drain(bounded))
	require.NoError(t, s.Stop(bounded))
	require.EqualValues(t, 3, sender.sent.Load())
	require.Error(t, s.Start(context.Background()))
}

func TestEmailQueueLifecycleCanceledWorkIsNotReportedDelivered(t *testing.T) {
	sender := &lifecycleEmailSender{started: make(chan struct{}), release: make(chan struct{})}
	s := NewEmailQueueService(nil, 1)
	s.emailService = sender
	require.NoError(t, s.Start(context.Background()))
	require.NoError(t, s.EnqueueVerifyCode("test@example.invalid", "site"))
	<-sender.started
	require.NoError(t, s.EnqueueManagementSecurityChange("test@example.invalid", "site", 1, 2))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorContains(t, s.Stop(ctx), "unfinished")
	require.Zero(t, sender.sent.Load())
	select {
	case <-s.done:
	default:
		t.Fatal("worker not joined")
	}
	require.Error(t, s.EnqueueVerifyCode("later", "site"))
}

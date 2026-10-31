//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type lifecyclePricingRemote struct{ started chan struct{} }

func (c *lifecyclePricingRemote) FetchPricingJSON(ctx context.Context, _ string) ([]byte, error) {
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func (c *lifecyclePricingRemote) FetchHashText(ctx context.Context, _ string) (string, error) {
	return "", ctx.Err()
}
func TestPricingLifecycleStopCancelsAndJoinsAcceptedManualRefresh(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.RemoteURL = "https://pricing.example.invalid/pricing.json"
	cfg.Pricing.DataDir = t.TempDir()
	remote := &lifecyclePricingRemote{started: make(chan struct{})}
	s := NewPricingService(cfg, remote)
	require.False(t, s.started)
	require.Nil(t, s.done)
	require.NoError(t, s.Start(context.Background()))
	update := make(chan error, 1)
	go func() { update <- s.ForceUpdate() }()
	<-remote.started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Drain(ctx), context.DeadlineExceeded)
	require.Error(t, s.ForceUpdate())
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, s.Stop(bounded))
	require.ErrorIs(t, <-update, context.Canceled)
	require.Error(t, s.Start(context.Background()))
	require.Error(t, s.Initialize())
}

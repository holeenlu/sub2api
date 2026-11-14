package admin

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Test fixtures own the same explicit lifecycle as the application root.
// Always attempt Stop, including when a bounded Drain reports an error.
func startAdminFixtureService(t *testing.T, svc interface {
	Start(context.Context) error
	Drain(context.Context) error
	Stop(context.Context) error
}) {
	t.Helper()
	t.Cleanup(func() {
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
		err := svc.Drain(drainCtx)
		cancelDrain()
		if err != nil {
			t.Errorf("drain fixture service: %v", err)
		}
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelStop()
		require.NoError(t, svc.Stop(stopCtx), "stop and join fixture service")
	})
	require.NoError(t, svc.Start(t.Context()), "start fixture service")
}

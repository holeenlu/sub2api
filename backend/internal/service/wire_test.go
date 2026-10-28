package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

func TestProvideTimingWheelService_StartErrorRetainsRollbackOwner(t *testing.T) {
	original := newTimingScheduler
	t.Cleanup(func() { newTimingScheduler = original })
	failed := errors.New("scheduler construction failed")
	calls := 0
	newTimingScheduler = func() (*cron.Cron, error) { calls++; return nil, failed }
	life := host.NewLifetime()
	svc, err := ProvideLifetimeTimingWheelService(life)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Zero(t, calls, "construction must not create a scheduler")
	require.ErrorIs(t, life.Start(context.Background()), failed)
	require.Equal(t, 1, calls)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, life.Cleanup(ctx))
	require.Empty(t, life.Pending(), "failed Start must still release its retained owner")
}

func TestProvideTimingWheelService_ExplicitStartAndJoinedStop(t *testing.T) {
	life := host.NewLifetime()
	svc, err := ProvideLifetimeTimingWheelService(life)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.ErrorIs(t, svc.Schedule("before-start", time.Second, func() {}), errTimingWheelNotStarted)
	require.NoError(t, life.Start(context.Background()))
	require.NoError(t, svc.Schedule("pending", time.Hour, func() { t.Error("shutdown must cancel pending timers, not execute them") }))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, life.Cleanup(ctx))
	require.Empty(t, life.Pending())
	require.Error(t, svc.Schedule("after-stop", time.Second, func() {}))
}

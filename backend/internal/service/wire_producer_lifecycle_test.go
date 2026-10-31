package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/stretchr/testify/require"
)

func TestRootScheduledTestRunnerOwnsStartAndJoinedCleanup(t *testing.T) {
	life := host.NewLifetime()
	scheduled := &ScheduledTestService{}
	runner := ProvideLifetimeScheduledTestRunnerService(nil, scheduled, nil, nil, nil, life)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, life.Cleanup(ctx))
	})
	require.Nil(t, runner.cron, "constructing the graph must not start diagnostic polling")

	router := http.NewServeMux()
	router.HandleFunc("/diagnostic-check", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	scheduled.SetDiagnosticRouter(router)
	require.Nil(t, runner.cron, "router injection must not start the scheduler")
	response := httptest.NewRecorder()
	scheduled.diagnosticRouter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/diagnostic-check", nil))
	require.Equal(t, http.StatusNoContent, response.Code)

	var storageClosed atomic.Bool
	life.Add(host.Component{Name: "scheduled-test-storage", Phase: host.Infrastructure, Constructed: true, Stop: func(context.Context) error {
		storageClosed.Store(true)
		return nil
	}})
	require.NoError(t, life.Start(context.Background()))
	require.NotNil(t, runner.cron, "the real root lifetime must start the scheduler")
	require.Len(t, runner.cron.Entries(), 2, "both scheduled plans and queued diagnostics remain registered")
	require.Same(t, router, scheduled.diagnosticRouter)

	// Admit through the real callback owner and retain its completion to prove
	// a timed-out cleanup cannot close storage underneath accepted work.
	workCtx, finish, err := runner.work.begin()
	require.NoError(t, err)
	defer finish()
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, life.Cleanup(short), context.DeadlineExceeded)
	require.ErrorIs(t, context.Cause(workCtx), errBackgroundWorkStopped)
	require.False(t, storageClosed.Load())
	require.Contains(t, life.Pending(), "ScheduledTestRunnerService")
	_, _, err = runner.work.begin()
	require.ErrorIs(t, err, errBackgroundWorkStopped, "new callbacks must be rejected after drain")

	finish()
	joined, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, life.Cleanup(joined))
	require.True(t, storageClosed.Load())
	require.Empty(t, life.Pending())
}

func TestRootBalanceNotificationLifetimeFollowsFinalUsage(t *testing.T) {
	life := host.NewLifetime()
	notify := ProvideBalanceNotifyService(nil, nil, nil, nil, life)
	entered, release := make(chan context.Context, 1), make(chan struct{})
	var released sync.Once
	var notified, storageClosed atomic.Bool
	t.Cleanup(func() {
		released.Do(func() { close(release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, notify.Stop(ctx))
	})
	life.Add(host.Component{Name: "final-settlement", Phase: host.Usage, ShutdownOrder: 30, Constructed: true, Stop: func(context.Context) error {
		// The real notifier owns this handoff before the Usage stop returns.
		notify.submitNotification("root-lifecycle", func(ctx context.Context) { entered <- ctx; <-release; notified.Store(true) })
		return nil
	}})
	life.Add(host.Component{Name: "test-storage", Phase: host.Infrastructure, Constructed: true, Stop: func(context.Context) error {
		require.True(t, notified.Load(), "storage must outlive final settlement notifications")
		storageClosed.Store(true)
		return nil
	}})
	require.NoError(t, life.Start(context.Background()))
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, life.Cleanup(short), context.DeadlineExceeded)
	select {
	case ctx := <-entered:
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("notification owner was closed before final Usage handoff")
	}
	require.False(t, storageClosed.Load())
	require.Contains(t, life.Pending(), "BalanceNotifyService")
	released.Do(func() { close(release) })
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, life.Cleanup(join))
	require.True(t, storageClosed.Load())
}

func TestRootCloudProviderDefersRecoveryBindingUntilReadiness(t *testing.T) {
	life := host.NewLifetime()
	gateway := &OpenAIGatewayService{}
	cloud := ProvideOpenAICloudService(nil, nil, nil, gateway, life)
	require.Nil(t, gateway.cloudService, "construction must not invoke the recovery-starting setter")
	require.Nil(t, gateway.cloudSessions)
	life.Add(host.Component{Name: "subscription-ready", Phase: host.Producers, StartOrder: 10, Start: func(context.Context) error {
		require.Nil(t, gateway.cloudService, "subscription handle must be ready before Cloud recovery starts")
		return nil
	}, Stop: func(context.Context) error { return nil }})
	life.Add(host.Component{Name: "usage-ready", Phase: host.Usage, StartOrder: 20, Start: func(context.Context) error {
		require.Nil(t, gateway.cloudService)
		return nil
	}, Stop: func(context.Context) error { return nil }})
	require.NoError(t, life.Start(context.Background()))
	require.Same(t, cloud, gateway.cloudService)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, life.Cleanup(ctx))
}

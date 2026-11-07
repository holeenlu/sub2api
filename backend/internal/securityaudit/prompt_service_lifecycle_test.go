package securityaudit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type promptLifecycleConfig struct {
	*fakeConfigStore
	shutdowns    atomic.Int32
	shutdownErr  error
	startEntered chan context.Context
	startRelease chan struct{}
}

func (c *promptLifecycleConfig) Start(ctx context.Context) error {
	if c.startEntered != nil {
		c.startEntered <- ctx
		<-c.startRelease
		return ctx.Err()
	}
	return nil
}

func (c *promptLifecycleConfig) Shutdown(context.Context) error {
	c.shutdowns.Add(1)
	return c.shutdownErr
}

type promptLifecycleBlockedEnqueue struct {
	*fakeJobRepository
	entered chan context.Context
	release chan struct{}
}

func (r *promptLifecycleBlockedEnqueue) CreateStagingWithCapacity(ctx context.Context, _ PromptSnapshot, _ int64, _, _ int) (*Job, error) {
	r.entered <- ctx
	<-r.release
	return nil, ctx.Err()
}

func newPromptLifecycleService(config ConfigStore, repo JobRepository) *PromptService {
	payload := &fakePayloadStore{}
	return &PromptService{
		config: config, enqueuer: NewEnqueuer(config, repo, payload),
		runner:       NewRunner(config, repo, payload, NewOpenAICompatibleScanner(), nil),
		enqueueSlots: make(chan struct{}, 2),
	}
}

func TestPromptServiceLifecycleRetainsBlockedEnqueueCompletion(t *testing.T) {
	config := &promptLifecycleConfig{fakeConfigStore: &fakeConfigStore{cfg: asyncConfig(), active: true}}
	repo := &promptLifecycleBlockedEnqueue{fakeJobRepository: &fakeJobRepository{}, entered: make(chan context.Context, 1), release: make(chan struct{})}
	svc := newPromptLifecycleService(config, repo)
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(repo.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Shutdown(ctx))
	})
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Enqueue(context.Background(), asyncRequest()))
	var enqueueCtx context.Context
	select {
	case enqueueCtx = <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("accepted enqueue did not reach real staging write")
	}
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Shutdown(short), context.DeadlineExceeded)
	require.ErrorIs(t, enqueueCtx.Err(), context.Canceled)
	done := svc.shutdownDone
	require.NotNil(t, done)
	require.Zero(t, config.shutdowns.Load(), "config must outlive the accepted write")
	require.ErrorIs(t, svc.Enqueue(context.Background(), asyncRequest()), errPromptServiceStopped)
	require.ErrorIs(t, svc.Start(context.Background()), errPromptServiceStopped)
	second, stopSecond := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stopSecond()
	require.ErrorIs(t, svc.Shutdown(second), context.DeadlineExceeded)
	require.Equal(t, done, svc.shutdownDone, "retry must retain the same shutdown execution")
	require.Zero(t, config.shutdowns.Load())
	release.Do(func() { close(repo.release) })
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, svc.Shutdown(join))
	require.NoError(t, svc.Shutdown(join))
	require.EqualValues(t, 1, config.shutdowns.Load())
	require.Empty(t, svc.enqueueSlots)
}

func TestPromptServiceLifecycleRetainsDependencyShutdownError(t *testing.T) {
	failure := errors.New("configuration subscription close failed")
	config := &promptLifecycleConfig{fakeConfigStore: &fakeConfigStore{}, shutdownErr: failure}
	svc := &PromptService{config: config}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorIs(t, svc.Shutdown(ctx), failure)
	done := svc.shutdownDone
	require.ErrorIs(t, svc.Shutdown(ctx), failure)
	require.Equal(t, done, svc.shutdownDone)
	require.EqualValues(t, 1, config.shutdowns.Load())
}

func TestPromptServiceLifecycleJoinsStartupBeforeClosingDependencies(t *testing.T) {
	config := &promptLifecycleConfig{fakeConfigStore: &fakeConfigStore{}, startEntered: make(chan context.Context, 1), startRelease: make(chan struct{})}
	svc := newPromptLifecycleService(config, &fakeJobRepository{})
	startResult := make(chan error, 1)
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(config.startRelease) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Shutdown(ctx))
	})
	go func() { startResult <- svc.Start(context.Background()) }()
	var starting context.Context
	select {
	case starting = <-config.startEntered:
	case <-time.After(time.Second):
		t.Fatal("configuration startup did not enter")
	}
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Shutdown(short), context.DeadlineExceeded)
	require.ErrorIs(t, starting.Err(), context.Canceled)
	require.Zero(t, config.shutdowns.Load())
	release.Do(func() { close(config.startRelease) })
	select {
	case err := <-startResult:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("startup failed to return after release")
	}
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, svc.Shutdown(join))
	require.EqualValues(t, 1, config.shutdowns.Load())
}

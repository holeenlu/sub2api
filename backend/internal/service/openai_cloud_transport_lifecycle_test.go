package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cloudBlockedLifecycleConn struct {
	*cloudRPCFakeConn
	readEntered, readRelease   chan struct{}
	closeEntered, closeRelease chan struct{}
	readOnce, closeSignal      sync.Once
}

func (c *cloudBlockedLifecycleConn) ReadMessage(ctx context.Context) ([]byte, error) {
	if c.readEntered != nil {
		c.readOnce.Do(func() { close(c.readEntered) })
		<-c.readRelease
	}
	return c.cloudRPCFakeConn.ReadMessage(ctx)
}
func (c *cloudBlockedLifecycleConn) CloseNow() error {
	if c.closeEntered != nil {
		c.closeSignal.Do(func() { close(c.closeEntered) })
		<-c.closeRelease
	}
	return c.cloudRPCFakeConn.CloseNow()
}

func TestCloudRPCStopTimeoutDoesNotPretendBlockedWorkersJoined(t *testing.T) {
	for _, blocked := range []string{"read", "close"} {
		t.Run(blocked, func(t *testing.T) {
			wire := &cloudBlockedLifecycleConn{cloudRPCFakeConn: newCloudRPCFakeConn()}
			entered, release := make(chan struct{}), make(chan struct{})
			if blocked == "read" {
				wire.readEntered, wire.readRelease = entered, release
			} else {
				wire.closeEntered, wire.closeRelease = entered, release
			}
			rpc := newCloudRPC(t.Context(), wire)
			t.Cleanup(func() { _ = rpc.Close() })
			var once sync.Once
			unlock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unlock)
			if blocked == "read" {
				awaitCloudLifecycleSignal(t, entered)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			require.ErrorIs(t, rpc.Stop(ctx), context.DeadlineExceeded)
			awaitCloudLifecycleSignal(t, entered)
			select {
			case <-rpc.done:
				t.Fatal("RPC reported completion with a blocked worker")
			default:
			}
			unlock()
			require.NoError(t, rpc.Stop(t.Context()))
			require.Zero(t, rpc.remaining.Load())
		})
	}
}

func TestCloudRPCReaderFailureStopsAndJoinsWithoutSelfWait(t *testing.T) {
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(t.Context(), wire)
	t.Cleanup(func() { _ = rpc.Close() })
	wire.reads <- []byte("not json")
	select {
	case <-rpc.done:
	case <-time.After(time.Second):
		t.Fatal("reader failure deadlocked shutdown")
	}
	require.NoError(t, rpc.Wait(t.Context()))
	require.Error(t, rpc.Err())
	require.Zero(t, rpc.remaining.Load())
}

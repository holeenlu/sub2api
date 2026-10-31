package service

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResponsesEventWriterKeepaliveStopIsIdempotent(t *testing.T) {
	var calls atomic.Int32
	wrote := make(chan struct{}, 1)
	writer := &responsesEventWriter{maxFrameBytes: 256, write: func([]byte) error {
		calls.Add(1)
		select {
		case wrote <- struct{}{}:
		default:
		}
		return nil
	}}
	stop := writer.startKeepalive(time.Millisecond)
	defer stop()
	select {
	case <-wrote:
	case <-time.After(time.Second):
		t.Fatal("keepalive never started")
	}
	var joined sync.WaitGroup
	for range 2 {
		joined.Add(1)
		go func() { defer joined.Done(); stop() }()
	}
	joined.Wait()
	before := calls.Load()
	<-time.After(3 * time.Millisecond)
	require.Equal(t, before, calls.Load(), "no timer write may survive joined stop")
}

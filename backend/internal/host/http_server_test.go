package host

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServeHTTPBindFailureReturnsToCleanup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	server := &http.Server{Addr: listener.Addr().String()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cleaned := false
	err = func() error { defer func() { cleaned = true }(); return ServeHTTP(ctx, server, time.Second) }()
	require.Error(t, err)
	require.True(t, cleaned)
}

func TestServeHTTPDrainsExistingHandler(t *testing.T) {
	serving, requests := make(chan string, 1), make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requests)
		<-release
		_, _ = w.Write([]byte("complete"))
	})}
	server.BaseContext = func(l net.Listener) context.Context { serving <- l.Addr().String(); return context.Background() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- ServeHTTP(ctx, server, time.Second) }()
	address := <-serving
	clientDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + address)
		if err == nil {
			body, e := io.ReadAll(response.Body)
			response.Body.Close()
			err = e
			if string(body) != "complete" {
				err = errors.New("truncated response")
			}
		}
		clientDone <- err
	}()
	<-requests
	cancel()
	select {
	case <-done:
		t.Fatal("returned before accepted HTTP handler drained")
	default:
	}
	close(release)
	require.NoError(t, <-clientDone)
	require.NoError(t, <-done)
}

func TestServeHTTPDeadlineClosesTransportAndCancelsHandler(t *testing.T) {
	serving, started, finished := make(chan string, 1), make(chan struct{}), make(chan struct{})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(finished) })}
	server.BaseContext = func(l net.Listener) context.Context { serving <- l.Addr().String(); return context.Background() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeHTTP(ctx, server, 10*time.Millisecond) }()
	address := <-serving
	var client sync.WaitGroup
	client.Add(1)
	go func() {
		defer client.Done()
		res, _ := http.Get("http://" + address)
		if res != nil {
			res.Body.Close()
		}
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.DeadlineExceeded)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("HTTP close did not cancel request")
	}
	client.Wait()
}

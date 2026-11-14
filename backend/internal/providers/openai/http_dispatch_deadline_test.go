package openai_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

func TestOpenSendPermissionExpiresDuringConnectionWait(t *testing.T) {
	a := newAuthority()
	a.validation.ValidUntil = time.Now().Add(250 * time.Millisecond)
	d, _ := dispatcher(t, a, 2)
	var dials atomic.Int32
	base := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		dials.Add(1)
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}}
	t.Cleanup(base.CloseIdleConnections)
	p := prepare(t, t.Context(), specFor(t, http.MethodPost, "http://127.0.0.1:1/responses", "body"), &localTransport{base: base})
	permit := admit(t, d, p)
	result, err := native.Open(t.Context(), p, permit)
	if !errors.Is(err, execution.ErrDispatchExpired) {
		t.Fatalf("connection wait outlived permission: %v", err)
	}
	if dials.Load() != 1 {
		t.Fatalf("unexpected dial count %d", dials.Load())
	}
	_ = result.Close()
}

func TestOpenSendDeadlineDoesNotTruncateStartedResponse(t *testing.T) {
	deadline := time.Now().Add(350 * time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		timer := time.NewTimer(time.Until(deadline) + 60*time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			_, _ = io.WriteString(w, "complete response")
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	base := &http.Transport{}
	t.Cleanup(base.CloseIdleConnections)
	a := newAuthority()
	a.validation.ValidUntil = deadline
	d, _ := dispatcher(t, a, 1)
	p := prepare(t, t.Context(), specFor(t, http.MethodPost, server.URL, "body"), &localTransport{base: base})
	result, err := native.Open(t.Context(), p, admit(t, d, p))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(result.Response.Body)
	_ = result.Close()
	if err != nil || string(body) != "complete response" {
		t.Fatalf("response incorrectly inherited send deadline: %q %v", body, err)
	}
}

func TestOpenRechecksRevocationAfterAcquiringConnection(t *testing.T) {
	a := newAuthority()
	d, _ := dispatcher(t, a, 1)
	var written atomic.Bool
	transport := transportFunc(func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
		a.mu.Lock()
		a.denied = errors.New("fixture revoked during connection wait")
		a.mu.Unlock()
		trace := httptrace.ContextClientTrace(req.Context())
		trace.GotConn(httptrace.GotConnInfo{})
		if err := req.Context().Err(); err != nil {
			return nil, context.Cause(req.Context())
		}
		written.Store(true)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("unexpected"))}, nil
	})
	p := prepare(t, t.Context(), specFor(t, http.MethodPost, "https://example.invalid/responses", "body"), transport)
	_, err := native.Open(t.Context(), p, admit(t, d, p))
	if err == nil || written.Load() {
		t.Fatalf("revoked connection was sent: written=%v err=%v", written.Load(), err)
	}
}

func TestOpenRejectsTransportReplayAfterHeadersWereWritten(t *testing.T) {
	a := newAuthority()
	d, _ := dispatcher(t, a, 2)
	var writes atomic.Int32
	transport := transportFunc(func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
		trace := httptrace.ContextClientTrace(req.Context())
		trace.GotConn(httptrace.GotConnInfo{})
		trace.WroteHeaders()
		writes.Add(1)
		trace.GotConn(httptrace.GotConnInfo{})
		if err := req.Context().Err(); err != nil {
			return nil, context.Cause(req.Context())
		}
		writes.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("unexpected"))}, nil
	})
	p := prepare(t, t.Context(), specFor(t, http.MethodGet, "https://example.invalid/control", ""), transport)
	_, err := native.Open(t.Context(), p, admit(t, d, p))
	if !errors.Is(err, execution.ErrSendPermitConsumed) || writes.Load() != 1 {
		t.Fatalf("implicit replay escaped: writes=%d err=%v", writes.Load(), err)
	}
}

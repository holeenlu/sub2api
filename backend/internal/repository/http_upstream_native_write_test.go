package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

var nativeWriteDenied = errors.New("fixture revoked at actual connection handoff")

type nativeWriteAuthority struct {
	checks atomic.Int32
	mode   string
	cancel context.CancelFunc
}

func (a *nativeWriteAuthority) RevalidateDispatch(ctx context.Context, identity model.PreparedIdentity) (execution.DispatchValidation, error) {
	proof, err := (nativeCompositionAuthority{}).RevalidateDispatch(ctx, identity)
	if a.checks.Add(1) == 3 {
		switch a.mode {
		case "revoked":
			return execution.DispatchValidation{}, nativeWriteDenied
		case "expired":
			proof.ValidUntil = time.Now().Add(-time.Second)
		case "canceled":
			a.cancel()
		}
	}
	return proof, err
}

// Compose the actual execution permit, provider, controlled repository pool,
// and network writer. Only live-authority data and upstream are local fixtures.
func nativeWriteAttempt(t *testing.T, s *httpUpstreamService, options native.TransportOptions, method, target, mode string, kind model.BodyKind) (*native.Result, error, httptrace.GotConnInfo) {
	t.Helper()
	ctx, cancel := context.WithCancel(nativeCompositionContext(t, options.Profile))
	t.Cleanup(cancel)
	connections := make(chan httptrace.GotConnInfo, 4)
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		connections <- info
		if mode == "observer_canceled" {
			cancel()
		}
	}})
	payload := []byte(`{"input":"local fixture"}`)
	if method == http.MethodGet || method == http.MethodDelete {
		payload = nil
	}
	source := &nativeSourceBody{Reader: bytes.NewReader(payload)}
	req, err := http.NewRequestWithContext(ctx, method, target, source)
	require.NoError(t, err)
	if kind == model.BodyReplayable {
		req.GetBody = func() (io.ReadCloser, error) {
			t.Error("caller replay must not be used")
			return nil, errors.New("untrusted replay")
		}
	}
	p := nativeCompositionPrepare(t, s, req, options, kind)
	authority := &nativeWriteAuthority{mode: mode, cancel: cancel}
	budget := execution.NewAttemptBudget(execution.AttemptLimits{Total: 1, PerOwner: 1})
	dispatcher, err := execution.NewDispatcher(ctx, "native-composition", budget, execution.NewFirstOutput(time.Now()), authority)
	require.NoError(t, err)
	result, sendErr := native.Open(ctx, p, nativeCompositionAdmit(t, dispatcher, p))
	require.Equal(t, 1, budget.Snapshot().Attempts)
	if mode != "allow" {
		want := error(nativeWriteDenied)
		if mode == "expired" {
			want = execution.ErrDispatchExpired
		} else if mode == "canceled" || mode == "observer_canceled" {
			want = context.Canceled
		}
		require.ErrorIs(t, sendErr, want)
		require.NoError(t, result.Close())
		require.EqualValues(t, 1, source.closed.Load())
		// A closed connection is not proof about every preceding handoff.
		require.Equal(t, model.SendAmbiguous, result.SendState)
	}
	select {
	case info := <-connections:
		require.Empty(t, connections, "one controlled send must not hand off twice")
		return result, sendErr, info
	case <-time.After(time.Second):
		t.Fatal("fixture did not reach the actual connection boundary")
		return nil, sendErr, httptrace.GotConnInfo{}
	}
}

func TestNativeWriteGuardHTTP1RejectsBeforeRealWrite(t *testing.T) {
	for _, lane := range []string{"http", "https", "http_proxy", "https_socks5"} {
		t.Run(lane, func(t *testing.T) {
			var denied, allowed atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/denied" {
					denied.Add(1)
				} else {
					allowed.Add(1)
				}
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = io.WriteString(w, "ok")
			}))
			if lane == "https" || lane == "https_socks5" {
				server.StartTLS()
			} else {
				server.Start()
			}
			t.Cleanup(server.Close)
			s := NewHTTPUpstream(nil).(*httpUpstreamService)
			options := native.TransportOptions{Profile: native.ProfileDefault, AccountID: 31, AccountConcurrency: 2}
			target := server.URL
			if lane == "http_proxy" {
				options.ProxyURL, target = server.URL, "http://native-fixture.invalid"
			} else if lane == "https_socks5" {
				options.ProxyURL, _ = startTestSOCKS5Proxy(t)
				options.Profile = native.ProfileOpenAI
			}
			_, counter := nativeCompositionPool(t, s, server, options)
			positive := func(method string) {
				result, err, _ := nativeWriteAttempt(t, s, options, method, target+"/allowed", "allow", model.BodyReplayable)
				require.NoError(t, err)
				require.Equal(t, 1, result.Response.ProtoMajor)
				body, err := io.ReadAll(result.Response.Body)
				require.NoError(t, err)
				require.Equal(t, "ok", string(body))
				require.NoError(t, result.Close())
			}
			positive(http.MethodPost) // Prime a reusable, previously healthy connection.
			attempts := 20
			if lane == "http" {
				attempts = 200 // Keep the original 2/200 reproduction.
			}
			for i := 0; i < attempts; i++ {
				kind := model.BodyReplayable
				if i%2 != 0 {
					kind = model.BodyExclusiveStream
				}
				_, _, info := nativeWriteAttempt(t, s, options, http.MethodPost, target+"/denied", "revoked", kind)
				if i == 0 {
					require.True(t, info.Reused, "revocation must also fence reused HTTP/1 connections")
				}
			}
			for _, mode := range []string{"expired", "canceled", "observer_canceled"} {
				nativeWriteAttempt(t, s, options, http.MethodPost, target+"/denied", mode, model.BodyReplayable)
			}
			positive(http.MethodDelete) // Bodyless controls remain usable after rejection.
			server.Close()
			require.Zero(t, denied.Load(), "denied business requests reached the actual wire")
			require.EqualValues(t, 2, allowed.Load())
			require.EqualValues(t, attempts+5, counter.calls.Load())
			requireNoUpstreamInFlight(t, s)
		})
	}
}

func TestNativeWriteGuardHTTP2RejectsOnlyItsStream(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		name := "direct"
		if proxy {
			name = "socks5"
		}
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			releasePeer := sync.OnceFunc(func() { close(release) })
			defer releasePeer()
			var denied, peerCanceled atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("X-Fixture-Connection", r.RemoteAddr)
				if r.URL.Path == "/live" {
					_, _ = io.WriteString(w, "prefix-")
					w.(http.Flusher).Flush()
					select {
					case <-release:
						_, _ = io.WriteString(w, "suffix")
					case <-r.Context().Done():
						peerCanceled.Add(1)
					}
					return
				}
				if r.URL.Path == "/denied" {
					denied.Add(1)
				}
				_, _ = io.WriteString(w, "ok")
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			t.Cleanup(server.Close)
			cfg := &config.Config{}
			cfg.Gateway.OpenAIHTTP2.Enabled = true
			cfg.Gateway.OpenAIHTTP2.AllowProxyFallbackToHTTP1 = true
			cfg.Gateway.OpenAIHTTP2.FallbackErrorThreshold = 100
			s := NewHTTPUpstream(cfg).(*httpUpstreamService)
			options := native.TransportOptions{Profile: native.ProfileOpenAI, AccountID: 35, AccountConcurrency: 2}
			if proxy {
				options.ProxyURL, _ = startTestSOCKS5Proxy(t)
			}
			entry, counter := nativeCompositionPool(t, s, server, options)
			health := s.getOrCreateOpenAIHTTP2FallbackState(entry.feedbackKey)
			peer, err, peerConn := nativeWriteAttempt(t, s, options, http.MethodPost, server.URL+"/live", "allow", model.BodyReplayable)
			require.NoError(t, err)
			t.Cleanup(func() { _ = peer.Close() })
			require.Equal(t, 2, peer.Response.ProtoMajor, "the isolation proof requires actual HTTP/2")
			for _, mode := range []string{"revoked", "expired", "canceled", "observer_canceled"} {
				for i := 0; i < 20; i++ {
					_, _, info := nativeWriteAttempt(t, s, options, http.MethodPost, server.URL+"/denied", mode, model.BodyExclusiveStream)
					require.Same(t, peerConn.Conn, info.Conn, "denied request must share the live peer's connection")
				}
			}
			health.mu.Lock()
			healthFailures := health.errorCount
			health.mu.Unlock()
			require.Zero(t, healthFailures, "local rejection must not penalize upstream HTTP/2 health")
			control, err, conn := nativeWriteAttempt(t, s, options, http.MethodGet, server.URL+"/allowed", "allow", model.BodyReplayable)
			require.NoError(t, err)
			require.Same(t, peerConn.Conn, conn.Conn)
			require.Equal(t, 2, control.Response.ProtoMajor)
			require.Equal(t, peer.Response.Header.Get("X-Fixture-Connection"), control.Response.Header.Get("X-Fixture-Connection"))
			_, err = io.ReadAll(control.Response.Body)
			require.NoError(t, err)
			require.NoError(t, control.Close())
			releasePeer()
			body, err := io.ReadAll(peer.Response.Body)
			require.NoError(t, err)
			require.Equal(t, "prefix-suffix", string(body))
			require.NoError(t, peer.Close())
			require.Zero(t, denied.Load())
			require.Zero(t, peerCanceled.Load())
			require.EqualValues(t, 82, counter.calls.Load())
			requireNoUpstreamInFlight(t, s)
		})
	}
}

func TestNativeWriteGuardPreventsImplicitHTTP1ControlReplay(t *testing.T) {
	var calls atomic.Int32
	firstWritten := make(chan struct{})
	markWritten := sync.OnceFunc(func() { close(firstWritten) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/warm" {
			_, _ = io.WriteString(w, "warm")
			return
		}
		calls.Add(1)
		select {
		case <-firstWritten:
		case <-r.Context().Done():
			return
		}
		// A bodyless GET on a previously reused connection is replayable to
		// net/http after this EOF. The second GotConn must not write it again.
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(server.Close)
	s := NewHTTPUpstream(nil).(*httpUpstreamService)
	options := native.TransportOptions{Profile: native.ProfileDefault, AccountID: 39, AccountConcurrency: 1}
	_, counter := nativeCompositionPool(t, s, server, options)
	warm, err, _ := nativeWriteAttempt(t, s, options, http.MethodGet, server.URL+"/warm", "allow", model.BodyReplayable)
	require.NoError(t, err)
	_, err = io.ReadAll(warm.Response.Body)
	require.NoError(t, err)
	require.NoError(t, warm.Close())
	var handoffs atomic.Int32
	ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { handoffs.Add(1) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				markWritten()
			}
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/control", nil)
	require.NoError(t, err)
	p := nativeCompositionPrepare(t, s, req, options, model.BodyReplayable)
	dispatcher, budget := nativeCompositionDispatcher(t, ctx, 1)
	result, err := native.Open(ctx, p, nativeCompositionAdmit(t, dispatcher, p))
	require.ErrorIs(t, err, execution.ErrSendPermitConsumed)
	require.NoError(t, result.Close())
	server.Close()
	require.EqualValues(t, 2, handoffs.Load(), "the fixture must reach the attempted implicit reconnect")
	require.EqualValues(t, 1, calls.Load(), "the transport's reconnect must not send the control request twice")
	require.Equal(t, model.SendSent, result.SendState, "rejected replay must not erase the first send")
	require.Equal(t, 1, budget.Snapshot().Attempts)
	require.EqualValues(t, 2, counter.calls.Load(), "warmup and one controlled client call only")
	requireNoUpstreamInFlight(t, s)
}

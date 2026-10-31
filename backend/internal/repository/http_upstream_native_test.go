package repository

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

type nativeSourceBody struct {
	*bytes.Reader
	closed atomic.Int64
}

// These tests compose the real permit, provider and repository. Only dispatch
// authority uses synthetic local principals; it is not production auth evidence.
type nativeCompositionAuthority struct{}

func (nativeCompositionAuthority) RevalidateDispatch(context.Context, model.PreparedIdentity) (execution.DispatchValidation, error) {
	return execution.DispatchValidation{Authorization: sha256.Sum256([]byte("native test scope")), Leases: sha256.Sum256([]byte("native test owned lease"))}, nil
}

func nativeCompositionDispatcher(t *testing.T, ctx context.Context, limit int) (*execution.Dispatcher, *execution.AttemptBudget) {
	t.Helper()
	budget := execution.NewAttemptBudget(execution.AttemptLimits{Total: limit, PerOwner: limit, RetryWait: time.Second})
	d, err := execution.NewDispatcher(ctx, "native-composition", budget, execution.NewFirstOutput(time.Now()), nativeCompositionAuthority{})
	require.NoError(t, err)
	return d, budget
}

func nativeCompositionPrepare(t *testing.T, s *httpUpstreamService, req *http.Request, options native.TransportOptions, kind model.BodyKind) *native.PreparedHTTP {
	t.Helper()
	wire, err := s.PrepareNativeRequest(req, 4096)
	require.NoError(t, err)
	identity := model.PreparedIdentity{OperationID: "native-composition", ResourceID: options.AccountID,
		Owner: model.ResourceOwner{AccountID: options.AccountID}, UserID: 11, APIKeyID: 12, GroupID: 13, GroupBound: true,
		Mode: "native", Transport: "http", GrantDigest: sha256.Sum256([]byte("native test grant")), BodyKind: kind}
	p, err := native.PrepareHTTP(wire.Context(), native.Spec{Request: wire, Identity: identity, Transport: options, MaxReplayBodyBytes: 4096}, s)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, p.Close()) })
	return p
}

func nativeCompositionAdmit(t *testing.T, d *execution.Dispatcher, p *native.PreparedHTTP) *execution.SendPermit {
	t.Helper()
	permit, err := d.Admit(t.Context(), p.Identity(), model.AttemptTarget{Owner: p.Identity().Owner, Mode: "http", Endpoint: "/responses", Limit: 2})
	require.NoError(t, err)
	return permit
}

type nativeCompositionCounter struct {
	base   http.RoundTripper
	calls  atomic.Int64
	closes atomic.Int64
}

func (c *nativeCompositionCounter) RoundTrip(req *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	response, err := c.base.RoundTrip(req)
	if response != nil && response.Body != nil {
		response.Body = &nativeCompositionBody{ReadCloser: response.Body, closed: &c.closes}
	}
	return response, err
}
func (c *nativeCompositionCounter) CloseIdleConnections() {
	if v, ok := c.base.(interface{ CloseIdleConnections() }); ok {
		v.CloseIdleConnections()
	}
}

type nativeCompositionBody struct {
	io.ReadCloser
	closed *atomic.Int64
}

func (b *nativeCompositionBody) Close() error { b.closed.Add(1); return b.ReadCloser.Close() }

func nativeCompositionPool(t *testing.T, s *httpUpstreamService, server *httptest.Server, options native.TransportOptions) (*upstreamClientEntry, *nativeCompositionCounter) {
	t.Helper()
	profile := service.HTTPUpstreamProfileDefault
	if options.Profile == native.ProfileOpenAI {
		profile = service.HTTPUpstreamProfileOpenAI
	}
	entry, err := s.getClientEntry(options.ProxyURL, options.AccountID, options.AccountConcurrency, profile, false, false, server.URL)
	require.NoError(t, err)
	transport, ok := entry.client.Transport.(*http.Transport)
	require.True(t, ok)
	if server.TLS != nil {
		roots := x509.NewCertPool()
		roots.AddCert(server.Certificate())
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	counter := &nativeCompositionCounter{base: transport}
	entry.client.Transport = counter
	t.Cleanup(counter.CloseIdleConnections)
	return entry, counter
}

func nativeCompositionContext(t *testing.T, profile native.TransportProfile) context.Context {
	t.Helper()
	if profile == native.ProfileOpenAI {
		return service.WithHTTPUpstreamProfile(t.Context(), service.HTTPUpstreamProfileOpenAI)
	}
	return t.Context()
}

func TestNativeCompositionOwnedBodiesAndSingleClientCall(t *testing.T) {
	for _, profile := range []native.TransportProfile{native.ProfileDefault, native.ProfileOpenAI} {
		for _, kind := range []model.BodyKind{model.BodyReplayable, model.BodyExclusiveStream} {
			name := string(profile) + "/replayable"
			if kind == model.BodyExclusiveStream {
				name = string(profile) + "/stream"
			}
			t.Run(name, func(t *testing.T) {
				received := make(chan string, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					received <- string(data)
					w.Header().Set("Content-Encoding", "gzip")
					zw := gzip.NewWriter(w)
					_, _ = io.WriteString(zw, "complete response")
					_ = zw.Close()
				}))
				t.Cleanup(server.Close)
				s := NewHTTPUpstream(nil).(*httpUpstreamService)
				options := native.TransportOptions{Profile: profile, AccountID: 31, AccountConcurrency: 2}
				entry, count := nativeCompositionPool(t, s, server, options)
				ctx := nativeCompositionContext(t, profile)
				source := &nativeSourceBody{Reader: bytes.NewReader([]byte("actual body"))}
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/responses", source)
				require.NoError(t, err)
				req.Header.Set("Accept-Encoding", "gzip")
				if kind == model.BodyReplayable {
					req.GetBody = func() (io.ReadCloser, error) { t.Error("untrusted replay used"); return nil, errors.New("bad replay") }
				}
				p := nativeCompositionPrepare(t, s, req, options, kind)
				if kind == model.BodyExclusiveStream {
					require.Equal(t, source.Size(), int64(source.Len()))
					require.Zero(t, source.closed.Load())
				}
				require.Zero(t, count.calls.Load())
				d, budget := nativeCompositionDispatcher(t, ctx, 1)
				result, err := native.Open(ctx, p, nativeCompositionAdmit(t, d, p))
				require.NoError(t, err)
				require.Equal(t, model.SendSent, result.SendState)
				require.EqualValues(t, 1, atomic.LoadInt64(&entry.inFlight))
				data, err := io.ReadAll(result.Response.Body)
				require.NoError(t, err)
				require.Equal(t, "complete response", string(data))
				require.Equal(t, "actual body", <-received)
				var wg sync.WaitGroup
				for range 4 {
					wg.Add(1)
					go func() { defer wg.Done(); _ = result.Close() }()
				}
				wg.Wait()
				require.NoError(t, result.Response.Body.Close())
				require.NoError(t, p.Close())
				require.EqualValues(t, 1, count.calls.Load())
				require.EqualValues(t, 1, count.closes.Load())
				require.EqualValues(t, 1, source.closed.Load())
				require.Equal(t, 1, budget.Snapshot().Attempts)
				requireNoUpstreamInFlight(t, s)
			})
		}
	}
}

func TestNativeCompositionCancellationDoesNotLeakPoolSlot(t *testing.T) {
	for _, headers := range []bool{false, true} {
		name := "headers"
		if headers {
			name = "body"
		}
		t.Run(name, func(t *testing.T) {
			started, stopped := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if headers {
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				close(started)
				<-r.Context().Done()
				close(stopped)
			}))
			t.Cleanup(server.Close)
			s := NewHTTPUpstream(nil).(*httpUpstreamService)
			options := native.TransportOptions{Profile: native.ProfileDefault, AccountID: 32, AccountConcurrency: 1}
			entry, count := nativeCompositionPool(t, s, server, options)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			t.Cleanup(cancel)
			req, err := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader("body"))
			require.NoError(t, err)
			p := nativeCompositionPrepare(t, s, req, options, model.BodyReplayable)
			d, _ := nativeCompositionDispatcher(t, ctx, 1)
			permit := nativeCompositionAdmit(t, d, p)
			type outcome struct {
				r   *native.Result
				err error
			}
			done := make(chan outcome, 1)
			go func() { r, e := native.Open(ctx, p, permit); done <- outcome{r, e} }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("server did not receive request")
			}
			require.EqualValues(t, 1, atomic.LoadInt64(&entry.inFlight))
			cause := errors.New("test live authority revoked")
			if headers {
				got := <-done
				require.NoError(t, got.err)
				readStarted := make(chan struct{})
				readDone := make(chan error, 1)
				body := &notifyReadCloser{ReadCloser: got.r.Response.Body, started: readStarted}
				go func() { _, e := body.Read(make([]byte, 1)); readDone <- e }()
				<-readStarted
				got.r.Cancel(cause)
				select {
				case e := <-readDone:
					require.Error(t, e)
				case <-ctx.Done():
					t.Fatal("canceled body reader remained blocked")
				}
				require.NoError(t, got.r.Close())
				require.EqualValues(t, 1, count.closes.Load())
			} else {
				p.Cancel(cause)
				select {
				case got := <-done:
					require.ErrorIs(t, got.err, cause)
					require.Equal(t, model.SendSent, got.r.SendState)
				case <-ctx.Done():
					t.Fatal("canceled header wait remained blocked")
				}
				require.Zero(t, count.closes.Load())
			}
			select {
			case <-stopped:
			case <-ctx.Done():
				t.Fatal("server request was not canceled")
			}
			requireNoUpstreamInFlight(t, s)
			require.EqualValues(t, 1, count.calls.Load())
		})
	}
}

func TestNativeCompositionRedirectAndCookieJarCannotResendOrMutate(t *testing.T) {
	for _, jar := range []bool{false, true} {
		name := "redirect"
		if jar {
			name = "cookie_jar"
		}
		t.Run(name, func(t *testing.T) {
			var posts atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				w.Header().Set("Location", "/second")
				w.WriteHeader(307)
			}))
			t.Cleanup(server.Close)
			s := NewHTTPUpstream(nil).(*httpUpstreamService)
			options := native.TransportOptions{Profile: native.ProfileDefault, AccountID: 33, AccountConcurrency: 1}
			entry, count := nativeCompositionPool(t, s, server, options)
			if jar {
				entry.client.Jar, _ = cookiejar.New(nil)
			}
			req, err := http.NewRequestWithContext(t.Context(), "POST", server.URL, strings.NewReader("body"))
			require.NoError(t, err)
			p := nativeCompositionPrepare(t, s, req, options, model.BodyReplayable)
			d, _ := nativeCompositionDispatcher(t, t.Context(), 1)
			result, err := native.Open(t.Context(), p, nativeCompositionAdmit(t, d, p))
			if jar {
				require.Error(t, err)
				require.Zero(t, posts.Load())
				require.Zero(t, count.calls.Load())
				require.False(t, execution.PreRequestRetryAllowed(t.Context(), result.PreRequest, 0))
			} else {
				require.NoError(t, err)
				require.Equal(t, 307, result.Response.StatusCode)
				require.NoError(t, result.Close())
				require.EqualValues(t, 1, posts.Load())
				require.EqualValues(t, 1, count.calls.Load())
				require.EqualValues(t, 1, count.closes.Load())
			}
			requireNoUpstreamInFlight(t, s)
		})
	}
}

func TestNativeCompositionTLSFailureRequiresFreshPreparationAndBudget(t *testing.T) {
	for _, limit := range []int{1, 2} {
		name := "budget_exhausted"
		if limit == 2 {
			name = "explicit_second_attempt"
		}
		t.Run(name, func(t *testing.T) {
			var posts atomic.Int64
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				b, _ := io.ReadAll(r.Body)
				if string(b) != "one-generation" {
					t.Error("body changed")
				}
				w.WriteHeader(204)
			}))
			listener := &failFirstTLSListener{Listener: server.Listener}
			server.Listener = listener
			server.StartTLS()
			t.Cleanup(server.Close)
			proxy, proxyCalls := startTestSOCKS5Proxy(t)
			s := NewHTTPUpstream(nil).(*httpUpstreamService)
			options := native.TransportOptions{Profile: native.ProfileOpenAI, AccountID: 34, AccountConcurrency: 1, ProxyURL: proxy}
			_, count := nativeCompositionPool(t, s, server, options)
			ctx := nativeCompositionContext(t, options.Profile)
			d, budget := nativeCompositionDispatcher(t, ctx, limit)
			prepareNext := func() *native.PreparedHTTP {
				req, e := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader("one-generation"))
				require.NoError(t, e)
				return nativeCompositionPrepare(t, s, req, options, model.BodyReplayable)
			}
			first := prepareNext()
			result, err := native.Open(ctx, first, nativeCompositionAdmit(t, d, first))
			require.Error(t, err)
			require.Equal(t, model.SendNotSent, result.SendState)
			require.True(t, result.PreRequest.TLSHandshakeFailed)
			require.True(t, result.PreRequest.TransientTLSHandshakeFailure)
			require.True(t, result.PreRequest.TransientTransportFailure)
			require.True(t, execution.PreRequestRetryAllowed(ctx, result.PreRequest, 0))
			require.False(t, execution.PreRequestRetryAllowed(ctx, result.PreRequest, 1))
			require.EqualValues(t, 1, count.calls.Load())
			require.EqualValues(t, 1, listener.accepts.Load())
			require.EqualValues(t, 1, proxyCalls.Load())
			require.Zero(t, posts.Load())
			requireNoUpstreamInFlight(t, s)
			// The test explicitly owns the single wait and recovery count. Neither
			// provider nor repository starts it or creates a second allowance.
			delay, e := budget.ReserveWait(execution.PreRequestRetryDelay, 0)
			require.NoError(t, e)
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			second := prepareNext()
			require.Equal(t, first.Identity(), second.Identity(), "TLS-only recovery must keep the same sealed target and wire")
			permit, e := d.Admit(ctx, second.Identity(), model.AttemptTarget{Owner: second.Identity().Owner, Mode: "http", Endpoint: "/responses", Limit: 2})
			if limit == 1 {
				require.ErrorIs(t, e, execution.ErrAttemptBudgetExhausted)
				require.EqualValues(t, 1, count.calls.Load())
				require.Zero(t, posts.Load())
				return
			}
			require.NoError(t, e)
			require.NotEqual(t, first, second)
			secondResult, e := native.Open(ctx, second, permit)
			require.NoError(t, e)
			require.EqualValues(t, 2, secondResult.AttemptSequence)
			require.Equal(t, model.SendSent, secondResult.SendState)
			require.NoError(t, secondResult.Close())
			require.Equal(t, 2, budget.Snapshot().Attempts)
			require.EqualValues(t, 2, count.calls.Load())
			require.EqualValues(t, 2, listener.accepts.Load())
			require.EqualValues(t, 1, posts.Load())
			requireNoUpstreamInFlight(t, s)
		})
	}
}

func TestNativeCompositionHTTP2FeedbackIsOwnedOnce(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "complete"
		if fail {
			name = "stream_failure"
		}
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			releaseOnce := sync.OnceFunc(func() { close(release) })
			t.Cleanup(releaseOnce)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: start\n\n")
				w.(http.Flusher).Flush()
				if fail {
					select {
					case <-release:
						panic(http.ErrAbortHandler)
					case <-r.Context().Done():
						return
					}
				}
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			t.Cleanup(server.Close)
			proxy, _ := startTestSOCKS5Proxy(t)
			cfg := &config.Config{}
			cfg.Gateway.OpenAIHTTP2.Enabled = true
			cfg.Gateway.OpenAIHTTP2.AllowProxyFallbackToHTTP1 = true
			cfg.Gateway.OpenAIHTTP2.FallbackErrorThreshold = 10
			s := NewHTTPUpstream(cfg).(*httpUpstreamService)
			options := native.TransportOptions{Profile: native.ProfileOpenAI, AccountID: 35, AccountConcurrency: 1, ProxyURL: proxy}
			entry, count := nativeCompositionPool(t, s, server, options)
			state := s.getOrCreateOpenAIHTTP2FallbackState(entry.feedbackKey)
			if !fail {
				state.recordFailure(time.Now(), 10, time.Minute, time.Minute)
			}
			ctx := nativeCompositionContext(t, options.Profile)
			req, err := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader("body"))
			require.NoError(t, err)
			p := nativeCompositionPrepare(t, s, req, options, model.BodyReplayable)
			d, _ := nativeCompositionDispatcher(t, ctx, 1)
			result, err := native.Open(ctx, p, nativeCompositionAdmit(t, d, p))
			require.NoError(t, err)
			require.Equal(t, 2, result.Response.ProtoMajor)
			if fail {
				releaseOnce()
			}
			_, err = io.ReadAll(result.Response.Body)
			if fail {
				require.Error(t, err)
				_, _ = result.Response.Body.Read(make([]byte, 1))
			} else {
				require.NoError(t, err)
			}
			state.mu.Lock()
			errorsBefore := state.errorCount
			state.mu.Unlock()
			require.Equal(t, 1, errorsBefore, "headers/EOF must not clear health; repeated failed reads must not count twice")
			service.CompleteHTTPUpstreamResponse(result.Response)
			state.mu.Lock()
			errorsAfter := state.errorCount
			state.mu.Unlock()
			if fail {
				require.Equal(t, 1, errorsAfter, "completion cannot overwrite prior failure")
			} else {
				require.Zero(t, errorsAfter)
				state.recordFailure(time.Now(), 10, time.Minute, time.Minute)
				service.CompleteHTTPUpstreamResponse(result.Response)
				state.mu.Lock()
				errorsAfter = state.errorCount
				state.mu.Unlock()
				require.Equal(t, 1, errorsAfter, "completion callback ran twice")
			}
			require.NoError(t, result.Close())
			require.NoError(t, result.Close())
			require.EqualValues(t, 1, count.calls.Load())
			require.EqualValues(t, 1, count.closes.Load())
			requireNoUpstreamInFlight(t, s)
		})
	}
}

func TestNativeCompositionCodexEncodingSealedBeforeRealTLSWrite(t *testing.T) {
	actual := []byte("{\"model\":\"fixture\",\"input\":\"one actual body\"}")
	type observed struct {
		body     []byte
		encoding string
	}
	received := make(chan observed, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- observed{body, r.Header.Get("Content-Encoding")}
		w.WriteHeader(204)
	}))
	t.Cleanup(server.Close)
	s := NewHTTPUpstream(nil).(*httpUpstreamService)
	options := native.TransportOptions{Profile: native.ProfileOpenAI, AccountID: 36, AccountConcurrency: 1}
	entry, err := s.getClientEntry("", 36, 1, service.HTTPUpstreamProfileOpenAI, false, false, "https://chatgpt.com")
	require.NoError(t, err)
	transport := entry.client.Transport.(*http.Transport)
	// Fixed local routing preserves the real sealed URL/Host and TLS verification;
	// no DNS or connection to chatgpt.com can occur in this fixture.
	localURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "chatgpt.com:443" {
			return nil, errors.New("unexpected test destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, localURL.Host)
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}
	count := &nativeCompositionCounter{base: transport}
	entry.client.Transport = count
	t.Cleanup(count.CloseIdleConnections)
	ctx := nativeCompositionContext(t, options.Profile)
	source := &nativeSourceBody{Reader: bytes.NewReader(actual)}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://chatgpt.com/backend-api/codex/responses", source)
	require.NoError(t, err)
	req.ContentLength = int64(len(actual))
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) {
		t.Error("foreign GetBody trusted")
		return io.NopCloser(strings.NewReader("wrong bytes")), nil
	}
	p := nativeCompositionPrepare(t, s, req, options, model.BodyReplayable)
	require.Zero(t, count.calls.Load())
	require.EqualValues(t, 1, source.closed.Load())
	d, _ := nativeCompositionDispatcher(t, ctx, 1)
	result, err := native.Open(ctx, p, nativeCompositionAdmit(t, d, p))
	require.NoError(t, err)
	require.NoError(t, result.Close())
	got := <-received
	require.Equal(t, "zstd", got.encoding)
	require.Equal(t, sha256.Sum256(got.body), p.Identity().BodyDigest)
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(got.body, nil)
	require.NoError(t, err)
	require.Equal(t, actual, decoded)
	require.EqualValues(t, 1, count.calls.Load())
	require.EqualValues(t, 1, source.closed.Load())
	requireNoUpstreamInFlight(t, s)
}

func (b *nativeSourceBody) Close() error { b.closed.Add(1); return nil }

func TestNativePrepareEncodesActualOwnedBodyBeforeSeal(t *testing.T) {
	actual := []byte("{\"model\":\"fixture\",\"input\":\"actual body\"}")
	source := &nativeSourceBody{Reader: bytes.NewReader(actual)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", source)
	require.NoError(t, err)
	req.ContentLength = int64(len(actual))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fixture", "original")
	var replayCalls atomic.Int64
	req.GetBody = func() (io.ReadCloser, error) {
		replayCalls.Add(1)
		return io.NopCloser(bytes.NewReader([]byte("different replay"))), nil
	}
	upstream := NewHTTPUpstream(nil).(*httpUpstreamService)
	wire, err := upstream.PrepareNativeRequest(req, int64(len(actual)))
	require.NoError(t, err)
	require.Equal(t, "zstd", wire.Header.Get("Content-Encoding"))
	encoded, err := io.ReadAll(wire.Body)
	require.NoError(t, err)
	require.NoError(t, wire.Body.Close())
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(encoded, nil)
	require.NoError(t, err)
	require.Equal(t, actual, decoded)
	require.Zero(t, replayCalls.Load(), "do not trust the caller's replay function")
	require.EqualValues(t, 1, source.closed.Load())
	req.Header.Set("X-Fixture", "changed")
	require.Equal(t, "original", wire.Header.Get("X-Fixture"))
	require.Empty(t, req.Header.Get("Content-Encoding"), "request header copy must stay detached")
}

func TestNativePreparePreservesExclusiveStreamUntilOpen(t *testing.T) {
	source := &nativeSourceBody{Reader: bytes.NewReader([]byte("streamed body"))}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", source)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	upstream := NewHTTPUpstream(nil).(*httpUpstreamService)
	wire, err := upstream.PrepareNativeRequest(req, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), source.Size()-int64(source.Len()), "stream must not be consumed to fabricate a digest")
	require.Zero(t, source.closed.Load())
	require.Same(t, source, wire.Body)
	require.Nil(t, wire.GetBody)
	require.NoError(t, wire.Body.Close())
}

func TestNativeRoundTripOnceReturnsRedirectWithoutHiddenSend(t *testing.T) {
	var initial, redirected atomic.Int64
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			redirected.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		initial.Add(1)
		data, _ := io.ReadAll(r.Body)
		received <- data
		w.Header().Set("Location", "/next")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/first", bytes.NewReader([]byte("sealed bytes")))
	require.NoError(t, err)
	upstream := NewHTTPUpstream(nil).(*httpUpstreamService)
	resp, err := upstream.RoundTripOnce(req, native.TransportOptions{Profile: native.ProfileDefault, AccountID: 7, AccountConcurrency: 1})
	require.NoError(t, err)
	require.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 1, initial.Load())
	require.Zero(t, redirected.Load())
	require.Equal(t, "sealed bytes", string(<-received))
}

func TestNativeRoundTripOnceRejectsPolicyMismatchBeforeNetwork(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
	t.Cleanup(server.Close)
	upstream := NewHTTPUpstream(nil).(*httpUpstreamService)
	for _, options := range []native.TransportOptions{
		{Profile: native.ProfileDefault, AccountID: 7, AccountConcurrency: 1},
		{Profile: native.ProfileOpenAI, AccountID: 7, AccountConcurrency: 1, PublicHostsOnly: true},
	} {
		req, err := http.NewRequestWithContext(service.WithHTTPUpstreamProfile(t.Context(), service.HTTPUpstreamProfileOpenAI), http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		_, err = upstream.RoundTripOnce(req, options)
		require.ErrorIs(t, err, native.ErrInvalidHTTPPreparation)
	}
	require.Zero(t, calls.Load())
}

package openai_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

// All network fixtures are local. This adapter deliberately uses RoundTrip so
// Client.Do cannot hide redirects; production transport integration is MAIN's.
type localTransport struct {
	base    http.RoundTripper
	calls   atomic.Int32
	inspect func(*http.Request, native.TransportOptions)
}

func (u *localTransport) RoundTripOnce(req *http.Request, options native.TransportOptions) (*http.Response, error) {
	u.calls.Add(1)
	if u.inspect != nil {
		u.inspect(req, options)
	}
	return u.base.RoundTrip(req)
}

type transportFunc func(*http.Request, native.TransportOptions) (*http.Response, error)

// Negative tests never escape to DNS/proxy/network even if admission regresses.
type blockedRoundTripper struct{}

func (blockedRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected call to blocked test transport")
}

func (f transportFunc) RoundTripOnce(req *http.Request, o native.TransportOptions) (*http.Response, error) {
	return f(req, o)
}

// Only tests construct this synthetic authority. The provider never sees it.
type testAuthority struct {
	mu         sync.Mutex
	validation execution.DispatchValidation
	denied     error
	calls      int
}

func newAuthority() *testAuthority {
	return &testAuthority{validation: execution.DispatchValidation{
		Authorization: sha256.Sum256([]byte("test user/key/resource/grant scope")),
		Leases:        sha256.Sum256([]byte("test owned capacity lease")),
	}}
}
func (a *testAuthority) RevalidateDispatch(context.Context, model.PreparedIdentity) (execution.DispatchValidation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return a.validation, a.denied
}

func identity() model.PreparedIdentity {
	return model.PreparedIdentity{OperationID: "test-operation", ResourceID: 17,
		Owner: model.ResourceOwner{AccountID: 9, VendorID: 3}, UserID: 4, APIKeyID: 5,
		GroupID: 6, GroupBound: true, Mode: "native", Transport: "http",
		GrantDigest: sha256.Sum256([]byte("test resolved grant")), BodyKind: model.BodyReplayable}
}

func specFor(t *testing.T, method, target, body string) native.Spec {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return native.Spec{Request: req, Identity: identity(), MaxReplayBodyBytes: 1024,
		Transport: native.TransportOptions{Profile: native.ProfileOpenAI, AccountID: 17, AccountConcurrency: 2}}
}

func TestBPSHTTPPreparationBindsModeProfileAndProbePrincipal(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*native.Spec)
		valid  bool
	}{
		{"customer", func(*native.Spec) {}, true},
		{"wrong_profile", func(s *native.Spec) { s.Transport.Profile = native.ProfileOpenAI }, false},
		{"native_with_bps_profile", func(s *native.Spec) { s.Identity.Mode = "native" }, false},
		{"missing_role", func(s *native.Spec) { s.Identity.Role = "" }, false},
		{"system_probe", func(s *native.Spec) {
			s.Identity.Principal = model.PrincipalSystemProbe
			s.Identity.UserID, s.Identity.APIKeyID, s.Identity.GroupID, s.Identity.GroupBound = 0, 0, 0, false
			s.Identity.ProbeID, s.Identity.ProbeClaimDigest = "persisted-probe", [32]byte{1}
			s.Identity.Binding.ProbeID = s.Identity.ProbeID
		}, true},
		{"customer_with_probe_binding", func(s *native.Spec) { s.Identity.Binding.ProbeID = "probe" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := specFor(t, http.MethodPost, "https://bps.invalid/responses", "{}")
			spec.Identity.Mode, spec.Identity.Role = "bps", model.AttemptGenerate
			spec.Identity.Binding = model.ModeBinding{BillingScope: "original-operation"}
			spec.Transport.Profile = native.ProfileExcelBPS
			test.change(&spec)
			transport := &localTransport{base: blockedRoundTripper{}}
			prepared, err := native.PrepareHTTP(t.Context(), spec, transport)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
			if prepared != nil {
				if err := prepared.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if transport.calls.Load() != 0 {
				t.Fatal("preparation sent a request")
			}
		})
	}
}

func dispatcher(t *testing.T, a *testAuthority, limit int) (*execution.Dispatcher, *execution.AttemptBudget) {
	t.Helper()
	budget := execution.NewAttemptBudget(execution.AttemptLimits{Total: limit, PerOwner: limit})
	d, err := execution.NewDispatcher(context.Background(), "test-operation", budget, execution.NewFirstOutput(time.Now()), a)
	if err != nil {
		t.Fatal(err)
	}
	return d, budget
}

func admit(t *testing.T, d *execution.Dispatcher, p *native.PreparedHTTP) *execution.SendPermit {
	t.Helper()
	permit, err := d.Admit(context.Background(), p.Identity(), model.AttemptTarget{Owner: p.Identity().Owner, Mode: "http", Endpoint: "/responses", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	return permit
}

func prepare(t *testing.T, ctx context.Context, spec native.Spec, transport native.Transport) *native.PreparedHTTP {
	t.Helper()
	p, err := native.PrepareHTTP(ctx, spec, transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func mustReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for fixture synchronization")
		var zero T
		return zero
	}
}

type countBody struct {
	io.ReadCloser
	reads  atomic.Int32
	closes atomic.Int32
}

func (b *countBody) Read(p []byte) (int, error) { b.reads.Add(1); return b.ReadCloser.Read(p) }
func (b *countBody) Close() error               { b.closes.Add(1); return b.ReadCloser.Close() }

func TestPrepareHTTPSealsActualBodyHeadersTargetAndTransport(t *testing.T) {
	type observed struct{ body, host, uri, auth string }
	observedRequest := make(chan observed, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		observedRequest <- observed{string(b), r.Host, r.RequestURI, r.Header.Get("Authorization")}
		_, _ = io.WriteString(w, "response")
	}))
	defer server.Close()
	transport := &localTransport{base: server.Client().Transport}
	spec := specFor(t, "POST", server.URL+"/responses?a=1", "actual-body")
	source := &countBody{ReadCloser: spec.Request.Body}
	spec.Request.Body = source
	var getBodyCalls atomic.Int32
	spec.Request.GetBody = func() (io.ReadCloser, error) {
		getBodyCalls.Add(1)
		return io.NopCloser(strings.NewReader("different-body")), nil
	}
	spec.Request.Header.Set("Authorization", "Bearer fixture-secret")
	spec.Request.Host = "sealed.test"
	spec.Transport.ProxyURL = "http://fixture-user:fixture-pass@proxy.invalid:8118"
	spec.Transport.PublicHostsOnly = true
	spec.Transport.TLSProfile = &tlsfingerprint.Profile{Name: "fixture", CipherSuites: []uint16{4865}, Curves: []uint16{29}, ALPNProtocols: []string{"http/1.1"}}
	transport.inspect = func(req *http.Request, o native.TransportOptions) {
		if o.ProxyURL != "http://fixture-user:fixture-pass@proxy.invalid:8118" || o.AccountID != 17 || !o.PublicHostsOnly || o.TLSProfile.CipherSuites[0] != 4865 || o.TLSProfile.Curves[0] != 29 {
			t.Error("transport inputs were not sealed")
		}
		copyBody, err := req.GetBody()
		if err != nil {
			t.Error(err)
			return
		}
		replay, err := io.ReadAll(copyBody)
		_ = copyBody.Close()
		if err != nil || string(replay) != "actual-body" {
			t.Errorf("wrong frozen replay: %q %v", replay, err)
		}
	}
	p := prepare(t, context.Background(), spec, transport)
	sealed := p.Identity()
	if !sealed.Valid() || sealed.BodyDigest != sha256.Sum256([]byte("actual-body")) {
		t.Fatal("body was not derived from actual request")
	}
	if transport.calls.Load() != 0 || getBodyCalls.Load() != 0 || source.closes.Load() != 1 {
		t.Fatal("preparation sent, used caller replay, or leaked source")
	}
	spec.Request.URL.Path = "/mutated"
	spec.Request.URL.RawQuery = "secret=mutated"
	spec.Request.Host = "mutated.test"
	spec.Request.Header.Set("Authorization", "Bearer mutated")
	spec.Transport.TLSProfile.CipherSuites[0] = 0
	spec.Transport.TLSProfile.Curves[0] = 0
	spec.Transport.ProxyURL = ""
	spec.Identity.UserID = 99
	d, budget := dispatcher(t, newAuthority(), 2)
	result, err := native.Open(context.Background(), p, admit(t, d, p))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	got := mustReceive(t, observedRequest)
	if got != (observed{"actual-body", "sealed.test", "/responses?a=1", "Bearer fixture-secret"}) {
		t.Fatalf("request changed: %+v", got)
	}
	if result.SendState != model.SendSent || result.AttemptSequence != 1 || p.Identity() != sealed || budget.Snapshot().Attempts != 1 {
		t.Fatal("identity, budget or send evidence changed")
	}
}

func TestPrepareHTTPDigestsBindEveryTransportInput(t *testing.T) {
	transport := transportFunc(func(*http.Request, native.TransportOptions) (*http.Response, error) {
		t.Fatal("prepare must not send")
		return nil, nil
	})
	baseline := prepare(t, context.Background(), specFor(t, "POST", "https://upstream.invalid/responses?a=1", "payload"), transport).Identity()
	cases := []struct {
		name   string
		change func(*native.Spec)
		target bool
	}{
		{"method", func(s *native.Spec) { s.Request.Method = "PUT" }, true},
		{"scheme", func(s *native.Spec) { s.Request.URL.Scheme = "http" }, true},
		{"host", func(s *native.Spec) { s.Request.URL.Host = "other.invalid:8443" }, true},
		{"path", func(s *native.Spec) { s.Request.URL.Path = "/control" }, true},
		{"raw_path", func(s *native.Spec) { s.Request.URL.RawPath = "/%72esponses" }, true},
		{"query", func(s *native.Spec) { s.Request.URL.RawQuery = "a=2" }, true},
		{"host_header", func(s *native.Spec) { s.Request.Host = "other.invalid" }, true},
		{"auth", func(s *native.Spec) { s.Request.Header.Set("Authorization", "fixture") }, false},
		{"account_header", func(s *native.Spec) { s.Request.Header.Set("ChatGPT-Account-ID", "fixture-account") }, false},
		{"encoding", func(s *native.Spec) { s.Request.Header.Set("Content-Encoding", "zstd") }, false},
		{"proxy", func(s *native.Spec) { s.Transport.ProxyURL = "http://u:p@proxy.invalid" }, true},
		{"capacity", func(s *native.Spec) { s.Transport.AccountConcurrency = 3 }, true},
		{"actual_profile", func(s *native.Spec) { s.Transport.Profile = native.ProfileDefault }, true},
		{"public_hosts", func(s *native.Spec) { s.Transport.PublicHostsOnly = true }, true},
		{"tls", func(s *native.Spec) {
			s.Transport.TLSProfile = &tlsfingerprint.Profile{EnableGREASE: true, CipherSuites: []uint16{4865}}
		}, true},
		{"trailer", func(s *native.Spec) { s.Request.Trailer = http.Header{"X-Checksum": []string{"fixed"}} }, false},
		{"transfer_encoding", func(s *native.Spec) { s.Request.TransferEncoding = []string{"chunked"} }, false},
		{"connection_close", func(s *native.Spec) { s.Request.Close = true }, false},
		{"body", func(s *native.Spec) { s.Request.Body = io.NopCloser(strings.NewReader("changed")) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := specFor(t, "POST", "https://upstream.invalid/responses?a=1", "payload")
			tc.change(&spec)
			actual := prepare(t, context.Background(), spec, transport).Identity()
			if actual.WireDigest == baseline.WireDigest {
				t.Fatal("changed wire retained its identity")
			}
			if tc.target && actual.TargetDigest == baseline.TargetDigest {
				t.Fatal("changed target/transport retained its identity")
			}
		})
	}
}

func TestExclusiveStreamTransfersWithoutPrepareReadOrReplay(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received <- string(b)
		w.WriteHeader(204)
	}))
	defer server.Close()
	transport := &localTransport{base: server.Client().Transport, inspect: func(r *http.Request, _ native.TransportOptions) {
		if r.GetBody != nil {
			t.Error("stream unexpectedly became replayable")
		}
	}}
	spec := specFor(t, "POST", server.URL, "streaming-body")
	source := &countBody{ReadCloser: spec.Request.Body}
	spec.Request.Body = source
	spec.Identity.BodyKind = model.BodyExclusiveStream
	spec.MaxReplayBodyBytes = 0
	spec.Request.ContentLength = -1
	spec.Request.GetBody = func() (io.ReadCloser, error) { t.Error("stream replay called"); return nil, errors.New("no replay") }
	p := prepare(t, context.Background(), spec, transport)
	if source.reads.Load() != 0 || source.closes.Load() != 0 || p.Identity().BodyDigest != ([32]byte{}) || p.Identity().BodyHandle == ([16]byte{}) {
		t.Fatal("stream was read or given an invented digest")
	}
	d, _ := dispatcher(t, newAuthority(), 1)
	result, err := native.Open(context.Background(), p, admit(t, d, p))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReceive(t, received); got != "streaming-body" {
		t.Fatalf("lost body: %q", got)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	if source.closes.Load() != 1 || transport.calls.Load() != 1 {
		t.Fatal("stream owner was closed/sent more than once")
	}
}

func TestHTTPControlMethodsAndEmptyBody(t *testing.T) {
	for _, method := range []string{"GET", "DELETE", "POST", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			observed := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { observed <- r.Method; w.WriteHeader(204) }))
			defer server.Close()
			spec := specFor(t, method, server.URL+"/models", "")
			spec.Request.Body = nil
			spec.Request.GetBody = nil
			p := prepare(t, context.Background(), spec, &localTransport{base: server.Client().Transport})
			if p.Identity().BodyDigest != sha256.Sum256(nil) {
				t.Fatal("empty body is known, not unknown")
			}
			d, _ := dispatcher(t, newAuthority(), 1)
			result, err := native.Open(context.Background(), p, admit(t, d, p))
			if err != nil {
				t.Fatal(err)
			}
			defer result.Close()
			if got := mustReceive(t, observed); got != method {
				t.Fatalf("changed method to %s", got)
			}
		})
	}
}

func TestOpenOneShotPermitAndPreparedOwnership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	transport := &localTransport{base: server.Client().Transport}
	p := prepare(t, context.Background(), specFor(t, "POST", server.URL, "body"), transport)
	second := prepare(t, context.Background(), specFor(t, "POST", server.URL, "body"), transport)
	d, budget := dispatcher(t, newAuthority(), 2)
	permit := admit(t, d, p)
	copied := *permit
	result, err := native.Open(context.Background(), p, &copied)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	refused, err := native.Open(context.Background(), second, permit)
	if !errors.Is(err, execution.ErrSendPermitConsumed) || refused.SendState != model.SendNotSent {
		t.Fatalf("copy reused capability: %v", err)
	}
	duplicate, err := native.Open(context.Background(), p, permit)
	if !errors.Is(err, native.ErrHTTPAttemptOpened) {
		t.Fatalf("opened preparation reused: %v", err)
	}
	_ = duplicate.Close() // A rejected duplicate must not close the winning response.
	if _, err := io.ReadAll(result.Response.Body); err != nil {
		t.Fatal(err)
	}
	if transport.calls.Load() != 1 || budget.Snapshot().Attempts != 1 {
		t.Fatal("duplicate attempt consumed transport/budget")
	}
}

func TestOpenRejectsChangedOrMissingPermissionBeforeNetwork(t *testing.T) {
	for _, name := range []string{"nil", "zero", "mismatched_wire", "revoked", "lease_changed", "expired", "canceled"} {
		t.Run(name, func(t *testing.T) {
			transport := &localTransport{base: blockedRoundTripper{}}
			p := prepare(t, context.Background(), specFor(t, "POST", "https://must-not-connect.invalid/responses", "body"), transport)
			a := newAuthority()
			d, _ := dispatcher(t, a, 2)
			permit := admit(t, d, p)
			var expected error
			ctx := context.Background()
			switch name {
			case "nil":
				permit = nil
				expected = execution.ErrDispatchUnavailable
			case "zero":
				permit = &execution.SendPermit{}
				expected = execution.ErrDispatchUnavailable
			case "mismatched_wire":
				p = prepare(t, ctx, specFor(t, "POST", "https://must-not-connect.invalid/responses", "other"), transport)
				expected = execution.ErrInvalidPreparedIdentity
			case "revoked":
				expected = errors.New("test scope revoked")
				a.denied = expected
			case "lease_changed":
				a.validation.Leases = sha256.Sum256([]byte("replacement lease"))
				expected = execution.ErrDispatchChanged
			case "expired":
				a.validation.ValidUntil = time.Now().Add(-time.Second)
				expected = execution.ErrDispatchExpired
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				expected = context.Canceled
			}
			result, err := native.Open(ctx, p, permit)
			if !errors.Is(err, expected) || result.SendState != model.SendNotSent || transport.calls.Load() != 0 {
				t.Fatalf("unsafe rejected dispatch: %v state=%v calls=%d", err, result.SendState, transport.calls.Load())
			}
		})
	}
}

func TestOpenConcurrentUsesOnePreparation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	transport := &localTransport{base: server.Client().Transport}
	p := prepare(t, context.Background(), specFor(t, "POST", server.URL, "body"), transport)
	d, _ := dispatcher(t, newAuthority(), 2)
	permit := admit(t, d, p)
	start := make(chan struct{})
	type outcome struct {
		result *native.Result
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() { <-start; r, e := native.Open(context.Background(), p, permit); results <- outcome{r, e} }()
	}
	close(start)
	wins := 0
	var winning *native.Result
	for range 2 {
		got := mustReceive(t, results)
		if got.err == nil {
			wins++
			winning = got.result
		} else if !errors.Is(got.err, native.ErrHTTPAttemptOpened) {
			t.Fatal(got.err)
		}
	}
	if wins != 1 || transport.calls.Load() != 1 {
		t.Fatal("concurrent Open generated more than once")
	}
	_ = winning.Close()
}

func TestOpenRealTransportSendEvidence(t *testing.T) {
	t.Run("connection_refused", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected connection") }))
		target, transport := server.URL, server.Client().Transport
		server.Close()
		p := prepare(t, context.Background(), specFor(t, "POST", target, "body"), &localTransport{base: transport})
		d, _ := dispatcher(t, newAuthority(), 1)
		result, err := native.Open(context.Background(), p, admit(t, d, p))
		if err == nil || result.SendState != model.SendNotSent {
			t.Fatalf("connection failure: %v state=%v", err, result.SendState)
		}
	})
	t.Run("response_lost_after_post", func(t *testing.T) {
		posts := make(chan string, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			posts <- string(b)
			c, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = c.Close()
		}))
		defer server.Close()
		transport := &localTransport{base: server.Client().Transport}
		p := prepare(t, context.Background(), specFor(t, "POST", server.URL, "one-generation"), transport)
		d, _ := dispatcher(t, newAuthority(), 1)
		result, err := native.Open(context.Background(), p, admit(t, d, p))
		if err == nil || result.SendState != model.SendSent || transport.calls.Load() != 1 {
			t.Fatalf("lost response incorrectly replayable: %v state=%v", err, result.SendState)
		}
		if mustReceive(t, posts) != "one-generation" {
			t.Fatal("wrong transmitted body")
		}
	})
	t.Run("unobserved_failure", func(t *testing.T) {
		sentinel := errors.New("unobserved custom transport failure")
		p := prepare(t, context.Background(), specFor(t, "POST", "https://unused.invalid", "body"), transportFunc(func(r *http.Request, _ native.TransportOptions) (*http.Response, error) {
			_ = r.Body.Close()
			return nil, sentinel
		}))
		d, _ := dispatcher(t, newAuthority(), 1)
		result, err := native.Open(context.Background(), p, admit(t, d, p))
		if !errors.Is(err, sentinel) || result.SendState != model.SendAmbiguous {
			t.Fatalf("missing evidence became safe: %v state=%v", err, result.SendState)
		}
	})
}

func TestOpenReturnsRedirectWithoutHiddenSend(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.Header().Set("Location", "/another-generation")
		w.WriteHeader(307)
	}))
	defer server.Close()
	p := prepare(t, context.Background(), specFor(t, "POST", server.URL, "body"), &localTransport{base: server.Client().Transport})
	d, _ := dispatcher(t, newAuthority(), 1)
	result, err := native.Open(context.Background(), p, admit(t, d, p))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	if posts.Load() != 1 || result.Response.StatusCode != 307 || result.SendState != model.SendSent {
		t.Fatal("redirect generated another request")
	}
}

func TestCancelStopsHeaderWaitAndResponseRead(t *testing.T) {
	for _, headers := range []bool{false, true} {
		name := "header_wait"
		if headers {
			name = "body_read"
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
			defer server.Close()
			transport := &localTransport{base: server.Client().Transport}
			p := prepare(t, context.Background(), specFor(t, "POST", server.URL, "body"), transport)
			d, _ := dispatcher(t, newAuthority(), 1)
			permit := admit(t, d, p)
			type outcome struct {
				r *native.Result
				e error
			}
			opened := make(chan outcome, 1)
			go func() { r, e := native.Open(context.Background(), p, permit); opened <- outcome{r, e} }()
			mustReceive(t, started)
			cause := errors.New("test revoked during send")
			if headers {
				got := mustReceive(t, opened)
				if got.e != nil {
					t.Fatal(got.e)
				}
				read := make(chan error, 1)
				go func() { _, e := got.r.Response.Body.Read(make([]byte, 1)); read <- e }()
				got.r.Cancel(cause)
				if err := mustReceive(t, read); err == nil {
					t.Fatal("blocked read did not stop")
				}
				_ = got.r.Close()
			} else {
				p.Cancel(cause)
				got := mustReceive(t, opened)
				if !errors.Is(got.e, cause) || got.r.SendState != model.SendSent {
					t.Fatalf("lost cancellation/send fact: %v %v", got.e, got.r.SendState)
				}
			}
			mustReceive(t, stopped)
			if transport.calls.Load() != 1 {
				t.Fatal("cancel started another send")
			}
		})
	}
}

func TestPrepareBoundsCancellationAndBodyOwnership(t *testing.T) {
	noSend := transportFunc(func(*http.Request, native.TransportOptions) (*http.Response, error) {
		t.Error("Prepare sent request")
		return nil, nil
	})
	for _, name := range []string{"too_large", "unknown_length_too_large", "invalid_identity", "bad_length", "bad_kind", "missing_limit", "caller_digest", "missing_profile", "unknown_profile"} {
		t.Run(name, func(t *testing.T) {
			spec := specFor(t, "POST", "https://unused.invalid", "body")
			source := &countBody{ReadCloser: spec.Request.Body}
			spec.Request.Body = source
			expected := native.ErrInvalidHTTPPreparation
			switch name {
			case "too_large":
				spec.MaxReplayBodyBytes = 3
				expected = native.ErrReplayBodyLimit
			case "unknown_length_too_large":
				spec.MaxReplayBodyBytes = 3
				spec.Request.ContentLength = -1
				expected = native.ErrReplayBodyLimit
			case "invalid_identity":
				spec.Identity.GrantDigest = [32]byte{}
			case "bad_length":
				spec.Request.ContentLength = 8
			case "bad_kind":
				spec.Identity.BodyKind = model.BodyUnknown
			case "missing_limit":
				spec.MaxReplayBodyBytes = 0
			case "caller_digest":
				spec.Identity.BodyDigest = sha256.Sum256([]byte("fake"))
			case "missing_profile":
				spec.Transport.Profile = ""
			case "unknown_profile":
				spec.Transport.Profile = "unverified"
			}
			p, err := native.PrepareHTTP(context.Background(), spec, noSend)
			if p != nil || !errors.Is(err, expected) || source.closes.Load() != 1 {
				t.Fatalf("preparation ownership/error: %v closes=%d", err, source.closes.Load())
			}
		})
	}
	t.Run("cancel_blocked_read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reader, writer := io.Pipe()
		defer writer.Close()
		started := make(chan struct{})
		spec := specFor(t, "POST", "https://unused.invalid", "")
		spec.Request.ContentLength = -1
		spec.Request.Body = &notifyingBody{ReadCloser: reader, started: started}
		done := make(chan error, 1)
		go func() { _, err := native.PrepareHTTP(ctx, spec, noSend); done <- err }()
		mustReceive(t, started)
		cancel()
		if err := mustReceive(t, done); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	})
}

type notifyingBody struct {
	io.ReadCloser
	started chan struct{}
	once    sync.Once
}

func (b *notifyingBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	return b.ReadCloser.Read(p)
}

func TestPreparedCloseAndCancellationBeforeOpen(t *testing.T) {
	for _, name := range []string{"close", "cancel"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			spec := specFor(t, "POST", "https://unused.invalid", "stream")
			source := &countBody{ReadCloser: spec.Request.Body}
			spec.Request.Body = source
			spec.Identity.BodyKind = model.BodyExclusiveStream
			transport := &localTransport{base: blockedRoundTripper{}}
			p := prepare(t, ctx, spec, transport)
			d, _ := dispatcher(t, newAuthority(), 1)
			permit := admit(t, d, p)
			if name == "close" {
				_ = p.Close()
			} else {
				cancel()
			}
			result, err := native.Open(context.Background(), p, permit)
			if err == nil || result.SendState != model.SendNotSent || transport.calls.Load() != 0 {
				t.Fatal("closed/canceled preparation sent")
			}
			_ = p.Close()
			if source.closes.Load() != 1 {
				t.Fatal("exclusive body was not released exactly once")
			}
		})
	}
}

func TestPrepareTLSAndResponseCompletionOwnership(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) }))
	defer server.Close()
	var body *completionBody
	transport := transportFunc(func(r *http.Request, _ native.TransportOptions) (*http.Response, error) {
		response, err := server.Client().Transport.RoundTrip(r)
		if response != nil {
			body = &completionBody{ReadCloser: response.Body}
			response.Body = body
		}
		return response, err
	})
	p := prepare(t, context.Background(), specFor(t, "POST", server.URL, "body"), transport)
	d, _ := dispatcher(t, newAuthority(), 1)
	result, err := native.Open(context.Background(), p, admit(t, d, p))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(result.Response.Body)
	if err != nil || !bytes.Equal(payload, []byte("payload")) || body.complete.Load() != 0 {
		t.Fatal("EOF was promoted to completion")
	}
	result.Response.Body.(interface{ MarkComplete() }).MarkComplete()
	_ = result.Response.Body.Close()
	_ = result.Close()
	_ = p.Close()
	if body.complete.Load() != 1 || body.closes.Load() != 1 {
		t.Fatal("completion/close ownership lost")
	}
}

type completionBody struct {
	io.ReadCloser
	complete, closes atomic.Int32
}

func (b *completionBody) MarkComplete() { b.complete.Add(1) }
func (b *completionBody) Close() error  { b.closes.Add(1); return b.ReadCloser.Close() }

func TestCloseRacingLateTransportResponseReleasesBody(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	responseBody := &countBody{ReadCloser: io.NopCloser(strings.NewReader("late"))}
	transport := transportFunc(func(r *http.Request, _ native.TransportOptions) (*http.Response, error) {
		close(started)
		<-release
		_ = r.Body.Close()
		return &http.Response{StatusCode: 200, Body: responseBody}, nil
	})
	p := prepare(t, context.Background(), specFor(t, "POST", "https://unused.invalid", "body"), transport)
	d, _ := dispatcher(t, newAuthority(), 1)
	permit := admit(t, d, p)
	result := make(chan error, 1)
	go func() { _, e := native.Open(context.Background(), p, permit); result <- e }()
	mustReceive(t, started)
	_ = p.Close()
	close(release)
	if err := mustReceive(t, result); !errors.Is(err, native.ErrHTTPAttemptClosed) {
		t.Fatal(err)
	}
	if responseBody.closes.Load() != 1 {
		t.Fatal("late response leaked")
	}
}

func TestOpenRetainsBothCancellationOwners(t *testing.T) {
	for _, parent := range []string{"prepare", "open"} {
		t.Run(parent, func(t *testing.T) {
			received, disconnected := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(received)
				<-r.Context().Done()
				close(disconnected)
			}))
			defer server.Close()
			prepareCtx, cancelPrepare := context.WithCancel(context.Background())
			defer cancelPrepare()
			openCtx, cancelOpen := context.WithCancel(context.Background())
			defer cancelOpen()
			p := prepare(t, prepareCtx, specFor(t, "POST", server.URL, "body"), &localTransport{base: server.Client().Transport})
			d, _ := dispatcher(t, newAuthority(), 1)
			result, err := native.Open(openCtx, p, admit(t, d, p))
			if err != nil {
				t.Fatal(err)
			}
			mustReceive(t, received)
			if parent == "prepare" {
				cancelPrepare()
			} else {
				cancelOpen()
			}
			mustReceive(t, disconnected)
			if err := result.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenNilResponseAndFailureBodyCleanup(t *testing.T) {
	for _, name := range []string{"no_response", "error_with_response", "response_close_error"} {
		t.Run(name, func(t *testing.T) {
			sentinel := errors.New("test response failure")
			source := &countBody{ReadCloser: io.NopCloser(strings.NewReader("body"))}
			response := &countBody{ReadCloser: io.NopCloser(strings.NewReader("response"))}
			transport := transportFunc(func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
				_ = req.Body.Close()
				if name == "no_response" {
					return nil, nil
				}
				if name == "response_close_error" {
					response.ReadCloser = &closeErrorBody{ReadCloser: response.ReadCloser, err: sentinel}
					return &http.Response{StatusCode: 200, Body: response}, nil
				}
				return &http.Response{StatusCode: 503, Body: response}, sentinel
			})
			spec := specFor(t, "POST", "https://unused.invalid", "body")
			spec.Request.Body = source
			spec.Identity.BodyKind = model.BodyExclusiveStream
			p, err := native.PrepareHTTP(context.Background(), spec, transport)
			if err != nil {
				t.Fatal(err)
			}
			d, _ := dispatcher(t, newAuthority(), 1)
			result, err := native.Open(context.Background(), p, admit(t, d, p))
			if name == "no_response" {
				if !errors.Is(err, native.ErrMissingHTTPResponse) || result.SendState != model.SendAmbiguous {
					t.Fatalf("missing response: %v %v", err, result.SendState)
				}
			} else if name == "error_with_response" {
				if !errors.Is(err, sentinel) || result.SendState != model.SendSent || response.closes.Load() != 1 {
					t.Fatalf("failure response ownership: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !errors.Is(result.Close(), sentinel) || !errors.Is(p.Close(), sentinel) {
					t.Fatal("close error disappeared")
				}
			}
			_ = result.Close()
			_ = p.Close()
			if source.closes.Load() != 1 {
				t.Fatal("failed transport did not release request body")
			}
			if name != "no_response" && response.closes.Load() != 1 {
				t.Fatal("response close was repeated")
			}
		})
	}
}

type closeErrorBody struct {
	io.ReadCloser
	err error
}

func (b *closeErrorBody) Close() error { return errors.Join(b.ReadCloser.Close(), b.err) }

func TestEmptyAndNilPreparedValuesAreNotCapabilities(t *testing.T) {
	var p native.PreparedHTTP
	p.Cancel(nil)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := native.Open(context.Background(), &p, &execution.SendPermit{})
	if !errors.Is(err, native.ErrInvalidHTTPPreparation) || result.SendState != model.SendNotSent {
		t.Fatal("zero preparation sent")
	}
	result, err = native.Open(context.Background(), nil, nil)
	if !errors.Is(err, native.ErrInvalidHTTPPreparation) || result.SendState != model.SendNotSent {
		t.Fatal("nil preparation sent")
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenBodyHandoffCannotBecomeDefinitelyUnsent(t *testing.T) {
	for _, which := range []string{"body", "replay"} {
		t.Run(which, func(t *testing.T) {
			sentinel := errors.New("test transport failed after body handoff")
			transport := transportFunc(func(r *http.Request, _ native.TransportOptions) (*http.Response, error) {
				trace := httptrace.ContextClientTrace(r.Context())
				trace.GetConn(r.URL.Host)
				body := r.Body
				if which == "replay" {
					var err error
					body, err = r.GetBody()
					if err != nil {
						return nil, err
					}
				}
				_, err := body.Read(make([]byte, 1))
				_ = body.Close()
				if err != nil {
					t.Error(err)
				}
				return nil, sentinel
			})
			p := prepare(t, context.Background(), specFor(t, "POST", "https://unused.invalid", "body"), transport)
			d, _ := dispatcher(t, newAuthority(), 1)
			result, err := native.Open(context.Background(), p, admit(t, d, p))
			if !errors.Is(err, sentinel) || result.SendState != model.SendAmbiguous {
				t.Fatalf("body handoff was forgotten: %v state=%v", err, result.SendState)
			}
		})
	}
}

func TestPreparedValueCopiesShareSingleOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	transport := &localTransport{base: server.Client().Transport}
	p := prepare(t, context.Background(), specFor(t, "POST", server.URL, "body"), transport)
	copyPreparation := *p
	d, budget := dispatcher(t, newAuthority(), 2)
	firstPermit, secondPermit := admit(t, d, p), admit(t, d, &copyPreparation)
	first, err := native.Open(context.Background(), p, firstPermit)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := native.Open(context.Background(), &copyPreparation, secondPermit)
	if !errors.Is(err, native.ErrHTTPAttemptOpened) || second.SendState != model.SendNotSent || transport.calls.Load() != 1 {
		t.Fatalf("preparation copy duplicated send: %v", err)
	}
	_ = second.Close()
	if _, err := io.ReadAll(first.Response.Body); err != nil {
		t.Fatal("rejected Open took the original owner's body")
	}
	if budget.Snapshot().Attempts != 2 {
		t.Fatal("provider changed the execution allowance")
	}
}

func TestPrepareRejectsTypedNilTransportAndClosesSource(t *testing.T) {
	var nilPointer *localTransport
	var nilFunction transportFunc
	for _, tr := range []native.Transport{nil, nilPointer, nilFunction} {
		spec := specFor(t, "POST", "https://unused.invalid", "body")
		source := &countBody{ReadCloser: spec.Request.Body}
		spec.Request.Body = source
		p, err := native.PrepareHTTP(context.Background(), spec, tr)
		if p != nil || !errors.Is(err, native.ErrInvalidHTTPPreparation) || source.closes.Load() != 1 {
			t.Fatalf("typed nil transport was admitted: %v", err)
		}
	}
}

func TestOpenPreservesActualDefaultAndOpenAIProfiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	for _, profile := range []native.TransportProfile{native.ProfileDefault, native.ProfileOpenAI} {
		t.Run(string(profile), func(t *testing.T) {
			transport := &localTransport{base: server.Client().Transport, inspect: func(_ *http.Request, o native.TransportOptions) {
				if o.Profile != profile {
					t.Errorf("profile silently changed to %q", o.Profile)
				}
			}}
			spec := specFor(t, "POST", server.URL, "body")
			spec.Transport.Profile = profile
			p := prepare(t, context.Background(), spec, transport)
			d, _ := dispatcher(t, newAuthority(), 1)
			result, err := native.Open(context.Background(), p, admit(t, d, p))
			if err != nil {
				t.Fatal(err)
			}
			if err := result.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenCannotSendAfterOperationOutputCommitted(t *testing.T) {
	transport := &localTransport{base: blockedRoundTripper{}}
	p := prepare(t, context.Background(), specFor(t, "POST", "https://must-not-connect.invalid", "body"), transport)
	output := execution.NewFirstOutput(time.Now())
	budget := execution.NewAttemptBudget(execution.AttemptLimits{Total: 2, PerOwner: 2})
	d, err := execution.NewDispatcher(context.Background(), "test-operation", budget, output, newAuthority())
	if err != nil {
		t.Fatal(err)
	}
	permit := admit(t, d, p)
	output.Commit()
	result, err := native.Open(context.Background(), p, permit)
	if !errors.Is(err, execution.ErrDispatchOutputCommitted) || result.SendState != model.SendNotSent || transport.calls.Load() != 0 {
		t.Fatalf("committed output allowed a new send: %v", err)
	}
	if budget.Snapshot().Attempts != 1 {
		t.Fatal("provider changed previously admitted budget")
	}
}

type authorityFunc func(context.Context, model.PreparedIdentity) (execution.DispatchValidation, error)

func (f authorityFunc) RevalidateDispatch(ctx context.Context, i model.PreparedIdentity) (execution.DispatchValidation, error) {
	return f(ctx, i)
}

func TestOpenCancellationDuringLiveRevalidationNeverSends(t *testing.T) {
	transport := &localTransport{base: blockedRoundTripper{}}
	p := prepare(t, context.Background(), specFor(t, "POST", "https://must-not-connect.invalid", "body"), transport)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	a := authorityFunc(func(context.Context, model.PreparedIdentity) (execution.DispatchValidation, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return newAuthority().validation, nil
	})
	d, err := execution.NewDispatcher(context.Background(), "test-operation", execution.NewAttemptBudget(execution.AttemptLimits{Total: 1, PerOwner: 1}), execution.NewFirstOutput(time.Now()), a)
	if err != nil {
		t.Fatal(err)
	}
	result, err := native.Open(ctx, p, admit(t, d, p))
	if !errors.Is(err, context.Canceled) || result.SendState != model.SendNotSent || transport.calls.Load() != 0 {
		t.Fatalf("cancellation escaped live check: %v", err)
	}
}

func TestOpenReportsOnlyObservedTransientTLSBeforeHTTP(t *testing.T) {
	cases := []struct {
		name                string
		handshake, returned error
		trace               bool
		handoff             bool
		want                bool
	}{
		{"eof", io.EOF, fmt.Errorf("dial TLS: %w", io.EOF), true, false, true},
		{"unexpected_eof", io.ErrUnexpectedEOF, io.ErrUnexpectedEOF, true, false, true},
		{"reset", syscall.ECONNRESET, fmt.Errorf("dial TLS: %w", syscall.ECONNRESET), true, false, true},
		{"certificate", x509.UnknownAuthorityError{}, x509.UnknownAuthorityError{}, true, false, false},
		{"different_final_error", io.EOF, context.DeadlineExceeded, true, false, false},
		{"lookalike_text", errors.New("EOF"), errors.New("EOF"), true, false, false},
		{"no_trace", io.EOF, io.EOF, false, false, false},
		{"already_handed", io.EOF, io.EOF, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := transportFunc(func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
				defer req.Body.Close()
				if tc.trace {
					trace := httptrace.ContextClientTrace(req.Context())
					trace.GetConn(req.URL.Host)
					trace.TLSHandshakeStart()
					trace.TLSHandshakeDone(tls.ConnectionState{}, tc.handshake)
					if tc.handoff {
						trace.GotConn(httptrace.GotConnInfo{})
					}
				}
				return nil, tc.returned
			})
			spec := specFor(t, "POST", "https://unused.invalid/responses", "body")
			spec.Transport.ProxyURL = "http://fixture.invalid:8118"
			p := prepare(t, context.Background(), spec, tr)
			d, _ := dispatcher(t, newAuthority(), 1)
			result, err := native.Open(context.Background(), p, admit(t, d, p))
			if !errors.Is(err, tc.returned) {
				t.Fatal("transport cause lost")
			}
			if result.PreRequest.SendState != result.SendState || result.PreRequest.BodyKind != model.BodyReplayable {
				t.Fatal("facts do not belong to this attempt")
			}
			if got := execution.PreRequestRetryAllowed(context.Background(), result.PreRequest, 0); got != tc.want {
				t.Fatalf("recovery=%v want=%v facts=%+v", got, tc.want, result.PreRequest)
			}
		})
	}
}

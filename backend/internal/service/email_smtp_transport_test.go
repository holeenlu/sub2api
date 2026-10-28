//go:build unit

package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type smtpTestCall struct {
	done chan struct{}
	err  error
}

func startSMTPTestCall(t *testing.T, cancel context.CancelFunc, call func() error) *smtpTestCall {
	t.Helper()
	r := &smtpTestCall{done: make(chan struct{})}
	go func() { r.err = call(); close(r.done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(2 * time.Second):
			t.Error("SMTP caller did not join")
		}
	})
	return r
}

func waitSMTPTestSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitSMTPTestResult(t *testing.T, call *smtpTestCall) error {
	t.Helper()
	select {
	case <-call.done:
		return call.err
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP cancellation did not interrupt real IO")
		return nil
	}
}

func TestSMTPCancellationInterruptsWireStages(t *testing.T) {
	cases := []struct {
		stage                          string
		implicit, startTLS, requireTLS bool
	}{
		{stage: "implicit_tls", implicit: true, requireTLS: true},
		{stage: "greeting"},
		{stage: "ehlo"},
		{stage: "starttls", startTLS: true, requireTLS: true},
		{stage: "auth", implicit: true, requireTLS: true},
		{stage: "data_command"},
		{stage: "data_write"},
		{stage: "data_ack"},
	}
	for _, tc := range cases {
		t.Run(tc.stage, func(t *testing.T) {
			srv, port := startStalledSMTPServer(t, tc.implicit, tc.startTLS, tc.stage)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			body := "<p>local cancellation fixture</p>"
			if tc.stage == "data_write" {
				body = strings.Repeat("x", 8<<20)
			}
			call := startSMTPTestCall(t, cancel, func() error {
				return (&EmailService{}).SendEmailWithConfig(ctx, smtpTestConfig(port, tc.requireTLS), "local@example.invalid", "cancel", body)
			})
			waitSMTPTestSignal(t, srv.reached, tc.stage)
			cancel()
			err := waitSMTPTestResult(t, call)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context cancellation, got %v", err)
			}
			if tc.stage == "data_write" && !strings.Contains(err.Error(), "write msg") {
				t.Fatalf("did not interrupt DATA write: %v", err)
			}
			srv.releaseOnce.Do(func() { close(srv.release) })
			waitSMTPTestSignal(t, srv.disconnected, "peer EOF")
			if tc.stage == "implicit_tls" && srv.conns.Load() != 1 {
				t.Fatal("cancellation triggered a TLS fallback connection")
			}
			if (tc.stage == "starttls" || tc.stage == "implicit_tls") && srv.sawCommand("AUTH") {
				t.Fatal("authentication continued after canceled TLS")
			}
		})
	}
}

func TestSMTPDeadlineCancelsBlockedAuthentication(t *testing.T) {
	srv, port := startStalledSMTPServer(t, false, false, "auth")
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	call := startSMTPTestCall(t, cancel, func() error { return (&EmailService{}).TestSMTPConnectionWithConfig(ctx, smtpTestConfig(port, false)) })
	waitSMTPTestSignal(t, srv.reached, "authentication")
	if err := waitSMTPTestResult(t, call); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline identity lost: %v", err)
	}
	waitSMTPTestSignal(t, srv.disconnected, "peer EOF")
}

func TestSMTPAlreadyCanceledDoesNotDial(t *testing.T) {
	srv, port := startFakeSMTPServer(t, false, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (&EmailService{}).SendEmailWithConfig(ctx, smtpTestConfig(port, false), "local@example.invalid", "cancel", "body"); !errors.Is(err, context.Canceled) {
		t.Fatalf("send: %v", err)
	}
	if srv.conns.Load() != 0 {
		t.Fatal("already canceled send opened a socket")
	}
}

func TestSMTPAcceptedDataRemainsSuccessWhenQuitIsCanceled(t *testing.T) {
	srv, port := startStalledSMTPServer(t, false, false, "quit")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	call := startSMTPTestCall(t, cancel, func() error {
		return (&EmailService{}).SendEmailWithConfig(ctx, smtpTestConfig(port, false), "local@example.invalid", "accepted", "body")
	})
	waitSMTPTestSignal(t, srv.reached, "post-DATA QUIT")
	cancel()
	if err := waitSMTPTestResult(t, call); err != nil {
		t.Fatalf("accepted mail became retryable: %v", err)
	}
	waitSMTPTestSignal(t, srv.disconnected, "peer EOF")
}

func TestSMTPAcceptedDataIgnoresNonstandardQuitReply(t *testing.T) {
	_, port := startStalledSMTPServer(t, false, false, "quit_reject")
	if err := (&EmailService{}).SendEmailWithConfig(t.Context(), smtpTestConfig(port, false), "local@example.invalid", "accepted", "body"); err != nil {
		t.Fatal(err)
	}
}

func TestSMTPAuthenticatedProbeRemainsSuccessWhenQuitIsCanceled(t *testing.T) {
	srv, port := startStalledSMTPServer(t, false, false, "quit")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	call := startSMTPTestCall(t, cancel, func() error { return (&EmailService{}).TestSMTPConnectionWithConfig(ctx, smtpTestConfig(port, false)) })
	waitSMTPTestSignal(t, srv.reached, "authenticated probe QUIT")
	cancel()
	if err := waitSMTPTestResult(t, call); err != nil {
		t.Fatalf("authenticated probe was reclassified: %v", err)
	}
	waitSMTPTestSignal(t, srv.disconnected, "peer EOF")
	if srv.sawCommand("DATA") {
		t.Fatal("connection probe sent a message")
	}
}

func TestSMTPCertificateFailureDoesNotDowngradeOrAuthenticate(t *testing.T) {
	srv, port := startFakeSMTPServer(t, true, false)
	smtpTestRootCAs = x509.NewCertPool()
	err := (&EmailService{}).SendEmailWithConfig(t.Context(), smtpTestConfig(port, true), "local@example.invalid", "untrusted", "body")
	var untrusted x509.UnknownAuthorityError
	if !errors.As(err, &untrusted) {
		t.Fatalf("certificate verification identity lost: %v", err)
	}
	if srv.conns.Load() != 1 || srv.sawCommand("AUTH") || srv.sawCommand("STARTTLS") {
		t.Fatal("certificate failure retried through a weaker path")
	}
}

func TestSMTPPostHandshakeRecordErrorDoesNotFallback(t *testing.T) {
	srv, port := startStalledSMTPServer(t, true, false, "post_tls_record_error")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := (&EmailService{}).SendEmailWithConfig(ctx, smtpTestConfig(port, true), "local@example.invalid", "invalid record", "body")
	var recordErr tls.RecordHeaderError
	if !errors.As(err, &recordErr) {
		t.Fatalf("expected post-handshake record error, got: %v", err)
	}
	var handshakeErr *smtpImplicitHandshakeError
	if errors.As(err, &handshakeErr) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("post-handshake failure crossed the handshake boundary: %v", err)
	}
	if srv.conns.Load() != 1 || srv.sawCommand("AUTH") {
		t.Fatalf("post-handshake failure caused fallback: %v", err)
	}
}

type smtpCloseGate struct {
	net.Conn
	entered, release chan struct{}
	count            atomic.Int64
}

func (c *smtpCloseGate) Close() error {
	c.count.Add(1)
	close(c.entered)
	<-c.release
	return c.Conn.Close()
}

func TestSMTPConnectionFinishJoinsActualCancellationClose(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	gate := &smtpCloseGate{Conn: client, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(gate.release) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	conn := newSMTPConnection(ctx, gate)
	cancel()
	waitSMTPTestSignal(t, gate.entered, "physical close")
	finished := make(chan error, 1)
	go func() { finished <- conn.finish() }()
	select {
	case <-finished:
		t.Fatal("finish returned before physical Close")
	case <-time.After(10 * time.Millisecond):
	}
	release.Do(func() { close(gate.release) })
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not join")
	}
	select {
	case <-conn.cancellationDone:
	default:
		t.Fatal("callback was not joined")
	}
	if err := conn.finish(); err != nil {
		t.Fatal(err)
	}
	if gate.count.Load() != 1 {
		t.Fatal("physical socket closed more than once")
	}
}

type smtpCloseErrorConnection struct {
	net.Conn
	err error
}

func (c smtpCloseErrorConnection) Close() error { return errors.Join(c.Conn.Close(), c.err) }

func TestSMTPConnectionFinishReportsPhysicalCloseError(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	want := errors.New("local physical close error")
	conn := newSMTPConnection(t.Context(), smtpCloseErrorConnection{Conn: client, err: want})
	if err := conn.finish(); !errors.Is(err, want) {
		t.Fatalf("close error lost: %v", err)
	}
	select {
	case <-conn.cancellationDone:
	default:
		t.Fatal("cleanup returned without joining cancellation")
	}
}

type smtpDeadlineRecorder struct {
	net.Conn
	deadline time.Time
}

func (c *smtpDeadlineRecorder) SetDeadline(deadline time.Time) error {
	c.deadline = deadline
	return nil
}

func TestSMTPDeadlineNeverExtendsCallerBudget(t *testing.T) {
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	conn := &smtpDeadlineRecorder{Conn: client}
	deadline := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	if err := setSMTPDeadline(ctx, conn, smtpIOTimeout); err != nil {
		t.Fatal(err)
	}
	if !conn.deadline.Equal(deadline) {
		t.Fatalf("caller deadline extended to %v", conn.deadline)
	}
	before := time.Now()
	if err := setSMTPDeadline(context.Background(), conn, smtpIOTimeout); err != nil {
		t.Fatal(err)
	}
	if conn.deadline.Before(before.Add(smtpIOTimeout)) || conn.deadline.After(time.Now().Add(smtpIOTimeout)) {
		t.Fatal("existing IO limit changed")
	}
}

type smtpSettingsFixture struct {
	SettingRepository
	values map[string]string
}

func (r smtpSettingsFixture) GetMultiple(ctx context.Context, _ []string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.values, nil
}
func (r smtpSettingsFixture) GetValue(ctx context.Context, key string) (string, error) {
	return r.values[key], ctx.Err()
}

func TestBalanceNotifyLifecycleRealSMTPCancelAndJoin(t *testing.T) {
	srv, port := startStalledSMTPServer(t, false, false, "auth")
	settings := smtpSettingsFixture{values: map[string]string{
		SettingKeySMTPHost: "127.0.0.1", SettingKeySMTPPort: strconv.Itoa(port), SettingKeySMTPUsername: "local", SettingKeySMTPPassword: "local-fixture", SettingKeySMTPFrom: "local@example.invalid",
		SettingKeyBalanceLowNotifyEnabled: "true", SettingKeyBalanceLowNotifyThreshold: "10",
	}}
	notifier := NewBalanceNotifyService(NewEmailService(settings, nil), settings, nil)
	if err := notifier.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := notifier.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	user := &User{ID: 1, BalanceNotifyEnabled: true, BalanceNotifyExtraEmails: []NotifyEmailEntry{{Email: "local@example.invalid", Verified: true}}}
	notifier.CheckBalanceAfterDeduction(t.Context(), user, 11, 2)
	waitSMTPTestSignal(t, srv.reached, "real SMTP authentication")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := notifier.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	waitSMTPTestSignal(t, srv.disconnected, "SMTP EOF after notifier stop")
	select {
	case <-notifier.tasks.done:
	default:
		t.Fatal("notifier returned before SMTP finished")
	}
	notifier.CheckBalanceAfterDeduction(t.Context(), user, 11, 2)
	if srv.conns.Load() != 1 {
		t.Fatal("stopped notifier created another SMTP connection")
	}
}

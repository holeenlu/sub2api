package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"sync"
	"time"
)

const smtpDialTimeout = 10 * time.Second
const smtpIOTimeout = 20 * time.Second

// smtpConnection owns the physical socket, including the cancellation callback.
// Cancellation closes the raw socket, so SMTP reads/writes and either TLS
// handshake actually unblock. finish joins the callback before returning.
type smtpConnection struct {
	net.Conn
	closeOnce        sync.Once
	closeErr         error
	finishOnce       sync.Once
	stopCancellation func() bool
	cancellationDone chan struct{}
}

func newSMTPConnection(ctx context.Context, conn net.Conn) *smtpConnection {
	c := &smtpConnection{Conn: conn, cancellationDone: make(chan struct{})}
	c.stopCancellation = context.AfterFunc(ctx, func() {
		defer close(c.cancellationDone)
		_ = c.Close()
	})
	return c
}

func (c *smtpConnection) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.Conn.Close() })
	return c.closeErr
}

func (c *smtpConnection) finish() error {
	c.finishOnce.Do(func() {
		_ = c.Close()
		if c.stopCancellation() {
			close(c.cancellationDone)
		} else {
			<-c.cancellationDone
		}
	})
	return c.closeErr
}

type smtpSession struct {
	*smtp.Client
	connection *smtpConnection
}

// Fallback is limited to the original implicit-handshake boundary. A malformed
// TLS record while reading the SMTP greeting must never start another session.
type smtpImplicitHandshakeError struct{ err error }

func (e *smtpImplicitHandshakeError) Error() string { return "tls dial: " + e.err.Error() }
func (e *smtpImplicitHandshakeError) Unwrap() error { return e.err }

func smtpContextError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cause := ctx.Err(); cause != nil {
		return errors.Join(cause, err)
	}
	// A socket deadline can fire before the context timer gets CPU time.
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return errors.Join(context.DeadlineExceeded, err)
	}
	return err
}

func setSMTPDeadline(ctx context.Context, conn net.Conn, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	if caller, ok := ctx.Deadline(); ok && caller.Before(deadline) {
		deadline = caller
	}
	return conn.SetDeadline(deadline)
}

// SendEmailWithConfig uses the existing SMTP policy with caller-owned lifetime.
// DATA's positive final response commits delivery; cancellation/QUIT failures
// after that point must not turn success into a retryable send failure.
func (s *EmailService) SendEmailWithConfig(ctx context.Context, config *SMTPConfig, to, subject, body string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	message, err := buildSMTPMessage(config, to, subject, body)
	if err != nil {
		return err
	}
	client, err := s.connectSMTP(ctx, config)
	if err != nil {
		return err
	}
	accepted := false
	defer func() {
		closeErr := client.connection.finish()
		if !accepted {
			err = errors.Join(smtpContextError(ctx, err), closeErr)
		} else if closeErr != nil {
			slog.Warn("SMTP cleanup failed after accepted delivery", "error", closeErr)
		}
	}()
	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err = client.Mail(message.envelopeFrom); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err = client.Rcpt(message.envelopeTo); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err = w.Write(message.data); err != nil {
		return fmt.Errorf("write msg: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}
	accepted = true
	_ = client.Quit() // Existing policy: ignore non-standard QUIT responses.
	return nil
}

// smtpTestRootCAs is injected only by serial local-server tests. Production uses
// the system trust chain, with the same TLS 1.2 minimum and hostname checks.
var smtpTestRootCAs *x509.CertPool

func smtpTLSConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: smtpTestRootCAs}
}

// connectSMTP preserves implicit TLS -> mandatory STARTTLS fallback only for
// a plaintext TLS record header. TLS-disabled connections still opportunistically
// upgrade if advertised. No fallback occurs because of cancellation or timeout.
func (s *EmailService) connectSMTP(ctx context.Context, config *SMTPConfig) (*smtpSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	tlsConfig := smtpTLSConfig(config.Host)
	if config.UseTLS {
		client, err := connectSMTPImplicitTLS(ctx, dialer, addr, config.Host, tlsConfig)
		if err == nil {
			return client, nil
		}
		var handshakeErr *smtpImplicitHandshakeError
		var recordErr tls.RecordHeaderError
		if ctx.Err() != nil || !errors.As(err, &handshakeErr) || !errors.As(handshakeErr.err, &recordErr) {
			return nil, smtpContextError(ctx, err)
		}
		return connectSMTPStartTLS(ctx, dialer, addr, config.Host, tlsConfig, true)
	}
	return connectSMTPStartTLS(ctx, dialer, addr, config.Host, tlsConfig, false)
}

func connectSMTPImplicitTLS(ctx context.Context, dialer *net.Dialer, addr, host string, config *tls.Config) (_ *smtpSession, err error) {
	// One existing 10s dial budget includes the implicit TLS handshake.
	dialCtx, cancel := context.WithTimeout(ctx, smtpDialTimeout)
	defer cancel()
	raw, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, smtpContextError(ctx, fmt.Errorf("tls dial: %w", err))
	}
	conn := newSMTPConnection(ctx, raw)
	transferred := false
	defer func() {
		if !transferred {
			err = errors.Join(smtpContextError(ctx, err), conn.finish())
		}
	}()
	if err = setSMTPDeadline(dialCtx, conn, smtpDialTimeout); err != nil {
		return nil, err
	}
	tlsConn := tls.Client(conn, config)
	if err = tlsConn.HandshakeContext(dialCtx); err != nil {
		return nil, &smtpImplicitHandshakeError{err: err}
	}
	if err = setSMTPDeadline(ctx, conn, smtpIOTimeout); err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(tlsConn, host)
	if err != nil {
		return nil, fmt.Errorf("new smtp client: %w", err)
	}
	transferred = true
	return &smtpSession{Client: client, connection: conn}, nil
}

func connectSMTPStartTLS(ctx context.Context, dialer *net.Dialer, addr, host string, config *tls.Config, mandatory bool) (_ *smtpSession, err error) {
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, smtpContextError(ctx, fmt.Errorf("smtp dial: %w", err))
	}
	conn := newSMTPConnection(ctx, raw)
	transferred := false
	defer func() {
		if !transferred {
			err = errors.Join(smtpContextError(ctx, err), conn.finish())
		}
	}()
	if err = setSMTPDeadline(ctx, conn, smtpIOTimeout); err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return nil, fmt.Errorf("new smtp client: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); !ok {
		if mandatory {
			return nil, errors.New("smtp server does not support STARTTLS")
		}
	} else if err = client.StartTLS(config); err != nil {
		return nil, fmt.Errorf("starttls: %w", err)
	}
	transferred = true
	return &smtpSession{Client: client, connection: conn}, nil
}

// TestSMTPConnectionWithConfig shares the exact connect/authentication path.
// As before, authenticated sessions remain successful despite QUIT anomalies.
func (s *EmailService) TestSMTPConnectionWithConfig(ctx context.Context, config *SMTPConfig) (err error) {
	client, err := s.connectSMTP(ctx, config)
	if err != nil {
		return fmt.Errorf("smtp connection failed: %w", err)
	}
	authenticated := false
	defer func() {
		closeErr := client.connection.finish()
		if !authenticated {
			err = errors.Join(smtpContextError(ctx, err), closeErr)
		} else if closeErr != nil {
			slog.Warn("SMTP cleanup failed after successful authentication", "error", closeErr)
		}
	}()
	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("smtp authentication failed: %w", err)
	}
	authenticated = true
	_ = client.Quit()
	return nil
}

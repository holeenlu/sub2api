// Package openai owns prepared, single-use native OpenAI HTTP attempts.
// Protocol parsing, recovery, authorization and billing remain with their owners.
package openai

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
)

var (
	ErrInvalidHTTPPreparation = errors.New("invalid native HTTP preparation")
	ErrReplayBodyLimit        = errors.New("native HTTP replay body exceeds its existing limit")
	ErrHTTPAttemptOpened      = errors.New("native HTTP preparation was already opened")
	ErrHTTPAttemptClosed      = errors.New("native HTTP attempt is closed")
	ErrMissingHTTPResponse    = errors.New("native HTTP transport returned neither response nor error")
)

// Transport is injected by trusted composition, never selected by a request.
// RoundTripOnce must use the existing controlled client/pool, TLS, proxy and
// destination checks. It sends exactly this request: no late encoding, redirects,
// authentication refresh or retry. A redirect is returned to the caller. Existing
// TLS-before-HTTP recovery belongs to execution and needs another admitted attempt.
// Implementations must honor cancellation, close request bodies, and return
// response bodies whose Close releases their existing connection/in-flight state.
// This deliberately does not match legacy HTTPUpstream.Do/DoWithTLS.
type Transport interface {
	RoundTripOnce(*http.Request, TransportOptions) (*http.Response, error)
}

// TransportProfile names the existing native HTTP policy selected at the root.
// Default and OpenAI are intentionally distinct: native API-key, OAuth and probe
// paths do not all use the same client/TLS policy. The empty value is invalid;
// the root must explicitly map the actual resolved profile, never infer it from
// the account type or substitute OpenAI for the existing default policy.
type TransportProfile string

const (
	ProfileDefault  TransportProfile = "default"
	ProfileOpenAI   TransportProfile = "openai"
	ProfileExcelBPS TransportProfile = "excel_bps"
)

// TransportOptions are the security/connection inputs actually consumed by the
// controlled transport, including the actual preselected profile. AccountID
// must equal the sealed resource, not its possibly different credential owner.
// PublicHostsOnly preserves the existing stronger destination-check policy.
// TLSProfile is detached during Prepare and again before handing it to transport.
type TransportOptions struct {
	Profile            TransportProfile
	AccountID          int64
	AccountConcurrency int
	ProxyURL           string
	TLSProfile         *tlsfingerprint.Profile
	PublicHostsOnly    bool
}

// Spec supplies the final encoded request after all authentication, target and
// security header changes. Identity supplies the trusted operation/principal/
// resource/grant facts and BodyKind; its derived digests and BodyHandle must be
// zero. Prepare derives them rather than trusting caller-provided hashes.
// MaxReplayBodyBytes is the existing ingress/endpoint limit, not a new provider
// default. It must be positive for replayable bodies; streams are never buffered.
// Prepare takes ownership of Request.Body on entry, including errors. The caller
// must stop reading/writing/closing that body. Other request fields are copied;
// dynamic trailer mutation after preparation is not an admitted wire contract.
type Spec struct {
	Request            *http.Request
	Identity           model.PreparedIdentity
	Transport          TransportOptions
	MaxReplayBodyBytes int64
}

// PreparedHTTP owns a sealed request and its lifetime. Value copies share the
// same private one-shot state; they cannot duplicate stream ownership. Identity
// is detached. Callers cannot mutate the sealed request or change its transport
// after admission. Close also works before Open. Its zero value cannot be opened.
type PreparedHTTP struct {
	*preparedHTTPState
}

type preparedHTTPState struct {
	identity           model.PreparedIdentity
	request            *http.Request
	options            TransportOptions
	transport          Transport
	maxReplayBodyBytes int64
	ctx                context.Context
	cancel             context.CancelCauseFunc
	body               *ownedBody

	// mu protects only ownership transitions; I/O and external callbacks run
	// outside it. A response arriving after Close is closed by Open itself.
	mu       sync.Mutex
	opened   bool
	closed   bool
	response *ownedBody
	stopLife func() bool
	stopOpen func() bool
	closeOne sync.Once
	closeErr error
}

// Identity returns a detached seal; nil/zero preparations have no valid identity.
func (p *PreparedHTTP) Identity() model.PreparedIdentity {
	if p == nil || p.preparedHTTPState == nil {
		return model.PreparedIdentity{}
	}
	return p.identity
}

// Result exposes the original HTTP response without interpreting frames, EOF,
// usage or delivery. SendState concerns transmission only; even HTTP errors can
// be Sent. Call Close after processing a response. Cancel never starts recovery.
// A non-nil Result is returned on every Open failure so NotSent is explicit.
type Result struct {
	Response        *http.Response
	SendState       model.SendState
	AttemptSequence uint64
	// PreRequest is detached evidence, not a retry action or a second budget.
	// It is populated only after transport is invoked; its zero value denies recovery.
	PreRequest execution.PreRequestFailure
	owner      *PreparedHTTP
}

// Open consumes the execution-issued capability exactly once immediately before
// invoking the single-send transport. It never constructs an authority, budget
// or permit. A failed Open consumes this preparation; retry requires Prepare.
// ctx and the preparation's original context both retain cancellation ownership.
func Open(ctx context.Context, p *PreparedHTTP, permit *execution.SendPermit) (*Result, error) {
	result := &Result{SendState: model.SendNotSent}
	if p == nil || p.preparedHTTPState == nil || ctx == nil || !p.identity.Valid() || p.transport == nil {
		return result, ErrInvalidHTTPPreparation
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return result, ErrHTTPAttemptClosed
	}
	if p.opened {
		p.mu.Unlock()
		return result, ErrHTTPAttemptOpened
	}
	p.opened = true
	p.mu.Unlock()
	result.owner = p

	stop := context.AfterFunc(ctx, func() { p.Cancel(context.Cause(ctx)) })
	p.mu.Lock()
	closed := p.closed
	if !closed {
		p.stopOpen = stop
	}
	p.mu.Unlock()
	if closed {
		stop()
	}
	if err := ctx.Err(); err != nil {
		p.Cancel(context.Cause(ctx))
	}
	if err := permit.Consume(p.ctx, p.identity); err != nil {
		return result, errors.Join(err, p.Close())
	}
	result.AttemptSequence = permit.AttemptSequence()
	// Connection acquisition can outlive the permission checked above. Cancel
	// that wait at the ORIGINAL send deadline, then disarm only when request
	// headers are actually written. Do not impose this on the response lifetime.
	sendGuard := &sendDeadlineGuard{cancel: p.cancel}
	sendGuard.tighten(permit.SendDeadline())
	defer sendGuard.stop()
	// The open caller can cancel during live revalidation before its AfterFunc
	// runs. Check that context synchronously as well as the owned lifetime.
	if err := ctx.Err(); err != nil {
		p.Cancel(context.Cause(ctx))
		return result, errors.Join(context.Cause(ctx), p.Close())
	}
	if err := p.ctx.Err(); err != nil {
		return result, errors.Join(context.Cause(p.ctx), p.Close())
	}

	var evidence transportdiag.Trace
	var sent atomic.Bool
	var writeAttempted atomic.Bool
	var handshakeFailed, transientHandshake atomic.Bool
	checkHandoff := func(info httptrace.GotConnInfo) {
		if writeAttempted.Load() {
			p.rejectHTTPHandoff(info, execution.ErrSendPermitConsumed)
			return
		}
		if err := permit.RevalidateConsumed(p.ctx, p.identity); err != nil {
			p.rejectHTTPHandoff(info, err)
			return
		}
		sendGuard.tighten(permit.SendDeadline())
		if cause := context.Cause(p.ctx); cause != nil {
			p.rejectHTTPHandoff(info, cause)
		}
	}
	trace := &httptrace.ClientTrace{
		WroteHeaders: func() { writeAttempted.Store(true); sendGuard.wroteHeaders() },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err != nil {
				handshakeFailed.Store(true)
				if transientTLSFailure(err) {
					transientHandshake.Store(true)
				}
			}
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			writeAttempted.Store(true)
			if info.Err == nil {
				sent.Store(true)
			}
		},
		GotFirstResponseByte: func() { sent.Store(true) },
	}
	req := evidence.Request(p.request)
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	// WithClientTrace composes into this request-private trace. Run observers
	// first, then the final veto: an earlier observer may cancel the operation.
	// Do not mutate the caller's trace or invoke its callbacks twice.
	previousGotConn := trace.GotConn
	trace.GotConn = func(info httptrace.GotConnInfo) {
		if previousGotConn != nil {
			previousGotConn(info)
		}
		checkHandoff(info)
	}
	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &observedBody{ReadCloser: req.Body, trace: &evidence}
	}
	if replay := req.GetBody; replay != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			body, err := replay()
			if err != nil {
				return nil, err
			}
			return &observedBody{ReadCloser: body, trace: &evidence}, nil
		}
	}
	// Prepared bytes stay private even if a trusted transport mutates its input.
	resp, err := p.transport.RoundTripOnce(req, cloneTransportOptions(p.options))
	result.SendState = model.SendAmbiguous
	switch {
	case resp != nil || sent.Load():
		result.SendState = model.SendSent
	case evidence.DefinitelyUnsent():
		result.SendState = model.SendNotSent
	}
	if resp == nil && err == nil {
		err = ErrMissingHTTPResponse
	}
	result.PreRequest = execution.PreRequestFailure{
		SendState: result.SendState, BodyKind: p.identity.BodyKind,
		OpenAIProfile:                p.options.Profile == ProfileOpenAI,
		HTTPS:                        strings.EqualFold(p.request.URL.Scheme, "https"),
		ConfiguredProxy:              strings.TrimSpace(p.options.ProxyURL) != "",
		TLSHandshakeFailed:           handshakeFailed.Load(),
		TransientTLSHandshakeFailure: transientHandshake.Load(),
		TransientTransportFailure:    transientTLSFailure(err),
	}
	if resp != nil && resp.Body != nil {
		owned := &ownedBody{ReadCloser: resp.Body}
		resp.Body = &responseBody{ownedBody: owned, owner: p}
		p.mu.Lock()
		closed = p.closed
		if !closed {
			p.response = owned
		}
		p.mu.Unlock()
		if closed {
			err = errors.Join(err, owned.Close())
		}
	}
	result.Response = resp
	if cause := context.Cause(p.ctx); cause != nil {
		err = errors.Join(err, cause)
	}
	if err != nil {
		return result, errors.Join(err, p.Close())
	}
	return result, nil
}

// Preserve the legacy classifier's exact error identities; certificate, DNS,
// timeout, cancellation and arbitrary EOF-like text do not prove transient TLS.
func transientTLSFailure(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// Cancel stops the producer and closes owned streams. The first cancellation
// cause wins; a nil cause means context.Canceled. Repeated cancellation is safe.
func (p *PreparedHTTP) Cancel(cause error) {
	if p == nil || p.preparedHTTPState == nil || p.cancel == nil {
		return
	}
	p.cancel(cause)
	_ = p.Close() // Close retains its error for the owning caller.
}

// Close releases both body owners and their cancellation hooks exactly once.
// It cannot undo a sent request and never supplies a usage/terminal fact.
func (p *PreparedHTTP) Close() error {
	if p == nil || p.preparedHTTPState == nil || p.cancel == nil {
		return nil
	}
	p.closeOne.Do(func() {
		p.mu.Lock()
		p.closed = true
		response, stopLife, stopOpen := p.response, p.stopLife, p.stopOpen
		p.mu.Unlock()
		if stopLife != nil {
			stopLife()
		}
		if stopOpen != nil {
			stopOpen()
		}
		p.cancel(ErrHTTPAttemptClosed)
		p.closeErr = errors.Join(p.body.Close(), response.Close())
	})
	return p.closeErr
}

// Cancel stops this result's attempt; nil/rejected results own no resources.
func (r *Result) Cancel(cause error) {
	if r != nil {
		r.owner.Cancel(cause)
	}
}

// Close releases the attempt and returns its retained first close outcome.
func (r *Result) Close() error {
	if r == nil {
		return nil
	}
	return r.owner.Close()
}

type ownedBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (b *ownedBody) Close() error {
	if b == nil {
		return nil
	}
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

type observedBody struct {
	io.ReadCloser
	trace *transportdiag.Trace
}

func (b *observedBody) Read(buf []byte) (int, error) {
	if len(buf) > 0 {
		b.trace.MarkBodyRead()
	}
	return b.ReadCloser.Read(buf)
}

type responseBody struct {
	*ownedBody
	owner *PreparedHTTP
}

func (b *responseBody) Close() error { return b.owner.Close() }

// MarkComplete preserves the controlled transport's existing explicit protocol
// completion hook. EOF/Close alone must not report a complete healthy stream.
func (b *responseBody) MarkComplete() {
	if complete, ok := b.ReadCloser.(interface{ MarkComplete() }); ok {
		complete.MarkComplete()
	}
}

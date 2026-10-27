package repository

import (
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Adapted from ranxi2001/sub2api@11be589504b482d77827ab383b4b53f777241238.
// Observe every OpenAI attempt, including direct connections and non-replayable
// bodies. Only an observed connection acquisition with no HTTP handoff proves
// that nothing was sent. Keep the evidence across redirects in each Client.Do;
// a custom transport that emits no trace remains an unknown execution.
func doWithOpenAIPreRequestRetry(client *http.Client, req *http.Request, proxyURL string, profile service.HTTPUpstreamProfile) (*http.Response, error) {
	if req == nil || req.URL == nil || profile != service.HTTPUpstreamProfileOpenAI {
		return doUpstreamRequest(client, req)
	}
	doAttempt := func(request *http.Request) (*http.Response, error, bool) {
		var mu sync.Mutex
		var acquiring, transientFailure, handedToHTTP bool
		markAcquiring := func() { mu.Lock(); acquiring = true; mu.Unlock() }
		markHTTP := func() { mu.Lock(); handedToHTTP = true; mu.Unlock() }
		trace := &httptrace.ClientTrace{
			GetConn:           func(string) { markAcquiring() },
			DNSStart:          func(httptrace.DNSStartInfo) { markAcquiring() },
			ConnectStart:      func(string, string) { markAcquiring() },
			TLSHandshakeStart: markAcquiring,
			TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
				mu.Lock()
				if acquiring && transientTLSHandshakeError(err) {
					transientFailure = true
				}
				mu.Unlock()
			},
			GotConn:              func(httptrace.GotConnInfo) { markHTTP() },
			WroteHeaderField:     func(string, []string) { markHTTP() },
			WroteHeaders:         markHTTP,
			WroteRequest:         func(httptrace.WroteRequestInfo) { markHTTP() },
			GotFirstResponseByte: markHTTP,
		}
		resp, err := doUpstreamRequest(client, request.Clone(httptrace.WithClientTrace(request.Context(), trace)))
		mu.Lock()
		unsent := acquiring && !handedToHTTP && resp == nil
		retry := unsent && transientFailure && transientTLSHandshakeError(err)
		mu.Unlock()
		if err != nil {
			err = &service.OpenAITransportAttemptError{Cause: err, Unsent: unsent}
		}
		return resp, err, retry
	}
	resp, err, retry := doAttempt(req)
	if err == nil || !retry || req.Context().Err() != nil || req.URL.Scheme != "https" ||
		strings.TrimSpace(proxyURL) == "" || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, err
	}
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-req.Context().Done():
		return nil, &service.OpenAITransportAttemptError{Cause: req.Context().Err(), Unsent: true}
	case <-timer.C:
	}
	second := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody {
		body, bodyErr := req.GetBody()
		if bodyErr != nil {
			return nil, err
		}
		second.Body = body
	}
	if budgetErr := service.TakeOpenAITurnAttempt(req.Context()); budgetErr != nil {
		if second.Body != nil {
			_ = second.Body.Close()
		}
		return nil, budgetErr
	}
	// Same client/proxy and TLS verification; at most one additional attempt.
	slog.Info("openai.upstream_pre_request_retry", "attempt", 2, "reason", "tls_handshake_before_http")
	resp, err, _ = doAttempt(second)
	return resp, err
}

func transientTLSHandshakeError(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

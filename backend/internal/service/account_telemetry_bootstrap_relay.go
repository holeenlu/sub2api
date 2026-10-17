package service

import (
	"bytes"
	"context"
	"crypto/subtle"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

// The official CLI uses a private, short-lived loopback endpoint so bootstrap
// follows the account's existing proxy (including SOCKS), relay and TLS policy.
// It is not a public route and cannot schedule another account or refresh tokens.
func (s *GatewayService) startClaudeTelemetryBootstrapRelay(ctx context.Context, account *Account, token string) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, telemetryForwardFailure("telemetry_bootstrap_relay_unavailable")
	}
	var messages, counts atomic.Int32
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 4 * time.Minute, IdleTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || (r.URL.Path != "/v1/messages" && r.URL.Path != "/v1/messages/count_tokens") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !accountTelemetryEligible(account) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		current, err := s.accountRepo.GetByID(ctx, account.ID)
		if err != nil || current == nil || current.Status != StatusActive || (current.ExpiresAt != nil && !current.ExpiresAt.After(time.Now())) ||
			telemetryCredential(current, "account_uuid", "email") != telemetryCredential(account, "account_uuid", "email") ||
			validateVendorNativeDispatch(ctx, s.accountRepo, current, nil) != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// One hello inference, no automatic retry or unbounded CLI tool loop.
		if (r.URL.Path == "/v1/messages" && messages.Add(1) > 1) || (r.URL.Path == "/v1/messages/count_tokens" && counts.Add(1) > 2) {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, AccountTelemetryMaxBodyBytes))
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		target, proxy, err := s.claudeTelemetryTarget(account, r.URL.Path)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.URL.Query().Get("beta") == "true" {
			parsed, parseErr := url.Parse(target)
			if parseErr != nil {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			query := parsed.Query()
			query.Set("beta", "true")
			parsed.RawQuery = query.Encode()
			target = parsed.String()
		}
		request, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), r.Method, target, bytes.NewReader(body))
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		request.Header = accountTelemetryHeaders(r.Header)
		request.Header.Set("Anthropic-Beta", r.Header.Get("Anthropic-Beta"))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", r.Header.Get("Accept"))
		response, err := s.httpUpstream.DoWithTLS(request, proxy, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
		if response != nil && response.Body != nil {
			defer response.Body.Close()
		}
		if err != nil || response == nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		for _, name := range []string{"Content-Type", "Request-Id", "Retry-After"} {
			if value := response.Header.Get(name); value != "" {
				w.Header().Set(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		if response.Body != nil {
			_, _ = io.Copy(&telemetryFlushWriter{ResponseWriter: w}, io.LimitReader(response.Body, 4<<20))
		}
	})
	go func() { _ = server.Serve(listener) }()
	return "http://" + listener.Addr().String(), func() { _ = server.Close() }, nil
}

type telemetryFlushWriter struct{ http.ResponseWriter }

func (w *telemetryFlushWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if flush, ok := w.ResponseWriter.(http.Flusher); ok {
		flush.Flush()
	}
	return n, err
}

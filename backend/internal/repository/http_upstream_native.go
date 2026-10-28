package repository

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ native.Transport = (*httpUpstreamService)(nil)
var _ service.NativeHTTPUpstream = (*httpUpstreamService)(nil)

// ValidateNativeRedirect is the existing transport policy exposed to the
// operation's explicit redirect controller. It sends nothing. The subsequent
// hop still needs a newly sealed request, live permission and one network call.
func (s *httpUpstreamService) ValidateNativeRedirect(request *http.Request, via []*http.Request, proxy string) error {
	if request == nil || request.URL == nil || request.URL.User != nil || len(via) == 0 {
		return native.ErrInvalidHTTPPreparation
	}
	if service.HTTPUpstreamRedirectsDisabled(request.Context()) {
		return http.ErrUseLastResponse
	}
	check := s.redirectChecker
	parsed, err := url.Parse(proxy)
	if err != nil {
		return err
	}
	if proxyutil.IsVendorProxy(parsed) {
		check = proxyutil.VendorRedirectPolicy(check)
	}
	return check(request, via)
}

// PrepareNativeRequest performs the existing repository-owned header/encoding
// steps BEFORE provider preparation seals the wire identity. It transfers Body
// ownership, closes on failure, and sends no network request. maxSourceBytes is
// the caller's already established finite request bound, not a new global limit.
// Streaming/non-replayable requests are not consumed merely to fabricate a hash.
func (s *httpUpstreamService) PrepareNativeRequest(req *http.Request, maxSourceBytes int64) (_ *http.Request, err error) {
	if req == nil || req.URL == nil {
		return nil, native.ErrInvalidHTTPPreparation
	}
	wire := req.Clone(req.Context())
	defer func() {
		if err != nil && wire.Body != nil {
			err = errors.Join(err, wire.Body.Close())
		}
	}()
	applyGrokCLIProxyHeaders(wire)
	if err := s.validateRequestHost(wire); err != nil {
		return nil, err
	}
	if err := wire.Context().Err(); err != nil {
		return nil, context.Cause(wire.Context())
	}
	started := time.Now()
	semanticLength := wire.ContentLength
	if codexResponsesCompressionEligible(wire) {
		if maxSourceBytes <= 0 || maxSourceBytes == math.MaxInt64 || wire.ContentLength > maxSourceBytes {
			return nil, native.ErrReplayBodyLimit
		}
		// The old encoder reads GetBody. Replace that replay function from the
		// actual owned body, so a caller cannot supply different replay bytes.
		var closeOnce sync.Once
		var closeErr error
		closeSource := func() { closeOnce.Do(func() { closeErr = req.Body.Close() }) }
		stop := context.AfterFunc(wire.Context(), closeSource)
		payload, readErr := io.ReadAll(io.LimitReader(wire.Body, maxSourceBytes+1))
		stop()
		closeSource()
		wire.Body = nil
		if readErr != nil || closeErr != nil {
			return nil, errors.Join(readErr, closeErr, context.Cause(wire.Context()))
		}
		if int64(len(payload)) > maxSourceBytes {
			return nil, native.ErrReplayBodyLimit
		}
		if semanticLength > 0 && semanticLength != int64(len(payload)) {
			return nil, native.ErrInvalidHTTPPreparation
		}
		semanticLength = int64(len(payload))
		wire.Body = httputil.NewPrereadBody(payload)
		wire.ContentLength = semanticLength
		wire.GetBody = func() (io.ReadCloser, error) { return httputil.NewPrereadBody(payload), nil }
		if err := compressCodexResponsesRequest(wire); err != nil {
			return nil, err
		}
	}
	encoding := wire.Header.Get("Content-Encoding")
	if encoding == "" {
		encoding = "identity"
	}
	service.RecordOpenAIRequestBodyEncoding(wire.Context(), semanticLength, wire.ContentLength, encoding, time.Since(started))
	return wire, nil
}

// RoundTripOnce implements the prepared native transport with the same pool,
// destination protection, TLS/proxy and in-flight ownership as generic callers.
// It performs no header rewriting, compression, redirect follow or application
// retry. Any second controlled send must return through execution admission.
func (s *httpUpstreamService) RoundTripOnce(req *http.Request, options native.TransportOptions) (*http.Response, error) {
	if req == nil || req.URL == nil || options.AccountID <= 0 || options.AccountConcurrency < 0 {
		return nil, native.ErrInvalidHTTPPreparation
	}
	var expected service.HTTPUpstreamProfile
	switch options.Profile {
	case native.ProfileDefault:
		expected = service.HTTPUpstreamProfileDefault
	case native.ProfileOpenAI:
		expected = service.HTTPUpstreamProfileOpenAI
	case native.ProfileExcelBPS:
		expected = service.HTTPUpstreamProfileExcelBPS
	default:
		return nil, native.ErrInvalidHTTPPreparation
	}
	if actual := service.HTTPUpstreamProfileFromContext(req.Context()); actual != expected {
		return nil, native.ErrInvalidHTTPPreparation
	}
	// Refuse a context/identity mismatch instead of weakening the existing
	// public-host-only policy or changing it after the request was sealed.
	if service.HTTPUpstreamPublicHostsOnly(req.Context()) != options.PublicHostsOnly {
		return nil, native.ErrInvalidHTTPPreparation
	}
	return s.doControlledHTTP(req, options.ProxyURL, options.AccountID, options.AccountConcurrency, options.TLSProfile, sealedNativeHTTPRequest)
}

func sealedNativeHTTPRequest(client *http.Client, req *http.Request, _ string, _ service.HTTPUpstreamProfile) (*http.Response, error) {
	if client.Jar != nil {
		return nil, errors.New("prepared native transport cannot inject implicit cookies")
	}
	controlled := *client
	controlled.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithCancel(req.Context())
	wire := req.Clone(ctx)
	// Explicit recovery can reconstruct from the operation input. The HTTP
	// transport must not replay a generation using an unregistered GetBody call.
	wire.GetBody = nil
	resp, err := servertiming.Do(&controlled, wire)
	if err != nil {
		cancel()
		return resp, err
	}
	decompressResponseBody(resp)
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// Keep one eligibility predicate for standard and pre-sealed request encoding.
func codexResponsesCompressionEligible(req *http.Request) bool {
	if req == nil || req.URL == nil || req.Method != http.MethodPost ||
		!strings.EqualFold(req.URL.Scheme, "https") || !strings.EqualFold(req.URL.Hostname(), "chatgpt.com") ||
		req.URL.Path != "/backend-api/codex/responses" || req.Body == nil || req.GetBody == nil {
		return false
	}
	mediaType, _, _ := strings.Cut(req.Header.Get("Content-Type"), ";")
	if !strings.EqualFold(strings.TrimSpace(mediaType), "application/json") {
		return false
	}
	encoding := strings.TrimSpace(req.Header.Get("Content-Encoding"))
	return encoding == "" || strings.EqualFold(encoding, "identity")
}

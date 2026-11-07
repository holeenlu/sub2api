package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// PrepareHTTP consumes only the actual Body, never a potentially different
// GetBody. It sends nothing and never resolves/replaces grant or account identity.
// ctx is the operation/preparation lifetime, including any existing trace values.
// Native HTTP control requests (GET/DELETE etc.) use exactly the same boundary.
func PrepareHTTP(ctx context.Context, spec Spec, transport Transport) (_ *PreparedHTTP, err error) {
	var source *ownedBody
	if spec.Request != nil && spec.Request.Body != nil {
		source = &ownedBody{ReadCloser: spec.Request.Body}
	}
	transferred := false
	defer func() {
		if !transferred {
			err = errors.Join(err, source.Close())
		}
	}()
	if ctx == nil || nilTransport(transport) || !validSpec(spec) {
		return nil, ErrInvalidHTTPPreparation
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	life, cancel := context.WithCancelCause(ctx)
	succeeded := false
	defer func() {
		if !succeeded {
			cancel(ErrHTTPAttemptClosed)
		}
	}()
	req := spec.Request.Clone(life)
	if req.Method == "" {
		req.Method = http.MethodGet
	}
	// Request.Clone intentionally does not clone or own Body/GetBody.
	req.GetBody = nil
	identity := spec.Identity
	var body *ownedBody
	switch identity.BodyKind {
	case model.BodyReplayable:
		if spec.MaxReplayBodyBytes <= 0 || spec.MaxReplayBodyBytes == math.MaxInt64 {
			return nil, ErrInvalidHTTPPreparation
		}
		if req.ContentLength > spec.MaxReplayBodyBytes {
			return nil, ErrReplayBodyLimit
		}
		var payload []byte
		if source != nil {
			// net/http's Body contract requires Close to unblock a pending Read.
			stop := context.AfterFunc(life, func() { _ = source.Close() })
			payload, err = io.ReadAll(io.LimitReader(source, spec.MaxReplayBodyBytes+1))
			stop()
			err = errors.Join(err, source.Close())
			if err != nil {
				return nil, errors.Join(err, context.Cause(life))
			}
		}
		if int64(len(payload)) > spec.MaxReplayBodyBytes {
			return nil, ErrReplayBodyLimit
		}
		if req.ContentLength > 0 && req.ContentLength != int64(len(payload)) {
			return nil, ErrInvalidHTTPPreparation
		}
		identity.BodyDigest = sha256.Sum256(payload)
		req.ContentLength = int64(len(payload))
		req.GetBody = func() (io.ReadCloser, error) {
			if len(payload) == 0 {
				return http.NoBody, nil
			}
			return io.NopCloser(bytes.NewReader(payload)), nil
		}
		sealed, _ := req.GetBody() // Locally constructed, infallible byte reader.
		body = &ownedBody{ReadCloser: sealed}
		req.Body = body
		if len(payload) == 0 {
			req.Body = http.NoBody
		}
	case model.BodyExclusiveStream:
		if source == nil || spec.Request.Body == http.NoBody {
			return nil, ErrInvalidHTTPPreparation
		}
		// This is an opaque ownership handle, not an invented content digest or
		// authorization epoch. The final bytes are explicitly unknown.
		if _, err := rand.Read(identity.BodyHandle[:]); err != nil {
			return nil, err
		}
		body, req.Body = source, source
	}
	options := cloneTransportOptions(spec.Transport)
	identity.TargetDigest, identity.WireDigest, err = requestDigests(req, options, identity)
	if err != nil || !identity.Valid() {
		return nil, errors.Join(ErrInvalidHTTPPreparation, err, body.Close())
	}
	if err := life.Err(); err != nil {
		return nil, errors.Join(context.Cause(life), body.Close())
	}
	p := &PreparedHTTP{preparedHTTPState: &preparedHTTPState{identity: identity, request: req, options: options, transport: transport, maxReplayBodyBytes: spec.MaxReplayBodyBytes, ctx: life, cancel: cancel, body: body}}
	// Install while holding mu so a cancellation callback cannot miss its hook.
	p.mu.Lock()
	p.stopLife = context.AfterFunc(life, func() { p.Cancel(context.Cause(life)) })
	p.mu.Unlock()
	transferred, succeeded = true, true
	return p, nil
}

// ReprepareHTTP reconstructs only the immutable bytes and transport sealed by a
// prior replayable preparation. The operation owner must justify recovery and
// acquire another permit from its original Dispatcher before Open. This method
// performs no I/O, chooses no retry, and cannot replay an exclusive stream.
// Closing the prior attempt does not erase these immutable preparation inputs.
func ReprepareHTTP(ctx context.Context, prior *PreparedHTTP) (*PreparedHTTP, error) {
	if ctx == nil || prior == nil || prior.preparedHTTPState == nil {
		return nil, ErrInvalidHTTPPreparation
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	if prior.identity.BodyKind != model.BodyReplayable || prior.request.GetBody == nil {
		return nil, ErrInvalidHTTPPreparation
	}
	request := prior.request.Clone(ctx)
	body, err := prior.request.GetBody()
	if err != nil {
		return nil, err
	}
	request.Body = body
	identity := prior.identity
	identity.TargetDigest, identity.WireDigest, identity.BodyDigest = [32]byte{}, [32]byte{}, [32]byte{}
	identity.BodyHandle = [16]byte{}
	prepared, err := PrepareHTTP(ctx, Spec{Request: request, Identity: identity,
		Transport: prior.options, MaxReplayBodyBytes: prior.maxReplayBodyBytes}, prior.transport)
	if err != nil {
		return nil, err
	}
	if prepared.Identity() != prior.Identity() {
		return nil, errors.Join(ErrInvalidHTTPPreparation, prepared.Close())
	}
	return prepared, nil
}

// HTTPControlRequest returns a detached request for the operation's HTTP
// redirect controller. Its replay function is derived only from the sealed
// bytes. It grants no permission to send: every controller hop must still use
// Prepare, Admit and Open. Close the returned body if it is not consumed.
// Exclusive streams cannot create a second reader through this boundary.
func HTTPControlRequest(ctx context.Context, prepared *PreparedHTTP) (*http.Request, error) {
	if ctx == nil || prepared == nil || prepared.preparedHTTPState == nil ||
		prepared.identity.BodyKind != model.BodyReplayable || prepared.request.GetBody == nil {
		return nil, ErrInvalidHTTPPreparation
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	request := prepared.request.Clone(ctx)
	body, err := prepared.request.GetBody()
	if err != nil {
		return nil, err
	}
	request.Body = body
	return request, nil
}

func nilTransport(transport Transport) bool {
	if transport == nil {
		return true
	}
	switch v := reflect.ValueOf(transport); v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func validSpec(s Spec) bool {
	r, i := s.Request, s.Identity
	if r == nil || r.URL == nil || r.URL.Host == "" || r.URL.User != nil ||
		(r.URL.Scheme != "http" && r.URL.Scheme != "https") || r.RequestURI != "" ||
		s.Transport.AccountID != i.ResourceID || s.Transport.AccountConcurrency < 0 ||
		(s.Transport.Profile != ProfileDefault && s.Transport.Profile != ProfileOpenAI && s.Transport.Profile != ProfileExcelBPS) ||
		i.OperationID == "" || i.ResourceID <= 0 || i.Owner.AccountID <= 0 ||
		!model.ValidExecutionPrincipal(i.Principal, i.UserID, i.APIKeyID, i.GroupID, i.GroupBound, i.ProbeID, i.ProbeClaimDigest) || i.GrantDigest == ([32]byte{}) ||
		(i.Mode != "native" && i.Mode != "bps") || i.Transport != "http" ||
		i.TargetDigest != ([32]byte{}) || i.WireDigest != ([32]byte{}) ||
		i.BodyDigest != ([32]byte{}) || i.BodyHandle != ([16]byte{}) {
		return false
	}
	if i.Mode == "bps" {
		purpose, ok := i.Role.Purpose()
		if !ok || purpose != i.Purpose || !i.Binding.Valid("bps") || s.Transport.Profile != ProfileExcelBPS {
			return false
		}
	} else if s.Transport.Profile == ProfileExcelBPS {
		return false
	}
	return i.BodyKind == model.BodyReplayable || i.BodyKind == model.BodyExclusiveStream
}

func requestDigests(req *http.Request, options TransportOptions, identity model.PreparedIdentity) ([32]byte, [32]byte, error) {
	// JSON encodes map keys deterministically and delimits every field. Include
	// req.Host separately: it may differ from URL.Host for authenticated upstreams.
	target, err := json.Marshal(struct {
		Method    string
		URL       string
		Host      string
		Redirects bool
		Options   TransportOptions
	}{req.Method, req.URL.String(), req.Host, false, options})
	if err != nil {
		return [32]byte{}, [32]byte{}, err
	}
	targetDigest := sha256.Sum256(target)
	wire, err := json.Marshal(struct {
		Target           [32]byte
		Headers          http.Header
		Trailers         http.Header
		ContentLength    int64
		TransferEncoding []string
		Close            bool
		BodyKind         model.BodyKind
		BodyDigest       [32]byte
		BodyHandle       [16]byte
	}{targetDigest, req.Header, req.Trailer, req.ContentLength, req.TransferEncoding, req.Close, identity.BodyKind, identity.BodyDigest, identity.BodyHandle})
	if err != nil {
		return [32]byte{}, [32]byte{}, err
	}
	return targetDigest, sha256.Sum256(wire), nil
}

func cloneTransportOptions(options TransportOptions) TransportOptions {
	if options.TLSProfile != nil {
		p := *options.TLSProfile
		p.CipherSuites = slices.Clone(p.CipherSuites)
		p.Curves = slices.Clone(p.Curves)
		p.PointFormats = slices.Clone(p.PointFormats)
		p.SignatureAlgorithms = slices.Clone(p.SignatureAlgorithms)
		p.ALPNProtocols = slices.Clone(p.ALPNProtocols)
		p.SupportedVersions = slices.Clone(p.SupportedVersions)
		p.KeyShareGroups = slices.Clone(p.KeyShareGroups)
		p.PSKModes = slices.Clone(p.PSKModes)
		p.Extensions = slices.Clone(p.Extensions)
		options.TLSProfile = &p
	}
	return options
}

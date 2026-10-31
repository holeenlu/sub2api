package httputil

import (
	"context"
	"crypto/sha256"
	"hash"
	"io"
	"net/http"
	"sync"
)

type requestWireDigestKey struct{}
type requestWireCapture struct {
	mu       sync.Mutex
	digest   hash.Hash
	complete bool
	failed   bool
}

// CaptureRequestWire installs one observer before the first actual body read.
// A pre-read body without provenance cannot be promoted to original wire.
// Replacing the working body later retains this same technical observation.
func CaptureRequestWire(req *http.Request) {
	if req == nil || req.Context().Value(requestWireDigestKey{}) != nil {
		return
	}
	if _, preread := req.Body.(*PrereadBody); preread {
		return
	}
	capture := &requestWireCapture{digest: sha256.New(), complete: req.Body == nil || req.Body == http.NoBody}
	*req = *req.WithContext(context.WithValue(req.Context(), requestWireDigestKey{}, capture))
	if !capture.complete {
		req.Body = &requestWireReader{ReadCloser: req.Body, capture: capture}
	}
}

// RequestWireDigest returns a hash only after the original reader reached EOF.
// A decoded replacement, partial read, limit error or failed read is not proof
// of complete original wire, even when a content length happened to match.
func RequestWireDigest(req *http.Request) ([32]byte, bool) {
	var result [32]byte
	if req == nil {
		return result, false
	}
	capture, _ := req.Context().Value(requestWireDigestKey{}).(*requestWireCapture)
	if capture == nil {
		return result, false
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if !capture.complete || capture.failed {
		return result, false
	}
	copy(result[:], capture.digest.Sum(nil))
	return result, true
}

// A later tightening wrapper can reject a read after the inner source returned
// EOF. Propagate that read failure into this same proof, never install another
// hash observer or certify an over-limit original body from its inner EOF.
func failRequestWireRead(req *http.Request) {
	capture, _ := req.Context().Value(requestWireDigestKey{}).(*requestWireCapture)
	if capture != nil {
		capture.mu.Lock()
		capture.failed = true
		capture.mu.Unlock()
	}
}

type requestWireReader struct {
	io.ReadCloser
	capture *requestWireCapture
}

func (r *requestWireReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.capture.mu.Lock()
	defer r.capture.mu.Unlock()
	if n > 0 {
		_, _ = r.capture.digest.Write(p[:n])
	}
	if err == io.EOF {
		r.capture.complete = true
	} else if err != nil {
		r.capture.failed = true
	}
	return n, err
}

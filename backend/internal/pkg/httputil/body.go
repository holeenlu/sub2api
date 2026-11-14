package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	requestBodyReadInitCap    = 512
	requestBodyReadMaxInitCap = 1 << 20
	jsonUTF8BOMLen            = 3
	// Callers without an endpoint budget retain the generic decoded-body cap.
	maxDecompressedBodySize = 64 << 20
	// RFC 9659 bounds the zstd HTTP content-coding window at 8 MiB.
	maxZstdRequestWindowSize = 8 << 20
)

var errUnsupportedContentEncoding = errors.New("unsupported Content-Encoding")

type requestBodyLimitKey struct{}

// WithRequestBodyLimit carries the route body budget through preread and decode
// layers. MaxBytesReader independently enforces the same budget on wire bytes.
// Nested middleware may tighten a positive budget, but cannot widen it.
func WithRequestBodyLimit(ctx context.Context, limit int64) context.Context {
	if limit <= 0 {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if current, _ := ctx.Value(requestBodyLimitKey{}).(int64); current > 0 && current <= limit {
		return ctx
	}
	return context.WithValue(ctx, requestBodyLimitKey{}, limit)
}

func effectiveRequestBodyLimit(req *http.Request, explicit int64) (limit int64, routed bool) {
	if req != nil {
		if route, _ := req.Context().Value(requestBodyLimitKey{}).(int64); route > 0 {
			routed = true
			if explicit <= 0 || route < explicit {
				explicit = route
			}
		}
	}
	if explicit <= 0 {
		explicit = maxDecompressedBodySize
	}
	return explicit, routed
}

// Check cancellation between underlying reads and after each codec block. This
// does not interrupt a Read already executing inside a codec or network stack.
type requestBodyContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r requestBodyContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if err == nil || err == io.EOF {
		if canceled := r.ctx.Err(); canceled != nil {
			return n, canceled
		}
	}
	return n, err
}

func requestBodyContentEncoding(header http.Header) (string, error) {
	var encoding string
	count := 0
	for name, values := range header {
		if !strings.EqualFold(name, "Content-Encoding") {
			continue
		}
		for _, value := range values {
			count++
			if count > 1 || strings.Contains(value, ",") {
				return "", fmt.Errorf("%w: multiple codings are not supported", errUnsupportedContentEncoding)
			}
			encoding = strings.ToLower(strings.TrimSpace(value))
		}
	}
	switch encoding {
	case "", "identity", "zstd", "gzip", "x-gzip", "deflate":
		return encoding, nil
	default:
		return encoding, fmt.Errorf("%w: %q", errUnsupportedContentEncoding, encoding)
	}
}

// PrereadBody 回填已读取完成的请求体：作为 io.ReadCloser 可被再次顺序消费
// （multipart 流式解析），同时暴露 Bytes() 让 ReadRequestBodyWithPrealloc
// 直接返回原始切片，避免二次分配与复制。
//
// 注意：ReadRequestBodyWithPrealloc 对 PrereadBody 的快速路径不检查内部
// reader 是否已被（部分）消费——包装的字节完整且不可变，即使 reader 已被
// 流式消费过，Bytes() 也始终返回完整请求体。
type PrereadBody struct {
	body   []byte
	reader *bytes.Reader
}

// NewPrereadBody 包装一段已读取的请求体。
func NewPrereadBody(body []byte) *PrereadBody {
	return &PrereadBody{body: body, reader: bytes.NewReader(body)}
}

// Read 实现 io.Reader（转发给内部 bytes.Reader）。
func (p *PrereadBody) Read(b []byte) (int, error) {
	if p == nil {
		return 0, io.EOF
	}
	return p.reader.Read(b)
}

// WriteTo preserves bytes.Reader's direct copy path for HTTP uploads while
// advancing the same cursor as Read. Bytes still exposes the full body.
func (p *PrereadBody) WriteTo(w io.Writer) (int64, error) {
	if p == nil {
		return 0, nil
	}
	return p.reader.WriteTo(w)
}

// Close 实现 io.Closer；请求体已在内存中，无需释放资源。
func (p *PrereadBody) Close() error { return nil }

// Bytes 返回完整的原始请求体切片。
func (p *PrereadBody) Bytes() []byte {
	if p == nil {
		return nil
	}
	return p.body
}

// ReadRequestBodyWithPrealloc reads request body with preallocated buffer based
// on content length, transparently decoding any Content-Encoding the upstream
// client used to compress the body (zstd, gzip, deflate).
// 已由 PrereadBody 回填的请求体直接返回其完整切片（零拷贝），不检查内部
// reader 是否已被消费——见 PrereadBody 的文档说明。
func ReadRequestBodyWithPrealloc(req *http.Request) ([]byte, error) {
	limit, routed := effectiveRequestBodyLimit(req, 0)
	return readRequestBodyWithLimit(req, limit, routed)
}
func ReadRequestBodyWithPreallocLimit(req *http.Request, maxDecodedBytes int64) ([]byte, error) {
	limit, _ := effectiveRequestBodyLimit(req, maxDecodedBytes)
	return readRequestBodyWithLimit(req, limit, true)
}
func readRequestBodyWithLimit(req *http.Request, limit int64, enforce bool) ([]byte, error) {
	CaptureRequestWire(req)
	if req == nil || req.Body == nil {
		return nil, nil
	}
	started := time.Now()
	if err := req.Context().Err(); err != nil {
		return nil, newBodyReadError(req, err, 0, started, "read")
	}
	enc, err := requestBodyContentEncoding(req.Header)
	if err != nil {
		return nil, newBodyReadError(req, err, 0, started, "decode")
	}
	if preread, ok := req.Body.(*PrereadBody); ok {
		if enforce && int64(len(preread.Bytes())) > limit {
			return nil, newBodyReadError(req, &http.MaxBytesError{Limit: limit}, 0, time.Now(), "cached_size")
		}
		if err := req.Context().Err(); err != nil {
			return nil, newBodyReadError(req, err, 0, started, "read")
		}
		return preread.Bytes(), nil
	}

	capHint := requestBodyReadInitCap
	if req.ContentLength > 0 {
		switch {
		case req.ContentLength < int64(requestBodyReadInitCap):
			capHint = requestBodyReadInitCap
		case req.ContentLength > int64(requestBodyReadMaxInitCap):
			capHint = requestBodyReadMaxInitCap
		default:
			capHint = int(req.ContentLength)
		}
	}

	var reader io.Reader = requestBodyContextReader{ctx: req.Context(), reader: req.Body}
	if enforce && (enc == "" || enc == "identity") {
		readLimit := limit
		if readLimit < 1<<63-1 {
			readLimit++
		}
		reader = io.LimitReader(reader, readLimit)
		capHint = int(min(int64(capHint), readLimit))
	}
	raw, bytesRead, err := readRequestBodyChunks(reader, capHint, req.ContentLength, req.Context())
	if err != nil {
		failRequestWireRead(req)
		return nil, newBodyReadError(req, err, bytesRead, started, "read")
	}

	keepRaw := false
	defer func() {
		if !keepRaw {
			releaseRequestBodyMemory(req.Context(), int64(cap(raw)))
		}
	}()

	if enc == "" || enc == "identity" {
		if enforce && int64(len(raw)) > limit {
			return nil, newBodyReadError(req, &http.MaxBytesError{Limit: limit}, bytesRead, started, "decoded_size")
		}
		if err := req.Context().Err(); err != nil {
			return nil, newBodyReadError(req, err, bytesRead, started, "read")
		}
		keepRaw = true
		return raw, nil
	}

	decoded, err := decompressRequestBodyLimit(req.Context(), enc, raw, limit)
	if err != nil {
		phase := "decode"
		var limitErr *http.MaxBytesError
		if errors.As(err, &limitErr) {
			phase = "decoded_size"
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// Cancellation aborts the body read; it is not malformed encoded
			// content. Preserve the existing cancellation/timeout attribution.
			phase = "read"
		}
		return nil, newBodyReadError(req, fmt.Errorf("decode Content-Encoding %q: %w", enc, err), bytesRead, started, phase)
	}

	req.Header.Del("Content-Encoding")
	req.Header.Del("Content-Length")
	req.ContentLength = int64(len(decoded))

	return decoded, nil
}

// Read bounded chunks as bytes arrive, then assemble the exact-size result.
// This avoids doubling large buffers or eagerly allocating an untrusted
// Content-Length before the corresponding bytes have arrived.
type bodyCountingReader struct {
	io.Reader
	read int64
}

func (r *bodyCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += int64(n)
	return n, err
}
func readRequestBodyChunks(reader io.Reader, initialCapacity int, contentLength int64, contexts ...context.Context) ([]byte, int64, error) {
	ctx := context.Background()
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	counted := &bodyCountingReader{Reader: reader}
	body, err := readRequestBodyChunksWithBudget(ctx, counted, initialCapacity, contentLength)
	return body.data, counted.read, err
}

type requestBodyBuffer struct {
	data    []byte
	charged int64
}

func (b *requestBodyBuffer) release(ctx context.Context) {
	charged := b.charged
	*b = requestBodyBuffer{}
	releaseRequestBodyMemory(ctx, charged)
}

func readRequestBodyChunksWithBudget(ctx context.Context, reader io.Reader, initialCapacity int, contentLength int64) (result requestBodyBuffer, err error) {
	var reserved int64
	defer func() {
		// Chunks and the EOF probe are temporary once the final buffer is
		// assembled. On failure there is no result, so refund all allocations.
		releaseRequestBodyMemory(ctx, reserved-result.charged)
	}()
	capacity := initialCapacity
	var chunks [][]byte
	total := 0
	for {
		chunkCapacity := capacity
		if remaining := contentLength - int64(total); remaining >= 0 && remaining < int64(chunkCapacity) {
			chunkCapacity = int(remaining) + 1
		}
		if err := reserveRequestBodyMemory(ctx, int64(chunkCapacity)); err != nil {
			return requestBodyBuffer{}, err
		}
		reserved += int64(chunkCapacity)
		chunk := make([]byte, chunkCapacity)
		n := 0
		var err error
		for n < len(chunk) && err == nil {
			if err := ctx.Err(); err != nil {
				return requestBodyBuffer{}, err
			}
			var read int
			read, err = reader.Read(chunk[n:])
			n += read
		}
		if err != nil && err != io.EOF {
			return requestBodyBuffer{}, err
		}
		if err := ctx.Err(); err != nil {
			return requestBodyBuffer{}, err
		}
		if n > 0 {
			chunks = append(chunks, chunk[:n])
			total += n
		}
		if err != nil {
			if len(chunks) == 0 {
				return requestBodyBuffer{}, nil
			}
			if len(chunks) == 1 {
				return requestBodyBuffer{data: chunks[0], charged: int64(cap(chunks[0]))}, nil
			}
			if err := reserveRequestBodyMemory(ctx, int64(total)); err != nil {
				return requestBodyBuffer{}, err
			}
			reserved += int64(total)
			body := make([]byte, total)
			offset := 0
			for _, part := range chunks {
				offset += copy(body[offset:], part)
			}
			return requestBodyBuffer{data: body, charged: int64(total)}, nil
		}
		if capacity < requestBodyReadMaxInitCap {
			capacity *= 2
			if capacity > requestBodyReadMaxInitCap {
				capacity = requestBodyReadMaxInitCap
			}
		}
	}
}

// ReadLenientJSONRequestBodyWithPrealloc reads a request body and normalizes
// JSON string control bytes before strict validation.
func ReadLenientJSONRequestBodyWithPrealloc(req *http.Request, maxNormalizedBytes int64) ([]byte, error) {
	ctx := context.Background()
	if req != nil {
		ctx = req.Context()
	}
	limit, _ := effectiveRequestBodyLimit(req, maxNormalizedBytes)
	normalizedLimit := limit
	// Use the endpoint's explicit decoded-body budget for compressed input as
	// well. Reserve only the BOM that normalization removes; expansion remains
	// bounded by the effective route/explicit limit below.
	if limit <= (1<<63-1)-jsonUTF8BOMLen {
		limit += jsonUTF8BOMLen
	}
	body, err := readRequestBodyWithLimit(req, limit, true)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) && size.Limit == limit {
			size.Limit = normalizedLimit
			// The outer error logger reads the failure-time snapshot as well as
			// this error chain; both must report the route budget, not BOM room.
			*req = *req.WithContext(context.WithValue(req.Context(), bodyReadFailureKey{}, DescribeRequestBodyReadFailure(req, err)))
		}
		return nil, err
	}
	started := time.Now()
	if req != nil {
		if err := req.Context().Err(); err != nil {
			return nil, newBodyReadError(req, err, int64(len(body)), started, "read")
		}
	}
	normalizedBuffer, err := normalizeLenientJSONRequestBody(ctx, body, normalizedLimit)
	normalized := normalizedBuffer.data
	borrowed := false
	if req != nil {
		_, borrowed = req.Body.(*PrereadBody)
	}
	if !borrowed && (err != nil || normalizedBuffer.charged > 0) {
		releaseRequestBodyMemory(ctx, int64(cap(body)))
	}
	if err != nil {
		// Size limits hit while normalizing are reported like decoded-size
		// limits: never attributed to the client leaving.
		return nil, newBodyReadError(req, err, int64(len(body)), started, "normalize")
	}
	if req != nil {
		if err := req.Context().Err(); err != nil {
			return nil, newBodyReadError(req, err, int64(len(body)), started, "read")
		}
	}
	return normalized, nil
}

// decompressRequestBodyLimit always rejects decoded bodies above limit, even
// for callers that skip the identity length check: a truncated prefix would
// otherwise reach the JSON/multipart parser and surface as a 400 instead of 413.
func decompressRequestBodyLimit(ctx context.Context, encoding string, raw []byte, limit int64) ([]byte, error) {
	read := func(r io.Reader) ([]byte, error) {
		readLimit := limit
		if readLimit < 1<<63-1 {
			readLimit++
		}
		data, _, err := readRequestBodyChunks(io.LimitReader(requestBodyContextReader{ctx: ctx, reader: r}, readLimit), requestBodyReadInitCap, -1, ctx)
		if int64(len(data)) > limit {
			releaseRequestBodyMemory(ctx, int64(cap(data)))
			return nil, &http.MaxBytesError{Limit: limit}
		}
		if err == nil {
			err = ctx.Err()
		}
		return data, err
	}
	input := requestBodyContextReader{ctx: ctx, reader: bytes.NewReader(raw)}
	switch encoding {
	case "zstd":
		if err := reserveRequestBodyMemory(ctx, maxZstdRequestWindowSize); err != nil {
			return nil, err
		}
		defer releaseRequestBodyMemory(ctx, maxZstdRequestWindowSize)
		// MaxMemory also covers single-segment frames whose window is their
		// advertised content size. Keep these decoder errors distinct from the
		// observed output limit: a large window is not proof of a large body.
		// HTTP decoders must accept an 8 MiB window even for a small body budget.
		dec, err := zstd.NewReader(input, zstd.WithDecoderMaxWindow(maxZstdRequestWindowSize),
			zstd.WithDecoderMaxMemory(maxZstdRequestWindowSize), zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		return read(dec)
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(input)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		return read(gr)
	case "deflate":
		zr, err := zlib.NewReader(input)
		if err != nil {
			return nil, err
		}
		defer func() { _ = zr.Close() }()
		return read(zr)
	default:
		return nil, errUnsupportedContentEncoding
	}
}

// NormalizeLenientJSONRequestBody escapes raw control bytes that broken
// OpenAI-compatible clients sometimes place inside JSON strings.
func NormalizeLenientJSONRequestBody(body []byte, maxNormalizedBytes int64) ([]byte, error) {
	normalized, err := normalizeLenientJSONRequestBody(context.Background(), body, maxNormalizedBytes)
	return normalized.data, err
}

func normalizeLenientJSONRequestBody(ctx context.Context, body []byte, maxNormalizedBytes int64) (requestBodyBuffer, error) {
	if err := ctx.Err(); err != nil {
		return requestBodyBuffer{}, err
	}
	if maxNormalizedBytes <= 0 {
		maxNormalizedBytes = maxDecompressedBodySize
	}

	body = trimUTF8BOM(body)
	if len(body) == 0 {
		return requestBodyBuffer{data: body}, nil
	}
	if int64(len(body)) > maxNormalizedBytes {
		return requestBodyBuffer{}, &http.MaxBytesError{Limit: maxNormalizedBytes}
	}

	normalizedSize := int64(len(body))
	inString := false
	escaped := false
	for _, b := range body {
		if inString && isJSONControlByte(b) {
			if maxNormalizedBytes-normalizedSize < 5 {
				return requestBodyBuffer{}, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			normalizedSize += 5
			escaped = false
			continue
		}

		switch {
		case escaped:
			escaped = false
		case inString && b == '\\':
			escaped = true
		case b == '"':
			inString = !inString
		}
	}
	if err := ctx.Err(); err != nil {
		return requestBodyBuffer{}, err
	}
	if normalizedSize == int64(len(body)) {
		return requestBodyBuffer{data: body}, nil
	}
	if err := reserveRequestBodyMemory(ctx, normalizedSize); err != nil {
		return requestBodyBuffer{}, err
	}
	// Allocate once after counting expansion, so escaped control bytes cannot
	// bypass the ingress budget through append's geometric capacity growth.
	out := make([]byte, 0, int(normalizedSize))
	inString, escaped = false, false
	for _, b := range body {
		if inString && isJSONControlByte(b) {
			out = appendJSONUnicodeEscape(out, b)
			escaped = false
			continue
		}
		switch {
		case escaped:
			escaped = false
		case inString && b == '\\':
			escaped = true
		case b == '"':
			inString = !inString
		}
		out = append(out, b)
	}
	if err := ctx.Err(); err != nil {
		releaseRequestBodyMemory(ctx, normalizedSize)
		return requestBodyBuffer{}, err
	}
	return requestBodyBuffer{data: out, charged: normalizedSize}, nil
}

func trimUTF8BOM(body []byte) []byte {
	if len(body) >= jsonUTF8BOMLen && body[0] == 0xef && body[1] == 0xbb && body[2] == 0xbf {
		return body[jsonUTF8BOMLen:]
	}
	return body
}

func isJSONControlByte(b byte) bool {
	return b < 0x20 || b == 0x7f
}

func appendJSONUnicodeEscape(dst []byte, b byte) []byte {
	const hex = "0123456789abcdef"
	return append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0x0f])
}

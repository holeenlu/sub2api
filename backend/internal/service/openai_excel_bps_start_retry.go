package service

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// BPS reports its provider organization's TPM/RPM limit inside an HTTP 200
// stream, right after response.created/in_progress. The limit is shared by
// every BPS account, so switching or cooling accounts cannot help. No model
// output exists at that point, so the same request may be sent again on the
// same account after the upstream's sub-second hint. Once anything else has
// been seen, or the window expires, the stream keeps the no-replay rule.
const (
	excelBPSStartPeekWindow       = 5 * time.Second
	excelBPSStartPeekMaxBytes     = 16 << 20
	excelBPSStartRetryMaxAttempts = 3
	excelBPSStartRetryMinDelay    = 100 * time.Millisecond
	excelBPSStartRetryMaxDelay    = 2 * time.Second
	excelBPSStartRetryBudget      = 5 * time.Second
)

// excelBPSStartPeek reads upstream SSE until the first event that is not
// response lifecycle metadata. Every consumed byte is retained so the stream
// can continue byte-for-byte when no retry happens.
type excelBPSStartPeek struct {
	upstream  io.ReadCloser
	reader    *bufio.Reader
	held      []byte
	rateLimit []byte
	err       error
	done      chan struct{}
}

func startExcelBPSPeek(body io.ReadCloser) *excelBPSStartPeek {
	peek := &excelBPSStartPeek{upstream: body, reader: bufio.NewReaderSize(body, 64<<10), done: make(chan struct{})}
	go peek.run()
	return peek
}

func (p *excelBPSStartPeek) run() {
	defer close(p.done)
	var line, data []byte
	event := ""
	for {
		chunk, err := p.reader.ReadSlice('\n')
		p.held = append(p.held, chunk...)
		if len(p.held) > excelBPSStartPeekMaxBytes {
			return
		}
		if err == bufio.ErrBufferFull {
			line = append(line, chunk...)
			continue
		}
		if err != nil {
			p.err = err
			return
		}
		line = bytes.TrimRight(append(line, chunk...), "\r\n")
		switch {
		case len(line) == 0:
			if len(data) > 0 {
				payload := bytes.TrimSuffix(data, []byte("\n"))
				kind := gjson.GetBytes(payload, "type").String()
				if kind == "" {
					kind = event
				}
				switch kind {
				case "response.created", "response.in_progress", "response.queued":
				default:
					hasOutput := len(gjson.GetBytes(payload, "response.output").Array()) > 0 ||
						gjson.GetBytes(payload, "response.usage.output_tokens").Int() > 0 ||
						gjson.GetBytes(payload, "usage.output_tokens").Int() > 0
					if !hasOutput && isExcelBPSStreamRateLimit(payload) &&
						basispoints.IsOrganizationRateLimitMessage(excelBPSUpstreamError(payload).Get("message").String()) {
						p.rateLimit = bytes.Clone(payload)
					}
					return
				}
			}
			data, event = data[:0], ""
		case bytes.HasPrefix(line, []byte("event:")):
			event = string(bytes.TrimSpace(line[len("event:"):]))
		case bytes.HasPrefix(line, []byte("data:")):
			data = append(data, bytes.TrimPrefix(line[len("data:"):], []byte(" "))...)
			data = append(data, '\n')
		}
		line = line[:0]
	}
}

// wait returns the rate-limit payload that opened the stream, or nil when the
// stream must continue unchanged.
func (p *excelBPSStartPeek) wait(ctx context.Context, window time.Duration) []byte {
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case <-p.done:
		return p.rateLimit
	case <-timer.C:
	case <-ctx.Done():
	}
	return nil
}

// excelBPSPeekBody replays the peeked bytes, then the rest of the upstream
// body. A read waits for the peek to stop at its next event boundary.
type excelBPSPeekBody struct {
	peek *excelBPSStartPeek
	rest io.Reader
}

type excelBPSStickyErrReader struct{ err error }

func (r excelBPSStickyErrReader) Read([]byte) (int, error) { return 0, r.err }

func (b *excelBPSPeekBody) Read(p []byte) (int, error) {
	if b.rest == nil {
		<-b.peek.done
		tail := io.Reader(b.peek.reader)
		if b.peek.err != nil {
			tail = excelBPSStickyErrReader{b.peek.err}
		}
		b.rest = io.MultiReader(bytes.NewReader(b.peek.held), tail)
	}
	return b.rest.Read(p)
}

// Close interrupts a pending peek read as well as later reads.
func (b *excelBPSPeekBody) Close() error { return b.peek.upstream.Close() }

// retryExcelBPSStartRateLimit resends the request while a 2xx stream opens
// with a rate-limit rejection. It returns the final request and response;
// startRateLimit is non-nil when that response still opens with one. On error
// every response it received is already closed.
func (s *OpenAIGatewayService) retryExcelBPSStartRateLimit(ctx context.Context, c *gin.Context, account *Account, model, proxyURL, token string, req *http.Request, resp *http.Response, resend func() (*http.Request, error)) (*http.Request, *http.Response, []byte, error) {
	var waited time.Duration
	for attempt := 1; ; attempt++ {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return req, resp, nil, nil
		}
		peek := startExcelBPSPeek(resp.Body)
		resp.Body = &excelBPSPeekBody{peek: peek}
		rateLimit := peek.wait(ctx, excelBPSStartPeekWindow)
		if rateLimit == nil {
			return req, resp, nil, nil
		}
		delay, ok := excelBPSRateLimitDelay(resp.Header.Get("Retry-After"), rateLimit)
		if !ok || delay < excelBPSStartRetryMinDelay {
			delay = excelBPSStartRetryMinDelay
		}
		if delay > excelBPSStartRetryMaxDelay || attempt > excelBPSStartRetryMaxAttempts {
			return req, resp, rateLimit, nil
		}
		// Back off from the upstream hint and spread concurrent retries.
		delay <<= attempt - 1
		delay += rand.N(delay/2 + 1)
		if waited+delay > excelBPSStartRetryBudget || ctx.Err() != nil {
			return req, resp, rateLimit, nil
		}
		_ = resp.Body.Close()
		message, detail := s.excelBPSRateLimitOpsDetail(rateLimit, token, account)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			UpstreamStatusCode: http.StatusTooManyRequests, UpstreamRequestID: resp.Header.Get("x-request-id"),
			UpstreamURL: basispoints.ResponsesURL, Kind: "rate_limit_retry",
			Message: message, Detail: detail, UpstreamResponseBody: detail,
		})
		logger.LegacyPrintf("service.openai_excel_bps", "upstream rate limit before output; retrying on the same account: account_id=%d attempt=%d delay=%s", account.ID, attempt, delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return req, nil, nil, ctx.Err()
		case <-timer.C:
		}
		waited += delay
		next, err := resend()
		if err != nil {
			return req, nil, nil, err
		}
		req = next
		c.Set("excel_bps_upstream_attempt", c.GetInt("excel_bps_upstream_attempt")+1)
		resp, err = s.doExcelBPSSend(ctx, c, account, model, req, proxyURL)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return req, nil, nil, err
		}
	}
}

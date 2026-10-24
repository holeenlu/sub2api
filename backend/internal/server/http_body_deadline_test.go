package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestBodyDeadlineBoundsUploadWithoutTimingOutResponse(t *testing.T) {
	server := httptest.NewServer(requestBodyReadDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body timeout", http.StatusRequestTimeout)
			return
		}
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "stream remains writable")
	}), 40*time.Millisecond))
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10\r\n\r\nx")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status=%d", response.StatusCode)
	}
	response.Body.Close()
	response, err = http.Post(server.URL, "text/plain", strings.NewReader("complete"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "stream remains writable" {
		t.Fatalf("response=%q", body)
	}
}

// Deadlines are idle deadlines: a slow upload whose total duration exceeds the
// timeout succeeds as long as no single pause exceeds it, and a stalled upload
// still fails with 408 from the handler.
func TestRequestBodyDeadlineRenewsWhileUploadProgresses(t *testing.T) {
	const timeout = 300 * time.Millisecond
	server := httptest.NewServer(requestBodyReadDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body timeout", http.StatusRequestTimeout)
			return
		}
		fmt.Fprintf(w, "received %d", len(body))
	}), timeout))
	defer server.Close()

	t.Run("slow_but_steady_upload_succeeds", func(t *testing.T) {
		conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		const chunks = 15
		if _, err := fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\n\r\n", chunks); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		for i := 0; i < chunks; i++ {
			time.Sleep(60 * time.Millisecond) // < timeout per gap, total ≈ 3× timeout
			if _, err := conn.Write([]byte{'x'}); err != nil {
				t.Fatalf("chunk %d: %v", i, err)
			}
		}
		if elapsed := time.Since(started); elapsed <= timeout {
			t.Fatalf("upload must outlast the timeout to prove renewal, took %v", elapsed)
		}
		response, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != http.StatusOK || string(body) != fmt.Sprintf("received %d", chunks) {
			t.Fatalf("status=%d body=%q", response.StatusCode, body)
		}
	})

	t.Run("stalled_upload_fails", func(t *testing.T) {
		conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10\r\n\r\nxxx"); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		response, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusRequestTimeout {
			t.Fatalf("status=%d", response.StatusCode)
		}
		if elapsed := time.Since(started); elapsed < timeout || elapsed > 5*timeout {
			t.Fatalf("stall detected after %v, want within [%v, %v]", elapsed, timeout, 5*timeout)
		}
	})
}

// deadlineRecordingWriter stands in for a net/http writer with socket deadlines.
type deadlineRecordingWriter struct {
	http.ResponseWriter
	deadlines []time.Time
}

func (w *deadlineRecordingWriter) SetReadDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestRequestBodyDeadlineSkipsRequestsWithoutBody(t *testing.T) {
	for _, tc := range []struct {
		name          string
		request       *http.Request
		wantDeadlines int
	}{
		{name: "get_without_body", request: httptest.NewRequest(http.MethodGet, "/", nil)},
		{name: "post_no_body", request: httptest.NewRequest(http.MethodPost, "/", http.NoBody)},
		{name: "post_zero_content_length", request: func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
			r.ContentLength = 0
			return r
		}()},
		{name: "post_with_body", request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader("payload")), wantDeadlines: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &deadlineRecordingWriter{ResponseWriter: httptest.NewRecorder()}
			requestBodyReadDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.ReadAll(r.Body)
			}), time.Second).ServeHTTP(recorder, tc.request)
			// With a body: the initial deadline plus the clear at EOF.
			if len(recorder.deadlines) != tc.wantDeadlines {
				t.Fatalf("deadline calls=%d want %d (%v)", len(recorder.deadlines), tc.wantDeadlines, recorder.deadlines)
			}
			if tc.wantDeadlines > 0 && !recorder.deadlines[len(recorder.deadlines)-1].IsZero() {
				t.Fatalf("deadline must be cleared after EOF, got %v", recorder.deadlines)
			}
		})
	}
}

// scriptedBody returns one scripted chunk per Read, advancing a fake clock.
type scriptedBody struct {
	chunks [][]byte
	err    error
}

func (b *scriptedBody) Read(p []byte) (int, error) {
	if len(b.chunks) == 0 {
		if b.err != nil {
			return 0, b.err
		}
		return 0, io.EOF
	}
	n := copy(p, b.chunks[0])
	b.chunks = b.chunks[1:]
	return n, nil
}
func (*scriptedBody) Close() error { return nil }

func TestDeadlineRequestBodyRenewsOnProgressWithThrottle(t *testing.T) {
	const timeout = time.Second
	start := time.Unix(1_700_000_000, 0)
	now := start
	var deadlines []time.Time
	body := newDeadlineRequestBody(&scriptedBody{chunks: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")}},
		timeout, start, func(d time.Time) error { deadlines = append(deadlines, d); return nil })
	body.now = func() time.Time { return now }
	buf := make([]byte, 8)

	now = start.Add(100 * time.Millisecond) // below timeout/4 since the initial deadline: no syscall
	if n, err := body.Read(buf); n != 1 || err != nil {
		t.Fatalf("read=%d err=%v", n, err)
	}
	if len(deadlines) != 0 {
		t.Fatalf("renewal must be throttled, got %v", deadlines)
	}
	now = start.Add(300 * time.Millisecond) // ≥ timeout/4: renew to now+timeout
	_, _ = body.Read(buf)
	if len(deadlines) != 1 || !deadlines[0].Equal(now.Add(timeout)) {
		t.Fatalf("want one renewal to %v, got %v", now.Add(timeout), deadlines)
	}
	now = now.Add(200 * time.Millisecond) // throttled again
	_, _ = body.Read(buf)
	if len(deadlines) != 1 {
		t.Fatalf("second read within throttle window must not renew, got %v", deadlines)
	}
	now = now.Add(5 * timeout) // long gap but bytes arrived: renew relative to now
	_, _ = body.Read(buf)
	if len(deadlines) != 2 || !deadlines[1].Equal(now.Add(timeout)) {
		t.Fatalf("want renewal to %v, got %v", now.Add(timeout), deadlines)
	}
	if _, err := body.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("err=%v", err)
	}
	if len(deadlines) != 3 || !deadlines[2].IsZero() {
		t.Fatalf("EOF must clear the deadline once, got %v", deadlines)
	}
	_ = body.Close()
	if len(deadlines) != 3 {
		t.Fatalf("Close after EOF must not clear twice, got %v", deadlines)
	}
}

func TestDeadlineRequestBodyDoesNotRenewWithoutProgress(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var deadlines []time.Time
	body := newDeadlineRequestBody(&scriptedBody{err: io.ErrUnexpectedEOF}, time.Second, start,
		func(d time.Time) error { deadlines = append(deadlines, d); return nil })
	body.now = func() time.Time { return start.Add(time.Hour) }
	if _, err := body.Read(make([]byte, 4)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err=%v", err)
	}
	if len(deadlines) != 0 {
		t.Fatalf("a read without bytes must not extend the deadline, got %v", deadlines)
	}
	_ = body.Close()
	if len(deadlines) != 1 || !deadlines[0].IsZero() {
		t.Fatalf("Close must clear the deadline, got %v", deadlines)
	}
}

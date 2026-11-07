package tlsfingerprint

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Exact requests[0].raw_header from Linux/arm64 Claude Code 2.1.301 captures
// oauth-ascii-capture.json and apikey-ascii-capture.json, 2026-10-16. Credentials
// are capture-only placeholders. Bodies below are synthetic, not capture goldens.
const claudeLinuxOAuthHeader = "POST /v1/messages?beta=true HTTP/1.1\r\n" +
	"Accept: application/json\r\n" +
	"Authorization: Bearer sk-ant-oat01-local-capture-only\r\n" +
	"Content-Type: application/json\r\n" +
	"User-Agent: claude-cli/2.1.301 (external, sdk-cli)\r\n" +
	"X-Claude-Code-Session-Id: 2e993ace-c85d-478f-9ced-c19a0d116985\r\n" +
	"X-Stainless-Arch: arm64\r\n" +
	"X-Stainless-Lang: js\r\n" +
	"X-Stainless-OS: Linux\r\n" +
	"X-Stainless-Package-Version: 0.128.0\r\n" +
	"X-Stainless-Retry-Count: 0\r\n" +
	"X-Stainless-Runtime: node\r\n" +
	"X-Stainless-Runtime-Version: v26.3.0\r\n" +
	"X-Stainless-Timeout: 600\r\n" +
	"anthropic-beta: claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,thinking-token-count-2026-05-13,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24,thinking-binding-controls-2026-08-01,extended-cache-ttl-2025-04-11,cache-diagnosis-2026-04-07\r\n" +
	"anthropic-dangerous-direct-browser-access: true\r\n" +
	"anthropic-version: 2023-06-01\r\n" +
	"x-app: cli\r\n" +
	"x-claude-code-prompt-id: e12b3b30-625f-4d65-a40a-3d1aef7461a8\r\n" +
	"x-claude-code-request-class: main\r\n" +
	"x-client-request-id: cea8f484-f9a4-407f-805b-c337ea675834\r\n" +
	"Connection: keep-alive\r\n" +
	"Host: api.anthropic.com\r\n" +
	"Accept-Encoding: gzip, deflate, br, zstd\r\n" +
	"Content-Length: 28869\r\n\r\n"

const claudeLinuxAPIKeyHeader = "POST /v1/messages?beta=true HTTP/1.1\r\n" +
	"Accept: application/json\r\n" +
	"Content-Type: application/json\r\n" +
	"User-Agent: claude-cli/2.1.301 (external, sdk-cli)\r\n" +
	"X-Claude-Code-Session-Id: a438f9a3-07a1-43e9-95b8-fc336f2bbf01\r\n" +
	"X-Stainless-Arch: arm64\r\n" +
	"X-Stainless-Lang: js\r\n" +
	"X-Stainless-OS: Linux\r\n" +
	"X-Stainless-Package-Version: 0.128.0\r\n" +
	"X-Stainless-Retry-Count: 0\r\n" +
	"X-Stainless-Runtime: node\r\n" +
	"X-Stainless-Runtime-Version: v26.3.0\r\n" +
	"X-Stainless-Timeout: 600\r\n" +
	"anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14,thinking-token-count-2026-05-13,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24,thinking-binding-controls-2026-08-01,cache-diagnosis-2026-04-07\r\n" +
	"anthropic-dangerous-direct-browser-access: true\r\n" +
	"anthropic-version: 2023-06-01\r\n" +
	"x-api-key: sk-ant-api03-local-capture-only\r\n" +
	"x-app: cli\r\n" +
	"x-claude-code-prompt-id: 1f1a497a-5f7e-4759-8f24-6c0f8a950a9d\r\n" +
	"x-claude-code-request-class: main\r\n" +
	"x-client-request-id: 70d6853b-602d-4a19-8508-bc5559240f88\r\n" +
	"Connection: keep-alive\r\n" +
	"Host: api.anthropic.com\r\n" +
	"Accept-Encoding: gzip, deflate, br, zstd\r\n" +
	"Content-Length: 28836\r\n\r\n"

func TestClaudeHTTP1WireLinuxGolden(t *testing.T) {
	for _, golden := range []string{claudeLinuxOAuthHeader, claudeLinuxAPIKeyHeader} {
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(golden)))
		if err != nil {
			t.Fatal(err)
		}
		body := strings.Repeat("x", int(req.ContentLength))
		req.Body = io.NopCloser(strings.NewReader(body))
		var serialized bytes.Buffer
		if err := req.Write(&serialized); err != nil {
			t.Fatal(err)
		}
		if serialized.String() == golden+body {
			t.Fatal("fixture must exercise net/http's different header order")
		}
		for _, step := range []int{1, 7, 4096, serialized.Len()} {
			checkClaudeWire(t, serialized.String(), golden+body, step)
		}
	}
}

func TestClaudeHTTP1WireFragmentationAndBodies(t *testing.T) {
	fakeRequest, _ := claudeWireFixture("/v1/messages", "")
	body := "中文🙂\x00\xff\r\n\r\n" + fakeRequest + "trailing bytes"
	input, want := claudeWireFixture("/v1/messages?beta=true", body)
	next, nextWant := claudeWireFixture("/v1/messages/count_tokens?beta=true", "{}")
	input, want = input+next, want+nextWant
	for split := 0; split <= len(input); split++ {
		sink := &claudeWireConn{}
		conn := WrapClaudeHTTP1Conn(sink)
		writeClaudeWire(t, conn, input[:split])
		writeClaudeWire(t, conn, input[split:])
		if sink.String() != want {
			t.Fatalf("wire mismatch at split %d\ngot %q\nwant %q", split, sink.String(), want)
		}
	}
	for _, step := range []int{1, 2, 3, 4, 17, len(input)} {
		checkClaudeWire(t, input, want, step)
	}
	largeInput, largeWant := claudeWireFixture("/v1/messages", strings.Repeat(body, 1000))
	checkClaudeWire(t, largeInput, largeWant, len(largeInput))
}

func TestClaudeHTTP1WireEligibilityAndReuse(t *testing.T) {
	native, nativeWant := claudeWireFixture("/v1/messages", "{}")
	for name, input := range map[string]string{
		"browser":          strings.Replace(native, "claude-cli/2.1.301 (external, cli)", "Mozilla/5.0", 1),
		"missing-agent":    strings.Replace(native, "User-Agent: claude-cli/2.1.301 (external, cli)\r\n", "", 1),
		"duplicate-agent":  strings.Replace(native, "Host:", "User-Agent: claude-cli/2.1.301\r\nHost:", 1),
		"prefix-substring": strings.Replace(native, "claude-cli/", "proxy-claude-cli/", 1),
		"prefix-case":      strings.Replace(native, "claude-cli/", "Claude-cli/", 1),
		"empty-version":    strings.Replace(native, "claude-cli/2.1.301 (external, cli)", "claude-cli/", 1),
		"get":              strings.Replace(native, "POST ", "GET ", 1),
		"other-path":       strings.Replace(native, "/v1/messages ", "/v1/models ", 1),
		"suffix":           strings.Replace(native, "/v1/messages ", "/v1/messages/else ", 1),
		"count-suffix":     strings.Replace(native, "/v1/messages ", "/v1/messages/count_tokens/else ", 1),
		"encoded-path":     strings.Replace(native, "/v1/messages ", "/v1/%6dessages ", 1),
		"absolute-target":  strings.Replace(native, "/v1/messages ", "https://api.anthropic.com/v1/messages ", 1),
		"duplicate-case":   strings.Replace(native, "X-App: cli", "x-custom: first\r\nX-Custom: second", 1),
	} {
		t.Run(name, func(t *testing.T) {
			// Well-framed non-Claude requests remain byte-exact between native requests.
			checkClaudeWire(t, native+input+native, nativeWant+input+nativeWant, 11)
		})
	}
}

func TestClaudeHTTP1WirePreservesValuesAndMissingHeaders(t *testing.T) {
	input := "POST /v1/messages/count_tokens HTTP/1.1\r\n" +
		"content-length:\t0002 \t\r\n" +
		"x-app:\tcli \t\r\n" +
		"authorization:  Bearer opaque:ABC== \t\r\n" +
		"X-Stainless-Os: Linux\r\n" +
		"z-unknown: z\r\n" +
		"User-Agent: claude-cli/2.1.301 (external, cli)\r\n" +
		"X-Unknown: A\r\n" +
		"Anthropic-Beta: first\r\n" +
		"anthropic-beta: second\r\n\r\n{}"
	want := "POST /v1/messages/count_tokens HTTP/1.1\r\n" +
		"Authorization:  Bearer opaque:ABC== \t\r\n" +
		"User-Agent: claude-cli/2.1.301 (external, cli)\r\n" +
		"X-Stainless-OS: Linux\r\n" +
		"X-Unknown: A\r\n" +
		"anthropic-beta: first\r\n" +
		"anthropic-beta: second\r\n" +
		"x-app:\tcli \t\r\n" +
		"z-unknown: z\r\n" +
		"Content-Length:\t0002 \t\r\n\r\n{}"
	checkClaudeWire(t, input, want, 1)
}

func TestClaudeHTTP1WireUnknownFramingPassthrough(t *testing.T) {
	native, _ := claudeWireFixture("/v1/messages", "{}")
	for name, input := range map[string]string{
		"chunked":              strings.Replace(native, "Content-Length: 2", "Transfer-Encoding: chunked", 1),
		"both-framings":        strings.Replace(native, "Content-Length: 2", "Transfer-Encoding: chunked\r\nContent-Length: 2", 1),
		"other-transfer":       strings.Replace(native, "Content-Length: 2", "Transfer-Encoding: identity", 1),
		"missing-length":       strings.Replace(native, "Content-Length: 2\r\n", "", 1),
		"duplicate-length":     strings.Replace(native, "Content-Length: 2", "Content-Length: 2\r\ncontent-length: 2", 1),
		"conflicting-length":   strings.Replace(native, "Content-Length: 2", "Content-Length: 2\r\ncontent-length: 3", 1),
		"comma-length":         strings.Replace(native, "Content-Length: 2", "Content-Length: 2, 2", 1),
		"negative-length":      strings.Replace(native, "Content-Length: 2", "Content-Length: -2", 1),
		"signed-length":        strings.Replace(native, "Content-Length: 2", "Content-Length: +2", 1),
		"empty-length":         strings.Replace(native, "Content-Length: 2", "Content-Length:", 1),
		"overflow-length":      strings.Replace(native, "Content-Length: 2", "Content-Length: 9223372036854775808", 1),
		"invalid-field":        strings.Replace(native, "X-App: cli", "X App: cli", 1),
		"invalid-value":        strings.Replace(native, "X-App: cli", "X-App: c\x00li", 1),
		"folded-header":        strings.Replace(native, "X-App: cli", "X-App: cli\r\n continuation", 1),
		"bare-lf":              strings.Replace(native, "X-App: cli", "X-App: cli\nAnother: header", 1),
		"no-colon":             strings.Replace(native, "X-App: cli", "broken header", 1),
		"upgrade":              strings.Replace(native, "X-App: cli", "Upgrade: websocket", 1),
		"upgrade-connection":   strings.Replace(native, "keep-alive", "keep-alive, Upgrade", 1),
		"trailers":             strings.Replace(native, "X-App: cli", "Trailer: X-Checksum", 1),
		"http10":               strings.Replace(native, "HTTP/1.1", "HTTP/1.0", 1),
		"connect":              strings.Replace(native, "POST /v1/messages", "CONNECT api.anthropic.com:443", 1),
		"invalid-request-line": strings.Replace(native, "POST /v1/messages", "POST  /v1/messages", 1),
		"http2":                "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n",
		"unknown":              "unrecognized\r\n\r\nopaque bytes",
	} {
		t.Run(name, func(t *testing.T) {
			input += "2\r\n{}\r\n0\r\nX-Trailer: value\r\n\r\n" + native
			for _, step := range []int{1, 19, len(input)} {
				checkClaudeWire(t, input, input, step)
			}
		})
	}
}

func TestClaudeHTTP1WireNetHTTPChunkedPassthrough(t *testing.T) {
	native, _ := claudeWireFixture("/v1/messages", "{}")
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", io.NopCloser(strings.NewReader(native)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "claude-cli/2.1.301 (external, cli)")
	req.Trailer = http.Header{"X-Checksum": {"opaque"}}
	var wire bytes.Buffer
	if err := req.Write(&wire); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire.String(), "Transfer-Encoding: chunked\r\n") {
		t.Fatal("net/http did not produce chunked fixture")
	}
	input := wire.String() + native
	for _, step := range []int{1, 23, len(input)} {
		checkClaudeWire(t, input, input, step)
	}
}

func TestClaudeHTTP1WireHeaderLimit(t *testing.T) {
	native, nativeWant := claudeWireFixture("/v1/messages", "")
	for _, size := range []int{claudeHTTP1HeaderLimit - 1, claudeHTTP1HeaderLimit, claudeHTTP1HeaderLimit + 1} {
		padding := "Z-Padding: " + strings.Repeat("a", size-len(native)-len("Z-Padding: \r\n")) + "\r\n"
		input := strings.Replace(native, "Accept:", padding+"Accept:", 1)
		want := strings.Replace(nativeWant, "x-app:", padding+"x-app:", 1)
		if size > claudeHTTP1HeaderLimit {
			want = input
		}
		checkClaudeWire(t, input, want, 1)
		checkClaudeWire(t, input, want, len(input))
	}
	sink := &claudeWireConn{}
	conn := WrapClaudeHTTP1Conn(sink).(*claudeHTTP1Conn)
	oversized := strings.Repeat("a", claudeHTTP1HeaderLimit)
	writeClaudeWire(t, conn, oversized[:len(oversized)-1])
	if sink.Len() != 0 || len(conn.header) != claudeHTTP1HeaderLimit-1 {
		t.Fatal("incomplete header was not bounded and buffered")
	}
	writeClaudeWire(t, conn, oversized[len(oversized)-1:])
	writeClaudeWire(t, conn, native)
	if sink.String() != oversized+native || len(conn.header) != 0 {
		t.Fatal("header limit fallback lost or transformed bytes")
	}
}

func TestClaudeHTTP1WireWriteFailures(t *testing.T) {
	input, want := claudeWireFixture("/v1/messages", "body")
	headerEnd := strings.Index(input, "\r\n\r\n") + 4
	failed := errors.New("injected write failure")
	for _, tc := range []struct {
		name               string
		split, call, count int
		err                error
		wantN, wireN       int
	}{
		{"header-short", 0, 1, 7, nil, headerEnd, 7},
		{"header-zero", 0, 1, 0, nil, headerEnd, 0},
		{"header-error", 0, 1, 7, failed, headerEnd, 7},
		{"buffered-header-error", 13, 1, 7, failed, headerEnd - 13, 7},
		{"complete-header-error", 0, 1, headerEnd, failed, headerEnd, headerEnd},
		{"body-short", 0, 2, 2, nil, headerEnd + 2, headerEnd + 2},
		{"body-error", 0, 2, 2, failed, headerEnd + 2, headerEnd + 2},
		{"body-complete-error", 0, 2, 4, failed, len(input), len(want)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &claudeWireConn{failCall: tc.call, failCount: tc.count, failErr: tc.err}
			conn := WrapClaudeHTTP1Conn(sink)
			writeClaudeWire(t, conn, input[:tc.split])
			n, err := conn.Write([]byte(input[tc.split:]))
			wantErr := tc.err
			if wantErr == nil {
				wantErr = io.ErrShortWrite
			}
			if n != tc.wantN || !errors.Is(err, wantErr) || sink.String() != want[:tc.wireN] || !sink.closed {
				t.Fatalf("n=%d err=%v closed=%v wire=%q", n, err, sink.closed, sink.String())
			}
			if n, err = conn.Write([]byte(input)); n != 0 || !errors.Is(err, wantErr) || sink.calls != tc.call {
				t.Fatal("failed connection allowed replay")
			}
		})
	}
}

func TestClaudeHTTP1WireTransportReuse(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	_ = server.SetDeadline(time.Now().Add(5 * time.Second))
	var dials atomic.Int32
	transport := &http.Transport{ExpectContinueTimeout: time.Second, DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
		if dials.Add(1) != 1 {
			return nil, errors.New("connection was not reused")
		}
		return WrapClaudeHTTP1Conn(client), nil
	}}
	t.Cleanup(transport.CloseIdleConnections)
	done := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		for _, path := range []string{"/v1/messages?beta=true", "/v1/messages/count_tokens"} {
			var got strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					done <- err
					return
				}
				got.WriteString(line)
				if line == "\r\n" {
					break
				}
			}
			if path == "/v1/messages/count_tokens" {
				if _, err := io.WriteString(server, "HTTP/1.1 100 Continue\r\n\r\n"); err != nil {
					done <- err
					return
				}
			}
			body := make([]byte, 2)
			if _, err := io.ReadFull(reader, body); err != nil {
				done <- err
				return
			}
			_, want := claudeWireFixture(path, "{}")
			want = strings.Replace(want, "Content-Length: 2", "Accept-Encoding: gzip\r\nContent-Length: 2", 1)
			if path == "/v1/messages/count_tokens" {
				want = strings.Replace(want, "User-Agent:", "Expect: 100-continue\r\nUser-Agent:", 1)
			}
			if got.String()+string(body) != want {
				done <- fmt.Errorf("transport wire mismatch: %q", got.String())
				return
			}
			if _, err := io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, path := range []string{"/v1/messages?beta=true", "/v1/messages/count_tokens"} {
		req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com"+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", "claude-cli/2.1.301 (external, cli)")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("X-App", "cli")
		if path == "/v1/messages/count_tokens" {
			req.Header.Set("Expect", "100-continue")
		}
		response, err := httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || string(body) != "OK" {
			t.Fatalf("response %q: %v", body, err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 1 {
		t.Fatal("expected one pooled connection")
	}
}

func TestClaudeHTTP1WireTransportCancellation(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	bodyWrite := make(chan struct{})
	var writes atomic.Int32
	observed := &claudeWireObservedConn{Conn: client, beforeWrite: func() {
		if writes.Add(1) == 2 {
			close(bodyWrite)
		}
	}}
	transport := &http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
		return WrapClaudeHTTP1Conn(observed), nil
	}}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(strings.Repeat("x", 128<<10)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "claude-cli/2.1.301 (external, cli)")
	done := make(chan error, 1)
	go func() {
		response, err := (&http.Client{Transport: transport}).Do(req)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(server)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	select {
	case <-bodyWrite:
	case <-time.After(5 * time.Second):
		t.Fatal("transport did not start body write")
	}
	// The server stops reading while Write holds the adapter mutex. Cancellation
	// must still close the connection and release both transport goroutines.
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation blocked behind Write")
	}
}

func TestClaudeHTTP1WireConnOperations(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	started := make(chan struct{})
	conn := WrapClaudeHTTP1Conn(&claudeWireObservedConn{Conn: client, beforeWrite: func() { close(started) }})
	if conn.LocalAddr() != client.LocalAddr() || conn.RemoteAddr() != client.RemoteAddr() {
		t.Fatal("connection addresses changed")
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	input, _ := claudeWireFixture("/v1/messages", "{}")
	written := make(chan error, 1)
	go func() { _, err := conn.Write([]byte(input)); written <- err }()
	<-started
	peerWrite := make(chan error, 1)
	go func() { _, err := server.Write([]byte("raw response\x00\xff")); peerWrite <- err }()
	response := make([]byte, len("raw response\x00\xff"))
	if _, err := io.ReadFull(conn, response); err != nil || string(response) != "raw response\x00\xff" {
		t.Fatalf("concurrent read: %q, %v", response, err)
	}
	if err := <-peerWrite; err != nil {
		t.Fatal(err)
	}
	// Changing deadlines must not acquire the writer mutex either.
	if err := conn.SetWriteDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-written:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("deadline error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deadline did not release writer")
	}

	readerClient, readerServer := net.Pipe()
	t.Cleanup(func() { _ = readerClient.Close(); _ = readerServer.Close() })
	reader := WrapClaudeHTTP1Conn(readerClient)
	if err := reader.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	var timeout net.Error
	if _, err := reader.Read(make([]byte, 1)); !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("read deadline: %v", err)
	}
	writeClaudeWire(t, reader, "POST /v1/")
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := reader.Write([]byte("messages")); n != 0 || !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed buffered Write = (%d, %v)", n, err)
	}
}

func TestClaudeHTTP1WireConcurrentWriters(t *testing.T) {
	sink := &claudeWireConn{}
	conn := WrapClaudeHTTP1Conn(sink)
	var writers sync.WaitGroup
	want := make(map[string]bool)
	for i := 0; i < 16; i++ {
		input, expected := claudeWireFixture("/v1/messages", fmt.Sprintf("%02d", i))
		want[expected] = true
		writers.Add(1)
		go func() {
			defer writers.Done()
			if n, err := conn.Write([]byte(input)); n != len(input) || err != nil {
				t.Errorf("Write = (%d, %v)", n, err)
			}
		}()
	}
	writers.Wait()
	remaining := sink.String()
	for len(remaining) > 0 {
		end := strings.Index(remaining, "\r\n\r\n") + 6
		if end < 6 || end > len(remaining) || !want[remaining[:end]] {
			t.Fatal("concurrent requests interleaved")
		}
		delete(want, remaining[:end])
		remaining = remaining[end:]
	}
	if len(want) != 0 {
		t.Fatal("concurrent request was lost")
	}
}

type claudeWireObservedConn struct {
	net.Conn
	beforeWrite func()
}

func (c *claudeWireObservedConn) Write(p []byte) (int, error) {
	c.beforeWrite()
	return c.Conn.Write(p)
}

func claudeWireFixture(path, body string) (string, string) {
	request := "POST " + path + " HTTP/1.1\r\n"
	agent := "User-Agent: claude-cli/2.1.301 (external, cli)\r\n"
	length := fmt.Sprintf("Content-Length: %d\r\n", len(body))
	input := request + "Host: api.anthropic.com\r\n" + agent + length + "X-App: cli\r\nAccept: application/json\r\nConnection: keep-alive\r\n\r\n" + body
	want := request + "Accept: application/json\r\n" + agent + "x-app: cli\r\nConnection: keep-alive\r\nHost: api.anthropic.com\r\n" + length + "\r\n" + body
	return input, want
}

func checkClaudeWire(t *testing.T, input, want string, step int) {
	t.Helper()
	sink := &claudeWireConn{}
	conn := WrapClaudeHTTP1Conn(sink)
	for offset := 0; offset < len(input); offset += step {
		writeClaudeWire(t, conn, input[offset:min(offset+step, len(input))])
	}
	if sink.String() != want {
		t.Fatalf("wire mismatch (step %d): got %d bytes, want %d\ngot %q\nwant %q", step, sink.Len(), len(want), sink.String(), want)
	}
}

func writeClaudeWire(t *testing.T, conn net.Conn, s string) {
	t.Helper()
	p := []byte(s)
	if n, err := conn.Write(p); n != len(p) || err != nil {
		t.Fatalf("Write returned (%d, %v), want (%d, nil)", n, err, len(p))
	}
	if string(p) != s {
		t.Fatal("Write modified its input")
	}
}

type claudeWireConn struct {
	net.Conn
	bytes.Buffer
	calls, failCall, failCount int
	failErr                    error
	closed                     bool
}

func (c *claudeWireConn) Write(p []byte) (int, error) {
	c.calls++
	if c.calls == c.failCall {
		_, _ = c.Buffer.Write(p[:c.failCount])
		return c.failCount, c.failErr
	}
	return c.Buffer.Write(p)
}

func (c *claudeWireConn) Read(p []byte) (int, error) { return c.Buffer.Read(p) }
func (c *claudeWireConn) Close() error               { c.closed = true; return nil }

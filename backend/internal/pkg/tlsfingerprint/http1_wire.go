package tlsfingerprint

import (
	"bytes"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/net/http/httpguts"
)

const claudeHTTP1HeaderLimit = 64 << 10

// WrapClaudeHTTP1Conn wraps an already-handshaken TLS connection used for HTTP/1.1.
// It changes only header spelling/order on claude-cli/ POSTs to /v1/messages or
// /v1/messages/count_tokens. Values, request lines and bodies are preserved.
// Unknown framing, upgrades and oversized headers disable adaptation for the
// rest of the connection. Incomplete headers are buffered up to 64 KiB.
// A write failure is terminal: a reordered partial header cannot be retried
// using an input byte offset. Read and deadlines go directly to conn; Close
// never waits for a blocked writer. This is not a general-purpose buffered conn.
func WrapClaudeHTTP1Conn(conn net.Conn) net.Conn {
	return &claudeHTTP1Conn{Conn: conn}
}

type claudeHTTP1Conn struct {
	net.Conn
	mu          sync.Mutex // Serialize writers without blocking Read, Close or deadlines.
	header      []byte
	bodyLeft    int64
	passthrough bool
	writeErr    error
	closed      atomic.Bool
}

func (c *claudeHTTP1Conn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func (c *claudeHTTP1Conn) Write(p []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	for len(p) > 0 {
		if c.passthrough || c.bodyLeft > 0 {
			chunk := p
			if !c.passthrough && int64(len(chunk)) > c.bodyLeft {
				chunk = chunk[:int(c.bodyLeft)]
			}
			written, err := c.write(chunk)
			n += written
			p = p[written:]
			if !c.passthrough {
				c.bodyLeft -= int64(written)
			}
			if err != nil {
				return n, err
			}
			continue
		}

		previous := len(c.header)
		take := min(len(p), claudeHTTP1HeaderLimit-previous)
		c.header = append(c.header, p[:take]...)
		// Only the previous three bytes can start a split header terminator.
		search := max(0, previous-3)
		end := bytes.Index(c.header[search:], []byte("\r\n\r\n"))
		if end >= 0 {
			end += search + 4
			take = end - previous
			c.header = c.header[:end]
		}
		n += take // Buffered header bytes are accepted, even if flushing later fails.
		p = p[take:]
		if end < 0 && len(c.header) < claudeHTTP1HeaderLimit {
			continue
		}

		wire := c.header
		if end >= 0 {
			var framed bool
			wire, c.bodyLeft, framed = claudeHTTP1Header(wire)
			c.passthrough = !framed
		} else {
			c.passthrough = true
		}
		c.header = nil
		if _, err := c.write(wire); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (c *claudeHTTP1Conn) write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n < 0 || n > len(p) {
		n, err = 0, io.ErrShortWrite
	} else if n < len(p) && err == nil {
		err = io.ErrShortWrite
	}
	if err != nil {
		c.writeErr = err
		_ = c.Close()
	}
	return n, err
}

type claudeHTTP1Field struct {
	name string
	rest string // Preserve the colon, whitespace and value verbatim.
	tail int
}

// claudeHTTP1Header recognizes only unambiguous Content-Length framing; it is
// not an HTTP validator. Anything else is left to the existing HTTP transport.
func claudeHTTP1Header(raw []byte) ([]byte, int64, bool) {
	lines := strings.Split(string(raw[:len(raw)-4]), "\r\n")
	request := strings.Split(lines[0], " ")
	if len(request) != 3 || request[2] != "HTTP/1.1" || request[0] == "CONNECT" {
		return raw, 0, false
	}
	fields := make([]claudeHTTP1Field, 0, len(lines)-1)
	names := make(map[string]string, len(lines)-1)
	ambiguousNames := false
	length, userAgent, agentCount := int64(-1), "", 0
	for _, line := range lines[1:] {
		name, value, ok := strings.Cut(line, ":")
		if !ok || !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
			return raw, 0, false
		}
		key := strings.ToLower(name)
		value = strings.Trim(value, " \t")
		switch key {
		case "transfer-encoding", "trailer", "upgrade":
			return raw, 0, false
		case "connection":
			for _, token := range strings.Split(value, ",") {
				if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
					return raw, 0, false
				}
			}
		case "content-length":
			if length >= 0 || value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return raw, 0, false
			}
			var err error
			length, err = strconv.ParseInt(value, 10, 64)
			if err != nil {
				return raw, 0, false
			}
		case "user-agent":
			userAgent, agentCount = value, agentCount+1
		}
		field := claudeHTTP1Field{name: name, rest: line[len(name):]}
		if spelling, ok := claudeHTTP1Names[key]; ok {
			field.name = spelling
		}
		// Sorting differently-cased duplicates could change their value order.
		if previous, ok := names[key]; ok && previous != field.name {
			ambiguousNames = true
		}
		names[key] = field.name
		switch key {
		case "connection":
			field.tail = 1
		case "host":
			field.tail = 2
		case "accept-encoding":
			field.tail = 3
		case "content-length":
			field.tail = 4
		}
		fields = append(fields, field)
	}
	if length < 0 {
		return raw, 0, false
	}
	path, _, _ := strings.Cut(request[1], "?")
	if ambiguousNames || request[0] != "POST" || (path != "/v1/messages" && path != "/v1/messages/count_tokens") ||
		agentCount != 1 || !strings.HasPrefix(userAgent, "claude-cli/") || userAgent == "claude-cli/" {
		return raw, length, true
	}
	slices.SortStableFunc(fields, func(a, b claudeHTTP1Field) int {
		if a.tail != b.tail {
			return a.tail - b.tail
		}
		return strings.Compare(a.name, b.name)
	})
	wire := make([]byte, 0, len(raw))
	wire = append(wire, lines[0]...)
	wire = append(wire, '\r', '\n')
	for _, field := range fields {
		wire = append(wire, field.name...)
		wire = append(wire, field.rest...)
		wire = append(wire, '\r', '\n')
	}
	return append(wire, '\r', '\n'), length, true
}

var claudeHTTP1Names = map[string]string{
	"accept":                                    "Accept",
	"authorization":                             "Authorization",
	"content-type":                              "Content-Type",
	"user-agent":                                "User-Agent",
	"x-claude-code-session-id":                  "X-Claude-Code-Session-Id",
	"x-stainless-arch":                          "X-Stainless-Arch",
	"x-stainless-lang":                          "X-Stainless-Lang",
	"x-stainless-os":                            "X-Stainless-OS",
	"x-stainless-package-version":               "X-Stainless-Package-Version",
	"x-stainless-retry-count":                   "X-Stainless-Retry-Count",
	"x-stainless-runtime":                       "X-Stainless-Runtime",
	"x-stainless-runtime-version":               "X-Stainless-Runtime-Version",
	"x-stainless-timeout":                       "X-Stainless-Timeout",
	"anthropic-beta":                            "anthropic-beta",
	"anthropic-dangerous-direct-browser-access": "anthropic-dangerous-direct-browser-access",
	"anthropic-version":                         "anthropic-version",
	"x-api-key":                                 "x-api-key",
	"x-app":                                     "x-app",
	"x-claude-code-prompt-id":                   "x-claude-code-prompt-id",
	"x-claude-code-request-class":               "x-claude-code-request-class",
	"x-client-request-id":                       "x-client-request-id",
	"connection":                                "Connection",
	"host":                                      "Host",
	"accept-encoding":                           "Accept-Encoding",
	"content-length":                            "Content-Length",
}

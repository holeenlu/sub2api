package openai

import (
	"crypto/tls"
	"net/http/httptrace"
)

// rejectHTTPHandoff vetoes the connection just handed to this attempt. A trace
// callback alone cannot veto net/http's HTTP/1 write: Go 1.27.3 getConn calls
// GotConn, then persistConn.roundTrip queues writech before selecting ctx.Done.
// Close the borrowed HTTP/1 connection synchronously before that handoff can
// return. This connection is exclusive to this request, including pool reuse,
// CONNECT/SOCKS tunnels and the existing HTTP/1 TLS-fingerprint transport.
//
// Negotiated HTTP/2 connections are shared and MUST NOT be closed here. Both
// Go 1.27.3 internal/http2 and the pinned x/net/http2 writer check ctx.Done under
// ClientConn.wmu in encodeAndWriteHeaders, before HPACK or HEADERS are written.
// Synchronous request cancellation is consumed by that actual stream write
// guard; it does not cancel unrelated streams on the connection. The controlled
// repository uses HTTP/1 for plaintext and TLS ALPN for multiplexed HTTP/2.
//
// Do not infer NotSent from Close: an earlier transport handoff may have sent
// bytes. The existing accumulated transmission evidence remains authoritative.
func (p *PreparedHTTP) rejectHTTPHandoff(info httptrace.GotConnInfo, cause error) {
	p.cancel(cause)
	if info.Conn == nil {
		return
	}
	if conn, ok := info.Conn.(interface{ ConnectionState() tls.ConnectionState }); ok && conn.ConnectionState().NegotiatedProtocol == "h2" {
		return
	}
	_ = info.Conn.Close()
}

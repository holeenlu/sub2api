package urlvalidator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PublicTransport is for untrusted downloads. It connects to the exact address
// it validated, including when an HTTP or SOCKS proxy resolves destinations.
// Host and TLS identity retain the original URL hostname.
type PublicTransport struct {
	base     *http.Transport
	lookupIP func(context.Context, string, string) ([]netip.Addr, error)
}

// NewPublicTransport keeps explicit proxy and TLS trust configuration while
// enforcing an independent, pinned-destination transport for each download.
func NewPublicTransport(base *http.Transport) *PublicTransport {
	if base == nil {
		base = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
		}
	}
	return &PublicTransport{base: base.Clone(), lookupIP: net.DefaultResolver.LookupNetIP}
}

func (t *PublicTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || t == nil || t.base == nil {
		return nil, errors.New("public download request is invalid")
	}
	// Some fingerprint dialers hide a proxy inside the TLS callback. Dropping
	// that callback would silently bypass the proxy; executing it could ignore
	// the pinned address/SNI. Such clients must use the ordinary download path.
	if t.base.DialTLSContext != nil || t.base.DialTLS != nil { //nolint:staticcheck // Reject the deprecated callback too; it can bypass destination policy.
		return nil, errors.New("custom TLS dialers are unsupported for public downloads")
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return nil, errors.New("public downloads only support GET and HEAD")
	}
	if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
		return nil, errors.New("public download requires HTTP or HTTPS")
	}
	if req.URL.User != nil || req.URL.Opaque != "" || req.URL.Hostname() == "" {
		return nil, errors.New("public download URL is invalid")
	}
	if req.Host != "" && !strings.EqualFold(req.Host, req.URL.Host) {
		return nil, errors.New("public download host must match its URL")
	}
	host := strings.TrimSuffix(req.URL.Hostname(), ".")
	port := req.URL.Port()
	if port == "" {
		port = "80"
		if req.URL.Scheme == "https" {
			port = "443"
		}
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return nil, errors.New("public download port is invalid")
	}
	ips, err := t.publicAddresses(req.Context(), host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		transport := t.base.Clone()
		transport.DisableKeepAlives = true
		// Discard protocol handlers that may close over the original client's
		// HTTP/2 pool. Any negotiated HTTP/2 connection must belong to this
		// fresh transport and its approved destination.
		transport.TLSNextProto = nil
		transport.Protocols = nil
		transport.ForceAttemptHTTP2 = true
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
		transport.TLSClientConfig.ServerName = host
		transport.TLSClientConfig.InsecureSkipVerify = false
		transport.TLSClientConfig.NextProtos = nil
		if transport.Proxy != nil {
			proxyURL, proxyErr := transport.Proxy(req)
			if proxyErr != nil {
				return nil, proxyErr
			}
			if proxyURL != nil && (proxyURL.Scheme == "http" || proxyURL.Scheme == "https") {
				// WriteProxy uses Request.Host in the absolute request target. A
				// CONNECT tunnel is needed even for HTTP images to preserve Host
				// without asking the proxy to resolve the original hostname again.
				proxyBase := t.base
				transport.Proxy = nil
				transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialPublicDownloadTunnel(ctx, proxyBase, proxyURL, network, addr)
				}
			}
		}
		pinned := req.Clone(req.Context())
		pinned.URL.Host = net.JoinHostPort(ip.String(), port)
		pinned.Host = req.URL.Host
		resp, roundTripErr := transport.RoundTrip(pinned)
		if roundTripErr != nil {
			transport.CloseIdleConnections()
			lastErr = roundTripErr
			continue
		}
		resp.Request = req
		resp.Body = &publicDownloadBody{ReadCloser: resp.Body, transport: transport}
		return resp, nil
	}
	return nil, fmt.Errorf("public download failed: %w", lastErr)
}

func (t *PublicTransport) publicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if IsBlockedHost(host) {
		return nil, errors.New("download host is not allowed")
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var err error
		ips, err = t.lookupIP(lookupCtx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("download DNS resolution failed: %w", err)
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("download host has no allowed addresses")
	}
	approved := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		ip = ip.Unmap()
		if !isPublicDownloadIP(ip) {
			return nil, errors.New("download destination is not allowed")
		}
		approved = append(approved, ip)
	}
	return approved, nil
}

var nonPublicDownloadPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:2::/48"),
}

func isPublicDownloadIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range nonPublicDownloadPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	if ip.Is6() {
		bytes := ip.As16()
		// Check the embedded IPv4 destination too; address translation must
		// not turn an apparently global IPv6 address into a private request.
		if netip.MustParsePrefix("64:ff9b::/96").Contains(ip) {
			return isPublicDownloadIP(netip.AddrFrom4([4]byte(bytes[12:16])))
		}
		if netip.MustParsePrefix("2002::/16").Contains(ip) {
			return isPublicDownloadIP(netip.AddrFrom4([4]byte(bytes[2:6])))
		}
		if netip.MustParsePrefix("2001::/32").Contains(ip) {
			return isPublicDownloadIP(netip.AddrFrom4([4]byte(bytes[4:8]))) &&
				isPublicDownloadIP(netip.AddrFrom4([4]byte{^bytes[12], ^bytes[13], ^bytes[14], ^bytes[15]}))
		}
	}
	return true
}

func dialPublicDownloadTunnel(ctx context.Context, base *http.Transport, proxyURL *url.URL, network, target string) (_ net.Conn, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	port := proxyURL.Port()
	if port == "" {
		port = "80"
		if proxyURL.Scheme == "https" {
			port = "443"
		}
	}
	dial := base.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	conn, err := dial(ctx, network, net.JoinHostPort(proxyURL.Hostname(), port))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if proxyURL.Scheme == "https" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if base.TLSClientConfig != nil {
			tlsConfig = base.TLSClientConfig.Clone()
		}
		tlsConfig.ServerName = proxyURL.Hostname()
		tlsConfig.InsecureSkipVerify = false
		tlsConfig.NextProtos = []string{"http/1.1"}
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = tlsConn
	}
	headers := base.ProxyConnectHeader.Clone()
	if base.GetProxyConnectHeader != nil {
		headers, err = base.GetProxyConnectHeader(ctx, proxyURL, target)
		if err != nil {
			return nil, err
		}
		headers = headers.Clone()
	}
	if headers == nil {
		headers = make(http.Header)
	}
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		headers.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username()+":"+password)))
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: headers}
	if err := request.Write(conn); err != nil {
		return nil, err
	}
	limited := &io.LimitedReader{R: conn, N: 64 << 10}
	reader := bufio.NewReader(limited)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("public download proxy CONNECT failed: HTTP %d", response.StatusCode)
	}
	// Do not close the CONNECT body: it owns the tunnel. Preserve any bytes
	// already buffered, then remove the header-only limit for the image stream.
	buffered, err := reader.Peek(reader.Buffered())
	if err != nil {
		return nil, err
	}
	prefix := append([]byte(nil), buffered...)
	if !stop() {
		return nil, ctx.Err()
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return &publicTunnelConn{Conn: conn, reader: io.MultiReader(bytes.NewReader(prefix), conn)}, nil
}

type publicTunnelConn struct {
	net.Conn
	reader io.Reader
}

func (c *publicTunnelConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

type publicDownloadBody struct {
	io.ReadCloser
	transport *http.Transport
}

func (b *publicDownloadBody) Close() error {
	err := b.ReadCloser.Close()
	b.transport.CloseIdleConnections()
	return err
}

package urlvalidator

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/stretchr/testify/require"
)

func TestPublicTransportPinsResolutionAndPreservesHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "images.example", r.Host)
		require.Equal(t, "/a%2Fb?token=signed%2Fvalue", r.RequestURI)
		_, _ = io.WriteString(w, "image")
	}))
	defer server.Close()
	var lookups, dials atomic.Int64
	transport := NewPublicTransport(&http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		require.Equal(t, "93.184.216.34:80", addr, "dial must use the approved IP, never resolve the hostname again")
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}})
	transport.lookupIP = func(context.Context, string, string) ([]netip.Addr, error) {
		if lookups.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Get("http://images.example/a%2Fb?token=signed%2Fvalue")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "image", string(body))
	require.EqualValues(t, 1, lookups.Load())
	require.EqualValues(t, 1, dials.Load())
}

func TestPublicTransportRejectsPrivateAndMixedAnswersBeforeDial(t *testing.T) {
	for _, answer := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "::1", "fc00::1", "::ffff:127.0.0.1", "0.0.0.0", "224.0.0.1", "0.1.2.3", "198.18.0.1", "240.1.2.3", "64:ff9b::7f00:1", "2002:7f00:1::1"} {
		t.Run(answer, func(t *testing.T) {
			transport := NewPublicTransport(&http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("a forbidden answer must never reach the dialer")
				return nil, nil
			}})
			transport.lookupIP = func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr(answer)}, nil
			}
			req, err := http.NewRequest(http.MethodGet, "http://images.example/a", nil)
			require.NoError(t, err)
			_, err = transport.RoundTrip(req)
			require.ErrorContains(t, err, "not allowed")
		})
	}
}

func TestPublicTransportRevalidatesRedirects(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	}))
	defer server.Close()
	transport := NewPublicTransport(&http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}})
	transport.lookupIP = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	client := &http.Client{Transport: transport}
	_, err := client.Get("http://images.example/public")
	require.ErrorContains(t, err, "not allowed")
	require.EqualValues(t, 1, calls.Load())
}

func TestPublicTransportPinsProxyDestination(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "images.example", r.Host)
		_, _ = io.WriteString(w, "image")
	}))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodConnect, r.Method)
		require.Equal(t, "93.184.216.34:80", r.Host, "proxy must receive a literal approved destination")
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok)
		downstream, rw, err := hijacker.Hijack()
		require.NoError(t, err)
		defer func() { _ = downstream.Close() }()
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		require.NoError(t, err)
		defer func() { _ = upstream.Close() }()
		_, err = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		require.NoError(t, err)
		require.NoError(t, rw.Flush())
		go func() { _, _ = io.Copy(upstream, rw) }()
		_, _ = io.Copy(downstream, upstream)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	transport := NewPublicTransport(&http.Transport{Proxy: http.ProxyURL(proxyURL)})
	transport.lookupIP = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	resp, err := (&http.Client{Transport: transport}).Get("http://images.example/image")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestPublicTransportRetainsVerifiedTLSHostname(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "example.com", r.Host)
		require.Equal(t, "example.com", r.TLS.ServerName)
		_, _ = io.WriteString(w, "verified")
	}))
	defer server.Close()
	require.NoError(t, server.Certificate().VerifyHostname("example.com"))
	serverTransport, ok := server.Client().Transport.(*http.Transport)
	require.True(t, ok)
	base := serverTransport.Clone()
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		require.Equal(t, "93.184.216.34:443", addr)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	transport := NewPublicTransport(base)
	transport.lookupIP = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Get("https://example.com/")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, err = client.Get("https://different.example/")
	require.Error(t, err, "the pinned IP must not disable verification of the original hostname")
}

func TestPublicTransportPinsSOCKSProxyDestination(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "images.example", r.Host)
		_, _ = io.WriteString(w, "image")
	}))
	defer origin.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		greeting := make([]byte, 2)
		_, err = io.ReadFull(conn, greeting)
		require.NoError(t, err)
		methods := make([]byte, int(greeting[1]))
		_, err = io.ReadFull(conn, methods)
		require.NoError(t, err)
		_, err = conn.Write([]byte{5, 0})
		require.NoError(t, err)
		request := make([]byte, 10)
		_, err = io.ReadFull(conn, request)
		require.NoError(t, err)
		require.Equal(t, byte(1), request[3], "SOCKS must receive an IP, not a remotely resolved hostname")
		require.Equal(t, []byte{93, 184, 216, 34}, request[4:8])
		require.EqualValues(t, 80, binary.BigEndian.Uint16(request[8:10]))
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		require.NoError(t, err)
		defer func() { _ = upstream.Close() }()
		_, err = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		require.NoError(t, err)
		go func() { _, _ = io.Copy(upstream, conn) }()
		_, _ = io.Copy(conn, upstream)
	}()
	proxyURL, err := url.Parse("socks5h://" + listener.Addr().String())
	require.NoError(t, err)
	base := &http.Transport{}
	require.NoError(t, proxyutil.ConfigureTransportProxy(base, proxyURL))
	transport := NewPublicTransport(base)
	transport.lookupIP = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	response, err := (&http.Client{Transport: transport}).Get("http://images.example/image")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	<-done
}

func TestPublicTransportDoesNotBypassHiddenTLSProxy(t *testing.T) {
	transport := NewPublicTransport(&http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("unsupported TLS callback must not run")
		return nil, nil
	}})
	req, err := http.NewRequest(http.MethodGet, "https://93.184.216.34/image", nil)
	require.NoError(t, err)
	_, err = transport.RoundTrip(req)
	require.ErrorContains(t, err, "custom TLS dialers are unsupported")
}

func TestPublicTransportEnvironmentProxy(t *testing.T) {
	// net/http caches environment proxy configuration. A child test process
	// provides a fresh environment without mutating the rest of the test suite.
	if os.Getenv("SUB2API_PUBLIC_PROXY_TEST") != "1" {
		executable, err := os.Executable()
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestPublicTransportEnvironmentProxy$", "-test.count=1")
		cmd.Env = append(os.Environ(), "SUB2API_PUBLIC_PROXY_TEST=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "example.com", r.Host)
		_, _ = io.WriteString(w, "proxied")
	}))
	defer origin.Close()
	secureOrigin := httptest.NewTLSServer(origin.Config.Handler)
	defer secureOrigin.Close()
	var proxyCalls atomic.Int64
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		require.Equal(t, http.MethodConnect, r.Method)
		target := origin.Listener.Addr().String()
		if r.Host == "93.184.216.34:443" {
			target = secureOrigin.Listener.Addr().String()
		} else {
			require.Equal(t, "93.184.216.34:80", r.Host)
		}
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, rw, err := hijacker.Hijack()
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		upstream, err := net.Dial("tcp", target)
		require.NoError(t, err)
		defer func() { _ = upstream.Close() }()
		_, err = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		require.NoError(t, err)
		require.NoError(t, rw.Flush())
		go func() { _, _ = io.Copy(upstream, rw) }()
		_, _ = io.Copy(conn, upstream)
	}))
	defer proxyServer.Close()
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"} {
		t.Setenv(key, proxyServer.URL)
	}
	for _, key := range []string{"NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(key, "")
	}
	transport := NewPublicTransport(nil)
	roots := x509.NewCertPool()
	roots.AddCert(secureOrigin.Certificate())
	transport.base.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	transport.base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != proxyServer.Listener.Addr().String() {
			return nil, fmt.Errorf("direct access attempted instead of environment proxy: %s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	transport.lookupIP = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, target := range []string{"http://example.com/image", "https://example.com/image"} {
		resp, err := client.Get(target)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, "proxied", string(body))
	}
	require.EqualValues(t, 2, proxyCalls.Load())
}

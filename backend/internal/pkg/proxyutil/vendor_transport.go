package proxyutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// IsVendorProxy marks URLs created by the server's owned Proxy object. It also
// separates pools by configuration version without exposing policy in HTTP.
func IsVendorProxy(u *url.URL) bool {
	return u != nil && strings.HasPrefix(u.Fragment, "vendor-proxy:")
}

// VendorRedirectPolicy keeps the native redirect policy and prevents a fixed
// supplier operation from being redirected to an arbitrary target or downgrade.
func VendorRedirectPolicy(previous func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req == nil || req.URL == nil || len(via) == 0 {
			return errors.New("supplier redirect rejected")
		}
		origin := via[0].URL
		if origin == nil || req.URL.User != nil || !strings.EqualFold(req.URL.Hostname(), origin.Hostname()) || (req.URL.Scheme != "https" && req.URL.Scheme != "http") || (origin.Scheme == "https" && req.URL.Scheme != "https") {
			return errors.New("supplier redirect rejected")
		}
		if req.URL.Port() != origin.Port() {
			return errors.New("supplier redirect port rejected")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
}

type DialContextFunc func(context.Context, string, string) (net.Conn, error)

// GuardProxyDial resolves on EVERY new socket, rejects the entire mixed DNS
// answer, and dials the approved literal address. Never resolve a second time.
// The URL hostname remains unchanged for proxy TLS SNI/certificate validation.
func GuardProxyDial(u *url.URL, dial DialContextFunc) DialContextFunc {
	return guardProxyDial(u, dial, net.DefaultResolver.LookupNetIP)
}

func guardProxyDial(u *url.URL, dial DialContextFunc, lookup func(context.Context, string, string) ([]netip.Addr, error)) DialContextFunc {
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	if !IsVendorProxy(u) {
		return dial
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		host, port, err := net.SplitHostPort(address)
		wantPort := u.Port()
		if wantPort == "" {
			switch u.Scheme {
			case "https":
				wantPort = "443"
			case "http":
				wantPort = "80"
			default:
				wantPort = "1080"
			}
		}
		if err != nil || port != wantPort || !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(u.Hostname(), ".")) {
			return nil, errors.New("supplier proxy dial destination mismatch")
		}
		if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
			return nil, errors.New("supplier proxy address is not public")
		}
		var ips []netip.Addr
		if ip, e := netip.ParseAddr(host); e == nil {
			ips = []netip.Addr{ip}
		} else {
			ips, err = lookup(ctx, "ip", host)
		}
		if err != nil || len(ips) == 0 {
			return nil, errors.New("supplier proxy DNS lookup failed")
		}
		for _, ip := range ips {
			if !IsPublicIP(ip) {
				return nil, errors.New("supplier proxy address is not public")
			}
		}
		for _, ip := range ips {
			conn, e := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			if e == nil {
				return conn, nil
			}
			err = e
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.New("supplier proxy connection failed")
	}
}

// ContextForwardDialer also implements the legacy x/net/proxy Dial interface.
type ContextForwardDialer struct{ DialFunc DialContextFunc }

func (d ContextForwardDialer) DialContext(ctx context.Context, n, a string) (net.Conn, error) {
	return d.DialFunc(ctx, n, a)
}
func (d ContextForwardDialer) Dial(n, a string) (net.Conn, error) {
	return d.DialFunc(context.Background(), n, a)
}

var nonPublicDownloadPrefixes = []netip.Prefix{
	netip.MustParsePrefix("::/96"),
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

func IsPublicIP(ip netip.Addr) bool {
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
			return IsPublicIP(netip.AddrFrom4([4]byte(bytes[12:16])))
		}
		if netip.MustParsePrefix("2002::/16").Contains(ip) {
			return IsPublicIP(netip.AddrFrom4([4]byte(bytes[2:6])))
		}
		if netip.MustParsePrefix("2001::/32").Contains(ip) {
			return IsPublicIP(netip.AddrFrom4([4]byte(bytes[4:8]))) &&
				IsPublicIP(netip.AddrFrom4([4]byte{^bytes[12], ^bytes[13], ^bytes[14], ^bytes[15]}))
		}
	}
	return true
}

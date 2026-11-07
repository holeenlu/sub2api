package proxyutil

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"net"
	"net/netip"
	"net/url"
	"testing"
)

func TestVendorProxyDialPinsEveryDNSAnswer(t *testing.T) {
	u, _ := url.Parse("http://proxy.invalid:8080#vendor-proxy:1:2:3")
	lookups, dials := 0, 0
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		if lookups == 1 {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	dial := func(_ context.Context, _ string, addr string) (net.Conn, error) {
		dials++
		require.Equal(t, "8.8.8.8:8080", addr)
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	guard := guardProxyDial(u, dial, lookup)
	c, err := guard(context.Background(), "tcp", "proxy.invalid:8080")
	require.NoError(t, err)
	c.Close()
	_, err = guard(context.Background(), "tcp", "proxy.invalid:8080")
	require.Error(t, err)
	require.Equal(t, 2, lookups)
	require.Equal(t, 1, dials)
}
func TestVendorProxyDialRejectsMixedDNSBeforeAnyDial(t *testing.T) {
	u, _ := url.Parse("socks5h://proxy.invalid:1080#vendor-proxy:1:2:3")
	guard := guardProxyDial(u, func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dialed mixed DNS response")
		return nil, nil
	}, func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("169.254.169.254")}, nil
	})
	_, err := guard(context.Background(), "tcp", "proxy.invalid:1080")
	require.Error(t, err)
}
func TestVendorProxyNonPublicForms(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "172.16.2.1", "192.168.1.1", "169.254.169.254", "::1", "::ffff:127.0.0.1", "fe80::1", "fc00::1", "100.64.0.1", "64:ff9b::a00:1", "2002:7f00:1::", "::", "0.0.0.0", "198.18.0.1"} {
		t.Run(host, func(t *testing.T) { require.False(t, IsPublicIP(netip.MustParseAddr(host))) })
	}
}
func TestLegacyProxyPrivateDialUnchanged(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:8080")
	sentinel := errors.New("original dial result")
	guard := guardProxyDial(u, func(_ context.Context, _ string, addr string) (net.Conn, error) {
		require.Equal(t, "127.0.0.1:8080", addr)
		return nil, sentinel
	}, func(context.Context, string, string) ([]netip.Addr, error) {
		t.Fatal("legacy DNS policy changed")
		return nil, nil
	})
	_, err := guard(context.Background(), "tcp", "127.0.0.1:8080")
	require.ErrorIs(t, err, sentinel)
}
func TestVendorProxyRejectsAlternateDialDestination(t *testing.T) {
	u, _ := url.Parse("https://proxy.invalid:443#vendor-proxy:1:2:3")
	_, err := GuardProxyDial(u, nil)(context.Background(), "tcp", "metadata.google.internal:443")
	require.ErrorContains(t, err, "mismatch")
	_, err = GuardProxyDial(u, nil)(context.Background(), "tcp", "proxy.invalid:22")
	require.ErrorContains(t, err, "mismatch")
}

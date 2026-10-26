package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestVendorProxyNativePoolsPreservePolicyAndVersion(t *testing.T) {
	raw := "http://user:password@127.0.0.1:8080#vendor-proxy:7:2:3"
	key, u, err := normalizeProxyURL(raw)
	require.NoError(t, err)
	require.Equal(t, "vendor-proxy:7:2:3", u.Fragment)
	other, _, err := normalizeProxyURL("http://user:password@127.0.0.1:8080#vendor-proxy:7:2:4")
	require.NoError(t, err)
	require.NotEqual(t, key, other)
	transport, err := buildUpstreamTransport(defaultPoolSettings(nil), u, upstreamProtocolModeDefault)
	require.NoError(t, err)
	_, err = transport.DialContext(context.Background(), "tcp", "127.0.0.1:8080")
	require.ErrorContains(t, err, "not public")
	for _, scheme := range []string{"http", "https", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			p := *u
			p.Scheme = scheme
			tr, e := buildUpstreamTransportWithTLSFingerprint(defaultPoolSettings(nil), &p, &tlsfingerprint.Profile{})
			require.NoError(t, e)
			if tr.DialTLSContext != nil {
				_, e = tr.DialTLSContext(context.Background(), "tcp", "target.invalid:443")
			} else {
				_, e = tr.DialContext(context.Background(), "tcp", "127.0.0.1:8080")
			}
			require.ErrorContains(t, e, "not public")
		})
	}
}
func TestVendorProxyOAuthAndRefreshClientGuard(t *testing.T) {
	for _, makeClient := range []func(string) (*http.Client, error){
		func(p string) (*http.Client, error) {
			c, e := createReqClient(p)
			if e != nil {
				return nil, e
			}
			return c.GetClient(), nil
		},
		func(p string) (*http.Client, error) {
			c, e := getSharedReqClient(reqClientOptions{ProxyURL: p, Timeout: time.Second, Impersonate: true})
			if e != nil {
				return nil, e
			}
			return c.GetClient(), nil
		},
	} {
		for _, scheme := range []string{"http", "https", "socks5h"} {
			client, err := makeClient(scheme + "://PRIVATE_USER:PRIVATE_PASSWORD@127.0.0.1:8080#vendor-proxy:7:2:3")
			require.NoError(t, err)
			req, _ := http.NewRequest("GET", "https://target.invalid/", nil)
			_, err = client.Do(req)
			require.ErrorContains(t, err, "not public")
			require.NotContains(t, err.Error(), "PRIVATE_PASSWORD")
			err = client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "other.invalid"}}, []*http.Request{req})
			require.Error(t, err)
		}
	}
}

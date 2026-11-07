package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestVendorProxyWebSocketTransportRejectsPrivate(t *testing.T) {
	d := &coderOpenAIWSClientDialer{proxyClients: make(map[string]*openAIWSProxyClientEntry)}
	for _, scheme := range []string{"http", "https", "socks5h"} {
		client, err := d.proxyHTTPClient(scheme + "://127.0.0.1:8080#vendor-proxy:7:2:3")
		require.NoError(t, err)
		tr := client.Transport.(*http.Transport)
		if scheme == "socks5h" {
			_, err = tr.DialContext(context.Background(), "tcp", "target.invalid:443")
		} else {
			_, err = tr.DialContext(context.Background(), "tcp", "127.0.0.1:8080")
		}
		require.ErrorContains(t, err, "not public")
	}
}

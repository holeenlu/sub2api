//go:build !integration

package repository

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestBuildUpstreamTransportClaudeHTTP1Wire(t *testing.T) {
	const body = `{"messages":[{"role":"user","content":"中文🙂 <>&"}]}`
	paths := []string{"/v1/messages?beta=true", "/v1/messages/count_tokens"}
	if target := os.Getenv("SUB2API_TEST_CLAUDE_WIRE_TARGET"); target != "" {
		var proxy *url.URL
		if raw := os.Getenv("SUB2API_TEST_CLAUDE_WIRE_PROXY"); raw != "" {
			var err error
			proxy, err = url.Parse(raw)
			require.NoError(t, err)
		}
		tr, err := buildUpstreamTransportWithTLSFingerprint(defaultPoolSettings(nil), proxy, &tlsfingerprint.Profile{})
		require.NoError(t, err)
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
		for _, path := range paths {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://"+target+path, strings.NewReader(body))
			require.NoError(t, err)
			req.Header = http.Header{
				"User-Agent": {"claude-cli/2.1.301 (external, cli)"}, "Accept": {"application/json"},
				"Content-Type": {"application/json"}, "Connection": {"keep-alive"},
				"X-Stainless-Os": {"Linux"}, "Anthropic-Version": {"2023-06-01"}, "X-App": {"cli"},
			}
			resp, err := client.Do(req)
			if os.Getenv("SUB2API_TEST_CLAUDE_WIRE_UNTRUSTED") == "true" {
				var unknown x509.UnknownAuthorityError
				require.ErrorAs(t, err, &unknown)
				return
			}
			require.NoError(t, err)
			data, err := io.ReadAll(resp.Body)
			require.NoError(t, resp.Body.Close())
			require.NoError(t, err)
			require.Equal(t, "OK", string(data))
		}
		return
	}

	// Isolate the process-wide root cache; production certificate verification stays enabled.
	seed := httptest.NewTLSServer(http.NotFoundHandler())
	seed.Close()
	certFile, emptyFile := filepath.Join(t.TempDir(), "root.pem"), filepath.Join(t.TempDir(), "empty.pem")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: seed.Certificate().Raw}), 0600))
	require.NoError(t, os.WriteFile(emptyFile, nil, 0600))
	emptyCertDir := t.TempDir()
	for _, route := range []string{"direct", "http", "https", "socks5"} {
		t.Run(route, func(t *testing.T) {
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: seed.TLS.Certificates, NextProtos: []string{"http/1.1"}})
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			target := listener.Addr().String()
			var proxyURL string
			proxyCalls := &atomic.Int64{}
			if route == "socks5" {
				proxyURL, proxyCalls = startTestSOCKS5Proxy(t)
			} else if route == "http" || route == "https" {
				proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxyCalls.Add(1)
					if r.Method != http.MethodConnect || r.Host != target || (r.TLS != nil) != (route == "https") {
						http.Error(w, "unexpected CONNECT", http.StatusBadRequest)
						return
					}
					upstream, err := net.DialTimeout("tcp", target, 5*time.Second)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadGateway)
						return
					}
					defer upstream.Close()
					downstream, buffered, err := w.(http.Hijacker).Hijack()
					if err != nil {
						return
					}
					defer downstream.Close()
					if _, err = io.WriteString(downstream, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
						return
					}
					go func() { _, _ = io.Copy(upstream, buffered); _ = upstream.Close() }()
					_, _ = io.Copy(downstream, upstream)
				}))
				if route == "https" {
					proxy.TLS = &tls.Config{Certificates: seed.TLS.Certificates}
					proxy.StartTLS()
				} else {
					proxy.Start()
				}
				t.Cleanup(proxy.Close)
				proxyURL = proxy.URL
			}
			runClient := func(trust string, untrusted bool) {
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBuildUpstreamTransportClaudeHTTP1Wire$", "-test.timeout=15s")
				cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+trust, "SSL_CERT_DIR="+emptyCertDir,
					"GODEBUG="+os.Getenv("GODEBUG")+",x509sslcertoverrideplatform=1",
					"SUB2API_TEST_CLAUDE_WIRE_TARGET="+target, "SUB2API_TEST_CLAUDE_WIRE_PROXY="+proxyURL,
					"SUB2API_TEST_CLAUDE_WIRE_UNTRUSTED="+fmt.Sprint(untrusted))
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
			}
			if route == "https" {
				runClient(emptyFile, true)
				require.Zero(t, proxyCalls.Load(), "untrusted TLS proxy must fail before CONNECT")
			}
			done := make(chan error, 1)
			go func() {
				done <- func() error {
					conn, err := listener.Accept()
					if err != nil {
						return err
					}
					defer conn.Close()
					if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
						return err
					}
					if err := conn.(*tls.Conn).Handshake(); err != nil {
						return err
					}
					reader := bufio.NewReader(conn)
					// Both requests must arrive on this single, verified TLS connection.
					for _, path := range paths {
						var header strings.Builder
						for {
							line, err := reader.ReadString('\n')
							if err != nil {
								return err
							}
							header.WriteString(line)
							if line == "\r\n" {
								break
							}
						}
						// Fixed native ordering: uppercase ordinary fields, lowercase ordinary fields, runtime tail.
						want := "POST " + path + " HTTP/1.1\r\nAccept: application/json\r\nContent-Type: application/json\r\n" +
							"User-Agent: claude-cli/2.1.301 (external, cli)\r\nX-Stainless-OS: Linux\r\nanthropic-version: 2023-06-01\r\nx-app: cli\r\n" +
							"Connection: keep-alive\r\nHost: " + target + "\r\nAccept-Encoding: gzip\r\n" + fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
						data := make([]byte, len(body))
						if _, err := io.ReadFull(reader, data); err != nil {
							return err
						}
						if header.String() != want || string(data) != body {
							return fmt.Errorf("wire mismatch: header %q; body %q", header.String(), data)
						}
						if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"); err != nil {
							return err
						}
					}
					return nil
				}()
			}()
			runClient(certFile, false)
			require.NoError(t, <-done)
			if route != "direct" {
				require.EqualValues(t, 1, proxyCalls.Load(), "reuse must not create another proxy tunnel")
			}
		})
	}
}

package repository

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

type siwcRoundTripper func(*http.Request) (*http.Response, error)

func (f siwcRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func siwcResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func TestSIWCTokenUsesOpaqueAccessMetadataAndVerifiedIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keys, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kid": "key", "kty": "RSA", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	claims := jwt.MapClaims{"iss": openai.SIWCIssuer, "sub": "siwc-sub", "aud": "oaiapp_test", "iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": "nonce"}
	signed := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed.Header["kid"] = "key"
	id, err := signed.SignedString(key)
	require.NoError(t, err)
	// Deliberately has no Codex user/workspace metadata, matching the public
	// opaque authorization contract. Never parse this token to infer ownership.
	at := "opaque-access-token"
	payload, _ := json.Marshal(openai.TokenResponse{AccessToken: at, RefreshToken: "rotating", IDToken: id, TokenType: "Bearer", ExpiresIn: 3600, Scope: openai.SIWCScopes})
	requests := 0
	c := &http.Client{Transport: siwcRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		require.Empty(t, r.Header.Get("Authorization"))
		if r.URL.String() == openai.SIWCJWKSURL {
			return siwcResponse(200, string(keys)), nil
		}
		require.Equal(t, openai.SIWCTokenURL, r.URL.String())
		require.NoError(t, r.ParseForm())
		require.Equal(t, openai.SIWCResource, r.Form.Get("resource"))
		require.Equal(t, "code-secret", r.Form.Get("code"))
		require.Equal(t, "verifier-secret", r.Form.Get("code_verifier"))
		return siwcResponse(200, string(payload)), nil
	})}
	token, identity, err := siwcToken(t.Context(), c, url.Values{"grant_type": {"authorization_code"}, "client_id": {"oaiapp_test"}, "code": {"code-secret"}, "code_verifier": {"verifier-secret"}}, "nonce")
	require.NoError(t, err)
	require.Equal(t, at, token.AccessToken)
	require.Equal(t, "siwc-sub", identity.Subject)
	require.Equal(t, 2, requests)
	for _, scope := range []string{"openid profile email", ""} {
		payload, _ = json.Marshal(openai.TokenResponse{AccessToken: at, IDToken: id, TokenType: "Bearer", ExpiresIn: 3600, Scope: scope})
		identityOnly, verified, err := siwcToken(t.Context(), c, url.Values{"grant_type": {"authorization_code"}, "client_id": {"oaiapp_test"}, "code": {"code-secret"}, "code_verifier": {"verifier-secret"}}, "nonce")
		require.NoError(t, err)
		require.Equal(t, "siwc-sub", verified.Subject)
		require.False(t, openai.HasSIWCPermission(identityOnly.Scope))
	}

}
func TestSIWCTokenErrorsAreSanitizedAndTerminalOnlyOnOAuthRejection(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keys, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kid": "key", "kty": "RSA", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})

	for _, tc := range []struct {
		name     string
		status   int
		body     string
		terminal bool
	}{{"invalid grant", 400, "{\"error\":\"invalid_grant\",\"debug\":\"secret-rt\"}", true}, {"server", 500, "{\"error\":\"invalid_grant\",\"debug\":\"secret-rt\"}", false}, {"rate limit", 429, "secret-rt", false}, {"malformed", 200, "secret-rt", false}} {
		t.Run(tc.name, func(t *testing.T) {
			c := &http.Client{Transport: siwcRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == openai.SIWCJWKSURL {
					return siwcResponse(200, string(keys)), nil
				}
				return siwcResponse(tc.status, tc.body), nil
			})}
			_, _, err := siwcToken(t.Context(), c, url.Values{"grant_type": {"refresh_token"}, "client_id": {"oaiapp_test"}, "refresh_token": {"secret-rt"}}, "")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret-rt")
			var rejected *openai.SIWCOAuthError
			require.Equal(t, tc.terminal, errors.As(err, &rejected) && rejected.Terminal())
		})
	}
}
func TestSIWCRevocationUsesDiscoveredFixedOriginAndReportsConfirmation(t *testing.T) {
	for _, host := range []string{"https://auth.openai.com/oauth/revoke", "https://evil.example/revoke", "https://auth.openai.com@evil.example/revoke", "https://auth.openai.com:443/revoke"} {
		t.Run(host, func(t *testing.T) {
			posts := 0
			c := &http.Client{Transport: siwcRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == openai.SIWCDiscoveryURL {
					raw, _ := json.Marshal(map[string]string{"issuer": openai.SIWCIssuer, "revocation_endpoint": host})
					return siwcResponse(200, string(raw)), nil
				}
				posts++
				require.Equal(t, "https://auth.openai.com/oauth/revoke", r.URL.String())
				require.NoError(t, r.ParseForm())
				require.Equal(t, "oaiapp_test", r.Form.Get("client_id"))
				require.Empty(t, r.Header.Get("Authorization"))
				return siwcResponse(200, ""), nil
			})}
			confirmed, err := siwcRevoke(context.Background(), c, "refresh-secret", "access-secret", "oaiapp_test")
			if host == "https://auth.openai.com/oauth/revoke" {
				require.NoError(t, err)
				require.True(t, confirmed)
				require.Equal(t, 2, posts)
			} else {
				require.Error(t, err)
				require.False(t, confirmed)
				require.Zero(t, posts)
			}
		})
	}
}

func TestSIWCInvalidSigningKeysDoNotConsumeRefreshToken(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: siwcRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, openai.SIWCJWKSURL, r.URL.String())
		return siwcResponse(200, "{\"keys\":[]}"), nil
	})}
	_, _, err := siwcToken(t.Context(), client, url.Values{"grant_type": {"refresh_token"}, "client_id": {"oaiapp_test"}, "refresh_token": {"must-not-send"}}, "")
	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func TestSIWCNoTokenCannotClaimRemoteRevocation(t *testing.T) {
	confirmed, err := siwcRevoke(t.Context(), nil, "", "", "oaiapp_test")
	require.Error(t, err)
	require.False(t, confirmed)
}

func TestSIWCOfficialOAuthErrorCodeClassificationAcrossHTTPStatuses(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keys, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kid": "key", "kty": "RSA", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	for _, code := range []string{"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused", "invalid_client", "unauthorized_client"} {
		for _, status := range []int{400, 401, 403, 429, 503} {
			t.Run(code+"-"+http.StatusText(status), func(t *testing.T) {
				client := &http.Client{Transport: siwcRoundTripper(func(r *http.Request) (*http.Response, error) {
					if r.URL.String() == openai.SIWCJWKSURL {
						return siwcResponse(200, string(keys)), nil
					}
					body, _ := json.Marshal(map[string]string{"error": code})
					return siwcResponse(status, string(body)), nil
				})}
				_, _, err := siwcToken(t.Context(), client, url.Values{"grant_type": {"refresh_token"}, "client_id": {"oaiapp_test"}, "refresh_token": {"secret"}}, "")
				require.Error(t, err)
				var protocolError *openai.SIWCOAuthError
				typed := errors.As(err, &protocolError)
				require.Equal(t, status != 429 && status != 503, typed)
				expectedTerminal := status != 429 && status != 503 && code != "invalid_client" && code != "unauthorized_client"
				require.Equal(t, expectedTerminal, typed && protocolError.Terminal())
			})
		}
	}
}

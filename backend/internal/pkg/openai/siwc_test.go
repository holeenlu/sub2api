package openai

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestSIWCIdentitySignatureClaimsAndNonce(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	jwk := map[string]any{"kty": "RSA", "kid": "trusted", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}
	keys, err := json.Marshal(map[string]any{"keys": []any{jwk}})
	require.NoError(t, err)
	base := jwt.MapClaims{"iss": SIWCIssuer, "sub": "verified-subject", "aud": "oaiapp_test", "iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": "expected", "email": "display@example.com"}
	for _, tc := range []struct {
		name   string
		mutate func(jwt.MapClaims, map[string]any)
		valid  bool
	}{
		{"valid", func(jwt.MapClaims, map[string]any) {}, true},
		{"issuer", func(c jwt.MapClaims, _ map[string]any) { c["iss"] = "https://evil.example" }, false},
		{"audience", func(c jwt.MapClaims, _ map[string]any) { c["aud"] = "another-client" }, false},
		{"expiry", func(c jwt.MapClaims, _ map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }, false},
		{"nonce", func(c jwt.MapClaims, _ map[string]any) { c["nonce"] = "wrong" }, false},
		{"subject", func(c jwt.MapClaims, _ map[string]any) { delete(c, "sub") }, false},
		{"issued at", func(c jwt.MapClaims, _ map[string]any) { delete(c, "iat") }, false},
		{"multiple audience without azp", func(c jwt.MapClaims, _ map[string]any) { c["aud"] = []string{"oaiapp_test", "other"} }, false},
		{"multiple audience with azp", func(c jwt.MapClaims, _ map[string]any) {
			c["aud"] = []string{"oaiapp_test", "other"}
			c["azp"] = "oaiapp_test"
		}, true},
		{"header key URL", func(_ jwt.MapClaims, h map[string]any) { h["jku"] = "https://evil.example/keys" }, false},
		{"untrusted kid", func(_ jwt.MapClaims, h map[string]any) { h["kid"] = "untrusted" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := jwt.MapClaims{}
			for k, v := range base {
				c[k] = v
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
			token.Header["kid"] = "trusted"
			tc.mutate(c, token.Header)
			raw, err := token.SignedString(key)
			require.NoError(t, err)
			identity, err := VerifySIWCIdentity(raw, "oaiapp_test", "expected", keys)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, "verified-subject", identity.Subject)
			} else {
				require.Error(t, err)
				require.Nil(t, identity)
			}
		})
	}
	duplicate, _ := json.Marshal(map[string]any{"keys": []any{jwk, jwk}})
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, base)
	token.Header["kid"] = "trusted"
	signed, _ := token.SignedString(key)
	_, err = VerifySIWCIdentity(signed, "oaiapp_test", "expected", duplicate)
	require.Error(t, err)
}
func TestSIWCDynamicAndReturningAuthorization(t *testing.T) {
	first, _ := url.Parse(BuildSIWCAuthorizationURL("state", "challenge", "nonce", "urn:uuid:host", "", "", "RealApp"))
	q := first.Query()
	require.Equal(t, "dynamic_agent_client", q.Get("client_id"))
	require.Equal(t, "RealApp", q.Get("agent_name_hint"))
	require.Equal(t, SIWCResource, q.Get("resource"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.True(t, HasSIWCPermission(q.Get("scope")))
	returning, _ := url.Parse(BuildSIWCAuthorizationURL("new-state", "new-challenge", "new-nonce", "urn:uuid:host", "oaiapp_issued", "signed-identity", "RealApp"))
	rq := returning.Query()
	require.Equal(t, "oaiapp_issued", rq.Get("client_id"))
	require.Empty(t, rq.Get("agent_name_hint"))
	require.Equal(t, "signed-identity", rq.Get("id_token_hint"))
	require.Equal(t, q.Get("ext_agent_host_id"), rq.Get("ext_agent_host_id"))
}

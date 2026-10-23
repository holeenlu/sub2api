package openai

import (
	"crypto/rsa"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	SIWCDynamicClientID = "dynamic_agent_client"
	SIWCRedirectURI     = "http://127.0.0.1:1455/auth/callback"
	SIWCIssuer          = "https://auth.openai.com"
	SIWCTokenURL        = SIWCIssuer + "/api/accounts/oauth/token"
	SIWCJWKSURL         = SIWCIssuer + "/.well-known/jwks.json"
	SIWCDiscoveryURL    = SIWCIssuer + "/.well-known/openid-configuration"
	SIWCResource        = "https://api.openai.com/v1"
	SIWCDirectScope     = "chatgpt.tokens.use.direct"
	SIWCScopes          = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
	SIWCTokenLimit      = 128 << 10
)

func BuildSIWCAuthorizationURL(state, challenge, nonce, hostID, clientID, hint, agentName string) string {
	if clientID == "" {
		clientID = SIWCDynamicClientID
	}
	q := url.Values{"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {SIWCRedirectURI}, "resource": {SIWCResource}, "scope": {SIWCScopes}, "state": {state}, "nonce": {nonce}, "ext_agent_host_id": {hostID}, "code_challenge_method": {"S256"}, "code_challenge": {challenge}}
	if clientID == SIWCDynamicClientID {
		q.Set("agent_name_hint", agentName)
	} else if hint != "" {
		q.Set("id_token_hint", hint)
	}
	return SIWCIssuer + "/api/accounts/authorize?" + q.Encode()
}

type SIWCIdentity struct {
	jwt.RegisteredClaims
	Nonce           string `json:"nonce"`
	Email           string `json:"email"`
	AuthorizedParty string `json:"azp"`
	AccessTokenHash string `json:"at_hash"`
}

// Verification trusts only fixed-provider JWKS fetched by our transport, never
// header-supplied jku/x5u/jwk keys. Access-token metadata remains opaque.
func VerifySIWCIdentity(raw, clientID, nonce string, jwks []byte) (*SIWCIdentity, error) {
	if len(raw) == 0 || len(raw) > SIWCTokenLimit || !ValidSIWCClientID(clientID) || len(jwks) > 1<<20 {
		return nil, errors.New("SIWC identity validation failed")
	}
	keys, err := parseSIWCSigningKeys(jwks)
	if err != nil {
		return nil, err
	}
	claims := new(SIWCIdentity)
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		for _, field := range []string{"jku", "x5u", "jwk", "crit"} {
			if _, ok := t.Header[field]; ok {
				return nil, errors.New("unsupported identity header")
			}
		}
		kid, _ := t.Header["kid"].(string)
		key := keys[kid]
		if key == nil {
			return nil, errors.New("OpenAI signing key unavailable")
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(SIWCIssuer), jwt.WithAudience(clientID), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithStrictDecoding())
	if err != nil || token == nil || !token.Valid || strings.TrimSpace(claims.Subject) == "" || len(claims.Subject) > 512 || claims.IssuedAt == nil || claims.ExpiresAt == nil || !claims.ExpiresAt.After(claims.IssuedAt.Time) || claims.IssuedAt.After(time.Now()) || (len(claims.Audience) > 1 && claims.AuthorizedParty != clientID) || (claims.AuthorizedParty != "" && claims.AuthorizedParty != clientID) || (nonce != "" && subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1) {
		return nil, errors.New("SIWC identity validation failed")
	}
	return claims, nil
}

// ValidateSIWCSigningKeys validates fixed-provider verification material before
// an authorization-code exchange or rotating refresh token is consumed.
func ValidateSIWCSigningKeys(jwks []byte) error {
	_, err := parseSIWCSigningKeys(jwks)
	return err
}
func parseSIWCSigningKeys(jwks []byte) (map[string]*rsa.PublicKey, error) {
	if len(jwks) > 1<<20 {
		return nil, errors.New("invalid OpenAI signing keys")
	}
	var document struct {
		Keys []struct {
			Kty    string
			Kid    string
			Use    string
			Alg    string
			N      string
			E      string
			KeyOps []string `json:"key_ops"`
		}
	}
	if json.Unmarshal(jwks, &document) != nil || len(document.Keys) == 0 || len(document.Keys) > 32 {
		return nil, errors.New("invalid OpenAI signing keys")
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range document.Keys {
		if k.Kty != "RSA" || k.Alg != "RS256" || k.Use != "sig" {
			continue
		}
		if k.Kid == "" || len(k.Kid) > 256 || keys[k.Kid] != nil || (len(k.KeyOps) > 0 && (len(k.KeyOps) != 1 || k.KeyOps[0] != "verify")) {
			return nil, errors.New("invalid OpenAI signing keys")
		}
		n, ne := base64.RawURLEncoding.Strict().DecodeString(k.N)
		e, ee := base64.RawURLEncoding.Strict().DecodeString(k.E)
		if ne != nil || ee != nil || len(n) < 256 || len(n) > 1024 || len(e) == 0 || len(e) > 4 {
			return nil, errors.New("invalid OpenAI signing keys")
		}
		modulus := new(big.Int).SetBytes(n)
		exponent := new(big.Int).SetBytes(e).Int64()
		if modulus.BitLen() < 2048 || modulus.Bit(0) != 1 || exponent < 3 || exponent > 1<<31-1 || exponent%2 == 0 {
			return nil, errors.New("invalid OpenAI signing keys")
		}
		keys[k.Kid] = &rsa.PublicKey{N: modulus, E: int(exponent)}
	}
	if len(keys) == 0 {
		return nil, errors.New("OpenAI signing key unavailable")
	}
	return keys, nil
}
func ValidSIWCClientID(id string) bool {
	if id == "" || id == SIWCDynamicClientID || len(id) > 512 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func HasSIWCPermission(scope string) bool {
	for _, s := range strings.Fields(scope) {
		if s == SIWCDirectScope {
			return true
		}
	}
	return false
}

// Only terminal OAuth protocol errors invalidate a rotating grant. Transport,
// server and rate-limit failures retain it for a later retry.
type SIWCOAuthError struct{ Code string }

func (e *SIWCOAuthError) Error() string {
	return "SIWC authorization server rejected the grant: " + e.Code
}
func (e *SIWCOAuthError) Terminal() bool {
	switch e.Code {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	default:
		return false
	}
}

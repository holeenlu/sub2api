package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// This is a cache identity, never token validation or an authorization grant.
// Structured OAuth principal/organization/scope changes invalidate discovery;
// routine expiration/refresh rotation for the same principal does not. Opaque
// credentials retain their complete credential hash as the conservative case.
func catalogStableOAuthCredentials(source *Account, credentials map[string]any) {
	if source == nil || !source.IsOpenAIOAuth() {
		return
	}
	token, _ := credentials["access_token"].(string)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return
	}
	var claims map[string]any
	if json.Unmarshal(body, &claims) != nil {
		return
	}
	subject, _ := claims["sub"].(string)
	issuer, _ := claims["iss"].(string)
	if subject == "" || issuer == "" {
		return
	}
	for _, key := range []string{"exp", "iat", "nbf", "jti", "sid", "session_id", "auth_time"} {
		delete(claims, key)
	}
	for _, key := range []string{"access_token", "refresh_token", "id_token", "expires_at", "token_expires_at", "refresh_token_expires_at"} {
		delete(credentials, key)
	}
	credentials["catalog_oauth_principal"] = claims
}

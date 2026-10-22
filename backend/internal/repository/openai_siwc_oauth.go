package repository

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

func siwcHTTPClient(proxy string) (*http.Client, error) {
	c, err := httpclient.GetClient(httpclient.Options{ProxyURL: proxy, Timeout: 40 * time.Second, ValidateResolvedIP: true})
	if err != nil {
		return nil, errors.New("SIWC proxy is unavailable")
	}
	copy := *c
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &copy, nil
}
func siwcRequest(ctx context.Context, c *http.Client, method, endpoint string, form url.Values) ([]byte, int, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, 0, errors.New("invalid SIWC request")
	}
	r.Header.Set("Accept", "application/json")
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := c.Do(r)
	if err != nil {
		return nil, 0, errors.New("SIWC authorization server is unavailable")
	}
	defer res.Body.Close()
	if res.ContentLength > 1<<20 {
		return nil, res.StatusCode, errors.New("SIWC authorization response exceeds limit")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, res.StatusCode, errors.New("invalid SIWC authorization response")
	}
	return raw, res.StatusCode, nil
}

func (s *openaiOAuthService) SIWCToken(ctx context.Context, form url.Values, proxy, nonce string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
	c, err := siwcHTTPClient(proxy)
	if err != nil {
		return nil, nil, err
	}
	return siwcToken(ctx, c, form, nonce)
}
func siwcToken(ctx context.Context, c *http.Client, form url.Values, nonce string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
	if !openai.ValidSIWCClientID(form.Get("client_id")) || (form.Get("grant_type") != "authorization_code" && form.Get("grant_type") != "refresh_token") {
		return nil, nil, errors.New("invalid SIWC exchange")
	}
	// Fetch fixed-provider verification keys before consuming a rotating token.
	keys, status, err := siwcRequest(ctx, c, http.MethodGet, openai.SIWCJWKSURL, nil)
	if err != nil || status != http.StatusOK || openai.ValidateSIWCSigningKeys(keys) != nil {
		return nil, nil, errors.New("OpenAI signing keys unavailable")
	}
	values := url.Values{}
	for k, v := range form {
		values[k] = append([]string(nil), v...)
	}
	values.Set("resource", openai.SIWCResource)
	raw, status, err := siwcRequest(ctx, c, http.MethodPost, openai.SIWCTokenURL, values)
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusOK {
		var e struct{ Error string }
		_ = json.Unmarshal(raw, &e)
		if status >= http.StatusBadRequest && status < http.StatusInternalServerError && status != http.StatusTooManyRequests {
			switch e.Error {
			case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused", "invalid_client", "unauthorized_client", "invalid_request", "invalid_scope":
				return nil, nil, &openai.SIWCOAuthError{Code: e.Error}
			}
		}
		return nil, nil, errors.New("SIWC token exchange or refresh failed")
	}
	var token openai.TokenResponse
	if json.Unmarshal(raw, &token) != nil || (token.AccessToken == "" && token.IDToken == "") || len(token.AccessToken) > openai.SIWCTokenLimit || len(token.RefreshToken) > openai.SIWCTokenLimit || token.ExpiresIn > 3155760000 || (token.AccessToken != "" && (!strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn <= 0)) || (openai.HasSIWCPermission(token.Scope) && token.AccessToken == "") {
		return nil, nil, errors.New("SIWC grant is incomplete")
	}
	var identity *openai.SIWCIdentity
	if token.IDToken != "" {
		identity, err = openai.VerifySIWCIdentity(token.IDToken, form.Get("client_id"), nonce, keys)
		if err != nil {
			return nil, nil, err
		}
		if identity.AccessTokenHash != "" {
			hash := sha256.Sum256([]byte(token.AccessToken))
			expected := base64.RawURLEncoding.EncodeToString(hash[:16])
			if subtle.ConstantTimeCompare([]byte(expected), []byte(identity.AccessTokenHash)) != 1 {
				return nil, nil, errors.New("SIWC identity access token binding failed")
			}
		}
	} else if form.Get("grant_type") == "authorization_code" {
		return nil, nil, errors.New("SIWC identity token is missing")
	}
	if token.ExpiresIn <= 0 && identity != nil {
		token.ExpiresIn = identity.ExpiresAt.Unix() - time.Now().Unix()
	}
	if token.ExpiresIn <= 0 {
		return nil, nil, errors.New("SIWC grant expiry is missing")
	}
	return &token, identity, nil
}

// Metadata and revocation never leave the fixed authentication origin. The
// result distinguishes local disconnection from confirmed provider revocation.
func (s *openaiOAuthService) SIWCRevoke(ctx context.Context, refresh, access, clientID, proxy string) (bool, error) {
	c, err := siwcHTTPClient(proxy)
	if err != nil {
		return false, err
	}
	return siwcRevoke(ctx, c, refresh, access, clientID)
}
func siwcRevoke(ctx context.Context, c *http.Client, refresh, access, clientID string) (bool, error) {
	if refresh == "" && access == "" {
		return false, errors.New("SIWC authorization has no revocable token; remote revocation was not confirmed")
	}
	if !openai.ValidSIWCClientID(clientID) {
		return false, errors.New("invalid SIWC registration")
	}
	raw, status, err := siwcRequest(ctx, c, http.MethodGet, openai.SIWCDiscoveryURL, nil)
	if err != nil || status != http.StatusOK {
		return false, errors.New("SIWC revocation discovery unavailable")
	}
	var discovery struct {
		Issuer             string `json:"issuer"`
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	if json.Unmarshal(raw, &discovery) != nil || discovery.Issuer != openai.SIWCIssuer {
		return false, errors.New("invalid SIWC discovery document")
	}
	endpoint, err := url.Parse(discovery.RevocationEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host != "auth.openai.com" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.RawPath != "" || endpoint.Path == "" {
		return false, errors.New("SIWC revocation endpoint unavailable or untrusted")
	}
	for _, item := range []struct{ token, hint string }{{refresh, "refresh_token"}, {access, "access_token"}} {
		if item.token == "" {
			continue
		}
		_, status, err = siwcRequest(ctx, c, http.MethodPost, endpoint.String(), url.Values{"token": {item.token}, "token_type_hint": {item.hint}, "client_id": {clientID}})
		if err != nil || status != http.StatusOK {
			return false, errors.New("SIWC remote revocation was not confirmed")
		}
	}
	return true, nil
}

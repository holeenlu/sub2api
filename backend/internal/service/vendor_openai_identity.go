package service

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode"

	nativehttpclient "github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/golang-jwt/jwt/v5"
)

const (
	vendorOpenAIIssuer         = "https://auth.openai.com"
	vendorOpenAIJWKSURL        = "https://auth.openai.com/.well-known/jwks.json"
	vendorOpenAIAccessAudience = "https://api.openai.com/v1"
	vendorOpenAIKeyTTL         = 10 * time.Minute
	vendorOpenAIKeyRetry       = time.Minute
	vendorOpenAIVerifyTimeout  = 10 * time.Second
	vendorOpenAIJWKSBodyLimit  = 1 << 20
	vendorOpenAITokenLimit     = 32 << 10
)

// Keep existing verification rejection semantics while exposing a safe source
// availability fact to callers that distinguish infrastructure from credentials.
var (
	// Match this plain marker, not the joined domain error: ApplicationError.Is
	// intentionally matches wrapped domain codes and would broaden that test.
	errVendorOpenAIKeySourceUnavailable = errors.New("OpenAI signing keys are unavailable")
	errVendorOpenAIKeysUnavailable      = errors.Join(ErrVendorIdentityUnverified, errVendorOpenAIKeySourceUnavailable)
)

// Only this fixed provider supplies signing keys. The client factory is private
// and instance-local so tests can serve signed fixtures without changing the
// production trust root, adding a configurable URL, or replacing global state.
type vendorOpenAIJWKSCache struct {
	mu               *contextMutex
	keys             map[string]*rsa.PublicKey
	validUntil       time.Time
	nextFetch        time.Time
	fetchUnavailable bool
	now              func() time.Time
	clientFactory    func(string) (*http.Client, error)
}

func newVendorOpenAIJWKSCache() *vendorOpenAIJWKSCache {
	return &vendorOpenAIJWKSCache{
		mu: newContextMutex(), now: time.Now,
		clientFactory: func(proxy string) (*http.Client, error) {
			return nativehttpclient.GetClient(nativehttpclient.Options{
				ProxyURL: proxy, Timeout: vendorOpenAIVerifyTimeout,
				ResponseHeaderTimeout: vendorOpenAIVerifyTimeout, ValidateResolvedIP: true,
			})
		},
	}
}

// Public provider keys, not credentials or verification results, are shared.
var vendorOpenAISigningKeys = newVendorOpenAIJWKSCache()

func (s *VendorService) vendorVerifyOpenAIProfile(ctx context.Context, t *openai.TokenResponse, method, proxy, mode string) (*vendorVerifiedCredential, error) {
	switch mode {
	case VendorOpenAIModeNative:
		return s.vendorVerifyOpenAI(ctx, t, method, proxy)
	case VendorOpenAIModeBPS:
		return s.vendorVerifyOpenAIBPS(ctx, t, method, proxy, vendorOpenAISigningKeys)
	default:
		return nil, ErrVendorIdentityUnverified
	}
}

type vendorOpenAISignedClaims struct {
	jwt.RegisteredClaims
	ClientID   string                   `json:"client_id"`
	AZP        string                   `json:"azp"`
	Email      string                   `json:"email"`
	AccessHash string                   `json:"at_hash"`
	Auth       *openai.OpenAIAuthClaims `json:"https://api.openai.com/auth"`
	Profile    *struct {
		Email string `json:"email"`
	} `json:"https://api.openai.com/profile"`
}

// Code exchange, refresh and raw-token import all enter the same signature and
// claims checks. DecodeIDToken is intentionally never an identity authority.
func (s *VendorService) vendorVerifyOpenAIBPS(ctx context.Context, t *openai.TokenResponse, method, proxy string, keys *vendorOpenAIJWKSCache) (*vendorVerifiedCredential, error) {
	if s == nil || s.openaiOAuth == nil || t == nil || keys == nil || t.AccessToken == "" ||
		len(t.AccessToken) > vendorOpenAITokenLimit || len(t.IDToken) > vendorOpenAITokenLimit || len(t.RefreshToken) > vendorOpenAITokenLimit ||
		(t.TokenType != "" && !strings.EqualFold(t.TokenType, "Bearer")) {
		return nil, ErrVendorIdentityUnverified
	}
	ctx, cancel := context.WithTimeout(ctx, vendorOpenAIVerifyTimeout)
	defer cancel()
	at, err := keys.verify(ctx, t.AccessToken, proxy, vendorOpenAIAccessAudience, true)
	if err != nil || at.Auth == nil || !vendorOpenAIIdentityValue(at.Auth.ChatGPTAccountID) || at.Auth.ChatGPTAccountID == "default" {
		return nil, ErrVendorIdentityUnverified
	}
	user, ok := vendorOpenAIClaimUser(at.Auth)
	if !ok || user == "" {
		return nil, ErrVendorIdentityUnverified
	}
	email, plan := at.Email, at.Auth.ChatGPTPlanType
	if email == "" && at.Profile != nil {
		email = at.Profile.Email
	}
	if t.IDToken != "" {
		id, err := keys.verify(ctx, t.IDToken, proxy, openai.ExcelClientID, false)
		if err != nil || id.Subject != at.Subject {
			return nil, ErrVendorIdentityUnverified
		}
		if id.Auth != nil {
			if id.Auth.ChatGPTAccountID != "" && id.Auth.ChatGPTAccountID != at.Auth.ChatGPTAccountID {
				return nil, ErrVendorIdentityUnverified
			}
			idUser, valid := vendorOpenAIClaimUser(id.Auth)
			if !valid || (idUser != "" && idUser != user) {
				return nil, ErrVendorIdentityUnverified
			}
			if plan == "" {
				plan = id.Auth.ChatGPTPlanType
			}
		}
		if id.AccessHash != "" {
			sum := sha256.Sum256([]byte(t.AccessToken))
			want := base64.RawURLEncoding.EncodeToString(sum[:sha256.Size/2])
			if subtle.ConstantTimeCompare([]byte(id.AccessHash), []byte(want)) != 1 {
				return nil, ErrVendorIdentityUnverified
			}
		}
		if id.Email != "" {
			email = id.Email
		}
	}
	if ctx.Err() != nil {
		return nil, ErrVendorIdentityUnverified
	}
	info := &OpenAITokenInfo{
		AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, IDToken: t.IDToken,
		ClientID: openai.ExcelClientID, ExpiresAt: at.ExpiresAt.Unix(),
		ChatGPTAccountID: at.Auth.ChatGPTAccountID, ChatGPTUserID: user, Email: email, PlanType: plan,
	}
	// The canonical workspace identity is exactly the native mode's identity.
	// Adding a mode never creates another ownership namespace or account quota.
	return &vendorVerifiedCredential{
		Credentials: s.openaiOAuth.BuildAccountCredentials(info), Identity: info.ChatGPTAccountID,
		AccountID: info.ChatGPTAccountID, Email: email, Method: method,
	}, nil
}

func vendorOpenAIIdentityValue(value string) bool {
	return value != "" && len(value) <= 512 && strings.TrimSpace(value) == value &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func vendorOpenAIClaimUser(auth *openai.OpenAIAuthClaims) (string, bool) {
	if auth == nil {
		return "", true
	}
	user := auth.ChatGPTUserID
	if user == "" {
		user = auth.UserID
	}
	if (user != "" && !vendorOpenAIIdentityValue(user)) ||
		(auth.ChatGPTUserID != "" && auth.UserID != "" && auth.ChatGPTUserID != auth.UserID) {
		return "", false
	}
	return user, true
}

func (c *vendorOpenAIJWKSCache) verify(ctx context.Context, raw, proxy, audience string, access bool) (*vendorOpenAISignedClaims, error) {
	return c.verifyForClient(ctx, raw, proxy, audience, openai.ExcelClientID, access)
}

// Cloud uses the native Codex OAuth client. Share only provider signing keys
// and claims verification; do not inherit an Excel audience or credentials.
func (c *vendorOpenAIJWKSCache) verifyForClient(ctx context.Context, raw, proxy, audience, clientID string, access bool) (*vendorOpenAISignedClaims, error) {
	if raw == "" || len(raw) > vendorOpenAITokenLimit || ctx.Err() != nil {
		return nil, ErrVendorIdentityUnverified
	}
	claims := new(vendorOpenAISignedClaims)
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || !vendorOpenAIIdentityValue(kid) || len(kid) > 256 {
			return nil, ErrVendorIdentityUnverified
		}
		// Token headers cannot introduce a new key source or JOSE extension.
		for _, name := range []string{"jku", "jwk", "x5u", "crit"} {
			if _, present := token.Header[name]; present {
				return nil, ErrVendorIdentityUnverified
			}
		}
		return c.key(ctx, kid, proxy)
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(vendorOpenAIIssuer), jwt.WithAudience(audience),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(c.now), jwt.WithStrictDecoding())
	if errors.Is(err, errVendorOpenAIKeySourceUnavailable) {
		return nil, errVendorOpenAIKeysUnavailable
	}
	if err != nil || token == nil || !token.Valid || !vendorOpenAIIdentityValue(claims.Subject) || claims.IssuedAt == nil || claims.ExpiresAt == nil ||
		!claims.ExpiresAt.After(claims.IssuedAt.Time) || len(claims.Audience) != 1 ||
		(claims.ClientID != "" && claims.ClientID != clientID) ||
		(claims.AZP != "" && claims.AZP != clientID) ||
		(access && claims.ClientID == "" && claims.AZP == "") {
		// Never return JWT parser or transport errors: they can include inputs.
		return nil, ErrVendorIdentityUnverified
	}
	return claims, nil
}

func (c *vendorOpenAIJWKSCache) key(ctx context.Context, kid, proxy string) (*rsa.PublicKey, error) {
	if err := c.mu.Lock(ctx); err != nil {
		return nil, errVendorOpenAIKeysUnavailable
	}
	defer c.mu.Unlock()
	now := c.now()
	if key := c.keys[kid]; key != nil && now.Before(c.validUntil) {
		return key, nil
	}
	// Unknown kids may prompt a rotation refresh, but cannot create an unbounded
	// fetch storm. Failed fetches never extend the lifetime of cached keys.
	if now.Before(c.nextFetch) {
		if c.fetchUnavailable {
			return nil, errVendorOpenAIKeysUnavailable
		}
		return nil, ErrVendorIdentityUnverified
	}
	c.nextFetch = now.Add(vendorOpenAIKeyRetry)
	keys, err := c.fetch(ctx, proxy)
	if err != nil {
		c.fetchUnavailable = true
		return nil, errVendorOpenAIKeysUnavailable
	}
	c.fetchUnavailable = false
	c.keys, c.validUntil = keys, c.now().Add(vendorOpenAIKeyTTL)
	if key := c.keys[kid]; key != nil {
		return key, nil
	}
	return nil, ErrVendorIdentityUnverified
}

func (c *vendorOpenAIJWKSCache) fetch(ctx context.Context, proxy string) (map[string]*rsa.PublicKey, error) {
	client, err := c.clientFactory(proxy)
	if err != nil || client == nil {
		return nil, ErrVendorIdentityUnverified
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, vendorOpenAIJWKSURL, nil)
	if err != nil {
		return nil, ErrVendorIdentityUnverified
	}
	request.Header.Set("Accept", "application/json")
	copyClient := *client
	copyClient.Timeout = vendorOpenAIVerifyTimeout
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(request)
	if err != nil {
		return nil, ErrVendorIdentityUnverified
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > vendorOpenAIJWKSBodyLimit {
		return nil, ErrVendorIdentityUnverified
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, vendorOpenAIJWKSBodyLimit+1))
	if err != nil || len(body) > vendorOpenAIJWKSBodyLimit {
		return nil, ErrVendorIdentityUnverified
	}
	var document struct {
		Keys []struct {
			Kty    string   `json:"kty"`
			Kid    string   `json:"kid"`
			Use    string   `json:"use"`
			Alg    string   `json:"alg"`
			N      string   `json:"n"`
			E      string   `json:"e"`
			KeyOps []string `json:"key_ops"`
		} `json:"keys"`
	}
	if json.Unmarshal(body, &document) != nil || len(document.Keys) == 0 || len(document.Keys) > 32 {
		return nil, ErrVendorIdentityUnverified
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, key := range document.Keys {
		if key.Kty != "RSA" || key.Alg != "RS256" || key.Use != "sig" {
			continue
		}
		if !vendorOpenAIIdentityValue(key.Kid) || len(key.Kid) > 256 || keys[key.Kid] != nil ||
			(len(key.KeyOps) != 0 && (len(key.KeyOps) != 1 || key.KeyOps[0] != "verify")) {
			return nil, ErrVendorIdentityUnverified
		}
		n, nErr := base64.RawURLEncoding.Strict().DecodeString(key.N)
		e, eErr := base64.RawURLEncoding.Strict().DecodeString(key.E)
		if nErr != nil || eErr != nil || len(n) < 256 || len(n) > 1024 || len(e) == 0 || len(e) > 4 {
			return nil, ErrVendorIdentityUnverified
		}
		modulus := new(big.Int).SetBytes(n)
		exponent := new(big.Int).SetBytes(e).Int64()
		if modulus.BitLen() < 2048 || modulus.Bit(0) != 1 || exponent < 3 || exponent > 1<<31-1 || exponent%2 == 0 {
			return nil, ErrVendorIdentityUnverified
		}
		keys[key.Kid] = &rsa.PublicKey{N: modulus, E: int(exponent)}
	}
	if len(keys) == 0 {
		return nil, ErrVendorIdentityUnverified
	}
	return keys, nil
}

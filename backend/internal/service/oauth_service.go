package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/redis/go-redis/v9"
	"log"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// OpenAIOAuthClient interface for OpenAI OAuth operations
type OpenAIOAuthClient interface {
	ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error)
	RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error)
}

// GrokOAuthClient interface for xAI/Grok OAuth operations.
type GrokOAuthClient interface {
	ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*xai.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken, proxyURL, clientID string) (*xai.TokenResponse, error)
	// LoginWithPassword exchanges email/password for a short-lived Web SSO cookie.
	// Callers must convert via ConvertSSOToBuild and must not persist password or raw SSO.
	LoginWithPassword(ctx context.Context, email, password, proxyURL string) (*GrokPasswordLoginResult, error)
	ConvertSSOToBuild(ctx context.Context, ssoToken, proxyURL string) (*xai.TokenResponse, error)
}

// GrokOAuthTokenService is the narrow refresh port used by Grok token providers.
type GrokOAuthTokenService interface {
	RefreshAccountToken(ctx context.Context, account *Account) (*GrokTokenInfo, error)
	BuildAccountCredentials(tokenInfo *GrokTokenInfo) map[string]any
}

// ClaudeOAuthClient handles HTTP requests for Claude OAuth flows
type ClaudeOAuthClient interface {
	GetOrganizationUUID(ctx context.Context, sessionKey, proxyURL string) (string, error)
	GetAuthorizationCode(ctx context.Context, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL string) (string, error)
	ExchangeCodeForToken(ctx context.Context, code, codeVerifier, state, proxyURL string, isSetupToken bool) (*oauth.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*oauth.TokenResponse, error)
}

// NormalizeClaudeSetupToken accepts the CLI's raw token or export assignment.
// Parsing is not proof of identity or connectivity; those are separate concerns.
func NormalizeClaudeSetupToken(raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if strings.HasPrefix(token, "export ") {
		name, value, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(token, "export ")), "=")
		if !ok || strings.TrimSpace(name) != "CLAUDE_CODE_OAUTH_TOKEN" {
			return "", vendorAccountInputError("Expected a Claude Setup Token")
		}
		token = strings.TrimSpace(value)
	}
	if len(token) >= 2 && ((token[0] == '\'' && token[len(token)-1] == '\'') || (token[0] == '"' && token[len(token)-1] == '"')) {
		token = strings.TrimSpace(token[1 : len(token)-1])
	}
	if !strings.HasPrefix(token, "sk-ant-oat") || strings.ContainsAny(token, " \t\r\n\"'") {
		return "", vendorAccountInputError("Setup Token must be a single sk-ant-oat token")
	}
	return token, nil
}

// OAuthService handles OAuth authentication flows
type OAuthService struct {
	rdb          *redis.Client
	sessionStore *oauth.SessionStore
	proxyRepo    ProxyRepository
	oauthClient  ClaudeOAuthClient
}

// NewOAuthService creates a new OAuth service
func NewOAuthService(proxyRepo ProxyRepository, oauthClient ClaudeOAuthClient, shared ...*redis.Client) *OAuthService {
	var rdb *redis.Client
	if len(shared) > 0 {
		rdb = shared[0]
	}
	return &OAuthService{
		rdb:          rdb,
		sessionStore: oauth.NewSessionStore(),
		proxyRepo:    proxyRepo,
		oauthClient:  oauthClient,
	}
}

// Retain provider-verified identity across the existing exchange/save requests.
// Only identity metadata is cached; the key binds it to the exact token, and the
// authorization lease binds it to the actor/session. Client UUID fields are not proof.
type claudeOAuthIdentity struct {
	Authorization *authz.Lease `json:"authorization"`
	AccountUUID   string       `json:"account_uuid"`
	OrgUUID       string       `json:"org_uuid"`
	EmailAddress  string       `json:"email_address"`
}

func claudeTokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
func claudeOAuthIdentityKey(token string) string {
	return "oauth:anthropic:identity:" + claudeTokenDigest(token)
}
func (s *OAuthService) rememberClaudeIdentity(ctx context.Context, t *TokenInfo) error {
	if s.rdb == nil || t.AccountUUID == "" || t.OrgUUID == "" || t.AccessToken == "" {
		return nil
	}
	lease := authz.CaptureLease(ctx)
	if lease == nil {
		return nil
	}
	if err := authz.CheckLease(ctx, lease, "accounts.authorize"); err != nil {
		return err
	}
	proof := claudeOAuthIdentity{lease, t.AccountUUID, t.OrgUUID, t.EmailAddress}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, claudeOAuthIdentityKey(t.AccessToken), raw, oauth.SessionTTL).Err()
}
func (s *OAuthService) claudeIdentityForToken(ctx context.Context, token string) (*claudeOAuthIdentity, error) {
	if s == nil || s.rdb == nil || authz.CaptureLease(ctx) == nil {
		return nil, nil
	}
	proof, _, err := oauth.ReadSharedSession[claudeOAuthIdentity](ctx, s.rdb, claudeOAuthIdentityKey(token))
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := authz.CheckLease(ctx, proof.Authorization, "accounts.authorize"); err != nil {
		return nil, nil // Another session must obtain its own identity evidence.
	}
	if proof.AccountUUID == "" || proof.OrgUUID == "" {
		return nil, nil
	}
	return proof, nil
}

// GenerateAuthURLResult contains the authorization URL and session info
type GenerateAuthURLResult struct {
	AuthURL   string `json:"auth_url"`
	SessionID string `json:"session_id"`
}

// GenerateAuthURL generates an OAuth authorization URL with full scope
func (s *OAuthService) GenerateAuthURL(ctx context.Context, proxyID *int64) (*GenerateAuthURLResult, error) {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return nil, authz.ErrAuthorizationExpired
	}
	return s.generateAuthURLWithScope(ctx, oauth.ScopeOAuth, proxyID)
}

// GenerateSetupTokenURL generates an OAuth authorization URL for setup token (inference only)
func (s *OAuthService) GenerateSetupTokenURL(ctx context.Context, proxyID *int64) (*GenerateAuthURLResult, error) {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	scope := oauth.ScopeInference
	return s.generateAuthURLWithScope(ctx, scope, proxyID)
}

func (s *OAuthService) generateAuthURLWithScope(ctx context.Context, scope string, proxyID *int64) (*GenerateAuthURLResult, error) {
	// Generate PKCE values
	state, err := oauth.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %w", err)
	}

	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	// Generate session ID
	sessionID, err := oauth.GenerateSessionID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}

	// Get proxy URL if specified
	var proxyURL string
	if proxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// Store session
	session := &oauth.OAuthSession{
		Authorization: authz.CaptureLease(ctx),
		State:         state,
		CodeVerifier:  codeVerifier,
		Scope:         scope,
		ProxyURL:      proxyURL,
		CreatedAt:     time.Now(),
	}
	if s.rdb != nil {
		if err := oauth.PutSharedSession(ctx, s.rdb, "oauth:anthropic:"+sessionID, session, oauth.SessionTTL); err != nil {
			return nil, err
		}
	} else {
		s.sessionStore.Set(sessionID, session)
	}

	// Build authorization URL
	authURL := oauth.BuildAuthorizationURL(state, codeChallenge, scope)

	return &GenerateAuthURLResult{
		AuthURL:   authURL,
		SessionID: sessionID,
	}, nil
}

// ExchangeCodeInput represents the input for code exchange
type ExchangeCodeInput struct {
	SessionID string
	Code      string
	ProxyID   *int64
}

// TokenInfo represents the token information stored in credentials
type TokenInfo struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	OrgUUID      string `json:"org_uuid,omitempty"`
	AccountUUID  string `json:"account_uuid,omitempty"`
	EmailAddress string `json:"email_address,omitempty"`
}

// ExchangeCode exchanges authorization code for tokens
func (s *OAuthService) ExchangeCode(ctx context.Context, input *ExchangeCodeInput) (*TokenInfo, error) {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return nil, authz.ErrAuthorizationExpired
	}
	if input == nil || input.SessionID == "" || strings.TrimSpace(input.Code) == "" {
		return nil, fmt.Errorf("invalid OAuth exchange input")
	}
	// Get session
	session, ok := s.sessionStore.Get(input.SessionID)
	var raw string
	if s.rdb != nil {
		var err error
		session, raw, err = oauth.ReadSharedSession[oauth.OAuthSession](ctx, s.rdb, "oauth:anthropic:"+input.SessionID)
		ok = err == nil
	}
	if !ok {
		return nil, fmt.Errorf("session not found or expired")
	}
	if err := authz.CheckLease(ctx, session.Authorization, "accounts.authorize"); err != nil {
		return nil, err
	}

	if code, state, hasState := strings.Cut(strings.TrimSpace(input.Code), "#"); hasState {
		if code == "" || subtle.ConstantTimeCompare([]byte(state), []byte(session.State)) != 1 {
			return nil, fmt.Errorf("invalid oauth state")
		}
	}
	// Get proxy URL
	proxyURL := session.ProxyURL
	if input.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *input.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// Determine if this is a setup token (scope is inference only)
	isSetupToken := session.Scope == oauth.ScopeInference

	// Atomically claim before the remote side effect; failures are not replayable.
	if s.rdb != nil {
		if _, err := oauth.CompareSwapSession(ctx, s.rdb, "oauth:anthropic:"+input.SessionID, raw, nil); err != nil {
			return nil, oauth.ErrSessionConflict
		}
	} else if !s.sessionStore.Consume(input.SessionID, session) {
		return nil, oauth.ErrSessionConflict
	}
	// Exchange code for token
	tokenInfo, err := s.exchangeCodeForToken(ctx, input.Code, session.CodeVerifier, session.State, proxyURL, isSetupToken)
	if err != nil {
		return nil, err
	}

	if err := authz.CheckLease(ctx, session.Authorization, "accounts.authorize"); err != nil {
		return nil, err
	}

	if err := s.rememberClaudeIdentity(ctx, tokenInfo); err != nil {
		return nil, err
	}

	// Delete session after successful exchange
	s.sessionStore.Delete(input.SessionID)

	return tokenInfo, nil
}

// CookieAuthInput represents the input for cookie-based authentication
type CookieAuthInput struct {
	SessionKey string
	ProxyID    *int64
	Scope      string // "full" or "inference"
}

// CookieAuth performs OAuth using sessionKey (cookie-based auto-auth)
func (s *OAuthService) CookieAuth(ctx context.Context, input *CookieAuthInput) (*TokenInfo, error) {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	// Get proxy URL if specified
	var proxyURL string
	if input.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *input.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// Determine scope and if this is a setup token
	// Internal API call uses ScopeAPI (org:create_api_key not supported)
	scope := oauth.ScopeAPI
	isSetupToken := false
	if input.Scope == "inference" {
		scope = oauth.ScopeInference
		isSetupToken = true
	}

	// Step 1: Get organization info using sessionKey
	orgUUID, err := s.getOrganizationUUID(ctx, input.SessionKey, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization info: %w", err)
	}

	// Step 2: Generate PKCE values
	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %w", err)
	}
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	state, err := oauth.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	// Step 3: Get authorization code using cookie
	authCode, err := s.getAuthorizationCode(ctx, input.SessionKey, orgUUID, scope, codeChallenge, state, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization code: %w", err)
	}

	// Step 4: Exchange code for token
	tokenInfo, err := s.exchangeCodeForToken(ctx, authCode, codeVerifier, state, proxyURL, isSetupToken)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Ensure org_uuid is set (from step 1 if not from token response)
	if tokenInfo.OrgUUID == "" && orgUUID != "" {
		tokenInfo.OrgUUID = orgUUID
		log.Printf("[OAuth] Set org_uuid from cookie auth")
	}

	if err := s.rememberClaudeIdentity(ctx, tokenInfo); err != nil {
		return nil, err
	}
	return tokenInfo, nil
}

// getOrganizationUUID gets the organization UUID from claude.ai using sessionKey
func (s *OAuthService) getOrganizationUUID(ctx context.Context, sessionKey, proxyURL string) (string, error) {
	return s.oauthClient.GetOrganizationUUID(ctx, sessionKey, proxyURL)
}

// getAuthorizationCode gets the authorization code using sessionKey
func (s *OAuthService) getAuthorizationCode(ctx context.Context, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL string) (string, error) {
	return s.oauthClient.GetAuthorizationCode(ctx, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL)
}

// exchangeCodeForToken exchanges authorization code for tokens
func (s *OAuthService) exchangeCodeForToken(ctx context.Context, code, codeVerifier, state, proxyURL string, isSetupToken bool) (*TokenInfo, error) {
	tokenResp, err := s.oauthClient.ExchangeCodeForToken(ctx, code, codeVerifier, state, proxyURL, isSetupToken)
	if err != nil {
		return nil, err
	}

	tokenInfo := &TokenInfo{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    time.Now().Unix() + tokenResp.ExpiresIn,
		RefreshToken: tokenResp.RefreshToken,
		Scope:        tokenResp.Scope,
	}

	if tokenResp.Organization != nil && tokenResp.Organization.UUID != "" {
		tokenInfo.OrgUUID = tokenResp.Organization.UUID
		log.Printf("[OAuth] Got org_uuid")
	}
	if tokenResp.Account != nil {
		if tokenResp.Account.UUID != "" {
			tokenInfo.AccountUUID = tokenResp.Account.UUID
			log.Printf("[OAuth] Got account_uuid")
		}
		if tokenResp.Account.EmailAddress != "" {
			tokenInfo.EmailAddress = tokenResp.Account.EmailAddress
			log.Printf("[OAuth] Got email_address")
		}
	}

	return tokenInfo, nil
}

// RefreshToken refreshes an OAuth token
func (s *OAuthService) RefreshToken(ctx context.Context, refreshToken string, proxyURL string) (*TokenInfo, error) {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	tokenResp, err := s.oauthClient.RefreshToken(ctx, refreshToken, proxyURL)
	if err != nil {
		return nil, err
	}

	return &TokenInfo{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    time.Now().Unix() + tokenResp.ExpiresIn,
		RefreshToken: tokenResp.RefreshToken,
		Scope:        tokenResp.Scope,
	}, nil
}

// RefreshAccountToken refreshes token for an account
func (s *OAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*TokenInfo, error) {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	refreshToken := account.GetCredential("refresh_token")
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	var proxyURL string
	if account.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	return s.RefreshToken(ctx, refreshToken, proxyURL)
}

// Stop stops the session store cleanup goroutine
// Start explicitly starts the session owner; constructors do not start workers.
func (s *OAuthService) Start(ctx context.Context) error {
	return s.sessionStore.Start(ctx)
}

// Drain rejects new OAuth work and waits for admitted operations and the janitor.
func (s *OAuthService) Drain(ctx context.Context) error {
	return s.sessionStore.Drain(ctx)
}

// Stop cancels and joins the same owner, retaining join state after a timeout.
func (s *OAuthService) Stop(ctx context.Context) error {
	return s.sessionStore.Stop(ctx)
}

package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
)

const OpenAISIWCEnabledKey = "openai_siwc"

type OpenAISIWCCredentialRepository interface {
	SIWCInstallationID(context.Context, string) (string, error)
	GetSIWCRecord(context.Context, *Account) (string, string, error)
	StoreSIWCRecord(context.Context, *Account, string, string, string, string) (bool, error)
}
type openAISIWCTokenClient interface {
	SIWCToken(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error)
}
type openAISIWCRevocationClient interface {
	SIWCRevoke(context.Context, string, string, string, string) (bool, error)
}

// Registration survives signout and terminal refresh failures. It is encrypted
// independently of the rotating grant and never copies native Codex credentials.
type siwcRegistration struct {
	HostID          string `json:"host_id"`
	ClientID        string `json:"client_id,omitempty"`
	Issuer          string `json:"issuer,omitempty"`
	Subject         string `json:"subject,omitempty"`
	Email           string `json:"email,omitempty"`
	PrimaryIdentity string `json:"primary_identity"`
	Generation      string `json:"generation,omitempty"`
}
type siwcGrant struct {
	EarliestRefreshAt json.RawMessage `json:"earliest_refresh_at,omitempty"`
	AccessToken       string          `json:"access_token"`
	RefreshToken      string          `json:"refresh_token,omitempty"`
	IDToken           string          `json:"id_token,omitempty"`
	Scope             string          `json:"scope"`
	ExpiresAt         int64           `json:"expires_at"`
	Rejected          bool            `json:"rejected,omitempty"`
}
type OpenAISIWCStatus struct {
	IdentityVerified   bool   `json:"identity_verified"`
	DirectAccess       bool   `json:"direct_access"`
	RefreshRequired    bool   `json:"refresh_required"`
	Bound              bool   `json:"bound"`
	Enabled            bool   `json:"enabled"`
	Registered         bool   `json:"registered"`
	IndependentBinding bool   `json:"independent_binding"`
	ClientID           string `json:"client_id,omitempty"`
	Issuer             string `json:"issuer,omitempty"`
	Subject            string `json:"subject,omitempty"`
	Email              string `json:"email,omitempty"`
	ExpiresAt          int64  `json:"expires_at,omitempty"`
}
type OpenAISIWCRevokeResult struct {
	Bound                     bool   `json:"bound"`
	RemoteRevocationConfirmed bool   `json:"remote_revocation_confirmed"`
	Message                   string `json:"message"`
}

func (a *Account) IsSIWCEligible() bool {
	return a != nil && a.IsOpenAIOAuth() && !a.IsShadow() && a.VendorID == nil && !a.IsOpenAIPersonalAccessToken() && !a.IsOpenAIAgentIdentity() && !a.IsExcelOAuth()
}

// Selection is separate from eligibility. A selected account that becomes
// ineligible must fail in SIWC authorization, never fall back to Codex/BPS.
func (a *Account) IsSIWCEnabled() bool {
	return a != nil && a.Platform == PlatformOpenAI && a.Extra[OpenAISIWCEnabledKey] == true
}
func (s *OpenAIOAuthService) SetSIWCAgentName(name string) { s.siwcAgentName = strings.TrimSpace(name) }
func siwcDirectRequired() error {
	return infraerrors.Forbidden("OPENAI_SIWC_DIRECT_ACCESS_REQUIRED", "Sign-in identity is verified but this grant does not include chatgpt.tokens.use.direct; inference is unavailable")
}
func siwcRequired() error {
	return infraerrors.ServiceUnavailable("OPENAI_SIWC_AUTH_REQUIRED", "SIWC authorization is required; authorize this account with Sign in with ChatGPT")
}
func siwcConflict() error {
	return infraerrors.Conflict("OPENAI_SIWC_AUTH_CHANGED", "SIWC authorization or account changed; start a new authorization")
}
func (s *OpenAIOAuthService) siwcRepository(account *Account) (OpenAISIWCCredentialRepository, error) {
	if !account.IsSIWCEligible() {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_ACCOUNT_INVALID", "SIWC requires an ordinary administrator-owned OpenAI OAuth parent account")
	}
	repo, ok := s.excelAccountRepo.(OpenAISIWCCredentialRepository)
	if !ok || s.excelEncryptor == nil {
		return nil, infraerrors.ServiceUnavailable("OPENAI_SIWC_STORAGE_UNAVAILABLE", "SIWC requires private storage and a persistent encryption key")
	}
	return repo, nil
}
func siwcHash(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func siwcPrimaryIdentity(account *Account) string {
	return siwcHash([]any{account.GetCredential("chatgpt_account_id"), account.GetCredential("chatgpt_user_id"), account.GetCredential("email"), account.GetCredential("client_id"), account.GetCredential(openAIAuthModeCredentialKey), account.GetCredential(openAIAuthModeLegacyCredentialKey)})
}

func siwcHasPrimaryIdentity(account *Account) bool {
	return account.GetCredential("chatgpt_account_id") != "" || account.GetCredential("chatgpt_user_id") != "" || account.GetCredential("email") != ""
}

func (s *OpenAIOAuthService) readSIWCRecord(ctx context.Context, account *Account) (string, string, *siwcRegistration, *siwcGrant, error) {
	repo, err := s.siwcRepository(account)
	if err != nil {
		return "", "", nil, nil, err
	}
	registration, grant, err := repo.GetSIWCRecord(ctx, account)
	if err != nil {
		return "", "", nil, nil, errors.New("SIWC credential storage unavailable")
	}
	if registration == "" {
		return "", "", nil, nil, nil
	}
	raw, err := s.excelEncryptor.Decrypt(registration)
	if err != nil {
		return "", "", nil, nil, errors.New("SIWC registration decryption failed")
	}
	var r siwcRegistration
	if json.Unmarshal([]byte(raw), &r) != nil || r.HostID == "" || r.PrimaryIdentity != siwcPrimaryIdentity(account) || (r.ClientID != "" && !openai.ValidSIWCClientID(r.ClientID)) || (r.Subject != "" && (r.Issuer != openai.SIWCIssuer || r.Generation == "")) {
		return "", "", nil, nil, siwcConflict()
	}
	if grant == "" {
		return registration, "", &r, nil, nil
	}
	raw, err = s.excelEncryptor.Decrypt(grant)
	if err != nil {
		return "", "", nil, nil, errors.New("SIWC credential decryption failed")
	}
	var g siwcGrant
	if json.Unmarshal([]byte(raw), &g) != nil || r.Subject == "" || (g.AccessToken == "" && g.IDToken == "") || (openai.HasSIWCPermission(g.Scope) && g.AccessToken == "") || g.ExpiresAt <= 0 {
		return "", "", nil, nil, errors.New("invalid saved SIWC credentials")
	}
	return registration, grant, &r, &g, nil
}
func (s *OpenAIOAuthService) saveSIWCRecord(ctx context.Context, account *Account, previousRegistration, previousGrant string, r *siwcRegistration, g *siwcGrant) (string, string, error) {
	repo, err := s.siwcRepository(account)
	if err != nil {
		return "", "", err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", "", err
	}
	registration, err := s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return "", "", errors.New("SIWC registration encryption failed")
	}
	grant := ""
	if g != nil {
		raw, err = json.Marshal(g)
		if err != nil {
			return "", "", err
		}
		grant, err = s.excelEncryptor.Encrypt(string(raw))
		if err != nil {
			return "", "", errors.New("SIWC credential encryption failed")
		}
	}
	saved, err := repo.StoreSIWCRecord(ctx, account, previousRegistration, previousGrant, registration, grant)
	if err != nil {
		return "", "", errors.New("SIWC credential storage failed")
	}
	if !saved {
		return "", "", siwcConflict()
	}
	return registration, grant, nil
}
func (s *OpenAIOAuthService) siwcProxy(ctx context.Context, a *Account) (string, error) {
	if a.ProxyID == nil {
		return "", nil
	}
	if s.proxyRepo != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *a.ProxyID)
		if err != nil || proxy == nil {
			return "", errors.New("SIWC account proxy unavailable")
		}
		return proxy.URL(), nil
	}
	if a.Proxy != nil {
		return a.Proxy.URL(), nil
	}
	return "", errors.New("SIWC account proxy unavailable")
}
func (s *OpenAIOAuthService) SIWCAuthorizationStatus(ctx context.Context, account *Account) (*OpenAISIWCStatus, error) {
	_, _, r, g, err := s.readSIWCRecord(ctx, account)
	if err != nil {
		return nil, err
	}
	status := &OpenAISIWCStatus{Enabled: account.IsSIWCEnabled(), IndependentBinding: true}
	if r != nil {
		status.Registered = r.ClientID != ""
		status.ClientID = r.ClientID
		status.Issuer = r.Issuer
		status.Subject = r.Subject
		status.Email = r.Email
		status.IdentityVerified = r.Issuer == openai.SIWCIssuer && r.Subject != ""
		status.RefreshRequired = status.IdentityVerified && g == nil
	}
	if g != nil {
		status.Bound = g.RefreshToken != "" || (!g.Rejected && g.ExpiresAt > time.Now().Unix())
		status.ExpiresAt = g.ExpiresAt
		status.DirectAccess = openai.HasSIWCPermission(g.Scope)
		status.RefreshRequired = !status.Bound
	}
	return status, nil
}
func (s *OpenAIOAuthService) BeginSIWCAuthorization(ctx context.Context, account *Account) (*OpenAIAuthURLResult, error) {
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return nil, authz.ErrPermissionDenied
	}
	lease := authz.CaptureLease(ctx)
	if err := authz.CheckLease(ctx, lease, "accounts.authorize"); err != nil {
		return nil, err
	}
	repo, err := s.siwcRepository(account)
	if err != nil {
		return nil, err
	}
	if !siwcHasPrimaryIdentity(account) {
		return nil, errors.New("SIWC requires stable parent account identity metadata")
	}
	registration, grant, r, g, err := s.readSIWCRecord(ctx, account)
	if err != nil {
		return nil, err
	}
	if r == nil {
		host, err := repo.SIWCInstallationID(ctx, "urn:uuid:"+uuid.NewString())
		if err != nil || host == "" {
			return nil, errors.New("SIWC installation registration unavailable")
		}
		r = &siwcRegistration{HostID: host, PrimaryIdentity: siwcPrimaryIdentity(account)}
		registration, grant, err = s.saveSIWCRecord(ctx, account, "", "", r, nil)
		if err != nil {
			return nil, err
		}
	}
	proxy, err := s.siwcProxy(ctx, account)
	if err != nil {
		return nil, err
	}
	state, err := openai.GenerateState()
	if err != nil {
		return nil, err
	}
	nonce, err := openai.GenerateState()
	if err != nil {
		return nil, err
	}
	verifier, err := openai.GenerateCodeVerifier()
	if err != nil {
		return nil, err
	}
	sessionID, err := openai.GenerateSessionID()
	if err != nil {
		return nil, err
	}
	session := &openai.OAuthSession{Authorization: lease, State: state, CodeVerifier: verifier, ClientID: r.ClientID, ProxyURL: proxy, RedirectURI: openai.SIWCRedirectURI, CreatedAt: time.Now(), SIWCAccountID: account.ID, SIWCNonce: nonce, SIWCPrimaryHash: siwcHash(account.Credentials), SIWCRegistration: registration, SIWCGrant: grant}
	if s.rdb != nil {
		err = oauth.PutSharedSession(ctx, s.rdb, "oauth:siwc:"+sessionID, session, openai.SessionTTL)
	} else {
		s.sessionStore.Set(sessionID, session)
	}
	if err != nil {
		return nil, err
	}
	hint := ""
	if g != nil {
		hint = g.IDToken
	}
	name := s.siwcAgentName
	if name == "" {
		name = "Sub2API"
	}
	authURL := openai.BuildSIWCAuthorizationURL(state, openai.GenerateCodeChallenge(verifier), nonce, r.HostID, r.ClientID, hint, name)
	// This explicit reconnect follows an identity-only grant. Request additional
	// plan consent, not the undeployed force_reconsent extension; ordinary
	// returning sign-ins do not force consent.
	if g != nil && !openai.HasSIWCPermission(g.Scope) {
		parsed, _ := url.Parse(authURL)
		query := parsed.Query()
		query.Set("prompt", "consent")
		parsed.RawQuery = query.Encode()
		authURL = parsed.String()
	}
	return &OpenAIAuthURLResult{SessionID: sessionID, AuthURL: authURL}, nil
}
func (s *OpenAIOAuthService) CompleteSIWCAuthorization(ctx context.Context, account *Account, sessionID, callback string) error {
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return authz.ErrPermissionDenied
	}
	if _, err := s.siwcRepository(account); err != nil {
		return err
	}
	var session *openai.OAuthSession
	var raw string
	var err error
	if s.rdb != nil {
		session, raw, err = oauth.ReadSharedSession[openai.OAuthSession](ctx, s.rdb, "oauth:siwc:"+sessionID)
	} else {
		var ok bool
		session, ok = s.sessionStore.Get(sessionID)
		if !ok {
			err = oauth.ErrSessionConflict
		}
	}
	if err != nil || session == nil || session.SIWCAccountID != account.ID || time.Since(session.CreatedAt) > openai.SessionTTL || session.SIWCPrimaryHash != siwcHash(account.Credentials) {
		return siwcConflict()
	}
	if err = authz.CheckLease(ctx, session.Authorization, "accounts.authorize"); err != nil {
		return err
	}
	if len(callback) > 32<<10 {
		return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Invalid SIWC callback")
	}
	u, err := url.Parse(callback)
	if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:1455" || u.Path != "/auth/callback" || u.User != nil || u.RawPath != "" || u.Fragment != "" {
		return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Invalid SIWC callback")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Invalid SIWC callback")
	}
	for _, key := range []string{"state", "code", "client_id", "error", "iss"} {
		if len(q[key]) > 1 {
			return infraerrors.BadRequest("OPENAI_SIWC_CALLBACK_INVALID", "Duplicate SIWC callback parameter")
		}
	}
	if len(q["iss"]) != 0 && q.Get("iss") != openai.SIWCIssuer {
		return infraerrors.BadRequest("OPENAI_SIWC_ISSUER_INVALID", "SIWC callback issuer does not match OpenAI")
	}
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(session.State)) != 1 {
		return infraerrors.BadRequest("OPENAI_SIWC_STATE_INVALID", "Invalid SIWC state")
	}
	consume := func() error {
		if s.rdb != nil {
			_, err := oauth.CompareSwapSession(ctx, s.rdb, "oauth:siwc:"+sessionID, raw, nil)
			return err
		}
		if !s.sessionStore.Consume(sessionID, session) {
			return oauth.ErrSessionConflict
		}
		return nil
	}
	if q.Get("error") != "" {
		if err = consume(); err != nil {
			return err
		}
		return infraerrors.BadRequest("OPENAI_SIWC_CONSENT_REQUIRED", "SIWC authorization was declined or requires user interaction")
	}
	clientID := q.Get("client_id")
	if session.ClientID != "" {
		if clientID != "" && clientID != session.ClientID {
			return siwcConflict()
		}
		clientID = session.ClientID
	}
	if !openai.ValidSIWCClientID(clientID) || q.Get("code") == "" {
		return infraerrors.BadRequest("OPENAI_SIWC_REGISTRATION_INCOMPLETE", "SIWC callback requires code and issued client ID")
	}
	client, ok := s.oauthClient.(openAISIWCTokenClient)
	if !ok {
		return errors.New("SIWC token client unavailable")
	}
	unlock, err := s.lockSIWCCredentials(ctx, account.ID, nil, true)
	if err != nil {
		return err
	}
	defer unlock()
	previousRegistration, previousGrant, r, priorGrant, err := s.readSIWCRecord(ctx, account)
	if err != nil {
		return err
	}
	if r == nil || previousRegistration != session.SIWCRegistration || previousGrant != session.SIWCGrant {
		return siwcConflict()
	}
	if err = consume(); err != nil {
		return err
	}
	// Persist issued registration before consuming the code. Even invalid_grant
	// must reuse this client ID on a fresh authorization, never create another.
	r.ClientID = clientID
	previousRegistration, previousGrant, err = s.saveSIWCRecord(ctx, account, previousRegistration, previousGrant, r, priorGrant)
	if err != nil {
		return err
	}
	exchangeCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	token, identity, err := client.SIWCToken(exchangeCtx, url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {q.Get("code")}, "code_verifier": {session.CodeVerifier}, "redirect_uri": {session.RedirectURI}, "resource": {openai.SIWCResource}}, session.ProxyURL, session.SIWCNonce)
	if err != nil {
		return err
	}
	if token == nil || identity == nil || identity.Issuer != openai.SIWCIssuer || identity.Subject == "" || (token.AccessToken == "" && token.IDToken == "") || token.ExpiresIn <= 0 || (openai.HasSIWCPermission(token.Scope) && token.AccessToken == "") {
		return errors.New("SIWC credentials are incomplete")
	}
	if r.Subject != "" && (r.Issuer != identity.Issuer || r.Subject != identity.Subject) {
		return infraerrors.BadRequest("OPENAI_SIWC_IDENTITY_CHANGED", "SIWC subject does not match this registration; native workspace identity is not inferred")
	}
	if err = authz.CheckLease(ctx, session.Authorization, "accounts.authorize"); err != nil {
		return err
	}
	r.Issuer, r.Subject, r.Email, r.Generation = identity.Issuer, identity.Subject, identity.Email, uuid.NewString()
	g := &siwcGrant{EarliestRefreshAt: append(json.RawMessage(nil), token.EarliestRefreshAt...), AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, IDToken: token.IDToken, Scope: token.Scope, ExpiresAt: time.Now().Unix() + token.ExpiresIn}
	saveCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	_, _, err = s.saveSIWCRecord(saveCtx, account, previousRegistration, previousGrant, r, g)
	return err
}

var siwcCredentialLocks sync.Map

func (s *OpenAIOAuthService) lockSIWCCredentials(ctx context.Context, id int64, cache OpenAITokenCache, wait bool) (func(), error) {
	key := fmt.Sprintf("openai:siwc:account:%d", id)
	value, _ := siwcCredentialLocks.LoadOrStore(key, newContextMutex())
	mu := value.(*contextMutex)
	if wait {
		if err := mu.Lock(ctx); err != nil {
			return nil, err
		}
	} else {
		select {
		case mu.token <- struct{}{}:
		default:
			return nil, nil
		}
	}
	if s.rdb != nil {
		owner := uuid.NewString()
		acquired, err := s.rdb.SetNX(ctx, key, owner, 60*time.Second).Result()
		if err != nil || !acquired {
			mu.Unlock()
			if err == nil && !wait {
				return nil, nil
			}
			return nil, infraerrors.ServiceUnavailable("OPENAI_SIWC_REFRESH_BUSY", "SIWC authorization is busy; retry later")
		}
		return func() {
			defer mu.Unlock()
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_, _ = oauth.CompareSwapSession(cleanup, s.rdb, key, owner, nil)
		}, nil
	}
	if cache != nil {
		acquired, err := cache.AcquireRefreshLock(ctx, key, 60*time.Second)
		if err != nil || !acquired {
			mu.Unlock()
			if err == nil && !wait {
				return nil, nil
			}
			return nil, infraerrors.ServiceUnavailable("OPENAI_SIWC_REFRESH_BUSY", "SIWC authorization is busy; retry later")
		}
		return func() {
			defer mu.Unlock()
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = cache.ReleaseRefreshLock(cleanup, key)
		}, nil
	}
	return mu.Unlock, nil
}
func (s *OpenAIOAuthService) SIWCAccessToken(ctx context.Context, account *Account, cache OpenAITokenCache) (string, error) {
	token, _, err := s.siwcAccessToken(ctx, account, cache)
	return token, err
}
func siwcGrantOwner(r *siwcRegistration) string {
	return siwcHash([]string{r.Issuer, r.Subject, r.ClientID, r.Generation})
}
func (s *OpenAIOAuthService) siwcAccessToken(ctx context.Context, account *Account, cache OpenAITokenCache) (string, string, error) {
	_, _, r, g, err := s.readSIWCRecord(ctx, account)
	if err != nil {
		return "", "", err
	}
	if g == nil {
		return "", "", siwcRequired()
	}
	if !openai.HasSIWCPermission(g.Scope) {
		return "", "", siwcDirectRequired()
	}
	fresh := func(g *siwcGrant) bool {
		if g == nil || g.Rejected {
			return false
		}
		deadline := time.Now()
		if g.RefreshToken != "" {
			deadline = deadline.Add(openAITokenRefreshSkew)
		}
		return g.ExpiresAt > deadline.Unix()
	}
	if fresh(g) {
		return g.AccessToken, siwcGrantOwner(r), nil
	}
	unlock, err := s.lockSIWCCredentials(ctx, account.ID, cache, true)
	if err != nil {
		return "", "", err
	}
	defer unlock()
	previousRegistration, previousGrant, r, g, err := s.readSIWCRecord(ctx, account)
	if err != nil {
		return "", "", err
	}
	if g != nil && !openai.HasSIWCPermission(g.Scope) {
		return "", "", siwcDirectRequired()
	}
	if fresh(g) {
		return g.AccessToken, siwcGrantOwner(r), nil
	}
	if g == nil || g.RefreshToken == "" {
		return "", "", siwcRequired()
	}
	client, ok := s.oauthClient.(openAISIWCTokenClient)
	if !ok {
		return "", "", errors.New("SIWC token client unavailable")
	}
	proxy, err := s.siwcProxy(ctx, account)
	if err != nil {
		return "", "", err
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	token, identity, err := client.SIWCToken(refreshCtx, url.Values{"grant_type": {"refresh_token"}, "client_id": {r.ClientID}, "refresh_token": {g.RefreshToken}, "resource": {openai.SIWCResource}}, proxy, "")
	if err != nil {
		var rejected *openai.SIWCOAuthError
		if errors.As(err, &rejected) && rejected.Terminal() {
			saveCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer stop()
			if _, _, saveErr := s.saveSIWCRecord(saveCtx, account, previousRegistration, previousGrant, r, nil); saveErr != nil {
				return "", "", saveErr
			}
			return "", "", siwcRequired()
		}
		return "", "", infraerrors.ServiceUnavailable("OPENAI_SIWC_REFRESH_FAILED", "SIWC refresh failed; credentials were retained")
	}
	if token == nil || token.AccessToken == "" || token.ExpiresIn <= 0 || (identity != nil && (identity.Issuer != r.Issuer || identity.Subject != r.Subject)) {
		return "", "", errors.New("refreshed SIWC grant does not match registration")
	}
	g.EarliestRefreshAt = append(json.RawMessage(nil), token.EarliestRefreshAt...)
	g.AccessToken = token.AccessToken
	g.ExpiresAt = time.Now().Unix() + token.ExpiresIn
	g.Rejected = false
	if token.RefreshToken != "" {
		g.RefreshToken = token.RefreshToken
	}
	if token.IDToken != "" {
		g.IDToken = token.IDToken
	}
	if token.Scope != "" {
		g.Scope = token.Scope
	}
	saveCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if _, _, err = s.saveSIWCRecord(saveCtx, account, previousRegistration, previousGrant, r, g); err != nil {
		return "", "", err
	}
	if !openai.HasSIWCPermission(g.Scope) {
		return "", "", siwcDirectRequired()
	}
	return g.AccessToken, siwcGrantOwner(r), nil
}

// Stable across token rotations, changed by new authorization and unavailable
// after revocation. WS ownership must include this value, not a Codex token key.
func (s *OpenAIOAuthService) SIWCGrantIdentity(ctx context.Context, account *Account) (string, error) {
	_, _, r, g, err := s.readSIWCRecord(ctx, account)
	if err != nil {
		return "", err
	}
	if r == nil || g == nil {
		return "", siwcRequired()
	}
	if !openai.HasSIWCPermission(g.Scope) {
		return "", siwcDirectRequired()
	}
	return siwcGrantOwner(r), nil
}
func (s *OpenAIOAuthService) rejectSIWCAccessToken(ctx context.Context, account *Account, token string, cache OpenAITokenCache) error {
	unlock, err := s.lockSIWCCredentials(ctx, account.ID, cache, false)
	if err != nil {
		return err
	}
	if unlock == nil {
		return nil
	}
	defer unlock()
	previousRegistration, previousGrant, r, g, err := s.readSIWCRecord(ctx, account)
	if err != nil {
		return err
	}
	if g == nil || g.AccessToken != token || g.Rejected {
		return nil
	}
	g.Rejected = true
	_, _, err = s.saveSIWCRecord(ctx, account, previousRegistration, previousGrant, r, g)
	return err
}
func (s *OpenAIOAuthService) DeleteSIWCAuthorization(ctx context.Context, account *Account) (*OpenAISIWCRevokeResult, error) {
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return nil, authz.ErrPermissionDenied
	}
	lease := authz.CaptureLease(ctx)
	if err := authz.CheckLease(ctx, lease, "accounts.authorize"); err != nil {
		return nil, err
	}
	unlock, err := s.lockSIWCCredentials(ctx, account.ID, nil, true)
	if err != nil {
		return nil, err
	}
	defer unlock()
	previousRegistration, previousGrant, r, g, err := s.readSIWCRecord(ctx, account)
	if err != nil {
		return nil, err
	}
	result := &OpenAISIWCRevokeResult{Message: "Local SIWC authorization disconnected; registration retained"}
	if r == nil {
		return result, nil
	}
	if client, ok := s.oauthClient.(openAISIWCRevocationClient); ok && g != nil {
		if proxy, e := s.siwcProxy(ctx, account); e == nil {
			revokeCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
			result.RemoteRevocationConfirmed, _ = client.SIWCRevoke(revokeCtx, g.RefreshToken, g.AccessToken, r.ClientID, proxy)
			cancel()
		}
	}
	if err = authz.CheckLease(ctx, lease, "accounts.authorize"); err != nil {
		return nil, err
	}
	// Disconnect is also cancellation of pending browser authorization. Rotate
	// even before a first token exists, so any callback bound to the previous
	// encrypted registration snapshot fails before token exchange.
	r.Generation = uuid.NewString()
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, _, err = s.saveSIWCRecord(saveCtx, account, previousRegistration, previousGrant, r, nil)
	if err != nil {
		return nil, err
	}
	if g == nil {
		result.Message = "Local SIWC authorization disconnected and pending authorizations cancelled; registration retained"
	} else if result.RemoteRevocationConfirmed {
		result.Message = "SIWC authorization revoked remotely and disconnected locally; registration retained"
	} else {
		result.Message = "SIWC disconnected locally; remote revocation was not confirmed. Remove this app in ChatGPT connected apps to revoke any remaining authorization"
	}
	return result, nil
}

// SIWCAccessTokenWithIdentity returns a coherent credential/owner snapshot.
// Reading these independently could associate a newly rebound grant with the
// previous authorization's connection-local recovery history.
func (s *OpenAIOAuthService) SIWCAccessTokenWithIdentity(ctx context.Context, account *Account, cache OpenAITokenCache) (string, string, error) {
	return s.siwcAccessToken(ctx, account, cache)
}

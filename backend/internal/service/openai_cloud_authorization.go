package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

var (
	ErrOpenAICloudAuthorization = infraerrors.BadRequest("OPENAI_CLOUD_AUTHORIZATION_REQUIRED", "Cloud authorization is missing or changed; authorize this account again")
	ErrOpenAICloudSettings      = infraerrors.BadRequest("OPENAI_CLOUD_SETTINGS_INVALID", "Cloud requires an authorized ordinary OpenAI OAuth account, verified models, and BPS disabled")
	errOpenAICloudConflict      = infraerrors.Conflict("OPENAI_CLOUD_AUTHORIZATION_CHANGED", "Account or Cloud authorization changed; start authorization again")
)

// This table contains no OAuth token. Native credentials and TokenProvider are
// the only token/refresh authority; a separate refresh family is not assumed.
type OpenAICloudAuthorizationBinding struct {
	Revision        int64                              `json:"revision"`
	Generation      string                             `json:"generation"`
	AccountID       string                             `json:"account_id"`
	UserID          string                             `json:"user_id"`
	Subject         string                             `json:"subject"`
	Revoked         bool                               `json:"revoked"`
	RuntimeBlocked  bool                               `json:"runtime_blocked"`
	ProtocolVersion string                             `json:"protocol_version"`
	Models          map[string]OpenAICloudCapabilities `json:"models"`
	UpdatedAt       time.Time                          `json:"updated_at"`
}

// Only internal, real gateway probes may publish these facts. Neither model
// discovery nor callback JSON may grant a model scheduling eligibility.
type OpenAICloudCapabilities struct {
	Text             bool      `json:"text"`
	Tools            bool      `json:"tools"`
	Usage            bool      `json:"usage"`
	Settlement       bool      `json:"settlement"`
	ImageInput       bool      `json:"image_input"`
	EncryptedContext bool      `json:"encrypted_context"`
	WebSearch        bool      `json:"web_search"`
	ImageGeneration  bool      `json:"image_generation"`
	VerifiedAt       time.Time `json:"verified_at"`
}

func (c OpenAICloudCapabilities) Complete() bool {
	return c.Text && c.Tools && c.Usage && c.Settlement && c.ImageInput && c.EncryptedContext && c.WebSearch && c.ImageGeneration && !c.VerifiedAt.IsZero()
}

type OpenAICloudAuthorizationRepository interface {
	GetCloudAuthorization(context.Context, int64) (*OpenAICloudAuthorizationBinding, error)
	// credentials=nil leaves native credentials unchanged; a replacement is
	// atomic with the binding CAS and disables Cloud until capabilities pass.
	StoreCloudAuthorization(context.Context, *Account, string, *OpenAICloudAuthorizationBinding, map[string]any) (bool, error)
}

type OpenAICloudAccess struct {
	Account       *Account
	Authorization *OpenAICloudAuthorizationBinding
	AccessToken   string
	Generation    string
	Identity      string
}

type OpenAICloudStatus struct {
	Authorized     bool       `json:"authorized"`
	Enabled        bool       `json:"enabled"`
	Status         string     `json:"status"`
	VerifiedModels []string   `json:"verified_models"`
	Models         []string   `json:"models"`
	AccountID      string     `json:"account_id,omitempty"`
	UserID         string     `json:"user_id,omitempty"`
	Generation     string     `json:"generation,omitempty"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

type openAICloudOAuthSession struct {
	OAuth      openai.OAuthSession `json:"oauth"`
	AccountID  int64               `json:"account_id"`
	Workspace  string              `json:"workspace"`
	UserID     string              `json:"user_id"`
	Generation string              `json:"generation"`
	Subject    string              `json:"subject"`
}

type OpenAICloudService struct {
	accounts       AccountRepository
	repo           OpenAICloudAuthorizationRepository
	tokens         *OpenAITokenProvider
	oauth          *OpenAIOAuthService
	keys           *vendorOpenAIJWKSCache
	discoverModels func(context.Context, *OpenAICloudAccess) ([]string, error)
}

// SetModelDiscovery is wired once at startup to the gateway's verified Cloud
// transport. Candidate names are never capability proof.
func (s *OpenAICloudService) SetModelDiscovery(discover func(context.Context, *OpenAICloudAccess) ([]string, error)) {
	s.discoverModels = discover
}

func NewOpenAICloudService(accounts AccountRepository, tokens *OpenAITokenProvider, oauthService *OpenAIOAuthService) *OpenAICloudService {
	repo, _ := accounts.(OpenAICloudAuthorizationRepository)
	return &OpenAICloudService{accounts: accounts, repo: repo, tokens: tokens, oauth: oauthService, keys: vendorOpenAISigningKeys}
}

func (s *OpenAICloudService) account(ctx context.Context, id int64) (*Account, error) {
	if s == nil || s.accounts == nil || s.repo == nil {
		return nil, ErrOpenAICloudAuthorization
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !a.IsOpenAICloudEligible() || a.GetChatGPTAccountID() == "" || a.GetChatGPTUserID() == "" {
		return nil, ErrOpenAICloudAuthorization
	}
	return a, nil
}

func cloudBindingMatches(a *Account, b *OpenAICloudAuthorizationBinding) bool {
	return a != nil && b != nil && !b.Revoked && b.Subject != "" && b.Generation != "" &&
		a.IsOpenAICloudEligible() && a.GetChatGPTAccountID() == b.AccountID && a.GetChatGPTUserID() == b.UserID
}

func (s *OpenAICloudService) AuthorizationStatus(ctx context.Context, id int64) (*OpenAICloudStatus, error) {
	a, err := s.account(ctx, id)
	if err != nil {
		return nil, err
	}
	b, err := s.repo.GetCloudAuthorization(ctx, id)
	if err != nil {
		return nil, err
	}
	status := &OpenAICloudStatus{Status: "unauthorized", Models: []string{}, VerifiedModels: []string{}}
	if b == nil || b.Revoked {
		return status, nil
	}
	if !cloudBindingMatches(a, b) {
		status.Status = "reauthorization_required"
		return status, nil
	}
	status.Authorized, status.Status = true, "authorized"
	status.AccountID, status.UserID, status.Generation = b.AccountID, b.UserID, b.Generation
	if b.ProtocolVersion == OpenAICloudProtocolVersion {
		for model, capabilities := range b.Models {
			if capabilities.Complete() {
				status.VerifiedModels = append(status.VerifiedModels, model)
				if status.LastVerifiedAt == nil || capabilities.VerifiedAt.After(*status.LastVerifiedAt) {
					at := capabilities.VerifiedAt
					status.LastVerifiedAt = &at
				}
			}
		}
	}
	sort.Strings(status.VerifiedModels)
	if len(status.VerifiedModels) > 0 {
		status.Status = "verified"
	}
	status.Enabled = a.IsOpenAICloudEnabled() && cloudModelsVerified(a, b)
	if b.RuntimeBlocked {
		status.LastError = "Cloud settlement is unresolved; operator reconciliation is required"
	}
	if s.discoverModels != nil {
		access, err := s.GetAccessToken(ctx, id)
		if err == nil {
			var candidates []string
			candidates, err = s.discoverModels(ctx, access)
			seen := map[string]bool{}
			for _, model := range candidates {
				if model != "" && len(model) <= 200 && strings.TrimSpace(model) == model && !seen[model] && len(status.Models) < 64 {
					status.Models = append(status.Models, model)
					seen[model] = true
				}
			}
			sort.Strings(status.Models)
		}
		if err != nil && status.LastError == "" {
			status.LastError = "Cloud model discovery is unavailable; retry the account test"
		}
	}
	return status, nil
}

func cloudModelsVerified(a *Account, b *OpenAICloudAuthorizationBinding) bool {
	if !cloudBindingMatches(a, b) || b.RuntimeBlocked || b.ProtocolVersion != OpenAICloudProtocolVersion || len(a.OpenAICloudModels()) == 0 {
		return false
	}
	for _, model := range a.OpenAICloudModels() {
		if !b.Models[model].Complete() {
			return false
		}
	}
	return true
}

func (s *OpenAICloudService) GetAccessToken(ctx context.Context, id int64) (*OpenAICloudAccess, error) {
	a, err := s.account(ctx, id)
	if err != nil {
		return nil, err
	}
	b, err := s.repo.GetCloudAuthorization(ctx, id)
	if err != nil {
		return nil, err
	}
	if !cloudBindingMatches(a, b) || s.tokens == nil {
		return nil, ErrOpenAICloudAuthorization
	}
	token, err := s.tokens.GetAccessToken(ctx, a)
	if err != nil {
		return nil, err
	}
	// A cached AT cannot establish identity: verify the signature and binding,
	// including after a normal native refresh or a concurrent account edit.
	at, err := s.verifyAccess(ctx, token, cloudAccountProxy(a))
	if err != nil || at.Subject != b.Subject || at.Auth.ChatGPTAccountID != b.AccountID {
		return nil, ErrOpenAICloudAuthorization
	}
	user, _ := vendorOpenAIClaimUser(at.Auth)
	if user != b.UserID {
		return nil, ErrOpenAICloudAuthorization
	}
	fresh, err := s.account(ctx, id)
	if err != nil {
		return nil, err
	}
	current, err := s.repo.GetCloudAuthorization(ctx, id)
	if err != nil {
		return nil, err
	}
	if !cloudBindingMatches(fresh, current) || current.Generation != b.Generation {
		return nil, ErrOpenAICloudAuthorization
	}
	return &OpenAICloudAccess{Account: fresh, Authorization: current, AccessToken: token, Generation: b.Generation, Identity: b.AccountID + "/" + b.UserID}, nil
}

func cloudAccountProxy(a *Account) string {
	if a != nil && a.Proxy != nil {
		return a.Proxy.URL()
	}
	return ""
}

func (s *OpenAICloudService) verifyAccess(ctx context.Context, token, proxy string) (*vendorOpenAISignedClaims, error) {
	ctx, cancel := context.WithTimeout(ctx, vendorOpenAIVerifyTimeout)
	defer cancel()
	at, err := s.keys.verifyForClient(ctx, token, proxy, vendorOpenAIAccessAudience, openai.ClientID, true)
	if err != nil || at.Auth == nil || !vendorOpenAIIdentityValue(at.Auth.ChatGPTAccountID) || at.Auth.ChatGPTAccountID == "default" {
		return nil, ErrOpenAICloudAuthorization
	}
	user, ok := vendorOpenAIClaimUser(at.Auth)
	if !ok || user == "" {
		return nil, ErrOpenAICloudAuthorization
	}
	return at, nil
}

func (s *OpenAICloudService) GenerateAuthURL(ctx context.Context, id int64) (*OpenAIAuthURLResult, error) {
	a, err := s.account(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.oauth == nil || s.oauth.rdb == nil {
		return nil, ErrOpenAICloudAuthorization
	}
	lease := authz.CaptureLease(ctx)
	if err := authz.CheckLease(ctx, lease, "accounts.authorize"); err != nil {
		return nil, err
	}
	if s.tokens == nil {
		return nil, ErrOpenAICloudAuthorization
	}
	token, err := s.tokens.GetAccessToken(ctx, a)
	if err != nil {
		return nil, err
	}
	identity, err := s.verifyAccess(ctx, token, cloudAccountProxy(a))
	if err != nil {
		return nil, err
	}
	identityUser, _ := vendorOpenAIClaimUser(identity.Auth)
	a, err = s.account(ctx, id)
	if err != nil {
		return nil, err
	}
	if identity.Auth.ChatGPTAccountID != a.GetChatGPTAccountID() || identityUser != a.GetChatGPTUserID() {
		return nil, ErrOpenAICloudAuthorization
	}
	b, err := s.repo.GetCloudAuthorization(ctx, id)
	if err != nil {
		return nil, err
	}
	if b == nil {
		generation, err := openai.GenerateSessionID()
		if err != nil {
			return nil, err
		}
		b = &OpenAICloudAuthorizationBinding{Generation: generation, AccountID: a.GetChatGPTAccountID(), UserID: a.GetChatGPTUserID(), Revoked: true, Models: map[string]OpenAICloudCapabilities{}, ProtocolVersion: OpenAICloudProtocolVersion}
		ok, err := s.repo.StoreCloudAuthorization(ctx, a, "", b, nil)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errOpenAICloudConflict
		}
	}
	state, err := openai.GenerateState()
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
	session := &openAICloudOAuthSession{AccountID: id, Workspace: a.GetChatGPTAccountID(), UserID: a.GetChatGPTUserID(), Subject: identity.Subject, Generation: b.Generation, OAuth: openai.OAuthSession{Authorization: lease, State: state, CodeVerifier: verifier, ClientID: openai.ClientID, RedirectURI: openai.DefaultRedirectURI, ProxyURL: cloudAccountProxy(a), CreatedAt: time.Now()}}
	if err := oauth.PutSharedSession(ctx, s.oauth.rdb, "oauth:openai:cloud:"+sessionID, session, openai.SessionTTL); err != nil {
		return nil, err
	}
	return &OpenAIAuthURLResult{AuthURL: openai.BuildAuthorizationURLForPlatform(state, openai.GenerateCodeChallenge(verifier), openai.DefaultRedirectURI, PlatformOpenAI), SessionID: sessionID}, nil
}

func (s *OpenAICloudService) CompleteAuthorization(ctx context.Context, id int64, input *OpenAIExchangeCodeInput) (*OpenAICloudStatus, error) {
	if s == nil || s.oauth == nil || s.oauth.rdb == nil || input == nil || input.SessionID == "" || len(input.SessionID) > 256 || strings.TrimSpace(input.Code) == "" || len(input.Code) > 16384 {
		return nil, ErrOpenAICloudAuthorization
	}
	session, raw, err := oauth.ReadSharedSession[openAICloudOAuthSession](ctx, s.oauth.rdb, "oauth:openai:cloud:"+input.SessionID)
	if err != nil || session.AccountID != id || input.State == "" || subtle.ConstantTimeCompare([]byte(input.State), []byte(session.OAuth.State)) != 1 {
		return nil, ErrOpenAICloudAuthorization
	}
	if err := authz.CheckLease(ctx, session.OAuth.Authorization, "accounts.authorize"); err != nil {
		return nil, err
	}
	a, err := s.account(ctx, id)
	if err != nil {
		return nil, err
	}
	unlock, err := s.lockNativeAuthority(ctx, a)
	if err != nil {
		return nil, err
	}
	release := sync.OnceFunc(unlock)
	defer release()
	// Bound the exchange/persistence inside the distributed refresh lease.
	ctx, cancel := context.WithTimeout(ctx, min(45*time.Second, s.tokens.refreshAPI.lockTTL-5*time.Second))
	defer cancel()
	a, err = s.account(ctx, id)
	if err != nil {
		return nil, err
	}
	b, err := s.repo.GetCloudAuthorization(ctx, id)
	if err != nil {
		return nil, err
	}
	if b == nil || b.Generation != session.Generation || a.GetChatGPTAccountID() != session.Workspace || a.GetChatGPTUserID() != session.UserID || cloudAccountProxy(a) != session.OAuth.ProxyURL {
		return nil, errOpenAICloudConflict
	}
	if _, err := oauth.CompareSwapSession(ctx, s.oauth.rdb, "oauth:openai:cloud:"+input.SessionID, raw, nil); err != nil {
		return nil, errOpenAICloudConflict
	}
	tokens, err := s.oauth.oauthClient.ExchangeCode(ctx, input.Code, session.OAuth.CodeVerifier, session.OAuth.RedirectURI, session.OAuth.ProxyURL, openai.ClientID)
	if err != nil {
		return nil, infraerrors.BadRequest("OPENAI_CLOUD_EXCHANGE_FAILED", "Cloud login could not be completed; start authorization again")
	}
	info, subject, err := s.verifyAuthorization(ctx, tokens, session.OAuth.ProxyURL)
	if err != nil {
		return nil, err
	}
	if info.ChatGPTAccountID != session.Workspace || info.ChatGPTUserID != session.UserID || subject != session.Subject {
		return nil, ErrOpenAICloudAuthorization
	}
	if err := authz.CheckLease(ctx, session.OAuth.Authorization, "accounts.authorize"); err != nil {
		return nil, err
	}
	generation, err := openai.GenerateSessionID()
	if err != nil {
		return nil, err
	}
	next := &OpenAICloudAuthorizationBinding{Revision: b.Revision, Generation: generation, AccountID: info.ChatGPTAccountID, UserID: info.ChatGPTUserID, Subject: subject, RuntimeBlocked: b.RuntimeBlocked, Models: map[string]OpenAICloudCapabilities{}, ProtocolVersion: OpenAICloudProtocolVersion}
	credentials := make(map[string]any, len(a.Credentials)+8)
	for key, value := range a.Credentials {
		credentials[key] = value
	}
	for key, value := range s.oauth.BuildAccountCredentials(info) {
		credentials[key] = value
	}
	credentials["_token_version"] = time.Now().UnixMilli()
	ok, err := s.repo.StoreCloudAuthorization(ctx, a, session.Generation, next, credentials)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errOpenAICloudConflict
	}
	// Cache deletion is an optimization; access above verifies returned identity.
	if s.tokens.tokenCache != nil {
		_ = s.tokens.tokenCache.DeleteAccessToken(ctx, OpenAITokenCacheKey(a))
	}
	// Model discovery can encounter a resource-server 401 and enter the native
	// refresh authority again. Commit and invalidate first, then release its
	// lock before any status/discovery work to avoid waiting on our own lease.
	release()
	return s.AuthorizationStatus(ctx, id)
}

func (s *OpenAICloudService) verifyAuthorization(ctx context.Context, tokens *openai.TokenResponse, proxy string) (*OpenAITokenInfo, string, error) {
	ctx, cancel := context.WithTimeout(ctx, vendorOpenAIVerifyTimeout)
	defer cancel()
	if tokens == nil || tokens.RefreshToken == "" || tokens.IDToken == "" || len(tokens.RefreshToken) > vendorOpenAITokenLimit || (tokens.TokenType != "" && !strings.EqualFold(tokens.TokenType, "Bearer")) {
		return nil, "", ErrOpenAICloudAuthorization
	}
	at, err := s.verifyAccess(ctx, tokens.AccessToken, proxy)
	if err != nil {
		return nil, "", err
	}
	id, err := s.keys.verifyForClient(ctx, tokens.IDToken, proxy, openai.ClientID, openai.ClientID, false)
	if err != nil || id.Subject != at.Subject {
		return nil, "", ErrOpenAICloudAuthorization
	}
	user, _ := vendorOpenAIClaimUser(at.Auth)
	if id.Auth != nil {
		idUser, ok := vendorOpenAIClaimUser(id.Auth)
		if !ok || (idUser != "" && idUser != user) || (id.Auth.ChatGPTAccountID != "" && id.Auth.ChatGPTAccountID != at.Auth.ChatGPTAccountID) {
			return nil, "", ErrOpenAICloudAuthorization
		}
	}
	if id.AccessHash != "" {
		sum := sha256.Sum256([]byte(tokens.AccessToken))
		want := base64.RawURLEncoding.EncodeToString(sum[:sha256.Size/2])
		if subtle.ConstantTimeCompare([]byte(id.AccessHash), []byte(want)) != 1 {
			return nil, "", ErrOpenAICloudAuthorization
		}
	}
	return &OpenAITokenInfo{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, IDToken: tokens.IDToken, ClientID: openai.ClientID, ExpiresAt: at.ExpiresAt.Unix(), Email: id.Email, ChatGPTAccountID: at.Auth.ChatGPTAccountID, ChatGPTUserID: user, PlanType: at.Auth.ChatGPTPlanType}, at.Subject, nil
}

func (s *OpenAICloudService) lockNativeAuthority(ctx context.Context, a *Account) (func(), error) {
	if s.tokens == nil || s.tokens.refreshAPI == nil || s.tokens.executor == nil || s.tokens.refreshAPI.lockTTL <= 5*time.Second {
		return nil, ErrOpenAICloudAuthorization
	}
	api := s.tokens.refreshAPI
	key := s.tokens.executor.CacheKey(a)
	mu := api.getLocalLock(key)
	if err := mu.Lock(ctx); err != nil {
		return nil, err
	}
	if api.tokenCache != nil {
		ok, err := api.tokenCache.AcquireRefreshLock(ctx, key, api.lockTTL)
		if err != nil || !ok {
			mu.Unlock()
			return nil, infraerrors.Conflict("OPENAI_CLOUD_REFRESH_BUSY", "Native authorization is refreshing; retry shortly")
		}
		return func() { defer mu.Unlock(); api.releaseRefreshLock(ctx, key) }, nil
	}
	mu.Unlock()
	return nil, ErrOpenAICloudAuthorization
}

func (s *OpenAICloudService) Disconnect(ctx context.Context, id int64) error {
	a, err := s.account(ctx, id)
	if err != nil {
		return err
	}
	if err := authz.CheckLease(ctx, authz.CaptureLease(ctx), "accounts.authorize"); err != nil {
		return err
	}
	b, err := s.repo.GetCloudAuthorization(ctx, id)
	if err != nil {
		return err
	}
	previous := ""
	if b != nil {
		previous = b.Generation
	}
	generation, err := openai.GenerateSessionID()
	if err != nil {
		return err
	}
	next := &OpenAICloudAuthorizationBinding{Generation: generation, AccountID: a.GetChatGPTAccountID(), UserID: a.GetChatGPTUserID(), Revoked: true, Models: map[string]OpenAICloudCapabilities{}, ProtocolVersion: OpenAICloudProtocolVersion}
	if b != nil {
		next.Revision = b.Revision
		next.RuntimeBlocked = b.RuntimeBlocked
	}
	ok, err := s.repo.StoreCloudAuthorization(ctx, a, previous, next, nil)
	if err != nil {
		return err
	}
	if !ok {
		return errOpenAICloudConflict
	}
	return nil
}

func (s *OpenAICloudService) RecordVerifiedModel(ctx context.Context, id int64, generation, model string, capabilities OpenAICloudCapabilities) error {
	capabilities.VerifiedAt = time.Now().UTC()
	if model == "" || len(model) > 200 || strings.TrimSpace(model) != model || !capabilities.Complete() {
		return errors.New("Cloud model has not passed the complete gateway verification")
	}
	a, err := s.account(ctx, id)
	if err != nil {
		return err
	}
	b, err := s.repo.GetCloudAuthorization(ctx, id)
	if err != nil {
		return err
	}
	if !cloudBindingMatches(a, b) || b.Generation != generation {
		return errOpenAICloudConflict
	}
	if b.RuntimeBlocked {
		return infraerrors.Conflict("OPENAI_CLOUD_SETTLEMENT_UNRESOLVED", "Cloud settlement must be reconciled before enabling this account")
	}
	if b.ProtocolVersion != OpenAICloudProtocolVersion {
		b.Models = map[string]OpenAICloudCapabilities{}
	}
	if b.Models == nil {
		b.Models = map[string]OpenAICloudCapabilities{}
	}
	if len(b.Models) >= 64 {
		if _, exists := b.Models[model]; !exists {
			return ErrOpenAICloudSettings
		}
	}
	b.Models[model] = capabilities
	b.ProtocolVersion = OpenAICloudProtocolVersion
	ok, err := s.repo.StoreCloudAuthorization(ctx, a, generation, b, nil)
	if err != nil {
		return err
	}
	if !ok {
		return errOpenAICloudConflict
	}
	return nil
}

// Unknown usage cannot be treated as zero or cleared by reauthorization or an
// unrelated successful probe. Keep this durable block until explicit, trusted
// reconciliation is implemented; no browser API can clear it.
func (s *OpenAICloudService) BlockRuntime(ctx context.Context, id int64, generation string, _ string) error {
	for attempt := 0; attempt < 3; attempt++ {
		a, err := s.account(ctx, id)
		if err != nil {
			return err
		}
		b, err := s.repo.GetCloudAuthorization(ctx, id)
		if err != nil {
			return err
		}
		if b == nil || b.Generation != generation {
			return errOpenAICloudConflict
		}
		if b.RuntimeBlocked {
			return nil
		}
		b.RuntimeBlocked = true
		b.Models = map[string]OpenAICloudCapabilities{}
		ok, err := s.repo.StoreCloudAuthorization(ctx, a, generation, b, nil)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return errOpenAICloudConflict
}

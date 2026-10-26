package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

func cloudOwnerValidationFailure(err error) error {
	if errors.Is(err, errCloudAuthorizationChanged) {
		return errCloudAuthorizationChanged
	}
	return errCloudStateUnavailable
}

var (
	ErrOpenAICloudAuthorization   = infraerrors.BadRequest("OPENAI_CLOUD_AUTHORIZATION_REQUIRED", "Native Codex authorization is unavailable or changed; reconnect the native account")
	ErrOpenAICloudSettings        = infraerrors.BadRequest("OPENAI_CLOUD_SETTINGS_INVALID", "Cloud requests require an enabled native OAuth account and a model allowed by account and group settings")
	ErrOpenAICloudAccountSettings = infraerrors.BadRequest("OPENAI_CLOUD_ACCOUNT_SETTINGS_INVALID", "Cloud can be enabled only for an ordinary OpenAI account with native Codex OAuth identity; check the account type and native authorization")
	errOpenAICloudConflict        = infraerrors.Conflict("OPENAI_CLOUD_AUTHORIZATION_CHANGED", "Native account identity changed concurrently; retry the request")
	// An unavailable state dependency is not proof of revoked credentials. Keep
	// the public classification fixed; repository errors may contain private data.
	errCloudStateUnavailable = &cloudGatewayError{http.StatusServiceUnavailable, "cloud_state_unavailable", "Cloud account authorization state is unavailable; the request was not replayed."}
)

// This table contains no OAuth token. Native credentials and TokenProvider are
// the only token/refresh authority; a separate refresh family is not assumed.
type OpenAICloudAuthorizationBinding struct {
	Revision   int64     `json:"revision"`
	Generation string    `json:"generation"`
	AccountID  string    `json:"account_id"`
	UserID     string    `json:"user_id"`
	Subject    string    `json:"subject"`
	Revoked    bool      `json:"revoked"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type OpenAICloudAuthorizationRepository interface {
	GetCloudAuthorization(context.Context, int64) (*OpenAICloudAuthorizationBinding, error)
	// Binding writes never replace native credentials or enable Cloud.
	StoreCloudAuthorization(context.Context, *Account, string, *OpenAICloudAuthorizationBinding) (bool, error)
}

type OpenAICloudAccess struct {
	Account       *Account
	Authorization *OpenAICloudAuthorizationBinding
	AccessToken   string
	Generation    string
	Identity      string
}

type OpenAICloudService struct {
	gateway        *OpenAIGatewayService
	accounts       AccountRepository
	repo           OpenAICloudAuthorizationRepository
	tokens         *OpenAITokenProvider
	oauth          *OpenAIOAuthService
	keys           *vendorOpenAIJWKSCache
	discoverModels func(context.Context, *OpenAICloudAccess) ([]string, error)
}

// SetModelDiscovery is wired once at startup to the gateway's verified Cloud
// transport. Catalog discovery is separate from authorization and admission.
func (s *OpenAICloudService) SetModelDiscovery(discover func(context.Context, *OpenAICloudAccess) ([]string, error)) {
	s.discoverModels = discover
}

func NewOpenAICloudService(accounts AccountRepository, tokens *OpenAITokenProvider, oauthService *OpenAIOAuthService) *OpenAICloudService {
	repo, _ := accounts.(OpenAICloudAuthorizationRepository)
	return &OpenAICloudService{accounts: accounts, repo: repo, tokens: tokens, oauth: oauthService, keys: vendorOpenAISigningKeys}
}

// State-service failures cannot invalidate credentials or penalize an account.
// Preserve caller cancellation while keeping dependency details private.
func cloudStateDependencyError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errCloudStateUnavailable
}

func (s *OpenAICloudService) account(ctx context.Context, id int64) (*Account, error) {
	if s == nil || s.accounts == nil || s.repo == nil {
		return nil, ErrOpenAICloudAuthorization
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil, ErrOpenAICloudAuthorization
		}
		return nil, cloudStateDependencyError(err)
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

// Native TokenProvider remains the only credential/refresh authority. The
// private binding fences active sessions and settlement; it stores no token.
// Establish it lazily from a signed native identity, without changing switches.
func (s *OpenAICloudService) GetAccessToken(ctx context.Context, id int64) (*OpenAICloudAccess, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a, err := s.account(ctx, id)
		if err != nil {
			return nil, err
		}
		if s.tokens == nil {
			return nil, ErrOpenAICloudAuthorization
		}
		token, err := s.tokens.GetAccessToken(ctx, a)
		if err != nil {
			return nil, err
		}
		at, err := s.verifyAccess(ctx, token, cloudAccountProxy(a))
		if err != nil {
			return nil, err
		}
		user, _ := vendorOpenAIClaimUser(at.Auth)
		fresh, err := s.account(ctx, id)
		if err != nil {
			return nil, err
		}
		if at.Auth.ChatGPTAccountID != fresh.GetChatGPTAccountID() || user != fresh.GetChatGPTUserID() {
			return nil, ErrOpenAICloudAuthorization
		}
		b, err := s.repo.GetCloudAuthorization(ctx, id)
		if err != nil {
			return nil, cloudStateDependencyError(err)
		}
		if !cloudBindingMatches(fresh, b) || b.Subject != at.Subject {
			generation, err := openai.GenerateSessionID()
			if err != nil {
				return nil, err
			}
			next := &OpenAICloudAuthorizationBinding{Generation: generation, AccountID: fresh.GetChatGPTAccountID(), UserID: user, Subject: at.Subject}
			previous := ""
			if b != nil {
				previous, next.Revision = b.Generation, b.Revision
			}
			if _, err := s.repo.StoreCloudAuthorization(ctx, fresh, previous, next); err != nil {
				return nil, cloudStateDependencyError(err)
			}
			continue // Read the CAS winner, including concurrent first-use requests.
		}
		currentAccount, err := s.account(ctx, id)
		if err != nil {
			return nil, err
		}
		current, err := s.repo.GetCloudAuthorization(ctx, id)
		if err != nil {
			return nil, cloudStateDependencyError(err)
		}
		if !cloudBindingMatches(currentAccount, current) || current.Generation != b.Generation || current.Subject != at.Subject {
			return nil, ErrOpenAICloudAuthorization
		}
		return &OpenAICloudAccess{Account: currentAccount, Authorization: current, AccessToken: token, Generation: current.Generation, Identity: current.AccountID + "/" + current.UserID}, nil
	}
	return nil, errOpenAICloudConflict
}

func cloudAccountProxy(a *Account) string {
	if a != nil && a.Proxy != nil {
		return a.Proxy.URL()
	}
	return ""
}

func (s *OpenAICloudService) verifyAccess(ctx context.Context, token, proxy string) (*vendorOpenAISignedClaims, error) {
	caller := ctx
	ctx, cancel := context.WithTimeout(ctx, vendorOpenAIVerifyTimeout)
	defer cancel()
	at, err := s.keys.verifyForClient(ctx, token, proxy, vendorOpenAIAccessAudience, openai.ClientID, true)
	if caller.Err() != nil {
		return nil, caller.Err()
	}
	if errors.Is(err, errVendorOpenAIKeySourceUnavailable) {
		return nil, errCloudStateUnavailable
	}
	if err != nil || at == nil || at.Auth == nil || !vendorOpenAIIdentityValue(at.Auth.ChatGPTAccountID) || at.Auth.ChatGPTAccountID == "default" {
		return nil, ErrOpenAICloudAuthorization
	}
	user, ok := vendorOpenAIClaimUser(at.Auth)
	if !ok || user == "" {
		return nil, ErrOpenAICloudAuthorization
	}
	return at, nil
}

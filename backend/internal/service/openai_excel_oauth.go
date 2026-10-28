package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/google/uuid"
)

// Private encrypted storage is separate from the account's native Codex grant.
type OpenAIExcelCredentialRepository interface {
	GetExcelCredentials(context.Context, int64) (string, error)
	StoreExcelCredentials(context.Context, *Account, string, string) (bool, error)
}

// Lists need only grant presence, never credential decryption or N+1 reads.
type OpenAIExcelAuthorizationPresenceRepository interface {
	ExcelAuthorizationPresence(context.Context, []int64) (map[int64]bool, error)
}

// AccountAuthorizationModes projects stored grant presence for already-authorized
// accounts. It does not grant dispatch eligibility or interpret routing switches.
// Storage failures return no modes so callers can omit presentation metadata.
func (s *OpenAIOAuthService) AccountAuthorizationModes(ctx context.Context, accounts []*Account) (map[int64][]string, error) {
	result := make(map[int64][]string)
	ids := make([]int64, 0, len(accounts))
	for _, a := range accounts {
		if a == nil || !a.IsOpenAIOAuth() || a.ParentAccountID != nil {
			continue
		}
		result[a.ID] = []string{}
		ids = append(ids, a.ID)
		if a.GetCredential("access_token") != "" {
			result[a.ID] = append(result[a.ID], vendorAccountPrimaryMode(a))
		}
	}
	if len(ids) == 0 || s == nil {
		return result, nil
	}
	if repo, ok := s.excelAccountRepo.(OpenAIExcelAuthorizationPresenceRepository); ok {
		present, err := repo.ExcelAuthorizationPresence(ctx, ids)
		if err != nil {
			return nil, err
		}
		for id, modes := range result {
			if present[id] && !(len(modes) == 1 && modes[0] == VendorOpenAIModeBPS) {
				result[id] = append(modes, VendorOpenAIModeBPS)
			}
		}
	}
	return result, nil
}

func excelMapString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}
func excelFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
func (a *Account) IsExcelOAuth() bool {
	return a != nil && a.IsOpenAIOAuth() && a.GetCredential("client_id") == openai.ExcelClientID
}
func IsExcelAuthorizationError(err error) bool {
	return strings.HasPrefix(infraerrors.Reason(err), "OPENAI_EXCEL_AUTH_")
}
func excelAuthRequired() error {
	return infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_REQUIRED", "Excel authorization is required for BPS; authorize or import an Excel grant in account settings")
}

func excelGrantStorageUnavailable() error {
	return infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_STORAGE_UNAVAILABLE", "Excel authorization requires private credential storage and a persistent TOTP_ENCRYPTION_KEY")
}

func excelPrimaryGrantManaged() error {
	return infraerrors.BadRequest("OPENAI_EXCEL_AUTH_PRIMARY", "This account uses its primary Excel authorization; manage it with the account's existing OAuth credentials")
}
func validateExcelIdentity(account *Account, info *OpenAITokenInfo) error {
	if account == nil || info == nil || !account.IsExcelBPSEligible() || account.GetChatGPTAccountID() == "" || info.ChatGPTAccountID != account.GetChatGPTAccountID() {
		return errors.New("Excel workspace does not match this account")
	}
	if user := account.GetChatGPTUserID(); user != "" && info.ChatGPTUserID != user {
		return errors.New("Excel user does not match this account")
	}
	if email := account.GetCredential("email"); email != "" && !strings.EqualFold(email, info.Email) {
		return errors.New("Excel email does not match this account")
	}
	return nil
}
func excelOAuthTokenInfo(credentials map[string]any) (*OpenAITokenInfo, error) {
	return decodeExcelOAuthTokenInfo(credentials, false)
}

func decodeExcelOAuthTokenInfo(credentials map[string]any, allowExpired bool) (*OpenAITokenInfo, error) {
	if excelMapString(credentials, "client_id") != openai.ExcelClientID || excelMapString(credentials, "refresh_token") == "" {
		return nil, errors.New("excel OAuth client or refresh token is missing")
	}
	access := excelMapString(credentials, "access_token")
	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid Excel access token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid Excel access token")
	}
	var client struct {
		ClientID string `json:"client_id"`
		AZP      string `json:"azp"`
	}
	if json.Unmarshal(payload, &client) != nil || excelFirstNonEmpty(client.ClientID, client.AZP) != openai.ExcelClientID {
		return nil, errors.New("excel access token client mismatch")
	}
	at, err := openai.DecodeIDToken(access)
	if err != nil || at.Exp <= 0 || (!allowExpired && at.Exp <= time.Now().Unix()) || at.OpenAIAuth == nil || at.OpenAIAuth.ChatGPTAccountID == "" {
		return nil, errors.New("excel access token identity or expiry is missing")
	}
	id, err := openai.DecodeIDToken(excelMapString(credentials, "id_token"))
	if err != nil {
		return nil, errors.New("invalid Excel identity token")
	}
	audienceOK := false
	for _, aud := range id.Aud {
		audienceOK = audienceOK || aud == openai.ExcelClientID
	}
	if !audienceOK {
		return nil, errors.New("excel identity token client mismatch")
	}
	if id.OpenAIAuth != nil {
		if id.OpenAIAuth.ChatGPTAccountID != "" && id.OpenAIAuth.ChatGPTAccountID != at.OpenAIAuth.ChatGPTAccountID {
			return nil, errors.New("Excel token workspace mismatch")
		}
		atUser := excelFirstNonEmpty(at.OpenAIAuth.ChatGPTUserID, at.OpenAIAuth.UserID)
		idUser := excelFirstNonEmpty(id.OpenAIAuth.ChatGPTUserID, id.OpenAIAuth.UserID)
		if atUser != "" && idUser != "" && atUser != idUser {
			return nil, errors.New("Excel token user mismatch")
		}
	}
	if workspace := excelMapString(credentials, "chatgpt_account_id"); workspace != "" && workspace != at.OpenAIAuth.ChatGPTAccountID {
		return nil, errors.New("excel workspace mismatch")
	}
	return &OpenAITokenInfo{AccessToken: access, RefreshToken: excelMapString(credentials, "refresh_token"),
		IDToken: excelMapString(credentials, "id_token"), ClientID: openai.ExcelClientID, ExpiresAt: at.Exp,
		Email: id.Email, ChatGPTAccountID: at.OpenAIAuth.ChatGPTAccountID,
		ChatGPTUserID: excelFirstNonEmpty(at.OpenAIAuth.ChatGPTUserID, at.OpenAIAuth.UserID, id.GetUserInfo().ChatGPTUserID)}, nil
}

// This decoder is exclusively for private grants read from authenticated
// encrypted storage. Supplier writers must have verified the provider signature
// and saved the canonical identity first. It is never an import validator:
// ImportExcelCredentials continues to require its original RT and ID Token.
func decodeStoredExcelOAuthTokenInfo(account *Account, credentials map[string]any, allowExpired bool) (*OpenAITokenInfo, error) {
	if account == nil || account.VendorID == nil {
		stored := credentials
		if excelMapString(credentials, "refresh_token") == "" {
			// Import still requires an RT. A previously authenticated saved
			// grant can have an access-only lifetime; expiry/rejection below
			// reports the precise reauthorization fact rather than a decode
			// dependency failure.
			stored = cloneExcelBPSRecoveryExtra(credentials)
			stored["refresh_token"] = "stored-access-only"
		}
		info, err := decodeExcelOAuthTokenInfo(stored, allowExpired)
		if err == nil {
			info.RefreshToken = excelMapString(credentials, "refresh_token")
			err = validateExcelIdentity(account, info)
		}
		return info, err
	}
	access := excelMapString(credentials, "access_token")
	if *account.VendorID <= 0 || excelMapString(credentials, "client_id") != openai.ExcelClientID || len(access) > vendorOpenAITokenLimit {
		return nil, errors.New("invalid saved supplier Excel grant")
	}
	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid saved supplier Excel access token")
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	var at vendorOpenAISignedClaims
	if err != nil || json.Unmarshal(payload, &at) != nil || at.Auth == nil || at.ExpiresAt == nil || at.ExpiresAt.Unix() <= 0 ||
		at.Issuer != vendorOpenAIIssuer || len(at.Audience) != 1 || at.Audience[0] != vendorOpenAIAccessAudience ||
		(at.ClientID != "" && at.ClientID != openai.ExcelClientID) || (at.AZP != "" && at.AZP != openai.ExcelClientID) ||
		(at.ClientID == "" && at.AZP == "") || !vendorOpenAIIdentityValue(at.Subject) || at.IssuedAt == nil ||
		!at.ExpiresAt.After(at.IssuedAt.Time) || at.IssuedAt.After(time.Now()) ||
		(at.NotBefore != nil && at.NotBefore.After(time.Now())) || (!allowExpired && !at.ExpiresAt.After(time.Now())) {
		return nil, errors.New("invalid saved supplier Excel claims")
	}
	user, valid := vendorOpenAIClaimUser(at.Auth)
	workspace := at.Auth.ChatGPTAccountID
	if !valid || user == "" || !vendorOpenAIIdentityValue(workspace) || workspace == "default" ||
		workspace != excelMapString(credentials, "chatgpt_account_id") || user != excelMapString(credentials, "chatgpt_user_id") {
		return nil, errors.New("saved supplier Excel identity mismatch")
	}
	// A retained ID Token may describe an earlier refresh (including its old
	// expiry and at_hash). The saved verified identity, not that historical token,
	// is authoritative here. Missing display email can use the same account's
	// existing metadata; ownership still requires exact workspace/user matching.
	info := &OpenAITokenInfo{
		AccessToken: access, RefreshToken: excelMapString(credentials, "refresh_token"),
		IDToken: excelMapString(credentials, "id_token"), ClientID: openai.ExcelClientID, ExpiresAt: at.ExpiresAt.Unix(),
		ChatGPTAccountID: workspace, ChatGPTUserID: user, PlanType: excelMapString(credentials, "plan_type"),
		Email: excelFirstNonEmpty(excelMapString(credentials, "email"), account.GetCredential("email")),
	}
	if err := validateExcelIdentity(account, info); err != nil {
		return nil, err
	}
	return info, nil
}

func excelGrantReauthorizationRequired() error {
	return infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_REAUTHORIZE_REQUIRED", "Excel authorization has expired or was rejected and has no refresh token; reauthorize BPS")
}

var excelOAuthRefreshLocks sync.Map

// Stored inside the encrypted grant. A resource-server 401 rejects the access
// token, not necessarily the refresh token; refresh it on the next token read.
const excelAccessTokenRejectedKey = "_access_token_rejected"

// Read-only legacy health marker; new failures use ExcelBPSFailureKey.
const ExcelBPSAuthorizationErrorKey = "openai_excel_bps_authorization_error"
const excelAccessTokenRefreshedKey = "_access_token_refreshed_after_rejection"
const excelGrantIdentityKey = "_grant_identity"

// Both the refresh writer and 401 marker use the same local/distributed lock.
// A 401 never waits behind a refresh or modifies its snapshot: the refresh
// already owns recovery, and a failed attempt remains due for the next request.
func lockExcelCredentials(ctx context.Context, id int64, tokenCache OpenAITokenCache, wait bool) (func(), error) {
	key := fmt.Sprintf("openai:excel:account:%d", id)
	actual, _ := excelOAuthRefreshLocks.LoadOrStore(key, newContextMutex())
	mu, ok := actual.(*contextMutex)
	if !ok {
		return nil, errors.New("excel refresh lock is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if wait {
		if err := mu.Lock(ctx); err != nil {
			return nil, err
		}
	} else {
		select {
		case mu.token <- struct{}{}:
		default:
			return nil, nil // Refresh owns the grant; do not change its CAS snapshot.
		}
	}
	// Fail closed on distributed-lock errors: Excel refresh tokens rotate.
	if tokenCache != nil {
		acquired, err := tokenCache.AcquireRefreshLock(ctx, key, 60*time.Second)
		if err != nil || !acquired {
			mu.Unlock()
			if err == nil && !wait {
				return nil, nil
			}
			return nil, errors.New("excel credential refresh is busy or unavailable")
		}
		return func() {
			defer mu.Unlock()
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = tokenCache.ReleaseRefreshLock(cleanup, key)
		}, nil
	}
	return mu.Unlock, nil
}

func (s *OpenAIOAuthService) readExcelGrant(ctx context.Context, account *Account) (string, map[string]any, error) {
	repo, ok := s.excelAccountRepo.(OpenAIExcelCredentialRepository)
	if !ok || s.excelEncryptor == nil {
		return "", nil, excelGrantStorageUnavailable()
	}
	ciphertext, err := repo.GetExcelCredentials(ctx, account.ID)
	if err != nil {
		return "", nil, errors.New("Excel credential storage unavailable")
	}
	if ciphertext == "" {
		return "", nil, excelAuthorizationUnavailable("grant_missing", "", excelAuthRequired())
	}
	raw, err := s.excelEncryptor.Decrypt(ciphertext)
	if err != nil {
		return "", nil, errors.New("Excel credential decryption failed")
	}
	var credentials map[string]any
	if json.Unmarshal([]byte(raw), &credentials) != nil {
		return "", nil, errors.New("invalid saved Excel credentials")
	}
	_, err = decodeStoredExcelOAuthTokenInfo(account, credentials, true)
	if err != nil {
		return "", nil, err
	}
	return ciphertext, credentials, nil
}

// Import is an explicit administrator action. It cannot overwrite primary credentials.
func (s *OpenAIOAuthService) ImportExcelCredentials(ctx context.Context, account *Account, credentials map[string]any) error {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if account.IsExcelOAuth() {
		return excelPrimaryGrantManaged()
	}
	repo, ok := s.excelAccountRepo.(OpenAIExcelCredentialRepository)
	if !ok || s.excelEncryptor == nil {
		return excelGrantStorageUnavailable()
	}
	info, err := excelOAuthTokenInfo(credentials)
	if err == nil {
		err = validateExcelIdentity(account, info)
	}
	if err != nil {
		return infraerrors.BadRequest("OPENAI_EXCEL_AUTH_INVALID", "Excel credentials must be unexpired and match the account's user and workspace")
	}
	imported := s.BuildAccountCredentials(info)
	imported[excelGrantIdentityKey] = uuid.NewString()
	raw, err := json.Marshal(imported)
	if err != nil {
		return err
	}
	encrypted, err := s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return errors.New("Excel credential encryption failed")
	}
	previous, err := repo.GetExcelCredentials(ctx, account.ID)
	if err != nil {
		return err
	}
	changed, err := repo.StoreExcelCredentials(ctx, account, previous, encrypted)
	if err != nil {
		return errors.New("Excel credential storage unavailable")
	}
	if !changed {
		return infraerrors.Conflict("OPENAI_EXCEL_AUTH_CHANGED", "Account or Excel authorization changed; reload and retry")
	}
	return nil
}

func (s *OpenAIOAuthService) ExcelAccessToken(ctx context.Context, account *Account, cache OpenAITokenCache) (string, error) {
	return s.excelAccessToken(ctx, account, cache, (&VendorService{openaiOAuth: s}).vendorVerifyOpenAIProfile)
}

// The private verifier argument lets tests use local signed provider fixtures;
// the public runtime entry always binds the fixed-provider profile verifier.
func (s *OpenAIOAuthService) excelAccessToken(ctx context.Context, account *Account, cache OpenAITokenCache,
	verify func(context.Context, *openai.TokenResponse, string, string, string) (*vendorVerifiedCredential, error),
) (string, error) {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer finish()
	previous, credentials, err := s.readExcelGrant(ctx, account)
	if err != nil {
		return "", err
	}
	if code := excelMapString(account.Extra, ExcelBPSAuthorizationErrorKey); code != "" {
		return "", excelAuthorizationUnavailable(code, previous, excelGrantReauthorizationRequired())
	}
	fresh := func(c map[string]any) bool {
		info, err := decodeStoredExcelOAuthTokenInfo(account, c, true)
		deadline := time.Now()
		if account.VendorID == nil || excelMapString(c, "refresh_token") != "" {
			deadline = deadline.Add(openAITokenRefreshSkew)
		}
		return err == nil && c[excelAccessTokenRejectedKey] != true && info.ExpiresAt > deadline.Unix()
	}
	if fresh(credentials) {
		return excelMapString(credentials, "access_token"), nil
	}
	if excelMapString(credentials, "refresh_token") == "" {
		return "", excelAuthorizationUnavailable("refresh_grant_missing", previous, excelGrantReauthorizationRequired())
	}
	unlock, err := lockExcelCredentials(ctx, account.ID, cache, true)
	if err != nil {
		return "", err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	previous, credentials, err = s.readExcelGrant(ctx, account)
	if err != nil {
		return "", err
	}
	if fresh(credentials) {
		return excelMapString(credentials, "access_token"), nil
	}
	if excelMapString(credentials, "refresh_token") == "" {
		return "", excelAuthorizationUnavailable("refresh_grant_missing", previous, excelGrantReauthorizationRequired())
	}
	if s.oauthClient == nil {
		return "", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_REFRESH_FAILED", "Excel token refresh is unavailable; credentials were retained")
	}
	if excelMapString(credentials, excelGrantIdentityKey) == "" {
		credentials[excelGrantIdentityKey] = excelStoredGrantIdentity(previous, credentials)
	}
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	refreshCtx, stop := context.WithTimeout(ctx, 40*time.Second)
	response, err := s.oauthClient.RefreshTokenWithClientID(refreshCtx, excelMapString(credentials, "refresh_token"), proxy, openai.ExcelClientID)
	stop()
	if err != nil || response == nil {
		if reason := excelRefreshRejection(err); reason != "" {
			current, newer, readErr := s.readExcelGrant(ctx, account)
			if readErr != nil {
				return "", readErr
			}
			if current != previous {
				if fresh(newer) {
					return excelMapString(newer, "access_token"), nil
				}
				return "", infraerrors.Conflict("OPENAI_EXCEL_AUTH_CHANGED", "Excel authorization changed during refresh")
			}
			return "", excelAuthorizationUnavailable(reason, previous, excelGrantReauthorizationRequired())
		}
		return "", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_REFRESH_FAILED", "Excel token refresh failed; credentials were retained")
	}
	if account.VendorID != nil {
		// Validate exactly this provider response. Never pass a retained, possibly
		// expired ID Token (or its at_hash for the old AT) as fresh evidence.
		verified, err := verify(ctx, response, "refresh_token", proxy, VendorOpenAIModeBPS)
		if err != nil || verified == nil || verified.AccountID != excelMapString(credentials, "chatgpt_account_id") ||
			excelMapString(verified.Credentials, "chatgpt_user_id") != excelMapString(credentials, "chatgpt_user_id") {
			return "", errors.New("refreshed supplier Excel identity could not be verified")
		}
		for key, value := range verified.Credentials {
			credentials[key] = value
		}
	} else {
		credentials["access_token"] = response.AccessToken
		if response.RefreshToken != "" {
			credentials["refresh_token"] = response.RefreshToken
		}
		if response.IDToken != "" {
			credentials["id_token"] = response.IDToken
		}
	}
	credentials[excelAccessTokenRefreshedKey] = credentials[excelAccessTokenRejectedKey] == true
	delete(credentials, excelAccessTokenRejectedKey)
	info, err := decodeStoredExcelOAuthTokenInfo(account, credentials, false)
	if err != nil {
		return "", errors.New("refreshed Excel identity mismatch")
	}
	raw, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	encrypted, err := s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return "", errors.New("Excel credential encryption failed")
	}
	saveCtx, stopSave := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stopSave()
	changed, err := s.excelAccountRepo.(OpenAIExcelCredentialRepository).StoreExcelCredentials(WithExcelCredentialRefresh(saveCtx), account, previous, encrypted)
	if err != nil {
		return "", errors.New("Excel refresh persistence failed")
	}
	if !changed {
		_, newer, err := s.readExcelGrant(saveCtx, account)
		if err != nil {
			return "", err
		}
		if fresh(newer) {
			return excelMapString(newer, "access_token"), nil
		}
		return "", errors.New("Excel authorization changed during refresh")
	}
	return info.AccessToken, nil
}

func (s *OpenAIOAuthService) rejectExcelAccessToken(ctx context.Context, account *Account, token string, cache OpenAITokenCache, codes ...string) error {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return err
	}
	defer finish()
	unlock, err := lockExcelCredentials(ctx, account.ID, cache, false)
	if err != nil || unlock == nil {
		return err
	}
	defer unlock()
	previous, credentials, err := s.readExcelGrant(ctx, account)
	if err != nil {
		return err
	}
	if token == "" || excelMapString(credentials, "access_token") != token {
		return nil
	}
	if len(codes) > 0 {
		switch codes[0] {
		case "token_revoked", "token_invalidated", "grant_revoked":
			return excelAuthorizationUnavailable(codes[0], previous, excelGrantReauthorizationRequired())
		}
	}
	if excelMapString(credentials, excelGrantIdentityKey) == "" {
		credentials[excelGrantIdentityKey] = excelStoredGrantIdentity(previous, credentials)
	}
	if credentials[excelAccessTokenRefreshedKey] == true || excelMapString(credentials, "refresh_token") == "" {
		return excelAuthorizationUnavailable("refreshed_access_token_rejected", previous, excelGrantReauthorizationRequired())
	}
	credentials[excelAccessTokenRejectedKey] = true
	raw, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	encrypted, err := s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return err
	}
	_, err = s.excelAccountRepo.(OpenAIExcelCredentialRepository).StoreExcelCredentials(WithExcelCredentialRefresh(ctx), account, previous, encrypted)
	return err
}
func (s *OpenAIGatewayService) getExcelBPSAccessToken(ctx context.Context, account *Account) (token string, err error) {
	failureCtx := ctx
	defer func() { s.disableExcelBPSForAuthorizationFailure(failureCtx, account, err) }()
	ctx = selectionbudget.PreparationContext(ctx)
	ctx, stopPreparation := OpenAITurnWaitContext(ctx)
	defer stopPreparation()
	if account != nil && account.VendorID != nil {
		if err := validateVendorModeDispatch(ctx, s.accountRepo, account, nil, VendorOpenAIModeBPS); err != nil {
			return "", err
		}
	}
	if account.IsExcelOAuth() {
		if err := s.excelPrimaryMissingRefresh(ctx, account); err != nil {
			return "", err
		}
		token, _, err := s.GetAccessToken(ctx, account)
		if reason := excelRefreshRejection(err); reason != "" {
			err = excelAuthorizationUnavailable(reason, "", err)
			err.(*excelAuthorizationFailure).fact.GrantFingerprint = "primary"
		}
		return token, err
	}
	if s.openAITokenProvider == nil {
		return "", excelBPSDependencyFailure()
	}
	oauth := s.openAITokenProvider.openAIOAuthService
	if oauth == nil {
		return "", excelBPSDependencyFailure()
	}
	return oauth.ExcelAccessToken(ctx, account, s.openAITokenProvider.tokenCache)
}

func excelStoredGrantIdentity(ciphertext string, credentials map[string]any) string {
	if identity := excelMapString(credentials, excelGrantIdentityKey); identity != "" {
		return identity
	}
	return "legacy-" + ExcelGrantFingerprint(ciphertext)
}

// ExcelBPSGrantIdentity is a nonsecret continuation binding. Reauthorization
// changes it even for the same user/workspace; token refresh does not.
func (s *OpenAIGatewayService) ExcelBPSGrantIdentity(ctx context.Context, account *Account) (string, error) {
	if account == nil {
		return "", excelBPSDependencyFailure()
	}
	if bound, ok := ctx.Value(excelBPSDispatchGrantKey{}).(excelBPSDispatchGrant); ok && bound.accountID == account.ID && bound.identity != "" {
		return bound.identity, nil
	}
	if account.IsExcelOAuth() {
		return "excel-primary:" + codexAccountIdentityNamespace(account), nil
	}
	if s == nil || s.openAITokenProvider == nil || s.openAITokenProvider.openAIOAuthService == nil {
		return "", excelBPSDependencyFailure()
	}
	oauth := s.openAITokenProvider.openAIOAuthService
	ciphertext, grant, err := oauth.readExcelGrant(ctx, account)
	if err != nil {
		return "", err
	}
	return "excel:" + excelStoredGrantIdentity(ciphertext, grant), nil
}

func (s *OpenAIOAuthService) ExcelAuthorizationStatus(ctx context.Context, account *Account) (map[string]any, error) {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	code := excelMapString(account.Extra, ExcelBPSAuthorizationErrorKey)
	if failure, ok := account.Extra[ExcelBPSFailureKey].(map[string]any); ok && failure["reauth_required"] == true && failure["recovery_cancelled"] != true {
		code, _ = failure["reason"].(string)
	}
	if account.IsExcelOAuth() {
		present := account.GetCredential("access_token") != ""
		if account.Status == StatusError && code == "" {
			code = "authorization_unavailable"
		}
		status := map[string]any{"authorized": present && account.Status != StatusError && code == "", "present": present, "source": "primary", "authorization_error": code}
		if expiry := account.GetOpenAITokenExpiresAt(); expiry != nil {
			refresh := !expiry.After(time.Now())
			status["expires_at"], status["refresh_required"] = expiry.Unix(), refresh
			if refresh {
				status["authorized"] = false
			}
		}
		return status, nil
	}
	_, credentials, err := s.readExcelGrant(ctx, account)
	if infraerrors.Reason(err) == "OPENAI_EXCEL_AUTH_REQUIRED" {
		return map[string]any{"authorized": false, "present": false}, nil
	}
	if err != nil {
		if IsExcelAuthorizationError(err) {
			return nil, err
		}
		return nil, infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_UNAVAILABLE", "Excel authorization is unavailable")
	}
	info, err := decodeStoredExcelOAuthTokenInfo(account, credentials, true)
	if err != nil {
		return nil, err
	}
	refresh := credentials[excelAccessTokenRejectedKey] == true || info.ExpiresAt <= time.Now().Unix()
	if refresh && excelMapString(credentials, "refresh_token") == "" && code == "" {
		code = "refresh_grant_missing"
	}
	return map[string]any{"authorized": code == "" && !refresh, "present": true, "expires_at": info.ExpiresAt, "refresh_required": code == "" && refresh, "authorization_error": code}, nil
}

func (s *OpenAIOAuthService) DeleteExcelAuthorization(ctx context.Context, account *Account) error {
	ctx, finish, err := s.sessionStore.Begin(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if account.IsExcelOAuth() {
		return excelPrimaryGrantManaged()
	}
	repo, ok := s.excelAccountRepo.(OpenAIExcelCredentialRepository)
	if !ok {
		return errors.New("Excel credential storage unavailable")
	}
	previous, err := repo.GetExcelCredentials(ctx, account.ID)
	if err != nil || previous == "" {
		return err
	}
	changed, err := repo.StoreExcelCredentials(ctx, account, previous, "")
	if err != nil {
		return err
	}
	if !changed {
		return infraerrors.Conflict("OPENAI_EXCEL_AUTH_CHANGED", "Excel authorization changed; reload and retry")
	}
	return nil
}

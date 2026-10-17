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
)

// Private encrypted storage is separate from the account's native Codex grant.
type OpenAIExcelCredentialRepository interface {
	GetExcelCredentials(context.Context, int64) (string, error)
	StoreExcelCredentials(context.Context, *Account, string, string) (bool, error)
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

var excelOAuthRefreshLocks sync.Map

// Stored inside the encrypted grant. A resource-server 401 rejects the access
// token, not necessarily the refresh token; refresh it on the next token read.
const excelAccessTokenRejectedKey = "_access_token_rejected"

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
		return "", nil, excelAuthRequired()
	}
	raw, err := s.excelEncryptor.Decrypt(ciphertext)
	if err != nil {
		return "", nil, errors.New("Excel credential decryption failed")
	}
	var credentials map[string]any
	if json.Unmarshal([]byte(raw), &credentials) != nil {
		return "", nil, errors.New("invalid saved Excel credentials")
	}
	info, err := decodeExcelOAuthTokenInfo(credentials, true)
	if err == nil {
		err = validateExcelIdentity(account, info)
	}
	if err != nil {
		return "", nil, err
	}
	return ciphertext, credentials, nil
}

// Import is an explicit administrator action. It cannot overwrite primary credentials.
func (s *OpenAIOAuthService) ImportExcelCredentials(ctx context.Context, account *Account, credentials map[string]any) error {
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
	raw, err := json.Marshal(s.BuildAccountCredentials(info))
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
	_, credentials, err := s.readExcelGrant(ctx, account)
	if err != nil {
		return "", err
	}
	fresh := func(c map[string]any) bool {
		info, err := decodeExcelOAuthTokenInfo(c, true)
		return err == nil && c[excelAccessTokenRejectedKey] != true && info.ExpiresAt > time.Now().Add(openAITokenRefreshSkew).Unix()
	}
	if fresh(credentials) {
		return excelMapString(credentials, "access_token"), nil
	}
	unlock, err := lockExcelCredentials(ctx, account.ID, cache, true)
	if err != nil {
		return "", err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	previous, credentials, err := s.readExcelGrant(ctx, account)
	if err != nil {
		return "", err
	}
	if fresh(credentials) {
		return excelMapString(credentials, "access_token"), nil
	}
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	refreshCtx, stop := context.WithTimeout(ctx, 40*time.Second)
	response, err := s.oauthClient.RefreshTokenWithClientID(refreshCtx, excelMapString(credentials, "refresh_token"), proxy, openai.ExcelClientID)
	stop()
	if err != nil || response == nil {
		return "", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_REFRESH_FAILED", "Excel token refresh failed; credentials were retained")
	}
	credentials["access_token"] = response.AccessToken
	if response.RefreshToken != "" {
		credentials["refresh_token"] = response.RefreshToken
	}
	if response.IDToken != "" {
		credentials["id_token"] = response.IDToken
	}
	delete(credentials, excelAccessTokenRejectedKey)
	info, err := excelOAuthTokenInfo(credentials)
	if err == nil {
		err = validateExcelIdentity(account, info)
	}
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
	changed, err := s.excelAccountRepo.(OpenAIExcelCredentialRepository).StoreExcelCredentials(saveCtx, account, previous, encrypted)
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

func (s *OpenAIOAuthService) rejectExcelAccessToken(ctx context.Context, account *Account, token string, cache OpenAITokenCache) error {
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
	credentials[excelAccessTokenRejectedKey] = true
	raw, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	encrypted, err := s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return err
	}
	_, err = s.excelAccountRepo.(OpenAIExcelCredentialRepository).StoreExcelCredentials(ctx, account, previous, encrypted)
	return err
}
func (s *OpenAIGatewayService) getExcelBPSAccessToken(ctx context.Context, account *Account) (string, error) {
	if account.IsExcelOAuth() {
		token, _, err := s.GetAccessToken(ctx, account)
		return token, err
	}
	if s.openAITokenProvider == nil {
		return "", excelAuthRequired()
	}
	oauth := s.openAITokenProvider.openAIOAuthService
	if oauth == nil {
		return "", excelAuthRequired()
	}
	return oauth.ExcelAccessToken(ctx, account, s.openAITokenProvider.tokenCache)
}

func (s *OpenAIOAuthService) ExcelAuthorizationStatus(ctx context.Context, account *Account) (map[string]any, error) {
	if account.IsExcelOAuth() {
		status := map[string]any{"authorized": account.GetCredential("access_token") != "", "source": "primary"}
		if expiry := account.GetOpenAITokenExpiresAt(); expiry != nil {
			status["expires_at"], status["refresh_required"] = expiry.Unix(), !expiry.After(time.Now())
		}
		return status, nil
	}
	_, credentials, err := s.readExcelGrant(ctx, account)
	if infraerrors.Reason(err) == "OPENAI_EXCEL_AUTH_REQUIRED" {
		return map[string]any{"authorized": false}, nil
	}
	if err != nil {
		if IsExcelAuthorizationError(err) {
			return nil, err
		}
		return nil, infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_UNAVAILABLE", "Excel authorization is unavailable")
	}
	info, err := decodeExcelOAuthTokenInfo(credentials, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"authorized": true, "expires_at": info.ExpiresAt, "refresh_required": credentials[excelAccessTokenRejectedKey] == true || info.ExpiresAt <= time.Now().Unix()}, nil
}
func (s *OpenAIOAuthService) DeleteExcelAuthorization(ctx context.Context, account *Account) error {
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

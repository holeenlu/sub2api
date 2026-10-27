package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

// Reuse the native structured account/workspace classifier. Only an error's
// own code is evidence; messages, echoed input and generic 403s are not.
func openAIPermanentCredentialCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if isOpenAIUpstreamAccessStateCode(code) {
		return code
	}
	switch code {
	case "token_revoked", "token_invalidated", "invalid_grant", "invalid_refresh_token",
		"refresh_token_reused", "refresh_token_invalidated", "token_expired", "app_session_terminated":
		return code
	}
	return ""
}

func openAIResponseCredentialCode(body []byte) string {
	if len(body) > cloudErrorPayloadLimit || !cloudUniqueJSON(body) {
		return ""
	}
	for _, path := range []string{"error.code", "response.error.code", "detail.code", "code", "error"} {
		if value := gjson.GetBytes(body, path); value.Type == gjson.String {
			if code := strings.ToLower(strings.TrimSpace(value.String())); isOpenAIUpstreamAccessStateCode(code) || code == "token_revoked" || code == "token_invalidated" {
				return code
			}
		}
	}
	return ""
}

// OpenAIOAuthTokenRefreshFailure retains safe machine-readable facts at the
// token endpoint. A temporary transport/server failure cannot revoke a grant.
func OpenAIOAuthTokenRefreshFailure(status int, body []byte) error {
	code := openAIResponseCredentialCode(body)
	if status != 400 && status != 401 && status != 403 {
		code = ""
		body = nil
	}
	if code == "" && len(body) <= cloudErrorPayloadLimit && cloudUniqueJSON(body) {
		for _, path := range []string{"error.code", "error"} {
			if terminal := openAIPermanentCredentialCode(gjson.GetBytes(body, path).String()); terminal != "" {
				code = terminal
				break
			}
			switch value := gjson.GetBytes(body, path).String(); value {
			case "invalid_client", "unauthorized_client", "invalid_scope", "access_denied":
				code = value
			}
		}
	}
	return infraerrors.New(http.StatusBadGateway, "OPENAI_OAUTH_TOKEN_REFRESH_FAILED", fmt.Sprintf("OpenAI token refresh was rejected (status %d)", status)).WithMetadata(map[string]string{
		"upstream_status": strconv.Itoa(status), "upstream_code": code,
	})
}

func openAIRefreshCredentialCode(err error) string {
	var failure *infraerrors.ApplicationError
	if !errors.As(err, &failure) || failure.Reason != "OPENAI_OAUTH_TOKEN_REFRESH_FAILED" {
		return ""
	}
	status, _ := strconv.Atoi(failure.Metadata["upstream_status"])
	if status != 400 && status != 401 && status != 403 {
		return ""
	}
	return openAIPermanentCredentialCode(failure.Metadata["upstream_code"])
}

func persistOpenAICredentialFailure(ctx context.Context, repo AccountRepository, account *Account, code string) (bool, error) {
	if account == nil || (!isNativeOpenAIOAuthRefresh(account) && !(account.IsExcelOAuth() && account.VendorID == nil && account.ParentAccountID == nil)) || openAIPermanentCredentialCode(code) == "" {
		return false, nil
	}
	conditional, err := nativeOpenAIRefreshRepository(repo)
	if err != nil {
		return false, err
	}
	return conditional.SetOpenAIOAuthRefreshErrorIfUnchanged(ctx, account, "OpenAI authorization rejected ("+code+"); reconnect the account")
}

// Both Cloud and quota reads use this native recovery authority. The actual
// rejected token is compared before the repository's atomic credential CAS,
// so an old response cannot disable a newly authorized account.
func (p *OpenAITokenProvider) recoverRejectedAccessToken(ctx context.Context, id int64, token string, status int, body []byte, refresh bool) (bool, error) {
	code := openAIResponseCredentialCode(body)
	if p == nil || p.accountRepo == nil || (code == "" && status != http.StatusUnauthorized) || ctx.Err() != nil {
		return false, ctx.Err()
	}
	account, err := p.accountRepo.GetByID(ctx, id)
	if err != nil || !isNativeOpenAIOAuthRefresh(account) {
		return false, err
	}
	if account.GetCredential("access_token") != token {
		return refresh && account.IsActive(), nil
	}
	if code != "" {
		_, err = persistOpenAICredentialFailure(ctx, p.accountRepo, account, code)
		return false, err
	}
	if refresh && p.refreshAPI != nil && p.executor != nil && account.GetCredential("refresh_token") != "" {
		result, refreshErr := p.refreshAPI.RefreshRejected(ctx, account, p.executor, token)
		if refreshErr == nil && result != nil && result.Account != nil && result.Account.IsActive() && result.Account.GetCredential("access_token") != token {
			return true, nil
		}
		if refreshErr != nil {
			return false, refreshErr
		}
	}
	conditional, err := nativeOpenAIRefreshRepository(p.accountRepo)
	if err != nil {
		return false, err
	}
	if account.GetCredential("refresh_token") == "" {
		_, err = conditional.SetOpenAIOAuthRefreshErrorIfUnchanged(ctx, account, "OpenAI authorization rejected (401); refresh token missing, reconnect the account")
	} else {
		_, err = conditional.SetOpenAIOAuthRefreshTempUnschedulableIfUnchanged(ctx, account, time.Now().Add(10*time.Minute), "OpenAI authorization rejected (401); refresh or reconnect the account")
	}
	if p.tokenCache != nil {
		_ = p.tokenCache.DeleteAccessToken(ctx, OpenAITokenCacheKey(account))
	}
	return false, err
}

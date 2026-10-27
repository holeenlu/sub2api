package service

import (
	"context"
	"crypto/md5" // Opaque CAS fingerprint, never an authentication or password digest.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/tidwall/gjson"
)

// ExcelOAuthRefreshError is emitted only by the OAuth HTTP boundary. Never
// classify arbitrary error strings (including a proxy's 5xx body) as revocation.
type ExcelOAuthRefreshError struct {
	Status int
	Code   string
}

func (e *ExcelOAuthRefreshError) Error() string { return "Excel OAuth refresh rejected" }

// ExcelBPSFailure describes a verified authorization observation. An empty
// fingerprint means an authoritative lookup found no independent grant.
type ExcelBPSFailure struct {
	Reason           string
	ReauthRequired   bool
	GrantFingerprint string
}

type excelAuthorizationFailure struct {
	fact  ExcelBPSFailure
	cause error
}

func (e *excelAuthorizationFailure) Error() string { return e.cause.Error() }
func (e *excelAuthorizationFailure) Unwrap() error { return e.cause }

func excelAuthorizationUnavailable(reason, ciphertext string, cause error) error {
	return &excelAuthorizationFailure{fact: ExcelBPSFailure{
		Reason: reason, ReauthRequired: true, GrantFingerprint: ExcelGrantFingerprint(ciphertext),
	}, cause: cause}
}

func ExcelGrantFingerprint(ciphertext string) string {
	if ciphertext == "" {
		return ""
	}
	sum := md5.Sum([]byte(ciphertext))
	return hex.EncodeToString(sum[:])
}

func excelRefreshRejection(err error) string {
	var rejection *ExcelOAuthRefreshError
	if !errors.As(err, &rejection) || (rejection.Status != http.StatusBadRequest && rejection.Status != http.StatusUnauthorized) {
		return ""
	}
	switch rejection.Code {
	case "invalid_grant", "token_revoked", "grant_revoked":
		return rejection.Code
	}
	return ""
}

type AccountExcelBPSFailureRepository interface {
	DisableExcelBPSForFailure(context.Context, *Account, ExcelBPSFailure) (bool, error)
}

type AccountExcelBPSDispatchRepository interface {
	ValidateExcelBPSDispatch(context.Context, *Account, *int64, string) error
}

type excelBPSDispatchGrantKey struct{}
type excelBPSDispatchGrant struct {
	accountID   int64
	fingerprint string
	identity    string
}
type excelBPSRecoveryProbeKey struct{}

func IsExcelBPSRecoveryProbe(ctx context.Context) bool {
	probe, _ := ctx.Value(excelBPSRecoveryProbeKey{}).(bool)
	return probe
}

func ExcelBPSDispatchFingerprint(ctx context.Context, accountID int64) (string, bool) {
	binding, ok := ctx.Value(excelBPSDispatchGrantKey{}).(excelBPSDispatchGrant)
	return binding.fingerprint, ok && binding.accountID == accountID
}

func (s *OpenAIGatewayService) ExcelBPSDispatchContext(ctx context.Context, account *Account, token string) (context.Context, error) {
	grant, ok := s.excelBPSGrantForToken(ctx, account, token)
	if !ok {
		return nil, excelBPSDependencyFailure()
	}
	if previous, bound := ExcelBPSDispatchFingerprint(ctx, account.ID); bound {
		if previous != grant.fingerprint {
			return nil, errOAuthRefreshAccountStateChanged
		}
		return ctx, nil
	}
	return context.WithValue(ctx, excelBPSDispatchGrantKey{}, grant), nil
}

// Refresh and reauthorization use the same credential CAS, but only a refresh
// may advance the fingerprint of the SAME disabled grant. Imports, revocations,
// and all unmarked writers invalidate the old recovery authorization.
type excelCredentialRefreshContextKey struct{}

func WithExcelCredentialRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, excelCredentialRefreshContextKey{}, true)
}

func IsExcelCredentialRefresh(ctx context.Context) bool {
	refresh, _ := ctx.Value(excelCredentialRefreshContextKey{}).(bool)
	return refresh
}

func (s *OpenAIGatewayService) disableExcelBPSForAuthorizationFailure(ctx context.Context, account *Account, err error) {
	var failure *excelAuthorizationFailure
	if s == nil || !errors.As(err, &failure) || ctx.Err() != nil {
		return
	}
	repo, ok := s.accountRepo.(AccountExcelBPSFailureRepository)
	if !ok {
		return
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	// Storage errors remain errors; never report a successful shutdown from a
	// failed or unconfirmed write.
	_, _ = repo.DisableExcelBPSForFailure(stateCtx, account, failure.fact)
}

// All actual BPS HTTP consumers share this conservative account-level rule.
// Unknown/HTML/message-only 403s are not evidence that the account is banned.
func excelBPSAccountForbidden(status int, raw []byte) bool {
	if status != http.StatusForbidden {
		return false
	}
	switch gjson.GetBytes(raw, "error.code").String() {
	case "account_deactivated", "account_suspended", "account_banned", "account_disabled",
		"workspace_deactivated", "workspace_suspended", "bps_account_blocked":
		return true
	default:
		return false
	}
}

// In-band terminals carry the same verified authorization facts as an HTTP
// rejection. Observe them after the driver has joined, without starting a new
// generation or discarding usage already reported by that turn.
func (s *OpenAIGatewayService) observeExcelBPSTerminalAuthorization(ctx context.Context, account *Account, failure *basispoints.UpstreamFailure, headers http.Header, token, model string) {
	if failure == nil || failure.Local {
		return
	}
	if cyber, _, _ := detectOpenAICyberPolicy(failure.Diagnostic); cyber || failure.Code == basispoints.FileOwnershipFailureCode {
		return
	}
	s.handleExcelBPSUnauthorized(ctx, account, failure.Status, headers, failure.Diagnostic, token)
	if isExcelBPSModelPermissionError(failure.Status, failure.Code) {
		s.coolDownExcelBPSModel(account, model)
		return
	}
	if excelBPSAccountForbidden(failure.Status, failure.Diagnostic) {
		s.moveExcelBPSOn403(ctx, account)
		s.disableExcelBPSOn403(ctx, account, token)
	}
}

func excelBPSDependencyFailure() error {
	return infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_STORAGE_UNAVAILABLE", "Excel authorization service is unavailable")
}

func (s *OpenAIGatewayService) excelBPSFailureForToken(ctx context.Context, account *Account, token, reason string, reauth bool) (ExcelBPSFailure, bool) {
	grant, ok := s.excelBPSGrantForToken(ctx, account, token)
	return ExcelBPSFailure{Reason: reason, ReauthRequired: reauth, GrantFingerprint: grant.fingerprint}, ok
}

func (s *OpenAIGatewayService) excelBPSGrantForToken(ctx context.Context, account *Account, token string) (excelBPSDispatchGrant, bool) {
	bound := excelBPSDispatchGrant{}
	if account == nil || token == "" {
		return bound, false
	}
	bound.accountID = account.ID
	if account.IsExcelOAuth() {
		bound.fingerprint = "primary"
		bound.identity = "excel-primary:" + codexAccountIdentityNamespace(account)
		return bound, token == account.GetCredential("access_token")
	}
	if s.openAITokenProvider == nil || s.openAITokenProvider.openAIOAuthService == nil {
		return bound, false
	}
	ciphertext, grant, err := s.openAITokenProvider.openAIOAuthService.readExcelGrant(ctx, account)
	if err != nil || excelMapString(grant, "access_token") != token {
		return bound, false
	}
	bound.fingerprint = ExcelGrantFingerprint(ciphertext)
	bound.identity = "excel:" + excelStoredGrantIdentity(ciphertext, grant)
	return bound, true
}

func (s *OpenAIGatewayService) refreshRejectedExcelPrimary(ctx context.Context, account *Account, token string) bool {
	provider := s.openAITokenProvider
	if provider == nil || provider.refreshAPI == nil || provider.executor == nil || s.accountRepo == nil {
		return false
	}
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	key := turnRecoveryTarget{owner: recoveryOwner(account), mode: "excel-primary-refreshed", grant: sha256.Sum256([]byte(token))}
	if budget != nil {
		budget.mu.Lock()
		rejectedFresh := budget.refreshedGrants[key]
		budget.mu.Unlock()
		if rejectedFresh {
			failure := &excelAuthorizationFailure{fact: ExcelBPSFailure{Reason: "refreshed_access_token_rejected", ReauthRequired: true, GrantFingerprint: "primary"}, cause: excelGrantReauthorizationRequired()}
			s.disableExcelBPSForAuthorizationFailure(ctx, account, failure)
			return false
		}
	}
	// Fixed grant identity bounds refresh across rotating access tokens.
	if takeTurnGrantRefresh(ctx, account, "excel-primary", "") != nil {
		return false
	}
	result, err := provider.refreshAPI.RefreshRejected(ctx, account, provider.executor, token)
	if reason := excelRefreshRejection(err); reason != "" {
		s.disableExcelBPSForAuthorizationFailure(ctx, account, &excelAuthorizationFailure{
			fact: ExcelBPSFailure{Reason: reason, ReauthRequired: true, GrantFingerprint: "primary"}, cause: excelGrantReauthorizationRequired(),
		})
	}
	if err != nil || result == nil || result.Account == nil || !result.Account.IsActive() || result.Account.IsVendorArchived() {
		return false
	}
	fresh := result.Account.GetCredential("access_token")
	if fresh == "" || fresh == token {
		return false
	}
	if provider.refreshAPI.tokenCache != nil {
		_ = provider.refreshAPI.tokenCache.DeleteAccessToken(ctx, provider.executor.CacheKey(account))
	}
	if budget != nil {
		key.grant = sha256.Sum256([]byte(fresh))
		budget.mu.Lock()
		budget.refreshedGrants[key] = true
		budget.mu.Unlock()
	}
	return true
}

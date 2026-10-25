package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// The existing turn budget owns recovery across adapters. These keys contain
// identity and bounded digests only, never prompts or credential plaintext.
type turnRecoveryTargetKey struct{}

var (
	ErrTurnTargetExhausted      = execution.ErrTargetBudgetExhausted
	ErrTurnPreparationExhausted = execution.ErrPreparationBudgetExhausted
	ErrTurnRetryWaitExhausted   = execution.ErrRetryWaitBudgetExhausted
	ErrSelectionDependency      = errors.New("account selection dependency unavailable")
)

func recoveryOwner(account *Account) model.ResourceOwner {
	if account == nil {
		return model.ResourceOwner{}
	}
	id := account.ID
	if account.ParentAccountID != nil && *account.ParentAccountID > 0 {
		id = *account.ParentAccountID
	}
	var vendor int64
	if account.VendorID != nil {
		vendor = *account.VendorID
	}
	return model.ResourceOwner{AccountID: id, VendorID: vendor}
}

func retrySelectionDependency(ctx context.Context, err error) (bool, error) {
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b == nil {
		return false, err
	}
	if !errors.Is(err, ErrSelectionDependency) || ctx.Err() != nil {
		return false, err
	}
	if budgetErr := TakeTurnPreparationRetry(ctx); budgetErr != nil {
		return false, fmt.Errorf("%w: %w", err, budgetErr)
	}
	if waitErr := WaitOpenAITurnRetry(ctx, 100*time.Millisecond); waitErr != nil {
		return false, fmt.Errorf("%w: %w", err, waitErr)
	}
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "retry", Action: "dependency_retry", Reason: "selection_dependency"})
	return true, nil
}

func turnRetryWaitError(ctx context.Context) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return ErrTurnRetryWaitExhausted
}

func takeTurnGrantRefresh(ctx context.Context, account *Account, mode, token string) error {
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return ErrTurnPreparationExhausted
	}
	key := model.AttemptTarget{Owner: recoveryOwner(account), Mode: mode, GrantDigest: sha256.Sum256([]byte(token))}
	b.mu.Lock()
	if b.refreshedGrants == nil {
		b.refreshedGrants = make(map[model.AttemptTarget]bool)
	}
	if b.refreshedGrants[key] {
		b.mu.Unlock()
		return ErrTurnPreparationExhausted
	}
	b.refreshedGrants[key] = true
	b.mu.Unlock()
	return TakeTurnPreparationRetry(ctx)
}

func refreshRejectedCredential(ctx context.Context, repo AccountRepository, account *Account, token string, api *OAuthRefreshAPI, executor OAuthRefreshExecutor) (string, bool) {
	ctx = selectionbudget.PreparationContext(ctx)
	if api == nil || executor == nil || account == nil || account.Type != AccountTypeOAuth || account.IsOpenAIAgentIdentity() {
		return "", false
	}
	owner, err := resolveCredentialAccount(ctx, repo, account)
	if err != nil || owner == nil || !executor.CanRefresh(owner) {
		return "", false
	}
	if takeTurnGrantRefresh(ctx, owner, owner.Platform, token) != nil {
		return "", false
	}
	waitCtx, cancel := OpenAITurnWaitContext(ctx)
	defer cancel()
	result, err := api.RefreshRejected(waitCtx, owner, executor, token)
	if err != nil || result == nil || result.Account == nil || !result.Account.IsActive() || result.Account.IsVendorArchived() || result.Account.Platform != account.Platform || result.Account.Type != account.Type || recoveryOwner(result.Account) != recoveryOwner(owner) {
		return "", false
	}
	fresh := result.Account.GetCredential("access_token")
	if fresh != "" && fresh != token && api.tokenCache != nil {
		_ = api.tokenCache.DeleteAccessToken(waitCtx, executor.CacheKey(owner))
	}
	return fresh, fresh != "" && fresh != token
}

func withTurnRecoveryTarget(ctx context.Context, account *Account, mode, endpoint, proxy, grant string, variants ...any) context.Context {
	ctx = context.WithValue(ctx, schedulingRecoveryAccountKey{}, account)
	limit := 2
	if account != nil && account.GetPoolModeRetryCount() == 0 {
		limit = 1
	}
	modelName := ""
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		modelName = b.model
		b.mu.Unlock()
	}
	var variant [32]byte
	if len(variants) > 0 {
		data, ok := variants[0].([]byte)
		if !ok {
			data, _ = json.Marshal(variants[0])
		}
		variant = sha256.Sum256(data)
		if mode == "websocket" {
			ctx = WithSchedulingQuotaRequest(ctx, data, ContentModerationProtocolOpenAIResponses)
		}
	}
	return context.WithValue(ctx, turnRecoveryTargetKey{}, model.AttemptTarget{Model: modelName, InputDigest: variant, Owner: recoveryOwner(account), Mode: mode, Endpoint: endpoint, ProxyDigest: sha256.Sum256([]byte(proxy)), GrantDigest: sha256.Sum256([]byte(grant)), Limit: limit})
}

// Attach the target to the actual request so repository-level unsent retries
// consume the same target allowance, not an independent counter.
func takeHTTPAccountTurnAttempt(req *http.Request, account *Account, proxy, mode string) (*http.Request, error) {
	if strings.HasPrefix(req.URL.Path, "/basispoints/") {
		mode = "bps"
	}
	grant := req.Header.Get("Authorization") + req.Header.Get("x-api-key")

	ctx := withTurnRecoveryTarget(req.Context(), account, mode, req.URL.Path, proxy, grant)
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, body)
		_ = body.Close()
		if err != nil {
			return nil, err
		}
		target, _ := ctx.Value(turnRecoveryTargetKey{}).(model.AttemptTarget)
		copy(target.InputDigest[:], hash.Sum(nil))
		ctx = context.WithValue(ctx, turnRecoveryTargetKey{}, target)
	}
	if err := TakeOpenAITurnAttempt(ctx); err != nil {
		if errors.Is(err, ErrTurnTargetExhausted) {
			failedMode := "native"
			if mode == "bps" {
				failedMode = mode
			}
			if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
				b.mu.Lock()
				owner := recoveryOwner(account)
				if b.excludedModes == nil {
					b.excludedModes = make(map[model.ResourceOwner]map[string]bool)
				}
				if b.excludedModes[owner] == nil {
					b.excludedModes[owner] = make(map[string]bool)
				}
				b.excludedModes[owner][failedMode] = true
				b.mu.Unlock()
			}
		}
		return nil, err
	}
	return req.WithContext(ctx), nil
}

func TakeTurnPreparationRetry(ctx context.Context) error {
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if b.control != nil && b.control.Err() != nil {
		return b.control.Err()
	}
	if !b.deadline.IsZero() && !time.Now().Before(b.deadline) {
		return ErrOpenAIWSFirstOutputBudgetExhausted
	}
	return b.attemptBudget.TakePreparation()
}

// Pinning is a hard request-local constraint, independent of sticky affinity.
// Every selection still performs its normal eligibility and slot checks.
func PinTurnRetryAccount(ctx context.Context, account *Account) {
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.pinnedAccount = account
	}
}

func TurnRetryAccount(ctx context.Context) *Account {
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.pinnedAccount
	}
	return nil
}

func turnAccountEligible(ctx context.Context, account *Account, modes ...string) bool {
	if account == nil {
		return false
	}
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pinnedAccount != nil && (account.ID != b.pinnedAccount.ID || recoveryOwner(account) != recoveryOwner(b.pinnedAccount)) {
		return false
	}
	mode := "native"
	if len(modes) > 0 {
		mode = modes[0]
	}

	return b.attemptBudget.OwnerAvailable(recoveryOwner(account)) && !b.excludedModes[recoveryOwner(account)][mode]
}

func IsTurnBudgetError(err error) bool {
	return errors.Is(err, ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) || errors.Is(err, ErrTurnPreparationExhausted) || errors.Is(err, ErrTurnRetryWaitExhausted)
}

func turnAttemptFailure(err error) error {
	if errors.Is(err, ErrTurnTargetExhausted) {
		return &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, RequestNotSent: true, Cause: err, Reason: "recovery_target_exhausted", RequestScopedTransient: true}
	}
	return err
}

func filterTurnRecoveryAccounts(ctx context.Context, accounts []Account) []Account {
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if turnAccountEligible(ctx, &account) {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

// Credential preparation failed before sending inference. The cause remains in
// the internal chain; client output contains no provider error or credential.
func newCredentialPreparationFailure(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || IsTurnBudgetError(err) {
		return err
	}
	var existing *UpstreamFailoverError
	if errors.As(err, &existing) {
		return err
	}
	return &UpstreamFailoverError{
		StatusCode: http.StatusServiceUnavailable, Stage: GatewayFailureStageAccountAuth,
		Scope: GatewayFailureScopeProvider, Reason: "credential_preparation_failed", Cause: err,
		RequestNotSent: true, RetryableOnSameAccount: true, SameAccountRetryMax: 1,
		NextAccountAction: NextAccountRetry, ClientStatusCode: http.StatusServiceUnavailable,
		ClientMessage: "Upstream credentials temporarily unavailable",
	}
}

// Native payloads have account-owned references too. Ordinary inline history
// and client-executed function calls remain portable; opaque/server state does
// not become portable merely because no text was written yet.
func NativeAccountReplayAllowed(body []byte) bool {
	allowed := true
	var walk func(gjson.Result)
	walk = func(value gjson.Result) {
		if !allowed {
			return
		}
		if value.IsArray() {
			for _, child := range value.Array() {
				walk(child)
			}
			return
		}
		if !value.IsObject() {
			return
		}
		value.ForEach(func(key, child gjson.Result) bool {
			switch key.String() {
			case "previous_response_id", "cachedContent", "file_id", "fileId", "file_uri", "fileUri", "vector_store_ids", "container", "encrypted_content", "signature":
				if child.Exists() && child.Type != gjson.Null && child.Raw != `""` {
					allowed = false
					return false
				}
			}
			walk(child)
			return allowed
		})
	}
	walk(gjson.ParseBytes(body))
	return allowed
}

func NativeUncertainReplaySafe(body []byte) bool {
	// Native server tools may already have executed even when no output reached
	// us. Only known client-executed tools are replayable without send evidence.
	for _, payload := range []gjson.Result{gjson.ParseBytes(body), gjson.GetBytes(body, "request")} {
		if payload.Get("mcp_servers.#").Int() > 0 {
			return false
		}
		for _, tool := range payload.Get("tools").Array() {
			typ := tool.Get("type").String()
			switch {
			case typ == "function", typ == "custom":
				continue
			case strings.HasPrefix(typ, "computer_"), strings.HasPrefix(typ, "bash_"), strings.HasPrefix(typ, "text_editor_"):
				continue
			case typ == "" && tool.Get("name").String() != "" && tool.Get("input_schema").IsObject():
				continue // Claude client-side custom tool.
			case typ == "" && tool.Get("functionDeclarations").IsArray():
				clientOnly := true
				tool.ForEach(func(key, _ gjson.Result) bool {
					clientOnly = key.String() == "functionDeclarations"
					return clientOnly
				})
				if clientOnly {
					continue
				}
			}
			return false
		}
	}
	return true
}

// Exclude the failed physical owner only in the attempted mode. A shadow does
// not buy another attempt with the same grant; native authorization is separate
// from Excel authorization and remains available in the native recovery phase.
func ExcludeTurnRecoveryAccount(ctx context.Context, account *Account, failure *UpstreamFailoverError) {
	if account == nil || failure == nil || failure.Reason == "recovery_target_exhausted" {
		return
	}
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return
	}
	mode := "native"

	if failure.Reason == CloudPreInferenceFailureReason {
		mode = UsageAuthorizationModeCloud
	} else if strings.HasPrefix(string(failure.Reason), "basispoints_") {
		mode = "bps"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	owner := recoveryOwner(account)
	if b.excludedModes == nil {
		b.excludedModes = make(map[model.ResourceOwner]map[string]bool)
	}
	if b.excludedModes[owner] == nil {
		b.excludedModes[owner] = make(map[string]bool)
	}
	b.excludedModes[owner][mode] = true
	if mode == "bps" && b.bpsRecovery.managed {
		if b.bpsRecovery.rejected == nil {
			b.bpsRecovery.rejected = make(map[int64]bool)
		}
		b.bpsRecovery.rejected[account.ID] = true
	}
	if failure.Stage == "selection" || failure.Scope == GatewayFailureScopeRequest || failure.Reason == "recovery_target_exhausted" {
		return
	}
	if b.failedDomains == nil {
		b.failedDomains = make(map[model.ResourceOwner]turnRecoveryDomain)
	}
	domain := turnRecoveryDomain{vendor: owner.VendorID}
	if account.ProxyID != nil {
		domain.proxy = *account.ProxyID
	}
	b.failedDomains[owner] = domain
}

type turnRecoveryDomain struct{ vendor, proxy int64 }

// Correlation requires two independent owners. Prefer another domain only
// inside an existing priority/cost layer; this is not a global account ban.
func turnRecoveryDomainPenalty(ctx context.Context, account *Account) int {
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	vendor, proxy := 0, 0
	for _, domain := range b.failedDomains {
		if account.VendorID != nil && *account.VendorID > 0 && domain.vendor == *account.VendorID {
			vendor++
		}
		if account.ProxyID != nil && *account.ProxyID > 0 && domain.proxy == *account.ProxyID {
			proxy++
		}
	}
	if vendor >= 2 || proxy >= 2 {
		return 1
	}
	return 0
}

func requestDefinitelyUnsent(err error) bool {
	var attempt *OpenAITransportAttemptError
	return errors.As(err, &attempt) && attempt.Unsent
}

func vendorDispatchFailure(err error) error {
	if errors.Is(err, context.Canceled) || IsTurnBudgetError(err) {
		return err
	}
	return &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, Stage: "selection", Scope: GatewayFailureScopeProvider, Reason: "vendor_dispatch_unavailable", Cause: err, RequestNotSent: true, RequestScopedTransient: true, NextAccountAction: NextAccountRetry}
}

// MarkTurnOutputCommitted stops the shared first-output timer without imposing
// a deadline on a long response that has already started. Retry writer guards
// still forbid replacing any identity or content already sent to the client.
func MarkTurnOutputCommitted(ctx context.Context) {
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.outputCommitted {
			b.outputCommitted = true
			b.deadline = time.Time{}
			if b.outputReady != nil {
				close(b.outputReady)
			}
		}
	}
}

// Bind the native request through response-body close. Metadata alone does not
// disable its absolute first-output bound; the downstream commit does.
func guardTurnFirstOutput(req *http.Request) (*http.Request, context.CancelFunc) {
	b, _ := req.Context().Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return req, func() {}
	}
	b.mu.Lock()
	deadline, committed := b.deadline, b.outputCommitted
	if b.outputReady == nil {
		b.outputReady = make(chan struct{})
	}
	ready := b.outputReady
	b.mu.Unlock()
	if committed || deadline.IsZero() {
		return req, func() {}
	}
	ctx, cancel := context.WithCancelCause(req.Context())
	go func() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-ready:
		case <-ctx.Done():
		case <-timer.C:
			b.mu.Lock()
			committed := b.outputCommitted
			b.mu.Unlock()
			if !committed {
				cancel(ErrOpenAIWSFirstOutputBudgetExhausted)
			}
		}
	}()
	return req.WithContext(ctx), func() { cancel(context.Canceled) }
}

func newAccountConfigurationFailure(err error) error {
	var existing *UpstreamFailoverError
	if err == nil || errors.As(err, &existing) || IsTurnBudgetError(err) || errors.Is(err, context.Canceled) {
		return err
	}
	return &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeAccount, Reason: "account_configuration_invalid", Cause: err, RequestNotSent: true, NextAccountAction: NextAccountRetry, ClientMessage: "Upstream account configuration is unavailable"}
}

// Failed credential/configuration preparation may retry a target or move to a
// different owner, but either action spends the same small preparation budget.
func PrepareTurnRecovery(ctx context.Context, failure *UpstreamFailoverError) error {
	if failure == nil || !failure.IsCredentialFailure() || !failure.RequestNotSent || failure.RejectedBeforeGeneration {
		return nil
	}
	return TakeTurnPreparationRetry(ctx)
}

// planOpenAIWSTransportRecovery owns the native WS-to-HTTP decision and converts
// transport facts to the same failover errors used by every ingress controller.
// Adapters retain wire-specific response encoding; all sends spend turn budget.
func planOpenAIWSTransportRecovery(ctx context.Context, c *gin.Context, account *Account, body []byte, failure error) (bool, error) {
	if ctx.Err() != nil || TurnOutputCommitted(ctx) {
		return false, failure
	}
	var failover *UpstreamFailoverError
	if IsTurnBudgetError(failure) || errors.As(failure, &failover) {
		return false, failure
	}
	reason, _ := classifyOpenAIWSReconnectReason(failure)
	unsent := openAIWSFailureBeforeSend(reason)
	canHTTP := unsent && openAIWSRequestAllowsHTTP(account, body) && !openAIStreamClientOutputStarted(c, false)
	if !canHTTP {
		var rejectedGrant *openAIWSDialError
		if errors.As(failure, &rejectedGrant) && rejectedGrant.StatusCode == http.StatusUnauthorized && !openAIStreamClientOutputStarted(c, false) {
			return false, &UpstreamFailoverError{StatusCode: http.StatusUnauthorized, Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeAccount, Reason: "upstream_credentials_rejected", Cause: failure, RequestNotSent: true, RejectedBeforeGeneration: true}
		}
		if unsent && !openAIStreamClientOutputStarted(c, false) {
			return false, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, Cause: failure, RequestNotSent: true, Reason: "websocket_unavailable"}
		}
		if ctx.Err() == nil && !openAIStreamClientOutputStarted(c, false) && openAIWSUncertainExecutionReplaySafe(body) &&
			(reason == "read_event" || reason == openAIWSFailureWriteRequest || reason == "upstream_5xx") {
			return false, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, Cause: failure, Reason: "websocket_transport_exhausted"}
		}
		return false, failure
	}
	return true, nil
}

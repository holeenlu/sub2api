package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	gatewaymodel "github.com/Wei-Shaw/sub2api/internal/gateway/model"
	responseswire "github.com/Wei-Shaw/sub2api/internal/gateway/protocol/responses"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var excelBPSReplay basispoints.ReplayCache
var excelBPSCatalog basispoints.CatalogCache

// A resource-server rejection is not a revoked refresh grant. BPS changes only
// its own switch; primary and independent authorization share that rule.
func (s *OpenAIGatewayService) handleExcelBPSUnauthorized(ctx context.Context, account *Account, status int, headers http.Header, raw []byte, tokens ...string) {
	code := extractUpstreamErrorCode(raw)
	tokenRevoked := code == "token_revoked" || code == "token_invalidated" || code == "grant_revoked"
	if status != http.StatusUnauthorized && !tokenRevoked {
		return
	}

	if !account.IsExcelOAuth() && s.openAITokenProvider != nil && s.openAITokenProvider.openAIOAuthService != nil {
		token := ""
		if len(tokens) > 0 {
			token = tokens[0]
		}
		stateCtx, cancel := openAIAccountStateContext(ctx)
		defer cancel()
		err := s.openAITokenProvider.openAIOAuthService.rejectExcelAccessToken(stateCtx, account, token, s.openAITokenProvider.tokenCache, code)
		s.disableExcelBPSForAuthorizationFailure(ctx, account, err)
		return
	}

	if account.IsExcelOAuth() && (tokenRevoked || account.GetCredential("refresh_token") == "") {
		reason := "refresh_grant_missing"
		if tokenRevoked {
			reason = code
		}
		failure := excelAuthorizationUnavailable(reason, "", excelGrantReauthorizationRequired())
		failure.(*excelAuthorizationFailure).fact.GrantFingerprint = "primary"
		s.disableExcelBPSForAuthorizationFailure(ctx, account, failure)
	}
}

func (s *OpenAIGatewayService) moveExcelBPSOn403(ctx context.Context, account *Account) bool {
	target, enabled := account.ExcelBPS403GroupTarget()
	if !enabled {
		return false
	}
	repo, ok := s.accountRepo.(AccountExcelBPSGroupRepository)
	if !ok {
		return false
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	changed, err := repo.MoveExcelBPSOn403(stateCtx, account)
	if err != nil {
		logger.LegacyPrintf("service.openai_excel_bps", "automatic group action failed: account_id=%d error_type=%T", account.ID, err)
		return false
	}
	if changed {
		logger.LegacyPrintf("service.openai_excel_bps", "automatically updated groups after upstream HTTP 403: account_id=%d target_group_id=%d", account.ID, target)
	} else {
		// The guarded update matched nothing: settings, credentials or group
		// membership changed concurrently. Leave a trace so a real 403 that was
		// not acted on can be found without upstream bodies or query arguments.
		logger.LegacyPrintf("service.openai_excel_bps", "automatic group action skipped after upstream HTTP 403: account_id=%d target_group_id=%d reason=account_snapshot_changed", account.ID, target)
	}
	return changed
}

func (s *OpenAIGatewayService) disableExcelBPSOn403(ctx context.Context, account *Account, tokens ...string) bool {
	if !account.IsExcelBPSAutoDisableOn403Enabled() {
		return false
	}
	repo, ok := s.accountRepo.(AccountExcelBPSRepository)
	if !ok {
		return false
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	var changed bool
	var err error
	if failureRepo, ok := s.accountRepo.(AccountExcelBPSFailureRepository); ok && len(tokens) > 0 {
		fact, valid := s.excelBPSFailureForToken(stateCtx, account, tokens[0], "account_forbidden", false)
		if !valid {
			return false
		}
		changed, err = failureRepo.DisableExcelBPSForFailure(stateCtx, account, fact)
	} else {
		changed, err = repo.DisableExcelBPSOn403(stateCtx, account)
	}
	if err != nil {
		// Do not log upstream bodies, credentials or database query arguments.
		logger.LegacyPrintf("service.openai_excel_bps", "auto-disable failed: account_id=%d error_type=%T", account.ID, err)
		return false
	}
	if changed {
		recordExcelBPSRecoveryEvent("excel_bps.disabled_after_403", account.ID, map[string]any{"upstream_status_code": 403, "from": "enabled", "to": "disabled"})
	} else {
		logger.LegacyPrintf("service.openai_excel_bps", "auto-disable skipped after upstream HTTP 403: account_id=%d reason=account_snapshot_changed", account.ID)
	}
	return changed
}

func (s *OpenAIGatewayService) excelBPSImageRelay(ctx context.Context) (*basispoints.ImageRelay, error) {
	settings, err := s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.excelBPSImageRelayForSettings(settings)
}

func (s *OpenAIGatewayService) excelBPSImageRelayForSettings(settings ExcelBPSImageRelaySettings) (*basispoints.ImageRelay, error) {
	if !settings.Enabled || settings.Mode == ExcelBPSImageModeNative {
		if s != nil {
			s.excelBPSImagesMu.Lock()
			if s.excelBPSImages != nil {
				_ = s.excelBPSImages.Close()
				s.excelBPSImages = nil
			}
			s.excelBPSImagesMu.Unlock()
		}
		return nil, nil
	}
	var err error
	s.excelBPSImagesMu.Lock()
	defer s.excelBPSImagesMu.Unlock()
	if s.excelBPSImages == nil {
		dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
		if dataDir == "" {
			dataDir = "./data"
		}
		s.excelBPSImages, err = basispoints.NewImageRelay(settings.BaseURL, filepath.Join(dataDir, "bps-images"))

	}
	if err == nil {
		err = s.excelBPSImages.Configure(settings.BaseURL, settings.Limits)
	}
	return s.excelBPSImages, err
}

func (s *OpenAIGatewayService) CloseExcelBPSImages() error {
	if s == nil {
		return nil
	}
	s.excelBPSImagesMu.Lock()
	defer s.excelBPSImagesMu.Unlock()
	return s.excelBPSImages.Close()
}

// currentExcelBPSImageRelay returns the relay already configured by a
// forwarded request, without reading settings.
func (s *OpenAIGatewayService) currentExcelBPSImageRelay() *basispoints.ImageRelay {
	if s == nil {
		return nil
	}
	s.excelBPSImagesMu.Lock()
	defer s.excelBPSImagesMu.Unlock()
	return s.excelBPSImages
}

// ServeExcelBPSImage allows the upstream to retrieve an unguessable temporary URL.
// A settings read failure must not fail an in-flight generation whose images
// were already published: keep serving from the relay configured for them.
// A deliberate disable (relay nil without error) still answers 404.
func (s *OpenAIGatewayService) ServeExcelBPSImage(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	relay, err := s.excelBPSImageRelay(c.Request.Context())
	if err != nil {
		relay = s.currentExcelBPSImageRelay()
	}
	if relay == nil {
		http.NotFound(c.Writer, c.Request)
		return
	}
	relay.ServeHTTP(c.Writer, c.Request)
}

func excelBPSAccountID(account *Account, accessToken string) string {
	if accountID := strings.TrimSpace(account.GetChatGPTAccountID()); accountID != "" {
		return accountID
	}
	claims, err := openai.DecodeIDToken(accessToken)
	if err != nil || claims.OpenAIAuth == nil {
		return ""
	}
	return strings.TrimSpace(claims.OpenAIAuth.ChatGPTAccountID)
}

func newExcelBPSRequest(ctx context.Context, body []byte, token, accountID string) (*http.Request, error) {
	return newExcelBPSRequestTo(ctx, basispoints.ResponsesURL, "text/event-stream", body, token, accountID)
}

func newExcelBPSRequestTo(ctx context.Context, targetURL, accept string, body []byte, token, accountID string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Authorization": {"Bearer " + token}, "Chatgpt-Account-Id": {accountID}, "X-Openai-Account-Id": {accountID},
		"X-Basispoints-Auth-Mode": {"chatgpt"}, "Content-Type": {"application/json"}, "Accept": {accept},
		"Origin": {"https://bps.openai.com"}, "User-Agent": {"Mozilla/5.0"},
		"X-Openai-Internal-Basispoints-Client-Product":       {"basispoints-excel-plugin"},
		"X-Openai-Internal-Basispoints-Client-Agent-Profile": {"excel"},
	}
	return req, nil
}

// excelBPSEncryptedHistoryRecovery carries the reduced history after BPS
// refused ciphertext it cannot read before generation. Nothing was delivered,
// so forwardExcelBPS retries on the same account; it never reaches a caller.
type excelBPSEncryptedHistoryRecovery struct {
	body   []byte
	reason string
}

func (e *excelBPSEncryptedHistoryRecovery) Error() string {
	return "retry Excel BPS request after " + e.reason
}

type excelBPSEncryptedHistoryRecoveryDoneKey struct{}

// excelBPSMaxEncryptedHistoryRecoveries matches the two reductions: reasoning
// ciphertext, then compaction. Each is tried once.
const excelBPSMaxEncryptedHistoryRecoveries = 2

// forwardExcelBPS recovers a conversation whose history carries ciphertext
// another upstream or account issued, as native Responses does: each reduction
// is retried once on this account under the same turn budget.
func (s *OpenAIGatewayService) forwardExcelBPS(ctx context.Context, c *gin.Context, account *Account, body []byte, start time.Time, mappedModels ...string) (*OpenAIForwardResult, error) {
	ctx = EnsureOpenAITurnBudget(ctx, s.cfg, gjson.GetBytes(body, "reasoning.effort").String())
	pinExcelBPSExecution(ctx)
	// The WS BPS bridge returns before the Native HTTP attempt entry. Re-enter
	// the original root's finalization cycle after an authorized account retry,
	// including a credential failure that never creates a prepared attempt.
	if owner := nativeHTTPFromContext(ctx); owner != nil && owner.finalized && owner.dispatch.request.Transport == "ws" {
		if err := prepareNativeWSAttempt(ctx); err != nil {
			return nil, err
		}
	}
	for recoveries := 0; ; recoveries++ {
		attemptCtx := ctx
		if recoveries >= excelBPSMaxEncryptedHistoryRecoveries {
			attemptCtx = context.WithValue(ctx, excelBPSEncryptedHistoryRecoveryDoneKey{}, true)
		}
		result, err := s.forwardExcelBPSAttempt(attemptCtx, c, account, body, start, mappedModels...)
		var recovery *excelBPSEncryptedHistoryRecovery
		if !errors.As(err, &recovery) {
			return result, err
		}
		logger.LegacyPrintf("service.openai_excel_bps", "Retrying Excel BPS request after %s (account: %d)", recovery.reason, account.ID)
		body = recovery.body
	}
}

// excelBPSEncryptedHistoryRetry returns the recovery for a pre-generation
// refusal of unreadable history, or nil when none applies.
func excelBPSEncryptedHistoryRetry(ctx context.Context, originalBody, rejection []byte) error {
	if done, _ := ctx.Value(excelBPSEncryptedHistoryRecoveryDoneKey{}).(bool); done {
		return nil
	}
	retryBody, reason, changed, err := openAIEncryptedHistoryRecovery(originalBody, rejection)
	if err != nil || !changed {
		return nil
	}
	return &excelBPSEncryptedHistoryRecovery{body: retryBody, reason: reason}
}

// BPS deliberately bypasses Codex ticket/cookie injection and OAuth plugins:
// only the selected account's bearer and ChatGPT account ID belong on this host.
func (s *OpenAIGatewayService) forwardExcelBPSAttempt(ctx context.Context, c *gin.Context, account *Account, body []byte, start time.Time, mappedModels ...string) (forwardResult *OpenAIForwardResult, forwardErr error) {
	// WS/compat adapters accept normalized Responses events directly. Their
	// serialized output boundary owns write deadlines, cancellation and framing.
	frameWriter, _ := c.Writer.(*responsesEventWriter)
	eventWriter, emitsEvents := c.Writer.(interface{ WriteResponseEvent([]byte) error })
	timing := newOpenAITransportTiming(c, account, "http", int64(len(body)))
	timing.SetRequestMetadata(body)
	defer func() {
		if forwardResult != nil {
			forwardResult.LatencyMetrics = timing.UsageLatencyMetrics(forwardResult.Stream)
		}
		timing.Finish(forwardErr)
	}()
	if gjson.GetBytes(body, "stream").Bool() && !emitsEvents {
		defer GuardSSEWrites(c)()
	}
	if writeCtx := streamWriteContext(c); writeCtx != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		stop := context.AfterFunc(writeCtx, cancel)
		defer func() { stop(); cancel() }()
	}
	setActualOpenAIUsageAuthorizationMode(c, UsageAuthorizationModeBPS)
	parentCtx := ctx
	ctx = EnsureOpenAITurnBudget(ctx, s.cfg, gjson.GetBytes(body, "reasoning.effort").String())
	var firstOutputGuard *openAIFirstOutputHeaderGuard
	if timeout := s.openAIFirstOutputTimeout(gjson.GetBytes(body, "reasoning.effort").String()); timeout > 0 {
		ctx, firstOutputGuard = newOpenAIFirstOutputHeaderGuard(ctx, func() {}, openAITurnFirstOutputDeadline(ctx, timeout, start))
		defer firstOutputGuard.close()
	}
	firstOutputStopped := false
	stopFirstOutput := func() bool {
		if firstOutputGuard != nil && !firstOutputStopped {
			firstOutputStopped = true
			if firstOutputGuard.stopHeaderWait() {
				return false
			}
			MarkTurnOutputCommitted(ctx)
		}
		return true
	}
	c.Writer.Header().Del("X-Codex2API-Basispoints-Bypass")
	c.Writer.Header().Del("X-Codex2API-Upstream")
	var compactUsage OpenAIUsage
	var compactID string
	originalImagePolicyModel := gjson.GetBytes(body, "model").String()
	var compactEffort string
	defer func() {
		if compactID != "" && openAIUsageHasTokens(&compactUsage) {
			if forwardResult == nil {
				forwardResult = &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeBPS, Model: originalImagePolicyModel, ReasoningEffort: &compactEffort, UpstreamModel: gjson.GetBytes(body, "model").String(), UpstreamEndpoint: "/basispoints/api/responses", RequestID: compactID, Stream: gjson.GetBytes(body, "stream").Bool(), Duration: time.Since(start)}
			}
			addImagePolicyUsage(&forwardResult.Usage, compactUsage)
		}
		// Conversion/repair has joined before this defer. Freeze all observed
		// usage, including compaction, before the handler records the cyber event.
		if mark := GetOpsCyberPolicy(c); mark != nil && forwardResult != nil {
			mark.UpstreamInTok = forwardResult.Usage.InputTokens
			mark.UpstreamOutTok = forwardResult.Usage.OutputTokens
		}
	}()
	markOpenAIResponsesClientStream(c, gjson.GetBytes(body, "stream").Bool())
	failWithOrigin := func(status int, code, message string, dependency bool, param ...string) (*OpenAIForwardResult, error) {
		if parentCtx.Err() == nil && firstOutputGuard.expired() {
			dependency = false
			status, code, message = http.StatusGatewayTimeout, "first_output_timeout", "Excel BPS produced no semantic output before the deadline; the request was not replayed"
			RecordOpenAIRetryStop(c, account, OpenAIRetryStopTurnBudget)
			c.Set(OpsGatewayResponsesFailedKey, true)
		}
		if dependency {
			MarkOpsLocalDependencyFailure(c)
		}
		// A compact keepalive may already have committed SSE headers. Otherwise
		// finish a single JSON response so the handler cannot append another error.
		failureParam := ""
		if len(param) > 0 {
			failureParam = param[0]
		}
		committed := StopOpenAICompactSSEKeepaliveCommitted(c)
		MarkResponseCommitted(c)
		if committed {
			writeOpenAICompactSSEFailureMessageParam(c, status, code, message, failureParam)
		} else if code == "context_length_exceeded" && writeOpenAIContextWindowSSEFailure(c, status, message, failureParam) {
			// Streaming clients learn about a full context window from
			// response.failed, matching the BPS in-stream failure path and Codex.
		} else {
			errorType := "invalid_request_error"
			if status >= 500 {
				errorType = "server_error"
			}
			errorBody := gin.H{"type": errorType, "code": code, "message": message}
			if len(param) > 0 && param[0] != "" {
				errorBody["param"] = param[0]
			}
			c.JSON(status, gin.H{"error": errorBody})
		}
		return nil, &excelBPSForwardError{code: code, status: status, dependency: dependency}
	}
	fail := func(status int, code, message string, param ...string) (*OpenAIForwardResult, error) {
		return failWithOrigin(status, code, message, false, param...)
	}
	failLocal := func(status int, code, message string, param ...string) (*OpenAIForwardResult, error) {
		return failWithOrigin(status, code, message, true, param...)
	}
	originalBody := body // preserve client history before BPS attachment/tool rewrites
	failRequest := func(err error) (*OpenAIForwardResult, error) {
		if errors.Is(err, basispoints.ErrStateUnavailable) || errors.Is(err, basispoints.ErrStateCapacity) {
			code := "basispoints_state_unavailable"
			if errors.Is(err, basispoints.ErrStateCapacity) {
				code = "basispoints_state_capacity"
			}
			result, failure := failLocal(503, code, err.Error())
			return result, errors.Join(failure, err)
		}
		if errors.Is(err, basispoints.ErrPreviousResponseNotFound) {
			result, failure := fail(http.StatusBadRequest, "previous_response_not_found", err.Error(), "previous_response_id")
			var rejection *excelBPSForwardError
			if errors.As(failure, &rejection) && rejection.code == "previous_response_not_found" {
				return result, errors.Join(failure, err)
			}
			return result, failure
		}
		message, param := err.Error(), ""
		if errors.Is(err, basispoints.ErrUnsupportedToolChoice) {
			param = "tool_choice"

		}
		var contentErr *basispoints.ContentValidationError
		if errors.As(err, &contentErr) {
			param = contentErr.Path
			if contentErr.ContentType == "encrypted_content" {
				// Report the effective option on this selected account, not a global
				// default. Do not infer the ciphertext's origin or log its payload.
				message += fmt.Sprintf("; account setting %s=%t (history omission does not decrypt content or recover a new encrypted task)", ExcelBPSIgnoreEncryptedContentKey, account.IsExcelBPSIgnoreEncryptedContentEnabled())
			}
		}
		return fail(http.StatusBadRequest, "basispoints_request_invalid", message, param)
	}
	originalModel := gjson.GetBytes(body, "model").String()
	model := account.GetMappedModel(originalModel)
	if len(mappedModels) > 0 {
		model = mappedModels[0]
	}
	ctx = withExcelBPSUpstreamModel(ctx, model)
	stream := gjson.GetBytes(body, "stream").Bool()
	clientCanceled := func() (*OpenAIForwardResult, error) {
		StopOpenAICompactSSEKeepaliveCommitted(c)
		MarkResponseCommitted(c)
		MarkOpsClientCancellation(c, stream)
		// No response or metered usage exists before headers; do not create a usage row.
		return nil, context.Canceled
	}
	{
		until, err := s.excelBPSRateLimitDeadline(ctx, account.ID, excelBPSRateLimitScope{model: model, endpoint: basispoints.ResponsesURL})
		if err != nil {
			return nil, newExcelBPSStateUnavailable(err, true)
		}
		if remaining := time.Until(until); remaining > 0 {
			if isExcelBPSClientCancellation(ctx, c, ctx.Err()) {
				return clientCanceled()
			}
			seconds := int64(remaining / time.Second)
			if remaining%time.Second != 0 {
				seconds++
			}
			failure := newExcelBPSRateLimitedFailoverError(fmt.Sprint(seconds))
			failure.RequestNotSent = true // local cooldown: no upload or inference attempted
			return nil, failure
		}
	}
	modelUntil, modelErr := s.excelBPSModelCooldownDeadline(ctx, account.ID, model)
	if modelErr != nil {
		return nil, newExcelBPSStateUnavailable(modelErr, true)
	}
	if time.Now().Before(modelUntil) {
		if ctx.Err() != nil {
			return clientCanceled()
		}
		failure := newExcelBPSModelPermissionFailoverError()
		failure.RequestNotSent = true
		return nil, failure
	}
	// No output exists yet, so the handler may replay the request on another
	// account within its switch budget unless the client is already gone.
	failoverRateLimited := func(retryAfter string) (*OpenAIForwardResult, error) {
		if isExcelBPSClientCancellation(ctx, c, ctx.Err()) {
			return clientCanceled()
		}
		return nil, newExcelBPSRateLimitedFailoverError(retryAfter)
	}
	// Recover only an exact, authenticated BPS response snapshot before size,
	// replay-safety, image and tool checks see the effective complete request.
	restoredBody, restoreErr := s.restoreExcelBPSContinuation(ctx, c, account, body)
	if restoreErr != nil {
		if ctx.Err() != nil {
			MarkOpsClientCancellation(c, gjson.GetBytes(body, "stream").Bool())
			return nil, ctx.Err()
		}
		if !errors.Is(restoreErr, basispoints.ErrPreviousResponseNotFound) {
			c.Set(OpsUpstreamStatusCodeKey, 0)
			c.Set(OpsUpstreamErrorMessageKey, "")
			c.Set(OpsUpstreamErrorDetailKey, "")
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: "bridge_error", Scope: "excel_bps", Stage: "history_restore", Reason: "state_unavailable", AccountID: account.ID})
			result, failure := failLocal(http.StatusServiceUnavailable, "basispoints_state_unavailable", "Excel BPS continuation state is temporarily unavailable; retry without changing the conversation")
			return result, errors.Join(failure, restoreErr)
		}
		return failRequest(restoreErr)
	}
	body, originalBody = restoredBody, restoredBody
	if next, changed, err := normalizeOpenAIReadableCompactionBody(body); err != nil {
		var invalid *openAIResponsesInputError
		if errors.As(err, &invalid) {
			return fail(http.StatusBadRequest, invalid.Code, invalid.Message, invalid.Param)
		}
		return failRequest(err)
	} else if changed {
		body = next
	}
	setOpenAIReplaySafety(c, body)
	imageSettings, err := s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		if isExcelBPSClientCancellation(ctx, c, err) {
			return clientCanceled()
		}
		return failLocal(503, "basispoints_image_settings_unavailable", "Excel BPS image settings are unavailable")
	}
	// Reserve only after the BPS route is selected, before rewriting or
	// converting the body. No upstream work or semantic output exists yet.
	releaseImageAdmission, err := s.admitExcelBPSImageRequest(c, imageSettings, len(body))
	if err != nil {
		return nil, err
	}
	defer releaseImageAdmission()
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return fail(400, "basispoints_request_invalid", "Invalid model request")
	}
	body, err = s.applyOpenAIFastPolicyToBody(ctx, account, model, body)
	if err != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(err, &blocked) {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			return fail(http.StatusForbidden, "permission_error", blocked.Message)
		}
		return fail(http.StatusBadRequest, "basispoints_request_invalid", err.Error())
	}
	identity, transient := resolveExcelBPSIdentity(c, body, getAPIKeyIDFromContext(c), false)
	if identity != "" {
		body, err = sjson.SetBytes(body, "prompt_cache_key", identity)
		if err != nil {
			return nil, err
		}
	}
	if isOpenAIResponsesCompactPath(c) {
		var request map[string]any
		if err = decodeJSONUseNumber(body, &request); err != nil {
			return fail(400, "basispoints_request_invalid", "Invalid compact request")
		}
		var input []any
		switch v := request["input"].(type) {
		case []any:
			input = v
		case string:
			input = []any{map[string]any{"role": "user", "content": v}}
		default:
			return fail(400, "basispoints_request_invalid", "Compact requires input")
		}
		request["input"] = append(input, map[string]any{"type": "compaction_trigger"})
		request["tool_choice"] = "none"
		body, err = json.Marshal(request)
		if err != nil {
			return nil, err
		}
	}
	scope := fmt.Sprintf("account:%d/key:%d/thread:%s", account.ID, getAPIKeyIDFromContext(c), identity)
	// Local rows can be reauthorized for a different upstream user/workspace.
	// Reuse the native stable credential namespace, never the rotating AT.
	if credentialScope := codexAccountIdentityNamespace(account); credentialScope != "" {
		scope += "/credential:" + gatewayCacheIdentity(0, credentialScope)
	}
	if transient {
		scope = "transient:" + scope
	}
	if err = basispoints.ValidateNewAgentMessage(body); err != nil {
		return failRequest(err)
	}
	if account.IsExcelBPSIgnoreEncryptedContentEnabled() {
		body, err = basispoints.StripEncryptedContent(body)
		if err != nil {
			return failRequest(err)
		}
	}
	imagePolicy, err := s.prepareExcelImagePolicy(ctx, c, body, imageSettings, scope+"/model:"+model, identity != "" && !transient)
	if err != nil {
		var policyErr *excelImagePolicyError
		if errors.As(err, &policyErr) {
			detail, _ := json.Marshal(policyErr.diagnostics)
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Kind: "request_error", Scope: "excel_bps", Stage: "image_preflight", Reason: policyErr.Code,
				Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
				ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
				Message: policyErr.Message, Detail: string(detail),
			})
			return fail(policyErr.Status, policyErr.Code, policyErr.Message)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	if len(imagePolicy.reconciledBody) > 0 {
		maxImageMiB, maxTotalMiB := imageSettings.Limits.MaxImageMiB, imageSettings.Limits.MaxTotalMiB
		if imageSettings.Mode == ExcelBPSImageModeNative {
			// Authenticated checkpoints may remove an old prefix larger than the
			// active image window. Raw input remains bounded by ingress admission.
			maxImageMiB, maxTotalMiB = basispoints.NativeImageMaxMiB, imageSettings.BodyLimitMiB
		}
		if err = basispoints.ValidateImageBudget(body, maxImageMiB, maxTotalMiB); err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
		body = imagePolicy.reconciledBody
	}
	var compactOutput []any
	var compactUsageWire map[string]any
	var images *basispoints.NativeImages
	if imageSettings.Enabled && imageSettings.Mode == ExcelBPSImageModeNative {
		if imagePolicy.split > 0 {
			images, err = basispoints.PrepareNativeImagesForCompaction(body, imageSettings.Limits.MaxImages, imagePolicy.split)
		} else {
			images, err = basispoints.PrepareNativeImagesWithLimit(body, imagePolicy.maxImages)
		}
		if err == nil {
			body, err = images.Body()
		}
	} else {
		var relay *basispoints.ImageRelay
		relay, err = s.excelBPSImageRelayForSettings(imageSettings)
		if err != nil {
			return failLocal(503, "basispoints_image_relay_unavailable", "Excel BPS image relay is unavailable")
		}
		body, err = relay.RewriteWithImageLimit(body, scope, imagePolicy.maxImages)
	}
	if err != nil {
		if errors.Is(err, basispoints.ErrImageRelayFull) {
			return failLocal(503, "basispoints_image_relay_full", err.Error())
		}
		if errors.Is(err, basispoints.ErrImageRelayStorage) {
			return failLocal(503, "basispoints_image_relay_unavailable", err.Error())
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	// Validate the complete request before uploading any attachments. The native
	// plan contains valid placeholder IDs until all local protocol checks pass.
	var replay *basispoints.ReplayCache
	var catalog *basispoints.CatalogCache
	if identity != "" {
		replay, catalog = &excelBPSReplay, &excelBPSCatalog
		if s.cfg != nil && s.cfg.Gateway.ExcelBPSStateRedisEnabled && !transient {
			store, ok := s.cache.(basispoints.StateStore)
			if !ok {
				return failLocal(503, "basispoints_state_unavailable", basispoints.ErrStateUnavailable.Error())
			}
			replay, catalog = basispoints.NewPersistentCaches(ctx, store)
		}
	}
	var upstreamBody []byte
	var bridge *basispoints.Bridge
	if images != nil {
		upstreamBody, bridge, err = images.PrepareWithCatalog(scope, replay, catalog)
	} else {
		upstreamBody, bridge, err = basispoints.PrepareWithCatalog(body, scope, replay, catalog)
	}
	if err != nil {
		return failRequest(err)
	}
	if reason := account.excelBPSUnsupportedToolsReason(body); reason != "" {
		return fail(400, "basispoints_unsupported_tools", "Excel BPS cannot execute requested hosted tools; explicitly allow omission or select a native model", "tools")
	}
	token, preparedAccount, err := s.getExcelBPSAccessTokenSnapshot(ctx, account)
	if err != nil {
		if isExcelBPSClientCancellation(ctx, c, err) {
			return clientCanceled()
		}
		return nil, newExcelBPSCredentialFailure(c, account, err)
	}
	account = preparedAccount
	ctx, err = s.ExcelBPSDispatchContext(ctx, account, token)
	if err != nil {
		return nil, newExcelBPSCredentialFailure(c, account, err)
	}
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return fail(400, "basispoints_account_id_missing", "Excel BPS requires chatgpt_account_id")
	}

	attachmentProxy := ""
	if account.Proxy != nil {
		attachmentProxy = account.Proxy.URL()
	}

	compactionCompleted := false
	if imagePolicy.split > 0 {
		logger.LegacyPrintf("service.openai_excel_bps", "image policy auto compact preflight: account_id=%d history_images=%d new_images=%d limit=%d history_bytes=%d new_bytes=%d", account.ID, basispoints.CountInlineImages(imagePolicy.history.Input[:imagePolicy.split]), basispoints.CountInlineImages(imagePolicy.history.Input[imagePolicy.split:]), imageSettings.Limits.MaxImages, imagePolicy.historyBytes, imagePolicy.addedBytes)
	}
	// Reuse attachment IDs across requests of the same trusted conversation,
	// account, API key, stable credential and workspace (2026-10-24). Transient
	// scopes have no trusted conversation identity and always upload afresh.
	if images != nil && identity != "" && !transient {
		images.UseCache(&s.excelBPSAttachmentCache, scope+"/workspace:"+accountID)
	}
	attachmentUpload := s.excelBPSAttachmentUpload(c, account, token, accountID, attachmentProxy, c.GetBool(OpenAIAccountReplayAllowedContextKey), excelBPSReplayableEncryptedHistory(originalBody), &compactionCompleted)
	uploadImages := func() (*OpenAIForwardResult, error) {
		// Attachment uploads have their own deadline and do not consume the
		// first-output budget, here or after a later account switch.
		resumeFirstOutput := func() {}
		if firstOutputGuard.pause(excelBPSAttachmentPhaseTimeout) {
			resumeBudget := pauseOpenAITurnFirstOutput(ctx, excelBPSAttachmentPhaseTimeout)
			resumeFirstOutput = func() { resumeBudget(); firstOutputGuard.resume() }
		}
		uploadCtx, cancelUpload := context.WithTimeoutCause(ctx, excelBPSAttachmentPhaseTimeout, errExcelBPSAttachmentPhaseTimeout)
		body, err = images.Upload(uploadCtx, &s.excelBPSAttachments, attachmentUpload)
		phaseExpired := errors.Is(context.Cause(uploadCtx), errExcelBPSAttachmentPhaseTimeout)
		cancelUpload()
		resumeFirstOutput()
		if err != nil {
			if isExcelBPSClientCancellation(ctx, c, err) {
				return clientCanceled()
			}
			if ctx.Err() == nil && (phaseExpired || isExcelBPSAttachmentTimeout(err)) {
				recordExcelBPSAttachmentFailure(ctx, c, account, err, "", 0)
				return fail(http.StatusGatewayTimeout, "attachment_timeout", fmt.Sprintf("Excel BPS attachment upload did not finish in time (phase limit %ds); request was not replayed and account scheduling was not changed", int(excelBPSAttachmentPhaseTimeout/time.Second)))
			}
			var admission *UpstreamFailoverError
			if errors.As(err, &admission) && (admission.Stage == "selection" || admission.RequestNotSent || admission.Reason == "vendor_dispatch_unavailable") {
				recordExcelBPSCredentialFailure(c, account, admission)
				if compactionCompleted {
					copy := *admission
					copy.RequestNotSent, copy.NextAccountAction = false, NextAccountStop
					return nil, &copy
				}
				return nil, err
			}
			status, code := http.StatusBadGateway, "basispoints_attachment_error"
			var uploadError *excelBPSAttachmentError
			if errors.As(err, &uploadError) {
				status = uploadError.status
			}
			if uploadError != nil && uploadError.kind == "content_refusal" {
				recordExcelBPSAttachmentFailure(ctx, c, account, err, "", 0)
				return fail(excelBPSUpstreamAuthClientStatus(status), "content_policy_violation", "Excel BPS refused this attachment; request was not replayed")
			}
			if errors.Is(err, basispoints.ErrAttachmentBusy) {
				recordExcelBPSAttachmentFailure(ctx, c, account, err, "", 0)
				return failLocal(503, "basispoints_attachment_capacity", "Excel BPS attachment upload capacity is busy; retry later")
			}
			if status == http.StatusTooManyRequests && uploadError != nil && !uploadError.usageObserved {
				recordExcelBPSAttachmentFailure(ctx, c, account, err, "failover", 0)
				result, failure := failoverRateLimited(uploadError.retryAfter)
				if failover, ok := failure.(*UpstreamFailoverError); ok {
					// Preserve the existing original-context recovery checks after compact.
					failover.RequestNotSent = !compactionCompleted
				}
				return result, failure
			}
			if status == http.StatusForbidden && uploadError != nil && uploadError.stage == "attachment_http" && !uploadError.usageObserved && !compactionCompleted {
				recordExcelBPSAttachmentFailure(ctx, c, account, err, "failover", 0)
				failure := excelBPSForbiddenFailover(nil)
				failure.RequestNotSent = true // the rejected upload never sent inference
				return nil, failure
			}
			// An explicit transient attachment HTTP rejection precedes inference.
			// Reuse the normal recovery budget; malformed responses, transport
			// uncertainty and post-compaction failures retain their existing stop.
			if uploadError != nil && uploadError.stage == "attachment_http" && !uploadError.usageObserved && !compactionCompleted &&
				(uploadError.upstreamStatus == http.StatusInternalServerError || uploadError.upstreamStatus == http.StatusBadGateway || uploadError.upstreamStatus == http.StatusServiceUnavailable || uploadError.upstreamStatus == http.StatusGatewayTimeout) &&
				OpenAIResponsesAccountReplayAllowed(originalBody) && openAIWSUncertainExecutionReplaySafe(originalBody) {
				recordExcelBPSAttachmentFailure(ctx, c, account, err, "failover", 0)
				failure := newExcelBPSUpstreamFailureError(status)
				failure.RequestNotSent = true
				return nil, failure
			}
			recordExcelBPSAttachmentFailure(ctx, c, account, err, "", 0)
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				return fail(excelBPSUpstreamAuthClientStatus(status), code, "Excel BPS attachment authentication failed; request was not replayed")
			}
			return fail(status, code, "Excel BPS attachment upload failed; request was not replayed and account scheduling was not changed")
		}
		upstreamBody, bridge, err = bridge.Reprepare(body)
		if err != nil {
			return failRequest(err)
		}
		return nil, nil
	}
	if images != nil && images.HasImages() {
		if result, uploadErr := uploadImages(); uploadErr != nil {
			return result, uploadErr
		}
	}
	// Ownership recovery is limited to original managed image data, before any
	// visible output or usage. Never move an opaque file reference to an account.
	fileOwnershipFailed := false
	cacheEntriesInvalidated := 0
	observeFileOwnershipFailure := func(failure *basispoints.UpstreamFailure) bool {
		if failure == nil || failure.Status < 500 || failure.Code != basispoints.FileOwnershipFailureCode {
			return false
		}
		fileOwnershipFailed = true
		return true
	}
	invalidateOwnershipCache := func() {
		if !fileOwnershipFailed || images == nil {
			return
		}
		cacheEntriesInvalidated += images.InvalidateAttachmentCache()
	}
	defer invalidateOwnershipCache()
	recordFailure := func(resp *http.Response, raw []byte, kind, fallback string, wire []byte, stage string) {
		var fileContext *excelBPSFileOwnershipContext
		if fileOwnershipFailed {
			// Count references before the request's cached IDs are invalidated.
			references := images.FileReferences(wire)
			invalidateOwnershipCache()
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				fileContext = &excelBPSFileOwnershipContext{
					FileReferenceCounts:     references,
					Stage:                   stage,
					CompactionCompleted:     compactionCompleted,
					CacheEntriesInvalidated: cacheEntriesInvalidated,
				}
			}
		}
		s.recordExcelBPSUpstreamFailure(c, account, resp, raw, token, kind, fallback, fileContext)
	}
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileExcelBPS))

	if imagePolicy.split > 0 {
		uploaded, inspectErr := basispoints.InspectImageHistory(body)
		if inspectErr != nil {
			return fail(400, "basispoints_request_invalid", inspectErr.Error())
		}
		compactBody, buildErr := uploaded.WithInput(uploaded.Input[:imagePolicy.split], true)
		if buildErr != nil {
			return fail(400, "basispoints_request_invalid", "Could not prepare image compaction")
		}
		compactWire, _, buildErr := bridge.Reprepare(compactBody)
		if buildErr != nil {
			return failRequest(buildErr)
		}
		SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
		cr, _, compactErr := s.doExcelBPSRequest(withResponseBPSRole(requestCtx, gatewaymodel.AttemptCompact), c, account, scope, compactWire, token, accountID)

		if compactErr != nil {
			// Nothing has been generated: a compaction transport failure follows the
			// same pre-generation recovery as a failed generation request.
			if isExcelBPSClientCancellation(ctx, c, compactErr) {
				return clientCanceled()
			}
			if errors.Is(compactErr, ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(compactErr, ErrOpenAIWSFirstOutputBudgetExhausted) || IsTurnBudgetError(compactErr) {
				return nil, compactErr
			}
			var compactRecovery *UpstreamFailoverError
			if errors.As(compactErr, &compactRecovery) {
				return nil, compactErr
			}
			var compactAttempt *OpenAITransportAttemptError
			if errors.As(compactErr, &compactAttempt) && compactAttempt.SendState == gatewaymodel.SendNotSent || openAIWSUncertainExecutionReplaySafe(originalBody) {
				failure := newExcelBPSUpstreamFailureError(http.StatusBadGateway)
				failure.Cause = compactErr
				failure.RequestNotSent = compactAttempt != nil && compactAttempt.SendState == gatewaymodel.SendNotSent
				failure.RejectedBeforeGeneration = true
				return nil, failure
			}
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction connection failed; generation was not started")
		}
		compactID = cr.Header.Get("x-request-id")
		if compactID == "" && responseBPSFromContext(ctx) == nil {
			compactID = "bps-compaction-" + uuid.NewString()
		}
		if cr.StatusCode < 200 || cr.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(cr.Body, 512<<10))
			_ = cr.Body.Close()
			cyber, _, _ := detectOpenAICyberPolicy(raw)
			if IsUpstreamContentRefusal(raw) && !cyber {
				recordFailure(cr, raw, "http_error", "Excel BPS refused image compaction", compactWire, "image_compaction")
				return fail(excelBPSUpstreamAuthClientStatus(cr.StatusCode), "content_policy_violation", "Excel BPS refused image compaction; request was not replayed")
			}
			if failure := basispoints.ParseHTTPFailure(cr.StatusCode, raw); failure != nil && (observeFileOwnershipFailure(failure) || cyber) {
				recordFailure(cr, raw, "http_error", failure.Message, compactWire, "image_compaction")
				return fail(failure.Status, failure.Code, failure.Message)
			}
			s.handleExcelBPSUnauthorized(ctx, account, cr.StatusCode, cr.Header, raw, token)
			if cr.StatusCode == http.StatusTooManyRequests {
				if err := s.observeExcelBPSRateLimit(ctx, account, cr.StatusCode, cr.Header, raw, model, basispoints.ResponsesURL); err != nil {
					return nil, newExcelBPSStateUnavailable(err, false)
				}
				recordFailure(cr, raw, "http_error", excelBPSRateLimitedClientMessage, compactWire, "image_compaction")
				return failoverRateLimited(cr.Header.Get("Retry-After"))
			}
			if isExcelBPSModelPermissionError(cr.StatusCode, gjson.GetBytes(raw, "error.code").String()) {
				s.coolDownExcelBPSModel(account, model)
				recordFailure(cr, raw, "failover", excelBPSModelPermissionClientMessage, compactWire, "image_compaction")
				failure := newExcelBPSModelPermissionFailoverError()
				failure.RejectedBeforeGeneration = !gjson.GetBytes(raw, "usage").Exists() && !gjson.GetBytes(raw, "response.usage").Exists()
				return nil, failure
			}
			if cr.StatusCode == http.StatusUnauthorized && openAIWSUncertainExecutionReplaySafe(originalBody) {
				// Same contract as a generation 401 before output: refresh the Excel
				// grant once on this account, otherwise let the handler switch.
				recordFailure(cr, raw, "failover", "Excel BPS authentication failed during compaction", compactWire, "image_compaction")
				failure := newExcelBPSUpstreamFailureError(http.StatusUnauthorized)
				failure.Stage, failure.Reason = GatewayFailureStageAccountAuth, "basispoints_credentials_rejected"
				failure.RejectedBeforeGeneration = true
				failure.RetryableOnSameAccount = !account.IsExcelOAuth() && takeTurnGrantRefresh(ctx, account, "excel", token) == nil
				failure.SameAccountRetryMax = 1
				return nil, failure
			}
			if excelBPSAccountForbidden(cr.StatusCode, raw) {
				s.moveExcelBPSOn403(ctx, account)
				s.disableExcelBPSOn403(ctx, account, token)
			}
			if cr.StatusCode == http.StatusForbidden {
				if failure := excelBPSForbiddenFailover(raw); failure != nil {
					recordFailure(cr, raw, "failover", "Excel BPS compaction access rejected", compactWire, "image_compaction")
					return nil, failure
				}
			}
			if cr.StatusCode >= 500 && ctx.Err() == nil && !isOpenAIEncryptedContextRejection(raw) && openAIWSUncertainExecutionReplaySafe(originalBody) {
				// Compaction never started generation; a 5xx here is as recoverable
				// as a 5xx on the generation request.
				recordFailure(cr, raw, "failover", fmt.Sprintf("Excel BPS compaction returned HTTP %d", cr.StatusCode), compactWire, "image_compaction")
				failure := newExcelBPSUpstreamFailureError(cr.StatusCode)
				failure.RejectedBeforeGeneration = true
				return nil, failure
			}
			return fail(excelBPSUpstreamAuthClientStatus(cr.StatusCode), "basispoints_image_compaction_failed", "Image history compaction was rejected; generation was not started")
		}
		s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, cr.Header)
		compactEffort = bridge.Effort
		var compactStateErr error
		compactContentRefused := false
		compactResponse, compactErr := basispoints.ReadImageCompaction(cr.Body, func(payload []byte) {
			compactStateErr = errors.Join(compactStateErr, observeResponseBPSEvent(requestCtx, cr, payload))
			s.parseSSEUsageBytes(payload, &compactUsage)
			compactContentRefused = compactContentRefused || IsUpstreamContentRefusal(payload)
			if !compactContentRefused {
				compactStateErr = errors.Join(compactStateErr, s.observeExcelBPSRateLimit(ctx, account, 0, cr.Header, payload, model, basispoints.ResponsesURL))
			}
		})
		_ = cr.Body.Close()
		if compactStateErr != nil {
			return nil, newExcelBPSStateUnavailable(compactStateErr, false)
		}
		if compactResponse != nil {
			if compactID == "" {
				compactID, _ = compactResponse["id"].(string)
			}
			encoded, _ := json.Marshal(map[string]any{"type": "response.completed", "response": compactResponse})
			s.parseSSEUsageBytes(encoded, &compactUsage)
			compactUsageWire, _ = compactResponse["usage"].(map[string]any)
		}
		if compactErr != nil {
			var compactFailure *basispoints.UpstreamFailure
			if errors.As(compactErr, &compactFailure) {
				cyber, _, _ := detectOpenAICyberPolicy(compactFailure.Diagnostic)
				if compactContentRefused && !cyber {
					recordFailure(cr, compactFailure.Diagnostic, "stream_error", "Excel BPS refused image compaction", compactWire, "image_compaction")
					return fail(excelBPSUpstreamAuthClientStatus(compactFailure.Status), "content_policy_violation", "Excel BPS refused image compaction; request was not replayed")
				}
				if observeFileOwnershipFailure(compactFailure) || cyber {
					recordFailure(cr, compactFailure.Diagnostic, "stream_error", compactFailure.Message, compactWire, "image_compaction")
					return fail(compactFailure.Status, compactFailure.Code, compactFailure.Message)
				}
				s.observeExcelBPSTerminalAuthorization(ctx, account, compactFailure, cr.Header, token, model)
				if compactFailure.Status == http.StatusTooManyRequests {
					retryAfter := cr.Header.Get("Retry-After")
					recordFailure(cr, compactFailure.Diagnostic, "stream_error", compactFailure.Message, compactWire, "image_compaction")
					if openAIWSUncertainExecutionReplaySafe(body) {
						return failoverRateLimited(retryAfter)
					}
					// The context cannot move to another account: answer as the rate
					// limit it is, with the upstream Retry-After, instead of a 502.
					if _, valid := excelBPSRetryAfter(retryAfter, time.Now()); valid {
						c.Header("Retry-After", strings.TrimSpace(retryAfter))
					}
					return fail(http.StatusTooManyRequests, string(ExcelBPSRateLimitedReason), excelBPSRateLimitedClientMessage)
				}
			}
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction did not complete; generation was not started")
		}
		window, _, compactErr := basispoints.CompactWindow(compactResponse)
		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", compactErr.Error())
		}
		compactionCompleted = true
		tail := uploaded.Input[imagePolicy.split:]
		if images != nil {
			// The first upload intentionally left the new images untouched. Use
			// their originals, not preflight placeholders, for the continuation.
			tail = imagePolicy.history.Input[imagePolicy.split:]
		}
		next := append(append([]any{}, window...), tail...)
		// Count retained inline tool images, not the uploaded attachment placeholders.
		if basispoints.CountInlineImages(window)+basispoints.CountInlineImages(imagePolicy.history.Input[imagePolicy.split:]) > imageSettings.Limits.MaxImages {
			return fail(400, "basispoints_image_compaction_insufficient", "Compacted history still exceeds the image limit; reduce new images or compact manually")
		}
		body, compactErr = uploaded.WithInput(next, false)
		if compactErr != nil {
			return fail(400, "basispoints_request_invalid", "Could not prepare compacted continuation")
		}
		if images != nil {
			images, compactErr = images.Continue(body, imageSettings.Limits.MaxImages)
			if compactErr != nil {
				return fail(400, "basispoints_image_compaction_insufficient", "Compacted history plus new images still exceeds native image limits or contains invalid images; reduce image sizes or start a new conversation")
			}
			body, compactErr = images.Body()
			if compactErr != nil {
				return fail(400, "basispoints_request_invalid", "Could not prepare compacted image continuation")
			}
		}
		upstreamBody, bridge, compactErr = bridge.Reprepare(body)
		if compactErr != nil {
			return failRequest(compactErr)
		}
		if images != nil && images.HasImages() {
			if result, uploadErr := uploadImages(); uploadErr != nil {
				return result, uploadErr
			}
		}
		if compactErr = imagePolicy.checkpoint(ctx, window); compactErr != nil {
			return failLocal(503, "basispoints_image_checkpoint_unavailable", "Could not save compacted history safely; retry or run compact manually")
		}
		compactOutput = window

	}
	SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
	SetOpsUpstreamModel(c, model)
	sent := time.Now()
	generationRole := gatewaymodel.AttemptGenerate
	if imagePolicy.compact {
		generationRole = gatewaymodel.AttemptCompact
	}
	resp, _, err := s.doExcelBPSRequest(withResponseBPSRole(requestCtx, generationRole), c, account, scope, upstreamBody, token, accountID)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
	if err != nil {
		if parentCtx.Err() == nil && firstOutputGuard.expired() {
			return fail(504, "first_output_timeout", "Excel BPS produced no output before the deadline")
		}
		if errors.Is(err, ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) {
			return nil, err
		}
		if isExcelBPSClientCancellation(ctx, c, err) {
			return clientCanceled()
		}
		var attempt *OpenAITransportAttemptError
		var recovery *UpstreamFailoverError
		if IsTurnBudgetError(err) || errors.As(err, &recovery) {
			return nil, err
		}
		if errors.As(err, &attempt) && attempt.SendState == gatewaymodel.SendNotSent && !compactionCompleted || (!compactionCompleted && openAIWSUncertainExecutionReplaySafe(originalBody)) {
			failure := newExcelBPSUpstreamFailureError(http.StatusBadGateway)
			failure.Cause = err
			failure.RequestNotSent = attempt != nil && attempt.SendState == gatewaymodel.SendNotSent
			failure.RetryableOnSameAccount, failure.SameAccountRetryMax = true, 1
			return nil, failure
		}
		return fail(502, "basispoints_transport_error", "Excel BPS connection failed; request was not replayed after sending")
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		observeBPSTestResponse(ctx, resp, model, token, "")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {

		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		observeBPSTestResponse(ctx, resp, model, token, gjson.GetBytes(raw, "error.code").String())
		// BPS throttles its own endpoint. A BPS 429 must not write Codex
		// quota/cooldown state; it cools only the BPS route and fails over.
		// Preserve the original rejection for Ops without exposing it to clients.
		// BPS errors can echo request fields, so redact before storing diagnostics.
		// A failover attempt is only an event; the handler records the final state.
		code := gjson.GetBytes(raw, "error.code").String()
		modelDenied := isExcelBPSModelPermissionError(resp.StatusCode, code)
		failure := basispoints.ParseHTTPFailure(resp.StatusCode, raw)
		cyber, _, _ := detectOpenAICyberPolicy(raw)
		if IsUpstreamContentRefusal(raw) && !cyber {
			recordFailure(resp, raw, "http_error", "Excel BPS refused this request", upstreamBody, "generation")
			return fail(excelBPSUpstreamAuthClientStatus(resp.StatusCode), "content_policy_violation", "Excel BPS refused this request; request was not replayed")
		}
		reportedUsage := gjson.GetBytes(raw, "usage").Exists() || gjson.GetBytes(raw, "response.usage").Exists()
		fileOwnershipFailed = observeFileOwnershipFailure(failure)
		// Snapshot eligibility before diagnostic recording invalidates the IDs.
		retryOwnership := fileOwnershipFailed && !gjson.GetBytes(raw, "usage").Exists() && !gjson.GetBytes(raw, "response.usage").Exists() && canRetryExcelBPSAttachmentOwnership(images, upstreamBody, originalBody, compactionCompleted)
		replayBlocker := ""
		if resp.StatusCode >= 500 {
			replayBlocker = openAIWSUncertainExecutionReplayBlocker(body)
		}
		retryServerError := !cyber && resp.StatusCode >= 500 && !fileOwnershipFailed &&
			!isOpenAIEncryptedContextRejection(raw) && replayBlocker == ""
		var forbiddenFailure *UpstreamFailoverError
		if resp.StatusCode == http.StatusForbidden && !compactionCompleted {
			forbiddenFailure = excelBPSForbiddenFailover(raw)
		}
		kind := "http_error"
		if !cyber && (resp.StatusCode == http.StatusTooManyRequests || modelDenied || retryServerError || forbiddenFailure != nil) {
			kind = "failover"
		}
		recordFailure(resp, raw, kind, fmt.Sprintf("Excel BPS returned HTTP %d", resp.StatusCode), upstreamBody, "generation")
		if cyber {
			return fail(resp.StatusCode, "cyber_policy", "Excel BPS upstream failure: cyber_policy; request was not replayed")
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			if err := s.observeExcelBPSRateLimit(ctx, account, resp.StatusCode, resp.Header, raw, model, basispoints.ResponsesURL); err != nil {
				return nil, newExcelBPSStateUnavailable(err, false)
			}
			result, failure := failoverRateLimited(resp.Header.Get("Retry-After"))
			if rejected, ok := failure.(*UpstreamFailoverError); ok {
				rejected.RejectedBeforeGeneration = !compactionCompleted
			}
			return result, failure
		}
		if isExcelBPSClientCancellation(ctx, c, ctx.Err()) {
			return clientCanceled()
		}
		if fileOwnershipFailed {
			invalidateOwnershipCache()
			if retryOwnership {
				return nil, newExcelBPSAttachmentOwnershipRetryError(failure.Status)
			}
			return fail(failure.Status, failure.Code, failure.Message)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			if account.IsExcelOAuth() && !compactionCompleted && s.openAITokenProvider != nil {
				if s.refreshRejectedExcelPrimary(ctx, account, token) {
					failure := newExcelBPSUpstreamFailureError(http.StatusUnauthorized)
					failure.Stage, failure.Reason = GatewayFailureStageAccountAuth, "basispoints_credentials_rejected"
					failure.RejectedBeforeGeneration, failure.RetryableOnSameAccount, failure.SameAccountRetryMax = true, true, 1
					return nil, failure
				}
			}
			s.handleExcelBPSUnauthorized(ctx, account, resp.StatusCode, resp.Header, raw, token)
			if !compactionCompleted {
				failure := newExcelBPSUpstreamFailureError(http.StatusUnauthorized)
				failure.Stage, failure.Reason = GatewayFailureStageAccountAuth, "basispoints_credentials_rejected"
				failure.RejectedBeforeGeneration = true
				failure.RetryableOnSameAccount = !account.IsExcelOAuth() && takeTurnGrantRefresh(ctx, account, "excel", token) == nil
				failure.SameAccountRetryMax = 1
				return nil, failure
			}
			return fail(excelBPSUpstreamAuthClientStatus(resp.StatusCode), "basispoints_upstream_error", "Excel BPS authentication failed; request was not replayed")
		}
		if modelDenied {
			s.coolDownExcelBPSModel(account, model)
			failure := newExcelBPSModelPermissionFailoverError()
			failure.RejectedBeforeGeneration = !compactionCompleted && !reportedUsage
			return nil, failure
		}
		if retryServerError && ctx.Err() == nil {
			return nil, newExcelBPSUpstreamFailureError(resp.StatusCode)
		}
		if resp.StatusCode >= 500 && replayBlocker != "" && !firstOutputGuard.expired() {
			details, _ := json.Marshal(map[string]any{"blocking_tool_kind": replayBlocker})
			RecordOpenAIRetryStop(c, account, OpenAIRetryStopReplayUnsafe, string(details))
		}
		if resp.StatusCode == http.StatusBadRequest && !compactionCompleted {
			if recovery := excelBPSEncryptedHistoryRetry(ctx, originalBody, raw); recovery != nil {
				return nil, recovery
			}
		}
		message := "Excel BPS rejected this request; account scheduling was not changed"
		errorCode := "basispoints_upstream_error"
		if resp.StatusCode == http.StatusBadRequest && isOpenAIEncryptedContextRejection(raw) {
			errorCode = "invalid_encrypted_content"
			message = "Excel BPS could not verify encrypted conversation state; resend the original plaintext history or start a new conversation"
		} else if resp.StatusCode == http.StatusBadRequest && isOpenAIContextWindowError(extractUpstreamErrorMessage(raw), raw) {
			// Keep the identifier Codex uses to report a full context window, as the
			// in-stream failure path already does; a generic code hides it. Share the
			// native classifier so context_too_large and message-only variants match.
			errorCode = "context_length_exceeded"
			message = "Excel BPS upstream failure: context_length_exceeded; request was not replayed"
		}
		if excelBPSAccountForbidden(resp.StatusCode, raw) {
			// Apply group routing before the independent protocol switch is disabled.
			moved := s.moveExcelBPSOn403(ctx, account)
			disabled := s.disableExcelBPSOn403(ctx, account, token)
			switch {
			case moved && disabled:
				message = "Excel BPS rejected this request; Excel BPS was automatically disabled and account groups were updated; request was not replayed"
			case disabled:
				message = "Excel BPS rejected this request; Excel BPS was automatically disabled for this account; request was not replayed"
			case moved:
				message = "Excel BPS rejected this request; account groups were automatically updated; request was not replayed"
			}
		}
		if forbiddenFailure != nil {
			return nil, forbiddenFailure
		}
		return fail(excelBPSUpstreamAuthClientStatus(resp.StatusCode), errorCode, message)
	}
	// BPS and Codex share quota. Refresh at the HTTP boundary even if the client
	// disconnects or a later stream/protocol error prevents normal completion.
	s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, resp.Header)
	// The application repair driver owns sends and diagnostic origin. The
	// consumer reads these only after joining it, separately from the billing ID.
	diagnosticResponse := resp
	diagnosticBody, diagnosticStage := upstreamBody, "generation"
	// Observe the original complete event before tool/structured validation can
	// buffer it. The callback captures only this attempt, never Gin state.
	var streamStateErr error      // Read only after Close joins the producing driver.
	streamContentRefused := false // Same producer and Join boundary as streamStateErr.
	bridge.OnUpstreamEvent = func(payload []byte) {
		streamStateErr = errors.Join(streamStateErr, observeResponseBPSEvent(requestCtx, diagnosticResponse, payload))
		streamContentRefused = streamContentRefused || IsUpstreamContentRefusal(payload)
		if !streamContentRefused {
			streamStateErr = errors.Join(streamStateErr, s.observeExcelBPSRateLimit(requestCtx, account, 0, diagnosticResponse.Header, payload, model, basispoints.ResponsesURL))
		}
		if stream {
			timing.ObserveEvent(payload)
		} else if responseswire.IsTerminalEvent(gjson.GetBytes(payload, "type").String()) {
			timing.ObserveBufferedResponse([]byte(gjson.GetBytes(payload, "response").Raw))
		}
	}
	driver := &excelBPSRepairDriver{
		service: s, gateway: c, account: account, bridge: bridge,
		scope: scope, token: token, accountID: accountID, model: model, prepared: upstreamBody,
		diagnosticResponse: &diagnosticResponse, diagnosticBody: &diagnosticBody,
		diagnosticStage: &diagnosticStage, observeOwnership: observeFileOwnershipFailure,
	}
	converted := driver.stream(withResponseBPSRole(requestCtx, gatewaymodel.AttemptRepair), resp.Body, compactOutput, compactUsageWire)
	// The bridge sees the body after group policy mapping. Keep the original
	// client effort for usage display, and the BPS-normalized effort for billing.
	requestedEffort := coalesceRequestedReasoningEffort(RequestedReasoningEffortFromContext(ctx), &bridge.RequestedEffort)
	result := &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeBPS, Model: originalModel, UpstreamModel: model, UpstreamEndpoint: "/basispoints/api/responses", Stream: stream, ReasoningEffort: &bridge.Effort, RequestedReasoningEffort: requestedEffort, RequestID: resp.Header.Get("x-request-id")}
	var createdAtTracker responsesCreatedAtStamper
	streamFailureRecorded := false
	recordStreamFailure := func(kind string) {
		// A terminal has already been parsed. Join the converter before reading
		// its snapshot, and record diagnostics before Ops freezes the turn.
		_ = converted.Close()
		if !streamFailureRecorded {
			if failure := converted.ObservedFailure(); failure != nil {
				if !streamContentRefused && !IsUpstreamContentRefusal(failure.Diagnostic) {
					s.observeExcelBPSTerminalAuthorization(ctx, account, failure, diagnosticResponse.Header, token, model)
				}
				if failure.Local {
					kind = "bridge_error"
					c.Set(OpsGatewayResponsesFailedKey, true)
				}
				observeFileOwnershipFailure(failure)
				if failure.RequestScoped() {
					MarkOpsLocalDependencyFailure(c)
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: "bridge_error", Scope: "excel_bps", Stage: "state_persist", Reason: failure.Code, AccountID: account.ID, Message: failure.Message})
				} else {
					recordFailure(diagnosticResponse, failure.Diagnostic, kind, failure.Message, diagnosticBody, diagnosticStage)
				}
				streamFailureRecorded = true
			}
		}
	}
	captureObservedUsage := func() {
		if usage := converted.ObservedUsage(); usage != nil {
			raw, _ := json.Marshal(map[string]any{"usage": usage})
			var observed OpenAIUsage
			s.parseSSEUsageBytes(raw, &observed)
			if openAIUsageHasTokens(&observed) {
				result.Usage = observed // compaction is added once by the outer defer
				if result.RequestID == "" {
					result.RequestID = result.ResponseID
					if result.RequestID == "" && responseBPSFromContext(ctx) == nil {
						// Billing identity only; never expose it as a response ID or
						// use it as a continuation/cancellation anchor.
						result.RequestID = "bps-usage-" + uuid.NewString()
					}
				}
				if forwardResult == nil {
					forwardResult = result
				}
			}
		}
	}
	defer func() {
		// Join conversion/repair before freezing billing. Validated tool output
		// can be withheld while its already reported usage remains billable.
		kind := "stream_error"
		var failover *UpstreamFailoverError
		if errors.As(forwardErr, &failover) {
			kind = "failover"
		}
		recordStreamFailure(kind)
		captureObservedUsage()
		forwardErr = errors.Join(forwardErr, streamStateErr)
	}()
	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
	}
	// Same gateway.stream_keepalive_interval and semantics as the native and
	// Cloud HTTP paths: 0 or no config disables the downstream comment heartbeat.
	var heartbeatCh <-chan time.Time
	if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		heartbeat := time.NewTicker(time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second)
		defer heartbeat.Stop()
		heartbeatCh = heartbeat.C
	}
	keepalive := func() {
		if stream && !emitsEvents && ctx.Err() == nil {
			_, _ = c.Writer.WriteString(": keepalive\n\n")
			c.Writer.Flush()
		}
	}
	var completed []byte
	var upstreamFailure *basispoints.UpstreamFailure
	var staged []string
	stagedBytes := 0
	contentCommitted := false
	// Buffer complete lifecycle frames until semantic output commits the turn.
	writeStream := func(block string, flush bool) error {
		if emitsEvents {
			writeErr := eventWriter.WriteResponseEvent([]byte(block))
			recordResponseBPSDelivery(ctx, []byte(block), writeErr)
			return writeErr
		}
		if _, writeErr := c.Writer.WriteString(block); writeErr != nil {
			return writeErr
		}
		if flush {
			c.Writer.Flush()
		}
		return downstreamStreamError(c)
	}
	terminal := ""
	var finishContinuation excelBPSContinuationCompletion
	completeContinuation := func(delivered bool) {
		if finishContinuation == nil {
			return
		}
		finish := finishContinuation
		finishContinuation = nil
		outcome, completionErr := finish(delivered)
		observeExcelBPSContinuationCompletion(c, account, outcome, completionErr)
	}
	defer func() { completeContinuation(false) }()
	lastEventType, eventsSeen, nextSequence := "none", 0, int64(0)
	cacheCreationAsInput := account.IsExcelBPSCacheCreationAsInputEnabled()
	for converted.Next(ctx, heartbeatCh, keepalive) {
		event := converted.Event()
		data := string(event.payload)
		kind := event.kind
		upstreamResponseModel := gjson.Get(data, "response.model").String()
		// Match native HTTP/SSE model mapping without rewriting tool/text
		// payloads. Retain the provider's model separately for diagnostics.
		data = replaceOpenAISSEModelData(data, model, originalModel)
		payload := []byte(data)
		createdAtTracker.Observe(payload, kind)
		eventsSeen++
		sequence := gjson.GetBytes(payload, "sequence_number").Int()
		nextSequence = sequence + 1
		// Event types are upstream-controlled; persist only known labels.
		switch kind {
		case "response.created", "response.queued", "response.in_progress",
			"response.output_item.added", "response.output_item.done", "response.content_part.added", "response.content_part.done",
			"response.output_text.delta", "response.output_text.done", "response.refusal.delta", "response.refusal.done",
			"response.reasoning_summary_part.added", "response.reasoning_summary_part.done", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done",
			"response.function_call_arguments.delta", "response.function_call_arguments.done",
			"response.custom_tool_call_input.delta", "response.custom_tool_call_input.done",
			"response.completed", "response.failed", "response.cancelled", "response.incomplete", "error":
			lastEventType = kind
		default:
			lastEventType = "other"
		}
		if id := gjson.GetBytes(payload, "response.id").String(); id != "" {
			result.ResponseID = id
		}
		// Terminal errors are classified for recovery before disarming the
		// logical turn's deadline; native error semantics differ from BPS.
		startsOutput := kind != "error" && kind != "response.failed" && kind != "response.cancelled" && excelBPSStreamStartsClientOutput(payload, kind)
		if startsOutput {
			if !stopFirstOutput() {
				break
			}
		}
		s.parseSSEUsageBytes(payload, &result.Usage)
		// Codex recognizes a full context window from response.failed, including
		// when BPS accepts HTTP and rejects the request inside its SSE stream.
		// Use the same envelope for HTTP/SSE and the WS bridge; keep the original
		// upstream diagnostic in the stream observer for operations reporting.
		if kind == "error" || (kind == "response.failed" && gjson.GetBytes(payload, "response.id").String() == "") {
			if failure := basispoints.ParseUpstreamFailure(payload); failure != nil && failure.Code == "context_length_exceeded" {
				failureID := result.ResponseID
				if failureID == "" {
					failureID = "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				}
				usage := gjson.GetBytes(payload, "response.usage")
				if !usage.IsObject() {
					usage = gjson.GetBytes(payload, "usage")
				}
				payload = buildOpenAIWSHTTPBridgeFailedEvent(failureID, originalModel, createdAtTracker.Value(), payload, "Excel BPS context window exceeded")
				if usage.IsObject() {
					payload, _ = sjson.SetRawBytes(payload, "response.usage", []byte(usage.Raw))
				}
				payload, _ = sjson.SetBytes(payload, "sequence_number", sequence)
				kind = "response.failed"
				data = string(payload)
			}
		}
		if kind == "response.failed" && gjson.GetBytes(payload, "response.id").String() == "" {
			payload = buildOpenAIWSHTTPBridgeFailedEvent(result.ResponseID, originalModel, createdAtTracker.Value(), payload, "Excel BPS response failed")
			payload, _ = sjson.SetBytes(payload, "sequence_number", sequence)
			kind = gjson.GetBytes(payload, "type").String()
			data = string(payload)
		}

		if len(compactOutput) > 0 && (kind == "response.completed" || kind == "response.failed" || kind == "response.cancelled" || kind == "response.incomplete") {
			subtractImagePolicyUsage(&result.Usage, compactUsage)
		}
		if cacheCreationAsInput {
			payload, err = excelBPSDownstreamUsage(payload)
			if err != nil {
				return fail(http.StatusBadGateway, "basispoints_usage_invalid", "Excel BPS usage could not be normalized")
			}
			data = string(payload)
		}
		if result.FirstTokenMs == nil && startsOutput {
			ms := int(time.Since(start).Milliseconds())
			result.FirstTokenMs = &ms
		}
		switch kind {
		case "response.completed", "response.failed", "response.cancelled", "response.incomplete", "error":
			terminal = kind
			completed = []byte(gjson.GetBytes(payload, "response").Raw)
			result.UpstreamResponseModel = upstreamResponseModel
			result.UpstreamIncompleteReason = gjson.GetBytes(payload, "response.incomplete_details.reason").String()
			upstreamFailure = basispoints.ParseUpstreamFailure(payload)
			if upstreamFailure != nil {
				cyber, _, _ := detectOpenAICyberPolicy(payload)
				// Classification may normalize unknown code spellings. Use the
				// original joined diagnostic for native cyber detection before retry.
				_ = converted.Close()
				if streamStateErr != nil {
					return result, newExcelBPSStateUnavailable(streamStateErr, false)
				}
				if original := converted.ObservedFailure(); original != nil {
					cyber, _, _ = detectOpenAICyberPolicy(original.Diagnostic)
					if original.Local {
						upstreamFailure = original
					}
				}
				contentRefused := streamContentRefused || IsUpstreamContentRefusal(payload)
				modelDenied := !contentRefused && isExcelBPSModelPermissionError(upstreamFailure.Status, upstreamFailure.Code)
				rateLimited := !cyber && !contentRefused && upstreamFailure.Status == http.StatusTooManyRequests
				retryRateLimit := rateLimited && diagnosticStage == "generation" && openAIWSUncertainExecutionReplaySafe(body)
				// Explicit rejection and transient 5xx use the existing account
				// switch loop; unknown failures and cancellation are not replayed.
				// Normalization removes signature diagnostics; retain the code's
				// rejection even when the event reports a transient HTTP status.
				retryServerError := !cyber && !contentRefused && !upstreamFailure.Local && upstreamFailure.Status >= 500 && upstreamFailure.Code != basispoints.FileOwnershipFailureCode && !isOpenAIEncryptedContextRejection(payload) && upstreamFailure.Code != "thinking_signature_invalid" &&
					(upstreamFailure.Code == "server_error" || upstreamFailure.Code == "internal_server_error" || upstreamFailure.Status == http.StatusServiceUnavailable) &&
					openAIWSUncertainExecutionReplaySafe(body)
				retryOwnership := !cyber && !contentRefused && upstreamFailure.Status >= 500 && upstreamFailure.Code == basispoints.FileOwnershipFailureCode &&
					diagnosticStage == "generation" && !openAIUsageHasTokens(&result.Usage) &&
					canRetryExcelBPSAttachmentOwnership(images, diagnosticBody, originalBody, compactionCompleted)
				if (kind == "error" || kind == "response.failed") && !contentCommitted && ctx.Err() == nil && retryOwnership {
					// Record the ownership failure (which invalidates the cached IDs),
					// then retry once on this account with fresh uploads.
					recordStreamFailure("failover")
					invalidateOwnershipCache()
					result.Duration = time.Since(start)
					if result.RequestID == "" {
						result.RequestID = result.ResponseID // billing only; this ID was not delivered
					}
					result.ResponseID = ""
					return result, newExcelBPSAttachmentOwnershipRetryError(upstreamFailure.Status)
				}
				// The same refusal in-band before any content, usage or compaction
				// proves nothing ran; the lifecycle events stayed staged.
				if (kind == "error" || kind == "response.failed") && !contentCommitted && ctx.Err() == nil && !cyber && !contentRefused &&
					!compactionCompleted && diagnosticStage == "generation" && !openAIUsageHasTokens(&result.Usage) {
					if recovery := excelBPSEncryptedHistoryRetry(ctx, originalBody, payload); recovery != nil {
						return nil, recovery
					}
				}
				if (kind == "error" || kind == "response.failed") && !contentCommitted && ctx.Err() == nil && diagnosticStage == "generation" && (modelDenied || retryServerError || retryRateLimit) {
					result.Duration = time.Since(start)
					if result.RequestID == "" {
						result.RequestID = result.ResponseID // billing only; this ID was not delivered
					}
					result.ResponseID = ""
					if retryRateLimit {
						return result, newExcelBPSRateLimitedFailoverError(resp.Header.Get("Retry-After"))
					}
					if modelDenied {
						failure := newExcelBPSModelPermissionFailoverError()
						failure.RejectedBeforeGeneration = !compactionCompleted && !gjson.GetBytes(payload, "usage").Exists() && !gjson.GetBytes(payload, "response.usage").Exists() && !openAIUsageHasTokens(&result.Usage)
						return result, failure
					}
					return result, newExcelBPSUpstreamFailureError(upstreamFailure.Status)
				}
				// Preserve the actual HTTP status separately from the event's
				// semantic status. Committed content is never replayed.
				recordStreamFailure("stream_error")
				MarkOpsStreamErrorValue(c, OpsStreamError{
					ErrType: upstreamFailure.Type, Code: upstreamFailure.Code, Message: upstreamFailure.Message,
					IntendedStatus: upstreamFailure.Status, CountTowardsSLA: true, NonStream: !stream,
				})
			}
		}
		if terminal != "" && !stopFirstOutput() {
			terminal = ""
			break
		}
		if terminal == "response.completed" && upstreamFailure == nil {
			// Publish a pending snapshot before the terminal, then commit only
			// after the client write succeeds. Reconnecting readers wait briefly
			// for this exact publication; a failed delivery never becomes history.
			var stateErr error
			snapshotBody, snapshotResponse := originalBody, completed
			if len(compactOutput) > 0 {
				snapshotBody, snapshotResponse, stateErr = excelBPSCompactedContinuationSources(originalBody, completed, compactOutput, imagePolicy.history.Input[imagePolicy.split:])
			}
			if stateErr == nil && !gjson.GetBytes(snapshotBody, "tools").Exists() && gjson.GetBytes(snapshotBody, "tool_choice").String() != "none" {
				// The request may have inherited a catalog even though its original
				// body omitted tools. Freeze this request's effective declarations,
				// not a potentially newer session catalog at completion time.
				var tools []byte
				tools, stateErr = bridge.ClientToolsJSON()
				if stateErr == nil {
					snapshotBody, stateErr = sjson.SetRawBytes(snapshotBody, "tools", tools)
				}
			}
			if stateErr == nil {
				finishContinuation, stateErr = s.prepareExcelBPSContinuation(ctx, c, account, snapshotBody, snapshotResponse)
			}
			if stateErr != nil {
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: "recovery", Scope: "excel_bps", Stage: "history_publish", Reason: "state_unavailable", Message: "BPS response completed but its reconnect history could not be retained", AccountID: account.ID})
			}
			// Publish the existing owner/route binding before the client can send
			// the next request on another socket or another gateway instance.
			s.bindHTTPResponseAccount(ctx, c, account, result.ResponseID)
		}
		if stream {
			var block string
			if emitsEvents {
				block = data
			} else {
				// SSE is encoded only at the real HTTP client boundary.
				block = fmt.Sprintf("event: %s\ndata: %s\n\n", kind, data)
			}
			if block == "" {
				continue
			}
			if !contentCommitted && terminal == "" && result.FirstTokenMs == nil && stagedBytes+len(block) <= int(openAIFirstOutputStageMaxBytes) {
				staged = append(staged, block)
				stagedBytes += len(block)
				continue
			}
			for _, pending := range staged {
				if err = writeStream(pending, false); err != nil {
					break
				}
			}
			staged, stagedBytes = nil, 0
			contentCommitted = true
			if err == nil {
				err = writeStream(block, true)
			}
			if err != nil {
				result.ClientDisconnect = true
				result.UpstreamTerminalEvent = terminal
				if isExcelBPSClientCancellation(ctx, c, c.Request.Context().Err()) {
					MarkOpsClientCancellation(c, stream)
				}
				result.Duration = time.Since(start)
				return result, err
			}
		}
		if stream && finishContinuation != nil && terminal == "response.completed" {
			completeContinuation(frameWriter == nil || !frameWriter.disconnected)
		}
		if terminal != "" {
			break // a delivered complete terminal wins over a later cancellation/EOF
		}
	}
	result.Duration = time.Since(start)
	result.UpstreamTerminalEvent = terminal
	if err = converted.Err(); err != nil || terminal == "" {
		if parentCtx.Err() != nil {
			if isExcelBPSClientCancellation(parentCtx, c, parentCtx.Err()) {
				MarkOpsClientCancellation(c, stream)
			}
			result.ClientDisconnect = true
			return result, context.Cause(parentCtx)
		}
		// Join conversion/repair before reading the final HTTP identity. EOF does
		// not prove the generation was unaccepted, so retain the no-replay boundary.
		_ = converted.Close()
		// A withheld tool completion/correction can be metered even when no
		// usage frame reached this reader. Observe the joined snapshot before
		// deciding whether an interrupted generation is safe to replay.
		captureObservedUsage()
		endReason := "read_error"
		if firstOutputGuard.expired() {
			endReason = "first_output_timeout"
		} else if errors.Is(err, basispoints.ErrStreamIncomplete) {
			endReason = "missing_terminal"
		} else if errors.Is(err, basispoints.ErrStreamProgressTimeout) {
			endReason = "progress_timeout"
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, err, c.GetInt("excel_bps_upstream_attempt"), OpsUpstreamErrorEvent{
			UpstreamURL: basispoints.ResponsesURL, Stage: "stream",
			UpstreamStatusCode: diagnosticResponse.StatusCode, UpstreamRequestID: diagnosticResponse.Header.Get("x-request-id"),
		}, map[string]any{
			"stream_end_reason": endReason, "last_event_type": lastEventType, "stream_event_count": eventsSeen,
			"content_committed": contentCommitted, "semantic_output_seen": result.FirstTokenMs != nil,
			"response_id_observed": result.ResponseID != "", "elapsed_ms": result.Duration.Milliseconds(),
		})
		code, message, status := "basispoints_stream_incomplete", "Upstream stream ended before completion", http.StatusBadGateway
		if firstOutputGuard.expired() {
			code, message, status = "first_output_timeout", "Excel BPS produced no semantic output before the deadline", http.StatusGatewayTimeout
			RecordOpenAIRetryStop(c, account, OpenAIRetryStopTurnBudget)
		}
		repairCause := converted.ObservedCause()
		if repairCause == nil && !contentCommitted && !compactionCompleted && !firstOutputGuard.expired() && openAIWSUncertainExecutionReplaySafe(originalBody) && !openAIUsageHasTokens(&result.Usage) {
			failure := newExcelBPSUpstreamFailureError(status)
			failure.RetryableOnSameAccount, failure.SameAccountRetryMax = true, 1
			return nil, failure
		}
		c.Set(OpsGatewayResponsesFailedKey, true)
		MarkOpsStreamErrorValue(c, OpsStreamError{
			ErrType: "server_error", Code: code, Message: message, IntendedStatus: status,
			CountTowardsSLA: true, NonStream: !stream,
		})
		MarkResponseCommitted(c)
		if stream {
			source, _ := json.Marshal(gin.H{"error": gin.H{"type": "server_error", "code": code, "message": message}})
			failure := buildOpenAIWSHTTPBridgeFailedEvent(result.ResponseID, originalModel, createdAtTracker.Value(), source, message)
			if contentCommitted {
				failure, _ = sjson.SetBytes(failure, "sequence_number", nextSequence)
			}
			result.UpstreamTerminalEvent = gjson.GetBytes(failure, "type").String()
			if emitsEvents {
				writeErr := eventWriter.WriteResponseEvent(failure)
				recordResponseBPSDelivery(ctx, failure, writeErr)
			} else {
				_, _ = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", result.UpstreamTerminalEvent, failure)
				c.Writer.Flush()
			}
		} else {
			c.JSON(status, gin.H{"error": gin.H{"type": "server_error", "code": code, "message": message}})
		}
		if repairCause != nil {
			return result, repairCause
		}
		return result, fmt.Errorf("excel BPS stream incomplete")
	}
	success := result.SucceededForScheduling()
	if !success {
		MarkResponseCommitted(c)
	}
	if !stream {
		if upstreamFailure != nil {
			if StopOpenAICompactSSEKeepaliveCommitted(c) {
				writeOpenAICompactSSEFailureMessage(c, upstreamFailure.Status, upstreamFailure.Code, upstreamFailure.Message)
			} else {
				c.JSON(upstreamFailure.Status, gin.H{"error": upstreamFailure.Details()})
			}
		} else if !success {
			c.JSON(502, gin.H{"error": gin.H{"code": "basispoints_protocol_error", "message": "Excel BPS did not complete the response"}})
		} else {
			MarkResponseCommitted(c)
			c.Header("Content-Type", "application/json")
			c.Status(http.StatusOK)
			written, writeErr := c.Writer.Write(completed)
			if writeErr == nil && written != len(completed) {
				writeErr = io.ErrShortWrite
			}
			if finishContinuation != nil {
				completeContinuation(writeErr == nil && (frameWriter == nil || !frameWriter.disconnected))
			}
			if writeErr != nil {
				result.ClientDisconnect = true
				return result, writeErr
			}
		}
	}
	if !success {
		_ = converted.Close()
		if streamContentRefused {
			status := http.StatusBadGateway
			if upstreamFailure != nil {
				status = excelBPSUpstreamAuthClientStatus(upstreamFailure.Status)
			}
			return result, errors.Join(&excelBPSForwardError{code: "content_policy_violation", status: status}, converted.ObservedCause())
		}
		if cause := converted.ObservedCause(); cause != nil {
			return result, cause
		}
		if upstreamFailure != nil && upstreamFailure.RequestScoped() {
			return result, upstreamFailure
		}
		return result, fmt.Errorf("excel BPS terminal: %s", terminal)
	}
	imagePolicy.finish(ctx, imagePolicy.compact || len(compactOutput) > 0 || (imagePolicy.history != nil && imagePolicy.history.Count < imageSettings.Limits.MaxImages-imageSettings.WarningRemaining))
	s.bindHTTPResponseAccount(ctx, c, account, result.ResponseID)
	return result, nil
}

var excelBPSBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[^\s"',;<>]+`)
var excelBPSURLCredentialsPattern = regexp.MustCompile(`(https?://)[^/\s@]+@`)
var excelBPSAttachmentIDPattern = regexp.MustCompile(`\bfile-[A-Za-z0-9_-]+`)
var excelBPSImageCapabilityPattern = regexp.MustCompile(`/api/bps-images/[A-Za-z0-9_-]+`)

func excelBPSSanitizeErrorBody(raw, token string, account *Account) string {
	if !json.Valid([]byte(raw)) {
		return ""
	}
	secrets := append([]string{token}, excelBPSAccountSecrets(account)...)
	fields := make(map[string]string)
	for _, key := range []string{"message", "code", "type", "param"} {
		value := gjson.Get(raw, "error."+key)
		if value.Type != gjson.String {
			continue
		}
		clean := value.String()
		for _, secret := range secrets {
			if secret != "" {
				clean = strings.ReplaceAll(clean, secret, "[redacted]")
			}
		}
		clean = excelBPSBearerPattern.ReplaceAllString(clean, "Bearer [redacted]")
		clean = excelBPSURLCredentialsPattern.ReplaceAllString(clean, "${1}[redacted]@")
		clean = excelBPSImageCapabilityPattern.ReplaceAllString(clean, "/api/bps-images/[redacted]")
		clean = excelBPSAttachmentIDPattern.ReplaceAllString(clean, "file-[redacted]")
		clean = sanitizeUpstreamErrorMessage(clean)
		fields[key] = truncateString(logredact.RedactText(clean, "authorization", "api_key", "apikey", "token", "secret", "key", "cookie", "ticket", "recovery_ticket"), 2048)
	}
	encoded, _ := json.Marshal(map[string]any{"error": fields})
	return string(encoded)
}

func excelBPSAccountSecrets(account *Account) []string {
	var secrets []string
	for _, key := range []string{"access_token", "refresh_token", "id_token", "api_key", "session_key", "cookie"} {
		if value := account.GetCredential(key); value != "" {
			secrets = append(secrets, value)
		}
	}
	if account.Proxy != nil && account.Proxy.Password != "" {
		secrets = append(secrets, account.Proxy.Password)
	}
	return secrets
}

// Explicit conversation identity remains sticky. Anonymous requests get a
// request-local identity reused across internal retries, never across callers.
func resolveExcelBPSIdentity(c *gin.Context, body []byte, apiKeyID int64, managed bool) (string, bool) {
	if identity, _ := resolveOpenAIWSExecutionScope(c, body, apiKeyID); identity != "" {
		return identity, false
	}
	if !managed {
		return "", false
	}
	const key = "excel_bps_transient_identity"
	if identity := c.GetString(key); identity != "" {
		return identity, true
	}
	identity := uuid.NewString()
	c.Set(key, identity)
	return identity, true
}

// Both WS admission and HTTP generation must preserve the failed grant mode.
func newExcelBPSCredentialFailure(c *gin.Context, account *Account, err error) error {
	if err == nil || IsTurnBudgetError(err) || errors.Is(err, context.Canceled) {
		return err
	}
	failure := newExcelBPSUpstreamFailureError(http.StatusServiceUnavailable)
	failure.Stage, failure.Scope, failure.Reason = GatewayFailureStageAccountAuth, GatewayFailureScopeAccount, "basispoints_credentials_unavailable"
	failure.RequestNotSent, failure.Cause = true, err
	failure.ClientMessage = "Excel BPS credentials are unavailable"
	failure.RetryableOnSameAccount, failure.SameAccountRetryMax = true, 1
	var authorization *excelAuthorizationFailure
	if !errors.As(err, &authorization) {
		failure.Scope = GatewayFailureScopeProvider
	} else {
		failure.RetryableOnSameAccount, failure.SameAccountRetryMax = false, 0
	}
	recordExcelBPSCredentialFailure(c, account, failure)
	return failure
}

// BPS withholds tools until validation. Empty reasoning/message lifecycle
// items do not commit semantic output; unknown/malformed output stays conservative.
func excelBPSStreamStartsClientOutput(payload []byte, kind string) bool {
	if kind == "response.output_item.added" || kind == "response.output_item.done" {
		item := gjson.GetBytes(payload, "item")
		field := ""
		switch item.Get("type").String() {
		case "reasoning":
			field = "summary"
		case "message":
			field = "content"
		}
		if field != "" {
			parts := item.Get(field)
			if !parts.Exists() {
				return kind == "response.output_item.done" // malformed completion is not proof of no output
			}
			if !parts.IsArray() {
				return true
			}
			for _, part := range parts.Array() {
				switch part.Get("type").String() {
				case "output_text", "summary_text":
					if part.Get("text").Type != gjson.String || part.Get("text").String() != "" {
						return true
					}
				case "refusal":
					if part.Get("refusal").Type != gjson.String || part.Get("refusal").String() != "" {
						return true
					}
				default:
					return true // unknown output cannot be discarded for replay
				}
			}
			return false
		}
	}
	return openAIStreamDataStartsClientOutput(string(payload), kind)
}

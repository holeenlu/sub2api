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

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
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

// Independently authorized Excel grants never quarantine the primary Codex
// account. Legacy single-grant accounts retain their authentication policy.
func (s *OpenAIGatewayService) handleExcelBPSUnauthorized(ctx context.Context, account *Account, status int, headers http.Header, raw []byte, tokens ...string) {
	if status != http.StatusUnauthorized {
		return
	}

	if !account.IsExcelOAuth() && s.openAITokenProvider != nil && s.openAITokenProvider.openAIOAuthService != nil {
		token := ""
		if len(tokens) > 0 {
			token = tokens[0]
		}
		stateCtx, cancel := openAIAccountStateContext(ctx)
		defer cancel()
		_ = s.openAITokenProvider.openAIOAuthService.rejectExcelAccessToken(stateCtx, account, token, s.openAITokenProvider.tokenCache)
		return
	}

	if s.rateLimitService == nil {
		return
	}
	fields := map[string]string{"message": "Excel BPS authentication failed"}
	code := extractUpstreamErrorCode(raw)
	if code == "token_invalidated" || code == "token_revoked" {
		fields["code"] = code
	}
	authError := map[string]any{"error": fields}
	if gjson.GetBytes(raw, "detail").String() == "Unauthorized" {
		authError["detail"] = "Unauthorized"
	}
	body, _ := json.Marshal(authError)
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	s.rateLimitService.HandleUpstreamError(stateCtx, account, status, headers, body)
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
	}
	return changed
}

func (s *OpenAIGatewayService) disableExcelBPSOn403(ctx context.Context, account *Account) bool {
	if !account.IsExcelBPSAutoDisableOn403Enabled() {
		return false
	}
	repo, ok := s.accountRepo.(AccountExcelBPSRepository)
	if !ok {
		return false
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	changed, err := repo.DisableExcelBPSOn403(stateCtx, account)
	if err != nil {
		// Do not log upstream bodies, credentials or database query arguments.
		logger.LegacyPrintf("service.openai_excel_bps", "auto-disable failed: account_id=%d error_type=%T", account.ID, err)
		return false
	}
	if changed {
		logger.LegacyPrintf("service.openai_excel_bps", "automatically disabled Excel BPS after upstream HTTP 403: account_id=%d", account.ID)
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

// ServeExcelBPSImage allows the upstream to retrieve an unguessable temporary URL.
func (s *OpenAIGatewayService) ServeExcelBPSImage(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	relay, err := s.excelBPSImageRelay(c.Request.Context())
	if err != nil || relay == nil {
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

// BPS deliberately bypasses Codex ticket/cookie injection and OAuth plugins:
// only the selected account's bearer and ChatGPT account ID belong on this host.
func (s *OpenAIGatewayService) forwardExcelBPS(ctx context.Context, c *gin.Context, account *Account, body []byte, start time.Time, mappedModels ...string) (forwardResult *OpenAIForwardResult, forwardErr error) {
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
			if budget, ok := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); ok {
				budget.mu.Lock()
				budget.deadline = time.Time{}
				budget.mu.Unlock()
			}
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
				forwardResult = &OpenAIForwardResult{Model: originalImagePolicyModel, ReasoningEffort: &compactEffort, UpstreamModel: gjson.GetBytes(body, "model").String(), UpstreamEndpoint: "/basispoints/api/responses", RequestID: compactID, Stream: gjson.GetBytes(body, "stream").Bool(), Duration: time.Since(start)}
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
	fail := func(status int, code, message string, param ...string) (*OpenAIForwardResult, error) {
		if parentCtx.Err() == nil && firstOutputGuard.expired() {
			status, code, message = http.StatusGatewayTimeout, "first_output_timeout", "Excel BPS produced no semantic output before the deadline; the request was not replayed"
		}
		// A compact keepalive may already have committed SSE headers. Otherwise
		// finish a single JSON response so the handler cannot append another error.
		committed := StopOpenAICompactSSEKeepaliveCommitted(c)
		MarkResponseCommitted(c)
		if committed {
			failureParam := ""
			if len(param) > 0 {
				failureParam = param[0]
			}
			writeOpenAICompactSSEFailureMessageParam(c, status, code, message, failureParam)
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
		return nil, &excelBPSForwardError{code: code}
	}
	originalModel := gjson.GetBytes(body, "model").String()
	model := account.GetMappedModel(originalModel)
	if len(mappedModels) > 0 {
		model = mappedModels[0]
	}
	stream := gjson.GetBytes(body, "stream").Bool()
	originalBody := body // preserve client history before BPS attachment/tool rewrites
	setOpenAIReplaySafety(c, body)
	clientCanceled := func() (*OpenAIForwardResult, error) {
		StopOpenAICompactSSEKeepaliveCommitted(c)
		MarkResponseCommitted(c)
		MarkOpsClientCancellation(c, stream)
		// No response or metered usage exists before headers; do not create a usage row.
		return nil, context.Canceled
	}
	if !c.GetBool(bpsAccountProbeRequiredContextKey) {
		if until := s.excelBPSCooldownDeadline(account.ID); !until.IsZero() {
			if isExcelBPSClientCancellation(c, ctx.Err()) {
				return clientCanceled()
			}
			failure := newExcelBPSRateLimitedFailoverError(fmt.Sprintf("%d", int64((time.Until(until)+time.Second-1)/time.Second)))
			failure.RequestNotSent = true // local cooldown: no upload or inference attempted
			return nil, failure
		}
	}
	if s.isExcelBPSUpstreamModelCoolingDown(account.ID, model) {
		if ctx.Err() != nil {
			return clientCanceled()
		}
		return nil, newExcelBPSModelPermissionFailoverError()
	}
	// No output exists yet, so the handler may replay the request on another
	// account within its switch budget unless the client is already gone.
	failoverRateLimited := func(retryAfter string) (*OpenAIForwardResult, error) {
		s.coolDownExcelBPS(ctx, account, retryAfter)
		if isExcelBPSClientCancellation(c, ctx.Err()) {
			return clientCanceled()
		}
		return nil, newExcelBPSRateLimitedFailoverError(retryAfter)
	}
	imageSettings, err := s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		return fail(503, "basispoints_image_settings_unavailable", "Excel BPS image settings are unavailable")
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
		if err = json.Unmarshal(body, &request); err != nil {
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
	if transient {
		scope = "transient:" + scope
	}
	if err = basispoints.ValidateNewAgentMessage(body); err != nil {
		var contentErr *basispoints.ContentValidationError
		if errors.As(err, &contentErr) {
			return fail(400, "basispoints_request_invalid", err.Error(), contentErr.Path)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	if account.IsExcelBPSIgnoreEncryptedContentEnabled() {
		body, err = basispoints.StripEncryptedContent(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
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
			return fail(503, "basispoints_image_relay_unavailable", "Excel BPS image relay is unavailable")
		}
		body, err = relay.RewriteWithImageLimit(body, scope, imagePolicy.maxImages)
	}
	if err != nil {
		if errors.Is(err, basispoints.ErrImageRelayFull) {
			return fail(503, "basispoints_image_relay_full", err.Error())
		}
		if errors.Is(err, basispoints.ErrImageRelayStorage) {
			return fail(503, "basispoints_image_relay_unavailable", err.Error())
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
				return fail(503, "basispoints_state_unavailable", basispoints.ErrStateUnavailable.Error())
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
		if errors.Is(err, basispoints.ErrStateUnavailable) {
			return fail(503, "basispoints_state_unavailable", basispoints.ErrStateUnavailable.Error())
		}
		var contentErr *basispoints.ContentValidationError
		if errors.As(err, &contentErr) {
			return fail(400, "basispoints_request_invalid", err.Error(), contentErr.Path)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	if reason := account.excelBPSNativeFallbackReason(body); reason != "" {
		return fail(400, "basispoints_unsupported_tools", "Excel BPS cannot execute requested hosted tools; explicitly allow omission or select a native model", "tools")
	}
	token, err := s.getExcelBPSAccessToken(ctx, account)
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		if IsExcelAuthorizationError(err) {
			code := "basispoints_auth_" + strings.ToLower(strings.TrimPrefix(infraerrors.Reason(err), "OPENAI_EXCEL_AUTH_"))
			return fail(503, code, infraerrors.Message(err))
		}
		return fail(502, "basispoints_auth_unavailable", "Excel OAuth credential is unavailable; see Credential Operations")
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
	attachmentUpload := s.excelBPSAttachmentUpload(c, account, token, accountID, attachmentProxy, c.GetBool(OpenAIAccountReplayAllowedContextKey), excelBPSReplayableEncryptedHistory(originalBody), &compactionCompleted)
	uploadImages := func() (*OpenAIForwardResult, error) {
		body, err = images.Upload(ctx, &s.excelBPSAttachments, attachmentUpload)
		if err != nil {
			if isExcelBPSClientCancellation(c, err) {
				return clientCanceled()
			}
			status, code := http.StatusBadGateway, "basispoints_attachment_error"
			var uploadError *excelBPSAttachmentError
			if errors.As(err, &uploadError) {
				status = uploadError.status
			}
			if errors.Is(err, basispoints.ErrAttachmentBusy) {
				status = http.StatusServiceUnavailable
			}
			if status == http.StatusTooManyRequests && uploadError != nil {
				recordExcelBPSAttachmentFailure(ctx, c, account, err, "failover", 0)
				result, failure := failoverRateLimited(uploadError.retryAfter)
				if failover, ok := failure.(*UpstreamFailoverError); ok {
					// Preserve the existing original-context recovery checks after compact.
					failover.RequestNotSent = !compactionCompleted
				}
				return result, failure
			}
			recordExcelBPSAttachmentFailure(ctx, c, account, err, "", 0)
			if status == http.StatusUnauthorized {
				return fail(status, code, "Excel BPS attachment authentication failed; request was not replayed")
			}
			return fail(status, code, "Excel BPS attachment upload failed; request was not replayed and account scheduling was not changed")
		}
		upstreamBody, bridge, err = bridge.Reprepare(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
		return nil, nil
	}
	if images != nil && images.HasImages() {
		if result, uploadErr := uploadImages(); uploadErr != nil {
			return result, uploadErr
		}
	}
	// Classify ownership failures without replaying this generation or changing
	// account scheduling. No attachment cache survives the client request.
	fileOwnershipFailed := false
	observeFileOwnershipFailure := func(failure *basispoints.UpstreamFailure) bool {
		if failure == nil || failure.Status < 500 || failure.Code != basispoints.FileOwnershipFailureCode {
			return false
		}
		fileOwnershipFailed = true
		return true
	}
	recordFailure := func(resp *http.Response, raw []byte, kind, fallback string, wire []byte, stage string) {
		var fileContext *excelBPSFileOwnershipContext
		if fileOwnershipFailed && s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
			fileContext = &excelBPSFileOwnershipContext{
				FileReferenceCounts: images.FileReferences(wire),
				Stage:               stage,
				CompactionCompleted: compactionCompleted,
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
			return fail(400, "basispoints_request_invalid", buildErr.Error())
		}
		SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
		cr, _, compactErr := s.doExcelBPSRequest(requestCtx, c, account, scope, compactWire, token, accountID)

		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction connection failed; generation was not started")
		}
		compactID = cr.Header.Get("x-request-id")
		if compactID == "" {
			compactID = "bps-compaction-" + uuid.NewString()
		}
		if cr.StatusCode < 200 || cr.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(cr.Body, 512<<10))
			_ = cr.Body.Close()
			cyber, _, _ := detectOpenAICyberPolicy(raw)
			if failure := basispoints.ParseHTTPFailure(cr.StatusCode, raw); failure != nil && (observeFileOwnershipFailure(failure) || cyber) {
				recordFailure(cr, raw, "http_error", failure.Message, compactWire, "image_compaction")
				return fail(failure.Status, failure.Code, failure.Message)
			}
			s.handleExcelBPSUnauthorized(ctx, account, cr.StatusCode, cr.Header, raw, token)
			if cr.StatusCode == http.StatusTooManyRequests {
				recordFailure(cr, raw, "http_error", excelBPSRateLimitedClientMessage, compactWire, "image_compaction")
				return failoverRateLimited(cr.Header.Get("Retry-After"))
			}
			if cr.StatusCode == 403 {
				s.moveExcelBPSOn403(ctx, account)
				s.disableExcelBPSOn403(ctx, account)
			}
			return fail(cr.StatusCode, "basispoints_image_compaction_failed", "Image history compaction was rejected; generation was not started")
		}
		s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, cr.Header)
		compactEffort = bridge.Effort
		compactResponse, compactErr := basispoints.ReadImageCompaction(cr.Body, func(payload []byte) {
			s.parseSSEUsageBytes(payload, &compactUsage)
		})
		_ = cr.Body.Close()
		if compactResponse != nil {
			encoded, _ := json.Marshal(map[string]any{"type": "response.completed", "response": compactResponse})
			s.parseSSEUsageBytes(encoded, &compactUsage)
			compactUsageWire, _ = compactResponse["usage"].(map[string]any)
		}
		if compactErr != nil {
			var compactFailure *basispoints.UpstreamFailure
			if errors.As(compactErr, &compactFailure) {
				cyber, _, _ := detectOpenAICyberPolicy(compactFailure.Diagnostic)
				if observeFileOwnershipFailure(compactFailure) || cyber {
					recordFailure(cr, compactFailure.Diagnostic, "stream_error", compactFailure.Message, compactWire, "image_compaction")
					return fail(compactFailure.Status, compactFailure.Code, compactFailure.Message)
				}
				if compactFailure.Status == http.StatusTooManyRequests {
					s.coolDownExcelBPS(ctx, account, cr.Header.Get("Retry-After"))
					recordFailure(cr, compactFailure.Diagnostic, "stream_error", compactFailure.Message, compactWire, "image_compaction")
					if openAIWSUncertainExecutionReplaySafe(body) {
						return failoverRateLimited(cr.Header.Get("Retry-After"))
					}
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
			return fail(400, "basispoints_request_invalid", compactErr.Error())
		}
		if images != nil && images.HasImages() {
			if result, uploadErr := uploadImages(); uploadErr != nil {
				return result, uploadErr
			}
		}
		if compactErr = imagePolicy.checkpoint(ctx, window); compactErr != nil {
			return fail(503, "basispoints_image_checkpoint_unavailable", "Could not save compacted history safely; retry or run compact manually")
		}
		compactOutput = window

	}
	SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
	SetOpsUpstreamModel(c, model)
	sent := time.Now()
	resp, _, err := s.doExcelBPSRequest(requestCtx, c, account, scope, upstreamBody, token, accountID)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
	if err != nil {
		if errors.Is(err, ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) {
			return nil, err
		}
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
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
		fileOwnershipFailed = observeFileOwnershipFailure(failure)
		retryServerError := !cyber && resp.StatusCode >= 500 && !fileOwnershipFailed && !isExcelBPSInvalidEncryptedContent(raw) &&
			!isOpenAIEncryptedContextRejection(raw) && openAIWSUncertainExecutionReplaySafe(body)
		kind := "http_error"
		if !cyber && (resp.StatusCode == http.StatusTooManyRequests || modelDenied || retryServerError) {
			kind = "failover"
		}
		recordFailure(resp, raw, kind, fmt.Sprintf("Excel BPS returned HTTP %d", resp.StatusCode), upstreamBody, "generation")
		if cyber {
			return fail(resp.StatusCode, "cyber_policy", "Excel BPS upstream failure: cyber_policy; request was not replayed")
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			result, failure := failoverRateLimited(resp.Header.Get("Retry-After"))
			if rejected, ok := failure.(*UpstreamFailoverError); ok {
				rejected.RejectedBeforeGeneration = !compactionCompleted
			}
			return result, failure
		}
		if isExcelBPSClientCancellation(c, ctx.Err()) {
			return clientCanceled()
		}
		if fileOwnershipFailed {
			return fail(failure.Status, failure.Code, failure.Message)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			s.handleExcelBPSUnauthorized(ctx, account, resp.StatusCode, resp.Header, raw, token)
			return fail(resp.StatusCode, "basispoints_upstream_error", "Excel BPS authentication failed; request was not replayed")
		}
		if modelDenied {
			s.coolDownExcelBPSModel(account, model)
			return nil, newExcelBPSModelPermissionFailoverError()
		}
		if retryServerError && ctx.Err() == nil {
			return nil, newExcelBPSUpstreamFailureError(resp.StatusCode)
		}
		message := "Excel BPS rejected this request; account scheduling was not changed"
		errorCode := "basispoints_upstream_error"
		if resp.StatusCode == http.StatusBadRequest && isExcelBPSInvalidEncryptedContent(raw) {
			errorCode = "invalid_encrypted_content"
			message = "Excel BPS could not verify encrypted conversation state; resend the original plaintext history or start a new conversation"
		}
		if resp.StatusCode == http.StatusForbidden {
			// Apply group routing before the independent protocol switch is disabled.
			moved := s.moveExcelBPSOn403(ctx, account)
			disabled := s.disableExcelBPSOn403(ctx, account)
			switch {
			case moved && disabled:
				message = "Excel BPS rejected this request; Excel BPS was automatically disabled and account groups were updated; request was not replayed"
			case disabled:
				message = "Excel BPS rejected this request; Excel BPS was automatically disabled for this account; request was not replayed"
			case moved:
				message = "Excel BPS rejected this request; account groups were automatically updated; request was not replayed"
			}
		}
		return fail(resp.StatusCode, errorCode, message)
	}
	// BPS and Codex share quota. Refresh at the HTTP boundary even if the client
	// disconnects or a later stream/protocol error prevents normal completion.
	s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, resp.Header)
	// Repair callbacks run on the converter goroutine. The consumer reads this
	// origin only after joining it, separately from the original billing ID.
	diagnosticResponse := resp
	diagnosticBody, diagnosticStage := upstreamBody, "generation"
	converted := bridge.StreamWithRepairs(requestCtx, resp.Body, func(repairCtx context.Context, failed map[string]any, validation error) (map[string]any, error) {

		correctedBody, err := basispoints.BuildToolRepairRequest(upstreamBody, failed, validation)
		if err != nil {
			return nil, err
		}
		repairResp, _, err := s.doExcelBPSRequest(repairCtx, c, account, scope, correctedBody, token, accountID)
		if err != nil {
			if repairCtx.Err() != nil {
				return nil, repairCtx.Err()
			}
			return nil, fmt.Errorf("excel BPS correction connection failed")
		}
		diagnosticBody, diagnosticStage = correctedBody, "tool_repair"
		defer func() { _ = repairResp.Body.Close() }()
		stop := context.AfterFunc(repairCtx, func() { _ = repairResp.Body.Close() })
		defer stop()
		if repairResp.StatusCode >= 200 && repairResp.StatusCode < 300 {
			observeBPSTestResponse(repairCtx, repairResp, model, token, "")
		}
		if repairResp.StatusCode < 200 || repairResp.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(repairResp.Body, 512<<10))
			observeBPSTestResponse(repairCtx, repairResp, model, token, gjson.GetBytes(raw, "error.code").String())
			cyber, _, _ := detectOpenAICyberPolicy(raw)
			if failure := basispoints.ParseHTTPFailure(repairResp.StatusCode, raw); failure != nil && (observeFileOwnershipFailure(failure) || cyber) {
				diagnosticResponse = repairResp
				return nil, failure
			}
			if repairResp.StatusCode == http.StatusTooManyRequests {
				// Output was already accepted: cool the route, never replay the request.
				s.coolDownExcelBPS(repairCtx, account, repairResp.Header.Get("Retry-After"))
			}
			s.handleExcelBPSUnauthorized(repairCtx, account, repairResp.StatusCode, repairResp.Header, raw, token)
			if isExcelBPSModelPermissionError(repairResp.StatusCode, gjson.GetBytes(raw, "error.code").String()) {
				s.coolDownExcelBPSModel(account, model)
			}
			if repairResp.StatusCode == http.StatusForbidden && !isExcelBPSModelPermissionError(http.StatusForbidden, gjson.GetBytes(raw, "error.code").String()) {
				s.moveExcelBPSOn403(repairCtx, account)
				s.disableExcelBPSOn403(repairCtx, account)
			}
			return nil, fmt.Errorf("excel BPS correction returned HTTP %d", repairResp.StatusCode)
		}
		s.UpdateCodexUsageSnapshotFromHeaders(repairCtx, account.ID, repairResp.Header)
		diagnosticResponse = repairResp
		upstreamBody = correctedBody
		return basispoints.ReadToolRepairResponse(repairResp.Body)
	}, func(repairCtx context.Context) (io.ReadCloser, error) {
		repairBody, err := basispoints.RepairRequest(upstreamBody)
		if err != nil {
			return nil, err
		}
		repaired, _, err := s.doExcelBPSRequest(repairCtx, c, account, scope, repairBody, token, accountID)
		if err != nil {
			return nil, fmt.Errorf("excel BPS tool correction transport failed")
		}
		diagnosticBody, diagnosticStage = repairBody, "tool_regeneration"
		if repaired.StatusCode >= 200 && repaired.StatusCode < 300 {
			observeBPSTestResponse(repairCtx, repaired, model, token, "")
		}
		if repaired.StatusCode < 200 || repaired.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(repaired.Body, 512<<10))
			observeBPSTestResponse(repairCtx, repaired, model, token, gjson.GetBytes(raw, "error.code").String())
			_ = repaired.Body.Close()
			cyber, _, _ := detectOpenAICyberPolicy(raw)
			if failure := basispoints.ParseHTTPFailure(repaired.StatusCode, raw); failure != nil && (observeFileOwnershipFailure(failure) || cyber) {
				diagnosticResponse = repaired
				return nil, failure
			}
			if repaired.StatusCode == http.StatusTooManyRequests {
				s.coolDownExcelBPS(repairCtx, account, repaired.Header.Get("Retry-After"))
			}
			s.handleExcelBPSUnauthorized(repairCtx, account, repaired.StatusCode, repaired.Header, raw, token)
			if isExcelBPSModelPermissionError(repaired.StatusCode, gjson.GetBytes(raw, "error.code").String()) {
				s.coolDownExcelBPSModel(account, model)
			}
			if repaired.StatusCode == http.StatusForbidden && !isExcelBPSModelPermissionError(http.StatusForbidden, gjson.GetBytes(raw, "error.code").String()) {
				s.moveExcelBPSOn403(repairCtx, account)
				s.disableExcelBPSOn403(repairCtx, account)
			}
			return nil, fmt.Errorf("excel BPS tool correction returned HTTP %d", repaired.StatusCode)
		}
		s.UpdateCodexUsageSnapshotFromHeaders(repairCtx, account.ID, repaired.Header)
		diagnosticResponse = repaired
		return repaired.Body, nil
	})
	meteredStream := converted
	if len(compactOutput) > 0 {
		converted = basispoints.WithCompactedWindow(requestCtx, converted, compactOutput, compactUsageWire)
	}
	// The bridge sees the body after group policy mapping. Keep the original
	// client effort for usage display, and the BPS-normalized effort for billing.
	requestedEffort := coalesceRequestedReasoningEffort(RequestedReasoningEffortFromContext(ctx), &bridge.RequestedEffort)
	result := &OpenAIForwardResult{Model: originalModel, UpstreamModel: model, UpstreamEndpoint: "/basispoints/api/responses", Stream: stream, ReasoningEffort: &bridge.Effort, RequestedReasoningEffort: requestedEffort, RequestID: resp.Header.Get("x-request-id")}
	var createdAtTracker responsesCreatedAtStamper
	streamFailureRecorded := false
	recordStreamFailure := func(kind string) {
		// A terminal has already been parsed. Join the converter before reading
		// its snapshot, and record diagnostics before Ops freezes the turn.
		_ = converted.Close()
		if !streamFailureRecorded {
			if failure := basispoints.ObservedStreamFailure(meteredStream); failure != nil {
				observeFileOwnershipFailure(failure)
				recordFailure(diagnosticResponse, failure.Diagnostic, kind, failure.Message, diagnosticBody, diagnosticStage)
				streamFailureRecorded = true
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
		if usage := basispoints.ObservedStreamUsage(meteredStream); usage != nil {
			raw, _ := json.Marshal(map[string]any{"usage": usage})
			var observed OpenAIUsage
			s.parseSSEUsageBytes(raw, &observed)
			if openAIUsageHasTokens(&observed) {
				result.Usage = observed // compaction is added once by the outer defer
				if result.RequestID == "" {
					result.RequestID = result.ResponseID
					if result.RequestID == "" {
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
	}()
	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
	}
	scanner := newOpenAISSEReadPump(converted, 16<<20)
	defer scanner.Close()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	keepalive := func() {
		if stream && ctx.Err() == nil {
			_, _ = c.Writer.WriteString(": keepalive\n\n")
			c.Writer.Flush()
		}
	}
	var completed []byte
	var upstreamFailure *basispoints.UpstreamFailure
	var staged strings.Builder
	contentCommitted := false
	// Buffer lifecycle metadata as the native Responses bridge does. A created
	// event alone must not prevent recovery or expose a failed response ID.
	writeStream := func(line string) error {
		_, writeErr := c.Writer.WriteString(line + "\n")
		if line == "" {
			c.Writer.Flush()
		}
		return writeErr
	}
	terminal := ""
	lastEventType, eventsSeen, nextSequence := "none", 0, int64(0)
	cacheCreationAsInput := account.IsExcelBPSCacheCreationAsInputEnabled()
	for scanner.Next(ctx, 0, heartbeat.C, keepalive) {
		line := scanner.Text()
		// Frame from the normalized JSON type so a response-level failure without
		// an ID can become a request error consistently for SSE and WS clients.
		if strings.HasPrefix(line, "event:") {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			payload := []byte(strings.TrimPrefix(line, "data: "))
			kind := gjson.GetBytes(payload, "type").String()
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
			startsOutput := kind != "error" && kind != "response.failed" && kind != "response.cancelled" && openAIStreamDataStartsClientOutput(string(payload), kind)
			if startsOutput {
				if !stopFirstOutput() {
					break
				}
			}
			s.parseSSEUsageBytes(payload, &result.Usage)
			if kind == "response.failed" && gjson.GetBytes(payload, "response.id").String() == "" {
				payload = buildOpenAIWSHTTPBridgeFailedEvent(result.ResponseID, originalModel, createdAtTracker.Value(), payload, "Excel BPS response failed")
				payload, _ = sjson.SetBytes(payload, "sequence_number", sequence)
				kind = gjson.GetBytes(payload, "type").String()
				line = "data: " + string(payload)
			}

			if len(compactOutput) > 0 && (kind == "response.completed" || kind == "response.failed" || kind == "response.cancelled" || kind == "response.incomplete") {
				subtractImagePolicyUsage(&result.Usage, compactUsage)
			}
			if cacheCreationAsInput {
				payload, err = excelBPSDownstreamUsage(payload)
				if err != nil {
					return fail(http.StatusBadGateway, "basispoints_usage_invalid", "Excel BPS usage could not be normalized")
				}
				line = "data: " + string(payload)
			}
			if result.FirstTokenMs == nil && startsOutput {
				ms := int(time.Since(start).Milliseconds())
				result.FirstTokenMs = &ms
			}
			switch kind {
			case "response.completed", "response.failed", "response.cancelled", "response.incomplete", "error":
				terminal = kind
				completed = []byte(gjson.GetBytes(payload, "response").Raw)
				result.UpstreamResponseModel = gjson.GetBytes(payload, "response.model").String()
				result.UpstreamIncompleteReason = gjson.GetBytes(payload, "response.incomplete_details.reason").String()
				upstreamFailure = basispoints.ParseUpstreamFailure(payload)
				if upstreamFailure != nil {
					cyber, _, _ := detectOpenAICyberPolicy(payload)
					// Classification may normalize unknown code spellings. Use the
					// original joined diagnostic for native cyber detection before retry.
					_ = converted.Close()
					if original := basispoints.ObservedStreamFailure(meteredStream); original != nil {
						cyber, _, _ = detectOpenAICyberPolicy(original.Diagnostic)
					}
					modelDenied := isExcelBPSModelPermissionError(upstreamFailure.Status, upstreamFailure.Code)
					if modelDenied {
						s.coolDownExcelBPSModel(account, model)
					}
					rateLimited := !cyber && upstreamFailure.Status == http.StatusTooManyRequests
					if rateLimited {
						s.coolDownExcelBPS(ctx, account, resp.Header.Get("Retry-After"))
					}
					retryRateLimit := rateLimited && diagnosticStage == "generation" && openAIWSUncertainExecutionReplaySafe(body)
					// Explicit rejection and transient 5xx use the existing account
					// switch loop; unknown failures and cancellation are not replayed.
					// Normalization removes signature diagnostics; retain the code's
					// rejection even when the event reports a transient HTTP status.
					retryServerError := !cyber && upstreamFailure.Status >= 500 && upstreamFailure.Code != basispoints.FileOwnershipFailureCode && !isOpenAIEncryptedContextRejection(payload) && upstreamFailure.Code != "thinking_signature_invalid" &&
						(upstreamFailure.Code == "server_error" || upstreamFailure.Code == "internal_server_error" || upstreamFailure.Status == http.StatusServiceUnavailable) &&
						openAIWSUncertainExecutionReplaySafe(body)
					if (kind == "error" || kind == "response.failed") && !contentCommitted && ctx.Err() == nil && (modelDenied || retryServerError || retryRateLimit) {
						result.Duration = time.Since(start)
						if result.RequestID == "" {
							result.RequestID = result.ResponseID // billing only; this ID was not delivered
						}
						result.ResponseID = ""
						if retryRateLimit {
							return result, newExcelBPSRateLimitedFailoverError(resp.Header.Get("Retry-After"))
						}
						if modelDenied {
							return result, newExcelBPSModelPermissionFailoverError()
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
		}
		if terminal != "" && !stopFirstOutput() {
			terminal = ""
			break
		}
		if stream {
			if strings.HasPrefix(line, "data: ") {
				if kind := gjson.Get(strings.TrimPrefix(line, "data: "), "type").String(); kind != "" {
					line = "event: " + kind + "\n" + line
				}
			}
			if !contentCommitted && terminal == "" && result.FirstTokenMs == nil && staged.Len()+len(line)+1 <= int(openAIFirstOutputStageMaxBytes) {
				staged.WriteString(line + "\n")
				continue
			}
			if staged.Len() > 0 {
				_, err = c.Writer.WriteString(staged.String())
				staged.Reset()
			}
			contentCommitted = true
			if err == nil {
				err = writeStream(line)
			}
			if err != nil {
				result.ClientDisconnect = true
				if isExcelBPSClientCancellation(c, c.Request.Context().Err()) {
					MarkOpsClientCancellation(c, stream)
				}
				result.Duration = time.Since(start)
				return result, err
			}
		}
		if terminal != "" {
			if stream {
				_, _ = c.Writer.WriteString("\n")
				c.Writer.Flush()
			}
			break // a delivered terminal wins over a later cancellation/EOF
		}
	}
	result.Duration = time.Since(start)
	result.UpstreamTerminalEvent = terminal
	if err = scanner.Err(); err != nil || terminal == "" {
		if parentCtx.Err() != nil {
			if isExcelBPSClientCancellation(c, parentCtx.Err()) {
				MarkOpsClientCancellation(c, stream)
			}
			result.ClientDisconnect = true
			return result, parentCtx.Err()
		}
		// Join conversion/repair before reading the final HTTP identity. EOF does
		// not prove the generation was unaccepted, so retain the no-replay boundary.
		_ = converted.Close()
		endReason := "read_error"
		if errors.Is(err, basispoints.ErrStreamIncomplete) {
			endReason = "missing_terminal"
		} else if firstOutputGuard.expired() {
			endReason = "first_output_timeout"
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
		}
		MarkOpsStreamError(c, code, message, status)
		MarkResponseCommitted(c)
		if stream {
			source, _ := json.Marshal(gin.H{"error": gin.H{"type": "server_error", "code": code, "message": message}})
			failure := buildOpenAIWSHTTPBridgeFailedEvent(result.ResponseID, originalModel, createdAtTracker.Value(), source, message)
			if contentCommitted {
				failure, _ = sjson.SetBytes(failure, "sequence_number", nextSequence)
			}
			result.UpstreamTerminalEvent = gjson.GetBytes(failure, "type").String()
			_, _ = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", result.UpstreamTerminalEvent, failure)
			c.Writer.Flush()
		} else {
			c.JSON(status, gin.H{"error": gin.H{"type": "server_error", "code": code, "message": message}})
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
			c.Data(200, "application/json", completed)
		}
	}
	if !success {
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

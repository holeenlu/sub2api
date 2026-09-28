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
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var excelBPSReplay basispoints.ReplayCache
var excelBPSCatalog basispoints.CatalogCache

// BPS uses the account's OAuth credentials, so authentication failures must
// update the same scheduling state as ordinary OpenAI requests. Keep arbitrary
// BPS error text (which may echo request data) out of persisted account reasons.
func (s *OpenAIGatewayService) handleExcelBPSUnauthorized(ctx context.Context, account *Account, status int, headers http.Header, raw []byte) {
	if status != http.StatusUnauthorized || s.rateLimitService == nil {
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
	relay, _ := s.excelBPSImageRelay(c.Request.Context())
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, basispoints.ResponsesURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Authorization": {"Bearer " + token}, "Chatgpt-Account-Id": {accountID}, "X-Openai-Account-Id": {accountID},
		"X-Basispoints-Auth-Mode": {"chatgpt"}, "Content-Type": {"application/json"}, "Accept": {"text/event-stream"},
		"Origin": {"https://bps.openai.com"}, "User-Agent": {"Mozilla/5.0"},
		"X-Openai-Internal-Basispoints-Client-Product":       {"basispoints-excel-plugin"},
		"X-Openai-Internal-Basispoints-Client-Agent-Profile": {"excel"},
	}
	return (&transportdiag.Trace{}).Request(req), nil
}

// BPS deliberately bypasses Codex ticket/cookie injection and OAuth plugins:
// only the selected account's bearer and ChatGPT account ID belong on this host.
func (s *OpenAIGatewayService) forwardExcelBPS(ctx context.Context, c *gin.Context, account *Account, body []byte, start time.Time) (forwardResult *OpenAIForwardResult, forwardErr error) {
	var compactUsage OpenAIUsage
	var compactID string
	originalImagePolicyModel := gjson.GetBytes(body, "model").String()
	var compactEffort string
	var compactCommitted bool
	var fail func(int, string, string, ...string) (*OpenAIForwardResult, error)
	defer func() {
		var failover *UpstreamFailoverError
		if (compactCommitted || openAIUsageHasTokens(&compactUsage)) && (IsOpenAITurnAdmissionError(forwardErr) || IsOpenAIRPMError(forwardErr) || errors.As(forwardErr, &failover)) {
			// Compaction has consumed usage and may have saved account-bound state.
			// Keep this a billable terminal error, never replay it on another account.
			status := http.StatusServiceUnavailable
			if IsOpenAIRPMError(forwardErr) || (failover != nil && failover.StatusCode == http.StatusTooManyRequests) {
				status = http.StatusTooManyRequests
			}
			_, forwardErr = fail(status, "basispoints_image_continuation_interrupted", "Image history was compacted but continuation could not start; retry with the same conversation and complete context")
		}

		if compactID == "" || !openAIUsageHasTokens(&compactUsage) {
			return
		}
		if forwardResult == nil {
			forwardResult = &OpenAIForwardResult{Model: originalImagePolicyModel, ReasoningEffort: &compactEffort, UpstreamModel: gjson.GetBytes(body, "model").String(), UpstreamEndpoint: "/basispoints/api/responses", RequestID: compactID, Stream: gjson.GetBytes(body, "stream").Bool(), Duration: time.Since(start)}
		}
		addImagePolicyUsage(&forwardResult.Usage, compactUsage)
	}()
	fail = func(status int, code, message string, param ...string) (*OpenAIForwardResult, error) {
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
			switch {
			case status >= 500:
				errorType = "server_error"
			case status == http.StatusTooManyRequests:
				errorType = "rate_limit_error"
			}
			errorBody := gin.H{"type": errorType, "code": code, "message": message}
			if len(param) > 0 && param[0] != "" {
				errorBody["param"] = param[0]
			}
			c.JSON(status, gin.H{"error": errorBody})
		}
		return nil, fmt.Errorf("excel BPS: %s", code)
	}
	originalModel := gjson.GetBytes(body, "model").String()
	model := account.GetMappedModel(originalModel)
	stream := gjson.GetBytes(body, "stream").Bool()
	clientCanceled := func() (*OpenAIForwardResult, error) {
		StopOpenAICompactSSEKeepaliveCommitted(c)
		MarkResponseCommitted(c)
		MarkOpsClientCancellation(c, stream)
		// No response or metered usage exists before headers; do not create a usage row.
		return nil, context.Canceled
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
	var err error
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return fail(400, "basispoints_request_invalid", "Invalid model request")
	}
	// BPS never runs hosted tools; the bridge omits their declarations and
	// tells the model. A forced selection of one would otherwise fail the
	// whole request, so let the model proceed in automatic mode instead.
	var relaxedChoice string
	body, relaxedChoice, err = basispoints.RelaxHostedToolChoice(body)
	if err != nil {
		return fail(400, "basispoints_request_invalid", "Invalid tool_choice request")
	}
	if relaxedChoice != "" {
		logger.LegacyPrintf("service.openai_excel_bps", "relaxed forced hosted tool_choice to auto: account_id=%d tool=%s", account.ID, relaxedChoice)
	}
	identity, _ := resolveOpenAIWSExecutionScope(c, body, getAPIKeyIDFromContext(c))
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
	imageSettings, err := s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		return fail(503, "basispoints_image_settings_unavailable", "Excel BPS image settings are unavailable")
	}
	if !imageSettings.Enabled && account.IsExcelBPSIgnoreImagesEnabled() {
		body, err = basispoints.StripInputImages(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
	}
	if account.IsExcelBPSIgnoreEncryptedContentEnabled() {
		body, err = basispoints.StripEncryptedContent(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
	}
	imagePolicy, err := s.prepareExcelImagePolicy(ctx, c, body, imageSettings, scope+"/model:"+model, identity != "")
	if err != nil {
		var policyErr *excelImagePolicyError
		if errors.As(err, &policyErr) {
			return fail(policyErr.Status, policyErr.Code, policyErr.Message)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	if len(imagePolicy.reconciledBody) > 0 {
		maxImageMiB, maxTotalMiB := imageSettings.Limits.MaxImageMiB, imageSettings.Limits.MaxTotalMiB
		if imageSettings.Mode == ExcelBPSImageModeNative {
			maxImageMiB, maxTotalMiB = 20, 32
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
		images, err = basispoints.PrepareNativeImagesWithLimit(body, imagePolicy.maxImages)
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
	}
	var upstreamBody []byte
	var bridge *basispoints.Bridge
	if images != nil {
		upstreamBody, bridge, err = images.PrepareWithCatalog(scope, replay, catalog)
	} else {
		upstreamBody, bridge, err = basispoints.PrepareWithCatalog(body, scope, replay, catalog)
	}
	if err != nil {
		var contentErr *basispoints.ContentValidationError
		if errors.As(err, &contentErr) {
			return fail(400, "basispoints_request_invalid", err.Error(), contentErr.Path)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		return fail(502, "basispoints_auth_unavailable", "Account OAuth credential is unavailable")
	}
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return fail(400, "basispoints_account_id_missing", "Excel BPS requires chatgpt_account_id")
	}
	if images != nil && images.HasImages() {
		attachmentScope := ""
		if identity != "" {
			attachmentScope = scope + "\x00" + accountID + "\x00" + token
		}
		body, err = images.Upload(ctx, &s.excelBPSAttachments, attachmentScope, func(uploadCtx context.Context, img basispoints.InlineAttachment) (string, error) {
			latest, err := s.admitOpenAITurn(uploadCtx, c, account, model)
			if err != nil {
				return "", err
			}
			SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/attachments")
			return s.uploadExcelBPSAttachment(uploadCtx, latest, token, accountID, img)
		})
		if err != nil {
			if IsOpenAITurnAdmissionError(err) || IsOpenAIRPMError(err) {
				return nil, err
			}
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
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
					ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
					UpstreamStatusCode: status, UpstreamURL: basispoints.AttachmentsURL, Kind: "failover",
					Message: "Excel BPS attachment upload was rate limited",
				})
				return failoverRateLimited(uploadError.retryAfter)
			}
			setOpsUpstreamError(c, status, "Excel BPS attachment upload failed", "")
			if status == http.StatusUnauthorized {
				return fail(status, code, "Excel BPS attachment authentication failed; request was not replayed")
			}
			return fail(status, code, "Excel BPS attachment upload failed; request was not replayed and account scheduling was not changed")
		}
		upstreamBody, bridge, err = bridge.Reprepare(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
	}
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileExcelBPS))
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	if imagePolicy.split > 0 {
		logger.LegacyPrintf("service.openai_excel_bps", "image policy auto compact: account_id=%d history_images=%d new_images=%d limit=%d", account.ID, basispoints.CountInlineImages(imagePolicy.history.Input[:imagePolicy.split]), basispoints.CountInlineImages(imagePolicy.history.Input[imagePolicy.split:]), imageSettings.Limits.MaxImages)
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
		compactReq, compactErr := newExcelBPSRequest(requestCtx, compactWire, token, accountID)
		if compactErr != nil {
			return nil, compactErr
		}
		cr, compactErr := s.doExcelBPSSend(requestCtx, c, account, model, compactReq, proxyURL)
		var compactRateLimit []byte
		if compactErr == nil {
			compactReq, cr, compactRateLimit, compactErr = s.retryExcelBPSStartRateLimit(requestCtx, c, account, model, proxyURL, token, compactReq, cr, func() (*http.Request, error) {
				return newExcelBPSRequest(requestCtx, compactWire, token, accountID)
			})
		}
		if compactErr != nil {
			if IsOpenAITurnAdmissionError(compactErr) || IsOpenAIRPMError(compactErr) {
				return nil, compactErr
			}
			if isExcelBPSClientCancellation(c, compactErr) {
				return clientCanceled()
			}
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction connection failed; generation was not started")
		}
		if compactRateLimit != nil {
			_ = cr.Body.Close()
			delay := excelBPSClientRateLimitDelay(cr.Header.Get("Retry-After"), compactRateLimit)
			c.Header("Retry-After", strconv.FormatInt(excelBPSCeilSeconds(delay), 10))
			return fail(http.StatusTooManyRequests, excelBPSClientRateLimitCode, excelBPSClientRateLimitMessage(delay))
		}
		compactID = cr.Header.Get("x-request-id")
		if compactID == "" {
			compactID = "image-compaction"
		}
		if cr.StatusCode < 200 || cr.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(cr.Body, 512<<10))
			_ = cr.Body.Close()
			s.handleExcelBPSUnauthorized(ctx, account, cr.StatusCode, cr.Header, raw)
			if cr.StatusCode == http.StatusTooManyRequests {
				return failoverRateLimited(excelBPSRateLimitRetryAfter(cr.Header.Get("Retry-After"), raw))
			}
			if cr.StatusCode == 403 && gjson.GetBytes(raw, "error.code").String() != "basispoints_model_access_changed" {
				s.moveExcelBPSOn403(ctx, account)
				s.disableExcelBPSOn403(ctx, account)
			}
			return fail(cr.StatusCode, "basispoints_image_compaction_failed", "Image history compaction was rejected; generation was not started")
		}
		s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, cr.Header)
		compactEffort = bridge.Effort
		compactResponse, compactErr := basispoints.ReadImageCompaction(cr.Body, func(payload []byte) { s.parseSSEUsageBytes(payload, &compactUsage) })
		_ = cr.Body.Close()
		if compactResponse != nil {
			encoded, _ := json.Marshal(map[string]any{"type": "response.completed", "response": compactResponse})
			s.parseSSEUsageBytes(encoded, &compactUsage)
			compactUsageWire, _ = compactResponse["usage"].(map[string]any)
		}
		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction did not complete; generation was not started")
		}
		window, _, compactErr := basispoints.CompactWindow(compactResponse)
		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", compactErr.Error())
		}
		next := append(append([]any{}, window...), uploaded.Input[imagePolicy.split:]...)
		// Count retained inline tool images, not the uploaded attachment placeholders.
		if basispoints.CountInlineImages(window)+basispoints.CountInlineImages(imagePolicy.history.Input[imagePolicy.split:]) > imageSettings.Limits.MaxImages {
			return fail(400, "basispoints_image_compaction_insufficient", "Compacted history still exceeds the image limit; reduce new images or compact manually")
		}
		body, compactErr = uploaded.WithInput(next, false)
		if compactErr != nil {
			return fail(400, "basispoints_request_invalid", "Could not prepare compacted continuation")
		}
		upstreamBody, bridge, compactErr = bridge.Reprepare(body)
		if compactErr != nil {
			return fail(400, "basispoints_request_invalid", compactErr.Error())
		}
		if compactErr = imagePolicy.checkpoint(ctx, window); compactErr != nil {
			return fail(503, "basispoints_image_checkpoint_unavailable", "Could not save compacted history safely; retry or run compact manually")
		}
		compactCommitted = true
		compactOutput = window
	}
	req, err := newExcelBPSRequest(requestCtx, upstreamBody, token, accountID)
	if err != nil {
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
	SetOpsUpstreamModel(c, model)
	sent := time.Now()
	resp, err := s.doExcelBPSSend(requestCtx, c, account, model, req, proxyURL)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
	if err != nil {
		if IsOpenAITurnAdmissionError(err) || IsOpenAIRPMError(err) {
			return nil, err
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, err, "transport", 1, false, transportdiag.FromContext(req.Context()))
		return fail(502, "basispoints_transport_error", "Excel BPS connection failed; request was not replayed")
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	if resp.StatusCode == http.StatusBadRequest {
		// Read and close before retrying: the HTTP body owns the account's
		// concurrency slot. Keep the proxy lease for the exact same exit.
		const maxRejectionBytes = 512 << 10
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxRejectionBytes+1))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		if retryBody, retry := prepareExcelBPSInvalidEncryptedRetry(upstreamBody, raw); retry && readErr == nil && len(raw) <= maxRejectionBytes && ctx.Err() == nil {
			retryReq, retryErr := newExcelBPSRequest(requestCtx, retryBody, token, accountID)
			if retryErr != nil {
				return fail(502, "basispoints_transport_error", "Excel BPS recovery request could not be prepared")
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
				ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
				UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
				UpstreamURL: basispoints.ResponsesURL, Kind: "invalid_encrypted_content_retry",
				Message: "Excel BPS rejected encrypted reasoning; retrying once without opaque reasoning on the same route",
			})
			logger.LegacyPrintf("service.openai_excel_bps", "retrying invalid encrypted reasoning once: account_id=%d", account.ID)
			c.Set("excel_bps_upstream_attempt", c.GetInt("excel_bps_upstream_attempt")+1)
			// Do not re-enter proxy acquisition or transport retries after sending.
			req = retryReq
			resp, err = s.doExcelBPSSend(requestCtx, c, account, model, req, proxyURL)
			SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
			if err != nil {
				if IsOpenAITurnAdmissionError(err) || IsOpenAIRPMError(err) {
					return nil, err
				}
				if isExcelBPSClientCancellation(c, err) {
					return clientCanceled()
				}
				return fail(502, "basispoints_transport_error", "Excel BPS recovery connection failed; request was not replayed again")
			}
			upstreamBody = retryBody
		}
	}
	var startRateLimit []byte
	req, resp, startRateLimit, err = s.retryExcelBPSStartRateLimit(requestCtx, c, account, model, proxyURL, token, req, resp, func() (*http.Request, error) {
		return newExcelBPSRequest(requestCtx, upstreamBody, token, accountID)
	})
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
	if err != nil {
		if IsOpenAITurnAdmissionError(err) || IsOpenAIRPMError(err) {
			return nil, err
		}
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, err, "transport", c.GetInt("excel_bps_upstream_attempt")+1, false, transportdiag.FromContext(req.Context()))
		return fail(502, "basispoints_transport_error", "Excel BPS rate-limit retry connection failed; request was not replayed again")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		// BPS throttles its own endpoint. A BPS 429 must not write Codex
		// quota/cooldown state; it cools only the BPS route and fails over.
		// Preserve the original rejection for Ops without exposing it to clients.
		// BPS errors can echo request fields, so redact before storing diagnostics.
		upstreamMessage := fmt.Sprintf("Excel BPS returned HTTP %d", resp.StatusCode)
		upstreamDetail := ""
		if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
			safeBody := excelBPSSanitizeErrorBody(string(raw), token, account)
			maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
			if maxBytes <= 0 {
				maxBytes = 2048
			}
			upstreamDetail, _ = sanitizeErrorBodyForStorage(safeBody, maxBytes)
			if message := strings.TrimSpace(extractUpstreamErrorMessage([]byte(safeBody))); message != "" {
				upstreamMessage = truncateString(message, 2048)
			}
		}
		// A failover attempt is only an event; the handler records the final state.
		kind := "failover"
		if resp.StatusCode != http.StatusTooManyRequests {
			kind = "http_error"
			setOpsUpstreamError(c, resp.StatusCode, upstreamMessage, upstreamDetail)
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
			UpstreamURL: basispoints.ResponsesURL, Kind: kind,
			Message: upstreamMessage, Detail: upstreamDetail, UpstreamResponseBody: upstreamDetail,
		})
		if resp.StatusCode == http.StatusTooManyRequests {
			return failoverRateLimited(excelBPSRateLimitRetryAfter(resp.Header.Get("Retry-After"), raw))
		}
		if resp.StatusCode == http.StatusUnauthorized {
			s.handleExcelBPSUnauthorized(ctx, account, resp.StatusCode, resp.Header, raw)
			return fail(resp.StatusCode, "basispoints_upstream_error", "Excel BPS authentication failed; request was not replayed")
		}
		code := gjson.GetBytes(raw, "error.code").String()
		if code == "basispoints_model_access_changed" {
			return fail(resp.StatusCode, code, "This model is not available on the account's Excel BPS endpoint")
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
	if startRateLimit != nil {
		// Retries are exhausted and nothing was generated. The limit belongs to
		// the provider organization shared by all BPS accounts: do not cool this
		// account or fail over, and answer with a plain retryable 429.
		message, detail := s.excelBPSRateLimitOpsDetail(startRateLimit, token, account)
		setOpsUpstreamError(c, http.StatusTooManyRequests, message, detail)
		delay := excelBPSClientRateLimitDelay(resp.Header.Get("Retry-After"), startRateLimit)
		c.Header("Retry-After", strconv.FormatInt(excelBPSCeilSeconds(delay), 10))
		return fail(http.StatusTooManyRequests, excelBPSClientRateLimitCode, excelBPSClientRateLimitMessage(delay))
	}
	converted := bridge.StreamWithRepairs(requestCtx, resp.Body, func(repairCtx context.Context, failed map[string]any, validation error) (map[string]any, error) {
		correctedBody, err := basispoints.BuildToolRepairRequest(upstreamBody, failed, validation)
		if err != nil {
			return nil, err
		}
		repairReq, err := newExcelBPSRequest(repairCtx, correctedBody, token, accountID)
		if err != nil {
			return nil, err
		}
		repairResp, err := s.doExcelBPSSend(repairCtx, c, account, model, repairReq, proxyURL)
		if err != nil {
			if IsOpenAITurnAdmissionError(err) || IsOpenAIRPMError(err) {
				return nil, err
			}
			if repairCtx.Err() != nil {
				return nil, repairCtx.Err()
			}
			return nil, fmt.Errorf("excel BPS correction connection failed")
		}
		defer func() { _ = repairResp.Body.Close() }()
		stop := context.AfterFunc(repairCtx, func() { _ = repairResp.Body.Close() })
		defer stop()
		if repairResp.StatusCode < 200 || repairResp.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(repairResp.Body, 512<<10))
			if repairResp.StatusCode == http.StatusTooManyRequests {
				// Output was already accepted: cool the route, never replay the request.
				s.coolDownExcelBPS(repairCtx, account, excelBPSRateLimitRetryAfter(repairResp.Header.Get("Retry-After"), raw))
			}
			s.handleExcelBPSUnauthorized(repairCtx, account, repairResp.StatusCode, repairResp.Header, raw)
			if repairResp.StatusCode == http.StatusForbidden && gjson.GetBytes(raw, "error.code").String() != "basispoints_model_access_changed" {
				s.moveExcelBPSOn403(repairCtx, account)
				s.disableExcelBPSOn403(repairCtx, account)
			}
			return nil, fmt.Errorf("excel BPS correction returned HTTP %d", repairResp.StatusCode)
		}
		s.UpdateCodexUsageSnapshotFromHeaders(repairCtx, account.ID, repairResp.Header)
		upstreamBody = correctedBody
		return basispoints.ReadToolRepairResponse(repairResp.Body)
	}, func(repairCtx context.Context) (io.ReadCloser, error) {
		repairBody, err := basispoints.RepairRequest(upstreamBody)
		if err != nil {
			return nil, err
		}
		retry, err := newExcelBPSRequest(repairCtx, repairBody, token, accountID)
		if err != nil {
			return nil, err
		}
		repaired, err := s.doExcelBPSSend(repairCtx, c, account, model, retry, proxyURL)
		if err != nil {
			if IsOpenAITurnAdmissionError(err) || IsOpenAIRPMError(err) {
				return nil, err
			}
			return nil, fmt.Errorf("excel BPS tool correction transport failed")
		}
		if repaired.StatusCode < 200 || repaired.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(repaired.Body, 512<<10))
			_ = repaired.Body.Close()
			if repaired.StatusCode == http.StatusTooManyRequests {
				s.coolDownExcelBPS(repairCtx, account, excelBPSRateLimitRetryAfter(repaired.Header.Get("Retry-After"), raw))
			}
			s.handleExcelBPSUnauthorized(repairCtx, account, repaired.StatusCode, repaired.Header, raw)
			if repaired.StatusCode == http.StatusForbidden && gjson.GetBytes(raw, "error.code").String() != "basispoints_model_access_changed" {
				s.moveExcelBPSOn403(repairCtx, account)
				s.disableExcelBPSOn403(repairCtx, account)
			}
			return nil, fmt.Errorf("excel BPS tool correction returned HTTP %d", repaired.StatusCode)
		}
		s.UpdateCodexUsageSnapshotFromHeaders(repairCtx, account.ID, repaired.Header)
		return repaired.Body, nil
	})
	if len(compactOutput) > 0 {
		converted = basispoints.WithCompactedWindow(requestCtx, converted, compactOutput, compactUsageWire)
	}
	defer func() { _ = converted.Close() }()
	// The bridge sees the body after group policy mapping. Keep the original
	// client effort for usage display, and the BPS-normalized effort for billing.
	requestedEffort := coalesceRequestedReasoningEffort(RequestedReasoningEffortFromContext(ctx), &bridge.RequestedEffort)
	result := &OpenAIForwardResult{Model: originalModel, UpstreamModel: model, UpstreamEndpoint: "/basispoints/api/responses", Stream: stream, ReasoningEffort: &bridge.Effort, RequestedReasoningEffort: requestedEffort, RequestID: resp.Header.Get("x-request-id")}
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
	var completed, lastResponse []byte
	terminal := ""
	pendingEvent := ""
	cacheCreationAsInput := account.IsExcelBPSCacheCreationAsInputEnabled()
	for scanner.Next(ctx, 0, heartbeat.C, keepalive) {
		line := scanner.Text()
		// The bridge emits one event line then one data line. Hold only that
		// header so an immediate failure can still return its real HTTP status,
		// and a rewritten failure can rename its event.
		if stream && strings.HasPrefix(line, "event: ") {
			pendingEvent = line + "\n"
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			payload := []byte(strings.TrimPrefix(line, "data: "))
			kind := gjson.GetBytes(payload, "type").String()
			s.parseSSEUsageBytes(payload, &result.Usage)
			if len(compactOutput) > 0 && (kind == "response.completed" || kind == "response.failed" || kind == "response.incomplete") {
				subtractImagePolicyUsage(&result.Usage, compactUsage)
			}
			if cacheCreationAsInput {
				payload, err = excelBPSDownstreamUsage(payload)
				if err != nil {
					return fail(http.StatusBadGateway, "basispoints_usage_invalid", "Excel BPS usage could not be normalized")
				}
				line = "data: " + string(payload)
			}
			if result.FirstTokenMs == nil && (kind == "response.output_text.delta" || kind == "response.output_item.added") {
				ms := int(time.Since(start).Milliseconds())
				result.FirstTokenMs = &ms
			}
			switch kind {
			case "response.completed", "response.failed", "response.incomplete", "error":
				terminal = kind
				completed = []byte(gjson.GetBytes(payload, "response").Raw)
				result.ResponseID = gjson.GetBytes(payload, "response.id").String()
				result.UpstreamResponseModel = gjson.GetBytes(payload, "response.model").String()
			}
			if kind == "response.created" || kind == "response.in_progress" {
				lastResponse = []byte(gjson.GetBytes(payload, "response").Raw)
			}
			if isExcelBPSStreamRateLimit(payload) {
				// Output or a tool correction may already exist, even if the bridge
				// withheld it, so this stage never replays. The limit belongs to the
				// provider organization shared by all BPS accounts: no cooldown.
				message, detail := s.excelBPSRateLimitOpsDetail(payload, token, account)
				setOpsUpstreamError(c, http.StatusTooManyRequests, message, detail)
				delay := excelBPSClientRateLimitDelay(resp.Header.Get("Retry-After"), payload)
				clientMessage := excelBPSClientRateLimitMessage(delay)
				MarkResponseCommitted(c)
				MarkOpsStreamErrorValue(c, OpsStreamError{
					ErrType: "rate_limit_error", Code: excelBPSClientRateLimitCode,
					Message: clientMessage, IntendedStatus: http.StatusTooManyRequests,
					NonStream: !stream, CountTowardsSLA: true,
				})
				result.Duration = time.Since(start)
				result.UpstreamTerminalEvent = terminal
				safeError := gin.H{"type": "rate_limit_error", "code": excelBPSClientRateLimitCode, "message": clientMessage}
				if !stream || !c.Writer.Written() {
					c.Header("Retry-After", strconv.FormatInt(excelBPSCeilSeconds(delay), 10))
					c.Header("Content-Type", "application/json")
					c.JSON(http.StatusTooManyRequests, gin.H{"error": safeError})
					return result, fmt.Errorf("excel BPS: %s", excelBPSClientRateLimitCode)
				}
				// Clients classify rate limits from response.failed; Codex ignores
				// a top-level error event. Keep accepted output and usage, but never
				// echo the upstream message or its diagnostic fields.
				var response, terminalResponse map[string]any
				if err = json.Unmarshal(lastResponse, &response); err != nil || response == nil {
					response = make(map[string]any)
				}
				if json.Unmarshal(completed, &terminalResponse) == nil {
					for key, value := range terminalResponse {
						response[key] = value
					}
				}
				response["status"] = "failed"
				response["error"] = safeError
				payload, err = json.Marshal(gin.H{"type": "response.failed", "sequence_number": gjson.GetBytes(payload, "sequence_number").Int(), "response": response})
				if err != nil {
					return result, err
				}
				pendingEvent = "event: response.failed\n"
				line = "data: " + string(payload)
			}
		}
		if stream {
			if _, err = c.Writer.WriteString(pendingEvent + line + "\n"); err != nil {
				result.ClientDisconnect = true
				if isExcelBPSClientCancellation(c, c.Request.Context().Err()) {
					MarkOpsClientCancellation(c, stream)
				}
				result.Duration = time.Since(start)
				return result, err
			}
			pendingEvent = ""
			if line == "" {
				c.Writer.Flush()
			}
		}
	}
	result.Duration = time.Since(start)
	result.UpstreamTerminalEvent = terminal
	if err = scanner.Err(); err != nil || terminal == "" {
		if ctx.Err() != nil {
			if isExcelBPSClientCancellation(c, ctx.Err()) {
				MarkOpsClientCancellation(c, stream)
			}
			result.ClientDisconnect = true
			return result, ctx.Err()
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, err, "stream", 1, false, transportdiag.FromContext(req.Context()))
		MarkOpsStreamError(c, "basispoints_stream_incomplete", "Excel BPS stream ended before completion", http.StatusBadGateway)
		MarkResponseCommitted(c)
		if stream {
			_, _ = c.Writer.WriteString("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"type\":\"server_error\",\"code\":\"basispoints_stream_incomplete\",\"message\":\"Upstream stream ended before completion\"}}}\n\n")
			c.Writer.Flush()
		} else {
			c.JSON(502, gin.H{"error": gin.H{"type": "server_error", "code": "basispoints_stream_incomplete", "message": "Excel BPS stream ended before completion"}})
		}
		return result, fmt.Errorf("excel BPS stream incomplete")
	}
	if terminal != "response.completed" {
		MarkResponseCommitted(c)
	}
	if !stream {
		if terminal != "response.completed" {
			c.JSON(502, gin.H{"error": gin.H{"code": "basispoints_protocol_error", "message": "Excel BPS did not complete the response"}})
		} else {
			c.Data(200, "application/json", completed)
		}
	}
	if terminal != "response.completed" {
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

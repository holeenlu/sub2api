package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/imroc/req/v3"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type cloudContinuationKey struct{}
type cloudWebsocketModelKey struct{}

func cloudContinuationFromContext(ctx context.Context) *OpenAICloudBinding {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(cloudContinuationKey{}).(*OpenAICloudBinding)
	return b
}

func HasOpenAICloudContinuation(ctx context.Context) bool {
	return cloudContinuationFromContext(ctx) != nil
}

// Credentials rotate independently of execution settings. A setting change
// invalidates pending work without accidentally making every refresh a logout.
func cloudSettingsDigest(a *Account) string {
	if a == nil {
		return ""
	}
	b, _ := json.Marshal(struct {
		Protocol string
		Enabled  bool
		Mapping  map[string]string
		ProxyID  *int64
		Identity string
		Proxy    string
	}{OpenAICloudProtocolVersion, a.IsOpenAICloudEnabled(), a.GetModelMapping(), a.ProxyID, a.GetChatGPTAccountID() + "/" + a.GetChatGPTUserID(), cloudAccountProxy(a)})
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func (s *OpenAIGatewayService) SetCloudService(cloud *OpenAICloudService) {
	if s == nil {
		return
	}
	s.cloudService = cloud
	if cloud == nil {
		return
	}
	cloud.gateway = s
	cloud.SetModelDiscovery(s.discoverCloudModels)
	store, ok := s.cache.(OpenAICloudSessionStore)
	if !ok {
		return
	}
	s.cloudSessions = NewOpenAICloudSessions(store, s.connectCloudSession, s.validateCloudSessionOwner)
	s.StartCloudBilling(store)
}

func (s *OpenAIGatewayService) validateCloudSessionOwner(ctx context.Context, owner OpenAICloudOwner) error {
	if s == nil || s.cloudService == nil {
		return errors.New("Cloud authorization service is unavailable")
	}
	store, ok := s.cache.(OpenAICloudSessionStore)
	if !ok {
		return errors.New("Cloud shared state is unavailable")
	}
	blocked, blockErr := store.CloudAccountBlocked(ctx, owner.AccountID)
	if blockErr != nil {
		return errors.New("Cloud settlement admission is unavailable")
	}
	if blocked {
		return errors.New("Cloud account has unresolved consumption and cannot run new work")
	}
	access, err := s.cloudService.GetAccessToken(ctx, owner.AccountID)
	if err != nil {
		return err
	}
	if access.Authorization.RuntimeBlocked {
		return errors.New("Cloud account has unresolved consumption and cannot run new work")
	}
	if access.Generation != owner.GrantGeneration || cloudSettingsDigest(access.Account) != owner.SettingsDigest {
		return errors.New("Cloud authorization or account settings changed")
	}
	if !owner.Probe {
		if !access.Account.IsOpenAICloudEnabled() {
			return errors.New("Cloud is disabled for this account")
		}
		// Admission checks the pre-mapping model against the ordinary account
		// whitelist. Owner.Model is the mapped upstream ID; rechecking it here
		// would reject valid aliases. Mapping edits are fenced by the digest.
	}
	return nil
}

func (s *OpenAIGatewayService) connectCloudSession(ctx context.Context, owner OpenAICloudOwner) (*cloudRPC, error) {
	if err := s.validateCloudSessionOwner(ctx, owner); err != nil {
		return nil, err
	}
	access, err := s.cloudService.GetAccessToken(ctx, owner.AccountID)
	if err != nil {
		return nil, err
	}
	access, err = s.checkCloudUsage(ctx, access)
	if err != nil {
		return nil, err
	}
	if access.Generation != owner.GrantGeneration {
		return nil, ErrOpenAICloudAuthorization
	}
	return dialOpenAICloud(ctx, access.AccessToken, access.Account.GetChatGPTAccountID(), cloudAccountProxy(access.Account))
}

// wham/usage is an admission check, never the token meter. Credentials are
// explicit and the shared native authority owns the sole 401 refresh attempt.
func (s *OpenAIGatewayService) checkCloudUsage(ctx context.Context, access *OpenAICloudAccess) (*OpenAICloudAccess, error) {
	if access == nil || s.cloudService == nil || s.cloudService.oauth == nil || s.cloudService.oauth.privacyClientFactory == nil {
		return nil, errors.New("Cloud usage client is unavailable")
	}
	for attempt := 0; attempt < 2; attempt++ {
		client, err := s.cloudService.oauth.privacyClientFactory(cloudAccountProxy(access.Account))
		if err != nil {
			return nil, errors.New("Cloud usage client is unavailable")
		}
		client.SetRedirectPolicy(req.NoRedirectPolicy())
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		var usage OpenAIQuotaUsage
		response, err := client.R().SetContext(callCtx).SetHeaders(map[string]string{
			"Authorization": "Bearer " + access.AccessToken, "ChatGPT-Account-Id": access.Account.GetChatGPTAccountID(),
			"originator": "sub2api", "User-Agent": "sub2api-cloud/0.1",
		}).SetSuccessResult(&usage).Get(chatGPTUsageURL)
		cancel()
		if err != nil {
			return nil, errors.New("Cloud usage check failed")
		}
		if response.StatusCode == http.StatusUnauthorized && attempt == 0 {
			provider := s.cloudService.tokens
			if provider == nil || provider.refreshAPI == nil || provider.executor == nil {
				return nil, ErrOpenAICloudAuthorization
			}
			if _, err := provider.refreshAPI.RefreshRejected(ctx, access.Account, provider.executor, access.AccessToken); err != nil {
				return nil, ErrOpenAICloudAuthorization
			}
			access, err = s.cloudService.GetAccessToken(ctx, access.Account.ID)
			if err != nil {
				return nil, err
			}
			continue
		}
		if response.StatusCode != http.StatusOK {
			return nil, &cloudGatewayError{status: mapCloudHTTPStatus(response.StatusCode), code: "cloud_usage_rejected", message: "Cloud usage admission was rejected"}
		}
		if usage.AccountID != "" && usage.AccountID != access.Account.GetChatGPTAccountID() {
			return nil, ErrOpenAICloudAuthorization
		}
		if usage.RateLimit == nil {
			return nil, errors.New("Cloud usage response is incomplete")
		}
		if !usage.RateLimit.Allowed || usage.RateLimit.LimitReached {
			return nil, &cloudGatewayError{status: http.StatusTooManyRequests, code: "cloud_quota_exhausted", message: "Cloud account quota is exhausted"}
		}
		return access, nil
	}
	return nil, ErrOpenAICloudAuthorization
}

func (s *OpenAIGatewayService) discoverCloudModels(ctx context.Context, access *OpenAICloudAccess) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	access, err := s.checkCloudUsage(ctx, access)
	if err != nil {
		return nil, err
	}
	conn, err := dialOpenAICloud(ctx, access.AccessToken, access.Account.GetChatGPTAccountID(), cloudAccountProxy(access.Account))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return readCloudModelCatalog(ctx, conn)
}

func readCloudModelCatalog(ctx context.Context, conn *cloudRPC) ([]string, error) {
	models, seen, cursors := []string{}, map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < 32; page++ {
		params := map[string]any{"includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, err := conn.Call(ctx, "model/list", params)
		if err != nil {
			return nil, err
		}
		var payload struct {
			Data []struct {
				Model string `json:"model"`
				ID    string `json:"id"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if json.Unmarshal(result, &payload) != nil || len(payload.Data) > 1024 {
			return nil, errors.New("Cloud model catalog is invalid")
		}
		for _, m := range payload.Data {
			if m.Model != "" && len(m.Model) <= 200 && !seen[m.Model] {
				seen[m.Model] = true
				models = append(models, m.Model)
			}
		}
		if payload.NextCursor == nil || *payload.NextCursor == "" {
			return models, nil
		}
		cursor = *payload.NextCursor
		if len(cursor) > 2048 || cursors[cursor] {
			return nil, errors.New("Cloud model catalog cursor is invalid")
		}
		cursors[cursor] = true
	}
	return nil, errors.New("Cloud model catalog exceeds its page limit")
}

// Resolve before generic bootstrap normalization: a real Cloud tool result
// must never be turned into a user message or sent with a different account.
func (s *OpenAIGatewayService) PrepareCloudContinuation(ctx context.Context, userID, keyID, groupID int64, body []byte) (context.Context, error) {
	// A WS connection can start by resuming a Cloud tool, then submit an
	// independent turn. Never retain the initial turn's billing root in ctx.
	if cloudContinuationFromContext(ctx) != nil {
		ctx = context.WithValue(ctx, cloudContinuationKey{}, (*OpenAICloudBinding)(nil))
	}
	// Legacy tool outputs must be resolved before the scheduler picks an
	// account, just like Responses input. Do not normalize unrelated native calls.
	legacyCloud := cloudIsResponseID(gjson.GetBytes(body, "previous_response_id").String()) && gjson.GetBytes(body, "messages").Exists()
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").String() == "tool" && strings.HasPrefix(message.Get("tool_call_id").String(), "call_cloud_") {
			legacyCloud = true
			break
		}
	}
	if legacyCloud {
		normalized, err := s.normalizeCloudResponsesIngress(body)
		if err != nil {
			return nil, err
		}
		body = normalized
	}
	hasCloudMarker := cloudIsResponseID(gjson.GetBytes(body, "previous_response_id").String())
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() == "function_call_output" && strings.HasPrefix(item.Get("call_id").String(), "call_cloud_") {
			hasCloudMarker = true
			break
		}
	}
	if !hasCloudMarker {
		return ctx, nil
	}
	previous, calls, err := cloudResponseReferences(body)
	if err != nil {
		return nil, err
	}
	hasCloud := cloudIsResponseID(previous)
	for _, id := range calls {
		hasCloud = hasCloud || strings.HasPrefix(id, "call_cloud_")
	}
	if !hasCloud {
		return ctx, nil
	}
	if s == nil || s.cloudSessions == nil {
		return nil, errors.New("Cloud continuation is unavailable")
	}
	binding, err := s.cloudSessions.Resolve(ctx, userID, keyID, groupID, body)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, errors.New("Cloud continuation is unknown or expired")
	}
	return context.WithValue(ctx, cloudContinuationKey{}, binding), nil
}

type cloudGatewayError struct {
	status        int
	code, message string
}

func (e *cloudGatewayError) Error() string { return e.message }
func mapCloudHTTPStatus(status int) int {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return http.StatusForbidden
	case http.StatusTooManyRequests:
		return status
	default:
		return http.StatusBadGateway
	}
}

func writeCloudGatewayError(c *gin.Context, err error) {
	if c == nil || c.Writer.Written() {
		return
	}
	status, code, message := http.StatusBadGateway, "cloud_execution_failed", "Cloud execution failed; the request was not replayed"
	if mappedStatus, mappedCode, mappedMessage, ok := cloudPublicError(err); ok {
		status, code, message = mappedStatus, mappedCode, mappedMessage
	}
	writeOpenAIResponsesErrorJSON(c, status, gin.H{"error": gin.H{"type": "api_error", "code": code, "message": message}})
}

// Both admission and dispatch need the same legacy conversion: resolving a
// tool result only after account selection would lose its owner affinity.
func (s *OpenAIGatewayService) normalizeCloudResponsesIngress(body []byte) ([]byte, error) {
	// Reuse native ingress repairs only after checking the original envelope:
	// map-based legacy conversion must not conceal duplicate keys or oversize.
	if len(body) == 0 || len(body) > OpenAICloudMaxRequestBytes || !cloudUniqueJSON(body) {
		return nil, &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud request JSON is invalid, ambiguous, or too large."}
	}
	previous := gjson.GetBytes(body, "previous_response_id")
	if normalized, changed, err := normalizeOpenAIResponsesLegacyIngressWithConfig(s.cfg, body); err != nil {
		return nil, &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud could not normalize the legacy Responses input."}
	} else if changed {
		body = normalized
		// Native legacy messages represent complete history and drop previous;
		// Cloud response IDs instead fence the owning live actor and must survive.
		if previous.Exists() {
			body, err = sjson.SetRawBytes(body, "previous_response_id", []byte(previous.Raw))
			if err != nil {
				return nil, err
			}
		}
	}
	if normalized, changed, err := sanitizeOpenAIResponsesToolSchemasForPlatform(body, PlatformOpenAI); err != nil {
		return nil, &cloudGatewayError{http.StatusBadRequest, "cloud_invalid_request", "Cloud could not normalize the Responses tool schema."}
	} else if changed {
		body = normalized
	}
	return body, nil
}

func (s *OpenAIGatewayService) forwardOpenAICloud(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	start := time.Now()
	fail := func(err error) (*OpenAIForwardResult, error) { writeCloudGatewayError(c, err); return nil, err }
	if s.cloudSessions == nil || s.cloudService == nil || account == nil {
		return fail(errors.New("Cloud gateway is unavailable"))
	}
	if !account.IsOpenAICloudEnabled() {
		return fail(ErrOpenAICloudSettings)
	}
	body, err := s.normalizeCloudResponsesIngress(body)
	if err != nil {
		return fail(err)
	}
	access, err := s.cloudService.GetAccessToken(ctx, account.ID)
	if err != nil {
		if status, code, message, known := cloudPublicError(err); known {
			if failure := cloudPreInferenceFailover(ctx, body, status, code, message, err, true); failure != nil {
				failure.RejectedBeforeGeneration = false // credential preparation uses its existing small retry budget
				return nil, failure
			}
		}
		return fail(err)
	}
	_, cloudWS := ctx.Value(cloudWebsocketModelKey{}).(string)
	if cloudWS && cloudSettingsDigest(account) != cloudSettingsDigest(access.Account) {
		// WS already mapped its payload after checking the ordinary whitelist.
		// Reject a concurrent account edit instead of reinterpreting the target.
		return fail(ErrOpenAICloudSettings)
	}
	account = access.Account
	restriction := s.detectCodexClientRestriction(c, account, body)
	if restriction.Enabled && !restriction.Matched {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		return fail(&cloudGatewayError{http.StatusForbidden, "client_restricted", CodexClientRestrictionMessage(restriction)})
	}
	model := gjson.GetBytes(body, "model").String()
	if !account.IsOpenAICloudEnabled() || access.Authorization.RuntimeBlocked {
		return fail(ErrOpenAICloudSettings)
	}
	if !cloudWS && !account.IsModelSupported(model) {
		return fail(&cloudGatewayError{http.StatusBadRequest, "model_not_allowed", "The requested model is not available for this account"})
	}
	upstreamModel := account.GetMappedModel(model)
	if original, bridged := ctx.Value(cloudWebsocketModelKey{}).(string); bridged {
		upstreamModel, model = model, original
	}
	if !cloudWS && s.IsOpenAICloudRequestUnsupported(c, body) {
		return fail(&cloudGatewayError{http.StatusBadRequest, "cloud_unsupported_endpoint", "Cloud supports the HTTP Responses endpoint; this transport or operation is unsupported"})
	}
	if body, err = sjson.SetBytes(body, "model", upstreamModel); err != nil {
		return fail(err)
	}
	// Earlier native models use pro as the legacy alias for max effort. GPT-6
	// mode is independent; leave it for Cloud's explicit capability validation.
	if !isOpenAIGPT6Model(upstreamModel) && strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, "reasoning.mode").String()), "pro") {
		if normalized, changed, err := normalizeOpenAIResponsesReasoningMode(body, upstreamModel); err != nil {
			return fail(err)
		} else if changed {
			body = normalized
		}
	}
	if _, err = parseCloudProtocolRequest(body); err != nil {
		return fail(&cloudGatewayError{http.StatusBadRequest, "cloud_unsupported_parameter", err.Error()})
	}
	raw, ok := c.Get(openAIHTTPResponseOwnerContextKey)
	if !ok {
		return fail(errors.New("Cloud request owner is missing"))
	}
	httpOwner, ok := raw.(openAIHTTPResponseOwner)
	if !ok || httpOwner.userID <= 0 || httpOwner.apiKeyID <= 0 {
		return fail(errors.New("Cloud request owner is invalid"))
	}
	owner := OpenAICloudOwner{UserID: httpOwner.userID, APIKeyID: httpOwner.apiKeyID, GroupID: getOpenAIGroupIDFromContext(c), AccountID: account.ID, GrantGeneration: access.Generation, Model: upstreamModel, SettingsDigest: cloudSettingsDigest(account)}
	effectiveEffort := gjson.GetBytes(body, "reasoning.effort").String()
	if binding := cloudContinuationFromContext(ctx); binding != nil && binding.Status == "completed" {
		effectiveEffort, err = s.resolveCloudBillingEffort(ctx, binding, owner, body)
		if err != nil {
			return fail(err)
		}
	}
	ctx = EnsureOpenAITurnBudget(ctx, s.cfg, effectiveEffort)
	ctx = withTurnRecoveryTarget(ctx, account, UsageAuthorizationModeCloud, "/", cloudAccountProxy(account), access.Generation)
	if err = TakeOpenAITurnAttempt(ctx); err != nil {
		return fail(err)
	}
	rootID := "cloud_" + uuid.NewString()
	newBillingRoot := true
	if binding := cloudContinuationFromContext(ctx); binding != nil && binding.Status != "completed" {
		rootID = binding.RootRequestID
		newBillingRoot = false
		if binding.OpenAICloudOwner != owner {
			return fail(ErrOpenAICloudSettings)
		}
	} else {
		ctx, err = s.FinalizeCloudBillingSnapshot(ctx, upstreamModel, gjson.GetBytes(body, "service_tier").String(), effectiveEffort)
		if err != nil {
			return fail(err)
		}
		if err = s.HoldCloudBillingReservation(ctx, rootID); err != nil {
			return fail(err)
		}
	}
	setActualOpenAIUsageAuthorizationMode(c, UsageAuthorizationModeCloud)
	SetActualOpenAIUpstreamEndpoint(c, "wss://codex-cloud-backend.chatgpt.com/")
	stream := gjson.GetBytes(body, "stream").Bool()
	if stream && !cloudWS {
		defer GuardSSEWrites(c)()
	}
	var guard *openAIFirstOutputHeaderGuard
	if timeout := s.openAIFirstOutputTimeout(effectiveEffort); timeout > 0 {
		ctx, guard = newOpenAIFirstOutputHeaderGuard(ctx, func() {}, openAITurnFirstOutputDeadline(ctx, timeout, start))
		defer guard.close()
	}
	var first *int
	begun := false
	lastWrite := time.Now()
	begin := func() {
		if !begun {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("X-Accel-Buffering", "no")
			begun = true
		}
	}
	// Hold the two startup events until output, and a failed terminal until the
	// persisted result arrives. An early response ID would prevent safe failover.
	var pendingFailure json.RawMessage
	var pendingStart []json.RawMessage
	emitPublic := func(event json.RawMessage) error {
		kind := gjson.GetBytes(event, "type").String()
		if openAIStreamDataStartsClientOutput(string(event), kind) || isOpenAIWSTerminalEvent(kind) {
			if guard != nil && guard.stopHeaderWait() {
				return ErrOpenAIWSFirstOutputBudgetExhausted
			}
			MarkTurnOutputCommitted(ctx)
		}
		if first == nil && kind == "response.output_text.delta" {
			ms := int(time.Since(start).Milliseconds())
			first = &ms
		}
		if !stream {
			return nil
		}
		begin()
		if gjson.GetBytes(event, "response.model").Exists() {
			var err error
			event, err = sjson.SetBytes(event, "response.model", model)
			if err != nil {
				return err
			}
		}
		if ctx.Err() == context.Canceled && !IsTurnBudgetError(context.Cause(ctx)) {
			return errors.Join(ctx.Err(), context.Cause(ctx))
		}
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", event); err != nil {
			return errors.Join(err, context.Canceled)
		}
		c.Writer.Flush()
		if err := downstreamStreamError(c); err != nil {
			return errors.Join(err, context.Canceled)
		}
		lastWrite = time.Now()
		return nil
	}
	// Run invokes idle callbacks on its reader goroutine, so heartbeat and
	// response writes are serialized without sharing gin with a timer writer.
	heartbeat := func() error {
		if !stream || excelBPSWSKeepaliveInterval <= 0 || time.Since(lastWrite) < excelBPSWSKeepaliveInterval {
			return nil
		}
		begin()
		n, err := c.Writer.WriteString(": keepalive\n\n")
		recordOpenAIStreamKeepaliveBytes(c, n)
		if err != nil {
			return errors.Join(err, context.Canceled)
		}
		c.Writer.Flush()
		if err := downstreamStreamError(c); err != nil {
			return errors.Join(err, context.Canceled)
		}
		lastWrite = time.Now()
		return nil
	}
	flushStart := func() error {
		for _, event := range pendingStart {
			if err := emitPublic(event); err != nil {
				return err
			}
		}
		pendingStart = nil
		return nil
	}
	emit := func(event json.RawMessage) error {
		kind := gjson.GetBytes(event, "type").String()
		if (kind == "response.created" || kind == "response.in_progress") && len(pendingStart) < 2 {
			pendingStart = append(pendingStart, append(json.RawMessage(nil), event...))
			return nil
		}
		if kind == "response.failed" {
			pendingFailure = append(json.RawMessage(nil), event...)
			return nil
		}
		if err := flushStart(); err != nil {
			return err
		}
		return emitPublic(event)
	}
	result, err := s.cloudSessions.Run(ctx, OpenAICloudRequest{Owner: owner, Body: body, RootRequestID: rootID, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), onIdle: heartbeat}, emit)
	if newBillingRoot && (cloudResultProvesNoInference(result, rootID) || (result == nil && errors.Is(err, ErrOpenAICloudNotStarted))) {
		// Persisted zero-usage settlements still use the durable consumer, while
		// local admission reservations must not obstruct the next attempt.
		s.ReleaseCloudBillingReservation(rootID)
		status, code, message, known := cloudPublicError(err)
		if result != nil {
			status = result.ErrorStatus
			if status == 0 {
				status = http.StatusBadGateway
			}
			code = gjson.GetBytes(result.Response, "error.code").String()
			message = "Cloud failed before inference started"
			known = true
		}
		if known {
			if failure := cloudPreInferenceFailover(ctx, body, status, code, message, err, result == nil || result.TurnID == ""); failure != nil {
				return nil, failure
			}
		}
	}
	// The WS control owner sends the cancellation terminal. HTTP clients that
	// disconnected cannot receive another error. Preserve uncertain billing and
	// never turn a downstream write/flush failure into an upstream health sample.
	if result == nil && err != nil && !IsTurnBudgetError(context.Cause(ctx)) &&
		(errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled || downstreamStreamError(c) != nil) {
		if newBillingRoot && errors.Is(err, ErrOpenAICloudNotStarted) {
			s.ReleaseCloudBillingReservation(rootID)
		}
		return nil, errors.Join(err, context.Cause(ctx))
	}
	// The HTTP wire may already be 200 after a heartbeat or model output.
	// Record the final semantic failure just as native/BPS do, after the safe
	// failover decision. The WS adapter owns its own per-turn failure marker.
	markFailure := func(status int, code, message string, upstream bool) {
		if cloudWS || errors.Is(context.Cause(ctx), context.Canceled) || streamClientDisconnected(c) {
			return
		}
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		if upstream {
			setOpsUpstreamError(c, status, message, "Cloud response failed")
		}
		c.Set(OpsGatewayResponsesFailedKey, true)
		MarkOpsStreamErrorValue(c, OpsStreamError{ErrType: "upstream_error", Code: code, Message: message,
			IntendedStatus: status, CountTowardsSLA: true, NonStream: !stream})
	}
	if result != nil && gjson.GetBytes(result.Response, "status").String() == "failed" {
		code, message := gjson.GetBytes(result.Response, "error.code").String(), gjson.GetBytes(result.Response, "error.message").String()
		// Use only the already sanitized error, never model output or encrypted
		// history, for the shared HTTP/WS cyber-policy marker and session fence.
		failure, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message}})
		var usage *OpenAIUsage
		if result.Usage != nil {
			usage = &OpenAIUsage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens}
		}
		markOpenAICyberPolicyEvent(c, failure, result.ErrorStatus, usage)
		markFailure(result.ErrorStatus, code, message, true)
	}
	if emitErr := flushStart(); emitErr != nil {
		return nil, emitErr
	}
	if len(pendingFailure) > 0 {
		if emitErr := emitPublic(pendingFailure); emitErr != nil {
			return nil, emitErr
		}
	}
	if err != nil && ctx.Err() != nil {
		// Preserve owner proof of non-dispatch even if cancellation races its reply.
		err = errors.Join(err, context.Cause(ctx))
	}
	if result == nil && begun {
		status, code, message, ok := cloudPublicError(err)
		if !ok {
			status, code, message = http.StatusBadGateway, "cloud_execution_failed", "Cloud execution failed; the request was not replayed."
		}
		markFailure(status, code, message, false)
		raw, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": map[string]string{"code": code, "message": message}})
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", raw)
		c.Writer.Flush()
	}
	if result == nil {
		if newBillingRoot && errors.Is(err, ErrOpenAICloudNotStarted) {
			s.ReleaseCloudBillingReservation(rootID)
		}
		return fail(err)
	}
	if newBillingRoot && result.RootRequestID != rootID {
		// The actor returned an already completed idempotent tool result. This
		// generated root never started and will never have a settlement to ACK.
		s.ReleaseCloudBillingReservation(rootID)
	}
	responseID := gjson.GetBytes(result.Response, "id").String()
	if responseID != "" {
		s.bindHTTPResponseAccount(ctx, c, account, responseID)
	}
	if !stream && len(result.Response) > 0 {
		status := http.StatusOK
		if result.ErrorStatus >= 400 && result.ErrorStatus <= 599 {
			status = result.ErrorStatus
		}
		response, rewriteErr := sjson.SetBytes(result.Response, "model", model)
		if rewriteErr != nil {
			return fail(rewriteErr)
		}
		c.Data(status, "application/json", response)
	}
	forward := &OpenAIForwardResult{RequestID: result.RootRequestID, Model: model, UpstreamModel: upstreamModel, UpstreamResponseModel: upstreamModel, AuthorizationMode: UsageAuthorizationModeCloud, Stream: stream, Duration: time.Since(start), FirstTokenMs: first, CloudManagedBilling: true, UpstreamEndpoint: "wss://codex-cloud-backend.chatgpt.com/"}
	forward.ResponseID = responseID
	if result.Terminal && !result.Replayed {
		forward.UpstreamTerminalEvent = "response." + gjson.GetBytes(result.Response, "status").String()
		forward.ClientDisconnect = !result.UsageCredible
		forward.wsContextRejected = result.ErrorStatus == http.StatusBadRequest
	}
	if result.Usage != nil {
		forward.Usage = OpenAIUsage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, CacheReadInputTokens: result.Usage.CachedInputTokens, CacheCreationInputTokens: result.Usage.CacheWriteInputTokens}
	}
	return forward, err
}

// The actor inherits omitted reasoning across a completed thread. Freeze the
// same effective effort before reserving the next billing root; otherwise a
// high-effort turn could be charged at the omitted/default effort rate.
func (s *OpenAIGatewayService) resolveCloudBillingEffort(ctx context.Context, binding *OpenAICloudBinding, owner OpenAICloudOwner, body []byte) (string, error) {
	effort := gjson.GetBytes(body, "reasoning.effort").String()
	if binding == nil {
		return effort, nil
	}
	if binding.OpenAICloudOwner != owner || binding.Status != "completed" || s.cloudSessions == nil || s.cloudSessions.store == nil {
		return "", ErrOpenAICloudSettings
	}
	if gjson.GetBytes(body, "reasoning").Exists() {
		return effort, nil
	}
	record, err := s.cloudSessions.store.LoadCloudSession(ctx, binding.SessionID)
	if err != nil {
		return "", err
	}
	if record == nil || record.OpenAICloudOwner != owner || record.RootRequestID != binding.RootRequestID || record.Status != "completed" {
		return "", ErrOpenAICloudSettings
	}
	var previous OpenAICloudBillingSnapshot
	if json.Unmarshal(record.BillingSnapshot, &previous) != nil || previous.Version != 1 || previous.UserID != owner.UserID || previous.APIKeyID != owner.APIKeyID || valueOrZero(previous.GroupID) != owner.GroupID || previous.AccountID != owner.AccountID || previous.UpstreamModel != owner.Model {
		return "", errors.New("Cloud preceding turn has no matching billing settings")
	}
	// Only billing/deadline selection needs the effective effort here. The
	// actor must inherit the complete reasoning object, including summary. A
	// partial object manufactured here would suppress that inheritance.
	return previous.ReasoningEffort, nil
}

func (s *OpenAIGatewayService) IsOpenAICloudRequestUnsupported(c *gin.Context, body []byte) bool {
	return c == nil || c.Request == nil || strings.EqualFold(c.GetHeader("Upgrade"), "websocket") || IsOpenAIResponsesCompactPath(c) || gjson.GetBytes(body, "compaction_trigger").Exists()
}

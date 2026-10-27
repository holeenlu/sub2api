package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type openAIWSFieldRepairOptions struct {
	state          *openAIResponsesRejectedFieldRetryState
	webSearchAdded bool
}

func (s *OpenAIGatewayService) forwardOpenAIWSV2(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	reqBody map[string]any,
	clientPromptCacheKey string,
	executionScope string,
	token string,
	decision OpenAIWSProtocolDecision,
	isCodexCLI bool,
	reqStream bool,
	originalModel string,
	mappedModel string,
	startTime time.Time,
	attempt int,
	preparationDeadline time.Time,
	lastFailureReason string,
	agentTaskRecoveryTried *bool,
	recoveryPayload []byte,
	repairOptions ...openAIWSFieldRepairOptions,
) (out *OpenAIForwardResult, returnErr error) {
	if reqStream {
		defer GuardSSEWrites(c)()
	}
	if s == nil || account == nil {
		return nil, wrapOpenAIWSFallback("invalid_state", errors.New("service or account is nil"))
	}
	if c != nil {
		c.Set(OpsOpenAIWSPayloadBytesKey, -1) // do not reuse a previous attempt's size
	}
	if err := validateVendorNativeDispatch(ctx, s.accountRepo, account, vendorDispatchGroup(c)); err != nil {
		return nil, err
	}
	responseModelObserver := newUpstreamResponseModelObserver(c)

	wsURL, err := s.buildOpenAIResponsesWSURL(account)
	if err != nil {
		return nil, wrapOpenAIWSFallback("build_ws_url", err)
	}
	wsHost := "-"
	wsPath := "-"
	if parsed, parseErr := url.Parse(wsURL); parseErr == nil && parsed != nil {
		if h := strings.TrimSpace(parsed.Host); h != "" {
			wsHost = normalizeOpenAIWSLogValue(h)
		}
		if p := strings.TrimSpace(parsed.Path); p != "" {
			wsPath = normalizeOpenAIWSLogValue(p)
		}
	}
	logOpenAIWSModeDebug(
		"dial_target account_id=%d account_type=%s ws_host=%s ws_path=%s",
		account.ID,
		account.Type,
		wsHost,
		wsPath,
	)

	var payload map[string]any
	payloadStrategy := ""
	var removedKeys []string
	turnState := ""
	turnMetadata := ""
	if c != nil && c.Request != nil {
		turnState = strings.TrimSpace(c.GetHeader(openAIWSTurnStateHeader))
		turnMetadata = strings.TrimSpace(c.GetHeader(openAIWSTurnMetadataHeader))
	}
	if len(recoveryPayload) > 0 {
		// The retained request is already scoped. Do not prepare and then
		// discard another copy or regenerate its turn identity on recovery.
		if err := decodeOpenAIJSONUseNumber(recoveryPayload, &payload); err != nil {
			return nil, fmt.Errorf("decode retained websocket request: %w", err)
		}
		turnMetadata = ""
	} else {
		payload = s.buildOpenAIWSCreatePayload(reqBody, account)
		payloadStrategy, removedKeys = applyOpenAIWSRetryPayloadStrategy(payload, attempt)
		setOpenAIWSTurnMetadata(payload, turnMetadata)
		if turnMetadata != "" {
			if metadata, ok := payload["client_metadata"].(map[string]any); ok {
				applyCodexAccountIdentityEmbeddedMetadata(metadata, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c))
			}
		}
		applyStagedCodexFingerprintClientMetadata(c, account, payload)
	}
	previousResponseID := openAIWSPayloadString(payload, "previous_response_id")
	previousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
	promptCacheKey := strings.TrimSpace(clientPromptCacheKey)
	if promptCacheKey == "" {
		// Fingerprint convergence may inject a default key when the client did
		// not send one; retain that fallback without replacing an explicit raw key.
		promptCacheKey = openAIWSPayloadString(payload, "prompt_cache_key")
	}
	_, hasTools := payload["tools"]
	debugEnabled := isOpenAIWSModeDebugEnabled()

	stateStore := s.getOpenAIWSStateStore()
	groupID := getOpenAIGroupIDFromContext(c)
	sessionHash := s.GenerateSessionHash(c, nil)
	if sessionHash == "" {
		var legacySessionHash string
		sessionHash, legacySessionHash = openAIWSSessionHashesFromID(promptCacheKey)
		attachOpenAILegacySessionHashToGin(c, legacySessionHash)
	}
	// 与 WS 接入路径共用执行作用域：codex 多智能体共用 session-id，turn state 与
	// store=false 的连接绑定必须按线程隔离，同一线程在两条路径之间也才能共享状态。
	// 作用域由 Forward 从改写前的原始请求算出后传入，reqBody 此时已带账号 namespace。
	if executionScope = strings.TrimSpace(executionScope); executionScope != "" {
		sessionHash = executionScope
	}
	if turnState == "" && stateStore != nil && sessionHash != "" {
		if savedTurnState, ok := stateStore.GetSessionTurnState(groupID, sessionHash); ok {
			turnState = savedTurnState
		}
	}
	preferredConnID := ""
	previousScope := openAIWSResponseScopeUnknown
	if stateStore != nil && previousResponseID != "" {
		if connID, scope, ok := stateStore.GetResponseContinuation(previousResponseID); ok {
			preferredConnID, previousScope = connID, scope
		}
	}
	if previousResponseID != "" && account.IsOpenAIOAuthLike() && previousScope == openAIWSResponseScopeUnknown {
		WriteOpenAIContinuationError(c)
		return nil, ErrOpenAIContinuationUnavailable
	}
	requiredResponseID := ""
	if previousScope == openAIWSResponseScopeConnectionLocal {
		requiredResponseID = previousResponseID
	}
	storeDisabled := s.isOpenAIWSStoreDisabledInRequest(reqBody, account)
	if stateStore != nil && storeDisabled && previousResponseID == "" && sessionHash != "" {
		if connID, ok := stateStore.GetSessionConn(groupID, sessionHash); ok {
			preferredConnID = connID
		}
	}
	storeDisabledConnMode := s.openAIWSStoreDisabledConnMode()
	forceNewConnByPolicy := shouldForceNewConnOnStoreDisabled(storeDisabledConnMode, lastFailureReason)
	forceNewConn := forceNewConnByPolicy && storeDisabled && previousResponseID == "" && sessionHash != "" && preferredConnID == ""
	if len(recoveryPayload) > 0 {
		preferredConnID, forceNewConn = "", true
	}
	wsHeaders, sessionResolution, buildHdrErr := s.buildOpenAIWSHeaders(
		ctx,
		c,
		account,
		token,
		decision,
		isCodexCLI,
		turnState,
		turnMetadata,
		promptCacheKey,
		openAIWSPayloadString(payload, "model"),
		openAIWSPayloadString(payload, "service_tier"),
		payloadAsJSONBytes(map[string]any{"client_metadata": payload["client_metadata"]}),
	)
	if buildHdrErr != nil {
		return nil, fmt.Errorf("build ws headers: %w", buildHdrErr)
	}
	if err := normalizeCodexRequestMetadataMap(payload, wsHeaders, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c)); err != nil {
		return nil, err
	}
	projectCodexOutboundHeaders(wsHeaders, codexAccountIdentitySource(c, account), true)
	// Encode after the final metadata/header projection. Retain the complete
	// request for recovery even when a held connection can send a smaller delta.
	finalPayload, encodeErr := json.Marshal(payload)
	if encodeErr != nil {
		return nil, fmt.Errorf("encode websocket request: %w", encodeErr)
	}

	fullPayload := finalPayload
	fullPayloadBytes := len(fullPayload)
	payloadBytes := fullPayloadBytes
	streamValue := "-"
	if raw, ok := payload["stream"]; ok {
		streamValue = normalizeOpenAIWSLogValue(strings.TrimSpace(fmt.Sprintf("%v", raw)))
	}
	payloadEventType := openAIWSPayloadString(payload, "type")
	if payloadEventType == "" {
		payloadEventType = "response.create"
	}
	if s.shouldEmitOpenAIWSPayloadSchema(attempt) {
		logOpenAIWSModeInfo(
			"[debug] payload_schema account_id=%d attempt=%d event=%s payload_keys=%s payload_bytes=%d payload_key_sizes=%s input_summary=%s stream=%s payload_strategy=%s removed_keys=%s has_previous_response_id=%v has_prompt_cache_key=%v has_tools=%v",
			account.ID,
			attempt,
			payloadEventType,
			normalizeOpenAIWSLogValue(strings.Join(sortedKeys(payload), ",")),
			payloadBytes,
			normalizeOpenAIWSLogValue(summarizeOpenAIWSPayloadKeySizes(payload, openAIWSPayloadKeySizeTopN)),
			normalizeOpenAIWSLogValue(summarizeOpenAIWSInput(payload["input"])),
			streamValue,
			normalizeOpenAIWSLogValue(payloadStrategy),
			normalizeOpenAIWSLogValue(strings.Join(removedKeys, ",")),
			previousResponseID != "",
			promptCacheKey != "",
			hasTools,
		)
	}

	optionalWS := requiredResponseID == "" && GetOpenAIClientTransport(c) == OpenAIClientTransportHTTP && openAIWSRequestAllowsHTTP(account, fullPayload) &&
		(attempt <= 1 || openAIWSFailureBeforeSend(lastFailureReason))
	// Above the observer's existing input bound no delta can be established.
	// Other eligible requests are checked again after selecting the real wire
	// bytes on an exclusively held connection.
	if optionalWS && s.openAIWSRequestPrefersHTTPSize(fullPayloadBytes) &&
		(!account.IsOpenAIOAuthLike() || fullPayloadBytes > openAIIncrementalMaxBody) {
		return nil, wrapOpenAIWSFallback(openAIWSPayloadPrefersHTTP, nil)
	}

	logOpenAIWSModeDebug(
		"acquire_start account_id=%d account_type=%s transport=%s preferred_conn_id=%s has_previous_response_id=%v session_hash=%s has_turn_state=%v turn_state_len=%d has_turn_metadata=%v turn_metadata_len=%d store_disabled=%v store_disabled_conn_mode=%s retry_last_reason=%s force_new_conn=%v header_user_agent=%s header_openai_beta=%s header_originator=%s header_accept_language=%s header_session_id=%s header_conversation_id=%s session_id_source=%s conversation_id_source=%s has_prompt_cache_key=%v has_chatgpt_account_id=%v has_authorization=%v has_session_id=%v has_conversation_id=%v proxy_enabled=%v",
		account.ID,
		account.Type,
		normalizeOpenAIWSLogValue(string(decision.Transport)),
		truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
		previousResponseID != "",
		truncateOpenAIWSLogValue(sessionHash, 12),
		turnState != "",
		len(turnState),
		turnMetadata != "",
		len(turnMetadata),
		storeDisabled,
		normalizeOpenAIWSLogValue(storeDisabledConnMode),
		truncateOpenAIWSLogValue(lastFailureReason, openAIWSLogValueMaxLen),
		forceNewConn,
		openAIWSHeaderValueForLog(wsHeaders, "user-agent"),
		openAIWSHeaderValueForLog(wsHeaders, "openai-beta"),
		openAIWSHeaderValueForLog(wsHeaders, "originator"),
		openAIWSHeaderValueForLog(wsHeaders, "accept-language"),
		openAIWSHeaderValueForLog(wsHeaders, "session_id"),
		openAIWSHeaderValueForLog(wsHeaders, "conversation_id"),
		normalizeOpenAIWSLogValue(sessionResolution.SessionSource),
		normalizeOpenAIWSLogValue(sessionResolution.ConversationSource),
		promptCacheKey != "",
		hasOpenAIWSHeader(wsHeaders, "chatgpt-account-id"),
		hasOpenAIWSHeader(wsHeaders, "authorization"),
		hasOpenAIWSHeader(wsHeaders, "session_id"),
		hasOpenAIWSHeader(wsHeaders, "conversation_id"),
		account.ProxyID != nil && account.Proxy != nil,
	)

	var timing *openAIForwardTiming
	if payload["generate"] != false {
		timing = newOpenAITransportTiming(c, account, "websocket", int64(payloadBytes))
		timing.SetTransportReason("http_to_ws")
	} else {
		prewarmTiming := openAIForwardTimingFromContext(c)
		prewarmStarted := time.Now()
		defer func() { prewarmTiming.MarkPrewarm(time.Since(prewarmStarted)) }()
	}
	var timingErr error
	finishTiming := func() {
		if returnErr != nil {
			timingErr = returnErr
		}
		timing.Finish(timingErr)
	}
	defer finishTiming()
	acquireDeadline := time.Now().Add(s.openAIWSAcquireTimeout())
	optionalPreparationBound := optionalWS && !preparationDeadline.IsZero() && preparationDeadline.Before(acquireDeadline)
	if optionalPreparationBound {
		acquireDeadline = preparationDeadline
	}
	acquireCtx, acquireCancel := context.WithDeadline(ctx, acquireDeadline)
	defer acquireCancel()

	lease, err := s.getOpenAIWSConnPool().Acquire(acquireCtx, openAIWSAcquireRequest{
		TenantScope: openAIWSTenantScope(c),
		Account:     account,
		WSURL:       wsURL,
		Headers:     wsHeaders,
		HeadersFactory: func(factoryCtx context.Context, headers http.Header) (http.Header, error) {
			return s.refreshOpenAIAgentIdentityHeaders(factoryCtx, account, headers)
		},
		PreferredConnID:    preferredConnID,
		RequiredResponseID: requiredResponseID,
		ForceNewConn:       forceNewConn,
		ProxyURL: func() string {
			if account.ProxyID != nil && account.Proxy != nil {
				return account.Proxy.URL()
			}
			return ""
		}(),
	})
	preparationExpired := optionalPreparationBound && errors.Is(acquireCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	acquireCancel()
	if preparationExpired {
		if lease != nil {
			lease.Release()
		}
		// Acquire did not send any response.create. Do not reconnect with a
		// fresh budget or start HTTP until the canceled preparation has joined.
		return nil, wrapOpenAIWSFallback(openAIWSOptionalPreparationTimedOut, context.DeadlineExceeded)
	}
	if err != nil {
		if errors.Is(err, errOpenAIWSPreferredConnUnavailable) {
			WriteOpenAIContinuationError(c)
			return nil, errors.Join(ErrOpenAIContinuationUnavailable, err)
		}
		var agentDialErr *openAIWSDialError
		if s.isAgentIdentityAccount(ctx, account) && errors.As(err, &agentDialErr) && isAgentIdentityTaskInvalidWSDialError(agentDialErr) && agentTaskRecoveryTried != nil && !*agentTaskRecoveryTried {
			*agentTaskRecoveryTried = true
			if recoveryErr := s.recoverAgentIdentityTask(ctx, account, account.GetCredential("task_id")); recoveryErr != nil {
				return nil, fmt.Errorf("agent identity task recovery failed: %w", recoveryErr)
			}
			return nil, &agentIdentityTaskRecoveredError{}
		}
		s.handleOpenAIWSDialTransientFailure(ctx, account, mappedModel, err)
		dialStatus, dialClass, dialCloseStatus, dialCloseReason, dialRespServer, dialRespVia, dialRespCFRay, dialRespReqID := summarizeOpenAIWSDialError(err)
		logOpenAIWSModeInfo(
			"acquire_fail account_id=%d account_type=%s transport=%s reason=%s dial_status=%d dial_class=%s dial_close_status=%s dial_close_reason=%s dial_resp_server=%s dial_resp_via=%s dial_resp_cf_ray=%s dial_resp_x_request_id=%s cause=%s preferred_conn_id=%s force_new_conn=%v ws_host=%s ws_path=%s proxy_enabled=%v",
			account.ID,
			account.Type,
			normalizeOpenAIWSLogValue(string(decision.Transport)),
			normalizeOpenAIWSLogValue(classifyOpenAIWSAcquireError(err)),
			dialStatus,
			dialClass,
			dialCloseStatus,
			truncateOpenAIWSLogValue(dialCloseReason, openAIWSHeaderValueMaxLen),
			dialRespServer,
			dialRespVia,
			dialRespCFRay,
			dialRespReqID,
			truncateOpenAIWSLogValue(err.Error(), openAIWSLogValueMaxLen),
			truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
			forceNewConn,
			wsHost,
			wsPath,
			account.ProxyID != nil && account.Proxy != nil,
		)
		var dialErr *openAIWSDialError
		if errors.As(err, &dialErr) && dialErr != nil && dialErr.StatusCode == http.StatusTooManyRequests {
			s.persistOpenAIWSRateLimitSignal(ctx, account, dialErr.ResponseHeaders, nil, "rate_limit_exceeded", "rate_limit_error", strings.TrimSpace(err.Error()), mappedModel)
		}
		return nil, wrapOpenAIWSFallback(classifyOpenAIWSAcquireError(err), err)
	}
	timing.ConnectionReady(lease.Reused(), lease.ConnPickDuration(), lease.QueueWaitDuration(), lease.AgeBefore(), lease.IdleBefore())
	// cleanExit 标记正常终端事件退出，此时上游不会再发送帧，连接可安全归还复用。
	// 所有异常路径（读写错误、error 事件等）已在各自分支中提前调用 MarkBroken，
	// 因此 defer 中只需处理正常退出时不 MarkBroken 即可。
	cleanExit := false
	defer func() {
		// Publish complete history before making this connection available to
		// the next HTTP turn. Failed/cancelled turns invalidate the old anchor.
		finishTiming()
		if !cleanExit {
			lease.MarkBroken()
		}
		lease.Release()
	}()
	connID := strings.TrimSpace(lease.ConnID())
	logOpenAIWSModeDebug(
		"connected account_id=%d account_type=%s transport=%s conn_id=%s conn_reused=%v conn_idle_ms=%d conn_age_ms=%d upstream_pings=%d has_previous_response_id=%v",
		account.ID,
		account.Type,
		normalizeOpenAIWSLogValue(string(decision.Transport)),
		connID,
		lease.Reused(),
		lease.IdleBefore().Milliseconds(),
		lease.AgeBefore().Milliseconds(),
		lease.UpstreamPingCount(),
		previousResponseID != "",
	)
	if previousResponseID != "" {
		logOpenAIWSModeInfo(
			"continuation_probe account_id=%d account_type=%s conn_id=%s previous_response_id=%s previous_response_id_kind=%s preferred_conn_id=%s conn_reused=%v store_disabled=%v session_hash=%s header_session_id=%s header_conversation_id=%s session_id_source=%s conversation_id_source=%s has_turn_state=%v turn_state_len=%d has_prompt_cache_key=%v",
			account.ID,
			account.Type,
			truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
			truncateOpenAIWSLogValue(previousResponseID, openAIWSIDValueMaxLen),
			normalizeOpenAIWSLogValue(previousResponseIDKind),
			truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
			lease.Reused(),
			storeDisabled,
			truncateOpenAIWSLogValue(sessionHash, 12),
			openAIWSHeaderValueForLog(wsHeaders, "session_id"),
			openAIWSHeaderValueForLog(wsHeaders, "conversation_id"),
			normalizeOpenAIWSLogValue(sessionResolution.SessionSource),
			normalizeOpenAIWSLogValue(sessionResolution.ConversationSource),
			turnState != "",
			len(turnState),
			promptCacheKey != "",
		)
	}
	if c != nil {
		SetOpsLatencyMs(c, OpsOpenAIWSConnPickMsKey, lease.ConnPickDuration().Milliseconds())
		SetOpsLatencyMs(c, OpsOpenAIWSQueueWaitMsKey, lease.QueueWaitDuration().Milliseconds())
		c.Set(OpsOpenAIWSConnReusedKey, lease.Reused())
		if connID != "" {
			c.Set(OpsOpenAIWSConnIDKey, connID)
		}
	}

	handshakeTurnState := strings.TrimSpace(lease.HandshakeHeader(openAIWSTurnStateHeader))
	logOpenAIWSModeDebug(
		"handshake account_id=%d conn_id=%s has_turn_state=%v turn_state_len=%d",
		account.ID,
		connID,
		handshakeTurnState != "",
		len(handshakeTurnState),
	)
	if handshakeTurnState != "" {
		s.noteOpenAICodexTurnStateProvenance(c, account, handshakeTurnState)
		if stateStore != nil && sessionHash != "" {
			stateStore.BindSessionTurnState(groupID, sessionHash, handshakeTurnState, s.openAIWSSessionStickyTTL())
		}
		if c != nil {
			c.Header(http.CanonicalHeaderKey(openAIWSTurnStateHeader), handshakeTurnState)
		}
	}

	recoveryModel, _ := payload["model"].(string)
	s.installSchedulingRecovery(ctx, recoveryModel)
	observation := s.attachOpenAIIncrementalObservation(ctx, c, account, finalPayload, wsURL, wsHeaders, lease.ConnID(), timing)
	synthesizedContinuation := false
	if len(recoveryPayload) == 0 && GetOpenAIClientTransport(c) == OpenAIClientTransportHTTP && account.IsOpenAIOAuthLike() {
		if candidate := observation.wsPayload(ctx, finalPayload, lease); len(candidate) < len(finalPayload) {
			var selected map[string]any
			if decodeOpenAIJSONUseNumber(candidate, &selected) == nil {
				payload, finalPayload = selected, candidate
				previousResponseID = openAIWSPayloadString(payload, "previous_response_id")
				previousResponseIDKind = ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
				payloadBytes = len(finalPayload)
				synthesizedContinuation = true
			}
		}
	}
	if optionalWS && s.openAIWSRequestPrefersHTTPSize(len(finalPayload)) {
		cleanExit = true // no WS frame was sent; preserve the healthy lease
		return nil, wrapOpenAIWSFallback(openAIWSPayloadPrefersHTTP, nil)
	}
	if err := s.performOpenAIWSGeneratePrewarm(ctx, lease, decision, payload, previousResponseID, reqBody, account, stateStore, groupID, timing); err != nil {
		return nil, err
	}
	if err := TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, account, "websocket", "/v1/responses", resolveAccountProxyURL(account), token, fullPayload)); err != nil {
		return nil, err
	}
	if err := validateVendorNativeDispatch(ctx, s.accountRepo, account, vendorDispatchGroup(c)); err != nil {
		return nil, err
	}
	timing.SetRequestMetadata(finalPayload)
	if synthesizedContinuation && timing != nil {
		timing.mu.Lock()
		timing.requestShape = "gateway_continuation"
		timing.mu.Unlock()
	}
	logGatewayCacheWSPayload(ctx, account, payload)
	if err := lease.WriteJSONWithContextTimeout(ctx, json.RawMessage(finalPayload), s.openAIWSWriteTimeout()); err != nil {
		lease.MarkBroken()
		logOpenAIWSModeInfo(
			"write_request_fail account_id=%d conn_id=%s cause=%s payload_bytes=%d",
			account.ID,
			connID,
			truncateOpenAIWSLogValue(err.Error(), openAIWSLogValueMaxLen),
			payloadBytes,
		)
		if !openAIWSUncertainExecutionReplaySafe(finalPayload) {
			return nil, fmt.Errorf("openai ws write request with non-replayable tools: %w", err)
		}
		return nil, wrapOpenAIWSFallback(openAIWSFailureWriteRequest, err)
	}
	observation.markWSRequestWritten(lease)
	timing.MarkRequestWritten(int64(len(finalPayload)))
	if observation != nil && GetOpenAIClientTransport(c) == OpenAIClientTransportHTTP && account.IsOpenAIOAuthLike() {
		timing.MarkIncrementalPayloadWritten(int64(fullPayloadBytes), int64(len(finalPayload)))
	}
	if debugEnabled {
		logOpenAIWSModeDebug(
			"write_request_sent account_id=%d conn_id=%s stream=%v payload_bytes=%d previous_response_id=%s",
			account.ID,
			connID,
			reqStream,
			payloadBytes,
			truncateOpenAIWSLogValue(previousResponseID, openAIWSIDValueMaxLen),
		)
	}

	usage := &OpenAIUsage{}
	imageCounter := newOpenAIImageOutputCounter()
	var firstTokenMs *int
	responseID := ""
	continuationResponseID := ""
	var finalResponse []byte
	wroteDownstream := false
	upstreamOutputStarted := false
	needModelReplace := originalModel != mappedModel
	var mappedModelBytes []byte
	if needModelReplace && mappedModel != "" {
		mappedModelBytes = []byte(mappedModel)
	}
	var prelude *openAIFirstOutputStage
	if reqStream {
		prelude = newDefaultOpenAIFirstOutputStage()
		defer func() {
			if err := prelude.Close(); err != nil {
				logger.LegacyPrintf("service.openai_gateway", "WS pre-output staging cleanup failed: account=%d error=%v", account.ID, err)
			}
		}()
	}
	stagedEvents := 0
	var stagedFacts openAITimingEvent
	eventCount := 0
	tokenEventCount := 0
	terminalEventCount := 0
	bufferedEventCount := 0
	flushedBufferedEventCount := 0
	firstEventType := ""
	lastEventType := ""
	upstreamTerminalEvent := ""
	clientDisconnected := false
	clientDisconnectDrainStartedAt := time.Time{}
	readTimeout := s.openAIWSReadTimeout()
	upstreamReadCtx := ctx
	upstreamReadDetached := false
	stopUpstreamReadLease := func() {}
	defer func() { stopUpstreamReadLease() }()
	clientRequestCanceled := func() bool {
		return ctx != nil && errors.Is(ctx.Err(), context.Canceled)
	}
	markClientDisconnected := func(cause string) {
		if clientDisconnected {
			return
		}
		clientDisconnected = true
		clientDisconnectDrainStartedAt = time.Now()
		if !upstreamReadDetached {
			upstreamReadCtx, stopUpstreamReadLease = withConcurrencySlotCancellation(context.WithoutCancel(ctx))
			upstreamReadDetached = true
		}
		logOpenAIWSModeInfo(
			"client_disconnected account_id=%d conn_id=%s cause=%s events=%d token_events=%d",
			account.ID,
			connID,
			cause,
			eventCount,
			tokenEventCount,
		)
	}
	markClientRequestCanceled := func() {
		if clientRequestCanceled() {
			markClientDisconnected("request_context_canceled")
		}
	}
	resultWithUsage := func() *OpenAIForwardResult {
		return &OpenAIForwardResult{
			AuthorizationMode:             nativeOpenAIUsageAuthorizationMode(account),
			RequestID:                     responseID,
			ResponseID:                    responseID,
			Usage:                         *usage,
			Model:                         originalModel,
			UpstreamModel:                 mappedModel,
			UpstreamResponseModel:         responseModelObserver.Model(),
			UpstreamResponseModelConflict: responseModelObserver.Conflict(),
			UpstreamResponseServiceTier:   responseModelObserver.ServiceTier(),
			ServiceTier:                   resolvedOpenAIUpstreamServiceTierFromObserver(responseModelObserver, extractOpenAIServiceTier(reqBody)),
			ReasoningEffort:               extractOpenAIReasoningEffort(reqBody, mappedModel, originalModel),
			Stream:                        reqStream,
			OpenAIWSMode:                  true,
			UpstreamTerminalEvent:         upstreamTerminalEvent,
			ResponseHeaders:               lease.HandshakeHeaders(),
			Duration:                      time.Since(startTime),
			FirstTokenMs:                  firstTokenMs,
			ClientDisconnect:              clientDisconnected,
			ImageCount:                    imageCounter.Count(),
			ImageOutputSizes:              imageCounter.Sizes(),
		}
	}
	partialResult := func() *OpenAIForwardResult {
		if !openAIUsageHasTokens(usage) && imageCounter.Count() == 0 && !clientDisconnected {
			return nil // No observed consumption: do not manufacture a zero-token bill.
		}
		return resultWithUsage()
	}
	canReplay := func() bool {
		return !synthesizedContinuation && !wroteDownstream && !upstreamOutputStarted && !clientDisconnected &&
			!openAIUsageHasTokens(usage) && imageCounter.Count() == 0
	}
	progress := beginOpenAIResponseProgress(c, "ctx_pool", "websocket", func() (bool, bool) {
		return wroteDownstream, upstreamOutputStarted
	})
	recordFinalStop := func(stage string, cause error) {
		if c == nil || clientDisconnected || ctx.Err() != nil || coderws.CloseStatus(cause) == coderws.StatusMessageTooBig {
			return
		}
		blocker, unresolved := openAIWSExecutionReplayAssessment(finalPayload, false)
		c.Set(openAIReplayBlockerContextKey, blocker)
		c.Set(openAIReplayUnresolvedContextKey, unresolved)
		progress.upstreamCloseCode = int(coderws.CloseStatus(cause))
		detail := progress.details()
		detail["failure_stage"] = stage
		detail["usage_observed"] = openAIUsageHasTokens(usage) || imageCounter.Count() > 0
		detail["gateway_continuation"] = synthesizedContinuation
		reason := OpenAIRetryStopReplayUnsafe
		if wroteDownstream {
			reason = OpenAIRetryStopDownstreamCommitted
		}
		raw, _ := json.Marshal(detail)
		RecordOpenAIRetryStop(c, account, reason, string(raw))
	}

	var flusher http.Flusher
	if reqStream {
		if s.responseHeaderFilter != nil {
			responseheaders.WriteFilteredHeaders(c.Writer.Header(), http.Header{}, s.responseHeaderFilter)
		}
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		f, ok := c.Writer.(http.Flusher)
		if !ok {
			lease.MarkBroken()
			return nil, wrapOpenAIWSFallback("streaming_not_supported", errors.New("streaming not supported"))
		}
		flusher = f
	}

	flushBatchSize := s.openAIWSEventFlushBatchSize()
	flushInterval := s.openAIWSEventFlushInterval()
	pendingFlushEvents := 0
	firstSemanticFlushed := false
	firstContentFlushed := false
	firstTextFlushed := false
	lastFlushAt := time.Now()
	flushStreamWriter := func(force bool) {
		if clientDisconnected || flusher == nil || pendingFlushEvents <= 0 {
			return
		}
		if !force && flushBatchSize > 1 && pendingFlushEvents < flushBatchSize {
			if flushInterval <= 0 || time.Since(lastFlushAt) < flushInterval {
				return
			}
		}
		flusher.Flush()
		if err := downstreamStreamError(c); err != nil {
			timingErr = err
			markClientDisconnected("downstream_flush_error")
			return
		}
		timing.MarkFlush()
		pendingFlushEvents = 0
		lastFlushAt = time.Now()
	}
	emitStreamMessage := func(message []byte, facts openAITimingEvent, forceFlush bool) {
		if clientDisconnected {
			return
		}
		frame := make([]byte, 0, len(message)+8)
		frame = append(frame, "data: "...)
		frame = append(frame, message...)
		frame = append(frame, '\n', '\n')
		_, wErr := c.Writer.Write(frame)
		if wErr == nil {
			timing.stageSSEDownstreamEvent(facts)
			timing.completeSSEDownstream()
			wroteDownstream = true
			pendingFlushEvents++
			// Prelude metadata may have just reset the batch timer. Each first
			// semantic/content/text boundary must reach the client immediately.
			firstSemantic := !firstSemanticFlushed && facts.semantic
			firstContent := !firstContentFlushed && facts.content
			firstText := !firstTextFlushed && facts.text
			flushStreamWriter(forceFlush || firstSemantic || firstContent || firstText)
			firstSemanticFlushed = firstSemanticFlushed || firstSemantic
			firstContentFlushed = firstContentFlushed || firstContent
			firstTextFlushed = firstTextFlushed || firstText
			return
		}
		timingErr = wErr
		markClientDisconnected("downstream_write_error")
		logger.LegacyPrintf("service.openai_gateway", "[OpenAI WS Mode] client disconnected, continue draining upstream: account=%d", account.ID)
	}
	flushBufferedStreamEvents := func(reason string) {
		if stagedEvents == 0 || clientDisconnected {
			return
		}
		flushed := stagedEvents
		if err := prelude.CommitTo(c.Writer); err != nil {
			timingErr = err
			markClientDisconnected("downstream_write_error")
			return
		}
		timing.stageSSEDownstreamEvent(stagedFacts)
		timing.completeSSEDownstream()
		wroteDownstream = true
		progress.commitReason = reason
		pendingFlushEvents += stagedEvents
		stagedEvents = 0
		flushStreamWriter(true)
		flushedBufferedEventCount += flushed
		if debugEnabled {
			logOpenAIWSModeDebug("buffer_flush account_id=%d conn_id=%s reason=%s flushed=%d total_flushed=%d client_disconnected=%v", account.ID, connID, truncateOpenAIWSLogValue(reason, openAIWSLogValueMaxLen), flushed, flushedBufferedEventCount, clientDisconnected)
		}
	}

	// Keep per-read timeouts unchanged for connected clients. Once a client
	// disconnects, use the same timeout as a bounded total drain budget.
	var pendingJSONDocuments [][]byte

readLoop:
	for {
		markClientRequestCanceled()
		var message []byte
		var readErr error
		readUsedDetachedContext := upstreamReadDetached
		if len(pendingJSONDocuments) > 0 {
			message = pendingJSONDocuments[0]
			pendingJSONDocuments = pendingJSONDocuments[1:]
		} else {
			currentReadTimeout := readTimeout
			if clientDisconnected && !clientDisconnectDrainStartedAt.IsZero() {
				remaining := readTimeout - time.Since(clientDisconnectDrainStartedAt)
				if remaining <= 0 {
					lease.MarkBroken()
					break readLoop
				}
				if remaining < currentReadTimeout {
					currentReadTimeout = remaining
				}
			}
			message, readErr = lease.ReadMessageWithContextTimeout(upstreamReadCtx, currentReadTimeout)
			if readErr == nil {
				if documents, repaired := splitOpenAIConcatenatedJSONDocuments(message); repaired {
					logOpenAIWSModeInfo(
						"concatenated_json_repaired account_id=%d conn_id=%s documents=%d bytes=%d",
						account.ID,
						truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
						len(documents),
						len(message),
					)
					message = documents[0]
					pendingJSONDocuments = append(pendingJSONDocuments, documents[1:]...)
				}
			}
		}
		markClientRequestCanceled()
		if readErr == nil && !json.Valid(message) {
			eventType, _, _ := parseOpenAIWSEventEnvelope(message)
			if eventType == "" {
				eventType = "unknown"
			}
			lease.MarkBroken()
			logOpenAIWSModeInfo(
				"invalid_event_json account_id=%d conn_id=%s event_type=%s bytes=%d wrote_downstream=%v",
				account.ID,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(eventType, openAIWSLogValueMaxLen),
				len(message),
				wroteDownstream,
			)
			if canReplay() && openAIWSUncertainExecutionReplaySafe(finalPayload) {
				return nil, wrapOpenAIWSFallback("invalid_event_json", errors.New("upstream websocket returned malformed Responses event JSON"))
			}
			recordFinalStop("invalid_event_json", nil)
			return partialResult(), errors.New("upstream websocket returned malformed Responses event JSON after execution started")
		}
		if readErr != nil {
			lease.MarkBroken()
			if c != nil {
				c.Set(OpsOpenAIWSPayloadBytesKey, payloadBytes)
			}
			closeStatus, closeReason := summarizeOpenAIWSReadCloseError(readErr)
			logOpenAIWSModeInfo(
				"read_fail account_id=%d conn_id=%s wrote_downstream=%v close_status=%s close_reason=%s cause=%s events=%d token_events=%d terminal_events=%d buffered_pending=%d buffered_flushed=%d first_event=%s last_event=%s payload_bytes=%d",
				account.ID,
				connID,
				wroteDownstream,
				closeStatus,
				closeReason,
				truncateOpenAIWSLogValue(readErr.Error(), openAIWSLogValueMaxLen),
				eventCount,
				tokenEventCount,
				terminalEventCount,
				stagedEvents,
				flushedBufferedEventCount,
				truncateOpenAIWSLogValue(firstEventType, openAIWSLogValueMaxLen),
				truncateOpenAIWSLogValue(lastEventType, openAIWSLogValueMaxLen),
				payloadBytes,
			)
			if clientDisconnected {
				if !readUsedDetachedContext && errors.Is(readErr, context.Canceled) && clientRequestCanceled() {
					continue
				}
				break
			}
			if canReplay() && openAIWSUncertainExecutionReplaySafe(finalPayload) {
				return nil, wrapOpenAIWSFallback(classifyOpenAIWSReadFallbackReason(readErr), readErr)
			}
			recordFinalStop("read_upstream", readErr)
			setOpsUpstreamError(c, 0, sanitizeUpstreamErrorMessage(readErr.Error()), "")
			return partialResult(), fmt.Errorf("openai ws read event: %w", readErr)
		}
		if normalized, changed := normalizeCompletedImageGenerationStatus(message); changed {
			message = normalized
		}

		eventType, eventResponseID, responseField := parseOpenAIWSEventEnvelope(message)
		progress.observeUpstream(eventType)
		facts := classifyOpenAITimingEventType(message, eventType)
		timing.observeTimingEvent(facts)
		timing.observeIncrementalEvent(message)
		if eventType == "" {
			continue
		}
		responseModelObserver.ObserveOpenAI(message, eventType)
		if facts.startsOutput {
			upstreamOutputStarted = true
		}
		eventCount++
		if firstEventType == "" {
			firstEventType = eventType
		}
		lastEventType = eventType

		if responseID == "" && eventResponseID != "" {
			responseID = eventResponseID
		}

		isTokenEvent := isOpenAIWSTokenEvent(eventType)
		if isTokenEvent {
			tokenEventCount++
		}
		isTerminalEvent := isOpenAIWSTerminalEvent(eventType)
		if isTerminalEvent {
			terminalEventCount++
		}
		if firstTokenMs == nil && isTokenEvent {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		if debugEnabled && shouldLogOpenAIWSEvent(eventCount, eventType) {
			logOpenAIWSModeDebug(
				"event_received account_id=%d conn_id=%s idx=%d type=%s bytes=%d token=%v terminal=%v buffered_pending=%d",
				account.ID,
				connID,
				eventCount,
				truncateOpenAIWSLogValue(eventType, openAIWSLogValueMaxLen),
				len(message),
				isTokenEvent,
				isTerminalEvent,
				stagedEvents,
			)
		}

		if !clientDisconnected {
			rewritten := false
			if needModelReplace && len(mappedModelBytes) > 0 && openAIWSEventMayContainModel(eventType) && bytes.Contains(message, mappedModelBytes) {
				message = replaceOpenAIWSMessageModel(message, mappedModel, originalModel)
				rewritten = true
			}
			if openAIWSEventMayContainToolCalls(eventType) && openAIWSMessageLikelyContainsToolCalls(message) {
				if corrected, changed := s.toolCorrector.CorrectToolCallsInSSEBytes(message); changed {
					message = corrected
					rewritten = true
				}
			}
			restored := restoreCodexToolNamesFromContext(c, message)
			if len(restored) > 0 && (len(message) == 0 || &restored[0] != &message[0]) {
				rewritten = true
			}
			message = restored
			if rewritten {
				facts = classifyOpenAITimingEventType(message, eventType)
			}
		}
		if openAIWSMessageShouldParseUsage(eventType, message) {
			parseOpenAIWSResponseUsageFromCompletedEvent(message, usage)
		}
		imageCounter.AddSSEData(message)

		if eventType == "error" || eventType == "response.failed" {
			markOpenAICyberPolicyEvent(c, message, http.StatusOK, usage)
		}

		if (eventType == "error" || eventType == "response.failed") && ctx.Err() == nil &&
			len(pendingJSONDocuments) == 0 && canReplay() && previousResponseID == "" && len(repairOptions) > 0 {
			next, reason, changed, repairErr := normalizeOpenAIResponsesRejectedFieldRetryBody(
				openAIWSRejectedFieldRetryHTTPStatus(message), finalPayload, message, repairOptions[0].webSearchAdded)
			if repairErr != nil {
				return nil, repairErr
			}
			if changed && repairOptions[0].state.Allow(next) {
				lease.MarkBroken()
				return nil, &openAIWSRejectedFieldRetryError{body: next, reason: reason}
			}
		}
		if eventType == "error" {
			s.handleOpenAIWSErrorEventTransientFailure(ctx, account, mappedModel, lease.HandshakeHeaders(), message)
			errCodeRaw, errTypeRaw, errMsgRaw := parseOpenAIWSErrorEventFields(message)
			s.persistOpenAIWSRateLimitSignal(ctx, account, lease.HandshakeHeaders(), message, errCodeRaw, errTypeRaw, errMsgRaw, mappedModel)
			errMsg := strings.TrimSpace(errMsgRaw)
			if errMsg == "" {
				errMsg = "Upstream websocket error"
			}
			fallbackReason, canFallback := classifyOpenAIWSErrorEventFromRaw(errCodeRaw, errTypeRaw, errMsgRaw)
			errCode, errType, errMessage := summarizeOpenAIWSErrorEventFieldsFromRaw(errCodeRaw, errTypeRaw, errMsgRaw)
			logOpenAIWSModeInfo(
				"error_event account_id=%d conn_id=%s idx=%d fallback_reason=%s can_fallback=%v err_code=%s err_type=%s err_message=%s",
				account.ID,
				connID,
				eventCount,
				truncateOpenAIWSLogValue(fallbackReason, openAIWSLogValueMaxLen),
				canFallback,
				errCode,
				errType,
				errMessage,
			)
			if fallbackReason == "previous_response_not_found" {
				logOpenAIWSModeInfo(
					"previous_response_not_found_diag account_id=%d account_type=%s conn_id=%s previous_response_id=%s previous_response_id_kind=%s response_id=%s event_idx=%d req_stream=%v store_disabled=%v conn_reused=%v session_hash=%s header_session_id=%s header_conversation_id=%s session_id_source=%s conversation_id_source=%s has_turn_state=%v turn_state_len=%d has_prompt_cache_key=%v err_code=%s err_type=%s err_message=%s",
					account.ID,
					account.Type,
					connID,
					truncateOpenAIWSLogValue(previousResponseID, openAIWSIDValueMaxLen),
					normalizeOpenAIWSLogValue(previousResponseIDKind),
					truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
					eventCount,
					reqStream,
					storeDisabled,
					lease.Reused(),
					truncateOpenAIWSLogValue(sessionHash, 12),
					openAIWSHeaderValueForLog(wsHeaders, "session_id"),
					openAIWSHeaderValueForLog(wsHeaders, "conversation_id"),
					normalizeOpenAIWSLogValue(sessionResolution.SessionSource),
					normalizeOpenAIWSLogValue(sessionResolution.ConversationSource),
					turnState != "",
					len(turnState),
					promptCacheKey != "",
					errCode,
					errType,
					errMessage,
				)
			}
			// error 事件后连接不再可复用，避免回池后污染下一请求。
			lease.MarkBroken()
			// A concatenated frame may contradict its leading rejection. Retain
			// already-received consumption without delivering output after error,
			// and never treat a rejection with a trailing document as replay proof.
			for _, tail := range pendingJSONDocuments {
				tailType, tailResponseID, _ := parseOpenAIWSEventEnvelope(tail)
				if responseID == "" && tailResponseID != "" {
					responseID = tailResponseID
				}
				upstreamOutputStarted = upstreamOutputStarted || classifyOpenAITimingEventType(tail, tailType).startsOutput
				responseModelObserver.ObserveOpenAI(tail, tailType)
				if openAIWSMessageShouldParseUsage(tailType, tail) {
					parseOpenAIWSResponseUsageFromCompletedEvent(tail, usage)
				}
				imageCounter.AddSSEData(tail)
			}
			publicError := s.redactAgentIdentitySensitiveBody(ctx, account, message)
			if synthesizedContinuation && strings.TrimSpace(errCodeRaw) == "previous_response_not_found" &&
				!wroteDownstream && !upstreamOutputStarted && !clientDisconnected &&
				!openAIUsageHasTokens(usage) && imageCounter.Count() == 0 && len(pendingJSONDocuments) == 0 {
				return nil, &openAIWSFallbackError{Reason: "previous_response_not_found", Err: errors.New(errMsg), recoveryPayload: fullPayload, clientError: publicError}
			}
			// HTTP ingress uses the same account recovery as native WS ingress.
			// A rate-limit rejection must not fall back to same-account HTTP,
			// nor be replayed after delivered output or observed consumption.
			rateLimited := isOpenAIWSRateLimitError(errCodeRaw, errTypeRaw, errMsgRaw)
			if rateLimited && len(pendingJSONDocuments) == 0 && canReplay() {
				s.recordOpenAIStreamUpstreamError(c, account, false, "", "failover", message, errMsg)
				return nil, s.newOpenAIWSRateLimitFailoverError(account, lease.HandshakeHeaders(), message, errMsg, mappedModel)
			}
			if len(pendingJSONDocuments) == 0 && canFallback && canReplay() && (fallbackReason == "ws_connection_limit_reached" || openAIWSUncertainExecutionReplaySafe(finalPayload)) {
				return nil, &openAIWSFallbackError{Reason: fallbackReason, Err: errors.New(errMsg), clientError: publicError}
			}
			if canFallback || rateLimited {
				recordFinalStop("upstream_error", nil)
			}
			statusCode := openAIWSErrorHTTPStatusFromRaw(errCodeRaw, errTypeRaw)
			setOpsUpstreamError(c, statusCode, errMsg, "")
			if reqStream && !clientDisconnected {
				flushBufferedStreamEvents("error_event")
				emitStreamMessage(message, facts, true)
				if !clientDisconnected {
					markOpenAIStreamTerminalFailureDelivered(c, message, errMsg)
				}
			}
			if !reqStream {
				writeOpenAIUpstreamClientError(c, statusCode, publicError, sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(publicError)))
			}
			return partialResult(), fmt.Errorf("openai ws error event: %s", errMsg)
		}

		if reqStream {
			// Keep replaceable metadata staged only until actual progress. Once
			// committed, later metadata must not reopen a closed staging buffer.
			// Legacy TTFT classification never controls delivery.
			shouldBuffer := !wroteDownstream && !facts.startsOutput && !facts.content && !isTerminalEvent
			if shouldBuffer && !clientDisconnected {
				frame := append(append(append(make([]byte, 0, len(message)+8), "data: "...), message...), '\n', '\n')
				if _, err := prelude.Write(frame); err != nil {
					lease.MarkBroken()
					return partialResult(), &UpstreamFailoverError{StatusCode: http.StatusBadGateway, Scope: GatewayFailureScopeRequest, Reason: "prelude_buffer_limit", Cause: err, NextAccountAction: NextAccountStop, ClientMessage: "Unable to buffer upstream pre-output events"}
				}
				stagedEvents++
				mergeOpenAITimingEvent(&stagedFacts, facts)
				bufferedEventCount++
				if debugEnabled && shouldLogOpenAIWSBufferedEvent(bufferedEventCount) {
					logOpenAIWSModeDebug(
						"buffer_enqueue account_id=%d conn_id=%s idx=%d event_idx=%d event_type=%s buffer_size=%d",
						account.ID,
						connID,
						bufferedEventCount,
						eventCount,
						truncateOpenAIWSLogValue(eventType, openAIWSLogValueMaxLen),
						stagedEvents,
					)
				}
			} else {
				flushBufferedStreamEvents(eventType)
				emitStreamMessage(message, facts, isTerminalEvent)
			}
		} else {
			if responseField.Exists() && responseField.Type == gjson.JSON {
				finalResponse = []byte(responseField.Raw)
			}
		}

		if isTerminalEvent {
			if !clientDisconnected {
				markOpenAIWSClientVisibleFailure(c, eventType, message)
			}
			upstreamTerminalEvent = s.handleOpenAIWSTerminalTransientFailure(ctx, account, mappedModel, lease.HandshakeHeaders(), message)
			// A terminal event must be the final JSON document in its WS message.
			// Ignore any tail for the completed client turn, but never reuse the
			// ambiguous upstream connection for another request.
			cleanExit = len(pendingJSONDocuments) == 0
			if cleanExit && eventResponseID != "" &&
				(eventType == "response.completed" || eventType == "response.done" ||
					(eventType == "response.incomplete" && gjson.GetBytes(message, "response.incomplete_details.reason").String() == "interrupted")) {
				responseID, continuationResponseID = eventResponseID, eventResponseID
				lease.ConfirmResponse(eventResponseID)
			}
			break
		}
	}

	if clientDisconnected && terminalEventCount == 0 {
		return resultWithUsage(), fmt.Errorf("openai ws stream incomplete after client disconnect: %w", context.Canceled)
	}
	if !reqStream {
		if clientDisconnected {
			return resultWithUsage(), nil
		}
		if len(finalResponse) == 0 {
			logOpenAIWSModeInfo(
				"missing_final_response account_id=%d conn_id=%s events=%d token_events=%d terminal_events=%d wrote_downstream=%v",
				account.ID,
				connID,
				eventCount,
				tokenEventCount,
				terminalEventCount,
				wroteDownstream,
			)
			if canReplay() && openAIWSUncertainExecutionReplaySafe(finalPayload) {
				return nil, wrapOpenAIWSFallback("missing_final_response", errors.New("no terminal response payload"))
			}
			recordFinalStop("missing_final_response", nil)
			return partialResult(), errors.New("ws finished without final response")
		}

		if needModelReplace {
			finalResponse = s.replaceModelInResponseBody(finalResponse, mappedModel, originalModel)
		}
		finalResponse = s.correctToolCallsInResponseBody(finalResponse)
		populateOpenAIUsageFromResponseJSON(finalResponse, usage)
		if responseID == "" {
			responseID = strings.TrimSpace(gjson.GetBytes(finalResponse, "id").String())
		}

		errorCount := len(c.Errors)
		c.Data(http.StatusOK, "application/json", finalResponse)
		if len(c.Errors) == errorCount {
			timing.MarkDownstream(finalResponse)
		} else {
			timingErr = c.Errors.Last().Err
		}
	} else {
		flushStreamWriter(true)
	}

	if continuationResponseID != "" && stateStore != nil {
		ttl := s.openAIWSResponseStickyTTL()
		logOpenAIWSBindResponseAccountWarn(groupID, account.ID, continuationResponseID, stateStore.BindResponseMode(ctx, groupID, continuationResponseID, ResponseModeNative, ttl))
		logOpenAIWSBindResponseAccountWarn(groupID, account.ID, continuationResponseID, stateStore.BindResponseAccount(ctx, groupID, continuationResponseID, account.ID, ttl))
		stateStore.BindResponseConn(continuationResponseID, lease.ConnID(), ttl, openAIWSResponseScopeForStore(storeDisabled))
	}
	if stateStore != nil && storeDisabled && sessionHash != "" {
		stateStore.BindSessionConn(groupID, sessionHash, lease.ConnID(), s.openAIWSSessionStickyTTL())
	}
	logOpenAIWSModeDebug(
		"completed account_id=%d conn_id=%s response_id=%s stream=%v events=%d token_events=%d terminal_events=%d buffered_events=%d buffered_flushed=%d first_event=%s last_event=%s wrote_downstream=%v client_disconnected=%v",
		account.ID,
		connID,
		truncateOpenAIWSLogValue(strings.TrimSpace(responseID), openAIWSIDValueMaxLen),
		reqStream,
		eventCount,
		tokenEventCount,
		terminalEventCount,
		bufferedEventCount,
		flushedBufferedEventCount,
		truncateOpenAIWSLogValue(firstEventType, openAIWSLogValueMaxLen),
		truncateOpenAIWSLogValue(lastEventType, openAIWSLogValueMaxLen),
		wroteDownstream,
		clientDisconnected,
	)

	return resultWithUsage(), nil
}

// ProxyResponsesWebSocketFromClient 处理客户端入站 WebSocket（OpenAI Responses WS Mode）并转发到上游。
// 当前实现按“单请求 -> 终止事件 -> 下一请求”的顺序代理，适配 Codex CLI 的 turn 模式。
// stripCodexSparkImageGenerationToolFromRawPayload removes the image_generation
// tool from a raw /responses payload when the upstream model is gpt-5.3-codex-spark.
// Spark rejects that tool upstream with HTTP 400 (invalid_request_error, param=tools);
// Codex clients advertise it by default. Returns the (possibly unchanged) payload,
// whether it changed, and any JSON decode error.
func stripCodexSparkImageGenerationToolFromRawPayload(payload []byte, model string) ([]byte, bool, error) {
	if !isCodexSparkModel(model) {
		return payload, false, nil
	}
	return stripOpenAIImageGenerationToolsFromRawPayload(payload)
}

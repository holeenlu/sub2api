package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// These facts are local to one selected account, never shared across failover.
// They replace the native adapter's cross-phase locals, not live authorization.
type openAIResponsesPreparation struct {
	body, canonicalBody                                  []byte
	view                                                 openAIRequestView
	codexClient, compact, lite, nativeCN, messagesBridge bool
	imageToolPolicy                                      string
}

type openAIPreparedResponses struct {
	webSearchHistoryToolAdded                        bool
	body                                             []byte
	decoded                                          map[string]any
	originalModel, billingModel, upstreamModel       string
	promptCacheKey, clientPromptCacheKey             string
	imageBillingModel, imageSizeTier, imageInputSize string
	stream, codexClient                              bool
}

func (p *openAIPreparedResponses) decode(c *gin.Context) (map[string]any, error) {
	if p.decoded != nil {
		return p.decoded, nil
	}
	value, err := newOpenAIRequestView(p.body).Decode(c)
	if err == nil {
		p.decoded = value
	}
	return value, err
}

func (s *OpenAIGatewayService) prepareNativeOpenAIResponses(ctx context.Context, c *gin.Context, account *Account, plan openAIResponsesPreparation) (*openAIPreparedResponses, error) {
	body, canonicalImageIntentBody, requestView := plan.body, plan.canonicalBody, plan.view
	reqModel, promptCacheKey := requestView.Model, requestView.PromptCacheKey
	isCodexCLI, compactPath, responsesLite := plan.codexClient, plan.compact, plan.lite
	nativeCNResponses, compatMessagesBridge := plan.nativeCN, plan.messagesBridge
	codexImageGenerationExplicitToolPolicy := plan.imageToolPolicy
	bodyModified := false
	webSearchHistoryToolAdded := false
	clientPromptCacheKey := promptCacheKey
	var reqBody map[string]any
	ensureReqBody := func() (map[string]any, error) {
		if requestView.HasPatches() {
			patchedBody, patchErr := requestView.ApplyPatches()
			if patchErr != nil {
				return nil, patchErr
			}
			body = patchedBody
			requestView = newOpenAIRequestView(body)
			reqBody = nil
			bodyModified = false
		}
		if reqBody != nil {
			return reqBody, nil
		}
		decoded, decodeErr := requestView.Decode(c)
		if decodeErr != nil {
			return nil, decodeErr
		}
		reqBody = decoded
		return reqBody, nil
	}
	markPatchSet := func(path string, value any) {
		bodyModified = true
		if requestView.patchesDisabled {
			if reqBody != nil {
				setOpenAIRequestMapPath(reqBody, path, value)
			}
			return
		}
		requestView.MarkPatchSet(path, value)
	}
	markPatchDelete := func(path string) {
		bodyModified = true
		if requestView.patchesDisabled {
			if reqBody != nil {
				deleteOpenAIRequestMapPath(reqBody, path)
			}
			return
		}
		requestView.MarkPatchDelete(path)
	}
	disablePatch := func() {
		requestView.DisablePatches()
	}
	markDecodedModified := func() {
		bodyModified = true
		disablePatch()
	}

	apiKey := getAPIKeyFromContext(c)
	imageGenerationAllowed := GroupAllowsImageGeneration(nil)
	if apiKey != nil {
		imageGenerationAllowed = GroupAllowsImageGeneration(apiKey.Group)
	}
	codexImageGenerationBridgeEnabled := isCodexCLI &&
		!isOpenAIResponsesLiteHeader(c.GetHeader(responsesLiteHeader)) &&
		imageGenerationAllowed &&
		codexImageGenerationExplicitToolPolicy != codexImageGenerationExplicitToolPolicyStrip &&
		s.isCodexImageGenerationBridgeEnabled(ctx, account, apiKey)
	var imageIntent bool
	canonicalImageIntent := resolveOpenAIImageIntentHint(c, reqModel, canonicalImageIntentBody, IsImageGenerationIntent)
	if isCodexCLI && codexImageGenerationExplicitToolPolicy == codexImageGenerationExplicitToolPolicyStrip {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if stripOpenAIImageGenerationTools(decoded) {
			markDecodedModified()
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Stripped /responses image_generation tool for Codex client by account policy")
		}
		imageIntent = IsImageGenerationIntentMap(openAIResponsesEndpoint, reqModel, decoded)
	} else {
		imageIntent = canonicalImageIntent
	}
	if imageIntent && !imageGenerationAllowed {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalFeatureGate)
		writeOpenAIResponsesErrorJSON(c, http.StatusForbidden, gin.H{"error": gin.H{"type": "permission_error", "message": ImageGenerationPermissionMessage()}})
		return nil, errors.New("image generation disabled for group")
	}

	isCompactRequest := compactPath
	requestedModel := reqModel
	billingModel, upstreamModel := resolveOpenAIForwardMappedModels(account, requestedModel, isCompactRequest)
	if isCompactRequest {
		if compactModel := s.resolveOpenAICompactFallbackModel(account, requestedModel); compactModel != "" {
			upstreamModel = compactModel
		}
	}
	instructions := gjson.GetBytes(body, "instructions")
	instructionsEmpty := !instructions.Exists() || instructions.Type != gjson.String || strings.TrimSpace(instructions.String()) == ""
	if instructionsEmpty && account.UsesOpenAICodexProtocol() && !compatMessagesBridge && !nativeCNResponses {
		markPatchSet("instructions", defaultCodexSynthInstructions(upstreamModel))
	}
	if billingModel != requestedModel {
		logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Model mapping applied: %s -> %s (account: %s, isCodexCLI: %v)", requestedModel, billingModel, account.Name, isCodexCLI)
	}
	reqModel = billingModel
	if upstreamModel != requestedModel {
		markPatchSet("model", upstreamModel)
	}
	if upstreamModel != billingModel {
		if isCompactRequest {
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Compact model mapping applied: %s -> %s (account: %s, isCodexCLI: %v)", requestedModel, upstreamModel, account.Name, isCodexCLI)
		} else {
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Upstream model resolved: %s -> %s (account: %s, type: %s, isCodexCLI: %v)", billingModel, upstreamModel, account.Name, account.Type, isCodexCLI)
		}
	}
	if account.UsesOpenAICodexProtocol() && strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String()) == "minimal" {
		markPatchSet("reasoning.effort", "none")
		logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Normalized reasoning.effort: minimal -> none (account: %s)", account.Name)
	}
	if account.UsesOpenAICodexProtocol() && (strings.TrimSpace(gjson.GetBytes(body, "text.format.type").String()) == "json_schema" ||
		strings.TrimSpace(gjson.GetBytes(body, "response_format.type").String()) == "json_schema") {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if normalizeOpenAIResponseFormatSchemas(decoded) {
			markDecodedModified()
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Normalized Responses JSON schema compatibility")
		}
	}

	imageIntent = imageIntent || IsImageGenerationIntent(openAIResponsesEndpoint, reqModel, nil) || isOpenAIImageGenerationModel(upstreamModel)
	if imageIntent && !imageGenerationAllowed {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalFeatureGate)
		writeOpenAIResponsesErrorJSON(c, http.StatusForbidden, gin.H{"error": gin.H{"type": "permission_error", "message": ImageGenerationPermissionMessage()}})
		return nil, errors.New("image generation disabled for group")
	}

	// /responses/compact 是会话压缩请求：上游不接受 tool_choice（400 unknown_parameter），
	// 注入 image_generation 工具也没有意义，整块豁免。
	if imageGenerationAllowed && !isCompactRequest && (codexImageGenerationBridgeEnabled || isOpenAIImageGenerationModel(requestView.Model) || openAIRequestBodyImageGenerationToolNeedsNormalization(body) || isOpenAIImageGenerationModel(upstreamModel)) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if codexImageGenerationBridgeEnabled && ensureOpenAIResponsesImageGenerationTool(decoded) {
			markDecodedModified()
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Injected /responses image_generation tool for Codex client")
		}
		if codexImageGenerationBridgeEnabled && ensureOpenAIResponsesImageGenerationToolChoiceAuto(decoded) {
			markDecodedModified()
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Set /responses image_generation tool_choice=auto for Codex client")
		}
		if normalizeOpenAIResponsesImageGenerationTools(decoded) {
			markDecodedModified()
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Normalized /responses image_generation tool payload")
		}
		if normalizeOpenAIResponsesImageOnlyModel(decoded) {
			markDecodedModified()
			if model, ok := decoded["model"].(string); ok {
				upstreamModel = strings.TrimSpace(model)
			}
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Normalized /responses image-only model request inbound_model=%s image_model=%s upstream_model=%s", requestView.Model, billingModel, upstreamModel)
		}
		if err := validateOpenAIResponsesImageModel(decoded, upstreamModel); err != nil {
			setOpsUpstreamError(c, http.StatusBadRequest, err.Error(), "")
			writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error(), "param": "model"}})
			return nil, err
		}
		if hasOpenAIImageGenerationTool(decoded) {
			imageIntent = true
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] /responses image_generation request inbound_model=%s mapped_model=%s account_type=%s", requestView.Model, upstreamModel, account.Type)
		}
		if codexImageGenerationBridgeEnabled && applyCodexImageGenerationBridgeInstructions(decoded) {
			markDecodedModified()
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Added Codex image_generation bridge instructions")
		}
	} else if imageGenerationAllowed && imageIntent && openAIRequestBodyHasImageGenerationDeclaration(body) {
		// 完整 image_generation tool 只做 raw 计费读取，校验/桥接/旧字段迁移命中时才展开大 input map。
		logger.LegacyPrintf("service.openai_gateway", "[OpenAI] /responses image_generation request inbound_model=%s mapped_model=%s account_type=%s", requestView.Model, upstreamModel, account.Type)
	}

	if isCodexSparkModel(upstreamModel) && openAIRequestBodyMayContainImageInput(body) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if err := validateCodexSparkInput(decoded, upstreamModel); err != nil {
			setOpsUpstreamError(c, http.StatusBadRequest, err.Error(), "")
			writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error(), "param": "input"}})
			return nil, err
		}
	}

	// gpt-5.3-codex-spark also rejects the image_generation tool (HTTP 400,
	// param=tools). Strip it here so both APIKey and OAuth /responses paths are
	// covered regardless of the image-generation feature gate.
	if isCodexSparkModel(upstreamModel) && openAIRequestBodyHasImageGenerationDeclaration(body) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if stripCodexSparkImageGenerationTools(decoded) {
			markDecodedModified()
		}
	}

	if account.UsesOpenAICodexProtocol() {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		// Responses OAuth 与 Chat 兼容入口保持一致：纯文本 system 可以无损提升后删除，
		// JSON object 模式仍需在 input 中保留 JSON 指令供上游兼容校验。
		omitPromotedSystemMessages := !strings.EqualFold(
			strings.TrimSpace(gjson.GetBytes(body, "text.format.type").String()),
			"json_object",
		)
		codexResult := codexTransformResult{}
		if compatMessagesBridge {
			codexResult = applyCodexOAuthTransformWithOptions(decoded, codexOAuthTransformOptions{
				IsCodexCLI:                          isCodexCLI,
				IsCompact:                           isCompactRequest,
				SkipDefaultInstructions:             true,
				PreserveToolCallIDs:                 true,
				OmitPromotedSystemMessagesFromInput: omitPromotedSystemMessages,
				ResponsesLite:                       responsesLite,
			})
			ensureCodexOAuthInstructionsField(decoded)
			markDecodedModified()
		} else {
			codexResult = applyCodexOAuthTransformWithOptions(decoded, codexOAuthTransformOptions{
				IsCodexCLI:                          isCodexCLI,
				IsCompact:                           isCompactRequest,
				OmitPromotedSystemMessagesFromInput: omitPromotedSystemMessages,
				ResponsesLite:                       responsesLite,
			})
		}
		if codexResult.Error != nil {
			writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": codexResult.Error.Error()}})
			return nil, codexResult.Error
		}
		webSearchHistoryToolAdded = codexResult.WebSearchHistoryToolAdded
		setCodexToolNameReverse(c, codexResult.ToolNameReverse)
		if codexResult.Modified {
			markDecodedModified()
		}
		// 带真实 device_id 时补齐 client_metadata 安装标识，与真实 Codex 对齐（compact 形态不同，跳过）。
		if !isCompactRequest && applyCodexClientMetadata(decoded, account) {
			markDecodedModified()
		}
		if currentClientPromptCacheKey, ok := decoded["prompt_cache_key"].(string); ok {
			clientPromptCacheKey = currentClientPromptCacheKey
		}
		// Account namespace is orthogonal to fingerprint convergence: preserve
		// each client's identity cardinality, but never reuse it across OAuth
		// credentials after scheduler failover.
		if isCompactRequest {
			// Compact intentionally keeps its existing cache-key bytes. Scope
			// only the metadata so the header builder never treats a raw client
			// identity as an already isolated value.
			if metadata, ok := decoded["client_metadata"].(map[string]any); ok {
				source := codexAccountIdentitySource(c, account)
				apiKeyID := getAPIKeyIDFromContext(c)
				if applyCodexAccountIdentityFields(metadata, source, apiKeyID) {
					markDecodedModified()
				}
				if applyCodexAccountIdentityEmbeddedMetadata(metadata, source, apiKeyID) {
					markDecodedModified()
				}
			}
		} else if applyCodexAccountIdentityClientMetadataMap(decoded, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c)) {
			markDecodedModified()
		}
		stageCodexFingerprintIDs(c, nil)
		// 指纹收敛：一次性解析收敛 ID，请求体和出站头共享同一份 IDs（保证 turn_id 等随机字段一致）。
		// fingerprintIDs 在此处解析，后续 buildUpstreamRequest 中使用同一份。
		if !isCompactRequest {
			var clientHeaders http.Header
			if c != nil && c.Request != nil {
				clientHeaders = c.Request.Header
			}
			fpIDs := resolveCodexFingerprintIDsForCaller(codexAccountIdentitySource(c, account), clientHeaders, getAPIKeyIDFromContext(c))
			if fpIDs != nil {
				if applyCodexFingerprintClientMetadata(decoded, fpIDs) {
					markDecodedModified()
				}
			}
			// 将 fpIDs 存入 gin context，供 buildUpstreamRequest 中头改写使用。
			// 无条件覆写（含 nil）：failover 从收敛账号切到 off 账号时，上一
			// 账号的 IDs 不得残留（stageCodexFingerprintIDs 注释）。
			stageCodexFingerprintIDs(c, fpIDs)
		}
		if codexResult.NormalizedModel != "" {
			upstreamModel = codexResult.NormalizedModel
		}
		if strings.TrimSpace(clientPromptCacheKey) != "" {
			// The body now carries an account-scoped value. Keep the original here
			// so the header builder derives the same namespace exactly once.
			promptCacheKey = clientPromptCacheKey
		} else if currentPromptCacheKey, ok := decoded["prompt_cache_key"].(string); ok && currentPromptCacheKey != "" {
			// Fingerprint convergence may inject a default key when the client did
			// not provide one; preserve that existing fallback.
			promptCacheKey = currentPromptCacheKey
		} else if codexResult.PromptCacheKey != "" {
			promptCacheKey = codexResult.PromptCacheKey
		}
	}

	if !SupportsVerbosity(upstreamModel) && gjson.GetBytes(body, "text.verbosity").Exists() {
		markPatchDelete("text.verbosity")
	}

	if !isCodexCLI {
		maxOutputTokens := gjson.GetBytes(body, "max_output_tokens")
		if maxOutputTokens.Exists() {
			switch account.Platform {
			case PlatformOpenAI, PlatformDeepseek:
				// Preserve Responses-native output limits unless the selected upstream
				// explicitly rejects the field in the bounded HTTP retry loop below.
			case PlatformAnthropic:
				decoded, decodeErr := ensureReqBody()
				if decodeErr != nil {
					return nil, decodeErr
				}
				delete(decoded, "max_output_tokens")
				if _, hasMaxTokens := decoded["max_tokens"]; !hasMaxTokens {
					decoded["max_tokens"] = maxOutputTokens.Value()
				}
				markDecodedModified()
			case PlatformGemini:
				markPatchDelete("max_output_tokens")
			default:
				markPatchDelete("max_output_tokens")
			}
		}
		// /v1/responses 的规范输出上限字段是 max_output_tokens；部分客户端仍按
		// Chat Completions 习惯发送 max_tokens，兼容 Responses 上游会拒绝该字段（#4417）。
		// 仅对 OpenAI 平台归一化：Anthropic 合法使用 max_tokens，其 max_output_tokens
		// 反向转换已在上方 switch 中处理。
		if account.Platform == PlatformOpenAI {
			if maxTokens := gjson.GetBytes(body, "max_tokens"); maxTokens.Exists() {
				if !gjson.GetBytes(body, "max_output_tokens").Exists() {
					markPatchSet("max_output_tokens", maxTokens.Value())
				}
				markPatchDelete("max_tokens")
			}
		}
		if gjson.GetBytes(body, "max_completion_tokens").Exists() && (account.Type == AccountTypeAPIKey || account.Platform != PlatformOpenAI) {
			markPatchDelete("max_completion_tokens")
		}
		// Cache policy belongs to the selected upstream protocol, not the client's
		// User-Agent. Preserve Responses fields here; Codex-only unsupported fields
		// are already handled by applyCodexOAuthTransformWithOptions above.
	}
	// Ollama Cloud（实际 Responses 上游为 ollama.com）输出上限 clamp：对 Codex 与
	// 非 Codex 客户端一律执行（真实 Codex 客户端同样会带超限 max_output_tokens 被
	// ollama.com 以 400 拒绝）。在 `!isCodexCLI` 归一化块之后独立调用：非 Codex 时
	// 位于平台字段归一化之后，不跳过原有平台 switch（patch 按追加顺序应用，set 在
	// 先前的 delete/set 之后生效）；Codex 请求不做归一化，直接按 body 现值判定。
	if clampedCap, ok := ollamaCloudResponsesMaxOutputTokensClamp(account, upstreamModel, body); ok {
		markPatchSet("max_output_tokens", clampedCap)
	}
	// Keep continuation anchors on HTTP as on the WS HTTP bridge. A nonempty
	// input can be only a delta; transport selection is not evidence that the
	// complete history is available. Only the existing replay owner may replace
	// an anchor with complete, ownership-checked history.
	if openAIRequestBodyMayContainEmptyBase64InputImage(body) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if sanitizeEmptyBase64InputImagesInOpenAIRequestBodyMap(decoded) {
			markDecodedModified()
		}
	}

	rawTier := requestView.ServiceTier
	if openAIGroupForcesFast(ctx, account) {
		rawTier = OpenAIFastTierPriority
		if requestView.ServiceTier != OpenAIFastTierPriority {
			markPatchSet("service_tier", OpenAIFastTierPriority)
		}
	}
	if rawTier != "" {
		if normTier := normalizedOpenAIServiceTierValue(rawTier); normTier != "" {
			action, errMsg := s.evaluateOpenAIFastPolicy(ctx, account, upstreamModel, normTier)
			switch action {
			case BetaPolicyActionBlock:
				msg := errMsg
				if msg == "" {
					msg = fmt.Sprintf("openai service_tier=%s is not allowed for model %s", normTier, upstreamModel)
				}
				blocked := &OpenAIFastBlockedError{Message: msg}
				writeOpenAIFastPolicyBlockedResponse(c, blocked)
				return nil, blocked
			case BetaPolicyActionFilter:
				markPatchDelete("service_tier")
			case OpenAIFastPolicyActionForcePriority:
				if rawTier != OpenAIFastTierPriority {
					markPatchSet("service_tier", OpenAIFastTierPriority)
				}
			default:
				if normTier != rawTier {
					markPatchSet("service_tier", normTier)
				}
			}
		}
	} else if s.shouldForceOpenAIFastPriorityForMissingTier(ctx, account, upstreamModel) {
		markPatchSet("service_tier", OpenAIFastTierPriority)
	}

	if account.UsesOpenAICodexProtocol() {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if input, ok := decoded["input"].([]any); ok {
			changed, inputErr := sanitizeOpenAIResponsesOrphanToolOutputs(decoded, input, hasOpenAIResponsesContextReference(decoded))
			if inputErr != nil {
				writeOpenAIResponsesInputError(c, inputErr)
				return nil, inputErr
			}
			if changed {
				markDecodedModified()
			}
		}
	}
	if reqBody != nil || openAIResponsesInputMayNeedTruncation(body) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if truncateOpenAIResponsesInputText(decoded) {
			markDecodedModified()
		}
	}

	if bodyModified {
		if requestView.HasPatches() {
			if patchedBody, patchErr := requestView.ApplyPatches(); patchErr == nil {
				body = patchedBody
				reqBody = nil
				bodyModified = false
			}
		}
		if bodyModified {
			decoded, decodeErr := ensureReqBody()
			if decodeErr != nil {
				return nil, decodeErr
			}
			previousBody := body
			var marshalErr error
			body, marshalErr = marshalOpenAIUpstreamJSON(decoded)
			body = restoreOpenAIUpstreamJSONTopLevelOrder(previousBody, body)
			if marshalErr != nil {
				return nil, fmt.Errorf("serialize request body: %w", marshalErr)
			}
		}
	}
	// Run after orphan-output filtering and all request-map rebuilds so a
	// compaction trigger cannot remain ahead of surviving history items.
	if normalizedBody, changed, normalizeErr := NormalizeCompactionTriggerInputOrder(body); normalizeErr != nil {
		return nil, fmt.Errorf("normalize compaction trigger order: %w", normalizeErr)
	} else if changed {
		body = normalizedBody
		reqBody = nil
	}
	imageBillingModel := ""
	imageSizeTier := ""
	imageInputSize := ""
	if imageIntent {
		var imageCfg OpenAIResponsesImageBillingConfig
		var imageCfgErr error
		if reqBody != nil {
			imageCfg, imageCfgErr = resolveOpenAIResponsesImageBillingConfigDetailed(reqBody, billingModel)
		} else {
			imageCfg, imageCfgErr = resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body, billingModel)
		}
		if imageCfgErr != nil {
			setOpsUpstreamError(c, http.StatusBadRequest, imageCfgErr.Error(), "")
			writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": imageCfgErr.Error(), "param": "size"}})
			return nil, imageCfgErr
		}
		imageBillingModel = imageCfg.Model
		imageSizeTier = imageCfg.SizeTier
		imageInputSize = imageCfg.InputSize
	}
	if account.IsOpenAI() && !isCompactRequest {
		if inputErr := validateOpenAIResponsesInputTransition(canonicalImageIntentBody, body); inputErr != nil {
			writeOpenAIResponsesInputError(c, inputErr)
			return nil, inputErr
		}
	}
	return &openAIPreparedResponses{
		webSearchHistoryToolAdded: webSearchHistoryToolAdded,
		body:                      body, decoded: reqBody, originalModel: requestedModel,
		billingModel: billingModel, upstreamModel: upstreamModel,
		promptCacheKey: promptCacheKey, clientPromptCacheKey: clientPromptCacheKey,
		imageBillingModel: imageBillingModel, imageSizeTier: imageSizeTier, imageInputSize: imageInputSize,
		stream: plan.view.Stream, codexClient: isCodexCLI,
	}, nil
}

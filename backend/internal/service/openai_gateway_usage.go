package service

// 本文件由 openai_gateway_service.go 纯移动拆分而来：用量记录、计费成本计算与
// Codex 用量快照。仅做代码搬迁，无任何行为变更。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"go.uber.org/zap"
)

// OpenAIRecordUsageInput input for recording usage
type OpenAIRecordUsageInput struct {
	// nativePricing is supplied only by the validated intent preparer.
	// It is explicit and must never be synthesized from late entities.
	nativePricing      *nativeIntentSettlementScope
	Result             *OpenAIForwardResult
	APIKey             *APIKey
	User               *User
	Account            *Account
	Subscription       *UserSubscription
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string // 请求的 User-Agent
	IPAddress          string // 请求的客户端 IP 地址
	SessionID          string // 客户端显式会话标识（session_id / X-Session-Id 等请求头），仅用于用量行会话关联
	RequestPayloadHash string
	APIKeyService      APIKeyQuotaUpdater
	QuotaPlatform      string // user×platform quota platform resolved by the handler before async billing.
	// PricingAt 是请求级定价时刻（请求开始捕获，与利润门的 D 同源）：高峰因子
	// 按该时刻计算，保证同一请求从准入到扣费不中途变价。零值回退记录时刻
	//（既有行为），供未装配的路径（图片/异步/cyber 等）沿用。
	PricingAt time.Time
	// CyberBlocked 为 true 时把该用量行标记为 cyber（request_type=cyber），计费逻辑不变。
	CyberBlocked bool
	// NativeCompactionV2 is an orthogonal semantic flag captured by the
	// Responses handler from stream=true + compaction_trigger. It never stores
	// the request payload and does not replace the transport request type.
	NativeCompactionV2 bool
	ChannelUsageFields
}

// CyberPolicyUsageInput 是 cyber 拒绝、未走正常 RecordUsage 的请求记录用量的入参。
// 用量按上游真实 token 计费，与 WS cyber 及正常请求口径一致（InputTokens/OutputTokens
// 取自上游 response.failed 报告的 usage，即 mark.UpstreamInTok/OutTok）。
type CyberPolicyUsageInput struct {
	AuthorizationMode string
	APIKey            *APIKey
	Account           *Account
	Subscription      *UserSubscription
	RequestID         string
	Model             string
	Stream            bool
	InputTokens       int
	OutputTokens      int
	// 渠道归因与请求级 meta，使 cyber 计费行与正常 RecordUsage 行口径一致
	// （否则 cyber 行 channel_id 等为空，渠道维度统计会遗漏 cyber 命中）。
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	IPAddress          string
	SessionID          string
	RequestPayloadHash string
	APIKeyService      APIKeyQuotaUpdater
	NativeCompactionV2 bool
	ChannelUsageFields
}

// RecordCyberPolicyUsageLog 为被上游 cyber_policy 拒绝、未走正常 RecordUsage 的请求
// （HTTP forward 返回错误路径）记录用量并按上游真实 token 计费，使其与 WS cyber 路径、
// 与正常请求的计费口径统一（不再是 tokens=0 免费行）。token 取自上游 response.failed
// 报告的 usage（非流式直接拒通常为 0，cost 随之为 0）。复用 RecordUsage 完成成本计算、
// 扣费与用量行写入（request_type=cyber 由 CyberBlocked 置位）。仅 forward 返回错误的
// 路径由 handler 调用，避免与成功路径的正常 RecordUsage 重复。
func (s *OpenAIGatewayService) RecordCyberPolicyUsageLog(ctx context.Context, in CyberPolicyUsageInput) {
	if s == nil || in.APIKey == nil || in.APIKey.User == nil || in.Account == nil || strings.TrimSpace(in.Model) == "" {
		return
	}
	result := &OpenAIForwardResult{
		AuthorizationMode: in.AuthorizationMode,
		RequestID:         in.RequestID,
		Model:             in.Model,
		Stream:            in.Stream,
		Usage: OpenAIUsage{
			InputTokens:  in.InputTokens,
			OutputTokens: in.OutputTokens,
		},
	}
	if err := s.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result:             result,
		APIKey:             in.APIKey,
		User:               in.APIKey.User,
		Account:            in.Account,
		Subscription:       in.Subscription,
		InboundEndpoint:    in.InboundEndpoint,
		UpstreamEndpoint:   in.UpstreamEndpoint,
		UserAgent:          in.UserAgent,
		IPAddress:          in.IPAddress,
		SessionID:          in.SessionID,
		RequestPayloadHash: in.RequestPayloadHash,
		APIKeyService:      in.APIKeyService,
		ChannelUsageFields: in.ChannelUsageFields,
		CyberBlocked:       true,
		NativeCompactionV2: in.NativeCompactionV2,
	}); err != nil {
		logger.LegacyPrintf("service.openai_gateway", "cyber usage record failed: request_id=%s err=%v", in.RequestID, err)
	}
}

// ResolveUserGroupRateMultiplier resolves the same cached multiplier used by OpenAI usage billing.
func (s *OpenAIGatewayService) ResolveUserGroupRateMultiplier(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	if s == nil {
		return groupDefaultMultiplier
	}
	resolver := s.userGroupRateResolver
	if resolver == nil {
		resolver = newUserGroupRateResolver(nil, nil, resolveUserGroupRateCacheTTL(s.cfg), nil, "service.openai_gateway")
	}
	return resolver.Resolve(ctx, userID, groupID, groupDefaultMultiplier)
}

// openAIUsagePricingAt 返回本次用量记录使用的定价时刻：优先请求级 PricingAt
// （与利润门 D 同源同刻），未装配时回退记录时刻（既有行为）。
func openAIUsagePricingAt(input *OpenAIRecordUsageInput) time.Time {
	if input != nil && !input.PricingAt.IsZero() {
		return input.PricingAt
	}
	return timezone.Now()
}

func groupBillsOpenAIFastAtStandard(apiKey *APIKey, account *Account, serviceTier string) bool {
	if apiKey == nil || apiKey.Group == nil || !apiKey.Group.FreeOpenAIFast {
		return false
	}
	if account == nil || !account.IsOpenAI() {
		return false
	}
	if !groupSupportsOpenAIFast(apiKey.Group.Platform) {
		return false
	}
	return openAIFastAtStandardTier(serviceTier)
}

func openAIFastAtStandardTier(serviceTier string) bool {
	switch normalizeBillingServiceTier(serviceTier) {
	case "priority", "fast":
		return true
	default:
		return false
	}
}

// RecordUsage records usage and deducts balance
// openAIUsageRecord carries the existing calculator and command-builder inputs.
// Preparation never commits money or writes a usage row; both consumers below
// use this same result rather than recomputing token buckets or pricing policy.
type openAIUsageRecord struct {
	requestID       string
	log             *UsageLog
	params          *postUsageBillingParams
	simpleUsageOnly bool
}

func (s *OpenAIGatewayService) RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error {
	// Delayed Cloud settlement is a billing replay, not a fresh observation of
	// account health. Native roots observe their live result before finalization.
	if cloudSettlingSnapshot(ctx) == nil {
		s.observeOpenAILiveUsage(ctx, input)
	}
	record, err := s.prepareOpenAIUsage(ctx, input)
	if err != nil || record == nil {
		return err
	}
	if record.simpleUsageOnly {
		s.writeOpenAIAuthorizationUsage(ctx, record.log, input.Result, input.Account)
		logger.LegacyPrintf("service.openai_gateway", "[SIMPLE MODE] Usage recorded (not billed): user=%d, tokens=%d", record.log.UserID, record.log.TotalTokens())
		if s.deferredService != nil {
			s.deferredService.ScheduleLastUsedUpdate(input.Account.ID)
		}
		return nil
	}
	_, err = applyUsageBilling(ctx, record.requestID, record.log, record.params, s.billingDeps(), s.usageBillingRepo)
	// Delayed log-only ACK is gated by that single durable transaction.
	if record.params.UsageLogOnly {
		return err
	}
	if err != nil {
		record.log.BillingStatus = "pending"
		if record.params.Cost.PricingError != "" {
			record.log.BillingStatus = "pending_pricing"
		}
		record.log.ActualCost = 0
	}
	s.writeOpenAIAuthorizationUsage(ctx, record.log, input.Result, input.Account)
	return err
}

// observeOpenAILiveUsage belongs to the live completion boundary. Quote and
// journal recovery may run repeatedly and must never reset a newer 403 counter
// or attribute historical cache tokens to the current scheduling cohort.
func (s *OpenAIGatewayService) observeOpenAILiveUsage(ctx context.Context, input *OpenAIRecordUsageInput) {
	if s == nil || ctx == nil || input == nil || input.Result == nil || input.Result.CloudManagedBilling {
		return
	}
	RecordSchedulingCacheTokens(ctx, input.Result.Usage.InputTokens, input.Result.Usage.CacheReadInputTokens)
	if s.rateLimitService != nil && input.Account != nil && input.Account.Platform == PlatformOpenAI {
		s.rateLimitService.ResetOpenAI403Counter(ctx, input.Account.ID)
	}
}

func (s *OpenAIGatewayService) prepareOpenAIUsage(ctx context.Context, input *OpenAIRecordUsageInput) (*openAIUsageRecord, error) {
	if input == nil {
		return nil, errors.New("openai usage input is nil")
	}
	result := input.Result
	if result == nil {
		return nil, errors.New("openai usage result is nil")
	}
	if result.CloudManagedBilling {
		return nil, nil
	}
	cloudSnapshot := cloudSettlingSnapshot(ctx)
	native := input.nativePricing
	if native != nil && cloudSnapshot != nil {
		return nil, nativePreparationFailure(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict)
	}

	apiKey := input.APIKey
	user := input.User
	account := input.Account
	subscription := input.Subscription
	billingAccount := account
	var err error
	if native == nil {
		billingAccount, err = resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return nil, err
		}
	}
	if !isGrokVideoUsageResult(result, nil) {
		ApplyOpenAIImageBillingResolution(result)
	}
	if native == nil {
		logServiceTierBillingDowngrade("service.openai_gateway", account, result.RequestID, ApplyOpenAIServiceTierBillingResolution(billingAccount, result))
	} else {
		tier := resolveOpenAIServiceTierBillingPolicy(native.attempt.Billing.CredentialOAuthLike, native.attempt.ServiceTier, result.UpstreamResponseServiceTier)
		result.ServiceTier = optionalTrimmedStringPtr(tier.Billing)
		result.ReasoningEffort = optionalTrimmedStringPtr(native.attempt.ReasoningEffort)
	}

	// OpenAI input_tokens 是总输入，包含缓存读取和缓存写入明细。
	// 将三类 token 拆成互斥桶，避免缓存写入同时按普通输入和 cache_write 重复计费。
	cacheCreationTokens := result.Usage.CacheCreationInputTokens
	cacheCreationAsInput := false
	cacheWriteTTL := false
	if native == nil {
		cacheCreationAsInput = account.IsExcelBPSCacheCreationAsInputEnabled() && result.UpstreamEndpoint == "/basispoints/api/responses"
		cacheWriteTTL = account.Type == AccountTypeAPIKey && !account.UsesOpenAICodexProtocol()
	} else {
		cacheCreationAsInput = native.attempt.Billing.CacheCreationAsInput
		cacheWriteTTL = native.attempt.Billing.OpenAICacheWriteTTL
	}
	if cacheCreationAsInput {
		cacheCreationTokens = 0
	}
	actualInputTokens := result.Usage.InputTokens - result.Usage.CacheReadInputTokens - cacheCreationTokens
	if actualInputTokens < 0 {
		actualInputTokens = 0
	}

	// Calculate cost
	tokens := UsageTokens{
		OpenAICacheWriteTTL:    cacheWriteTTL,
		InputTokens:            actualInputTokens,
		ImageInputTokens:       max(result.Usage.ImageInputTokens-result.Usage.ImageCacheReadTokens, 0),
		ImageCacheReadTokens:   result.Usage.ImageCacheReadTokens,
		OutputTokens:           result.Usage.OutputTokens,
		CacheCreationTokens:    cacheCreationTokens,
		CacheCreation5mTokens:  result.Usage.CacheCreation5mTokens,
		CacheCreation1hTokens:  result.Usage.CacheCreation1hTokens,
		CacheCreation30mTokens: result.Usage.CacheCreation30mTokens,
		CacheCreation12hTokens: result.Usage.CacheCreation12hTokens,
		CacheReadTokens:        result.Usage.CacheReadInputTokens,
		ImageOutputTokens:      result.Usage.ImageOutputTokens,
	}

	if cacheCreationAsInput {
		tokens.CacheCreation5mTokens, tokens.CacheCreation1hTokens = 0, 0
		tokens.CacheCreation30mTokens, tokens.CacheCreation12hTokens = 0, 0
	}
	observed := ClaudeUsage{InputTokens: max(result.Usage.InputTokens-result.Usage.CacheReadInputTokens-result.Usage.CacheCreationInputTokens, 0), OutputTokens: result.Usage.OutputTokens, CacheReadInputTokens: result.Usage.CacheReadInputTokens, CacheCreationInputTokens: result.Usage.CacheCreationInputTokens, CacheCreation5mTokens: result.Usage.CacheCreation5mTokens, CacheCreation1hTokens: result.Usage.CacheCreation1hTokens, CacheCreation30mTokens: result.Usage.CacheCreation30mTokens, CacheCreation12hTokens: result.Usage.CacheCreation12hTokens}
	billed := observed
	billed.InputTokens, billed.CacheCreationInputTokens = actualInputTokens, cacheCreationTokens
	billed.CacheCreation5mTokens, billed.CacheCreation1hTokens = tokens.CacheCreation5mTokens, tokens.CacheCreation1hTokens
	billed.CacheCreation30mTokens, billed.CacheCreation12hTokens = tokens.CacheCreation30mTokens, tokens.CacheCreation12hTokens
	logGatewayUsageAccounting(ctx, account, apiKey, result.RequestID, input.SessionID, observed, billed, false, false)

	var multiplier, baseMultiplier, imageMultiplier, videoMultiplier float64
	var pricingAt time.Time
	if native != nil {
		r := native.request
		baseMultiplier, multiplier, imageMultiplier, videoMultiplier = r.BaseMultiplier, r.TokenMultiplier, r.ImageMultiplier, r.VideoMultiplier
		pricingAt = r.PricingAt
	} else {
		multiplier = 1.0
		if s.cfg != nil {
			multiplier = s.cfg.Default.RateMultiplier
		}
		if cloudSnapshot != nil {
			multiplier = cloudSnapshot.BaseMultiplier
		} else if apiKey.GroupID != nil && apiKey.Group != nil {
			multiplier = s.ResolveUserGroupRateMultiplier(ctx, user.ID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
		}
		baseMultiplier = multiplier
		pricingAt = openAIUsagePricingAt(input)
		multiplier, imageMultiplier = computePeakAwareMultipliers(apiKey, baseMultiplier, pricingAt)
		if cloudSnapshot != nil {
			multiplier = cloudSnapshot.TokenMultiplier
		}
		videoMultiplier = resolveVideoRateMultiplier(apiKey, baseMultiplier)
	}

	var cost *CostBreakdown
	billingModel := forwardResultBillingModel(result.Model, result.UpstreamModel)
	if result.BillingModel != "" {
		billingModel = strings.TrimSpace(result.BillingModel)
	}
	if input.BillingModelSource == BillingModelSourceChannelMapped && input.ChannelMappedModel != "" && input.ChannelMappedModel != input.OriginalModel {
		billingModel = input.ChannelMappedModel
	}
	if input.BillingModelSource == BillingModelSourceRequested && input.OriginalModel != "" {
		billingModel = input.OriginalModel
	}
	billingModels := usageBillingModelCandidates(
		billingModel,
		result.BillingModel,
		input.ChannelMappedModel,
		input.OriginalModel,
		result.UpstreamModel,
		result.Model,
	)
	if native != nil {
		billingModels, err = native.billingModels(result)
		if err != nil {
			return nil, err
		}
		billingModel = firstUsageBillingModel(billingModels)
	}
	pricingCandidates := append([]string(nil), billingModels...)
	if native == nil {
		billingModels = s.filterCNProviderBillingModelCandidates(ctx, account, apiKey, billingModels)
	}
	serviceTier := ""
	if result.ServiceTier != nil {
		serviceTier = strings.TrimSpace(*result.ServiceTier)
	}
	var longContextBillingGate *bool
	var modelLongContextExempt, freeFast bool
	if native == nil {
		longContextBillingGate = openAILongContextBillingGate(billingAccount)
		modelLongContextExempt = openAISubscriptionLongContextExempt(billingAccount, result)
		freeFast = groupBillsOpenAIFastAtStandard(apiKey, billingAccount, serviceTier)
	} else {
		longContextBillingGate, modelLongContextExempt = native.attempt.LongContextEnabled, native.attempt.ModelLongContextExempt
		freeFast, err = native.freeFast(serviceTier)
		if err != nil {
			return nil, err
		}
	}
	cost, err = s.calculateOpenAIRecordUsageCost(
		ctx,
		result,
		apiKey,
		billingModels,
		multiplier,
		imageMultiplier,
		videoMultiplier,
		baseMultiplier,
		tokens,
		serviceTier,
		longContextBillingGate,
		modelLongContextExempt,
		pricingAt, native,
	)
	if err != nil && (cloudSnapshot != nil || native != nil) {
		return nil, err
	}
	if err != nil {
		logger.L().With(
			zap.String("component", "service.openai_gateway"),
			zap.Strings("billing_models", billingModels),
			zap.String("requested_model", input.OriginalModel),
			zap.String("mapped_model", input.ChannelMappedModel),
			zap.String("upstream_model", result.UpstreamModel),
			zap.Int64("api_key_id", apiKey.ID),
			zap.Int64("account_id", account.ID),
		).Warn("openai_usage.pricing_pending", zap.Error(err))
		cost = &CostBreakdown{BillingMode: string(BillingModeToken), PricingError: err.Error()}
	}
	// response_model：按上游成功响应自报的模型计费（渠道显式开启才生效）。
	// 采纳条件见 responseModelBillingDeclaration + hasIdentifiedOpenAIResponsePricing
	// + responseModelBillingAdoptable。任一条件不满足都静默回落基线，即开启本模式前的
	// 既有行为。响应模型与基线同名时直接跳过：重算必然同价，白跑一次定价解析。
	baselineBillingModel := firstUsageBillingModel(billingModels)
	if native != nil {
		billingModels, cost, err = native.selectResponsePricing(ctx, s, result, apiKey, tokens, serviceTier, billingModels, cost)
		if err != nil {
			return nil, err
		}
	} else if responseModel := responseModelBillingDeclaration(
		input.BillingModelSource,
		result.UpstreamResponseModel,
		result.UpstreamResponseModelConflict,
		result.ImageCount > 0 || result.VideoCount > 0 || result.WebSearchCalls > 0 ||
			result.AudioUsage != nil || result.SearchCount > 0,
	); responseModel != "" && !strings.EqualFold(responseModel, baselineBillingModel) {
		if identified, responseChannelPriced := s.hasIdentifiedOpenAIResponsePricing(ctx, responseModel, apiKey); identified {
			responseModels := s.filterCNProviderBillingModelCandidates(ctx, account, apiKey, usageBillingModelCandidates(responseModel))
			responseCost, responseErr := s.calculateOpenAIRecordUsageCost(
				ctx, result, apiKey, responseModels, multiplier, imageMultiplier,
				videoMultiplier, baseMultiplier, tokens, serviceTier, longContextBillingGate, modelLongContextExempt, pricingAt, native,
			)
			// 基线定价源以 baselineBillingModel 为准：它正是 calculateOpenAIRecordUsageCost
			// 内部做渠道定价判断时使用的模型，且"首候选有渠道价"必然意味着首候选就是实际
			// 定价基准（有渠道价就一定能算出价，循环不会落到后续候选）。
			_, baselineChannelPriced := s.hasIdentifiedOpenAIResponsePricing(ctx, baselineBillingModel, apiKey)
			if responseErr == nil && responseModelBillingAdoptable(cost, responseCost, baselineChannelPriced, responseChannelPriced) {
				logResponseModelBillingApplied("service.openai_gateway", account, result.RequestID,
					baselineBillingModel, responseModel, cost, responseCost)
				billingModels = responseModels
				cost = responseCost
			}
		}
	}

	// Free Fast changes only the customer charge. Keep priority TotalCost and
	// service_tier for upstream accounting, but evaluate ActualCost once more at
	// the Standard tier using the same channel, peak, and long-context policy.
	if freeFast {
		standardCost, standardErr := s.calculateOpenAIRecordUsageCost(
			ctx,
			result,
			apiKey,
			billingModels,
			multiplier,
			imageMultiplier,
			videoMultiplier,
			baseMultiplier,
			tokens,
			"",
			longContextBillingGate,
			modelLongContextExempt,
			pricingAt, native,
		)
		if standardErr != nil {
			if native != nil {
				return nil, standardErr
			}
			cost.PricingError = standardErr.Error()
		}
		// Missing pricing already fell back to a zero-cost log above; keep that
		// usage row instead of dropping it on the Standard re-evaluation.
		if standardErr == nil && cost != nil && standardCost != nil {
			cost.ActualCost = standardCost.ActualCost
			if cost.SamplingDetails != nil && standardCost.SamplingDetails != nil && len(cost.SamplingDetails.Samples) == len(standardCost.SamplingDetails.Samples) {
				cost.SamplingDetails.HostedActualCost = standardCost.SamplingDetails.HostedActualCost
				for i := range cost.SamplingDetails.Samples {
					cost.SamplingDetails.Samples[i].ActualCost = standardCost.SamplingDetails.Samples[i].ActualCost
				}
			}
		}
	}

	// Preserve the real normalization/quote inputs for both known and pending
	// prices. The Native journal consumes this same mapping, never a new formula.
	if native != nil {
		pricingCandidates = append([]string(nil), billingModels...)
	}
	cost.PricingRequest = openAIPricingRequest(result, apiKey, pricingCandidates, tokens, multiplier, imageMultiplier, videoMultiplier, baseMultiplier, serviceTier, longContextBillingGate, modelLongContextExempt, freeFast, pricingAt)
	if cost.PricingRequest != nil && apiKey.Group != nil {
		cost.PricingRequest.GroupLongContextEnabled = apiKey.Group.LongContextPricingEnabled
	}
	if cost.PricingRequest != nil {
		cost.PricingRequest.AccountPlatform = account.Platform
	}
	// Determine billing type
	isSubscriptionBilling := subscription != nil && apiKey.Group != nil && apiKey.Group.IsSubscriptionType()
	if native != nil {
		isSubscriptionBilling = native.request.Billing.IsSubscriptionBill
	}
	billingType := BillingTypeBalance
	if isSubscriptionBilling {
		billingType = BillingTypeSubscription
	}

	// Create usage log
	durationMs := int(result.Duration.Milliseconds())
	accountRateMultiplier := account.BillingRateMultiplier()
	if native != nil {
		accountRateMultiplier = native.attempt.FrozenPricing.Policy.AccountRateMultiplier
	}
	var requestID string
	if native != nil {
		requestID = native.identity.RequestID
	} else {
		requestID = resolveUsageBillingRequestID(ctx, result.RequestID)
		if cloudSnapshot != nil {
			requestID = result.RequestID
		}
		// One HTTP BPS request can settle compaction before a safe account failover.
		// As with separate WS turns, distinct metered attempts need distinct billing
		// identities; the connection/client correlation ID must not deduplicate them.
		if result.OpenAIWSMode || result.UpstreamEndpoint == "/basispoints/api/responses" {
			if upstreamRequestID := strings.TrimSpace(result.RequestID); upstreamRequestID != "" {
				requestID = upstreamRequestID
			}
		}
		// Async Grok video: always use the stable task id for dedup (status + content polls
		// share one bill). Context-local client/local IDs would otherwise create a new row
		// per poll if Redis claim is lost.
		if result.VideoCount > 0 {
			if stable := StableGrokVideoBillingRequestID(firstNonEmpty(
				strings.TrimPrefix(strings.TrimSpace(result.RequestID), "grok-video:"),
				strings.TrimSpace(result.ResponseID),
				strings.TrimPrefix(strings.TrimSpace(requestID), "grok-video:"),
			)); stable != "" {
				requestID = stable
			}
		}

	}

	// 确定 RequestedModel（渠道映射前的原始模型）
	requestedModel := result.Model
	if input.OriginalModel != "" {
		requestedModel = input.OriginalModel
	}
	sentModel := upstreamSentModel(result.Model, result.UpstreamModel)
	if result.UpstreamResponseModelConflict {
		logger.L().Warn("upstream_response_model_conflict",
			zap.String("platform", account.Platform),
			zap.Int64("account_id", account.ID),
			zap.String("request_id", requestID),
			zap.String("sent_model", sentModel),
			zap.String("selected_response_model", strings.TrimSpace(result.UpstreamResponseModel)),
		)
	}

	imageSizeBreakdown := cloneImageSizeBreakdown(result.ImageSizeBreakdown)
	if result.Usage.ImageCacheReadTokens > 0 {
		if imageSizeBreakdown == nil {
			imageSizeBreakdown = make(map[string]int)
		}
		// Keep the image cache split in the existing usage_logs JSONB payload.
		imageSizeBreakdown["image_cache_read_tokens"] = result.Usage.ImageCacheReadTokens
	}
	usageLog := &UsageLog{
		AuthorizationMode:        result.AuthorizationMode,
		UserID:                   user.ID,
		APIKeyID:                 apiKey.ID,
		AccountID:                account.ID,
		RequestID:                requestID,
		UpstreamRequestID:        usageUpstreamRequestIDPtr(account, result.UpstreamHeaders, result.OpenAIWSMode),
		Model:                    result.Model,
		RequestedModel:           requestedModel,
		UpstreamModel:            optionalTrimmedStringPtr(result.UpstreamModel),
		UpstreamResponseModel:    optionalTrimmedStringPtr(result.UpstreamResponseModel),
		UpstreamModelMismatch:    upstreamModelMismatch(sentModel, result.UpstreamResponseModel),
		ServiceTier:              result.ServiceTier,
		ReasoningEffort:          result.ReasoningEffort,
		RequestedReasoningEffort: coalesceRequestedReasoningEffort(result.RequestedReasoningEffort, result.ReasoningEffort),
		InboundEndpoint:          optionalTrimmedStringPtr(input.InboundEndpoint),
		UpstreamEndpoint:         optionalTrimmedStringPtr(input.UpstreamEndpoint),
		InputTokens:              actualInputTokens,
		OutputTokens:             result.Usage.OutputTokens,
		CacheCreationTokens:      cacheCreationTokens,
		CacheCreation5mTokens:    tokens.CacheCreation5mTokens,
		CacheCreation1hTokens:    tokens.CacheCreation1hTokens,
		CacheCreation30mTokens:   tokens.CacheCreation30mTokens,
		CacheCreation12hTokens:   tokens.CacheCreation12hTokens,
		CacheReadTokens:          result.Usage.CacheReadInputTokens,
		ImageInputTokens:         result.Usage.ImageInputTokens,
		ImageOutputTokens:        result.Usage.ImageOutputTokens,
		ImageCount:               result.ImageCount,
		ImageSize:                optionalTrimmedStringPtr(result.ImageSize),
		ImageInputSize:           optionalTrimmedStringPtr(result.ImageInputSize),
		ImageOutputSize:          optionalTrimmedStringPtr(result.ImageOutputSize),
		ImageSizeSource:          optionalTrimmedStringPtr(result.ImageSizeSource),
		ImageSizeBreakdown:       imageSizeBreakdown,
		NativeCompactionV2:       input.NativeCompactionV2,
	}
	if native == nil {
		usageLog.CaptureVendorSnapshot(account, apiKey.Group)
	} else {
		p := native.attempt.Billing
		usageLog.VendorIDSnapshot = copyPricingValue(p.VendorIDSnapshot)
		usageLog.AccountNameSnapshot, usageLog.GroupNameSnapshot, usageLog.PlatformSnapshot = copyPricingValue(p.AccountNameSnapshot), copyPricingValue(p.GroupNameSnapshot), copyPricingValue(p.PlatformSnapshot)
	}
	isVideoUsage := isGrokVideoUsageResult(result, billingModels)
	if isVideoUsage {
		usageLog.VideoCount = result.VideoCount
		usageLog.VideoResolution = optionalTrimmedStringPtr(NormalizeVideoBillingResolutionOrDefault(result.VideoResolution))
		videoDurationSeconds := NormalizeVideoBillingDurationSecondsOrDefault(result.VideoDurationSeconds)
		usageLog.VideoDurationSeconds = &videoDurationSeconds
	}
	if cost != nil {
		usageLog.InputCost = cost.InputCost
		usageLog.ImageInputCost = cost.ImageInputCost
		usageLog.OutputCost = cost.OutputCost
		usageLog.ImageOutputCost = cost.ImageOutputCost
		usageLog.CacheCreationCost = cost.CacheCreationCost
		usageLog.CacheReadCost = cost.CacheReadCost
		usageLog.TotalCost = cost.TotalCost
		usageLog.ActualCost = cost.ActualCost
		usageLog.LongContextBillingApplied = cost.LongContextBillingApplied
	}
	if isVideoUsage && (cost == nil || cost.BillingMode != string(BillingModeToken)) {
		usageLog.RateMultiplier = videoMultiplier
	} else if result.ImageCount > 0 && (cost == nil || cost.BillingMode != string(BillingModeToken)) {
		usageLog.RateMultiplier = imageMultiplier
	} else {
		usageLog.RateMultiplier = multiplier
	}
	usageLog.AccountRateMultiplier = &accountRateMultiplier
	usageLog.BillingType = billingType
	usageLog.Stream = result.Stream
	if input.CyberBlocked {
		usageLog.RequestType = RequestTypeCyberBlocked
	}
	usageLog.OpenAIWSMode = result.OpenAIWSMode
	usageLog.UsageLowerBound = result.UsageLowerBound
	usageLog.DurationMs = &durationMs
	usageLog.FirstTokenMs = result.FirstTokenMs
	usageLog.LatencyMetrics = result.LatencyMetrics
	if cost != nil {
		usageLog.SamplingDetails = cost.SamplingDetails
	}
	usageLog.CreatedAt = time.Now()
	// 设置渠道信息
	usageLog.ChannelID = optionalInt64Ptr(input.ChannelID)
	usageLog.ModelMappingChain = optionalTrimmedStringPtr(input.ModelMappingChain)
	// 设置计费模式
	if cost != nil && cost.BillingMode != "" {
		billingMode := cost.BillingMode
		usageLog.BillingMode = &billingMode
	} else if isVideoUsage {
		billingMode := string(BillingModeVideo)
		usageLog.BillingMode = &billingMode
	} else if result.ImageCount > 0 {
		billingMode := string(BillingModeImage)
		usageLog.BillingMode = &billingMode
	} else {
		billingMode := string(BillingModeToken)
		usageLog.BillingMode = &billingMode
	}
	// 添加 UserAgent
	if input.UserAgent != "" {
		usageLog.UserAgent = &input.UserAgent
	}

	// 添加 IPAddress
	if input.IPAddress != "" {
		usageLog.IPAddress = &input.IPAddress
	}

	// 添加 SessionID（客户端显式会话标识；缺失/无效时保持 nil）
	usageLog.SessionID = optionalTrimmedStringPtr(input.SessionID)

	if apiKey.GroupID != nil {
		usageLog.GroupID = apiKey.GroupID
	}
	if subscription != nil {
		usageLog.SubscriptionID = &subscription.ID
	}

	// 计算账号统计定价费用（使用最终上游模型匹配自定义规则）
	if native != nil {
		usageLog.AccountStatsCost, err = native.statsCost(ctx, s.billingService, result, tokens, cost.TotalCost, serviceTier)
		if err != nil {
			return nil, err
		}
	} else if cloudSnapshot != nil {
		usageLog.AccountStatsCost, err = s.cloudFrozenStatsCost(ctx, cloudSnapshot, result, tokens, cost.TotalCost)
		if err != nil {
			return nil, err
		}
	} else if apiKey.GroupID != nil {
		applyAccountStatsCost(ctx, usageLog, s.channelService, s.billingService,
			account.ID, *apiKey.GroupID, result.UpstreamModel, result.Model,
			tokens, cost.TotalCost, pricingAt,
			accountStatsLongContextPricingEnabled(longContextBillingGate) && !modelLongContextExempt,
		)
	}

	var billingMode UsageBillingMode
	if native != nil {
		billingMode = native.request.Mode
	} else {
		billingMode = resolveUsageBillingMode(s.cfg, apiKey)
	}
	if cloudSnapshot != nil {
		var valid bool
		billingMode, valid = cloudSnapshot.billingMode()
		if !valid {
			return nil, errors.New("Cloud billing policy is invalid")
		}
	}
	simpleModeKeyRateLimitOnly := billingMode == UsageBillingModeKeyWindowOnly
	simpleUsageOnly := cloudSnapshot == nil && billingMode == UsageBillingModeLogOnly

	// Async usage billing runs outside the original request context, so it
	// cannot recover ForcePlatform there. Fall back for internal/test callers.
	quotaPlatform := input.QuotaPlatform
	if quotaPlatform == "" && native == nil {
		quotaPlatform = PlatformFromAPIKey(apiKey)
	}

	payloadHash := input.RequestPayloadHash
	var funding *nativeIntentFrozenFunding
	if native != nil {
		funding = native.funding()
	} else {
		payloadHash = resolveUsageBillingPayloadFingerprint(ctx, payloadHash)
	}
	return &openAIUsageRecord{requestID: requestID, log: usageLog, simpleUsageOnly: simpleUsageOnly, params: &postUsageBillingParams{
		FrozenFunding: funding,
		UsageLogs:     splitOpenAIUsageAuthorization(usageLog, result, account),
		BillingModel:  billingModel, PricingAt: pricingAt,
		Cost:                       cost,
		User:                       user,
		APIKey:                     apiKey,
		Account:                    account,
		Subscription:               subscription,
		RequestPayloadHash:         payloadHash,
		IsSubscriptionBill:         isSubscriptionBilling && !simpleModeKeyRateLimitOnly,
		AccountRateMultiplier:      accountRateMultiplier,
		APIKeyService:              input.APIKeyService,
		Platform:                   quotaPlatform,
		SimpleModeKeyRateLimitOnly: simpleModeKeyRateLimitOnly,
		UsageLogOnly:               billingMode == UsageBillingModeLogOnly,
	}}, nil
}

// hasIdentifiedOpenAIResponsePricing 判断上游自报的响应模型是否可以作为计费基准，
// 并回传它是否解析到了渠道级定价（供 responseModelBillingAdoptable 的跨定价源守卫使用，
// 避免为此再解析一次）。
// 只接受管理员为该模型显式配置的渠道定价，或价格表中能被确定性识别的条目；
// 刻意不接受按子串猜出来的系列兜底价，否则上游随便编一个含 "haiku" 的名字就能把
// 计费拉到最便宜的系列价上。详见 responseModelBillingDeclaration。
func (s *OpenAIGatewayService) hasIdentifiedOpenAIResponsePricing(ctx context.Context, model string, apiKey *APIKey) (identified bool, channelPriced bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return false, false
	}
	if snapshot := cloudSettlingSnapshot(ctx); snapshot != nil {
		// Never consult current prices or invent a response model for a delayed
		// bill. V1 retains its prior policy; v2 requires frozen price evidence.
		price, exists := snapshot.Prices[model]
		if snapshot.Version != 2 || !exists {
			return false, false
		}
		channelPriced := price.Resolved.Source == PricingSourceGroup || price.Resolved.Source == PricingSourceChannel
		return price.ResponsePriceIdentified || channelPriced, channelPriced
	}
	if s.resolveOpenAIChannelPricing(ctx, model, apiKey) != nil {
		return true, true
	}
	return s.billingService.HasIdentifiedTokenPricing(model), false
}

// openAILongContextBillingGate returns the per-account long-context opt-in.
// The flag is an OpenAI-only account setting, so other platforms (Grok) return
// nil — "no per-account gate" — and are governed by the group toggle alone.
// Returning a hardcoded false for them would veto the official model ladders
// (e.g. the Grok >=200k 2x card) that no account setting can ever re-enable.
func openAILongContextBillingGate(account *Account) *bool {
	if account == nil || !account.IsOpenAI() {
		return nil
	}
	enabled := account.IsOpenAILongContextBillingEnabled()
	return &enabled
}

// openAISubscriptionLongContextExempt 按实际凭据和最终发送模型判断订阅豁免。
// GPT-6.1 Sol 的订阅请求不再收取模型长上下文附加费；API Key 仍使用 API 价卡。
// 使用发送模型，避免客户别名、渠道计费模型或响应模型计费切换改变上游订阅规则。
func openAISubscriptionLongContextExempt(account *Account, result *OpenAIForwardResult) bool {
	if !account.IsOpenAIOAuthLike() || result == nil {
		return false
	}
	model := strings.TrimSpace(result.UpstreamModel)
	if model == "" {
		model = result.Model
	}
	return openai.IsGPT61SolModelSpelling(model)
}

func (s *OpenAIGatewayService) calculateOpenAIRecordUsageCost(
	ctx context.Context,
	result *OpenAIForwardResult,
	apiKey *APIKey,
	billingModels []string,
	multiplier float64,
	imageMultiplier float64,
	videoMultiplier float64,
	webSearchMultiplier float64,
	tokens UsageTokens,
	serviceTier string,
	longContextBillingGate *bool,
	modelLongContextExempt bool,
	pricingAt time.Time,
	frozen ...*nativeIntentSettlementScope,
) (*CostBreakdown, error) {
	native := nativeIntentPricingArgument(frozen)
	if snapshot := cloudSettlingSnapshot(ctx); snapshot != nil && native == nil {
		return s.cloudFrozenCost(ctx, snapshot, result, billingModels, tokens, multiplier, serviceTier, modelLongContextExempt)
	}
	billingModel := firstUsageBillingModel(billingModels)
	if native != nil {
		if err := native.validateHosted(result, billingModels); err != nil {
			return nil, err
		}
		native.quotedModel = billingModel
	}
	channelPrice := func(name string) *ResolvedPricing {
		if native != nil {
			return native.channelPrice(name)
		}
		return s.resolveOpenAIChannelPricing(ctx, name, apiKey)
	}
	if result != nil && result.WebSearchCalls > 0 {
		// Codex alpha/search 网页搜索按次计费：上游不返回 usage/token 字段，单价只取
		// 分组覆盖价（nil 时默认 0.01 = 官方 $10/1000 次），不参与渠道级模型定价。
		// 倍率与 image/video 按次口径一致：使用不含高峰因子的基础倍率
		//（用户专属 > 分组 rate_multiplier > 系统默认），与分组表单的价格预览承诺一致。
		price := webSearchPricePerCallFromAPIKey(apiKey)
		if native != nil {
			price = native.request.Billing.Hosted.WebSearchPerCall
		}
		return s.billingService.CalculateWebSearchCost(result.WebSearchCalls, price, webSearchMultiplier), nil
	}
	if isGrokVideoUsageResult(result, billingModels) {
		if resolved := channelPrice(billingModel); resolved == nil || resolved.Mode != BillingModeToken {
			return s.calculateOpenAIVideoCost(ctx, billingModel, apiKey, result, videoMultiplier, native)
		}
	}
	if result != nil && result.AudioUsage != nil {
		if resolved := channelPrice(billingModel); resolved != nil &&
			(resolved.Mode == BillingModePerRequest) {
			gid := apiKey.Group.ID
			return s.billingService.CalculateCostUnified(CostInput{
				Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
				UsageUnits: result.AudioUsage.DurationOrUnits, SizeTier: result.AudioUsage.Mode,
				RateMultiplier: webSearchMultiplier, Resolver: s.resolver, Resolved: resolved,
				ReasoningEffort: optionalStringValue(result.ReasoningEffort), PricingAt: nativeIntentPricingAt(native),
			})
		}
		cfg := groupAudioPriceConfigFromAPIKey(apiKey)
		if native != nil {
			h := native.request.Billing.Hosted
			cfg = &audioPriceConfig{RealtimePerMin: h.AudioRealtimePerMin, TTSPerMChars: h.AudioTTSPerMChars, STTPerHour: h.AudioSTTPerHour}
		}
		return s.billingService.CalculateAudioCost(result.AudioUsage.Mode, result.AudioUsage.DurationOrUnits, cfg, webSearchMultiplier), nil
	}

	if result != nil && result.ImageCount > 0 && (native == nil || result.ImageEndpoint) {
		// 渠道定价为 token 计费时走 token 路径，否则走图片计费
		if imageCountBillingApplies(apiKey, result.ImageEndpoint) {
			return s.calculateOpenAIImageCost(ctx, billingModel, apiKey, result, imageMultiplier, native)
		}
		// An absent group/channel card does not make mixed output a dedicated
		// image request. Let the token path resolve catalog prices (or report
		// missing pricing), while retaining explicit non-token model policies.
		resolved := channelPrice(billingModel)
		if (resolved == nil && result.ImageEndpoint) || (resolved != nil && resolved.Mode != BillingModeToken) {
			return s.calculateOpenAIImageCost(ctx, billingModel, apiKey, result, imageMultiplier, native)
		}
	}

	// Token path (optional search surcharge is additive — never replaces token cost).
	var tokenCost *CostBreakdown
	var lastErr error
	if len(billingModels) > 0 && billingModel != "" {
		for _, candidate := range billingModels {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			cost, err := s.calculateOpenAIRecordUsageTokenCost(
				ctx,
				apiKey,
				candidate,
				multiplier,
				pricingAt,
				tokens,
				serviceTier,
				optionalStringValue(result.ReasoningEffort),
				longContextBillingGate,
				modelLongContextExempt, native,
			)
			if err == nil {
				if native != nil {
					native.quotedModel = candidate
				}
				tokenCost = cost
				break
			}
			lastErr = err
		}
	}
	// Search surcharge is additive. Never let a zero/default search cost mask a
	// real token-pricing failure for requests that attempted token billing.
	searchCost := (*CostBreakdown)(nil)
	if result != nil && result.SearchCount > 0 {
		price := groupSearchPricePer1kFromAPIKey(apiKey)
		if native != nil {
			price = native.request.Billing.Hosted.SearchPer1k
		}
		if price != nil && *price == 0 {
			logger.L().Info("openai_usage.search_price_per_1k_explicit_free",
				zap.Int("search_count", result.SearchCount),
				zap.String("model", billingModel),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
			)
		}
		searchCost = s.billingService.CalculateSearchCost(result.SearchCount, price, webSearchMultiplier)
	}

	tokenBillingAttempted := len(billingModels) > 0 && billingModel != ""
	if tokenCost == nil {
		if tokenBillingAttempted {
			if lastErr == nil {
				lastErr = fmt.Errorf("%w: no non-empty billing model candidates", ErrModelPricingUnavailable)
			}
			return nil, fmt.Errorf("calculate OpenAI usage cost failed for billing models %s: %w", strings.Join(billingModels, ","), lastErr)
		}
		// Search-only (no model / pure tool path): allow search billing alone.
		if searchCost != nil {
			return searchCost, nil
		}
		// 空候选按「无价可循」处理并携带 ErrModelPricingUnavailable：上层据此走
		// 零成本+告警落账，而不是丢弃整条 usage 记录。CN 账号的 claude-* 候选被
		// filterCNProviderBillingModelCandidates 全数过滤后即落到这里。
		if lastErr == nil {
			lastErr = fmt.Errorf("%w: openai usage billing model is empty", ErrModelPricingUnavailable)
		}
		return nil, fmt.Errorf("calculate OpenAI usage cost failed for billing models %s: %w", strings.Join(billingModels, ","), lastErr)
	}
	if searchCost == nil || (searchCost.TotalCost == 0 && searchCost.ActualCost == 0) {
		return tokenCost, nil
	}
	// Additive: tokens + search surcharge.
	tokenCost.TotalCost += searchCost.TotalCost
	tokenCost.ActualCost += searchCost.ActualCost
	return tokenCost, nil
}

func isGrokVideoBillingModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "grok-imagine-video")
}

func isGrokVideoUsageResult(result *OpenAIForwardResult, billingModels []string) bool {
	if result == nil || result.VideoCount <= 0 {
		return false
	}
	// VideoCount alone is authoritative for async video completion billing.
	// Prefer model-family match when present; never drop video mode on rename/mapping.
	candidates := append([]string{}, billingModels...)
	candidates = append(candidates, result.BillingModel, result.Model, result.UpstreamModel)
	for _, candidate := range candidates {
		if isGrokVideoBillingModel(candidate) {
			return true
		}
	}
	return true
}

func (s *OpenAIGatewayService) calculateOpenAIRecordUsageTokenCost(
	ctx context.Context,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	pricingAt time.Time,
	tokens UsageTokens,
	serviceTier string,
	reasoningEffort string,
	longContextBillingGate *bool,
	modelLongContextExempt bool,
	frozen ...*nativeIntentSettlementScope,
) (*CostBreakdown, error) {
	if native := nativeIntentPricingArgument(frozen); native != nil {
		price, err := native.price(billingModel)
		if err != nil {
			return nil, err
		}
		return s.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: billingModel,
			Tokens: tokens, RequestCount: 1, RateMultiplier: multiplier, PricingAt: pricingAt,
			ServiceTier: serviceTier, ReasoningEffort: reasoningEffort, Resolved: price,
			LongContextBillingEnabled: longContextBillingGate, DisableModelLongContextPricing: modelLongContextExempt})
	}

	if s.resolver != nil && apiKey.Group != nil {
		gid := apiKey.Group.ID
		return s.billingService.CalculateCostUnified(CostInput{
			Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
			Tokens: tokens, RequestCount: 1, RateMultiplier: multiplier, PricingAt: pricingAt,
			ServiceTier: serviceTier, ReasoningEffort: reasoningEffort, Resolver: s.resolver,
			LongContextBillingEnabled:      longContextBillingGate,
			DisableModelLongContextPricing: modelLongContextExempt,
		})
	}
	cost, err := s.billingService.calculateCostWithServiceTierPolicy(
		billingModel,
		tokens,
		multiplier,
		serviceTier,
		(longContextBillingGate == nil || *longContextBillingGate) && !modelLongContextExempt,
	)
	if err == nil && cost != nil {
		// The catalog calculator does not label the mode; retain the token decision
		// for usage projection and recovery even when the output has images.
		cost.BillingMode = string(BillingModeToken)
	}
	return cost, err
}

func (s *OpenAIGatewayService) calculateOpenAIImageCost(
	ctx context.Context,
	billingModel string,
	apiKey *APIKey,
	result *OpenAIForwardResult,
	multiplier float64,
	frozen ...*nativeIntentSettlementScope,
) (*CostBreakdown, error) {
	native := nativeIntentPricingArgument(frozen)
	sizeTier := NormalizeImageBillingTierOrDefault(result.ImageSize)
	var resolved *ResolvedPricing
	if native != nil {
		resolved = native.channelPrice(billingModel)
	} else {
		resolved = s.resolveOpenAIChannelPricing(ctx, billingModel, apiKey)
	}
	if resolved != nil && resolved.Source == PricingSourceGroup &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage) {
		gid := apiKey.Group.ID
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
			RequestCount: result.ImageCount, SizeTier: sizeTier,
			RateMultiplier: multiplier, Resolver: s.resolver, Resolved: resolved,
			ReasoningEffort: optionalStringValue(result.ReasoningEffort), PricingAt: nativeIntentPricingAt(native),
		})
		if err == nil {
			return cost, nil
		}
		if native != nil {
			return nil, err
		}
	}
	groupConfig := imagePriceConfigFromAPIKey(apiKey)
	configured := apiKeyHasConfiguredImagePrice(apiKey, sizeTier)
	if native != nil {
		var err error
		groupConfig, configured, err = native.imagePrices(billingModel, sizeTier)
		if err != nil {
			return nil, err
		}
	}
	if configured {
		return s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier), nil
	}
	if native == nil {
		if refreshed := s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey); refreshed != apiKey {
			apiKey = refreshed
			groupConfig = imagePriceConfigFromAPIKey(apiKey)
			if apiKeyHasConfiguredImagePrice(apiKey, sizeTier) {
				return s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier), nil
			}
		}
	}
	if resolved != nil && resolved.Source == PricingSourceChannel &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage) {
		gid := apiKey.Group.ID
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:             ctx,
			Model:           billingModel,
			GroupID:         &gid,
			Group:           apiKey.Group,
			RequestCount:    result.ImageCount,
			SizeTier:        sizeTier,
			ReasoningEffort: optionalStringValue(result.ReasoningEffort),
			RateMultiplier:  multiplier,
			PricingAt:       nativeIntentPricingAt(native),
			Resolver:        s.resolver,
			Resolved:        resolved,
		})
		if err == nil {
			return cost, nil
		}
		if native != nil {
			return nil, err
		}
		logger.LegacyPrintf("service.openai_gateway", "Calculate image channel cost failed: %v", err)
	}

	return s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier), nil
}

func (s *OpenAIGatewayService) calculateOpenAIVideoCost(
	ctx context.Context,
	billingModel string,
	apiKey *APIKey,
	result *OpenAIForwardResult,
	multiplier float64,
	frozen ...*nativeIntentSettlementScope,
) (*CostBreakdown, error) {
	native := nativeIntentPricingArgument(frozen)
	videoCount := result.VideoCount
	if videoCount <= 0 {
		videoCount = 1
	}
	resolution := NormalizeVideoBillingResolutionOrDefault(result.VideoResolution)
	durationSeconds := NormalizeVideoBillingDurationSecondsOrDefault(result.VideoDurationSeconds)
	var resolved *ResolvedPricing
	if native != nil {
		resolved = native.channelPrice(billingModel)
	} else {
		resolved = s.resolveOpenAIChannelPricing(ctx, billingModel, apiKey)
	}
	if resolved != nil && resolved.Source == PricingSourceGroup && resolved.Mode == BillingModeVideo {
		gid := apiKey.Group.ID
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
			UsageUnits: float64(videoCount * durationSeconds), SizeTier: resolution,
			RateMultiplier: multiplier, Resolver: s.resolver, Resolved: resolved,
			ReasoningEffort: optionalStringValue(result.ReasoningEffort), PricingAt: nativeIntentPricingAt(native),
		})
		if err == nil {
			return cost, nil
		}
		if native != nil {
			return nil, err
		}
	}
	groupConfig := videoPriceConfigFromAPIKey(apiKey)
	configured := apiKeyHasConfiguredVideoPrice(apiKey, billingModel, resolution)
	if native != nil {
		var err error
		groupConfig, configured, err = native.videoPrices(billingModel, resolution)
		if err != nil {
			return nil, err
		}
	}
	if configured {
		return s.billingService.CalculateVideoCost(billingModel, resolution, videoCount, durationSeconds, groupConfig, multiplier), nil
	}
	if native == nil {
		if refreshed := s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey); refreshed != apiKey {
			apiKey = refreshed
			groupConfig = videoPriceConfigFromAPIKey(apiKey)
			if apiKeyHasConfiguredVideoPrice(apiKey, billingModel, resolution) {
				return s.billingService.CalculateVideoCost(billingModel, resolution, videoCount, durationSeconds, groupConfig, multiplier), nil
			}
		}
	}
	if resolved != nil && resolved.Source == PricingSourceChannel &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage || resolved.Mode == BillingModeVideo) {
		// 渠道 per_request/image 定价保持"按请求次数"口径（价格由管理员按次配置），不乘视频时长。
		gid := apiKey.Group.ID
		units := float64(videoCount)
		if resolved.Mode == BillingModeVideo {
			units = float64(videoCount * durationSeconds)
		}
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:             ctx,
			Model:           billingModel,
			GroupID:         &gid,
			Group:           apiKey.Group,
			RequestCount:    videoCount,
			UsageUnits:      units,
			SizeTier:        resolution,
			ReasoningEffort: optionalStringValue(result.ReasoningEffort),
			RateMultiplier:  multiplier,
			PricingAt:       nativeIntentPricingAt(native),
			Resolver:        s.resolver,
			Resolved:        resolved,
		})
		if err == nil {
			cost.BillingMode = string(BillingModeVideo)
			return cost, nil
		}
		if native != nil {
			return nil, err
		}
		logger.LegacyPrintf("service.openai_gateway", "Calculate video channel cost failed: %v", err)
	}

	return s.billingService.CalculateVideoCost(billingModel, resolution, videoCount, durationSeconds, groupConfig, multiplier), nil
}

func (s *OpenAIGatewayService) apiKeyWithFreshGroupMediaPricing(ctx context.Context, apiKey *APIKey) *APIKey {
	if apiKey == nil || apiKey.GroupID == nil || *apiKey.GroupID <= 0 {
		return apiKey
	}
	if !groupMediaPricingLooksIncomplete(apiKey.Group) {
		return apiKey
	}
	if s == nil || s.channelService == nil || s.channelService.groupRepo == nil {
		return apiKey
	}
	group, err := s.channelService.groupRepo.GetByIDLite(ctx, *apiKey.GroupID)
	if err != nil || group == nil {
		return apiKey
	}
	clone := *apiKey
	clone.Group = group
	return &clone
}

// groupMediaPricingLooksIncomplete 判断分组对象是否可能缺失媒体/搜索/语音计费字段
// （例如由不含这些字段的旧快照或手工构造的上下文对象生成）。image/video 独立倍率在
// 数据库中的默认值均为 1.0；正常加载的分组不可能两个倍率同时为 0 且未开启独立倍率、
// 全部媒体/搜索/语音价为 nil——只有这种情况才回源查库，避免对未配置覆盖价的分组每条
// 用量都多打一次 DB 查询。
//
// 注意：apiKeyAuthSnapshotVersion 升级会强制刷新存量快照；本函数是热路径上的二次兜底，
// 不能仅凭 legacy video_price_* 判定完整而跳过 VideoModelPrices/search/audio 的回源。
func groupMediaPricingLooksIncomplete(group *Group) bool {
	if group == nil {
		return true
	}
	if group.ImageRateIndependent || group.VideoRateIndependent {
		return false
	}
	if group.ImageRateMultiplier != 0 || group.VideoRateMultiplier != 0 {
		return false
	}
	// Any first-class pricing field present means the projection is not a blank shell.
	if len(group.VideoModelPrices) > 0 {
		return false
	}
	if len(group.ModelPricing) > 0 || group.LongContextPricingEnabled {
		return false
	}
	if group.SearchPricePer1k != nil ||
		group.AudioRealtimePricePerMin != nil ||
		group.AudioTTSPricePerMillionChars != nil ||
		group.AudioSTTPricePerHour != nil ||
		group.WebSearchPricePerCall != nil {
		return false
	}
	return group.ImagePrice1K == nil && group.ImagePrice2K == nil && group.ImagePrice4K == nil &&
		group.VideoPrice480P == nil && group.VideoPrice720P == nil && group.VideoPrice1080P == nil
}

// filterCNProviderBillingModelCandidates 过滤国产供应商（kimi/zhipu/deepseek）
// 账号的计费候选模型名：claude-* 候选仅在运营者显式配置了分组/渠道定价时保留。
//
// 背景：候选链的兜底候选含客户端请求的原始模型名。CN 上游的 Anthropic 兼容端点
// 接受 claude-* 模型名但从不真正服务 Claude 模型；若放行，目录里的 Claude 价卡
// 与 getFallbackPricing 的 "claude"→Sonnet 统一兜底会把 CN 流量按 Claude 原价
// （数倍～数十倍）静默误计，且 usage 日志显示的正是 claude-* 名，无从察觉。
// 候选全部落空时走既有的零成本+告警路径（openai_usage.pricing_missing_record_
// zero_cost），与定价层「未知型号不回退以避免误计价」的既有设计意图一致；
// 运营者的修复手段是配置账号级 model_mapping（映射到已定价的 CN 模型）或
// 分组/渠道显式定价。
func (s *OpenAIGatewayService) filterCNProviderBillingModelCandidates(ctx context.Context, account *Account, apiKey *APIKey, candidates []string) []string {
	if account == nil || (!account.IsCNProvider() && !account.IsOpenCodeGo()) {
		return candidates
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), "claude") &&
			s.resolveOpenAIChannelPricing(ctx, trimmed, apiKey) == nil {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func (s *OpenAIGatewayService) resolveOpenAIChannelPricing(ctx context.Context, billingModel string, apiKey *APIKey) *ResolvedPricing {
	if s.resolver == nil || apiKey == nil || apiKey.Group == nil {
		return nil
	}
	gid := apiKey.Group.ID
	resolved := s.resolver.Resolve(ctx, PricingInput{Model: billingModel, GroupID: &gid, Group: apiKey.Group})
	if resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel {
		return resolved
	}
	return nil
}

// ParseCodexRateLimitHeaders extracts Codex usage limits from response headers.
// Exported for use in ratelimit_service when handling OpenAI 429 responses.
func ParseCodexRateLimitHeaders(headers http.Header) *OpenAICodexUsageSnapshot {
	snapshot := &OpenAICodexUsageSnapshot{}
	hasData := false

	// Helper to parse float64 from header
	parseFloat := func(key string) *float64 {
		if v := headers.Get(key); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return &f
			}
		}
		return nil
	}

	// Helper to parse int from header
	parseInt := func(key string) *int {
		if v := headers.Get(key); v != "" {
			if i, err := strconv.Atoi(v); err == nil {
				return &i
			}
		}
		return nil
	}

	// Primary (weekly) limits
	if v := parseFloat("x-codex-primary-used-percent"); v != nil {
		snapshot.PrimaryUsedPercent = v
		hasData = true
	}
	if v := parseInt("x-codex-primary-reset-after-seconds"); v != nil {
		snapshot.PrimaryResetAfterSeconds = v
		hasData = true
	}
	if v := parseInt("x-codex-primary-window-minutes"); v != nil {
		snapshot.PrimaryWindowMinutes = v
		hasData = true
	}

	// Secondary (5h) limits
	if v := parseFloat("x-codex-secondary-used-percent"); v != nil {
		snapshot.SecondaryUsedPercent = v
		hasData = true
	}
	if v := parseInt("x-codex-secondary-reset-after-seconds"); v != nil {
		snapshot.SecondaryResetAfterSeconds = v
		hasData = true
	}
	if v := parseInt("x-codex-secondary-window-minutes"); v != nil {
		snapshot.SecondaryWindowMinutes = v
		hasData = true
	}

	// Overflow ratio
	if v := parseFloat("x-codex-primary-over-secondary-limit-percent"); v != nil {
		snapshot.PrimaryOverSecondaryPercent = v
		hasData = true
	}

	if !hasData {
		return nil
	}

	snapshot.UpdatedAt = time.Now().Format(time.RFC3339)
	return snapshot
}

func codexSnapshotBaseTime(snapshot *OpenAICodexUsageSnapshot, fallback time.Time) time.Time {
	if snapshot == nil {
		return fallback
	}
	if snapshot.UpdatedAt == "" {
		return fallback
	}
	base, err := time.Parse(time.RFC3339, snapshot.UpdatedAt)
	if err != nil {
		return fallback
	}
	return base
}

func codexResetAtRFC3339(base time.Time, resetAfterSeconds *int) *string {
	if resetAfterSeconds == nil {
		return nil
	}
	sec := *resetAfterSeconds
	if sec < 0 {
		sec = 0
	}
	resetAt := base.Add(time.Duration(sec) * time.Second).Format(time.RFC3339)
	return &resetAt
}

func buildCodexUsageExtraUpdates(snapshot *OpenAICodexUsageSnapshot, fallbackNow time.Time) map[string]any {
	if snapshot == nil {
		return nil
	}

	baseTime := codexSnapshotBaseTime(snapshot, fallbackNow)
	updates := make(map[string]any)

	// 保存原始 primary/secondary 字段，便于排查问题
	if snapshot.PrimaryUsedPercent != nil {
		updates["codex_primary_used_percent"] = *snapshot.PrimaryUsedPercent
	}
	if snapshot.PrimaryResetAfterSeconds != nil {
		updates["codex_primary_reset_after_seconds"] = *snapshot.PrimaryResetAfterSeconds
	}
	if snapshot.PrimaryWindowMinutes != nil {
		updates["codex_primary_window_minutes"] = *snapshot.PrimaryWindowMinutes
	}
	if snapshot.SecondaryUsedPercent != nil {
		updates["codex_secondary_used_percent"] = *snapshot.SecondaryUsedPercent
	}
	if snapshot.SecondaryResetAfterSeconds != nil {
		updates["codex_secondary_reset_after_seconds"] = *snapshot.SecondaryResetAfterSeconds
	}
	if snapshot.SecondaryWindowMinutes != nil {
		updates["codex_secondary_window_minutes"] = *snapshot.SecondaryWindowMinutes
	}
	if snapshot.PrimaryOverSecondaryPercent != nil {
		updates["codex_primary_over_secondary_percent"] = *snapshot.PrimaryOverSecondaryPercent
	}
	updates["codex_usage_updated_at"] = baseTime.Format(time.RFC3339)

	// 归一化到 5h/7d 规范字段
	if normalized := snapshot.Normalize(); normalized != nil {
		if normalized.Used5hPercent != nil {
			updates["codex_5h_used_percent"] = *normalized.Used5hPercent
		}
		if normalized.Reset5hSeconds != nil {
			updates["codex_5h_reset_after_seconds"] = *normalized.Reset5hSeconds
		}
		if normalized.Window5hMinutes != nil {
			updates["codex_5h_window_minutes"] = *normalized.Window5hMinutes
		}
		if normalized.Used7dPercent != nil {
			updates["codex_7d_used_percent"] = *normalized.Used7dPercent
		}
		if normalized.Reset7dSeconds != nil {
			updates["codex_7d_reset_after_seconds"] = *normalized.Reset7dSeconds
		}
		if normalized.Window7dMinutes != nil {
			updates["codex_7d_window_minutes"] = *normalized.Window7dMinutes
		}
		if reset5hAt := codexResetAtRFC3339(baseTime, normalized.Reset5hSeconds); reset5hAt != nil {
			updates["codex_5h_reset_at"] = *reset5hAt
		}
		if reset7dAt := codexResetAtRFC3339(baseTime, normalized.Reset7dSeconds); reset7dAt != nil {
			updates["codex_7d_reset_at"] = *reset7dAt
		}
	}
	appendSchedulingQuotaObservations(updates)

	return updates
}

// updateCodexUsageSnapshot saves the Codex usage snapshot to account's Extra field
// updateCodexUsageSnapshot 把 /responses 的 x-codex-* 全局头快照写入账号 codex_* Extra。
// ⚠️ 调用方必须排除 spark 影子账号(account.IsShadow()):影子的 codex_* 仅由 QueryUsage
// (/wham/usage bengalfox 道)更新,不能被全局头口径污染(外审第7轮 P1)。本函数仅持 accountID,
// 无法在此自检影子,故守卫前置到各调用点。
func (s *OpenAIGatewayService) updateCodexUsageSnapshot(ctx context.Context, accountID int64, snapshot *OpenAICodexUsageSnapshot) {
	if snapshot == nil {
		return
	}
	if s == nil || s.accountRepo == nil {
		return
	}

	now := time.Now()
	updates := buildCodexUsageExtraUpdates(snapshot, now)
	if len(updates) == 0 {
		return
	}
	if !s.getCodexSnapshotThrottle().Allow(accountID, now) {
		return
	}

	go func() {
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.accountRepo.UpdateExtra(updateCtx, accountID, updates); err == nil {
			notifyOpenAIAutoReset(accountID)
		}
	}()
}

func (s *OpenAIGatewayService) UpdateCodexUsageSnapshotFromHeaders(ctx context.Context, accountID int64, headers http.Header) {
	if accountID <= 0 || headers == nil {
		return
	}
	if snapshot := ParseCodexRateLimitHeaders(headers); snapshot != nil {
		s.updateCodexUsageSnapshot(ctx, accountID, snapshot)
	}
}

func cloneImageSizeBreakdown(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

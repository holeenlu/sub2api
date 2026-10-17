package service

// 国产供应商 Anthropic 协议账号的 CC 入站反向路径。
//
// 客户端说 OpenAI Chat Completions、上游是供应商原生 Anthropic 端点
// （api_protocol=anthropic）时的交叉组合：请求 CC→Responses→Anthropic 转换，
// 响应 Anthropic→Responses→CC 转换。转换链与 Anthropic 平台的
// gateway_forward_as_chat_completions.go 完全一致（复用同一组 apicompat
// 状态机），仅上游发送/错误处理对齐 OpenAI 网关语义。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// forwardChatCompletionsViaNativeAnthropic serves OpenAI /v1/chat/completions
// clients through a CN provider's native Anthropic endpoint.
//
// Conversion chain:
//
//	Request:  Chat Completions → Responses → Anthropic (chained)
//	Response: Anthropic events → Responses events → CC chunks (chained state machines)
func (s *OpenAIGatewayService) forwardChatCompletionsViaNativeAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	// 1. Parse Chat Completions request
	var ccReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &ccReq); err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}
	originalModel := ccReq.Model
	if strings.TrimSpace(originalModel) == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	clientStream := ccReq.Stream

	// 2. Convert CC → Responses → Anthropic (chained conversion)
	responsesReq, err := chatCompletionsToResponses(s.cfg, &ccReq)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to convert request")
		return nil, fmt.Errorf("convert chat completions to responses: %w", err)
	}
	// Resolve the mapped model before choosing its thinking/tool protocol.
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	if err := validateClaude55Request(body, upstreamModel); err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	responsesReq.Model = upstreamModel
	anthropicReq, err := apicompat.ResponsesToAnthropicRequest(responsesReq)
	if anthropicReq != nil {
		recordConversionDiagnostics(c, "openai", "anthropic", anthropicReq.Model, anthropicReq.Diagnostics)
	}
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert responses to anthropic: %w", err)
	}

	// 4. Force upstream streaming（客户端原始终决定响应格式；
	// 上游恒为流式，非流式由缓冲路径组装）。
	anthropicReq.Stream = true
	reqStream := true

	logger.L().Debug("openai chat_completions: forwarding via native anthropic endpoint",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("client_stream", clientStream),
	)

	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}
	anthropicBody, err = normalizeClaudeThinkingForUpstream(ctx, anthropicBody, upstreamModel)
	if err != nil {
		writeClaudeThinkingError(c, err, writeChatCompletionsError)
		return nil, err
	}

	// 与 /v1/messages 直通路径相同的 pre-filter。
	anthropicBody = StripEmptyTextBlocks(anthropicBody)
	anthropicBody = FilterWebSearchHistoryBlocks(anthropicBody, upstreamModel)
	anthropicBody = enforceCacheControlLimit(anthropicBody)

	apiKey := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if apiKey == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	targetURL, err := s.nativeAnthropicTargetURL(account)
	if err != nil {
		return nil, err
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, reqStream, streamWriteContext(c))
	upstreamReq, forwardedBody, err := s.buildNativeAnthropicUpstreamRequest(upstreamCtx, c, account, anthropicBody, apiKey, targetURL, body)
	defer releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	resp, err := s.doOpenAIUpstream(upstreamReq, proxyURL, account, vendorDispatchGroup(c))
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, true)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if foErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		writeChatCompletionsError(c, mapUpstreamStatusCode(resp.StatusCode), "server_error", upstreamMsg)
		return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
	}

	// Bill the Anthropic effort actually sent upstream (for example xhigh → max).
	reasoningEffort := NormalizeClaudeOutputEffort(gjson.GetBytes(forwardedBody, "output_config.effort").String())
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, forwardedBody, upstreamModel)

	if clientStream {
		return s.handleCCStreamingFromNativeAnthropic(resp, c, originalModel, billingModel, upstreamModel, reasoningEffort, startTime)
	}
	return s.handleCCBufferedFromNativeAnthropic(resp, c, originalModel, billingModel, upstreamModel, reasoningEffort, startTime)
}

// handleCCBufferedFromNativeAnthropic reads Anthropic SSE events, assembles the
// full response, then converts Anthropic → Responses → Chat Completions.
func (s *OpenAIGatewayService) handleCCBufferedFromNativeAnthropic(
	resp *http.Response,
	c *gin.Context,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	// Reuse the same SSE lifecycle and converters as the primary Anthropic gateway.
	compat := &GatewayService{cfg: s.cfg, responseHeaderFilter: s.responseHeaderFilter}
	result, err := compat.handleCCBufferedFromAnthropic(resp, c, originalModel, upstreamModel, reasoningEffort, startTime)
	return nativeAnthropicCompatForwardResult(result, billingModel), err
}

// handleCCStreamingFromNativeAnthropic reads Anthropic SSE events, converts each
// to Responses events, then to Chat Completions chunks, and writes them.
func (s *OpenAIGatewayService) handleCCStreamingFromNativeAnthropic(
	resp *http.Response,
	c *gin.Context,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	// Reuse the same SSE lifecycle and converters as the primary Anthropic gateway.
	compat := &GatewayService{cfg: s.cfg, responseHeaderFilter: s.responseHeaderFilter}
	result, err := compat.handleCCStreamingFromAnthropic(resp, c, originalModel, upstreamModel, reasoningEffort, startTime)
	return nativeAnthropicCompatForwardResult(result, billingModel), err
}

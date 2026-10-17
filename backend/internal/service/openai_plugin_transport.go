package service

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account, groupIDs ...*int64) (*http.Response, error) {
	var groupID int64
	if len(groupIDs) > 0 && groupIDs[0] != nil {
		groupID = *groupIDs[0]
	}
	if err := validateVendorNativeDispatch(request.Context(), s.accountRepo, account, &groupID); err != nil {
		return nil, vendorDispatchFailure(err)
	}
	if account.VendorID != nil && proxyURL != resolveAccountProxyURL(account) {
		return nil, vendorDispatchFailure(ErrVendorSupplyUnavailable)
	}

	attemptMode := "http"
	if account.IsSIWCEnabled() {
		if request.URL == nil || request.URL.Scheme != "https" || request.URL.Host != "api.openai.com" || request.URL.Path != "/v1/responses" || request.Method != http.MethodPost {
			return nil, fmt.Errorf("SIWC must use the official Responses endpoint")
		}
		// Enforce at the actual send boundary, after all compatibility adapters.
		authorization := request.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			return nil, fmt.Errorf("SIWC authorization is missing")
		}
		request.Header = http.Header{"Authorization": {authorization}, "Content-Type": {"application/json"}, "Accept": {"text/event-stream"}, "User-Agent": {"Sub2API"}}
		request.Host = ""
		attemptMode = "siwc"
	}
	var err error
	request, err = takeHTTPAccountTurnAttempt(request, account, proxyURL, attemptMode)
	if err != nil {
		return nil, turnAttemptFailure(err)
	}

	if !account.IsSIWCEnabled() && s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			if timing, _ := request.Context().Value(openAITransportTimingKey{}).(*openAIForwardTiming); timing != nil {
				timing.markOpaqueTransport()
			}
			return response, err
		}
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if !account.IsSIWCEnabled() && s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}

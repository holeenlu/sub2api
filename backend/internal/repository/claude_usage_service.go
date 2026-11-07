package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const defaultClaudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

// 默认 User-Agent，与用户抓包的请求一致
const defaultUsageUserAgent = "claude-code/2.1.7"

type claudeUsageService struct {
	usageURL          string
	allowPrivateHosts bool
	httpUpstream      service.HTTPUpstream
}

// NewClaudeUsageFetcher 创建 Claude 用量获取服务
// httpUpstream: 可选，如果提供则支持 TLS 指纹伪装
func NewClaudeUsageFetcher(httpUpstream service.HTTPUpstream) service.ClaudeUsageFetcher {
	return &claudeUsageService{
		usageURL:     defaultClaudeUsageURL,
		httpUpstream: httpUpstream,
	}
}

// FetchUsageWithOptions 完整版本，支持 TLS 指纹和自定义 User-Agent
func (s *claudeUsageService) FetchUsageWithOptions(ctx context.Context, opts *service.ClaudeUsageFetchOptions) (*service.ClaudeUsageResponse, error) {
	var result service.ClaudeUsageResponse
	if err := s.fetchJSON(ctx, opts, s.usageURL, &result); err != nil {
		return nil, err
	}
	if _, failed := result.RawFields["error"]; failed {
		return nil, fmt.Errorf("invalid usage response")
	}
	for _, key := range []string{"five_hour", "seven_day", "seven_day_sonnet", "seven_day_overage_included", "limits"} {
		if _, present := result.RawFields[key]; present {
			return &result, nil
		}
	}
	return nil, fmt.Errorf("empty or unsupported usage response")
}

func (s *claudeUsageService) fetchJSON(ctx context.Context, opts *service.ClaudeUsageFetchOptions, endpoint string, result any) error {
	if opts == nil {
		return fmt.Errorf("options is nil")
	}
	ctx = service.WithHTTPUpstreamRedirectsDisabled(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	userAgent := defaultUsageUserAgent
	if opts.Fingerprint != nil && opts.Fingerprint.UserAgent != "" {
		userAgent = opts.Fingerprint.UserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	var resp *http.Response
	if opts.TLSProfile != nil && s.httpUpstream != nil {
		resp, err = s.httpUpstream.DoWithTLS(req, opts.ProxyURL, opts.AccountID, 0, opts.TLSProfile)
	} else {
		client, clientErr := httpclient.GetClient(httpclient.Options{ProxyURL: opts.ProxyURL, Timeout: 30 * time.Second, ValidateResolvedIP: true, AllowPrivateHosts: s.allowPrivateHosts})
		if clientErr != nil {
			return fmt.Errorf("create http client failed: %w", clientErr)
		}
		isolated := *client
		isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err = isolated.Do(req)
	}
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		failure := &service.ClaudeUsageHTTPError{StatusCode: resp.StatusCode}
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&body) == nil {
			message := strings.ToLower(body.Error.Message)
			if resp.StatusCode == http.StatusUnauthorized && strings.Contains(message, "revoked") {
				failure.Reason = "revoked"
			}
			if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusForbidden {
				if strings.Contains(message, "organization has been disabled") || strings.Contains(message, "account has been disabled") {
					failure.Reason = "disabled"
				}
			}
		}
		return failure
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(result); err != nil {
		return fmt.Errorf("decode response failed: %w", err)
	}
	return nil
}

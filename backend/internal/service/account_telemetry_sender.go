package service

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
)

// Do not include tokens, raw transport errors or event bodies.
type accountTelemetryForwardError struct {
	Code           string
	UpstreamStatus int
}

func (e *accountTelemetryForwardError) Error() string { return e.Code }

func telemetryForwardFailure(code string) error {
	return &accountTelemetryForwardError{Code: code}
}

func logClaudeTelemetrySkipped(accountID int64, err error) {
	code, status := "telemetry_forwarding_unavailable", 0
	if failure, ok := err.(*accountTelemetryForwardError); ok {
		code, status = failure.Code, failure.UpstreamStatus
	}
	slog.Info("account_telemetry.not_forwarded", "account_id", accountID, "reason", code, "upstream_status", status)
}

type claudeTelemetryDispatch struct {
	account  *Account
	headers  http.Header
	identity claudeTelemetryIdentity
}

func (s *GatewayService) sendClaudeTelemetry(ctx context.Context, path string, group *claudeTelemetryDispatch, body []byte) error {
	if err := s.requireAccountTelemetrySendingEnabled(ctx); err != nil {
		return err
	}
	if !accountTelemetryEligible(group.account) {
		return nil
	}
	account := group.account
	target, proxyURL, err := s.claudeTelemetryTarget(account, path)
	if err != nil {
		return err
	}
	if len(body) > int(AccountTelemetryMaxBodyBytes) {
		return telemetryForwardFailure("telemetry_replacement_too_large")
	}
	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return telemetryForwardFailure("telemetry_credentials_unavailable")
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return telemetryForwardFailure("telemetry_upstream_invalid")
	}
	req.Header = accountTelemetryHeaders(group.headers)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-code/"+group.identity.Version)
	req.Header.Set("Accept", "application/json")
	req.Header.Del("Anthropic-Beta")
	req.Header.Set("X-Claude-Code-Session-Id", group.identity.SessionID)
	req.Header.Set("X-Service-Name", "claude-code")
	if tokenType != "oauth" {
		return telemetryForwardFailure("telemetry_credentials_unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if err := validateVendorNativeDispatch(ctx, s.accountRepo, account, nil); err != nil {
		return telemetryForwardFailure("telemetry_account_not_authorized")
	}
	current, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || !accountTelemetryEligible(current) || ReadAccountTelemetry(current).Revision != ReadAccountTelemetry(account).Revision || accountTelemetryOwner(current) != accountTelemetryOwner(account) {
		return telemetryForwardFailure("telemetry_profile_changed")
	}
	if err := s.requireAccountTelemetrySendingEnabled(ctx); err != nil {
		return err
	}
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		return telemetryForwardFailure("telemetry_upstream_unavailable")
	}
	if resp.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &accountTelemetryForwardError{Code: "telemetry_upstream_rejected", UpstreamStatus: resp.StatusCode}
	}
	slog.Info("account_telemetry.forwarded", "account_id", account.ID, "path", path, "upstream_status", resp.StatusCode)
	return nil
}

func (s *GatewayService) claudeTelemetryTarget(account *Account, path string) (string, string, error) {
	target, proxyURL := "https://api.anthropic.com"+path, ""
	if account.ProxyID != nil && account.Proxy == nil {
		return "", "", telemetryForwardFailure("telemetry_proxy_unavailable")
	}
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if account.IsCustomBaseURLEnabled() {
		if s.cfg == nil {
			return "", "", telemetryForwardFailure("telemetry_upstream_invalid")
		}
		validated, err := s.validateUpstreamBaseURL(account.GetCustomBaseURL())
		if err != nil {
			return "", "", telemetryForwardFailure("telemetry_upstream_invalid")
		}
		target, proxyURL = s.buildCustomRelayURL(validated, path, account), ""
	}
	return target, proxyURL, nil
}

var _ error = (*accountTelemetryForwardError)(nil)

package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// Adapted from sub4api a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e.
// Only control/context fields actually supplied by an upstream response may
// become defaults for later OAuth mimic requests. Identity and auth stay separate.
var claudeCodeControlHeaderNames = [...]string{
	"anthropic-usage-limit",
	"x-claude-code-prev-tool-durations",
	"x-claude-code-context-compacted",
	"x-cc-context-compacted",
	"x-claude-code-compaction",
	"x-cc-compaction-request",
}

func filterClaudeCodeControlHeaders(headers http.Header) http.Header {
	var filtered http.Header
	for key, values := range headers {
		for _, name := range claudeCodeControlHeaderNames {
			if !strings.EqualFold(key, name) {
				continue
			}
			if filtered == nil {
				filtered = make(http.Header)
			}
			if previous, exists := filtered[name]; exists {
				filtered[name] = append(previous, values...)
			} else if values == nil {
				filtered[name] = nil
			} else {
				filtered[name] = append(make([]string, 0, len(values)), values...)
			}
			break
		}
	}
	return filtered
}

func (s *GatewayService) applyClaudeCodeControlHeaders(ctx context.Context, req *http.Request, incoming http.Header, account *Account, tokenType string, mimic bool) {
	caller := filterClaudeCodeControlHeaders(incoming)
	// Copy explicit caller fields, including present-but-empty value arrays.
	// Presence, not Header.Get's nonempty result, decides precedence.
	for name, values := range caller {
		deleteHeaderAllForms(req.Header, name)
		req.Header[resolveWireCasing(name)] = values
	}
	if !mimic || tokenType != "oauth" || account == nil || !account.IsAnthropicOAuthOrSetupToken() || s.identityService == nil || s.identityService.cache == nil {
		return
	}
	cached, err := s.identityService.cache.GetClaudeCodeHeaders(ctx, account.ID)
	if err != nil {
		logger.LegacyPrintf("service.gateway", "Warning: failed to read Claude Code headers for account %d: %v", account.ID, err)
		return
	}
	for name, values := range filterClaudeCodeControlHeaders(cached) {
		if _, supplied := caller[name]; supplied {
			continue
		}
		deleteHeaderAllForms(req.Header, name)
		req.Header[resolveWireCasing(name)] = values
	}
}

// Observe every upstream response before error handling or retries. Incoming
// client headers must never be persisted as account-wide feedback.
func (s *GatewayService) observeClaudeCodeResponseHeaders(ctx context.Context, account *Account, resp *http.Response) {
	if resp == nil || account == nil || !account.IsAnthropicOAuthOrSetupToken() || s.identityService == nil || s.identityService.cache == nil {
		return
	}
	observed := filterClaudeCodeControlHeaders(resp.Header)
	if len(observed) > 0 {
		if err := s.identityService.cache.UpdateClaudeCodeHeaders(ctx, account.ID, observed); err != nil {
			logger.LegacyPrintf("service.gateway", "Warning: failed to update Claude Code headers for account %d: %v", account.ID, err)
		}
	}
}

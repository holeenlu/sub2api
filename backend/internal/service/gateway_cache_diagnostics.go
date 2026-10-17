package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type gatewayCacheDiagnosticKey struct{}
type gatewayCacheDiagnostic struct {
	Session, Routing, Client, Protocol, InputPrefix string
}

// Fingerprint rendered-prefix inputs independently of object-key order. This is
// evidence of transformation, not a claim that the provider's cache missed.
func gatewayCachePrefixFingerprint(body []byte) string {
	fields := make(map[string]any)
	// Visit the top-level object once instead of rescanning a long conversation
	// for each absent optional prefix field.
	parseRawJSONView(body).ForEach(func(key, value gjson.Result) bool {
		switch key.String() {
		case "model", "system", "instructions", "tools", "reasoning", "text":
			var decoded any
			if decodeJSONUseNumber([]byte(value.Raw), &decoded) == nil {
				fields[key.String()] = decoded
			}
		}
		return true
	})
	encoded, err := json.Marshal(fields)
	if err != nil {
		return ""
	}
	return gatewayCacheIdentity(0, string(encoded))
}

func logGatewayCacheTransform(ctx context.Context, account *Account, before string, body []byte) {
	fields := gatewayCacheDiagnosticFields(ctx)
	if len(fields) == 0 || account == nil {
		return
	}
	if before == "" {
		if d, ok := ctx.Value(gatewayCacheDiagnosticKey{}).(gatewayCacheDiagnostic); ok {
			before = d.InputPrefix
		}
	}
	after := gatewayCachePrefixFingerprint(body)
	fields = append(fields, zap.Int64("account_id", account.ID), zap.String("model", gjson.GetBytes(body, "model").String()), zap.String("prefix_before", before), zap.String("prefix_after", after), zap.Bool("prefix_changed", before != "" && before != after), zap.Bool("prefix_comparison_available", before != ""))
	logger.FromContext(ctx).Info("gateway.cache_prefix_transform", fields...)
}

func gatewayCacheIdentity(apiKeyID int64, raw string) string {
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", apiKeyID, len(raw), raw)))
	return fmt.Sprintf("%x", sum[:])
}

// WithGatewayCacheDiagnostics separates explicit conversation correlation from
// the routing hint being measured. Missing correlation stays unknown; prompt
// content, raw session IDs, credentials and bodies are never logged.
func WithGatewayCacheDiagnostics(ctx context.Context, apiKeyID int64, protocol, session, routing, ua string) context.Context {
	client := "other"
	if strings.Contains(strings.ToLower(ua), "claude-cli") {
		client = "claude_code"
	}
	if strings.Contains(strings.ToLower(ua), "codex") {
		client = "codex"
	}
	return context.WithValue(ctx, gatewayCacheDiagnosticKey{}, gatewayCacheDiagnostic{
		Session: gatewayCacheIdentity(apiKeyID, session), Routing: gatewayCacheIdentity(apiKeyID, routing), Client: client, Protocol: protocol,
	})
}

func gatewayCacheDiagnosticFields(ctx context.Context) []zap.Field {
	if ctx == nil {
		return nil
	}
	d, ok := ctx.Value(gatewayCacheDiagnosticKey{}).(gatewayCacheDiagnostic)
	if !ok {
		return nil
	}
	return []zap.Field{zap.String("correlation_id", d.Session), zap.String("routing_fingerprint", d.Routing), zap.String("client_class", d.Client), zap.String("ingress_protocol", d.Protocol)}
}

func logGatewayCacheSelection(ctx context.Context, event SchedulingDecision) {
	fields := gatewayCacheDiagnosticFields(ctx)
	if len(fields) == 0 || event.AccountID <= 0 || event.Stage != "selection" {
		return
	}
	fields = append(fields, zap.Int64("account_id", event.AccountID), zap.String("action", event.Action))
	logger.FromContext(ctx).Info("gateway.cache_route", fields...)
}

// The DB usage row remains the settlement ledger. This independent structured
// event preserves actual upstream counters, including partial usage, before
// discounts. Zero counters do not establish that an upstream attempt was free.
func logGatewayUsageAccounting(ctx context.Context, account *Account, apiKey *APIKey, requestID, sessionID string, observed, billed ClaudeUsage, forced, ttlOverride bool) {
	if account == nil || apiKey == nil {
		return
	}
	fields := gatewayCacheDiagnosticFields(ctx)
	if len(fields) == 0 {
		fields = append(fields, zap.String("correlation_id", gatewayCacheIdentity(apiKey.ID, sessionID)))
	}
	fields = append(fields,
		zap.String("upstream_request_id", requestID), zap.Int64("api_key_id", apiKey.ID), zap.Int64("account_id", account.ID), zap.String("platform", account.Platform),
		zap.Int("upstream_input_uncached", observed.InputTokens), zap.Int("upstream_cache_read", observed.CacheReadInputTokens),
		zap.Int("upstream_cache_write", observed.CacheCreationInputTokens), zap.Int("upstream_cache_write_5m", observed.CacheCreation5mTokens), zap.Int("upstream_cache_write_1h", observed.CacheCreation1hTokens),
		zap.Int("upstream_output", observed.OutputTokens), zap.Bool("has_observed_tokens", observed.hasObservedTokens()),
		zap.Int("billed_input", billed.InputTokens), zap.Int("billed_cache_read", billed.CacheReadInputTokens), zap.Int("billed_cache_write", billed.CacheCreationInputTokens),
		zap.Int("billed_cache_write_5m", billed.CacheCreation5mTokens), zap.Int("billed_cache_write_1h", billed.CacheCreation1hTokens),
		zap.Bool("force_cache_billing", forced), zap.Bool("cache_ttl_overridden", ttlOverride))
	logger.FromContext(ctx).Info("gateway.cache_usage", fields...)
}

func withGatewayCachePrefix(ctx context.Context, body []byte) context.Context {
	d, ok := ctx.Value(gatewayCacheDiagnosticKey{}).(gatewayCacheDiagnostic)
	if !ok {
		return ctx
	}
	d.InputPrefix = gatewayCachePrefixFingerprint(body)
	return context.WithValue(ctx, gatewayCacheDiagnosticKey{}, d)
}

func logGatewayCacheWSPayload(ctx context.Context, account *Account, payload map[string]any) {
	if len(gatewayCacheDiagnosticFields(ctx)) == 0 {
		return
	}
	fields := make(map[string]any)
	for _, name := range []string{"model", "system", "instructions", "tools", "reasoning", "text"} {
		if value, ok := payload[name]; ok {
			fields[name] = value
		}
	}
	if body, err := json.Marshal(fields); err == nil {
		logGatewayCacheTransform(ctx, account, "", body)
	}
}

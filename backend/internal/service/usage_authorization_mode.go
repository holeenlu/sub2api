package service

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const (
	UsageAuthorizationModeNative = "native"
	UsageAuthorizationModeBPS    = "bps"
	UsageAuthorizationModeSIWC   = "siwc"
)

// UsageAuthorizationModePtr is deliberately closed: missing/unknown historical
// facts stay NULL in storage, never reconstructed from the current account.
func UsageAuthorizationModePtr(mode string) *string {
	switch mode {
	case UsageAuthorizationModeNative, UsageAuthorizationModeBPS, UsageAuthorizationModeSIWC:
		return &mode
	default:
		return nil
	}
}

func DisplayUsageAuthorizationMode(mode string) string {
	if UsageAuthorizationModePtr(mode) != nil {
		return mode
	}
	return "unknown"
}

// Only native forwarding paths call this, when producing their measured result.
// OpenAI-compatible endpoints on other platforms must not claim OpenAI grants.
func nativeOpenAIUsageAuthorizationMode(account *Account) string {
	if account.IsSIWCEnabled() {
		return UsageAuthorizationModeSIWC
	}
	if account != nil && account.Platform == PlatformOpenAI {
		return UsageAuthorizationModeNative
	}
	return ""
}

const actualOpenAIUsageModeKey = "actual_openai_usage_authorization_mode"

func setActualOpenAIUsageAuthorizationMode(c *gin.Context, mode string) {
	if c != nil {
		c.Set(actualOpenAIUsageModeKey, mode)
	}
}

// ActualOpenAIUsageAuthorizationMode also carries failed upstream cyber usage
// when no result can be returned. It is captured before asynchronous recording.
func ActualOpenAIUsageAuthorizationMode(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(actualOpenAIUsageModeKey)
}

// A same-account recovery remains one customer bill, with its original pricing,
// request fingerprint and quota deduction. Only its persisted supply facts split
// by the executions captured while forwarding. Applying pricing independently
// here would change long-context tiers and charge per-request prices twice.
func (s *OpenAIGatewayService) writeOpenAIAuthorizationUsage(ctx context.Context, aggregate *UsageLog, result *OpenAIForwardResult, account *Account) {
	for _, row := range splitOpenAIUsageAuthorization(aggregate, result, account) {
		writeUsageLogBestEffort(ctx, s.usageLogRepo, row, "service.openai_gateway")
	}
}

func splitOpenAIUsageAuthorization(aggregate *UsageLog, result *OpenAIForwardResult, account *Account) []*UsageLog {
	if len(result.PriorAuthorizationResults) == 0 {
		return []*UsageLog{aggregate}
	}
	parts := append([]*OpenAIForwardResult(nil), result.PriorAuthorizationResults...)
	current := *result
	if result.authorizationUsage != nil {
		current.Usage = *result.authorizationUsage
	}
	parts = append(parts, &current)
	rows := make([]*UsageLog, len(parts))
	for i, part := range parts {
		row := *aggregate
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%s", aggregate.RequestID, aggregate.AccountID, i, part.AuthorizationMode)))
		row.RequestID = fmt.Sprintf("auth:%x", digest[:28])
		row.AuthorizationMode = part.AuthorizationMode
		row.UpstreamRequestID = usageUpstreamRequestIDPtr(account, part.UpstreamHeaders, part.OpenAIWSMode)
		row.UpstreamEndpoint = optionalTrimmedStringPtr(part.UpstreamEndpoint)
		row.UpstreamModel = optionalTrimmedStringPtr(part.UpstreamModel)
		row.UpstreamResponseModel = optionalTrimmedStringPtr(part.UpstreamResponseModel)
		row.UpstreamModelMismatch = upstreamModelMismatch(upstreamSentModel(part.Model, part.UpstreamModel), part.UpstreamResponseModel)
		row.ServiceTier = part.ServiceTier
		row.InputTokens = max(0, part.Usage.InputTokens-part.Usage.CacheReadInputTokens-part.Usage.CacheCreationInputTokens)
		row.OutputTokens = part.Usage.OutputTokens
		row.CacheCreationTokens = part.Usage.CacheCreationInputTokens
		row.CacheCreation5mTokens = part.Usage.CacheCreation5mTokens
		row.CacheCreation1hTokens = part.Usage.CacheCreation1hTokens
		row.CacheCreation30mTokens = part.Usage.CacheCreation30mTokens
		row.CacheCreation12hTokens = part.Usage.CacheCreation12hTokens
		row.CacheReadTokens = part.Usage.CacheReadInputTokens
		row.ImageInputTokens = part.Usage.ImageInputTokens
		row.ImageOutputTokens = part.Usage.ImageOutputTokens
		row.ImageCount = part.ImageCount
		row.ImageSizeBreakdown = cloneImageSizeBreakdown(part.ImageSizeBreakdown)
		if part.Usage.ImageCacheReadTokens > 0 {
			if row.ImageSizeBreakdown == nil {
				row.ImageSizeBreakdown = make(map[string]int)
			}
			row.ImageSizeBreakdown["image_cache_read_tokens"] = part.Usage.ImageCacheReadTokens
		}
		duration := int(part.Duration.Milliseconds())
		row.DurationMs, row.FirstTokenMs = &duration, part.FirstTokenMs
		row.LatencyMetrics = part.LatencyMetrics
		row.Stream, row.OpenAIWSMode = part.Stream, part.OpenAIWSMode
		rows[i] = &row
	}
	allocateAuthorizationUsageRows(aggregate, rows)
	return rows
}

// Theoretical costs retain ten decimals; charged amounts use the debit scale.
// Allocate the remainder so splitting cannot add or lose stored money.
func allocateAuthorizationUsageCost(total float64, weights []float64, scale int32) []float64 {
	amounts := make([]float64, len(weights))
	if len(weights) == 0 {
		return amounts
	}
	var sum float64
	last := len(weights) - 1
	for i, weight := range weights {
		sum += weight
		if weight > 0 {
			last = i
		}
	}
	roundedTotal := decimal.NewFromFloat(total).Round(scale)
	remaining := roundedTotal
	if sum == 0 {
		amounts[last], _ = remaining.Float64()
		return amounts
	}
	for i, weight := range weights {
		if weight <= 0 {
			continue
		}
		share := remaining
		if i != last {
			share = roundedTotal.Mul(decimal.NewFromFloat(weight)).Div(decimal.NewFromFloat(sum)).Round(scale)
			// Several half-unit shares must not over-allocate and make the final
			// row negative. This also keeps negative adjustments symmetric.
			if share.Abs().GreaterThan(remaining.Abs()) {
				share = remaining
			}
		}
		amounts[i], _ = share.Float64()
		remaining = remaining.Sub(share)
	}
	return amounts
}

func allocateAuthorizationUsageRows(aggregate *UsageLog, rows []*UsageLog) {
	if len(rows) == 0 {
		return
	}
	// Keep the already-priced input/output/cache buckets; their measured token
	// shares attribute costs without reinterpreting the account's current mode.
	for _, bucket := range []struct {
		cost   func(*UsageLog) *float64
		weight func(*UsageLog) float64
	}{
		{func(r *UsageLog) *float64 { return &r.InputCost }, func(r *UsageLog) float64 { return float64(r.InputTokens) }},
		{func(r *UsageLog) *float64 { return &r.OutputCost }, func(r *UsageLog) float64 { return float64(r.OutputTokens) }},
		{func(r *UsageLog) *float64 { return &r.CacheCreationCost }, func(r *UsageLog) float64 { return float64(r.CacheCreationTokens) }},
		{func(r *UsageLog) *float64 { return &r.CacheReadCost }, func(r *UsageLog) float64 { return float64(r.CacheReadTokens) }},
		{func(r *UsageLog) *float64 { return &r.ImageInputCost }, func(r *UsageLog) float64 { return float64(r.ImageInputTokens) }},
		{func(r *UsageLog) *float64 { return &r.ImageOutputCost }, func(r *UsageLog) float64 { return float64(r.ImageOutputTokens) }},
	} {
		weights := make([]float64, len(rows))
		for i, row := range rows {
			weights[i] = bucket.weight(row)
		}
		for i, amount := range allocateAuthorizationUsageCost(*bucket.cost(aggregate), weights, 10) {
			*bucket.cost(rows[i]) = amount
		}
	}
	weights := make([]float64, len(rows))
	var weightSum float64
	for i, row := range rows {
		weights[i] = row.InputCost + row.OutputCost + row.CacheCreationCost + row.CacheReadCost + row.ImageInputCost + row.ImageOutputCost
		weightSum += weights[i]
	}
	if weightSum == 0 {
		// Flat/request prices have no token cost buckets. Apportion that one
		// price by metered tokens, keeping a zero-usage final result cost-free.
		for i, row := range rows {
			weights[i] = float64(row.TotalTokens())
		}
	}
	for i, amount := range allocateAuthorizationUsageCost(aggregate.TotalCost, weights, 10) {
		rows[i].TotalCost = amount
	}
	for i, amount := range allocateAuthorizationUsageCost(aggregate.ActualCost, weights, UsageBillingMonetaryScale) {
		rows[i].ActualCost = amount
	}
	if aggregate.AccountStatsCost != nil {
		for i, amount := range allocateAuthorizationUsageCost(*aggregate.AccountStatsCost, weights, 10) {
			value := amount
			rows[i].AccountStatsCost = &value
		}
	}
}

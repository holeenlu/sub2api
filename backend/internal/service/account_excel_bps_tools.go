package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"go.uber.org/zap"
)

const ExcelBPSOmitUnsupportedToolsKey = "openai_excel_bps_omit_unsupported_tools"

// IsExcelBPSOmitUnsupportedToolsEnabled keeps requests that need a hosted
// capability on BPS, where the bridge omits the tool and tells the model. By
// default such requests use the account's native Codex channel instead.
func (a *Account) IsExcelBPSOmitUnsupportedToolsEnabled() bool {
	return a.IsExcelBPSEnabled() && a.Extra[ExcelBPSOmitUnsupportedToolsKey] == true
}

// excelBPSNativeFallbackReason names the hosted capability that moves this
// request to the native channel. Codex's default cached web search is not
// one: it stays on BPS and is omitted, otherwise nearly every request would
// leave the bridge.
func (a *Account) excelBPSNativeFallbackReason(body []byte) string {
	if a.IsExcelBPSOmitUnsupportedToolsEnabled() {
		return ""
	}
	return basispoints.NativeFallbackReason(body)
}

func recordExcelBPSNativeFallback(ctx context.Context, account *Account, reason string) {
	// The context logger carries the request ID. Never log the request body,
	// tool arguments, account credentials or session identity here.
	logger.FromContext(ctx).Info("excel_bps.native_fallback",
		zap.Int64("account_id", account.ID),
		zap.String("policy", "native_fallback"),
		zap.String("reason", reason),
		zap.String("upstream_endpoint", openAIResponsesUpstreamEndpoint))
}

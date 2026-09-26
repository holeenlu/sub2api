package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Retain sanitized diagnostics for the existing account proxy transport.
// Managed Mihomo acquisition, failover and health tracking are not enabled.
func recordExcelBPSTransportFailure(ctx context.Context, c *gin.Context, account *Account, scope string, err error, stage string, attempt int, retry bool) {
	kind := transportdiag.Classify(err)
	digest := sha256.Sum256([]byte(scope))
	sessionHash := hex.EncodeToString(digest[:8])
	detail, _ := json.Marshal(map[string]any{
		"error_kind": kind, "error_type": fmt.Sprintf("%T", err),
		"session_hash": sessionHash, "attempt": attempt, "retry_before_send": retry,
	})
	message := "Excel BPS " + stage + " failed: " + kind
	// Keep UI client errors generic; persist only explicitly safe diagnostics.
	if !retry {
		setOpsUpstreamError(c, 0, message, string(detail))
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID,
		UpstreamURL: basispoints.ResponsesURL, Kind: "request_error", Stage: stage,
		Scope: "excel_bps", Reason: kind, Message: message, Detail: string(detail),
	})
	logger.FromContext(ctx).Warn("excel_bps.transport_failed",
		zap.Int64("account_id", account.ID), zap.String("stage", stage),
		zap.String("error_kind", kind), zap.String("error_type", fmt.Sprintf("%T", err)),
		zap.String("session_hash", sessionHash),
		zap.Int("attempt", attempt), zap.Bool("retry_before_send", retry))
}

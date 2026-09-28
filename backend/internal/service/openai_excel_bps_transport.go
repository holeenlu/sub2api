package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Recheck every BPS generation, including repair attempts, against the current
// primary snapshot. Never inject Codex ticket headers into a BPS request.
func (s *OpenAIGatewayService) doExcelBPSSend(ctx context.Context, c *gin.Context, account *Account, model string, req *http.Request, proxy string) (*http.Response, error) {
	if _, err := s.admitOpenAITurn(ctx, c, account, model); err != nil {
		return nil, err
	}
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	if err != nil && resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return resp, err
}

// Retain sanitized diagnostics for the existing account proxy transport.
func recordExcelBPSTransportFailure(ctx context.Context, c *gin.Context, account *Account, scope string, err error, stage string, attempt int, retry bool, evidence ...*transportdiag.Trace) {
	if isExcelBPSClientCancellation(c, err) {
		logger.FromContext(ctx).Info("excel_bps.client_canceled",
			zap.Int64("account_id", account.ID), zap.String("stage", stage))
		return
	}
	kind := transportdiag.Classify(err)
	digest := sha256.Sum256([]byte(scope))
	sessionHash := hex.EncodeToString(digest[:8])
	details := map[string]any{
		"error_kind": kind, "error_type": fmt.Sprintf("%T", err),
		"session_hash": sessionHash, "attempt": attempt, "retry_before_send": retry,
	}
	if len(evidence) > 0 && evidence[0] != nil {
		details["transport"] = evidence[0].Snapshot()
	}
	detail, _ := json.Marshal(details)
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
		zap.Int("attempt", attempt), zap.Bool("retry_before_send", retry), zap.Any("transport", details["transport"]))
}

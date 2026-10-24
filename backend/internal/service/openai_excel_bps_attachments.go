package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type excelBPSAttachmentError struct {
	status              int
	retryAfter          string
	upstreamStatus      int
	stage, kind         string
	cause               error
	retryReason         string
	retryDelay          time.Duration
	retrySource         string
	retryAttempts       int
	compactionCompleted bool
}

func (e *excelBPSAttachmentError) Error() string {
	return fmt.Sprintf("excel BPS attachment failed: %s/%s (status %d)", e.stage, e.kind, e.status)
}

func (e *excelBPSAttachmentError) Unwrap() error { return e.cause }

// Retain only allowlisted diagnostics and context sentinels, never a raw URL,
// response body, file ID or credential-bearing transport error.
func excelBPSAttachmentFailure(stage, kind string, upstreamStatus int, cause error) *excelBPSAttachmentError {
	failure := &excelBPSAttachmentError{status: http.StatusBadGateway, stage: stage, kind: kind, upstreamStatus: upstreamStatus}
	if errors.Is(cause, context.Canceled) {
		failure.cause = context.Canceled
	} else if errors.Is(cause, context.DeadlineExceeded) {
		failure.cause = context.DeadlineExceeded
	}
	return failure
}

// excelBPSAttachmentPhaseTimeout bounds one upload phase (all images of one
// request plan, uploaded with bounded parallelism). The first-output budget is
// paused meanwhile, so a slow upload reports attachment_timeout instead of
// first_output_timeout and does not shorten the model's own time to respond.
// A variable so tests can shorten it.
var excelBPSAttachmentPhaseTimeout = 120 * time.Second

// excelBPSAttachmentUploadTimeout bounds a single image upload request.
const excelBPSAttachmentUploadTimeout = 60 * time.Second

var errExcelBPSAttachmentPhaseTimeout = errors.New("excel BPS attachment phase deadline exceeded")

// isExcelBPSAttachmentTimeout reports a per-image or per-phase upload timeout.
func isExcelBPSAttachmentTimeout(err error) bool {
	var failure *excelBPSAttachmentError
	return errors.As(err, &failure) && errors.Is(failure.cause, context.DeadlineExceeded)
}

func (s *OpenAIGatewayService) uploadExcelBPSAttachment(ctx context.Context, account *Account, token, accountID, proxyURL string, img basispoints.InlineAttachment) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, excelBPSAttachmentUploadTimeout)
	defer cancel()
	ctx = WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileExcelBPS))
	reader, contentType, length, err := img.Multipart()
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_prepare", "multipart", 0, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, basispoints.AttachmentsURL, reader)
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_prepare", "request_construction", 0, err)
	}
	auth, err := newExcelBPSRequest(ctx, nil, token, accountID)
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_prepare", "authentication", 0, err)
	}
	req.Header = auth.Header
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.ContentLength = length
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_transport", transportdiag.Classify(err), 0, err)
	}
	if resp == nil || resp.Body == nil {
		return "", excelBPSAttachmentFailure("attachment_response", "missing_response", 0, nil)
	}
	// Release the account's upstream connection slot before starting Responses.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		s.handleExcelBPSUnauthorized(ctx, account, resp.StatusCode, resp.Header, raw, token)
		status := resp.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		failure := excelBPSAttachmentFailure("attachment_http", "upstream_http", resp.StatusCode, nil)
		failure.status, failure.retryAfter = status, resp.Header.Get("Retry-After")
		return "", failure
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_response", transportdiag.Classify(err), resp.StatusCode, err)
	}
	if len(raw) > 64<<10 {
		return "", excelBPSAttachmentFailure("attachment_response", "response_too_large", resp.StatusCode, nil)
	}
	var result struct {
		OpenAIFileID string `json:"openai_file_id"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", excelBPSAttachmentFailure("attachment_response", "invalid_json", resp.StatusCode, nil)
	}
	if !basispoints.ValidAttachmentID(result.OpenAIFileID) {
		return "", excelBPSAttachmentFailure("attachment_response", "invalid_file_id", resp.StatusCode, nil)
	}
	return result.OpenAIFileID, nil
}

// Only an owner-bound request needs local attachment recovery: portable requests
// retain the existing account-switch policy. One rejected upload may be retried
// per client request, without re-uploading successful parts or replaying inference.
const maxExcelBPSAttachmentRetryDelay = 5 * time.Second

func (s *OpenAIGatewayService) excelBPSAttachmentUpload(c *gin.Context, account *Account, token, accountID, proxyURL string, portable, replayableHistory bool, compactionCompleted *bool) basispoints.AttachmentUpload {
	// Uploads of one request run in parallel; the single same-account retry is
	// shared by all of them, so claim it under a lock.
	var retryMu sync.Mutex
	retried := false
	retryClaimed := func() bool {
		retryMu.Lock()
		defer retryMu.Unlock()
		return retried
	}
	claimRetry := func() bool {
		retryMu.Lock()
		defer retryMu.Unlock()
		if retried {
			return false
		}
		retried = true
		return true
	}
	return func(ctx context.Context, img basispoints.InlineAttachment) (string, error) {
		SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/attachments")
		id, err := s.uploadExcelBPSAttachment(ctx, account, token, accountID, proxyURL, img)
		var failure *excelBPSAttachmentError
		if errors.As(err, &failure) {
			failure.compactionCompleted = compactionCompleted != nil && *compactionCompleted
		}
		if err == nil ||
			!errors.As(err, &failure) || failure.stage != "attachment_http" || failure.upstreamStatus != http.StatusTooManyRequests {
			return id, err
		}
		stop := func(reason string) (string, error) {
			failure.retryReason = reason
			return "", err
		}
		if portable {
			return stop("portable_context") // native fallback/account switching is decided by the caller
		}
		if replayableHistory && !failure.compactionCompleted {
			return stop("replayable_history") // no inference to replay; retain original ciphertext
		}
		if retryClaimed() {
			failure.retryAttempts = 1
			return stop("retry_exhausted")
		}
		delay, ok := excelBPSRetryAfter(failure.retryAfter, time.Now())
		failure.retrySource = "retry_after"
		if !ok {
			failure.retrySource = "fallback_setting"
			delay, ok = s.excelBPS429FallbackCooldown(ctx, account)
		}
		if !ok {
			return stop("fallback_disabled")
		}
		if delay < time.Second {
			delay = time.Second
		}
		failure.retryDelay = delay
		if delay > maxExcelBPSAttachmentRetryDelay {
			return stop("delay_exceeds_limit")
		}
		waitCtx, cancel := OpenAITurnWaitContext(ctx)
		defer cancel()
		deadline, bounded := waitCtx.Deadline()
		if bounded && !time.Now().Add(delay).Before(deadline) {
			return stop("turn_deadline")
		}
		if !claimRetry() {
			failure.retryAttempts = 1
			return stop("retry_exhausted")
		}
		if budgetErr := TakeTurnPreparationRetry(ctx); budgetErr != nil {
			if errors.Is(budgetErr, ErrOpenAITurnAttemptBudgetExhausted) {
				return stop("attempt_budget")
			}
			if errors.Is(budgetErr, context.Canceled) {
				return stop("client_canceled")
			}
			return stop("turn_deadline")
		}
		failure.retryReason = "short_wait"
		// The 429 response body is already closed. Avoid scheduling additional BPS
		// work on the account while this request waits for the same short cooldown.
		s.coolDownExcelBPS(ctx, account, failure.retryAfter)
		recordExcelBPSAttachmentFailure(ctx, c, account, err, "retry", delay)
		waitErr := WaitOpenAITurnRetry(waitCtx, delay)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if waitErr != nil || waitCtx.Err() != nil {
			return stop("turn_deadline")
		}
		// A concurrent rejection may have extended the cooldown. Never clear it or
		// shorten a longer Retry-After merely because our original wait has elapsed.
		if value, exists := s.excelBPSCooldownUntil.Load(account.ID); exists {
			if until, valid := value.(time.Time); valid && time.Now().Before(until) {
				return stop("cooldown_extended")
			}
		}
		id, retryErr := s.uploadExcelBPSAttachment(waitCtx, account, token, accountID, proxyURL, img)
		var retryFailure *excelBPSAttachmentError
		if errors.As(retryErr, &retryFailure) {
			retryFailure.compactionCompleted = failure.compactionCompleted
			retryFailure.retryReason = "retry_exhausted"
			retryFailure.retryDelay, retryFailure.retrySource = delay, failure.retrySource
			retryFailure.retryAttempts = 1
		}
		return id, retryErr
	}
}

func recordExcelBPSAttachmentFailure(ctx context.Context, c *gin.Context, account *Account, err error, recovery string, retryDelay time.Duration) {
	stage, reason, upstreamStatus := "attachment_upload", "unknown", 0
	var failure *excelBPSAttachmentError
	if errors.As(err, &failure) {
		stage, reason, upstreamStatus = failure.stage, failure.kind, failure.upstreamStatus
	} else if errors.Is(err, basispoints.ErrAttachmentBusy) {
		stage, reason = "attachment_capacity", "capacity_exhausted"
	}
	diagnostic := map[string]any{"stage": stage, "error_kind": reason, "upstream_status_code": upstreamStatus, "generation_started": false}
	if failure != nil && failure.compactionCompleted {
		diagnostic["compaction_completed"] = true
	}
	if failure != nil && failure.retryReason != "" {
		diagnostic["attachment_retry_reason"] = failure.retryReason
		diagnostic["attachment_retry_attempts"] = failure.retryAttempts
		diagnostic["retry_max_delay_ms"] = maxExcelBPSAttachmentRetryDelay.Milliseconds()
		if failure.retrySource != "" {
			diagnostic["retry_delay_source"] = failure.retrySource
			diagnostic["retry_delay_ms"] = failure.retryDelay.Milliseconds()
		}
	}
	if c != nil {
		if portable, known := c.Get(OpenAIAccountReplayAllowedContextKey); known {
			if value, ok := portable.(bool); ok {
				diagnostic["context_portable"] = value
			}
		}
		if blocker := c.GetString(openAIAccountReplayBlockerContextKey); blocker != "" {
			diagnostic["context_blocker"] = blocker
		}
	}
	if recovery == "retry" {
		diagnostic["retry_delay_ms"] = retryDelay.Milliseconds()
		diagnostic["retry_same_account"] = true
	}
	detail, _ := json.Marshal(diagnostic)
	message := "Excel BPS " + stage + " failed: " + reason
	kind := "request_error"
	if upstreamStatus >= 300 {
		kind = "http_error"
	}
	if recovery != "" {
		kind = recovery
	} else {
		setOpsUpstreamError(c, upstreamStatus, message, string(detail))
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID,
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
		UpstreamStatusCode: upstreamStatus, UpstreamURL: basispoints.AttachmentsURL,
		Kind: kind, Stage: stage, Scope: "excel_bps", Reason: reason, Message: message, Detail: string(detail),
	})
	logger.FromContext(ctx).Warn("excel_bps.attachment_failed", zap.Int64("account_id", account.ID),
		zap.String("stage", stage), zap.String("error_kind", reason), zap.Int("upstream_status", upstreamStatus), zap.Bool("generation_started", false))
}

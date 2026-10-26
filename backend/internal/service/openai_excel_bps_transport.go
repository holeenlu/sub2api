package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// excelBPSForwardError retains the client-safe code and the HTTP status that
// was written to the client after the response is committed.
type excelBPSForwardError struct {
	code       string
	status     int
	dependency bool // trusted local origin, never decoded from provider data
}

func (e *excelBPSForwardError) Error() string { return "excel BPS: " + e.code }

// RequestScoped supplies scheduler scope, including local dependencies. Ops
// keeps dependency 503s as gateway SLA failures, not business limitations.
// Client 4xx scope covers: an invalid
// request, unsupported tools, image limits or an upstream 4xx that the gateway
// forwards unchanged. These describe the request, not the account, and must
// not lower the account's scheduling health. Upstream credential rejections are
// mapped to 503 before this check, so they still count as account failures.
func (e *excelBPSForwardError) RequestScoped() bool {
	return e != nil && (e.dependency || e.status >= 400 && e.status < 500)
}

// excelBPSUpstreamAuthClientStatus hides BPS 401/403 from clients. Codex treats
// a gateway 401 as its own login expiring and a 403 as a banned API key, while
// the failure belongs to this account's Excel authorization. The handler maps
// the same failures to 503 when failover is exhausted; keep that contract here.
func excelBPSUpstreamAuthClientStatus(upstreamStatus int) int {
	if upstreamStatus == http.StatusUnauthorized || upstreamStatus == http.StatusForbidden {
		return http.StatusServiceUnavailable
	}
	return upstreamStatus
}

const ExcelBPSUpstreamFailureReason = GatewayFailureReason("basispoints_upstream_error")

func newExcelBPSUpstreamFailureError(status int) *UpstreamFailoverError {
	return &UpstreamFailoverError{
		StatusCode: status, Stage: GatewayFailureStageInference,
		Scope: GatewayFailureScopeProvider, Reason: ExcelBPSUpstreamFailureReason,
		NextAccountAction: NextAccountRetry, ClientStatusCode: status,
		ClientMessage:            "Excel BPS service temporarily unavailable, please retry later",
		RequestScopedTransient:   true,
		SafeToFailoverAfterWrite: true, // inference output has not been committed
	}
}

// Original managed image bytes can be uploaded again after ownership rejection.
// Inline tool images and reasoning ciphertext are preserved unchanged in this
// same-account retry. Opaque compaction remains unresolved context. Accepted
// output/usage is checked by the caller.
func canRetryExcelBPSAttachmentOwnership(images *basispoints.NativeImages, wire, original []byte, compacted bool) bool {
	if images == nil || !images.HasImages() || compacted || !openAIWSUncertainExecutionReplaySafe(original) {
		return false
	}
	refs := images.FileReferences(wire)
	return refs.FreshUpload+refs.ReusedUpload > 0 && refs.Unmanaged == 0 && refs.Remote == 0 &&
		refs.OpaqueCompactionItems == 0
}

// One same-account refresh only, shared with the handler's retry budget. Fresh
// IDs can also fail ownership. Neither this error nor retry exhaustion may
// change accounts or mark account health/quota as failed.
func newExcelBPSAttachmentOwnershipRetryError(status int) *UpstreamFailoverError {
	failure := newExcelBPSUpstreamFailureError(status)
	failure.Scope, failure.Reason = GatewayFailureScopeRequest, "basispoints_attachment_ownership"
	failure.NextAccountAction = NextAccountStop
	failure.ClientMessage = "Excel BPS could not validate file ownership after bounded recovery; resend the original image data or use the channel that owns the files"
	failure.RetryableOnSameAccount, failure.SameAccountRetryMax = true, 1
	return failure
}

// Local provenance is kept separately from the sanitized upstream body.
// All fields are fixed phase names, counts or flags; no request values are stored.
type excelBPSFileOwnershipContext struct {
	basispoints.FileReferenceCounts
	Stage               string `json:"stage"`
	CompactionCompleted bool   `json:"compaction_completed"`
	// Cached attachment entries removed because this request failed ownership.
	CacheEntriesInvalidated int `json:"cache_entries_invalidated"`
}

// Keep original HTTP and in-band failures in the same existing Ops event path.
// Response content and credentials are never copied into client error bodies.
func (s *OpenAIGatewayService) recordExcelBPSUpstreamFailure(c *gin.Context, account *Account, resp *http.Response, raw []byte, token, kind, fallback string, fileContext *excelBPSFileOwnershipContext) {
	// Risk control is independent of diagnostic logging. Reuse the native mark
	// after BPS credential redaction; handlers own session TTL and user exemptions.
	if cyber, _, _ := detectOpenAICyberPolicy(raw); cyber {
		markOpenAICyberPolicyEvent(c, []byte(excelBPSSanitizeErrorBody(string(raw), token, account)), resp.StatusCode, nil)
	}
	message, detail, upstreamDetail := fallback, "", ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		safeBody := excelBPSSanitizeErrorBody(string(raw), token, account)
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail, _ = sanitizeErrorBodyForStorage(safeBody, maxBytes)
		if fileContext != nil {
			var diagnostic map[string]any
			if json.Unmarshal([]byte(safeBody), &diagnostic) == nil && diagnostic != nil {
				diagnostic["file_ownership_context"] = fileContext
				if encoded, err := json.Marshal(diagnostic); err == nil {
					safeBody = string(encoded)
				}
			}
		}
		detail, _ = sanitizeErrorBodyForStorage(safeBody, maxBytes)
		if upstreamMessage := strings.TrimSpace(extractUpstreamErrorMessage([]byte(safeBody))); upstreamMessage != "" {
			message = truncateString(upstreamMessage, 2048)
		}
	}
	if kind != "failover" && kind != "bridge_error" {
		setOpsUpstreamError(c, resp.StatusCode, message, detail)
	}
	if kind == "bridge_error" {
		upstreamDetail = "" // The provider did not generate the bridge diagnostic.
		// Preserve earlier attempts in the timeline, without attributing this
		// final converter failure to a previous provider rejection.
		c.Set(OpsUpstreamStatusCodeKey, 0)
		c.Set(OpsUpstreamErrorMessageKey, "")
		c.Set(OpsUpstreamErrorDetailKey, "")
		c.Set(OpsSkipPassthroughKey, false)
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
		UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
		UpstreamURL: basispoints.ResponsesURL, Kind: kind,
		Message: message, Detail: detail, UpstreamResponseBody: upstreamDetail,
	})
}

// Missing trace is not evidence of safety. Standard net/http emits GetConn
// before dialing and GotConn before handing a connection to request writing.
// Keep evidence for the whole attempt, including any internal reconnects.
type excelBPSWriteEvidence struct{ transportdiag.Trace }

func (e *excelBPSWriteEvidence) request(req *http.Request) *http.Request {
	req = e.Request(req)
	if req.Body != nil {
		req.Body = &excelBPSTrackedBody{ReadCloser: req.Body, mark: e.MarkBodyRead}
	}
	if getBody := req.GetBody; getBody != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			body, err := getBody()
			if err != nil {
				return nil, err
			}
			return &excelBPSTrackedBody{ReadCloser: body, mark: e.MarkBodyRead}, nil
		}
	}
	return req
}
func (e *excelBPSWriteEvidence) unsent() bool { return e.DefinitelyUnsent() }

type excelBPSTrackedBody struct {
	io.ReadCloser
	mark func()
}

func (b *excelBPSTrackedBody) Read(p []byte) (int, error) {
	if len(p) > 0 {
		b.mark()
	}
	return b.ReadCloser.Read(p)
}

// BPS uses the account's existing proxy. A submitted generation is never replayed
// by this transport layer; native scheduler failover handles explicit rejections.
func (s *OpenAIGatewayService) doExcelBPSRequest(ctx context.Context, c *gin.Context, account *Account, scope string, body []byte, token, accountID string) (*http.Response, string, error) {
	response, proxy, err := s.doExcelBPSRequestTo(ctx, c, account, scope, basispoints.ResponsesURL, func(ctx context.Context) (*http.Request, error) {
		return newExcelBPSRequest(ctx, body, token, accountID)
	})
	if err == nil {
		s.guardExcelBPSProgress(ctx, response)
	}
	return response, proxy, err
}

func (s *OpenAIGatewayService) doExcelBPSRequestTo(ctx context.Context, c *gin.Context, account *Account, scope, upstreamURL string, build func(context.Context) (*http.Request, error)) (*http.Response, string, error) {
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	if err := ctx.Err(); err != nil {
		return nil, proxy, err
	}
	req, err := build(ctx)
	if err != nil {
		return nil, proxy, err
	}
	if c != nil {
		c.Set("excel_bps_upstream_attempt", c.GetInt("excel_bps_upstream_attempt")+1)
	}
	evidence := &excelBPSWriteEvidence{}
	req = req.WithContext(context.WithValue(req.Context(), vendorAuthorizationModeKey{}, VendorOpenAIModeBPS))
	resp, err := qualifiedAccountUpstream(s.accountRepo, s.httpUpstream, account, vendorDispatchGroup(c)).Do(evidence.request(req), proxy, account.ID, account.Concurrency)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		var admission *UpstreamFailoverError
		if IsTurnBudgetError(err) || (errors.As(err, &admission) && admission.Stage == "selection") {
			// The local dispatch guard did not contact the provider. Let the
			// existing admission/recovery owner record its own failure source.
			return nil, proxy, err
		}
		attempt := 1
		if c != nil {
			attempt = c.GetInt("excel_bps_upstream_attempt")
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, err, attempt,
			OpsUpstreamErrorEvent{UpstreamURL: upstreamURL, Stage: "transport"}, nil, evidence)
		return nil, proxy, &OpenAITransportAttemptError{Cause: err, Unsent: evidence.unsent()}
	}
	return resp, proxy, nil
}

func recordExcelBPSTransportFailure(ctx context.Context, c *gin.Context, account *Account, scope string, err error, attempt int, event OpsUpstreamErrorEvent, diagnostics map[string]any, evidence ...*excelBPSWriteEvidence) {
	if isExcelBPSClientCancellation(ctx, c, err) {
		logger.FromContext(ctx).Info("excel_bps.client_canceled",
			zap.Int64("account_id", account.ID), zap.String("stage", event.Stage))
		return
	}
	kind := transportdiag.Classify(err)
	digest := sha256.Sum256([]byte(scope))
	sessionHash := hex.EncodeToString(digest[:8])
	if diagnostics == nil {
		diagnostics = make(map[string]any)
	}
	if diagnostics["stream_end_reason"] == "first_output_timeout" {
		diagnostics["transport_error_kind"] = kind
		kind = "first_output_timeout" // guard cancellation is not a client cancel
	}
	diagnostics["error_kind"], diagnostics["error_type"] = kind, fmt.Sprintf("%T", err)
	diagnostics["session_hash"], diagnostics["attempt"], diagnostics["retry_before_send"] = sessionHash, attempt, false
	if len(evidence) > 0 && evidence[0] != nil {
		diagnostics["transport"] = evidence[0].Snapshot()
	}
	detail, _ := json.Marshal(diagnostics)
	message := "Excel BPS " + event.Stage + " failed: " + kind
	// Keep UI client errors generic; persist only explicitly safe diagnostics.
	setOpsUpstreamError(c, event.UpstreamStatusCode, message, string(detail))
	event.Platform, event.AccountID, event.AccountName = account.Platform, account.ID, account.Name
	event.ProxyID, event.ProxyName = opsUpstreamProxyAttribution(account)
	event.Kind, event.Scope, event.Reason = "request_error", "excel_bps", kind
	event.Message, event.Detail = message, string(detail)
	appendOpsUpstreamError(c, event)
	logger.FromContext(ctx).Warn("excel_bps.transport_failed",
		zap.Int64("account_id", account.ID), zap.String("stage", event.Stage),
		zap.String("error_kind", kind), zap.String("error_type", fmt.Sprintf("%T", err)),
		zap.String("session_hash", sessionHash),
		zap.Int("attempt", attempt), zap.Bool("retry_before_send", false),
		zap.Any("diagnostics", diagnostics))
}

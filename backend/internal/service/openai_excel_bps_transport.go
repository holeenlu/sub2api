package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// excelBPSForwardError retains the client-safe code after the response is written.
type excelBPSForwardError struct{ code string }

func (e *excelBPSForwardError) Error() string { return "excel BPS: " + e.code }

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

// Local provenance is kept separately from the sanitized upstream body.
// All fields are fixed phase names or counts; no request values are stored.
type excelBPSFileOwnershipContext struct {
	basispoints.FileReferenceCounts
	Stage string `json:"stage"`
	// Kept as zero for diagnostic compatibility after removing the ID cache.
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
	if kind != "failover" {
		setOpsUpstreamError(c, resp.StatusCode, message, detail)
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
	if err := TakeOpenAITurnAttempt(ctx); err != nil {
		return nil, proxy, err
	}
	if c != nil {
		c.Set("excel_bps_upstream_attempt", c.GetInt("excel_bps_upstream_attempt")+1)
	}
	evidence := &excelBPSWriteEvidence{}
	resp, err := qualifiedAccountUpstream(s.accountRepo, s.httpUpstream, account, vendorDispatchGroup(c)).Do(evidence.request(req), proxy, account.ID, account.Concurrency)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		attempt := 1
		if c != nil {
			attempt = c.GetInt("excel_bps_upstream_attempt")
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, err, attempt,
			OpsUpstreamErrorEvent{UpstreamURL: upstreamURL, Stage: "transport"}, nil, evidence)
		return nil, proxy, err
	}
	return resp, proxy, nil
}

func recordExcelBPSTransportFailure(ctx context.Context, c *gin.Context, account *Account, scope string, err error, attempt int, event OpsUpstreamErrorEvent, diagnostics map[string]any, evidence ...*excelBPSWriteEvidence) {
	if isExcelBPSClientCancellation(c, err) {
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

package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"github.com/Wei-Shaw/sub2api/internal/gateway/protocol/sse"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const excelBPS403RecoveryScanInterval = time.Minute
const excelBPS403RecoveryTimeout = 45 * time.Second
const excelBPS403RecoveryConcurrency = 3

// Claims persist before network I/O, preventing duplicate attempts across
// instances and preserving the configured interval after a restart. Claims also
// persist an unknown consumption fact atomically before network I/O. Result and
// restore writes verify immutable evidence; their bool reports only UI/state
// changes, while a nil error confirms the original probe's evidence is durable.
type AccountExcelBPSRecoveryRepository interface {
	ClaimExcelBPS403Probe(context.Context, *Account, time.Time) (bool, error)
	RestoreExcelBPSAfter403(context.Context, *Account, ExcelBPS403ProbeResult) (bool, error)
	RecordExcelBPS403ProbeResult(context.Context, *Account, ExcelBPS403ProbeResult) (bool, error)
}

// The real repository revalidates the durable claim, grant and policy both
// after credential preparation and immediately before the restore transaction.
type AccountExcelBPSRecoveryValidator interface {
	ReadExcelBPSRecoverySnapshot(context.Context, *Account, string) (*Account, error)
	ValidateExcelBPSRecovery(context.Context, *Account) (bool, error)
}

func (a *Account) IsExcelBPS403RecoveryPending() bool {
	if a != nil && excelMapString(a.Extra, ExcelBPSAuthorizationErrorKey) != "" {
		return false
	}
	if a != nil {
		if record, ok := a.Extra[ExcelBPSFailureKey].(map[string]any); ok &&
			(record["reauth_required"] == true || record["recovery_cancelled"] == true ||
				record["source"] != "system" || record["state"] != "disabled") {
			return false
		}
	}
	if a.IsOpenAICloudConfigured() || !a.IsExcelBPSEligible() || !a.IsActive() || !a.Schedulable || a.IsExcelBPSEnabled() {
		return false
	}
	if a.Extra[ExcelBPSAutoRecoverOn403Key] != true || a.Extra["openai_excel_bps_auto_disable_on_403"] != true {
		return false
	}
	at, _ := a.Extra[ExcelBPS403DisabledAtKey].(string)
	_, err := time.Parse(time.RFC3339Nano, at)
	return err == nil && excelBPS403RecoveryModel(a) != ""
}

func (a *Account) ExcelBPS403RecoveryDue(now time.Time) bool {
	if !a.IsExcelBPS403RecoveryPending() || (a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt)) {
		return false
	}
	if record, ok := a.Extra[ExcelBPSFailureKey].(map[string]any); ok {
		if raw, present := record["next_probe_at"]; present && raw != nil {
			text, ok := raw.(string)
			next, err := time.Parse(time.RFC3339Nano, text)
			if !ok || err != nil || now.Before(next) {
				return false
			}
		}
	}
	disabledAtText, ok := a.Extra[ExcelBPS403DisabledAtKey].(string)
	if !ok {
		return false
	}
	disabledAt, err := time.Parse(time.RFC3339Nano, disabledAtText)
	if err != nil {
		return false
	}
	last := disabledAt
	if raw, exists := a.Extra[ExcelBPS403LastProbeAtKey]; exists {
		text, ok := raw.(string)
		at, err := time.Parse(time.RFC3339Nano, text)
		if !ok || err != nil {
			return false
		}
		if at.After(last) {
			last = at
		}
	}
	return !now.Before(last.Add(a.ExcelBPS403RecoveryInterval()))
}

func cloneExcelBPSRecoveryExtra(extra map[string]any) map[string]any {
	cloned := make(map[string]any, len(extra)+1)
	for key, value := range extra {
		cloned[key] = value
	}
	return cloned
}

func excelBPS403RecoveryModel(a *Account) string {
	raw := a.Extra["openai_excel_bps_models"]
	var models []string
	switch values := raw.(type) {
	case []string:
		models = values
	case []any:
		for _, value := range values {
			if model, ok := value.(string); ok {
				models = append(models, model)
			}
		}
	}
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" && !catalog.IsGPTImageGenerationModel(model) {
			return model
		}
	}
	return ""
}

func (s *OpenAIGatewayService) StartBPS403Recovery(parent context.Context) error {
	if s == nil || s.accountRepo == nil {
		return nil
	}
	if _, ok := s.accountRepo.(AccountExcelBPSRecoveryRepository); !ok {
		return nil
	}
	s.excelBPSRecoveryMu.Lock()
	defer s.excelBPSRecoveryMu.Unlock()
	if err := parent.Err(); err != nil {
		return err
	}
	if s.excelBPSRecoveryStopped {
		return errors.New("BPS recovery stopped")
	}
	if s.excelBPSRecoveryCancel != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.excelBPSRecoveryCancel, s.excelBPSRecoveryDone = cancel, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(excelBPS403RecoveryScanInterval)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			s.syncExcelBPS403Recovery(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func (s *OpenAIGatewayService) StopBPS403Recovery(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.excelBPSRecoveryMu.Lock()
	s.excelBPSRecoveryStopped = true
	cancel, done := s.excelBPSRecoveryCancel, s.excelBPSRecoveryDone
	s.excelBPSRecoveryMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *OpenAIGatewayService) syncExcelBPS403Recovery(ctx context.Context) {
	if !s.excelBPSGloballyEnabled(ctx) {
		return
	}
	defer func() {
		if recover() != nil {
			logger.FromContext(ctx).Error("excel_bps.recovery_scan_panicked")
		}
	}()
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	accounts, err := s.accountRepo.ListByPlatform(queryCtx, PlatformOpenAI)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			logger.FromContext(ctx).Warn("excel_bps.recovery_scan_failed")
		}
		return
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, excelBPS403RecoveryConcurrency)
	for i := range accounts {
		account := &accounts[i]
		if !account.ExcelBPS403RecoveryDue(time.Now()) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case slots <- struct{}{}:
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			s.recoverExcelBPS403Account(ctx, account, time.Now())
		}()
	}
}

func (s *OpenAIGatewayService) recoverExcelBPS403Account(ctx context.Context, account *Account, now time.Time) {
	if s == nil || account == nil || ctx.Err() != nil {
		return
	}
	defer func() {
		if recover() != nil {
			logger.FromContext(ctx).Error("excel_bps.recovery_probe_panicked", zap.Int64("account_id", account.ID))
		}
	}()
	repo, ok := s.accountRepo.(AccountExcelBPSRecoveryRepository)
	if !ok || !account.ExcelBPS403RecoveryDue(now) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, excelBPS403RecoveryTimeout)
	defer cancel()
	claimed, err := repo.ClaimExcelBPS403Probe(ctx, account, now)
	if err != nil {
		logger.FromContext(ctx).Warn("excel_bps.recovery_claim_failed", zap.Int64("account_id", account.ID))
		return
	}
	if !claimed {
		return
	}
	snapshot := *account
	snapshot.Extra = cloneExcelBPSRecoveryExtra(account.Extra)
	snapshot.Extra[ExcelBPS403LastProbeAtKey] = now.UTC().Format(time.RFC3339Nano)
	if validator, ok := s.accountRepo.(AccountExcelBPSRecoveryValidator); ok {
		fresh, err := validator.ReadExcelBPSRecoverySnapshot(ctx, &snapshot, snapshot.Extra[ExcelBPS403LastProbeAtKey].(string))
		if err != nil || fresh == nil {
			return
		}
		snapshot = *fresh
	}
	started := time.Now()
	usage, probeErr := s.probeExcelBPS403Recovery(ctx, &snapshot, func(fresh *Account) { snapshot = *fresh })
	if probeErr == nil && ctx.Err() != nil {
		probeErr = ctx.Err()
	}
	if probeErr == nil && !s.excelBPSRecoveryStillAuthorized(ctx, &snapshot) {
		probeErr = &excelBPS403ProbeError{Stage: "restore", Reason: "authorization_changed"}
	}
	outcome := excelBPS403ProbeResult(probeErr, started)
	outcome.Usage = usage
	outcome.DisabledAt, _ = snapshot.Extra[ExcelBPS403DisabledAtKey].(string)
	outcome.AttemptedAt, _ = snapshot.Extra[ExcelBPS403LastProbeAtKey].(string)
	recordCtx, stopRecord := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stopRecord()
	if probeErr != nil {
		changed, recordErr := repo.RecordExcelBPS403ProbeResult(recordCtx, &snapshot, outcome)
		recordExcelBPSRecoveryEvent("excel_bps.recovery_probe_failed", account.ID, map[string]any{"stage": outcome.Stage, "reason": outcome.Reason, "http_status": outcome.HTTPStatus, "duration_ms": outcome.DurationMS, "result_saved": recordErr == nil, "snapshot_updated": changed, "disabled_at": outcome.DisabledAt, "attempted_at": outcome.AttemptedAt, "upstream_request_id": outcome.UpstreamRequestID, "error_code": outcome.ErrorCode, "error_type": outcome.ErrorType})
		logger.FromContext(ctx).Warn("excel_bps.recovery_probe_failed", zap.Bool(logger.OpsSystemLogSkipField, true),
			zap.Int64("account_id", account.ID), zap.String("stage", outcome.Stage),
			zap.String("reason", outcome.Reason), zap.Int("http_status", outcome.HTTPStatus),
			zap.Int64("duration_ms", outcome.DurationMS), zap.Bool("result_saved", recordErr == nil),
			zap.Bool("snapshot_updated", changed))
		if recordErr != nil {
			logger.FromContext(ctx).Warn("excel_bps.recovery_result_save_failed", zap.Int64("account_id", account.ID))
		}
		return
	}
	changed, err := repo.RestoreExcelBPSAfter403(ctx, &snapshot, outcome)
	if err != nil {
		outcome.Status, outcome.Stage, outcome.Reason = "failed", "restore", "state_write_failed"
		if _, recordErr := repo.RecordExcelBPS403ProbeResult(recordCtx, &snapshot, outcome); recordErr != nil {
			logger.FromContext(ctx).Warn("excel_bps.recovery_result_save_failed", zap.Int64("account_id", account.ID))
		}
		logger.FromContext(ctx).Warn("excel_bps.recovery_restore_failed", zap.Int64("account_id", account.ID))
	} else if changed {
		recordExcelBPSRecoveryEvent("excel_bps.recovery_restored", account.ID, map[string]any{"from": "probing", "to": "enabled", "disabled_at": outcome.DisabledAt, "attempted_at": outcome.AttemptedAt, "duration_ms": outcome.DurationMS})
		logger.FromContext(ctx).Info("excel_bps.recovery_restored", zap.Bool(logger.OpsSystemLogSkipField, true), zap.Int64("account_id", account.ID))
	} else {
		outcome.Status, outcome.Stage, outcome.Reason = "failed", "restore", "claim_changed"
		if _, recordErr := repo.RecordExcelBPS403ProbeResult(recordCtx, &snapshot, outcome); recordErr != nil {
			logger.FromContext(ctx).Warn("excel_bps.recovery_result_save_failed", zap.Int64("account_id", account.ID))
		}
		logger.FromContext(ctx).Info("excel_bps.recovery_superseded", zap.Int64("account_id", account.ID))
	}
}

// Probe the actual authenticated BPS Responses endpoint without enabling
// production routing, replaying user requests or applying 403 group actions.
func (s *OpenAIGatewayService) probeExcelBPS403Recovery(ctx context.Context, account *Account, refreshed ...func(*Account)) (*ExcelBPS403ProbeUsage, error) {
	claim, _ := account.Extra[ExcelBPS403LastProbeAtKey].(string)
	claimedAt, _ := time.Parse(time.RFC3339Nano, claim)
	observation := bpsAccountProbeObservation{usage: *ExcelBPS403ProbeClaimResult(account, claimedAt).Usage}
	usage := &observation.usage
	usage.SendState = "not_sent"
	ctx = context.WithValue(ctx, excelBPSRecoveryProbeKey{}, true)
	model := excelBPS403RecoveryModel(account)
	if model == "" || s.httpUpstream == nil {
		return usage, &excelBPS403ProbeError{Stage: "prepare", Reason: "probe_unavailable"}
	}
	ctx = withExcelBPSUpstreamModel(ctx, model)
	token, preparedAccount, err := s.getExcelBPSAccessTokenSnapshot(ctx, account)
	if err != nil {
		return usage, &excelBPS403ProbeError{Stage: "authorization", Reason: "authorization_failed", cause: err}
	}
	account = preparedAccount
	if len(refreshed) > 0 {
		refreshed[0](account)
	}
	if !s.excelBPSRecoveryStillAuthorized(ctx, account) {
		return usage, &excelBPS403ProbeError{Stage: "authorization", Reason: "authorization_changed"}
	}
	until, err := s.excelBPSRateLimitDeadline(ctx, account.ID, excelBPSRateLimitScope{model: model, endpoint: "/basispoints/api/responses"})
	if err != nil {
		return usage, &excelBPS403ProbeError{Stage: "prepare", Reason: "state_unavailable", cause: err}
	}
	if until.After(time.Now()) {
		return usage, &excelBPS403ProbeError{Stage: "prepare", Reason: "rate_limited", HTTPStatus: http.StatusTooManyRequests}
	}
	modelUntil, err := s.excelBPSModelCooldownDeadline(ctx, account.ID, model)
	if err != nil {
		return usage, &excelBPS403ProbeError{Stage: "prepare", Reason: "state_unavailable", cause: err}
	}
	if modelUntil.After(time.Now()) {
		return usage, &excelBPS403ProbeError{Stage: "prepare", Reason: "model_unavailable"}
	}
	if validator, ok := s.accountRepo.(AccountExcelBPSDispatchRepository); ok {
		ctx, err = s.ExcelBPSDispatchContext(ctx, account, token)
		if err != nil {
			return usage, err
		}
		if err := validator.ValidateExcelBPSDispatch(ctx, account, nil, model); err != nil {
			return usage, err
		}
	}
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return usage, &excelBPS403ProbeError{Stage: "authorization", Reason: "identity_unavailable"}
	}
	nonce := uuid.NewString()
	raw, err := json.Marshal(map[string]any{
		"model": model, "stream": true, "store": false,
		"reasoning": map[string]any{"effort": "low"},
		"input":     []any{bpsProbeUserMessage("Reply with exactly this nonce and nothing else: " + nonce)},
	})
	if err != nil {
		return usage, err
	}
	body, _, err := basispoints.Prepare(raw, "", nil)
	if err != nil {
		return usage, err
	}
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}

	ctx = WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileExcelBPS))
	req, err := newExcelBPSRequest(ctx, body, token, accountID)
	if err != nil {
		return usage, err
	}
	if !s.excelBPSRecoveryStillAuthorized(ctx, account) {
		return usage, &excelBPS403ProbeError{Stage: "authorization", Reason: "claim_changed"}
	}
	usage.SendState = "ambiguous"
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	if resp != nil {
		usage.SendState = "sent"
	}
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return usage, &excelBPS403ProbeError{Stage: "request", Reason: "transport_failed", cause: err}
	}
	if resp == nil || resp.Body == nil {
		return usage, &excelBPS403ProbeError{Stage: "request", Reason: "empty_response"}
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		// Some rejected requests still carry a structured Responses terminal.
		// Preserve its measurements without treating the HTTP rejection as health.
		_ = observation.observe(raw)
		if failure := basispoints.ParseHTTPFailure(resp.StatusCode, raw); failure != nil {
			if err := s.observeExcelBPSRecoveryAuthorization(ctx, account, failure, resp.Header, token, model); err != nil {
				return usage, &excelBPS403ProbeError{Stage: "request", Reason: "state_write_failed", HTTPStatus: resp.StatusCode, cause: err}
			}
		} else {
			s.handleExcelBPSUnauthorized(ctx, account, resp.StatusCode, resp.Header, raw, token)
		}
		if err := s.observeExcelBPSRateLimit(ctx, account, resp.StatusCode, resp.Header, raw, model, basispoints.ResponsesURL); err != nil {
			return usage, &excelBPS403ProbeError{Stage: "request", Reason: "state_write_failed", HTTPStatus: resp.StatusCode, cause: err}
		}
		return usage, excelBPSProbeHTTPFailure(&http.Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: io.NopCloser(bytes.NewReader(raw))})
	}
	const maxProbeBytes = 2 << 20
	// Classify terminals as they arrive. A peer may keep the SSE connection
	// open after rejecting the probe; waiting for EOF would turn that known
	// rejection into a timeout and miss its authorization/cooldown observation.
	limited := &io.LimitedReader{R: resp.Body, N: maxProbeBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Split(sse.ScanLines())
	scanner.Buffer(make([]byte, 4096), maxProbeBytes+1)
	for scanner.Scan() {
		if limited.N == 0 {
			return usage, &excelBPS403ProbeError{Stage: "response", Reason: "response_too_large", HTTPStatus: resp.StatusCode}
		}
		if bytes.HasPrefix(scanner.Bytes(), []byte("data:")) {
			payload := bytes.TrimSpace(scanner.Bytes()[len("data:"):])
			if bytes.Equal(payload, []byte("[DONE]")) {
				continue
			}
			observationErr := observation.observe(payload)
			if err := s.observeExcelBPSRateLimit(ctx, account, 0, resp.Header, payload, model, basispoints.ResponsesURL); err != nil {
				return usage, &excelBPS403ProbeError{Stage: "response", Reason: "state_write_failed", HTTPStatus: resp.StatusCode, cause: err}
			}
			if failure := basispoints.ParseUpstreamFailure(payload); failure != nil {
				// Reuse normal BPS terminal classification without replaying
				// generation or enabling the disabled route. The cause retains
				// semantic status (e.g. 401); HTTPStatus remains the real HTTP 200.
				if err := s.observeExcelBPSRecoveryAuthorization(ctx, account, failure, resp.Header, token, model); err != nil {
					return usage, &excelBPS403ProbeError{Stage: "response", Reason: "state_write_failed", HTTPStatus: resp.StatusCode, cause: err}
				}
				return usage, &excelBPS403ProbeError{Stage: "response", Reason: "upstream_rejected", HTTPStatus: resp.StatusCode, cause: failure}
			}
			if observationErr != nil {
				return usage, &excelBPS403ProbeError{Stage: "response", Reason: "invalid_response", HTTPStatus: resp.StatusCode, cause: observationErr}
			}
		}
	}
	if limited.N == 0 {
		return usage, &excelBPS403ProbeError{Stage: "response", Reason: "response_too_large", HTTPStatus: resp.StatusCode}
	}
	if err := scanner.Err(); err != nil {
		return usage, &excelBPS403ProbeError{Stage: "response", Reason: "read_failed", HTTPStatus: resp.StatusCode, cause: err}
	}
	if !observation.terminal {
		return usage, &excelBPS403ProbeError{Stage: "response", Reason: "invalid_response", HTTPStatus: resp.StatusCode}
	}
	if !bpsProbeExactText(observation.response, nonce) {
		return usage, &excelBPS403ProbeError{Stage: "response", Reason: "response_mismatch", HTTPStatus: resp.StatusCode}
	}

	return usage, nil
}

// A probe shares the normal authorization observer. Model-cooldown writes use
// its context-aware port so persistence failure remains visible to the recovery
// owner instead of being logged and discarded by the foreground convenience API.
func (s *OpenAIGatewayService) observeExcelBPSRecoveryAuthorization(ctx context.Context, account *Account, failure *basispoints.UpstreamFailure, headers http.Header, token, model string) error {
	if failure != nil && !failure.Local && isExcelBPSModelPermissionError(failure.Status, failure.Code) {
		_, err := s.coolDownExcelBPSModelWithContext(ctx, account, model)
		return err
	}
	s.observeExcelBPSTerminalAuthorization(ctx, account, failure, headers, token, model)
	return nil
}

func (s *OpenAIGatewayService) excelBPSRecoveryStillAuthorized(ctx context.Context, account *Account) bool {
	if ctx.Err() != nil || !account.IsExcelBPS403RecoveryPending() {
		return false
	}
	if s.settingService != nil {
		if s.settingService.settingRepo == nil {
			return false
		}
		// Bypass the routing cache for the rare background recovery write.
		value, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyExcelBPSEnabled)
		if err != nil || value != "true" {
			return false
		}
	}
	if validator, ok := s.accountRepo.(AccountExcelBPSRecoveryValidator); ok {
		valid, err := validator.ValidateExcelBPSRecovery(ctx, account)
		return err == nil && valid
	}
	return true
}

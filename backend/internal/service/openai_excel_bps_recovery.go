package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	excelBPS403RecoveryScanInterval = time.Minute
	excelBPS403RecoveryTimeout      = 45 * time.Second
	excelBPS403RecoveryConcurrency  = 3
)

// AccountExcelBPSRecoveryRepository provides a database claim and a guarded
// restore so multiple application instances never probe the same account at once.
type AccountExcelBPSRecoveryRepository interface {
	ClaimExcelBPS403Probe(context.Context, *Account, time.Time) (bool, error)
	RestoreExcelBPSAfter403(context.Context, *Account) (bool, error)
}

func (a *Account) IsExcelBPS403RecoveryPending() bool {
	if a == nil || a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || a.IsShadow() ||
		a.IsOpenAIAgentIdentity() || a.IsOpenAIPersonalAccessToken() || !a.IsActive() || !a.Schedulable || a.IsExcelBPSEnabled() {
		return false
	}
	if a.Extra[ExcelBPSAutoRecoverOn403Key] != true || a.Extra["openai_excel_bps_auto_disable_on_403"] != true {
		return false
	}
	at, _ := a.Extra[ExcelBPS403DisabledAtKey].(string)
	_, err := time.Parse(time.RFC3339Nano, at)
	return err == nil
}

func (a *Account) ExcelBPS403RecoveryDue(now time.Time) bool {
	if !a.IsExcelBPS403RecoveryPending() || (a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt)) {
		return false
	}
	disabledAt, _ := time.Parse(time.RFC3339Nano, a.Extra[ExcelBPS403DisabledAtKey].(string))
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
	raw, scoped := a.Extra["openai_excel_bps_models"]
	if !scoped {
		return openai.DefaultTestModel
	}
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
		if model = strings.TrimSpace(model); model != "" {
			return model
		}
	}
	return ""
}

func (s *OpenAIGatewayService) StartBPS403Recovery() {
	if s == nil || s.accountRepo == nil {
		return
	}
	if _, ok := s.accountRepo.(AccountExcelBPSRecoveryRepository); !ok {
		return
	}
	s.excelBPSRecoveryMu.Lock()
	defer s.excelBPSRecoveryMu.Unlock()
	if s.excelBPSRecoveryStopped || s.excelBPSRecoveryCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.excelBPSRecoveryCancel, s.excelBPSRecoveryDone = cancel, done
	go func() {
		defer close(done)
		s.syncExcelBPS403Recovery(ctx)
		ticker := time.NewTicker(excelBPS403RecoveryScanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.syncExcelBPS403Recovery(ctx)
			}
		}
	}()
}

func (s *OpenAIGatewayService) StopBPS403Recovery() {
	if s == nil {
		return
	}
	s.excelBPSRecoveryMu.Lock()
	s.excelBPSRecoveryStopped = true
	cancel, done := s.excelBPSRecoveryCancel, s.excelBPSRecoveryDone
	s.excelBPSRecoveryMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *OpenAIGatewayService) syncExcelBPS403Recovery(ctx context.Context) {
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
	slots := make(chan struct{}, excelBPS403RecoveryConcurrency)
scan:
	for i := range accounts {
		account := &accounts[i]
		if !account.ExcelBPS403RecoveryDue(time.Now()) {
			continue
		}
		select {
		case <-ctx.Done():
			break scan
		case slots <- struct{}{}:
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			s.recoverExcelBPS403Account(ctx, account, time.Now())
		}()
	}
	wg.Wait()
}

func (s *OpenAIGatewayService) recoverExcelBPS403Account(ctx context.Context, account *Account, now time.Time) {
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
	if err != nil || !claimed {
		return
	}
	snapshot := *account
	snapshot.Extra = cloneExcelBPSRecoveryExtra(account.Extra)
	snapshot.Extra[ExcelBPS403LastProbeAtKey] = now.UTC().Format(time.RFC3339Nano)
	if err := s.probeExcelBPS403Recovery(ctx, &snapshot); err != nil {
		logger.FromContext(ctx).Info("excel_bps.recovery_probe_failed", zap.Int64("account_id", account.ID))
		return
	}
	changed, err := repo.RestoreExcelBPSAfter403(ctx, &snapshot)
	if err != nil {
		logger.FromContext(ctx).Warn("excel_bps.recovery_restore_failed", zap.Int64("account_id", account.ID))
	} else if changed {
		logger.FromContext(ctx).Info("excel_bps.recovery_restored", zap.Int64("account_id", account.ID))
	}
}

// Probe the authenticated BPS endpoint directly. It never routes user traffic,
// replays a user request, or applies the automatic 403 group action.
func (s *OpenAIGatewayService) probeExcelBPS403Recovery(ctx context.Context, account *Account) error {
	model := excelBPS403RecoveryModel(account)
	if model == "" || s.httpUpstream == nil {
		return errors.New("BPS recovery probe unavailable")
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return err
	}
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return errors.New("BPS recovery account identity unavailable")
	}
	nonce := uuid.NewString()
	raw, err := json.Marshal(map[string]any{
		"model": model, "stream": true, "store": false,
		"reasoning": map[string]any{"effort": "low"},
		"input":     []any{bpsProbeUserMessage("Reply with exactly this nonce and nothing else: " + nonce)},
	})
	if err != nil {
		return err
	}
	body, _, err := basispoints.Prepare(raw, "", nil)
	if err != nil {
		return err
	}
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileExcelBPS))
	req, err := newExcelBPSRequest(requestCtx, body, token, accountID)
	if err != nil {
		return err
	}
	// A recovery scan/claim can become stale while credentials and the request
	// are prepared. Read the primary immediately before sending, and do not
	// charge RPM or probe an account whose recovery policy or identity changed.
	latest, err := s.latestOpenAITurnAccount(ctx, nil, account)
	if err != nil {
		return err
	}
	if !latest.IsExcelBPS403RecoveryPending() ||
		openAITurnRouteFingerprint(latest) != openAITurnRouteFingerprint(account) ||
		latest.GetCredential("access_token") != account.GetCredential("access_token") ||
		!sameExcelBPSRecoveryPolicy(latest.Extra, account.Extra) {
		return denyOpenAITurn("account_binding_changed")
	}
	if err := s.acquireOpenAIRPMForSend(ctx, latest); err != nil {
		return err
	}
	resp, err := s.httpUpstream.Do(req, proxy, latest.ID, latest.Concurrency)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return err
	}
	if resp == nil || resp.Body == nil || resp.StatusCode != http.StatusOK {
		return errors.New("BPS recovery request rejected")
	}
	const maxProbeBytes = 2 << 20
	wire, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeBytes+1))
	if err != nil {
		return err
	}
	if len(wire) > maxProbeBytes {
		return errors.New("BPS recovery response too large")
	}
	response, err := parseBPSAccountProbeResponse(wire)
	if err != nil {
		return err
	}
	if !bpsProbeExactText(response, nonce) {
		return errors.New("BPS recovery response mismatch")
	}
	return nil
}

// Compare JSON values rather than Go representations: SQL decodes numbers as
// float64 and arrays as []any, while direct callers can supply ints/[]string.
func sameExcelBPSRecoveryPolicy(a, b map[string]any) bool {
	policy := func(extra map[string]any) []byte {
		values := make(map[string]any)
		for key, value := range extra {
			if strings.HasPrefix(key, "openai_excel_bps") {
				values[key] = value
			}
		}
		raw, _ := json.Marshal(values)
		return raw
	}
	return string(policy(a)) == string(policy(b))
}

package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const excelBPSModelPermissionCooldown = 30 * time.Minute
const excelBPSModelPermissionFilterReason = "excel_bps_model_not_allowed"
const excelBPSModelPermissionMaxEntries = 4096

const ExcelBPSModelPermissionReason = GatewayFailureReason("basispoints_model_access_changed")
const excelBPSModelPermissionClientMessage = "This model is not available on the account's Excel BPS endpoint"

func newExcelBPSModelPermissionFailoverError() *UpstreamFailoverError {
	return &UpstreamFailoverError{
		StatusCode: http.StatusForbidden, Stage: GatewayFailureStageInference,
		Scope: GatewayFailureScopeAccount, Reason: ExcelBPSModelPermissionReason,
		NextAccountAction: NextAccountRetry, ClientStatusCode: http.StatusForbidden,
		ClientMessage:            excelBPSModelPermissionClientMessage,
		SafeToFailoverAfterWrite: true, // only compact/SSE keepalives can have been written
	}
}

type excelBPSModelKey struct {
	accountID int64
	model     string
}
type excelBPSModelCooldowns struct {
	mu    sync.Mutex
	until map[excelBPSModelKey]time.Time
}

func isExcelBPSModelPermissionError(status int, code string) bool {
	return status == 403 && (code == "basispoints_model_access_changed" || code == "model_not_allowed")
}

// Store the actual upstream model, so aliases share a cooldown while native
// Codex requests and the account's other BPS models remain eligible.
func (s *OpenAIGatewayService) coolDownExcelBPSModel(account *Account, upstreamModel string) {
	if _, err := s.coolDownExcelBPSModelWithContext(context.Background(), account, upstreamModel); err != nil {
		logger.LegacyPrintf("service.openai_excel_bps", "persist model cooldown failed: account_id=%d err=%v", account.ID, err)
	}
}

func excelBPSModelPermissionScope(upstreamModel string) string {
	return ExcelBPSCooldownScopePrefix + "model_permission:" + base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(upstreamModel)))
}

// The context-aware entry point lets the failure owner retain a persistence
// error without changing a model rejection into an account authorization error.
func (s *OpenAIGatewayService) coolDownExcelBPSModelWithContext(ctx context.Context, account *Account, upstreamModel string) (time.Time, error) {
	if s == nil || account == nil || strings.TrimSpace(upstreamModel) == "" {
		return time.Time{}, nil
	}
	upstreamModel = strings.TrimSpace(upstreamModel)
	until := s.extendExcelBPSModelLocalCooldown(account.ID, upstreamModel, time.Now().Add(excelBPSModelPermissionCooldown))
	if repo, ok := s.accountRepo.(AccountExcelBPSCooldownRepository); ok {
		stateCtx, cancel := openAIAccountStateContext(ctx)
		defer cancel()
		saved, err := repo.ExtendExcelBPSCooldown(stateCtx, account.ID, excelBPSModelPermissionScope(upstreamModel), until)
		if err != nil {
			return until, fmt.Errorf("persist BPS model cooldown: %w", err)
		}
		until = s.extendExcelBPSModelLocalCooldown(account.ID, upstreamModel, saved)
	}
	return until, nil
}

func (s *OpenAIGatewayService) extendExcelBPSModelLocalCooldown(accountID int64, upstreamModel string, until time.Time) time.Time {
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now := time.Now()
	if cache.until == nil {
		cache.until = make(map[excelBPSModelKey]time.Time)
	}
	for key, expiry := range cache.until {
		if !now.Before(expiry) {
			delete(cache.until, key)
		}
	}
	key := excelBPSModelKey{accountID, upstreamModel}
	if current := cache.until[key]; current.After(until) {
		return current
	}
	if _, exists := cache.until[key]; !exists && len(cache.until) >= excelBPSModelPermissionMaxEntries {
		var oldest excelBPSModelKey
		var expiry time.Time
		for candidate, until := range cache.until {
			if expiry.IsZero() || until.Before(expiry) {
				oldest, expiry = candidate, until
			}
		}
		delete(cache.until, oldest)
	}
	cache.until[key] = until
	return until
}

func (s *OpenAIGatewayService) isExcelBPSModelCoolingDown(account *Account, requestedModel string) bool {
	if !s.excelBPSCandidateCooldownEnabled(account, requestedModel) {
		return false
	}
	model := account.GetMappedModel(requestedModel)
	if until := account.modelRateLimitResetAt(excelBPSModelPermissionScope(model)); until != nil && time.Now().Before(*until) {
		return true
	}
	return s.isExcelBPSUpstreamModelCoolingDown(account.ID, model)
}

func (s *OpenAIGatewayService) isExcelBPSUpstreamModelCoolingDown(accountID int64, model string) bool {
	// This ID-only catalog hint cannot load a snapshot. Preparation and send
	// boundaries use the typed reader, which preserves dependency errors.
	return !s.excelBPSModelLocalCooldownDeadline(accountID, model).IsZero()
}

func (s *OpenAIGatewayService) excelBPSModelCooldownDeadline(ctx context.Context, accountID int64, model string) (time.Time, error) {
	if s == nil || strings.TrimSpace(model) == "" {
		return time.Time{}, nil
	}
	model = strings.TrimSpace(model)
	until := s.excelBPSModelLocalCooldownDeadline(accountID, model)
	stored, err := s.excelBPSStoredCooldownDeadline(ctx, accountID, []string{excelBPSModelPermissionScope(model)})
	if stored.After(until) {
		until = stored
	}
	return until, err
}

func (s *OpenAIGatewayService) excelBPSModelLocalCooldownDeadline(accountID int64, model string) time.Time {
	if s == nil || strings.TrimSpace(model) == "" {
		return time.Time{}
	}
	model = strings.TrimSpace(model)
	key := excelBPSModelKey{accountID, model}
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	until, exists := cache.until[key]
	active := exists && time.Now().Before(until)
	if exists && !active {
		delete(cache.until, key)
	}
	cache.mu.Unlock()
	if !active {
		return time.Time{}
	}
	return until
}

package service

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
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
	if s == nil || account == nil || strings.TrimSpace(upstreamModel) == "" {
		return
	}
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
	key := excelBPSModelKey{account.ID, upstreamModel}
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
	cache.until[key] = now.Add(excelBPSModelPermissionCooldown)
}

func (s *OpenAIGatewayService) isExcelBPSModelCoolingDown(account *Account, requestedModel string) bool {
	if s == nil || account == nil || !account.IsExcelBPSEnabledForModel(requestedModel) {
		return false
	}
	return s.isExcelBPSUpstreamModelCoolingDown(account.ID, account.GetMappedModel(requestedModel)) && s.excelBPSGloballyEnabled(context.Background())
}

func (s *OpenAIGatewayService) isExcelBPSUpstreamModelCoolingDown(accountID int64, model string) bool {
	key := excelBPSModelKey{accountID, model}
	cache := &s.excelBPSModelCooldowns
	cache.mu.Lock()
	until, exists := cache.until[key]
	active := exists && time.Now().Before(until)
	if exists && !active {
		delete(cache.until, key)
	}
	cache.mu.Unlock()
	return active
}

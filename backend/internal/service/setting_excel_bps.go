package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
)

const SettingKeyExcelBPSEnabled = "excel_bps_enabled"

// excelBPSEnabledCacheTTL bounds how long a process reuses the global BPS
// switch. Scheduling filters and cooldown checks consult it per candidate,
// some without a request context; a short TTL keeps those off the database
// while a toggle on another instance still takes effect within seconds. A
// local update invalidates immediately.
var excelBPSEnabledCacheTTL = 2 * time.Second

type cachedExcelBPSEnabled struct {
	enabled   bool
	expiresAt time.Time
}

func (s *OpenAIGatewayService) excelBPSGloballyEnabled(ctx context.Context) bool {
	if s == nil || s.settingService == nil {
		return false
	}
	return s.settingService.IsExcelBPSEnabled(ctx)
}

func (s *SettingService) IsExcelBPSEnabled(ctx context.Context) bool {
	enabled, _ := s.excelBPSEnabled(ctx)
	return enabled
}

func (s *SettingService) excelBPSEnabled(ctx context.Context) (bool, error) {
	if s == nil || s.settingRepo == nil {
		return false, nil
	}
	if cached, ok := s.excelBPSEnabledCache.Load().(*cachedExcelBPSEnabled); ok && cached != nil && time.Now().Before(cached.expiresAt) {
		return cached.enabled, nil
	}
	ctx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	defer cancel()
	value, err := s.settingRepo.GetValue(ctx, SettingKeyExcelBPSEnabled)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return false, nil
		}
		// Keep the existing fail-closed answer without caching a transient
		// database error as a settings decision.
		return false, err
	}
	enabled := value == "true"
	s.excelBPSEnabledCache.Store(&cachedExcelBPSEnabled{enabled: enabled, expiresAt: time.Now().Add(excelBPSEnabledCacheTTL)})
	return enabled, nil
}

// invalidateExcelBPSEnabledCache forces the next check to read the store.
func (s *SettingService) invalidateExcelBPSEnabledCache() {
	if s == nil {
		return
	}
	s.excelBPSEnabledCache.Store(&cachedExcelBPSEnabled{})
}

// Share the effective BPS route with continuation admission.
func (s *OpenAIGatewayService) IsExcelBPSRouteEnabled(ctx context.Context, account *Account, model string) bool {
	plan, err := s.planResponseExecution(ctx, account, model)
	return err == nil && plan.mode == session.ResponseModeBPS
}

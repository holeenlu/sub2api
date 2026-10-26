package service

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/google/uuid"
)

var ErrUsageExportTooLarge = infraerrors.BadRequest("USAGE_EXPORT_TOO_LARGE", "Export exceeds the configured row limit; select a smaller date range")

// Separate capability avoids expanding every scheduler/usage repository mock.
// The SQL repository shares filters and hydration with normal usage queries.
type usageExportRepository interface {
	StreamUsageLogs(context.Context, usagestats.UsageLogFilters, int, func([]UsageLog) error) error
}

func ProvideUsageService(repo UsageLogRepository, users UserRepository, client *dbent.Client, auth APIKeyAuthCacheInvalidator, locks LeaderLockCache, cfg *config.Config) *UsageService {
	s := NewUsageService(repo, users, client, auth)
	s.exportLocks = locks
	if cfg != nil {
		s.exportLimits = cfg.UsageExport
	}
	return s
}
func (s *UsageService) UsageExportTimeout() time.Duration {
	return time.Duration(s.exportLimits.Effective().TimeoutSeconds) * time.Second
}

func (s *UsageService) BoundUsageFilters(filters usagestats.UsageLogFilters, now time.Time) (usagestats.UsageLogFilters, error) {
	maxDays := s.exportLimits.Effective().MaxRangeDays
	end := now
	if filters.EndTime != nil {
		end = *filters.EndTime
	}
	start := end.AddDate(0, 0, -maxDays)
	if filters.StartTime != nil {
		start = *filters.StartTime
	}
	// Calendar arithmetic retains inclusive UI date semantics through DST.
	if !start.Before(end) || end.After(start.AddDate(0, 0, maxDays)) {
		return filters, infraerrors.BadRequest("USAGE_RANGE_LIMIT", fmt.Sprintf("Usage query range must be positive and at most %d days", maxDays))
	}
	filters.StartTime = &start
	filters.EndTime = &end
	return filters, nil
}
func (s *UsageService) ExportUsage(ctx context.Context, actorID int64, filters usagestats.UsageLogFilters, emit func([]UsageLog) error) error {
	if actorID <= 0 {
		return infraerrors.BadRequest("INVALID_EXPORT_ACTOR", "Export requires an authenticated user")
	}
	filters, err := s.BoundUsageFilters(filters, time.Now())
	if err != nil {
		return err
	}
	repo, ok := s.usageRepo.(usageExportRepository)
	if !ok || s.exportLocks == nil {
		return infraerrors.ServiceUnavailable("USAGE_EXPORT_UNAVAILABLE", "Usage export is unavailable")
	}
	timeout := s.UsageExportTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	owner := uuid.NewString()
	key := fmt.Sprintf("usage-export:user:%d", actorID)
	// Lease outlives the hard operation deadline; no renewal goroutine needed.
	acquired, err := s.exportLocks.TryAcquireLeaderLock(ctx, key, owner, timeout+30*time.Second)
	if err != nil {
		return infraerrors.ServiceUnavailable("USAGE_EXPORT_UNAVAILABLE", "Export coordination is unavailable")
	}
	if !acquired {
		return infraerrors.Conflict("USAGE_EXPORT_BUSY", "An export is already running for this user")
	}
	defer func() {
		releaseCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = s.exportLocks.ReleaseLeaderLock(releaseCtx, key, owner)
	}()
	return repo.StreamUsageLogs(ctx, filters, s.exportLimits.Effective().MaxRows, func(rows []UsageLog) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return emit(rows)
	})
}

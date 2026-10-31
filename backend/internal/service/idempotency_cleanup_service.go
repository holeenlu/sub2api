package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// IdempotencyCleanupService 定期清理已过期的幂等记录，避免表无限增长。
type IdempotencyCleanupService struct {
	repo     IdempotencyRepository
	interval time.Duration
	batch    int

	startOnce sync.Once
	work      backgroundWork
	startErr  error
}

func NewIdempotencyCleanupService(repo IdempotencyRepository, cfg *config.Config) *IdempotencyCleanupService {
	interval := 60 * time.Second
	batch := 500
	if cfg != nil {
		if cfg.Idempotency.CleanupIntervalSeconds > 0 {
			interval = time.Duration(cfg.Idempotency.CleanupIntervalSeconds) * time.Second
		}
		if cfg.Idempotency.CleanupBatchSize > 0 {
			batch = cfg.Idempotency.CleanupBatchSize
		}
	}
	return &IdempotencyCleanupService{
		repo:     repo,
		interval: interval,
		batch:    batch,
	}
}

func (s *IdempotencyCleanupService) Start(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	if err := s.work.bind(ctx, s.RequestStop); err != nil {
		return err
	}
	s.startOnce.Do(func() { s.startErr = s.work.launch(s.runLoop) })
	return s.startErr
}

func (s *IdempotencyCleanupService) RequestStop() {
	if s != nil {
		s.work.requestStop()
	}
}
func (s *IdempotencyCleanupService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.RequestStop()
	return s.work.join(ctx)
}
func (s *IdempotencyCleanupService) Drain(ctx context.Context) error { return s.Stop(ctx) }

func (s *IdempotencyCleanupService) runLoop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.cleanupOnceOwned(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *IdempotencyCleanupService) cleanupOnce() {
	parent, done, err := s.work.begin()
	if err != nil {
		return
	}
	defer done()
	s.cleanupOnceOwned(parent)
}
func (s *IdempotencyCleanupService) cleanupOnceOwned(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	deleted, err := s.repo.DeleteExpired(ctx, time.Now(), s.batch)
	if err != nil {
		logger.LegacyPrintf("service.idempotency_cleanup", "[IdempotencyCleanup] cleanup failed err=%v", err)
		return
	}
	if deleted > 0 {
		logger.LegacyPrintf("service.idempotency_cleanup", "[IdempotencyCleanup] cleaned expired records count=%d", deleted)
	}
}

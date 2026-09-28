package service

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"sync"
	"time"
)

func (s *ScheduledTestRunnerService) runPelicanPlan(ctx context.Context, plan *ScheduledTestPlan) {
	now := time.Now()
	next, err := nextPlanRun(plan, now)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "pelican plan=%d invalid config: %v", plan.ID, err)
		return
	}
	// Persisted lease prevents duplicate execution across ticks and server replicas.
	// It also recovers automatically after a process crash.
	until := now.Add(15 * time.Minute).Truncate(time.Microsecond)
	claimed, err := s.planRepo.ClaimPelican(ctx, plan, now, until, next)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "pelican plan=%d claim failed: %v", plan.ID, err)
	}
	if err != nil || !claimed {
		return
	}
	// Legacy rules can target API-key accounts, or an account can change type
	// after a rule is saved. Advance the claimed schedule without running a
	// probe or recording a misleading inconclusive quality round.
	if isOpenAICodexStateProbePlan(plan.PelicanConfig) && s.accountTestSvc != nil {
		account, lookupErr := s.accountTestSvc.accountRepo.GetByID(ctx, plan.AccountID)
		ignoreBPS := plan.PelicanConfig.Quality != nil && (plan.PelicanConfig.Quality.Action == QualityActionEnableBPS || plan.PelicanConfig.BPSRecoveryPending)
		if lookupErr != nil || openAICodexStateProbeUnsupportedReason(account, plan.ModelID, ignoreBPS) != "" {
			logger.LegacyPrintf("service.scheduled_test_runner", "state probe plan=%d account=%d skipped: account unavailable or unsupported", plan.ID, plan.AccountID)
			finishCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			if finishErr := s.planRepo.FinishPelican(finishCtx, plan.ID, until, time.Now()); finishErr != nil {
				logger.LegacyPrintf("service.scheduled_test_runner", "pelican plan=%d finish failed: %v", plan.ID, finishErr)
			}
			return
		}
		version := account.UpdatedAt
		plan.BPSAccountVersion = &version
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	results := make([]*ScheduledTestResult, plan.PelicanConfig.ParallelCount)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = s.runPelicanSample(runCtx, plan)
		}(i)
	}
	wg.Wait()
	// Persist timeout failures with a fresh context even after the request deadline.
	saveCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	qualityAction := ""
	if plan.PelicanConfig.Quality != nil {
		var actionErr error
		qualityAction, actionErr = s.planRepo.ApplyQualityOutcome(saveCtx, plan, until, qualityOutcome(results))
		if actionErr != nil {
			qualityAction = "action_error"
			logger.LegacyPrintf("service.scheduled_test_runner", "quality plan=%d action failed: %v", plan.ID, actionErr)
		}
	}
	for _, result := range results {
		result.QualityAction = qualityAction
		if plan.PelicanConfig.Quality != nil {
			result.QualityRoundID = until.Format(time.RFC3339Nano)
		}
		if err := s.scheduledSvc.SaveResult(saveCtx, plan.ID, plan.MaxResults, result); err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "pelican plan=%d save failed: %v", plan.ID, err)
		}
	}
	if err := s.planRepo.FinishPelican(saveCtx, plan.ID, until, time.Now()); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "pelican plan=%d finish failed: %v", plan.ID, err)
	}
}

// Each sample runs in its own goroutine, outside Gin recovery. Always return a
// result so an upstream adapter or judge panic cannot kill the process or leave
// the persistence loop with a nil entry. Panic values may contain request data.
func (s *ScheduledTestRunnerService) runPelicanSample(ctx context.Context, plan *ScheduledTestPlan) (result *ScheduledTestResult) {
	started := time.Now()
	failure := func(message string) *ScheduledTestResult {
		finished := time.Now()
		return &ScheduledTestResult{Status: "failed", ErrorMessage: message, StartedAt: started, FinishedAt: finished, LatencyMs: finished.Sub(started).Milliseconds(), PelicanConfig: plan.PelicanConfig}
	}
	defer func() {
		if recover() != nil {
			result = failure("scheduled_test_panic: background sample failed")
			logger.LegacyPrintf("service.scheduled_test_runner", "pelican plan=%d account=%d sample panicked", plan.ID, plan.AccountID)
		}
	}()
	var err error
	result, err = s.runPelican(ctx, plan.AccountID, plan.ModelID, plan.PelicanConfig)
	if err != nil {
		return failure(fmt.Sprint(err))
	}
	if result == nil {
		return failure("scheduled_test_empty_result: background sample returned no result")
	}
	return result
}

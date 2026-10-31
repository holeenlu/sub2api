package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

const scheduledTestDefaultMaxWorkers = 10

// ScheduledTestRunnerService periodically scans due test plans and executes them.
type ScheduledTestRunnerService struct {
	planRepo       ScheduledTestPlanRepository
	scheduledSvc   *ScheduledTestService
	accountTestSvc *AccountTestService
	rateLimitSvc   *RateLimitService
	cfg            *config.Config

	mu       sync.Mutex
	cron     *cron.Cron
	work     backgroundWork
	closing  bool
	stopDone chan struct{}
}

// NewScheduledTestRunnerService creates a new runner.
func NewScheduledTestRunnerService(
	planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
	cfg *config.Config,
) *ScheduledTestRunnerService {
	return &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   scheduledSvc,
		accountTestSvc: accountTestSvc,
		rateLimitSvc:   rateLimitSvc,
		cfg:            cfg,
	}
}

// Start begins the cron ticker (every minute).
func (s *ScheduledTestRunnerService) Start(parent context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return errBackgroundWorkStopped
	}
	if err := s.work.bind(parent, s.RequestStop); err != nil {
		return err
	}
	if s.cron != nil {
		return nil
	}
	loc := time.Local
	if s.cfg != nil {
		if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
			loc = parsed
		}
	}
	c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(loc))
	if _, err := c.AddFunc("* * * * *", s.runScheduled); err != nil {
		return err
	}
	c.Schedule(cron.Every(5*time.Second), cron.FuncJob(func() {
		ctx, done, err := s.work.begin()
		if err != nil {
			return
		}
		defer done()
		if s.scheduledSvc != nil {
			s.scheduledSvc.RunPendingDiagnostics(ctx)
		}
	}))
	s.cron = c
	c.Start()
	return nil
}

// RequestStop seals callback admission, cancels the owner, and retains the
// scheduler's real completion handle for Drain/Stop to wait on.
func (s *ScheduledTestRunnerService) RequestStop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.stopDone = make(chan struct{})
	c, done := s.cron, s.stopDone
	s.mu.Unlock()
	s.work.requestStop()
	go func() {
		if c != nil {
			joined := c.Stop()
			<-joined.Done()
		}
		_ = s.work.join(context.Background())
		close(done)
	}()
}
func (s *ScheduledTestRunnerService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("scheduled test stop context is nil")
	}
	s.RequestStop()
	s.mu.Lock()
	done := s.stopDone
	s.mu.Unlock()
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
func (s *ScheduledTestRunnerService) Drain(ctx context.Context) error { return s.Stop(ctx) }

func (s *ScheduledTestRunnerService) runScheduled() {
	parent, done, err := s.work.begin()
	if err != nil {
		return
	}
	defer done()
	// Keep the existing :10 offset, but cancellation must interrupt the wait.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-parent.Done():
		return
	}
	s.runDuePlans(parent)
}

// runDuePlans is the actual bounded scan/execution phase after the unchanged
// :10 offset. Every child is joined before the admitted callback completes.
func (s *ScheduledTestRunnerService) runDuePlans(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	if ctx.Err() != nil {
		return
	}

	now := time.Now()
	plans, err := s.planRepo.ListDue(ctx, now)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] ListDue error: %v", err)
		return
	}
	if len(plans) == 0 {
		return
	}

	logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] found %d due plans", len(plans))

	sem := make(chan struct{}, scheduledTestDefaultMaxWorkers)
	var wg sync.WaitGroup

	for _, plan := range plans {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(p *ScheduledTestPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runOnePlan(ctx, p)
		}(plan)
	}

	wg.Wait()
}

func (s *ScheduledTestRunnerService) runOnePlan(ctx context.Context, plan *ScheduledTestPlan) {
	if ctx.Err() != nil {
		return
	}
	// Background plans do not run under the HTTP recovery middleware.
	defer func() {
		if recover() != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "scheduled plan=%d account=%d panicked", plan.ID, plan.AccountID)
		}
	}()
	if plan.PelicanConfig != nil {
		// Never reinterpret a retired quality plan as a connectivity test.
		return
	}
	if s.scheduledSvc == nil || s.scheduledSvc.CheckScheduledAuthorization(ctx, plan.Authorization) != nil {
		return
	}
	result, err := s.accountTestSvc.RunTestBackground(ctx, plan.AccountID, plan.ModelID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d RunTestBackground error: %v", plan.ID, err)
		return
	}

	if err := s.scheduledSvc.SaveResult(ctx, plan.ID, plan.MaxResults, result); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d SaveResult error: %v", plan.ID, err)
	}

	// Auto-recover account if test succeeded and auto_recover is enabled.
	if result.Status == "success" && plan.AutoRecover && s.scheduledSvc.CheckScheduledAuthorization(ctx, plan.Authorization) == nil {
		s.tryRecoverAccount(ctx, plan.AccountID, plan.ID)
	}

	nextRun, err := computeNextRun(plan.CronExpression, time.Now())
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d computeNextRun error: %v", plan.ID, err)
		return
	}

	if err := s.planRepo.UpdateAfterRun(ctx, plan.ID, time.Now(), nextRun); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d UpdateAfterRun error: %v", plan.ID, err)
	}
}

// tryRecoverAccount attempts to recover an account from recoverable runtime state.
func (s *ScheduledTestRunnerService) tryRecoverAccount(ctx context.Context, accountID int64, planID int64) {
	if s.rateLimitSvc == nil {
		return
	}

	recovery, err := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover failed: %v", planID, err)
		return
	}
	if recovery == nil {
		return
	}

	if recovery.ClearedError {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d recovered from error status", planID, accountID)
	}
	if recovery.ClearedRateLimit {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d cleared rate-limit/runtime state", planID, accountID)
	}
}

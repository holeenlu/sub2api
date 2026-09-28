package service

import (
	"context"
	"sync"
	"time"
)

func pelicanPlan() *ScheduledTestPlan {
	return &ScheduledTestPlan{ID: 1, AccountID: 42, ModelID: "gpt-6-astra", CronExpression: "*/30 * * * *", Enabled: true, MaxResults: 50, PelicanConfig: stateProbePlanConfig()}
}

type pelicanPlanRepo struct {
	ScheduledTestPlanRepository
	mu       sync.Mutex
	claimed  bool
	finished bool
}

func (r *pelicanPlanRepo) ClaimPelican(context.Context, *ScheduledTestPlan, time.Time, time.Time, time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimed {
		return false, nil
	}
	r.claimed = true
	return true, nil
}
func (r *pelicanPlanRepo) FinishPelican(context.Context, int64, time.Time, time.Time) error {
	r.finished = true
	return nil
}

type pelicanResults struct {
	ScheduledTestResultRepository
	results []*ScheduledTestResult
	pruned  int
}

func (r *pelicanResults) Create(ctx context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.results = append(r.results, result)
	return result, nil
}
func (r *pelicanResults) PruneOldResults(_ context.Context, _ int64, count int) error {
	r.pruned = count
	return nil
}

type qualityPlanRepo struct {
	pelicanPlanRepo
	outcomes []string
}

func (r *qualityPlanRepo) ApplyQualityOutcome(_ context.Context, _ *ScheduledTestPlan, _ time.Time, outcome string) (string, error) {
	r.outcomes = append(r.outcomes, outcome)
	return "bps_enabled", nil
}

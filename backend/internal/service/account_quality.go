package service

import (
	"context"
	"fmt"
)

// QualityPolicy is opt-in. Legacy connectivity/HTML tests never modify membership.
type QualityPolicy struct {
	Judge          *QualityJudgeConfig `json:"judge,omitempty"`
	ExpectedAnswer string              `json:"expected_answer"`
	Action         string              `json:"action"`
	RemoveGroupIDs []int64             `json:"remove_group_ids"`
	AutoRestore    bool                `json:"auto_restore"`
	BPS            *QualityBPSPolicy   `json:"bps,omitempty"`
}

func validateQualityPolicy(plan *ScheduledTestPlan) error {
	q := plan.PelicanConfig.Quality
	if q == nil || q.Action != QualityActionEnableBPS || plan.PelicanConfig.QuestionKind != OpenAICodexStateProbeQuestionKind {
		return fmt.Errorf("only state-probe automatic BPS rules are supported")
	}
	if plan.AutoRecover {
		return fmt.Errorf("BPS rules use auto_restore, not connectivity auto_recover")
	}
	if q.Judge != nil {
		return fmt.Errorf("judge and group quarantine are not supported by automatic BPS rules")
	}
	q.RemoveGroupIDs = nil // Inactive legacy quarantine options are never applied.
	return validateQualityBPSPolicy(q.BPS)
}

// A completed anomalous probe counts toward the BPS threshold; restoration requires healthy probes.
// Transport errors alone are inconclusive, never evidence of degradation.
func qualityOutcome(results []*ScheduledTestResult) string {
	allPassed := len(results) > 0
	for _, r := range results {
		if r != nil && r.QualityJudgment != nil && r.QualityJudgment.Verdict == "incorrect" && r.Status == "failed" &&
			r.ErrorMessage == openAICodexStateDegradedError {
			return "failed"
		}
		if r == nil || r.Status != "success" || r.QualityJudgment == nil || r.QualityJudgment.Verdict != "correct" {
			allPassed = false
		}
	}
	if allPassed {
		return "passed"
	}
	return "inconclusive"
}

func (s *ScheduledTestService) ListQualityPlans(ctx context.Context) ([]*ScheduledTestPlan, error) {
	return s.planRepo.ListQualityPlans(ctx)
}
func (s *ScheduledTestService) TriggerQuality(ctx context.Context, id int64) error {
	return s.planRepo.TriggerQuality(ctx, id)
}

func (s *ScheduledTestService) ListQualityHistory(ctx context.Context, beforeID int64) (*QualityHistoryPage, error) {
	items, err := s.resultRepo.ListQualityHistory(ctx, beforeID, 101)
	if err != nil {
		return nil, err
	}
	page := &QualityHistoryPage{Items: items}
	if len(items) > 100 {
		page.Items = items[:100]
		page.NextCursor = items[99].ID
	}
	return page, nil
}

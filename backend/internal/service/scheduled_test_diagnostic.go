package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

const CodexDiagnosticDefaultIntervalMinutes = 60
const CodexDiagnosticConfidence = 0.8
const CodexDiagnosticMaxModels = 16
const CodexDiagnosticHistoryLimit = 10
const CodexDiagnosticMinIntervalMinutes = 60
const CodexDiagnosticMaxIntervalMinutes = 10080
const CodexDiagnosticParallelRuns = 2
const CodexDiagnosticLeaseMinutes = 40

// Rules are the only frontend policy source; SQL consumes these same constants.
func DiagnosticRules() map[string]any {
	return map[string]any{
		"default_interval_minutes": CodexDiagnosticDefaultIntervalMinutes,
		"min_interval_minutes":     CodexDiagnosticMinIntervalMinutes,
		"max_interval_minutes":     CodexDiagnosticMaxIntervalMinutes,
		"max_models":               CodexDiagnosticMaxModels, "history_limit": CodexDiagnosticHistoryLimit,
		"confidence_threshold": CodexDiagnosticConfidence,
	}
}

var ErrDiagnosticBusy = errors.New("diagnostic_already_running")
var ErrDiagnosticInvalid = errors.New("diagnostic_invalid_configuration")
var ErrDiagnosticNotFound = errors.New("diagnostic_not_found")

type CodexDiagnosticPlan struct {
	IntervalMinutes int        `json:"interval_minutes"`
	AccountID       int64      `json:"account_id"`
	OwnerID         int64      `json:"owner_id"`
	APIKeyID        int64      `json:"api_key_id"`
	Models          []string   `json:"models"`
	Enabled         bool       `json:"enabled"`
	Revision        int64      `json:"revision"`
	NextRunAt       *time.Time `json:"next_run_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
type CodexDiagnosticRun struct {
	ID              int64                 `json:"id"`
	AccountID       int64                 `json:"account_id"`
	OwnerID         int64                 `json:"owner_id"`
	APIKeyID        int64                 `json:"api_key_id"`
	APIKeyName      string                `json:"api_key_name"`
	PlanRevision    int64                 `json:"plan_revision"`
	Models          []string              `json:"models"`
	Source          string                `json:"source"`
	Status          string                `json:"status"`
	Reason          string                `json:"reason,omitempty"`
	Items           []CodexDiagnosticItem `json:"items"`
	CreatedAt       time.Time             `json:"created_at"`
	StartedAt       *time.Time            `json:"started_at"`
	FinishedAt      *time.Time            `json:"finished_at"`
	WorkerToken     string                `json:"-"`
	CancelRequested bool                  `json:"cancel_requested"`
}
type CodexDiagnosticSummary struct {
	Stale           bool       `json:"stale"`
	IntervalMinutes int        `json:"interval_minutes"`
	RunID           int64      `json:"run_id"`
	Status          string     `json:"status"`
	CheckedAt       *time.Time `json:"checked_at"`
	Enabled         bool       `json:"enabled"`
	NextRunAt       *time.Time `json:"next_run_at"`
}
type CodexDiagnosticRepository interface {
	GetPlan(context.Context, int64) (*CodexDiagnosticPlan, error)
	SavePlan(context.Context, *CodexDiagnosticPlan) error
	Enqueue(context.Context, *CodexDiagnosticPlan, string, string) (*CodexDiagnosticRun, error)
	EnqueueDue(context.Context) error
	Claim(context.Context, string) (*CodexDiagnosticRun, error)
	SaveProgress(context.Context, *CodexDiagnosticRun) error
	GetRun(context.Context, int64, int64) (*CodexDiagnosticRun, error)
	ListRuns(context.Context, int64) ([]CodexDiagnosticRun, error)
	Cancel(context.Context, int64, int64) error
	Summaries(context.Context, []int64) (map[int64]CodexDiagnosticSummary, error)
}

// SetDiagnosticRouter is called after gateway routes are registered.
func (s *ScheduledTestService) SetDiagnosticRouter(router http.Handler) { s.diagnosticRouter = router }

// RunPendingDiagnostics is owned by the existing ScheduledTestRunnerService.
// Every replica shares database ownership and the bounded diagnostic budget.
func (s *ScheduledTestService) RunPendingDiagnostics(ctx context.Context) {
	if s.diagnosticRouter == nil || s.gateway == nil {
		return
	}
	scan, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := s.planRepo.EnqueueDue(scan)
	var run *CodexDiagnosticRun
	if err == nil {
		run, err = s.planRepo.Claim(scan, uuid.NewString())
	}
	cancel()
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test", "diagnostic queue unavailable: %v", err)
		return
	}
	if run != nil {
		s.executeDiagnostic(ctx, run)
	}
}

func (s *ScheduledTestService) validateDiagnostic(ctx context.Context, p *CodexDiagnosticPlan) (*APIKey, error) {
	if p.APIKeyID <= 0 || p.OwnerID <= 0 || len(p.Models) == 0 || len(p.Models) > CodexDiagnosticMaxModels {
		return nil, ErrDiagnosticInvalid
	}
	key, account, err := s.authorizeDiagnostic(ctx, p)
	if err != nil {
		return nil, err
	}
	choices, err := buildDiagnosticChoices(key, account)
	if err != nil {
		return nil, err
	}
	if !choices.WhitelistEnabled {
		return nil, fmt.Errorf("%w: group_whitelist_required", ErrDiagnosticInvalid)
	}
	allowed := map[string]bool{}
	for _, model := range choices.Models {
		allowed[model] = true
	}
	seen := map[string]bool{}
	for _, m := range p.Models {
		if !allowed[m] || seen[m] {
			return nil, fmt.Errorf("%w: model_unavailable", ErrDiagnosticInvalid)
		}
		seen[m] = true
	}
	return key, nil
}
func (s *ScheduledTestService) GetDiagnosticPlan(ctx context.Context, id int64) (*CodexDiagnosticPlan, error) {
	return s.planRepo.GetPlan(ctx, id)
}
func (s *ScheduledTestService) SaveDiagnosticPlan(ctx context.Context, p *CodexDiagnosticPlan) error {
	if p.IntervalMinutes == 0 {
		p.IntervalMinutes = CodexDiagnosticDefaultIntervalMinutes
	}
	if p.IntervalMinutes < CodexDiagnosticMinIntervalMinutes || p.IntervalMinutes > CodexDiagnosticMaxIntervalMinutes || p.IntervalMinutes%60 != 0 {
		return fmt.Errorf("%w: interval_must_be_1_to_168_hours", ErrDiagnosticInvalid)
	}

	old, err := s.planRepo.GetPlan(ctx, p.AccountID)
	if err != nil {
		return err
	}
	if !p.Enabled && old != nil && p.APIKeyID == old.APIKeyID && reflect.DeepEqual(p.Models, old.Models) {
		p.OwnerID = old.OwnerID
		return s.planRepo.SavePlan(ctx, p)
	}
	if _, err := s.validateDiagnostic(ctx, p); err != nil {
		return err
	}
	return s.planRepo.SavePlan(ctx, p)
}

func (s *ScheduledTestService) RunDiagnosticNow(ctx context.Context, accountID, ownerID int64) (*CodexDiagnosticRun, error) {
	p, err := s.planRepo.GetPlan(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrDiagnosticInvalid
	}
	// An admin must explicitly save their own billing key before starting a run.
	if p.OwnerID != ownerID {
		return nil, fmt.Errorf("%w: save_own_key_first", ErrDiagnosticInvalid)
	}
	key, err := s.validateDiagnostic(ctx, p)
	if err != nil {
		return nil, err
	}
	return s.planRepo.Enqueue(ctx, p, "manual", key.Name)
}
func (s *ScheduledTestService) ListDiagnosticRuns(ctx context.Context, id int64) ([]CodexDiagnosticRun, error) {
	return s.planRepo.ListRuns(ctx, id)
}

func (s *ScheduledTestService) CancelDiagnostic(ctx context.Context, account, id int64) error {
	return s.planRepo.Cancel(ctx, account, id)
}
func (s *ScheduledTestService) DiagnosticSummaries(ctx context.Context, ids []int64) (map[int64]CodexDiagnosticSummary, error) {
	return s.planRepo.Summaries(ctx, ids)
}
func DiagnosticRunStatus(items []CodexDiagnosticItem, expected int) string {
	if len(items) == 0 {
		return "failed"
	}
	normal, failed := 0, 0
	for _, item := range items {
		if item.Status == "degraded" {
			return "degraded"
		}
		if item.Status == "normal" {
			normal++
		}
		if item.Status == "failed" {
			failed++
		}
	}
	if normal == expected {
		return "normal"
	}
	if failed == len(items) {
		return "failed"
	}
	return "uncertain"
}
func (s *ScheduledTestService) executeDiagnostic(parent context.Context, run *CodexDiagnosticRun) {
	ctx, cancel := context.WithTimeout(parent, 35*time.Minute)
	defer cancel()
	defer func() {
		if recover() != nil {
			run.Status = "failed"
			run.Reason = "internal_error"
		}
		if run.Status == "running" {
			check, stopCheck := context.WithTimeout(context.Background(), 3*time.Second)
			current, err := s.planRepo.GetRun(check, run.AccountID, run.ID)
			stopCheck()
			if err == nil && current.CancelRequested {
				run.Status = "canceled"
				run.Reason = "canceled_or_settings_changed"
			}
		}
		if run.Status == "running" {
			run.Status = DiagnosticRunStatus(run.Items, len(run.Models))
		}
		finished := time.Now()
		run.FinishedAt = &finished
		save, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := s.planRepo.SaveProgress(save, run); err != nil {
			logger.LegacyPrintf("service.codex_diagnostic", "persist run %d failed: %v", run.ID, err)
		}
	}()
	for _, model := range run.Models {
		if ctx.Err() != nil {
			run.Status = "failed"
			run.Reason = "interrupted"
			return
		}
		current, err := s.planRepo.GetRun(ctx, run.AccountID, run.ID)
		if err != nil {
			run.Status = "failed"
			run.Reason = "storage_unavailable"
			return
		}
		p, err := s.planRepo.GetPlan(ctx, run.AccountID)
		if err != nil {
			run.Status = "failed"
			run.Reason = "storage_unavailable"
			return
		}
		if current.WorkerToken != run.WorkerToken || current.Status != "running" {
			run.Status = "failed"
			run.Reason = "worker_interrupted"
			return
		}
		if current.CancelRequested || p == nil || p.Revision != run.PlanRevision || (run.Source == "scheduled" && !p.Enabled) {
			run.Status = "canceled"
			run.Reason = "canceled_or_settings_changed"
			return
		}
		key, err := s.validateDiagnostic(ctx, &CodexDiagnosticPlan{AccountID: run.AccountID, OwnerID: run.OwnerID, APIKeyID: run.APIKeyID, Models: []string{model}})
		if err != nil {
			run.Items = append(run.Items, CodexDiagnosticItem{Model: model, Status: "failed", Reason: "configuration_unavailable"})
			continue
		}
		run.APIKeyName = key.Name
		var item CodexDiagnosticItem
		if s.diagnosticProbe != nil {
			item = s.diagnosticProbe(ctx, key, run.AccountID, model)
		} else {
			item = RunCodexDiagnosticProbe(ctx, s.gateway, s.diagnosticRouter, key, run.AccountID, model, "127.0.0.1:0", "localhost", NewModelTraceChallenge)
		}
		run.Items = append(run.Items, item)
		if err := s.planRepo.SaveProgress(ctx, run); err != nil {
			run.Status = "failed"
			run.Reason = "storage_unavailable"
			return
		}
	}
}

func (s *ScheduledTestService) authorizeDiagnostic(ctx context.Context, p *CodexDiagnosticPlan) (*APIKey, *Account, error) {
	owner, err := s.users.GetByID(ctx, p.OwnerID)
	if err != nil || owner == nil || !owner.IsAdmin() || !owner.IsActive() || owner.DeletedAt != nil {
		return nil, nil, fmt.Errorf("%w: owner_unavailable", ErrDiagnosticInvalid)
	}
	key, err := s.keys.GetByID(ctx, p.APIKeyID)
	if err != nil || key == nil || key.UserID != p.OwnerID || !key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() || key.Key == "" {
		return nil, nil, fmt.Errorf("%w: api_key_unavailable", ErrDiagnosticInvalid)
	}
	account, err := s.accounts.GetByID(ctx, p.AccountID)
	if err != nil || account == nil || account.Platform != PlatformOpenAI || (account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken) {
		return nil, nil, fmt.Errorf("%w: account_unavailable", ErrDiagnosticInvalid)
	}
	if !account.IsActive() || !account.Schedulable {
		return nil, nil, fmt.Errorf("%w: account_not_schedulable", ErrDiagnosticInvalid)
	}
	if key.GroupID != nil {
		found := false
		for _, id := range account.GroupIDs {
			found = found || id == *key.GroupID
		}
		if !found {
			return nil, nil, fmt.Errorf("%w: account_not_in_key_group", ErrDiagnosticInvalid)
		}
	}

	return key, account, nil
}

type CodexDiagnosticModelChoice struct {
	ID       string `json:"id"`
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"`
}
type CodexDiagnosticChoices struct {
	Models           []string                     `json:"models"`
	Items            []CodexDiagnosticModelChoice `json:"items"`
	GroupName        string                       `json:"group_name"`
	WhitelistEnabled bool                         `json:"whitelist_enabled"`
	Commit           string                       `json:"commit"`
}

func (s *ScheduledTestService) DiagnosticModels(ctx context.Context, accountID, ownerID, keyID int64) (*CodexDiagnosticChoices, error) {
	key, account, err := s.authorizeDiagnostic(ctx, &CodexDiagnosticPlan{AccountID: accountID, OwnerID: ownerID, APIKeyID: keyID})
	if err != nil {
		return nil, err
	}
	return buildDiagnosticChoices(key, account)
}
func buildDiagnosticChoices(key *APIKey, account *Account) (*CodexDiagnosticChoices, error) {
	known, err := ModelTraceGPTModels()
	if err != nil {
		return nil, err
	}
	choices := &CodexDiagnosticChoices{Models: []string{}, Items: []CodexDiagnosticModelChoice{}, Commit: ModelTraceBankCommit()}
	if key.Group == nil {
		return choices, nil
	}
	choices.GroupName = key.Group.Name
	choices.WhitelistEnabled = key.Group.ModelAllowlist.Enabled
	if !choices.WhitelistEnabled {
		return choices, nil
	}
	bank := map[string]bool{}
	for _, model := range known {
		bank[model] = true
	}
	seen := map[string]bool{}
	appendChoice := func(model, reason string) {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			return
		}
		seen[model] = true
		if reason == "" && !bank[model] {
			reason = "fingerprint_unavailable"
		}
		if reason == "" && !account.IsModelSupported(model) {
			reason = "account_model_unavailable"
		}
		item := CodexDiagnosticModelChoice{ID: model, Eligible: reason == "", Reason: reason}
		choices.Items = append(choices.Items, item)
		if item.Eligible {
			choices.Models = append(choices.Models, model)
		}
	}
	for _, entry := range key.Group.ModelAllowlist.Models {
		if strings.Contains(entry, "*") {
			found := false
			for _, model := range known {
				if groupAllowlistPatternMatches(entry, model) {
					appendChoice(model, "")
					found = true
				}
			}
			if !found {
				appendChoice(entry, "pattern_no_fingerprint")
			}
		} else {
			appendChoice(entry, "")
		}
	}
	return choices, nil
}

func DiagnosticResultStale(checked *time.Time, intervalMinutes int, now time.Time) bool {
	return checked != nil && now.Sub(*checked) > 2*time.Duration(intervalMinutes)*time.Minute
}

// Diagnostic classification has one owner; every execution path calls the probe.
func diagnosticConclusion(model, predicted string, probability float64) (string, string) {
	if probability < CodexDiagnosticConfidence {
		return "uncertain", "low_confidence"
	}
	if model == predicted {
		return "normal", ""
	}
	return "degraded", "fingerprint_mismatch"
}

func (s *ScheduledTestService) RefreshDiagnosticFingerprint(ctx context.Context) (string, []string, error) {
	if s.gateway == nil || s.gateway.settingService == nil {
		return "", nil, ErrSettingNotFound
	}
	commit, err := s.gateway.settingService.RefreshModelTraceBank(ctx)
	if err != nil {
		return "", nil, err
	}
	models, err := ModelTraceGPTModels()
	return commit, models, err
}

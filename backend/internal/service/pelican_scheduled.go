package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

const PelicanDeliveryContract = "所有账号使用相同交付约定：直接返回独立 HTML，不使用 Markdown 代码块或外部依赖。只输出 HTML，不要解释。"

var pelicanHTMLPattern = regexp.MustCompile(`(?i)<(?:!doctype\s+html|html|svg)[\s>]`)

func (s *AccountTestService) RunPelicanBackground(ctx context.Context, accountID int64, model string, cfg *PelicanTestConfig) (*ScheduledTestResult, error) {
	started := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w := &pelicanRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	c, _ := gin.CreateTestContext(w)
	c.Request = (&http.Request{}).WithContext(ctx)
	err := s.TestPelicanAccountConnection(c, accountID, model, cfg.Prompt+"\n\n"+PelicanDeliveryContract, cfg.ReasoningEffort)
	output, message := parsePelicanOutput(w.Body.String())
	if w.overflow {
		output = ""
		message = "Response exceeds 4 MiB capture limit"
	}
	if err != nil && message == "" {
		message = err.Error()
	}
	if message == "" && !pelicanHTMLPattern.MatchString(output) {
		message = "Model did not return HTML or SVG"
	}
	// Bounded history storage; never persist a truncated animation as a success.
	if len(output) > 2<<20 {
		output = ""
		message = "HTML exceeds 2 MiB history limit"
	}
	status := "success"
	if message != "" {
		status = "failed"
	}
	finished := time.Now()
	snapshot := *cfg
	snapshot.ModelID = model
	return &ScheduledTestResult{Status: status, ResponseText: output, ErrorMessage: message, LatencyMs: finished.Sub(started).Milliseconds(), StartedAt: started, FinishedAt: finished, PelicanConfig: &snapshot}, nil
}

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
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	results := make([]*ScheduledTestResult, plan.PelicanConfig.ParallelCount)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, err := s.runPelican(runCtx, plan.AccountID, plan.ModelID, plan.PelicanConfig)
			if err != nil {
				result = &ScheduledTestResult{Status: "failed", ErrorMessage: fmt.Sprint(err), StartedAt: now, FinishedAt: time.Now(), PelicanConfig: plan.PelicanConfig}
			}
			results[index] = result
		}(i)
	}
	wg.Wait()
	// Persist timeout failures with a fresh context even after the request deadline.
	saveCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	succeeded := false
	for _, result := range results {
		if result.Status == "success" {
			succeeded = true
		}
		if err := s.scheduledSvc.SaveResult(saveCtx, plan.ID, plan.MaxResults, result); err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "pelican plan=%d save failed: %v", plan.ID, err)
		}
	}
	if succeeded && plan.AutoRecover {
		s.tryRecoverAccount(saveCtx, plan.AccountID, plan.ID)
	}
	if err := s.planRepo.FinishPelican(saveCtx, plan.ID, until, time.Now()); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "pelican plan=%d finish failed: %v", plan.ID, err)
	}
}

// The generated SSE is captured in memory, so cap it before buffering, not just at persistence.
type pelicanRecorder struct {
	*httptest.ResponseRecorder
	cancel   context.CancelFunc
	overflow bool
}

func (w *pelicanRecorder) Write(data []byte) (int, error) {
	if w.overflow || w.Body.Len()+len(data) > 4<<20 {
		w.overflow = true
		w.cancel()
		return 0, io.ErrShortWrite
	}
	return w.ResponseRecorder.Write(data)
}
func (w *pelicanRecorder) WriteString(data string) (int, error) { return w.Write([]byte(data)) }

func parsePelicanOutput(body string) (string, string) {
	output, message := parseTestSSEOutput(body)
	complete := false
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event TestEvent
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) != nil {
			continue
		}
		if event.Type == "test_complete" {
			complete = event.Success
			if !event.Success && message == "" {
				message = "Generation did not complete successfully"
			}
		}
	}
	if !complete && message == "" {
		message = "Generation stream ended before completion"
	}
	return output, message
}

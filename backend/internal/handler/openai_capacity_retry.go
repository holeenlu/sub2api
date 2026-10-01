package handler

import (
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	openAICapacityMaxRetries  = 3
	openAICapacityRetryWindow = 15 * time.Second
)

// Capacity shedding is request-scoped: switching accounts must not replenish
// retries. Only bound retry decisions after an overload; never cancel a generation
// that has already started. A successful WebSocket turn resets this budget.
type openAICapacityRetryBudget struct {
	mu      sync.Mutex
	retries int
	started time.Time
}

func (b *openAICapacityRetryBudget) allow(c *gin.Context, err *service.UpstreamFailoverError, switchCount int) bool {
	if !err.IsOpenAICapacityShed() {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	if b.started.IsZero() {
		b.started = now
	}
	if b.retries >= openAICapacityMaxRetries || now.Sub(b.started) >= openAICapacityRetryWindow {
		service.AnnotateLastOpsUpstreamFailure(c, err)
		service.AnnotateLastOpsUpstreamError(c, "capacity_retry_exhausted", b.retries, openAICapacityMaxRetries, switchCount, 0, false)
		requestLogger(c, "handler.openai_gateway").Warn("openai.capacity_retry_exhausted",
			zap.Int("retry_count", b.retries),
			zap.Duration("elapsed", now.Sub(b.started)),
		)
		return false
	}
	b.retries++
	return true
}

func (b *openAICapacityRetryBudget) observeTurn(result *service.OpenAIForwardResult, err error) {
	// Some WebSocket adapters report response.failed as a terminal result with
	// a nil Go error. That must not replenish an exhausted overload budget.
	if err != nil || result == nil || !result.SucceededForScheduling() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.retries = 0
	b.started = time.Time{}
}

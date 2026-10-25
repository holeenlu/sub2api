package service

import "context"

// TurnOutputCommitted reports the existing turn budget's semantic-output fence.
// Recovery must not use the remaining attempt/time budget to replay a turn once
// any response identity, usage, content or terminal event has been committed.
func TurnOutputCommitted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if budget == nil {
		return false
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.firstOutput.Snapshot().Committed
}

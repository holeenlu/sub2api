package service

import (
	"context"
	"time"
)

// SubscriptionCacheData represents cached subscription data
type SubscriptionCacheData struct {
	Status       string
	ExpiresAt    time.Time
	DailyUsage   float64
	WeeklyUsage  float64
	MonthlyUsage float64
	Version      int64
}

// SubscriptionCacheSubscription owns one Redis receive loop and its callbacks.
// Close idempotently fences new callbacks and cancels their context. An owned
// watcher interrupts receiving. Wait joins receive, heartbeat and cancellation
// goroutines within ctx and reports socket-close or callback errors.
// An already admitted callback may finish after Close; no callback runs after
// successful Wait. Callbacks must honor cancellation and must not call Wait on
// their own handle. The owner calls Close/Wait before closing its local cache.
type SubscriptionCacheSubscription interface {
	Close() error
	Wait(context.Context) error
}

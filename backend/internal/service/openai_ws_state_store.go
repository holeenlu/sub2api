package service

import (
	"context"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
)

type defaultOpenAIWSStateStore struct {
	*session.StateStore
	// Bounded observations and HTTP-to-WS continuation; never persisted through GatewayCache.
	incrementalMu         sync.Mutex
	incremental           map[[32]byte]*openAIIncrementalSlot
	incrementalBytes      int
	incrementalGeneration uint64
}

// NewOpenAIWSStateStore assembles the domain-owned indexes with the existing
// incremental observation consumer. It does not keep a second set of bindings.
func NewOpenAIWSStateStore(cache GatewayCache) session.Bindings {
	return &defaultOpenAIWSStateStore{StateStore: session.NewStateStore(cache)}
}

// publishOpenAIWSLocalBindings fences only bounded in-memory connection and
// turn-state updates. Network bindings keep their own context/storage contract
// outside this callback. Unarmed HTTP/bridge modes retain their existing path.
func publishOpenAIWSLocalBindings(ctx context.Context, publish func()) bool {
	if publish == nil {
		return false
	}
	if owner := OpenAIWSSessionOwner(ctx); owner != nil {
		return owner.Publish(publish)
	}
	publish()
	return true
}

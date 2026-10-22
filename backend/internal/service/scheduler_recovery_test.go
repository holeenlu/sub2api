package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/stretchr/testify/require"
)

// Lease storage semantics are tested against Redis in repository. Here the
// shared double verifies wiring, turn accounting and cancellation ownership.
type recoveryWiringCache struct {
	schedulerTestConcurrencyCache
	mu          sync.Mutex
	scopes      map[string]string
	unavailable bool
	cooldowns   map[string]time.Duration
}

func (c *recoveryWiringCache) BlockRecoveryScope(_ context.Context, scope string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scopes[scope] = ""
	return nil
}
func (c *recoveryWiringCache) AcquireRecoveryProbe(_ context.Context, scope string, _ int64, owner string) (bool, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return false, false, errors.New("redis down")
	}
	current, exists := c.scopes[scope]
	if !exists {
		return true, false, nil
	}
	if current != "" {
		return false, false, nil
	}
	c.scopes[scope] = owner
	return true, true, nil
}
func (c *recoveryWiringCache) RefreshRecoveryProbe(_ context.Context, scope, owner string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scopes[scope] == owner, nil
}
func (c *recoveryWiringCache) FinishRecoveryProbe(_ context.Context, scope, owner string, cooldown time.Duration, success bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scopes[scope] == owner {
		if c.cooldowns == nil {
			c.cooldowns = map[string]time.Duration{}
		}
		c.cooldowns[scope] = cooldown
		if success {
			delete(c.scopes, scope)
		} else {
			c.scopes[scope] = ""
		}
	}
	return nil
}

func TestRecoveryProbeTwoGatewayInstancesAndTurnCancellation(t *testing.T) {
	cache := &recoveryWiringCache{scopes: map[string]string{modelRecoveryScope(1, "gpt-5.1"): ""}}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	ctxA, cancelA := context.WithCancel(t.Context())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(t.Context())
	defer cancelB()
	a, b := NewOpenAITurnBudget(nil), NewOpenAITurnBudget(nil)
	ctxA = WithOpenAITurnBudget(ctxA, a)
	ctxB = WithOpenAITurnBudget(ctxB, b)
	for _, pair := range []struct {
		ctx context.Context
		svc *OpenAIGatewayService
	}{{ctxA, &OpenAIGatewayService{concurrencyService: NewConcurrencyService(cache)}}, {ctxB, &OpenAIGatewayService{concurrencyService: NewConcurrencyService(cache)}}} {
		pair.svc.installSchedulingRecovery(pair.ctx, "gpt-5.1")
	}
	request := func(ctx context.Context) context.Context {
		return withTurnRecoveryTarget(ctx, account, "http", "/responses", "", "grant")
	}
	require.NoError(t, TakeOpenAITurnAttempt(request(ctxA)))
	require.Error(t, TakeOpenAITurnAttempt(request(ctxB)))
	require.Equal(t, 1, a.attempts)
	require.Zero(t, b.attempts, "lease rejection is not an upstream send")
	// A child transport context closing must not release a still-running turn.
	child, closeChild := context.WithCancel(ctxA)
	_ = child
	closeChild()
	require.Error(t, TakeOpenAITurnAttempt(request(ctxB)))
	cancelA()
	require.Eventually(t, func() bool {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		return cache.scopes[modelRecoveryScope(1, "gpt-5.1")] == ""
	}, time.Second, time.Millisecond)
	require.NoError(t, TakeOpenAITurnAttempt(request(ctxB)))
	finishSchedulingRecovery(ctxB, account, true, nil)
	cache.mu.Lock()
	_, exists := cache.scopes[modelRecoveryScope(1, "gpt-5.1")]
	cache.mu.Unlock()
	require.False(t, exists)
	// Transport contexts reattached to an existing turn observe lease loss.
	reattached := WithOpenAITurnBudget(t.Context(), b)
	b.controlCancel(errors.New("lease lost"))
	require.Eventually(t, func() bool { return reattached.Err() != nil }, time.Second, time.Millisecond)
}

func TestRecoveryLeaseDependencyDoesNotBlockHealthyTraffic(t *testing.T) {
	cache := &recoveryWiringCache{scopes: map[string]string{}, unavailable: true}
	svc := &OpenAIGatewayService{concurrencyService: NewConcurrencyService(cache)}
	for _, cooling := range []bool{false, true} {
		ctx := WithOpenAITurnBudget(t.Context(), NewOpenAITurnBudget(nil))
		svc.installSchedulingRecovery(ctx, "gpt-5.1")
		account := &Account{ID: 1, Platform: PlatformOpenAI}
		if cooling {
			until := time.Now().Add(-time.Second)
			account.TempUnschedulableUntil = &until
			account.TempUnschedulableReason = openAIAPIKeyHealthBreakerReason
		}
		err := TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, account, "http", "/responses", "", "grant"))
		if cooling {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestRecoveryLeaseLossCancelsTurnAndPassthroughTransport(t *testing.T) {
	scope := modelRecoveryScope(1, "gpt-5.1")
	cache := &recoveryWiringCache{scopes: map[string]string{scope: ""}}
	svc := &OpenAIGatewayService{concurrencyService: NewConcurrencyService(cache)}
	ctx := WithOpenAITurnBudget(t.Context(), NewOpenAITurnBudget(nil))
	svc.installSchedulingRecovery(ctx, "gpt-5.1")
	var closed atomic.Bool
	setSchedulingRecoveryAbort(ctx, func() { closed.Store(true) })
	account := &Account{ID: 1, Platform: PlatformOpenAI}
	require.NoError(t, TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, account, "websocket", "/responses", "", "grant")))
	cache.mu.Lock()
	cache.scopes[scope] = "new-epoch-owner"
	cache.mu.Unlock()
	require.Eventually(t, func() bool { return closed.Load() && ctx.Err() != nil }, 25*time.Second, 25*time.Millisecond)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Equal(t, "new-epoch-owner", cache.scopes[scope], "lost holder cannot clear another lease")
}

func TestRecoveryProbeRetainsScopeCooldownAndRequestAttribution(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  time.Duration
	}{{"proxy failure", errors.New("stream lost"), 13 * time.Second}, {"request replay unavailable", ErrOpenAIWSReplayUnavailable, 0}, {"local selection budget", selectionbudget.ErrExhausted, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			proxy := int64(3)
			scope := proxyRecoveryScope(proxy)
			cache := &recoveryWiringCache{scopes: map[string]string{scope: ""}}
			cfg := &config.Config{}
			cfg.Gateway.OpenAIProxyStreamCircuit.TTLSeconds = 13
			svc := &OpenAIGatewayService{cfg: cfg, concurrencyService: NewConcurrencyService(cache)}
			ctx := WithOpenAITurnBudget(t.Context(), NewOpenAITurnBudget(nil))
			svc.installSchedulingRecovery(ctx, "gpt-5.1")
			account := &Account{ID: 1, Platform: PlatformOpenAI, ProxyID: &proxy}
			require.NoError(t, TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, account, "http", "/responses", "proxy", "grant")))
			finishSchedulingRecovery(ctx, account, false, tc.cause)
			cache.mu.Lock()
			defer cache.mu.Unlock()
			require.Equal(t, tc.want, cache.cooldowns[scope])
		})
	}
}

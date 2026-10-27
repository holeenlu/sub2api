package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

// RecoveryProbeCache extends the existing concurrency repository. Each scope
// uses one Redis key, including its epoch, cooldown and fenced lease owner.
type RecoveryProbeCache interface {
	BlockRecoveryScope(context.Context, string, time.Duration) error
	AcquireRecoveryProbe(context.Context, string, int64, string) (bool, bool, error)
	RefreshRecoveryProbe(context.Context, string, string) (bool, error)
	FinishRecoveryProbe(context.Context, string, string, time.Duration, bool) error
}

type schedulingRecoveryAccountKey struct{}
type schedulingRecoveryLease struct {
	scope, owner string
	cooldown     time.Duration
}
type schedulingRecovery struct {
	mu      sync.Mutex
	service *OpenAIGatewayService
	model   string
	cancel  context.CancelCauseFunc
	abort   func()
	root    context.Context
	active  map[int64][]schedulingRecoveryLease
	stops   map[int64]func()
}

func proxyRecoveryScope(id int64) string { return fmt.Sprintf("proxy:%d", id) }
func modelRecoveryScope(id int64, model string) string {
	return fmt.Sprintf("model:%d:%x", id, sha256.Sum256([]byte(normalizeOpenAIAccountModelTransientModel(model))))
}
func (s *OpenAIGatewayService) recoveryProbeCache() RecoveryProbeCache {
	if s == nil || s.concurrencyService == nil {
		return nil
	}
	cache, _ := s.concurrencyService.cache.(RecoveryProbeCache)
	return cache
}
func (s *OpenAIGatewayService) installSchedulingRecovery(ctx context.Context, models ...string) {
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.recovery == nil {
			model := b.model
			if len(models) > 0 && models[0] != "" {
				model = models[0]
			}
			b.recovery = &schedulingRecovery{service: s, model: model, cancel: b.controlCancel, root: b.control, active: map[int64][]schedulingRecoveryLease{}, stops: map[int64]func(){}}
		}
	}
}
func (s *OpenAIGatewayService) blockSchedulingRecovery(scope string, cooldown time.Duration) {
	if cache := s.recoveryProbeCache(); cache != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := cache.BlockRecoveryScope(ctx, scope, cooldown); err != nil {
			recordSchedulingHealthRejection("recovery_state_unavailable")
		}
	}
}

func (r *schedulingRecovery) beforeSend(ctx context.Context, account *Account) error {
	if account.Platform != PlatformOpenAI {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// A recovery request is a single generation send, not an extra retry lane.
	if len(r.active[account.ID]) > 0 {
		return recoveryProbeUnavailable(nil)
	}
	cache := r.service.recoveryProbeCache()
	model := canonicalOpenAIAccountSchedulingModel(account, r.model)
	scopes := []string{modelRecoveryScope(account.ID, model)}
	observed := map[string]int64{}
	cooldowns := map[string]time.Duration{scopes[0]: openAIModelTransientLongCooldown}
	knownBlocked := r.service.isOpenAIAccountModelRuntimeBlocked(account, r.model)
	if proxyID, ok := openAIProxyStreamCircuitProxyID(account); ok && !resolveOpenAIProxyStreamCircuitSettings(r.service).disabled {
		scopes = append(scopes, proxyRecoveryScope(proxyID))
		cooldowns[proxyRecoveryScope(proxyID)] = resolveOpenAIProxyStreamCircuitSettings(r.service).quarantineTTL
		knownBlocked = knownBlocked || r.service.isOpenAIProxyStreamQuarantined(ctx, account)
	}
	if until := account.TempUnschedulableUntil; until != nil && strings.Contains(account.TempUnschedulableReason, openAIAPIKeyHealthBreakerReason) && time.Since(*until) < 24*time.Hour {
		scope := fmt.Sprintf("account:%d", account.ID)
		scopes = append(scopes, scope)
		observed[scope] = until.UnixMilli()
		var state TempUnschedState
		if json.Unmarshal([]byte(account.TempUnschedulableReason), &state) == nil && state.TriggeredAtUnix > 0 && state.UntilUnix > state.TriggeredAtUnix {
			cooldowns[scope] = time.Duration(state.UntilUnix-state.TriggeredAtUnix) * time.Second
		}
		knownBlocked = true
	}
	if r.service.isOpenAIAccountModelRuntimeBlocked(account, r.model) || r.service.isOpenAIProxyStreamQuarantined(ctx, account) {
		return recoveryProbeUnavailable(nil)
	}
	if cache == nil {
		if knownBlocked {
			return recoveryProbeUnavailable(nil)
		}
		return nil
	}
	owner := generateRequestID()
	var leases []schedulingRecoveryLease
	release := func() {
		cleanup, cancel := context.WithTimeout(selectionbudget.CleanupContext(ctx), 2*time.Second)
		defer cancel()
		for _, lease := range leases {
			_ = cache.FinishRecoveryProbe(cleanup, lease.scope, lease.owner, 0, false)
		}
	}
	for _, scope := range scopes {
		allowed, probe, err := cache.AcquireRecoveryProbe(selectionbudget.PreparationContext(ctx), scope, observed[scope], owner)
		if err != nil {
			release()
			if ctx.Err() != nil || errors.Is(err, selectionbudget.ErrExhausted) {
				return err
			}
			if knownBlocked || len(leases) > 0 {
				return recoveryProbeUnavailable(err)
			}
			// Redis failure is not evidence that an otherwise healthy account failed.
			return nil
		}
		if !allowed {
			release()
			return recoveryProbeUnavailable(nil)
		}
		if probe {
			leases = append(leases, schedulingRecoveryLease{scope: scope, owner: owner, cooldown: cooldowns[scope]})
		}
	}
	if len(leases) == 0 {
		return nil
	}
	r.active[account.ID] = leases
	stop := make(chan struct{})
	var once sync.Once
	r.stops[account.ID] = func() { once.Do(func() { close(stop) }) }
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "recovery", Action: "probe_acquired", AccountID: account.ID})
	abort := r.abort
	go func() {
		lastConfirmed := time.Now()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-r.root.Done():
				r.finish(account.ID, false, nil)
				return
			case <-ticker.C:
				refresh, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				confirmed := true
				for _, lease := range leases {
					owned, err := cache.RefreshRecoveryProbe(refresh, lease.scope, lease.owner)
					if err != nil || !owned {
						select {
						case <-stop:
							cancel()
							return
						default:
						}
						confirmed = false
						recordSchedulingHealthRejection("recovery_lease_unconfirmed")
						if (err == nil || time.Since(lastConfirmed) >= 40*time.Second) && r.cancel != nil {
							r.cancel(recoveryProbeUnavailable(err))
							if abort != nil {
								abort()
							}
						}
					}
				}
				cancel()
				if confirmed {
					lastConfirmed = time.Now()
				}
			}
		}
	}()
	return nil
}

func recoveryProbeUnavailable(cause error) error {
	return &UpstreamFailoverError{StatusCode: 503, RequestNotSent: true, RequestScopedTransient: true, Stage: "selection", Reason: "recovery_probe_unavailable", Cause: cause}
}
func (r *schedulingRecovery) finish(accountID int64, success bool, cause error) {
	r.mu.Lock()
	leases := r.active[accountID]
	delete(r.active, accountID)
	if stop := r.stops[accountID]; stop != nil {
		stop()
		delete(r.stops, accountID)
	}
	r.mu.Unlock()
	if len(leases) == 0 {
		return
	}
	cooldown := time.Duration(0)
	if cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) && !IsTurnBudgetError(cause) {
		cooldown = openAIModelTransientLongCooldown
		var inputErr *openAIResponsesInputError
		if errors.Is(cause, ErrOpenAIWSReplayUnavailable) || errors.Is(cause, ErrOpenAIContinuationUnavailable) || errors.As(cause, &inputErr) || errors.Is(cause, ErrGatewayInstanceOverloaded) || errors.Is(cause, selectionbudget.ErrExhausted) {
			cooldown = 0
		}
		var failure *UpstreamFailoverError
		if errors.As(cause, &failure) && (failure.RequestNotSent || failure.StatusCode < 500 || failure.Scope == GatewayFailureScopeRequest || failure.Scope == GatewayFailureScopeProvider) {
			cooldown = 0
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, lease := range leases {
		delay := cooldown
		if delay > 0 && lease.cooldown > 0 {
			delay = lease.cooldown
		}
		_ = r.service.recoveryProbeCache().FinishRecoveryProbe(ctx, lease.scope, lease.owner, delay, success)
	}
}
func finishSchedulingRecovery(ctx context.Context, account *Account, success bool, cause error) {
	if account == nil {
		return
	}
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		recovery := b.recovery
		b.mu.Unlock()
		if recovery != nil {
			recovery.finish(account.ID, success, cause)
		}
	}
}

// Passthrough relay sends on the session socket, outside the per-turn context.
// Lease loss must also close that socket, rather than only canceling admission.
func setSchedulingRecoveryAbort(ctx context.Context, abort func()) {
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		r := b.recovery
		b.mu.Unlock()
		if r != nil {
			r.mu.Lock()
			r.abort = abort
			r.mu.Unlock()
		}
	}
}

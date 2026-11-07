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

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

type schedulingRecoveryAccountKey struct{}

// The service boundary projects Account health into immutable probe scopes and
// translates existing protocol errors. routing owns the only live lease state.
type schedulingRecovery struct {
	mu      sync.Mutex
	service *OpenAIGatewayService
	model   string
	abort   func()
	leases  *routing.Recovery
}

func proxyRecoveryScope(id int64) string { return fmt.Sprintf("proxy:%d", id) }
func modelRecoveryScope(id int64, model string) string {
	return fmt.Sprintf("model:%d:%x", id, sha256.Sum256([]byte(normalizeOpenAIAccountModelTransientModel(model))))
}
func (s *OpenAIGatewayService) recoveryProbeCache() routing.RecoveryStore {
	if s == nil || s.concurrencyService == nil {
		return nil
	}
	cache, _ := s.concurrencyService.cache.(routing.RecoveryStore)
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
			r := &schedulingRecovery{service: s, model: model}
			r.leases = routing.NewRecovery(b.control, s.recoveryProbeCache(), func(cause error) {
				b.controlCancel(recoveryProbeUnavailable(cause))
				r.mu.Lock()
				abort := r.abort
				r.mu.Unlock()
				if abort != nil {
					abort()
				}
			}, func(resourceID int64, event string) {
				if event == "probe_acquired" {
					RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "recovery", Action: event, AccountID: resourceID})
				} else {
					recordSchedulingHealthRejection(event)
				}
			})
			b.recovery = r
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
	qualified := make([]routing.RecoveryScope, 0, len(scopes))
	for _, scope := range scopes {
		qualified = append(qualified, routing.RecoveryScope{Key: scope, ObservedUntil: observed[scope], FailureCooldown: cooldowns[scope]})
	}
	if err := r.leases.Acquire(selectionbudget.PreparationContext(ctx), account.ID, generateRequestID(), qualified, knownBlocked); err != nil {
		if ctx.Err() != nil || errors.Is(err, selectionbudget.ErrExhausted) {
			return err
		}
		return recoveryProbeUnavailable(err)
	}
	return nil
}

func recoveryProbeUnavailable(cause error) error {
	return &UpstreamFailoverError{StatusCode: 503, RequestNotSent: true, RequestScopedTransient: true, Stage: "selection", Reason: "recovery_probe_unavailable", Cause: cause}
}
func (r *schedulingRecovery) finish(accountID int64, success bool, cause error) {
	cooldown := time.Duration(0)
	if cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) && !IsTurnBudgetError(cause) {
		cooldown = openAIModelTransientLongCooldown
		var inputErr *openAIResponsesInputError
		if errors.Is(cause, ErrOpenAIWSReplayUnavailable) || errors.As(cause, &inputErr) || errors.Is(cause, ErrGatewayInstanceOverloaded) || errors.Is(cause, selectionbudget.ErrExhausted) {
			cooldown = 0
		}
		var failure *UpstreamFailoverError
		if errors.As(cause, &failure) && (failure.RequestNotSent || failure.StatusCode < 500 || failure.Scope == GatewayFailureScopeRequest || failure.Scope == GatewayFailureScopeProvider) {
			cooldown = 0
		}
	}
	r.leases.Finish(accountID, success, cooldown)
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

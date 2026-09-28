package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"time"
)

// Charge only actual upstream attempts, after local validation and admission.
// Each retry, attachment upload and WebSocket turn acquires independently.
func (s *OpenAIGatewayService) acquireOpenAIRPMForSend(ctx context.Context, account *Account) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, _, err := s.TryAcquireOpenAIOAuthRPM(ctx, account)
	return err
}

func IsOpenAIRPMError(err error) bool {
	return errors.Is(err, ErrOpenAIRPMExhausted) || errors.Is(err, ErrOpenAIRPMUnavailable)
}

const (
	openAIRPMNeutralFactor  = 0.5
	openAIRPMHeadroomWeight = 3.0
)

func openAIRPMEffectiveLoad(ctx context.Context, account *Account, concurrencyLoad int) float64 {
	if rpm, ok := accountRPMStateFromContext(ctx, account); ok && account.IsOpenAIOAuth() {
		return math.Max(float64(concurrencyLoad), 100*rpm.Utilization())
	}
	return float64(concurrencyLoad)
}

var (
	// ErrOpenAIRPMExhausted tells handlers that every eligible OpenAI OAuth
	// account is at its strict per-minute ceiling.
	ErrOpenAIRPMExhausted = errors.New("openai oauth rpm exhausted")
	// ErrOpenAIRPMUnavailable is fail-closed when a configured account cannot
	// reach the shared counter store.
	ErrOpenAIRPMUnavailable = errors.New("openai oauth rpm unavailable")
)

func (s *OpenAIGatewayService) withOpenAIRPMPrefetch(ctx context.Context, accounts []Account) (context.Context, error) {
	prefetched, err := withAccountRPMPrefetch(ctx, s.rpmCache, accounts, PlatformOpenAI)
	if err != nil {
		return ctx, fmt.Errorf("%w: %v", ErrOpenAIRPMUnavailable, err)
	}
	return prefetched, nil
}

// OpenAIRPMSchedulable applies the 80% headroom rule to movable requests. A
// sticky continuation may remain on an account until the hard ceiling.
func (s *OpenAIGatewayService) OpenAIRPMSchedulable(ctx context.Context, account *Account, movable bool) (bool, AccountRPMState, error) {
	if account == nil || !account.IsOpenAIOAuth() {
		return true, AccountRPMState{}, nil
	}
	account, err := s.effectiveOpenAIRPMAccount(ctx, account)
	if err != nil {
		return false, AccountRPMState{}, err
	}
	state, err := readAccountRPMState(ctx, s.rpmCache, account)
	if err != nil {
		return false, state, fmt.Errorf("%w: %v", ErrOpenAIRPMUnavailable, err)
	}
	if !state.Enabled {
		return true, state, nil
	}
	if account.CheckRPMSchedulability(state.Current) == WindowCostNotSchedulable {
		return false, state, nil
	}
	if movable && state.Utilization() >= accountRPMWarningRatio {
		return false, state, nil
	}
	return true, state, nil
}

// TryAcquireOpenAIOAuthRPM reserves one outbound request immediately before
// forwarding. It is safe to call for every failover attempt; failed upstream
// requests remain counted as required by the strict RPM definition.
func (s *OpenAIGatewayService) TryAcquireOpenAIOAuthRPM(ctx context.Context, account *Account) (bool, AccountRPMState, error) {
	if err := ctx.Err(); err != nil {
		return false, AccountRPMState{}, err
	}
	account, err := s.effectiveOpenAIRPMAccount(ctx, account)
	if err != nil {
		return false, AccountRPMState{}, err
	}
	if account == nil || !account.IsOpenAIOAuth() || account.GetBaseRPM() <= 0 {
		return true, AccountRPMState{}, nil
	}
	if s == nil || s.rpmCache == nil {
		return false, AccountRPMState{Limit: account.GetBaseRPM(), Enabled: true}, fmt.Errorf("%w: counter cache is not configured", ErrOpenAIRPMUnavailable)
	}
	strict, ok := s.rpmCache.(StrictRPMCache)
	if !ok {
		return false, AccountRPMState{Limit: account.GetBaseRPM(), Enabled: true}, fmt.Errorf("%w: counter cache does not support atomic acquire", ErrOpenAIRPMUnavailable)
	}
	allowed, count, resetAt, err := strict.TryAcquireRPM(ctx, account.RPMAccountID(), account.GetBaseRPM())
	state := AccountRPMState{Current: count, Limit: account.GetBaseRPM(), ResetAt: resetAt, Enabled: true}
	if err != nil {
		return false, state, fmt.Errorf("%w: %v", ErrOpenAIRPMUnavailable, err)
	}
	if !allowed {
		return false, state, ErrOpenAIRPMExhausted
	}
	return true, state, nil
}

// A failed shared counter excludes only accounts that need strict protection.
// The remaining candidates still pass all normal eligibility/context checks.
func (s *OpenAIGatewayService) openAIRPMCandidates(ctx context.Context, accounts []Account) (context.Context, []Account, error) {
	resolved := make([]Account, 0, len(accounts))
	var policyErr error
	for i := range accounts {
		account, err := s.effectiveOpenAIRPMAccount(ctx, &accounts[i])
		if err != nil {
			policyErr = err
			continue
		}
		resolved = append(resolved, *account)
	}
	if len(resolved) == 0 && policyErr != nil {
		return ctx, nil, policyErr
	}
	accounts = resolved
	prefetched, err := s.withOpenAIRPMPrefetch(ctx, accounts)
	if err == nil {
		return prefetched, accounts, nil
	}
	if ctx.Err() != nil {
		return ctx, nil, ctx.Err()
	}
	unlimited := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if !account.IsOpenAIOAuth() || account.GetBaseRPM() <= 0 {
			unlimited = append(unlimited, account)
		}
	}
	if len(unlimited) == 0 {
		return ctx, nil, err
	}
	return ctx, unlimited, nil
}

// A credential shadow cannot disable or raise its parent's ceiling. Its own
// lower positive limit remains effective; never mutate the shared snapshot.
func (s *OpenAIGatewayService) effectiveOpenAIRPMAccount(ctx context.Context, account *Account) (*Account, error) {
	if account == nil || !account.IsOpenAIOAuth() || !account.IsShadow() {
		return account, nil
	}
	if s == nil || s.accountRepo == nil {
		if s != nil && !s.requireLatestTurnAdmission {
			return account, nil
		} // direct-call fixtures
		return nil, fmt.Errorf("%w: credential parent policy unavailable", ErrOpenAIRPMUnavailable)
	}
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	parent, err := resolveCredentialAccount(readCtx, s.accountRepo, account)
	if err != nil {
		return nil, fmt.Errorf("%w: credential parent policy unavailable", ErrOpenAIRPMUnavailable)
	}
	limit := parent.GetBaseRPM()
	if limit <= 0 || (account.GetBaseRPM() > 0 && account.GetBaseRPM() <= limit) {
		return account, nil
	}
	copy := *account
	copy.Extra = maps.Clone(account.Extra)
	if copy.Extra == nil {
		copy.Extra = make(map[string]any)
	}
	copy.Extra["base_rpm"] = limit
	return &copy, nil
}

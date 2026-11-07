package routing

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

type probeStore struct {
	mu        sync.Mutex
	owners    map[string]string
	failureAt string
	failure   error
	finishes  map[string]int
	cooldowns map[string]time.Duration
	acquire   func(context.Context)
}

func (s *probeStore) BlockRecoveryScope(context.Context, string, time.Duration) error { return nil }
func (s *probeStore) AcquireRecoveryProbe(ctx context.Context, key string, _ int64, owner string) (bool, bool, error) {
	if s.acquire != nil {
		s.acquire(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == s.failureAt {
		return false, false, s.failure
	}
	current, exists := s.owners[key]
	if !exists {
		return true, false, nil
	}
	if current != "" {
		return false, false, nil
	}
	s.owners[key] = owner
	return true, true, nil
}
func (s *probeStore) RefreshRecoveryProbe(_ context.Context, key, owner string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owners[key] == owner, nil
}
func (s *probeStore) FinishRecoveryProbe(ctx context.Context, key, owner string, cooldown time.Duration, success bool) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("cleanup has no deadline")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finishes == nil {
		s.finishes = map[string]int{}
		s.cooldowns = map[string]time.Duration{}
	}
	s.finishes[key]++
	if s.owners[key] == owner {
		s.cooldowns[key] = cooldown
		if success {
			delete(s.owners, key)
		} else {
			s.owners[key] = ""
		}
	}
	return nil
}

func TestRecoveryPartialAcquireRollsBackWithoutExpandingScope(t *testing.T) {
	store := &probeStore{owners: map[string]string{"model:1": "", "proxy:2": "other-owner"}}
	recovery := NewRecovery(t.Context(), store, nil, nil)
	err := recovery.Acquire(t.Context(), 1, "request-owner", []RecoveryScope{{Key: "model:1"}, {Key: "proxy:2"}}, true)
	require.ErrorIs(t, err, ErrRecoveryProbeUnavailable)
	require.Equal(t, "", store.owners["model:1"])
	require.Equal(t, "other-owner", store.owners["proxy:2"])
	require.Equal(t, 1, store.finishes["model:1"])
	require.Empty(t, recovery.active)
	for _, scopes := range [][]RecoveryScope{nil, {{Key: ""}}} {
		require.ErrorIs(t, recovery.Acquire(t.Context(), 1, "token", scopes, false), ErrRecoveryProbeUnavailable)
	}
}

func TestRecoveryDependencyPolicyDistinguishesHealthyBlockedAndPartial(t *testing.T) {
	for _, tc := range []struct {
		name             string
		blocked, partial bool
		cause            error
		denied           bool
	}{
		{"healthy", false, false, errors.New("redis down"), false},
		{"known blocked", true, false, errors.New("redis down"), true},
		{"partial grant", false, true, errors.New("redis down"), true},
		{"budget exhausted", false, false, selectionbudget.ErrExhausted, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &probeStore{owners: map[string]string{}, failureAt: "last", failure: tc.cause}
			scopes := []RecoveryScope{{Key: "last"}}
			if tc.partial {
				store.owners["first"] = ""
				scopes = append([]RecoveryScope{{Key: "first"}}, scopes...)
			}
			r := NewRecovery(t.Context(), store, nil, nil)
			err := r.Acquire(t.Context(), 1, "token", scopes, tc.blocked)
			if tc.denied {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.partial {
				require.Equal(t, 1, store.finishes["first"])
			}
			if errors.Is(tc.cause, selectionbudget.ErrExhausted) {
				require.ErrorIs(t, err, tc.cause)
			}
		})
	}
}

func TestRecoveryFinishIsFencedJoinedAndIdempotent(t *testing.T) {
	store := &probeStore{owners: map[string]string{"model:1": "", "proxy:2": ""}}
	r := NewRecovery(t.Context(), store, nil, nil)
	require.NoError(t, r.Acquire(t.Context(), 1, "old", []RecoveryScope{{Key: "model:1", FailureCooldown: time.Second}, {Key: "proxy:2", FailureCooldown: 13 * time.Second}}, true))
	require.ErrorIs(t, r.Acquire(t.Context(), 1, "duplicate", []RecoveryScope{{Key: "model:1"}}, true), ErrRecoveryProbeUnavailable)
	store.mu.Lock()
	store.owners["model:1"] = "new-owner"
	store.mu.Unlock()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { r.Finish(1, false, time.Minute) })
	}
	wg.Wait()
	require.Equal(t, "new-owner", store.owners["model:1"])
	require.Equal(t, "", store.owners["proxy:2"])
	require.Equal(t, 13*time.Second, store.cooldowns["proxy:2"])
	require.Equal(t, 1, store.finishes["proxy:2"])
	require.Empty(t, r.active)
}

func TestRecoveryFinishWaitsForAcquisitionAndRollsBackOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	entered, resume := make(chan struct{}), make(chan struct{})
	store := &probeStore{owners: map[string]string{"model:1": ""}, acquire: func(context.Context) { close(entered); <-resume }}
	r := NewRecovery(ctx, store, nil, nil)
	acquired := make(chan error, 1)
	go func() { acquired <- r.Acquire(ctx, 1, "token", []RecoveryScope{{Key: "model:1"}}, true) }()
	<-entered
	joined := make(chan struct{})
	go func() { r.Finish(1, true, 0); close(joined) }()
	select {
	case <-joined:
		t.Fatal("finish did not join acquire")
	default:
	}
	cancel()
	close(resume)
	require.ErrorIs(t, <-acquired, context.Canceled)
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("acquire cleanup was not joined")
	}
	require.Equal(t, "", store.owners["model:1"])
	require.Equal(t, 1, store.finishes["model:1"])
}

//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Only the returned grant changes. The actor and request fixtures keep their
// original immutable account/binding, just like a previously accepted turn.
type cloudTerminalOwnerRepo struct {
	*cloudAuthorizationRepoFixture
	terminal      atomic.Bool
	terminalReads atomic.Int32
	outcome       string
}

func (r *cloudTerminalOwnerRepo) GetCloudAuthorization(ctx context.Context, id int64) (*OpenAICloudAuthorizationBinding, error) {
	binding, err := r.cloudAuthorizationRepoFixture.GetCloudAuthorization(ctx, id)
	if err != nil || !r.terminal.Load() {
		return binding, err
	}
	r.terminalReads.Add(1)
	switch r.outcome {
	case "replacement":
		replacement := *binding
		replacement.Generation = "replacement-grant"
		return &replacement, nil
	case "unavailable":
		return nil, errors.New("temporary grant lookup failure")
	default:
		return binding, nil
	}
}

// A missing AccountPolicyApplied bit can come from an old owner or a failed
// owner-side dependency read. Neither permits the gateway to skip the grant
// fence and punish whichever credentials now occupy the same account row.
func TestCloudTerminalFallbackRechecksOwnerGeneration(t *testing.T) {
	for _, tc := range []struct {
		name            string
		outcome         string
		legacyOwner     bool
		wantPolicyCalls int
	}{
		{"old owner same generation", "", true, 1},
		{"owner lookup failed and identity replaced", "replacement", false, 0},
		{"owner lookup still unavailable", "unavailable", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			grants := &cloudTerminalOwnerRepo{cloudAuthorizationRepoFixture: svc.cloudService.repo.(*cloudAuthorizationRepoFixture), outcome: tc.outcome}
			svc.cloudService.repo = grants
			var ownerCalls atomic.Int32
			if tc.legacyOwner {
				svc.cloudSessions.terminalPolicy = nil
			} else {
				svc.cloudSessions.terminalPolicy = func(context.Context, OpenAICloudOwner, *OpenAICloudResult) bool {
					ownerCalls.Add(1)
					// Simulate an owner which could not confirm current grant
					// state; its result deliberately has no applied-policy bit.
					grants.terminal.Store(true)
					return false
				}
			}
			limits := &oauth429RateLimitRepo{}
			svc.rateLimitService = NewRateLimitService(limits, nil, &config.Config{}, nil, nil)
			svc.rateLimitService.SetAccountRuntimeBlocker(svc)
			svc.openaiOAuth429RetryStartedAt.Store(in.Account.ID, time.Now().Add(-openAIOAuth429RetryWindow-time.Second))
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			body, err := json.Marshal(map[string]any{"model": "client-model", "input": "go", "stream": true})
			require.NoError(t, err)
			ctx, c, recorder := cloudContractContext(t, svc, in, ctx, body)
			done := make(chan struct{})
			defer func() { cancel(); <-done }()
			go func() {
				defer close(done)
				cloudWireStart(t, ctx, wire)
				cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "answer", "type": "agentMessage"}}})
				cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "answer", "delta": "partial"}})
				if tc.legacyOwner {
					grants.terminal.Store(true)
				}
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"code": "rate_limit_exceeded", "httpStatusCode": 429}}}})
			}()
			result, err := svc.forwardOpenAICloud(ctx, c, in.Account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Contains(t, recorder.Body.String(), "partial", "the turn executed and is not a pre-inference replay")
			require.Equal(t, tc.wantPolicyCalls, limits.setRateLimitedCalls)
			require.Greater(t, grants.terminalReads.Load(), int32(0), "gateway fallback must re-read the current grant")
			record := store.snapshot()
			require.NotNil(t, record.Result)
			require.False(t, record.Result.AccountPolicyApplied, "test must exercise a missing applied-policy marker")
			require.Equal(t, "failed", gjson.GetBytes(record.Result.Response, "status").String())
			require.Equal(t, "grant", grants.binding.Generation, "the simulated lookup must not mutate the execution's original grant")
			if tc.legacyOwner {
				require.Zero(t, ownerCalls.Load())
			} else {
				require.EqualValues(t, 1, ownerCalls.Load())
			}
		})
	}
}

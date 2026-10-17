package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Redis owns both the per-account clock and the lease. Multiple gateway
// instances must not initialize or send the same account concurrently.
type AccountTelemetryRunState struct {
	Epoch             string    `json:"epoch"`
	BootstrapRevision string    `json:"bootstrap_revision"`
	StartedAt         time.Time `json:"started_at"`
	IdentityKey       string    `json:"identity_key"`
	Initialized       bool      `json:"initialized"`
	LastBatch         time.Time `json:"last_batch"`
	LastEval          time.Time `json:"last_eval"`
	BatchDisabled     bool      `json:"batch_disabled"`
	EvalDisabled      bool      `json:"eval_disabled"`
	RestartAfter      time.Time `json:"restart_after"`
	BootstrapAfter    time.Time `json:"bootstrap_after"`
}

type AccountTelemetryRuntimeCache interface {
	AcquireAccountTelemetryLeader(context.Context, string) (bool, error)
	ReleaseAccountTelemetryLeader(context.Context, string) error
	BeginAccountTelemetryRun(context.Context, int64, time.Duration) (string, AccountTelemetryRunState, error)
	EndAccountTelemetryRun(context.Context, int64, string, AccountTelemetryRunState) error
	TouchAccountTelemetry(context.Context, int64, AccountTelemetryActivity) error
	ActiveAccountTelemetry(context.Context) ([]int64, error)
	GetAccountTelemetryActivity(context.Context, int64) (AccountTelemetryActivity, error)
}

func (s *GatewayService) generateClaudeTelemetry(ctx context.Context, group *claudeTelemetryDispatch) error {
	if err := s.requireAccountTelemetrySendingEnabled(ctx); err != nil {
		return err
	}
	cache, ok := s.cache.(AccountTelemetryRuntimeCache)
	if !ok {
		return telemetryForwardFailure("telemetry_runtime_unavailable")
	}
	lease, state, err := cache.BeginAccountTelemetryRun(ctx, group.account.ID, 15*time.Second)
	if err != nil {
		return telemetryForwardFailure("telemetry_runtime_unavailable")
	}
	if lease == "" {
		return nil
	}
	defer func() {
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if cache.EndAccountTelemetryRun(finish, group.account.ID, lease, state) != nil {
			logClaudeTelemetrySkipped(group.account.ID, telemetryForwardFailure("telemetry_runtime_save_failed"))
		}
	}()
	if s.accountTelemetryWorker != nil && state.Epoch != s.accountTelemetryWorker.epoch {
		state = AccountTelemetryRunState{Epoch: s.accountTelemetryWorker.epoch}
	}
	now := time.Now()
	if !state.RestartAfter.IsZero() {
		if now.Before(state.RestartAfter) {
			return nil
		}
		state = AccountTelemetryRunState{Epoch: state.Epoch}
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = now
	}
	id := group.identity
	identityKey := telemetryDigest([]byte(id.DeviceID + "|" + id.SessionID + "|" + id.Version))
	if state.IdentityKey != identityKey {
		state.IdentityKey, state.Initialized = identityKey, false
	}
	postEvent := func(init bool) error {
		event := id.event(init, now, state.StartedAt)
		body, _ := json.Marshal(map[string]any{"events": []json.RawMessage{event}})
		return s.sendClaudeTelemetry(ctx, ClaudeTelemetryBatchPath, group, body)
	}
	var lastErr error
	if !state.Initialized {
		if err := postEvent(true); err != nil {
			lastErr = err
			if telemetryClientError(err) {
				// vm2api exits its worker on an init 4xx; its supervisor retries
				// after five minutes while the account activity window remains open.
				state.RestartAfter = now.Add(5 * time.Minute)
				return err
			}
		} else {
			state.Initialized = true
		}
	}
	if !state.BatchDisabled && (state.LastBatch.IsZero() || now.Sub(state.LastBatch) >= 10*time.Second) {
		if err := postEvent(false); err != nil {
			lastErr, state.BatchDisabled = err, telemetryClientError(err)
		} else {
			state.LastBatch = now
		}
	}
	if !state.EvalDisabled && (state.LastEval.IsZero() || now.Sub(state.LastEval) >= 6*time.Hour) {
		if err := s.sendClaudeTelemetry(ctx, claudeTelemetryEvalPath, group, id.growthbookEval()); err != nil {
			lastErr, state.EvalDisabled = err, telemetryClientError(err)
		} else {
			state.LastEval = now
		}
	}
	return lastErr
}

func telemetryClientError(err error) bool {
	var failure *accountTelemetryForwardError
	return errors.As(err, &failure) && failure.UpstreamStatus >= 400 && failure.UpstreamStatus < 500
}

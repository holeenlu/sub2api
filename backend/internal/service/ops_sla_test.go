package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOpsSLA(t *testing.T) {
	const payload = "Request payload is too large"
	for _, tc := range []struct {
		name, owner, phase, kind, message    string
		status, upstream                     int
		oldLimited, wantLimited, wantPayload bool
	}{
		{"upstream 400", "provider", "upstream", "invalid_request_error", payload, 400, 400, false, true, true},
		{"local 413", "client", "request", "api_error", payload, 413, 0, false, true, true},
		{"wrapped 413", "provider", "upstream", "api_error", payload, 502, 413, false, true, true},
		{"upstream 422", "provider", "upstream", "invalid_request_error", payload, 422, 422, false, true, true},
		{"case insensitive", "provider", "upstream", "api_error", "REQUEST PAYLOAD IS TOO LARGE", 400, 400, false, true, true},
		{"in-band failure after HTTP 200", "provider", "upstream", "invalid_request_error", payload, 400, 200, false, true, true},
		{"successful or recovered", "provider", "upstream", "api_error", payload, 200, 413, false, false, false},
		{"unexplained 502", "provider", "upstream", "api_error", payload, 502, 0, false, false, false},
		{"real upstream 503", "provider", "upstream", "api_error", payload, 400, 503, false, false, false},
		{"generic bad request", "provider", "upstream", "invalid_request_error", "Upstream request failed", 400, 400, false, false, false},
		{"encrypted validation", "provider", "upstream", "invalid_request_error", "Encrypted content could not be verified", 400, 400, false, false, false},
		{"other context error not broadened", "provider", "upstream", "invalid_request_error", "prompt is too long", 400, 400, false, false, false},
		{"provider model rejection not broadened", "provider", "upstream", "model_not_found", "model_not_found", 404, 404, false, false, false},
		{"account authentication", "provider", "account_auth", "api_error", payload, 503, 400, false, false, false},
		{"pool 503", "platform", "routing", "api_error", "No available accounts", 503, 0, true, false, false},
		{"pool 429", "platform", "routing", "rate_limit_error", "All available accounts are currently rate-limited", 429, 0, true, false, false},
		{"pool overrides stale payload", "platform", "routing", "api_error", payload, 503, 413, true, false, false},
		{"user concurrency", "client", "request", "rate_limit_error", "concurrency limit exceeded", 429, 0, true, true, false},
		{"user balance", "client", "request", "billing_error", "Insufficient user balance", 403, 0, true, true, false},
		{"provider 429", "provider", "upstream", "rate_limit_error", "rate limit", 429, 429, false, false, false},
		{"local model rejection", "platform", "routing", "model_not_found", "model unsupported", 404, 0, true, true, false},
		{"legacy model rejection on 503", "platform", "routing", "model_not_found", payload, 503, 413, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := &OpsInsertErrorLogInput{ErrorOwner: tc.owner, ErrorPhase: tc.phase, ErrorType: tc.kind,
				StatusCode: tc.status, UpstreamStatusCode: &tc.upstream, ErrorMessage: tc.message,
				UpstreamErrorMessage: &tc.message, ErrorSource: "upstream_http", ErrorBody: "original body",
				IsBusinessLimited: tc.oldLimited}
			want := *entry
			want.IsBusinessLimited = tc.wantLimited
			if tc.wantPayload {
				want.ErrorOwner, want.ErrorPhase, want.ErrorType = "client", "request", "context_limit"
			}
			normalizeOpsSLA(entry)
			require.Equal(t, &want, entry, "classification must retain original evidence")
			normalizeOpsSLA(entry)
			require.Equal(t, &want, entry, "normalization must be idempotent")
			if tc.wantPayload {
				require.Equal(t, "context_limit", ClassifyChannelMonitorV2Error(ChannelMonitorV2ErrorInput{
					ErrorType: entry.ErrorType, ErrorOwner: entry.ErrorOwner, StatusCode: entry.StatusCode,
					UpstreamStatusCode: tc.upstream, Message: tc.message,
				}))
			}
		})
	}
	normalizeOpsSLA(nil)
}

func TestOpsSLAPersistenceAfterFinalUpstreamEvidence(t *testing.T) {
	upstream := 413
	message := "Request payload is too large"
	entry := &OpsInsertErrorLogInput{StatusCode: 502, UpstreamStatusCode: &upstream,
		ErrorOwner: "provider", ErrorPhase: "upstream", ErrorType: "api_error",
		ErrorMessage: "upstream failed", UpstreamErrorMessage: &message, ErrorSource: "upstream_http"}
	called := false
	repo := &opsRepoMock{InsertErrorLogFn: func(_ context.Context, got *OpsInsertErrorLogInput) (int64, error) {
		called = true
		require.True(t, got.IsBusinessLimited)
		require.Equal(t, "client", got.ErrorOwner)
		require.Equal(t, "context_limit", got.ErrorType)
		require.Equal(t, 502, got.StatusCode)
		require.Equal(t, &upstream, got.UpstreamStatusCode)
		require.Equal(t, &message, got.UpstreamErrorMessage)
		return 1, nil
	}}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svc.RecordError(context.Background(), entry))
	require.True(t, called)
}

package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

// These former ForwardSystemOne assertions now traverse ExecuteSystemOne,
// including real selection, admission, provider decode, delivery and journal.
func TestExecuteSystemOneSchemaConformancePassthrough(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"omitted noul instructions", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"noul","extension":{"kept":true}}}}`},
		{"nullable noul fields", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"noul","instructions":null,"criteria":null}}}`},
		{"structured noul descriptions", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"noul","criteria":{"true":{"reason":"Yes"},"false":["No",null]}}}}`},
		{"object choice description", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"choice","criteria":{"a":{"description":"A","extra":null}}}}}`},
		{"array choice description", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"choice","instructions":null,"criteria":{"a":["A",null],"b":null}}}}`},
		{"empty choice criteria", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"choice","criteria":{}}}}`},
		{"one-level score", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"score","criteria":["only"]}}}`},
		{"object score level", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"score","instructions":null,"criteria":[{"description":"only","extra":null}]}}}`},
		{"array score level", `{"model":"jev-latest","state":"sample","questions":{"q":{"type":"score","criteria":[["only",null]]}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSystemOneExecutionFixture(t)
			f.in.Body = []byte(tc.body)
			f.in.OriginalWireDigest = sha256.Sum256(f.in.Body)
			f.transport.inspect = func(r *http.Request, _ native.TransportOptions) {
				require.Equal(t, typesafe.SystemOnePath, r.URL.Path)
				require.Equal(t, "Bearer ts-secret", r.Header.Get("Authorization"))
				got, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, f.in.Body, got)
			}
			f.reply(200, `{"answers":{},"usage":{"input_tokens":12}}`)
			result, err := f.execute()
			require.NoError(t, err)
			require.Equal(t, model.OperationCompleted, result.State)
			require.Equal(t, `{"answers":{},"usage":{"input_tokens":12}}`, f.writer.Body.String())
			require.EqualValues(t, 12, result.Attempts[0].Usage.Input.Value)
			require.Nil(t, result.Attempts[0].Usage.Output)
			require.Empty(t, result.Attempts[0].ReturnedModel)
			require.Nil(t, f.journal.projections[0].Settlement)
			require.Equal(t, model.BillingPendingUsage, result.Billing.Disposition)
		})
	}
}
func TestExecuteSystemOneEditableURLUsesExistingSecurityPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, base, want string
		policy           config.URLAllowlistConfig
		rejected         bool
	}{
		{"default", "", typesafe.DefaultBaseURL + typesafe.SystemOnePath, config.URLAllowlistConfig{}, false},
		{"api base", "https://relay.example/relay/v1/", "https://relay.example/relay/v1/systemone", config.URLAllowlistConfig{}, false},
		{"full endpoint", "https://relay.example/relay/v1/systemone/", "https://relay.example/relay/v1/systemone", config.URLAllowlistConfig{}, false},
		{"allowlisted", "https://relay.example/v1/", "https://relay.example/v1/systemone", config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"relay.example"}}, false},
		{"unlisted", "https://other.example/v1", "", config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"relay.example"}}, true},
		{"private host", "https://127.0.0.1/v1", "", config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"127.0.0.1"}}, true},
		{"private ipv6", "https://[::1]/v1", "", config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"::1"}}, true},
		{"http denied", "http://relay.example/v1", "", config.URLAllowlistConfig{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSystemOneExecutionFixture(t)
			f.svc.cfg.Security.URLAllowlist = tc.policy
			f.resource.Credentials["base_url"] = tc.base
			// Hostnames here are inspected by the fake transport, never dialed.
			f.transport.inspect = func(r *http.Request, _ native.TransportOptions) { require.Equal(t, tc.want, r.URL.String()) }
			result, err := f.execute()
			if tc.rejected {
				require.Error(t, err)
				require.Zero(t, f.transport.sends.Load())
				require.Zero(t, f.journal.stores)
				require.Empty(t, result.Attempts)
				require.Zero(t, f.delivered)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, f.transport.sends.Load())
				require.Equal(t, model.OperationCompleted, result.State)
			}
		})
	}
}
func TestTypeSafeAccountBaseURLNeverFallsBackToAnthropic(t *testing.T) {
	account := &Account{Platform: PlatformTypeSafe, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "ts-secret"}}
	require.Equal(t, typesafe.DefaultBaseURL, account.GetBaseURL())
	require.Equal(t, typesafe.DefaultBaseURL, account.GetTypeSafeBaseURL())
	for raw, want := range map[string]string{
		"https://api.typesafe.ai/v1":      "https://api.typesafe.ai",
		"https://api.typesafe.ai/V1/":     "https://api.typesafe.ai",
		"https://proxy.example/typesafe/": "https://proxy.example/typesafe",
		"https://proxy.example/apiv1":     "https://proxy.example/apiv1",
		" https://proxy.example/x/v1/ ":   "https://proxy.example/x",
		"/v1":                             typesafe.DefaultBaseURL,
	} {
		withBase := &Account{Platform: PlatformTypeSafe, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": raw}}
		require.Equal(t, want, withBase.GetTypeSafeBaseURL(), raw)
	}
	anthropic := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk"}}
	require.Equal(t, "https://api.anthropic.com", anthropic.GetBaseURL())
}

func TestTypeSafeModelsListCandidates(t *testing.T) {
	require.Equal(t, []string{typesafe.JevLatestModel}, defaultModelsListCandidateIDs(PlatformTypeSafe))
	require.NotContains(t, compositeDefaultModelsListCandidateIDs(), typesafe.JevLatestModel)
}

func TestExecuteSystemOneHTTPStatusAndAccountPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		status                            int
		custom, failover                  bool
		disabled, rateLimited, overloaded int
	}{
		{name: "bad request overrides custom rule", status: 400, custom: true},
		{name: "too large overrides custom rule", status: 413, custom: true},
		{name: "unprocessable overrides custom rule", status: 422, custom: true},
		{name: "unauthorized", status: 401, failover: true, disabled: 1},
		{name: "payment required", status: 402, failover: true, disabled: 1},
		{name: "forbidden", status: 403, failover: true, disabled: 1},
		{name: "rate limited", status: 429, failover: true, rateLimited: 1},
		{name: "overloaded", status: 529, failover: true, overloaded: 1},
		{name: "server error", status: 500, failover: true},
		{name: "unavailable", status: 503, failover: true},
		{name: "custom not found", status: 404, custom: true, failover: true, disabled: 1},
		{name: "unhandled conflict", status: 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSystemOneExecutionFixture(t)
			f.secondAccount()
			f.in.MaxAccountSwitches = 0
			if tc.custom {
				f.resource.Credentials["custom_error_codes_enabled"] = true
				f.resource.Credentials["custom_error_codes"] = []any{float64(tc.status)}
			}
			f.reply(tc.status, `{"detail":"private upstream body ts-secret"}`)
			result, err := f.execute()
			require.Error(t, err)
			require.Equal(t, model.OperationFailed, result.State)
			require.NotContains(t, err.Error(), "private")
			require.NotContains(t, err.Error(), "ts-secret")
			if tc.failover {
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, tc.status, failure.StatusCode)
				require.True(t, failure.ShouldRetryNextAccount())
				if tc.status == 401 {
					require.Equal(t, GatewayFailureStageAccountAuth, failure.Stage)
					require.Equal(t, GatewayFailureScopeAccount, failure.Scope)
					require.Equal(t, TypeSafeCredentialRejectedReason, failure.Reason)
				}
			} else {
				var failure *SystemOneUpstreamError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, tc.status, failure.StatusCode)
			}
			require.Len(t, f.repo.disabled, tc.disabled)
			require.Len(t, f.repo.rateLimited, tc.rateLimited)
			require.Len(t, f.repo.overloaded, tc.overloaded)
			require.Len(t, result.Attempts, 1)
			require.True(t, result.Attempts[0].Closed)
			require.Equal(t, "req-jev", result.Attempts[0].RequestID)
			require.Len(t, f.journal.projections, 1)
			require.Equal(t, tc.status, f.journal.projections[0].StatusCode)
			require.Nil(t, f.journal.projections[0].Settlement)
			require.EqualValues(t, 1, f.transport.sends.Load())
			require.Zero(t, f.delivered)
		})
	}
}

func TestExecuteSystemOneAmbiguousFailuresNeverReplay(t *testing.T) {
	for _, kind := range []string{"500", "503", "529", "402", "custom_404", "transport", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			f := newSystemOneExecutionFixture(t)
			f.secondAccount()
			var transportErr error
			switch kind {
			case "transport":
				transportErr = errors.New("network unavailable")
			case "timeout":
				transportErr = context.DeadlineExceeded
			case "500":
				f.reply(500, `{"error":"server"}`)
			case "503":
				f.reply(503, `{"error":"server"}`)
			case "529":
				f.reply(529, `{"error":"server"}`)
			case "402":
				f.reply(402, `{"error":"account"}`)
			case "custom_404":
				f.resource.Credentials["custom_error_codes_enabled"] = true
				f.resource.Credentials["custom_error_codes"] = []any{float64(404)}
				f.reply(404, `{"error":"missing"}`)
			}
			if transportErr != nil {
				f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) { return nil, transportErr }
			}
			result, err := f.execute()
			require.Error(t, err)
			if transportErr != nil {
				require.ErrorIs(t, err, transportErr)
			}
			require.Len(t, result.Attempts, 1)
			require.NotEqual(t, model.GenerationRejected, result.Attempts[0].Generation)
			require.NotEqual(t, model.SendNotSent, result.Attempts[0].Send)
			require.EqualValues(t, 1, f.transport.sends.Load())
			require.Zero(t, f.delivered)
			require.Len(t, f.journal.projections, 1)
			require.Nil(t, f.journal.projections[0].Settlement)
			require.Equal(t, model.BillingPendingUsage, result.Billing.Disposition)
		})
	}
}

func TestExecuteSystemOneNormalizesResponseContentType(t *testing.T) {
	for _, tc := range []struct{ upstream, want string }{
		{"application/json; charset=utf-8", "application/json; charset=utf-8"},
		{"application/problem+json", "application/problem+json"},
		{"text/html; charset=utf-8", "application/json"}, {"", "application/json"}, {"not a media type;;", "application/json"},
	} {
		t.Run(tc.upstream, func(t *testing.T) {
			f := newSystemOneExecutionFixture(t)
			reply := f.transport.reply
			f.transport.reply = func(r *http.Request, o native.TransportOptions) (*http.Response, error) {
				response, err := reply(r, o)
				response.Header.Set("Content-Type", tc.upstream)
				return response, err
			}
			_, err := f.execute()
			require.NoError(t, err)
			require.Equal(t, tc.want, f.writer.Header().Get("Content-Type"))
		})
	}
}
func TestExecuteSystemOneRejectsOversizedResponse(t *testing.T) {
	f := newSystemOneExecutionFixture(t)
	f.secondAccount()
	f.reply(200, `{"pad":"`+strings.Repeat("a", typesafe.MaxSystemOneResponseBytes)+`"}`)
	result, err := f.execute()
	require.ErrorIs(t, err, typesafe.ErrSystemOneResponseTooLarge)
	require.Equal(t, model.OperationFailed, result.State)
	require.Len(t, result.Attempts, 1)
	require.True(t, result.Attempts[0].Closed)
	require.Equal(t, "req-jev", result.Attempts[0].RequestID)
	require.Equal(t, model.TerminalUnknown, result.Attempts[0].Terminal)
	require.Zero(t, f.delivered)
	require.EqualValues(t, 1, f.transport.sends.Load())
	require.Len(t, f.journal.projections, 1)
	require.Equal(t, 200, f.journal.projections[0].StatusCode)
	require.Nil(t, f.journal.projections[0].Settlement)
	require.Equal(t, model.BillingPendingUsage, result.Billing.Disposition)
}
func TestExecuteSystemOneBillsLenientUsageShapes(t *testing.T) {
	f := newSystemOneExecutionFixture(t)
	f.reply(200, `{"answers":{},"usage":{"input_tokens":"21","output_tokens":1.0}}`)
	result, err := f.execute()
	require.NoError(t, err)
	require.Empty(t, result.Attempts[0].ReturnedModel)
	require.Equal(t, model.UsageFinal, result.Attempts[0].Usage.Confidence)
	require.EqualValues(t, 21, result.Attempts[0].Usage.Input.Value)
	require.EqualValues(t, 1, result.Attempts[0].Usage.Output.Value)
	require.Equal(t, 21, f.journal.projections[0].Settlement.UsageLog.InputTokens)
	require.Equal(t, 1, f.journal.projections[0].Settlement.UsageLog.OutputTokens)
	require.Equal(t, model.BillingSettled, result.Billing.Disposition)
}

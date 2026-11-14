package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bpsProbeUsageReadError struct {
	io.Reader
	err    error
	cancel context.CancelFunc
}

func (r bpsProbeUsageReadError) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		if r.cancel != nil {
			r.cancel()
		}
		return n, r.err
	}
	return n, err
}

func TestExcelBPS403RecoveryAttributesUsageOnActualWorkerPath(t *testing.T) {
	for _, tc := range []struct {
		name, terminal, usage, state  string
		mismatch, duplicate, failRead bool
		restored                      bool
		httpStatus                    int
	}{
		{name: "completed", terminal: "completed", usage: `{"input_tokens":11,"output_tokens":4,"total_tokens":15,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":3,"cache_write_tokens_details":{"ttl_30m_tokens":1,"ttl_12h_tokens":2}},"cache_creation":{"ephemeral_5m_input_tokens":1,"ephemeral_1h_input_tokens":2},"output_tokens_details":{"reasoning_tokens":2}}`, state: "measured", restored: true},
		{name: "absent usage", terminal: "completed", state: "unknown", restored: true},
		{name: "failed measured", terminal: "failed", usage: `{"input_tokens":11,"output_tokens":4}`, state: "lower_bound"},
		{name: "HTTP failed measured", terminal: "failed", usage: `{"input_tokens":11,"output_tokens":4}`, state: "lower_bound", httpStatus: http.StatusForbidden},
		{name: "incomplete measured", terminal: "incomplete", usage: `{"input_tokens":11,"output_tokens":4}`, state: "lower_bound"},
		{name: "interrupted measured", terminal: "interrupted", usage: `{"input_tokens":11,"output_tokens":4}`, state: "lower_bound"},
		{name: "EOF after watermark", terminal: "in_progress", usage: `{"input_tokens":11,"output_tokens":4}`, state: "unknown"},
		{name: "read failure after watermark", terminal: "in_progress", usage: `{"input_tokens":11,"output_tokens":4}`, state: "unknown", failRead: true},
		{name: "wrong nonce still consumed", terminal: "completed", usage: `{"input_tokens":11,"output_tokens":4}`, state: "measured", mismatch: true},
		{name: "duplicate not added", terminal: "completed", usage: `{"input_tokens":11,"output_tokens":4}`, state: "measured", duplicate: true},
		{name: "partial fields stay absent", terminal: "failed", usage: `{"input_tokens":11}`, state: "lower_bound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			now := time.Now().UTC()
			account := recoveryTestAccount(now)
			repo := &recoveryTestRepo{claim: true}
			calls := 0
			svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
				calls++
				raw, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				nonce := bpsProbeTestNonce.FindString(string(raw))
				require.NotEmpty(t, nonce)
				require.False(t, gjson.GetBytes(raw, "store").Bool())
				require.False(t, gjson.GetBytes(raw, "previous_response_id").Exists())
				if tc.mismatch {
					nonce = "unexpected"
				}
				response := map[string]any{"id": "resp_measured_probe", "model": "returned-probe-model", "status": tc.terminal,
					"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": nonce}}}}}
				if tc.usage != "" {
					response["usage"] = json.RawMessage(tc.usage)
				}
				event, err := json.Marshal(map[string]any{"type": "response." + tc.terminal, "response": response})
				require.NoError(t, err)
				wire := "data: " + string(event) + "\n\n"
				status := http.StatusOK
				if tc.httpStatus != 0 {
					status, wire = tc.httpStatus, string(event)
				}
				if tc.duplicate {
					wire += wire
				}
				var reader io.Reader = strings.NewReader(wire)
				if tc.failRead {
					reader = bpsProbeUsageReadError{Reader: reader, err: context.Canceled, cancel: cancel}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(reader)}, nil
			}}}
			svc.recoverExcelBPS403Account(ctx, account, now)
			require.Equal(t, 1, calls, "usage collection must never replay generation")
			var outcome ExcelBPS403ProbeResult
			if tc.restored {
				outcome = repo.restoredOutcome
				require.Equal(t, 1, repo.restores)
			} else {
				require.Zero(t, repo.restores)
				require.Len(t, repo.outcomes, 1)
				outcome = repo.outcomes[0]
				require.NoError(t, repo.recordContextErr)
			}
			require.NotNil(t, outcome.Usage)
			u := outcome.Usage
			require.Equal(t, excelBPSRecoveryProbeScope, u.Scope)
			require.Equal(t, account.ID, u.AccountID)
			require.Equal(t, now.Format(time.RFC3339Nano), u.ClaimAt)
			require.Equal(t, ExcelBPS403ProbeClaimResult(account, now).Usage.ProbeID, u.ProbeID)
			require.Equal(t, "gpt-6-astra", u.Model)
			require.Equal(t, "returned-probe-model", u.ResponseModel)
			require.Equal(t, "resp_measured_probe", u.ResponseID)
			require.Equal(t, "sent", u.SendState)
			require.Equal(t, tc.state, u.State)
			if tc.usage == "" {
				require.Nil(t, u.Tokens)
			} else {
				require.Equal(t, int64(11), *u.Tokens.Input)
				if tc.name == "partial fields stay absent" {
					require.Nil(t, u.Tokens.Output)
				} else {
					require.Equal(t, int64(4), *u.Tokens.Output)
				}
			}
			if tc.name == "completed" {
				require.Equal(t, int64(0), *u.Tokens.Cached)
				require.Equal(t, int64(2), *u.Tokens.Reasoning)
				require.Equal(t, int64(3), *u.Tokens.CacheCreation)
				require.Equal(t, int64(1), *u.Tokens.CacheCreation5m)
				require.Equal(t, int64(2), *u.Tokens.CacheCreation1h)
				require.Equal(t, int64(1), *u.Tokens.CacheCreation30m)
				require.Equal(t, int64(2), *u.Tokens.CacheCreation12h)
			} else if u.Tokens != nil {
				require.Nil(t, u.Tokens.CacheCreation)
				require.Nil(t, u.Tokens.CacheCreation5m)
				require.Nil(t, u.Tokens.CacheCreation1h)
				require.Nil(t, u.Tokens.CacheCreation30m)
				require.Nil(t, u.Tokens.CacheCreation12h)
			}
			raw, err := json.Marshal(outcome)
			require.NoError(t, err)
			for _, secretOrBilling := range []string{"test-token", "api_key_id", "user_id", "billing_root", "nonce"} {
				require.NotContains(t, string(raw), secretOrBilling)
			}
		})
	}
}

func recoveryTestAccount(now time.Time) *Account {
	a := excelAccount()
	a.ID, a.Status, a.Schedulable = 27, StatusActive, true
	a.Extra["openai_excel_bps"] = false
	a.Extra["openai_excel_bps_auto_disable_on_403"] = true
	a.Extra[ExcelBPSAutoRecoverOn403Key] = true
	a.Extra[ExcelBPS403DisabledAtKey] = now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	a.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	return a
}

func TestExcelBPS403RecoveryDue(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name   string
		mutate func(*Account)
		due    bool
	}{
		{"due", func(*Account) {}, true},
		{"first hour", func(a *Account) {
			a.Extra[ExcelBPS403DisabledAtKey] = now.Add(-time.Hour + time.Second).Format(time.RFC3339Nano)
		}, false},
		{"hour boundary", func(a *Account) { a.Extra[ExcelBPS403DisabledAtKey] = now.Add(-time.Hour).Format(time.RFC3339Nano) }, true},
		{"recent attempt", func(a *Account) { a.Extra[ExcelBPS403LastProbeAtKey] = now.Add(-time.Minute).Format(time.RFC3339Nano) }, false},
		{"next hour", func(a *Account) { a.Extra[ExcelBPS403LastProbeAtKey] = now.Add(-time.Hour).Format(time.RFC3339Nano) }, true},
		{"new shutdown", func(a *Account) {
			a.Extra[ExcelBPS403LastProbeAtKey] = now.Add(-3 * time.Hour).Format(time.RFC3339Nano)
			a.Extra[ExcelBPS403DisabledAtKey] = now.Format(time.RFC3339Nano)
		}, false},
		{"manual shutdown", func(a *Account) { delete(a.Extra, ExcelBPS403DisabledAtKey) }, false},
		{"opted out", func(a *Account) { a.Extra[ExcelBPSAutoRecoverOn403Key] = false }, false},
		{"auto shutdown off", func(a *Account) { a.Extra["openai_excel_bps_auto_disable_on_403"] = false }, false},
		{"already enabled", func(a *Account) { a.Extra["openai_excel_bps"] = true }, false},
		{"free account", func(a *Account) { a.Credentials["plan_type"] = "free" }, false},
		{"inactive", func(a *Account) { a.Status = "disabled" }, false},
		{"paused", func(a *Account) { a.Schedulable = false }, false},
		{"expired", func(a *Account) { at := now.Add(-time.Minute); a.ExpiresAt = &at; a.AutoPauseOnExpired = true }, false},
		{"API key", func(a *Account) { a.Type = AccountTypeAPIKey }, false},
		{"shadow", func(a *Account) { id := int64(1); a.ParentAccountID = &id }, false},
		{"invalid shutdown", func(a *Account) { a.Extra[ExcelBPS403DisabledAtKey] = "bad" }, false},
		{"invalid attempt", func(a *Account) { a.Extra[ExcelBPS403LastProbeAtKey] = 123 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := recoveryTestAccount(now)
			tc.mutate(a)
			require.Equal(t, tc.due, a.ExcelBPS403RecoveryDue(now))
		})
	}
	require.False(t, (*Account)(nil).ExcelBPS403RecoveryDue(now))
}

type recoveryTestRepo struct {
	AccountRepository
	claim            bool
	claimErr         error
	claims, restores int
	snapshot         *Account
	restoredOutcome  ExcelBPS403ProbeResult
	outcomes         []ExcelBPS403ProbeResult
	recordContextErr error
	failures         []ExcelBPSFailure
	disable403, move int
}

func (r *recoveryTestRepo) ClaimExcelBPS403Probe(_ context.Context, a *Account, _ time.Time) (bool, error) {
	r.claims++
	return r.claim, r.claimErr
}
func (r *recoveryTestRepo) RestoreExcelBPSAfter403(_ context.Context, a *Account, outcome ExcelBPS403ProbeResult) (bool, error) {
	r.restores++
	r.snapshot = a
	r.restoredOutcome = outcome
	return true, nil
}

func (r *recoveryTestRepo) RecordExcelBPS403ProbeResult(ctx context.Context, _ *Account, outcome ExcelBPS403ProbeResult) (bool, error) {
	r.recordContextErr = ctx.Err()
	r.outcomes = append(r.outcomes, outcome)
	return true, nil
}

func (r *recoveryTestRepo) DisableExcelBPSForFailure(_ context.Context, _ *Account, failure ExcelBPSFailure) (bool, error) {
	r.failures = append(r.failures, failure)
	return true, nil
}

func (r *recoveryTestRepo) DisableExcelBPSOn403(context.Context, *Account) (bool, error) {
	r.disable403++
	return true, nil
}

func (r *recoveryTestRepo) MoveExcelBPSOn403(context.Context, *Account) (bool, error) {
	r.move++
	return true, nil
}

func TestExcelBPS403RecoveryCannotSendWithoutDurableClaimEvidence(t *testing.T) {
	now := time.Now().UTC()
	upstream := &bpsProbeUpstream{}
	// Even an attempted claim update cannot permit network I/O when the
	// repository reports that its evidence insert/commit could not be proven.
	repo := &recoveryTestRepo{claim: true, claimErr: fmt.Errorf("probe claim evidence persistence failed")}
	svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream}
	svc.recoverExcelBPS403Account(t.Context(), recoveryTestAccount(now), now)
	require.Equal(t, 1, repo.claims)
	require.Empty(t, upstream.bodies)
	require.Zero(t, repo.restores)
	require.Empty(t, repo.outcomes)
}

func TestExcelBPS403RecoveryRestoresOnlyAfterSuccessfulClaimAndProbe(t *testing.T) {
	for _, tc := range []struct {
		name, mode         string
		claim              bool
		requests, restores int
	}{
		{"healthy", "", true, 1, 1}, {"wrong answer", "wrong_basic", true, 1, 0},
		{"failed terminal", "failed_terminal", true, 1, 0}, {"another worker claimed", "", false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			a := recoveryTestAccount(now)
			upstream := &bpsProbeUpstream{mode: tc.mode}
			repo := &recoveryTestRepo{claim: tc.claim}
			svc := &OpenAIGatewayService{httpUpstream: upstream, accountRepo: repo}
			svc.recoverExcelBPS403Account(t.Context(), a, now)
			require.Len(t, upstream.bodies, tc.requests)
			require.Equal(t, tc.restores, repo.restores)
			if tc.claim && tc.restores == 0 {
				require.Len(t, repo.outcomes, 1)
				require.Equal(t, "failed", repo.outcomes[0].Status)
				require.NoError(t, repo.recordContextErr)
			} else {
				require.Empty(t, repo.outcomes)
			}
			require.False(t, a.IsExcelBPSEnabled())
			require.NotContains(t, a.Extra, ExcelBPS403LastProbeAtKey)
			if repo.snapshot != nil {
				require.False(t, repo.snapshot.IsExcelBPSEnabled())
				require.Equal(t, now.Format(time.RFC3339Nano), repo.snapshot.Extra[ExcelBPS403LastProbeAtKey])
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(upstream.bodies[0], "model").String())
			}
		})
	}
}

func TestExcelBPS403RecoveryProbeRejectsHTTPAndIncompleteResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		wire   string
	}{
		{"forbidden", 403, "denied"}, {"unauthorized", 401, "expired"}, {"rate limited", 429, "wait"},
		{"HTML", 200, "<html>ok</html>"}, {"empty", 200, ""}, {"truncated", 200, "data: {"},
		{"incomplete", 200, "data: {\"type\":\"response.incomplete\"}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := &bpsTestUpstream{send: func(req *http.Request, proxy string) (*http.Response, error) {
				calls++
				require.Equal(t, "bps.openai.com", req.URL.Host)
				require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
				require.Equal(t, "test-account", req.Header.Get("Chatgpt-Account-Id"))
				require.Equal(t, HTTPUpstreamProfileExcelBPS, HTTPUpstreamProfileFromContext(req.Context()))
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.wire))}, nil
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			a := recoveryTestAccount(time.Now())
			_, err := svc.probeExcelBPS403Recovery(t.Context(), a)
			require.Error(t, err)
			require.Equal(t, 1, calls)
			require.False(t, a.IsExcelBPSEnabled())
		})
	}
}

func TestExcelBPS403RecoveryClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range []struct {
		name, detail          string
		reauth, modelCooldown bool
	}{
		{"expired access token", `{"code":"token_expired","status":401}`, true, false},
		{"account forbidden", `{"code":"account_deactivated","status":403}`, false, false},
		{"ambiguous forbidden", `{"code":"permission_denied","status":403}`, false, false},
		{"model permission", `{"code":"basispoints_model_access_changed","status":403}`, false, true},
		{"model not allowed", `{"code":"model_not_allowed","status":403}`, false, true},
		{"content rejection", `{"code":"content_policy_violation","status":403}`, false, false},
		{"cyber with auth status", `{"code":"cyber_policy","status":401}`, false, false},
		{"file ownership", `{"type":"server_error","message":"Unknown error while validating file ownership.","status":500}`, false, false},
		{"request rejection", `{"code":"invalid_request_error","status":400}`, false, false},
	} {
		for _, kind := range []string{"http", "response.failed", "error"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				now := time.Now()
				a := recoveryTestAccount(now)
				a.Extra[ExcelBPSAutoMoveOn403Key], a.Extra[ExcelBPS403TargetGroupIDKey] = true, 0
				repo := &recoveryTestRepo{claim: true}
				payload := fmt.Sprintf(`{"type":"error","error":%s}`, tc.detail)
				if kind == "response.failed" {
					payload = fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp_probe_failure","status":"failed","error":%s}}`, tc.detail)
				}
				status, stage, reason := http.StatusOK, "response", "upstream_rejected"
				wire := "data: " + payload + "\n\n"
				if kind == "http" {
					status, stage, reason = int(gjson.Get(tc.detail, "status").Int()), "request", "http_rejected"
					wire = fmt.Sprintf(`{"error":%s}`, tc.detail)
				}
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(status, wire)}
				svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream}
				svc.recoverExcelBPS403Account(t.Context(), a, now)
				require.Len(t, upstream.requests, 1, "observing a failure must not generate again")
				require.Zero(t, repo.restores)
				require.Len(t, repo.outcomes, 1)
				require.Equal(t, stage, repo.outcomes[0].Stage)
				require.Equal(t, reason, repo.outcomes[0].Reason)
				require.Equal(t, status, repo.outcomes[0].HTTPStatus, "semantic failure status must not overwrite the actual HTTP status")
				if tc.reauth {
					require.Equal(t, []ExcelBPSFailure{{Reason: "refresh_grant_missing", ReauthRequired: true, GrantFingerprint: "primary"}}, repo.failures)
				} else {
					require.Empty(t, repo.failures)
				}
				require.Equal(t, tc.modelCooldown, svc.isExcelBPSUpstreamModelCoolingDown(a.ID, "gpt-6-astra"))
				require.False(t, svc.isExcelBPSUpstreamModelCoolingDown(a.ID, "gpt-5.1"))
				require.Zero(t, repo.disable403, "a probe cannot create another shutdown identity")
				require.Zero(t, repo.move, "a disabled recovery snapshot cannot repeat group actions")
				require.False(t, a.IsExcelBPSEnabled())
				require.Equal(t, "test-token", a.GetCredential("access_token"))
			})
		}
	}
}

func TestExcelBPS403RecoveryInBand401PreservesGrantRefreshBoundary(t *testing.T) {
	for _, state := range []string{"refreshable", "refreshed", "reauthorized"} {
		t.Run(state, func(t *testing.T) {
			oauth, _, account := excelGrantService(t)
			account.Extra = recoveryTestAccount(time.Now()).Extra
			grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "probe-old")
			rotated := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "probe-new")
			require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
			refreshes := 0
			oauth.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
				refreshes++
				return &openai.TokenResponse{AccessToken: rotated["access_token"].(string), RefreshToken: rotated["refresh_token"].(string), IDToken: rotated["id_token"].(string)}, nil
			}}
			if state == "refreshed" {
				require.NoError(t, oauth.rejectExcelAccessToken(t.Context(), account, grant["access_token"].(string), nil))
				_, err := oauth.ExcelAccessToken(t.Context(), account, nil)
				require.NoError(t, err)
				require.Equal(t, 1, refreshes)
			}
			before, _, err := oauth.readExcelGrant(t.Context(), account)
			require.NoError(t, err)
			baselineRefreshes := refreshes
			repo := &recoveryTestRepo{}
			calls := 0
			svc := &OpenAIGatewayService{accountRepo: repo, openAITokenProvider: &OpenAITokenProvider{openAIOAuthService: oauth}}
			svc.httpUpstream = &bpsTestUpstream{send: func(*http.Request, string) (*http.Response, error) {
				calls++
				if state == "reauthorized" {
					require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, rotated))
				}
				return bpsCompletionResponse(http.StatusOK, "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"token_expired\",\"status\":401}}}\n\n"), nil
			}}
			_, err = svc.probeExcelBPS403Recovery(t.Context(), account)
			var failure *basispoints.UpstreamFailure
			require.ErrorAs(t, err, &failure)
			require.Equal(t, http.StatusUnauthorized, failure.Status)
			require.Equal(t, 1, calls)
			require.Equal(t, baselineRefreshes, refreshes, "terminal observation cannot start a second refresh or generation")
			_, stored, err := oauth.readExcelGrant(t.Context(), account)
			require.NoError(t, err)
			if state == "refreshed" {
				require.Equal(t, []ExcelBPSFailure{{Reason: "refreshed_access_token_rejected", ReauthRequired: true, GrantFingerprint: ExcelGrantFingerprint(before)}}, repo.failures)
			} else {
				require.Empty(t, repo.failures, "neither a refreshable token nor a replacement grant is revoked by this probe")
				require.Equal(t, state == "refreshable", stored[excelAccessTokenRejectedKey] == true)
			}
			if state == "reauthorized" {
				require.Equal(t, rotated["access_token"], stored["access_token"])
			}
			require.Equal(t, "test-token", account.GetCredential("access_token"), "independent Excel rejection cannot change Native credentials")
		})
	}
}

func TestExcelBPS403RecoveryIgnoresNonterminalAuthorizationText(t *testing.T) {
	repo := &recoveryTestRepo{}
	upstream := &bpsProbeUpstream{}
	svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: &bpsTestUpstream{send: func(req *http.Request, proxy string) (*http.Response, error) {
		resp, err := upstream.Do(req, proxy, 27, 1)
		require.NoError(t, err)
		wire, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		prefix := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"token_expired account_deactivated\",\"error\":{\"code\":\"token_expired\",\"status\":401}}\n\n"
		resp.Body = io.NopCloser(strings.NewReader(prefix + string(wire)))
		return resp, nil
	}}}
	_, err := svc.probeExcelBPS403Recovery(t.Context(), recoveryTestAccount(time.Now()))
	require.NoError(t, err)
	require.Len(t, upstream.bodies, 1)
	require.Empty(t, repo.failures)
}

type recoveryGuardTestRepo struct {
	recoveryTestRepo
	valid bool
}

func (r *recoveryGuardTestRepo) ReadExcelBPSRecoverySnapshot(_ context.Context, account *Account, _ string) (*Account, error) {
	return account, nil
}

func (r *recoveryGuardTestRepo) ValidateExcelBPSRecovery(context.Context, *Account) (bool, error) {
	return r.valid, nil
}

func TestExcelBPS403RecoverySuccessStillRequiresCurrentAuthorization(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed=%t", changed), func(t *testing.T) {
			repo := &recoveryGuardTestRepo{recoveryTestRepo: recoveryTestRepo{claim: true}, valid: true}
			upstream := &bpsProbeUpstream{}
			svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: &bpsTestUpstream{send: func(req *http.Request, proxy string) (*http.Response, error) {
				resp, err := upstream.Do(req, proxy, 27, 1)
				// The authority may reject a replaced grant, claim or policy even
				// when this old probe completed with the exact requested nonce.
				repo.valid = !changed
				return resp, err
			}}}
			now := time.Now()
			svc.recoverExcelBPS403Account(t.Context(), recoveryTestAccount(now), now)
			require.Len(t, upstream.bodies, 1)
			if changed {
				require.Zero(t, repo.restores)
				require.Len(t, repo.outcomes, 1)
				require.Equal(t, "authorization_changed", repo.outcomes[0].Reason)
			} else {
				require.Equal(t, 1, repo.restores)
				require.Empty(t, repo.outcomes)
			}
			require.Empty(t, repo.failures)
		})
	}
}

func TestExcelBPS403RecoveryPersistsClassifiedTimeoutOutcome(t *testing.T) {
	now := time.Now()
	repo := &recoveryTestRepo{claim: true}
	svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: &bpsTestUpstream{send: func(*http.Request, string) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	}}}
	svc.recoverExcelBPS403Account(t.Context(), recoveryTestAccount(now), now)
	require.Zero(t, repo.restores)
	require.Len(t, repo.outcomes, 1)
	require.Equal(t, "request", repo.outcomes[0].Stage)
	require.Equal(t, "timeout", repo.outcomes[0].Reason)
	require.Equal(t, "failed", repo.outcomes[0].Status)
	require.NoError(t, repo.recordContextErr)
	require.Equal(t, "unknown", repo.outcomes[0].Usage.State)
	require.Equal(t, "ambiguous", repo.outcomes[0].Usage.SendState)
	require.Nil(t, repo.outcomes[0].Usage.Tokens)
}

func TestExcelBPS403RecoveryEmptyScopeNeverProbesAnotherModel(t *testing.T) {
	for _, selection := range []string{"missing", "empty", "malformed", "images-only"} {
		t.Run(selection, func(t *testing.T) {
			a := recoveryTestAccount(time.Now())
			switch selection {
			case "missing":
				delete(a.Extra, "openai_excel_bps_models")
			case "empty":
				a.Extra["openai_excel_bps_models"] = []any{}
			case "malformed":
				a.Extra["openai_excel_bps_models"] = "gpt-6-astra"
			case "images-only":
				a.Extra["openai_excel_bps_models"] = []string{"gpt-image-2"}
			}
			upstream := &bpsProbeUpstream{}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			_, err := svc.probeExcelBPS403Recovery(t.Context(), a)
			require.Error(t, err)
			require.Empty(t, upstream.bodies)
		})
	}
}

func TestExcelBPS403RecoveryMarkerProtectedAcrossEdits(t *testing.T) {
	at := time.Now().UTC().Format(time.RFC3339Nano)
	current := map[string]any{ExcelBPS403DisabledAtKey: at, ExcelBPS403LastProbeAtKey: at}
	extra := MergeExcelBPS403Marker(map[string]any{ExcelBPS403LastProbeAtKey: "fake"}, current)
	require.Equal(t, at, extra[ExcelBPS403LastProbeAtKey])
	extra = MergeExcelBPS403Marker(map[string]any{"openai_excel_bps": true}, current)
	require.NotContains(t, extra, ExcelBPS403LastProbeAtKey)
	require.NotContains(t, extra, ExcelBPS403DisabledAtKey)
}

type recoveryLifecycleRepo struct {
	recoveryTestRepo
	started chan struct{}
}

func (r *recoveryLifecycleRepo) ListByPlatform(ctx context.Context, _ string) ([]Account, error) {
	close(r.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestExcelBPS403RecoveryWorkerLifecycle(t *testing.T) {
	repo := &recoveryLifecycleRepo{started: make(chan struct{})}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: excelBPSSettingsTestService()}
	svc.StartBPS403Recovery(context.Background())
	svc.StartBPS403Recovery(context.Background())
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("recovery worker did not start")
	}
	done := make(chan struct{})
	go func() { svc.StopBPS403Recovery(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery worker did not stop")
	}
	svc.StartBPS403Recovery(context.Background())
	svc.StopBPS403Recovery(context.Background())
}

func TestExcelBPS403RecoveryPolicyRequiresAutomaticShutdown(t *testing.T) {
	extra := map[string]any{ExcelBPSAutoRecoverOn403Key: "true"}
	_, err := normalizeBulkExcelBPSExtra(extra)
	require.Error(t, err)
	extra = map[string]any{"openai_excel_bps": false, ExcelBPSAutoRecoverOn403Key: true}
	_, err = normalizeBulkExcelBPSExtra(extra)
	require.NoError(t, err)
	require.Equal(t, false, extra[ExcelBPSAutoRecoverOn403Key])
}

func TestExcelBPS403NewCycleDoesNotReusePreviousFailure(t *testing.T) {
	now := time.Now().UTC()
	a := recoveryTestAccount(now)
	a.Extra[ExcelBPS403DisabledAtKey] = now.Add(-time.Minute).Format(time.RFC3339Nano)
	a.Extra[ExcelBPS403LastProbeResultKey] = ExcelBPS403ProbeResult{Status: "failed", Reason: "http_rejected", HTTPStatus: 403, CompletedAt: now.Add(-time.Hour).Format(time.RFC3339Nano)}
	state := ExcelBPS403RecoveryStatusFromAccount(a, true, now)
	require.Equal(t, "pending", state.State)
	require.NotNil(t, state.LastResult)
	require.Equal(t, "failed", state.LastResult.Status)
	require.False(t, a.ExcelBPS403RecoveryDue(now))
	current := a.Extra
	extra := MergeExcelBPS403Marker(map[string]any{"openai_excel_bps": true}, current)
	require.Contains(t, extra, ExcelBPS403LastProbeResultKey)
	require.NotContains(t, extra, ExcelBPS403DisabledAtKey)
}

func TestExcelBPS403ProbeRetainsSafeProviderIdentifiers(t *testing.T) {
	a := recoveryTestAccount(time.Now())
	repo := &recoveryTestRepo{claim: true}
	upstream := &bpsTestUpstream{send: func(*http.Request, string) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{"X-Request-Id": []string{"fe16cf52-47cd-418c-88d2-b88928bb3106"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"account_deactivated","type":"permission_error","message":"SECRET BODY"}}`))}, nil
	}}
	svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream}
	svc.recoverExcelBPS403Account(t.Context(), a, time.Now())
	raw, err := json.Marshal(repo.outcomes[0])
	require.NoError(t, err)
	require.Contains(t, string(raw), "account_deactivated")
	require.Contains(t, string(raw), "permission_error")
	require.Contains(t, string(raw), "fe16cf52-47cd-418c-88d2-b88928bb3106")
	require.NotContains(t, string(raw), "SECRET BODY")
}

func TestExcelBPS403RecoveryOnlyProbesAfterActualShutdown(t *testing.T) {
	for _, restored := range []bool{false, true} {
		a := recoveryTestAccount(time.Now())
		delete(a.Extra, ExcelBPS403DisabledAtKey)
		if restored {
			a.Extra["openai_excel_bps"] = true
		}
		a.Extra[ExcelBPS403LastProbeResultKey] = ExcelBPS403ProbeResult{Status: "failed", CompletedAt: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}
		upstream := &bpsProbeUpstream{}
		repo := &recoveryTestRepo{claim: true}
		svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream}
		svc.recoverExcelBPS403Account(t.Context(), a, time.Now().Add(24*time.Hour))
		require.Zero(t, repo.claims)
		require.Empty(t, upstream.bodies)
	}
}
func TestExcelBPS403ProbeRejectsUnboundedOrSensitiveDiagnostics(t *testing.T) {
	for _, raw := range []string{
		`{"error":{"code":"sk-private-token","type":"Bearer secret","message":"SECRET_BODY"}}`,
		`<html>SECRET_BODY</html>`, strings.Repeat("SECRET_BODY", 2000),
	} {
		resp := &http.Response{StatusCode: 403, Header: http.Header{"X-Request-Id": []string{"Bearer SECRET_HEADER"}}, Body: io.NopCloser(strings.NewReader(raw))}
		result := excelBPS403ProbeResult(excelBPSProbeHTTPFailure(resp), time.Now())
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "SECRET")
		require.NotContains(t, string(encoded), "sk-private")
		require.Empty(t, result.UpstreamRequestID)
		require.Equal(t, 403, result.HTTPStatus)
	}
}
func TestExcelBPS403RecoveryAuditSurvivesWarnLevel(t *testing.T) {
	_, cleanup := captureStructuredLog(t)
	t.Cleanup(cleanup)
	oldLevel := logger.CurrentLevel()
	require.NoError(t, logger.SetLevel("warn"))
	sink := &OpsSystemLogSink{queue: make(chan *logger.LogEvent, 16)}
	logger.SetSink(sink)
	t.Cleanup(func() { logger.SetSink(nil); _ = logger.SetLevel(oldLevel) })
	a := recoveryTestAccount(time.Now())
	upstream := &bpsProbeUpstream{}
	repo := &recoveryTestRepo{claim: true}
	svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream}
	svc.recoverExcelBPS403Account(t.Context(), a, time.Now())
	select {
	case event := <-sink.queue:
		require.Equal(t, "excel_bps.recovery_restored", event.Message)
		require.Equal(t, "info", event.Level)
		require.Equal(t, "audit.excel_bps_recovery", event.Component)
		require.Equal(t, int64(27), event.Fields["account_id"])
		require.Equal(t, "enabled", event.Fields["to"])
	default:
		t.Fatal("successful restoration was not recorded at warn level")
	}
	require.Empty(t, sink.queue, "success must not produce duplicate Ops events")
	recordExcelBPSRecoveryEvent("excel_bps.disabled_after_403", a.ID, map[string]any{"upstream_status_code": 403})
	require.Equal(t, "excel_bps.disabled_after_403", (<-sink.queue).Message)
}

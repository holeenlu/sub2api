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

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

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
	claims, restores int
	snapshot         *Account
	result           *ExcelBPS403ProbeResult
}

func (r *recoveryTestRepo) ClaimExcelBPS403Probe(_ context.Context, a *Account, _ time.Time) (bool, error) {
	r.claims++
	return r.claim, nil
}
func (r *recoveryTestRepo) RecordExcelBPS403ProbeResult(ctx context.Context, a *Account, result ExcelBPS403ProbeResult) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.result = &result
	return true, nil
}
func (r *recoveryTestRepo) RestoreExcelBPSAfter403(_ context.Context, a *Account) (bool, error) {
	r.restores++
	r.snapshot = a
	return true, nil
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
				require.NotNil(t, repo.result)
				require.Equal(t, "failed", repo.result.Status)
				require.NotEmpty(t, repo.result.Reason)
				require.NotEmpty(t, repo.result.CompletedAt)
			} else {
				require.Nil(t, repo.result)
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
			require.Error(t, svc.probeExcelBPS403Recovery(t.Context(), a))
			require.Equal(t, 1, calls)
			require.False(t, a.IsExcelBPSEnabled())
		})
	}
}

func TestExcelBPS403RecoveryEmptyScopeNeverProbesAnotherModel(t *testing.T) {
	a := recoveryTestAccount(time.Now())
	a.Extra["openai_excel_bps_models"] = []any{}
	upstream := &bpsProbeUpstream{}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	require.Error(t, svc.probeExcelBPS403Recovery(t.Context(), a))
	require.Empty(t, upstream.bodies)
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
	svc.StartBPS403Recovery()
	svc.StartBPS403Recovery()
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("recovery worker did not start")
	}
	done := make(chan struct{})
	go func() { svc.StopBPS403Recovery(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery worker did not stop")
	}
	svc.StartBPS403Recovery()
	svc.StopBPS403Recovery()
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

func TestExcelBPS403RecoveryStateMatchesPolicyAndClassifiesPause(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, state, reason string
		global              bool
		mutate              func(*Account)
	}{
		{"waiting", "pending", "", true, func(*Account) {}},
		{"global stopped", "paused", "global_disabled", false, func(*Account) {}},
		{"cloud", "paused", "cloud_enabled", true, func(a *Account) { a.Extra[OpenAICloudEnabledKey] = true }},
		{"manual stop", "paused", "manual_disabled", true, func(a *Account) { delete(a.Extra, "openai_excel_bps_auto_disable_on_403") }},
		{"recovery stopped", "paused", "recovery_disabled", true, func(a *Account) { delete(a.Extra, ExcelBPSAutoRecoverOn403Key) }},
		{"paused scheduling", "paused", "scheduling_paused", true, func(a *Account) { a.Schedulable = false }},
		{"inactive", "paused", "account_inactive", true, func(a *Account) { a.Status = "disabled" }},
		{"expired", "paused", "account_expired", true, func(a *Account) { at := now.Add(-time.Minute); a.ExpiresAt = &at; a.AutoPauseOnExpired = true }},
		{"claimed", "probing", "", true, func(a *Account) { a.Extra[ExcelBPS403LastProbeAtKey] = now.Format(time.RFC3339Nano) }},
		{"interrupted", "unknown", "", true, func(a *Account) { a.Extra[ExcelBPS403LastProbeAtKey] = now.Add(-time.Minute).Format(time.RFC3339Nano) }},
		{"failed", "failed", "", true, func(a *Account) {
			a.Extra[ExcelBPS403LastProbeAtKey] = now.Add(-time.Minute).Format(time.RFC3339Nano)
			a.Extra[ExcelBPS403LastProbeResultKey] = ExcelBPS403ProbeResult{Status: "failed", Reason: "http_rejected", HTTPStatus: 403, CompletedAt: now.Format(time.RFC3339Nano)}
		}},
		{"enabled again", "history", "", true, func(a *Account) { a.Extra["openai_excel_bps"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := recoveryTestAccount(now)
			a.Extra[ExcelBPS403RecoveryIntervalMinutesKey] = 30
			tc.mutate(a)
			state := ExcelBPS403RecoveryStatusFromAccount(a, tc.global, now)
			require.Equal(t, tc.state, state.State)
			require.Equal(t, tc.reason, state.Reason)
			require.Equal(t, 30, state.IntervalMinutes)
			if tc.state == "paused" || tc.state == "history" {
				require.Empty(t, state.NextProbeAt)
			} else {
				require.NotEmpty(t, state.NextProbeAt)
				require.True(t, a.IsExcelBPS403RecoveryPending())
			}
		})
	}
	require.Nil(t, ExcelBPS403RecoveryStatusFromAccount(nil, true, now))
	require.Nil(t, ExcelBPS403RecoveryStatusFromAccount(excelAccount(), true, now))
}

func TestExcelBPS403ProbeResultIsSanitizedAndPersistsAfterTimeout(t *testing.T) {
	now := time.Now().UTC()
	a := recoveryTestAccount(now)
	repo := &recoveryTestRepo{claim: true}
	upstream := &bpsTestUpstream{send: func(*http.Request, string) (*http.Response, error) {
		return nil, &excelBPS403ProbeError{Stage: "request", Reason: "do_not_expose", cause: context.DeadlineExceeded}
	}}
	svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream}
	svc.recoverExcelBPS403Account(t.Context(), a, now)
	require.NotNil(t, repo.result)
	require.Equal(t, "timeout", repo.result.Reason)
	require.Equal(t, "request", repo.result.Stage)
	require.Equal(t, 0, repo.restores)
	// Status is captured separately from the upstream body, including arbitrary
	// HTML/token-like content which must never reach the saved diagnostic.
	upstream.send = func(*http.Request, string) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("secret upstream body"))}, nil
	}
	svc.recoverExcelBPS403Account(t.Context(), a, now)
	require.Equal(t, 403, repo.result.HTTPStatus)
	require.Equal(t, "http_rejected", repo.result.Reason)
	raw, err := json.Marshal(repo.result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
}

func TestExcelBPS403RecoveryNeverRestoresAfterCancellationOrGlobalDisable(t *testing.T) {
	for _, globalStop := range []bool{false, true} {
		t.Run(fmt.Sprint(globalStop), func(t *testing.T) {
			now := time.Now()
			repo := &recoveryTestRepo{claim: true}
			settingsRepo := &excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSEnabled: "true"}}
			settings := &SettingService{settingRepo: settingsRepo}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			healthy := &bpsProbeUpstream{}
			upstream := &bpsTestUpstream{send: func(req *http.Request, proxy string) (*http.Response, error) {
				resp, err := healthy.Do(req, proxy, 27, 1)
				if globalStop {
					settingsRepo.values[SettingKeyExcelBPSEnabled] = "false"
					settings.invalidateExcelBPSEnabledCache()
				} else {
					cancel()
				}
				return resp, err
			}}
			svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream, settingService: settings}
			svc.recoverExcelBPS403Account(ctx, recoveryTestAccount(now), now)
			require.Zero(t, repo.restores)
			require.NotNil(t, repo.result)
			if globalStop {
				require.Equal(t, "global_disabled", repo.result.Reason)
			} else {
				require.Equal(t, "canceled", repo.result.Reason)
			}
		})
	}
}

func TestExcelBPS403RecoverySkipsImageOnlyModels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		models []string
		want   string
	}{
		{"mixed", []string{"gpt-image-2", "gpt-6-astra"}, "gpt-6-astra"},
		{"image only", []string{"gpt-image-2"}, ""},
		{"explicit empty", []string{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := recoveryTestAccount(time.Now())
			a.Extra["openai_excel_bps_models"] = tc.models
			require.Equal(t, tc.want, excelBPS403RecoveryModel(a))
			if tc.want == "" {
				require.False(t, a.ExcelBPS403RecoveryDue(time.Now()))
				state := ExcelBPS403RecoveryStatusFromAccount(a, true, time.Now())
				require.Equal(t, "paused", state.State)
				require.Equal(t, "no_text_model", state.Reason)
				require.Empty(t, state.NextProbeAt)
			} else {
				upstream := &bpsProbeUpstream{}
				svc := &OpenAIGatewayService{httpUpstream: upstream}
				require.NoError(t, svc.probeExcelBPS403Recovery(t.Context(), a))
				require.Len(t, upstream.bodies, 1)
				require.Equal(t, tc.want, gjson.GetBytes(upstream.bodies[0], "model").String())
			}
		})
	}
}

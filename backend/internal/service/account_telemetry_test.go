//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTelemetryMatchesPinnedVM2APIOracle(t *testing.T) {
	raw, err := os.ReadFile("testdata/account_telemetry_vm2api.json")
	require.NoError(t, err)
	var fixture struct {
		Input               map[string]any
		Init, Success, Eval json.RawMessage
		Now                 string
		Uptime              float64
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	str := func(key string) string { value, _ := fixture.Input[key].(string); return value }
	id := claudeTelemetryIdentity{DeviceID: str("user_id"), SessionID: str("session_id"), AccountUUID: str("account_uuid"), OrgUUID: str("org_uuid"), Email: str("email"), SubscriptionType: str("subscription_type"), Version: str("cli_version"), Platform: str("platform"), Entrypoint: str("entrypoint")}
	id.Env = fullClaudeTelemetryEnv(fixture.Input["env"].(map[string]any), id.Version, id.Platform, str("arch"), true)
	now, err := time.Parse(time.RFC3339Nano, fixture.Now)
	require.NoError(t, err)
	for _, init := range []bool{true, false} {
		event := id.event(init, now, now.Add(-12500*time.Millisecond))
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(event, &parsed))
		data := parsed["event_data"].(map[string]any)
		generated, err := uuid.Parse(data["event_id"].(string))
		require.NoError(t, err)
		require.Equal(t, uuid.Version(4), generated.Version())
		data["event_id"] = "<uuid>"
		want := fixture.Init
		if !init {
			delete(data, "process")
			want = fixture.Success
		}
		got, err := json.Marshal(parsed)
		require.NoError(t, err)
		require.JSONEq(t, string(want), string(got))
	}
	require.JSONEq(t, string(fixture.Eval), string(id.growthbookEval()))
	require.Equal(t, "2.1.296", telemetryVersionBase("2.1.296.build.1"))
	require.Equal(t, "2.1.296-beta", telemetryVersionBase("2.1.296-beta"))
	require.Equal(t, "pwsh", telemetryShell(`C:\Windows\pwsh.exe`))
	require.Equal(t, "other", telemetryShell("/opt/custom"))
	require.Equal(t, strings.Repeat("ab", 32), officialTelemetryDevice(strings.Repeat("AB", 32)))
	require.Empty(t, officialTelemetryDevice("gateway-generated-uuid"))
}

func TestAccountTelemetryProcessRefreshAndExclusiveRanges(t *testing.T) {
	now := time.Now()
	id := claudeTelemetryIdentity{DeviceID: strings.Repeat("a", 64), SessionID: uuid.NewString(), Entrypoint: "cli", Env: map[string]any{}}
	ids := map[string]bool{}
	processes := map[string]bool{}
	for i := 0; i < 100; i++ {
		event := id.event(false, now, now.Add(-12500*time.Millisecond))
		eventID := gjson.GetBytes(event, "event_data.event_id").String()
		require.False(t, ids[eventID])
		ids[eventID] = true
		encoded := gjson.GetBytes(event, "event_data.process").String()
		processes[encoded] = true
		raw, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		require.Equal(t, 12.5, gjson.GetBytes(raw, "uptime").Float())
		for name, bounds := range map[string][2]int64{"rss": {300000000, 500000000}, "heapTotal": {40000000, 80000000}, "heapUsed": {100000000, 200000000}, "external": {1000000, 3000000}, "arrayBuffers": {10000, 50000}, "cpuUsage.user": {50000, 500000}, "cpuUsage.system": {15000, 150000}} {
			n := gjson.GetBytes(raw, name).Int()
			require.GreaterOrEqual(t, n, bounds[0])
			require.Less(t, n, bounds[1])
		}
		require.GreaterOrEqual(t, gjson.GetBytes(raw, "cpuPercent").Float(), 0.5)
		require.Less(t, gjson.GetBytes(raw, "cpuPercent").Float(), 5.0)
		require.Zero(t, gjson.GetBytes(raw, "constrainedMemory").Int())
	}
	require.Len(t, processes, 100)
	require.False(t, gjson.GetBytes(id.event(true, now, now), "event_data.process").Exists())
}

type accountTelemetryCacheStub struct {
	GatewayCache
	mu       sync.Mutex
	state    AccountTelemetryRunState
	activity AccountTelemetryActivity
	active   bool
	lease    bool
}

func (c *accountTelemetryCacheStub) BeginAccountTelemetryRun(context.Context, int64, time.Duration) (string, AccountTelemetryRunState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lease {
		return "", c.state, nil
	}
	c.lease = true
	return "lease", c.state, nil
}
func (c *accountTelemetryCacheStub) EndAccountTelemetryRun(_ context.Context, _ int64, _ string, state AccountTelemetryRunState) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = state
	c.lease = false
	return nil
}
func (c *accountTelemetryCacheStub) TouchAccountTelemetry(_ context.Context, _ int64, a AccountTelemetryActivity) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activity = a
	c.active = true
	return nil
}
func (c *accountTelemetryCacheStub) ActiveAccountTelemetry(context.Context) ([]int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return []int64{42}, nil
	}
	return nil, nil
}
func (c *accountTelemetryCacheStub) GetAccountTelemetryActivity(context.Context, int64) (AccountTelemetryActivity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return AccountTelemetryActivity{}, errors.New("expired")
	}
	return c.activity, nil
}

type accountTelemetryRepoStub struct {
	AccountRepository
	mu          sync.Mutex
	account     *Account
	saves       int
	saveAllowed bool
}

func (r *accountTelemetryRepoStub) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := *r.account
	account.Extra = maps.Clone(r.account.Extra)
	return &account, nil
}
func (r *accountTelemetryRepoStub) ListAccountTelemetry(_ context.Context, after int64, _ int) ([]*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if after >= r.account.ID || ReadAccountTelemetry(r.account).DeviceInstallation == nil {
		return nil, nil
	}
	account := *r.account
	account.Extra = maps.Clone(r.account.Extra)
	return []*Account{&account}, nil
}
func (r *accountTelemetryRepoStub) SaveAccountTelemetry(_ context.Context, _ int64, credentials, expected string, state AccountTelemetryState) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves++
	if !r.saveAllowed || ReadAccountTelemetry(r.account).Revision != expected {
		return false, nil
	}
	r.account.Extra[AccountTelemetryExtraKey] = state
	return true, nil
}

type accountTelemetryHTTPStub struct {
	HTTPUpstream
	requests []*http.Request
	bodies   [][]byte
	proxies  []string
	statuses []int
	body     string
}

func (u *accountTelemetryHTTPStub) DoWithTLS(req *http.Request, proxy string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, body)
	u.proxies = append(u.proxies, proxy)
	status := 200
	if len(u.statuses) > 0 {
		status = u.statuses[0]
		u.statuses = u.statuses[1:]
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

func newAccountTelemetryFixture(t *testing.T) (*GatewayService, *Account, *accountTelemetryCacheStub, *accountTelemetryHTTPStub) {
	t.Helper()
	account := &Account{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "sk-ant-oat01-offline-fixture", "account_uuid": "account-fixture", "org_uuid": "org-fixture", "email": "fixture@example.test"}, Extra: map[string]any{}}
	now := time.Now().UTC()
	account.Extra[AccountTelemetryExtraKey] = AccountTelemetryState{Status: "ready", Revision: uuid.NewString(), Owner: accountTelemetryOwner(account), SessionID: uuid.NewString(), ImportedAt: &now,
		DeviceInstallation: &AccountTelemetryBundle{Format: "sub2api-account-telemetry", Version: 2, ExportedAt: now, DeviceID: strings.Repeat("a", 64), MachineID: "fixture-machine", CLIVersion: "2.1.296", Platform: "linux", Arch: "x64", Locale: "en-US", Environment: map[string]any{}}}

	cache := &accountTelemetryCacheStub{}
	upstream := &accountTelemetryHTTPStub{}
	svc := &GatewayService{cfg: &config.Config{}, accountRepo: &accountTelemetryRepoStub{account: account, saveAllowed: true}, cache: cache, httpUpstream: upstream}
	svc.telemetrySettings = &OpsService{}
	settings := defaultOpsAdvancedSettings()
	settings.AccountTelemetrySendingEnabled = true
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	return svc, account, cache, upstream
}

func TestAccountTelemetryActivationIntervalsAndFailures(t *testing.T) {
	svc, account, cache, http := newAccountTelemetryFixture(t)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Empty(t, http.requests, "no startup/request activation")
	svc.activateAccountTelemetry(t.Context(), account, nil)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, http.requests, 3)
	require.Equal(t, []string{ClaudeTelemetryBatchPath, ClaudeTelemetryBatchPath, claudeTelemetryEvalPath}, []string{http.requests[0].URL.Path, http.requests[1].URL.Path, http.requests[2].URL.Path})
	require.Equal(t, "tengu_init", gjson.GetBytes(http.bodies[0], "events.0.event_data.event_name").String())
	require.Equal(t, "tengu_api_success", gjson.GetBytes(http.bodies[1], "events.0.event_data.event_name").String())
	for i, request := range http.requests {
		require.True(t, HTTPUpstreamRedirectsDisabled(request.Context()))
		require.Equal(t, "Bearer "+account.GetCredential("access_token"), request.Header.Get("Authorization"))
		require.True(t, strings.HasPrefix(request.UserAgent(), "claude-code/"))
		require.Empty(t, request.Header.Get("Anthropic-Beta"))
		// Internal account/cache concurrency metadata must never change the
		// vm2api event/eval wire contract or become an outbound request header.
		for _, key := range []string{"revision", "profile_revision", "owner", "device_installation", "imported_at"} {
			require.NotContains(t, string(http.bodies[i]), `"`+key+`":`)
		}
		state := ReadAccountTelemetry(account)
		require.NotContains(t, string(http.bodies[i]), state.Revision)
		require.NotContains(t, string(http.bodies[i]), state.Owner)
		for name, values := range request.Header {
			require.NotContains(t, strings.ToLower(name), "revision")
			require.NotContains(t, strings.Join(values, " "), state.Revision)
			require.NotContains(t, strings.Join(values, " "), state.Owner)
		}
	}
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, http.requests, 3)
	cache.state.LastBatch = time.Now().Add(-11 * time.Second)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, http.requests, 4)
	cache.state.LastEval = time.Now().Add(-6 * time.Hour)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, http.requests, 5)
	require.Equal(t, claudeTelemetryEvalPath, http.requests[4].URL.Path)
	cache.active = false
	cache.state.LastBatch = time.Time{}
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, http.requests, 5, "expired ten-minute window")
	for _, statuses := range [][]int{{401}, {200, 429, 200}, {200, 200, 403}, {503, 200, 200}} {
		svc, account, cache, http := newAccountTelemetryFixture(t)
		http.statuses = statuses
		svc.activateAccountTelemetry(t.Context(), account, nil)
		svc.runAccountTelemetryTick(t.Context(), account.ID)
		if statuses[0] == 401 {
			require.Len(t, http.requests, 1)
			require.False(t, cache.state.RestartAfter.IsZero())
		} else {
			require.Len(t, http.requests, 3)
		}
		if len(statuses) > 1 && statuses[1] == 429 {
			require.True(t, cache.state.BatchDisabled)
		}
		if len(statuses) > 2 && statuses[2] == 403 {
			require.True(t, cache.state.EvalDisabled)
		}
		require.Equal(t, StatusActive, account.Status)
		require.Nil(t, account.RateLimitResetAt)
	}
}

func TestAccountTelemetryIgnoresRetiredCopiesAndWaitsForImport(t *testing.T) {
	svc, account, _, upstream := newAccountTelemetryFixture(t)
	account.Extra["client_telemetry"] = account.Extra[AccountTelemetryExtraKey]
	account.Extra[AccountTelemetryExtraKey] = map[string]any{"status": "ready", "revision": "old", "profile": map[string]any{"source": "official-cc-init", "payload_json": "old server event"}}
	require.Equal(t, "waiting_import", ReadAccountTelemetry(account).Status)
	svc.scanAccountTelemetry(t.Context(), true)
	svc.activateAccountTelemetry(t.Context(), account, nil)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Empty(t, upstream.requests)
	require.Zero(t, svc.accountRepo.(*accountTelemetryRepoStub).saves)
	PrepareAccountTelemetryImport(account)
	require.Equal(t, "waiting_import", ReadAccountTelemetry(account).Status)
	require.Nil(t, ReadAccountTelemetry(account).DeviceInstallation)
	account.Type = AccountTypeAPIKey
	PrepareAccountTelemetryImport(account)
	require.NotContains(t, account.Extra, AccountTelemetryExtraKey)
}

func TestAccountTelemetryRebindingPreservesDeviceAndRejectsStaleSave(t *testing.T) {
	svc, account, _, _ := newAccountTelemetryFixture(t)
	old := ReadAccountTelemetry(account)
	state, err := svc.bindAccountTelemetry(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, old, state)
	account.Credentials["account_uuid"] = "replacement"
	svc.accountRepo.(*accountTelemetryRepoStub).saveAllowed = false
	_, err = svc.bindAccountTelemetry(t.Context(), account)
	require.EqualError(t, err, "telemetry_account_changed")
	require.Equal(t, old, ReadAccountTelemetry(account))
}

func TestAccountTelemetryNeverAdoptsClientMetadata(t *testing.T) {
	svc, account, cache, upstream := newAccountTelemetryFixture(t)
	state := ReadAccountTelemetry(account)
	callerID := `{"device_id":"client-device","account_uuid":"caller","session_id":"client-session"}`
	body, _ := json.Marshal(map[string]any{"metadata": map[string]string{"user_id": callerID}})
	svc.activateAccountTelemetry(t.Context(), account, body)
	require.Empty(t, cache.activity.DeviceID)
	require.Empty(t, cache.activity.SessionID)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, upstream.requests, 3)
	require.Equal(t, state.SessionID, gjson.GetBytes(upstream.bodies[0], "events.0.event_data.session_id").String())
	require.NotContains(t, string(upstream.bodies[0]), "client-device")
}

func TestAccountTelemetryChangedIdleIdentityRebindsWithoutInitialization(t *testing.T) {
	svc, account, cache, upstream := newAccountTelemetryFixture(t)
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
	old := ReadAccountTelemetry(account)
	account.Credentials["account_uuid"] = "replacement-account"
	svc.scanAccountTelemetry(t.Context(), false)
	state := ReadAccountTelemetry(account)
	require.Equal(t, "ready", state.Status)
	require.NotEqual(t, old.Revision, state.Revision)
	require.NotEqual(t, old.SessionID, state.SessionID)
	require.Equal(t, accountTelemetryOwner(account), state.Owner)
	require.Equal(t, old.DeviceInstallation, state.DeviceInstallation)
	require.False(t, cache.active)
	require.Empty(t, upstream.requests)
}

func TestAccountTelemetryRetainsProxyAndCredentialBoundaries(t *testing.T) {
	svc, account, _, upstream := newAccountTelemetryFixture(t)
	proxyID := int64(7)
	account.ProxyID = &proxyID
	_, _, err := svc.claudeTelemetryTarget(account, ClaudeTelemetryBatchPath)
	require.Error(t, err, "a missing account proxy must never become direct egress")
	account.Proxy = &Proxy{ID: proxyID, Protocol: "socks5", Host: "proxy.example.test", Port: 1080}
	target, proxy, err := svc.claudeTelemetryTarget(account, ClaudeTelemetryBatchPath)
	require.NoError(t, err)
	require.Equal(t, account.Proxy.URL(), proxy)
	require.Equal(t, "https://api.anthropic.com"+ClaudeTelemetryBatchPath, target)
	account.Extra["custom_base_url_enabled"] = true
	account.Extra["custom_base_url"] = "https://relay.example.test"
	target, proxy, err = svc.claudeTelemetryTarget(account, ClaudeTelemetryBatchPath)
	require.NoError(t, err)
	require.Empty(t, proxy)
	require.Equal(t, svc.buildCustomRelayURL("https://relay.example.test", ClaudeTelemetryBatchPath, account), target)
	account.Type = AccountTypeAPIKey
	svc.activateAccountTelemetry(t.Context(), account, nil)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Empty(t, upstream.requests)
}

func (c *accountTelemetryCacheStub) AcquireAccountTelemetryLeader(context.Context, string) (bool, error) {
	return true, nil
}
func (c *accountTelemetryCacheStub) ReleaseAccountTelemetryLeader(context.Context, string) error {
	return nil
}

func TestAccountTelemetrySendingSwitchPreservesDeviceAndClocks(t *testing.T) {
	svc, account, cache, upstream := newAccountTelemetryFixture(t)
	profile := ReadAccountTelemetry(account)
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
	svc.activateAccountTelemetry(t.Context(), account, nil)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.False(t, cache.active)
	require.Empty(t, upstream.requests)
	// The final sender also rejects disabled calls, including already queued jobs.
	require.EqualError(t, svc.sendClaudeTelemetry(t.Context(), ClaudeTelemetryBatchPath, nil, nil), "telemetry_disabled")
	require.EqualError(t, svc.generateClaudeTelemetry(t.Context(), nil), "telemetry_disabled")
	require.False(t, (&GatewayService{}).accountTelemetrySendingEnabled())

	settings := defaultOpsAdvancedSettings()
	settings.AccountTelemetrySendingEnabled = true
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	svc.activateAccountTelemetry(t.Context(), account, nil)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, upstream.requests, 3)
	lastEval := cache.state.LastEval
	cache.state.LastBatch = time.Now().Add(-time.Minute)
	settings.AccountTelemetrySendingEnabled = false
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, upstream.requests, 3, "old active entries must not bypass the switch")
	require.Equal(t, profile, ReadAccountTelemetry(account))
	settings.AccountTelemetrySendingEnabled = true
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, upstream.requests, 4, "resume sends one due event without catch-up or early eval")
	require.Equal(t, lastEval, cache.state.LastEval)
}

type accountTelemetryBlockingHTTP struct {
	HTTPUpstream
	calls    atomic.Int32
	block    atomic.Bool
	started  chan struct{}
	canceled chan struct{}
}

func (u *accountTelemetryBlockingHTTP) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls.Add(1)
	if u.block.Load() {
		u.started <- struct{}{}
		<-req.Context().Done()
		u.canceled <- struct{}{}
		return nil, req.Context().Err()
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func TestAccountTelemetryWorkerOnlySendsImportedDevicesAndCancelsWhenDisabled(t *testing.T) {
	svc, _, cache, _ := newAccountTelemetryFixture(t)
	settings := defaultOpsAdvancedSettings()
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	upstream := &accountTelemetryBlockingHTTP{started: make(chan struct{}, 1), canceled: make(chan struct{}, 1)}
	upstream.block.Store(true)
	svc.httpUpstream = upstream
	svc.StartAccountTelemetry(svc.telemetrySettings)
	t.Cleanup(svc.StopAccountTelemetry)
	require.Zero(t, upstream.calls.Load())
	ids, err := cache.ActiveAccountTelemetry(t.Context())
	require.NoError(t, err)
	require.Empty(t, ids)
	settings.AccountTelemetrySendingEnabled = true
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	select {
	case <-upstream.started:
	case <-time.After(3 * time.Second):
		t.Fatal("enabling did not activate imported account")
	}
	settings.AccountTelemetrySendingEnabled = false
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	select {
	case <-upstream.canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("disabling did not cancel event")
	}
	require.Eventually(t, func() bool { return len(svc.accountTelemetryWorker.senders) == 0 }, time.Second, time.Millisecond)
	require.EqualValues(t, 1, upstream.calls.Load())
	upstream.block.Store(false)
	settings.AccountTelemetrySendingEnabled = true
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	require.Eventually(t, func() bool { return upstream.calls.Load() == 4 }, 3*time.Second, 5*time.Millisecond)
}

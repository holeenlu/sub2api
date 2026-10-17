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
	"path/filepath"
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
func (r *accountTelemetryRepoStub) ListAccountTelemetry(_ context.Context, after int64, _ int, pending bool) ([]*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if after >= r.account.ID || pending && ReadAccountTelemetry(r.account).Status == "ready" {
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
	InitializeNewAccountTelemetry(account)
	id := claudeTelemetryIdentity{DeviceID: strings.Repeat("a", 64), SessionID: uuid.NewString(), AccountUUID: "account-fixture", OrgUUID: "org-fixture", Email: "fixture@example.test", Version: "2.1.296", Platform: "linux", Entrypoint: "cli"}
	id.Env = fullClaudeTelemetryEnv(nil, id.Version, id.Platform, "x64", true)
	fp := defaultFingerprint()
	headers := http.Header{}
	fp.applyHeaders(&http.Request{Header: headers})
	headers.Set("X-Stainless-Os", "Linux")
	headers.Set("X-Stainless-Arch", "x64")
	state := ReadAccountTelemetry(account)
	state.Status = "ready"
	state.Profile = &AccountTelemetryProfile{Source: "official-cc-init", PayloadJSON: string(id.event(true, time.Now(), time.Now())), Headers: headers}
	account.Extra[AccountTelemetryExtraKey] = state
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
	for _, request := range http.requests {
		require.True(t, HTTPUpstreamRedirectsDisabled(request.Context()))
		require.Equal(t, "Bearer "+account.GetCredential("access_token"), request.Header.Get("Authorization"))
		require.True(t, strings.HasPrefix(request.UserAgent(), "claude-code/"))
		require.Empty(t, request.Header.Get("Anthropic-Beta"))
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

func TestAccountTelemetryIgnoresRetiredCopiesAndProtectsOfficialIdentity(t *testing.T) {
	svc, account, _, upstream := newAccountTelemetryFixture(t)
	account.Extra["client_telemetry"] = account.Extra[AccountTelemetryExtraKey]
	delete(account.Extra, AccountTelemetryExtraKey)
	_, exists, err := accountTelemetryProfile(account)
	require.NoError(t, err)
	require.False(t, exists)
	svc.activateAccountTelemetry(t.Context(), account, nil)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Empty(t, upstream.requests)
	InitializeNewAccountTelemetry(account)
	state := ReadAccountTelemetry(account)
	require.Equal(t, "pending", state.Status)
	require.Nil(t, state.Profile)
	account.Type = AccountTypeAPIKey
	InitializeNewAccountTelemetry(account)
	require.Empty(t, ReadAccountTelemetry(account).Status)
}

func TestAccountTelemetryManualQueuePreservesIdentityWhileRegenerating(t *testing.T) {
	svc, account, _, _ := newAccountTelemetryFixture(t)
	old := ReadAccountTelemetry(account)
	state, err := svc.QueueAccountTelemetry(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, "pending", state.Status)
	require.NotEqual(t, old.Revision, state.Revision)
	require.Equal(t, old.Profile, state.Profile)
	svc.accountRepo.(*accountTelemetryRepoStub).saveAllowed = false
	_, err = svc.QueueAccountTelemetry(t.Context(), account)
	require.Error(t, err)
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
	require.Equal(t, gjson.Get(state.Profile.PayloadJSON, "event_data.session_id").String(), gjson.GetBytes(upstream.bodies[0], "events.0.event_data.session_id").String())
	require.NotContains(t, string(upstream.bodies[0]), "client-device")
}

func TestAccountTelemetryChangedIdleIdentityQueuesInitialization(t *testing.T) {
	svc, account, cache, upstream := newAccountTelemetryFixture(t)
	old := ReadAccountTelemetry(account)
	account.Credentials["account_uuid"] = "replacement-account"
	svc.activateAccountTelemetry(t.Context(), account, nil)
	state := ReadAccountTelemetry(account)
	require.Equal(t, "pending", state.Status)
	require.NotEqual(t, old.Revision, state.Revision)
	require.Equal(t, accountTelemetryOwner(account), state.Owner)
	require.Nil(t, state.Profile)
	require.False(t, cache.active)
	require.Empty(t, upstream.requests)
}

func configureAccountTelemetryCLI(t *testing.T, svc *GatewayService) string {
	t.Helper()
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo '2.1.296 (Claude Code)'; exit; fi
test -z "$ANTHROPIC_API_KEY" || exit 4
test -n "$CLAUDE_CODE_OAUTH_TOKEN" || exit 5
test "$DISABLE_TELEMETRY" = 1 || exit 6
printf '{"userID":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","machineID":"official-machine"}' > "$CLAUDE_CONFIG_DIR/.claude.json"
printf '{"type":"result","is_error":false}'
`
	require.NoError(t, os.WriteFile(binary, []byte(script), 0700))
	svc.cfg.Gateway.AccountTelemetryCLIPath = binary
	svc.cfg.Gateway.AccountTelemetryHomeDir = home
	return binary
}

func TestAccountTelemetryOfficialBootstrapAndDurability(t *testing.T) {
	svc, account, cache, upstream := newAccountTelemetryFixture(t)
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
	binary := configureAccountTelemetryCLI(t, svc)
	InitializeNewAccountTelemetry(account)
	svc.generateAccountTelemetryProfile(t.Context(), account)
	state := ReadAccountTelemetry(account)
	require.Equal(t, "ready", state.Status)
	require.NotNil(t, state.Profile)
	require.NotNil(t, state.GeneratedAt)
	first := gjson.Get(state.Profile.PayloadJSON, "event_data.device_id").String()
	require.Equal(t, strings.Repeat("a", 64), first)
	require.False(t, gjson.Get(state.Profile.PayloadJSON, "event_data.process").Exists())
	require.NoError(t, os.Remove(binary))
	_, err := svc.QueueAccountTelemetry(t.Context(), account)
	require.NoError(t, err)
	svc.generateAccountTelemetryProfile(t.Context(), account)
	state = ReadAccountTelemetry(account)
	require.Equal(t, "ready", state.Status)
	require.Equal(t, first, gjson.Get(state.Profile.PayloadJSON, "event_data.device_id").String(), "existing installation survives regeneration")
	require.False(t, cache.active, "automatic and manual generation must not activate sending while disabled")
	require.Empty(t, upstream.requests)
}

func TestAccountTelemetryMissingOfficialIdentityWaits(t *testing.T) {
	svc, account, _, upstream := newAccountTelemetryFixture(t)
	InitializeNewAccountTelemetry(account)
	svc.generateAccountTelemetryProfile(t.Context(), account)
	require.Equal(t, "error", ReadAccountTelemetry(account).Status)
	require.Nil(t, ReadAccountTelemetry(account).Profile)
	require.NotNil(t, ReadAccountTelemetry(account).RetryAt)
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

// Explicit opt-in exercises an installed official binary against an in-memory
// upstream fixture. No real account credential or external model call is used.
func TestAccountTelemetryRealOfficialCLI(t *testing.T) {
	binary := os.Getenv("SUB2API_TEST_CLAUDE_CLI")
	if binary == "" {
		t.Skip("set SUB2API_TEST_CLAUDE_CLI for the offline official binary check")
	}
	svc, account, _, upstream := newAccountTelemetryFixture(t)
	svc.cfg.Gateway.AccountTelemetryCLIPath = binary
	svc.cfg.Gateway.AccountTelemetryHomeDir = t.TempDir()
	msg := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 1, "output_tokens": 0}}
	events := []map[string]any{
		{"type": "message_start", "message": msg},
		{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "Hello."}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 1}},
		{"type": "message_stop"},
	}
	for _, event := range events {
		raw, _ := json.Marshal(event)
		upstream.body += "event: " + event["type"].(string) + "\ndata: " + string(raw) + "\n\n"
	}
	InitializeNewAccountTelemetry(account)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	profile, err := svc.initializeClaudeTelemetryProfile(ctx, account)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Len(t, gjson.Get(profile.PayloadJSON, "event_data.device_id").String(), 64)
	require.NotEmpty(t, gjson.Get(profile.PayloadJSON, "event_data.env.build_time").String())
}

func (c *accountTelemetryCacheStub) AcquireAccountTelemetryLeader(context.Context, string) (bool, error) {
	return true, nil
}
func (c *accountTelemetryCacheStub) ReleaseAccountTelemetryLeader(context.Context, string) error {
	return nil
}

func TestAccountTelemetrySendingSwitchPreservesProfileAndClocks(t *testing.T) {
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

func TestAccountTelemetryWorkerInitializesWhileDisabledAndCancelsOnlySending(t *testing.T) {
	svc, account, cache, _ := newAccountTelemetryFixture(t)
	configureAccountTelemetryCLI(t, svc)
	InitializeNewAccountTelemetry(account)
	settings := defaultOpsAdvancedSettings()
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	upstream := &accountTelemetryBlockingHTTP{started: make(chan struct{}, 1), canceled: make(chan struct{}, 1)}
	upstream.block.Store(true)
	svc.httpUpstream = upstream
	svc.StartAccountTelemetry(svc.telemetrySettings)
	t.Cleanup(svc.StopAccountTelemetry)
	ready := func() bool {
		current, _ := svc.accountRepo.GetByID(t.Context(), 42)
		return ReadAccountTelemetry(current).Status == "ready"
	}
	require.Eventually(t, ready, 3*time.Second, 5*time.Millisecond, "disabled sending must still initialize new accounts")
	require.Zero(t, upstream.calls.Load())
	ids, err := cache.ActiveAccountTelemetry(t.Context())
	require.NoError(t, err)
	require.Empty(t, ids)
	settings.AccountTelemetrySendingEnabled = true
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	svc.accountTelemetryWake <- struct{}{}
	select {
	case <-upstream.started:
	case <-time.After(3 * time.Second):
		t.Fatal("enabling did not activate the ready account")
	}
	settings.AccountTelemetrySendingEnabled = false
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	svc.accountTelemetryWake <- struct{}{}
	select {
	case <-upstream.canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("disabling did not cancel the in-flight event")
	}
	require.Eventually(t, func() bool { return len(svc.accountTelemetryWorker.senders) == 0 }, time.Second, time.Millisecond)
	current, err := svc.accountRepo.GetByID(t.Context(), 42)
	require.NoError(t, err)
	before := ReadAccountTelemetry(current).Profile.PayloadJSON
	_, err = svc.QueueAccountTelemetry(t.Context(), current)
	require.NoError(t, err, "manual generation remains available with sending off")
	require.Eventually(t, ready, 3*time.Second, 5*time.Millisecond)
	current, err = svc.accountRepo.GetByID(t.Context(), 42)
	require.NoError(t, err)
	require.Equal(t, gjson.Get(before, "event_data.device_id").String(), gjson.Get(ReadAccountTelemetry(current).Profile.PayloadJSON, "event_data.device_id").String())
	require.EqualValues(t, 1, upstream.calls.Load())
	upstream.block.Store(false)
	settings.AccountTelemetrySendingEnabled = true
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(settings)
	svc.accountTelemetryWake <- struct{}{}
	require.Eventually(t, func() bool { return upstream.calls.Load() == 4 }, 3*time.Second, 5*time.Millisecond)
}

//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type telemetryAccountCacheStub struct {
	GatewayCache
	owners   map[string]int64
	bindings map[string]ClientTelemetryBinding
	err      error
}

func (c *telemetryAccountCacheStub) SetClientTelemetryAccount(_ context.Context, scope string, binding ClientTelemetryBinding, _ time.Duration) error {
	if c.err != nil {
		return c.err
	}
	id := binding.AccountID
	if old, exists := c.owners[scope]; exists && old != id {
		id = 0
	}
	c.owners[scope] = id
	if c.bindings == nil {
		c.bindings = make(map[string]ClientTelemetryBinding)
	}
	binding.AccountID = id
	c.bindings[scope] = binding
	return nil
}

func (c *telemetryAccountCacheStub) GetClientTelemetryAccount(_ context.Context, scope string, _ time.Time) (ClientTelemetryBinding, error) {
	if c.err != nil {
		return ClientTelemetryBinding{}, c.err
	}
	if id, ok := c.owners[scope]; ok {
		if binding, ok := c.bindings[scope]; ok {
			return binding, nil
		}
		return ClientTelemetryBinding{AccountID: id}, nil
	}
	return ClientTelemetryBinding{}, ErrStickySessionNotFound
}

type telemetryAccountRepoStub struct {
	AccountRepository
	accounts map[int64]*Account
}

func (r *telemetryAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	account := r.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	return account, nil
}

type telemetryHTTPStub struct {
	HTTPUpstream
	requests []*http.Request
	bodies   [][]byte
	proxies  []string
	accounts []int64
	status   int
	err      error
}

func (u *telemetryHTTPStub) DoWithTLS(req *http.Request, proxy string, accountID int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, body)
	u.proxies = append(u.proxies, proxy)
	u.accounts = append(u.accounts, accountID)
	if u.err != nil {
		return nil, u.err
	}
	return &http.Response{StatusCode: u.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("upstream response is not reflected"))}, nil
}

func newTelemetryForwardFixture(t *testing.T) (*GatewayService, *APIKey, *Account, *clientTelemetryCaptureStub, *telemetryHTTPStub, *telemetryAccountCacheStub) {
	t.Helper()
	groupID := int64(4)
	key := &APIKey{ID: 1, UserID: 11, GroupID: &groupID}
	account := &Account{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive,
		AccountGroups: []AccountGroup{{GroupID: groupID}},
		Credentials:   map[string]any{"access_token": "account-secret", "account_uuid": "actual-account", "org_uuid": "actual-org", "email": "account@example.test"},
		Extra: map[string]any{AccountClientTelemetryExtraKey: accountTelemetryCopy{
			Client: "claude", PayloadJSON: `{"event_type":"ClaudeCodeInternalEvent","event_data":{"event_name":"old_init","event_id":"old-id","client_timestamp":"old-time","device_id":"account-device","session_id":"account-session","env":{"platform":"linux"},"auth":{"account_uuid":"sample-account","accountUUID":"sample-account"},"accountUUID":"sample-account","organizationUUID":"sample-org","user_attributes":{"accountUUID":"sample-account","organizationUUID":"sample-org","email":"sample@example.test"}}}`,
			Headers: http.Header{"User-Agent": {"account-cli/1.0.0"}, "X-Stainless-Os": {"Linux"}},
		}}}
	store := &clientTelemetryCaptureStub{}
	upstream := &telemetryHTTPStub{status: 200}
	cache := &telemetryAccountCacheStub{owners: map[string]int64{clientTelemetryAccountScope(key, "client-session"): 42}}
	svc := &GatewayService{cfg: &config.Config{}, accountRepo: &telemetryAccountRepoStub{accounts: map[int64]*Account{account.ID: account}}, cache: cache, httpUpstream: upstream, clientTelemetryRepo: store}
	return svc, key, account, store, upstream, cache
}

const telemetryForwardPayload = `{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{"event_name":"tengu_api_error","event_id":"current-id","client_timestamp":"2026-10-20T00:00:00Z","session_id":"client-session","device_id":"caller-device","email":"caller@example.test","env":{"platform":"caller-os"},"auth":{"account_uuid":"caller-account"},"status_code":429,"unknown":9007199254740993}}]}`

func TestClaudeTelemetryAccountForwardPreservesOriginalAndCurrentEvent(t *testing.T) {
	for _, path := range []string{ClaudeTelemetryLegacyPath, ClaudeTelemetryBatchPath} {
		t.Run(path, func(t *testing.T) {
			svc, key, account, store, upstream, _ := newTelemetryForwardFixture(t)
			headers := http.Header{"Authorization": {"Bearer client-secret"}, "Cookie": {"client-cookie"}, "User-Agent": {"caller-cli"}}
			require.NoError(t, svc.ProcessClaudeTelemetry(t.Context(), key, path, headers, []byte(telemetryForwardPayload)))
			require.Len(t, store.samples, 1)
			require.Contains(t, store.samples[0].PayloadJSON, "caller-device")
			require.Empty(t, store.samples[0].Headers.Get("Authorization"))
			require.Len(t, upstream.requests, 1)
			req := upstream.requests[0]
			require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()), "a redirect cannot change the credential destination")
			require.Equal(t, "https://api.anthropic.com"+path, req.URL.String())
			require.Equal(t, "Bearer account-secret", req.Header.Get("Authorization"))
			require.Equal(t, "account-cli/1.0.0", req.Header.Get("User-Agent"))
			require.Empty(t, req.Header.Get("Cookie"))
			require.Equal(t, []int64{account.ID}, upstream.accounts)
			body := string(upstream.bodies[0])
			require.NotContains(t, body, "caller-")
			require.NotContains(t, body, "caller@example.test")
			require.NotContains(t, body, "old-id")
			require.NotContains(t, body, "old-time")
			require.NotContains(t, body, "sample-")
			require.NotContains(t, body, "sample@example.test")
			for path, want := range map[string]string{"device_id": "account-device", "session_id": generateUUIDFromSeed("42::client-session"), "auth.account_uuid": "actual-account", "auth.organization_uuid": "actual-org", "env.platform": "linux", "event_name": "tengu_api_error", "event_id": "current-id", "client_timestamp": "2026-10-20T00:00:00Z", "status_code": "429", "unknown": "9007199254740993"} {
				require.Equal(t, want, gjson.Get(body, "events.0.event_data."+path).String(), path)
			}
			for path, want := range map[string]string{"auth.accountUUID": "actual-account", "accountUUID": "actual-account", "organizationUUID": "actual-org", "user_attributes.accountUUID": "actual-account", "user_attributes.organizationUUID": "actual-org", "user_attributes.email": "account@example.test", "email": "account@example.test"} {
				require.Equal(t, want, gjson.Get(body, "events.0.event_data."+path).String(), path)
			}
			storedProfile, _ := json.Marshal(account.Extra[AccountClientTelemetryExtraKey])
			require.Contains(t, string(storedProfile), "sample-account", "outbound replacement must not mutate account data")
		})
	}
}

func TestClaudeTelemetryUnavailableAccountSkipsForwardAndAcknowledgesReceipt(t *testing.T) {
	for _, failure := range []string{"other_user", "other_key", "other_group", "forged_account", "unbound", "ambiguous", "disabled_account", "expired_account", "group_removed", "identity_unavailable", "invalid_sample", "invalid_attributes", "wrong_client", "cache_error", "storage_error"} {
		t.Run(failure, func(t *testing.T) {
			svc, key, account, store, upstream, cache := newTelemetryForwardFixture(t)
			payload := telemetryForwardPayload
			switch failure {
			case "other_user":
				key.UserID++
			case "other_key":
				key.ID++
			case "other_group":
				id := int64(99)
				key.GroupID = &id
			case "forged_account":
				cache.owners = map[string]int64{}
				payload = strings.Replace(payload, `"status_code":429`, `"status_code":429,"account_id":42`, 1)
			case "unbound":
				cache.owners = map[string]int64{}
			case "ambiguous":
				require.NoError(t, cache.SetClientTelemetryAccount(t.Context(), clientTelemetryAccountScope(key, "client-session"), ClientTelemetryBinding{AccountID: 99}, time.Hour))
			case "disabled_account":
				account.Status = StatusDisabled
			case "expired_account":
				past := time.Now().Add(-time.Hour)
				account.ExpiresAt = &past
			case "group_removed":
				account.AccountGroups = nil
			case "identity_unavailable":
				delete(account.Extra, AccountClientTelemetryExtraKey)
			case "invalid_sample":
				profile := account.Extra[AccountClientTelemetryExtraKey].(accountTelemetryCopy)
				profile.PayloadJSON = `{}`
				account.Extra[AccountClientTelemetryExtraKey] = profile
			case "invalid_attributes":
				profile := account.Extra[AccountClientTelemetryExtraKey].(accountTelemetryCopy)
				profile.PayloadJSON = `{"event_type":"ClaudeCodeInternalEvent","event_data":{"device_id":"sample-device","user_attributes":"invalid"}}`
				account.Extra[AccountClientTelemetryExtraKey] = profile
			case "wrong_client":
				profile := account.Extra[AccountClientTelemetryExtraKey].(accountTelemetryCopy)
				profile.Client = "codex"
				account.Extra[AccountClientTelemetryExtraKey] = profile
			case "cache_error":
				cache.err = errors.New("cache unavailable")
			case "storage_error":
				store.saveError = errors.New("database unavailable")
			}
			err := svc.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, []byte(payload))
			require.Empty(t, upstream.requests)
			if failure == "storage_error" {
				require.ErrorIs(t, err, ErrClientTelemetryStorage)
				require.Empty(t, store.samples)
			} else {
				require.NoError(t, err, "local receipt must survive forwarding rejection")
				require.Len(t, store.samples, 1)
			}
		})
	}
}

func TestClaudeTelemetryUpstreamFailureDoesNotRetryOrPenalizeAccount(t *testing.T) {
	for _, status := range []int{0, 302, 400, 401, 403, 429, 503} {
		svc, key, account, store, upstream, _ := newTelemetryForwardFixture(t)
		upstream.status = status
		if status == 0 {
			upstream.err = errors.New("network unavailable")
		}
		err := svc.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, []byte(telemetryForwardPayload))
		require.NoError(t, err, "upstream outcomes never change a saved upload's acknowledgement")
		require.Len(t, upstream.requests, 1)
		require.Len(t, store.samples, 1)
		require.Equal(t, StatusActive, account.Status)
		require.Nil(t, account.RateLimitResetAt)
	}
}

func TestClaudeTelemetryAccountBindingComesFromActualModelDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, apiKeyAccount := range []bool{false, true} {
		upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"content_policy_violation","message":"declined"}}`))}}}
		cache := &telemetryAccountCacheStub{owners: map[string]int64{}}
		svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, cache: cache, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}}
		account := newAnthropicOAuthAccountForPartialUsageTest()
		if apiKeyAccount {
			account = newAnthropicAPIKeyAccountForTest()
		}
		key := &APIKey{ID: 5, UserID: 8}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		c.Set("api_key", key)
		body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"caller\",\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":"hello"}]}`)
		parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
		require.NoError(t, err)
		_, err = svc.Forward(t.Context(), c, account, parsed)
		require.Error(t, err)
		require.Equal(t, 1, upstream.callCount)
		require.Equal(t, account.ID, cache.owners[clientTelemetryAccountScope(key, "client-session")])
	}
}

func TestClaudeTelemetryUsesAccountAuthenticationAndProxy(t *testing.T) {
	for _, mode := range []string{"oauth", "setup_token", "api_key", "api_key_bearer", "relay", "invalid_target"} {
		t.Run(mode, func(t *testing.T) {
			svc, key, account, store, upstream, _ := newTelemetryForwardFixture(t)
			proxyID := int64(3)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{ID: proxyID, Protocol: "socks5", Host: "proxy.example.test", Port: 1080}
			wantProxy, wantURL := account.Proxy.URL(), "https://api.anthropic.com"+ClaudeTelemetryBatchPath
			switch mode {
			case "setup_token":
				account.Type = AccountTypeSetupToken
			case "api_key", "api_key_bearer", "invalid_target":
				account.Type = AccountTypeAPIKey
				account.Credentials["api_key"] = "api-account-secret"
				account.Credentials["base_url"] = "https://relay.example.test"
				wantURL = "https://relay.example.test" + ClaudeTelemetryBatchPath
				if mode == "api_key_bearer" {
					account.Extra[anthropicAPIKeyAuthSchemeExtraKey] = AnthropicAPIKeyAuthSchemeAuthorizationBearer
				}
				if mode == "invalid_target" {
					account.Credentials["base_url"] = "http://127.0.0.1/private"
				}
			case "relay":
				account.Extra["custom_base_url_enabled"] = true
				account.Extra["custom_base_url"] = "https://relay.example.test"
				wantURL = svc.buildCustomRelayURL("https://relay.example.test", ClaudeTelemetryBatchPath, account)
				wantProxy = ""
			}
			err := svc.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, []byte(telemetryForwardPayload))
			require.Len(t, store.samples, 1)
			if mode == "invalid_target" {
				require.NoError(t, err)
				require.Empty(t, upstream.requests)
				return
			}
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, wantURL, upstream.requests[0].URL.String())
			require.Equal(t, []string{wantProxy}, upstream.proxies)
			headers := upstream.requests[0].Header
			if mode == "api_key" {
				require.Equal(t, "api-account-secret", headers.Get("X-Api-Key"))
				require.Empty(t, headers.Get("Authorization"))
			} else {
				wantToken := "account-secret"
				if mode == "api_key_bearer" {
					wantToken = "api-account-secret"
				}
				require.Equal(t, "Bearer "+wantToken, headers.Get("Authorization"))
				require.Empty(t, headers.Get("X-Api-Key"))
			}
		})
	}
}

func TestClaudeTelemetryBatchSkipsOnlyIneligibleAccounts(t *testing.T) {
	for _, mode := range []string{"both", "missing_first", "missing_second", "upstream_failure"} {
		svc, key, account, store, upstream, cache := newTelemetryForwardFixture(t)
		_, _, second, _, _, _ := newTelemetryForwardFixture(t)
		second.ID++
		second.Credentials["access_token"] = "second-secret"
		second.Credentials["account_uuid"] = "second-account"
		profile := second.Extra[AccountClientTelemetryExtraKey].(accountTelemetryCopy)
		profile.PayloadJSON = strings.ReplaceAll(profile.PayloadJSON, "account-device", "second-device")
		second.Extra[AccountClientTelemetryExtraKey] = profile
		if mode == "missing_second" {
			delete(second.Extra, AccountClientTelemetryExtraKey)
		}
		if mode == "missing_first" {
			delete(account.Extra, AccountClientTelemetryExtraKey)
		}
		if mode == "upstream_failure" {
			upstream.status = http.StatusServiceUnavailable
		}
		svc.accountRepo.(*telemetryAccountRepoStub).accounts[second.ID] = second
		cache.owners[clientTelemetryAccountScope(key, "second-session")] = second.ID
		firstEvent := gjson.Get(telemetryForwardPayload, "events.0").Raw
		secondEvent := strings.NewReplacer("client-session", "second-session", "current-id", "second-id").Replace(firstEvent)
		body := []byte(`{"events":[` + firstEvent + `,` + secondEvent + `]}`)
		err := svc.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, body)
		require.NoError(t, err)
		require.Len(t, store.samples, 2)
		wantAccounts := []int64{account.ID, second.ID}
		if mode == "missing_first" {
			wantAccounts = []int64{second.ID}
		} else if mode == "missing_second" {
			wantAccounts = []int64{account.ID}
		}
		require.Equal(t, wantAccounts, upstream.accounts, mode)
		for i, accountID := range wantAccounts {
			eventID, token, identity, device := "current-id", "account-secret", "actual-account", "account-device"
			if accountID == second.ID {
				eventID, token, identity, device = "second-id", "second-secret", "second-account", "second-device"
			}
			require.Equal(t, "Bearer "+token, upstream.requests[i].Header.Get("Authorization"))
			require.Equal(t, int64(1), gjson.GetBytes(upstream.bodies[i], "events.#").Int())
			require.Equal(t, eventID, gjson.GetBytes(upstream.bodies[i], "events.0.event_data.event_id").String())
			require.Equal(t, identity, gjson.GetBytes(upstream.bodies[i], "events.0.event_data.auth.account_uuid").String())
			require.Equal(t, device, gjson.GetBytes(upstream.bodies[i], "events.0.event_data.device_id").String())
		}
	}
}

func TestClaudeTelemetryInitializesMissingCopyOnlyOnRealUpload(t *testing.T) {
	svc, key, account, store, upstream, _ := newTelemetryForwardFixture(t)
	delete(account.Extra, AccountClientTelemetryExtraKey)
	fp := defaultFingerprint()
	fp.ClientID = strings.Repeat("a", 64)
	svc.identityService = NewIdentityService(&stubIdentityCache{fingerprint: &fp})
	require.Empty(t, upstream.requests)
	require.NoError(t, svc.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, []byte(telemetryForwardPayload)))
	require.Len(t, store.samples, 1)
	require.Len(t, upstream.requests, 1)
	body := upstream.bodies[0]
	require.Equal(t, fp.ClientID, gjson.GetBytes(body, "events.0.event_data.device_id").String())
	session := gjson.GetBytes(body, "events.0.event_data.session_id").String()
	require.Equal(t, generateUUIDFromSeed("42::client-session"), session)
	require.Equal(t, session, upstream.requests[0].Header.Get("X-Claude-Code-Session-Id"))
	require.Equal(t, "tengu_api_error", gjson.GetBytes(body, "events.0.event_data.event_name").String())
	require.EqualValues(t, 429, gjson.GetBytes(body, "events.0.event_data.status_code").Int())
	raw, err := base64.StdEncoding.DecodeString(gjson.GetBytes(body, "events.0.event_data.process").String())
	require.NoError(t, err)
	require.True(t, json.Valid(raw))
	require.Greater(t, gjson.GetBytes(raw, "rss").Int(), int64(0))
	require.NotContains(t, string(body), "caller-")
	require.NotContains(t, account.Extra, AccountClientTelemetryExtraKey, "generated profile must not silently overwrite saved account data")
}

func TestClaudeTelemetryCopyStringAttributesAndValidation(t *testing.T) {
	svc, key, account, _, upstream, _ := newTelemetryForwardFixture(t)
	profile := account.Extra[AccountClientTelemetryExtraKey].(accountTelemetryCopy)
	attrs, _ := json.Marshal(map[string]string{"deviceID": "sample-device", "accountUUID": "sample-account", "apiBaseUrlHost": "old-host"})
	data := map[string]any{"device_id": "account-device", "user_attributes": string(attrs)}
	payload, _ := json.Marshal(map[string]any{"event_type": "GrowthbookExperimentEvent", "event_data": data})
	profile.PayloadJSON = string(payload)
	account.Extra[AccountClientTelemetryExtraKey] = profile
	require.NoError(t, ValidateAccountClientTelemetry(account.Extra))
	require.NoError(t, svc.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, []byte(telemetryForwardPayload)))
	require.Len(t, upstream.requests, 1)
	encoded := gjson.GetBytes(upstream.bodies[0], "events.0.event_data.user_attributes").String()
	require.Equal(t, "actual-account", gjson.Get(encoded, "accountUUID").String())
	require.Equal(t, "account-device", gjson.Get(encoded, "deviceID").String())
	require.False(t, gjson.Get(encoded, "apiBaseUrlHost").Exists())
	profile.PayloadJSON = "{}"
	account.Extra[AccountClientTelemetryExtraKey] = profile
	require.Error(t, ValidateAccountClientTelemetry(account.Extra))
}

func TestClaudeTelemetryCleansCallerEnvironmentAndKeepsEventFacts(t *testing.T) {
	_, _, account, _, _, _ := newTelemetryForwardFixture(t)
	profile := account.Extra[AccountClientTelemetryExtraKey].(accountTelemetryCopy)
	data := map[string]any{"session_id": "client-session", "device_id": "caller-device", "event_name": "tengu_api_error", "additional_metadata": map[string]string{"hostname": "caller-host"}, "base_url": "https://caller.invalid", "gateway": "caller", "status_code": 429}
	raw, _ := json.Marshal(map[string]any{"event_type": "ClaudeCodeInternalEvent", "event_data": data})
	out, err := replaceClaudeTelemetryIdentity(raw, profile, account, ClientTelemetryBinding{AccountID: 42, SessionID: "actual-wire-session", DeviceID: "actual-wire-device"})
	require.NoError(t, err)
	require.NotContains(t, string(out), "caller")
	require.Equal(t, "actual-wire-session", gjson.GetBytes(out, "event_data.session_id").String())
	require.Equal(t, "actual-wire-device", gjson.GetBytes(out, "event_data.device_id").String())
	require.EqualValues(t, 429, gjson.GetBytes(out, "event_data.status_code").Int())
}
func TestClaudeTelemetryProtocolBridgesBindActualAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"chat", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			errorBody, _ := json.Marshal(map[string]any{"error": map[string]string{"code": "content_policy_violation", "message": "declined"}})
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(errorBody)))}}}
			cache := &telemetryAccountCacheStub{owners: map[string]int64{}}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, cache: cache, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}}
			account := newAnthropicOAuthAccountForPartialUsageTest()
			key := &APIKey{ID: 5, UserID: 8}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil)
			c.Request.Header.Set("X-Claude-Code-Session-Id", "client-session")
			c.Set("api_key", key)
			parsed := &ParsedRequest{SessionContext: &SessionContext{ExplicitSessionID: "client-session"}}
			body := map[string]any{"model": "claude-sonnet-4-5", "max_tokens": 16}
			var err error
			if protocol == "chat" {
				body["messages"] = []any{map[string]string{"role": "user", "content": "hello"}}
				data, _ := json.Marshal(body)
				_, err = svc.ForwardAsChatCompletions(t.Context(), c, account, data, parsed)
			} else {
				body["input"] = "hello"
				data, _ := json.Marshal(body)
				_, err = svc.ForwardAsResponses(t.Context(), c, account, data, parsed)
			}
			require.Error(t, err)
			require.Equal(t, 1, upstream.callCount)
			require.Equal(t, account.ID, cache.owners[clientTelemetryAccountScope(key, "client-session")], "completed Anthropic dispatch never binds the telemetry session")
		})
	}
}

func TestClaudeTelemetryUsesActualInferenceIdentityAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, key, account, _, sender, cache := newTelemetryForwardFixture(t)
	fp := defaultFingerprint()
	fp.ClientID = strings.Repeat("f", 64)
	svc.identityService = NewIdentityService(&stubIdentityCache{fingerprint: &fp})
	resetGatewayForwardingSettingsCacheForTest(t)
	svc.settingService = NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, svc.cfg)
	svc.rateLimitService = &RateLimitService{}
	svc.deferredService = &DeferredService{}
	modelUpstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"content_policy_violation","message":"declined"}}`))}}}
	svc.httpUpstream = modelUpstream
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set("api_key", key)
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":32,"metadata":{"user_id":"{\"device_id\":\"caller-device\",\"account_uuid\":\"caller\",\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	_, err = svc.Forward(t.Context(), c, account, parsed)
	require.Error(t, err)
	require.Equal(t, 1, modelUpstream.callCount)
	binding := cache.bindings[clientTelemetryAccountScope(key, "client-session")]
	require.Equal(t, "account-device", binding.DeviceID, "the saved copy must supply inference device identity too")
	require.Equal(t, generateUUIDFromSeed("42::client-session"), binding.SessionID)
	require.NotEmpty(t, binding.Headers.Get("User-Agent"))
	require.Empty(t, binding.Headers.Get("Authorization"))
	svc.httpUpstream = sender
	require.NoError(t, svc.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, []byte(telemetryForwardPayload)))
	require.Len(t, sender.requests, 1)
	require.Equal(t, binding.SessionID, gjson.GetBytes(sender.bodies[0], "events.0.event_data.session_id").String())
	require.Equal(t, binding.DeviceID, gjson.GetBytes(sender.bodies[0], "events.0.event_data.device_id").String())
	require.Equal(t, binding.Headers.Get("User-Agent"), sender.requests[0].Header.Get("User-Agent"))
	require.Equal(t, binding.SessionID, sender.requests[0].Header.Get("X-Claude-Code-Session-Id"))
}

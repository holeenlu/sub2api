package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Internal forwarding diagnostics never alter the local receipt acknowledgement.
// Do not include tokens, raw transport errors or event bodies.
type clientTelemetryForwardError struct {
	Code           string
	UpstreamStatus int
}

func (e *clientTelemetryForwardError) Error() string { return e.Code }

func telemetryForwardFailure(code string) error {
	return &clientTelemetryForwardError{Code: code}
}

func logClaudeTelemetrySkipped(accountID int64, err error) {
	code, status := "telemetry_forwarding_unavailable", 0
	if failure, ok := err.(*clientTelemetryForwardError); ok {
		code, status = failure.Code, failure.UpstreamStatus
	}
	slog.Info("client_telemetry.not_forwarded", "account_id", accountID, "reason", code, "upstream_status", status)
}

func claudeTelemetryRequestSession(c *gin.Context, parsed *ParsedRequest) string {
	if parsed != nil {
		if metadata := ParseMetadataUserID(parsed.MetadataUserID); metadata != nil && metadata.SessionID != "" {
			return metadata.SessionID
		}
		if parsed.SessionContext != nil && parsed.SessionContext.ExplicitSessionID != "" {
			return parsed.SessionContext.ExplicitSessionID
		}
	}
	return ExtractClientSessionID(c)
}

func clientTelemetryAccountScope(key *APIKey, session string) string {
	if key == nil || key.ID <= 0 || key.UserID <= 0 || session == "" || len(session) > 512 || strings.ContainsAny(session, "\x00\r\n") {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d:%s", key.UserID, key.ID, derefGroupID(key.GroupID), telemetryDigest([]byte(session)))
}

// Called only after an actual model dispatch returned an HTTP response. The
// binding is separate from client-controlled sticky keys and does not schedule
// an account for telemetry. Cache outages must not turn inference into failure.
func (s *GatewayService) bindClaudeTelemetryAccount(ctx context.Context, c *gin.Context, account *Account, session string, wireBody []byte, wireHeaders http.Header) {
	if s == nil || c == nil || c.Request == nil || account == nil || account.Platform != PlatformAnthropic || !s.ClientTelemetryCaptureAllowed(c.Request.Header) {
		return
	}
	value, _ := c.Get("api_key")
	key, _ := value.(*APIKey)
	scope := clientTelemetryAccountScope(key, session)
	cache, ok := s.cache.(ClientTelemetryAccountCache)
	if !ok || scope == "" {
		return
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 100*time.Millisecond)
	defer cancel()
	binding := ClientTelemetryBinding{AccountID: account.ID, SinceMS: time.Now().UnixMilli()}
	if len(wireBody) > 0 && account.IsOAuth() {
		if id := ParseMetadataUserID(gjson.GetBytes(wireBody, "metadata.user_id").String()); id != nil {
			binding.SessionID = id.SessionID
			binding.DeviceID = id.DeviceID
		}
	}
	if account.IsOAuth() {
		binding.Headers = clientTelemetryHeaders(wireHeaders)
	}
	encodedHeaders, _ := json.Marshal(binding.Headers)
	if len(encodedHeaders) > 16*1024 {
		binding.Headers = nil
	} else {
		binding.HeaderDigest = telemetryDigest(encodedHeaders)
	}
	if err := cache.SetClientTelemetryAccount(cacheCtx, scope, binding, time.Hour); err != nil {
		slog.Warn("client_telemetry.binding_failed", "account_id", account.ID)
	}
}

type accountTelemetryCopy struct {
	Client      string      `json:"client"`
	PayloadJSON string      `json:"payload_json"`
	Headers     http.Header `json:"headers"`
}

type claudeTelemetryEvent struct {
	Type string                     `json:"event_type"`
	Data map[string]json.RawMessage `json:"event_data"`
}

// These fields describe the credential account's environment. Event IDs,
// timestamps, event names, outcomes and usage stay with the real current event.
var accountTelemetryIdentityFields = []string{
	"device_id", "session_id", "email", "auth", "env", "process", "user_attributes",
	"account_uuid", "organization_uuid", "accountUUID", "organizationUUID", "deviceID", "user_id",
}

func replaceClaudeTelemetryIdentity(raw json.RawMessage, profile accountTelemetryCopy, account *Account, bindings ...ClientTelemetryBinding) (json.RawMessage, error) {
	var event claudeTelemetryEvent
	if json.Unmarshal(raw, &event) != nil || event.Data == nil {
		return nil, telemetryForwardFailure("telemetry_account_sample_invalid")
	}
	template, err := parseClaudeTelemetryProfile(profile)
	if err != nil {
		return nil, err
	}
	originalSession := ""
	_ = json.Unmarshal(event.Data["session_id"], &originalSession)
	cleanTelemetryEnvironment(event.Data)
	for _, field := range accountTelemetryIdentityFields {
		delete(event.Data, field)
		if value, exists := template.Data[field]; exists {
			event.Data[field] = value
		}
	}
	// Account identity is authoritative when present on the actual credentials;
	// a copied sample cannot substitute another subscription's account UUID.
	credentialValue := func(names ...string) string { return telemetryCredential(account, names...) }
	auth, err := telemetryObject(event.Data["auth"])
	if err != nil {
		return nil, err
	}
	if auth == nil {
		auth = make(map[string]json.RawMessage)
	}
	attributes, err := telemetryObject(event.Data["user_attributes"])
	if err != nil {
		return nil, err
	}
	var attributesString string
	stringAttributes := json.Unmarshal(event.Data["user_attributes"], &attributesString) == nil
	if event.Type == "GrowthbookExperimentEvent" && attributes == nil {
		attributes = make(map[string]json.RawMessage)
		stringAttributes = true
		for _, key := range []string{"id", "deviceID", "sessionId", "accountUUID", "organizationUUID", "email"} {
			attributes[key] = json.RawMessage(`""`)
		}
	}
	if env, err := telemetryObject(event.Data["env"]); err != nil {
		return nil, err
	} else if env != nil {
		cleanTelemetryEnvironment(env)
		if len(bindings) > 0 && bindings[0].Headers != nil {
			headers := bindings[0].Headers
			if ua := headers.Get("User-Agent"); isAcceptableFingerprintUserAgent(ua) {
				version := ExtractCLIVersion(ua)
				env["version"], _ = json.Marshal(version)
				env["version_base"], _ = json.Marshal(strings.Split(version, "-")[0])
			}
			for _, field := range []struct{ key, header string }{{"platform", "X-Stainless-OS"}, {"platform_raw", "X-Stainless-OS"}, {"arch", "X-Stainless-Arch"}, {"runtimes", "X-Stainless-Runtime"}} {
				if value := headers.Get(field.header); value != "" {
					if field.key == "platform" || field.key == "platform_raw" {
						value = strings.ToLower(value)
						if value == "macos" {
							value = "darwin"
						}
					}
					env[field.key], _ = json.Marshal(value)
				}
			}
		}
		event.Data["env"], _ = json.Marshal(env)
	}
	cleanTelemetryEnvironment(auth)
	cleanTelemetryEnvironment(attributes)
	for _, key := range []string{"access_token", "refresh_token", "authorization", "api_key", "token"} {
		delete(auth, key)
		delete(attributes, key)
	}
	// The current outbound session is supplied by trusted dispatch history. The
	// deterministic fallback also keeps direct callers out of the stale-copy namespace.
	session := generateUUIDFromSeed(fmt.Sprintf("%d::%s", account.ID, originalSession))
	if len(bindings) > 0 && bindings[0].SessionID != "" {
		session = bindings[0].SessionID
	}
	event.Data["session_id"], _ = json.Marshal(session)
	device := event.Data["device_id"]
	if len(bindings) > 0 && bindings[0].DeviceID != "" {
		device, _ = json.Marshal(bindings[0].DeviceID)
		event.Data["device_id"] = device
	}
	for _, key := range []string{"deviceID", "user_id"} {
		if _, ok := event.Data[key]; ok {
			event.Data[key] = device
		}
	}
	for _, key := range []string{"device_id", "deviceID", "id", "user_id"} {
		if _, ok := attributes[key]; ok {
			attributes[key] = device
		}
	}
	for _, key := range []string{"session_id", "sessionId"} {
		if _, ok := attributes[key]; ok {
			attributes[key] = event.Data["session_id"]
		}
	}
	for _, identity := range []struct {
		fields []string
		value  string
	}{
		{[]string{"account_uuid", "accountUUID"}, credentialValue("account_uuid")},
		{[]string{"organization_uuid", "organizationUUID"}, credentialValue("org_uuid", "organization_uuid")},
		{[]string{"email"}, credentialValue("email")},
	} {
		if identity.value == "" {
			continue
		}
		encoded, _ := json.Marshal(identity.value)
		for _, fields := range []map[string]json.RawMessage{event.Data, auth, attributes} {
			for _, field := range identity.fields {
				if _, exists := fields[field]; exists {
					fields[field] = encoded
				}
			}
		}
		if identity.fields[0] == "email" {
			event.Data["email"] = encoded
		} else {
			auth[identity.fields[0]] = encoded
		}
	}
	if len(auth) > 0 {
		event.Data["auth"], _ = json.Marshal(auth)
	}
	if len(attributes) > 0 {
		encoded, _ := json.Marshal(attributes)
		if stringAttributes {
			encoded, _ = json.Marshal(string(encoded))
		}
		event.Data["user_attributes"] = encoded
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	cleanTelemetryEnvironment(envelope)
	for _, field := range accountTelemetryIdentityFields {
		delete(envelope, field)
	}
	envelope["event_type"], _ = json.Marshal(event.Type)
	envelope["event_data"], _ = json.Marshal(event.Data)
	return json.Marshal(envelope)
}

type claudeTelemetryDispatch struct {
	account *Account
	profile accountTelemetryCopy
	events  []json.RawMessage
	bytes   int
}

func (s *GatewayService) forwardClaudeTelemetry(ctx context.Context, key *APIKey, path string, samples []*ClientTelemetryLog) {
	cache, ok := s.cache.(ClientTelemetryAccountCache)
	if !ok || s.accountRepo == nil || s.httpUpstream == nil {
		logClaudeTelemetrySkipped(0, telemetryForwardFailure("telemetry_forwarding_unavailable"))
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	groups := make(map[string]*claudeTelemetryDispatch)
	profiles := make(map[int64]*claudeTelemetryDispatch)
	for _, sample := range samples {
		scope := clientTelemetryAccountScope(key, sample.SessionID)
		if scope == "" {
			logClaudeTelemetrySkipped(0, telemetryForwardFailure("telemetry_session_unbound"))
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, gjson.Get(sample.PayloadJSON, "event_data.client_timestamp").String())
		binding, err := cache.GetClientTelemetryAccount(ctx, scope, at)
		if err != nil || binding.AccountID <= 0 {
			logClaudeTelemetrySkipped(0, telemetryForwardFailure("telemetry_session_unbound_or_ambiguous"))
			continue
		}
		profile, known := profiles[binding.AccountID]
		if !known {
			if len(profiles) >= 16 {
				logClaudeTelemetrySkipped(binding.AccountID, telemetryForwardFailure("telemetry_account_limit"))
				continue
			}
			profile, err = s.prepareClaudeTelemetryAccount(ctx, key, binding.AccountID)
			profiles[binding.AccountID] = profile
			if err != nil {
				logClaudeTelemetrySkipped(binding.AccountID, err)
			}
		}
		if profile == nil {
			continue
		}
		event, err := replaceClaudeTelemetryIdentity(json.RawMessage(sample.PayloadJSON), profile.profile, profile.account, binding)
		if err != nil {
			logClaudeTelemetrySkipped(binding.AccountID, err)
			continue
		}
		groupKey := fmt.Sprintf("%d:%s:%s", binding.AccountID, gjson.GetBytes(event, "event_data.session_id").String(), binding.HeaderDigest)
		group, known := groups[groupKey]
		if !known {
			if len(groups) >= 16 {
				logClaudeTelemetrySkipped(binding.AccountID, telemetryForwardFailure("telemetry_account_limit"))
				continue
			}
			group = &claudeTelemetryDispatch{account: profile.account, profile: profile.profile}
			group.profile.Headers = clientTelemetryHeaders(profile.profile.Headers)
			for name, values := range binding.Headers {
				group.profile.Headers[name] = append([]string(nil), values...)
			}
			groups[groupKey] = group
		}
		if group == nil {
			continue
		}
		group.bytes += len(event) + 1
		if group.bytes+16 > int(ClientTelemetryMaxBodyBytes) {
			groups[groupKey] = nil
			logClaudeTelemetrySkipped(binding.AccountID, telemetryForwardFailure("telemetry_replacement_too_large"))
			continue
		}
		group.events = append(group.events, event)
	}
	ids := make([]string, 0, len(groups))
	for id, group := range groups {
		if group != nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		group := groups[id]
		if err := s.sendClaudeTelemetry(ctx, key, path, group); err != nil {
			logClaudeTelemetrySkipped(group.account.ID, err)
		}
	}

}

func (s *GatewayService) prepareClaudeTelemetryAccount(ctx context.Context, key *APIKey, accountID int64) (*claudeTelemetryDispatch, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil || account.Platform != PlatformAnthropic || account.Status != StatusActive ||
		(account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken && account.Type != AccountTypeAPIKey) ||
		(account.ExpiresAt != nil && !account.ExpiresAt.After(time.Now())) {
		return nil, telemetryForwardFailure("telemetry_account_unavailable")
	}
	inGroup := s.isAccountInGroup(account, key.GroupID)
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		inGroup = true // Native simple-mode scheduling ignores group assignment.
	}
	if !inGroup && key.Group != nil && key.Group.ClaudeCodeOnly && key.Group.FallbackGroupID != nil {
		inGroup = s.isAccountInGroup(account, key.Group.FallbackGroupID)
	}
	if !inGroup {
		return nil, telemetryForwardFailure("telemetry_account_not_authorized")
	}
	if err := validateVendorNativeDispatch(ctx, s.accountRepo, account, key.GroupID); err != nil {
		return nil, telemetryForwardFailure("telemetry_account_not_authorized")
	}
	if err := ValidateAccountClientTelemetry(account.Extra); err != nil {
		return nil, telemetryForwardFailure("telemetry_account_sample_invalid")
	}
	profile, exists, err := accountTelemetryProfile(account)
	if err != nil {
		return nil, err
	}
	if !exists {
		profile, err = s.initializeClaudeTelemetryProfile(ctx, account)
		if err != nil {
			return nil, err
		}
	}
	return &claudeTelemetryDispatch{account: account, profile: profile}, nil
}

func (s *GatewayService) sendClaudeTelemetry(ctx context.Context, key *APIKey, path string, group *claudeTelemetryDispatch) error {
	if !s.ClientTelemetryCaptureAllowed(nil) {
		return nil
	}
	account := group.account
	target, baseURL, proxyURL := "https://api.anthropic.com"+path, "", ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if account.Type == AccountTypeAPIKey {
		baseURL = getAnthropicGatewayBaseURL(account)
		if baseURL != "" {
			if s.cfg == nil {
				return telemetryForwardFailure("telemetry_upstream_invalid")
			}
			validated, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return telemetryForwardFailure("telemetry_upstream_invalid")
			}
			target = strings.TrimRight(validated, "/") + path
		}
	} else if account.IsCustomBaseURLEnabled() {
		if s.cfg == nil {
			return telemetryForwardFailure("telemetry_upstream_invalid")
		}
		validated, err := s.validateUpstreamBaseURL(account.GetCustomBaseURL())
		if err != nil {
			return telemetryForwardFailure("telemetry_upstream_invalid")
		}
		target, proxyURL = s.buildCustomRelayURL(validated, path, account), ""
	}
	body, err := json.Marshal(struct {
		Events []json.RawMessage `json:"events"`
	}{group.events})
	if err != nil || len(body) > int(ClientTelemetryMaxBodyBytes) {
		return telemetryForwardFailure("telemetry_replacement_too_large")
	}
	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return telemetryForwardFailure("telemetry_credentials_unavailable")
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return telemetryForwardFailure("telemetry_upstream_invalid")
	}
	req.Header = clientTelemetryHeaders(group.profile.Headers)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if len(group.events) > 0 {
		req.Header.Set("X-Claude-Code-Session-Id", gjson.GetBytes(group.events[0], "event_data.session_id").String())
	}
	req.Header.Set("X-Service-Name", "claude-code")
	if tokenType == "oauth" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		setAnthropicAPIKeyAuthHeader(req.Header, account, token, baseURL)
	}
	if err := validateVendorNativeDispatch(ctx, s.accountRepo, account, key.GroupID); err != nil {
		return telemetryForwardFailure("telemetry_account_not_authorized")
	}
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		return telemetryForwardFailure("telemetry_upstream_unavailable")
	}
	if resp.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &clientTelemetryForwardError{Code: "telemetry_upstream_rejected", UpstreamStatus: resp.StatusCode}
	}
	slog.Info("client_telemetry.forwarded", "account_id", account.ID, "event_count", len(group.events), "upstream_status", resp.StatusCode)
	return nil
}

var _ error = (*clientTelemetryForwardError)(nil)

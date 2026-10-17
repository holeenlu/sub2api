//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type clientTelemetryCaptureStub struct {
	ClientTelemetryRepository
	samples   []*ClientTelemetryLog
	saveError error
}

func (r *clientTelemetryCaptureStub) CaptureClientTelemetry(ctx context.Context, rows []*ClientTelemetryLog) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.saveError != nil {
		return r.saveError
	}
	r.samples = rows
	return nil
}

func TestProcessClaudeTelemetryStoresOriginalWhenForwardingUnavailable(t *testing.T) {
	// Local receipt is acknowledged even when optional account forwarding is unavailable.
	for _, path := range []string{ClaudeTelemetryLegacyPath, ClaudeTelemetryBatchPath} {
		t.Run(path, func(t *testing.T) {
			raw := `{ "event_type":"ClaudeCodeInternalEvent", "event_data":{"device_id":"original-device","session_id":"unbound-session","client_timestamp":"2026-10-19T00:00:00Z","event_id":"original-event","env":{"version":"2.1.301","platform":"darwin","arch":"arm64"},"auth":{"account_uuid":"client-account"},"future":9007199254740993} }`
			headers := http.Header{
				"Authorization": {"Bearer gateway-secret"}, "X-Api-Key": {"gateway-secret"},
				"Cookie": {"private-session"}, "X-Forwarded-For": {"private-ip"},
				"User-Agent":                  {"claude-cli/2.1.301 (external, cli)"},
				"X-Stainless-Runtime-Version": {"client-runtime"},
			}
			store := &clientTelemetryCaptureStub{}
			s := &GatewayService{clientTelemetryRepo: store}
			err := s.ProcessClaudeTelemetry(t.Context(), &APIKey{ID: 1, UserID: 11}, path, headers, []byte(`{"events":[`+raw+`,{"event_type":"GrowthbookExperimentEvent","event_data":{}}]}`))
			require.NoError(t, err)
			require.Len(t, store.samples, 2, "an event without a session or device is also stored")
			row := store.samples[0]
			require.Equal(t, raw, row.PayloadJSON)
			require.Equal(t, int64(11), row.UserID)
			require.Equal(t, int64(1), row.APIKeyID)
			require.Equal(t, path, row.RequestPath)
			require.Equal(t, "unbound-session", row.SessionID)
			require.Equal(t, "original-device", row.DeviceID)
			for _, name := range []string{"Authorization", "X-Api-Key", "Cookie", "X-Forwarded-For"} {
				require.Empty(t, row.Headers.Get(name), name)
			}
			require.Equal(t, "client-runtime", row.Headers.Get("X-Stainless-Runtime-Version"))
			require.Equal(t, "Bearer gateway-secret", headers.Get("Authorization"))
			headers.Set("User-Agent", "changed-after-save")
			require.Equal(t, "claude-cli/2.1.301 (external, cli)", row.Headers.Get("User-Agent"))
		})
	}
}

func TestProcessClaudeTelemetryRejectsEntireInvalidBatch(t *testing.T) {
	for name, body := range map[string]string{
		"empty": "", "array": "[]", "null": "null", "no events": `{"events":[]}`,
		"invalid data":      `{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":null}]}`,
		"unknown type":      `{"events":[{"event_type":"Other","event_data":{}}]}`,
		"partially invalid": `{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{}},{"event_type":"Other","event_data":{}}]}`,
		"trailing object":   `{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{}}]}{}`,
		"too large":         strings.Repeat(" ", int(ClientTelemetryMaxBodyBytes)+1),
		"too many":          `{"events":[` + strings.Repeat(`{"event_type":"ClaudeCodeInternalEvent","event_data":{}},`, 200) + `{"event_type":"ClaudeCodeInternalEvent","event_data":{}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			store := &clientTelemetryCaptureStub{}
			s := &GatewayService{clientTelemetryRepo: store}
			err := s.ProcessClaudeTelemetry(t.Context(), &APIKey{ID: 1, UserID: 11}, ClaudeTelemetryBatchPath, nil, []byte(body))
			require.ErrorIs(t, err, ErrInvalidClientTelemetry)
			require.Empty(t, store.samples)
		})
	}
}

func TestClaudeTelemetryPrivacyChoicePrecedesCapture(t *testing.T) {
	for _, path := range []string{ClaudeTelemetryLegacyPath, ClaudeTelemetryBatchPath} {
		for _, preference := range []http.Header{{"Dnt": {"1"}}, {"Sec-Gpc": {"1"}}} {
			store := &clientTelemetryCaptureStub{}
			s := &GatewayService{clientTelemetryRepo: store}
			require.NoError(t, s.ProcessClaudeTelemetry(t.Context(), &APIKey{ID: 1, UserID: 11}, path, preference, []byte("not decoded")))
			require.Empty(t, store.samples)
		}
	}
	store := &clientTelemetryCaptureStub{}
	gw := &GatewayService{clientTelemetryRepo: store}
	settings := newRuntimeSettingRepoStub()
	ops := NewOpsService(nil, settings, nil, nil, nil, nil, gw, nil, nil, nil, nil)
	gw.clientTelemetryRepo = store
	settingsBefore := settings.getMultipleCalls + settings.getValueCalls
	cfg := ops.OpsAdvancedSettingsSnapshot()
	cfg.ClientTelemetryMode = ClientTelemetryModeDisabled
	_, err := ops.UpdateOpsAdvancedSettings(t.Context(), &cfg)
	require.NoError(t, err)
	for range 10 {
		require.NoError(t, gw.ProcessClaudeTelemetry(t.Context(), &APIKey{ID: 1, UserID: 11}, ClaudeTelemetryBatchPath, nil, []byte("not decoded")))
	}
	require.Empty(t, store.samples)
	require.Equal(t, settingsBefore, settings.getMultipleCalls+settings.getValueCalls, "privacy reads must use the shared runtime snapshot")
	require.Contains(t, settings.values[SettingKeyOpsAdvancedSettings], `"client_telemetry_mode":"disabled"`)
	cfg.ClientTelemetryMode = ClientTelemetryModeForward
	_, err = ops.UpdateOpsAdvancedSettings(t.Context(), &cfg)
	require.NoError(t, err)
	payload := []byte(`{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{}}]}`)
	require.NoError(t, gw.ProcessClaudeTelemetry(t.Context(), &APIKey{ID: 1, UserID: 11}, ClaudeTelemetryBatchPath, nil, payload), "missing account attribution does not invalidate local receipt")
	require.Len(t, store.samples, 1)
}

func TestProcessClaudeTelemetryStorageFailures(t *testing.T) {
	payload := []byte(`{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{}}]}`)
	key := &APIKey{ID: 1, UserID: 11}
	var unavailable *GatewayService
	require.ErrorIs(t, unavailable.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, payload), ErrClientTelemetryStorage)
	offline := errors.New("database offline")
	store := &clientTelemetryCaptureStub{saveError: offline}
	s := &GatewayService{clientTelemetryRepo: store}
	err := s.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, nil, payload)
	require.ErrorIs(t, err, ErrClientTelemetryStorage)
	require.ErrorIs(t, err, offline)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	cancel()
	err = s.ProcessClaudeTelemetry(ctx, key, ClaudeTelemetryBatchPath, nil, payload)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, store.samples)
	require.ErrorIs(t, s.ProcessClaudeTelemetry(t.Context(), key, "/other", nil, payload), ErrInvalidClientTelemetry)
	require.ErrorIs(t, s.ProcessClaudeTelemetry(t.Context(), nil, ClaudeTelemetryBatchPath, nil, payload), ErrInvalidClientTelemetry)
	headers := http.Header{"User-Agent": {strings.Repeat("x", 16*1024)}}
	require.ErrorIs(t, s.ProcessClaudeTelemetry(t.Context(), key, ClaudeTelemetryBatchPath, headers, payload), ErrInvalidClientTelemetry)
}

func TestClientTelemetryHashesPreserveLargeNumbersAndDeviceIdentity(t *testing.T) {
	raw := `{"event_type":"ClaudeCodeInternalEvent", "event_data":{"device_id":"device-one","future":9007199254740993}}`
	key := &APIKey{ID: 1, UserID: 11}
	first, err := clientTelemetrySample(key, ClaudeTelemetryBatchPath, nil, json.RawMessage(raw))
	require.NoError(t, err)
	changed, err := clientTelemetrySample(key, ClaudeTelemetryBatchPath, nil, json.RawMessage(strings.ReplaceAll(raw, "9007199254740993", "9007199254740994")))
	require.NoError(t, err)
	require.Equal(t, first.ClientKey, changed.ClientKey)
	require.NotEqual(t, first.PayloadHash, changed.PayloadHash)
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&value))
	formatted, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	identical, err := clientTelemetrySample(key, ClaudeTelemetryBatchPath, nil, formatted)
	require.NoError(t, err)
	require.Equal(t, first.PayloadHash, identical.PayloadHash, "JSON whitespace and key order do not create new samples")
}

func TestAccountTelemetryCopyValidation(t *testing.T) {
	valid := func() map[string]any {
		return map[string]any{"client": "claude", "payload_json": `{ "event_type":"ClaudeCodeInternalEvent","event_data":{"device_id":"account-device","unknown":9007199254740993} }`, "headers": map[string][]string{"User-Agent": {"original-cli"}}}
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"source reference", func(v map[string]any) { v["sample_id"] = 7 }},
		{"invalid json", func(v map[string]any) { v["payload_json"] = "{" }},
		{"too large", func(v map[string]any) { v["payload_json"] = strings.Repeat(" ", int(ClientTelemetryMaxBodyBytes)+1) }},
		{"authorization", func(v map[string]any) { v["headers"] = map[string][]string{"Authorization": {"Bearer secret"}} }},
		{"cookie", func(v map[string]any) { v["headers"] = map[string][]string{"Cookie": {"secret"}} }},
		{"header injection", func(v map[string]any) { v["headers"] = map[string][]string{"User-Agent": {"cli\r\nx-secret: x"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := valid()
			tc.mutate(v)
			require.Error(t, ValidateAccountClientTelemetry(map[string]any{AccountClientTelemetryExtraKey: v}))
		})
	}
	copy := valid()
	require.NoError(t, ValidateAccountClientTelemetry(map[string]any{AccountClientTelemetryExtraKey: copy}))
	require.Contains(t, copy["payload_json"], "9007199254740993")
	require.NoError(t, ValidateAccountClientTelemetry(nil))
	require.NoError(t, ValidateAccountClientTelemetry(map[string]any{AccountClientTelemetryExtraKey: nil}))
}

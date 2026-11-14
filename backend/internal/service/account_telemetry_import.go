package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Version 2 carries the exporting device's official installation and CLI
// facts. Account identity, runtime session and credentials belong to the gateway.
type AccountTelemetryBundle struct {
	Format      string         `json:"format"`
	Version     int            `json:"version"`
	ExportedAt  time.Time      `json:"exported_at"`
	DeviceID    string         `json:"device_id"`
	MachineID   string         `json:"machine_id"`
	CLIVersion  string         `json:"cli_version"`
	Platform    string         `json:"platform"`
	Arch        string         `json:"arch"`
	Locale      string         `json:"locale"`
	Environment map[string]any `json:"environment"`
}

func parseAccountTelemetryBundle(raw []byte) (*AccountTelemetryBundle, error) {
	if len(raw) == 0 || int64(len(raw)) > AccountTelemetryMaxBodyBytes {
		return nil, telemetryForwardFailure("telemetry_import_invalid")
	}
	var bundle AccountTelemetryBundle
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if decoder.Decode(&bundle) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, telemetryForwardFailure("telemetry_import_invalid")
	}
	if err := validateAccountTelemetryBundle(&bundle); err != nil {
		return nil, err
	}
	return &bundle, nil
}

// Also validates persisted observations after JSON round trips.
func validateAccountTelemetryBundle(bundle *AccountTelemetryBundle) error {
	invalid := telemetryForwardFailure("telemetry_import_invalid")
	if bundle == nil || bundle.Format != "sub2api-account-telemetry" || bundle.Version != 2 || bundle.ExportedAt.IsZero() || bundle.Environment == nil ||
		!slices.Contains([]string{"win32", "linux", "darwin"}, bundle.Platform) || !slices.Contains([]string{"x64", "arm64"}, bundle.Arch) ||
		officialTelemetryDevice(bundle.DeviceID) == "" || strings.TrimSpace(bundle.MachineID) == "" || bundle.CLIVersion != ExtractCLIVersion("claude-cli/"+bundle.CLIVersion) || !isAcceptableFingerprintUserAgent("claude-cli/"+bundle.CLIVersion+" (external, cli)") {
		return invalid
	}
	validText := func(value string, limit int) bool {
		return len(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
	}
	if !validText(bundle.Locale, 64) || !validText(bundle.MachineID, 256) {
		return invalid
	}
	for key, value := range bundle.Environment {
		if slices.Contains(claudeTelemetryFactStrings, key) {
			text, ok := value.(string)
			if !ok || !validText(text, 512) {
				return invalid
			}
			continue
		}
		if key != "cpu_cores" && key != "memory_bytes" {
			return invalid
		}
		var n float64
		switch number := value.(type) {
		case json.Number:
			var err error
			n, err = number.Float64()
			if err != nil {
				return invalid
			}
		case float64:
			n = number
		default:
			return invalid
		}
		if n < 0 || n > 1<<53-1 || n != math.Trunc(n) || (key == "cpu_cores" && n > 65536) {
			return invalid
		}
	}
	return nil
}

func (s *GatewayService) ImportAccountTelemetry(ctx context.Context, account *Account, raw []byte) (AccountTelemetryState, error) {
	if !accountTelemetryEligible(account) {
		return AccountTelemetryState{}, telemetryForwardFailure("telemetry_account_unavailable")
	}
	bundle, err := parseAccountTelemetryBundle(raw)
	if err != nil {
		return AccountTelemetryState{}, err
	}
	now := time.Now().UTC()
	state := AccountTelemetryState{Status: "ready", Revision: uuid.NewString(), Owner: accountTelemetryOwner(account), DeviceInstallation: bundle, SessionID: uuid.NewString(), ImportedAt: &now}
	return s.saveAccountTelemetryState(ctx, account, state)
}

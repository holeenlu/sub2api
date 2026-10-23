package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const AccountTelemetryExtraKey = "account_telemetry"
const AccountTelemetryMaxBodyBytes int64 = 1 << 20
const ClaudeTelemetryBatchPath = "/api/event_logging/v2/batch"

// Only imported device facts and the account binding are persisted. Events and
// headers are derived at dispatch; server CLI profiles are not an input source.
type AccountTelemetryState struct {
	Status             string                  `json:"status"`
	Revision           string                  `json:"revision"`
	Owner              string                  `json:"owner"`
	ImportedAt         *time.Time              `json:"imported_at,omitempty"`
	SessionID          string                  `json:"session_id,omitempty"`
	DeviceInstallation *AccountTelemetryBundle `json:"device_installation,omitempty"`
}

type AccountTelemetryRepository interface {
	ListAccountTelemetry(context.Context, int64, int) ([]*Account, error)
	SaveAccountTelemetry(context.Context, int64, string, string, AccountTelemetryState) (bool, error)
}

func accountTelemetryEligible(account *Account) bool {
	return account != nil && account.Platform == PlatformAnthropic &&
		(account.Type == AccountTypeOAuth || account.Type == AccountTypeSetupToken) &&
		account.Status == StatusActive && (account.ExpiresAt == nil || account.ExpiresAt.After(time.Now()))
}

func accountTelemetryOwner(account *Account) string {
	return telemetryDigest([]byte(telemetryCredential(account, "account_uuid", "email", "refresh_token", "access_token")))
}

func PrepareAccountTelemetryImport(account *Account) {
	if account == nil {
		return
	}
	delete(account.Extra, AccountTelemetryExtraKey)
	if account.Platform != PlatformAnthropic || (account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken) {
		return
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[AccountTelemetryExtraKey] = AccountTelemetryState{Status: "waiting_import", Revision: uuid.NewString()}
}

func ReadAccountTelemetry(account *Account) AccountTelemetryState {
	state := AccountTelemetryState{Status: "waiting_import"}
	if account == nil {
		return state
	}
	raw, err := json.Marshal(account.Extra[AccountTelemetryExtraKey])
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err != nil || decoder.Decode(&state) != nil {
		return AccountTelemetryState{Status: "waiting_import"}
	}
	// Existing version-2 imports remain usable; retired profiles, errors and
	// pending initialization never constitute an imported device installation.
	state.Status = "waiting_import"
	if validateAccountTelemetryBundle(state.DeviceInstallation) == nil {
		state.Status = "ready"
	} else {
		state.DeviceInstallation = nil
		state.SessionID, state.Owner, state.ImportedAt = "", "", nil
	}
	return state
}

func ValidateAccountTelemetry(extra map[string]any) error {
	value, exists := extra[AccountTelemetryExtraKey]
	if !exists || value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	var state AccountTelemetryState
	if err != nil || len(raw) > 2*int(AccountTelemetryMaxBodyBytes)+4096 || json.Unmarshal(raw, &state) != nil {
		return fmt.Errorf("invalid account telemetry")
	}
	if state.Revision == "" {
		return fmt.Errorf("missing account telemetry revision")
	}
	switch state.Status {
	case "waiting_import":
		if state.DeviceInstallation != nil || state.SessionID != "" || state.Owner != "" {
			return fmt.Errorf("invalid waiting account telemetry")
		}
	case "ready":
		if err := validateAccountTelemetryBundle(state.DeviceInstallation); err != nil {
			return err
		}
		if _, err := uuid.Parse(state.SessionID); err != nil || state.SessionID == uuid.Nil.String() || state.Owner == "" {
			return fmt.Errorf("invalid account telemetry binding")
		}
	default:
		return fmt.Errorf("invalid account telemetry state")
	}
	return nil
}

var accountTelemetryHeaderNames = []string{
	"X-Claude-Code-Session-Id", "X-Service-Name", "Accept-Language", "User-Agent", "X-App", "Anthropic-Version",
	"X-Stainless-Lang", "X-Stainless-Package-Version", "X-Stainless-OS", "X-Stainless-Arch", "X-Stainless-Runtime", "X-Stainless-Runtime-Version",
}

func accountTelemetryHeaders(incoming http.Header) http.Header {
	out := make(http.Header)
	for _, name := range accountTelemetryHeaderNames {
		if values := incoming.Values(name); len(values) > 0 {
			out[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		}
	}
	return out
}
func telemetryDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

type AccountTelemetryActivity struct {
	Revision  string `json:"revision"`
	SessionID string `json:"session_id"`
	DeviceID  string `json:"device_id"`
}

func (s *GatewayService) accountTelemetrySendingEnabled() bool {
	return s != nil && s.telemetrySettings.OpsAdvancedSettingsSnapshot().AccountTelemetrySendingEnabled
}
func (s *GatewayService) requireAccountTelemetrySendingEnabled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.accountTelemetrySendingEnabled() {
		return telemetryForwardFailure("telemetry_disabled")
	}
	return nil
}

// Rebinding changes only gateway-owned state. It never starts a process or
// obtains installation facts from the server, even when an old profile exists.
func (s *GatewayService) bindAccountTelemetry(ctx context.Context, account *Account) (AccountTelemetryState, error) {
	state := ReadAccountTelemetry(account)
	if !accountTelemetryEligible(account) || state.DeviceInstallation == nil {
		return state, telemetryForwardFailure("telemetry_import_required")
	}
	_, sessionErr := uuid.Parse(state.SessionID)
	if state.Owner == accountTelemetryOwner(account) && state.Revision != "" && sessionErr == nil && state.SessionID != uuid.Nil.String() {
		return state, nil
	}
	state.Owner, state.SessionID, state.Revision = accountTelemetryOwner(account), uuid.NewString(), uuid.NewString()
	return s.saveAccountTelemetryState(ctx, account, state)
}

func (s *GatewayService) saveAccountTelemetryState(ctx context.Context, account *Account, state AccountTelemetryState) (AccountTelemetryState, error) {
	repo, ok := s.accountRepo.(AccountTelemetryRepository)
	if !ok {
		return state, telemetryForwardFailure("telemetry_storage_unavailable")
	}
	credentials, _ := json.Marshal(account.Credentials)
	saved, err := repo.SaveAccountTelemetry(ctx, account.ID, string(credentials), ReadAccountTelemetry(account).Revision, state)
	if err != nil {
		return state, err
	}
	if !saved {
		return state, telemetryForwardFailure("telemetry_account_changed")
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[AccountTelemetryExtraKey] = state
	s.activateAccountTelemetry(ctx, account, nil)
	return state, nil
}

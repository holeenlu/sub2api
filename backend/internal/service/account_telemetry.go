package service

import (
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

// Account-owned output. There is no sample ID, client upload or copied identity.
type AccountTelemetryState struct {
	Status      string                   `json:"status"`
	Revision    string                   `json:"revision"`
	Owner       string                   `json:"owner"`
	ErrorCode   string                   `json:"error_code,omitempty"`
	RetryAt     *time.Time               `json:"retry_at,omitempty"`
	GeneratedAt *time.Time               `json:"generated_at,omitempty"`
	Profile     *AccountTelemetryProfile `json:"profile,omitempty"`
}

type AccountTelemetryRepository interface {
	ListAccountTelemetry(context.Context, int64, int, bool) ([]*Account, error)
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

func InitializeNewAccountTelemetry(account *Account) {
	if account == nil {
		return
	}
	// Generated state is server-owned; account imports cannot transplant it.
	delete(account.Extra, AccountTelemetryExtraKey)
	if account.Platform != PlatformAnthropic || (account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken) {
		return
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[AccountTelemetryExtraKey] = AccountTelemetryState{Status: "pending", Revision: uuid.NewString(), Owner: accountTelemetryOwner(account)}
}

func ReadAccountTelemetry(account *Account) AccountTelemetryState {
	var state AccountTelemetryState
	if account == nil {
		return state
	}
	raw, err := json.Marshal(account.Extra[AccountTelemetryExtraKey])
	if err != nil || json.Unmarshal(raw, &state) != nil {
		return AccountTelemetryState{}
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
	if err != nil || len(raw) > 6*int(AccountTelemetryMaxBodyBytes)+32768 || json.Unmarshal(raw, &state) != nil {
		return fmt.Errorf("invalid account telemetry")
	}
	if state.Revision == "" || (state.Status != "pending" && state.Status != "ready" && state.Status != "error") {
		return fmt.Errorf("invalid account telemetry state")
	}
	if state.Profile != nil {
		if len(state.Profile.PayloadJSON) > int(AccountTelemetryMaxBodyBytes) {
			return fmt.Errorf("account telemetry profile too large")
		}
		if _, err := parseClaudeTelemetryProfile(*state.Profile); err != nil {
			return err
		}
		raw, err := json.Marshal(state.Profile.Headers)
		if err != nil || len(raw) > 16384 {
			return fmt.Errorf("invalid account telemetry headers")
		}
	}
	if state.Status == "ready" && state.Profile == nil {
		return fmt.Errorf("missing account telemetry profile")
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

func (s *GatewayService) QueueAccountTelemetry(ctx context.Context, account *Account) (AccountTelemetryState, error) {
	if !accountTelemetryEligible(account) {
		return AccountTelemetryState{}, telemetryForwardFailure("telemetry_account_unavailable")
	}
	repo, ok := s.accountRepo.(AccountTelemetryRepository)
	if !ok {
		return AccountTelemetryState{}, telemetryForwardFailure("telemetry_storage_unavailable")
	}
	old := ReadAccountTelemetry(account)
	state := AccountTelemetryState{Status: "pending", Revision: uuid.NewString(), Owner: accountTelemetryOwner(account)}
	if old.Owner == state.Owner {
		state.Profile = old.Profile
		state.GeneratedAt = old.GeneratedAt
	}
	credentials, _ := json.Marshal(account.Credentials)
	saved, err := repo.SaveAccountTelemetry(ctx, account.ID, string(credentials), old.Revision, state)
	if err != nil {
		return state, err
	}
	if !saved {
		return state, telemetryForwardFailure("telemetry_account_changed")
	}
	if s.accountTelemetryWake != nil {
		select {
		case s.accountTelemetryWake <- struct{}{}:
		default:
		}
	}
	return state, nil
}

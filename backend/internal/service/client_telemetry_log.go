package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrClientTelemetryStorage = errors.New("client telemetry storage unavailable")

const AccountClientTelemetryExtraKey = "client_telemetry"

// ValidateAccountClientTelemetry validates a self-contained copy. It deliberately
// does not look up a sample, compare account identities, or create a relationship.
func ValidateAccountClientTelemetry(extra map[string]any) error {
	value, exists := extra[AccountClientTelemetryExtraKey]
	if !exists || value == nil {
		return nil
	}
	invalid := infraerrors.BadRequest("INVALID_ACCOUNT_TELEMETRY", "Invalid account telemetry copy")
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 6*int(ClientTelemetryMaxBodyBytes)+32*1024 {
		return invalid
	}
	var copy struct {
		Client      string      `json:"client"`
		PayloadJSON string      `json:"payload_json"`
		Headers     http.Header `json:"headers"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&copy) != nil || (copy.Client != "claude" && copy.Client != "codex") {
		return invalid
	}
	if len(copy.PayloadJSON) == 0 || len(copy.PayloadJSON) > int(ClientTelemetryMaxBodyBytes) || !utf8.ValidString(copy.PayloadJSON) || !json.Valid([]byte(copy.PayloadJSON)) {
		return invalid
	}
	if bytes.TrimSpace([]byte(copy.PayloadJSON))[0] != '{' {
		return invalid
	}
	if copy.Client == "claude" {
		if _, err := parseClaudeTelemetryProfile(accountTelemetryCopy{Client: copy.Client, PayloadJSON: copy.PayloadJSON, Headers: copy.Headers}); err != nil {
			return invalid
		}
	}
	headers, err := json.Marshal(copy.Headers)
	if err != nil || len(headers) > 16*1024 {
		return invalid
	}
	for name, values := range copy.Headers {
		allowed := false
		for _, candidate := range clientTelemetryHeaderNames {
			if name == http.CanonicalHeaderKey(candidate) {
				allowed = true
				break
			}
		}
		if !allowed {
			return invalid
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n\x00") {
				return invalid
			}
		}
	}
	return nil
}

// ClientTelemetryLog stores the first real event observed for a client, not an
// account environment template. Raw JSON is a string to preserve number precision.
type ClientTelemetryLog struct {
	ID            int64       `json:"id"`
	UserID        int64       `json:"user_id"`
	APIKeyID      int64       `json:"api_key_id"`
	GroupID       *int64      `json:"group_id"`
	Client        string      `json:"client"`
	RequestPath   string      `json:"request_path"`
	EventType     string      `json:"event_type"`
	SessionID     string      `json:"session_id"`
	DeviceID      string      `json:"device_id"`
	PayloadHash   string      `json:"payload_hash"`
	ClientKey     string      `json:"-"`
	PayloadJSON   string      `json:"payload_json,omitempty"`
	Headers       http.Header `json:"headers,omitempty"`
	ReceivedCount int64       `json:"received_count"`
	CreatedAt     time.Time   `json:"created_at"`
	LastSeenAt    time.Time   `json:"last_seen_at"`
}

type ClientTelemetryLogFilter struct {
	Page, PageSize     int
	UserID, APIKeyID   *int64
	StartTime, EndTime *time.Time
	SessionID          string
}

type ClientTelemetryLogList struct {
	Items    []*ClientTelemetryLog `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

type ClientTelemetryRepository interface {
	// Duplicate receipts update counters without replacing the first event or user attribution.
	CaptureClientTelemetry(context.Context, []*ClientTelemetryLog) error
	ListClientTelemetry(context.Context, *ClientTelemetryLogFilter) (*ClientTelemetryLogList, error)
	GetClientTelemetry(context.Context, int64) (*ClientTelemetryLog, error)
}

// ClientTelemetryAccountCache is separate from client-chosen sticky-session
// keys. Only an authenticated model dispatch may write these scoped bindings.
type ClientTelemetryAccountCache interface {
	SetClientTelemetryAccount(context.Context, string, ClientTelemetryBinding, time.Duration) error
	GetClientTelemetryAccount(context.Context, string, time.Time) (ClientTelemetryBinding, error)
}

type ClientTelemetryBinding struct {
	Headers      http.Header `json:"headers,omitempty"`
	HeaderDigest string      `json:"header_digest"`
	AccountID    int64       `json:"account_id"`
	SessionID    string      `json:"session_id"`
	DeviceID     string      `json:"device_id"`
	SinceMS      int64       `json:"since_ms"`
}

var clientTelemetryHeaderNames = []string{
	"X-Claude-Code-Session-Id", "X-Service-Name", "Accept-Language",
	"User-Agent", "X-App", "Anthropic-Version", "Anthropic-Beta",
	"X-Stainless-Lang", "X-Stainless-Package-Version", "X-Stainless-OS",
	"X-Stainless-Arch", "X-Stainless-Runtime", "X-Stainless-Runtime-Version",
}

func clientTelemetryHeaders(incoming http.Header) http.Header {
	out := make(http.Header)
	for _, name := range clientTelemetryHeaderNames {
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

func clientTelemetrySample(key *APIKey, path string, headers http.Header, raw json.RawMessage) (*ClientTelemetryLog, error) {
	// UseNumber prevents distinct large integers from collapsing through float64.
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, ErrInvalidClientTelemetry
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidClientTelemetry
	}
	var event struct {
		Type string `json:"event_type"`
		Data struct {
			SessionID string `json:"session_id"`
			DeviceID  string `json:"device_id"`
		} `json:"event_data"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, ErrInvalidClientTelemetry
	}
	if event.Data.SessionID == "" {
		event.Data.SessionID = headers.Get("X-Claude-Code-Session-Id")
	}
	row := &ClientTelemetryLog{
		UserID: key.UserID, APIKeyID: key.ID, GroupID: key.GroupID,
		Client: "claude", RequestPath: path, EventType: event.Type,
		SessionID: telemetryIndexValue(event.Data.SessionID), DeviceID: telemetryIndexValue(event.Data.DeviceID),
		PayloadHash: telemetryDigest(canonical), PayloadJSON: string(raw), Headers: headers,
	}
	if event.Data.DeviceID != "" {
		row.ClientKey = telemetryDigest([]byte(event.Data.DeviceID))
	}
	return row, nil
}

// Oversized identifiers remain intact in the raw event but never expand indexes.
func telemetryIndexValue(value string) string {
	if len(value) > 512 || strings.ContainsRune(value, 0) {
		return ""
	}
	return value
}

func (s *OpsService) ListClientTelemetry(ctx context.Context, filter *ClientTelemetryLogFilter) (*ClientTelemetryLogList, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if filter == nil {
		filter = &ClientTelemetryLogFilter{}
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	if s.opsRepo == nil {
		return nil, ErrClientTelemetryStorage
	}
	return s.opsRepo.ListClientTelemetry(ctx, filter)
}

func (s *OpsService) GetClientTelemetry(ctx context.Context, id int64) (*ClientTelemetryLog, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.opsRepo == nil {
		return nil, ErrClientTelemetryStorage
	}
	row, err := s.opsRepo.GetClientTelemetry(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("TELEMETRY_NOT_FOUND", "Telemetry sample not found")
	}
	return row, err
}

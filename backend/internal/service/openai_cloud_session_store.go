package service

import (
	"context"
	"encoding/json"
	"time"
)

const (
	OpenAICloudSessionTTL      = 15 * time.Minute
	OpenAICloudOwnerLease      = 20 * time.Second
	OpenAICloudStateTTL        = time.Hour
	OpenAICloudSettlementTTL   = 24 * time.Hour
	OpenAICloudMaxRequestBytes = 4 << 20
	OpenAICloudMaxResultBytes  = 4 << 20
	// Room for the 256KiB billing snapshot and bounded ownership/event metadata.
	OpenAICloudMaxEnvelopeBytes    = OpenAICloudMaxResultBytes + (512 << 10)
	OpenAICloudMaxEventStreamBytes = 16 << 20
	OpenAICloudMaxStreamEntries    = 8192
	OpenAICloudMaxCommands         = 64
	OpenAICloudMaxSessions         = 128
	OpenAICloudMaxUserSessions     = 8
	OpenAICloudMaxAccountSessions  = 16
)

type OpenAICloudOwner struct {
	Probe           bool   `json:"probe,omitempty"`
	UserID          int64  `json:"user_id"`
	APIKeyID        int64  `json:"api_key_id"`
	GroupID         int64  `json:"group_id"`
	AccountID       int64  `json:"account_id"`
	GrantGeneration string `json:"grant_generation"`
	Model           string `json:"model"`
	SettingsDigest  string `json:"settings_digest"`
}

type OpenAICloudBinding struct {
	OpenAICloudOwner
	SessionID     string    `json:"session_id"`
	RootRequestID string    `json:"root_request_id"`
	ThreadID      string    `json:"thread_id,omitempty"`
	TurnID        string    `json:"turn_id,omitempty"`
	OwnerInstance string    `json:"owner_instance"`
	ExpiresAt     time.Time `json:"expires_at"`
	Status        string    `json:"status"`
}

// A nil *OpenAICloudUsage distinguishes missing facts from measured zero.
// Parsing requires every field, nonnegative buckets and matching model/turn.
type OpenAICloudUsage struct {
	InputTokens           int            `json:"input_tokens"`
	OutputTokens          int            `json:"output_tokens"`
	CachedInputTokens     int            `json:"cached_input_tokens"`
	CacheWriteInputTokens int            `json:"cache_write_input_tokens"`
	TotalTokens           int            `json:"total_tokens"`
	WebSearchCalls        int            `json:"web_search_calls,omitempty"`
	ImageCount            int            `json:"image_count,omitempty"`
	ImageSizes            map[string]int `json:"image_sizes,omitempty"`
	ImageOutputSizes      []string       `json:"image_output_sizes,omitempty"`
}

type OpenAICloudRequest struct {
	Owner           OpenAICloudOwner `json:"owner"`
	Body            json.RawMessage  `json:"body"`
	RootRequestID   string           `json:"root_request_id"`
	BillingSnapshot json.RawMessage  `json:"billing_snapshot,omitempty"`
	// Local downstream keepalive only; never serialized to another owner.
	onIdle func() error
}

type OpenAICloudResult struct {
	// Owner proof persisted with the terminal zero-usage settlement. Missing on
	// older records means unknown, never permission to replay.
	RejectedBeforeInference bool              `json:"rejected_before_inference,omitempty"`
	Replayed                bool              `json:"replayed,omitempty"`
	Response                json.RawMessage   `json:"response"`
	ErrorStatus             int               `json:"error_status,omitempty"`
	SessionID               string            `json:"session_id"`
	RootRequestID           string            `json:"root_request_id"`
	AwaitingTools           bool              `json:"awaiting_tools"`
	Terminal                bool              `json:"terminal"`
	Usage                   *OpenAICloudUsage `json:"usage"`
	UsageCredible           bool              `json:"usage_credible"`
	ThreadID                string            `json:"thread_id"`
	TurnID                  string            `json:"turn_id"`
}

// This record contains no upstream credential or bearer material. Its owner
// token fences the live socket; sharing the record does not transfer that socket.
type OpenAICloudSessionRecord struct {
	OpenAICloudBinding
	OwnerToken      string             `json:"owner_token"`
	LeaseUntil      time.Time          `json:"lease_until"`
	BillingSnapshot json.RawMessage    `json:"billing_snapshot"`
	Usage           *OpenAICloudUsage  `json:"usage"`
	Sequence        uint64             `json:"sequence"`
	Result          *OpenAICloudResult `json:"result,omitempty"`
	Failure         string             `json:"failure,omitempty"`
}

type OpenAICloudSettlement struct {
	OpenAICloudBinding
	BillingSnapshot json.RawMessage   `json:"billing_snapshot"`
	Status          string            `json:"status"`
	Usage           *OpenAICloudUsage `json:"usage"`
	Sequence        uint64            `json:"sequence"`
	ObservedAt      time.Time         `json:"observed_at"`
	Failure         string            `json:"failure,omitempty"`
	// Sanitized protocol code only; retained through reconciliation so delayed
	// usage logging keeps the request classification without carrying raw errors.
	ErrorCode string `json:"error_code,omitempty"`
	Unknown   bool   `json:"unknown"`
}

type OpenAICloudCommand struct {
	Deadline       time.Time          `json:"deadline,omitempty"`
	RequestID      string             `json:"request_id"`
	Request        OpenAICloudRequest `json:"request"`
	Cancel         bool               `json:"cancel,omitempty"`
	CloseCompleted bool               `json:"close_completed,omitempty"`
}

type OpenAICloudStreamMessage struct {
	ID      string
	Payload json.RawMessage
}

type OpenAICloudStreamEvent struct {
	NotStarted bool               `json:"not_started,omitempty"`
	Event      json.RawMessage    `json:"event,omitempty"`
	Result     *OpenAICloudResult `json:"result,omitempty"`
	Error      string             `json:"error,omitempty"`
}

// Implementations fail closed if Redis is unavailable. Create atomically
// enforces global/user/account session limits. All payloads/streams are bounded
// and expire; records, reference indexes and command/event streams use StateTTL.
// The owner lease is separate from the retention TTL and must fence Save.
type OpenAICloudSessionStore interface {
	// Account-wide unresolved consumption survives credential generations.
	CloudAccountBlocked(context.Context, int64) (bool, error)
	CreateCloudSession(context.Context, *OpenAICloudSessionRecord) error
	LoadCloudSession(context.Context, string) (*OpenAICloudSessionRecord, error)
	FindCloudSession(context.Context, int64, int64, int64, string) (*OpenAICloudSessionRecord, error)
	// Save atomically updates state, binds response/call references to this
	// owner scope, and, when nonnil, publishes a root-keyed durable settlement.
	SaveCloudSession(context.Context, *OpenAICloudSessionRecord, []string, *OpenAICloudSettlement) error
	RenewCloudSession(context.Context, string, string, time.Time) (bool, error)
	EnqueueCloudCommand(context.Context, string, OpenAICloudCommand) error
	ReadCloudCommands(context.Context, string, string) ([]OpenAICloudStreamMessage, error)
	PublishCloudEvent(context.Context, string, OpenAICloudStreamEvent) error
	ReadCloudEvents(context.Context, string, string) ([]OpenAICloudStreamMessage, error)
	DeleteCloudEvents(context.Context, string) error
	CloudRequestClosed(context.Context, string) (bool, error)
	ReleaseCloudSession(context.Context, string, string) error
	// Settlement claim is a renewable lease, not removal. Ack must verify the
	// worker and remove only after the existing billing transaction succeeded.
	ListCloudSettlements(context.Context, int) ([]*OpenAICloudSettlement, error)
	GetCloudSettlement(context.Context, string) (*OpenAICloudSettlement, error)
	ClaimCloudSettlement(context.Context, string, string, time.Duration) (bool, error)
	AckCloudSettlement(context.Context, string, string) error
	FailCloudProbeSettlement(context.Context, string, string) error
	CloudSettlementAcknowledged(context.Context, string) (bool, error)
	ListExpiredCloudSessions(context.Context, int) ([]*OpenAICloudSessionRecord, error)
	// Claim only expired owners. The new owner may recover trusted final usage;
	// it must never replay turn/start or pretend pending server RPCs survived.
	ClaimCloudSession(context.Context, string, string, time.Duration) (*OpenAICloudSessionRecord, bool, error)
}

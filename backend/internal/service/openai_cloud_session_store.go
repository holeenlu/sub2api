package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	OpenAICloudSessionTTL     = 15 * time.Minute
	OpenAICloudOwnerLease     = 20 * time.Second
	OpenAICloudStateTTL       = time.Hour
	OpenAICloudSettlementTTL  = 24 * time.Hour
	OpenAICloudMaxResultBytes = 4 << 20
	// Room for the 256KiB billing snapshot and bounded ownership/event metadata.
	OpenAICloudMaxEnvelopeBytes    = OpenAICloudMaxResultBytes + (512 << 10)
	OpenAICloudMaxEventStreamBytes = 16 << 20
	OpenAICloudMaxStreamEntries    = 8192
	OpenAICloudMaxCommands         = 64
	// Cancel and close keep reserved queue slots above OpenAICloudMaxCommands,
	// in Redis and in the owner's local inbox, so a backlog never blocks them.
	OpenAICloudControlCommands = 16
	// One cross-instance command (request body, billing snapshot and routing
	// fields, encoded by OpenAICloudEncodeCommand) may use the whole unconsumed
	// command budget of its session, which equals the owner's single upstream
	// WebSocket frame. A request to an owner in this process never takes this
	// path and is bounded by the ingress body limit alone, like native.
	OpenAICloudMaxCommandBytes    = OpenAICloudMaxEventStreamBytes
	OpenAICloudMaxSessions        = 128
	OpenAICloudMaxUserSessions    = 8
	OpenAICloudMaxAccountSessions = 16
)

// ErrOpenAICloudCommandRejected is returned by EnqueueCloudCommand only when the
// store proved the command was not written (owner gone or queue full). Any
// other enqueue error may have written it and is never proof of non-dispatch.
var ErrOpenAICloudCommandRejected = errors.New("Cloud command was rejected by the session owner state")

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
	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	CacheWriteInputTokens int `json:"cache_write_input_tokens"`
	TotalTokens           int `json:"total_tokens"`
	// Part of OutputTokens; reported publicly, never billed separately.
	ReasoningOutputTokens int            `json:"reasoning_output_tokens,omitempty"`
	WebSearchCalls        int            `json:"web_search_calls,omitempty"`
	ImageCount            int            `json:"image_count,omitempty"`
	ImageSizes            map[string]int `json:"image_sizes,omitempty"`
	ImageOutputSizes      []string       `json:"image_output_sizes,omitempty"`
}

// OpenAICloudTelemetry contains observed timing only; unknown stages remain nil.
// WaitingToolsMs is retained separately from the full turn wall duration.
type OpenAICloudTelemetry struct {
	StartedAt         time.Time            `json:"started_at"`
	UpstreamStartedAt time.Time            `json:"upstream_started_at,omitempty"`
	FirstTokenMs      *int                 `json:"first_token_ms,omitempty"`
	LatencyMetrics    *UsageLatencyMetrics `json:"latency_metrics,omitempty"`
	OpenAIWSMode      bool                 `json:"openai_ws_mode"`
	WaitingToolsMs    int64                `json:"waiting_tools_ms,omitempty"`
}

type OpenAICloudRequest struct {
	Owner           OpenAICloudOwner      `json:"owner"`
	Body            json.RawMessage       `json:"body"`
	RootRequestID   string                `json:"root_request_id"`
	BillingSnapshot json.RawMessage       `json:"billing_snapshot,omitempty"`
	Telemetry       *OpenAICloudTelemetry `json:"telemetry,omitempty"`
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
	OwnerToken      string                `json:"owner_token"`
	LeaseUntil      time.Time             `json:"lease_until"`
	BillingSnapshot json.RawMessage       `json:"billing_snapshot"`
	Usage           *OpenAICloudUsage     `json:"usage"`
	Sequence        uint64                `json:"sequence"`
	Result          *OpenAICloudResult    `json:"result,omitempty"`
	Failure         string                `json:"failure,omitempty"`
	Telemetry       *OpenAICloudTelemetry `json:"telemetry,omitempty"`
}

type OpenAICloudSettlement struct {
	OpenAICloudBinding
	BillingSnapshot json.RawMessage   `json:"billing_snapshot"`
	Status          string            `json:"status"`
	Usage           *OpenAICloudUsage `json:"usage"`
	Sequence        uint64            `json:"sequence"`
	ObservedAt      time.Time         `json:"observed_at"`
	Failure         string            `json:"failure,omitempty"`
	// Sanitized protocol code only; retained with settlement facts so delayed
	// usage logging keeps the request classification without carrying raw errors.
	ErrorCode string `json:"error_code,omitempty"`
	Unknown   bool   `json:"unknown"`
	// LowerBound marks an interrupted/failed turn whose end the upstream
	// confirmed with turn/completed: Usage covers every completed sampling step
	// and is billed as reported, while the discarded in-flight step is not
	// estimated. Never set with Unknown or on a completed turn.
	LowerBound bool                  `json:"lower_bound,omitempty"`
	Telemetry  *OpenAICloudTelemetry `json:"telemetry,omitempty"`
	// Exact Redis bytes and validation failure are queue-reader metadata only.
	// Malformed records can be archived without decoding, rewriting or dropping facts.
	ArchivePayload []byte `json:"-"`
	ArchiveReason  string `json:"-"`
}

type OpenAICloudCommand struct {
	Deadline       time.Time          `json:"deadline,omitempty"`
	RequestID      string             `json:"request_id"`
	Request        OpenAICloudRequest `json:"request"`
	Cancel         bool               `json:"cancel,omitempty"`
	CloseCompleted bool               `json:"close_completed,omitempty"`
	// HistoryRoute marks a full-history request delivered to a completed thread
	// only because it carries that thread's old call IDs. The owner continues it
	// only if history and settings still match; otherwise it rejects unstarted.
	HistoryRoute bool `json:"history_route,omitempty"`
}

// OpenAICloudEncodeCommand is the byte-exact encoding of a command in the
// cross-instance command stream: compact JSON without HTML escaping, so an
// inline image or text body is stored at its request size instead of growing
// up to sixfold for `<`, `>` and `&`. The size check before dispatch and the
// store share it; Redis and Go decoders accept either form.
func OpenAICloudEncodeCommand(command OpenAICloudCommand) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(command); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
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
// enforces global/user/account session limits. Payloads/streams are bounded;
// reference indexes and command/event streams use StateTTL. Active
// records and unsettled facts remain until fenced cleanup/settlement/archival.
// The owner lease is separate from the idle window and must fence Save.
type OpenAICloudSessionStore interface {
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
	// Remove a byte-identical record only after durable SQL archival. No ACK/debit.
	ArchiveCloudSettlement(context.Context, string, string, []byte) error
	CloudSettlementArchived(context.Context, string) (bool, error)
	ClaimCloudSettlement(context.Context, string, string, time.Duration) (bool, error)
	AckCloudSettlement(context.Context, string, string) error
	FailCloudProbeSettlement(context.Context, string, string) error
	CloudSettlementAcknowledged(context.Context, string) (bool, error)
	ListExpiredCloudSessions(context.Context, int) ([]*OpenAICloudSessionRecord, error)
	// Claim only expired owners. The new owner may recover trusted final usage;
	// it must never replay turn/start or pretend pending server RPCs survived.
	ClaimCloudSession(context.Context, string, string, time.Duration) (*OpenAICloudSessionRecord, bool, error)
}

// OpenAICloudSettlementArchiveRepository preserves unresolved facts independently
// of hot Redis state and business-row deletion. It never performs a debit.
type OpenAICloudSettlementArchiveRepository interface {
	StoreCloudSettlementArchive(context.Context, string, string, []byte) error
	HasCloudSettlementArchive(context.Context, string) (bool, error)
}

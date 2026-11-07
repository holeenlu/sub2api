package model

// ClientProtocol is the authenticated ingress's response contract, not a mode
// or permission selector. An unset value does not identify an HTTP ingress.
type ClientProtocol string

const (
	ProtocolResponses       ClientProtocol = "responses"
	ProtocolMessages        ClientProtocol = "messages"
	ProtocolChatCompletions ClientProtocol = "chat_completions"
)

type OperationState uint8

const (
	OperationAccepted OperationState = iota
	OperationWaiting
	OperationPreparing
	OperationDispatching
	OperationReceiving
	OperationRetryPlanning
	OperationCompleted
	OperationFailed
	OperationCanceled
	OperationSuspended
)

type UsageConfidence uint8

const (
	UsageUnknown UsageConfidence = iota
	UsagePartial
	UsageConfirmedLowerBound
	UsageFinal
)

type TerminalState string

// GenerationState is independent of SendState: a sent RPC can be explicitly
// rejected before inference. Unknown is never proof of replay safety.
type GenerationState uint8

const (
	GenerationUnknown GenerationState = iota
	GenerationRejected
	GenerationStarted
)

const (
	TerminalUnknown    TerminalState = ""
	TerminalCompleted  TerminalState = "completed"
	TerminalFailed     TerminalState = "failed"
	TerminalIncomplete TerminalState = "incomplete"
	TerminalCanceled   TerminalState = "canceled"
)

// A nil measurement is unobserved; a present Value=0 is a measured zero.
type TokenMeasurement struct{ Value int64 }
type UsageFact struct {
	// Total and Reasoning are optional raw normalization evidence, not extra charges.
	Total, Reasoning                     *TokenMeasurement
	Input, Output, CacheRead, CacheWrite *TokenMeasurement
	// ImageInput/ImageOutput are explicitly observed image-token components.
	// Nil is absent, Value=0 is measured zero; neither is derived from totals.
	ImageInput, ImageOutput *TokenMeasurement
	// Hosted-image tools report an independent scope. A response's explicit
	// image zero must not erase or be rewritten by a tool measurement.
	HostedImageInput, HostedImageOutput *TokenMeasurement
	// ImageCount is the number of completed outputs proven by the existing
	// image counter. Nil is unobserved and does not imply zero token usage.
	ImageCount        *int64
	Confidence        UsageConfidence
	TerminalConfirmed bool
	ResponseID        string
	ReturnedModel     string
}

// FactDelta describes only observations from the same bytes returned by Next.
// EndOfStream never manufactures TerminalCompleted or a zero usage observation.
type FactDelta struct {
	ProviderTurnID      string
	Generation          GenerationState
	RejectionEvidenceID string
	ResponseID          string
	ReturnedModel       string
	Terminal            TerminalState
	Usage               *UsageFact
	EndOfStream         bool
}

type WireFrame struct{ Data []byte }

// Receipt is recorded after the real writer. A failed/partial business write
// may have committed output even when the caller cannot prove the byte count.
type DeliveryReceipt struct {
	BytesWritten      int64
	HeadersCommitted  bool
	BusinessCommitted bool
	PossiblyCommitted bool
	FlushAttempted    bool
	Flushed           bool
	Closed            bool
}

type BillingDisposition string

const (
	BillingNotRequired     BillingDisposition = "not_required"
	BillingPendingUsage    BillingDisposition = "pending_usage"
	BillingUsageRecorded   BillingDisposition = "usage_recorded"
	BillingPendingPricing  BillingDisposition = "pending_pricing"
	BillingSettled         BillingDisposition = "settled"
	BillingRecoveryPending BillingDisposition = "recovery_pending"
)

// Durable is an acknowledgement from the existing journal owner. Merely
// constructing an operation result or queuing a closure cannot set it true.
type BillingReceipt struct {
	Disposition BillingDisposition
	Durable     bool
	EvidenceID  string
}

type AttemptFact struct {
	// BillingRoot is observed from the existing journal, not an operation ID
	// invented before BPS returns its authoritative upstream request identity.
	BillingRoot         string
	ProviderTurnID      string
	Generation          GenerationState
	RejectionEvidenceID string
	Billing             BillingReceipt
	Sequence            uint64
	Identity            PreparedIdentity
	Send                SendState
	RequestID           string
	ResponseID          string
	ReturnedModel       string
	Terminal            TerminalState
	Usage               *UsageFact
	EndOfStream         bool
	Delivery            DeliveryReceipt
	Closed              bool
	Failure             error `json:"-"`
}

type OperationResult struct {
	Principal                 PrincipalKind
	ProbeID                   string
	ProbeClaimDigest          [32]byte
	OperationID               string
	UserID, APIKeyID, GroupID int64
	GroupBound                bool
	Protocol                  ClientProtocol
	OriginalWireDigest        [32]byte
	State                     OperationState
	Attempts                  []AttemptFact
	Delivery                  DeliveryReceipt
	Billing                   BillingReceipt
	// DelegatedCycles describes still-existing BPS/Cloud/plugin work. A Native
	// transport change on this same operation does not delegate its accounting.
	DelegatedCycles int
	Failure         error `json:"-"`
}

func CloneUsageFact(u *UsageFact) *UsageFact {
	if u == nil {
		return nil
	}
	v := *u
	clone := func(p *TokenMeasurement) *TokenMeasurement {
		if p == nil {
			return nil
		}
		q := *p
		return &q
	}
	v.Total = clone(u.Total)
	v.Reasoning = clone(u.Reasoning)
	v.Input = clone(u.Input)
	v.Output = clone(u.Output)
	v.CacheRead = clone(u.CacheRead)
	v.CacheWrite = clone(u.CacheWrite)
	v.ImageInput = clone(u.ImageInput)
	v.ImageOutput = clone(u.ImageOutput)
	v.HostedImageInput = clone(u.HostedImageInput)
	v.HostedImageOutput = clone(u.HostedImageOutput)
	if u.ImageCount != nil {
		count := *u.ImageCount
		v.ImageCount = &count
	}
	return &v
}

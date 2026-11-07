package model

// PrincipalKind preserves customer admission as the existing zero value.
// Explicit probe variants require a real durable claim owned by the trusted
// probe/admin host. They do not acquire customer scope or a fabricated API key.
type PrincipalKind uint8

const (
	PrincipalCustomer PrincipalKind = iota
	PrincipalAdministratorProbe
	PrincipalSystemProbe
)

func ValidExecutionPrincipal(kind PrincipalKind, userID, keyID, groupID int64, groupBound bool, probeID string, claim [32]byte) bool {
	switch kind {
	case PrincipalCustomer:
		return userID > 0 && keyID > 0 && probeID == "" && claim == ([32]byte{}) &&
			((groupBound && groupID > 0) || (!groupBound && groupID == 0))
	case PrincipalAdministratorProbe:
		return userID > 0 && keyID == 0 && groupID == 0 && !groupBound && probeID != "" && claim != ([32]byte{})
	case PrincipalSystemProbe:
		return userID == 0 && keyID == 0 && groupID == 0 && !groupBound && probeID != "" && claim != ([32]byte{})
	default:
		return false
	}
}

// AttemptRole identifies the actual effect of a sealed mode request. A repair
// or compaction that invokes inference remains a generation, even if its output
// is held internally. Empty is reserved for existing native preparations.
type AttemptRole string

const (
	AttemptGenerate         AttemptRole = "generate"
	AttemptRepair           AttemptRole = "repair"
	AttemptCompact          AttemptRole = "compact"
	AttemptResume           AttemptRole = "resume"
	AttemptControl          AttemptRole = "control"
	AttemptPreparationRetry AttemptRole = "preparation_retry"
)

func (r AttemptRole) Purpose() (DispatchPurpose, bool) {
	switch r {
	case AttemptGenerate, AttemptRepair, AttemptCompact, AttemptResume:
		return DispatchGeneration, true
	case AttemptControl:
		return DispatchControl, true
	case AttemptPreparationRetry:
		return DispatchInternalPreparation, true
	default:
		return DispatchGeneration, false
	}
}

// ModeBinding preserves the existing mode's billing and owner identity. A
// Cloud root may span several client operations; it is never replaced by the
// current operation ID. OwnerEpoch is a digest of the actual owned token, not
// a locally invented epoch or the public session ID. These values are claims
// until the dispatch authority checks their authoritative source.
type ModeBinding struct {
	// BillingScope is pre-send correlation, NEVER a monetary request_id. BPS
	// learns its actual upstream request/compaction settlement ID after sending.
	BillingScope string
	BillingRoot  string
	// ProbeID is internal consumption attribution, never a customer charge.
	ProbeID    string
	SessionID  string
	OwnerEpoch [32]byte
}

func (b ModeBinding) Valid(mode string) bool {
	switch mode {
	case "bps":
		return b.BillingScope != "" && b.BillingRoot == "" && b.SessionID == "" && b.OwnerEpoch == ([32]byte{})
	case "cloud":
		return (b.BillingRoot == "") != (b.ProbeID == "") && b.SessionID != "" && b.OwnerEpoch != ([32]byte{})
	default:
		return false
	}
}

// ModeBillingReceipt comes from the existing amount/usage journal, AFTER its
// actual canonical identity is known. BPS successful compaction can share the
// generation's root by naming both sequences; failed compaction has its own
// receipt and compact ID. No receipt implicitly covers every attempt in a scope.
// ProbeID identifies internal metering and cannot carry a customer charge/root.
type ModeBillingReceipt struct {
	RootRequestID string
	APIKeyID      int64
	ProbeID       string
	Attempts      []uint64
	Receipt       BillingReceipt
}

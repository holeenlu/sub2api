package model

// BodyKind distinguishes a known, replayable byte sequence from a transferred
// single-owner stream. Unknown is invalid, never an empty or replayable body.
type BodyKind uint8

const (
	BodyUnknown BodyKind = iota
	BodyReplayable
	BodyExclusiveStream
)

// DispatchPurpose comes from the authenticated root, never a client flag.
// Zero retains ordinary generation admission for all existing HTTP callers.
type DispatchPurpose uint8

const (
	DispatchGeneration DispatchPurpose = iota
	DispatchInternalPreparation
	// Control covers an initial upload/history/control request which cannot
	// start inference. Retrying it uses DispatchInternalPreparation instead.
	// Cleanup (interrupt/close) has its own bounded lifetime, not a send permit.
	DispatchControl
)

// PreparedIdentity seals one operation's final transport specification. Digests
// contain no credentials or wire plaintext. The provider derives WireDigest
// after encoding and all security-relevant header/target changes. An exclusive
// stream has a unique BodyHandle and an explicitly unknown BodyDigest; it cannot
// be silently converted into a replayable request. Values are comparable copies.
type PreparedIdentity struct {
	Purpose          DispatchPurpose
	Role             AttemptRole
	Binding          ModeBinding
	Principal        PrincipalKind
	ProbeID          string
	ProbeClaimDigest [32]byte
	OperationID      string
	ResourceID       int64
	Owner            ResourceOwner
	UserID           int64
	APIKeyID         int64
	GroupID          int64
	GroupBound       bool
	Mode             string
	Transport        string
	GrantDigest      [32]byte
	TargetDigest     [32]byte
	WireDigest       [32]byte
	BodyKind         BodyKind
	BodyDigest       [32]byte
	BodyHandle       [16]byte
}

// Valid checks completeness, not authority. Only execution's configured live
// authority can approve these facts; a client-supplied identity is never a grant.
func (i PreparedIdentity) Valid() bool {
	if i.Purpose > DispatchControl || i.OperationID == "" || i.ResourceID <= 0 || i.Owner.AccountID <= 0 ||
		!ValidExecutionPrincipal(i.Principal, i.UserID, i.APIKeyID, i.GroupID, i.GroupBound, i.ProbeID, i.ProbeClaimDigest) || i.Mode == "" || i.Transport == "" ||
		i.GrantDigest == ([32]byte{}) || i.TargetDigest == ([32]byte{}) || i.WireDigest == ([32]byte{}) {
		return false
	}
	if i.Principal != PrincipalCustomer && (i.Binding.BillingRoot != "" || i.Binding.ProbeID != i.ProbeID) || i.Principal == PrincipalCustomer && i.Binding.ProbeID != "" {
		return false
	}
	switch i.BodyKind {
	case BodyReplayable:
		return i.BodyDigest != ([32]byte{}) && i.BodyHandle == ([16]byte{})
	case BodyExclusiveStream:
		return i.BodyDigest == ([32]byte{}) && i.BodyHandle != ([16]byte{})
	default:
		return false
	}
}

// SendState describes transport evidence, not upstream generation completion.
// Its zero value is unknown; missing evidence must never become NotSent.
type SendState uint8

const (
	SendUnknown SendState = iota
	SendNotSent
	SendSent
	SendAmbiguous
)

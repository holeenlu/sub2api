package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

var ErrNativeUsageEvidence = errors.New("invalid native usage evidence")
var ErrNativeUsageConflict = errors.New("native usage evidence conflict")

type NativeUsageConfidence uint8

const (
	NativeUsageUnknown NativeUsageConfidence = iota
	NativeUsagePartial
	NativeUsageFinal
)

type NativeUsageSemantics uint8

const (
	NativeUsageSemanticsUnknown NativeUsageSemantics = iota
	NativeUsageCumulative
	NativeUsageIncremental
)

// Nil measurements remain absent. In particular a missing usage object is not
// a measured zero, and incremental samples are never added by this recorder.
type NativeUsageMeasurements struct {
	Semantics                                                       NativeUsageSemantics
	Source                                                          string
	ObservationID                                                   string // Optional actual upstream event ID, never generated locally.
	ReturnedModel                                                   string
	TotalTokens                                                     *int64
	InputTokens, OutputTokens, CacheReadTokens, CacheCreationTokens *int64
	CacheCreation5mTokens, CacheCreation1hTokens                    *int64
	ImageInputTokens, ImageOutputTokens, ReasoningTokens            *int64
	HostedImageInputTokens, HostedImageOutputTokens                 *int64
	AudioInputTokens, AudioOutputTokens, ImageCount                 *int64
}

type NativeUsageEvidence struct {
	// Zero/one retain legacy evidence. V2 belongs to a preserved technical
	// intent and may await its real canonical request ID.
	Version uint8
	// Assigned by the real Operation; zero denotes an unsequenced historical observation.
	AttemptSequence                       uint64
	Terminal                              model.TerminalState
	TerminalConfirmed                     bool
	ConfirmedLowerBound                   bool
	Identity                              Identity
	OperationID                           string
	UserID, GroupID                       int64
	GroupBound                            bool
	OriginalWireDigest                    [32]byte
	Prepared                              *model.PreparedIdentity
	UpstreamRequestID, UpstreamResponseID string
	SendState                             model.SendState
	Confidence                            NativeUsageConfidence
	Usage                                 *NativeUsageMeasurements
	// V1 hashes its frozen quote. Intent-backed V2 uses the immutable request
	// scope fingerprint here; each actual attempt has its own captured scope.
	PricingDigest      [32]byte
	AttemptScopeDigest [32]byte
}

func (e NativeUsageEvidence) Validate() error {
	if e.Version > 2 || (e.Version != 2 && strings.TrimSpace(e.Identity.RequestID) == "") ||
		(e.Identity.RequestID != "" && strings.TrimSpace(e.Identity.RequestID) != e.Identity.RequestID) ||
		len(e.Identity.RequestID) > 64 || e.Identity.APIKeyID <= 0 ||
		strings.TrimSpace(e.OperationID) == "" || e.UserID <= 0 || e.OriginalWireDigest == ([32]byte{}) ||
		e.PricingDigest == ([32]byte{}) || e.Confidence > NativeUsageFinal || e.SendState > model.SendAmbiguous ||
		(e.GroupBound && e.GroupID <= 0) || (!e.GroupBound && e.GroupID != 0) ||
		len(e.UpstreamRequestID) > 512 || len(e.UpstreamResponseID) > 512 {
		return ErrNativeUsageEvidence
	}
	if p := e.Prepared; p != nil && (!p.Valid() || p.OperationID != e.OperationID || p.APIKeyID != e.Identity.APIKeyID ||
		p.UserID != e.UserID || p.GroupID != e.GroupID || p.GroupBound != e.GroupBound || p.Mode != "native" ||
		(p.Transport != "http" && p.Transport != "ws")) {
		return ErrNativeUsageEvidence
	}
	if e.Prepared == nil && (e.SendState == model.SendSent || e.SendState == model.SendAmbiguous) {
		return ErrNativeUsageEvidence
	}
	if e.Confidence != NativeUsageUnknown && e.Usage == nil {
		return ErrNativeUsageEvidence
	}
	if e.Usage != nil {
		u := e.Usage
		if u.Semantics != NativeUsageCumulative && u.Semantics != NativeUsageIncremental {
			return ErrNativeUsageEvidence
		}
		observed := false
		for _, n := range []*int64{u.TotalTokens, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheCreationTokens, u.CacheCreation5mTokens, u.CacheCreation1hTokens, u.ImageInputTokens, u.ImageOutputTokens, u.HostedImageInputTokens, u.HostedImageOutputTokens, u.ReasoningTokens, u.AudioInputTokens, u.AudioOutputTokens, u.ImageCount} {
			if n != nil {
				observed = true
				if *n < 0 {
					return ErrNativeUsageEvidence
				}
			}
		}
		if !observed {
			return ErrNativeUsageEvidence
		}
	}
	if (e.ConfirmedLowerBound && e.Confidence == NativeUsageFinal) || (e.AttemptSequence > 0 && e.Prepared == nil) {
		return ErrNativeUsageEvidence
	}
	if e.Confidence == NativeUsageFinal && (e.Usage == nil || e.Prepared == nil || e.SendState == model.SendNotSent) {
		return ErrNativeUsageEvidence
	}
	return nil
}

// Fingerprint binds the immutable root, not amounts or optional provider IDs.
// A later confirmed fact can fill missing IDs but cannot change their owner.
func (e NativeUsageEvidence) Fingerprint() string {
	b, _ := json.Marshal([]any{"native-usage-evidence-v1", e.Identity, e.OperationID, e.UserID, e.GroupID, e.GroupBound, e.OriginalWireDigest, e.PricingDigest})
	if e.Version == 2 {
		// Money identity is a late proposal, not the technical request root.
		b, _ = json.Marshal([]any{"native-usage-evidence-v2", e.Identity.APIKeyID, e.OperationID, e.UserID, e.GroupID, e.GroupBound, e.OriginalWireDigest, e.PricingDigest})
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Merge retains all earlier known identity facts. The journal separately keeps
// immutable observations, so selecting a later cumulative snapshot is not an
// arithmetic aggregation or destruction of an earlier incremental sample.
func MergeNativeUsageEvidence(prior, next NativeUsageEvidence) (NativeUsageEvidence, error) {
	if prior.Validate() != nil || next.Validate() != nil || prior.Fingerprint() != next.Fingerprint() || prior.AttemptSequence != next.AttemptSequence {
		return NativeUsageEvidence{}, ErrNativeUsageConflict
	}
	if prior.Prepared != nil && next.Prepared != nil && *prior.Prepared != *next.Prepared {
		return NativeUsageEvidence{}, ErrNativeUsageConflict
	}
	if prior.AttemptScopeDigest != next.AttemptScopeDigest {
		return NativeUsageEvidence{}, ErrNativeUsageConflict
	}
	if prior.Version != next.Version || (prior.Identity.RequestID != "" && next.Identity.RequestID != "" && prior.Identity.RequestID != next.Identity.RequestID) {
		return NativeUsageEvidence{}, ErrNativeUsageConflict
	}
	if (prior.UpstreamRequestID != "" && next.UpstreamRequestID != "" && prior.UpstreamRequestID != next.UpstreamRequestID) ||
		(prior.UpstreamResponseID != "" && next.UpstreamResponseID != "" && prior.UpstreamResponseID != next.UpstreamResponseID) {
		return NativeUsageEvidence{}, ErrNativeUsageConflict
	}
	if prior.Terminal != model.TerminalUnknown && next.Terminal != model.TerminalUnknown && prior.Terminal != next.Terminal {
		return NativeUsageEvidence{}, ErrNativeUsageConflict
	}
	if prior.Usage != nil && next.Usage != nil {
		old, newUsage := prior.Usage, next.Usage
		if prior.Confidence == NativeUsageFinal {
			known := []*int64{old.TotalTokens, old.InputTokens, old.OutputTokens, old.CacheReadTokens, old.CacheCreationTokens, old.CacheCreation5mTokens, old.CacheCreation1hTokens, old.ImageInputTokens, old.ImageOutputTokens, old.HostedImageInputTokens, old.HostedImageOutputTokens, old.ReasoningTokens, old.AudioInputTokens, old.AudioOutputTokens, old.ImageCount}
			incoming := []*int64{newUsage.TotalTokens, newUsage.InputTokens, newUsage.OutputTokens, newUsage.CacheReadTokens, newUsage.CacheCreationTokens, newUsage.CacheCreation5mTokens, newUsage.CacheCreation1hTokens, newUsage.ImageInputTokens, newUsage.ImageOutputTokens, newUsage.HostedImageInputTokens, newUsage.HostedImageOutputTokens, newUsage.ReasoningTokens, newUsage.AudioInputTokens, newUsage.AudioOutputTokens, newUsage.ImageCount}
			for i, n := range known {
				if n != nil && incoming[i] != nil && *n != *incoming[i] {
					return NativeUsageEvidence{}, ErrNativeUsageConflict
				}
			}
		}
		if old.ReturnedModel != "" && newUsage.ReturnedModel != "" && old.ReturnedModel != newUsage.ReturnedModel {
			return NativeUsageEvidence{}, ErrNativeUsageConflict
		}
		if prior.Confidence == NativeUsageFinal && next.Confidence == NativeUsageFinal {
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(newUsage)
			if string(a) != string(b) {
				return NativeUsageEvidence{}, ErrNativeUsageConflict
			}
		}
		if old.Semantics == NativeUsageCumulative && newUsage.Semantics == NativeUsageCumulative {
			copy := *newUsage
			previous := []*int64{old.TotalTokens, old.InputTokens, old.OutputTokens, old.CacheReadTokens, old.CacheCreationTokens, old.CacheCreation5mTokens, old.CacheCreation1hTokens, old.ImageInputTokens, old.ImageOutputTokens, old.HostedImageInputTokens, old.HostedImageOutputTokens, old.ReasoningTokens, old.AudioInputTokens, old.AudioOutputTokens, old.ImageCount}
			current := []**int64{&copy.TotalTokens, &copy.InputTokens, &copy.OutputTokens, &copy.CacheReadTokens, &copy.CacheCreationTokens, &copy.CacheCreation5mTokens, &copy.CacheCreation1hTokens, &copy.ImageInputTokens, &copy.ImageOutputTokens, &copy.HostedImageInputTokens, &copy.HostedImageOutputTokens, &copy.ReasoningTokens, &copy.AudioInputTokens, &copy.AudioOutputTokens, &copy.ImageCount}
			for i, n := range previous {
				if n != nil && *current[i] != nil && **current[i] < *n {
					return NativeUsageEvidence{}, ErrNativeUsageConflict
				}
				if *current[i] == nil {
					*current[i] = n
				}
			}
			if copy.ReturnedModel == "" {
				copy.ReturnedModel = old.ReturnedModel
			}
			next.Usage = &copy
		} else if old.Semantics == NativeUsageIncremental && newUsage.Semantics == NativeUsageIncremental {
			if old.ObservationID == "" || newUsage.ObservationID == "" || old.ObservationID == newUsage.ObservationID {
				a, _ := json.Marshal(old)
				b, _ := json.Marshal(newUsage)
				if string(a) != string(b) {
					return NativeUsageEvidence{}, ErrNativeUsageConflict
				}
			}
		} else if newUsage.Semantics != NativeUsageCumulative && prior.Confidence >= NativeUsagePartial {
			return NativeUsageEvidence{}, ErrNativeUsageConflict
		}
	}
	merged := next
	if merged.Identity.RequestID == "" {
		merged.Identity.RequestID = prior.Identity.RequestID
	}
	if merged.Terminal == model.TerminalUnknown {
		merged.Terminal = prior.Terminal
	}
	merged.TerminalConfirmed = prior.TerminalConfirmed || next.TerminalConfirmed
	merged.ConfirmedLowerBound = next.ConfirmedLowerBound || (prior.ConfirmedLowerBound && next.Confidence != NativeUsageFinal)
	if prior.Confidence > next.Confidence {
		merged.Confidence, merged.Usage = prior.Confidence, prior.Usage
	}
	if merged.Usage == nil {
		merged.Usage = prior.Usage
	}
	if merged.Prepared == nil {
		merged.Prepared = prior.Prepared
	}
	if merged.UpstreamRequestID == "" {
		merged.UpstreamRequestID = prior.UpstreamRequestID
	}
	if merged.UpstreamResponseID == "" {
		merged.UpstreamResponseID = prior.UpstreamResponseID
	}
	switch {
	case prior.SendState == model.SendSent || next.SendState == model.SendSent:
		merged.SendState = model.SendSent
	case prior.SendState == model.SendAmbiguous || next.SendState == model.SendAmbiguous:
		merged.SendState = model.SendAmbiguous
	case prior.SendState == model.SendUnknown || next.SendState == model.SendUnknown:
		merged.SendState = model.SendUnknown
	}
	return merged, nil
}

type NativeUsageDisposition uint8

const (
	NativeUsageNotRecorded NativeUsageDisposition = iota
	DurablePendingUsage
	UsageRecorded
	SettlementPending
	NativePendingPricing
	NativeSettled
)

// A zero receipt cannot authorize reservation release. Settled describes the
// committed money transaction; RecoveryPending also reports cache completion.
type NativeUsageReceipt struct {
	Identity            Identity
	EvidenceFingerprint string
	Disposition         NativeUsageDisposition
	RecoveryPending     bool
}

func (r NativeUsageReceipt) Durable() bool {
	return r.Identity.RequestID != "" && r.Identity.APIKeyID > 0 && r.EvidenceFingerprint != "" && r.Disposition >= DurablePendingUsage && r.Disposition <= NativeSettled
}

type NativeEvidenceCommand interface {
	NativeEvidence() NativeUsageEvidence
	NativeSettlement() Command
}

// NativeEvidenceJournal extends the same journal. Persist returns the saved
// winning V1 settlement command, never a newly supplied conflicting price.
type NativeEvidenceJournal interface {
	PreserveNativeUsage(context.Context, NativeEvidenceCommand) (NativeUsageReceipt, Command, error)
	ReadNativeReceipt(context.Context, Identity) (NativeUsageReceipt, error)
}

func (l Ledger) RecordNativeUsage(ctx context.Context, input NativeEvidenceCommand) (NativeUsageReceipt, error) {
	var empty NativeUsageReceipt
	if ctx == nil {
		return empty, ErrNativeUsageEvidence
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	journal, ok := l.Journal.(NativeEvidenceJournal)
	if !ok || input == nil || l.Transaction == nil {
		return empty, ErrNativeUsageEvidence
	}
	if err := input.NativeEvidence().Validate(); err != nil {
		return empty, err
	}
	if proposal := input.NativeSettlement(); proposal != nil {
		e := input.NativeEvidence()
		if (e.AttemptSequence > 0 && !e.TerminalConfirmed) || e.Confidence != NativeUsageFinal || e.Usage == nil || e.Usage.Semantics != NativeUsageCumulative || !proposal.HasUsageRecord() || proposal.BillingIdentity() != e.Identity {
			return empty, ErrNativeUsageEvidence
		}
	}
	receipt, command, err := journal.PreserveNativeUsage(ctx, input)
	if err != nil {
		return empty, err
	}
	if !receipt.Durable() {
		return empty, ErrNativeUsageEvidence
	}
	if receipt.Identity != input.NativeEvidence().Identity || receipt.EvidenceFingerprint != input.NativeEvidence().Fingerprint() {
		return empty, ErrNativeUsageConflict
	}
	if command == nil || receipt.Disposition == NativeSettled {
		return receipt, nil
	}
	if !command.HasUsageRecord() || command.BillingIdentity() != receipt.Identity || (receipt.Disposition != SettlementPending && receipt.Disposition != NativePendingPricing) {
		return receipt, ErrNativeUsageEvidence
	}
	if command.NeedsPricing() {
		return receipt, nil
	}
	if err := l.settleRecorded(ctx, command); err != nil {
		return receipt, err
	}
	current, err := journal.ReadNativeReceipt(ctx, receipt.Identity)
	if err != nil {
		return receipt, err
	}
	return current, nil
}

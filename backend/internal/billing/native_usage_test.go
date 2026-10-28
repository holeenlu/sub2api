package billing

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"testing"
)

func nativeEvidenceFixture() NativeUsageEvidence {
	h := sha256.Sum256([]byte("wire"))
	return NativeUsageEvidence{Identity: Identity{"billing-root", 2}, OperationID: "real-operation", UserID: 3, OriginalWireDigest: h, PricingDigest: sha256.Sum256([]byte("null")), SendState: model.SendAmbiguous, Prepared: &model.PreparedIdentity{OperationID: "real-operation", ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: 3, APIKeyID: 2, Mode: "native", Transport: "http", GrantDigest: h, TargetDigest: h, WireDigest: h, BodyKind: model.BodyReplayable, BodyDigest: h}}
}
func nativeCount(n int64) *int64 { return &n }

func TestNativeUsageEvidencePresenceAndImmutableIdentity(t *testing.T) {
	e := nativeEvidenceFixture()
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	if e.Usage != nil || e.UpstreamRequestID != "" || e.UpstreamResponseID != "" {
		t.Fatal("unknown evidence fabricated facts")
	}
	partial := e
	partial.Confidence = NativeUsagePartial
	partial.Usage = &NativeUsageMeasurements{Semantics: NativeUsageCumulative, InputTokens: nativeCount(0)}
	merged, err := MergeNativeUsageEvidence(e, partial)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Usage.InputTokens == nil || *merged.Usage.InputTokens != 0 || merged.Usage.OutputTokens != nil {
		t.Fatal("presence facts changed")
	}
	final := partial
	final.SendState = model.SendSent
	final.Confidence = NativeUsageFinal
	final.Usage = &NativeUsageMeasurements{Semantics: NativeUsageCumulative, InputTokens: nativeCount(4), OutputTokens: nativeCount(2)}
	final.UpstreamResponseID = "observed-response"
	merged, err = MergeNativeUsageEvidence(merged, final)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Confidence != NativeUsageFinal || merged.SendState != model.SendSent || merged.Fingerprint() != e.Fingerprint() {
		t.Fatal("root identity changed on promotion")
	}
	for _, edit := range []func(*NativeUsageEvidence){
		func(v *NativeUsageEvidence) { v.UserID++ }, func(v *NativeUsageEvidence) { v.PricingDigest = sha256.Sum256([]byte("different price")) }, func(v *NativeUsageEvidence) { v.UpstreamResponseID = "different response" },
		func(v *NativeUsageEvidence) { u := *v.Usage; u.OutputTokens = nativeCount(3); v.Usage = &u },
	} {
		changed := final
		edit(&changed)
		if _, err := MergeNativeUsageEvidence(final, changed); !errors.Is(err, ErrNativeUsageConflict) {
			t.Fatalf("conflict accepted: %v", err)
		}
	}
}

func TestNativeUsageEvidencePartialNeverOverwritesKnownFacts(t *testing.T) {
	e := nativeEvidenceFixture()
	e.Confidence = NativeUsagePartial
	e.Usage = &NativeUsageMeasurements{Semantics: NativeUsageCumulative, InputTokens: nativeCount(8)}
	lower := e
	lower.Usage = &NativeUsageMeasurements{Semantics: NativeUsageCumulative, InputTokens: nativeCount(7)}
	if _, err := MergeNativeUsageEvidence(e, lower); !errors.Is(err, ErrNativeUsageConflict) {
		t.Fatal("decreasing cumulative usage accepted")
	}
	missing := e
	missing.Usage = &NativeUsageMeasurements{Semantics: NativeUsageCumulative, OutputTokens: nativeCount(1)}
	got, err := MergeNativeUsageEvidence(e, missing)
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.InputTokens == nil || *got.Usage.InputTokens != 8 {
		t.Fatal("known count was erased")
	}
	e.Usage = &NativeUsageMeasurements{Semantics: NativeUsageIncremental, InputTokens: nativeCount(2)}
	missing.Usage = &NativeUsageMeasurements{Semantics: NativeUsageIncremental, InputTokens: nativeCount(3)}
	if _, err := MergeNativeUsageEvidence(e, missing); !errors.Is(err, ErrNativeUsageConflict) {
		t.Fatal("unidentified increments were combined")
	}
	e.Usage.ObservationID = "event-one"
	missing.Usage.ObservationID = "event-two"
	got, err = MergeNativeUsageEvidence(e, missing)
	if err != nil {
		t.Fatal(err)
	}
	if *got.Usage.InputTokens != 3 {
		t.Fatal("recorder summed incremental samples")
	}
}

type nativeLedgerInput struct {
	e NativeUsageEvidence
	c Command
}

func (i nativeLedgerInput) NativeEvidence() NativeUsageEvidence { return i.e }
func (i nativeLedgerInput) NativeSettlement() Command           { return i.c }

type nativeLedgerPorts struct {
	*ledgerPorts
	receipt, current    NativeUsageReceipt
	persistErr, readErr error
	saved               Command
}

func (p *nativeLedgerPorts) PreserveNativeUsage(context.Context, NativeEvidenceCommand) (NativeUsageReceipt, Command, error) {
	p.calls = append(p.calls, "evidence")
	return p.receipt, p.saved, p.persistErr
}
func (p *nativeLedgerPorts) ReadNativeReceipt(context.Context, Identity) (NativeUsageReceipt, error) {
	p.calls = append(p.calls, "receipt")
	return p.current, p.readErr
}

func TestNativeUsageLedgerReceiptsFollowDurableFacts(t *testing.T) {
	for _, name := range []string{"unknown", "sql_failure", "settlement_failure", "receipt_read_failure", "settled", "pricing_pending"} {
		t.Run(name, func(t *testing.T) {
			e := nativeEvidenceFixture()
			base := newLedgerPorts()
			base.command.id = e.Identity
			p := &nativeLedgerPorts{ledgerPorts: base, receipt: NativeUsageReceipt{Identity: e.Identity, EvidenceFingerprint: e.Fingerprint(), Disposition: DurablePendingUsage}}
			failure := errors.New("database unavailable")
			if name != "unknown" && name != "sql_failure" {
				p.receipt.Disposition = SettlementPending
				p.saved = base.command
				p.current = p.receipt
				p.current.Disposition = NativeSettled
			}
			switch name {
			case "sql_failure":
				p.persistErr = failure
			case "settlement_failure":
				p.commitErr = failure
			case "receipt_read_failure":
				p.readErr = failure
			case "pricing_pending":
				base.command.pending = true
				p.receipt.Disposition = NativePendingPricing
			}
			got, err := (Ledger{Journal: p, Transaction: p}).RecordNativeUsage(t.Context(), nativeLedgerInput{e: e})
			switch name {
			case "sql_failure":
				if !errors.Is(err, failure) || got.Durable() {
					t.Fatal("failed SQL fabricated a receipt")
				}
			case "settlement_failure", "receipt_read_failure":
				if !errors.Is(err, failure) || got.Disposition != SettlementPending || !got.Durable() {
					t.Fatal("lost durable pending state")
				}
			default:
				if err != nil || !got.Durable() {
					t.Fatalf("receipt=%+v err=%v", got, err)
				}
			}
			if (name == "unknown" || name == "sql_failure" || name == "pricing_pending") && p.committed != nil {
				t.Fatal("unsettleable evidence debited")
			}
			if name == "settled" && got.Disposition != NativeSettled {
				t.Fatal("SQL completion lost")
			}
		})
	}
}

func TestNativeUsageLedgerRejectsUnprovenChargeBeforeJournal(t *testing.T) {
	for _, name := range []string{"unknown", "incremental", "missing_record", "foreign_key"} {
		t.Run(name, func(t *testing.T) {
			e := nativeEvidenceFixture()
			base := newLedgerPorts()
			base.command.id = e.Identity
			if name != "unknown" {
				e.Confidence = NativeUsageFinal
				e.Usage = &NativeUsageMeasurements{Semantics: NativeUsageCumulative, InputTokens: nativeCount(0)}
			}
			switch name {
			case "incremental":
				e.Usage.Semantics = NativeUsageIncremental
			case "missing_record":
				base.command.usage = false
			case "foreign_key":
				base.command.id.APIKeyID++
			}
			ports := &nativeLedgerPorts{ledgerPorts: base}
			receipt, err := (Ledger{Journal: ports, Transaction: ports}).RecordNativeUsage(t.Context(), nativeLedgerInput{e: e, c: base.command})
			if !errors.Is(err, ErrNativeUsageEvidence) || receipt.Durable() || len(base.calls) != 0 {
				t.Fatalf("unproven charge reached journal: receipt=%+v err=%v calls=%v", receipt, err, base.calls)
			}
		})
	}
}

func TestNativeUsageSequenceIsNotBillingIdentity(t *testing.T) {
	first := nativeEvidenceFixture()
	first.AttemptSequence = 1
	second := first
	second.AttemptSequence = 2
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("attempt minted a billing root")
	}
	if _, err := MergeNativeUsageEvidence(first, second); !errors.Is(err, ErrNativeUsageConflict) {
		t.Fatal("different attempts were merged into one observation")
	}
	second = first
	second.TerminalConfirmed = true
	second.Terminal = model.TerminalCompleted
	merged, err := MergeNativeUsageEvidence(first, second)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.TerminalConfirmed || merged.AttemptSequence != 1 {
		t.Fatal("terminal fact lost")
	}
}

func TestNativeUsageEvidenceNativeTransportBoundary(t *testing.T) {
	for _, transport := range []string{"http", "ws"} {
		for _, state := range []string{"unknown", "final", "preparation_observation"} {
			t.Run(transport+"/"+state, func(t *testing.T) {
				e := nativeEvidenceFixture()
				e.AttemptSequence = 1
				e.Prepared.Transport = transport
				if state == "final" {
					e.SendState, e.Confidence = model.SendSent, NativeUsageFinal
					e.Terminal, e.TerminalConfirmed = model.TerminalCompleted, true
					e.Usage = &NativeUsageMeasurements{Semantics: NativeUsageCumulative, InputTokens: nativeCount(10), OutputTokens: nativeCount(0)}
				}
				if state == "preparation_observation" {
					// Preparation remains archivable; selecting it for settlement
					// belongs to the operation/application owner, not this validator.
					e.Prepared.Purpose = model.DispatchInternalPreparation
				}
				if !e.Prepared.Valid() {
					t.Fatal("fixture must carry a complete sealed identity")
				}
				fingerprint := e.Fingerprint()
				if err := e.Validate(); err != nil {
					t.Fatalf("valid %s %s evidence rejected: %v", transport, state, err)
				}
				if e.Fingerprint() != fingerprint || e.Prepared.Transport != transport {
					t.Fatal("validation changed identity")
				}
				if state != "final" && (e.Usage != nil || e.Confidence != NativeUsageUnknown) {
					t.Fatal("missing usage became measured zero")
				}
			})
		}
	}
	for _, transport := range []string{"", "https", "wss", "websocket", "grpc", "WS", "ws "} {
		t.Run("reject_transport/"+transport, func(t *testing.T) {
			e := nativeEvidenceFixture()
			e.Prepared.Transport = transport
			if err := e.Validate(); !errors.Is(err, ErrNativeUsageEvidence) {
				t.Fatalf("transport %q accepted: %v", transport, err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*model.PreparedIdentity)
	}{
		{"bps", func(p *model.PreparedIdentity) { p.Mode = "bps" }},
		{"cloud", func(p *model.PreparedIdentity) { p.Mode = "cloud" }},
		{"other_operation", func(p *model.PreparedIdentity) { p.OperationID = "other-operation" }},
		{"other_key", func(p *model.PreparedIdentity) { p.APIKeyID++ }},
		{"other_user", func(p *model.PreparedIdentity) { p.UserID++ }},
		{"other_group", func(p *model.PreparedIdentity) { p.GroupID = 9; p.GroupBound = true }},
		{"missing_grant", func(p *model.PreparedIdentity) { p.GrantDigest = [32]byte{} }},
		{"missing_wire", func(p *model.PreparedIdentity) { p.WireDigest = [32]byte{} }},
		{"missing_target", func(p *model.PreparedIdentity) { p.TargetDigest = [32]byte{} }},
	} {
		t.Run("reject_ws/"+tc.name, func(t *testing.T) {
			e := nativeEvidenceFixture()
			e.Prepared.Transport = "ws"
			tc.change(e.Prepared)
			if err := e.Validate(); !errors.Is(err, ErrNativeUsageEvidence) {
				t.Fatalf("invalid WS identity %s accepted: %v", tc.name, err)
			}
		})
	}
}

func TestNativeUsageEvidenceNativeTransportAttemptIsolation(t *testing.T) {
	http := nativeEvidenceFixture()
	http.AttemptSequence = 1
	ws := http
	prepared := *http.Prepared
	prepared.Transport = "ws"
	ws.Prepared = &prepared
	if err := http.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := ws.Validate(); err != nil {
		t.Fatal(err)
	}
	if http.Fingerprint() != ws.Fingerprint() {
		t.Fatal("transport invented another billing root")
	}
	if _, err := MergeNativeUsageEvidence(http, ws); !errors.Is(err, ErrNativeUsageConflict) {
		t.Fatal("one attempt silently changed its sealed transport")
	}
}

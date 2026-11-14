package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.BPSUsageJournal = (*usageBillingRepository)(nil)

// BPS facts are technical rows in the existing customer journal. Only Command
// can enter the original money transaction; operation IDs never enter request_id.
// No account, API-key credential, request body or callback is persisted.
type bpsUsageEvidence struct {
	Version            int
	BeforeSendSequence uint64 `json:",omitempty"`
	Operation          model.OperationResult
	ClientTransport    service.OpenAIClientTransport `json:",omitempty"`
	Sequences          []uint64
	CanonicalID        string
	Usage              map[uint64]service.OpenAIUsage
	Command            *service.UsageBillingCommand
	ChargedSequences   []uint64
	SimpleUsage        bool
}

func prepareBPSUsageEvidence(input service.BPSUsageJournalInput) (*bpsUsageEvidence, error) {
	in := input.Finalization
	root := in.Operation
	beforeSend := input.BeforeSendSequence != 0
	if in.ClientTransport != "" && in.ClientTransport != service.OpenAIClientTransportHTTP && in.ClientTransport != service.OpenAIClientTransportWS {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if beforeSend && (input.Command != nil || input.UsageLog != nil || input.SimpleUsage || len(input.ChargedSequences) != 0 || in.CanonicalRequestID != "") {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if root.Principal != model.PrincipalCustomer || root.UserID <= 0 || root.APIKeyID <= 0 || strings.TrimSpace(root.OperationID) == "" || root.OriginalWireDigest == ([32]byte{}) ||
		len(in.CurrentSequences) == 0 || len(in.CanonicalRequestID) > 64 || strings.TrimSpace(in.CanonicalRequestID) != in.CanonicalRequestID {
		return nil, service.ErrUsageBillingRequestConflict
	}
	wanted := make(map[uint64]bool, len(in.CurrentSequences))
	for _, sequence := range in.CurrentSequences {
		if sequence == 0 || wanted[sequence] {
			return nil, service.ErrUsageBillingRequestConflict
		}
		wanted[sequence] = true
	}
	record := &bpsUsageEvidence{Version: 1, BeforeSendSequence: input.BeforeSendSequence, Operation: root, ClientTransport: in.ClientTransport, Sequences: append([]uint64(nil), in.CurrentSequences...), CanonicalID: in.CanonicalRequestID,
		Usage: make(map[uint64]service.OpenAIUsage), ChargedSequences: append([]uint64(nil), input.ChargedSequences...), SimpleUsage: input.SimpleUsage}
	record.Operation.Attempts, record.Operation.Failure = nil, nil
	record.Operation.Billing = model.BillingReceipt{}
	sort.Slice(record.Sequences, func(i, j int) bool { return record.Sequences[i] < record.Sequences[j] })
	if beforeSend && record.Sequences[len(record.Sequences)-1] != input.BeforeSendSequence {
		return nil, service.ErrUsageBillingRequestConflict
	}
	seen, chargeable := make(map[uint64]bool), make(map[uint64]bool)
	canonicalObserved := record.CanonicalID == ""
	for _, attempt := range root.Attempts {
		if !wanted[attempt.Sequence] {
			continue
		}
		p := attempt.Identity
		if seen[attempt.Sequence] || !p.Valid() || p.Mode != "bps" || p.OperationID != root.OperationID || p.UserID != root.UserID || p.APIKeyID != root.APIKeyID ||
			p.GroupID != root.GroupID || p.GroupBound != root.GroupBound || !p.Binding.Valid("bps") || (!attempt.Closed && !beforeSend) {
			return nil, service.ErrUsageBillingRequestConflict
		}
		if beforeSend && attempt.Sequence == input.BeforeSendSequence && (attempt.Closed || attempt.Send != model.SendUnknown || attempt.Usage != nil || attempt.Terminal != model.TerminalUnknown || attempt.RequestID != "" || attempt.ResponseID != "" || attempt.Delivery.BusinessCommitted || attempt.Delivery.PossiblyCommitted) {
			return nil, service.ErrUsageBillingRequestConflict
		}
		seen[attempt.Sequence] = true
		canonicalObserved = canonicalObserved || record.CanonicalID == service.BPSCanonicalRequestID(attempt, record.ClientTransport)
		attempt.Failure, attempt.Billing = nil, model.BillingReceipt{}
		record.Operation.Attempts = append(record.Operation.Attempts, attempt)
		if usage, ok := in.UsageByAttempt[attempt.Sequence]; ok {
			if attempt.Usage == nil {
				return nil, service.ErrUsageBillingRequestConflict
			}
			record.Usage[attempt.Sequence] = usage
			chargeable[attempt.Sequence], _ = service.BPSAttemptBillable(attempt)
		}
	}
	if len(seen) != len(wanted) || !canonicalObserved {
		return nil, service.ErrUsageBillingRequestConflict
	}
	sort.Slice(record.Operation.Attempts, func(i, j int) bool {
		return record.Operation.Attempts[i].Sequence < record.Operation.Attempts[j].Sequence
	})
	if input.Command != nil {
		if record.CanonicalID == "" || len(record.ChargedSequences) == 0 {
			return nil, service.ErrUsageBillingRequestConflict
		}
		charged := make(map[uint64]bool)
		for _, sequence := range record.ChargedSequences {
			if !chargeable[sequence] || charged[sequence] {
				return nil, service.ErrUsageBillingRequestConflict
			}
			charged[sequence] = true
		}
		copy := *input.Command
		copy.Normalize()
		if copy.RequestID != record.CanonicalID || copy.APIKeyID != root.APIKeyID || copy.UserID != root.UserID || copy.UsageLog == nil ||
			copy.UsageLog.RequestID != copy.RequestID || copy.UsageLog.APIKeyID != copy.APIKeyID || copy.UsageLog.UserID != copy.UserID {
			return nil, service.ErrUsageBillingRequestConflict
		}
		for _, attempt := range record.Operation.Attempts {
			if charged[attempt.Sequence] && attempt.Identity.ResourceID != copy.AccountID {
				return nil, service.ErrUsageBillingRequestConflict
			}
		}
		payload, err := usageBillingEventPayload(&copy)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &record.Command); err != nil {
			return nil, err
		}
		restoreLowerBound := func(log *service.UsageLog) {
			if log != nil && log.BillingStatus == "lower_bound" {
				log.UsageLowerBound = true
			}
		}
		restoreLowerBound(record.Command.UsageLog)
		for _, log := range record.Command.UsageLogs {
			restoreLowerBound(log)
		}
	} else if len(record.ChargedSequences) != 0 {
		return nil, service.ErrUsageBillingRequestConflict
	}
	return record, nil
}

func encodeBPSUsageEvidence(record *bpsUsageEvidence) ([]byte, string, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, "", err
	}
	if len(raw) > 1<<20 {
		return nil, "", service.ErrUsageBillingRequestConflict
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), nil
}

func decodeBPSUsageEvidence(raw []byte, fingerprint string) (*bpsUsageEvidence, error) {
	var record bpsUsageEvidence
	if len(raw) > 1<<20 || json.Unmarshal(raw, &record) != nil || record.Version != 1 {
		return nil, service.ErrUsageBillingRequestConflict
	}
	prepared, err := prepareBPSUsageEvidence(record.input())
	if err != nil {
		return nil, err
	}
	_, actual, err := encodeBPSUsageEvidence(prepared)
	if err != nil || actual != fingerprint {
		return nil, errors.Join(err, service.ErrUsageBillingRequestConflict)
	}
	return prepared, nil
}

func (r *bpsUsageEvidence) input() service.BPSUsageJournalInput {
	return service.BPSUsageJournalInput{BeforeSendSequence: r.BeforeSendSequence, Finalization: service.BPSOperationFinalization{Operation: r.Operation, ClientTransport: r.ClientTransport, CurrentSequences: r.Sequences, CanonicalRequestID: r.CanonicalID, UsageByAttempt: r.Usage},
		Command: r.Command, ChargedSequences: r.ChargedSequences, SimpleUsage: r.SimpleUsage}
}

func mergeBPSUsageEvidence(previous, next *bpsUsageEvidence) (*bpsUsageEvidence, error) {
	a, b := previous.Operation, next.Operation
	if a.OperationID != b.OperationID || a.UserID != b.UserID || a.APIKeyID != b.APIKeyID || a.GroupID != b.GroupID || a.GroupBound != b.GroupBound || a.OriginalWireDigest != b.OriginalWireDigest || previous.ClientTransport != next.ClientTransport ||
		(previous.CanonicalID != "" && next.CanonicalID != "" && previous.CanonicalID != next.CanonicalID) {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if next.BeforeSendSequence != 0 && previous.BeforeSendSequence == 0 {
		return nil, service.ErrUsageBillingRequestConflict // finalized facts cannot authorize another send
	}
	if !reflect.DeepEqual(previous.Sequences, next.Sequences) {
		// Only a registered pre-send stage may append the next actual attempt.
		// Finalization cannot invent another send or silently drop an old one.
		if next.BeforeSendSequence == 0 || previous.Command != nil || len(next.Sequences) != len(previous.Sequences)+1 || len(b.Attempts) != len(a.Attempts)+1 || !reflect.DeepEqual(previous.Sequences, next.Sequences[:len(previous.Sequences)]) {
			return nil, service.ErrUsageBillingRequestConflict
		}
	} else if len(a.Attempts) != len(b.Attempts) {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if next.CanonicalID == "" {
		next.CanonicalID = previous.CanonicalID
	}
	for i, old := range a.Attempts {
		fresh := &next.Operation.Attempts[i]
		if old.Sequence != fresh.Sequence || old.Identity != fresh.Identity || (old.Send != model.SendUnknown && old.Send != fresh.Send) || (old.Closed && !fresh.Closed) ||
			(old.RequestID != "" && fresh.RequestID != "" && old.RequestID != fresh.RequestID) ||
			(old.ResponseID != "" && fresh.ResponseID != "" && old.ResponseID != fresh.ResponseID) ||
			(old.Terminal != "" && fresh.Terminal != "" && old.Terminal != fresh.Terminal) {
			return nil, service.ErrUsageBillingRequestConflict
		}
		if fresh.RequestID == "" {
			fresh.RequestID = old.RequestID
		}
		if fresh.ResponseID == "" {
			fresh.ResponseID = old.ResponseID
		}
		if fresh.Terminal == "" {
			fresh.Terminal = old.Terminal
		}
		if old.Usage != nil && (fresh.Usage == nil || fresh.Usage.Confidence < old.Usage.Confidence) {
			fresh.Usage = model.CloneUsageFact(old.Usage)
			next.Usage[old.Sequence] = previous.Usage[old.Sequence]
		}
		if old.Usage != nil && fresh.Usage != nil {
			oldValues := []*model.TokenMeasurement{old.Usage.Input, old.Usage.Output, old.Usage.CacheRead, old.Usage.CacheWrite}
			newValues := []*model.TokenMeasurement{fresh.Usage.Input, fresh.Usage.Output, fresh.Usage.CacheRead, fresh.Usage.CacheWrite}
			for k, prior := range oldValues {
				if prior != nil && (newValues[k] == nil || newValues[k].Value < prior.Value) {
					return nil, service.ErrUsageBillingRequestConflict
				}
				if old.Usage.Confidence == model.UsageFinal && prior != nil && newValues[k] != nil && newValues[k].Value != prior.Value {
					return nil, service.ErrUsageBillingRequestConflict
				}
			}
		}
	}
	if previous.Command != nil {
		if next.Command != nil && (next.Command.RequestFingerprint != previous.Command.RequestFingerprint || !reflect.DeepEqual(next.ChargedSequences, previous.ChargedSequences)) {
			return nil, service.ErrUsageBillingRequestConflict
		}
		// A late observation cannot replace the first monetary command/price.
		next.Command, next.ChargedSequences, next.SimpleUsage = previous.Command, previous.ChargedSequences, previous.SimpleUsage
	}
	// An ACK cannot retroactively relabel already charged partial quantities as
	// final. Preserve the first observed business record; additional facts remain
	// available for reconciliation without another debit under a new identity.
	return next, nil
}

func (r *usageBillingRepository) RecordBPSUsageEvidence(ctx context.Context, input service.BPSUsageJournalInput) (service.BPSUsageJournalResult, error) {
	var result service.BPSUsageJournalResult
	if ctx == nil || r == nil || r.db == nil {
		return result, service.ErrResponseBPSJournalUnavailable
	}
	record, err := prepareBPSUsageEvidence(input)
	if err != nil {
		return result, err
	}
	raw, fingerprint, err := encodeBPSUsageEvidence(record)
	if err != nil {
		return result, err
	}
	op, key, first := record.Operation.OperationID, record.Operation.APIKeyID, record.Sequences[0]
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_billing_events(event_type,request_id,api_key_id,request_fingerprint,payload,status,bps_operation_id,bps_first_sequence,bps_evidence,bps_evidence_fingerprint)
 VALUES('bps_usage',NULL,$2,'',NULL,'pending_usage',$1,$3,$4,$5)
 ON CONFLICT(bps_operation_id,api_key_id,bps_first_sequence) DO NOTHING`, op, key, first, string(raw), fingerprint)
	if err != nil {
		return result, err
	}
	var saved []byte
	var savedFingerprint string
	err = tx.QueryRowContext(ctx, `SELECT bps_evidence,bps_evidence_fingerprint FROM usage_billing_events WHERE event_type='bps_usage' AND bps_operation_id=$1 AND api_key_id=$2 AND bps_first_sequence=$3 FOR UPDATE`, op, key, first).Scan(&saved, &savedFingerprint)
	if err != nil {
		return result, err
	}
	previous, err := decodeBPSUsageEvidence(saved, savedFingerprint)
	if err != nil {
		return result, err
	}
	record, err = mergeBPSUsageEvidence(previous, record)
	if err != nil {
		return result, err
	}
	var canonical any
	status := "pending_usage"
	if record.Command != nil {
		command := record.Command
		payload, err := usageBillingEventPayload(command)
		if err != nil {
			return result, err
		}
		moneyStatus := "pending"
		if command.Pricing != nil && command.Pricing.Pending {
			moneyStatus = "pending_pricing"
		}
		var actual string
		err = tx.QueryRowContext(ctx, `INSERT INTO usage_billing_events(request_id,api_key_id,request_fingerprint,payload,status)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(request_id,api_key_id)
 DO UPDATE SET updated_at=usage_billing_events.updated_at RETURNING request_fingerprint`, command.RequestID, command.APIKeyID, command.RequestFingerprint, string(payload), moneyStatus).Scan(&actual)
		if err != nil {
			return result, err
		}
		if actual != command.RequestFingerprint {
			return result, service.ErrUsageBillingRequestConflict
		}
		canonical, status = command.RequestID, "pending"
	}
	raw, fingerprint, err = encodeBPSUsageEvidence(record)
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE usage_billing_events SET bps_evidence=$4,bps_evidence_fingerprint=$5,bps_request_id=$6,status=$7,available_at=NOW(),updated_at=NOW()
 WHERE event_type='bps_usage' AND bps_operation_id=$1 AND api_key_id=$2 AND bps_first_sequence=$3`, op, key, first, string(raw), fingerprint, canonical, status)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	// Reuse the original customer ledger, transaction and cache invalidation.
	// An Apply error may still have a durable pending/settled receipt; read it.
	var applyErr error
	if record.Command != nil {
		result.ActualApply, applyErr = r.Apply(ctx, record.Command)
	}
	result.Receipts, err = r.bpsUsageReceipts(ctx, record, fingerprint)
	if err == nil {
		complete := len(result.Receipts) > 0 && record.BeforeSendSequence == 0
		for _, receipt := range result.Receipts {
			complete = complete && (receipt.Receipt.Disposition == model.BillingSettled || receipt.Receipt.Disposition == model.BillingUsageRecorded || receipt.Receipt.Disposition == model.BillingNotRequired)
		}
		if complete {
			completedState := "usage_recorded"
			if record.Command == nil {
				completedState = "not_required"
			}
			_, err = r.db.ExecContext(ctx, `UPDATE usage_billing_events SET status=$5,last_error=NULL,updated_at=NOW()
	 WHERE event_type='bps_usage' AND bps_operation_id=$1 AND api_key_id=$2 AND bps_first_sequence=$3 AND bps_evidence_fingerprint=$4`, op, key, first, fingerprint, completedState)
		}
	}
	return result, errors.Join(applyErr, err)
}

func (r *usageBillingRepository) bpsUsageReceipts(ctx context.Context, record *bpsUsageEvidence, fingerprint string) ([]model.ModeBillingReceipt, error) {
	charged := make(map[uint64]bool, len(record.ChargedSequences))
	var receipts []model.ModeBillingReceipt
	if command := record.Command; command != nil {
		var status, actual string
		err := r.db.QueryRowContext(ctx, `SELECT status,request_fingerprint FROM usage_billing_events WHERE event_type='money' AND request_id=$1 AND api_key_id=$2`, command.RequestID, command.APIKeyID).Scan(&status, &actual)
		if err != nil {
			return nil, err
		}
		if actual != command.RequestFingerprint {
			return nil, service.ErrUsageBillingRequestConflict
		}
		disposition := model.BillingRecoveryPending
		switch status {
		case "settled", "applied":
			disposition = model.BillingSettled
			if record.SimpleUsage {
				disposition = model.BillingUsageRecorded
			}
		case "pending_pricing":
			disposition = model.BillingPendingPricing
		case "pending", "failed":
		default:
			return nil, service.ErrUsageBillingRequestConflict
		}
		for _, sequence := range record.ChargedSequences {
			charged[sequence] = true
		}
		receipts = append(receipts, model.ModeBillingReceipt{APIKeyID: command.APIKeyID, RootRequestID: command.RequestID, Attempts: record.ChargedSequences,
			Receipt: model.BillingReceipt{Disposition: disposition, Durable: true, EvidenceID: fingerprint}})
	}
	roots := make(map[uint64]string, len(record.Operation.Attempts))
	notRequired := make(map[uint64]bool, len(record.Operation.Attempts))
	for _, attempt := range record.Operation.Attempts {
		notRequired[attempt.Sequence] = service.BPSAttemptNotRequired(attempt)
		roots[attempt.Sequence] = service.BPSCanonicalRequestID(attempt, record.ClientTransport)
	}
	for _, sequence := range record.Sequences {
		if !charged[sequence] {
			if notRequired[sequence] {
				receipts = append(receipts, model.ModeBillingReceipt{APIKeyID: record.Operation.APIKeyID, Attempts: []uint64{sequence},
					Receipt: model.BillingReceipt{Disposition: model.BillingNotRequired, Durable: true, EvidenceID: fingerprint}})
				continue
			}
			receipts = append(receipts, model.ModeBillingReceipt{APIKeyID: record.Operation.APIKeyID, RootRequestID: roots[sequence], Attempts: []uint64{sequence},
				Receipt: model.BillingReceipt{Disposition: model.BillingPendingUsage, Durable: true, EvidenceID: fingerprint}})
		}
	}
	return receipts, nil
}

// Existing host recovery owns this lease and bounded batch. Unknown facts never
// produce a command; a later same-identity observation can make them billable.
// Ready commands replay through Apply with the SAME canonical monetary key.
func (r *usageBillingRepository) recoverBPSUsageEvidence(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	if ctx == nil || r == nil || r.db == nil {
		return 0, service.ErrResponseBPSJournalUnavailable
	}
	rows, err := r.db.QueryContext(ctx, `WITH candidates AS (
 SELECT journal_event_id FROM usage_billing_events WHERE event_type='bps_usage' AND status IN ('pending','pending_usage') AND available_at<=NOW()
 ORDER BY available_at,created_at LIMIT $1 FOR UPDATE SKIP LOCKED
 ) UPDATE usage_billing_events e SET available_at=NOW()+INTERVAL '30 seconds',attempts=e.attempts+1
 FROM candidates c WHERE e.journal_event_id=c.journal_event_id RETURNING e.journal_event_id,e.bps_evidence,e.bps_evidence_fingerprint`, min(limit, 64))
	if err != nil {
		return 0, err
	}
	type claimedEvidence struct {
		id          int64
		raw         []byte
		fingerprint string
		record      *bpsUsageEvidence
	}
	var records []claimedEvidence
	var recoveryErr error
	for rows.Next() {
		var item claimedEvidence
		if err := rows.Scan(&item.id, &item.raw, &item.fingerprint); err != nil {
			recoveryErr = errors.Join(recoveryErr, err)
			continue
		}
		item.record, err = decodeBPSUsageEvidence(item.raw, item.fingerprint)
		recoveryErr = errors.Join(recoveryErr, err)
		records = append(records, item)
	}
	// Release the result connection before per-row writes, including bad-row
	// backoff. A local decode failure does not discard other leased records.
	recoveryErr = errors.Join(recoveryErr, rows.Err(), rows.Close())
	completed := 0
	for _, item := range records {
		if err := ctx.Err(); err != nil {
			return completed, errors.Join(recoveryErr, err)
		}
		record := item.record
		if record == nil {
			// Identity comes from the claimed SQL row, never its invalid payload.
			// Preserve the bytes/hash and do not punish a concurrent replacement.
			_, err := r.db.ExecContext(ctx, `UPDATE usage_billing_events
 SET available_at=NOW()+make_interval(secs=>GREATEST(30,LEAST(3600,POWER(2,LEAST(attempts,12))))::double precision),last_error='invalid_bps_evidence'
 WHERE journal_event_id=$1 AND event_type='bps_usage' AND bps_evidence_fingerprint=$2
 AND bps_evidence=$3::jsonb AND status IN ('pending','pending_usage')`, item.id, item.fingerprint, string(item.raw))
			recoveryErr = errors.Join(recoveryErr, err)
			continue
		}
		var replayErr error
		if record.Command != nil {
			result, err := r.RecordBPSUsageEvidence(ctx, record.input())
			replayErr = err
			if result.ActualApply != nil {
				completed++
			}
		}
		reason := "usage_unknown"
		if record.Command != nil {
			reason = "settlement_pending"
		}
		if replayErr != nil {
			reason = "recovery_failed"
			recoveryErr = errors.Join(recoveryErr, replayErr)
		}
		if _, err := r.db.ExecContext(ctx, `UPDATE usage_billing_events SET available_at=NOW()+make_interval(secs=>LEAST(3600,POWER(2,LEAST(attempts,12)))::double precision),last_error=$4
 WHERE event_type='bps_usage' AND bps_operation_id=$1 AND api_key_id=$2 AND bps_first_sequence=$3 AND status IN ('pending','pending_usage')`, record.Operation.OperationID, record.Operation.APIKeyID, record.Sequences[0], reason); err != nil {
			recoveryErr = errors.Join(recoveryErr, err)
		}
	}
	return completed, recoveryErr
}

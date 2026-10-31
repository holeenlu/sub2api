package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var errAccountProbeUsageConflict = errors.New("account probe usage identity or payload conflict")
var errAccountProbeUsageMissing = errors.New("account probe usage evidence is not persisted")

// The account recovery owner records observations, not money. These exact bytes
// are the counterproof for idempotence and an uncertain transaction commit.
type accountProbeUsageFact struct {
	accountID                            int64
	probeID, claimAt, scope, model, kind string
	fingerprint                          string
	payload                              []byte
}

func newAccountProbeUsageFact(kind string, outcome service.ExcelBPS403ProbeResult) (accountProbeUsageFact, error) {
	u := outcome.Usage
	if u == nil || u.AccountID <= 0 || len(u.ProbeID) != 64 || u.Scope != "internal_account_bps_recovery" ||
		u.Model == "" || len(u.Model) > 200 || len(u.ResponseID) > 512 || len(u.ResponseModel) > 200 {
		return accountProbeUsageFact{}, errAccountProbeUsageConflict
	}
	if at, err := time.Parse(time.RFC3339Nano, u.ClaimAt); err != nil || at.UTC().Format(time.RFC3339Nano) != u.ClaimAt {
		return accountProbeUsageFact{}, errAccountProbeUsageConflict
	}
	switch kind {
	case "claim":
		if outcome.Status != "probing" || u.State != "unknown" || u.SendState != "unknown" || u.Tokens != nil || u.Terminal != "" {
			return accountProbeUsageFact{}, errAccountProbeUsageConflict
		}
	case "outcome":
		if outcome.Status != "failed" && outcome.Status != "restored" {
			return accountProbeUsageFact{}, errAccountProbeUsageConflict
		}
		if _, err := time.Parse(time.RFC3339Nano, outcome.CompletedAt); err != nil || outcome.DurationMS < 0 {
			return accountProbeUsageFact{}, errAccountProbeUsageConflict
		}
		// A successful probe is not proof that its recovery CAS succeeded. The
		// UI owns restored; immutable evidence describes only the observation.
		if outcome.Status == "restored" {
			outcome.Status, outcome.Stage = "completed", "response"
		}
	default:
		return accountProbeUsageFact{}, errAccountProbeUsageConflict
	}
	raw, err := json.Marshal(outcome)
	if err != nil {
		return accountProbeUsageFact{}, err
	}
	if len(raw) > 16384 {
		return accountProbeUsageFact{}, errAccountProbeUsageConflict
	}
	sum := sha256.Sum256(raw)
	return accountProbeUsageFact{accountID: u.AccountID, probeID: u.ProbeID, claimAt: u.ClaimAt,
		scope: u.Scope, model: u.Model, kind: kind, fingerprint: hex.EncodeToString(sum[:]), payload: raw}, nil
}

// readAccountProbeUsageFact is used by all writes and commit reconciliation.
// Matching a digest alone never acknowledges different identity or bytes.
func readAccountProbeUsageFact(ctx context.Context, exec sqlExecutor, expected accountProbeUsageFact) error {
	rows, err := exec.QueryContext(ctx, `SELECT account_id,claim_at,scope,model,payload
 FROM account_probe_usage WHERE probe_id=$1 AND observation_kind=$2 AND payload_sha256=$3`,
		expected.probeID, expected.kind, expected.fingerprint)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errAccountProbeUsageMissing
	}
	var stored accountProbeUsageFact
	if err := rows.Scan(&stored.accountID, &stored.claimAt, &stored.scope, &stored.model, &stored.payload); err != nil {
		return err
	}
	if stored.accountID != expected.accountID || stored.claimAt != expected.claimAt || stored.scope != expected.scope ||
		stored.model != expected.model || !bytes.Equal(stored.payload, expected.payload) {
		return errAccountProbeUsageConflict
	}
	if rows.Next() {
		return errAccountProbeUsageConflict
	}
	return rows.Err()
}

func insertAccountProbeUsageFact(ctx context.Context, exec sqlExecutor, fact accountProbeUsageFact) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO account_probe_usage
 (probe_id,account_id,claim_at,scope,model,observation_kind,payload_sha256,payload)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
		fact.probeID, fact.accountID, fact.claimAt, fact.scope, fact.model, fact.kind, fact.fingerprint, fact.payload)
	if err != nil {
		return err
	}
	return readAccountProbeUsageFact(ctx, exec, fact)
}

// A late result is authorized by the original durable claim, never by current
// account policy or a fabricated customer. New claims and withdrawals therefore
// cannot discard consumption; they still fence every authorization/UI write.
func appendAccountProbeUsageOutcome(ctx context.Context, exec sqlExecutor, accountID int64, outcome service.ExcelBPS403ProbeResult) (accountProbeUsageFact, error) {
	fact, err := newAccountProbeUsageFact("outcome", outcome)
	if err != nil {
		return accountProbeUsageFact{}, err
	}
	if accountID != fact.accountID {
		return accountProbeUsageFact{}, errAccountProbeUsageConflict
	}
	// Recreate only the expected immutable identity for exact readback, not a
	// live Account or a new claim. The row must already exist before this call.
	claim := service.ExcelBPS403ProbeResult{Status: "probing", Stage: "claim", Reason: "pending",
		Usage: &service.ExcelBPS403ProbeUsage{Scope: fact.scope, ProbeID: fact.probeID, AccountID: fact.accountID,
			ClaimAt: fact.claimAt, Model: fact.model, SendState: "unknown", State: "unknown"}}
	initial, err := newAccountProbeUsageFact("claim", claim)
	if err != nil {
		return accountProbeUsageFact{}, err
	}
	if err := readAccountProbeUsageFact(ctx, exec, initial); err != nil {
		return accountProbeUsageFact{}, err
	}
	if err := insertAccountProbeUsageFact(ctx, exec, fact); err != nil {
		return accountProbeUsageFact{}, err
	}
	return fact, nil
}

// Only the existing UI snapshot is compared here; it is never the consumption
// archive. Used after a lost commit ACK to avoid claiming an unverified update.
func (r *accountRepository) excelBPSProbeSnapshotMatches(ctx context.Context, accountID int64, raw []byte) (bool, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL
 AND extra->'openai_excel_bps_403_last_probe_result'=$2::jsonb`, accountID, string(raw))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

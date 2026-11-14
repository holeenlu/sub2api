//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func probeUsageFactCount(t *testing.T, accountID int64) int {
	t.Helper()
	var n int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM account_probe_usage WHERE account_id=$1", accountID).Scan(&n))
	return n
}

func claimedProbeUsageFixture(t *testing.T) (*accountRepository, *service.Account, time.Time) {
	t.Helper()
	repo, a, now := newExcelBPSRecoveryFixture(t)
	claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
	require.NoError(t, err)
	require.True(t, claimed)
	snapshot, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	return repo, snapshot, now
}

func probeUsageFailure(a *service.Account) service.ExcelBPS403ProbeResult {
	outcome := recoveryProbeSuccess(a)
	outcome.Status, outcome.Stage, outcome.Reason = "failed", "response", "upstream_rejected"
	outcome.Usage.Terminal, outcome.Usage.State = "failed", "lower_bound"
	input, output, cached := int64(12), int64(3), int64(0)
	creation, short, long := int64(5), int64(2), int64(3)
	outcome.Usage.Tokens = &service.ExcelBPSProbeTokens{Input: &input, Output: &output, Cached: &cached,
		CacheCreation: &creation, CacheCreation30m: &short, CacheCreation12h: &long}
	return outcome
}

func requireProbeUsageFact(t *testing.T, repo *accountRepository, kind string, result service.ExcelBPS403ProbeResult) accountProbeUsageFact {
	t.Helper()
	fact, err := newAccountProbeUsageFact(kind, result)
	require.NoError(t, err)
	// Exercise the same exact read consumer used for writes and lost-ACK
	// reconciliation, including account/claim/model and nil-vs-zero counters.
	require.NoError(t, readAccountProbeUsageFact(t.Context(), repo.sql, fact))
	return fact
}

func TestAccountProbeUsageClaimAndUnknownAreOneTransaction(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	initial, err := newAccountProbeUsageFact("claim", service.ExcelBPS403ProbeClaimResult(a, now))
	require.NoError(t, err)
	tx, err := repo.client.Tx(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	claimed, err := repo.ClaimExcelBPS403Probe(dbent.NewTxContext(t.Context(), tx), a, now)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, readAccountProbeUsageFact(t.Context(), tx.Client(), initial))
	require.ErrorIs(t, readAccountProbeUsageFact(t.Context(), repo.sql, initial), errAccountProbeUsageMissing)
	require.NoError(t, tx.Rollback())
	require.Zero(t, probeUsageFactCount(t, a.ID))
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.NotContains(t, fresh.Extra, service.ExcelBPS403LastProbeAtKey)
	claimed, err = repo.ClaimExcelBPS403Probe(t.Context(), fresh, now)
	require.NoError(t, err)
	require.True(t, claimed)
	requireProbeUsageFact(t, repo, "claim", service.ExcelBPS403ProbeClaimResult(a, now))
	require.Equal(t, 1, probeUsageFactCount(t, a.ID))
}

func TestAccountProbeUsageLaterClaimPreservesEveryEarlierOutcome(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "incomplete", "interrupted", "canceled"} {
		t.Run(terminal, func(t *testing.T) {
			repo, original, now := claimedProbeUsageFixture(t)
			initial := service.ExcelBPS403ProbeClaimResult(original, now)
			claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), original, now.Add(time.Hour))
			require.NoError(t, err)
			require.True(t, claimed)
			current, err := repo.GetByID(t.Context(), original.ID)
			require.NoError(t, err)
			before, err := json.Marshal(current.Extra)
			require.NoError(t, err)
			outcome := probeUsageFailure(original)
			outcome.Usage.Terminal = terminal
			if terminal == "completed" {
				outcome.Status, outcome.Stage, outcome.Reason, outcome.Usage.State = "restored", "restore", "success", "measured"
			} else if terminal == "canceled" {
				outcome.Reason, outcome.Usage.Terminal, outcome.Usage.State = "canceled", "", "unknown"
			}
			for repeat := 0; repeat < 2; repeat++ {
				var changed bool
				if terminal == "completed" {
					changed, err = repo.RestoreExcelBPSAfter403(t.Context(), original, outcome)
				} else {
					changed, err = repo.RecordExcelBPS403ProbeResult(t.Context(), original, outcome)
				}
				require.NoError(t, err, "late consumption is durable even though current authorization no longer matches")
				require.False(t, changed, "old probe cannot change the new claim's UI or enable BPS")
			}
			require.Equal(t, 3, probeUsageFactCount(t, original.ID), "two claims and one idempotent outcome")
			requireProbeUsageFact(t, repo, "claim", initial)
			fact := requireProbeUsageFact(t, repo, "outcome", outcome)
			requireProbeUsageFact(t, repo, "claim", service.ExcelBPS403ProbeClaimResult(original, now.Add(time.Hour)))
			for _, absent := range []string{"api_key_id", "user_id", "billing_root", "balance_cost", "test-token", "reasoning_output_tokens"} {
				require.NotContains(t, string(fact.payload), absent)
			}
			require.Contains(t, string(fact.payload), `"cached_input_tokens":0`)
			fresh, err := repo.GetByID(t.Context(), original.ID)
			require.NoError(t, err)
			after, err := json.Marshal(fresh.Extra)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.False(t, fresh.IsExcelBPSEnabled())
		})
	}
}

func TestAccountProbeUsageDuplicatesAndCounterEvidenceAreImmutable(t *testing.T) {
	repo, snapshot, _ := claimedProbeUsageFixture(t)
	outcome := probeUsageFailure(snapshot)
	var updated atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed, err := repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
			if err != nil {
				t.Errorf("record probe outcome: %v", err)
				return
			}
			if changed {
				updated.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), updated.Load())
	require.Equal(t, 2, probeUsageFactCount(t, snapshot.ID))
	first := requireProbeUsageFact(t, repo, "outcome", outcome)
	conflicting := outcome
	usage := *outcome.Usage
	conflicting.Usage = &usage
	tokens := *outcome.Usage.Tokens
	conflicting.Usage.Tokens = &tokens
	otherCount := int64(13)
	conflicting.Usage.Tokens.Input = &otherCount
	changed, err := repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, conflicting)
	require.NoError(t, err)
	require.False(t, changed)
	second := requireProbeUsageFact(t, repo, "outcome", conflicting)
	require.NotEqual(t, first.fingerprint, second.fingerprint)
	requireProbeUsageFact(t, repo, "outcome", outcome)
	require.Equal(t, 3, probeUsageFactCount(t, snapshot.ID), "different observations are retained, never summed or overwritten")
	current, err := repo.GetByID(t.Context(), snapshot.ID)
	require.NoError(t, err)
	state := service.ExcelBPS403RecoveryStatusFromAccount(current, true, time.Now())
	require.Equal(t, int64(12), *state.LastResult.Usage.Tokens.Input)
}

func TestAccountProbeUsageRequiresOriginalPersistedIdentity(t *testing.T) {
	repo, snapshot, now := claimedProbeUsageFixture(t)
	for _, dimension := range []string{"account", "claim", "model", "probe"} {
		t.Run(dimension, func(t *testing.T) {
			outcome := probeUsageFailure(snapshot)
			switch dimension {
			case "account":
				outcome.Usage.AccountID++
			case "claim":
				outcome.Usage.ClaimAt = now.Add(time.Second).Format(time.RFC3339Nano)
			case "model":
				outcome.Usage.Model = "different-model"
			case "probe":
				outcome.Usage.ProbeID = service.ExcelBPS403ProbeClaimResult(snapshot, now.Add(time.Second)).Usage.ProbeID
			}
			changed, err := repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
			require.Error(t, err)
			require.False(t, changed)
			require.Equal(t, 1, probeUsageFactCount(t, snapshot.ID))
		})
	}
}

func TestAccountProbeUsageDoesNotTrustHashWithoutExactPayload(t *testing.T) {
	for _, phase := range []string{"claim", "outcome", "restore"} {
		t.Run(phase, func(t *testing.T) {
			repo, account, now := newExcelBPSRecoveryFixture(t)
			outcome := service.ExcelBPS403ProbeClaimResult(account, now)
			kind := "claim"
			if phase != "claim" {
				claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), account, now)
				require.NoError(t, err)
				require.True(t, claimed)
				account, err = repo.GetByID(t.Context(), account.ID)
				require.NoError(t, err)
				outcome, kind = probeUsageFailure(account), "outcome"
				if phase == "restore" {
					outcome = recoveryProbeSuccess(account)
				}
			}
			fact, err := newAccountProbeUsageFact(kind, outcome)
			require.NoError(t, err)
			// Simulate external corruption/collision: the key and claimed digest
			// match, but bytes differ. This is deliberately not a production writer.
			_, err = integrationDB.ExecContext(t.Context(), `INSERT INTO account_probe_usage
 (probe_id,account_id,claim_at,scope,model,observation_kind,payload_sha256,payload)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, fact.probeID, fact.accountID, fact.claimAt, fact.scope, fact.model, fact.kind, fact.fingerprint, []byte(`{"corrupt":true}`))
			require.NoError(t, err)
			var changed bool
			switch phase {
			case "claim":
				changed, err = repo.ClaimExcelBPS403Probe(t.Context(), account, now)
			case "outcome":
				changed, err = repo.RecordExcelBPS403ProbeResult(t.Context(), account, outcome)
			case "restore":
				changed, err = repo.RestoreExcelBPSAfter403(t.Context(), account, outcome)
			}
			require.ErrorIs(t, err, errAccountProbeUsageConflict)
			require.False(t, changed)
			fresh, err := repo.GetByID(t.Context(), account.ID)
			require.NoError(t, err)
			require.False(t, fresh.IsExcelBPSEnabled())
			if phase == "claim" {
				require.NotContains(t, fresh.Extra, service.ExcelBPS403LastProbeAtKey)
			} else {
				require.Equal(t, "probing", fresh.Extra[service.ExcelBPS403LastProbeResultKey].(map[string]any)["status"])
				requireProbeUsageFact(t, repo, "claim", service.ExcelBPS403ProbeClaimResult(account, now))
			}
		})
	}
}

func TestAccountProbeUsageWriteFailureLeavesDurableUnknownClaim(t *testing.T) {
	repo, snapshot, now := claimedProbeUsageFixture(t)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	outcome := probeUsageFailure(snapshot)
	changed, err := repo.RecordExcelBPS403ProbeResult(canceled, snapshot, outcome)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, changed)
	require.Equal(t, 1, probeUsageFactCount(t, snapshot.ID))
	requireProbeUsageFact(t, repo, "claim", service.ExcelBPS403ProbeClaimResult(snapshot, now))
	changed, err = repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, 2, probeUsageFactCount(t, snapshot.ID))
	requireProbeUsageFact(t, repo, "outcome", outcome)
}

func TestAccountProbeUsageDeletionCannotEraseAcceptedConsumption(t *testing.T) {
	repo, original, now := claimedProbeUsageFixture(t)
	_, err := integrationDB.ExecContext(t.Context(), "DELETE FROM scheduler_outbox WHERE account_id=$1", original.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(t.Context(), "DELETE FROM accounts WHERE id=$1", original.ID)
	require.NoError(t, err)
	outcome := probeUsageFailure(original)
	changed, err := repo.RecordExcelBPS403ProbeResult(t.Context(), original, outcome)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, 2, probeUsageFactCount(t, original.ID))
	requireProbeUsageFact(t, repo, "claim", service.ExcelBPS403ProbeClaimResult(original, now))
	requireProbeUsageFact(t, repo, "outcome", outcome)
}

var errProbeCommitACK = errors.New("probe transaction commit acknowledgement lost")

// Real PostgreSQL still executes every statement. Only the commit return value
// is replaced, with one branch committing and the other rolling back, so the
// production readback must distinguish the two rather than trust the error.
type probeCommitACKDriver struct {
	*entsql.Driver
	committed bool
}

func (d *probeCommitACKDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.Driver.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return &probeCommitACKTx{Tx: tx.(*entsql.Tx), committed: d.committed}, nil
}

type probeCommitACKTx struct {
	*entsql.Tx
	committed bool
}

func (tx *probeCommitACKTx) Commit() error {
	var err error
	if tx.committed {
		err = tx.Tx.Commit()
	} else {
		err = tx.Tx.Rollback()
	}
	if err != nil {
		return err
	}
	return errProbeCommitACK
}

func TestAccountProbeUsageCommitReadbackUsesDurableCounterproof(t *testing.T) {
	for _, phase := range []string{"claim", "outcome", "restore"} {
		for _, committed := range []bool{false, true} {
			name := phase + "/rollback"
			if committed {
				name = phase + "/committed"
			}
			t.Run(name, func(t *testing.T) {
				repo, account, now := newExcelBPSRecoveryFixture(t)
				before := 0
				outcome := service.ExcelBPS403ProbeClaimResult(account, now)
				kind := "claim"
				if phase != "claim" {
					claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), account, now)
					require.NoError(t, err)
					require.True(t, claimed)
					account, err = repo.GetByID(t.Context(), account.ID)
					require.NoError(t, err)
					outcome, kind, before = probeUsageFailure(account), "outcome", 1
					if phase == "restore" {
						outcome = recoveryProbeSuccess(account)
					}
				}
				// The wrapper borrows integrationDB; closing it would close the
				// shared fixture pool. It creates no independent connection pool.
				client := dbent.NewClient(dbent.Driver(&probeCommitACKDriver{
					Driver: entsql.OpenDB(dialect.Postgres, integrationDB), committed: committed,
				}))
				uncertain := newAccountRepositoryWithSQL(client, integrationDB, nil)
				var changed bool
				var err error
				switch phase {
				case "claim":
					changed, err = uncertain.ClaimExcelBPS403Probe(t.Context(), account, now)
				case "outcome":
					changed, err = uncertain.RecordExcelBPS403ProbeResult(t.Context(), account, outcome)
				case "restore":
					changed, err = uncertain.RestoreExcelBPSAfter403(t.Context(), account, outcome)
				}
				if committed {
					require.NoError(t, err)
					require.True(t, changed)
					requireProbeUsageFact(t, repo, kind, outcome)
					require.Equal(t, before+1, probeUsageFactCount(t, account.ID))
				} else {
					require.ErrorIs(t, err, errProbeCommitACK)
					require.False(t, changed)
					require.Equal(t, before, probeUsageFactCount(t, account.ID))
				}
				fresh, err := repo.GetByID(t.Context(), account.ID)
				require.NoError(t, err)
				require.Equal(t, committed && phase == "restore", fresh.IsExcelBPSEnabled())
			})
		}
	}
}

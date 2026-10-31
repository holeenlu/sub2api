package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
)

type excelBPSContinuationState string

// The empty state is invalid; only an explicit confirmed state grants publication.

const (
	excelBPSContinuationUnknown       excelBPSContinuationState = "unknown"
	excelBPSContinuationNotApplicable excelBPSContinuationState = "not_applicable"
	excelBPSContinuationCommitted     excelBPSContinuationState = "committed"
	excelBPSContinuationAborted       excelBPSContinuationState = "aborted"
)

// The result describes metadata confirmation, never a new inference outcome.
// Reasons are fixed local labels; no store error, key, nonce or history escapes.
type excelBPSContinuationOutcome struct {
	State    excelBPSContinuationState
	Action   string
	Reason   string
	Readback bool
	Writes   int
}

type excelBPSContinuationCompletion func(delivered bool) (excelBPSContinuationOutcome, error)

func excelBPSContinuationNoPublication(bool) (excelBPSContinuationOutcome, error) {
	return excelBPSContinuationOutcome{State: excelBPSContinuationNotApplicable}, nil
}

// Only the producer holding the nonce can finish its prepared marker. Ambiguous
// CAS acknowledgements are read back first, then retried at most once against
// the same owned pending marker. All operations share the original two seconds.
func newExcelBPSContinuationCompletion(ctx context.Context, store basispoints.StateStore, snapshot *excelBPSContinuationSnapshot, committed, aborted []byte, limit int64) excelBPSContinuationCompletion {
	var once sync.Once
	var outcome excelBPSContinuationOutcome
	var resultErr error
	return func(delivered bool) (excelBPSContinuationOutcome, error) {
		once.Do(func() {
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			target, next := excelBPSContinuationCommitted, committed
			outcome = excelBPSContinuationOutcome{State: excelBPSContinuationUnknown, Action: "commit"}
			if !delivered {
				target, next, outcome.Action = excelBPSContinuationAborted, aborted, "abort"
			}
			fail := func(reason string, cause error) {
				outcome.Reason = reason
				resultErr = fmt.Errorf("Excel BPS continuation %s %s: %w", outcome.Action, reason, cause)
			}
			for {
				raw, revision, err := store.LoadBPSState(finishCtx, snapshot.Scope, excelBPSContinuationKind(snapshot.ResponseID))
				if err != nil {
					fail("read_failure", basispoints.ErrStateUnavailable)
					return
				}
				if revision == 0 || len(raw) == 0 {
					fail("missing_marker", basispoints.ErrStateUnavailable)
					return
				}
				current, err := decodeExcelBPSContinuation(raw, snapshot.Scope, snapshot.ResponseID, limit, true)
				if err != nil {
					fail("invalid_marker", basispoints.ErrStateUnavailable)
					return
				}
				if current.Nonce != snapshot.Nonce || current.Digest != snapshot.Digest {
					fail("cas_conflict", errExcelBPSContinuationConflict)
					return
				}
				if current.State == string(target) {
					outcome.State, outcome.Readback = target, true
					return
				}
				if current.State != "pending" {
					fail("cas_conflict", errExcelBPSContinuationConflict)
					return
				}
				if outcome.Writes == 2 || finishCtx.Err() != nil {
					if outcome.Reason == "cas_conflict" {
						fail("cas_conflict", errExcelBPSContinuationConflict)
					} else {
						fail("write_unconfirmed", basispoints.ErrStateUnavailable)
					}
					return
				}
				outcome.Writes++
				changed, err := store.SaveBPSState(finishCtx, snapshot.Scope, excelBPSContinuationKind(snapshot.ResponseID), &revision, next)
				if err == nil && changed {
					outcome.State = target
					return
				}
				// A failed reply cannot tell us whether the write took effect. Do
				// not abort or overwrite another producer, nor extend the deadline.
				if err != nil {
					outcome.Reason = "write_unconfirmed"
				} else {
					outcome.Reason = "cas_conflict"
				}
			}
		})
		return outcome, resultErr
	}
}

// A delivered terminal remains successful even if its recovery metadata is
// unknown. Observe failures without generating another terminal or billing root.
func observeExcelBPSContinuationCompletion(c *gin.Context, account *Account, outcome excelBPSContinuationOutcome, err error) {
	if err == nil && outcome.Reason == "" {
		return
	}
	stage := "history_commit"
	if outcome.Action == "abort" {
		stage = "history_abort"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Kind: "continuation_completion", Scope: "excel_bps", Stage: stage,
		Reason: outcome.Reason, AccountID: account.ID,
		Message: "Excel BPS continuation metadata completion",
		Detail:  fmt.Sprintf("state=%s readback=%t writes=%d", outcome.State, outcome.Readback, outcome.Writes),
	})
}

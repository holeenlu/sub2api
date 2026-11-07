package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise acknowledgements separately from durable state; a failed reply may
// follow a successful CAS. No network, replacement source tree or fake permit.
type bpsCompletionFaultStore struct {
	*excelBPSContinuationTestStore
	mode      string
	writes    int
	payloads  [][]byte
	deadlines []time.Time
}

func (s *bpsCompletionFaultStore) LoadBPSState(ctx context.Context, scope, kind string) ([]byte, uint64, error) {
	if strings.HasPrefix(scope, "continuation:v1:") {
		deadline, _ := ctx.Deadline()
		s.deadlines = append(s.deadlines, deadline)
		if s.mode == "read_failure" || (s.mode == "readback_failure" || s.mode == "ack_lost_readback_failure") && s.writes > 0 {
			return nil, 0, errors.New("PRIVATE_READ_FAILURE")
		}
	}
	return s.excelBPSContinuationTestStore.LoadBPSState(ctx, scope, kind)
}

func (s *bpsCompletionFaultStore) SaveBPSState(ctx context.Context, scope, kind string, expected *uint64, raw []byte) (bool, error) {
	if !strings.HasPrefix(scope, "continuation:v1:") || expected == nil || *expected == 0 {
		return s.excelBPSContinuationTestStore.SaveBPSState(ctx, scope, kind, expected, raw)
	}
	s.writes++
	s.payloads = append(s.payloads, bytes.Clone(raw))
	deadline, _ := ctx.Deadline()
	s.deadlines = append(s.deadlines, deadline)
	privateErr := errors.New("PRIVATE_WRITE_FAILURE")
	switch s.mode {
	case "ack_lost", "false_ack", "ack_lost_readback_failure":
		changed, err := s.excelBPSContinuationTestStore.SaveBPSState(ctx, scope, kind, expected, raw)
		if !changed || err != nil {
			return changed, err
		}
		if s.mode == "false_ack" {
			return false, nil
		}
		return false, privateErr
	case "transient":
		if s.writes == 1 {
			return false, privateErr
		}
	case "permanent", "readback_failure":
		return false, privateErr
	case "cas_pending":
		return false, nil
	case "deadline":
		<-ctx.Done()
		return false, privateErr
	case "foreign_nonce", "foreign_digest", "foreign_committed", "opposite_terminal":
		s.mu.Lock()
		entry := s.entries[scope+"/"+kind]
		snapshot, err := decodeExcelBPSContinuation(entry.raw, scope, "resp_test", excelBPSContinuationMaxBytes)
		if err == nil {
			switch s.mode {
			case "foreign_nonce":
				snapshot.Nonce = "PRIVATE_FOREIGN_NONCE"
			case "foreign_digest":
				snapshot.Digest = "PRIVATE_FOREIGN_DIGEST"
			case "foreign_committed":
				snapshot, err = decodeExcelBPSContinuation(raw, scope, "resp_test", excelBPSContinuationMaxBytes, true)
				if err == nil {
					snapshot.Nonce = "PRIVATE_FOREIGN_NONCE"
				}
			case "opposite_terminal":
				snapshot.State = "aborted"
			}
		}
		if err == nil {
			entry.raw, err = encodeExcelBPSContinuation(snapshot, excelBPSContinuationMaxBytes)
		}
		if err == nil {
			entry.revision++
			s.entries[scope+"/"+kind] = entry
		}
		s.mu.Unlock()
		return false, err
	}
	return s.excelBPSContinuationTestStore.SaveBPSState(ctx, scope, kind, expected, raw)
}

func TestExcelBPSContinuationCompletionUncertainCAS(t *testing.T) {
	for _, delivered := range []bool{true, false} {
		for _, tc := range []struct {
			mode     string
			writes   int
			state    excelBPSContinuationState
			reason   string
			readback bool
		}{
			{"ack_lost", 1, excelBPSContinuationCommitted, "write_unconfirmed", true},
			{"false_ack", 1, excelBPSContinuationCommitted, "cas_conflict", true},
			{"transient", 2, excelBPSContinuationCommitted, "write_unconfirmed", false},
			{"permanent", 2, excelBPSContinuationUnknown, "write_unconfirmed", false},
			{"cas_pending", 2, excelBPSContinuationUnknown, "cas_conflict", false},
			{"read_failure", 0, excelBPSContinuationUnknown, "read_failure", false},
			{"readback_failure", 1, excelBPSContinuationUnknown, "read_failure", false},
			{"ack_lost_readback_failure", 1, excelBPSContinuationUnknown, "read_failure", false},
			{"foreign_nonce", 1, excelBPSContinuationUnknown, "cas_conflict", false},
			{"foreign_digest", 1, excelBPSContinuationUnknown, "cas_conflict", false},
			{"foreign_committed", 1, excelBPSContinuationUnknown, "cas_conflict", false},
		} {
			t.Run(fmt.Sprintf("%t/%s", delivered, tc.mode), func(t *testing.T) {
				svc, base := excelBPSContinuationTestService()
				store := &bpsCompletionFaultStore{excelBPSContinuationTestStore: base, mode: tc.mode}
				svc.cache = store
				c, account := excelBPSContinuationTestContext(), excelAccount()
				finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
				require.NoError(t, err)
				outcome, err := finish(delivered)
				want := tc.state
				if want == excelBPSContinuationCommitted && !delivered {
					want = excelBPSContinuationAborted
				}
				require.Equal(t, want, outcome.State)
				require.Equal(t, tc.reason, outcome.Reason)
				require.Equal(t, tc.readback, outcome.Readback)
				require.Equal(t, tc.writes, outcome.Writes)
				require.Equal(t, tc.writes, store.writes)
				if want == excelBPSContinuationUnknown {
					require.Error(t, err)
					if tc.reason == "cas_conflict" {
						require.ErrorIs(t, err, errExcelBPSContinuationConflict)
					} else {
						require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
					}
					require.NotContains(t, err.Error(), "PRIVATE_")
				} else {
					require.NoError(t, err)
				}
				for _, payload := range store.payloads {
					require.Equal(t, store.payloads[0], payload, "bounded retry only writes the same metadata")
				}
				for _, deadline := range store.deadlines {
					require.Equal(t, store.deadlines[0], deadline, "one deadline, including readback")
				}
				again, againErr := finish(!delivered)
				require.Equal(t, outcome, again)
				require.Equal(t, err, againErr)
				require.Equal(t, tc.writes, store.writes, "second completion cannot change publication")
				observeExcelBPSContinuationCompletion(c, account, outcome, err)
				events, ok := c.Get(OpsUpstreamErrorsKey)
				require.True(t, ok)
				event := events.([]*OpsUpstreamErrorEvent)[0]
				wantStage := "history_commit"
				if !delivered {
					wantStage = "history_abort"
				}
				require.Equal(t, wantStage, event.Stage)
				require.Equal(t, tc.reason, event.Reason)
				require.NotContains(t, fmt.Sprint(event), "PRIVATE_")
				require.Zero(t, event.UpstreamStatusCode)
				require.Empty(t, event.UpstreamRequestID)
				// Restore uses its normal decoder, which must never admit abort/pending.
				svc.cache = base
				if want == excelBPSContinuationCommitted {
					_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
					require.NoError(t, err)
				} else if want == excelBPSContinuationAborted {
					_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
					require.ErrorIs(t, err, basispoints.ErrPreviousResponseNotFound)
				} else if tc.mode == "ack_lost_readback_failure" {
					// Unknown describes this producer's knowledge. A later independent
					// read can prove a durable write whose acknowledgement was lost.
					// Never fabricate confirmation or try to undo that write blindly.
					_, restoreErr := svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
					if delivered {
						require.NoError(t, restoreErr)
					} else {
						require.ErrorIs(t, restoreErr, basispoints.ErrPreviousResponseNotFound)
					}
				} else if tc.mode != "foreign_committed" {
					scope, _ := excelBPSContinuationScope(c, account)
					raw, _, readErr := base.LoadBPSState(t.Context(), scope, excelBPSContinuationKind("resp_test"))
					require.NoError(t, readErr)
					state, decodeErr := decodeExcelBPSContinuation(raw, scope, "resp_test", excelBPSContinuationMaxBytes)
					require.NoError(t, decodeErr)
					require.Equal(t, "pending", state.State, "unconfirmed completion must not publish")
				}
			})
		}
	}
}

func TestExcelBPSContinuationCompletionDeadlineAndOnce(t *testing.T) {
	t.Run("two second deadline is not renewed", func(t *testing.T) {
		svc, base := excelBPSContinuationTestService()
		store := &bpsCompletionFaultStore{excelBPSContinuationTestStore: base, mode: "deadline"}
		svc.cache = store
		finish, err := svc.prepareExcelBPSContinuation(t.Context(), excelBPSContinuationTestContext(), excelAccount(), []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		started := time.Now()
		outcome, err := finish(true)
		require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
		require.Equal(t, excelBPSContinuationUnknown, outcome.State)
		require.Equal(t, 1, store.writes)
		require.GreaterOrEqual(t, time.Since(started), 1900*time.Millisecond)
		require.Less(t, time.Since(started), 3*time.Second)
		for _, deadline := range store.deadlines {
			require.Equal(t, store.deadlines[0], deadline)
		}
	})
	t.Run("concurrent contradictory completions share one decision", func(t *testing.T) {
		svc, base := excelBPSContinuationTestService()
		finish, err := svc.prepareExcelBPSContinuation(t.Context(), excelBPSContinuationTestContext(), excelAccount(), []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		outcomes := make([]excelBPSContinuationOutcome, 16)
		errs := make([]error, 16)
		var wg sync.WaitGroup
		for i := range outcomes {
			wg.Add(1)
			go func() { defer wg.Done(); outcomes[i], errs[i] = finish(i%2 == 0) }()
		}
		wg.Wait()
		for i := range outcomes {
			require.NoError(t, errs[i])
			require.Equal(t, outcomes[0], outcomes[i])
		}
		require.Contains(t, []excelBPSContinuationState{excelBPSContinuationCommitted, excelBPSContinuationAborted}, outcomes[0].State)
		require.Equal(t, 2, base.saves, "prepare plus one completion CAS")
	})
}

func TestExcelBPSContinuationCompletionConsumers(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, fault := range []string{"ack_lost", "permanent", "read_failure", "write_failure"} {
			t.Run(protocol+"/"+fault, func(t *testing.T) {
				first := bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":"+excelBPSContinuationTestResponse+"}\n\n")
				first.Header.Set("X-Request-Id", "req_original")
				upstream := &httpUpstreamRecorder{resp: first}
				svc := excelBPSGatewayTestService(upstream)
				svc.cfg.Gateway.ExcelBPSStateRedisEnabled = true
				base := &excelBPSContinuationTestStore{GatewayCache: &stubGatewayCache{}, entries: map[string]excelBPSContinuationTestEntry{}}
				mode := fault
				if fault == "write_failure" {
					mode = "permanent"
				}
				store := &bpsCompletionFaultStore{excelBPSContinuationTestStore: base, mode: mode}
				svc.cache = store
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Set("api_key", getAPIKeyFromContext(excelBPSContinuationTestContext()))
				downstreamErr := errors.New("PRIVATE_DOWNSTREAM_WRITE")
				if protocol != "ws" && fault == "write_failure" {
					c.Writer = &bpsCompletionWriteFailure{ResponseWriter: c.Writer, err: downstreamErr}
				}
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"start","stream":%t}`, protocol != "json"))
				var result *OpenAIForwardResult
				var err error
				var output strings.Builder
				if protocol == "ws" {
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(event []byte) error {
						if fault == "write_failure" {
							return downstreamErr
						}
						output.Write(event)
						return nil
					})
				} else {
					result, err = svc.Forward(t.Context(), c, excelAccount(), body)
					output.Write(rec.Body.Bytes())
				}
				if fault == "write_failure" {
					require.ErrorIs(t, err, downstreamErr)
				} else {
					require.NoError(t, err, "metadata uncertainty cannot change a delivered success")
					require.NotNil(t, result)
					require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
					require.Equal(t, "resp_test", result.ResponseID)
					wantBilling := "req_original"
					if protocol == "ws" {
						wantBilling = "resp_test"
					}
					require.Equal(t, wantBilling, result.RequestID)
					if protocol == "json" {
						require.Equal(t, "completed", gjson.Get(output.String(), "status").String())
					} else {
						require.Equal(t, 1, strings.Count(output.String(), `"type":"response.completed"`))
					}
					require.NotContains(t, output.String(), "response.failed")
					require.False(t, HasOpsCurrentStreamError(c), "metadata observation must not replace successful terminal Ops")
				}
				require.Len(t, upstream.requests, 1, "no inference retry for completion failure")
				events, ok := c.Get(OpsUpstreamErrorsKey)
				require.True(t, ok)
				found := false
				for _, event := range events.([]*OpsUpstreamErrorEvent) {
					if event.Kind != "continuation_completion" {
						continue
					}
					found = true
					wantStage := "history_commit"
					if fault == "write_failure" {
						wantStage = "history_abort"
					}
					require.Equal(t, wantStage, event.Stage)
					require.NotContains(t, fmt.Sprint(event), "PRIVATE_")
					require.Empty(t, event.UpstreamRequestID)
					require.Zero(t, event.UpstreamStatusCode)
				}
				require.True(t, found, "every real consumer must observe completion")
			})
		}
	}
}

type bpsCompletionWriteFailure struct {
	gin.ResponseWriter
	err error
}

func (w *bpsCompletionWriteFailure) Write([]byte) (int, error)       { return 0, w.err }
func (w *bpsCompletionWriteFailure) WriteString(string) (int, error) { return 0, w.err }

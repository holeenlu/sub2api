//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTurnRecoveryActualTargetsShareBudget(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil, "high")
	parent, vendor := int64(7), int64(3)
	a := &Account{ID: 10, ParentAccountID: &parent, VendorID: &vendor}
	shadow := &Account{ID: 11, ParentAccountID: &parent, VendorID: &vendor}
	// A shadow is the same physical credential; another mode is a distinct
	// target but cannot reset the owner's aggregate generation allowance.
	first := withTurnRecoveryTarget(ctx, a, "http", "/responses", "proxy-a", "grant-v1")
	second := withTurnRecoveryTarget(ctx, shadow, "http", "/responses", "proxy-a", "grant-v1")
	require.NoError(t, TakeOpenAITurnAttempt(first))
	require.NoError(t, TakeOpenAITurnAttempt(second))
	require.ErrorIs(t, TakeOpenAITurnAttempt(second), ErrTurnTargetExhausted)
	mode := withTurnRecoveryTarget(ctx, a, "websocket", "/responses", "proxy-a", "grant-v1")
	require.NoError(t, TakeOpenAITurnAttempt(mode))
	require.ErrorIs(t, TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, shadow, "http", "/responses", "proxy-b", "grant-v2")), ErrTurnTargetExhausted)
	for id := int64(20); id < 25; id++ {
		require.NoError(t, TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, &Account{ID: id}, "http", "/responses", "", "grant")))
	}
	require.ErrorIs(t, TakeOpenAITurnAttempt(ctx), ErrOpenAITurnAttemptBudgetExhausted)
	require.False(t, OpenAITurnRetryAllowed(ctx, 25, nil))
}

func TestTurnRecoveryPreparationAndSelectionAreIndependent(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	require.NoError(t, TakeTurnPreparationRetry(ctx))
	require.NoError(t, TakeTurnPreparationRetry(ctx))
	require.ErrorIs(t, TakeTurnPreparationRetry(ctx), ErrTurnPreparationExhausted)
	for range 8 {
		require.NoError(t, TakeOpenAITurnAttempt(ctx))
	}
	for range 4 {
		selectionCtx, _, cancel := selectionbudget.Start(ctx)
		for range selectionbudget.Limit {
			require.NoError(t, selectionbudget.Take(selectionCtx, "redis", 32))
		}
		cancel()
	}
	selectionCtx, _, cancel := selectionbudget.Start(ctx)
	defer cancel()
	require.ErrorIs(t, selectionbudget.Take(selectionCtx, "sql", 0), selectionbudget.ErrExhausted)
	require.NoError(t, selectionbudget.Take(selectionbudget.CleanupContext(selectionCtx), "redis", 1), "release is never blocked by an active-work budget")
}

func TestTurnRecoveryContinuationRejectionIsNotDependency(t *testing.T) {
	for _, dependencyFailed := range []bool{false, true} {
		t.Run(fmt.Sprintf("dependency_failed=%t", dependencyFailed), func(t *testing.T) {
			ctx := EnsureOpenAITurnBudget(t.Context(), nil)
			selectionCtx, budget, cancel := selectionbudget.Start(ctx)
			defer cancel()
			dependencyErr := errors.New("response owner storage unavailable")
			if dependencyFailed {
				selectionbudget.RecordError(selectionCtx, dependencyErr)
			}
			err := ErrOpenAIContinuationUnavailable
			finishSchedulingBudget(selectionCtx, budget, &err)
			if dependencyFailed {
				require.ErrorIs(t, err, ErrSelectionDependency)
				require.ErrorIs(t, err, dependencyErr)
				return
			}
			require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
			require.NotErrorIs(t, err, ErrSelectionDependency)
			retry, finalErr := retrySelectionDependency(ctx, err)
			require.False(t, retry)
			require.ErrorIs(t, finalErr, ErrOpenAIContinuationUnavailable)
			require.NoError(t, TakeTurnPreparationRetry(ctx))
			require.NoError(t, TakeTurnPreparationRetry(ctx))
		})
	}
}

func TestTurnRecoveryPinnedOwnerCannotEscape(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	a, b := Account{ID: 1}, Account{ID: 2}
	PinTurnRetryAccount(ctx, &a)
	require.Empty(t, filterTurnRecoveryAccounts(ctx, []Account{b}), "an unavailable owner must not become a soft sticky preference")
	require.Equal(t, []Account{a}, filterTurnRecoveryAccounts(ctx, []Account{b, a}))
	PinTurnRetryAccount(ctx, nil)
	require.Len(t, filterTurnRecoveryAccounts(ctx, []Account{b, a}), 2)
	owner := int64(99)
	a.ParentAccountID = &owner
	PinTurnRetryAccount(ctx, &a)
	replaced := a
	replaced.ParentAccountID = nil
	require.False(t, turnAccountEligible(ctx, &replaced), "same scheduling ID cannot silently change grant owner")
}

func TestTurnRecoveryGrantVersionAndHistory(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	a := &Account{ID: 1}
	require.NoError(t, takeTurnGrantRefresh(ctx, a, "excel", "v1"))
	require.ErrorIs(t, takeTurnGrantRefresh(ctx, a, "excel", "v1"), ErrTurnPreparationExhausted)
	require.NoError(t, takeTurnGrantRefresh(ctx, a, "native", "v1"))
	require.ErrorIs(t, takeTurnGrantRefresh(ctx, a, "excel", "v2"), ErrTurnPreparationExhausted)
	for _, body := range []string{`{"cachedContent":"private"}`, `{"contents":[{"parts":[{"fileData":{"fileUri":"gs://private/file"}}]}]}`, `{"messages":[{"content":[{"source":{"file_id":"file_private"}}]}]}`, `{"messages":[{"content":[{"signature":"opaque"}]}]}`} {
		require.False(t, NativeAccountReplayAllowed([]byte(body)))
	}
	require.True(t, NativeAccountReplayAllowed([]byte(`{"messages":[{"content":"ordinary inline text"}]}`)))
	for _, body := range []string{
		`{"tools":[{"type":"web_search_20250305"}]}`,
		`{"tools":[{"type":"unknown_hosted_tool"}]}`,
		`{"mcp_servers":[{"type":"url","url":"https://example.com/mcp"}]}`,
		`{"tools":[{"functionDeclarations":[],"googleSearch":{}}]}`,
		`{"tools":[{"urlContext":{}}]}`,
	} {
		require.False(t, NativeUncertainReplaySafe([]byte(body)), body)
	}
	for _, body := range []string{
		`{"messages":[{"content":"inline"}]}`,
		`{"tools":[{"name":"local","input_schema":{"type":"object"}}]}`,
		`{"tools":[{"type":"computer_20250124","name":"computer"}]}`,
		`{"tools":[{"functionDeclarations":[{"name":"local"}]}]}`,
	} {
		require.True(t, NativeUncertainReplaySafe([]byte(body)), body)
	}
}

func TestTurnRecoveryWSFailureThenNextCreateKeepsConnection(t *testing.T) {
	serverResult := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverResult <- err
			return
		}
		owner := NewOpenAIWSDownstream(conn, 0)
		defer func() { _ = owner.CloseNow() }()
		_, first, err := owner.Read(r.Context())
		if err != nil {
			serverResult <- err
			return
		}
		ctx := owner.TurnBudgetContext(r.Context(), nil, first)
		for range 8 {
			_ = TakeOpenAITurnAttempt(ctx)
		}
		owner.nativeHistory = openAIWSRecoveryHistory{accountID: 1, responseID: "previous_success", complete: true}
		next, err := owner.FinishTurnAndReadNext(ctx, nil, "retry_budget_exhausted", "turn failed")
		if err == nil && !strings.Contains(string(next), "second") {
			err = errors.New("received wrong next create")
		}
		if err == nil {
			err = TakeOpenAITurnAttempt(owner.TurnBudgetContext(r.Context(), nil, next))
		}
		if err == nil && owner.nativeHistory.responseID != "previous_success" {
			err = errors.New("successful history discarded")
		}
		if err == nil {
			err = owner.Write(r.Context(), coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"second_success"}}`))
		}
		serverResult <- err
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","input":"first"}`)))
	_, failure, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(failure), "retry_budget_exhausted")
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","input":"second"}`)))
	_, completed, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(completed), "second_success")
	require.NoError(t, <-serverResult)
}

// Drive the production selector with instrumented Redis/SQL doubles and fake
// generation results. These are local scheduling latencies, not supplier TTFT.
func TestTurnRecoveryLargePoolFaultReplay(t *testing.T) {
	for _, size := range []int{100, 1000, 5000} {
		for _, failed := range []int{0, 10, 50, 100} {
			t.Run(fmt.Sprintf("pool_%d_failure_%d", size, failed), func(t *testing.T) {
				accounts := make([]Account, size)
				snapshot := &openAISnapshotCacheStub{accountsByID: make(map[int64]*Account, size)}
				for i := range accounts {
					vendor, proxy := int64(i%5+1), int64(i%3+1)
					accounts[i] = Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10, VendorID: &vendor, ProxyID: &proxy}
					snapshot.snapshotAccounts = append(snapshot.snapshotAccounts, &accounts[i])
					snapshot.accountsByID[int64(i+1)] = &accounts[i]
				}
				cache := &replayConcurrencyCache{}
				repo := replayAccountRepo{finalBudgetAccountRepo: finalBudgetAccountRepo{schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}}}
				svc := &OpenAIGatewayService{accountRepo: repo, schedulerSnapshot: &SchedulerSnapshotService{cache: replaySnapshotCache{snapshot}, accountRepo: repo}, concurrencyService: NewConcurrencyService(cache)}
				svc.concurrencyService.SetAccountLoadBatchCacheTTL(0)
				scheduler := &defaultOpenAIAccountScheduler{service: svc, stats: newOpenAIAccountRuntimeStats()}
				const rounds = 30
				latencies := make([]time.Duration, 0, rounds)
				successes, sends, operations, commands, maxOps := 0, 0, 0, 0, 0
				for round := range rounds {
					ctx := EnsureOpenAITurnBudget(t.Context(), nil)
					excluded := make(map[int64]struct{})
					start := time.Now()
					roundSends, roundOps := 0, 0
					for roundSends < 8 {
						selectionCtx, budget, cancel := selectionbudget.Start(ctx)
						result, _, err := scheduler.Select(selectionCtx, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: fmt.Sprintf("replay-%d", round), ExcludedIDs: excluded})
						if result != nil && result.ReleaseFunc != nil {
							result.ReleaseFunc()
						}
						state := budget.Snapshot()
						cancel()
						roundOps += state.Operations
						commands += state.Commands
						require.LessOrEqual(t, state.Operations, 64)
						if err != nil {
							require.True(t, errors.Is(err, selectionbudget.ErrExhausted) || errors.Is(err, ErrNoAvailableAccounts), "%v", err)
							break
						}
						require.NotNil(t, result)
						account := result.Account
						require.NoError(t, TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, account, "http", "/responses", "proxy", "grant")))
						roundSends++
						if int(account.ID)%100 >= failed {
							successes++
							break
						}
						excluded[account.ID] = struct{}{}
						ExcludeTurnRecoveryAccount(ctx, account, &UpstreamFailoverError{StatusCode: 503})
					}
					require.LessOrEqual(t, roundOps, 256)
					if failed == 0 {
						require.Equal(t, 1, roundSends)
					}
					sends += roundSends
					operations += roundOps
					if roundOps > maxOps {
						maxOps = roundOps
					}
					latencies = append(latencies, time.Since(start))
				}
				sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
				t.Logf("pool=%d failure_pct=%d success=%d/%d p95=%s p99=%s sends/turn=%.2f operations/turn=%.2f max_operations=%d redis_commands/turn=%.2f", size, failed, successes, rounds, latencies[rounds*95/100], latencies[rounds-1], float64(sends)/rounds, float64(operations)/rounds, maxOps, float64(commands)/rounds)
			})
		}
	}
}

func TestTurnRecoveryOwnerExclusionAndDomainOrder(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	parent, vendor, proxy := int64(10), int64(1), int64(2)
	a := &Account{ID: 11, ParentAccountID: &parent, VendorID: &vendor, ProxyID: &proxy}
	shadow := *a
	shadow.ID = 12
	ExcludeTurnRecoveryAccount(ctx, a, &UpstreamFailoverError{StatusCode: 401, Reason: "basispoints_credentials_rejected"})
	require.False(t, turnAccountEligible(ctx, &shadow, "bps"))
	require.True(t, turnAccountEligible(ctx, &shadow, "native"))
	require.Zero(t, turnRecoveryDomainPenalty(ctx, a))
	other := *a
	other.ID, other.ParentAccountID = 13, nil
	ExcludeTurnRecoveryAccount(ctx, &other, &UpstreamFailoverError{StatusCode: 503})
	require.Equal(t, 1, turnRecoveryDomainPenalty(ctx, a))
	lowCost := openAIAccountCandidateScore{account: a, costBand: 0, domainPenalty: 1}
	highCost := openAIAccountCandidateScore{account: &other, costBand: 1}
	require.True(t, isOpenAIAccountCandidateBetter(lowCost, highCost), "failure-domain preference must not override business layers")
	highCost.costBand = 0
	require.True(t, isOpenAIAccountCandidateBetter(highCost, lowCost))
}

func TestTurnRecoveryHeaderDeadlineAndCommittedStream(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			ctx := EnsureOpenAITurnBudget(t.Context(), nil)
			b, ok := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
			require.True(t, ok)
			b.deadline = time.Now().Add(20 * time.Millisecond)
			req, cleanup := guardTurnFirstOutput(httptest.NewRequest(http.MethodPost, "/messages", nil).WithContext(ctx))
			defer cleanup()
			if commit {
				MarkTurnOutputCommitted(ctx)
			}
			select {
			case <-req.Context().Done():
				require.False(t, commit)
				require.ErrorIs(t, context.Cause(req.Context()), ErrOpenAIWSFirstOutputBudgetExhausted)
			case <-time.After(50 * time.Millisecond):
				require.True(t, commit)
			}
		})
	}
}

func TestTurnRecoveryRejectedGrantUsesCurrentVersion(t *testing.T) {
	t.Run("concurrent forced refreshes share one rotation", func(t *testing.T) {
		account := &Account{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "old", "refresh_token": "rt"}}
		repo := &refreshAPIAccountRepo{account: account}
		executor := &refreshAPIExecutorStub{credentials: map[string]any{"access_token": "fresh", "refresh_token": "rt"}}
		api := NewOAuthRefreshAPI(repo, nil)
		start := make(chan struct{})
		results := make(chan error, 16)
		var workers sync.WaitGroup
		for range 16 {
			workers.Go(func() {
				<-start
				_, err := api.RefreshRejected(t.Context(), account, executor, "old")
				results <- err
			})
		}
		close(start)
		workers.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		require.Equal(t, 1, executor.refreshCalls)
		require.Equal(t, "fresh", repo.account.GetCredential("access_token"))
	})

	account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "old", "refresh_token": "rt"}}
	repo := &refreshAPIAccountRepo{account: account}
	executor := &refreshAPIExecutorStub{credentials: map[string]any{"access_token": "fresh", "refresh_token": "rt"}}
	api := NewOAuthRefreshAPI(repo, nil)
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	token, ok := refreshRejectedCredential(ctx, repo, account, "old", api, executor)
	require.True(t, ok)
	require.Equal(t, "fresh", token)
	require.Equal(t, 1, executor.refreshCalls)
	// A concurrent request with the rejected old version reuses the fresh grant.
	_, err := api.RefreshRejected(t.Context(), account, executor, "old")
	require.NoError(t, err)
	require.Equal(t, 1, executor.refreshCalls)
	_, ok = refreshRejectedCredential(ctx, repo, account, "old", api, executor)
	require.False(t, ok)
	require.Equal(t, 1, executor.refreshCalls)
	account.Status = StatusDisabled
	_, ok = refreshRejectedCredential(EnsureOpenAITurnBudget(t.Context(), nil), repo, account, "old", api, executor)
	require.False(t, ok, "a fresh token on a disabled owner cannot authorize another send")
}

func TestTurnRecoveryPreparationStorageSharesAggregate(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	for range selectionbudget.TurnLimit - 1 {
		require.NoError(t, selectionbudget.Take(selectionbudget.PreparationContext(ctx), "sql", 0))
	}
	selection, _, cancel := selectionbudget.Start(ctx)
	defer cancel()
	require.NoError(t, selectionbudget.Take(selection, "redis", 1))
	require.ErrorIs(t, selectionbudget.Take(selectionbudget.PreparationContext(ctx), "redis", 1), selectionbudget.ErrExhausted)
	require.NoError(t, selectionbudget.Take(selectionbudget.CleanupContext(selectionbudget.PreparationContext(ctx)), "redis", 1))
}

func TestTurnRecoveryRepairDoesNotResetOwnerLimit(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	a := &Account{ID: 1}
	for i := range 3 {
		require.NoError(t, TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, a, "http", "/responses", "", "v1", []byte(fmt.Sprint(i)))))
	}
	require.ErrorIs(t, TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, a, "http", "/responses", "", "v2", []byte("new repair"))), ErrTurnTargetExhausted)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, newExcelBPSCredentialFailure(errors.New("storage unavailable")), &failure)
	ExcludeTurnRecoveryAccount(ctx, &Account{ID: 2}, failure)
	require.False(t, turnAccountEligible(ctx, &Account{ID: 2}, "bps"))
	require.True(t, turnAccountEligible(ctx, &Account{ID: 2}, "native"))
}

func TestTurnRecoveryNativeReservationAndDependencyRecovery(t *testing.T) {
	for _, native := range []bool{false, true} {
		ctx := EnsureOpenAITurnBudget(t.Context(), nil)
		b, ok := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
		require.True(t, ok)
		b.attempts = 6
		b.bpsRecovery.nativeAvailable = native
		b.bpsRecovery.rejected = map[int64]bool{1: true}
		excluded := map[int64]struct{}{1: {}}
		calls := 0
		svc := &OpenAIGatewayService{}
		svc.openaiScheduler = excelBPSBudgetScheduler{selectAccount: func(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
			calls++
			require.Equal(t, native, excelBPSNativeRecoveryActive(ctx))
			if calls == 1 {
				selectionbudget.RecordError(ctx, errors.New("temporary redis failure"))
				return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
			}
			return &AccountSelectionResult{Account: &Account{ID: 2}}, OpenAIAccountScheduleDecision{}, nil
		}}
		result, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, true)
		require.NoError(t, err)
		require.Equal(t, int64(2), result.Account.ID)
		require.Equal(t, 2, calls)
		require.Equal(t, 6, b.attempts, "dependency recovery must not spend generation attempts")
		require.Equal(t, 1, b.preparationRetries)
		if native {
			require.NotContains(t, excluded, int64(1))
		} else {
			require.Contains(t, excluded, int64(1))
		}
	}
}

func TestTurnRecoveryWSTerminalErrorNotRepeated(t *testing.T) {
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			result <- err
			return
		}
		owner := NewOpenAIWSDownstream(conn, 0)
		defer func() { _ = owner.CloseNow() }()
		_, first, err := owner.Read(r.Context())
		if err != nil {
			result <- err
			return
		}
		ctx := owner.TurnBudgetContext(r.Context(), nil, first)
		err = owner.writeControlError(ctx, []byte(`{"event_id":"control"}`), "response_not_active", "no active response")
		if err == nil && owner.terminalTurn.Load() == owner.turn.Load() {
			err = errors.New("control error ended the generation")
		}
		if err == nil {
			err = owner.Write(ctx, coderws.MessageText, []byte(`{"type":"error","error":{"code":"upstream_error"}}`))
		}
		if err == nil {
			_, err = owner.FinishTurnAndReadNext(ctx, nil, "duplicate", "must not be sent")
		}
		if err == nil {
			err = owner.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"next"}}`))
		}
		result <- err
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","input":"first"}`)))
	_, control, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(control), "response_not_active")
	_, failed, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(failed), "upstream_error")
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","input":"next"}`)))
	_, complete, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(complete), "response.completed")
	require.NoError(t, <-result)
}

func TestTurnRecoveryCandidateRemovalKeepsRemainingOrder(t *testing.T) {
	scheduler := &defaultOpenAIAccountScheduler{}
	req := OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: "stable-recovery"}
	candidates := buildOpenAISchedulerBenchmarkCandidates(100)
	before := scheduler.buildOpenAISelectionOrder(req, openAIAccountLoadPlan{candidates: candidates})
	failed := before[0].account.ID
	remaining := make([]openAIAccountCandidateScore, 0, len(candidates)-1)
	for i := len(candidates) - 1; i >= 0; i-- {
		if candidates[i].account.ID != failed {
			remaining = append(remaining, candidates[i])
		}
	}
	after := scheduler.buildOpenAISelectionOrder(req, openAIAccountLoadPlan{candidates: remaining})
	require.Len(t, after, len(before)-1)
	for i := range after {
		require.Equal(t, before[i+1].account.ID, after[i].account.ID)
	}
}

// Exercise the provider's own retry loop, before handler replay checks can run.
func TestTurnRecoveryNativeHostedToolDoesNotRepeatUnknownExecution(t *testing.T) {
	for _, provider := range []string{"claude", "gemini", "bedrock", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
				StatusCode: http.StatusServiceUnavailable, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporarily unavailable"}}`)),
			}}}
			ctx := EnsureOpenAITurnBudget(t.Context(), nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)
			gateway := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}}
			switch provider {
			case "claude":
				_, _ = gateway.forwardAnthropicAPIKeyPassthrough(ctx, c, newAnthropicAPIKeyAccountForTest(), body, "claude-sonnet-4-5", "claude-sonnet-4-5", false, time.Now())
			case "gemini":
				gemini := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: upstream}
				_, _ = gemini.ForwardNative(ctx, c, geminiPoolModeAPIKeyAccount(), "gemini-2.5-flash", "generateContent", false, []byte(`{"contents":[{"parts":[{"text":"hello"}]}],"tools":[{"googleSearch":{}}]}`))
			case "bedrock":
				account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{"auth_mode": "apikey"}}
				resp, err := gateway.executeBedrockUpstream(ctx, c, account, body, "anthropic.claude-sonnet-4-5", "us-east-1", false, nil, "test-key", "")
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
			case "antigravity":
				ag := &AntigravityGatewayService{}
				result, err := ag.antigravityRetryLoop(antigravityRetryLoopParams{
					ctx: ctx, c: c, account: &Account{ID: 1, Platform: PlatformAntigravity},
					httpUpstream: upstream, accessToken: "test-token", action: "generateContent", requestedModel: "gemini-2.5-flash",
					body: []byte(`{"request":{"contents":[{"parts":[{"text":"hello"}]}],"tools":[{"googleSearch":{}}]}}`),
				})
				require.NoError(t, err)
				require.NoError(t, result.resp.Body.Close())
			}
			require.Equal(t, 1, upstream.callCount, "unknown hosted execution must not be replayed inside the adapter")
		})
	}
}

func TestOpenAIWSTransportRecoveryBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, body, reason, exhausted                                string
		oauth, committed, canceled, http, retryDisabled, noResponses bool
	}{
		{name: "capability_before_send", body: `{"input":"hi"}`, reason: "ws_unsupported", http: true},
		{name: "payload_size_before_send", body: `{"input":"hi"}`, reason: openAIWSPayloadPrefersHTTP, http: true},
		{name: "optional_preparation_timeout", body: `{"input":"hi"}`, reason: openAIWSOptionalPreparationTimedOut, http: true},
		{name: "pool_retry_zero", body: `{"input":"hi"}`, reason: openAIWSPayloadPrefersHTTP, retryDisabled: true},
		{name: "no_responses_endpoint", body: `{"input":"hi"}`, reason: openAIWSPayloadPrefersHTTP, noResponses: true},
		{name: "api_key_continuation", body: `{"previous_response_id":"prev"}`, reason: "upgrade_required", http: true},
		{name: "api_key_exact_continuation", body: `{"previous_response_id":"prev","store":false}`, reason: "upgrade_required"},
		{name: "oauth_continuation", body: `{"previous_response_id":"prev"}`, reason: "upgrade_required", oauth: true},
		{name: "explicit_prewarm", body: `{"generate":false}`, reason: "acquire_timeout"},
		{name: "agent_task", body: `{"agent_task_id":"task"}`, reason: "acquire_timeout"},
		{name: "conversation", body: `{"conversation":"conv"}`, reason: "dial_failed"},
		{name: "uncertain_write_plain_input", body: `{"input":"hi"}`, reason: openAIWSFailureWriteRequest, exhausted: "websocket_transport_exhausted"},
		{name: "uncertain_read_plain_input", body: `{"input":"hi"}`, reason: "read_event", exhausted: "websocket_transport_exhausted"},
		{name: "uncertain_write_read_only_search", body: `{"input":"hi","tools":[{"type":"web_search"}]}`, reason: openAIWSFailureWriteRequest, exhausted: "websocket_transport_exhausted"},
		{name: "uncertain_write_hosted_tool", body: `{"input":"hi","tools":[{"type":"image_generation"}]}`, reason: openAIWSFailureWriteRequest},
		{name: "uncertain_write_committed", body: `{"input":"hi"}`, reason: openAIWSFailureWriteRequest, committed: true},
		{name: "uncertain_write_canceled", body: `{"input":"hi"}`, reason: openAIWSFailureWriteRequest, canceled: true},
		{name: "uncertain_hosted_tool", body: `{"tools":[{"type":"image_generation"}]}`, reason: "read_event"},
		{name: "output_committed", body: `{}`, reason: "dial_failed", committed: true},
		{name: "canceled", body: `{}`, reason: "dial_failed", canceled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(EnsureOpenAITurnBudget(t.Context(), nil))
			defer cancel()
			if tt.committed {
				MarkTurnOutputCommitted(ctx)
			}
			if tt.canceled {
				cancel()
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			if tt.oauth {
				account.Type = AccountTypeOAuth
			}
			if tt.retryDisabled {
				account.Credentials = map[string]any{"pool_mode": true, "pool_mode_retry_count": 0}
			}
			if tt.noResponses {
				account.Extra = map[string]any{"openai_responses_supported": false}
			}
			original := wrapOpenAIWSFallback(tt.reason, errors.New("original upstream failure"))
			useHTTP, err := planOpenAIWSTransportRecovery(ctx, c, account, []byte(tt.body), original)
			require.Equal(t, tt.http, useHTTP)
			if tt.exhausted != "" {
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, GatewayFailureReason(tt.exhausted), failure.Reason)
				require.False(t, failure.RequestNotSent, "a failed write may already have reached the upstream")
			} else if tt.reason == openAIWSFailureWriteRequest {
				require.Same(t, original, err, "unsafe replay must keep the original failure")
			}

			if !tt.http {
				require.Error(t, err)
				require.ErrorIs(t, err, original)
			}
		})
	}
	for _, original := range []error{ErrOpenAITurnAttemptBudgetExhausted, &UpstreamFailoverError{StatusCode: 429, Reason: "upstream_capacity"}, wrapOpenAIWSFallback("dial_failed", ErrOpenAITurnAttemptBudgetExhausted), wrapOpenAIWSFallback("dial_failed", &UpstreamFailoverError{StatusCode: 502, NextAccountAction: NextAccountStop})} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		useHTTP, err := planOpenAIWSTransportRecovery(t.Context(), c, &Account{}, []byte(`{}`), original)
		require.False(t, useHTTP)
		require.Same(t, original, err)
	}
}

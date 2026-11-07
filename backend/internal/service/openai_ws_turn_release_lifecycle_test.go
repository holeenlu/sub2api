package service

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// Exercise the actual completion -> completeOpenAIWSTurn -> hooks.ReleaseTurn
// path with the handler's mutable-release closure shape. This is an owner-port
// regression, not a claim to execute the handler file or inject a reachable
// duplicate callback. Real ingress sequencing is covered by the consumer tests.
func TestNativeWSLateCompletionCannotReleaseReacquiredTurn(t *testing.T) {
	for _, scenario := range []string{"next_turn", "same_turn_reacquire"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeOperationFixture(t, false, 3)
			f.resource.Concurrency = 2
			f.key.User.Concurrency = 2
			store := &nativeWSCapacityStore{}
			bind := func(id string) context.Context {
				request := f.op.request
				request.Transport, request.OperationID = "ws", id
				ctx, err := f.service.BindNativeWSDispatch(EnsureOpenAITurnBudget(t.Context(), f.service.cfg), &APIKeyService{apiKeyRepo: f.keys}, request)
				require.NoError(t, err)
				ctx, err = f.service.BindNativeWSOperation(ctx, []byte("original turn"))
				require.NoError(t, err)
				fixture := *f
				fixture.ctx, fixture.op = ctx, nativeDispatchFromContext(ctx)
				bindNativeRuntimeFixture(t, &fixture)
				return fixture.ctx
			}
			acquire := func(ctx context.Context, token string) (*routing.AcquireResult, *routing.AcquireResult) {
				user, err := routing.AcquireCapacity(ctx, store, routing.CapacityRequest{ID: f.key.UserID, User: true, Token: token, Limit: 2}, nil)
				require.NoError(t, err)
				require.True(t, user.Acquired)
				account, err := routing.AcquireCapacity(ctx, store, routing.CapacityRequest{ID: f.resource.ID, Token: token, Limit: 2}, nil)
				require.NoError(t, err)
				require.True(t, account.Acquired)
				t.Cleanup(account.ReleaseFunc)
				t.Cleanup(user.ReleaseFunc)
				return user, account
			}
			connection := func(ctx context.Context) *nativeWSRecordedConn {
				handshake, err := nativeWSHandshakeSnapshot(f.resource, f.resource, "wss://api.openai.com/v1/responses", "", http.Header{"Authorization": []string{"Bearer synthetic-upstream-key"}})
				require.NoError(t, err)
				handshake.succeeded = true
				return &nativeWSRecordedConn{handshake: handshake, operation: nativeDispatchFromContext(ctx)}
			}
			oldCtx := bind("old-release-root")
			currentCtx := oldCtx
			oldUser, oldAccount := acquire(currentCtx, "old-token")
			currentUserRelease := RetainNativeHTTPRelease(oldUser.ReleaseFunc).Release
			currentAccountRelease := RetainNativeHTTPRelease(oldAccount.ReleaseFunc).Release
			var afterCalls, releaseCalls atomic.Int32
			// Deliberately read mutable current handles, exactly the reported
			// risk. A repeated provider callback would release the new tokens.
			hooks := &OpenAIWSIngressHooks{
				AfterTurn: func(_ int, result *OpenAIForwardResult, cause error) {
					afterCalls.Add(1)
					_, err := finalizeNativeWSUsage(currentCtx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource}, cause, nativeWSFixtureJournal(), time.Second)
					require.NoError(t, err)
				},
				ReleaseTurn: func(int) {
					releaseCalls.Add(1)
					if currentAccountRelease != nil {
						currentAccountRelease()
						currentAccountRelease = nil
					}
					if currentUserRelease != nil {
						currentUserRelease()
						currentUserRelease = nil
					}
				},
			}
			oldConn := connection(oldCtx)
			body := []byte(`{"type":"response.create","model":"mapped-provider-model"}`)
			require.NoError(t, f.service.sendNativeWS(oldCtx, f.resource, oldConn, coderws.MessageText, body))
			terminal := []byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"old\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}")
			var retry error
			if scenario == "same_turn_reacquire" {
				terminal = []byte("{\"type\":\"response.failed\",\"response\":{\"id\":\"refused\",\"status\":\"failed\"}}")
				retry = &UpstreamFailoverError{StatusCode: 429, RetryableOnSameAccount: true, RejectedBeforeGeneration: true}
			}
			require.NoError(t, oldConn.attempt.observe(terminal, nil))
			oldConn.attempt.close(nil)
			completion := &openAIWSPassthroughTurnCompletion{finalize: func(cause error) error {
				return completeOpenAIWSTurn(oldCtx, hooks, 1, nil, cause)
			}}
			completion.advance(true, true, true, retry)
			if retry == nil {
				require.NoError(t, completion.err())
			} else {
				require.ErrorIs(t, completion.err(), retry)
			}
			require.EqualValues(t, 1, afterCalls.Load())
			require.EqualValues(t, 1, releaseCalls.Load())
			require.Error(t, oldUser.Lease.Validate(oldCtx))
			require.Error(t, oldAccount.Lease.Validate(oldCtx))
			if scenario == "next_turn" {
				currentCtx = bind("new-release-root")
			}
			newUser, newAccount := acquire(currentCtx, "new-token")
			currentUserRelease = RetainNativeHTTPRelease(newUser.ReleaseFunc).Release
			currentAccountRelease = RetainNativeHTTPRelease(newAccount.ReleaseFunc).Release
			before := nativeHTTPFromContext(oldCtx).core.Snapshot()
			var workers sync.WaitGroup
			start := make(chan struct{})
			for range 16 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					completion.advance(true, true, true)
					completion.afterJoin(context.Canceled)
					completion.settle("response.completed")
					completion.deliveredEvent("response.completed")
					completion.setFinalizer(func(error) error { t.Error("old completion replaced its finalized callback"); return nil })
					_ = oldConn.attempt.observe([]byte("{\"type\":\"response.failed\",\"response\":{\"id\":\"late\"}}"), nil)
				}()
			}
			close(start)
			workers.Wait()
			require.EqualValues(t, 1, afterCalls.Load())
			require.EqualValues(t, 1, releaseCalls.Load())
			require.NoError(t, newUser.Lease.Validate(currentCtx), "late old completion released the current user token")
			require.NoError(t, newAccount.Lease.Validate(currentCtx), "late old completion released the current account token")
			require.Equal(t, before.Attempts, nativeHTTPFromContext(oldCtx).core.Snapshot().Attempts)
			newConn := connection(currentCtx)
			require.NoError(t, f.service.sendNativeWS(currentCtx, f.resource, newConn, coderws.MessageText, body))
			require.NoError(t, newConn.attempt.observe([]byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"new\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}"), nil))
			newConn.attempt.close(nil)
			require.NoError(t, completeOpenAIWSTurn(currentCtx, hooks, 2, nil, nil))
			require.EqualValues(t, 2, afterCalls.Load())
			require.EqualValues(t, 2, releaseCalls.Load())
			require.Equal(t, model.OperationCompleted, nativeHTTPFromContext(currentCtx).core.Snapshot().State)
			require.Error(t, newUser.Lease.Validate(currentCtx))
			require.Error(t, newAccount.Lease.Validate(currentCtx))
		})
	}
}

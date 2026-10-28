package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func cloudLifecycleRequest() OpenAICloudRequest {
	return OpenAICloudRequest{Owner: OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}, Body: json.RawMessage(`{"model":"gpt-5.1","input":"hi"}`), RootRequestID: "lifecycle-root", BillingSnapshot: json.RawMessage(`{"frozen":true}`)}
}

func TestCloudSessionStopCancelsInitializationAndClosesAdmission(t *testing.T) {
	store := newCloudLiveRoutingStore()
	entered := make(chan struct{})
	var calls atomic.Int32
	sessions := NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	t.Cleanup(sessions.Close)
	finished := make(chan error, 1)
	go func() { _, err := sessions.Run(t.Context(), cloudLifecycleRequest(), nil); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("initialization not entered")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	require.NoError(t, sessions.Stop(ctx))
	require.Error(t, awaitCloudLifecycleError(t, finished))
	_, err := sessions.Run(t.Context(), cloudLifecycleRequest(), nil)
	require.ErrorIs(t, err, ErrOpenAICloudClosing)
	require.ErrorIs(t, err, ErrOpenAICloudNotStarted)
	require.EqualValues(t, 1, calls.Load())
	sessions.mu.Lock()
	require.Empty(t, sessions.actors)
	require.Zero(t, sessions.pending)
	sessions.mu.Unlock()
}

func TestCloudSessionLateDialCannotPublishAfterStop(t *testing.T) {
	store, wire := newCloudLiveRoutingStore(), newCloudRPCFakeConn()
	entered, release := make(chan struct{}), make(chan struct{})
	rpcs := make(chan *cloudRPC, 1)
	sessions := NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
		close(entered)
		<-release
		rpc := newCloudRPC(ctx, wire)
		rpcs <- rpc
		return rpc, nil
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	t.Cleanup(sessions.Close)
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	finished := make(chan error, 1)
	go func() { _, err := sessions.Run(t.Context(), cloudLifecycleRequest(), nil); finished <- err }()
	awaitCloudLifecycleSignal(t, entered)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, sessions.Drain(ctx), context.DeadlineExceeded)
	sessions.mu.Lock()
	require.NotEmpty(t, sessions.actors)
	sessions.mu.Unlock()
	unlock()
	require.NoError(t, sessions.Stop(t.Context()))
	require.Error(t, awaitCloudLifecycleError(t, finished))
	var rpc *cloudRPC
	select {
	case rpc = <-rpcs:
	case <-time.After(3 * time.Second):
		t.Fatal("late dial result was not joined")
	}
	require.NoError(t, rpc.Wait(t.Context()))
	require.Empty(t, wire.writes, "late initialization must not issue thread/start or turn/start")
}

type cloudLifecycleBlockedStore struct {
	*cloudLiveRoutingStore
	createEntered, createRelease chan struct{}
	readEntered, readRelease     chan struct{}
	createOnce, readOnce         sync.Once
}

func (s *cloudLifecycleBlockedStore) CreateCloudSession(ctx context.Context, r *OpenAICloudSessionRecord) error {
	if s.createEntered != nil {
		s.createOnce.Do(func() { close(s.createEntered) })
		<-s.createRelease
	}
	return s.cloudLiveRoutingStore.CreateCloudSession(ctx, r)
}
func (s *cloudLifecycleBlockedStore) ReadCloudCommands(ctx context.Context, id, after string) ([]OpenAICloudStreamMessage, error) {
	if s.readEntered != nil {
		s.readOnce.Do(func() { close(s.readEntered) })
		<-s.readRelease
	}
	return s.cloudLiveRoutingStore.ReadCloudCommands(ctx, id, after)
}

func TestCloudSessionStopTracksReservationAndActorReaderUntilJoined(t *testing.T) {
	for _, phase := range []string{"reservation", "command-reader"} {
		t.Run(phase, func(t *testing.T) {
			store := &cloudLifecycleBlockedStore{cloudLiveRoutingStore: newCloudLiveRoutingStore()}
			entered, release := make(chan struct{}), make(chan struct{})
			if phase == "reservation" {
				store.createEntered, store.createRelease = entered, release
			} else {
				store.readEntered, store.readRelease = entered, release
			}
			var connects atomic.Int32
			sessions := NewOpenAICloudSessions(store, func(context.Context, OpenAICloudOwner) (*cloudRPC, error) {
				connects.Add(1)
				return nil, errors.New("must not connect")
			}, func(context.Context, OpenAICloudOwner) error { return nil })
			t.Cleanup(sessions.Close)
			var once sync.Once
			unlock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unlock)
			created := make(chan error, 1)
			go func() { _, err := sessions.createSession(t.Context(), cloudLifecycleRequest()); created <- err }()
			awaitCloudLifecycleSignal(t, entered)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			require.ErrorIs(t, sessions.Stop(ctx), context.DeadlineExceeded)
			sessions.mu.Lock()
			require.Positive(t, sessions.pending)
			sessions.mu.Unlock()
			unlock()
			require.NoError(t, sessions.Stop(t.Context()))
			_ = awaitCloudLifecycleError(t, created)
			require.Zero(t, connects.Load())
			settlement := store.settlementFor("lifecycle-root")
			require.NotNil(t, settlement)
			require.True(t, settlement.Unknown)
			require.Nil(t, settlement.Usage)
			sessions.mu.Lock()
			require.Empty(t, sessions.actors)
			require.Zero(t, sessions.pending)
			sessions.mu.Unlock()
		})
	}
}

type cloudLifecycleFailFactsStore struct {
	*cloudLiveRoutingStore
	fail     atomic.Bool
	failLate atomic.Bool
}

func (s *cloudLifecycleFailFactsStore) PreserveCloudLateUsage(ctx context.Context, r *OpenAICloudSessionRecord, fact *OpenAICloudSettlement) error {
	if s.failLate.Load() {
		return errors.New("private late fact persistence failure")
	}
	return s.cloudLiveRoutingStore.PreserveCloudLateUsage(ctx, r, fact)
}

func (s *cloudLifecycleFailFactsStore) SaveCloudSession(ctx context.Context, r *OpenAICloudSessionRecord, refs []string, fact *OpenAICloudSettlement) error {
	if fact != nil && s.fail.Load() {
		return errors.New("private backend failure")
	}
	return s.cloudLiveRoutingStore.SaveCloudSession(ctx, r, refs, fact)
}

func TestCloudSessionDrainCollectsTerminalFactsBeforeRPCJoin(t *testing.T) {
	for _, mode := range []string{"confirmed", "unconfirmed", "persistence-failed", "late", "late-persistence-failed"} {
		t.Run(mode, func(t *testing.T) {
			store := &cloudLifecycleFailFactsStore{cloudLiveRoutingStore: newCloudLiveRoutingStore()}
			wire := newCloudRPCFakeConn()
			sessions := NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
				rpc := newCloudRPC(ctx, wire)
				if err := rpc.Initialize(ctx); err != nil {
					_ = rpc.Close()
					return nil, err
				}
				return rpc, nil
			}, func(context.Context, OpenAICloudOwner) error { return nil })
			t.Cleanup(sessions.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			started, peerDone := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(peerDone)
				cloudWireStart(t, ctx, wire)
				close(started)
				msg := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "turn/interrupt", msg.Method)
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
				cloudStepUsage(wire, "turn", 10, 2)
				if mode != "unconfirmed" {
					cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}}})
				}
				if mode == "late" || mode == "late-persistence-failed" {
					cloudStepUsage(wire, "turn", 20, 4)
				}
				if mode == "late-persistence-failed" {
					select {
					case <-wire.closed:
					case <-ctx.Done():
						t.Error("late fact persistence failure did not stop the producer")
					}
					return
				}
				archive := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "thread/archive", archive.Method)
				cloudTestRead(wire, map[string]any{"id": archive.ID, "result": map[string]any{}})
			}()
			finished := make(chan error, 1)
			go func() { _, err := sessions.Run(ctx, cloudLifecycleRequest(), nil); finished <- err }()
			awaitCloudLifecycleSignal(t, started)
			require.Eventually(t, func() bool {
				store.mu.Lock()
				defer store.mu.Unlock()
				// running is saved before turn/start is sent. The peer's queued
				// reply is not an actor acceptance barrier: wait for its persisted
				// thread/turn binding before cancelling the operation context.
				return store.record != nil && store.record.Status == "running" &&
					store.record.ThreadID == "thread" && store.record.TurnID == "turn"
			}, time.Second, time.Millisecond)
			if mode == "persistence-failed" {
				store.fail.Store(true)
			}
			store.failLate.Store(mode == "late-persistence-failed")
			err := sessions.Drain(ctx)
			if mode == "persistence-failed" || mode == "late-persistence-failed" {
				require.ErrorIs(t, err, ErrOpenAICloudFactsUnavailable)
				require.NotContains(t, err.Error(), "private")
			} else {
				require.NoError(t, err)
			}
			require.Error(t, awaitCloudLifecycleError(t, finished))
			awaitCloudLifecycleSignal(t, peerDone)
			require.Empty(t, wire.writes, "drain must not dispatch another turn or tool reply")
			select {
			case <-wire.closed:
			default:
				t.Fatal("drain did not close RPC")
			}
			fact := store.settlementFor("lifecycle-root")
			if mode == "persistence-failed" {
				require.Nil(t, fact)
			} else {
				require.NotNil(t, fact)
				require.Equal(t, &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}, fact.Usage)
				require.Equal(t, mode == "unconfirmed", fact.Unknown)
				require.Equal(t, mode != "unconfirmed", fact.LowerBound)
				require.Equal(t, 1, store.settlementCount())
				if mode == "late" {
					require.True(t, fact.ReconciliationOnly)
					require.Len(t, fact.LateUsage, 1)
					require.Equal(t, 24, fact.LateUsage[0].Usage.TotalTokens)
				} else {
					require.Empty(t, fact.LateUsage)
				}
			}
			sessions.mu.Lock()
			require.Empty(t, sessions.actors)
			require.Zero(t, sessions.pending)
			sessions.mu.Unlock()
		})
	}
}

func TestCloudSessionCreationBeyondFormerActorLimitPreservesCompletedOwner(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, _, _ := h.establish()
	completed := h.store.all()[0].snapshot()
	sessions := h.svc.cloudSessions
	const additional = 20
	for i := 0; i < additional; i++ {
		request := cloudLifecycleRequest()
		request.Owner = completed.OpenAICloudOwner
		request.RootRequestID = fmt.Sprintf("above-actor-limit-%d", i)
		id, err := sessions.createSession(h.ctx, request)
		require.NoError(t, err)
		require.NotEmpty(t, id)
	}
	sessions.mu.Lock()
	actors, pending := len(sessions.actors), sessions.pending
	sessions.mu.Unlock()
	require.Equal(t, additional+1, actors)
	require.Equal(t, additional+1, pending)
	retained, err := h.store.LoadCloudSession(h.ctx, completed.SessionID)
	require.NoError(t, err)
	require.NotNil(t, retained)
	require.Equal(t, "completed", retained.Status, "new roots cannot pressure-evict a completed continuation")
	require.Equal(t, completed.OwnerToken, retained.OwnerToken)
	require.False(t, h.store.session(completed.SessionID).wasReleased())

	// Explicit close still archives and releases the completed owner. No new
	// session above should have connected or dispatched a generation.
	require.Empty(t, h.wires)
	archived := make(chan struct{})
	go func() {
		defer close(archived)
		cloudWireResponse(t, h.ctx, wire, "thread/archive", map[string]any{})
	}()
	require.NoError(t, sessions.CloseCompleted(h.ctx, completed.OpenAICloudOwner, completed.SessionID))
	awaitCloudLifecycleSignal(t, archived)
	require.NoError(t, sessions.Stop(h.ctx))
	require.Equal(t, additional+1, h.store.settlementCount(), "stopping unstarted actors preserves their unknown facts and the original completed bill")
	sessions.mu.Lock()
	actors, pending = len(sessions.actors), sessions.pending
	sessions.mu.Unlock()
	require.Zero(t, actors)
	require.Zero(t, pending)
}

type cloudAdmissionBlockedCreateStore struct {
	*cloudMultiRoutingStore
	entered chan struct{}
	release chan struct{}
}

func (s *cloudAdmissionBlockedCreateStore) CreateCloudSession(ctx context.Context, r *OpenAICloudSessionRecord) error {
	s.entered <- struct{}{}
	// Model a successful write whose reply arrives after shutdown canceled
	// its caller. The registered reservation must remain owned until joined.
	<-s.release
	<-ctx.Done()
	return s.cloudMultiRoutingStore.CreateCloudSession(ctx, r)
}

func TestCloudSessionConcurrentCreationBeyondFormerLimitRemainsTrackedThroughStop(t *testing.T) {
	const count = 20
	store := &cloudAdmissionBlockedCreateStore{cloudMultiRoutingStore: newCloudMultiRoutingStore(), entered: make(chan struct{}, count), release: make(chan struct{})}
	var connects atomic.Int32
	sessions := NewOpenAICloudSessions(store, func(context.Context, OpenAICloudOwner) (*cloudRPC, error) {
		connects.Add(1)
		return nil, errors.New("must not connect")
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	t.Cleanup(sessions.Close)
	var once sync.Once
	unlock := func() { once.Do(func() { close(store.release) }) }
	t.Cleanup(unlock)
	type creation struct {
		id  string
		err error
	}
	results := make(chan creation, count)
	for i := 0; i < count; i++ {
		go func() {
			request := cloudLifecycleRequest()
			request.RootRequestID = fmt.Sprintf("concurrent-create-%d", i)
			id, err := sessions.createSession(t.Context(), request)
			results <- creation{id, err}
		}()
	}
	deadline, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for i := 0; i < count; i++ {
		select {
		case <-store.entered:
		case <-deadline.Done():
			t.Fatal("all creations must reach storage beyond the former local cap")
		}
	}
	sessions.mu.Lock()
	pending, actors := sessions.pending, len(sessions.actors)
	sessions.mu.Unlock()
	require.Equal(t, count, pending, "every creation is registered before I/O")
	require.Zero(t, actors)
	stopCtx, cancelStop := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancelStop()
	require.ErrorIs(t, sessions.Stop(stopCtx), context.DeadlineExceeded)
	id, err := sessions.createSession(t.Context(), cloudLifecycleRequest())
	require.Empty(t, id)
	require.ErrorIs(t, err, ErrOpenAICloudClosing)
	unlock()
	joinCtx, cancelJoin := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelJoin()
	for i := 0; i < count; i++ {
		select {
		case result := <-results:
			require.NotEmpty(t, result.id, "a late successful write must retain its persisted identity")
			require.ErrorIs(t, result.err, context.Canceled)
		case <-joinCtx.Done():
			t.Fatal("creation did not return after storage released")
		}
	}
	require.NoError(t, sessions.Stop(joinCtx))
	require.Zero(t, connects.Load())
	require.Len(t, store.all(), count)
	for _, sub := range store.all() {
		fact := sub.settlementFor(sub.snapshot().RootRequestID)
		require.NotNil(t, fact)
		require.True(t, fact.Unknown)
		require.Nil(t, fact.Usage)
	}
	sessions.mu.Lock()
	pending, actors = sessions.pending, len(sessions.actors)
	sessions.mu.Unlock()
	require.Zero(t, pending)
	require.Zero(t, actors)
}

func awaitCloudLifecycleSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("Cloud lifecycle fixture signal timed out")
	}
}
func awaitCloudLifecycleError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("Cloud lifecycle operation did not join")
		return nil
	}
}

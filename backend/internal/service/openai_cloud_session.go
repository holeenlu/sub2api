package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// Each actor owns exactly one live upstream connection. Redis carries commands
// and response events to that actor; a record on another instance is not a
// substitute for its socket or pending server RPCs.
type OpenAICloudSessions struct {
	store           OpenAICloudSessionStore
	connect         func(context.Context, OpenAICloudOwner) (*cloudRPC, error)
	validate        func(context.Context, OpenAICloudOwner) error
	instance        string
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	actors          map[string]*cloudSessionActor
	toolWaitTimeout time.Duration
}

var errCloudAuthorizationChanged = errors.New("Cloud authorization or account settings changed")

// Run and the owner may attach this marker only with proof that the request
// was not dispatched. Transport failures alone never establish that proof.
var ErrOpenAICloudNotStarted = errors.New("Cloud request was not submitted")

// Capacity is a definite refusal before persistence, unlike a Redis timeout.
var ErrOpenAICloudCapacity = errors.New("Cloud session capacity is exhausted")

type cloudIdleSessionStore interface {
	FindCloudIdleSession(context.Context, int64, int64) (*OpenAICloudSessionRecord, error)
}

// Retain continuation sockets while capacity is available. Under pressure only
// the owner may retire an idle completed session, serialized with new turns.
func (s *OpenAICloudSessions) evictLocalIdle(ctx context.Context) error {
	s.mu.Lock()
	var oldest *cloudSessionActor
	if len(s.actors) >= 16 {
		for _, a := range s.actors {
			if at := a.idleSince.Load(); at > 0 && (oldest == nil || at < oldest.idleSince.Load()) {
				oldest = a
			}
		}
	}
	s.mu.Unlock()
	if oldest == nil {
		return nil
	}
	if err := s.CloseCompleted(ctx, oldest.record.OpenAICloudOwner, oldest.record.SessionID); err != nil {
		return err
	}
	select {
	case <-oldest.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func NewOpenAICloudSessions(store OpenAICloudSessionStore, connect func(context.Context, OpenAICloudOwner) (*cloudRPC, error), validate func(context.Context, OpenAICloudOwner) error) *OpenAICloudSessions {
	ctx, cancel := context.WithCancel(context.Background())
	return &OpenAICloudSessions{store: store, connect: connect, validate: validate, instance: uuid.NewString(), ctx: ctx, cancel: cancel, actors: make(map[string]*cloudSessionActor), toolWaitTimeout: openAIWSPassthroughIdleTimeoutDefault}
}

func (s *OpenAICloudSessions) Close() { s.cancel() }

func (s *OpenAICloudSessions) Resolve(ctx context.Context, userID, keyID, groupID int64, body []byte) (*OpenAICloudBinding, error) {
	previous, calls, err := cloudResponseReferences(body)
	if err != nil {
		return nil, err
	}
	// Imported native/BPS call-output pairs are history, not Cloud routing
	// references. The owning actor compares their immutable outputs on resume.
	pairedHistory := make(map[string]bool)
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() == "function_call" {
			id := item.Get("call_id").String()
			if id != "" && !strings.HasPrefix(id, "call_cloud_") {
				pairedHistory[id] = true
			}
		}
	}
	var references []string
	for _, id := range calls {
		if !pairedHistory[id] {
			references = append(references, id)
		}
	}
	if previous != "" {
		references = append(references, previous)
	}
	var record *OpenAICloudSessionRecord
	unknown := false
	for _, ref := range references {
		found, e := s.store.FindCloudSession(ctx, userID, keyID, groupID, ref)
		if e != nil {
			return nil, errors.New("Cloud session state is unavailable")
		}
		if found == nil {
			unknown = true
			continue
		}
		if record != nil && record.SessionID != found.SessionID {
			return nil, errors.New("Cloud references belong to different sessions")
		}
		record = found
	}
	if record == nil {
		if cloudIsResponseID(previous) {
			return nil, errors.New("Cloud response is unknown or expired")
		}
		for _, id := range calls {
			if strings.HasPrefix(id, "call_cloud_") {
				return nil, errors.New("Cloud tool call is unknown or expired")
			}
		}
		return nil, nil
	}
	if unknown {
		return nil, errors.New("Cloud request contains an unknown continuation reference")
	}
	if record.UserID != userID || record.APIKeyID != keyID || record.GroupID != groupID {
		return nil, errors.New("Cloud continuation ownership does not match")
	}
	binding := record.OpenAICloudBinding
	return &binding, nil
}

func (s *OpenAICloudSessions) Run(ctx context.Context, request OpenAICloudRequest, emit func(json.RawMessage) error) (result *OpenAICloudResult, runErr error) {
	mayHaveStarted := false
	defer func() {
		if runErr != nil && !mayHaveStarted {
			runErr = fmt.Errorf("%w: %w", ErrOpenAICloudNotStarted, runErr)
		}
	}()
	ctx, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	if s == nil || s.store == nil || s.connect == nil || s.validate == nil {
		return nil, errors.New("Cloud sessions are unavailable")
	}
	parsed, err := parseCloudProtocolRequest(request.Body)
	if err != nil {
		return nil, err
	}
	if request.Owner.UserID <= 0 || (!request.Owner.Probe && request.Owner.APIKeyID <= 0) || (request.Owner.Probe && request.Owner.APIKeyID != 0) || request.Owner.AccountID <= 0 || request.Owner.GrantGeneration == "" || request.Owner.SettingsDigest == "" || parsed.model != request.Owner.Model {
		return nil, errors.New("Cloud execution ownership is incomplete")
	}
	if err := s.validate(ctx, request.Owner); err != nil {
		return nil, err
	}
	binding, err := s.Resolve(ctx, request.Owner.UserID, request.Owner.APIKeyID, request.Owner.GroupID, request.Body)
	if err != nil {
		return nil, err
	}
	var sessionID string
	if binding != nil {
		if binding.OpenAICloudOwner != request.Owner {
			return nil, errors.New("Cloud continuation identity or settings changed")
		}
		if time.Now().After(binding.ExpiresAt) {
			return nil, errors.New("Cloud session expired")
		}
		sessionID = binding.SessionID
	} else {
		if parsed.previous != "" {
			return nil, errors.New("Cloud cannot resume a response from another execution mode")
		}
		if len(parsed.input) == 0 {
			return nil, errors.New("Cloud initial request requires a final user message")
		}
		if request.RootRequestID == "" || (!request.Owner.Probe && len(request.BillingSnapshot) == 0) || len(request.BillingSnapshot) > 256<<10 {
			return nil, errors.New("Cloud billing snapshot is missing or too large")
		}
		if err := s.evictLocalIdle(ctx); err != nil {
			return nil, ErrOpenAICloudCapacity
		}
		s.mu.Lock()
		if len(s.actors) >= 16 {
			s.mu.Unlock()
			return nil, ErrOpenAICloudCapacity
		}
		sessionID = uuid.NewString()
		now := time.Now()
		record := &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: request.Owner, SessionID: sessionID, RootRequestID: request.RootRequestID, OwnerInstance: s.instance, ExpiresAt: now.Add(OpenAICloudSessionTTL), Status: "created"}, OwnerToken: uuid.NewString(), LeaseUntil: now.Add(OpenAICloudOwnerLease), BillingSnapshot: request.BillingSnapshot}
		actor := &cloudSessionActor{service: s, record: record, done: make(chan struct{}), tools: make(map[string]*cloudPendingTool), completed: make(map[string]*OpenAICloudResult)}
		mayHaveStarted = true
		createErr := s.store.CreateCloudSession(ctx, record)
		if errors.Is(createErr, ErrOpenAICloudCapacity) {
			s.mu.Unlock()
			retired := false
			if idleStore, ok := s.store.(cloudIdleSessionStore); ok {
				if idle, lookupErr := idleStore.FindCloudIdleSession(ctx, request.Owner.UserID, request.Owner.AccountID); lookupErr == nil && idle != nil {
					retired = s.CloseCompleted(ctx, idle.OpenAICloudOwner, idle.SessionID) == nil
				}
			}
			s.mu.Lock()
			if retired && len(s.actors) < 16 {
				createErr = s.store.CreateCloudSession(ctx, record)
			}
		}
		if createErr != nil {
			s.mu.Unlock()
			if errors.Is(createErr, ErrOpenAICloudCapacity) {
				mayHaveStarted = false
				return nil, ErrOpenAICloudCapacity
			}
			return nil, errors.New("Cloud could not persist the session reservation")
		}
		s.actors[sessionID] = actor
		s.mu.Unlock()
		go actor.run()
	}
	id := uuid.NewString()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = s.store.DeleteCloudEvents(cleanup, id)
	}()
	mayHaveStarted = true
	deadline, _ := ctx.Deadline()
	if err := s.store.EnqueueCloudCommand(ctx, sessionID, OpenAICloudCommand{RequestID: id, Request: request, Deadline: deadline}); err != nil {
		return nil, errors.New("Cloud owner is unavailable")
	}
	cancelRequest := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		// Fence dispatch before queueing the interrupt: the actor may be blocked
		// in connection/thread preparation and cannot consume its queue yet.
		_ = s.store.DeleteCloudEvents(cleanup, id)
		_ = s.store.EnqueueCloudCommand(cleanup, sessionID, OpenAICloudCommand{RequestID: id, Request: OpenAICloudRequest{Owner: request.Owner}, Cancel: true})
	}
	cursor := "0-0"
	for {
		messages, readErr := s.store.ReadCloudEvents(ctx, id, cursor)
		if readErr != nil {
			cancelRequest()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("Cloud response event delivery failed")
		}
		for _, message := range messages {
			cursor = message.ID
			var event OpenAICloudStreamEvent
			if json.Unmarshal(message.Payload, &event) != nil {
				return nil, errors.New("Cloud response state is invalid")
			}
			if event.Error != "" {
				if event.NotStarted {
					if event.Error == errCloudOriginalToolImage.Error() {
						return nil, errors.Join(ErrOpenAICloudNotStarted, &cloudGatewayError{400, "cloud_unsupported_parameter", event.Error})
					}
					return nil, errors.Join(ErrOpenAICloudNotStarted, errors.New(event.Error), &cloudGatewayError{400, "cloud_invalid_request", "Cloud continuation was rejected before dispatch; check its history and generation settings."})
				}
				return nil, errors.New(event.Error)
			}
			if len(event.Event) > 0 && emit != nil {
				if err := emit(event.Event); err != nil {
					cancelRequest()
					return nil, err
				}
			}
			if event.Result != nil {
				return event.Result, nil
			}
		}
		if len(messages) == 0 {
			if request.onIdle != nil {
				if err := request.onIdle(); err != nil {
					cancelRequest()
					return nil, err
				}
			}
			record, err := s.store.LoadCloudSession(ctx, sessionID)
			if err != nil || record == nil {
				return nil, errors.New("Cloud session state is unavailable")
			}
			if time.Now().After(record.LeaseUntil) {
				return nil, errors.New("Cloud owner is no longer available; execution will not be replayed")
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
}

func (s *OpenAICloudSessions) CloseCompleted(ctx context.Context, owner OpenAICloudOwner, sessionID string) error {
	ctx, cancelOperation := context.WithTimeout(ctx, 10*time.Second)
	defer cancelOperation()
	id := uuid.NewString()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = s.store.DeleteCloudEvents(cleanup, id)
	}()
	if err := s.store.EnqueueCloudCommand(ctx, sessionID, OpenAICloudCommand{RequestID: id, Request: OpenAICloudRequest{Owner: owner}, CloseCompleted: true}); err != nil {
		return err
	}
	cursor := "0-0"
	for {
		messages, err := s.store.ReadCloudEvents(ctx, id, cursor)
		if err != nil {
			return err
		}
		for _, msg := range messages {
			cursor = msg.ID
			var event OpenAICloudStreamEvent
			if json.Unmarshal(msg.Payload, &event) != nil {
				return errors.New("Cloud close acknowledgement is invalid")
			}
			if event.Error != "" {
				return errors.New(event.Error)
			}
			if event.Result != nil {
				if event.Result.ErrorStatus >= 400 {
					var failure struct {
						Error struct {
							Code    string `json:"code"`
							Message string `json:"message"`
						} `json:"error"`
					}
					if json.Unmarshal(event.Result.Response, &failure) != nil || failure.Error.Code == "" {
						return errors.New("Cloud close acknowledgement is invalid")
					}
					return &cloudGatewayError{event.Result.ErrorStatus, failure.Error.Code, failure.Error.Message}
				}
				return nil
			}
		}
	}
}

type cloudPendingTool struct {
	RPCID                   json.RawMessage
	CallID, Name, Arguments string
	Submitted               bool
	Published               bool
	Order                   int
	Digest                  string
}

type cloudSessionActor struct {
	idleSince               atomic.Int64
	done                    chan struct{}
	service                 *OpenAICloudSessions
	record                  *OpenAICloudSessionRecord
	rpc                     *cloudRPC
	protocol                *cloudProtocolRequest
	requestID               string
	responseID              string
	lastResponseID          string
	requestDigest           string
	sequence                int
	output                  []map[string]any
	items                   map[string]int
	outputBytes             int
	tools                   map[string]*cloudPendingTool
	completed               map[string]*OpenAICloudResult
	threadTotal             *OpenAICloudUsage
	turnBase                *OpenAICloudUsage
	history                 []any
	ctx                     context.Context
	cancel                  context.CancelFunc
	rpcCancel               context.CancelFunc
	upstream                <-chan cloudRPCMessage
	uncertain               bool
	usageTainted            bool
	retainedBytes           int
	metadata                any
	released                bool
	turnProducedOutput      bool
	rejectedBeforeInference bool
	publicFailure           map[string]any
	errorStatus             int
	rawItems                map[string]string
	pendingMessages         map[string]string
	fallbackMessages        map[string]string
	fallbackMessageBytes    int
	hostedUsage             OpenAICloudUsage
	hostedStates            map[string]*cloudHostedState
	hostedReady             []cloudHostedOutput
	hostedBufferedBytes     int
	bufferedEvents          []cloudRPCMessage
	bufferedEventBytes      int
	drainingBuffered        bool
	lastClientActivity      time.Time
	waitingSince            time.Time
}

func (a *cloudSessionActor) run() {
	a.ctx, a.cancel = context.WithCancel(a.service.ctx)
	rpcCtx, rpcCancel := context.WithCancel(context.Background())
	a.rpcCancel = rpcCancel
	defer a.cancel()
	defer func() {
		if a.record.Status == "completed" {
			_ = a.releaseCompleted()
		}
		if !a.released && a.rpc != nil && a.record.ThreadID != "" {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = a.rpc.Call(cleanup, "thread/archive", map[string]any{"threadId": a.record.ThreadID})
			stop()
		}
		if a.rpcCancel != nil {
			a.rpcCancel()
		}
		if a.rpc != nil {
			_ = a.rpc.Close()
		}
		a.service.mu.Lock()
		delete(a.service.actors, a.record.SessionID)
		a.service.mu.Unlock()
		if a.done != nil {
			close(a.done)
		}
	}()
	commands := make(chan OpenAICloudCommand, 8)
	failures := make(chan error, 2)
	events := make(chan cloudRPCMessage, 8)
	a.upstream = events
	go func() {
		cursor := "0-0"
		for {
			messages, err := a.service.store.ReadCloudCommands(a.ctx, a.record.SessionID, cursor)
			if err != nil {
				select {
				case failures <- errors.New("Cloud command routing failed"):
				case <-a.ctx.Done():
				}
				return
			}
			for _, msg := range messages {
				cursor = msg.ID
				var command OpenAICloudCommand
				if json.Unmarshal(msg.Payload, &command) != nil {
					select {
					case failures <- errors.New("Cloud command is invalid"):
					case <-a.ctx.Done():
					}
					return
				}
				select {
				case commands <- command:
				case <-a.ctx.Done():
					return
				}
			}
		}
	}()
	owner := a.record.OpenAICloudOwner
	sessionID, ownerToken := a.record.SessionID, a.record.OwnerToken
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		invalidSent := false
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				validErr := a.service.validate(a.ctx, owner)
				ok, err := a.service.store.RenewCloudSession(a.ctx, sessionID, ownerToken, time.Now().Add(OpenAICloudOwnerLease))
				if err != nil || !ok {
					rpcCancel()
					select {
					case failures <- errors.New("Cloud authorization or owner lease was lost"):
					case <-a.ctx.Done():
					}
					return
				}
				if validErr != nil && !invalidSent {
					invalidSent = true
					select {
					case failures <- errCloudAuthorizationChanged:
					case <-a.ctx.Done():
						return
					}
				}
			}
		}
	}()
	idleTicker := time.NewTicker(5 * time.Second)
	defer idleTicker.Stop()
	for {
		select {
		case now := <-idleTicker.C:
			if a.expireIdle(now) {
				return
			}
		case command := <-commands:
			if command.Request.Owner != a.record.OpenAICloudOwner {
				a.rejectUnstarted(command.RequestID, "Cloud continuation ownership does not match")
				continue
			}
			if command.CloseCompleted {
				if a.record.Status != "completed" || a.requestID != "" {
					a.reject(command.RequestID, "Cloud session still has an active turn")
					continue
				}
				if err := a.releaseCompleted(); err != nil {
					status, code, message, known := cloudPublicError(err)
					if !known {
						status, code, message = 502, "cloud_session_cleanup_failed", "Cloud completed session could not be released; retry verification."
					}
					raw, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message}})
					_ = a.service.store.PublishCloudEvent(a.ctx, command.RequestID, OpenAICloudStreamEvent{Result: &OpenAICloudResult{SessionID: a.record.SessionID, Terminal: true, ErrorStatus: status, Response: raw}})
					return
				}
				_ = a.service.store.PublishCloudEvent(a.ctx, command.RequestID, OpenAICloudStreamEvent{Result: &OpenAICloudResult{SessionID: a.record.SessionID, Terminal: true}})
				return
			}
			if command.Cancel {
				if a.requestID == command.RequestID {
					a.stop("Cloud client cancelled the request")
				}
				continue
			}
			if err := a.service.validate(a.ctx, command.Request.Owner); err != nil {
				a.rejectUnstarted(command.RequestID, "Cloud authorization or account settings changed")
				a.stop("Cloud authorization or account settings changed")
				continue
			}
			if err := a.checkRequest(command); err != nil {
				a.rejectUnstarted(command.RequestID, err.Error())
				if a.record.Status == "created" {
					a.terminateError(err, err.Error(), false)
					return
				}
				continue
			}
			if a.rpc == nil {
				rpc, err := a.service.connect(rpcCtx, a.record.OpenAICloudOwner)
				if err != nil {
					a.requestID = command.RequestID
					a.terminateError(err, "Cloud connection failed", false)
					return
				}
				a.rpc = rpc
				go func() {
					for {
						event, err := rpc.Next(rpcCtx)
						if err != nil {
							select {
							case failures <- err:
							case <-rpcCtx.Done():
							}
							return
						}
						select {
						case events <- event:
						case <-rpcCtx.Done():
							return
						}
					}
				}()
			}
			if err := a.handleCommand(command); err != nil {
				if a.requestID == command.RequestID {
					a.stop(err.Error())
				} else {
					a.rejectUnstarted(command.RequestID, err.Error())
					if a.record.Status == "created" {
						a.terminateError(err, err.Error(), false)
						return
					}
				}
			}
		case event := <-events:
			if err := a.handleEvent(event); err != nil {
				a.stop(err.Error())
			}
		case failure := <-failures:
			if a.record.Status == "completed" {
				return
			}
			if errors.Is(failure, errCloudAuthorizationChanged) {
				a.stop(failure.Error())
				return
			}
			a.terminate("unknown", "Cloud connection or routing was lost; execution will not be replayed", true)
			return
		case <-a.ctx.Done():
			a.stop("Cloud session expired or service stopped")
			return
		}
		if a.record.Status == "failed" || a.record.Status == "unknown" || a.record.Status == "interrupted" {
			return
		}
		if a.record.Status == "completed" && a.requestID == "" && a.idleSince.Load() == 0 {
			a.idleSince.Store(time.Now().UnixNano())
		}
	}
}

// Only a completed, inactive conversation is eligible for idle expiry. Pending
// tools and a running turn retain their owner until cancellation or lease loss.
func (a *cloudSessionActor) completedIdleExpired(now time.Time) bool {
	at := a.idleSince.Load()
	return a.record.Status == "completed" && a.requestID == "" && at > 0 && !now.Before(time.Unix(0, at).Add(OpenAICloudSessionTTL))
}

func (a *cloudSessionActor) toolWaitExpired(now time.Time) bool {
	if a.record.Status != "awaiting_tools" || a.requestID != "" {
		return false
	}
	idleSince := a.lastClientActivity
	if a.waitingSince.After(idleSince) {
		idleSince = a.waitingSince
	}
	if idleSince.IsZero() {
		return false
	}
	timeout := openAIWSPassthroughIdleTimeoutDefault
	if a.service != nil && a.service.toolWaitTimeout > 0 {
		timeout = a.service.toolWaitTimeout
	}
	return !now.Before(idleSince.Add(timeout))
}

func (a *cloudSessionActor) expireIdle(now time.Time) bool {
	if a.toolWaitExpired(now) {
		a.stop("Cloud client tool response idle timeout")
		return true
	}
	return a.completedIdleExpired(now)
}

func (a *cloudSessionActor) releaseCompleted() error {
	if a.released {
		return nil
	}
	a.released = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	var archiveErr error
	if a.rpc != nil && a.record.ThreadID != "" {
		_, archiveErr = a.rpc.Call(ctx, "thread/archive", map[string]any{"threadId": a.record.ThreadID})
	}
	cancel()
	cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := a.service.store.ReleaseCloudSession(cleanup, a.record.SessionID, a.record.OwnerToken); err != nil {
		return errors.New("Cloud completed session release failed")
	}
	if archiveErr != nil {
		if errors.Is(archiveErr, context.DeadlineExceeded) {
			return &cloudGatewayError{504, "cloud_archive_timeout", "Cloud completed the turn but thread archival timed out; retry verification."}
		}
		status, code, message, known := cloudPublicError(archiveErr)
		if known {
			return &cloudGatewayError{status, code, "Cloud completed the turn but thread archival failed. " + message}
		}
		return &cloudGatewayError{502, "cloud_archive_failed", "Cloud completed the turn but thread archival was not confirmed; retry verification."}
	}
	return nil
}

func (a *cloudSessionActor) reject(id, message string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
	defer cancel()
	_ = a.service.store.PublishCloudEvent(ctx, id, OpenAICloudStreamEvent{Error: message})
}

// Query persisted request state at every upstream dispatch boundary. The
// socket remains session-scoped across separate HTTP/WS tool requests.
func (a *cloudSessionActor) checkRequest(command OpenAICloudCommand) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if !command.Deadline.IsZero() && !time.Now().Before(command.Deadline) {
		return context.DeadlineExceeded
	}
	closed, err := a.service.store.CloudRequestClosed(a.ctx, command.RequestID)
	if err != nil {
		return errors.New("Cloud request state is unavailable")
	}
	if closed {
		return context.Canceled
	}
	return nil
}

func (a *cloudSessionActor) rejectUnstarted(id, message string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
	defer cancel()
	_ = a.service.store.PublishCloudEvent(ctx, id, OpenAICloudStreamEvent{Error: message, NotStarted: true})
}

func (a *cloudSessionActor) save(refs []string, settlement *OpenAICloudSettlement) error {
	a.record.Sequence++
	if settlement != nil {
		settlement.Sequence = a.record.Sequence
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
	defer cancel()
	return a.service.store.SaveCloudSession(ctx, a.record, refs, settlement)
}

func (a *cloudSessionActor) handleCommand(command OpenAICloudCommand) error {
	if err := a.checkRequest(command); err != nil {
		return err
	}
	p, err := parseCloudProtocolRequest(command.Request.Body)
	if err != nil {
		return err
	}
	digest := cloudDigest(command.Request.Body)
	if len(p.input) == 0 && len(p.outputs) > 0 {
		canonical, _ := json.Marshal(p.outputs)
		digest = "tools:" + cloudDigest(canonical)
	}
	if a.requestID != "" {
		return errors.New("Cloud already has an active response for this turn")
	}
	newUserTurn := a.record.Status == "completed" && len(p.input) > 0
	if a.protocol != nil {
		names := p.names
		if len(names) == 0 {
			names = a.protocol.names
		}
		for _, key := range []string{"instructions", "tools", "tool_choice", "parallel_tool_calls", "reasoning", "text"} {
			if key == "text" && newUserTurn {
				// outputSchema is per-turn, while verbosity was configured on the
				// thread. Inherit only verbosity into the next user turn.
				current, _ := p.body["text"].(map[string]any)
				previous, _ := a.protocol.body["text"].(map[string]any)
				if current == nil {
					current = map[string]any{}
				}
				if _, supplied := current["verbosity"]; !supplied {
					if value, ok := previous["verbosity"]; ok {
						current["verbosity"] = value
					}
				}
				if len(current) > 0 {
					p.body["text"] = current
				}
				continue
			}
			if _, exists := p.body[key]; !exists {
				if value, ok := a.protocol.body[key]; ok {
					p.body[key] = value
				}
			}
		}
		inherited, _ := json.Marshal(p.body)
		p, err = parseCloudProtocolRequest(inherited)
		if err != nil {
			return err
		}
		p.names = names
		if p.threadSettings != a.protocol.threadSettings || (!newUserTurn && p.settings != a.protocol.settings) {
			return errors.New("Cloud continuation tools or generation settings changed")
		}
		if err := a.checkHistory(p); err != nil {
			return err
		}
	}
	for id, output := range p.outputs {
		if tool := a.tools[id]; tool != nil && tool.Submitted && tool.Digest != cloudDigest([]byte(output)) {
			return errors.New("Cloud tool output conflicts with an already submitted result")
		}
	}
	if old := a.completed[digest]; old != nil {
		if old.RootRequestID != a.record.RootRequestID {
			return errors.New("Cloud tool response belongs to an earlier completed turn")
		}
		a.lastClientActivity = time.Now()
		replay := *old
		replay.Replayed = true
		return a.replayCompletedResponse(command.RequestID, &replay)
	}
	if p.previous != "" && p.previous != a.lastResponseID {
		return errors.New("Cloud branching from an earlier response is not supported")
	}
	activeTurn := a.record.Status == "awaiting_tools" || a.record.Status == "running"
	if activeTurn && len(p.input) > 0 {
		return errors.New("Cloud requires pending tool outputs before another user message")
	}
	if activeTurn && len(p.outputs) == 0 {
		return errors.New("Cloud continuation requires tool outputs")
	}
	if !activeTurn && len(p.input) == 0 {
		return errors.New("Cloud new turn requires a final user message")
	}
	if !activeTurn && (command.Request.RootRequestID == "" || (!command.Request.Owner.Probe && len(command.Request.BillingSnapshot) == 0)) {
		return errors.New("Cloud new turn requires its own billing snapshot")
	}
	if !activeTurn && a.protocol != nil && command.Request.RootRequestID == a.record.RootRequestID {
		return errors.New("Cloud new turn cannot reuse an earlier billing root")
	}
	if !activeTurn && a.retainedBytes+len(command.Request.Body)+len(command.Request.BillingSnapshot) > 16<<20 {
		return errors.New("Cloud session retained data reached its limit")
	}
	// Validate the entire batch before replying to even its first server RPC.
	for id, output := range p.outputs {
		tool := a.tools[id]
		if tool == nil {
			if !activeTurn {
				continue
			}
			return errors.New("Cloud tool call is unknown")
		}
		if tool.Submitted && tool.Digest != cloudDigest([]byte(output)) {
			return errors.New("Cloud tool output conflicts with an already submitted result")
		}
		if !tool.Submitted && !tool.Published {
			return errors.New("Cloud tool output was not requested by this response")
		}
		if !tool.Submitted && p.originalToolImages[id] {
			return errCloudOriginalToolImage
		}
	}
	a.idleSince.Store(0)
	a.lastClientActivity = time.Now()
	a.requestID = command.RequestID
	a.requestDigest = digest
	a.responseID = "resp_cloud_" + uuid.NewString()
	a.output = nil
	a.items = make(map[string]int)
	a.pendingMessages = make(map[string]string)
	a.outputBytes = 0
	a.sequence = 0
	a.metadata = p.body["metadata"]
	if !activeTurn {
		a.protocol = p
	}
	if err := a.emit("response.created", map[string]any{"response": a.response("in_progress", nil)}); err != nil {
		return err
	}
	if err := a.emit("response.in_progress", map[string]any{"response": a.response("in_progress", nil)}); err != nil {
		return err
	}
	if activeTurn {
		a.finishToolWait(time.Now())
		if err := a.flushHostedOutputs(); err != nil {
			return err
		}
		for id, output := range p.outputs {
			tool := a.tools[id]
			if tool.Submitted {
				continue
			}
			if err := a.checkRequest(command); err != nil {
				return err
			}
			if err := a.rpc.Reply(a.ctx, tool.RPCID, map[string]any{"contentItems": p.outputItems[id], "success": true}); err != nil {
				a.terminate("unknown", "Cloud tool output acceptance is unknown", true)
				return nil
			}
			tool.Submitted = true
			tool.Digest = cloudDigest([]byte(output))
			a.retainedBytes += len(output)
			if a.retainedBytes > 16<<20 {
				a.stop("Cloud retained session bytes exceeded their limit")
				return nil
			}
			a.history = append(a.history, map[string]any{"type": "function_call_output", "call_id": id, "output": p.outputHistory[id]})
		}
		// These events become public in this response, after the submitted tool
		// results. Preserve that same order in the full-history client contract.
		if err := a.flushBufferedEvents(); err != nil {
			return err
		}
		a.record.Status = "running"
		if err := a.save(nil, nil); err != nil {
			a.stop("Cloud continuation persistence failed")
			return nil
		}
		if a.hasPendingTools() {
			return a.finishAwaiting()
		}
		return nil
	}
	a.record.RootRequestID = command.Request.RootRequestID
	a.record.BillingSnapshot = command.Request.BillingSnapshot
	a.record.Telemetry = &OpenAICloudTelemetry{StartedAt: time.Now()}
	if command.Request.Telemetry != nil {
		copy := *command.Request.Telemetry
		a.record.Telemetry = &copy
	}
	a.waitingSince = time.Time{}
	a.record.Status = "running"
	a.record.Usage = nil
	a.turnProducedOutput = false
	a.rejectedBeforeInference = false
	a.publicFailure = nil
	a.errorStatus = 0
	a.rawItems = make(map[string]string)
	a.fallbackMessages = make(map[string]string)
	a.fallbackMessageBytes = 0
	a.hostedUsage = OpenAICloudUsage{}
	a.hostedStates = make(map[string]*cloudHostedState)
	a.hostedReady = nil
	a.hostedBufferedBytes = 0
	a.bufferedEvents = nil
	a.bufferedEventBytes = 0
	a.record.Result = nil
	a.record.TurnID = ""
	a.uncertain = false
	a.retainedBytes += len(command.Request.Body) + len(command.Request.BillingSnapshot)
	if a.retainedBytes > 16<<20 {
		a.stop("Cloud retained session bytes exceeded their limit")
		return nil
	}
	a.turnBase = a.threadTotal
	if err := a.save(nil, nil); err != nil {
		a.stop("Cloud turn reservation could not be persisted")
		return nil
	}
	checkDispatch := func() bool {
		if err := a.checkRequest(command); err != nil {
			a.terminateError(err, "Cloud request ended before inference started", false)
			return false
		}
		return true
	}
	if !checkDispatch() {
		return nil
	}
	if a.record.ThreadID == "" {
		result, err := a.rpc.Call(a.ctx, "thread/start", cloudThreadParams(p))
		if err != nil {
			a.terminateError(err, "Cloud thread creation could not be confirmed", false)
			return nil
		}
		var thread struct {
			Model  string `json:"model"`
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if json.Unmarshal(result, &thread) != nil || thread.Thread.ID == "" || thread.Model != p.model {
			a.terminate("failed", "Cloud returned a different or unknown model", true)
			return nil
		}
		a.record.ThreadID = thread.Thread.ID
		if err := a.save(nil, nil); err != nil {
			a.stop("Cloud thread binding could not be persisted")
			return nil
		}
		if len(p.history) > 0 {
			if !checkDispatch() {
				return nil
			}
			if _, err := a.rpc.Call(a.ctx, "thread/inject_items", map[string]any{"threadId": a.record.ThreadID, "items": cloudInjectionHistory(p)}); err != nil {
				a.terminateError(err, "Cloud rejected the supplied role or tool history", false)
				return nil
			}
			a.history = append(a.history, p.history...)
		}
	}
	turn := map[string]any{"threadId": a.record.ThreadID, "input": p.input, "environments": []any{}, "clientUserMessageId": uuid.NewString()}
	if p.effort != "" {
		turn["effort"] = p.effort
	}
	if p.summary != "" {
		turn["summary"] = p.summary
	}
	if !checkDispatch() {
		return nil
	}
	if p.outputSchema != nil {
		turn["outputSchema"] = p.outputSchema
	}
	a.record.Telemetry.UpstreamStartedAt = time.Now()
	result, err := a.rpc.Call(a.ctx, "turn/start", turn)
	if err != nil {
		a.terminateError(err, "Cloud turn acceptance is unknown; it will not be replayed", true)
		return nil
	}
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(result, &started) != nil || started.Turn.ID == "" {
		a.terminate("unknown", "Cloud turn identity was not confirmed", true)
		return nil
	}
	a.record.TurnID = started.Turn.ID
	a.history = append(a.history, p.inputMessage)
	if err := a.save(nil, nil); err != nil {
		a.stop("Cloud turn binding could not be persisted")
	}
	return nil
}

// Cached tool outputs are an idempotent downstream response, not another
// upstream turn. Rebuild the public SSE lifecycle from the immutable result so
// streaming and JSON retries both receive the same output without re-execution
// or another settlement. The replay has its own monotonic event sequence.
func (a *cloudSessionActor) replayCompletedResponse(requestID string, result *OpenAICloudResult) error {
	var response map[string]any
	if result == nil || json.Unmarshal(result.Response, &response) != nil {
		return errors.New("Cloud cached response is invalid")
	}
	status, _ := response["status"].(string)
	if status != "completed" && status != "failed" {
		return errors.New("Cloud cached response has no terminal downstream state")
	}
	outputs, ok := response["output"].([]any)
	if !ok {
		return errors.New("Cloud cached output is invalid")
	}
	copyObject := func(value map[string]any) map[string]any {
		copy := make(map[string]any, len(value))
		for key, item := range value {
			copy[key] = item
		}
		return copy
	}
	sequence := uint64(0)
	emit := func(kind string, fields map[string]any) error {
		fields["type"], fields["sequence_number"] = kind, sequence
		sequence++
		raw, err := json.Marshal(fields)
		if err != nil {
			return errors.New("Cloud cached event encoding failed")
		}
		return a.service.store.PublishCloudEvent(a.ctx, requestID, OpenAICloudStreamEvent{Event: raw})
	}
	started := copyObject(response)
	started["status"], started["output"], started["usage"], started["error"] = "in_progress", []any{}, nil, nil
	if err := emit("response.created", map[string]any{"response": started}); err != nil {
		return err
	}
	if err := emit("response.in_progress", map[string]any{"response": started}); err != nil {
		return err
	}
	for index, rawItem := range outputs {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return errors.New("Cloud cached output item is invalid")
		}
		initial := copyObject(item)
		initial["status"] = "in_progress"
		switch item["type"] {
		case "message":
			parts, ok := item["content"].([]any)
			if !ok {
				return errors.New("Cloud cached message is invalid")
			}
			initial["content"] = []any{}
			if err := emit("response.output_item.added", map[string]any{"output_index": index, "item": initial}); err != nil {
				return err
			}
			for contentIndex, rawPart := range parts {
				part, ok := rawPart.(map[string]any)
				if !ok || part["type"] != "output_text" {
					return errors.New("Cloud cached content is invalid")
				}
				text, ok := part["text"].(string)
				if !ok {
					return errors.New("Cloud cached text is invalid")
				}
				empty := copyObject(part)
				empty["text"] = ""
				fields := func() map[string]any {
					return map[string]any{"output_index": index, "item_id": item["id"], "content_index": contentIndex}
				}
				added := fields()
				added["part"] = empty
				if err := emit("response.content_part.added", added); err != nil {
					return err
				}
				if text != "" {
					delta := fields()
					delta["delta"] = text
					if err := emit("response.output_text.delta", delta); err != nil {
						return err
					}
				}
				done := fields()
				done["text"] = text
				if err := emit("response.output_text.done", done); err != nil {
					return err
				}
				partDone := fields()
				partDone["part"] = part
				if err := emit("response.content_part.done", partDone); err != nil {
					return err
				}
			}
		case "function_call":
			arguments, ok := item["arguments"].(string)
			if !ok {
				return errors.New("Cloud cached tool arguments are invalid")
			}
			initial["arguments"] = ""
			if err := emit("response.output_item.added", map[string]any{"output_index": index, "item": initial}); err != nil {
				return err
			}
			if arguments != "" {
				if err := emit("response.function_call_arguments.delta", map[string]any{"output_index": index, "item_id": item["id"], "delta": arguments}); err != nil {
					return err
				}
			}
			if err := emit("response.function_call_arguments.done", map[string]any{"output_index": index, "item_id": item["id"], "arguments": arguments}); err != nil {
				return err
			}
		case "reasoning", "compaction", "web_search_call", "image_generation_call":
			delete(initial, "encrypted_content")
			delete(initial, "result")
			if err := emit("response.output_item.added", map[string]any{"output_index": index, "item": initial}); err != nil {
				return err
			}
		default:
			return errors.New("Cloud cached output type is unsupported")
		}
		if err := emit("response.output_item.done", map[string]any{"output_index": index, "item": item}); err != nil {
			return err
		}
	}
	if err := emit("response."+status, map[string]any{"response": json.RawMessage(result.Response)}); err != nil {
		return err
	}
	return a.service.store.PublishCloudEvent(a.ctx, requestID, OpenAICloudStreamEvent{Result: result})
}

func (a *cloudSessionActor) checkHistory(p *cloudProtocolRequest) error {
	// Initial injected tool outputs have no live RPC. Full-history clients may
	// repeat them, but must not alter them or submit them as a new tool reply.
	knownOutputs := make(map[string]string)
	for _, raw := range a.history {
		if item, ok := raw.(map[string]any); ok && item["type"] == "function_call_output" {
			id, _ := item["call_id"].(string)
			encoded, _ := json.Marshal(item["output"])
			knownOutputs[id] = string(encoded)
		}
	}
	for id, output := range p.outputs {
		if a.tools[id] != nil {
			continue // Pending and submitted RPC results have their own digest checks.
		}
		if known, exists := knownOutputs[id]; !exists || known != output {
			return errors.New("Cloud continuation changed or added a historical tool output")
		}
		delete(p.outputs, id)
	}
	if len(p.history) == 0 {
		return nil
	}
	clean := func(items []any) []any {
		var out []any
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok || item["type"] == "function_call_output" {
				continue
			}
			copy := make(map[string]any)
			for key, v := range item {
				if key != "id" && key != "status" {
					copy[key] = v
				}
			}
			if copy["type"] == "message" && copy["role"] == "assistant" {
				if parts, ok := copy["content"].([]any); ok {
					normalized := make([]any, len(parts))
					for i, rawPart := range parts {
						normalized[i] = rawPart
						if part, ok := rawPart.(map[string]any); ok && part["type"] == "output_text" {
							partCopy := make(map[string]any, len(part))
							for key, value := range part {
								partCopy[key] = value
							}
							annotations, _ := part["annotations"].([]any)
							if part["annotations"] == nil || annotations != nil && len(annotations) == 0 {
								delete(partCopy, "annotations")
							}
							normalized[i] = partCopy
						}
					}
					copy["content"] = normalized
				}
			}
			out = append(out, copy)
		}
		return out
	}
	provided, known := clean(p.history), clean(a.history)
	if len(provided) > len(known) {
		return errors.New("Cloud continuation changed the existing history")
	}
	for i := range provided {
		x, _ := json.Marshal(provided[i])
		y, _ := json.Marshal(known[i])
		if string(x) != string(y) {
			return errors.New("Cloud continuation changed the existing history")
		}
	}
	return nil
}

func (a *cloudSessionActor) hasPendingTools() bool {
	for _, t := range a.tools {
		if !t.Submitted {
			return true
		}
	}
	return false
}

func (a *cloudSessionActor) emit(typ string, fields map[string]any) error {
	if a.requestID == "" {
		return nil
	}
	if typ == "response.output_text.delta" && a.record.Telemetry != nil && a.record.Telemetry.FirstTokenMs == nil {
		if delta, _ := fields["delta"].(string); delta != "" && !a.record.Telemetry.StartedAt.IsZero() {
			ms := int(time.Since(a.record.Telemetry.StartedAt).Milliseconds())
			a.record.Telemetry.FirstTokenMs = &ms
		}
	}
	fields["type"] = typ
	fields["sequence_number"] = a.sequence
	a.sequence++
	raw, err := json.Marshal(fields)
	if err != nil {
		return errors.New("Cloud response encoding failed")
	}
	if a.protocol != nil {
		raw = cloudRestorePayload(raw, a.protocol.names)
	}
	return a.service.store.PublishCloudEvent(a.ctx, a.requestID, OpenAICloudStreamEvent{Event: raw})
}

func (a *cloudSessionActor) response(status string, usage *OpenAICloudUsage) map[string]any {
	if a.rejectedBeforeInference {
		usage = nil
	}
	output := a.output
	if output == nil {
		output = []map[string]any{}
	}
	metadata := a.metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	response := map[string]any{"id": a.responseID, "object": "response", "created_at": time.Now().Unix(), "status": status, "model": a.record.Model, "output": output, "usage": cloudResponseUsage(usage), "error": nil, "metadata": metadata}
	if a.protocol != nil {
		response["parallel_tool_calls"] = !a.protocol.serialTools
		if text, ok := a.protocol.body["text"]; ok {
			response["text"] = text
		}
	}
	return response
}

func (a *cloudSessionActor) finishToolWait(now time.Time) {
	if !a.waitingSince.IsZero() {
		if a.record.Telemetry != nil {
			if elapsed := now.Sub(a.waitingSince).Milliseconds(); elapsed > 0 {
				a.record.Telemetry.WaitingToolsMs += elapsed
			}
		}
		a.waitingSince = time.Time{}
	}
}

func (a *cloudSessionActor) finishAwaiting() error {
	// Tool continuations may flush only the beginning of an interleaved message.
	// Keep this response open until every message finishes, just as a tool RPC
	// arriving during active text output does. This also covers multiple messages
	// completing separately without publishing an in-progress item as completed.
	for _, item := range a.output {
		if item["type"] == "message" && item["status"] != "completed" {
			a.record.Status = "awaiting_tools"
			return a.save(nil, nil)
		}
	}
	if err := a.flushHostedOutputs(); err != nil {
		return err
	}
	var refs []string
	pending := make([]*cloudPendingTool, 0, len(a.tools))
	for _, tool := range a.tools {
		if !tool.Submitted {
			pending = append(pending, tool)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Order < pending[j].Order })
	if a.protocol != nil && a.protocol.serialTools && len(pending) > 1 {
		pending = pending[:1]
	}
	for _, tool := range pending {
		if !tool.Published {
			tool.Published = true
			a.history = append(a.history, map[string]any{"type": "function_call", "call_id": tool.CallID, "name": tool.Name, "arguments": tool.Arguments})
		}
		item := map[string]any{"type": "function_call", "id": "fc_" + tool.CallID, "call_id": tool.CallID, "name": tool.Name, "arguments": "", "status": "in_progress"}
		index := len(a.output)
		a.output = append(a.output, item)
		if err := a.emit("response.output_item.added", map[string]any{"output_index": index, "item": item}); err != nil {
			return err
		}
		if err := a.emit("response.function_call_arguments.delta", map[string]any{"output_index": index, "item_id": item["id"], "delta": tool.Arguments}); err != nil {
			return err
		}
		item["arguments"] = tool.Arguments
		item["status"] = "completed"
		if err := a.emit("response.function_call_arguments.done", map[string]any{"output_index": index, "item_id": item["id"], "arguments": tool.Arguments}); err != nil {
			return err
		}
		if err := a.emit("response.output_item.done", map[string]any{"output_index": index, "item": item}); err != nil {
			return err
		}
		refs = append(refs, tool.CallID)
	}
	a.record.Status = "awaiting_tools"
	if a.waitingSince.IsZero() {
		a.waitingSince = time.Now()
	}
	return a.finish("completed", true, false, nil, refs)
}

func (a *cloudSessionActor) finish(status string, awaiting, terminal bool, usage *OpenAICloudUsage, refs []string) error {
	if status == "failed" {
		for _, item := range a.output {
			if item["status"] == "in_progress" {
				item["status"] = "incomplete"
			}
		}
	}
	body := a.response(status, usage)
	if status == "failed" {
		body["error"] = map[string]any{"code": "cloud_execution_failed", "message": a.record.Failure}
		if a.publicFailure != nil {
			body["error"] = a.publicFailure
		}
	}
	raw, _ := json.Marshal(body)
	if a.protocol != nil {
		raw = cloudRestorePayload(raw, a.protocol.names)
	}
	if len(raw) > OpenAICloudMaxResultBytes {
		if !terminal {
			return errors.New("Cloud response exceeded its size limit")
		}
		// Preserve the trusted terminal usage/settlement even when the public body
		// cannot fit. Never retry finish with the same oversized output forever.
		status = "failed"
		a.record.Status, a.record.Failure = "failed", "Cloud response exceeded its size limit"
		a.errorStatus = 502
		body["status"], body["output"], body["metadata"] = status, []any{}, map[string]any{}
		body["error"] = map[string]string{"code": "cloud_response_too_large", "message": a.record.Failure}
		raw, _ = json.Marshal(body)
	}
	result := &OpenAICloudResult{RejectedBeforeInference: a.rejectedBeforeInference && !a.uncertain && !a.turnProducedOutput, Response: raw, ErrorStatus: a.errorStatus, SessionID: a.record.SessionID, RootRequestID: a.record.RootRequestID, AwaitingTools: awaiting, Terminal: terminal, Usage: usage, UsageCredible: usage != nil && !awaiting && !a.uncertain, ThreadID: a.record.ThreadID, TurnID: a.record.TurnID}
	a.record.Result = result
	refs = append(refs, a.responseID)
	var settlement *OpenAICloudSettlement
	if terminal {
		a.finishToolWait(time.Now())
		settlement = a.settlement(a.record.Status, a.uncertain)
	}
	if err := a.save(refs, settlement); err != nil {
		return err
	}
	if err := a.emit("response."+status, map[string]any{"response": body}); err != nil {
		return err
	}
	if err := a.service.store.PublishCloudEvent(a.ctx, a.requestID, OpenAICloudStreamEvent{Result: result}); err != nil {
		return err
	}
	a.lastResponseID = a.responseID
	a.completed[a.requestDigest] = result
	a.retainedBytes += len(raw)
	a.requestID = ""
	if len(a.completed) > 128 || a.retainedBytes > 16<<20 {
		return errors.New("Cloud response history reached its limit")
	}
	return nil
}

func (a *cloudSessionActor) terminate(status, message string, unknown bool) {
	a.record.Status = status
	a.record.Failure = message
	a.uncertain = unknown
	if a.requestID != "" {
		if err := a.finish("failed", false, true, a.record.Usage, nil); err != nil {
			a.reject(a.requestID, "Cloud terminal state could not be persisted")
		}
	} else {
		a.finishToolWait(time.Now())
		_ = a.save(nil, a.settlement(status, unknown))
	}
}

func (a *cloudSessionActor) stop(message string) {
	// Interrupt is best-effort and never proof of termination. We retain the
	// last observed usage and an unknown settlement unless a terminal event is
	// actually consumed below before the bounded cleanup deadline.
	if a.rpc != nil && a.record.ThreadID != "" && a.record.TurnID != "" && (a.record.Status == "running" || a.record.Status == "awaiting_tools") {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
		_, _ = a.rpc.Call(ctx, "turn/interrupt", map[string]any{"threadId": a.record.ThreadID, "turnId": a.record.TurnID})
	collect:
		for {
			select {
			case event := <-a.upstream:
				if event.Method == "thread/tokenUsage/updated" || event.Method == "turn/completed" {
					_ = a.handleEvent(event)
					if a.record.Status == "completed" || a.record.Status == "failed" || a.record.Status == "interrupted" || a.record.Status == "unknown" {
						break collect
					}
				}
			case <-ctx.Done():
				break collect
			}
		}
		cancel()
	}
	if a.record.Status == "completed" || a.record.Status == "failed" || a.record.Status == "interrupted" || a.record.Status == "unknown" {
		return
	}
	a.terminate("unknown", message, true)
}

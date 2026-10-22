package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const siwcWebSocketURL = "wss://api.openai.com/v1/responses"

// A session owns one pool lease for one authenticated client connection. Its
// continuation proof contains only bounded digests, never another history copy.
type openAIWSSIWCSession struct {
	mu                   sync.Mutex
	ctx                  context.Context
	cancel               context.CancelFunc
	closed               atomic.Bool
	accountID            int64
	tenant, proxy, grant string
	bearer               [32]byte
	lease                *openAIWSConnLease
	active               *openAIWSSIWCSSEBody
	anchor               *openAIWSSIWCAnchor
}

type openAIWSSIWCAnchor struct {
	settings, history [32]byte
	items             int
	responseID        string
}

func newOpenAIWSSIWCSession(c *gin.Context, account *Account) *openAIWSSIWCSession {
	ctx, cancel := context.WithCancel(context.Background())
	session := &openAIWSSIWCSession{ctx: ctx, cancel: cancel, tenant: fmt.Sprintf("%s/key/%d", openAIWSTenantScope(c), getAPIKeyIDFromContext(c))}
	if account != nil {
		session.accountID = account.ID
		session.proxy = resolveAccountProxyURL(account)
	}
	return session
}

// close cancels preparation/acquisition before waiting for the state lock. A
// completed SIWC socket is evicted too: its authenticated context is not pooled
// across downstream connections, even for the same database account.
func (session *openAIWSSIWCSession) close() {
	if session == nil || session.closed.Swap(true) {
		return
	}
	session.cancel()
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.active != nil {
		session.active.cancel()
	}
	session.dropLeaseLocked()
}

func (session *openAIWSSIWCSession) dropLeaseLocked() {
	if session.lease != nil {
		session.lease.MarkBroken()
		session.lease.Release()
	}
	session.lease = nil
	session.anchor = nil
}

func (session *openAIWSSIWCSession) abortBody(body *openAIWSSIWCSSEBody) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.active == body {
		session.dropLeaseLocked()
	}
}

// finish is called by the existing ingress AFTER cancellation/output handling.
// A received completed frame alone is not proof that the turn was accepted.
func (session *openAIWSSIWCSession) finish(result *OpenAIForwardResult, err error) {
	if session == nil {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	body := session.active
	if body == nil {
		return
	}
	body.mu.Lock()
	candidate := body.candidate
	accepted := !session.closed.Load() && !body.failed && body.terminal && len(body.pending) == 0 && candidate != nil && err == nil && result != nil && !result.ClientDisconnect && !result.wsReplayIncomplete && result.UpstreamTerminalEvent == "response.completed" && result.RequestID == candidate.responseID
	body.mu.Unlock()
	body.cancel()
	session.active = nil
	if accepted && session.lease == body.lease {
		session.anchor = candidate
	} else {
		session.dropLeaseLocked()
	}
}

func (session *openAIWSSIWCSession) do(s *OpenAIGatewayService, request *http.Request, account *Account, proxy string) (response *http.Response, returnErr error) {
	if session == nil || s == nil || request == nil || request.URL == nil {
		return nil, errors.New("SIWC WebSocket transport is unavailable")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed.Load() {
		return nil, context.Canceled
	}
	if session.active != nil {
		return nil, errors.New("SIWC WebSocket turn is already active")
	}
	if account == nil || account.ID != session.accountID || !account.IsSIWCEnabled() || proxy != session.proxy || request.URL.String() != "https://api.openai.com/v1/responses" || request.Method != http.MethodPost {
		session.dropLeaseLocked()
		return nil, siwcRequired()
	}
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(session.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	defer func() {
		if response == nil || response.Proto != "websocket" {
			cleanup()
		}
		if returnErr != nil {
			session.dropLeaseLocked()
		}
	}()
	grant, ok := ctx.Value(siwcRequestGrantKey{}).(siwcRequestGrant)
	bearer := request.Header.Get("Authorization")
	if !ok || grant.accountID != account.ID || grant.identity == "" || grant.bearer != sha256.Sum256([]byte(bearer)) || !strings.HasPrefix(bearer, "Bearer ") {
		return nil, siwcRequired()
	}
	if session.grant != "" && session.grant != grant.identity {
		return nil, siwcRequired()
	}
	oauth := s.siwcOAuthService()
	if oauth == nil {
		return nil, siwcRequired()
	}
	validate := func(checkCtx context.Context) error {
		prepareCtx, stopPrepare := OpenAITurnWaitContext(selectionbudget.PreparationContext(checkCtx))
		defer stopPrepare()
		return validateSIWCInferenceGrant(prepareCtx, oauth, account, strings.TrimPrefix(bearer, "Bearer "), grant.identity)
	}
	if err := validate(ctx); err != nil {
		return nil, err
	}
	session.grant = grant.identity
	reader := request.Body
	if request.GetBody != nil {
		var err error
		reader, err = request.GetBody()
		if err != nil {
			return nil, err
		}
		defer reader.Close()
	}
	if reader == nil {
		return nil, errors.New("SIWC WebSocket request body is missing")
	}
	limit := ResolveOpenAIWSClientReadLimitBytes(s.cfg)
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_INVALID_REQUEST", "SIWC request exceeds the WebSocket size limit")
	}
	body, err = normalizeSIWCBody(body)
	if err != nil {
		return nil, err
	}
	if session.lease != nil && (session.bearer != grant.bearer || session.lease.conn.isClosed() || session.lease.conn.age(time.Now()) >= openAIWSConnMaxAge) {
		session.dropLeaseLocked()
	}
	reusedConn := session.lease != nil
	if session.lease == nil {
		headers := make(http.Header)
		headers.Set("Authorization", bearer)
		headers.Set("User-Agent", "Sub2API")
		waitCtx, stopWait := OpenAITurnWaitContext(ctx)
		acquireCtx, stopAcquire := context.WithTimeout(waitCtx, s.openAIWSAcquireTimeout())
		lease, acquireErr := s.getOpenAIWSConnPool().Acquire(acquireCtx, openAIWSAcquireRequest{
			TenantScope: session.tenant + "/siwc/" + grant.identity, Account: account,
			WSURL: siwcWebSocketURL, Headers: headers, ProxyURL: proxy, ForceNewConn: true,
			HeadersFactory: func(factoryCtx context.Context, headers http.Header) (http.Header, error) {
				if err := validate(factoryCtx); err != nil {
					return nil, err
				}
				return headers, nil
			},
		})
		stopAcquire()
		stopWait()
		if acquireErr != nil {
			var dialErr *openAIWSDialError
			if errors.As(acquireErr, &dialErr) && dialErr.StatusCode >= 400 {
				return &http.Response{StatusCode: dialErr.StatusCode, Header: cloneHeader(dialErr.ResponseHeaders), Body: io.NopCloser(bytes.NewReader(dialErr.ResponseBody)), Request: request}, nil
			}
			return nil, acquireErr
		}
		session.lease = lease
		session.grant = grant.identity
		session.bearer = grant.bearer
	}
	// Acquisition can wait. Revalidate the atomically captured grant at the
	// actual send boundary, including revoke/rebind while the dial was pending.
	wire, pending, fullBytes, err := siwcWebSocketPayload(body, session.anchor)
	if err != nil {
		return nil, err
	}
	if err := validate(ctx); err != nil {
		return nil, err
	}
	attempt, err := takeHTTPAccountTurnAttempt(request.WithContext(ctx), account, proxy, "siwc")
	if err != nil {
		return nil, err
	}
	// Reuse the request's shared timing record without HTTP trace callbacks:
	// the generation begins at the WS frame write, not its upgrade handshake.
	timing, _ := attempt.Context().Value(openAITransportTimingKey{}).(*openAIForwardTiming)
	if reusedConn {
		// A held session lease did not acquire again. Do not attribute its
		// original queue/pick durations or idle snapshot to each later turn.
		timing.ConnectionReady(true, 0, 0, -1, -1)
	} else {
		timing.ConnectionReady(false, session.lease.ConnPickDuration(), session.lease.QueueWaitDuration(), session.lease.AgeBefore(), session.lease.IdleBefore())
	}
	timing.SetRequestMetadata(wire)
	writeCtx, stopWrite := OpenAITurnWaitContext(attempt.Context())
	err = session.lease.WriteJSONWithContextTimeout(writeCtx, json.RawMessage(wire), s.openAIWSWriteTimeout())
	stopWrite()
	if err != nil {
		return nil, err
	} // No reconnect or HTTP fallback after any write.
	timing.MarkRequestWritten(int64(len(wire)))
	timing.MarkIncrementalPayloadWritten(int64(fullBytes), int64(len(wire)))
	stream := &openAIWSSIWCSSEBody{session: session, lease: session.lease, ctx: attempt.Context(), cancel: cleanup, input: pending, readTimeout: s.openAIWSReadTimeout(), maxFrame: defaultMaxLineSize}
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		stream.maxFrame = s.cfg.Gateway.MaxLineSize
	}
	session.active = stream
	return &http.Response{StatusCode: http.StatusSwitchingProtocols, Proto: "websocket", Header: session.lease.HandshakeHeaders(), Body: stream, Request: attempt}, nil
}

// Compare exact normalized settings and an ordered hash of canonical JSON
// input items. Retaining one count and two digests bounds memory independently
// of conversation length; the caller already owns the complete replay history.
func siwcWebSocketPayload(body []byte, anchor *openAIWSSIWCAnchor) ([]byte, *openAIWSSIWCAnchor, int, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, 0, err
	}
	var input []json.RawMessage
	if err := json.Unmarshal(payload["input"], &input); err != nil {
		return nil, nil, 0, err
	}
	delete(payload, "input")
	delete(payload, "previous_response_id")
	settings, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, 0, err
	}
	settings, err = normalizeOpenAIWSJSONForCompare(settings)
	if err != nil {
		return nil, nil, 0, err
	}
	summary := &openAIWSSIWCAnchor{settings: sha256.Sum256(settings), items: len(input)}
	prefix := [32]byte{}
	for i, item := range input {
		summary.history, err = siwcAppendJSONHash(summary.history, item)
		if err != nil {
			return nil, nil, 0, err
		}
		if anchor != nil && i+1 == anchor.items {
			prefix = summary.history
		}
	}
	// The full-input counterfactual has the identical WS envelope and settings.
	// Count only the changed input/parent members; do not serialize or retain a
	// second full request solely for telemetry.
	fullInputBytes, parentBytes := 0, 0
	if anchor != nil && anchor.responseID != "" && anchor.settings == summary.settings && len(input) >= anchor.items && prefix == anchor.history {
		fullInput, err := json.Marshal(input)
		if err != nil {
			return nil, nil, 0, err
		}
		fullInputBytes = len(fullInput)
		payload["previous_response_id"], _ = json.Marshal(anchor.responseID)
		parentBytes = len(`,"previous_response_id":`) + len(payload["previous_response_id"])
		input = input[anchor.items:]
	}
	payload["input"], err = json.Marshal(input)
	if err != nil {
		return nil, nil, 0, err
	}
	payload["type"] = json.RawMessage("\"response.create\"")
	delete(payload, "stream") // WebSocket generation always emits events.
	wire, err := json.Marshal(payload)
	fullBytes := len(wire)
	if parentBytes > 0 {
		fullBytes += fullInputBytes - len(payload["input"]) - parentBytes
	}
	return wire, summary, fullBytes, err
}

func siwcAppendJSONHash(previous [32]byte, raw []byte) ([32]byte, error) {
	canonical, err := normalizeOpenAIWSJSONForCompare(raw)
	if err != nil {
		return [32]byte{}, err
	}
	hash := sha256.New()
	_, _ = hash.Write(previous[:])
	_, _ = hash.Write(canonical)
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

// The adapter supplies framing only. Existing bridge parsing owns errors,
// billing, cancellation and whether a received terminal can become history.
type openAIWSSIWCSSEBody struct {
	mu                       sync.Mutex
	session                  *openAIWSSIWCSession
	lease                    *openAIWSConnLease
	ctx                      context.Context
	cancel                   context.CancelFunc
	input, candidate         *openAIWSSIWCAnchor
	pending                  []byte
	closed, terminal, failed bool
	readTimeout              time.Duration
	maxFrame                 int
}

func (body *openAIWSSIWCSSEBody) Read(p []byte) (int, error) {
	body.mu.Lock()
	defer body.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if body.closed {
		return 0, io.EOF
	}
	if len(body.pending) == 0 {
		if body.terminal {
			return 0, io.EOF
		}
		frame, err := body.lease.ReadMessageWithContextTimeout(body.ctx, body.readTimeout)
		if err != nil {
			body.failed = true
			return 0, err
		}
		if len(frame) > body.maxFrame {
			body.failed = true
			return 0, errors.New("SIWC WebSocket event exceeds the stream size limit")
		}
		var compact bytes.Buffer
		if err = json.Compact(&compact, frame); err != nil {
			body.failed = true
			return 0, err
		}
		frame = compact.Bytes()
		event := gjson.GetBytes(frame, "type").String()
		// A bare error may be followed by an authoritative failed/completed
		// response containing usage. The existing bridge owns its idle timeout.
		body.terminal = isOpenAIWSTerminalEvent(event)
		if event == "response.completed" && gjson.GetBytes(frame, "response.status").String() == "completed" {
			id := gjson.GetBytes(frame, "response.id").String()
			output := gjson.GetBytes(frame, "response.output")
			if strings.HasPrefix(id, "resp_") && len(id) <= 256 && output.IsArray() {
				candidate := *body.input
				candidate.responseID = id
				valid := true
				for _, item := range output.Array() {
					if !item.IsObject() || strings.TrimSpace(item.Get("type").String()) == "" {
						valid = false
						break
					}
					candidate.history, err = siwcAppendJSONHash(candidate.history, []byte(item.Raw))
					if err != nil {
						valid = false
						break
					}
					candidate.items++
				}
				if valid {
					body.candidate = &candidate
				}
			}
		}
		body.pending = append(append([]byte("data: "), frame...), '\n', '\n')
	}
	n := copy(p, body.pending)
	body.pending = body.pending[n:]
	return n, nil
}

func (body *openAIWSSIWCSSEBody) Close() error {
	externallyCancelled := body.ctx.Err() != nil
	body.cancel() // Wake a Read before waiting for its state lock.
	body.mu.Lock()
	bad := body.failed || !body.terminal || len(body.pending) > 0 || (!body.closed && externallyCancelled)
	body.closed = true
	if bad {
		body.failed = true
	}
	body.mu.Unlock()
	if bad {
		body.session.abortBody(body)
	}
	return nil
}

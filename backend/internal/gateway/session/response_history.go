package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

var ErrContinuationUnavailable = errors.New("cross-stream continuation is unavailable for this account or credential generation")

// ContinuationIdentity is the native WS contract's complete binding. The
// credential fingerprint comes from the existing full credential generation
// calculation; an access-token-only or account-only digest is insufficient.
// Group zero retains its current meaning; a nonzero account is required before
// publication. None of these values replace current access/lease validation.
type ContinuationIdentity struct {
	AccountID   int64
	APIKeyID    int64
	GroupID     int64
	Credentials [32]byte
}

// ContinuationSnapshot owns its input bytes. It never contains another lane's
// live socket or turn state. Unknown history is not permission to resume.
type ContinuationSnapshot struct {
	Known       bool
	CrossStream bool
	Input       []json.RawMessage
}

type responseHistoryEntry struct {
	stream     string
	identity   ContinuationIdentity
	input      []json.RawMessage
	size       int64
	ready      chan struct{}
	publishing bool
	resolved   bool
	committed  bool
}

// Publication is an opaque, single-use local owner proof for a downstream
// terminal write. Zero is invalid. Retaining it cannot keep an evicted entry
// authoritative; Finish also compares the exact current record.
type Publication struct {
	store      *ResponseHistory
	responseID string
	entry      *responseHistoryEntry
}

// ResponseHistory is owned by one physical connection. It replaces that
// connection's fork map, not its FIFO queues, readers, write gate or scheduler.
// Limits come from the existing ingress limits. There is no shared persistence,
// owner-recovery loop or cross-connection lookup. Close releases held history
// and wakes pending lookups; no goroutines are started here.
type ResponseHistory struct {
	mu         sync.Mutex
	entries    map[string]*responseHistoryEntry
	order      []string
	bytes      int64
	maxEntries int
	maxBytes   int64
	closed     bool
}

func NewResponseHistory(maxEntries int, maxBytes int64) *ResponseHistory {
	return &ResponseHistory{entries: make(map[string]*responseHistoryEntry), maxEntries: maxEntries, maxBytes: maxBytes}
}

// Observe records the source lane before writing an event. Seeing an ID alone
// never makes its input recoverable. The default stream name may be empty.
func (h *ResponseHistory) Observe(responseID, stream string) {
	if h == nil || responseID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.maxEntries <= 0 || h.maxBytes <= 0 || h.entries[responseID] != nil {
		return
	}
	h.evict(0, true)
	if len(h.entries) >= h.maxEntries {
		return
	}
	h.entries[responseID] = &responseHistoryEntry{stream: stream, ready: make(chan struct{})}
	h.order = append(h.order, responseID)
}

// BeginPublication must precede writing a completed/done frame. Competing
// lookups wait for its actual delivery result; terminal spelling is interpreted
// by protocol/ingress, not duplicated in this state owner.
func (h *ResponseHistory) BeginPublication(responseID, stream string) (Publication, bool) {
	if h == nil {
		return Publication{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.entries[responseID]
	if h.closed || e == nil || e.stream != stream || e.resolved || e.publishing {
		return Publication{}, false
	}
	e.publishing = true
	return Publication{store: h, responseID: responseID, entry: e}, true
}

// Finish publishes only after a successful downstream terminal write AND a
// complete, bounded replay was proven by the protocol collector. Set delivered
// false on write error/cancellation, or complete false on partial/tool-pending
// history. Both wake waiters without permitting a fork. The bytes are copied
// at publication and again on lookup; neither side can mutate stored history.
func (p Publication) Finish(identity ContinuationIdentity, input []json.RawMessage, delivered, complete bool) bool {
	h := p.store
	if h == nil || p.entry == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.entries[p.responseID]
	if h.closed || e != p.entry || !e.publishing || e.resolved {
		return false
	}
	if delivered && complete && identity.AccountID != 0 {
		size := int64(0)
		for _, item := range input {
			size += int64(len(item))
		}
		if size <= h.maxBytes {
			h.evict(size, false)
			if h.bytes+size <= h.maxBytes {
				e.input = cloneHistoryInput(input)
				e.size = size
				e.identity = identity
				e.committed = true
				h.bytes += size
			}
		}
	}
	e.publishing = false
	e.resolved = true
	close(e.ready)
	return e.committed
}

// Resolve permits a cross-lane fork only from successfully delivered complete
// history with identical Key/Group/account/full credentials. Same-lane misses
// remain unknown for the existing native continuation handler to validate.
// A pending write can be canceled by the request or by the target lane closing.
func (h *ResponseHistory) Resolve(ctx context.Context, closed <-chan struct{}, responseID, stream string, identity ContinuationIdentity) (ContinuationSnapshot, error) {
	if h == nil {
		return ContinuationSnapshot{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return ContinuationSnapshot{}, err
		}
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return ContinuationSnapshot{}, io.EOF
		}
		e := h.entries[responseID]
		if e == nil {
			h.mu.Unlock()
			return ContinuationSnapshot{}, nil
		}
		if e.publishing {
			ready := e.ready
			h.mu.Unlock()
			select {
			case <-ready:
				continue
			case <-ctx.Done():
				return ContinuationSnapshot{}, ctx.Err()
			case <-closed:
				return ContinuationSnapshot{}, io.EOF
			}
		}
		cross := e.stream != stream
		known := e.committed && e.identity == identity
		if cross && !known {
			h.mu.Unlock()
			return ContinuationSnapshot{}, ErrContinuationUnavailable
		}
		snapshot := ContinuationSnapshot{Known: known, CrossStream: cross}
		if known {
			snapshot.Input = cloneHistoryInput(e.input)
		}
		h.mu.Unlock()
		return snapshot, nil
	}
}

func (h *ResponseHistory) evict(extra int64, adding bool) {
	scanned := 0
	for len(h.order) > 0 && scanned < len(h.order) && (adding && len(h.entries) >= h.maxEntries || h.bytes+extra > h.maxBytes) {
		id := h.order[0]
		h.order = h.order[1:]
		if old := h.entries[id]; old != nil {
			if old.publishing {
				h.order = append(h.order, id)
				scanned++
				continue
			}
			h.bytes -= old.size
			delete(h.entries, id)
		}
	}
}

func (h *ResponseHistory) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for _, e := range h.entries {
		if !e.resolved {
			close(e.ready)
			e.resolved = true
			e.publishing = false
		}
		e.input = nil
	}
	h.entries = nil
	h.order = nil
	h.bytes = 0
}

func cloneHistoryInput(input []json.RawMessage) []json.RawMessage {
	if input == nil {
		return nil
	}
	result := make([]json.RawMessage, len(input))
	for i, item := range input {
		result[i] = append(json.RawMessage(nil), item...)
	}
	return result
}

package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Observation and HTTP-to-WS continuation share bounded, instance-local digests.
// No plaintext requests, outputs or credentials are retained between requests.
const (
	openAIIncrementalTTL         = 5 * time.Minute
	openAIIncrementalMaxEntries  = 1024
	openAIIncrementalMaxItems    = 2048
	openAIIncrementalMaxBody     = 2 << 20
	openAIIncrementalMaxMemory   = 16 << 20
	openAIIncrementalSlotBytes   = 512 // includes conservative map/object overhead
	openAIIncrementalItemBytes   = 64
	openAIIncrementalMaxIdentity = 4096
)

// Nonblocking observation admission bounds concurrent expensive snapshots.
// A saturated observer is unknown evidence, never gateway backpressure.
const openAIIncrementalParseBudget = 8 << 20

var openAIIncrementalParsing atomic.Int64

func acquireOpenAIIncrementalParsing(size int) bool {
	for used := openAIIncrementalParsing.Load(); ; used = openAIIncrementalParsing.Load() {
		if used+int64(size) > openAIIncrementalParseBudget {
			return false
		}
		if openAIIncrementalParsing.CompareAndSwap(used, used+int64(size)) {
			return true
		}
	}
}

var openAIIncrementalSecret = func() [32]byte {
	var secret [32]byte
	_, _ = rand.Read(secret[:])
	return secret
}()

// ExecutionScope must come from resolveOpenAIWSExecutionScope on the ORIGINAL
// authenticated request, before generated metadata; never use session affinity
// or prompt_cache_key/content fallback. CredentialGeneration must reflect the
// effective credential (the final bearer is acceptable). SettingsIdentity covers
// proxy/transport/configuration. Identity values are HMACed, not logged/stored.
// ConnectionReady means an actually held healthy lease, not account affinity.
type openAIIncrementalObservationOptions struct {
	ExecutionScope, WindowID, Endpoint, CredentialGeneration, SettingsIdentity string
	ConnID                                                                     string
	ConnectionReady                                                            bool
	Compaction                                                                 bool // invalidate the authenticated scope through this same begin lifecycle
}

// Semantic bytes sum the CURRENT wire item's JSON lengths, excluding separators,
// envelopes and compression. These are candidates, NOT actual bytes saved.
// No caller-controlled labels, identifiers or hashes may enter this report.
type openAIIncrementalObservationReport struct {
	Reason                 string `json:"reason"`
	AnchorReason           string `json:"anchor_reason"`
	PrefixMatch            string `json:"prefix_match"`
	StateReady             string `json:"state_ready"`
	Candidate              bool   `json:"candidate"`
	Assessable             bool   `json:"assessable"`
	InputItems             int    `json:"input_items"`
	CandidateItems         int    `json:"candidate_items"`
	InputSemanticBytes     int64  `json:"input_semantic_bytes"`
	CandidateSemanticBytes int64  `json:"candidate_semantic_bytes"`
}
type openAIIncrementalItem struct {
	digest [32]byte
	bytes  int64
}
type openAIIncrementalAnchor struct {
	boundary, settings  [32]byte
	items               []openAIIncrementalItem
	responseID, connID  string
	connWriteGeneration uint64
}
type openAIIncrementalPending struct {
	boundary, settings    [32]byte
	requestDigest         [32]byte
	input, output         []openAIIncrementalItem
	parentResponseID      string
	parentConnID          string
	prefixItems           int
	parentWriteGeneration uint64
	connWriteGeneration   uint64
	responseID            string
	completed             bool
	outputReason          string
	maxOutputIndex        int
	observedIDs           map[int][32]byte
	observedResponse      [32]byte
}
type openAIIncrementalSlot struct {
	generation uint64
	expires    time.Time
	active     int
	concurrent bool
	anchor     *openAIIncrementalAnchor
	pending    *openAIIncrementalPending
	bytes      int
}

// Handles do not own digests. Eviction/TTL can free an active observation, and a
// late handle cannot restore it or overwrite the replacement generation.
type openAIIncrementalObservation struct {
	mu         sync.Mutex
	store      *defaultOpenAIWSStateStore
	key        [32]byte
	generation uint64
	report     openAIIncrementalObservationReport
	finished   bool
}

func newOpenAIIncrementalReport(reason string) openAIIncrementalObservationReport {
	return openAIIncrementalObservationReport{Reason: reason, AnchorReason: "not_observed", PrefixMatch: "unknown", StateReady: "unknown"}
}
func openAIIncrementalHash(domain string, parts ...[]byte) [32]byte {
	mac := hmac.New(sha256.New, openAIIncrementalSecret[:])
	mac.Write([]byte(domain))
	var size [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		mac.Write(size[:])
		mac.Write(part)
	}
	var sum [32]byte
	copy(sum[:], mac.Sum(nil))
	return sum
}

func (s *OpenAIGatewayService) beginOpenAIIncrementalObservation(c *gin.Context, account *Account, finalBody []byte, opts openAIIncrementalObservationOptions) *openAIIncrementalObservation {
	t := &openAIIncrementalObservation{report: newOpenAIIncrementalReport("missing_identity")}
	t.report.Candidate = true
	// Classification precedes identity: excluded traffic never inflates candidate
	// coverage when authentication or an explicit execution scope is unavailable.
	if opts.Compaction {
		t.report.Reason, t.report.Candidate = "compact", false
	} else if reason := openAIIncrementalRequestClass(finalBody); reason != "" {
		t.report.Reason, t.report.Candidate = reason, false
	}
	if s == nil || c == nil || c.Request == nil {
		return t
	}
	raw, _ := c.Get("api_key")
	key, _ := raw.(*APIKey)
	if key == nil || key.ID <= 0 || key.UserID <= 0 {
		return t
	}
	if strings.TrimSpace(opts.ExecutionScope) == "" {
		if t.report.Candidate {
			t.report.Reason = "missing_scope"
		}
		return t
	}
	if len(opts.ExecutionScope) > openAIIncrementalMaxIdentity || !utf8.ValidString(opts.ExecutionScope) {
		if t.report.Candidate {
			t.report.Reason = "invalid_identity"
		}
		return t
	}
	groupID := int64(0)
	if key.GroupID != nil {
		groupID = *key.GroupID
	}
	if key.GroupID != nil && groupID <= 0 {
		return t
	}
	identity, _ := json.Marshal([]any{key.ID, key.UserID, groupID, opts.ExecutionScope})
	t.key = openAIIncrementalHash("scope", identity)
	var pending *openAIIncrementalPending
	reason, candidate := t.report.Reason, t.report.Candidate
	if !opts.Compaction {
		if account == nil {
			return t
		}
		owner := codexAccountIdentitySource(c, account)
		if account.ID <= 0 || owner == nil || owner.ID <= 0 {
			return t
		}
		if opts.Endpoint == "" || opts.CredentialGeneration == "" || opts.SettingsIdentity == "" {
			if t.report.Candidate {
				t.report.Reason = "missing_boundary"
			}
			return t
		}
		for _, value := range []string{opts.ExecutionScope, opts.WindowID, opts.Endpoint, opts.CredentialGeneration, opts.SettingsIdentity, opts.ConnID} {
			if !utf8.ValidString(value) {
				if t.report.Candidate {
					t.report.Reason = "invalid_identity"
				}
				return t
			}
			if len(value) > openAIIncrementalMaxIdentity {
				if t.report.Candidate {
					t.report.Reason = "identity_limit"
				}
				return t
			}
		}
		endpoint, err := url.Parse(opts.Endpoint)
		if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https" && endpoint.Scheme != "ws" && endpoint.Scheme != "wss") {
			if t.report.Candidate {
				t.report.Reason = "missing_boundary"
			}
			return t
		}
		namespace := codexAccountIdentityNamespace(owner)
		if !utf8.ValidString(namespace) {
			if t.report.Candidate {
				t.report.Reason = "invalid_identity"
			}
			return t
		}
		if len(namespace) > openAIIncrementalMaxIdentity {
			if t.report.Candidate {
				t.report.Reason = "identity_limit"
			}
			return t
		}
		boundary, _ := json.Marshal([]any{account.ID, owner.ID, namespace, nativeOpenAIUsageAuthorizationMode(account), opts.CredentialGeneration, opts.Endpoint, opts.SettingsIdentity, opts.WindowID})
		if candidate {
			pending, reason, candidate = openAIIncrementalRequest(finalBody)
		}
		if pending != nil {
			pending.boundary = openAIIncrementalHash("boundary", boundary)
		}
	}
	t.report = newOpenAIIncrementalReport(reason)
	t.report.Candidate = candidate
	if pending != nil {
		t.report.InputItems = len(pending.input)
		for _, item := range pending.input {
			t.report.InputSemanticBytes += item.bytes
		}
	}
	store, ok := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
	if !ok {
		t.report.Reason = "store_unavailable"
		return t
	}
	store.incrementalMu.Lock()
	defer store.incrementalMu.Unlock()
	now := time.Now()
	previous := store.incremental[t.key]
	expired := previous != nil && !now.Before(previous.expires)
	if expired {
		store.deleteIncrementalSlot(t.key)
		previous = nil
	}
	if previous != nil && previous.active > 0 {
		previous.active++
		previous.concurrent = true
		store.incrementalBytes -= previous.bytes - openAIIncrementalSlotBytes
		previous.bytes = openAIIncrementalSlotBytes
		previous.anchor, previous.pending = nil, nil
		t.store, t.generation = store, previous.generation
		if t.report.Candidate {
			t.report.Reason = "concurrent"
		}
		return t
	}
	if previous == nil {
		if store.incremental == nil {
			store.incremental = make(map[[32]byte]*openAIIncrementalSlot)
		}
		store.trimIncrementalState(now, t.key, openAIIncrementalSlotBytes)
		previous = &openAIIncrementalSlot{bytes: openAIIncrementalSlotBytes}
		store.incremental[t.key] = previous
		store.incrementalBytes += previous.bytes
	}
	store.incrementalGeneration++
	previous.generation, previous.active = store.incrementalGeneration, 1
	previous.expires = now.Add(openAIIncrementalTTL)
	t.store, t.generation = store, previous.generation
	if pending == nil {
		store.incrementalBytes -= previous.bytes - openAIIncrementalSlotBytes
		previous.bytes, previous.anchor = openAIIncrementalSlotBytes, nil
		return t
	}
	if expired {
		t.report.Reason = "expired"
	}
	if anchor := previous.anchor; anchor != nil {
		switch {
		case anchor.boundary != pending.boundary:
			t.report.Reason = "boundary_changed"
		case anchor.settings != pending.settings:
			t.report.Reason = "settings_changed"
			t.report.Assessable, t.report.PrefixMatch = true, "false"
		default:
			t.report.Assessable = true
			t.report.PrefixMatch = "false"
			t.report.Reason = "input_shortened"
			if len(pending.input) >= len(anchor.items) {
				t.report.Reason = "input_mismatch"
				match := true
				for i, item := range anchor.items {
					if item.digest != pending.input[i].digest {
						match = false
						break
					}
				}
				if match {
					t.report.Reason, t.report.PrefixMatch = "prefix_match", "true"
					t.report.CandidateItems = len(anchor.items)
					for _, item := range pending.input[:len(anchor.items)] {
						t.report.CandidateSemanticBytes += item.bytes
					}
					if opts.ConnID != "" && opts.WindowID != "" && opts.ConnectionReady {
						t.report.StateReady = "false"
						if conn, found := store.GetResponseConn(anchor.responseID); found && conn == opts.ConnID && conn == anchor.connID {
							t.report.StateReady = "true"
							pending.parentResponseID, pending.parentConnID = anchor.responseID, conn
							pending.prefixItems = len(anchor.items)
							pending.parentWriteGeneration = anchor.connWriteGeneration
						}
					}
				}
			}
		}
	}
	// Consume the anchor: failure/cancel must not expose an earlier stale turn.
	store.incrementalBytes -= previous.bytes - openAIIncrementalSlotBytes
	previous.bytes, previous.anchor = openAIIncrementalSlotBytes, nil
	reserve := 3*openAIIncrementalMaxItems*openAIIncrementalItemBytes + 3*openAIIncrementalMaxIdentity
	store.trimIncrementalState(now, t.key, reserve)
	if store.incrementalBytes+reserve > openAIIncrementalMaxMemory {
		t.report = newOpenAIIncrementalReport("memory_limit")
		t.report.Candidate = candidate
		return t
	}
	previous.pending = pending
	previous.bytes += reserve
	store.incrementalBytes += reserve
	return t
}

// wsPayload selects a suffix only for the exact observed full request while its
// exclusive lease still owns the parent. Begin consumed that parent already;
// concurrent/late turns cannot publish another anchor. Keep the full request in
// the caller for existing recovery, and the full input digests for the next turn.
func (t *openAIIncrementalObservation) wsPayload(ctx context.Context, full []byte, lease *openAIWSConnLease) []byte {
	if t == nil || t.store == nil || ctx == nil || ctx.Err() != nil || lease == nil {
		return full
	}
	if !acquireOpenAIIncrementalParsing(len(full)) {
		return full
	}
	defer openAIIncrementalParsing.Add(-int64(len(full)))
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.store
	valid := func(p *openAIIncrementalPending) bool {
		slot := s.incremental[t.key]
		if t.finished || ctx.Err() != nil || slot == nil || slot.generation != t.generation ||
			slot.active != 1 || slot.concurrent || slot.pending != p || p == nil ||
			!time.Now().Before(slot.expires) || p.parentResponseID == "" || p.prefixItems <= 0 {
			return false
		}
		conn, err := lease.activeConn()
		if err != nil || conn.isClosed() || conn.isUnusable() || conn.age(time.Now()) >= openAIWSConnMaxAge || lease.ConnID() != p.parentConnID {
			return false
		}
		if p.parentWriteGeneration == 0 || conn.writeGeneration.Load() != p.parentWriteGeneration {
			t.report.StateReady = "false"
			return false
		}
		bound, ok := s.GetResponseConn(p.parentResponseID)
		return ok && bound == p.parentConnID
	}
	s.incrementalMu.Lock()
	slot := s.incremental[t.key]
	var pending *openAIIncrementalPending
	if slot != nil {
		pending = slot.pending
	}
	ready := valid(pending)
	s.incrementalMu.Unlock()
	if !ready || pending.requestDigest != openAIIncrementalHash("request", full) {
		return full
	}
	items := gjson.GetBytes(full, "input").Array()
	if pending.prefixItems > len(items) {
		return full
	}
	suffix := make([]json.RawMessage, 0, len(items)-pending.prefixItems)
	for _, item := range items[pending.prefixItems:] {
		suffix = append(suffix, json.RawMessage(item.Raw))
	}
	input, err := json.Marshal(suffix)
	if err != nil {
		return full
	}
	wire, err := sjson.SetRawBytes(full, "input", input)
	if err != nil {
		return full
	}
	wire, err = sjson.SetBytes(wire, "previous_response_id", pending.parentResponseID)
	if err != nil || len(wire) >= len(full) {
		return full
	}
	// Encoding does not hold the shared state lock. Revalidate after it, at the
	// send decision; the held lease prevents another generation on this socket.
	s.incrementalMu.Lock()
	ready = valid(pending)
	s.incrementalMu.Unlock()
	if !ready {
		return full
	}
	return wire
}

// Record only a successful real write, while the caller still holds its lease.
// All pooled writers advance the same counter, even if they have no observer.
func (t *openAIIncrementalObservation) markWSRequestWritten(lease *openAIWSConnLease) {
	if t == nil || t.store == nil || lease == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	conn, err := lease.activeConn()
	if err != nil || t.finished {
		return
	}
	t.store.incrementalMu.Lock()
	defer t.store.incrementalMu.Unlock()
	if slot := t.store.incremental[t.key]; slot != nil && slot.generation == t.generation && !slot.concurrent && slot.pending != nil {
		slot.pending.connWriteGeneration = conn.writeGeneration.Load()
	}
}

// No new public interface, Redis operations or background worker.
func (s *defaultOpenAIWSStateStore) deleteIncrementalSlot(key [32]byte) {
	if slot := s.incremental[key]; slot != nil {
		s.incrementalBytes -= slot.bytes
		delete(s.incremental, key)
	}
}
func (s *defaultOpenAIWSStateStore) trimIncrementalState(now time.Time, keep [32]byte, extra int) {
	for key, slot := range s.incremental {
		if key != keep && !now.Before(slot.expires) {
			s.deleteIncrementalSlot(key)
		}
	}
	limit := openAIIncrementalMaxEntries
	if s.incremental[keep] == nil {
		limit--
	}
	for len(s.incremental) > limit || s.incrementalBytes+extra > openAIIncrementalMaxMemory {
		var oldestKey [32]byte
		var oldest *openAIIncrementalSlot
		for key, slot := range s.incremental {
			if key != keep && (oldest == nil || slot.expires.Before(oldest.expires)) {
				oldestKey, oldest = key, slot
			}
		}
		if oldest == nil {
			break
		}
		s.deleteIncrementalSlot(oldestKey)
	}
}

// ObserveEvent consumes the upstream representation matching final normalized
// input. A complete terminal output array is mandatory; done/delta alone cannot
// establish the complete order. Parsing/hashing never holds the store lock.
func (t *openAIIncrementalObservation) ObserveEvent(payload []byte) {
	t.observe(payload, false)
}

// ObserveResponse consumes a buffered Responses JSON body directly. It uses
// the same completion reader as SSE without fabricating an event envelope.
func (t *openAIIncrementalObservation) ObserveResponse(body []byte) {
	t.observe(body, true)
}

func (t *openAIIncrementalObservation) observe(payload []byte, responseBody bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished || t.store == nil {
		return
	}
	s := t.store
	// A rejected/evicted observation costs no further parsing per token. The
	// store may change while parsing, so recheck the generation after reading.
	s.incrementalMu.Lock()
	slot := s.incremental[t.key]
	active := slot != nil && slot.generation == t.generation && !slot.concurrent && slot.pending != nil && slot.pending.outputReason == "" && time.Now().Before(slot.expires)
	s.incrementalMu.Unlock()
	if !active {
		return
	}
	event := openAIIncrementalReadEvent(payload, responseBody)
	s.incrementalMu.Lock()
	defer s.incrementalMu.Unlock()
	slot = s.incremental[t.key]
	if slot == nil || slot.generation != t.generation || slot.concurrent || slot.pending == nil || !time.Now().Before(slot.expires) {
		return
	}
	p := slot.pending
	if p.outputReason != "" {
		return
	}
	if p.completed {
		p.outputReason = "event_after_completed"
		return
	}
	if event.reason != "" {
		p.outputReason = event.reason
		return
	}
	if event.responseIdentity != ([32]byte{}) {
		if p.observedResponse != ([32]byte{}) && p.observedResponse != event.responseIdentity {
			p.outputReason = "response_changed"
			return
		}
		p.observedResponse = event.responseIdentity
	}
	if event.index >= 0 {
		if p.observedIDs == nil {
			p.observedIDs = make(map[int][32]byte)
		}
		if id, known := p.observedIDs[event.index]; known && id != event.itemID {
			p.outputReason = "output_incomplete"
			return
		}
		p.observedIDs[event.index] = event.itemID
	}
	if event.index > p.maxOutputIndex {
		p.maxOutputIndex = event.index
	}
	if event.completed {
		if len(event.output)+len(p.input) > openAIIncrementalMaxItems {
			p.outputReason = "item_limit"
			return
		}
		if len(event.output) <= p.maxOutputIndex {
			p.outputReason = "output_incomplete"
			return
		}
		for index, id := range p.observedIDs {
			if event.outputIDs[index] != id {
				p.outputReason = "output_incomplete"
				return
			}
		}
		p.output, p.responseID, p.completed = event.output, event.responseID, true
	}
}

type openAIIncrementalEvent struct {
	reason, responseID string
	index              int
	completed          bool
	output             []openAIIncrementalItem
	itemID             [32]byte
	responseIdentity   [32]byte
	outputIDs          [][32]byte
}

func openAIIncrementalDelta(kind string) bool {
	switch kind {
	case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta",
		"response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		return true
	}
	return false
}

// Deltas are ordering/ownership evidence only; their text is NEVER part of H.
// Validate the flat envelope and bounded identities, not the token's Unicode or
// canonical form. The complete terminal snapshot receives the strict recursive
// duplicate/Unicode/shape checks. Unknown/nested delta extensions fail closed.
func openAIIncrementalDeltaEnvelope(root gjson.Result) openAIIncrementalEvent {
	e := openAIIncrementalEvent{index: -1}
	if !root.IsObject() {
		e.reason = "invalid_json"
		return e
	}
	if hasDuplicateJSONObjectKeys(root) {
		e.reason = "duplicate_json"
		return e
	}
	hasDelta, hasID := false, false
	root.ForEach(func(key, value gjson.Result) bool {
		field := key.String()
		switch field {
		case "type", "delta", "obfuscation":
			if value.Type != gjson.String {
				e.reason = "unknown_shape"
			}
			if field == "delta" {
				hasDelta = true
			}
		case "item_id", "response_id":
			if value.Type != gjson.String || len(value.Raw) > 6*openAIIncrementalMaxIdentity+2 || len(value.String()) > openAIIncrementalMaxIdentity || strings.TrimSpace(value.String()) == "" {
				e.reason = "output_incomplete"
				break
			}
			if !openAIIncrementalUnicode([]byte(value.Raw)) {
				e.reason = "invalid_json"
				break
			}
			if field == "item_id" {
				e.itemID, hasID = openAIIncrementalHash("output-id", []byte(value.String())), true
			} else {
				e.responseIdentity = openAIIncrementalHash("response-id", []byte(value.String()))
			}
		case "output_index", "content_index", "summary_index", "sequence_number":
			if value.Type != gjson.Number || value.Int() < 0 || value.Raw != strconv.FormatInt(value.Int(), 10) {
				e.reason = "unknown_shape"
				break
			}
			if field == "output_index" {
				if value.Int() >= openAIIncrementalMaxItems {
					e.reason = "output_incomplete"
				} else {
					e.index = int(value.Int())
				}
			}
		case "logprobs":
			if !value.IsArray() || strings.TrimSpace(value.Raw[1:len(value.Raw)-1]) != "" {
				e.reason = "unknown_shape"
			}
		default:
			e.reason = "unknown_field"
		}
		return e.reason == ""
	})
	if e.reason == "" {
		if !hasDelta {
			e.reason = "unknown_shape"
		} else if !hasID || e.index < 0 {
			e.reason = "output_incomplete"
		}
	}
	return e
}

func openAIIncrementalReadEvent(payload []byte, responseBody bool) openAIIncrementalEvent {
	e := openAIIncrementalEvent{index: -1}
	if len(payload) > openAIIncrementalMaxBody {
		e.reason = "body_limit"
		return e
	}
	if !gjson.ValidBytes(payload) {
		e.reason = "invalid_json"
		return e
	}
	root := gjson.ParseBytes(payload)
	kind := root.Get("type").String()
	if !responseBody && openAIIncrementalDelta(kind) {
		return openAIIncrementalDeltaEnvelope(root)
	}
	if !acquireOpenAIIncrementalParsing(len(payload)) {
		e.reason = "observation_busy"
		return e
	}
	defer openAIIncrementalParsing.Add(-int64(len(payload)))
	_, e.reason = openAIIncrementalJSON(payload)
	if e.reason != "" {
		return e
	}
	paths := []string{"response_id", "response.id"}
	if responseBody {
		kind, paths = "response.completed", []string{"id"}
	}
	for _, path := range paths {
		if id := root.Get(path); id.Exists() {
			if id.Type != gjson.String || strings.TrimSpace(id.String()) == "" || len(id.String()) > openAIIncrementalMaxIdentity {
				e.reason = "output_incomplete"
				return e
			}
			digest := openAIIncrementalHash("response-id", []byte(id.String()))
			if e.responseIdentity != ([32]byte{}) && digest != e.responseIdentity {
				e.reason = "response_changed"
				return e
			}
			e.responseIdentity = digest
		}
	}
	switch kind {
	case "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "error":
		e.reason = "unsuccessful_terminal"
	case "response.completed":
		response := root
		if !responseBody {
			if why := openAIIncrementalFields(root, "type response sequence_number stream_id event_id"); why != "" {
				e.reason = why
				return e
			}
			response = root.Get("response")
		}
		response.ForEach(func(key, _ gjson.Result) bool {
			switch key.String() {
			case "id", "object", "created_at", "completed_at", "status", "output", "error", "incomplete_details", "usage",
				"model", "instructions", "tools", "tool_choice", "parallel_tool_calls", "reasoning", "text", "store", "stream",
				"stream_options", "include", "service_tier", "prompt_cache_key", "prompt_cache_retention", "prompt_cache_options", "metadata", "client_metadata",
				"max_output_tokens", "max_tool_calls", "temperature", "top_p", "truncation", "user", "safety_identifier",
				"previous_response_id", "background":
			default:
				e.reason = "unknown_field"
			}
			return e.reason == ""
		})
		if e.reason != "" {
			return e
		}
		if options := response.Get("prompt_cache_options"); options.Exists() {
			if e.reason = openAIIncrementalPromptCache(options, false); e.reason != "" {
				return e
			}
		}
		output := response.Get("output")
		id := response.Get("id")
		if !response.IsObject() || id.Type != gjson.String || strings.TrimSpace(id.String()) == "" || len(id.String()) > openAIIncrementalMaxIdentity || response.Get("status").String() != "completed" || !output.IsArray() {
			e.reason = "output_incomplete"
			return e
		}
		if errorValue := response.Get("error"); errorValue.Exists() && errorValue.Type != gjson.Null {
			e.reason = "unsuccessful_terminal"
			return e
		}
		if details := response.Get("incomplete_details"); details.Exists() && details.Type != gjson.Null {
			e.reason = "unsuccessful_terminal"
			return e
		}
		items := output.Array()
		if len(items) > openAIIncrementalMaxItems {
			e.reason = "item_limit"
			return e
		}
		// Reuse the native collector only within this call. No collector plaintext
		// survives into txn/store state. Count equality rejects duplicate identities.
		collector := openAIWSToolCallReplayCollector{}
		for _, item := range items {
			collector.addAllItem(item)
		}
		complete := collector.AllItems()
		if len(complete) != len(items) || len(collector.pending) != 0 || collector.unidentifiedOutput {
			e.reason = "output_incomplete"
			return e
		}
		e.output = make([]openAIIncrementalItem, len(items))
		e.outputIDs = make([][32]byte, len(items))
		for i, raw := range complete {
			if status := gjson.GetBytes(raw, "status"); status.Exists() && status.String() != "completed" {
				e.reason = "output_incomplete"
				e.output = nil
				return e
			}
			e.outputIDs[i] = openAIIncrementalHash("output-id", []byte(gjson.GetBytes(raw, "id").String()))
			e.output[i], e.reason = openAIIncrementalSummarizeItem(gjson.ParseBytes(raw))
			if e.reason != "" {
				e.output = nil
				return e
			}
		}
		e.responseID, e.completed = strings.Clone(id.String()), true
	case "response.output_item.added", "response.output_item.done":
		item := root.Get("item")
		if _, why := openAIIncrementalSummarizeItem(item); why != "" && (kind == "response.output_item.done" || why == "unknown_item" || why == "unknown_field" || why == "ambiguous_null") {
			e.reason = why
			return e
		}
	default:
		if !openAIStreamEventIsMetadata(kind) {
			switch kind {
			case "response.output_text.delta", "response.output_text.done",
				"response.reasoning_text.delta", "response.reasoning_summary_text.delta",
				"response.function_call_arguments.delta", "response.function_call_arguments.done",
				"response.custom_tool_call_input.delta", "response.custom_tool_call_input.done",
				"response.content_part.added", "response.content_part.done":
			default:
				e.reason = "unknown_event"
			}
		}
	}
	if e.reason != "" {
		return e
	}
	if kind == "response.output_item.added" || kind == "response.output_item.done" || isOpenAIWSTokenEvent(kind) {
		index := root.Get("output_index")
		id := root.Get("item_id")
		if kind == "response.output_item.added" || kind == "response.output_item.done" {
			id = root.Get("item.id")
		}
		if index.Type != gjson.Number || index.Int() < 0 || index.Int() >= openAIIncrementalMaxItems || index.Raw != strconv.FormatInt(index.Int(), 10) || id.Type != gjson.String || id.String() == "" {
			e.reason = "output_incomplete"
		} else {
			e.index = int(index.Int())
			e.itemID = openAIIncrementalHash("output-id", []byte(id.String()))
		}
	}
	return e
}

// success is the caller's complete attempt outcome, including downstream write
// success; a completed upstream event alone must not publish a canceled attempt.
// Call once per begin (normally deferred), on failures as well as success.
func (t *openAIIncrementalObservation) Finish(ctx context.Context, success bool, connID string) openAIIncrementalObservationReport {
	if t == nil {
		return newOpenAIIncrementalReport("not_observed")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return t.report
	}
	t.finished = true
	if t.store == nil {
		return t.report
	}
	s := t.store
	s.incrementalMu.Lock()
	defer s.incrementalMu.Unlock()
	slot := s.incremental[t.key]
	if slot == nil || slot.generation != t.generation || !time.Now().Before(slot.expires) {
		candidate, reason := t.report.Candidate, t.report.Reason
		if candidate {
			reason = "expired_or_evicted"
		}
		t.report = newOpenAIIncrementalReport(reason)
		t.report.Candidate = candidate
		return t.report
	}
	slot.active--
	if slot.concurrent {
		candidate, reason := t.report.Candidate, t.report.Reason
		if candidate {
			reason = "concurrent"
		}
		t.report = newOpenAIIncrementalReport(reason)
		t.report.Candidate = candidate
		if slot.active == 0 {
			slot.concurrent = false
		}
		return t.report
	}
	p := slot.pending
	if p == nil {
		return t.report
	}
	slot.pending = nil
	s.incrementalBytes -= slot.bytes - openAIIncrementalSlotBytes
	slot.bytes = openAIIncrementalSlotBytes
	t.report.AnchorReason = "output_incomplete"
	switch {
	case ctx == nil || ctx.Err() != nil:
		t.report.AnchorReason = "cancelled"
	case !success:
		t.report.AnchorReason = "failed"
	case p.outputReason != "":
		t.report.AnchorReason = p.outputReason
	case !p.completed:
	case len(connID) > openAIIncrementalMaxIdentity:
		t.report.AnchorReason = "identity_limit"
	default:
		history := make([]openAIIncrementalItem, 0, len(p.input)+len(p.output))
		history = append(history, p.input...)
		history = append(history, p.output...)
		slot.anchor = &openAIIncrementalAnchor{boundary: p.boundary, settings: p.settings, items: history, responseID: p.responseID, connID: strings.Clone(connID), connWriteGeneration: p.connWriteGeneration}
		slot.bytes += len(history)*openAIIncrementalItemBytes + len(connID) + len(p.responseID)
		s.incrementalBytes += slot.bytes - openAIIncrementalSlotBytes
		t.report.AnchorReason = "completed"
	}
	return t.report
}

// Validate raw duplicates BEFORE decoding; the existing JSON decoder alone uses
// last-key wins. Bound nesting/node count before calling the existing canonical
// UseNumber comparator, which preserves large numbers instead of float rounding.
func openAIIncrementalJSON(raw []byte) (gjson.Result, string) {
	if len(raw) > openAIIncrementalMaxBody {
		return gjson.Result{}, "body_limit"
	}
	if !gjson.ValidBytes(raw) {
		return gjson.Result{}, "invalid_json"
	}
	if !openAIIncrementalUnicode(raw) {
		return gjson.Result{}, "invalid_json"
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() {
		return root, "invalid_json"
	}
	budget := 131072
	var walk func(gjson.Result, int) string
	walk = func(node gjson.Result, depth int) string {
		budget--
		if depth > 32 || budget < 0 {
			return "structure_limit"
		}
		if node.IsObject() && hasDuplicateJSONObjectKeys(node) {
			return "duplicate_json"
		}
		why := ""
		if node.IsObject() || node.IsArray() {
			node.ForEach(func(_, value gjson.Result) bool { why = walk(value, depth+1); return why == "" })
		}
		return why
	}
	return root, walk(root, 0)
}

// raw must already be syntactically valid JSON (or a scalar from that JSON).
func openAIIncrementalUnicode(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Such a
	// lossy canonicalization must never manufacture equality between inputs.
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return false
		}
		low, _ := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

// Narrow canonical JSON equality is NOT the complete Rust typed comparator.
// Item metadata/default/null exemptions are not applied. Only the explicit
// request-level projection below excludes delivery and per-turn fields.
func openAIIncrementalFields(node gjson.Result, fields string) string {
	if !node.IsObject() {
		return "unknown_shape"
	}
	why := ""
	allowed := strings.Fields(fields)
	node.ForEach(func(key, value gjson.Result) bool {
		known := false
		for _, field := range allowed {
			if field == key.String() {
				known = true
				break
			}
		}
		if !known {
			why = "unknown_field"
			return false
		}
		if value.Type == gjson.Null {
			why = "ambiguous_null"
			return false
		}
		return true
	})
	return why
}
func openAIIncrementalStrings(node gjson.Result, required, optional string) bool {
	for _, field := range strings.Fields(required) {
		if node.Get(field).Type != gjson.String {
			return false
		}
	}
	for _, field := range strings.Fields(optional) {
		v := node.Get(field)
		if v.Exists() && v.Type != gjson.String {
			return false
		}
	}
	return true
}

// Only the documented cache policy is evidence for automatic continuation.
// Keep its JSON in settings/item equality; never infer defaults or rewrite TTLs.
// https://developers.openai.com/api/docs/guides/prompt-caching
func openAIIncrementalPromptCache(node gjson.Result, breakpoint bool) string {
	fields := "mode ttl"
	if !breakpoint {
		fields += " prewarm comparison_response_id"
	}
	if why := openAIIncrementalFields(node, fields); why != "" {
		return why
	}
	mode := node.Get("mode")
	if breakpoint || mode.Exists() {
		if mode.Type != gjson.String || (mode.String() != "explicit" && (breakpoint || mode.String() != "implicit")) {
			return "unknown_shape"
		}
	}
	if ttl := node.Get("ttl"); ttl.Exists() && (ttl.Type != gjson.String || (ttl.String() != "30m" && ttl.String() != "12h")) {
		return "unknown_shape"
	}
	if !breakpoint {
		if prewarm := node.Get("prewarm"); prewarm.Exists() && prewarm.Type != gjson.True && prewarm.Type != gjson.False {
			return "unknown_shape"
		}
		if !openAIIncrementalStrings(node, "", "comparison_response_id") {
			return "unknown_shape"
		}
	}
	return ""
}

func openAIIncrementalRequestClass(raw []byte) string {
	if len(raw) > openAIIncrementalMaxBody || !gjson.ValidBytes(raw) {
		return ""
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() || hasDuplicateJSONObjectKeys(root) {
		return ""
	}
	if parent := root.Get("previous_response_id"); parent.Type == gjson.String && parent.String() != "" {
		return "native_continuation"
	}
	if root.Get("generate").Type == gjson.False {
		return "prewarm"
	}
	if options := root.Get("prompt_cache_options"); options.IsObject() && options.Get("prewarm").Type == gjson.True {
		return "prewarm"
	}
	return ""
}

func openAIIncrementalRequest(raw []byte) (*openAIIncrementalPending, string, bool) {
	if len(raw) > openAIIncrementalMaxBody {
		return nil, "body_limit", true
	}
	if !acquireOpenAIIncrementalParsing(len(raw)) {
		return nil, "observation_busy", true
	}
	defer openAIIncrementalParsing.Add(-int64(len(raw)))
	root, why := openAIIncrementalJSON(raw)
	if why != "" {
		return nil, why, true
	}
	if why = openAIIncrementalFields(root, "type model input instructions tools tool_choice parallel_tool_calls reasoning store stream include service_tier text client_metadata metadata prompt_cache_key prompt_cache_options prompt_cache_retention stream_options max_output_tokens temperature top_p previous_response_id generate"); why != "" {
		return nil, why, true
	}
	if root.Get("previous_response_id").Exists() {
		return nil, "ambiguous_parent", true
	}
	if typ := root.Get("type"); typ.Exists() && typ.String() != "response.create" {
		return nil, "unknown_shape", true
	}
	if !openAIIncrementalStrings(root, "model", "type instructions service_tier prompt_cache_key") || root.Get("model").String() == "" {
		return nil, "unknown_shape", true
	}
	if options := root.Get("prompt_cache_options"); options.Exists() {
		if why = openAIIncrementalPromptCache(options, false); why != "" {
			return nil, why, true
		}
	}
	if retention := root.Get("prompt_cache_retention"); retention.Exists() && (retention.Type != gjson.String || (retention.String() != "in_memory" && retention.String() != "24h")) {
		return nil, "unknown_shape", true
	}
	for _, key := range []string{"parallel_tool_calls", "store", "stream", "generate"} {
		v := root.Get(key)
		if v.Exists() && v.Type != gjson.True && v.Type != gjson.False {
			return nil, "unknown_shape", true
		}
	}
	for _, key := range []string{"max_output_tokens", "temperature", "top_p"} {
		v := root.Get(key)
		if v.Exists() && v.Type != gjson.Number {
			return nil, "unknown_shape", true
		}
	}
	for _, key := range []string{"client_metadata", "metadata"} {
		v := root.Get(key)
		if v.Exists() {
			if !v.IsObject() {
				return nil, "unknown_shape", true
			}
			valid := true
			v.ForEach(func(_, x gjson.Result) bool { valid = x.Type == gjson.String; return valid })
			if !valid {
				return nil, "unknown_shape", true
			}
		}
	}
	if reasoning := root.Get("reasoning"); reasoning.Exists() {
		if why = openAIIncrementalFields(reasoning, "effort summary context mode"); why != "" {
			return nil, why, true
		}
		if !openAIIncrementalStrings(reasoning, "", "effort summary context mode") {
			return nil, "unknown_shape", true
		}
	}
	if tools := root.Get("tools"); tools.Exists() {
		if why = openAIIncrementalTools(tools); why != "" {
			return nil, why, true
		}
	}
	if choice := root.Get("tool_choice"); choice.Exists() {
		if choice.Type == gjson.String {
			if choice.String() != "auto" && choice.String() != "none" && choice.String() != "required" {
				return nil, "unknown_shape", true
			}
		} else {
			if why = openAIIncrementalFields(choice, "type name"); why != "" {
				return nil, why, true
			}
			kind := choice.Get("type").String()
			if !openAIIncrementalStrings(choice, "type name", "") || (kind != "function" && kind != "custom") {
				return nil, "unknown_shape", true
			}
		}
	}
	if include := root.Get("include"); include.Exists() {
		if !include.IsArray() {
			return nil, "unknown_shape", true
		}
		for _, v := range include.Array() {
			if v.Type != gjson.String {
				return nil, "unknown_shape", true
			}
		}
	}
	if options := root.Get("stream_options"); options.Exists() {
		if why = openAIIncrementalFields(options, "include_usage"); why != "" {
			return nil, why, true
		}
		v := options.Get("include_usage")
		if v.Type != gjson.True && v.Type != gjson.False {
			return nil, "unknown_shape", true
		}
	}
	if text := root.Get("text"); text.Exists() {
		if why = openAIIncrementalFields(text, "verbosity format"); why != "" {
			return nil, why, true
		}
		if !openAIIncrementalStrings(text, "", "verbosity") {
			return nil, "unknown_shape", true
		}
		if format := text.Get("format"); format.Exists() {
			if why = openAIIncrementalFields(format, "type"); why != "" {
				return nil, why, true
			}
			if format.Get("type").String() != "text" {
				return nil, "unsupported_text_settings", true
			}
		}
	}
	input := root.Get("input")
	if !input.IsArray() {
		return nil, "unknown_shape", true
	}
	items := input.Array()
	if len(items) > openAIIncrementalMaxItems {
		return nil, "item_limit", true
	}
	p := &openAIIncrementalPending{requestDigest: openAIIncrementalHash("request", raw), input: make([]openAIIncrementalItem, len(items)), maxOutputIndex: -1}
	for i, item := range items {
		p.input[i], why = openAIIncrementalSummarizeItem(item)
		if why != "" {
			return nil, why, true
		}
	}
	// Keep semantic and unknown metadata. Only validated delivery options and
	// known per-turn Codex fields are excluded; their wire values are unchanged.
	settings := make(map[string]json.RawMessage)
	root.ForEach(func(key, value gjson.Result) bool {
		switch key.String() {
		case "input", "stream_options":
		case "client_metadata":
			var metadata map[string]string
			_ = json.Unmarshal([]byte(value.Raw), &metadata) // validated above
			delete(metadata, "turn_id")
			if embedded, ok := metadata[openAIWSTurnMetadataHeader]; ok {
				parsed, reason := openAIIncrementalJSON([]byte(embedded))
				if reason != "" || !parsed.IsObject() {
					why = "invalid_turn_metadata"
					return false
				}
				if id := parsed.Get("turn_id"); id.Exists() && id.Type != gjson.String {
					why = "invalid_turn_metadata"
					return false
				}
				if stamp := parsed.Get("turn_started_at_unix_ms"); stamp.Exists() && (stamp.Type != gjson.Number || stamp.Int() < 0 || stamp.Raw != strconv.FormatInt(stamp.Int(), 10)) {
					why = "invalid_turn_metadata"
					return false
				}
				var fields map[string]json.RawMessage
				_ = json.Unmarshal([]byte(embedded), &fields)
				delete(fields, "turn_id")
				delete(fields, "turn_started_at_unix_ms")
				encoded, _ := json.Marshal(fields)
				metadata[openAIWSTurnMetadataHeader] = string(encoded)
			}
			settings[key.String()], _ = json.Marshal(metadata)
		default:
			settings[key.String()] = json.RawMessage(value.Raw)
		}
		return true
	})
	if why != "" {
		return nil, why, true
	}
	serialized, _ := json.Marshal(settings)
	canonical, err := normalizeOpenAIWSJSONForCompare(serialized)
	if err != nil {
		return nil, "invalid_json", true
	}
	p.settings = openAIIncrementalHash("settings", canonical)
	return p, "no_anchor_or_evicted", true
}
func openAIIncrementalTools(tools gjson.Result) string {
	if !tools.IsArray() {
		return "unknown_shape"
	}
	for _, tool := range tools.Array() {
		fields := "type name description"
		kind := tool.Get("type").String()
		switch kind {
		case "function":
			fields += " parameters strict"
		case "custom":
			fields += " format"
		case "namespace":
			fields += " tools"
		default:
			return "unknown_tool"
		}
		if why := openAIIncrementalFields(tool, fields); why != "" {
			return why
		}
		if !openAIIncrementalStrings(tool, "type name", "description") || tool.Get("name").String() == "" {
			return "unknown_shape"
		}
		switch kind {
		case "function":
			if !tool.Get("parameters").IsObject() {
				return "unknown_shape"
			}
			if strict := tool.Get("strict"); strict.Exists() && strict.Type != gjson.True && strict.Type != gjson.False {
				return "unknown_shape"
			}
		case "namespace":
			// The root JSON reader already bounds total depth and node count.
			if why := openAIIncrementalTools(tool.Get("tools")); why != "" {
				return why
			}
		case "custom":
			format := tool.Get("format")
			if !format.Exists() {
				continue
			}
			formatFields := "type"
			if format.Get("type").String() == "grammar" {
				formatFields += " syntax definition"
				if !openAIIncrementalStrings(format, "syntax definition", "") || (format.Get("syntax").String() != "lark" && format.Get("syntax").String() != "regex") {
					return "unknown_shape"
				}
			} else if format.Get("type").String() != "text" {
				return "unknown_shape"
			}
			if why := openAIIncrementalFields(format, formatFields); why != "" {
				return why
			}
		}
	}
	return ""
}

// Only shapes with unambiguous canonical JSON semantics are observed. Native
// forwarding/normalization remains the sole protocol implementation; expanding
// this coverage table is optional and cannot change any delivered payload.
var openAIIncrementalItemShapes = map[string]struct{ fields, required, optional string }{
	"message":                 {"type id role content status", "type role", "id status"},
	"reasoning":               {"type id summary encrypted_content status", "type encrypted_content", "id status"},
	"function_call":           {"type id call_id name arguments status namespace", "type call_id name arguments", "id status namespace"},
	"function_call_output":    {"type id call_id output", "type call_id", "id"},
	"custom_tool_call":        {"type id call_id name input status namespace", "type call_id name input", "id status namespace"},
	"custom_tool_call_output": {"type id call_id output", "type call_id", "id"},
	"additional_tools":        {"type role tools", "type", "role"},
}

func openAIIncrementalSummarizeItem(item gjson.Result) (openAIIncrementalItem, string) {
	var empty openAIIncrementalItem
	kind := item.Get("type")
	if kind.Type != gjson.String {
		return empty, "unknown_item"
	}
	schema, known := openAIIncrementalItemShapes[kind.String()]
	if !known {
		return empty, "unknown_item"
	}
	if why := openAIIncrementalFields(item, schema.fields); why != "" {
		return empty, why
	}
	if !openAIIncrementalStrings(item, schema.required, schema.optional) {
		return empty, "unknown_shape"
	}
	if kind.String() == "additional_tools" {
		if why := openAIIncrementalTools(item.Get("tools")); why != "" {
			return empty, why
		}
	}
	if kind.String() == "message" {
		switch item.Get("role").String() {
		case "system", "developer", "user", "assistant":
		default:
			return empty, "unknown_shape"
		}
		content := item.Get("content")
		if content.Type != gjson.String {
			if why := openAIIncrementalTextParts(content, "input_text", "output_text"); why != "" {
				return empty, why
			}
		}
	}
	if kind.String() == "function_call_output" || kind.String() == "custom_tool_call_output" {
		// Tool results support input text blocks, including explicit breakpoints.
		// Images/files remain outside this observer's existing text-only subset.
		if output := item.Get("output"); output.Type != gjson.String {
			if why := openAIIncrementalTextParts(output, "input_text"); why != "" {
				return empty, why
			}
		}
	}
	if kind.String() == "reasoning" {
		if item.Get("encrypted_content").String() == "" {
			return empty, "output_incomplete"
		}
		if why := openAIIncrementalTextParts(item.Get("summary"), "summary_text"); why != "" {
			return empty, why
		}
	}
	canonical, err := normalizeOpenAIWSJSONForCompare([]byte(item.Raw))
	if err != nil {
		return empty, "invalid_json"
	}
	return openAIIncrementalItem{digest: openAIIncrementalHash("item", canonical), bytes: int64(len(item.Raw))}, ""
}
func openAIIncrementalTextParts(parts gjson.Result, kinds ...string) string {
	if !parts.IsArray() {
		return "unknown_shape"
	}
	for _, part := range parts.Array() {
		kind := part.Get("type").String()
		fields := "type text"
		switch kind {
		case "input_text":
			fields += " prompt_cache_breakpoint"
		case "output_text":
			fields += " annotations logprobs"
		}
		if why := openAIIncrementalFields(part, fields); why != "" {
			return why
		}
		if !openAIIncrementalStrings(part, "type text", "") {
			return "unknown_shape"
		}
		known := false
		for _, allowed := range kinds {
			if kind == allowed {
				known = true
				break
			}
		}
		if !known {
			return "unknown_item"
		}
		if breakpoint := part.Get("prompt_cache_breakpoint"); breakpoint.Exists() {
			if why := openAIIncrementalPromptCache(breakpoint, true); why != "" {
				return why
			}
		}
		for _, field := range []string{"annotations", "logprobs"} {
			v := part.Get(field)
			if v.Exists() && (!v.IsArray() || len(v.Array()) != 0) {
				return "unknown_shape"
			}
		}
	}
	return ""
}

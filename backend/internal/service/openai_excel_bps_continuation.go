package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	excelBPSContinuationVersion              = 1
	excelBPSContinuationTTL                  = 2 * time.Hour
	excelBPSContinuationMaxBytes       int64 = 64 << 20
	excelBPSContinuationMaxItems             = 8192
	excelBPSContinuationMaxOutputItems       = 4096
	excelBPSContinuationPendingWait          = time.Second
	excelBPSContinuationPoll                 = 25 * time.Millisecond
	excelBPSContinuationMagic                = "BPSC1:"
)

var (
	errExcelBPSContinuationInvalid  = errors.New("Excel BPS continuation state is invalid")
	errExcelBPSContinuationConflict = errors.New("Excel BPS continuation response conflicts with existing state")
)

// Incremental observation only stores digests and native WS replay belongs to
// one socket. These response-indexed snapshots use the existing BPS StateStore's
// 2h TTL, 16.5MiB entry budget and shared 32MiB global LRU. CreatedAt also caps
// semantic lifetime: an LRU read may touch Redis TTL but never extend history.
type excelBPSContinuationSnapshot struct {
	Version    int               `json:"version"`
	Scope      string            `json:"scope"`
	ResponseID string            `json:"response_id"`
	CreatedAt  int64             `json:"created_at"`
	State      string            `json:"state"`
	Nonce      string            `json:"nonce"`
	Digest     string            `json:"digest"`
	Input      []json.RawMessage `json:"input,omitempty"`
	Tools      json.RawMessage   `json:"tools,omitempty"`
}

func excelBPSContinuationScope(c *gin.Context, account *Account) (string, bool) {
	if c == nil || account == nil || account.ID <= 0 {
		return "", false
	}
	key := getAPIKeyFromContext(c)
	if key == nil || key.ID <= 0 || key.UserID <= 0 {
		return "", false
	}
	namespace := codexAccountIdentityNamespace(codexAccountIdentitySource(c, account))
	if namespace == "" {
		return "", false
	}
	group := "none"
	if key.GroupID != nil {
		group = fmt.Sprint(*key.GroupID)
	}
	raw := fmt.Sprintf("bps-continuation:v1/user:%d/key:%d/group:%s/account:%d/credential:%s", key.UserID, key.ID, group, account.ID, namespace)
	return fmt.Sprintf("continuation:v1:%x", sha256.Sum256([]byte(raw))), true
}

func excelBPSContinuationKind(id string) string {
	return fmt.Sprintf("continuation:v1:%x", sha256.Sum256([]byte(id)))
}

func (s *OpenAIGatewayService) excelBPSContinuationLimit() int64 {
	var limit int64
	if s != nil {
		limit = ResolveOpenAIWSClientReadLimitBytes(s.cfg)
	}
	if limit <= 0 || limit > excelBPSContinuationMaxBytes {
		limit = excelBPSContinuationMaxBytes
	}
	return limit
}

func excelBPSContinuationStoreError(err error) error {
	if errors.Is(err, basispoints.ErrStateCapacity) {
		return basispoints.ErrStateCapacity
	}
	return basispoints.ErrStateUnavailable
}

func excelBPSContinuationArrayLimit(value gjson.Result, limit int) bool {
	if !value.IsArray() {
		return false
	}
	count := 0
	value.ForEach(func(_, _ gjson.Result) bool { count++; return count <= limit })
	return count <= limit
}

// Check call/result completeness without round-tripping user values through
// float64. A reference alone cannot prove its unseen tool call is in history.
func excelBPSContinuationCompleteInput(items []json.RawMessage) bool {
	calls := make(map[string]struct{})
	for _, raw := range items {
		item := gjson.ParseBytes(raw)
		if !item.IsObject() {
			return false
		}
		kind := item.Get("type").String()
		if kind == "item_reference" || kind == "compaction_trigger" {
			return false
		}
		if isCodexToolCallContextItemType(kind) || strings.HasSuffix(kind, "_call") {
			id := strings.TrimSpace(item.Get("call_id").String())
			if id == "" {
				id = strings.TrimSpace(item.Get("id").String())
			}
			if id != "" {
				calls[id] = struct{}{}
			}
		}
	}
	for _, raw := range items {
		item := gjson.ParseBytes(raw)
		kind := item.Get("type").String()
		if !isCodexToolCallOutputItemType(kind) && !strings.HasSuffix(kind, "_call_output") && item.Get("role").String() != "tool" {
			continue
		}
		if openai.IsNamedStandaloneToolOutput(map[string]any{"type": kind, "name": item.Get("name").String(), "call_id": item.Get("call_id").String()}) {
			continue
		}
		id := strings.TrimSpace(item.Get("call_id").String())
		if id == "" && item.Get("role").String() == "tool" {
			id = strings.TrimSpace(item.Get("tool_call_id").String())
		}
		if _, ok := calls[id]; id == "" || !ok {
			return false
		}
	}
	return true
}

func excelBPSContinuationDigest(snapshot *excelBPSContinuationSnapshot) (string, error) {
	raw, err := json.Marshal(struct {
		Input []json.RawMessage `json:"input"`
		Tools json.RawMessage   `json:"tools,omitempty"`
	}{snapshot.Input, snapshot.Tools})
	if err != nil {
		return "", errExcelBPSContinuationInvalid
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

type excelBPSContinuationBoundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *excelBPSContinuationBoundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, basispoints.ErrStateCapacity
	}
	return b.Buffer.Write(p)
}

func encodeExcelBPSContinuation(snapshot *excelBPSContinuationSnapshot, limit int64) ([]byte, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, errExcelBPSContinuationInvalid
	}
	if int64(len(raw)) > limit {
		return nil, basispoints.ErrStateCapacity
	}
	header := len(excelBPSContinuationMagic) + 1
	fitsRaw := len(raw)+header <= basispoints.MaxStateEntryBytes
	if len(raw) <= 4<<10 && fitsRaw {
		return append([]byte(excelBPSContinuationMagic+"j"), raw...), nil
	}
	out := &excelBPSContinuationBoundedBuffer{limit: basispoints.MaxStateEntryBytes - header}
	zw, _ := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if _, err = zw.Write(raw); err != nil {
		_ = zw.Close()
		if fitsRaw {
			return append([]byte(excelBPSContinuationMagic+"j"), raw...), nil
		}
		return nil, basispoints.ErrStateCapacity
	}
	if err = zw.Close(); err != nil {
		if fitsRaw {
			return append([]byte(excelBPSContinuationMagic+"j"), raw...), nil
		}
		return nil, basispoints.ErrStateCapacity
	}
	if out.Len() >= len(raw) && fitsRaw {
		return append([]byte(excelBPSContinuationMagic+"j"), raw...), nil
	}
	return append([]byte(excelBPSContinuationMagic+"g"), out.Bytes()...), nil
}

func decodeExcelBPSContinuation(raw []byte, scope, id string, limit int64) (*excelBPSContinuationSnapshot, error) {
	if len(raw) > basispoints.MaxStateEntryBytes {
		return nil, basispoints.ErrStateCapacity
	}
	prefix := len(excelBPSContinuationMagic)
	if len(raw) <= prefix || !bytes.HasPrefix(raw, []byte(excelBPSContinuationMagic)) {
		return nil, errExcelBPSContinuationInvalid
	}
	payload := raw[prefix+1:]
	switch raw[prefix] {
	case 'j':
		if int64(len(payload)) > limit {
			return nil, basispoints.ErrStateCapacity
		}
	case 'g':
		zr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, errExcelBPSContinuationInvalid
		}
		payload, err = io.ReadAll(io.LimitReader(zr, limit+1))
		closeErr := zr.Close()
		if int64(len(payload)) > limit {
			return nil, basispoints.ErrStateCapacity
		}
		if err != nil || closeErr != nil {
			return nil, errExcelBPSContinuationInvalid
		}
	default:
		return nil, errExcelBPSContinuationInvalid
	}
	input := gjson.GetBytes(payload, "input")
	if input.IsArray() && !excelBPSContinuationArrayLimit(input, excelBPSContinuationMaxItems) {
		return nil, basispoints.ErrStateCapacity
	}
	var snapshot excelBPSContinuationSnapshot
	if json.Unmarshal(payload, &snapshot) != nil || snapshot.Version != excelBPSContinuationVersion || snapshot.Scope != scope || snapshot.ResponseID != id || snapshot.Nonce == "" || snapshot.Digest == "" || snapshot.CreatedAt <= 0 {
		return nil, errExcelBPSContinuationInvalid
	}
	age := time.Since(time.UnixMilli(snapshot.CreatedAt))
	if age >= excelBPSContinuationTTL {
		return nil, basispoints.ErrPreviousResponseNotFound
	}
	if age < -time.Minute {
		return nil, errExcelBPSContinuationInvalid
	}
	if snapshot.State == "aborted" {
		return nil, basispoints.ErrPreviousResponseNotFound
	}
	if snapshot.State != "pending" && snapshot.State != "committed" {
		return nil, errExcelBPSContinuationInvalid
	}
	if snapshot.State == "pending" {
		// Poll only the small publication marker; history is written once,
		// after successful terminal delivery, instead of decoded every 25ms.
		if len(snapshot.Input) != 0 || len(snapshot.Tools) != 0 {
			return nil, errExcelBPSContinuationInvalid
		}
		return &snapshot, nil
	}
	if len(snapshot.Input) == 0 || len(snapshot.Input) > excelBPSContinuationMaxItems || !excelBPSContinuationCompleteInput(snapshot.Input) {
		return nil, errExcelBPSContinuationInvalid
	}
	digest, err := excelBPSContinuationDigest(&snapshot)
	if err != nil || digest != snapshot.Digest {
		return nil, errExcelBPSContinuationInvalid
	}
	return &snapshot, nil
}

func (s *OpenAIGatewayService) restoreExcelBPSContinuation(ctx context.Context, c *gin.Context, account *Account, body []byte) ([]byte, error) {
	previous := gjson.GetBytes(body, "previous_response_id")
	if previous.Type != gjson.String || strings.TrimSpace(previous.String()) == "" {
		return body, nil
	}
	if s == nil || s.cfg == nil || !s.cfg.Gateway.ExcelBPSStateRedisEnabled {
		return nil, basispoints.ErrPreviousResponseNotFound
	}
	scope, trusted := excelBPSContinuationScope(c, account)
	if !trusted {
		return nil, basispoints.ErrPreviousResponseNotFound
	}
	store, ok := s.cache.(basispoints.StateStore)
	if !ok || store == nil {
		return nil, basispoints.ErrStateUnavailable
	}
	limit := s.excelBPSContinuationLimit()
	if int64(len(body)) > limit {
		return nil, basispoints.ErrStateCapacity
	}
	if !json.Valid(body) {
		return nil, errExcelBPSContinuationInvalid
	}
	input := gjson.GetBytes(body, "input")
	if input.IsArray() && !excelBPSContinuationArrayLimit(input, excelBPSContinuationMaxItems) {
		return nil, basispoints.ErrStateCapacity
	}
	id := strings.TrimSpace(previous.String())
	var snapshot *excelBPSContinuationSnapshot
	// Only an existing pending snapshot earns a short wait, never a cache miss.
	waitCtx, cancelWait := context.WithTimeout(ctx, excelBPSContinuationPendingWait)
	defer cancelWait()
	for {
		raw, _, err := store.LoadBPSState(waitCtx, scope, excelBPSContinuationKind(id))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, excelBPSContinuationStoreError(err)
		}
		if len(raw) == 0 {
			return nil, basispoints.ErrPreviousResponseNotFound
		}
		snapshot, err = decodeExcelBPSContinuation(raw, scope, id, limit)
		if err != nil {
			return nil, err
		}
		if snapshot.State == "committed" {
			break
		}
		poll := time.NewTimer(excelBPSContinuationPoll)
		select {
		case <-ctx.Done():
			poll.Stop()
			return nil, ctx.Err()
		case <-waitCtx.Done():
			poll.Stop()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, basispoints.ErrStateUnavailable
		case <-poll.C:
		}
	}
	history, exists, err := buildOpenAIWSReplayInputSequence(snapshot.Input, true, body, true)
	if err != nil || !exists {
		return nil, errExcelBPSContinuationInvalid
	}
	if len(history) > excelBPSContinuationMaxItems {
		return nil, basispoints.ErrStateCapacity
	}
	if !excelBPSContinuationCompleteInput(history) {
		return nil, errExcelBPSContinuationInvalid
	}
	restored, err := setOpenAIWSPayloadInputSequence(body, history, true)
	if err != nil {
		return nil, errExcelBPSContinuationInvalid
	}
	if !gjson.GetBytes(body, "tools").Exists() && len(snapshot.Tools) > 0 {
		restored, err = sjson.SetRawBytes(restored, "tools", snapshot.Tools)
		if err != nil {
			return nil, errExcelBPSContinuationInvalid
		}
	}
	restored, err = sjson.DeleteBytes(restored, "previous_response_id")
	if err != nil {
		return nil, errExcelBPSContinuationInvalid
	}
	if int64(len(restored)) > limit {
		return nil, basispoints.ErrStateCapacity
	}
	return restored, nil
}

// Prepare before response.completed delivery, then finish exactly once with the
// terminal write outcome. Persistence errors must never trigger generation
// replay. A failed write/cancelled delivery cannot publish usable history.
func (s *OpenAIGatewayService) prepareExcelBPSContinuation(ctx context.Context, c *gin.Context, account *Account, originalBody, completedResponse []byte) (func(bool), error) {
	return s.prepareExcelBPSContinuationSnapshot(ctx, c, account, originalBody, completedResponse, false)
}

// interrupted is only used for a locally synthesized missing-terminal failure
// whose delivered message/reasoning items were checked for completeness.
func (s *OpenAIGatewayService) prepareExcelBPSContinuationSnapshot(ctx context.Context, c *gin.Context, account *Account, originalBody, completedResponse []byte, interrupted bool) (func(bool), error) {
	noop := func(bool) {}
	if s == nil || s.cfg == nil || !s.cfg.Gateway.ExcelBPSStateRedisEnabled {
		return noop, nil
	}
	scope, trusted := excelBPSContinuationScope(c, account)
	if !trusted {
		return noop, nil
	}
	if isOpenAIResponsesCompactPath(c) || HasCompactionTriggerInInput(originalBody) {
		return noop, nil
	}
	response := gjson.ParseBytes(completedResponse)
	id := strings.TrimSpace(response.Get("id").String())
	validTerminal := response.Get("status").String() == "completed" && (!response.Get("error").Exists() || response.Get("error").Type == gjson.Null)
	if interrupted {
		validTerminal = response.Get("status").String() == "failed" && response.Get("error.code").String() == "basispoints_stream_incomplete" && len(response.Get("output").Array()) > 0
	}
	if response.Get("id").Type != gjson.String || id == "" || !validTerminal || !response.Get("output").IsArray() {
		return noop, nil
	}
	for _, field := range []string{"previous_response_id", "prompt", "conversation"} {
		value := gjson.GetBytes(originalBody, field)
		if value.Exists() && value.Type != gjson.Null && !(value.Type == gjson.String && strings.TrimSpace(value.String()) == "") {
			return noop, nil
		}
	}
	limit := s.excelBPSContinuationLimit()
	if int64(len(originalBody)) > limit || int64(len(completedResponse)) > limit {
		return noop, basispoints.ErrStateCapacity
	}
	if !json.Valid(originalBody) || !json.Valid(completedResponse) {
		return noop, errExcelBPSContinuationInvalid
	}
	output := response.Get("output")
	input := gjson.GetBytes(originalBody, "input")
	if !excelBPSContinuationArrayLimit(output, excelBPSContinuationMaxOutputItems) || (input.IsArray() && !excelBPSContinuationArrayLimit(input, excelBPSContinuationMaxItems)) {
		return noop, basispoints.ErrStateCapacity
	}
	history, exists, err := openAIWSExtractNormalizedInputSequence(originalBody)
	if err != nil {
		return noop, errExcelBPSContinuationInvalid
	}
	if !exists || len(history) == 0 || !excelBPSContinuationCompleteInput(history) {
		return noop, nil
	}
	for _, item := range output.Array() {
		if interrupted {
			_, complete := openAIResponsesSelfContainedInputItem(item)
			if !complete || (item.Get("type").String() != "message" && item.Get("type").String() != "reasoning") {
				return noop, nil
			}
		}
		status := item.Get("status").String()
		if !item.IsObject() || (status != "" && status != "completed") || item.Get("type").String() == "compaction" || item.Get("type").String() == "compaction_trigger" {
			return noop, nil
		}
		history = append(history, json.RawMessage(item.Raw))
	}
	if len(history) > excelBPSContinuationMaxItems {
		return noop, basispoints.ErrStateCapacity
	}
	if !excelBPSContinuationCompleteInput(history) {
		return noop, nil
	}
	store, ok := s.cache.(basispoints.StateStore)
	if !ok || store == nil {
		return noop, basispoints.ErrStateUnavailable
	}
	snapshot := &excelBPSContinuationSnapshot{Version: excelBPSContinuationVersion, Scope: scope, ResponseID: id, CreatedAt: time.Now().UnixMilli(), State: "committed", Input: history}
	if tools := gjson.GetBytes(originalBody, "tools"); tools.Exists() {
		snapshot.Tools = json.RawMessage(tools.Raw)
	}
	snapshot.Digest, err = excelBPSContinuationDigest(snapshot)
	if err != nil {
		return noop, err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return noop, basispoints.ErrStateUnavailable
	}
	snapshot.Nonce = hex.EncodeToString(nonce[:])
	// Preflight the full stored representation before terminal delivery.
	committedEncoded, err := encodeExcelBPSContinuation(snapshot, limit)
	if err != nil {
		return noop, err
	}
	pendingMarker := *snapshot
	pendingMarker.State, pendingMarker.Input, pendingMarker.Tools = "pending", nil, nil
	encoded, err := encodeExcelBPSContinuation(&pendingMarker, limit)
	if err != nil {
		return noop, err
	}
	expected := uint64(0)
	kind := excelBPSContinuationKind(id)
	changed, err := store.SaveBPSState(ctx, scope, kind, &expected, encoded)
	if err != nil {
		return noop, excelBPSContinuationStoreError(err)
	}
	if !changed {
		raw, _, loadErr := store.LoadBPSState(ctx, scope, kind)
		if loadErr != nil {
			return noop, excelBPSContinuationStoreError(loadErr)
		}
		existing, decodeErr := decodeExcelBPSContinuation(raw, scope, id, limit)
		if decodeErr != nil {
			return noop, decodeErr
		}
		if existing.Digest != snapshot.Digest {
			return noop, errExcelBPSContinuationConflict
		}
		if existing.State == "committed" {
			return noop, nil
		}
		// A second producer cannot commit or abort another producer's pending write.
		return noop, basispoints.ErrStateUnavailable
	}
	ownerNonce, ownerDigest := snapshot.Nonce, snapshot.Digest
	var once sync.Once
	return func(delivered bool) {
		once.Do(func() {
			finishCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			raw, revision, loadErr := store.LoadBPSState(finishCtx, scope, kind)
			if loadErr != nil || revision == 0 {
				return
			}
			pending, decodeErr := decodeExcelBPSContinuation(raw, scope, id, limit)
			if decodeErr != nil || pending.State != "pending" || pending.Nonce != ownerNonce || pending.Digest != ownerDigest {
				return
			}
			next := committedEncoded
			if !delivered {
				pending.State = "aborted"
				var encodeErr error
				next, encodeErr = encodeExcelBPSContinuation(pending, limit)
				if encodeErr != nil {
					return
				}
			}
			_, _ = store.SaveBPSState(finishCtx, scope, kind, &revision, next)
		})
	}, nil
}

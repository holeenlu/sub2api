package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Shared by both managers, but deliberately named as a fake routing store. It
// reuses the existing actor fixture with synchronization, owner fencing, bounded
// streams and snapshot copies; it makes no claim about actual Redis behavior.
type cloudLiveRoutingStore struct {
	*cloudActorTestStore
	mu       sync.Mutex
	changed  chan struct{}
	commands map[string][]OpenAICloudStreamMessage
	events   map[string][]OpenAICloudStreamMessage
	bytes    map[string]int
	closed   map[string]bool
	released bool
	sequence uint64
	latency  map[string]*UsageLatencyMetrics
}

func newCloudLiveRoutingStore() *cloudLiveRoutingStore {
	return &cloudLiveRoutingStore{cloudActorTestStore: &cloudActorTestStore{refs: map[string]bool{}}, changed: make(chan struct{}), commands: map[string][]OpenAICloudStreamMessage{}, events: map[string][]OpenAICloudStreamMessage{}, bytes: map[string]int{}, closed: map[string]bool{}}
}
func (s *cloudLiveRoutingStore) signal() { close(s.changed); s.changed = make(chan struct{}) }
func cloudLiveClone[T any](value T) T {
	raw, _ := json.Marshal(value)
	var copy T
	_ = json.Unmarshal(raw, &copy)
	return copy
}
func (s *cloudLiveRoutingStore) CreateCloudSession(_ context.Context, r *OpenAICloudSessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record != nil {
		return errors.New("live check allows one Cloud thread")
	}
	s.record = cloudLiveClone(r)
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) LoadCloudSession(_ context.Context, id string) (*OpenAICloudSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.SessionID != id {
		return nil, nil
	}
	return cloudLiveClone(s.record), nil
}
func (s *cloudLiveRoutingStore) FindCloudSession(_ context.Context, user, key, group int64, ref string) (*OpenAICloudSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.UserID != user || s.record.APIKeyID != key || s.record.GroupID != group || !s.refs[ref] {
		return nil, nil
	}
	return cloudLiveClone(s.record), nil
}
func (s *cloudLiveRoutingStore) SaveCloudSession(_ context.Context, r *OpenAICloudSessionRecord, refs []string, settlement *OpenAICloudSettlement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.released || s.record.OwnerToken != r.OwnerToken || s.record.OpenAICloudOwner != r.OpenAICloudOwner || !time.Now().Before(s.record.LeaseUntil) || r.Sequence <= s.record.Sequence {
		return errors.New("live Cloud owner fence rejected the update")
	}
	copy := cloudLiveClone(r)
	copy.LeaseUntil = s.record.LeaseUntil
	if settlement != nil {
		for _, old := range s.settlements {
			if old.RootRequestID == settlement.RootRequestID && !CloudUsesStageBilling(settlement.BillingSnapshot) {
				return errors.New("live Cloud root was settled twice")
			}
		}
		if settlement.Sequence != copy.Sequence || settlement.RootRequestID != copy.RootRequestID {
			return errors.New("live Cloud settlement snapshot mismatch")
		}
	}
	if err := s.cloudActorTestStore.SaveCloudSession(context.Background(), copy, refs, cloudLiveClone(settlement)); err != nil {
		return err
	}
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) RenewCloudSession(_ context.Context, id, owner string, until time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.released || s.record.SessionID != id || s.record.OwnerToken != owner || !time.Now().Before(s.record.LeaseUntil) {
		return false, nil
	}
	s.record.LeaseUntil = until
	return true, nil
}

func (s *cloudLiveRoutingStore) PreserveCloudLateUsage(ctx context.Context, r *OpenAICloudSessionRecord, original *OpenAICloudSettlement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.OwnerToken != r.OwnerToken || s.record.RootRequestID != r.RootRequestID || s.record.ThreadID != r.ThreadID || s.record.TurnID != r.TurnID || s.record.OpenAICloudOwner != r.OpenAICloudOwner {
		return errors.New("live Cloud late usage owner fence rejected the update")
	}
	if err := s.cloudActorTestStore.PreserveCloudLateUsage(ctx, r, original); err != nil {
		return err
	}
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) EnqueueCloudCommand(_ context.Context, id string, command OpenAICloudCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.SessionID != id || s.released || command.Request.Owner != s.record.OpenAICloudOwner {
		return ErrOpenAICloudCommandRejected
	}
	raw, err := OpenAICloudEncodeCommand(command)
	if err != nil || len(raw) > OpenAICloudMaxCommandBytes {
		return errors.New("live Cloud command exceeds its storage limit")
	}
	if len(s.commands[id]) >= OpenAICloudMaxCommands || s.bytes["command:"+id]+len(raw) > OpenAICloudMaxEventStreamBytes {
		return errors.New("live Cloud command limit exceeded")
	}
	s.sequence++
	s.commands[id] = append(s.commands[id], OpenAICloudStreamMessage{ID: strconv.FormatUint(s.sequence, 10) + "-0", Payload: raw})
	s.bytes["command:"+id] += len(raw)
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) PublishCloudEvent(_ context.Context, id string, event OpenAICloudStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed[id] {
		return errors.New("live Cloud request is closed")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return errors.New("live Cloud event encoding failed")
	}
	if len(s.events[id]) >= OpenAICloudMaxStreamEntries || s.bytes["event:"+id]+len(raw) > OpenAICloudMaxEventStreamBytes {
		return errors.New("live Cloud event limit exceeded")
	}
	s.sequence++
	s.events[id] = append(s.events[id], OpenAICloudStreamMessage{ID: strconv.FormatUint(s.sequence, 10) + "-0", Payload: raw})
	s.bytes["event:"+id] += len(raw)
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) ReadCloudCommands(ctx context.Context, id, after string) ([]OpenAICloudStreamMessage, error) {
	return s.read(ctx, id, after, true)
}
func (s *cloudLiveRoutingStore) ReadCloudEvents(ctx context.Context, id, after string) ([]OpenAICloudStreamMessage, error) {
	return s.read(ctx, id, after, false)
}
func (s *cloudLiveRoutingStore) read(ctx context.Context, id, after string, command bool) ([]OpenAICloudStreamMessage, error) {
	cursor, _ := strconv.ParseUint(strings.SplitN(after, "-", 2)[0], 10, 64)
	for {
		s.mu.Lock()
		messages := s.events[id]
		if command {
			messages = s.commands[id]
		}
		var out []OpenAICloudStreamMessage
		for _, message := range messages {
			number, _ := strconv.ParseUint(strings.SplitN(message.ID, "-", 2)[0], 10, 64)
			if number > cursor {
				out = append(out, cloudLiveClone(message))
			}
		}
		changed := s.changed
		s.mu.Unlock()
		if len(out) > 0 {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		case <-time.After(time.Second):
			return nil, nil
		}
	}
}
func (s *cloudLiveRoutingStore) CloudRequestClosed(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed[id], nil
}
func (s *cloudLiveRoutingStore) DeleteCloudEvents(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.events, id)
	delete(s.bytes, "event:"+id)
	s.closed[id] = true
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) ReleaseCloudSession(_ context.Context, id, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.SessionID != id || s.record.OwnerToken != owner || s.record.Status != "completed" {
		return errors.New("live Cloud release owner mismatch")
	}
	s.released = true
	s.signal()
	return nil
}
func (s *cloudLiveRoutingStore) snapshot() *OpenAICloudSessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloudLiveClone(s.record)
}
func (s *cloudLiveRoutingStore) settlementCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, settlement := range s.settlements {
		if cloudTerminalStatus(settlement.Status) {
			count++
		}
	}
	return count
}
func (s *cloudLiveRoutingStore) settlementMatches(root string, usage *OpenAICloudUsage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, settle := range s.settlements {
		if settle.RootRequestID == root {
			if settle.Unknown || settle.Status != "completed" || !reflect.DeepEqual(settle.Usage, usage) {
				return false
			}
			count++
		}
	}
	return count == 1
}
func (s *cloudLiveRoutingStore) wasReleased() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.released
}

// ReclaimCloudCommands mirrors the Redis owner-fenced deletion of consumed
// commands, returning their bytes to the bounded queue.
func (s *cloudLiveRoutingStore) ReclaimCloudCommands(_ context.Context, id, owner, upTo string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.SessionID != id || s.record.OwnerToken != owner {
		return errors.New("live Cloud reclaim owner mismatch")
	}
	limit, _ := strconv.ParseUint(strings.SplitN(upTo, "-", 2)[0], 10, 64)
	kept := s.commands[id][:0]
	for _, message := range s.commands[id] {
		number, _ := strconv.ParseUint(strings.SplitN(message.ID, "-", 2)[0], 10, 64)
		if number <= limit {
			s.bytes["command:"+id] -= len(message.Payload)
			continue
		}
		kept = append(kept, message)
	}
	s.commands[id] = kept
	return nil
}

func (s *cloudLiveRoutingStore) RecordCloudLatency(_ context.Context, root string, metrics *UsageLatencyMetrics) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latency == nil {
		s.latency = map[string]*UsageLatencyMetrics{}
	}
	if _, exists := s.latency[root]; !exists {
		s.latency[root] = cloudLiveClone(metrics)
	}
	return nil
}
func (s *cloudLiveRoutingStore) LoadCloudLatency(_ context.Context, root string) (*UsageLatencyMetrics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloudLiveClone(s.latency[root]), nil
}

func (s *cloudLiveRoutingStore) snapshotSettlement(root string) *OpenAICloudSettlement {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.settlements) - 1; i >= 0; i-- {
		if s.settlements[i].RootRequestID == root {
			return cloudLiveClone(s.settlements[i])
		}
	}
	return nil
}

func (s *cloudLiveRoutingStore) CloudSettlementAcknowledged(context.Context, string) (bool, error) {
	return false, nil
}

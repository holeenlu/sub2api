package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// All keys share a Redis slot. Payloads never contain OAuth credentials. Live
// sockets remain on their owner; commands and events carry bounded RPC work.
const cloudCachePrefix = "openai:{cloud}:"

var _ service.OpenAICloudSessionStore = (*gatewayCache)(nil)

func cloudHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

func cloudKey(kind, value string) string { return cloudCachePrefix + kind + ":" + cloudHash(value) }
func cloudScopeRef(user, key, group int64, ref string) string {
	return cloudKey("ref", fmt.Sprintf("%d/%d/%d/%s", user, key, group, ref))
}

func cloudRecordKeys(r *service.OpenAICloudSessionRecord) []string {
	return []string{cloudKey("session", r.SessionID), cloudKey("owner", r.SessionID),
		cloudCachePrefix + "live", cloudKey("user", strconv.FormatInt(r.UserID, 10)),
		cloudKey("account", strconv.FormatInt(r.AccountID, 10)), cloudCachePrefix + "leases"}
}

func cloudPayload(v any, maxBytes int) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil || len(b) > maxBytes {
		return nil, errors.New("Cloud state exceeds its storage limit")
	}
	return b, nil
}

func cloudValidRecord(r *service.OpenAICloudSessionRecord) bool {
	return r != nil && r.SessionID != "" && len(r.SessionID) <= 160 && r.RootRequestID != "" &&
		len(r.RootRequestID) <= 160 && r.OwnerToken != "" && r.OwnerInstance != "" &&
		r.UserID > 0 && ((!r.Probe && r.APIKeyID > 0) || (r.Probe && r.APIKeyID == 0)) && r.AccountID > 0 && r.GrantGeneration != "" && r.Model != "" &&
		r.ExpiresAt.After(time.Now()) && r.ExpiresAt.Before(time.Now().Add(service.OpenAICloudSessionTTL+time.Minute))
}

var createCloudSessionScript = redis.NewScript(`
local now=tonumber(ARGV[1])
-- Live owners count toward admission until the fenced reaper or clean close releases them.
if redis.call('EXISTS',KEYS[1])==1 then return -1 end
if redis.call('ZCARD',KEYS[7])>=tonumber(ARGV[8]) then return -2 end
if redis.call('ZCARD',KEYS[3])>=tonumber(ARGV[8]) or redis.call('ZCARD',KEYS[4])>=tonumber(ARGV[9]) or redis.call('ZCARD',KEYS[5])>=tonumber(ARGV[10]) then return -2 end
redis.call('SET',KEYS[1],ARGV[2])
redis.call('SET',KEYS[2],ARGV[3],'PX',ARGV[6])
for i=3,5 do redis.call('ZADD',KEYS[i],ARGV[5],ARGV[4]) end
redis.call('ZADD',KEYS[6],now+tonumber(ARGV[6]),ARGV[4])
return 1`)

func (c *gatewayCache) CreateCloudSession(ctx context.Context, r *service.OpenAICloudSessionRecord) error {
	if c == nil || c.rdb == nil || !cloudValidRecord(r) {
		return errors.New("invalid or unavailable Cloud session storage")
	}
	b, err := cloudPayload(r, service.OpenAICloudMaxEnvelopeBytes)
	if err != nil {
		return err
	}
	n, err := createCloudSessionScript.Run(ctx, c.rdb, append(cloudRecordKeys(r), cloudCachePrefix+"settlements"), time.Now().UnixMilli(), b,
		r.OwnerToken, r.SessionID, r.ExpiresAt.UnixMilli(), service.OpenAICloudOwnerLease.Milliseconds(),
		service.OpenAICloudStateTTL.Milliseconds(), service.OpenAICloudMaxSessions,
		service.OpenAICloudMaxUserSessions, service.OpenAICloudMaxAccountSessions).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		if n == -2 {
			return service.ErrOpenAICloudCapacity
		}
		return errors.New("Cloud session already exists or capacity is exhausted")
	}
	return nil
}

// Choose a completed continuation from the scope that actually exhausted its
// slots. This is only a candidate: its live owner rechecks state before closing.
func (c *gatewayCache) FindCloudIdleSession(ctx context.Context, userID, accountID int64) (*service.OpenAICloudSessionRecord, error) {
	for _, scope := range []struct {
		key   string
		limit int64
	}{
		{cloudKey("user", strconv.FormatInt(userID, 10)), service.OpenAICloudMaxUserSessions},
		{cloudKey("account", strconv.FormatInt(accountID, 10)), service.OpenAICloudMaxAccountSessions},
		{cloudCachePrefix + "live", service.OpenAICloudMaxSessions},
	} {
		count, err := c.rdb.ZCard(ctx, scope.key).Result()
		if err != nil {
			return nil, err
		}
		if count < scope.limit {
			continue
		}
		ids, err := c.rdb.ZRange(ctx, scope.key, 0, service.OpenAICloudMaxSessions-1).Result()
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			record, err := c.LoadCloudSession(ctx, id)
			if err != nil {
				return nil, err
			}
			if record != nil && record.Status == "completed" && record.LeaseUntil.After(time.Now()) {
				return record, nil
			}
		}
		return nil, nil
	}
	return nil, nil
}

func (c *gatewayCache) LoadCloudSession(ctx context.Context, id string) (*service.OpenAICloudSessionRecord, error) {
	if c == nil || c.rdb == nil || id == "" || len(id) > 160 {
		return nil, errors.New("invalid Cloud session lookup")
	}
	b, err := c.rdb.Get(ctx, cloudKey("session", id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > service.OpenAICloudMaxEnvelopeBytes {
		return nil, errors.New("Cloud session record is too large")
	}
	var r service.OpenAICloudSessionRecord
	if json.Unmarshal(b, &r) != nil || r.SessionID != id {
		return nil, errors.New("invalid Cloud session record")
	}
	return &r, nil
}

func (c *gatewayCache) FindCloudSession(ctx context.Context, user, key, group int64, ref string) (*service.OpenAICloudSessionRecord, error) {
	if err := cloudStorageReady(c); err != nil {
		return nil, err
	}

	if user <= 0 || key < 0 || len(ref) == 0 || len(ref) > 512 {
		return nil, errors.New("invalid Cloud response reference")
	}
	id, err := c.rdb.Get(ctx, cloudScopeRef(user, key, group, ref)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r, err := c.LoadCloudSession(ctx, id)
	if err != nil || r == nil {
		return r, err
	}
	if r.UserID != user || r.APIKeyID != key || r.GroupID != group {
		return nil, errors.New("Cloud response ownership mismatch")
	}
	return r, nil
}

// Lua cjson escapes every forward slash, unlike Go's JSON encoder. Normalize
// that optional escape so lease/save/recovery updates cannot double a valid
// record's byte size. Backslashes preceding a slash remain correctly escaped.
const cloudEncodeRecordLua = `
local function encodeCloudRecord(value)
 return (string.gsub(cjson.encode(value), '\\/', '/'))
end
`

var saveCloudSessionScript = redis.NewScript(cloudEncodeRecordLua + `
local function same(a,b)
 if type(a)~=type(b) then return false end
 if type(a)~='table' then return a==b end
 for k,v in pairs(a) do if not same(v,b[k]) then return false end end
 for k in pairs(b) do if a[k]==nil then return false end end
 return true
end

if redis.call('GET',KEYS[2])~=ARGV[1] then return -1 end
local previous=redis.call('GET',KEYS[1]); if not previous then return -1 end
local old=cjson.decode(previous); local next=cjson.decode(ARGV[2])
if old.owner_token~=ARGV[1] or old.user_id~=next.user_id or old.api_key_id~=next.api_key_id or old.group_id~=next.group_id or old.account_id~=next.account_id or old.grant_generation~=next.grant_generation or old.model~=next.model or old.settings_digest~=next.settings_digest or old.probe~=next.probe or old.owner_instance~=next.owner_instance then return -2 end
if old.root_request_id==next.root_request_id and not same(old.billing_snapshot,next.billing_snapshot) then return -2 end
if old.root_request_id~=next.root_request_id and (old.status~='completed' or next.status~='running' or redis.call('ZCARD',KEYS[8])>=128) then return -2 end
if tonumber(next.sequence)<=tonumber(old.sequence) then return -3 end
for i=9,#KEYS do local owner=redis.call('GET',KEYS[i]); if owner and owner~=ARGV[3] then return -4 end end
if ARGV[5]~='' then
 local incoming=cjson.decode(ARGV[5]);local prior=redis.call('GET',KEYS[7])
 if prior then
  local previous=cjson.decode(prior)
  if previous.session_id~=incoming.session_id or previous.user_id~=incoming.user_id or previous.api_key_id~=incoming.api_key_id or previous.account_id~=incoming.account_id or previous.grant_generation~=incoming.grant_generation or not same(previous.billing_snapshot,incoming.billing_snapshot) or tonumber(previous.sequence)>=tonumber(incoming.sequence) then return -5 end
 end
end
next.lease_until=old.lease_until;next.expires_at=old.expires_at
redis.call('SET',KEYS[1],encodeCloudRecord(next))
for i=9,#KEYS do redis.call('SET',KEYS[i],ARGV[3],'PX',ARGV[4]) end
if ARGV[5]~='' then
 local existing=redis.call('GET',KEYS[7]); local incoming=cjson.decode(ARGV[5])
 if not existing or tonumber(cjson.decode(existing).sequence)<=tonumber(incoming.sequence) then
  redis.call('SET',KEYS[7],ARGV[5]);redis.call('ZADD',KEYS[8],ARGV[7],incoming.root_request_id)
 end
end
if next.status=='closed' or next.status=='failed' or next.status=='unknown' or next.status=='interrupted' then
 redis.call('PEXPIRE',KEYS[1],ARGV[4])
 for i=3,6 do redis.call('ZREM',KEYS[i],ARGV[3]) end
 redis.call('DEL',KEYS[2])
end
return 1`)

func (c *gatewayCache) SaveCloudSession(ctx context.Context, r *service.OpenAICloudSessionRecord, refs []string, settlement *service.OpenAICloudSettlement) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}

	if r == nil || r.SessionID == "" || r.OwnerToken == "" || len(refs) > 256 {
		return errors.New("invalid Cloud state update")
	}
	b, err := cloudPayload(r, service.OpenAICloudMaxEnvelopeBytes)
	if err != nil {
		return err
	}
	keys := append(cloudRecordKeys(r), cloudKey("settlement", r.RootRequestID), cloudCachePrefix+"settlements")
	for _, ref := range refs {
		if ref == "" || len(ref) > 512 {
			return errors.New("invalid Cloud state reference")
		}
		keys = append(keys, cloudScopeRef(r.UserID, r.APIKeyID, r.GroupID, ref))
	}
	var sb []byte
	if settlement != nil {
		if settlement.RootRequestID != r.RootRequestID || settlement.SessionID != r.SessionID || settlement.OpenAICloudOwner != r.OpenAICloudOwner || settlement.Sequence != r.Sequence || !bytes.Equal(bytes.TrimSpace(settlement.BillingSnapshot), bytes.TrimSpace(r.BillingSnapshot)) || !cloudUsageEqual(settlement.Usage, r.Usage) {
			return errors.New("Cloud settlement ownership mismatch")
		}
		sb, err = cloudPayload(settlement, service.OpenAICloudMaxEnvelopeBytes)
		if err != nil {
			return err
		}
	}
	n, err := saveCloudSessionScript.Run(ctx, c.rdb, keys, r.OwnerToken, b, r.SessionID, service.OpenAICloudStateTTL.Milliseconds(), string(sb), service.OpenAICloudSettlementTTL.Milliseconds(), time.Now().UnixMilli()).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Cloud state owner lease or revision changed")
	}
	return nil
}

var renewCloudSessionScript = redis.NewScript(cloudEncodeRecordLua + `
if redis.call('GET',KEYS[2])~=ARGV[1] then return 0 end
local raw=redis.call('GET',KEYS[1]);if not raw then return 0 end
local r=cjson.decode(raw);if r.owner_token~=ARGV[1] then return 0 end
r.lease_until=ARGV[3];r.expires_at=ARGV[5];redis.call('SET',KEYS[1],encodeCloudRecord(r))
redis.call('PEXPIRE',KEYS[2],ARGV[2]);redis.call('ZADD',KEYS[3],ARGV[4],r.session_id)
return 1`)

func (c *gatewayCache) RenewCloudSession(ctx context.Context, id, owner string, until time.Time) (bool, error) {
	if err := cloudStorageReady(c); err != nil {
		return false, err
	}

	ttl := time.Until(until)
	if ttl <= 0 || ttl > service.OpenAICloudOwnerLease+time.Second {
		return false, errors.New("invalid Cloud lease duration")
	}
	expires := time.Now().Add(service.OpenAICloudSessionTTL)
	// Capacity indexes count live owners; their initial scores only order idle
	// candidates. Renew the record and lease atomically without reading the full
	// session payload back to Go or rewriting the three capacity indexes.
	n, err := renewCloudSessionScript.Run(ctx, c.rdb, []string{cloudKey("session", id), cloudKey("owner", id), cloudCachePrefix + "leases"}, owner, ttl.Milliseconds(), until.UTC().Format(time.RFC3339Nano), until.UnixMilli(), expires.UTC().Format(time.RFC3339Nano)).Int()
	return n == 1, err
}

var appendCloudStreamScript = redis.NewScript(`
if ARGV[5]=='event' and redis.call('EXISTS',KEYS[4])==1 then return -2 end
if ARGV[5]=='command' then
 if redis.call('EXISTS',KEYS[3])==0 then return -2 end
 local raw=redis.call('GET',KEYS[4]);if not raw then return -2 end
 local record=cjson.decode(raw);local command=cjson.decode(ARGV[1]);local owner=command.request.owner
 for _,key in ipairs({'user_id','api_key_id','group_id','account_id','grant_generation','model','settings_digest','probe'}) do if record[key]~=owner[key] then return -2 end end
end
local count=redis.call('XLEN',KEYS[1]);local bytes=tonumber(redis.call('GET',KEYS[2]) or '0')
if count>=tonumber(ARGV[2]) or bytes+string.len(ARGV[1])>tonumber(ARGV[3]) then return -1 end
redis.call('XADD',KEYS[1],'*','payload',ARGV[1]);redis.call('SET',KEYS[2],bytes+string.len(ARGV[1]),'PX',ARGV[4]);redis.call('PEXPIRE',KEYS[1],ARGV[4]);return 1`)

func (c *gatewayCache) cloudAppend(ctx context.Context, kind, id string, value any, maxEntries, maxBytes int) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}

	if id == "" || len(id) > 160 {
		return errors.New("invalid Cloud stream ID")
	}
	b, err := cloudPayload(value, service.OpenAICloudMaxEnvelopeBytes)
	if err != nil {
		return err
	}
	n, err := appendCloudStreamScript.Run(ctx, c.rdb, []string{cloudKey(kind, id), cloudKey(kind+"_bytes", id), cloudKey("owner", id), cloudStreamGuardKey(kind, id)}, b, maxEntries, maxBytes, service.OpenAICloudStateTTL.Milliseconds(), kind).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		if kind == "command" {
			// -2 (owner missing/mismatch) and -1 (bounded queue full) are
			// decided atomically before XADD: the command was not written.
			return service.ErrOpenAICloudCommandRejected
		}
		return errors.New("Cloud stream is unavailable or its bounded capacity is exhausted")
	}
	return nil
}

// Control commands (cancel, close) get reserved headroom above the business
// queue so a backlog of tool replies can never block an interrupt.
const (
	cloudControlCommandEntries = 16
	cloudControlCommandBytes   = 1 << 20
)

func (c *gatewayCache) EnqueueCloudCommand(ctx context.Context, id string, cmd service.OpenAICloudCommand) error {
	entries, bytes := service.OpenAICloudMaxCommands, service.OpenAICloudMaxEventStreamBytes
	if cmd.Cancel || cmd.CloseCompleted {
		entries, bytes = entries+cloudControlCommandEntries, bytes+cloudControlCommandBytes
	}
	return c.cloudAppend(ctx, "command", id, cmd, entries, bytes)
}

// RecordCloudLatency keeps the first gateway measurement for a billing root.
func (c *gatewayCache) RecordCloudLatency(ctx context.Context, root string, metrics *service.UsageLatencyMetrics) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}
	if root == "" || len(root) > 256 || metrics == nil {
		return errors.New("invalid Cloud latency record")
	}
	raw, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	return c.rdb.SetNX(ctx, cloudKey("latency", root), raw, service.OpenAICloudSettlementTTL).Err()
}

func (c *gatewayCache) LoadCloudLatency(ctx context.Context, root string) (*service.UsageLatencyMetrics, error) {
	if err := cloudStorageReady(c); err != nil {
		return nil, err
	}
	raw, err := c.rdb.Get(ctx, cloudKey("latency", root)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var metrics service.UsageLatencyMetrics
	if json.Unmarshal(raw, &metrics) != nil {
		return nil, errors.New("invalid Cloud latency record")
	}
	return &metrics, nil
}

// Only the fenced owner deletes the commands it has already read, and the byte
// budget is returned with them, so the bound applies to unconsumed work only.
var reclaimCloudCommandsScript = redis.NewScript(`
if redis.call('GET',KEYS[3])~=ARGV[1] then return -2 end
local rows=redis.call('XRANGE',KEYS[1],'-',ARGV[2])
local freed=0
for _,row in ipairs(rows) do
 local fields=row[2]
 for i=1,#fields,2 do if fields[i]=='payload' then freed=freed+string.len(fields[i+1]) end end
 redis.call('XDEL',KEYS[1],row[1])
end
if freed>0 and redis.call('EXISTS',KEYS[2])==1 and redis.call('DECRBY',KEYS[2],freed)<=0 then redis.call('DEL',KEYS[2]) end
return #rows`)

func (c *gatewayCache) ReclaimCloudCommands(ctx context.Context, id, owner, upTo string) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}
	if id == "" || len(id) > 160 || owner == "" || upTo == "" {
		return errors.New("invalid Cloud command reclaim")
	}
	n, err := reclaimCloudCommandsScript.Run(ctx, c.rdb, []string{cloudKey("command", id), cloudKey("command_bytes", id), cloudKey("owner", id)}, owner, upTo).Int()
	if err != nil {
		return err
	}
	if n < 0 {
		return errors.New("Cloud command queue owner changed")
	}
	return nil
}
func (c *gatewayCache) PublishCloudEvent(ctx context.Context, id string, event service.OpenAICloudStreamEvent) error {
	return c.cloudAppend(ctx, "event", id, event, service.OpenAICloudMaxStreamEntries, service.OpenAICloudMaxEventStreamBytes)
}

func (c *gatewayCache) cloudRead(ctx context.Context, kind, id, after string) ([]service.OpenAICloudStreamMessage, error) {
	if err := cloudStorageReady(c); err != nil {
		return nil, err
	}

	if id == "" || len(id) > 160 {
		return nil, errors.New("invalid Cloud stream ID")
	}
	if after == "" {
		after = "0-0"
	}
	rows, err := c.rdb.XRead(ctx, &redis.XReadArgs{Streams: []string{cloudKey(kind, id), after}, Count: 128, Block: time.Second}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []service.OpenAICloudStreamMessage
	for _, row := range rows {
		for _, m := range row.Messages {
			b, ok := m.Values["payload"].(string)
			if !ok || len(b) > service.OpenAICloudMaxEnvelopeBytes {
				return nil, errors.New("invalid Cloud stream payload")
			}
			out = append(out, service.OpenAICloudStreamMessage{ID: m.ID, Payload: json.RawMessage(b)})
		}
	}
	return out, nil
}
func (c *gatewayCache) ReadCloudCommands(ctx context.Context, id, after string) ([]service.OpenAICloudStreamMessage, error) {
	return c.cloudRead(ctx, "command", id, after)
}
func (c *gatewayCache) ReadCloudEvents(ctx context.Context, id, after string) ([]service.OpenAICloudStreamMessage, error) {
	return c.cloudRead(ctx, "event", id, after)
}

func (c *gatewayCache) ListCloudSettlements(ctx context.Context, limit int) ([]*service.OpenAICloudSettlement, error) {
	if err := cloudStorageReady(c); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > service.OpenAICloudMaxSessions*2 {
		limit = service.OpenAICloudMaxSessions * 2
	}
	// Drain both queues during upgrades. Unknown records written by the previous
	// version must not remain forever after the durable archive is introduced.
	out := make([]*service.OpenAICloudSettlement, 0, limit)
	seen := make(map[string]bool)
	for _, queue := range []string{"settlements", "unknown_settlements"} {
		ids, err := c.rdb.ZRange(ctx, cloudCachePrefix+queue, 0, int64(max(1, limit/2)-1)).Result()
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			b, err := c.rdb.Get(ctx, cloudKey("settlement", id)).Bytes()
			if errors.Is(err, redis.Nil) {
				if e := c.rdb.ZRem(ctx, cloudCachePrefix+queue, id).Err(); e != nil {
					return nil, e
				}
				continue
			}
			if err != nil {
				return nil, err
			}
			var v service.OpenAICloudSettlement
			if len(b) > service.OpenAICloudMaxEnvelopeBytes || json.Unmarshal(b, &v) != nil || v.RootRequestID != id {
				// Isolate this record while continuing valid records. Preserve every
				// original byte for SQL archival instead of silently dropping it.
				v = service.OpenAICloudSettlement{OpenAICloudBinding: service.OpenAICloudBinding{RootRequestID: id}, ArchiveReason: "invalid_record"}
			}
			v.ArchivePayload = b
			out = append(out, &v)
		}
	}
	return out, nil
}

func (c *gatewayCache) GetCloudSettlement(ctx context.Context, id string) (*service.OpenAICloudSettlement, error) {
	if err := cloudStorageReady(c); err != nil {
		return nil, err
	}
	if id == "" || len(id) > 160 {
		return nil, errors.New("invalid Cloud billing root")
	}
	b, err := c.rdb.Get(ctx, cloudKey("settlement", id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var settlement service.OpenAICloudSettlement
	if len(b) > service.OpenAICloudMaxEnvelopeBytes || json.Unmarshal(b, &settlement) != nil || settlement.RootRequestID != id {
		return nil, errors.New("invalid Cloud settlement record")
	}
	return &settlement, nil
}

var claimCloudSettlementScript = redis.NewScript(`
if redis.call('EXISTS',KEYS[1])==0 then return 0 end
local previous=redis.call('GET',KEYS[2]);if previous and previous~=ARGV[1] then return 0 end
redis.call('SET',KEYS[2],ARGV[1],'PX',ARGV[2]);return 1`)

func (c *gatewayCache) ClaimCloudSettlement(ctx context.Context, id, worker string, ttl time.Duration) (bool, error) {
	if err := cloudStorageReady(c); err != nil {
		return false, err
	}

	if worker == "" || ttl <= 0 || ttl > time.Minute {
		return false, errors.New("invalid Cloud settlement claim")
	}
	n, err := claimCloudSettlementScript.Run(ctx, c.rdb, []string{cloudKey("settlement", id), cloudKey("settlement_claim", id)}, worker, ttl.Milliseconds()).Int()
	return n == 1, err
}

var ackCloudSettlementScript = redis.NewScript(`
if redis.call('GET',KEYS[2])~=ARGV[1] then return 0 end
local raw=redis.call('GET',KEYS[1]);if not raw then return 0 end
local pending=cjson.decode(raw);if pending.unknown==true or pending.usage==cjson.null or pending.usage==nil then return -1 end
if tonumber(pending.account_id)~=tonumber(ARGV[4]) then return -1 end
redis.call('ZREM',KEYS[5],ARGV[2])
redis.call('SET',KEYS[4],'1','PX',ARGV[3]);redis.call('DEL',KEYS[1],KEYS[2]);redis.call('ZREM',KEYS[3],ARGV[2]);return 1`)

func (c *gatewayCache) AckCloudSettlement(ctx context.Context, id, worker string) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}

	pending, err := c.GetCloudSettlement(ctx, id)
	if err != nil {
		return err
	}
	if pending == nil {
		return errors.New("Cloud settlement is unavailable")
	}
	n, err := ackCloudSettlementScript.Run(ctx, c.rdb, []string{cloudKey("settlement", id), cloudKey("settlement_claim", id), cloudCachePrefix + "settlements", cloudKey("settled", id), cloudCachePrefix + "unknown_settlements"}, worker, id, service.OpenAICloudStateTTL.Milliseconds(), pending.AccountID).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Cloud settlement lease changed before acknowledgement")
	}
	return nil
}

func (c *gatewayCache) ListExpiredCloudSessions(ctx context.Context, limit int) ([]*service.OpenAICloudSessionRecord, error) {
	if err := cloudStorageReady(c); err != nil {
		return nil, err
	}

	if limit <= 0 || limit > 128 {
		limit = 128
	}
	ids, err := c.rdb.ZRangeByScore(ctx, cloudCachePrefix+"leases", &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(time.Now().UnixMilli(), 10), Count: int64(limit)}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]*service.OpenAICloudSessionRecord, 0, len(ids))
	for _, id := range ids {
		r, err := c.LoadCloudSession(ctx, id)
		if err != nil {
			return nil, err
		}
		if r == nil {
			if err = c.rdb.ZRem(ctx, cloudCachePrefix+"leases", id).Err(); err != nil {
				return nil, err
			}
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

var claimCloudSessionScript = redis.NewScript(cloudEncodeRecordLua + `
if redis.call('EXISTS',KEYS[2])==1 then return '' end
local raw=redis.call('GET',KEYS[1]);if not raw then return '' end
local r=cjson.decode(raw);if r.status=='closed' or r.status=='failed' or r.status=='unknown' or r.status=='interrupted' then return '' end
r.owner_token=ARGV[1];r.owner_instance=ARGV[1];r.lease_until=ARGV[3]
local updated=encodeCloudRecord(r);redis.call('SET',KEYS[1],updated);redis.call('SET',KEYS[2],ARGV[1],'PX',ARGV[2]);redis.call('ZADD',KEYS[3],ARGV[4],r.session_id);return updated`)

func (c *gatewayCache) ClaimCloudSession(ctx context.Context, id, worker string, ttl time.Duration) (*service.OpenAICloudSessionRecord, bool, error) {
	if err := cloudStorageReady(c); err != nil {
		return nil, false, err
	}

	if worker == "" || ttl <= 0 || ttl > service.OpenAICloudOwnerLease {
		return nil, false, errors.New("invalid Cloud owner claim")
	}
	until := time.Now().Add(ttl)
	b, err := claimCloudSessionScript.Run(ctx, c.rdb, []string{cloudKey("session", id), cloudKey("owner", id), cloudCachePrefix + "leases"}, worker, ttl.Milliseconds(), until.UTC().Format(time.RFC3339Nano), until.UnixMilli(), service.OpenAICloudStateTTL.Milliseconds()).Text()
	if err != nil {
		return nil, false, err
	}
	if b == "" {
		return nil, false, nil
	}
	var r service.OpenAICloudSessionRecord
	if json.Unmarshal([]byte(b), &r) != nil {
		return nil, false, errors.New("invalid recovered Cloud session")
	}
	return &r, true, nil
}

// Receipts establish that a different instance actually completed claim/ack;
// they contain no billing or credential data and are not the billing ledger.
func (c *gatewayCache) CloudSettlementAcknowledged(ctx context.Context, id string) (bool, error) {
	if err := cloudStorageReady(c); err != nil {
		return false, err
	}

	v, err := c.rdb.Get(ctx, cloudKey("settled", id)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if v == "failed" {
		return false, service.ErrOpenAICloudProbeSettlementFailed
	}
	return v == "1", nil
}

// Failed probes have no customer billing liability. They retain a failed
// receipt but cannot block an account or masquerade as a successful ACK.
var failCloudProbeSettlementScript = redis.NewScript(`
if redis.call('GET',KEYS[2])~=ARGV[1] then return 0 end
local raw=redis.call('GET',KEYS[1]);if not raw then return 0 end
local record=cjson.decode(raw);if record.probe~=true or record.api_key_id~=0 then return 0 end
redis.call('SET',KEYS[4],'failed','PX',ARGV[3]);redis.call('DEL',KEYS[1],KEYS[2]);redis.call('ZREM',KEYS[3],ARGV[2]);return 1`)

func (c *gatewayCache) FailCloudProbeSettlement(ctx context.Context, id, worker string) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}
	n, err := failCloudProbeSettlementScript.Run(ctx, c.rdb, []string{cloudKey("settlement", id), cloudKey("settlement_claim", id), cloudCachePrefix + "settlements", cloudKey("settled", id)}, worker, id, service.OpenAICloudStateTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("failed Cloud probe claim is unavailable")
	}
	return nil
}

func cloudStorageReady(c *gatewayCache) error {
	if c == nil || c.rdb == nil {
		return errors.New("Cloud Redis storage is unavailable")
	}
	return nil
}
func cloudUsageEqual(a, b *service.OpenAICloudUsage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.InputTokens == b.InputTokens && a.OutputTokens == b.OutputTokens && a.CachedInputTokens == b.CachedInputTokens && a.CacheWriteInputTokens == b.CacheWriteInputTokens && a.TotalTokens == b.TotalTokens && a.WebSearchCalls == b.WebSearchCalls && a.ImageCount == b.ImageCount && maps.Equal(a.ImageSizes, b.ImageSizes) && slices.Equal(a.ImageOutputSizes, b.ImageOutputSizes)
}
func cloudStreamGuardKey(kind, id string) string {
	if kind == "command" {
		return cloudKey("session", id)
	}
	return cloudKey("event_closed", id)
}

// The existing event tombstone is also the request cancellation fence. It
// survives deletion of the stream and is independent of actor command polling.
func (c *gatewayCache) CloudRequestClosed(ctx context.Context, id string) (bool, error) {
	if err := cloudStorageReady(c); err != nil {
		return false, err
	}
	if id == "" || len(id) > 160 {
		return false, errors.New("invalid Cloud request ID")
	}
	n, err := c.rdb.Exists(ctx, cloudKey("event_closed", id)).Result()
	return n != 0, err
}

func (c *gatewayCache) DeleteCloudEvents(ctx context.Context, id string) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}
	if id == "" || len(id) > 160 {
		return errors.New("invalid Cloud event stream")
	}
	_, err := c.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, cloudKey("event_closed", id), "1", service.OpenAICloudStateTTL)
		p.Del(ctx, cloudKey("event", id), cloudKey("event_bytes", id))
		return nil
	})
	return err
}

var releaseCloudSessionScript = redis.NewScript(cloudEncodeRecordLua + `
if redis.call('GET',KEYS[2])~=ARGV[1] then return 0 end
local raw=redis.call('GET',KEYS[1]);if not raw then return 0 end
local record=cjson.decode(raw)
if record.owner_token~=ARGV[1] or record.status~='completed' then return 0 end
record.status='closed';redis.call('SET',KEYS[1],encodeCloudRecord(record),'PX',ARGV[3])
redis.call('DEL',KEYS[2]);for i=3,6 do redis.call('ZREM',KEYS[i],ARGV[2]) end;return 1`)

func (c *gatewayCache) ReleaseCloudSession(ctx context.Context, id, owner string) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}
	r, err := c.LoadCloudSession(ctx, id)
	if err != nil {
		return err
	}
	if r == nil {
		return errors.New("Cloud session is unavailable")
	}
	n, err := releaseCloudSessionScript.Run(ctx, c.rdb, cloudRecordKeys(r), owner, id, service.OpenAICloudStateTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("only the live owner may release a completed Cloud session")
	}
	return nil
}

// SQL archival precedes this CAS. An archived marker is not an ACK; it only
// permits other instances to release execution reservations. Redis never drops
// a changed record or a record claimed by another worker.
var archiveCloudSettlementScript = redis.NewScript(`
local claimant=redis.call('GET',KEYS[2]);if claimant and claimant~=ARGV[1] then return 0 end
local raw=redis.call('GET',KEYS[1])
if not raw then return redis.call('EXISTS',KEYS[5]) end
if raw~=ARGV[3] then return 0 end
redis.call('SET',KEYS[5],'1','PX',ARGV[4])
redis.call('DEL',KEYS[1],KEYS[2]);redis.call('ZREM',KEYS[3],ARGV[2]);redis.call('ZREM',KEYS[4],ARGV[2]);return 1`)

func (c *gatewayCache) ArchiveCloudSettlement(ctx context.Context, id, worker string, payload []byte) error {
	if err := cloudStorageReady(c); err != nil {
		return err
	}
	if id == "" || worker == "" {
		return errors.New("invalid Cloud settlement archive")
	}
	n, err := archiveCloudSettlementScript.Run(ctx, c.rdb, []string{cloudKey("settlement", id), cloudKey("settlement_claim", id), cloudCachePrefix + "settlements", cloudCachePrefix + "unknown_settlements", cloudKey("archived", id)}, worker, id, payload, service.OpenAICloudSettlementTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Cloud settlement changed before archival")
	}
	return nil
}

func (c *gatewayCache) CloudSettlementArchived(ctx context.Context, id string) (bool, error) {
	if err := cloudStorageReady(c); err != nil {
		return false, err
	}
	n, err := c.rdb.Exists(ctx, cloudKey("archived", id)).Result()
	return n == 1, err
}

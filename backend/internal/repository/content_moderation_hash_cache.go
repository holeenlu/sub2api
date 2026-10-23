package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const contentModerationFlaggedHashSetKey = "content_moderation:flagged_hashes"

var moderationGuardKeys = []string{"content_moderation:{guard-memory}:data", "content_moderation:{guard-memory}:refusal", "content_moderation:{guard-memory}:device", "content_moderation:{guard-memory}:revision", "content_moderation:{guard-memory}:generation"}

func (c *contentModerationHashCache) LoadGuardState(ctx context.Context, revision string) (string, []service.ContentModerationGuardRecord, bool, error) {
	values, err := c.rdb.Eval(ctx, `
local removed=0
for i=2,3 do
 local stale=redis.call('ZRANGEBYSCORE',KEYS[i],'-inf',ARGV[1])
 for _,key in ipairs(stale) do redis.call('HDEL',KEYS[1],key);redis.call('ZREM',KEYS[i],key);removed=removed+1 end
end
if removed>0 then redis.call('INCR',KEYS[4]) end
local revision=(redis.call('GET',KEYS[4]) or '0')..':'..(redis.call('GET',KEYS[5]) or '0')
if revision==ARGV[2] then return {revision} end
local out={revision}
for _,value in ipairs(redis.call('HVALS',KEYS[1])) do table.insert(out,value) end
return out
`, moderationGuardKeys, time.Now().UnixMilli(), revision).Slice()
	if err != nil {
		return "", nil, false, err
	}
	if len(values) == 0 {
		return "", nil, false, fmt.Errorf("invalid shared guard state")
	}
	next := fmt.Sprint(values[0])
	if next == revision {
		return next, nil, false, nil
	}
	records := make([]service.ContentModerationGuardRecord, 0, len(values)-1)
	for _, value := range values[1:] {
		var record service.ContentModerationGuardRecord
		if err := json.Unmarshal([]byte(fmt.Sprint(value)), &record); err != nil {
			return "", nil, false, err
		}
		records = append(records, record)
	}
	return next, records, true, nil
}

func (c *contentModerationHashCache) StoreGuardEntries(ctx context.Context, records []service.ContentModerationGuardRecord, refusalLimit, deviceLimit int) error {
	pipe := c.rdb.Pipeline()
	for _, record := range records {
		generation := record.Generation
		if generation == "" {
			generation = "0"
		}
		if record.Kind != "refusal" && record.Kind != "device" {
			return fmt.Errorf("invalid guard kind")
		}
		limit := refusalLimit
		index := 2
		if record.Kind == "device" {
			limit = deviceLimit
			index = 3
		}
		if limit < 1 || limit > 4096 {
			return fmt.Errorf("invalid guard capacity")
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		pipe.Eval(ctx, `
if (redis.call('GET',KEYS[5]) or '0')~=ARGV[7] then return 0 end
if tonumber(ARGV[4])<=tonumber(ARGV[5]) then return 0 end
local index=tonumber(ARGV[1])
local stale=redis.call('ZRANGEBYSCORE',KEYS[index],'-inf',ARGV[5])
for _,key in ipairs(stale) do redis.call('HDEL',KEYS[1],key);redis.call('ZREM',KEYS[index],key) end
if index==3 and redis.call('HEXISTS',KEYS[1],ARGV[2])==1 then return 0 end
redis.call('HSET',KEYS[1],ARGV[2],ARGV[3])
redis.call('ZADD',KEYS[index],ARGV[4],ARGV[2])
local excess=redis.call('ZCARD',KEYS[index])-tonumber(ARGV[6])
if excess>0 then
 local oldest=redis.call('ZRANGE',KEYS[index],0,excess-1)
 for _,key in ipairs(oldest) do redis.call('HDEL',KEYS[1],key);redis.call('ZREM',KEYS[index],key) end
end
redis.call('INCR',KEYS[4])
return 1
`, moderationGuardKeys, index, record.Kind+":"+record.Key, string(raw), record.ExpiresMS, time.Now().UnixMilli(), limit, generation)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *contentModerationHashCache) ClearGuardEntries(ctx context.Context) (int64, error) {
	return c.rdb.Eval(ctx, `
local count=redis.call('HLEN',KEYS[1])
redis.call('DEL',KEYS[1],KEYS[2],KEYS[3])
redis.call('INCR',KEYS[4])
redis.call('INCR',KEYS[5])
return count
`, moderationGuardKeys).Int64()
}

type contentModerationHashCache struct {
	rdb *redis.Client
}

func NewContentModerationHashCache(rdb *redis.Client) service.ContentModerationHashCache {
	return &contentModerationHashCache{rdb: rdb}
}

func (c *contentModerationHashCache) RecordFlaggedInputHash(ctx context.Context, inputHash string) error {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return nil
	}
	return c.rdb.SAdd(ctx, contentModerationFlaggedHashSetKey, inputHash).Err()
}

func (c *contentModerationHashCache) HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	return c.rdb.SIsMember(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
}

func (c *contentModerationHashCache) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	deleted, err := c.rdb.SRem(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (c *contentModerationHashCache) ClearFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	deleted, err := c.rdb.SCard(ctx, contentModerationFlaggedHashSetKey).Result()
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, nil
	}
	if err := c.rdb.Del(ctx, contentModerationFlaggedHashSetKey).Err(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (c *contentModerationHashCache) CountFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	return c.rdb.SCard(ctx, contentModerationFlaggedHashSetKey).Result()
}

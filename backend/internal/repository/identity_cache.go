package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	fingerprintKeyPrefix     = "fingerprint:"
	fingerprintTTL           = 7 * 24 * time.Hour // 7天，配合每24小时懒续期可保持活跃账号永不过期
	sessionMaskSeedKeyPrefix = "session_mask_seed:"
	sessionMaskSeedTTL       = 15 * time.Minute
)

// fingerprintKey generates the Redis key for account fingerprint cache.
func fingerprintKey(accountID int64) string {
	return fmt.Sprintf("%s%d", fingerprintKeyPrefix, accountID)
}

// sessionMaskSeedKey isolates seeds from obsolete fixed-session cache entries.
func sessionMaskSeedKey(accountID int64) string {
	return fmt.Sprintf("%s%d", sessionMaskSeedKeyPrefix, accountID)
}

type identityCache struct {
	rdb *redis.Client
}

func NewIdentityCache(rdb *redis.Client) service.IdentityCache {
	return &identityCache{rdb: rdb}
}

func (c *identityCache) GetFingerprint(ctx context.Context, accountID int64) (*service.Fingerprint, error) {
	key := fingerprintKey(accountID)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var fp service.Fingerprint
	if err := json.Unmarshal([]byte(val), &fp); err != nil {
		return nil, err
	}
	return &fp, nil
}

func (c *identityCache) SetFingerprint(ctx context.Context, accountID int64, fp *service.Fingerprint) error {
	key := fingerprintKey(accountID)
	val, err := json.Marshal(fp)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, val, fingerprintTTL).Err()
}

// Adapted from sub4api a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e.
var getOrCreateSessionMaskSeedScript = redis.NewScript(`
local seed = redis.call('GET', KEYS[1])
if not seed then
  if ARGV[1] == '' then
    return ''
  end
  seed = ARGV[1]
  redis.call('SET', KEYS[1], seed, 'PX', ARGV[2])
else
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return seed
`)

func (c *identityCache) GetOrCreateSessionMaskSeed(ctx context.Context, accountID int64, candidate string) (string, error) {
	return getOrCreateSessionMaskSeedScript.Run(ctx, c.rdb, []string{sessionMaskSeedKey(accountID)}, candidate, sessionMaskSeedTTL.Milliseconds()).Text()
}

func claudeCodeHeadersKey(accountID int64) string {
	return fmt.Sprintf("claude_code_headers:%d", accountID)
}

func (c *identityCache) GetClaudeCodeHeaders(ctx context.Context, accountID int64) (http.Header, error) {
	fields, err := c.rdb.HGetAll(ctx, claudeCodeHeadersKey(accountID)).Result()
	if err != nil {
		return nil, err
	}
	headers := make(http.Header, len(fields))
	for name, encoded := range fields {
		var values []string
		if err := json.Unmarshal([]byte(encoded), &values); err != nil {
			return nil, fmt.Errorf("decode Claude Code header %q: %w", name, err)
		}
		headers[name] = values
	}
	return headers, nil
}

func (c *identityCache) UpdateClaudeCodeHeaders(ctx context.Context, accountID int64, headers http.Header) error {
	if len(headers) == 0 {
		return nil
	}
	fields := make(map[string]interface{}, len(headers))
	for name, values := range headers {
		encoded, err := json.Marshal(values)
		if err != nil {
			return err
		}
		fields[name] = string(encoded)
	}
	// Partial HSET and expiry execute together, without a read/modify/write
	// race that could discard concurrent updates to other fields.
	key := claudeCodeHeadersKey(accountID)
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, fingerprintTTL)
	_, err := pipe.Exec(ctx)
	return err
}

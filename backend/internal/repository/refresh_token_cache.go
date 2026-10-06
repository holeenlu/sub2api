package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	refreshTokenKeyPrefix   = "refresh_token:"
	userRefreshTokensPrefix = "user_refresh_tokens:"
	tokenFamilyPrefix       = "token_family:"
)

// refreshTokenKey generates the Redis key for a refresh token.
func refreshTokenKey(tokenHash string) string {
	return refreshTokenKeyPrefix + tokenHash
}

// userRefreshTokensKey generates the Redis key for user's token set.
func userRefreshTokensKey(userID int64) string {
	return fmt.Sprintf("%s%d", userRefreshTokensPrefix, userID)
}

// tokenFamilyKey generates the Redis key for token family set.
func tokenFamilyKey(familyID string) string {
	return tokenFamilyPrefix + familyID
}

type refreshTokenCache struct {
	rdb *redis.Client
}

// NewRefreshTokenCache creates a new RefreshTokenCache implementation.
func NewRefreshTokenCache(rdb *redis.Client) service.RefreshTokenCache {
	return &refreshTokenCache{rdb: rdb}
}

func (c *refreshTokenCache) StoreRefreshToken(ctx context.Context, tokenHash string, data *service.RefreshTokenData, ttl time.Duration) error {
	key := refreshTokenKey(tokenHash)
	val, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal refresh token data: %w", err)
	}
	result, err := c.rdb.Eval(ctx, `
 if redis.call('EXISTS', KEYS[4]) == 1 then return 0 end
 redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
 redis.call('SADD', KEYS[2], ARGV[3])
 redis.call('PEXPIRE', KEYS[2], math.max(redis.call('PTTL', KEYS[2]), tonumber(ARGV[2])))
 redis.call('SADD', KEYS[3], ARGV[3])
 redis.call('PEXPIRE', KEYS[3], math.max(redis.call('PTTL', KEYS[3]), tonumber(ARGV[2]), tonumber(ARGV[4])))
 return 1`, []string{key, userRefreshTokensKey(data.UserID), tokenFamilyKey(data.FamilyID), "revoked_family:" + data.FamilyID}, val, ttl.Milliseconds(), tokenHash, data.FamilyTTLMillis).Int64()
	if err != nil {
		return err
	}
	if result != 1 {
		return service.ErrTokenRevoked
	}
	return nil
}

func (c *refreshTokenCache) GetRefreshToken(ctx context.Context, tokenHash string) (*service.RefreshTokenData, error) {
	key := refreshTokenKey(tokenHash)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrRefreshTokenNotFound
		}
		return nil, err
	}
	var data service.RefreshTokenData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, fmt.Errorf("unmarshal refresh token data: %w", err)
	}
	return &data, nil
}

func (c *refreshTokenCache) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	key := refreshTokenKey(tokenHash)
	return c.rdb.Del(ctx, key).Err()
}

func (c *refreshTokenCache) DeleteUserRefreshTokens(ctx context.Context, userID int64) error {
	// Get all token hashes for this user
	tokenHashes, err := c.GetUserTokenHashes(ctx, userID)
	if err != nil && err != redis.Nil {
		return fmt.Errorf("get user token hashes: %w", err)
	}

	if len(tokenHashes) == 0 {
		return nil
	}

	// Build keys to delete
	keys := make([]string, 0, len(tokenHashes)+1)
	for _, hash := range tokenHashes {
		keys = append(keys, refreshTokenKey(hash))
	}
	keys = append(keys, userRefreshTokensKey(userID))

	// Delete all keys in a pipeline
	pipe := c.rdb.Pipeline()
	for _, key := range keys {
		pipe.Del(ctx, key)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) DeleteTokenFamily(ctx context.Context, familyID string) error {
	return c.RevokeTokenFamily(ctx, familyID, 24*time.Hour)
}

func (c *refreshTokenCache) RevokeTokenFamily(ctx context.Context, familyID string, minTTL time.Duration) error {
	return c.rdb.Eval(ctx, `
 local ttl = math.max(redis.call('PTTL', KEYS[1]), redis.call('PTTL', KEYS[2]), tonumber(ARGV[1]), 60000)
 redis.call('SET', KEYS[2], '1', 'PX', ttl)
 for _, hash in ipairs(redis.call('SMEMBERS', KEYS[1])) do redis.call('DEL', 'refresh_token:' .. hash) end
 redis.call('DEL', KEYS[1])
 return 1`, []string{tokenFamilyKey(familyID), "revoked_family:" + familyID}, minTTL.Milliseconds()).Err()
}

func (c *refreshTokenCache) AddToUserTokenSet(ctx context.Context, userID int64, tokenHash string, ttl time.Duration) error {
	key := userRefreshTokensKey(userID)
	pipe := c.rdb.Pipeline()
	pipe.SAdd(ctx, key, tokenHash)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) AddToFamilyTokenSet(ctx context.Context, familyID string, tokenHash string, ttl time.Duration) error {
	key := tokenFamilyKey(familyID)
	pipe := c.rdb.Pipeline()
	pipe.SAdd(ctx, key, tokenHash)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) GetUserTokenHashes(ctx context.Context, userID int64) ([]string, error) {
	key := userRefreshTokensKey(userID)
	return c.rdb.SMembers(ctx, key).Result()
}

func (c *refreshTokenCache) GetFamilyTokenHashes(ctx context.Context, familyID string) ([]string, error) {
	key := tokenFamilyKey(familyID)
	return c.rdb.SMembers(ctx, key).Result()
}

func (c *refreshTokenCache) IsTokenInFamily(ctx context.Context, familyID string, tokenHash string) (bool, error) {
	key := tokenFamilyKey(familyID)
	return c.rdb.SIsMember(ctx, key, tokenHash).Result()
}

// ConsumeRefreshToken atomically claims the capability and remembers its family for replay.
// Family tombstones and StoreRefreshToken's check serialize replay/revocation against rotation.
func (c *refreshTokenCache) ConsumeRefreshToken(ctx context.Context, tokenHash string) (*service.RefreshTokenData, error) {
	raw, err := c.rdb.Eval(ctx, `
 local raw = redis.call('GET', KEYS[1])
 if not raw then
  local used = redis.call('GET', KEYS[2])
  if used then
   local data = cjson.decode(used)
   local family = 'token_family:' .. data.family_id
   local ttl = math.max(redis.call('PTTL', family), redis.call('PTTL', KEYS[2]), redis.call('PTTL', 'revoked_family:' .. data.family_id), data.family_ttl_ms or 0, 60000)
   redis.call('SET', 'revoked_family:' .. data.family_id, '1', 'PX', ttl)
   for _, hash in ipairs(redis.call('SMEMBERS', family)) do redis.call('DEL', 'refresh_token:' .. hash) end
   redis.call('DEL', family)
  end
  return false
 end
 local data = cjson.decode(raw)
 if redis.call('EXISTS', 'revoked_family:' .. data.family_id) == 1 then return false end
 local ttl = redis.call('PTTL', KEYS[1])
 if ttl <= 0 then return false end
 redis.call('SET', KEYS[2], raw, 'PX', ttl)
 redis.call('DEL', KEYS[1])
 return raw`, []string{refreshTokenKey(tokenHash), "used_refresh_token:" + tokenHash}).Text()
	if err == redis.Nil {
		return nil, service.ErrRefreshTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	var data service.RefreshTokenData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *refreshTokenCache) IsFamilyRevoked(ctx context.Context, familyID string) (bool, error) {
	n, err := c.rdb.Exists(ctx, "revoked_family:"+familyID).Result()
	return n != 0, err
}

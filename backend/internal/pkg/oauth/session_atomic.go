package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/redis/go-redis/v9"
	"time"
)

var ErrSessionConflict = errors.New("OAuth session expired, changed or already claimed")

// Shared sessions fail closed on Redis errors. The CAS compares the complete
// validated binding before permitting a remote side effect.
func ReadSharedSession[T any](ctx context.Context, rdb *redis.Client, key string) (*T, string, error) {
	if rdb == nil {
		return nil, "", ErrSessionConflict
	}
	raw, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, "", err
	}
	var value T
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, "", err
	}
	return &value, raw, nil
}
func PutSharedSession(ctx context.Context, rdb *redis.Client, key string, value any, ttl time.Duration) error {
	if rdb == nil || ttl <= 0 {
		return ErrSessionConflict
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ok, err := rdb.SetNX(ctx, key, raw, ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrSessionConflict
	}
	return nil
}

var sessionCASScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current or current ~= ARGV[1] then return 0 end
if ARGV[2] == '' then redis.call('DEL', KEYS[1]); return 1 end
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ttl)
return 1
`)

// nil replacement consumes; rebinding/result publication preserve expiry.
func CompareSwapSession(ctx context.Context, rdb *redis.Client, key, expected string, replacement any) (string, error) {
	if rdb == nil {
		return "", ErrSessionConflict
	}
	next := ""
	if replacement != nil {
		b, err := json.Marshal(replacement)
		if err != nil {
			return "", err
		}
		next = string(b)
	}
	n, err := sessionCASScript.Run(ctx, rdb, []string{key}, expected, next).Int()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", ErrSessionConflict
	}
	return next, nil
}

// Consume also prevents concurrent exchanges in single-process deployments.
func (s *SessionStore) Consume(id string, expected *OAuthSession) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[id]
	if !ok || current != expected || time.Since(current.CreatedAt) > SessionTTL {
		return false
	}
	delete(s.sessions, id)
	return true
}

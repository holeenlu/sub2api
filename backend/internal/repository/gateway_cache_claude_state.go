package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

func claudeStateStorage(kind string) (prefix string, ttl time.Duration, limit int, err error) {
	switch kind {
	case service.ClaudeStateSignatures:
		return "claude_state:{signatures}:", 30 * 24 * time.Hour, 100000, nil
	case service.ClaudeStateThreads:
		return "claude_state:{threads}:", 24 * time.Hour, 10000, nil
	default:
		return "", 0, 0, fmt.Errorf("invalid Claude state kind")
	}
}
func validClaudeStateKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	for _, r := range key {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (c *gatewayCache) GetClaudeConversationState(ctx context.Context, kind string, keys []string) (map[string]string, error) {
	prefix, ttl, _, err := claudeStateStorage(kind)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	if len(keys) > 64 {
		return nil, fmt.Errorf("too many Claude state keys")
	}
	wire := make([]string, len(keys))
	for n, key := range keys {
		if !validClaudeStateKey(key) {
			return nil, fmt.Errorf("invalid Claude state key")
		}
		wire[n] = prefix + key
	}
	var values []any
	if kind == service.ClaudeStateSignatures {
		// Match the source's daily read refresh without a write on every lookup.
		// The value and expiry index are renewed atomically; ownership never changes.
		args := []any{time.Now().Unix(), int(ttl.Seconds()), int((24 * time.Hour).Seconds())}
		for _, key := range keys {
			args = append(args, key)
		}
		values, err = readClaudeSignaturesScript.Run(ctx, c.rdb, append([]string{prefix + "index"}, wire...), args...).Slice()
	} else {
		values, err = c.rdb.MGet(ctx, wire...).Result()
	}
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for n, value := range values {
		if text, ok := value.(string); ok {
			result[keys[n]] = text
		}
	}
	return result, nil
}

var readClaudeSignaturesScript = redis.NewScript(strings.Join([]string{
	"local result={}; local now=tonumber(ARGV[1]); local ttl=tonumber(ARGV[2]); local refresh=tonumber(ARGV[3])",
	"for i=2,#KEYS do",
	" local value=redis.call('GET',KEYS[i]); result[#result+1]=value",
	" if value and redis.call('TTL',KEYS[i])<=ttl-refresh then",
	"  redis.call('EXPIRE',KEYS[i],ttl); redis.call('ZADD',KEYS[1],now+ttl,ARGV[i+2]); redis.call('EXPIRE',KEYS[1],ttl)",
	" end",
	"end",
	"return result",
}, "\n"))

// Entry TTLs and a bounded expiry index prevent client-generated message IDs
// from growing Redis without limit. Signature ownership is first-writer-wins;
// daily reads renew its lifetime; another owner cannot replace it.
var storeClaudeStateScript = redis.NewScript(strings.Join([]string{
	"local now=tonumber(ARGV[1]); local ttl=tonumber(ARGV[2]); local cap=tonumber(ARGV[3])",
	"local prefix=ARGV[4]; local preserve=ARGV[5]=='1'",
	"redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)",
	"for i=6,#ARGV,2 do",
	" local key=prefix..ARGV[i]; local old=redis.call('GET',key)",
	" if not preserve or not old then",
	"  redis.call('SET',key,ARGV[i+1],'EX',ttl)",
	"  redis.call('ZADD',KEYS[1],now+ttl,ARGV[i])",
	" end",
	"end",
	"local excess=redis.call('ZCARD',KEYS[1])-cap",
	"if excess>0 then",
	" local victims=redis.call('ZRANGE',KEYS[1],0,excess-1)",
	" for _,victim in ipairs(victims) do redis.call('DEL',prefix..victim); redis.call('ZREM',KEYS[1],victim) end",
	"end",
	"redis.call('EXPIRE',KEYS[1],ttl)",
	"return 1",
}, "\n"))

func (c *gatewayCache) StoreClaudeConversationState(ctx context.Context, kind string, values map[string]string) error {
	prefix, ttl, limit, err := claudeStateStorage(kind)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	if len(values) > 64 {
		return fmt.Errorf("too many Claude state entries")
	}
	preserve := "0"
	if kind == service.ClaudeStateSignatures {
		preserve = "1"
	}
	args := []any{time.Now().Unix(), int(ttl.Seconds()), limit, prefix, preserve}
	for key, value := range values {
		if !validClaudeStateKey(key) || len(value) > 16384 {
			return fmt.Errorf("invalid Claude state entry")
		}
		args = append(args, key, value)
	}
	return storeClaudeStateScript.Run(ctx, c.rdb, []string{prefix + "index"}, args...).Err()
}

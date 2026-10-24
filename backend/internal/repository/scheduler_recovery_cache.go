package repository

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// One key per failure scope: Redis Cluster never executes a cross-slot script.
// Epoch invalidates delayed results; a random owner fences release and renewal.
var recoveryProbeScript = redis.NewScript(`
redis.replicate_commands()
local k, op = KEYS[1], ARGV[1]
local t = redis.call('TIME')
local now = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
local epoch = tonumber(redis.call('HGET', k, 'epoch') or '0')
local function block(untilTime)
 epoch = epoch + 1
 redis.call('HSET', k, 'epoch', epoch, 'until', untilTime, 'recovered', 0)
 redis.call('HDEL', k, 'owner', 'lease', 'lease_epoch')
 redis.call('PEXPIRE', k, 108000000 + math.max(0, untilTime-now))
end
if op == 'block' then block(now+tonumber(ARGV[2])); return 1 end
local owner = ARGV[2]
if op == 'acquire' then
 local observed = tonumber(ARGV[3])
 if observed > 0 and observed > tonumber(redis.call('HGET', k, 'observed') or '0') and now-observed < 86400000 then
  block(observed)
  redis.call('HSET', k, 'observed', observed)
 end
 if redis.call('EXISTS', k) == 0 or redis.call('HGET', k, 'recovered') == '1' then return 1 end
 if tonumber(redis.call('HGET', k, 'until') or '0') > now then return 0 end
 if tonumber(redis.call('HGET', k, 'lease') or '0') > now then return 0 end
 redis.call('HSET', k, 'owner', owner, 'lease', now+60000, 'lease_epoch', epoch)
 redis.call('PEXPIRE', k, 108000000)
 return 2
end
if redis.call('HGET', k, 'owner') ~= owner or tonumber(redis.call('HGET', k, 'lease_epoch') or '-1') ~= epoch or tonumber(redis.call('HGET', k, 'lease') or '0') <= now then return 0 end
if op == 'refresh' then redis.call('HSET', k, 'lease', now+60000); return 1 end
if op == 'finish' then
 redis.call('HDEL', k, 'owner', 'lease', 'lease_epoch')
 if ARGV[4] == '1' then redis.call('HSET', k, 'recovered', 1)
 elseif tonumber(ARGV[3]) > 0 then block(now+tonumber(ARGV[3])) end
 return 1
end
return 0
`)

func (c *concurrencyCache) BlockRecoveryScope(ctx context.Context, scope string, cooldown time.Duration) error {
	return recoveryProbeScript.Run(ctx, c.rdb, []string{"concurrency:recovery:" + scope}, "block", max(0, cooldown.Milliseconds())).Err()
}
func (c *concurrencyCache) AcquireRecoveryProbe(ctx context.Context, scope string, observed int64, owner string) (bool, bool, error) {
	v, err := recoveryProbeScript.Run(ctx, c.rdb, []string{"concurrency:recovery:" + scope}, "acquire", owner, observed).Int()
	return v > 0, v == 2, err
}
func (c *concurrencyCache) RefreshRecoveryProbe(ctx context.Context, scope, owner string) (bool, error) {
	v, err := recoveryProbeScript.Run(ctx, c.rdb, []string{"concurrency:recovery:" + scope}, "refresh", owner).Int()
	return v == 1, err
}
func (c *concurrencyCache) FinishRecoveryProbe(ctx context.Context, scope, owner string, cooldown time.Duration, success bool) error {
	return recoveryProbeScript.Run(ctx, c.rdb, []string{"concurrency:recovery:" + scope}, "finish", owner, cooldown.Milliseconds(), success).Err()
}

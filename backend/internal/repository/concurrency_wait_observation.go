package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// A separate single-key observation index avoids adding cross-slot keys to the
// admission script. Sorted arrival times measure age; member deadlines reap
// canceled/crashed waiters without changing who may acquire a slot.
var waitObservationScript = redis.NewScript(`
redis.replicate_commands()
local k,op,token=KEYS[1],ARGV[1],ARGV[2]
local t=redis.call('TIME')
local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local entries=redis.call('ZRANGE',k,0,-1,'WITHSCORES')
local oldest=now
local ownStart=nil
local live={}
for i=1,#entries,2 do
 local member=entries[i]
 local divider=string.find(member,'|',1,true)
 local deadline=tonumber(string.sub(member,1,divider-1))
 local identity=string.sub(member,divider+1)
 if deadline<=now or (op=='end' and identity==token) then redis.call('ZREM',k,member)
 else
  local start=tonumber(entries[i+1])
  table.insert(live,start)
  oldest=math.min(oldest,start)
  if identity==token then ownStart=start end
 end
end
if op=='start' then
 if #live<4096 then
  local ttl=math.min(3600000,math.max(1,tonumber(ARGV[3])))
  redis.call('ZADD',k,now,tostring(now+ttl)..'|'..token)
  redis.call('PEXPIRE',k,3600000)
 end
 return {0,0}
end
local older=0
if op=='admit' then
 for _,start in ipairs(live) do if ownStart==nil or start<ownStart then older=older+1 end end
end
return {math.max(0,now-oldest),older}
`)

func waitObservationKey(scope string, id int64) string {
	return fmt.Sprintf("concurrency:wait_observation:%s:%d", scope, id)
}
func (c *concurrencyCache) StartWaitObservation(ctx context.Context, scope string, id int64, token string, timeout time.Duration) error {
	return waitObservationScript.Run(ctx, c.rdb, []string{waitObservationKey(scope, id)}, "start", token, timeout.Milliseconds()).Err()
}
func (c *concurrencyCache) EndWaitObservation(ctx context.Context, scope string, id int64, token string) error {
	return waitObservationScript.Run(ctx, c.rdb, []string{waitObservationKey(scope, id)}, "end", token).Err()
}
func (c *concurrencyCache) ObserveWaitAdmission(ctx context.Context, scope string, id int64, token string) (int64, int, error) {
	values, err := waitObservationScript.Run(ctx, c.rdb, []string{waitObservationKey(scope, id)}, "admit", token).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	return values[0], int(values[1]), nil
}

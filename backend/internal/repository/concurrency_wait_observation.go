package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// This single-key diagnostic index never participates in capacity admission.
// Scores are Redis-serialized arrival order, not millisecond timestamps (which
// tie under contention). Members contain deadline|first-arrival|token. Repeated
// registration may shorten a deadline but cannot move or extend a live waiter.
// The v2 namespace avoids interpreting a rolling peer's old timestamp scores as
// sequence numbers; old diagnostic keys expire under their existing one-hour TTL.
var waitObservationScript = redis.NewScript(`
redis.replicate_commands()
local k,op,token = KEYS[1],ARGV[1],ARGV[2]
local t=redis.call('TIME')
local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local entries=redis.call('ZRANGE',k,0,-1,'WITHSCORES')
local oldest,lastOrder,lastDeadline=now,0,now
local own=nil
local live={}
for i=1,#entries,2 do
 local member=entries[i]
 local deadline,start,identity=string.match(member,'^(%d+)|(%d+)|(.+)$')
 deadline,start=tonumber(deadline),tonumber(start)
 local order=tonumber(entries[i+1])
 lastOrder=math.max(lastOrder,order)
 if deadline==nil or start==nil or deadline<=now or (op=='end' and identity==token) then
  redis.call('ZREM',k,member)
 else
  local entry={member=member,deadline=deadline,start=start,order=order}
  table.insert(live,entry)
  oldest=math.min(oldest,start)
  lastDeadline=math.max(lastDeadline,deadline)
  if identity==token then own=entry end
 end
end
if op=='start' then
 local ttl=math.min(3600000,tonumber(ARGV[3]))
 if ttl<=0 then return {0,0} end
 local deadline,start,order=now+ttl,now,lastOrder+1
 if own~=nil then
  deadline,start,order=math.min(deadline,own.deadline),own.start,own.order
  redis.call('ZREM',k,own.member)
 elseif #live>=4096 then
  return {0,0}
 end
 redis.call('ZADD',k,order,tostring(deadline)..'|'..tostring(start)..'|'..token)
 redis.call('PEXPIRE',k,math.max(lastDeadline,deadline)-now)
 return {1,0}
end
local older=0
if op=='admit' then
 for _,entry in ipairs(live) do
  if own==nil or entry.order<own.order then older=older+1 end
 end
 -- Once capacity has been granted this member is no longer waiting. Retire
 -- it in the same script so concurrent admissions do not count it again.
 if own~=nil then redis.call('ZREM',k,own.member) end
end
return {math.max(0,now-oldest),older}
`)

func waitObservationKey(scope string, id int64) string {
	return fmt.Sprintf("concurrency:wait_observation:v2:%s:%d", scope, id)
}

func validateWaitObservation(scope string, id int64, token string, allowEmpty bool) error {
	if (scope != "account" && scope != "user") || id <= 0 || (!allowEmpty && token == "") || len(token) > 256 || strings.Contains(token, "|") {
		return errors.New("invalid concurrency wait observation identity")
	}
	return nil
}

func (c *concurrencyCache) StartWaitObservation(ctx context.Context, scope string, id int64, token string, timeout time.Duration) error {
	if err := validateWaitObservation(scope, id, token, false); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if timeout.Milliseconds() <= 0 {
		return context.DeadlineExceeded
	}
	values, err := waitObservationScript.Run(ctx, c.rdb, []string{waitObservationKey(scope, id)}, "start", token, timeout.Milliseconds()).Int64Slice()
	if err != nil {
		return err
	}
	if len(values) != 2 || values[0] != 1 {
		return errors.New("concurrency wait observation index is full")
	}
	return nil
}
func (c *concurrencyCache) EndWaitObservation(ctx context.Context, scope string, id int64, token string) error {
	if err := validateWaitObservation(scope, id, token, false); err != nil {
		return err
	}
	return waitObservationScript.Run(ctx, c.rdb, []string{waitObservationKey(scope, id)}, "end", token).Err()
}
func (c *concurrencyCache) ObserveWaitAdmission(ctx context.Context, scope string, id int64, token string) (int64, int, error) {
	if err := validateWaitObservation(scope, id, token, true); err != nil {
		return 0, 0, err
	}
	values, err := waitObservationScript.Run(ctx, c.rdb, []string{waitObservationKey(scope, id)}, "admit", token).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	if len(values) != 2 {
		return 0, 0, errors.New("invalid concurrency wait observation result")
	}
	return values[0], int(values[1]), nil
}

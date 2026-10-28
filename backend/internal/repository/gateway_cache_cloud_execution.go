package repository

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

var _ service.OpenAICloudExecutionStore = (*gatewayCache)(nil)

func cloudExecutionKeys(session, command string) []string {
	return []string{cloudKey("execution", command), cloudKey("owner", session), cloudKey("session", session), cloudKey("event_closed", command), cloudKey("meta", session)}
}
func cloudExecutionDigest(digest [32]byte) string { return hex.EncodeToString(digest[:]) }

var offerCloudExecutionScript = redis.NewScript(`
local owner=redis.call('GET',KEYS[2]); if not owner or redis.call('PTTL',KEYS[2])<=0 or redis.call('EXISTS',KEYS[3])==0 or redis.call('EXISTS',KEYS[4])==1 then return 0 end
if redis.call('EXISTS',KEYS[1])==1 or redis.call('EXISTS',KEYS[6])==1 then return -1 end
redis.call('HSET',KEYS[1],'state','offered','session',ARGV[1],'operation',ARGV[2],'transfer',ARGV[3],'owner',owner)
redis.call('SET',KEYS[6],ARGV[5],'PX',ARGV[4])
redis.call('PEXPIRE',KEYS[1],ARGV[4]); return 1`)

func (c *gatewayCache) OfferCloudExecution(ctx context.Context, session, command, operation string, digest [32]byte) error {
	if cloudStorageReady(c) != nil || session == "" || command == "" || operation == "" || digest == ([32]byte{}) {
		return execution.ErrDispatchUnavailable
	}
	keys := append(cloudExecutionKeys(session, command), cloudKey("execution_operation", operation))
	n, err := offerCloudExecutionScript.Run(ctx, c.rdb, keys, session, operation, cloudExecutionDigest(digest), service.OpenAICloudStateTTL.Milliseconds(), command).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrOpenAICloudCommandRejected
	}
	return nil
}

var checkCloudExecutionScript = redis.NewScript(`
if redis.call('HGET',KEYS[1],'session')~=ARGV[1] or redis.call('HGET',KEYS[1],'operation')~=ARGV[2] or redis.call('HGET',KEYS[1],'transfer')~=ARGV[3] or redis.call('HGET',KEYS[1],'owner')~=ARGV[4] then return 0 end
if redis.call('GET',KEYS[2])~=ARGV[4] or redis.call('EXISTS',KEYS[4])==1 then return 0 end
local ttl=redis.call('PTTL',KEYS[2]); if ttl<=0 then return 0 end
local state=redis.call('HGET',KEYS[1],'state')
if ARGV[5]=='claim' then
 if state~='offered' then return -1 end
 redis.call('HSET',KEYS[1],'state','active')
elseif state~='active' then return 0 end
return ttl`)

func (c *gatewayCache) cloudExecutionCheck(ctx context.Context, claim service.CloudExecutionClaim, action string) (time.Duration, error) {
	if cloudStorageReady(c) != nil || claim.SessionID == "" || claim.CommandID == "" || claim.OwnerToken == "" || claim.OperationID == "" || claim.TransferDigest == ([32]byte{}) {
		return 0, execution.ErrDispatchUnavailable
	}
	n, err := checkCloudExecutionScript.Run(ctx, c.rdb, cloudExecutionKeys(claim.SessionID, claim.CommandID), claim.SessionID, claim.OperationID, cloudExecutionDigest(claim.TransferDigest), claim.OwnerToken, action).Int64()
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, execution.ErrOperationConsumed
	}
	if n == 0 {
		return 0, execution.ErrDispatchChanged
	}
	return time.Duration(n) * time.Millisecond, nil
}
func (c *gatewayCache) ClaimCloudExecution(ctx context.Context, claim service.CloudExecutionClaim) (time.Duration, error) {
	return c.cloudExecutionCheck(ctx, claim, "claim")
}
func (c *gatewayCache) CheckCloudExecution(ctx context.Context, claim service.CloudExecutionClaim) (time.Duration, error) {
	return c.cloudExecutionCheck(ctx, claim, "check")
}

var saveCloudExecutionReturnScript = redis.NewScript(`
if redis.call('HGET',KEYS[1],'session')~=ARGV[1] or redis.call('HGET',KEYS[1],'operation')~=ARGV[2] or redis.call('HGET',KEYS[1],'transfer')~=ARGV[3] or redis.call('HGET',KEYS[1],'owner')~=ARGV[4] then return 0 end
local state=redis.call('HGET',KEYS[1],'state')
if state=='returned' then if redis.call('HGET',KEYS[1],'return_digest')==ARGV[5] then return 1 else return 0 end end
if state~='active' then return 0 end
-- A terminal Save may have retired the lease. The persisted owner must still
-- be this exact token; another owner, including recovery, cannot return for it.
if redis.call('HGET',KEYS[5],'owner_token')~=ARGV[4] then return 0 end
local live=redis.call('GET',KEYS[2]); if live and live~=ARGV[4] then return 0 end
redis.call('HSET',KEYS[1],'state','returned','return_digest',ARGV[5],'return',ARGV[6]);redis.call('PEXPIRE',KEYS[1],ARGV[7]);return 1`)

func (c *gatewayCache) SaveCloudExecutionReturn(ctx context.Context, claim service.CloudExecutionClaim, returned execution.OperationReturn) error {
	if cloudStorageReady(c) != nil || returned.CommandID != claim.CommandID || returned.TransferDigest != claim.TransferDigest || returned.Result.OperationID != claim.OperationID {
		return execution.ErrOperationFacts
	}
	raw, err := cloudPayload(returned, service.OpenAICloudMaxEnvelopeBytes)
	if err != nil {
		return err
	}
	n, err := saveCloudExecutionReturnScript.Run(ctx, c.rdb, cloudExecutionKeys(claim.SessionID, claim.CommandID), claim.SessionID, claim.OperationID, cloudExecutionDigest(claim.TransferDigest), claim.OwnerToken, cloudExecutionDigest(returned.Digest()), raw, service.OpenAICloudStateTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return execution.ErrDispatchChanged
	}
	return nil
}

func (c *gatewayCache) LoadCloudExecutionReturn(ctx context.Context, session, command string, digest [32]byte) (*execution.OperationReturn, error) {
	if cloudStorageReady(c) != nil {
		return nil, execution.ErrDispatchUnavailable
	}
	values, err := c.rdb.HMGet(ctx, cloudKey("execution", command), "session", "transfer", "return", "return_digest").Result()
	if err != nil {
		return nil, err
	}
	if len(values) != 4 || values[0] != session || values[1] != cloudExecutionDigest(digest) {
		return nil, execution.ErrDispatchChanged
	}
	if values[2] == nil {
		return nil, nil
	}
	raw, ok := values[2].(string)
	if !ok || len(raw) > service.OpenAICloudMaxEnvelopeBytes {
		return nil, execution.ErrOperationFacts
	}
	var returned execution.OperationReturn
	if json.Unmarshal([]byte(raw), &returned) != nil || returned.CommandID != command || returned.TransferDigest != digest || values[3] != cloudExecutionDigest(returned.Digest()) {
		return nil, execution.ErrOperationFacts
	}
	return &returned, nil
}

var claimCloudExecutionReturnScript = redis.NewScript(`
if redis.call('HGET',KEYS[1],'session')~=ARGV[1] or redis.call('HGET',KEYS[1],'transfer')~=ARGV[2] or redis.call('HGET',KEYS[1],'return_digest')~=ARGV[3] or redis.call('HGET',KEYS[1],'state')~='returned' or redis.call('EXISTS',KEYS[4])==1 then return 0 end
local owner=redis.call('HGET',KEYS[1],'owner');if redis.call('HGET',KEYS[5],'owner_token')~=owner then return 0 end
local live=redis.call('GET',KEYS[2]);if live and live~=owner then return 0 end
if redis.call('GET',KEYS[6])~=ARGV[4] then return 0 end
redis.call('HSET',KEYS[1],'state','reclaimed');redis.call('DEL',KEYS[6]);return 1`)

func (c *gatewayCache) ClaimCloudExecutionReturn(ctx context.Context, session, command string, transfer, returned [32]byte) error {
	if cloudStorageReady(c) != nil {
		return execution.ErrDispatchUnavailable
	}
	operation, err := c.rdb.HGet(ctx, cloudKey("execution", command), "operation").Result()
	if err != nil {
		return err
	}
	keys := append(cloudExecutionKeys(session, command), cloudKey("execution_operation", operation))
	n, err := claimCloudExecutionReturnScript.Run(ctx, c.rdb, keys, session, cloudExecutionDigest(transfer), cloudExecutionDigest(returned), command).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.Join(execution.ErrOperationConsumed, execution.ErrDispatchChanged)
	}
	return nil
}

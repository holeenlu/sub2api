package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func telemetryRunKeys(accountID int64) []string {
	// Hash tag keeps the atomic operation in one Redis Cluster slot.
	base := fmt.Sprintf("account_telemetry_run:{%d}", accountID)
	return []string{base + ":lease", base + ":state"}
}

func (c *gatewayCache) BeginAccountTelemetryRun(ctx context.Context, accountID int64, ttl time.Duration) (string, service.AccountTelemetryRunState, error) {
	state := service.AccountTelemetryRunState{}
	lease := uuid.NewString()
	raw, err := c.rdb.Eval(ctx, `
if not redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', ARGV[2]) then return '' end
return redis.call('GET', KEYS[2]) or '{}'
`, telemetryRunKeys(accountID), lease, ttl.Milliseconds()).Text()
	if err != nil || raw == "" {
		return "", state, err
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		// Fail closed on corrupted state; don't start another account clock.
		return "", state, err
	}
	return lease, state, nil
}

func (c *gatewayCache) EndAccountTelemetryRun(ctx context.Context, accountID int64, lease string, state service.AccountTelemetryRunState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	result, err := c.rdb.Eval(ctx, `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[2], ARGV[2], 'EX', 604800)
redis.call('DEL', KEYS[1])
return 1
`, telemetryRunKeys(accountID), lease, string(raw)).Int()
	if err == nil && result != 1 {
		return fmt.Errorf("client telemetry lease expired")
	}
	return err
}

var _ service.AccountTelemetryRuntimeCache = (*gatewayCache)(nil)

func (c *gatewayCache) TouchAccountTelemetry(ctx context.Context, id int64, activity service.AccountTelemetryActivity) error {
	raw, err := json.Marshal(activity)
	if err != nil {
		return err
	}
	pipe := c.rdb.TxPipeline()
	pipe.Set(ctx, fmt.Sprintf("account_telemetry_activity:%d", id), raw, 10*time.Minute)
	pipe.ZAdd(ctx, "account_telemetry_active", redis.Z{Score: float64(time.Now().Add(10 * time.Minute).Unix()), Member: strconv.FormatInt(id, 10)})
	_, err = pipe.Exec(ctx)
	return err
}
func (c *gatewayCache) ActiveAccountTelemetry(ctx context.Context) ([]int64, error) {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	if err := c.rdb.ZRemRangeByScore(ctx, "account_telemetry_active", "-inf", now).Err(); err != nil {
		return nil, err
	}
	values, err := c.rdb.ZRange(ctx, "account_telemetry_active", 0, -1).Result()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
func (c *gatewayCache) GetAccountTelemetryActivity(ctx context.Context, id int64) (service.AccountTelemetryActivity, error) {
	var out service.AccountTelemetryActivity
	raw, err := c.rdb.Get(ctx, fmt.Sprintf("account_telemetry_activity:%d", id)).Bytes()
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

func (c *gatewayCache) AcquireAccountTelemetryLeader(ctx context.Context, owner string) (bool, error) {
	n, err := c.rdb.Eval(ctx, `if redis.call('GET',KEYS[1]) == ARGV[1] then redis.call('PEXPIRE',KEYS[1],15000); return 1 end
 if redis.call('SET',KEYS[1],ARGV[1],'NX','PX',15000) then return 1 end; return 0`, []string{"account_telemetry_leader"}, owner).Int()
	return n == 1, err
}
func (c *gatewayCache) ReleaseAccountTelemetryLeader(ctx context.Context, owner string) error {
	return c.rdb.Eval(ctx, `if redis.call('GET',KEYS[1]) == ARGV[1] then redis.call('DEL',KEYS[1]) end; return 1`, []string{"account_telemetry_leader"}, owner).Err()
}

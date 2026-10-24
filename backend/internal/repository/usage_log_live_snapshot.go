package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// The native live-call cache has an explicit field map. Keep the small usage
// snapshot in that same expiring hash without changing credentials or identity.
func (c *gatewayCache) SaveLiveUsageSnapshot(ctx context.Context, r *service.LiveCallRecord, ttl time.Duration) error {
	if r == nil || r.CallHash == "" || r.VendorIDSnapshot == nil {
		return nil
	}
	data, err := json.Marshal(struct {
		AccountID                 int64
		VendorID                  *int64
		Name, Platform, GroupName *string
	}{r.AccountID, r.VendorIDSnapshot, r.AccountNameSnapshot, r.PlatformSnapshot, r.GroupNameSnapshot})
	if err != nil {
		return err
	}
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, liveCallKey(r.CallHash), "vendor_usage_snapshot", string(data))
	pipe.Expire(ctx, liveCallKey(r.CallHash), ttl)
	_, err = pipe.Exec(ctx)
	return err
}
func (c *gatewayCache) LoadLiveUsageSnapshot(ctx context.Context, r *service.LiveCallRecord) error {
	if r == nil || r.CallHash == "" {
		return fmt.Errorf("invalid live usage snapshot")
	}
	data, err := c.rdb.HGet(ctx, liveCallKey(r.CallHash), "vendor_usage_snapshot").Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot struct {
		AccountID                 int64
		VendorID                  *int64
		Name, Platform, GroupName *string
	}
	if err = json.Unmarshal([]byte(data), &snapshot); err != nil {
		return err
	}
	if snapshot.AccountID != r.AccountID {
		return fmt.Errorf("live usage snapshot mismatch")
	}
	r.VendorIDSnapshot = snapshot.VendorID
	r.AccountNameSnapshot = snapshot.Name
	r.PlatformSnapshot = snapshot.Platform
	r.GroupNameSnapshot = snapshot.GroupName
	return nil
}

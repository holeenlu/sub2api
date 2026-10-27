package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const opsNodeRegistryKey = "ops:metrics:nodes"
const opsNodeSnapshotPrefix = "ops:metrics:node:"
const opsNodeRetention = 24 * time.Hour

// Node resources never share the leader's global business-statistics bucket.
type OpsNodeMetricsSnapshot struct {
	OpsSystemMetricsSnapshot
	NodeID      string    `json:"node_id"`
	NodeName    string    `json:"node_name"`
	CPUScope    string    `json:"cpu_scope"`
	MemoryScope string    `json:"memory_scope"`
	CPUCapacity float64   `json:"cpu_capacity"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	Stale       bool      `json:"stale"`
	AgeSeconds  float64   `json:"age_seconds"`
}
type opsNodeMetricsReader interface {
	ListNodeMetrics(context.Context) ([]*OpsNodeMetricsSnapshot, error)
}

func opsNodeKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return opsNodeSnapshotPrefix + hex.EncodeToString(sum[:])
}
func (c *OpsMetricsCollector) nodeIdentity() (string, string) {
	id, name := "", ""
	if c.cfg != nil {
		id, name = strings.TrimSpace(c.cfg.Ops.NodeID), strings.TrimSpace(c.cfg.Ops.NodeName)
	}
	if id == "" {
		id, _ = os.Hostname()
	}
	if id == "" {
		id = c.instanceID
	}
	if name == "" {
		name = id
	}
	return id, name
}

// Redis server time makes liveness independent of host clock skew.
var publishOpsNodeScript = redis.NewScript(`
local stamp = redis.call('TIME')
local now = stamp[1] * 1000 + math.floor(stamp[2] / 1000)
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
redis.call('ZADD', KEYS[1], now, ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - tonumber(ARGV[3]))
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return now
`)

func (c *OpsMetricsCollector) publishNodeSnapshot(ctx context.Context, sys *opsCollectedSystemStats) {
	id, name := c.nodeIdentity()
	node := &OpsNodeMetricsSnapshot{
		OpsSystemMetricsSnapshot: OpsSystemMetricsSnapshot{
			CreatedAt: time.Now().UTC(), CPUUsagePercent: sys.cpuUsagePercent,
			MemoryUsedMB: sys.memoryUsedMB, MemoryTotalMB: sys.memoryTotalMB, MemoryUsagePercent: sys.memoryUsagePercent,
			GoroutineCount: intPtr(runtime.NumGoroutine()),
		},
		NodeID: id, NodeName: name, CPUScope: sys.cpuScope, MemoryScope: sys.memoryScope, CPUCapacity: sys.cpuCapacity,
	}
	if c.db != nil {
		node.DBOK = boolPtr(c.checkDB(ctx))
		stats := c.db.Stats()
		node.DBConnActive, node.DBConnIdle = intPtr(stats.InUse), intPtr(stats.Idle)
		if stats.MaxOpenConnections > 0 {
			node.DBMaxOpenConns = intPtr(stats.MaxOpenConnections)
		}
	}
	if c.redisClient != nil {
		if ctx.Err() == nil {
			node.RedisOK = boolPtr(c.checkRedis(ctx))
		}
		total, idle, ok := c.redisPoolStats()
		if ok {
			node.RedisConnTotal, node.RedisConnIdle = intPtr(total), intPtr(idle)
		}
		if size := c.redisClient.Options().PoolSize; size > 0 {
			node.RedisPoolSize = intPtr(size)
		}
	}
	node.LastSeenAt = node.CreatedAt
	c.nodeStats.Store(node)
	if c.redisClient == nil {
		return
	}
	data, err := json.Marshal(node)
	if err != nil {
		return
	}
	// Failures are visible in dashboard reads without flooding gateway logs.
	_, _ = publishOpsNodeScript.Run(ctx, c.redisClient, []string{opsNodeRegistryKey, opsNodeKey(id)}, id, data, opsNodeRetention.Milliseconds()).Result()
}
func (c *OpsMetricsCollector) localNodeMetrics() []*OpsNodeMetricsSnapshot {
	if c == nil {
		return nil
	}
	value := c.nodeStats.Load()
	if value == nil {
		return nil
	}
	copy := *value
	copy.AgeSeconds = math.Max(0, time.Since(copy.CreatedAt).Seconds())
	copy.Stale = time.Since(copy.CreatedAt) > gatewayResourceSampleMaxAge || time.Until(copy.CreatedAt) > gatewayResourceSampleMaxAge
	return []*OpsNodeMetricsSnapshot{&copy}
}
func (c *OpsMetricsCollector) ListNodeMetrics(ctx context.Context) ([]*OpsNodeMetricsSnapshot, error) {
	if c.redisClient == nil {
		return c.localNodeMetrics(), nil
	}
	pipe := c.redisClient.Pipeline()
	entries := pipe.ZRangeWithScores(ctx, opsNodeRegistryKey, 0, -1)
	// Read the clock after the registry so a concurrent publisher cannot appear in the future.
	clock := pipe.Time(ctx)
	if _, err := pipe.Exec(ctx); err != nil {
		return c.localNodeMetrics(), err
	}
	now := clock.Val()
	ids, keys, seen := []string{}, []string{}, []time.Time{}
	for _, item := range entries.Val() {
		id, ok := item.Member.(string)
		at := time.UnixMilli(int64(item.Score)).UTC()
		if !ok || now.Sub(at) > opsNodeRetention {
			continue
		}
		ids, keys, seen = append(ids, id), append(keys, opsNodeKey(id)), append(seen, at)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	values, err := c.redisClient.MGet(ctx, keys...).Result()
	if err != nil {
		return c.localNodeMetrics(), err
	}
	nodes := make([]*OpsNodeMetricsSnapshot, 0, len(values))
	invalid := false
	for i, value := range values {
		raw, ok := value.(string)
		var node OpsNodeMetricsSnapshot
		if !ok || json.Unmarshal([]byte(raw), &node) != nil || node.NodeID != ids[i] {
			node = OpsNodeMetricsSnapshot{NodeID: ids[i], NodeName: ids[i], Stale: true}
			invalid = true
		}
		node.LastSeenAt = seen[i]
		node.AgeSeconds = math.Max(0, now.Sub(seen[i]).Seconds())
		node.Stale = node.Stale || now.Sub(seen[i]) > gatewayResourceSampleMaxAge || seen[i].After(now)
		nodes = append(nodes, &node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	if invalid {
		return nodes, errors.New("node metrics contain unavailable snapshots")
	}
	return nodes, nil
}

// Health scoring uses worst-node pressure; pool counts/capacities are summed.
// Do not combine max(memory percent) with summed memory used/total values.
func summarizeOpsNodes(nodes []*OpsNodeMetricsSnapshot) *OpsSystemMetricsSnapshot {
	fresh := []*OpsNodeMetricsSnapshot{}
	for _, node := range nodes {
		if node != nil && !node.Stale {
			fresh = append(fresh, node)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	out := &OpsSystemMetricsSnapshot{CreatedAt: fresh[0].LastSeenAt, WindowMinutes: 1}
	maxPercent := func(selectValue func(*OpsNodeMetricsSnapshot) *float64) *float64 {
		var result *float64
		for _, node := range fresh {
			if v := selectValue(node); v != nil && (result == nil || *v > *result) {
				result = float64Ptr(*v)
			}
		}
		return result
	}
	allOK := func(selectValue func(*OpsNodeMetricsSnapshot) *bool) *bool {
		unknown := false
		for _, node := range fresh {
			v := selectValue(node)
			if v == nil {
				unknown = true
			} else if !*v {
				return boolPtr(false)
			}
		}
		if unknown {
			return nil
		}
		return boolPtr(true)
	}
	sum := func(selectValue func(*OpsNodeMetricsSnapshot) *int) *int {
		total := 0
		for _, node := range fresh {
			v := selectValue(node)
			if v == nil {
				return nil
			}
			total += *v
		}
		return intPtr(total)
	}
	for _, node := range fresh {
		if node.LastSeenAt.Before(out.CreatedAt) {
			out.CreatedAt = node.LastSeenAt
		}
	}
	out.CPUUsagePercent = maxPercent(func(n *OpsNodeMetricsSnapshot) *float64 { return n.CPUUsagePercent })
	out.MemoryUsagePercent = maxPercent(func(n *OpsNodeMetricsSnapshot) *float64 { return n.MemoryUsagePercent })
	out.DBOK = allOK(func(n *OpsNodeMetricsSnapshot) *bool { return n.DBOK })
	out.RedisOK = allOK(func(n *OpsNodeMetricsSnapshot) *bool { return n.RedisOK })
	out.DBConnActive = sum(func(n *OpsNodeMetricsSnapshot) *int { return n.DBConnActive })
	out.DBConnIdle = sum(func(n *OpsNodeMetricsSnapshot) *int { return n.DBConnIdle })
	out.DBMaxOpenConns = sum(func(n *OpsNodeMetricsSnapshot) *int { return n.DBMaxOpenConns })
	out.RedisConnTotal = sum(func(n *OpsNodeMetricsSnapshot) *int { return n.RedisConnTotal })
	out.RedisConnIdle = sum(func(n *OpsNodeMetricsSnapshot) *int { return n.RedisConnIdle })
	out.RedisPoolSize = sum(func(n *OpsNodeMetricsSnapshot) *int { return n.RedisPoolSize })
	out.GoroutineCount = sum(func(n *OpsNodeMetricsSnapshot) *int { return n.GoroutineCount })
	return out
}

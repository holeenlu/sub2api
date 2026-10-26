package service

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const gatewayResourceSampleInterval = 5 * time.Second
const gatewayResourceSampleMaxAge = 30 * time.Second
const gatewayPressureTrigger = 90.0
const gatewayPressureRecover = 85.0

// The existing Ops collector is the sole source. Admission never samples the
// system or reads a remote instance's leader-only metrics.
var gatewayPressure instancePressure

var ErrGatewayInstanceOverloaded = errors.New("gateway instance is overloaded")

type instancePressure struct {
	mu                 sync.Mutex
	at                 time.Time
	high, low          int
	overloaded         bool
	cpu, memory        float64
	reportedAt         time.Time
	reportedOverloaded bool
	reportedRejects    [2]uint64
}

func (p *instancePressure) observe(at time.Time, cpu, memory *float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	valid := func(v *float64) bool { return v != nil && *v >= 0 && !math.IsNaN(*v) && !math.IsInf(*v, 0) }
	if !valid(cpu) || !valid(memory) {
		p.at = time.Time{}
		p.high = 0
		p.low = 0
		p.overloaded = false
		return
	}
	if !p.at.IsZero() && at.Sub(p.at) > gatewayResourceSampleMaxAge {
		p.high = 0
		p.low = 0
		p.overloaded = false
	}
	p.at = at
	p.cpu, p.memory = *cpu, *memory
	if *cpu >= gatewayPressureTrigger || *memory >= gatewayPressureTrigger {
		p.high++
		p.low = 0
		if p.high >= 3 {
			p.overloaded = true
		}
	} else if *cpu < gatewayPressureRecover && *memory < gatewayPressureRecover {
		p.high = 0
		p.low++
		if p.low >= 2 {
			p.overloaded = false
		}
	} else {
		p.high = 0
		p.low = 0
	}
}
func (p *instancePressure) rejecting(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.overloaded && !p.at.IsZero() && !now.Before(p.at) && now.Sub(p.at) <= gatewayResourceSampleMaxAge
}

// Report transitions immediately and rejection deltas at most every 30s.
// This uses the existing system-log pipeline even if metrics INSERT fails;
// ingress itself remains just an atomic counter, never a database/log flood.
type gatewayPressureObservation struct {
	overloaded  bool
	reason      string
	cpu, memory float64
	rejected    [2]uint64
}

func (p *instancePressure) observation(now time.Time, rejected [2]uint64) *gatewayPressureObservation {
	p.mu.Lock()
	defer p.mu.Unlock()
	fresh := !p.at.IsZero() && !now.Before(p.at) && now.Sub(p.at) <= gatewayResourceSampleMaxAge
	overloaded := fresh && p.overloaded
	changed := overloaded != p.reportedOverloaded
	delta := [2]uint64{rejected[0] - p.reportedRejects[0], rejected[1] - p.reportedRejects[1]}
	if !changed && (delta == [2]uint64{} || now.Sub(p.reportedAt) < 30*time.Second) {
		return nil
	}
	reason := "recovered"
	if !fresh {
		reason = "sample_unavailable"
	} else if overloaded {
		reason = "hysteresis"
		if p.cpu >= gatewayPressureTrigger && p.memory >= gatewayPressureTrigger {
			reason = "cpu_memory"
		} else if p.cpu >= gatewayPressureTrigger {
			reason = "cpu"
		} else if p.memory >= gatewayPressureTrigger {
			reason = "memory"
		}
	}
	p.reportedAt, p.reportedOverloaded, p.reportedRejects = now, overloaded, rejected
	return &gatewayPressureObservation{overloaded, reason, p.cpu, p.memory, delta}
}

func GatewayInstanceOverloaded() bool { return gatewayPressure.rejecting(time.Now()) }

func (c *OpsMetricsCollector) sampleResources() {
	node, _ := c.nodeIdentity()
	sample := func() {
		defer func() {
			counters := [2]uint64{schedulingCounters.overload[0].Load(), schedulingCounters.overload[1].Load()}
			if event := gatewayPressure.observation(time.Now(), counters); event != nil {
				logger.LegacyPrintfLevel(logger.LevelWarn, "service.gateway_pressure", "gateway_instance_pressure node=%s overloaded=%t reason=%s cpu_percent=%.2f memory_percent=%.2f rejected_http=%d rejected_ws=%d", node, event.overloaded, event.reason, event.cpu, event.memory, event.rejected[0], event.rejected[1])
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		stats, err := c.collectSystemStats(ctx)
		cancel()
		if err != nil || stats == nil {
			gatewayPressure.observe(time.Now(), nil, nil)
			return
		}
		gatewayPressure.observe(time.Now(), stats.cpuUsagePercent, stats.memoryUsagePercent)
		if c.nodesEnabled.Load() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			c.publishNodeSnapshot(ctx, stats)
			cancel()
		}
	}
	sample()
	ticker := time.NewTicker(gatewayResourceSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			sample()
		}
	}
}

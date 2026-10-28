package service

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
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
	mu         sync.Mutex
	at         time.Time
	high, low  int
	overloaded bool
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
func GatewayInstanceOverloaded() bool { return gatewayPressure.rejecting(time.Now()) }

func (c *OpsMetricsCollector) sampleResources() {
	parent := c.tasks.context()
	sample := func() {
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		stats, err := c.collectSystemStats(ctx)
		cancel()
		if err != nil || stats == nil {
			gatewayPressure.observe(time.Now(), nil, nil)
			return
		}
		gatewayPressure.observe(time.Now(), stats.cpuUsagePercent, stats.memoryUsagePercent)
		if c.nodesEnabled.Load() {
			publishCtx, publishCancel := context.WithTimeout(parent, 2*time.Second)
			c.publishNodeSnapshot(publishCtx, stats)
			publishCancel()
		}
	}
	sample()
	ticker := time.NewTicker(gatewayResourceSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-parent.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			sample()
		}
	}
}

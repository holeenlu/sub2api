package service

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGatewayPressureObservationTransitionsAndBoundedCounts(t *testing.T) {
	p := &instancePressure{}
	now := time.Now()
	hi, lo := 95.0, 20.0
	for i := 0; i < 3; i++ {
		p.observe(now, &hi, &lo)
	}
	e := p.observation(now, [2]uint64{2, 1})
	require.NotNil(t, e)
	require.True(t, e.overloaded)
	require.Equal(t, "cpu", e.reason)
	require.Equal(t, [2]uint64{2, 1}, e.rejected)
	require.Nil(t, p.observation(now.Add(time.Second), [2]uint64{20, 2}))
	p.observe(now.Add(30*time.Second), &hi, &lo)
	e = p.observation(now.Add(30*time.Second), [2]uint64{20, 2})
	require.Equal(t, [2]uint64{18, 1}, e.rejected)
	p.observe(now.Add(31*time.Second), &lo, &lo)
	p.observe(now.Add(32*time.Second), &lo, &lo)
	e = p.observation(now.Add(32*time.Second), [2]uint64{20, 2})
	require.False(t, e.overloaded)
	require.Equal(t, "recovered", e.reason)
	for i := 0; i < 3; i++ {
		p.observe(now.Add(33*time.Second), &lo, &hi)
	}
	e = p.observation(now.Add(33*time.Second), [2]uint64{20, 2})
	require.Equal(t, "memory", e.reason)
	e = p.observation(now.Add(64*time.Second), [2]uint64{20, 2})
	require.False(t, e.overloaded)
	require.Equal(t, "sample_unavailable", e.reason)
}

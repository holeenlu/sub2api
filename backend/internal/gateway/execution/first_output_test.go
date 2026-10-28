package execution

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFirstOutputPolicyCannotResetAfterDispatch(t *testing.T) {
	start := time.Now()
	f := NewFirstOutput(start)
	f.Configure(time.Minute, false, false)
	f.Configure(3*time.Minute, true, false)
	require.Equal(t, start.Add(3*time.Minute), f.Snapshot().Deadline)
	f.Configure(5*time.Minute, true, true)
	require.Equal(t, start.Add(3*time.Minute), f.Snapshot().Deadline)
	accepted := start.Add(-time.Second)
	require.Equal(t, accepted.Add(time.Minute), f.Limit(time.Minute, accepted))
	f.Configure(5*time.Minute, true, false)
	require.Equal(t, accepted.Add(time.Minute), f.Snapshot().Deadline)
}

func TestFirstOutputDispatchFreezesAutomaticPolicy(t *testing.T) {
	start := time.Now()
	f := NewFirstOutput(start)
	f.Configure(time.Minute, false, false)
	f.Configure(3*time.Minute, true, true)
	require.Equal(t, start.Add(time.Minute), f.Snapshot().Deadline)
}

func TestFirstOutputPauseIsBoundedAndCannotRearmCommittedOutput(t *testing.T) {
	f := NewFirstOutput(time.Now())
	f.Configure(time.Minute, true, false)
	before := f.Snapshot().Deadline
	resume := f.Pause(time.Nanosecond)
	require.Equal(t, before.Add(time.Nanosecond), f.Snapshot().Deadline)
	resume()
	resume()
	require.LessOrEqual(t, f.Snapshot().Paused, time.Nanosecond)
	require.Equal(t, before.Add(f.Snapshot().Paused), f.Snapshot().Deadline)
	resume = f.Pause(time.Second)
	f.Commit()
	resume()
	f.Configure(time.Minute, false, false)
	f.Limit(time.Second, time.Now())
	f.Pause(time.Minute)()
	require.True(t, f.Snapshot().Committed)
	require.True(t, f.Snapshot().Deadline.IsZero())
}

func TestFirstOutputCommitWakesAllWatchersOnce(t *testing.T) {
	f := NewFirstOutput(time.Now())
	f.Configure(time.Minute, true, false)
	_, before := f.Watch()
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			f.Commit()
			f.Configure(time.Second, true, false)
			f.Limit(time.Second, time.Now())
			_, ready := f.Watch()
			<-ready
		})
	}
	group.Wait()
	select {
	case <-before:
	default:
		t.Fatal("commit left an existing watcher blocked")
	}
	state, after := f.Watch()
	require.True(t, state.Committed)
	require.True(t, state.Deadline.IsZero())
	require.Equal(t, before, after)
}

func TestFirstOutputCommitBeforeWatchAndAbsentDeadline(t *testing.T) {
	var f FirstOutput
	f.Pause(time.Second)()
	require.True(t, f.Snapshot().Deadline.IsZero())
	f.Commit()
	state, ready := f.Watch()
	require.True(t, state.Committed)
	select {
	case <-ready:
	default:
		t.Fatal("late watcher missed committed output")
	}
}

package execution

import (
	"sync"
	"time"
)

// FirstOutput owns the operation's first-output deadline and irreversible
// delivery fence. It is independent of request contexts and transport timers.
// Construct it with the operation's acceptance time; do not copy it after use.
type FirstOutput struct {
	mu        sync.Mutex
	started   time.Time
	deadline  time.Time
	automatic time.Time
	paused    time.Duration
	committed bool
	ready     chan struct{}
}

// OutputSnapshot is an observation, not permission to retry or send.
type OutputSnapshot struct {
	Started   time.Time
	Deadline  time.Time
	Paused    time.Duration
	Committed bool
}

func NewFirstOutput(started time.Time) *FirstOutput {
	return &FirstOutput{started: started}
}

// Configure chooses the initial policy deadline. An explicit policy may replace
// an untouched automatic deadline before any attempt; retries never reset it.
// The caller serializes this operation with attempt admission.
func (f *FirstOutput) Configure(timeout time.Duration, explicit, attempted bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.committed || (!f.deadline.IsZero() &&
		(!explicit || attempted || f.automatic.IsZero() || !f.deadline.Equal(f.automatic))) {
		return
	}
	f.deadline = f.started.Add(timeout + f.paused)
	if explicit {
		f.automatic = time.Time{}
	} else {
		f.automatic = f.deadline
	}
}

// Limit applies a transport's tighter bound using the earlier acceptance time.
// Previously excluded preparation time survives account and transport changes.
func (f *FirstOutput) Limit(timeout time.Duration, accepted time.Time) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	started := f.started
	if !accepted.IsZero() && (started.IsZero() || accepted.Before(started)) {
		started = accepted
	}
	limit := started.Add(timeout + f.paused)
	if !f.committed && timeout > 0 && (f.deadline.IsZero() || limit.Before(f.deadline)) {
		f.deadline = limit
	}
	return f.deadline
}

// Pause temporarily extends the bound for an independently bounded preparation
// phase. Resume keeps only elapsed time up to maxPause and is safe to repeat.
func (f *FirstOutput) Pause(maxPause time.Duration) func() {
	f.mu.Lock()
	if maxPause <= 0 || f.committed || f.deadline.IsZero() {
		f.mu.Unlock()
		return func() {}
	}
	pausedAt := time.Now()
	f.deadline = f.deadline.Add(maxPause)
	f.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			elapsed := min(time.Since(pausedAt), maxPause)
			f.mu.Lock()
			defer f.mu.Unlock()
			f.paused += elapsed
			if !f.committed && !f.deadline.IsZero() {
				f.deadline = f.deadline.Add(elapsed - maxPause)
			}
		})
	}
}

// Commit records delivery reported by the ingress and releases first-output
// watchers. It never supplies evidence of upstream completion or final usage.
func (f *FirstOutput) Commit() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.committed {
		return
	}
	f.committed = true
	f.deadline = time.Time{}
	if f.ready != nil {
		close(f.ready)
	}
}

// Watch atomically observes the fence and registers for committed delivery.
func (f *FirstOutput) Watch() (OutputSnapshot, <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ready == nil {
		f.ready = make(chan struct{})
		if f.committed {
			close(f.ready)
		}
	}
	return f.snapshot(), f.ready
}

func (f *FirstOutput) Snapshot() OutputSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot()
}

func (f *FirstOutput) snapshot() OutputSnapshot {
	return OutputSnapshot{Started: f.started, Deadline: f.deadline, Paused: f.paused, Committed: f.committed}
}

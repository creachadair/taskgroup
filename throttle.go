package taskgroup

import "sync/atomic"

// A Throttle rate-limits the number of concurrent goroutines that can execute
// in parallel to some fixed number.  A zero Throttle is ready for use, but
// imposes no limit on parallel execution. See [Throttle.Enter] for use.
type Throttle struct {
	adm chan struct{}
}

// NewThrottle constructs a [Throttle] with a capacity of n goroutines.
// If n ≤ 0, the resulting Throttle imposes no limit.
func NewThrottle(n int) Throttle {
	if n <= 0 {
		return Throttle{}
	}
	return Throttle{adm: make(chan struct{}, n)}
}

// Enter blocks until a slot is available in t, then returns a [Leaver] that
// the caller must execute to return the slot when it is no longer in use.
func (t Throttle) Enter() Leaver {
	if t.adm == nil {
		return func() {}
	}
	t.adm <- struct{}{}
	var done atomic.Bool
	return func() {
		if done.CompareAndSwap(false, true) {
			<-t.adm
		}
	}
}

// A Leaver returns an in-use throttle slot to its underlying [Throttle].
// It is safe to call a Leaver multiple times; the slot will only be returned
// once.
type Leaver func()

// Leave returns the slot to its [Throttle]. This is a legibility alias for
// calling f.
func (f Leaver) Leave() { f() }

// Limit returns a function that starts each [Task] passed to it in g,
// respecting the rate limit imposed by t. Each call to Limit yields a fresh
// start function, and all the functions returned share the capacity of t.
func (t Throttle) Limit(g *Group) func(Task) {
	return func(task Task) {
		slot := t.Enter()
		g.Go(func() error {
			defer slot.Leave()
			return task()
		})
	}
}

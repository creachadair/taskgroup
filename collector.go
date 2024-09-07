package taskgroup

import "sync"

// A Collector collects values reported by task functions and delivers them to
// an accumulator function.
type Collector[T any] struct {
	μ      sync.Mutex
	handle func(T)
}

// report delivers v to the callback under the lock.
func (c *Collector[T]) report(v T) {
	c.μ.Lock()
	defer c.μ.Unlock()
	c.handle(v)
}

// NewCollector is an alias for [Collect].
//
// Deprecated: Use Collect instead.
func NewCollector[T any](value func(T)) *Collector[T] { return Collect(value) }

// Collect creates a new collector that delivers task values to the specified
// accumulator function. The collector serializes calls to value, so that it is
// safe for the function to access shared state without a lock.
//
// The tasks created from a collector do not return until all the values
// reported by the underlying function have been processed by the accumulator.
func Collect[T any](value func(T)) *Collector[T] { return &Collector[T]{handle: value} }

// Task is an alias for Call.
//
// Deprecated: Use Call instead.
func (c *Collector[T]) Task(f func() (T, error)) Task { return c.Call(f) }

// Call returns a Task wrapping a call to f. If f reports an error, that error
// is propagated as the return value of the task; otherwise, the non-error
// value reported by f is passed to the value callback.
func (c *Collector[T]) Call(f func() (T, error)) Task {
	return func() error {
		v, err := f()
		if err != nil {
			return err
		}
		c.report(v)
		return nil
	}
}

// Report returns a task wrapping a call to f, which is passed a function that
// sends results to the accumulator. The report function does not return until
// the accumulator has finished processing the value.
func (c *Collector[T]) Report(f func(report func(T)) error) Task {
	return func() error { return f(c.report) }
}

// NoError is an alias for Run.
//
// Deprecated: Use Run instead.
func (c *Collector[T]) NoError(f func() T) Task { return c.Run(f) }

// Run returns a Task wrapping a call to f. The resulting task reports a nil
// error for all calls.
func (c *Collector[T]) Run(f func() T) Task {
	return NoError(func() { c.report(f()) })
}

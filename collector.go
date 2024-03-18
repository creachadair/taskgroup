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

// NewCollector creates a new collector that delivers task values to the
// specified accumulator function. The collector serializes calls to value, so
// that it is safe for the function to access shared state without a lock.
//
// The tasks created from a collector do not return until all the values
// reported by the underlying function have been processed by the accumulator.
func NewCollector[T any](value func(T)) *Collector[T] { return &Collector[T]{handle: value} }

// Wait waits until the collector has finished processing.
//
// Deprecated: This method is now a noop; it is safe but unnecessary to call
// it.  The state serviced by c is settled once all the goroutines writing to
// the collector have returned.  It may be removed in a future version.
func (c *Collector[T]) Wait() {}

// Task returns a Task wrapping a call to f. If f reports an error, that error
// is propagated as the return value of the task; otherwise, the non-error
// value reported by f is passed to the value callback.
func (c *Collector[T]) Task(f func() (T, error)) Task {
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

// Stream returns a task wrapping a call to f, which is passed a channel on
// which results can be sent to the accumulator. Each call to Stream starts a
// goroutine to process the values on the channel.
//
// Deprecated: Tasks that wish to deliver multiple values should use Report
// instead, which does not spawn a goroutine. This method may be removed in a
// future version.
func (c *Collector[T]) Stream(f func(chan<- T) error) Task {
	return func() error {
		ch := make(chan T)
		s := Go(NoError(func() {
			for v := range ch {
				c.report(v)
			}
		}))
		defer func() { close(ch); s.Wait() }()
		return f(ch)
	}
}

// NoError returns a Task wrapping a call to f. The resulting task reports a
// nil error for all calls.
func (c *Collector[T]) NoError(f func() T) Task {
	return NoError(func() { c.report(f()) })
}

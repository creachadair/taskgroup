package taskgroup

// A single manages a single background goroutine. The task is started when the
// value is first created, and the caller can use the Wait method to block
// until it has exited.
type Single[T any] struct {
	valc chan T
	val  T
}

// Wait blocks until the task monitored by s has completed and returns the
// value it reported.
func (s *Single[T]) Wait() T {
	if v, ok := <-s.valc; ok {
		// This is the first call to receive a value; update err and close the
		// channel.
		s.val = v
		close(s.valc)
	}
	return s.val
}

// Go runs task in a new goroutine. The caller must call Wait to wait for the
// task to return and collect its error.
func Go(task Task) *Single[error] {
	// N.B. This is closed by Wait.
	errc := make(chan error, 1)
	go func() { errc <- task() }()

	return &Single[error]{valc: errc}
}

// Call starts task in a new goroutine. The caller must call Wait to wait for
// the task to return and collect its result.
func Call[T any](task func() (T, error)) *Single[Result[T]] {
	// N.B. This is closed by Wait.
	valc := make(chan Result[T], 1)
	go func() {
		v, err := task()
		valc <- Result[T]{Value: v, Err: err}
	}()
	return &Single[Result[T]]{valc: valc}
}

// A Result is a pair of an arbitrary value and an error.
type Result[T any] struct {
	Value T
	Err   error
}

// Get returns the fields of r as results. It is a convenience method for
// unpacking the results of a Call.
//
// Typical usage:
//
//	s := taskgroup.Call(func() (int, error) { ... })
//	v, err := s.Wait().Get()
func (r Result[T]) Get() (T, error) { return r.Value, r.Err }

// Package taskgroup manages a collection of cancellable goroutines.  It
// simplifies common concerns of waiting for goroutine termination and
// collecting errors.
package taskgroup

import "sync"

// A Task is a function that performs some arbitrary task and returns an error
// value, and is the basic unit of work in a Group.
type Task func() error

// A Group manages a collection of cooperating goroutines.  New tasks can be
// added to the group via the Go and StartN methods.  The caller can wait for
// the tasks to complete by calling the Wait method.
//
// The group collects any errors returned or reported by each task.  Errors can
// also optionally be reported to a user-defined callback provided via the
// OnError or FilterError options.  The first non-nil error reported by any
// task (and not otherwise filtered) is returned from the Wait method.
type Group struct {
	onError func(error) error // called each time a task returns non-nil
	wg      sync.WaitGroup    // counter for active goroutines

	gather *sync.Once    // setup for error collector
	errc   chan<- error  // errors generated by goroutines
	report func(error)   // receives error reports
	edone  chan struct{} // signals error completion
	err    error         // error returned from Wait
}

// New constructs a new, empty group.  Add tasks to the group using g.Go.  Wait
// for the group to complete with g.Wait.
func New(opts ...Option) *Group {
	g := &Group{gather: new(sync.Once)}
	for _, opt := range opts {
		opt(g)
	}
	if g.onError == nil {
		g.onError = func(e error) error { return e }
	}
	return g
}

// Go adds a new task to the group.  Returns g to permit chaining.
func (g *Group) Go(task Task) *Group {
	g.wg.Add(1)
	g.init()
	go func() {
		defer g.wg.Done()
		g.report(task())
	}()
	return g
}

func (g *Group) init() {
	// The first time a task is added to an otherwise clear group, set up the
	// error collector goroutine.  We don't do this in the constructor so that
	// an unused group can be abandoned without orphaning a goroutine.
	g.gather.Do(func() {
		errc := make(chan error)
		g.err = nil
		g.errc = errc
		g.report = func(err error) {
			if err != nil {
				errc <- err
			}
		}
		g.edone = make(chan struct{})
		go func() {
			defer close(g.edone)
			for err := range errc {
				if e := g.onError(err); e != nil {
					if g.err == nil {
						g.err = e // capture the first error always
					}
				}
			}
		}()
	})
}

// StartN starts n separate goroutines running task in g.  Each instance of
// task is called with a distinct ID 0 ≤ i < n.  Tasks report errors by calling
// the report function.  StartN returns g to permit chaining.
func (g *Group) StartN(n int, task func(i int, report func(error))) *Group {
	g.wg.Add(n)
	g.init()
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer g.wg.Done()
			task(i, g.report)
		}()
	}
	return g
}

// Wait blocks until all the goroutines currently active in the group have
// returned, and all reported errors have been delivered to the callback.
// Wait returns the first non-nil error returned by any of the goroutines
// in the group.
func (g *Group) Wait() error {
	g.wg.Wait()
	if g.errc != nil {
		close(g.errc)
		<-g.edone
		g.errc = nil
		g.report = nil
		g.gather = new(sync.Once)
	}
	return g.err
}

// An Option specifies a configurable option for a Group.
type Option func(*Group)

// FilterError returns an Option that sets a function to be invoked each time a
// task in the group reports a non-nil error.  The error value from the task is
// passed to f.  The return value from f replaces the error; if nil, the error
// is discarded.
//
// All calls to f are made from a single goroutine, so it is safe for f to
// access a resource under its control without further locking.
func FilterError(f func(error) error) Option { return func(g *Group) { g.onError = f } }

// OnError returns an Option that sets a function to be invoked each time a
// task in the group returns a non-nil error.  The error value from the task is
// passed to f.
//
// All calls to f are made from a single goroutine, so it is safe for f to
// access a resource under its control without further locking.
func OnError(f func(error)) Option { return FilterError(func(e error) error { f(e); return e }) }

// ErrTrigger returns an option that sets a function to be invoked each time a
// task in the group returns a non-nil error.
func ErrTrigger(f func()) Option { return FilterError(func(e error) error { f(); return e }) }

// Capacity returns a function that starts each task passed to it in g,
// allowing no more than n tasks to be active concurrently.  If n ≤ 0, the
// function is equivalent to g.Go, and enforces no limit.
func Capacity(g *Group, n int) func(Task) *Group {
	if n <= 0 {
		return g.Go
	}
	adm := make(chan struct{}, n)
	return func(task Task) *Group {
		g.Go(func() error {
			adm <- struct{}{}
			err := task()
			<-adm
			return err
		})
		return g
	}
}
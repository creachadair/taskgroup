// Package taskgroup manages collections of cooperating goroutines.
// It defines a Group that handles waiting for goroutine termination and the
// propagation of error values. The caller may provide a callback to filter
// and respond to task errors.
package taskgroup

import (
	"sync"
	"sync/atomic"
)

// A Task function is the basic unit of work in a Group. Errors reported by
// tasks are collected and reported by the group.
type Task func() error

// A Group manages a collection of cooperating goroutines.  Add new tasks to
// the group with the Go method.  Call the Wait method to wait for the tasks to
// complete. A zero value is ready for use, but must not be copied after its
// first use.
//
// The group collects any errors returned by the tasks in the group. The first
// non-nil error reported by any task (and not otherwise filtered) is returned
// from the Wait method.
type Group struct {
	wg      sync.WaitGroup // counter for active goroutines
	onError ErrorFunc      // called each time a task returns non-nil

	// active is nonzero when the group is "active", meaning there has been at
	// least one call to Go since the group was created or the last Wait.
	//
	// Together active and μ work as a kind of resettable sync.Once; the fast
	// path reads active and only acquires μ if it discovers setup is needed.
	active atomic.Uint32

	μ   sync.Mutex // guards err
	err error      // error returned from Wait
}

// activate resets the state of the group and marks it as active.  This is
// triggered by adding a goroutine to an empty group.
func (g *Group) activate() {
	g.μ.Lock()
	defer g.μ.Unlock()
	if g.active.Load() == 0 { // still inactive
		g.err = nil
		g.active.Store(1)
	}
}

// New constructs a new empty group.  If ef != nil, it is called for each error
// reported by a task running in the group.  The value returned by ef replaces
// the task's error. If ef == nil, errors are not filtered.
//
// Calls to ef are issued by a single goroutine, so it is safe for ef to
// manipulate local data structures without additional locking.
func New(ef ErrorFunc) *Group { return &Group{onError: ef} }

// Go runs task in a new goroutine in g, and returns g to permit chaining.
func (g *Group) Go(task Task) *Group {
	g.wg.Add(1)
	if g.active.Load() == 0 {
		g.activate()
	}
	go func() {
		defer g.wg.Done()
		if err := task(); err != nil {
			g.handleError(err)
		}
	}()
	return g
}

func (g *Group) handleError(err error) {
	g.μ.Lock()
	defer g.μ.Unlock()
	e := g.onError.filter(err)
	if e != nil && g.err == nil {
		g.err = e // capture the first unfiltered error always
	}
}

// Wait blocks until all the goroutines currently active in the group have
// returned, and all reported errors have been delivered to the callback.  It
// returns the first non-nil error reported by any of the goroutines in the
// group and not filtered by an ErrorFunc.
//
// As with sync.WaitGroup, new tasks can be added to g during a call to Wait
// only if the group contains at least one active task when Wait is called and
// continuously thereafter until the last concurrent call to g.Go returns.
//
// Wait may be called from at most one goroutine at a time.  After Wait has
// returned, the group is ready for reuse.
func (g *Group) Wait() error {
	g.wg.Wait()
	g.μ.Lock()
	defer g.μ.Unlock()

	// If the group is still active, deactivate it now.
	if g.active.Load() != 0 {
		g.active.Store(0)
	}
	return g.err
}

// An ErrorFunc is called by a group each time a task reports an error.  Its
// return value replaces the reported error, so the ErrorFunc can filter or
// suppress errors by modifying or discarding the input error.
type ErrorFunc func(error) error

func (ef ErrorFunc) filter(err error) error {
	if ef == nil {
		return err
	}
	return ef(err)
}

// Trigger creates an ErrorFunc that calls f each time a task reports an error.
// The resulting ErrorFunc returns task errors unmodified.
func Trigger(f func()) ErrorFunc { return func(e error) error { f(); return e } }

// Listen creates an ErrorFunc that reports each non-nil task error to f.  The
// resulting ErrorFunc returns task errors unmodified.
func Listen(f func(error)) ErrorFunc { return func(e error) error { f(e); return e } }

// NoError adapts f to a Task that executes f and reports a nil error.
func NoError(f func()) Task { return func() error { f(); return nil } }

// Limit returns g and a function that starts each task passed to it in g,
// allowing no more than n tasks to be active concurrently.  If n ≤ 0, the
// start function is equivalent to g.Go, which enforces no limit.
//
// The limiting mechanism is optional, and the underlying group is not
// restricted. A call to the start function will block until a slot is
// available, but calling g.Go directly will add a task unconditionally and
// will not take up a limiter slot.
func (g *Group) Limit(n int) (*Group, func(Task) *Group) {
	if n <= 0 {
		return g, g.Go
	}
	adm := make(chan struct{}, n)
	return g, func(task Task) *Group {
		adm <- struct{}{}
		return g.Go(func() error {
			defer func() { <-adm }()
			return task()
		})
	}
}

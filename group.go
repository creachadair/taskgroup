// Package group manages a collection of cancellable goroutines.
//
// Basic usage example:
//   g := group.New(context.Background())
//   g.Go(func(ctx context.Context) error {
//     ...
//   })
//   if err := g.Wait(); err != nil {
//     log.Fatal(err)
//   }
//
package group

import (
	"sync"

	"golang.org/x/net/context"
)

// A Task is a function that performs some arbitrary task and returns an error
// value, and is the basic unit of work in a Group.  A Task that requires other
// state can be expressed as a method, for example:
//
//   type myTask struct{
//      // ... various fields
//   }
//   func (t *myTask) Do(ctx context.Context) error { ... }
//
// The caller can use t.Do as an argument to the Go method of a Group.
type Task func(context.Context) error

// Interface is the interface satisfied by a Group.  It is defined as an
// interface to allow composition of groups with throttlers.
type Interface interface {
	// Go adds a task to the group, returning an error if that is impossible.
	Go(Task) error

	// Wait blocks until all the tasks in the group are complete, and returns
	// the error value from the first failed task (if any) or nil.
	//
	// It is safe to invoke Wait concurrently from multiple goroutines, and the
	// result is idempotent.  After Wait has completed at least once, the group
	// will reject any additional tasks.
	Wait() error

	// Cancel signals the tasks in the group to stop their work by cancelling
	// their context.  It does not block.
	Cancel()
}

// A Group represents a collection of cooperating goroutines that share a
// context.Context.  New tasks can be added to the group via the Go method.
//
// By default, if any task in the group returns an error, the context
// associated with the group is cancelled; this can be overridden with the
// OnError option.  Tasks should check the done channel of the context as
// appropriate to detect such a cancellation.
//
// The caller may explicitly cancel the goroutines using the Cancel method.
// The Wait method should be called to wait for all the goroutines to finish.
type Group struct {
	ctx     context.Context
	cancel  context.CancelFunc
	onError func(error)    // called the first time a task returns non-nil
	err     error          // final result
	errc    chan<- error   // input to error collector
	edone   chan struct{}  // completion signal from error collector
	wait    sync.Once      // triggers shutdown in Wait
	wg      sync.WaitGroup // active goroutines
}

// New constructs a new, empty group based on the specified context.
func New(ctx context.Context, opts ...Option) *Group {
	gc, cancel := context.WithCancel(ctx)
	errc := make(chan error)
	g := &Group{
		ctx:     gc,
		cancel:  cancel,
		onError: func(error) { cancel() },
		errc:    errc,
		edone:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(g)
	}
	go func() {
		defer close(g.edone)
		for e := range errc {
			if e != nil && g.err == nil {
				g.err = e
				g.onError(e)
			}
		}
	}()
	return g
}

// Go adds a new task to the group.  If the group context has been cancelled,
// this function returns an error.
func (g *Group) Go(task Task) error {
	g.wg.Add(1)
	select {
	case <-g.ctx.Done(): // context is cancelled; no more tasks
		g.wg.Done()
		return g.ctx.Err()
	default:
		go func() {
			defer g.wg.Done()
			if err := task(g.ctx); err != nil {
				g.errc <- err
			}
		}()
	}
	return nil
}

// Wait blocks until all the goroutines currently in the group are finished
// executing (or have been cancelled). Wait returns nil if all tasks completed
// successfully; otherwise it returns the first non-nil error returned by a
// task (or caused by a cancellation).
//
// After Wait has been called, no further goroutines may enter the group.
func (g *Group) Wait() error {
	g.wait.Do(func() {
		g.wg.Wait() // wait for all active goroutines to finish
		g.cancel()  // no more goroutines may enter

		// At this point, additional goroutines may have entered the group
		// after we finished waiting, but before we finished cancelling.  We
		// need to wait for the stragglers to notice the cancellation and exit.
		// Usually this will be a (near) no-op.  Any task that reaches Go now
		// will be turned away because we cancelled.
		g.wg.Wait()

		close(g.errc) // signal the error collector to stop
	})
	<-g.edone // wait for the error collector to finish
	return g.err
}

// Cancel cancels the goroutines in the group.  This method does not block;
// call Wait if you want to know when the effect is complete.
func (g *Group) Cancel() { g.cancel() }

// An Option is a setting that controls the behaviour of a *Group.
type Option func(*Group)

// OnError returns an Option that provides a function to be invoked the first
// time a task in the group returns a non-nil error.  The error value from the
// task is passed to f.
//
// By default, the group will cancel itself on the first error value; setting
// OnError(nil) will disable this behaviour.
func OnError(f func(error)) Option {
	if f == nil {
		return func(g *Group) { g.onError = func(error) {} } // do nothing
	}
	return func(g *Group) { g.onError = f }
}

// WaitThen acts as g.Wait, and executes then (unconditionally) before
// returning the resulting error value.
func WaitThen(g Interface, then func()) error {
	defer then()
	return g.Wait()
}

// Single returns a new group containing a single task.  A typical use for this is
// to manage a worker that updates a data structure from a channel, and to know when
// it has completed.
//
// Example:
//    var results []result
//    ch := make(chan result)
//    collect := group.Single(ctx, func(ctx context.Context) error {
//       for r := range ch {
//         results = append(results, r)
//       }
//       return nil
//    })
//    g := group.New(ctx)
//    // ... add tasks to g as desired ...
//    err := group.WaitThen(g, func() { close(ch) }) // all the tasks are done
//    collect.Wait() // wait for the collector to complete
//
func Single(ctx context.Context, task Task) *Group {
	g := New(ctx)
	if err := g.Go(task); err != nil {
		panic(err) // should not be a possible condition
	}
	return g
}

// CheckDone returns ctx.Err() if ctx has been cancelled or reached its
// deadline, otherwise it returns nil.  This function does not block.
func CheckDone(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

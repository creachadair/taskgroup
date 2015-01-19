// Package group manages a collection of cancellable goroutines.
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
	// the error value from the first failed task (if any) or nil.  It is safe
	// to invoke Wait concurrently from multiple goroutines, and the result is
	// idempotent.
	Wait() error

	// Cancel signals the tasks in the group to stop their work by cancelling
	// their context.  It does not block.
	Cancel()
}

// A Group represents a collection of cooperating goroutines that share a
// context.Context.  New tasks can be added to the group via the Go method.
//
// If any task in the group returns an error, the context associated with the
// group is cancelled.  Tasks should check the done channel of the context as
// appropriate to detect such a cancellation.
//
// The caller may explicitly cancel the goroutines using the Cancel method.
// The Wait method should be called to wait for all the goroutines to finish.
type Group struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	errc   chan<- error
	edone  chan struct{}
	wg     sync.WaitGroup
}

// New constructs a new, empty group based on the specified context.
func New(ctx context.Context) *Group {
	gc, cancel := context.WithCancel(ctx)
	errc := make(chan error)
	g := &Group{
		ctx:    gc,
		cancel: cancel,
		errc:   errc,
		edone:  make(chan struct{}),
	}
	go func() {
		defer close(g.edone)
		for e := range errc {
			if e != nil && g.err == nil {
				g.err = e
				g.cancel()
			}
		}
	}()
	return g
}

// Go adds a new task to the group.  If the group context has been cancelled,
// this function returns an error.
func (g *Group) Go(task Task) error {
	select {
	case <-g.ctx.Done():
		return g.ctx.Err()
	default:
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			if err := task(g.ctx); err != nil {
				g.errc <- err
			}
		}()
	}
	return nil
}

// Wait blocks until all the goroutines in the group are finished executing (or
// have been cancelled). Wait returns nil if all tasks completed successfully;
// otherwise it returns the first non-nil error returned by a task (or caused
// by a cancellation).
func (g *Group) Wait() error {
	g.wg.Wait()
	close(g.errc)
	<-g.edone
	return g.err
}

// WaitThen acts as Wait, then executes f (unconditionally) before returning
// the resulting error value.
func (g *Group) WaitThen(f func()) error {
	defer f()
	return g.Wait()
}

// Cancel cancels the goroutines in the group.  This method does not block;
// call Wait if you want to know when the effect is complete.
func (g *Group) Cancel() { g.cancel() }

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
//    addTasksTo(g)
//    ...
//    err := g.Wait()  // all the tasks are done
//    close(ch)        // signal the collector
//    collect.Wait()   // ... and wait for it to complete
//
func Single(ctx context.Context, task Task) *Group {
	g := New(ctx)
	if err := g.Go(task); err != nil {
		panic(err) // should not be a possible condition
	}
	return g
}

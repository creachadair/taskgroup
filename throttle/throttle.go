// Package throttle defines rate and concurrency controllers for groups.  A
// throttler delegates tasks to an underlying group under certain conditions.
//
// This package defines the following functions:
//
// Rate returns a wrapper that ensures that tasks enter the group no faster
// than a predefined number per unit time.
//
// Capacity returns a wrapper that restricts the number of active goroutines in
// the group to be no more than a given limit.
//
// Each of these implementations satisfies group.Interface, allowing them to be
// composed (i.e., you can combine Rate and Capacity).
package throttle

import (
	"time"

	"golang.org/x/net/context"

	"bitbucket.org/creachadair/group"
)

type rateLimit struct {
	group.Interface
	adm *time.Ticker // current admission ticker
}

// Rate constructs a group that admits at most one new task to g per interval.
func Rate(g group.Interface, interval time.Duration) group.Interface {
	return &rateLimit{
		Interface: g,
		adm:       time.NewTicker(interval),
	}
}

// Go adds the task to the group, blocking until the next available admission
// interval has been reached.  If the group is complete, this blocks forever.
func (r *rateLimit) Go(task group.Task) error {
	<-r.adm.C
	return r.Interface.Go(task)
}

// Wait waits for all the tasks in the group to finish, then disables the ticker.
func (r *rateLimit) Wait() error { return group.WaitThen(r.Interface, r.adm.Stop) }

// A capacity restricts the number of active goroutines in the group to a fixed
// upper limit.
type capacity struct {
	group.Interface
	adm chan struct{}
}

// Capacity constructs a group that allows at most n (> 0) concurrent
// goroutines to be active in g at a time.
func Capacity(g group.Interface, n int) group.Interface {
	return &capacity{
		Interface: g,
		adm:       make(chan struct{}, n),
	}
}

// Go adds the task to the group, blocking until an available task slot is
// available.  The task slot will automatically be returned when the task
// returns, whether or not it was successful.
func (c *capacity) Go(task group.Task) error {
	c.adm <- struct{}{} // enter a token into the bucket
	return c.Interface.Go(func(ctx context.Context) error {
		defer func() { <-c.adm }() // reclaim a token from the bucket
		return task(ctx)
	})
}

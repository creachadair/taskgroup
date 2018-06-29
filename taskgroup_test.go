package taskgroup

import (
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/context"
)

const numTasks = 64

// randwait sleeps for a random time of up to n milliseconds.
func randwait(n int) <-chan time.Time {
	return time.After(time.Duration(rand.Intn(n)) * time.Millisecond)
}

// busyWork returns a Task that does nothing for n ms and returns err.
func busyWork(n int, err error) Task { return func() error { <-randwait(n); return err } }

func TestBasic(t *testing.T) {
	// Verify that the group works at all.
	g := New(nil).Go(busyWork(25, nil))
	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected task error: %v", err)
	}

	// Verify that the group can be reused.
	g.Go(busyWork(50, nil))
	g.Go(busyWork(75, nil))
	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected task error: %v", err)
	}
}

func TestErrorPropagation(t *testing.T) {
	var errBogus = errors.New("bogus")
	g := New(nil).Go(func() error { return errBogus })
	if err := g.Wait(); err != errBogus {
		t.Errorf("Wait: got error %v, wanted %v", err, errBogus)
	}
}

func TestCancellation(t *testing.T) {
	var errs []error
	g := New(Listen(func(err error) {
		errs = append(errs, err)
	}))

	errOther := errors.New("something is wrong")
	ctx, cancel := context.WithCancel(context.Background())
	var numOK int32
	for i := 0; i < numTasks; i++ {
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-randwait(1):
				return errOther
			case <-randwait(1):
				atomic.AddInt32(&numOK, 1)
				return nil
			}
		})
	}
	cancel()
	g.Wait()
	var numCanceled, numOther int
	for _, err := range errs {
		switch err {
		case context.Canceled:
			numCanceled++
		case errOther:
			numOther++
		default:
			t.Errorf("Unexpected error: %v", err)
		}
	}
	t.Logf("Got %d successful tasks, %d cancelled tasks, and %d other errors",
		numOK, numCanceled, numOther)
	if total := int(numOK) + numCanceled + numOther; total != numTasks {
		t.Errorf("Task count mismatch: got %d results, wanted %d", total, numTasks)
	}
}

func TestCapacity(t *testing.T) {
	const maxCapacity = 25
	const numTasks = 1492
	g := New(nil)
	start := g.Limit(maxCapacity)

	var p peakValue
	var n int32
	for i := 0; i < numTasks; i++ {
		start(func() error {
			p.inc()
			defer p.dec()
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt32(&n, 1)
			return nil
		})
	}
	g.Wait()
	t.Logf("Total tasks completed: %d", n)
	if p.max > maxCapacity {
		t.Errorf("Exceeded maximum capacity: got %d, want %d", p.max, maxCapacity)
	} else {
		t.Logf("Maximum concurrent tasks: %d", p.max)
	}
}

type peakValue struct {
	μ        sync.Mutex
	cur, max int
}

func (p *peakValue) inc() {
	p.μ.Lock()
	p.cur++
	if p.cur > p.max {
		p.max = p.cur
	}
	p.μ.Unlock()
}

func (p *peakValue) dec() {
	p.μ.Lock()
	p.cur--
	p.μ.Unlock()
}

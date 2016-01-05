package group

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

func TestSimple(t *testing.T) {
	g := New()
	g.Go(func() error { <-randwait(250); return nil })
	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected task error: %v", err)
	}
}

func TestErrorPropagation(t *testing.T) {
	var errBogus = errors.New("bogus")
	g := New()
	g.Go(func() error { return errBogus })
	if err := g.Wait(); err != errBogus {
		t.Errorf("Wait: got error %v, wanted %v", err, errBogus)
	}
}

func TestCancellation(t *testing.T) {
	var errs []error
	g := New(OnError(func(err error) {
		errs = append(errs, err)
	}))

	errOther := errors.New("something is wrong")
	ctx, cancel := context.WithCancel(context.Background())
	var numOK int32
	g.StartN(numTasks, func() error {
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
	g := New()
	start := Capacity(g, maxCapacity)

	var p peakValue
	for i := 0; i < numTasks; i++ {
		start(func() error {
			p.inc()
			defer p.dec()
			time.Sleep(2 * time.Millisecond)
			return nil
		})
	}
	g.Wait()
	if p.max > maxCapacity {
		t.Errorf("Exceeded maximum capacity: got %d, want %d", p.max, maxCapacity)
	} else {
		t.Logf("Saw a maximum of %d concurrent tasks", p.max)
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

package taskgroup_test

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creachadair/taskgroup"
	"github.com/fortytw2/leaktest"
)

const numTasks = 64

// randms returns a random duration of up to n milliseconds.
func randms(n int) time.Duration { return time.Duration(rand.Intn(n)) * time.Millisecond }

// busyWork returns a Task that does nothing for n ms and returns err.
func busyWork(n int, err error) taskgroup.Task {
	return func() error { time.Sleep(randms(n)); return err }
}

func TestBasic(t *testing.T) {
	defer leaktest.Check(t)()

	// Verify that the group works at all.
	g := taskgroup.New(nil).Go(busyWork(25, nil))
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
	defer leaktest.Check(t)()

	var errBogus = errors.New("bogus")
	g := taskgroup.New(nil).Go(func() error { return errBogus })
	if err := g.Wait(); err != errBogus {
		t.Errorf("Wait: got error %v, wanted %v", err, errBogus)
	}
}

func TestCancellation(t *testing.T) {
	defer leaktest.Check(t)()

	var errs []error
	g := taskgroup.New(taskgroup.Listen(func(err error) {
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
			case <-time.After(randms(1)):
				return errOther
			case <-time.After(randms(1)):
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
	defer leaktest.Check(t)()

	const maxCapacity = 25
	const numTasks = 1492
	g, start := taskgroup.New(nil).Limit(maxCapacity)

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

func TestRegression(t *testing.T) {
	t.Run("WaitRace", func(t *testing.T) {
		ready := make(chan struct{})
		g := taskgroup.New(nil).Go(func() error {
			<-ready
			return nil
		})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); g.Wait() }()
		go func() { defer wg.Done(); g.Wait() }()

		close(ready)
		wg.Wait()
	})
}

func TestSingleTask(t *testing.T) {
	defer leaktest.Check(t)()

	sentinel := errors.New("expected value")
	release := make(chan error)

	s := taskgroup.Single(func() error {
		return <-release
	})

	g := taskgroup.New(nil).Go(func() error {
		if err := s.Wait(); err != sentinel {
			t.Errorf("Background Wait: got %v, want %v", err, sentinel)
		}
		return nil
	})

	release <- sentinel
	if err := s.Wait(); err != sentinel {
		t.Errorf("Foreground Wait: got %v, want %v", err, sentinel)
	}
	g.Wait()
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

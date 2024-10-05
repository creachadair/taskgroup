package taskgroup_test

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creachadair/taskgroup"
	"github.com/fortytw2/leaktest"
)

const numTasks = 64

// randms returns a random duration of up to n milliseconds.
func randms(n int) time.Duration { return time.Duration(rand.IntN(n)) * time.Millisecond }

// busyWork returns a Task that does nothing for n ms and returns err.
func busyWork(n int, err error) taskgroup.Task {
	return func() error { time.Sleep(randms(n)); return err }
}

func TestBasic(t *testing.T) {
	defer leaktest.Check(t)()

	t.Logf("Group value is %d bytes", reflect.TypeOf((*taskgroup.Group)(nil)).Elem().Size())

	// Verify that the group works at all.
	var g taskgroup.Group
	g.Go(busyWork(25, nil))
	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected task error: %v", err)
	}

	// Verify that the group can be reused.
	g.Go(busyWork(50, nil))
	g.Go(busyWork(75, nil))
	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected task error: %v", err)
	}

	t.Run("Zero", func(t *testing.T) {
		g := taskgroup.New(nil)
		g.Go(busyWork(30, nil))
		if err := g.Wait(); err != nil {
			t.Errorf("Unexpected task error: %v", err)
		}

		_, run := g.Limit(1)
		run(busyWork(60, nil))
		if err := g.Wait(); err != nil {
			t.Errorf("Unexpected task error: %v", err)
		}
	})
}

func TestErrorPropagation(t *testing.T) {
	defer leaktest.Check(t)()

	var errBogus = errors.New("bogus")

	var g taskgroup.Group
	g.Go(func() error { return errBogus })
	if err := g.Wait(); err != errBogus {
		t.Errorf("Wait: got error %v, wanted %v", err, errBogus)
	}

	g.OnError(func(error) error { return nil }) // discard
	g.Go(func() error { return errBogus })
	if err := g.Wait(); err != nil {
		t.Errorf("Wait: got error %v, wanted nil", err)
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
	for range numTasks {
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

	// Verify that multiple groups sharing a throttle respect the combined
	// capacity limit.
	throttle := taskgroup.NewThrottle(maxCapacity)
	var g1, g2 taskgroup.Group
	start1 := throttle.Limit(&g1)
	start2 := throttle.Limit(&g2)

	var p peakValue
	var n int32
	for i := range numTasks {
		start := start1
		if i%2 == 1 {
			start = start2
		}
		start(func() error {
			p.inc()
			defer p.dec()
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt32(&n, 1)
			return nil
		})
	}
	g1.Wait()
	g2.Wait()
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
		var g taskgroup.Group
		g.Go(func() error {
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
	t.Run("WaitUnstarted", func(t *testing.T) {
		defer func() {
			if x := recover(); x != nil {
				t.Errorf("Unexpected panic: %v", x)
			}
		}()
		var g taskgroup.Group
		g.Wait()
	})
}

func TestSingleTask(t *testing.T) {
	defer leaktest.Check(t)()

	sentinel := errors.New("expected value")

	t.Run("Early", func(t *testing.T) {
		release := make(chan struct{})

		s := taskgroup.Go(func() error {
			defer close(release)
			return sentinel
		})

		select {
		case <-release:
			if err := s.Wait(); err != sentinel {
				t.Errorf("Wait: got %v, want %v", err, sentinel)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("Timed out waiting for task to finish")
		}
	})

	t.Run("Late", func(t *testing.T) {
		release := make(chan error, 1)
		s := taskgroup.Go(func() error {
			return <-release
		})

		var g taskgroup.Group
		g.Run(func() {
			if err := s.Wait(); err != sentinel {
				t.Errorf("Background Wait: got %v, want %v", err, sentinel)
			}
		})

		release <- sentinel
		if err := s.Wait(); err != sentinel {
			t.Errorf("Foreground Wait: got %v, want %v", err, sentinel)
		}
		g.Wait()
	})
}

func TestWaitMoreTasks(t *testing.T) {
	defer leaktest.Check(t)()

	var results int
	coll := taskgroup.Collect(func(int) {
		results++
	})

	var g taskgroup.Group

	// Test that if a task spawns more tasks on its own recognizance, waiting
	// correctly waits for all of them provided we do not let the group go empty
	// before all the tasks are spawned.
	var countdown func(int) int
	countdown = func(n int) int {
		if n > 1 {
			// The subordinate task, if there is one, is started before this one
			// exits, ensuring the group is kept "afloat".
			g.Go(coll.Run(func() int {
				return countdown(n - 1)
			}))
		}
		return n
	}

	g.Go(coll.Run(func() int { return countdown(15) }))
	g.Wait()

	if results != 15 {
		t.Errorf("Got %d results, want 10", results)
	}
}

func TestSingleResult(t *testing.T) {
	defer leaktest.Check(t)()

	release := make(chan struct{})

	s := taskgroup.Call(func() (int, error) {
		<-release
		return 25, nil
	})
	time.AfterFunc(2*time.Millisecond, func() { close(release) })

	res, err := s.Wait().Get()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if res != 25 {
		t.Errorf("Result: got %v, want 25", res)
	}
}

func TestCollector(t *testing.T) {
	defer leaktest.Check(t)()

	var sum int
	c := taskgroup.Collect(func(v int) { sum += v })

	vs := rand.Perm(15)
	var g taskgroup.Group

	for i, v := range vs {
		v := v
		if v > 10 {
			// This value should not be accumulated.
			g.Go(c.Call(func() (int, error) {
				return -100, errors.New("don't add this")
			}))
		} else if i%2 == 0 {
			// A function with an error.
			g.Go(c.Call(func() (int, error) { return v, nil }))
		} else {
			// A function without an error.
			g.Go(c.Run(func() int { return v }))
		}
	}
	g.Wait() // wait for tasks to finish

	if want := (10 * 11) / 2; sum != want {
		t.Errorf("Final result: got %d, want %d", sum, want)
	}
}

func TestCollector_Report(t *testing.T) {
	defer leaktest.Check(t)()

	var sum int
	c := taskgroup.Collect(func(v int) { sum += v })

	var g taskgroup.Group
	g.Go(c.Report(func(report func(v int)) error {
		for _, v := range rand.Perm(10) {
			report(v)
		}
		return nil
	}))

	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected error from group: %v", err)
	}
	if want := (9 * 10) / 2; sum != want {
		t.Errorf("Final result: got %d, want %d", sum, want)
	}
}

func TestGatherer(t *testing.T) {
	defer leaktest.Check(t)()

	g, run := taskgroup.New(nil).Limit(4)
	checkWait := func(t *testing.T) {
		t.Helper()
		if err := g.Wait(); err != nil {
			t.Errorf("Unexpected error from Wait: %v", err)
		}
	}

	t.Run("Call", func(t *testing.T) {
		var sum int
		r := taskgroup.Gather(run, func(v int) {
			sum += v
		})

		for _, v := range rand.Perm(15) {
			r.Call(func() (int, error) {
				if v > 10 {
					return -100, errors.New("don't add this")
				}
				return v, nil
			})
		}

		g.Wait()
		if want := (10 * 11) / 2; sum != want {
			t.Errorf("Final result: got %d, want %d", sum, want)
		}
	})

	t.Run("Run", func(t *testing.T) {
		var sum int
		r := taskgroup.Gather(run, func(v int) {
			sum += v
		})
		for _, v := range rand.Perm(15) {
			r.Run(func() int { return v + 1 })
		}

		checkWait(t)
		if want := (15 * 16) / 2; sum != want {
			t.Errorf("Final result: got %d, want %d", sum, want)
		}
	})

	t.Run("Report", func(t *testing.T) {
		var sum uint32
		r := taskgroup.Gather(g.Go, func(v uint32) {
			sum |= v
		})

		for _, i := range rand.Perm(32) {
			r.Report(func(report func(v uint32)) error {
				for _, v := range rand.Perm(i + 1) {
					report(uint32(1 << v))
				}
				return nil
			})
		}

		checkWait(t)
		if sum != math.MaxUint32 {
			t.Errorf("Final result: got %d, want %d", sum, math.MaxUint32)
		}
	})
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

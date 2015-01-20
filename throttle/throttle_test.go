package throttle

import (
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"

	"bitbucket.org/creachadair/group"
)

// timer keeps track of the (approximate) moments when tasks were successfully
// added to the group.
type timer struct {
	group.Interface
	μ   sync.Mutex
	arr []time.Time
}

func (t *timer) Go(task group.Task) error {
	if err := t.Interface.Go(task); err != nil {
		return err
	}
	now := time.Now()
	t.μ.Lock()
	t.arr = append(t.arr, now)
	t.μ.Unlock()
	return nil
}

func TestRateLimit(t *testing.T) {
	const numTasks = 765
	const interval = 1 * time.Millisecond
	g := &timer{
		Interface: RateLimit(group.New(context.Background()), interval),
	}
	for i := 0; i < numTasks; i++ {
		g.Go(func(_ context.Context) error { time.Sleep(2 * interval); return nil })
	}
	t.Logf("All %d tasks queued", numTasks)
	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify that each arrival is at least one interval greater than the
	// previous.  Because there is some slop between when the task is actually
	// accepted and when we measure, we use a conservative bound: It's OK as
	// long as we are within 3/4 of an interval. If interval is too small, the
	// skew will dominate and the test will flake.
	var min time.Duration
	for i, when := range g.arr[1:] {
		diff := when.Sub(g.arr[i])
		if min == 0 || diff < min {
			min = diff
		}
		if diff < 3*interval/4 {
			t.Errorf("Rate limit exceeded at #%d [%v]: %v", i, when, diff)
		}
	}
	t.Logf("Nominal interval: %v, minimum observed: %v", interval, min)
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

func TestCapacity(t *testing.T) {
	const maxCapacity = 25
	const numTasks = 1492
	g := Capacity(group.New(context.Background()), maxCapacity)

	var p peakValue
	for i := 0; i < numTasks; i++ {
		g.Go(func(_ context.Context) error {
			p.inc()
			defer p.dec()
			time.Sleep(5 * time.Millisecond)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if p.max > maxCapacity {
		t.Errorf("Exceeded maximum capacity: got %d, want %d", p.max, maxCapacity)
	} else {
		t.Logf("Saw a maximum of %d concurrent tasks", p.max)
	}
}

package taskgroup

import (
	"errors"
	"fmt"
	"log"
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
	g := New().Go(func() error { <-randwait(250); return nil })
	if err := g.Wait(); err != nil {
		t.Errorf("Unexpected task error: %v", err)
	}
}

func TestErrorPropagation(t *testing.T) {
	var errBogus = errors.New("bogus")
	g := New().Go(func() error { return errBogus })
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
	g.StartN(numTasks, func(_ int, report func(error)) {
		select {
		case <-ctx.Done():
			report(ctx.Err())
		case <-randwait(1):
			report(errOther)
		case <-randwait(1):
			atomic.AddInt32(&numOK, 1)
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

func ExampleGroup() {
	msg := make(chan string)
	g := New()
	g.Go(func() error {
		msg <- "ping"
		fmt.Println(<-msg)
		return nil
	})
	g.Go(func() error {
		fmt.Println(<-msg)
		msg <- "pong"
		return nil
	})
	g.Wait()
	fmt.Println("<done>")

	// Output:
	// ping
	// pong
	// <done>
}

func ExampleGroup_StartN() {
	var sum int32
	g := New().StartN(15, func(i int, report func(error)) {
		atomic.AddInt32(&sum, int32(i+1))
	})
	g.Wait()
	fmt.Print("sum = ", sum)
	// Output: sum = 120
}

func ExampleErrTrigger() {
	ctx, cancel := context.WithCancel(context.Background())

	const badTask = 5
	g := New(ErrTrigger(cancel))
	g.StartN(10, func(i int, report func(error)) {
		if i == badTask {
			report(fmt.Errorf("task %d failed", i))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
			return
		}
	})

	if err := g.Wait(); err == nil {
		log.Fatal("I expected an error here")
	} else {
		fmt.Println(err.Error())
	}
	// Output: task 5 failed
}

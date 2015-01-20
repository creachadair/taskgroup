package group

import (
	"errors"
	"math/rand"
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
	g := New(context.Background())
	for i := 0; i < numTasks; i++ {
		g.Go(func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-randwait(250):
				return nil
			}
		})
	}
	if err := g.Wait(); err != nil {
		t.Errorf("Group failed, error %v", err)
	}
}

func TestCancellation(t *testing.T) {
	g := New(context.Background())
	for i := 0; i < numTasks; i++ {
		g.Go(func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-randwait(250):
				return nil
			}
		})
	}
	g.Cancel()
	if err := g.Wait(); err != context.Canceled {
		t.Errorf("Group error: got %v, want %v", err, context.Canceled)
	} else {
		t.Logf("Got desired error: %v", err)
	}
}

func TestErrors(t *testing.T) {
	g := New(context.Background())
	errc := make(chan error)
	var failed, cancelled int32
	for i := 0; i < numTasks; i++ {
		g.Go(func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				atomic.AddInt32(&cancelled, 1)
				return ctx.Err()
			case err := <-errc:
				atomic.AddInt32(&failed, 1)
				return err
			}
		})
	}
	want := errors.New("fall over and kack")
	go func() {
		time.Sleep(75 * time.Millisecond)
		errc <- want
	}()
	if err := g.Wait(); err == nil {
		t.Errorf("Group error: got %v, want %v", err, want)
	} else {
		t.Logf("Got desired error: %v", err)
	}
	if failed != 1 {
		t.Errorf("Failed: got %d, want 1", failed)
	}
	if cancelled != numTasks-1 {
		t.Errorf("Cancelled: got %d, want %d", cancelled, numTasks-1)
	}
}

func TestSingle(t *testing.T) {
	ctx := context.Background()

	// A single task that accumulates values from ch.
	ch := make(chan int)
	var n, sum int
	task := Single(ctx, func(_ context.Context) error {
		for v := range ch {
			n++
			sum += v
		}
		return nil
	})

	// A bunch of tasks that send work to ch.
	const numValues = 1357
	g := New(ctx)
	for i := 0; i < numValues; i++ {
		g.Go(func(_ context.Context) error {
			ch <- rand.Intn(100) - 40
			return nil
		})
	}
	if err := WaitThen(g, func() { close(ch) }); err != nil {
		t.Errorf("Wait for writers: unexpected error: %v", err)
	}

	if err := task.Wait(); err != nil {
		t.Errorf("Wait for reader: unexpected error: %v", err)
	}
	t.Logf("Results: n=%d, sum=%d", n, sum)
	if n != numValues {
		t.Errorf("Value count: got %d, want %d", n, numValues)
	}
}

func TestMultipleWaiters(t *testing.T) {
	g := New(context.Background()) // Tasks doing nonsense work
	w := New(context.Background()) // Tasks waiting for the outcome of g
	want := errors.New("a failure of imagination")

	var cancellations int32
	for i := 0; i < numTasks; i++ {
		i := i
		g.Go(func(ctx context.Context) error {
			if err := CheckDone(ctx); err != nil {
				atomic.AddInt32(&cancellations, 1)
				return err
			}
			randwait(500)
			if i == 1 {
				return want
			}
			return nil
		})
	}

	for i := 0; i < numTasks; i++ {
		w.Go(func(_ context.Context) error { return g.Wait() })
	}
	if got := w.Wait(); got != want {
		t.Errorf("Incorrect error from waiters: got %v, want %v", got, want)
	}
	t.Logf("[FYI] Saw %d cancellations", cancellations)
}

func TestOnError(t *testing.T) {
	want := errors.New("that's just, like, your opinion, man")
	var called bool
	g := New(context.Background(), OnError(func(e error) {
		t.Logf("Callback received error: %v", e)
		called = true
	}))
	g.Go(func(_ context.Context) error { return want })
	if err := g.Wait(); err != want {
		t.Errorf("Wrong error returned: got %v, want %v", err, want)
	}
	if !called {
		t.Error("The OnError callback was not invoked")
	}
}

func TestAutoCancellation(t *testing.T) {
	// Verify the default behaviour that when one of the goroutines in a group
	// fails, the others are cancelled.
	g := New(context.Background())

	// This little goroutine sleeps until cancelled, then sets cancelled=true.
	// If the cancellation doesn't occur, we'll get a deadlock (all goroutines
	// asleep) and the test will fail.
	var cancelled bool
	g.Go(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			cancelled = true
			return ctx.Err()
		}
	})

	// This little goroutine returns an error.
	g.Go(func(_ context.Context) error { return errors.New("you lose") })

	if err := g.Wait(); err == nil {
		t.Error("Wait should have returned an error, but did not")
	}
	if !cancelled {
		t.Error("The sleeper was not cancelled")
	}
}

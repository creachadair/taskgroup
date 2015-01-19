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

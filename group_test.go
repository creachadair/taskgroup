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

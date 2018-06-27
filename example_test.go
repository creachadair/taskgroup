package taskgroup

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"
)

func ExampleGroup() {
	msg := make(chan string)
	g := New(nil)
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
	g := New(nil).StartN(15, func(i int, report func(error)) {
		atomic.AddInt32(&sum, int32(i+1))
	})
	g.Wait()
	fmt.Print("sum = ", sum)
	// Output: sum = 120
}

func ExampleTrigger() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const badTask = 5
	g := New(Trigger(cancel))
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

func ExampleListen() {
	g := New(Listen(func(e error) { fmt.Println(e) }))
	g.Go(func() error { return errors.New("heard you") })
	fmt.Println(g.Wait()) // the error was preserved
	// Output:
	// heard you
	// heard you
}

func ExampleCapacity() {
	var p peakValue

	g := New(nil)
	start := Capacity(g, 4)
	for i := 0; i < 100; i++ {
		start(func() error {
			p.inc()
			defer p.dec()
			time.Sleep(1 * time.Microsecond)
			return nil
		})
	}
	g.Wait()
	fmt.Printf("Max active ≤ 4: %v\n", p.max <= 4)
	// Output:
	// Max active ≤ 4: true
}

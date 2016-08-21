package taskgroup

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"
)

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

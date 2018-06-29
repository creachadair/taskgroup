package taskgroup_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"bitbucket.org/creachadair/taskgroup"
)

func ExampleGroup() {
	msg := make(chan string)
	g := taskgroup.New(nil)
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

func ExampleTrigger() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const badTask = 5
	g := taskgroup.New(taskgroup.Trigger(cancel))
	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			if i == badTask {
				return fmt.Errorf("task %d failed", i)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
				return nil
			}
		})
	}

	if err := g.Wait(); err == nil {
		log.Fatal("I expected an error here")
	} else {
		fmt.Println(err.Error())
	}
	// Output: task 5 failed
}

func ExampleListen() {
	g := taskgroup.New(taskgroup.Listen(func(e error) {
		fmt.Println(e)
	}))
	g.Go(func() error { return errors.New("heard you") })
	fmt.Println(g.Wait()) // the error was preserved
	// Output:
	// heard you
	// heard you
}

func ExampleGroup_Limit() {
	var p peakValue

	g := taskgroup.New(nil)
	start := g.Limit(4)
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

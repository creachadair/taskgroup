package taskgroup_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/creachadair/taskgroup"
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

	// Construct a group in which any task error cancels the context.
	g := taskgroup.New(taskgroup.Trigger(cancel))

	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			if i == badTask {
				return fmt.Errorf("task %d failed", i)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
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

	g, start := taskgroup.New(nil).Limit(4)
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

func shuffled(n int) []int {
	rand.Seed(1)

	vs := make([]int, n)
	for i := range vs {
		vs[i] = i + 1
	}
	for i := n - 1; i > 0; i-- {
		p := rand.Intn(i + 1)
		vs[i], vs[p] = vs[p], vs[i]
	}
	return vs
}

func ExampleSingle() {
	var total int
	results := make(chan int)

	// Start a task to collect the results of a "search" process.
	s := taskgroup.Go(func() error {
		for v := range results {
			total += v
		}
		return nil
	})

	const numTasks = 25
	input := shuffled(500)

	// Start a bunch of tasks to find elements in the input...
	g := taskgroup.New(nil)
	for i := 0; i < numTasks; i++ {
		target := i + 1
		g.Go(func() error {
			for i, v := range input {
				if v == target {
					results <- i
					break
				}
			}
			return nil
		})
	}

	// Wait for the searchers to finish.
	g.Wait()

	// Signal the collector to stop, and wait for it to do so.
	close(results)
	s.Wait()

	// Now it is safe to use the results.
	fmt.Println(total)
	// Output:
	// 5972
}

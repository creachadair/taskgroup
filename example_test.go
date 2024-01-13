package taskgroup_test

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	vs := make([]int, n)
	for i := range vs {
		vs[i] = i + 1
	}
	rand.Shuffle(n, func(i, j int) {
		vs[i], vs[j] = vs[j], vs[i]
	})
	return vs
}

type slowReader struct {
	n int
	d time.Duration
}

func (s *slowReader) Read(data []byte) (int, error) {
	if s.n == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.d)
	nr := min(len(data), s.n)
	s.n -= nr
	for i := 0; i < nr; i++ {
		data[i] = 'x'
	}
	return nr, nil
}

func ExampleSingle() {
	// A fake reader to simulate a slow file read.
	// 2500 bytes and each read takes 50ms.
	sr := &slowReader{2500, 50 * time.Millisecond}

	var data []byte

	// Start a task to read te "file" in the background.
	fmt.Println("start")
	s := taskgroup.Go(func() error {
		var err error
		data, err = io.ReadAll(sr)
		return err
	})

	fmt.Println("work, work")
	if err := s.Wait(); err != nil {
		log.Fatalf("Read failed: %v", err)
	}
	fmt.Println("done")
	fmt.Println(len(data), "bytes")

	// Output:
	// start
	// work, work
	// done
	// 2500 bytes
}

func ExampleCollector() {
	var total int
	c := taskgroup.NewCollector(func(v int) {
		total += v
	})

	const numTasks = 25
	input := shuffled(500)

	// Start a bunch of tasks to find elements in the input...
	g := taskgroup.New(nil)
	for i := 0; i < numTasks; i++ {
		target := i + 1
		g.Go(c.Task(func() (int, error) {
			for _, v := range input {
				if v == target {
					return v, nil
				}
			}
			return 0, errors.New("not found")
		}))
	}

	// Wait for the searchers to finish, then signal the collector to stop.
	g.Wait()
	c.Wait()

	// Now get the final result.
	fmt.Println(total)
	// Output:
	// 325
}

func ExampleCollector_Stream() {
	type val struct {
		who string
		v   int
	}
	c := taskgroup.NewCollector(func(z val) { fmt.Println(z.who, z.v) })

	err := taskgroup.New(nil).
		// The Stream method passes its argument a channel where it may report
		// multiple values to the collector.
		Go(c.Stream(func(zs chan<- val) error {
			for i := 0; i < 3; i++ {
				zs <- val{"even", 2 * i}
			}
			return nil
		})).
		// Multiple streams are fine.
		Go(c.Stream(func(zs chan<- val) error {
			for i := 0; i < 3; i++ {
				zs <- val{"odd", 2*i + 1}
			}
			// An error reported by a stream is propagated just like any other
			// task error.
			return errors.New("no bueno")
		})).
		Wait()
	if err == nil || err.Error() != "no bueno" {
		log.Fatalf("Unexpected error: %v", err)
	}

	c.Wait()
	// Output unordered:
	// even 0
	// odd 1
	// even 2
	// odd 3
	// even 4
	// odd 5
}

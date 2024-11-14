package taskgroup_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/creachadair/taskgroup"
)

func ExampleGroup() {
	msg := make(chan string)
	g := taskgroup.New(nil)
	g.Run(func() {
		msg <- "ping"
		fmt.Println(<-msg)
	})
	g.Run(func() {
		fmt.Println(<-msg)
		msg <- "pong"
	})
	g.Wait()
	fmt.Println("<done>")

	// Output:
	// ping
	// pong
	// <done>
}

func ExampleNew_cancel() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const badTask = 5

	// Construct a group in which any task error cancels the context.
	g := taskgroup.New(cancel)

	for i := range 10 {
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

func ExampleNew_listen() {
	// The taskgroup itself will only report the first non-nil task error, but
	// you can use an error listener used to accumulate all of them.
	// Calls to the listener are synchronized, so we don't need a lock.
	var all []error
	g := taskgroup.New(func(e error) {
		all = append(all, e)
	})
	g.Go(func() error { return errors.New("badness 1") })
	g.Go(func() error { return errors.New("badness 2") })
	g.Go(func() error { return errors.New("badness 3") })

	if err := g.Wait(); err == nil || !strings.Contains(err.Error(), "badness") {
		log.Fatalf("Unexpected error: %v", err)
	}
	fmt.Println(errors.Join(all...))
	// Unordered output:
	// badness 1
	// badness 2
	// badness 3
}

func ExampleGroup_Limit() {
	var p peakValue

	g, start := taskgroup.New(nil).Limit(4)
	for range 100 {
		start.Run(func() {
			p.inc()
			defer p.dec()
			time.Sleep(1 * time.Microsecond)
		})
	}
	g.Wait()
	fmt.Printf("Max active ≤ 4: %v\n", p.max <= 4)
	// Output:
	// Max active ≤ 4: true
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
	for i := range nr {
		data[i] = 'x'
	}
	return nr, nil
}

func ExampleSingle() {
	// A fake reader to simulate a slow file read.
	// 2500 bytes and each read takes 50ms.
	sr := &slowReader{2500, 50 * time.Millisecond}

	// Start a task to read te "file" in the background.
	fmt.Println("start")
	s := taskgroup.Call(func() ([]byte, error) {
		return io.ReadAll(sr)
	})

	fmt.Println("work, work")
	data, err := s.Wait().Get()
	if err != nil {
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

func ExampleGatherer() {
	const numTasks = 25
	input := rand.Perm(500)

	// Start a bunch of tasks to find elements in the input...
	g := taskgroup.New(nil)

	var total int
	c := taskgroup.Gather(g.Go, func(v int) {
		total += v
	})

	for i := range numTasks {
		target := i + 1
		c.Call(func() (int, error) {
			for _, v := range input {
				if v == target {
					return v, nil
				}
			}
			return 0, errors.New("not found")
		})
	}

	// Wait for the searchers to finish, then signal the collector to stop.
	g.Wait()

	// Now get the final result.
	fmt.Println(total)
	// Output:
	// 325
}

func ExampleGatherer_Report() {
	type val struct {
		who string
		v   int
	}

	g := taskgroup.New(nil)
	c := taskgroup.Gather(g.Go, func(z val) {
		fmt.Println(z.who, z.v)
	})

	// The Report method passes its argument a function to report multiple
	// values to the collector.
	c.Report(func(report func(v val)) error {
		for i := range 3 {
			report(val{"even", 2 * i})
		}
		return nil
	})
	// Multiple reporters are fine.
	c.Report(func(report func(v val)) error {
		for i := range 3 {
			report(val{"odd", 2*i + 1})
		}
		// An error from a reporter is propagated like any other task error.
		return errors.New("no bueno")
	})
	err := g.Wait()
	if err == nil || err.Error() != "no bueno" {
		log.Fatalf("Unexpected error: %v", err)
	}
	// Unordered output:
	// even 0
	// odd 1
	// even 2
	// odd 3
	// even 4
	// odd 5
}

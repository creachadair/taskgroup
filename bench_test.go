package taskgroup_test

import (
	"math/rand/v2"
	"sync"
	"testing"
)

// A very rough benchmark comparing the performance of accumulating values with
// a separate goroutine via a channel, vs. accumulating them directly under a
// lock. The workload here is intentionally minimal, so the benchmark is
// measuring more or less just the overhead.

func BenchmarkChan(b *testing.B) {
	ch := make(chan int)
	done := make(chan struct{})
	var total int
	go func() {
		defer close(done)
		for v := range ch {
			total += v
		}
	}()
	b.ResetTimer() // discount the setup time.

	var wg sync.WaitGroup
	wg.Add(b.N)
	for i := 0; i < b.N; i++ {
		go func() {
			defer wg.Done()
			ch <- rand.IntN(1000)
		}()
	}
	wg.Wait()
	close(ch)
	<-done
}

func BenchmarkLock(b *testing.B) {
	var μ sync.Mutex
	var total int
	report := func(v int) {
		μ.Lock()
		defer μ.Unlock()
		total += v
	}
	b.ResetTimer() // discount the setup time.

	var wg sync.WaitGroup
	wg.Add(b.N)
	for i := 0; i < b.N; i++ {
		go func() {
			defer wg.Done()
			report(rand.IntN(1000))
		}()
	}
	wg.Wait()
}

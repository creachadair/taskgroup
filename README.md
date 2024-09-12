# taskgroup

[![GoDoc](https://img.shields.io/static/v1?label=godoc&message=reference&color=khaki)](https://pkg.go.dev/github.com/creachadair/taskgroup)
[![CI](https://github.com/creachadair/taskgroup/actions/workflows/go-presubmit.yml/badge.svg?event=push&branch=main)](https://github.com/creachadair/taskgroup/actions/workflows/go-presubmit.yml)

A `*taskgroup.Group` represents a group of goroutines working on related tasks.
New tasks can be added to the group at will, and the caller can wait until all
tasks are complete. Errors are automatically collected and delivered
synchronously to a user-provided callback.  This does not replace the full
generality of Go's built-in features, but it simplifies some of the plumbing
for common concurrent tasks.

Here is a [working example in the Go Playground](https://go.dev/play/p/wCZzMDXRUuM).

## Rationale

Go provides powerful concurrency primitives, including
[goroutines](http://golang.org/ref/spec#Go_statements),
[channels](http://golang.org/ref/spec#Channel_types),
[select](http://golang.org/ref/spec#Select_statements), and the standard
library's [sync](http://godoc.org/sync) package. In some common situations,
however, managing goroutine lifetimes can become unwieldy using only what is
built in.

For example, consider the case of copying a large directory tree: Walk through
a source directory recursively, creating a parallel target directory structure
and starting a goroutine to copy each of the files concurrently.  In outline:

```go
func copyTree(source, target string) error {
	err := filepath.Walk(source, func(path string, fi os.FileInfo, err error) error {
		adjusted := adjustPath(path)
		if fi.IsDir() {
			return os.MkdirAll(adjusted, 0755)
		}
		go copyFile(adjusted, target)
		return nil
	})
	if err != nil {
		// ... clean up the output directory ...
	}
	return err
}
```

This solution is deficient, however, as it does not provide any way to detect
when all the file copies are finished. To do that we will typically use a
`sync.WaitGroup`:

```go
var wg sync.WaitGroup
...
wg.Add(1)
go func() {
    defer wg.Done()
    copyFile(adjusted, target)
}()
...
wg.Wait() // block until all the tasks signal done
```

In addition, we need to handle errors. Copies might fail (the disk may fill, or
there might be a permissions error). For some applications it might suffice to
log the error and continue, but usually in case of error we should back out and
clean up the partial state.

To do that, we need to capture the return value from the function inside the
goroutine―and that will require us either to add a lock or plumb in another
channel:

```go
errs := make(chan error)
...
go copyFile(adjusted, target, errs)
```

Since multiple operations can be running in parallel, we will also need another
goroutine to drain the errors channel and accumulate the results somewhere:

```go
var failures []error
go func() {
    for e := range errs {
        failures = append(failures, e)
    }
}()
...
wg.Wait()
close(errs)
```

Once the work is finished, we must also detect when the error collector is
done, so we can examine the `failures` without a data race.  We'll need another
channel or wait group to signal for this:

```go
var failures []error
edone := make(chan struct{})
go func() {
    defer close(edone)
    for e := range errs {
        failures = append(failures, e)
    }
}()
...
wg.Wait()   // all the workers are done
close(errs) // signal the error collector to stop
<-edone     // wait for the error collector to be done
```

Another issue is, if one of the file copies fails, we don't necessarily want to
wait around for all the copies to finish before reporting the error―we want to
stop everything and clean up the whole operation. Typically we would do this
using a `context.Context`:

```go
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	...
	copyFile(ctx, adjusted, target, errs)
```

Now `copyFile` will have to check for `ctx` to be finished:

```go
func copyFile(ctx context.Context, source, target string, errs chan<- error) {
	if ctx.Err() != nil {
		return
	}
 	// ... do the copy as normal, or propagate an error
}
```

Finally, we want the ability to to limit the number of concurrent copies. Even
if the host has plenty of memory and CPU, unbounded concurrency is likely to
run us out of file descriptors.  To handle this we might use a
[semaphore](https://godoc.org/golang.org/x/sync/semaphore) or a throttling
channel:

```go
throttle := make(chan struct{}, 64)  // allow up to 64 concurrent copies
go func() {
   throttle <- struct{}{} // block until the throttle has a free slot
   defer func() { wg.Done(); <-throttle }()
   copyFile(ctx, adjusted, target, errs)
}()
```

So far, we're up to four channels (errs, edone, context, and throttle) plus a
wait group.  The point to note is that while these tools are quite able to
express what we want, it can be tedious to wire them all together and keep
track of the current state of the system.

The `taskgroup` package exists to handle the plumbing for the common case of a
group of tasks that are all working on a related outcome (_e.g.,_ copying a
directory structure), and where an error on the part of any _single_ task may
be grounds for terminating the work as a whole.

The package provides a `taskgroup.Group` type that has built-in support for
some of these concerns:

- Limiting the number of active goroutines.
- Collecting and filtering errors.
- Waiting for completion and delivering status.

A `taskgroup.Group` collects error values from each task and can deliver them
to a user-provided callback. The callback can filter them or take other actions
(such as cancellation). Invocations of the callback are all done from a single
goroutine so it is safe to manipulate local resources without a lock.

A group does not directly support cancellation, but integrates cleanly with the
standard [context](https://godoc.org/context) package. A `context.CancelFunc`
can be used as a trigger to signal the whole group when an error occurs.

A task is expressed as a `func() error`, and is added to a group using the `Go`
method:

```go
g := taskgroup.New(nil).Go(myTask)
```

Any number of tasks may be added, and it is safe to do so from multiple
goroutines concurrently.  To wait for the tasks to finish, use:

```go
err := g.Wait()
```

`Wait` blocks until all the tasks in the group have returned, and then reports
the first non-nil error returned by any of the worker tasks.

An implementation of this example can be found in `examples/copytree/copytree.go`.

## Filtering Errors

The `taskgroup.New` function takes an optional callback to be invoked for each
non-nil error reported by a task in the group. The callback may choose to
propagate, replace, or discard the error. For example, suppose we want to
ignore "file not found" errors from a copy operation:

```go
g := taskgroup.New(func(err error) error {
   if os.IsNotExist(err) {
      return nil // ignore files that do not exist
   }
   return err
})
```

This mechanism can also be used to trigger a context cancellation if a task
fails, for example:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

g := taskgroup.New(taskgroup.Trigger(cancel))
```

Now, if a task in `g` reports an error, it will cancel the context, allowing
any other running tasks to observe a context cancellation and bail out.

## Controlling Concurrency

The `Limit` method supports limiting the number of concurrently _active_
goroutines in the group. It returns a `start` function that adds goroutines to
the group, but will will block when the limit of goroutines is reached until
some of the goroutines already running have finished.

For example:

```go
// Allow at most 3 concurrently-active goroutines in the group.
g, start := taskgroup.New(nil).Limit(3)

// Start tasks by calling the function returned by taskgroup.Limit:
start(task1)
start(task2)
start(task3)
start(task4) // blocks until one of the previous tasks is finished
// ...
```

## Solo Tasks

In some cases it is useful to start a single background task to handle an
isolated concern (elsewhere sometimes described as a "promise" or a "future").

For example, suppose we want to read a file into a buffer while we take care of
some other work.  Rather than creating a whole group for a single goroutine, we
can create a solo task using the `Go` constructor.

```go
var data []byte
s := taskgroup.Go(func() error {
  f, err := os.Open(filePath)
  if err != nil {
     return err
  }
  defer f.Close()
  data, err = io.ReadAll(f)
  return err
})
```

When we're ready, we call `Wait` to receive the result of the task:

```go
if err := s.Wait(); err != nil {
   log.Fatalf("Loading config failed: %v", err)
}
doThingsWith(data)
```

## Collecting Results

One common use for a background task is accumulating the results from a batch
of concurrent workers. This can be handled by a solo task, as described above,
and it is a common enough case that the library provides a `Collector` type to
handle it specifically.

To use it, pass a function to `Collect` to receive the values:

```go
var sum int
c := taskgroup.Collect(func(v int) { sum += v })
```

The `Call`, `Run`, and `Report` methods of `c` wrap a function that yields a
value into a task. If the function reports an error, that error is returned
from the task as usual. Otherwise, its non-error value is given to the
accumulator callback. As in the above example, calls to the function are
serialized so that it is safe to access state without additional locking:

```go
// Report an error, no value for the collector.
g.Go(c.Call(func() (int, error) {
   return -1, errors.New("bad")
}))

// Report the value 25 to the collector.
g.Go(c.Call(func() (int, error) {
   return 25, nil
}))

// Report a random integer to the collector.
g.Go(c.Run(func() int { return rand.Intn(1000) })

// Report multiple values to the collector.
g.Go(c.Report(func(report func(int)) error {
   report(10)
   report(20)
   report(30)
   return nil
}))
```

Once all the tasks derived from the collector are done, it is safe to access
the values accumulated by the callback:

```go
g.Wait()  // wait for tasks to finish

// Now you can access the values accumulated by c.
fmt.Println(sum)
```

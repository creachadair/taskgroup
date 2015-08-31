# group

A `*group.Group` represents a group of goroutines working on related tasks.
New tasks can be added to the group at will, and the caller can wait until all
tasks are complete. Errors are automatically collected and delivered to a
user-provided callback in a single goroutine.  This does not replace the full
generality of Go's built-in features, but it simplifies some of the plumbing
for common concurrent tasks.

## Rationale

Go provides excellent and powerful concurrency primitives, in the form of
[goroutines](http://golang.org/ref/spec#Go_statements),
[channels](http://golang.org/ref/spec#Channel_types),
[select](http://golang.org/ref/spec#Select_statements), and the standard
library's [sync](http://godoc.org/sync) package. In some common situations,
however, managing goroutine lifetimes can become unwieldy using only what is
built in.

For example, consider the case of copying a large directory tree: Walk throught
the source directory recursively, creating the parallel target directory
structure and spinning up a goroutine to copy each of the files
concurrently. This part is simple:

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

But of course, it's not quite as easy as that, because how will you know when
all the file copies are finished? That's easy: Use a `sync.WaitGroup`:

```go
var wg sync.WaitGroup
...
wg.Add(1)
go func() {
    defer wg.Done()
    copyFile(adjusted, target)
}()
...
wg.Wait()
```

Okay. Remember, though that copies might fail -- the disk might fill up, or
there might be a permissions error, say. If you don't care, you might just log
the error and continue, but often in case of error you'd like to back out and
clean up your mess. But there's no way to capture the return value from the
function inside the goroutine; you will have to pass it back over a channel,
which means you now have to thread a channel in through the functions you
invoke from the goroutine:

```go
errs := make(chan error)
...
go copyFile(adjusted, target, errs)
```

And of course, you may get more than one error, so you will either need to
buffer the channel (so the goroutines don't block trying to write to it), or
have another goroutine to clear it out:

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

You're still not out of the woods, though, because how do you know when the
error collector is finished? You'll need another channel or `sync.WaitGroup`)
to signal for that:

```go
var failures []error
edone := make(chan struct{})
go func() {
    for e := range errs {
        failures = append(failures, e)
	}
	close(edone)	
}()
...
wg.Wait()   // all the workers are done
close(errs) // signal the error collector to stop
<-edone     // wait for the error collector to be done
```

Okay, so that works. But for this scenario, you really don't want to wait
around for all the copies to finish -- if any of the file copies fails, you
want to stop what you're doing and give up.  To do that, you'll need to pass in
another channel (say) to the workers, which you can close to signal
cancellation:

	cancel := make(chan struct{})
	...
	copyFile(adjusted, target, errs, cancel)

then `copyFile` will have to check for that:

```go
func copyFile(source, target string, errs chan<- error, cancel chan struct{}) {
	select {
	case <-cancel:
		return
	default:
	 	// ... do the copy as normal, or propagate an error
	}
}
```

The lesson here is that, while Go's concurrency primitives are easily powerful
enough to express these relationships, it can be tedious to wire them all
together. The `group` package was created to help simplify some of the plumbing
for the common case of a group of tasks that are all working on a related
outcome (_e.g.,_ copying a directory structure), and where an error on the part
of any _single_ task is grounds for terminating the work as a whole.

The `group` package supports some of the plumbing described above:

 - For cancellation, you can use [context](http://godoc.org/golang.org/x/net/context) package.
   The `group` package doesn't handle this piece.

 - The `*group.Group` value manages collecting `error` values from its tasks,
   and delivers them to a user-provided callback. Invocations of the callback
   are all done from a single goroutine, so it is safe to have the callback
   manipulate local resources without a lock.

The API for the caller is straightforward: A task is expressed as a `func()
error`, and is added to a group using the `Go` method,

```go
g := group.New()
g.Go(myTask)
```

Any number of tasks may be added, and it is safe to do so from multiple goroutines concurrently.  To wait for the tasks to finish, use:

```go
g.Wait()
```

This blocks until all the tasks in the group have returned (either successfully, or with an error).

A working program demonstrating this example can be found in the `cmd/copytree` subdirectory.

## Controlling Concurrency

Although goroutines are inexpensive to start up, it is often useful to limit the number of _active_ goroutines in a program, to avoid various kinds of resource exhaustion (CPU, memory, in-flight RPCs, etc.).  Doing so often requires a bunch of additional plumbing to restrict when goroutines may be started or become active.

The `Capacity` function limits the maximum number of goroutines that a group will have active concurrently.

Adding this is simple:

```go
g := group.New(/* ... */)

// Allow at most 25 concurrently-active goroutines in the group.
start := group.Capacity(g, 25)

// Start tasks by calling the function returned by group.Capacity:
start(task1)
start(task2)
// ...
```

# Package Documentation

You can view package documentation for the
[group](http://godoc.org/bitbucket.org/creachadair/group) package on
[GoDoc](http://godoc.org/).

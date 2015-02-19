# group

A `*group.Group` represents a group of goroutines working on related tasks, sharing a `context.Context`. As long as the group has not failed or been cancelled, new tasks can be added at will. The caller can explicitly cancel the work in progress, or wait for all the goroutines to complete. It is not intended to replace the full generality of Go's built-in features, but it simplifies some of the plumbing for common concurrent tasks.

If any task in the group returns an error, the context associated with the group is cancelled (by default). Tasks should check the done channel of the context as appropriate to detect such a cancellation.

## Rationale

Go provides excellent and powerful concurrency primitives, in the form of [goroutines](http://golang.org/ref/spec#Go_statements), [channels](http://golang.org/ref/spec#Channel_types), [select](http://golang.org/ref/spec#Select_statements), and the standard library's [sync](http://godoc.org/sync) package. In some common situations, however, managing goroutine lifetimes can become unwieldy using only what is built in.

For example, consider the case of copying a large directory tree: Walk throught the source directory recursively, creating the parallel target directory structure and spinning up a goroutine to copy each of the files concurrently. This part is simple:

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

But of course, it's not quite as easy as that, because how will you know when all the file copies are finished? That's easy: Use a `sync.WaitGroup`:

	var wg sync.WaitGroup
	...
	wg.Add(1)
	go func() {
	    defer wg.Done()
	    copyFile(adjusted, target)
	}()
	...
	wg.Wait()

Okay. Remember, though that copies might fail -- the disk might fill up, or there might be a permissions error, say. If you don't care, you might just log the error and continue, but often in case of error you'd like to back out and clean up your mess. But there's no way to capture the return value from the function inside the goroutine; you will have to pass it back over a channel, which means you now have to thread a channel in through the functions you invoke from the goroutine:

    errs := make(chan error)
    ...
	copyFile(adjusted, target, errs)

And of course, you may get more than one error, so you will either need to buffer the channel (so the goroutines don't block trying to write to it), or have another goroutine to clear it out:

	var failures []error
	go func() {
	    for e := range errs {
	        failures = append(failures, e)
	    }
	}()
	...
	wg.Wait()
	close(errs)

You're still not out of the woods, though, because how do you know when the error collector is finished? You'll need another channel or `sync.WaitGroup`) to signal for that:

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

Okay, so that works. But for this scenario, you really don't want to wait around for all the copies to finish -- if any of the file copies fails, you want to stop what you're doing and give up.  To do that, you'll need to pass in another channel (say) to the workers, which you can close to signal cancellation:

	cancel := make(chan struct{})
	...
	copyFile(adjusted, target, errs, cancel)

then `copyFile` will have to check for that:

	func copyFile(source, target string, errs chan<- error, cancel chan struct{}) {
		select {
		case <-cancel:
			return
		default:
		 	// ... do the copy as normal, or propagate an error
		}
	}

The lesson here is that, while Go's concurrency primitives are easily powerful enough to express these relationships, it can be tedious to wire them all together. The `group` package was created to help simplify some of the plumbing for the common case of a group of tasks that are all working on a related outcome (_e.g.,_ copying a directory structure), and where an error on the part of any _single_ task is grounds for terminating the work as a whole.

Under the covers, `group` uses almost exactly the plumbing described above:

 - For cancellation, it uses the [context](http://godoc.org/golang.org/x/net/context) package.
   Under the covers, `context.Context` uses a channel to signal completion.

 - The `*group.Group` value manages collecting `error` values from its tasks, and returns just one
   of them to the caller (rather than everything).

The API for the caller is straightforward:  A task is expressed as a `func(context.Context) error`, and is added to a group using the `Go` method,

	g := group.New(context.Background())
	g.Go(myTask)

Any number of tasks may be added, and it is safe to do so from multiple goroutines concurrently.  To wait for the tasks to finish, use:

	err := g.Wait()

This blocks until all the tasks in the group have returned (either successfully, or with an error).  By default, if any task returns a non-nil error, the rest of the tasks are cancelled automatically (this can be overridden with an option to `group.New`).  It is safe to call `g.Wait` from multiple concurrent goroutines, and the result is idempotent.

If the caller wants to cancel the group explicitly, simply invoke:

	g.Cancel()

A working program demonstrating this example can be found in the `cmd/copytree` subdirectory.

## Controlling Concurrency: Capacity and Rate

Although goroutines are inexpensive to start up, it is often useful to limit the number of _active_ goroutines in a program, to avoid various kinds of resource exhaustion (CPU, memory, in-flight RPCs, etc.).  Doing so often requires a bunch of additional plumbing to restrict when goroutines may be started or become active.

The `group/throttle` package supports a couple of simple cases of this:

 - You can limit the _capacity_ of a group, that is the maximum number of goroutines that the group
   will allow to be active concurrently.  This is handled by `throttle.Capacity`.
 
 - You can limit the _admission rate_ of a group, that is how quickly new goroutines are accepted and
   started by the group.  This is handled by `throttle.Rate`.

Adding these to an existing group is simple:

	// Allow at most 25 concurrently-active goroutines in the group.
	g := throttle.Capacity(group.New(context.Background()), 25)

	// Admit at most 1 goroutine to the group every 750 milliseconds.
	g := throttle.Rate(group.New(context.Background()), 750*time.Millisecond)

The values returned by the `throttle` functions also satisfy the group interface, so they can be composed with each other if you wish.  In each case, their `Go` method will block until the constraint is satisfied.

# Package Documentation

You can view package documentation for the
[group](http://godoc.org/bitbucket.org/creachadair/group) and 
[throttle](http://godoc.org/bitbucket.org/creachadair/group/throttle)
packages on [GoDoc](http://godoc.org/).

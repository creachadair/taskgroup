# group

A `*group.Group` represents a group of goroutines working on related tasks,
that share a `context.Context`.  As long as the group has not been cancelled
or failed, new tasks can be added at will.  The caller can explicitly cancel
the work in progress, or wait for all the goroutines to complete.  This
simplifies some of the plumbing for a common concurrency pattern.

If any task in the group returns an error, the context associated with the
group is cancelled.  Tasks should check the done channel of the context as
appropriate to detect such a cancellation.

View documentation on [GoDoc](http://godoc.org/bitbucket.org/creachadair/group).

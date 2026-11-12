// Coroutines for iter.Pull, built on goroutines that hand control back and
// forth with park and ready. Compiled only by marmot.

package runtime

import "unsafe"

// coro is a coroutine: at any time exactly one of the two sides runs.
type coro struct {
	lock    mutex
	f       func(*coro)
	other   *g // the parked side, resumed by the next coroswitch
	started bool
	done    bool
}

// newcoro and coroswitch are pulled by package iter with
// //go:linknamestd (symbols runtime.newcoro, runtime.coroswitch); the
// iter_ variants also cover a plain iter.newcoro symbol.
func newcoro(f func(*coro)) *coro {
	// The gc runtime creates the coroutine's goroutine here; marmot
	// starts it on the first switch, and counts it as a goroutine
	// meanwhile (NumGoroutine).
	atomicXadd32(&corosUnstarted, 1)
	return &coro{f: f}
}

// corosUnstarted counts coroutines made by newcoro whose goroutine has not
// started yet.
var corosUnstarted uint32

//go:linkname iter_newcoro iter.newcoro
func iter_newcoro(f func(*coro)) *coro { return newcoro(f) }

//go:linkname iter_coroswitch iter.coroswitch
func iter_coroswitch(c *coro) { coroswitch(c) }

// coroReadyCommit readies the other side (a *g) after the switching
// goroutine is off its stack, so the two never run at once.
func coroReadyCommit(p unsafe.Pointer) {
	if p != nil {
		rt_ready((*g)(p).handle)
	}
}

func coroswitch(c *coro) {
	gp := getg()
	lock(&c.lock)
	if c.done {
		unlock(&c.lock)
		panic(plainError("coroswitch on exited coro"))
	}
	if !c.started {
		c.started = true
		c.other = gp
		unlock(&c.lock)
		atomicXadd32(&corosUnstarted, -1)
		rt_go(lazyPC(&corostartPC, corostartCode), unsafe.Pointer(c))
		goparkForever()
		return
	}
	next := c.other
	c.other = gp
	unlock(&c.lock)
	rt_park(lazyPC(&coroReadyCommitPC, coroReadyCommitCode), unsafe.Pointer(next))
}

// corostart runs the coroutine body on its own goroutine. When the body
// returns, or calls runtime.Goexit (which runs deferred calls before the
// goroutine ends), control goes back to the side that switched to it
// last.
func corostart(p unsafe.Pointer) {
	c := (*coro)(p)
	defer coroexit(c)
	c.f(c)
}

// coroexit marks c done and readies the goroutine waiting for it.
func coroexit(c *coro) {
	lock(&c.lock)
	c.done = true
	next := c.other
	c.other = nil
	unlock(&c.lock)
	if next != nil {
		rt_ready(next.handle)
	}
}

func corostartCode() unsafe.Pointer {
	f := corostart
	return funcPC(unsafe.Pointer(&f))
}

func coroReadyCommitCode() unsafe.Pointer {
	f := coroReadyCommit
	return funcPC(unsafe.Pointer(&f))
}

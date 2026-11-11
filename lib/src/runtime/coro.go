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
	return &coro{f: f}
}

//go:linkname iter_newcoro iter.newcoro
func iter_newcoro(f func(*coro)) *coro { return newcoro(f) }

//go:linkname iter_coroswitch iter.coroswitch
func iter_coroswitch(c *coro) { coroswitch(c) }

// coroSwitchArg passes the goroutine to wake to the park commit function.
type coroSwitchArg struct {
	next *g
}

// coroReadyCommit readies the other side after the switching goroutine is
// off its stack, so the two never run at once.
func coroReadyCommit(p unsafe.Pointer) {
	a := (*coroSwitchArg)(p)
	if a.next != nil {
		rt_ready(a.next.handle)
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
		start := corostart
		newprocArg(start, unsafe.Pointer(c))
		gopark(nil, nil)
		return
	}
	next := c.other
	c.other = gp
	unlock(&c.lock)
	arg := &coroSwitchArg{next: next}
	commit := coroReadyCommit
	rt_park(funcPC(unsafe.Pointer(&commit)), unsafe.Pointer(arg))
}

// corostart runs the coroutine body on its own goroutine. When the body
// returns, control goes back to the side that switched to it last.
func corostart(p unsafe.Pointer) {
	c := (*coro)(p)
	c.f(c)
	lock(&c.lock)
	c.done = true
	next := c.other
	c.other = nil
	unlock(&c.lock)
	if next != nil {
		rt_ready(next.handle)
	}
}

// newprocArg starts a goroutine running fn(arg). fn must be a top-level
// function.
func newprocArg(fn func(unsafe.Pointer), arg unsafe.Pointer) {
	rt_go(funcPC(unsafe.Pointer(&fn)), arg)
}

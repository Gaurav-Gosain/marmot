// Defer, panic, recover and Goexit.
//
// Protocol with the compiler (see docs/runtime-entry.md for the full text):
//
//   - A function that contains a defer statement owns one _deferframe in its
//     stack frame. On entry it calls rt_setjmp(&fr.jmpbuf) (a returns_twice
//     call). A nonzero result means a deferred call recovered a panic: the
//     function then calls deferreturn(&fr) and returns its named results
//     (the SSA Recover block).
//   - Each defer statement fills a _defer record (in the frame, or on the
//     heap when the statement is in a loop) with fn and frame = &fr, and
//     calls deferpush(d). fn is a func() closure; for `defer f(a, b)` the
//     compiler builds a closure that calls f(a, b) and sets wrapped = true.
//   - Every return path calls deferreturn(&fr) before returning.
//   - A defer statement in a range-over-func loop body belongs to the
//     enclosing function: d.frame is that function's frame (the SSA
//     DeferStack operand) and d.rangefunc is set.
//   - recover() compiles to gorecover(fp) where fp is the frame address of
//     the function that contains the recover call (llvm.frameaddress(0)).
//     Functions that call recover must not be inlined and must keep frame
//     records. A wrapped thunk must call its target with a real call (no
//     tail call), so that the target's frame record points at the thunk's
//     frame. A zero fp skips the "called directly by a deferred function"
//     check.
//
// Compiled only by marmot (it uses rt_setjmp/rt_longjmp and getcallerfp).

package runtime

import "unsafe"

// _deferframe is allocated in the stack frame of a function with defers.
type _deferframe struct {
	jmpbuf [rtJmpbufWords]uintptr
}

// _defer is one pending deferred call.
type _defer struct {
	link  *_defer
	fn    func()
	frame *_deferframe
	// wrapped is true when fn is a compiler generated closure that calls
	// the deferred function directly (defer f(args)).
	wrapped bool
	// rangefunc is true for a defer statement in a range-over-func loop
	// body: the record belongs to the enclosing function's frame but is
	// pushed while other frames (the iterator, the body closure) are
	// active, so it may sit above their records on the chain.
	rangefunc bool
}

// deferpush adds d to the current goroutine's defer chain.
func deferpush(d *_defer) {
	gp := getg()
	d.link = gp._defer
	gp._defer = d
}

// deferreturn runs and unlinks, in LIFO order, the deferred calls that
// belong to frame fr. The compiler calls it on every return path of a
// function with defers. Records of other frames that were pushed from a
// range-over-func body (rangefunc) are skipped; any other record of
// another frame ends the search, since it belongs to a caller.
func deferreturn(fr *_deferframe) {
	gp := getg()
	prev := &gp._defer
	for {
		d := *prev
		if d == nil {
			return
		}
		if d.frame != fr {
			if !d.rangefunc {
				return
			}
			prev = &d.link
			continue
		}
		*prev = d.link
		fn := d.fn
		d.fn = nil
		fn()
		// The deferred call may have pushed and popped records; restart
		// from the top so the chain is read fresh.
		prev = &gp._defer
	}
}

// getcallerfp returns the frame address of its caller (the compiler lowers
// it to llvm.frameaddress(0) in the calling function).
func getcallerfp() uintptr

// parentfp returns the frame address of the caller of the frame at fp,
// following the frame record chain ([fp] holds the caller's frame pointer
// on arm64 and x86_64).
func parentfp(fp uintptr) uintptr {
	if fp == 0 {
		return 0
	}
	return *(*uintptr)(unsafe.Pointer(fp))
}

// gopanic implements panic(v).
func gopanic(v any) {
	if v == nil {
		v = new(PanicNilError)
	}
	gp := getg()
	var p _panic
	p.arg = v
	p.link = gp._panic
	gp._panic = &p
	runDefersForPanic(gp, &p)
	fatalpanic(gp._panic, gp.goid)
}

// runDefersForPanic runs deferred calls for p until one recovers or the
// chain is empty. On recovery it does not return: it resumes the frame that
// owns the recovering defer.
//
//go:noinline
func runDefersForPanic(gp *g, p *_panic) {
	p.deferfp = getcallerfp()
	for {
		d := gp._defer
		if d == nil {
			return
		}
		gp._defer = d.link
		fn := d.fn
		d.fn = nil
		p.wrapped = d.wrapped
		fn()
		if p.recovered {
			recovery(gp, p, d.frame)
		}
	}
}

// recovery resumes execution in the frame that owns the recovering defer.
// Panics whose gopanic frames are below that frame on the stack are gone.
func recovery(gp *g, p *_panic, fr *_deferframe) {
	q := gp._panic
	for q != nil && uintptr(unsafe.Pointer(q)) < uintptr(unsafe.Pointer(fr)) {
		q = q.link
	}
	gp._panic = q
	rt_longjmp(unsafe.Pointer(&fr.jmpbuf), 1)
}

// gorecover implements recover(). fp is the frame address of the function
// that called recover. It returns nil unless a panic is in progress and the
// caller was called directly by a deferred call of that panic.
func gorecover(fp uintptr) any {
	gp := getg()
	p := gp._panic
	if p == nil || p.goexit || p.recovered {
		return nil
	}
	if fp != 0 {
		// The caller of recover must have been called by the code that
		// runs deferred calls, either directly or (wrapped) through
		// exactly one compiler generated thunk.
		caller := parentfp(fp)
		if p.wrapped {
			caller = parentfp(caller)
		}
		if caller != p.deferfp {
			return nil
		}
	}
	p.recovered = true
	return p.arg
}

// goexit implements runtime.Goexit: run all deferred calls of the goroutine,
// then end it.
func goexit() {
	gp := getg()
	var p _panic
	p.goexit = true
	p.link = gp._panic
	gp._panic = &p
	for {
		d := gp._defer
		if d == nil {
			break
		}
		gp._defer = d.link
		fn := d.fn
		d.fn = nil
		fn()
	}
	gp._panic = p.link
	rt_goexit()
}

// Goexit terminates the goroutine that calls it after running its deferred
// calls.
func Goexit() {
	goexit()
}

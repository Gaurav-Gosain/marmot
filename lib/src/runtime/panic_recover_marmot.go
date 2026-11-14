//go:build !wasm

package runtime

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
		p.wrapfp = 0 // a wrapper that panicked never ran recoverWrapExit
		fn()
		if p.recovered {
			recovery(gp, p, d.frame)
		}
	}
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
		// exactly one compiler generated thunk, or by a compiler wrapper
		// that recoverWrapEnter marked.
		if p.wrapfp != 0 && parentfp(fp) == p.wrapfp {
			p.recovered = true
			return p.arg
		}
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

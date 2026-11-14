package runtime

import "unsafe"

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
		// The recover token: the code generator's wasmRecoverToken.
		wasmDeferCallee = uintptr(**(**unsafe.Pointer)(unsafe.Pointer(&fn)))
		fn()
		if p.recovered {
			recovery(gp, p, d.frame)
		}
	}
}

// gorecover implements recover() on wasm, which keeps no frame records:
// fp is 1 when the function that called recover was called directly by a
// deferred call of the panic (the code generator's recover token), else 2.
func gorecover(fp uintptr) any {
	gp := getg()
	p := gp._panic
	if p == nil || p.goexit || p.recovered || fp != 1 {
		return nil
	}
	p.recovered = true
	return p.arg
}

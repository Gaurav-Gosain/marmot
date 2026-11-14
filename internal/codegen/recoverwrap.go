package codegen

import (
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Recover through compiler wrappers (gc treats wrappers as transparent for
// recover): f := t.M; defer f() runs the bound method wrapper, which calls
// M; defer r.M() with a value receiver runs the $ifn wrapper. A wrapper
// whose target calls recover registers its frame with the runtime around
// the forwarded call (recoverWrapEnter/Exit, lib/src/runtime), so that
// gorecover in the target accepts the wrapper as the deferring frame.

// isRecoverWrapper reports whether fn is a synthetic wrapper that forwards
// to a function calling recover.
func (p *Program) isRecoverWrapper(fn *ssa.Function) bool {
	if !isWrapper(fn) || len(fn.Blocks) == 0 || p.runtimeFunc("recoverWrapEnter") == nil {
		return false
	}
	p.escMu.Lock()
	r, ok := p.recWrap[fn]
	p.escMu.Unlock()
	if ok {
		return r
	}
	r = wrapsRecover(fn, map[*ssa.Function]bool{})
	p.escMu.Lock()
	if p.recWrap == nil {
		p.recWrap = map[*ssa.Function]bool{}
	}
	p.recWrap[fn] = r
	p.escMu.Unlock()
	return r
}

// mayRecover reports whether calling fn can run recover in a frame that
// recover would accept: fn calls it, or fn is a wrapper forwarding to such
// a function.
func mayRecover(fn *ssa.Function) bool {
	return callsRecover(fn) || wrapsRecover(fn, map[*ssa.Function]bool{})
}

func wrapsRecover(fn *ssa.Function, seen map[*ssa.Function]bool) bool {
	if seen[fn] || !isWrapper(fn) || len(fn.Blocks) == 0 {
		return false
	}
	seen[fn] = true
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			c, ok := instr.(ssa.CallInstruction)
			if !ok {
				continue
			}
			if callee := c.Common().StaticCallee(); callee != nil && (callsRecover(callee) || wrapsRecover(callee, seen)) {
				return true
			}
		}
	}
	return false
}

// recoverWrapEnter emits the registration at the wrapper's entry and
// returns the saved value for recoverWrapExit.
func (f *funcGen) recoverWrapEnter() string {
	return f.rtcall("recoverWrapEnter", f.frameAddr())
}

// isWrapper reports whether fn is a compiler wrapper that forwards to the
// function it wraps (go/ssa: method and interface wrappers, bound method
// wrappers, method expression thunks, instantiation wrappers). Other
// synthetic functions are real calls: a generic instance ("instance of")
// or the package initializer calling a function that runs recover is not
// a frame recover sees through (gc: func G[T any]() { helper() }, defer
// G[int](): recover in helper returns nil).
func isWrapper(fn *ssa.Function) bool {
	s := fn.Synthetic
	for _, k := range []string{"wrapper for ", "bound method wrapper for ", "thunk for ", "instantiation wrapper of "} {
		if strings.HasPrefix(s, k) {
			return true
		}
	}
	return false
}

package codegen

import (
	"github.com/Gaurav-Gosain/marmot/internal/mlir"
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
	if fn.Synthetic == "" || len(fn.Blocks) == 0 || p.runtimeFunc("recoverWrapEnter") == nil {
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
	if seen[fn] || fn.Synthetic == "" || len(fn.Blocks) == 0 {
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
	fp := f.op("llvm.call_intrinsic \"llvm.frameaddress.p0\"(%s) : (i32) -> !llvm.ptr", f.constInt(0, mlir.I32))
	return f.rtcall("recoverWrapEnter", f.op("llvm.ptrtoint %s : !llvm.ptr to i64", fp))
}

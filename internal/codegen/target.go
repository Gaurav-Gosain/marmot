package codegen

import (
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"github.com/Gaurav-Gosain/marmot/internal/target"
	"golang.org/x/tools/go/ssa"
)

// sizes is the target's layout (target.Sizes), also for a Program made
// without NewProgram (tests).
func (p *Program) sizes() types.Sizes {
	if p.opts.Sizes == nil {
		return target.Sizes(p.opts.GOARCH)
	}
	return p.opts.Sizes
}

// ptrSize is the size of a pointer on the target (4 on wasm, else 8).
func (p *Program) ptrSize() int64 { return target.PtrSize(p.opts.GOARCH) }

// itabFun is the byte offset of method slot idx in an itab: after inter,
// typ (pointers), hash and flags (uint32 each), as runtime.itab.
func (p *Program) itabFun(idx int) int64 { return 2*p.ptrSize() + 8 + p.ptrSize()*int64(idx) }

// ptrAlign is the alignment attribute of a pointer load or store.
func (f *funcGen) ptrAlign() string {
	if f.m.p.ptrSize() == 4 {
		return "{alignment = 4 : i64}"
	}
	return "{alignment = 8 : i64}"
}

// frameAddr is the frame address of the current function as an i64, for
// the runtime's frame record walks (recover's caller check, tracebacks,
// runtime.Callers). wasm keeps no frame records in memory: it is 0 there,
// which the runtime reads as "no frame chain" (recover then accepts any
// call from a deferred function, and tracebacks and Callers are empty).
func (f *funcGen) frameAddr() string {
	if f.m.p.opts.GOARCH == "wasm" {
		return f.constInt(0, mlir.I64)
	}
	fp := f.markAddr(f.op("llvm.call_intrinsic \"llvm.frameaddress.p0\"(%s) : (i32) -> !llvm.ptr", f.constInt(0, mlir.I32)))
	return f.op("llvm.ptrtoint %s : !llvm.ptr to i64", fp)
}

// recover on wasm. gc's recover stops a panic only when called by the
// function a deferred call invoked; native targets check that through the
// frame records, which wasm does not keep. There, whoever invokes a deferred
// call (the runtime's panic loop, a defer thunk, an open defer runner) stores
// the code address of the function it calls in runtime.wasmDeferCallee (one
// thread: nothing runs between the store and the callee's entry). A function
// that calls recover takes the value at entry and clears it: it was called
// directly when the value is its own address or that of its func value
// wrapper. A function in between (a helper the deferred call calls) leaves
// the value naming another function, and a recursive call finds it cleared.

// wasmRecoverToken takes runtime.wasmDeferCallee at entry: 1 when it names
// this function (sym), else 2, the argument gorecover gets on wasm.
func (f *funcGen) wasmRecoverToken(sym string) string {
	g := f.m.p.wasmDeferCalleeVar()
	if g == nil {
		return f.constInt(2, mlir.I64)
	}
	addr := f.addressOf(f.m.globalRef(g))
	tok := f.op("llvm.load %s {alignment = 8 : i64} : %s -> i64", addr, f.pty(addr))
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : i64, %s", f.constInt(0, mlir.I64), addr, f.pty(addr))
	direct := f.names(tok, sym, true)
	return f.op("llvm.select %s, %s, %s : i1, i64", direct, f.constInt(1, mlir.I64), f.constInt(2, mlir.I64))
}

// names is i1: tok (an i64 code address) is sym or, for a function without
// free variables or receiver, its func value wrapper sym$fv (defer f() of a
// plain function calls that).
func (f *funcGen) names(tok, sym string, fv bool) string {
	self := f.op("llvm.ptrtoint %s : !llvm.ptr to i64", f.addressOf(sym))
	direct := f.op("llvm.icmp \"eq\" %s, %s : i64", tok, self)
	if fn := f.fn; fv && len(fn.FreeVars) == 0 && fn.Signature.Recv() == nil {
		f.m.funcValueSym(fn)
		w := f.op("llvm.ptrtoint %s : !llvm.ptr to i64", f.addressOf(sym+"$fv"))
		direct = f.op("llvm.or %s, %s : i1", direct, f.op("llvm.icmp \"eq\" %s, %s : i64", tok, w))
	}
	return direct
}

// wasmClaim is i1 at the entry of a compiler wrapper (a method or bound
// method wrapper, a method expression thunk): true when the token names the
// wrapper, so the deferred call invoked it and its call of the wrapped
// function passes the token on (passDeferCallee).
func (f *funcGen) wasmClaim(sym string) string {
	g := f.m.p.wasmDeferCalleeVar()
	if g == nil {
		return ""
	}
	addr := f.addressOf(f.m.globalRef(g))
	tok := f.op("llvm.load %s {alignment = 8 : i64} : %s -> i64", addr, f.pty(addr))
	return f.names(tok, sym, !f.deferThunk)
}

// forwardsToRecover reports whether fn is a compiler wrapper whose call may
// reach a function that calls recover: the token must go through it.
func forwardsToRecover(fn *ssa.Function) bool {
	if fn.Synthetic == "" {
		return false
	}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			c, ok := instr.(ssa.CallInstruction)
			if !ok {
				continue
			}
			if c.Common().IsInvoke() {
				return true
			}
			callee := c.Common().StaticCallee()
			if callee == nil || callsRecover(callee) || callee.Synthetic != "" {
				return true
			}
		}
	}
	return false
}

// passDeferCallee records code, the function about to be called, as the
// deferred call's callee: always in a defer thunk or open defer runner, and
// in a compiler wrapper only when the deferred call invoked the wrapper.
func (f *funcGen) passDeferCallee(code string) {
	if f.m.p.opts.GOARCH != "wasm" {
		return
	}
	claim := f.wrapClaim
	if f.deferThunk && f.selfSym != "" {
		// At each call (an open defer runner has one per branch): invoked
		// by the deferred call when the token names the thunk.
		claim = f.wasmClaim(f.selfSym)
	}
	switch {
	case claim != "":
		g := f.m.p.wasmDeferCalleeVar()
		addr := f.addressOf(f.m.globalRef(g))
		old := f.op("llvm.load %s {alignment = 8 : i64} : %s -> i64", addr, f.pty(addr))
		v := f.op("llvm.ptrtoint %s : !llvm.ptr to i64", code)
		// Not invoked by the deferred call (a normal return running its
		// defers, a go statement): the token is cleared, so a later recover
		// cannot take it.
		if f.deferThunk {
			old = f.constInt(0, mlir.I64)
		}
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : i64, %s", f.op("llvm.select %s, %s, %s : i1, i64", claim, v, old), addr, f.pty(addr))
	}
}

// setDeferCallee stores code, the function a defer thunk is about to call,
// in runtime.wasmDeferCallee.
func (f *funcGen) setDeferCallee(code string) {
	g := f.m.p.wasmDeferCalleeVar()
	if g == nil {
		return
	}
	addr := f.addressOf(f.m.globalRef(g))
	v := f.op("llvm.ptrtoint %s : !llvm.ptr to i64", code)
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : i64, %s", v, addr, f.pty(addr))
}

// wasmDeferCalleeVar is runtime.wasmDeferCallee (wasm only), or nil.
func (p *Program) wasmDeferCalleeVar() *ssa.Global {
	if p.runtime == nil {
		return nil
	}
	g, _ := p.runtime.Members["wasmDeferCallee"].(*ssa.Global)
	return g
}

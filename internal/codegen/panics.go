package codegen

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// May-panic analysis. An open-coded defer frame needs its _defer record only
// so a panic (or Goexit) raised while calls are pending can run them. When
// nothing that runs while a defer is pending can panic or call Goexit, the
// record is dropped (docs/frontend-passes.md, "Open-coded defers").
//
// Context: a set of values known to be non-nil (allocations, globals,
// field and element addresses, and parameters the caller passes such
// values for). Summaries are memoized per (function, non-nil parameter
// mask); recursion and unknown code count as may-panic.

type panicKey struct {
	fn   *ssa.Function
	mask uint64
}

// mayPanicCall reports whether calling fn with args (non-nil where known)
// may panic.
func (p *Program) mayPanicCall(fn *ssa.Function, args []ssa.Value, nonnil func(ssa.Value) bool) bool {
	if len(fn.Blocks) == 0 || fn.Recover != nil || len(fn.Params) > 64 {
		return true
	}
	var mask uint64
	for i, a := range args {
		if i < 64 && nonnil(a) {
			mask |= 1 << i
		}
	}
	k := panicKey{fn, mask}
	p.escMu.Lock()
	if p.panicMemo == nil {
		p.panicMemo = map[panicKey]int{}
	}
	switch p.panicMemo[k] {
	case 1:
		p.escMu.Unlock()
		return false
	case 2, 3: // may panic, or in progress (recursion)
		p.escMu.Unlock()
		return true
	}
	p.panicMemo[k] = 3
	p.escMu.Unlock()
	known := map[ssa.Value]bool{}
	for i, prm := range fn.Params {
		if i < 64 && mask&(1<<i) != 0 {
			known[prm] = true
		}
	}
	may := false
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if p.instrMayPanic(instr, known) {
				may = true
				break
			}
		}
		if may {
			break
		}
	}
	p.escMu.Lock()
	if may {
		p.panicMemo[k] = 2
	} else {
		p.panicMemo[k] = 1
	}
	p.escMu.Unlock()
	return may
}

// nonNil reports whether v is a pointer that cannot be nil.
func nonNil(v ssa.Value, known map[ssa.Value]bool) bool {
	if known[v] {
		return true
	}
	switch x := v.(type) {
	case *ssa.Alloc, *ssa.Global, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.MakeClosure, *ssa.Function:
		return true
	case *ssa.ChangeType:
		return nonNil(x.X, known)
	}
	return false
}

// instrMayPanic reports whether instr may panic or call Goexit.
func (p *Program) instrMayPanic(instr ssa.Instruction, known map[ssa.Value]bool) bool {
	nn := func(v ssa.Value) bool { return nonNil(v, known) }
	switch x := instr.(type) {
	case *ssa.DebugRef, *ssa.Jump, *ssa.If, *ssa.Return, *ssa.RunDefers, *ssa.Phi,
		*ssa.Alloc, *ssa.MakeClosure, *ssa.Extract, *ssa.ChangeType, *ssa.MakeInterface,
		*ssa.Field, *ssa.MakeMap, *ssa.ChangeInterface, *ssa.Defer:
		return false
	case *ssa.Convert:
		return false
	case *ssa.UnOp:
		switch x.Op {
		case token.MUL:
			return !nn(x.X)
		case token.ARROW:
			return true
		}
		return false
	case *ssa.Store:
		return !nn(x.Addr)
	case *ssa.FieldAddr:
		return !nn(x.X)
	case *ssa.BinOp:
		switch x.Op {
		case token.QUO, token.REM:
			if !isInteger(x.Type()) {
				return false
			}
			c, ok := x.Y.(*ssa.Const)
			return !ok || c.Value == nil || constant.Sign(c.Value) == 0
		case token.SHL, token.SHR:
			if _, ok := x.Y.(*ssa.Const); ok {
				return false
			}
			return isSigned(x.Y.Type())
		case token.EQL, token.NEQ:
			// Comparing interfaces holding uncomparable types panics.
			return types.IsInterface(x.X.Type())
		}
		return false
	case *ssa.IndexAddr:
		if pt, ok := x.X.Type().Underlying().(*types.Pointer); ok {
			if !nn(x.X) {
				return true
			}
			if c, ok := x.Index.(*ssa.Const); ok && c.Value != nil {
				n := pt.Elem().Underlying().(*types.Array).Len()
				i, exact := constant.Int64Val(c.Value)
				return !exact || i < 0 || i >= n
			}
		}
		return true
	case *ssa.Call:
		c := &x.Call
		if c.IsInvoke() {
			return true
		}
		if bi, ok := c.Value.(*ssa.Builtin); ok {
			switch bi.Name() {
			case "len", "cap", "print", "println", "append", "copy", "min", "max", "real", "imag", "complex":
				return false
			}
			return true
		}
		fn := c.StaticCallee()
		if fn == nil {
			return true
		}
		return p.mayPanicCall(fn, c.Args, nn)
	}
	return true
}

// deferRecordNeeded reports whether an open-coded defer frame needs its
// _defer record: something may panic while a deferred call is pending.
func (p *Program) deferRecordNeeded(fn *ssa.Function, od *openDefers) bool {
	known := map[ssa.Value]bool{}
	// Any instruction of the function may run while a defer is pending
	// (conservative: also those before the first defer).
	for _, b := range fn.Blocks {
		if b == fn.Recover {
			continue
		}
		for _, instr := range b.Instrs {
			if _, ok := instr.(*ssa.Defer); ok {
				continue
			}
			if p.instrMayPanic(instr, known) {
				return true
			}
		}
	}
	// The deferred calls themselves.
	for _, d := range od.defers {
		c := &d.Call
		switch v := c.Value.(type) {
		case *ssa.Builtin:
			return true
		case *ssa.Function:
			if p.mayPanicCall(v, c.Args, func(a ssa.Value) bool { return nonNil(a, known) }) {
				return true
			}
		case *ssa.MakeClosure:
			fn, ok := v.Fn.(*ssa.Function)
			if !ok || len(c.Args) > 0 {
				return true
			}
			// Free variables bound to allocations are non-nil.
			k2 := map[ssa.Value]bool{}
			for i, bnd := range v.Bindings {
				if nonNil(bnd, known) {
					k2[fn.FreeVars[i]] = true
				}
			}
			for _, b := range fn.Blocks {
				for _, instr := range b.Instrs {
					if p.instrMayPanic(instr, k2) {
						return true
					}
				}
			}
		default:
			return true
		}
	}
	return false
}

var _ = fmt.Sprint

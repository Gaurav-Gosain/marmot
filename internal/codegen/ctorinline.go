package codegen

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Closure constructors built in the caller. A function whose whole body
// makes a closure over its parameters and returns it (slices.Backward,
// slices.All, maps.Keys: iterator constructors) allocates that closure on
// the heap, because it is returned. When the call's result does not escape
// the caller (it is only called: range over the iterator), the caller
// builds the closure and the variables it captures in its own frame and
// does not call the constructor. gc gets there by inlining. Escape
// summaries switch (MARMOT_ESCAPE_SUMMARY).

// closureCtor is a constructor's shape: the closure it returns and, per
// binding, the parameter whose value the binding holds.
type closureCtor struct {
	mc    *ssa.MakeClosure
	param []int // per binding: index into the constructor's parameters
	alloc []bool
}

// closureCtorOf returns the shape of fn when fn is a closure constructor:
// one block of allocations each stored once from a parameter, the
// MakeClosure over them (or over parameters), type changes of its result
// and the return; else nil.
func (p *Program) closureCtorOf(fn *ssa.Function) *closureCtor {
	if fn == nil || len(fn.Blocks) != 1 || fn.Recover != nil || len(fn.FreeVars) > 0 {
		return nil
	}
	b := fn.Blocks[0]
	stored := map[*ssa.Alloc]int{} // alloc -> parameter index
	var mc *ssa.MakeClosure
	var ret *ssa.Return
	for _, instr := range b.Instrs {
		switch x := instr.(type) {
		case *ssa.DebugRef:
		case *ssa.Alloc:
			stored[x] = -1
		case *ssa.Store:
			a, ok := x.Addr.(*ssa.Alloc)
			if !ok || stored[a] != -1 {
				return nil
			}
			k := paramIndex(fn, x.Val)
			if k < 0 {
				return nil
			}
			stored[a] = k
		case *ssa.MakeClosure:
			if mc != nil {
				return nil
			}
			mc = x
		case *ssa.ChangeType:
		case *ssa.Return:
			ret = x
		default:
			return nil
		}
	}
	if mc == nil || ret == nil || len(ret.Results) != 1 {
		return nil
	}
	res := ret.Results[0]
	for {
		ct, ok := res.(*ssa.ChangeType)
		if !ok {
			break
		}
		res = ct.X
	}
	if res != ssa.Value(mc) {
		return nil
	}
	if _, ok := mc.Fn.(*ssa.Function); !ok {
		return nil
	}
	c := &closureCtor{mc: mc}
	for _, bnd := range mc.Bindings {
		switch x := bnd.(type) {
		case *ssa.Alloc:
			k, ok := stored[x]
			if !ok || k < 0 {
				return nil
			}
			// Only the store and the closure use the variable.
			for _, r := range *x.Referrers() {
				switch r.(type) {
				case *ssa.Store, *ssa.MakeClosure, *ssa.DebugRef:
				default:
					return nil
				}
			}
			c.param = append(c.param, k)
			c.alloc = append(c.alloc, true)
		default:
			k := paramIndex(fn, bnd)
			if k < 0 {
				return nil
			}
			c.param = append(c.param, k)
			c.alloc = append(c.alloc, false)
		}
	}
	return c
}

func paramIndex(fn *ssa.Function, v ssa.Value) int {
	for i, p := range fn.Params {
		if p == v {
			return i
		}
	}
	return -1
}

// ctorCall returns the constructor shape of call when the caller may build
// its closure in place: summaries on, a static callee the analysis of the
// caller may look into.
func (p *Program) ctorCall(call *ssa.Call) *closureCtor {
	if !optEscSummary || call.Call.IsInvoke() {
		return nil
	}
	fn := p.calleeFor(call.Parent(), &call.Call)
	if fn == nil || len(call.Call.Args) != len(fn.Params) {
		return nil
	}
	return p.closureCtorOf(fn)
}

// inlineCtor builds the closure of constructor call v in the frame and
// returns it.
func (f *funcGen) inlineCtor(v *ssa.Call, c *closureCtor) string {
	g := c.mc.Fn.(*ssa.Function)
	sym := f.m.funcRef(g)
	byv := f.m.p.byValueFreeVars(g)
	btypes := make([]types.Type, len(c.mc.Bindings))
	bvals := make([]string, len(c.mc.Bindings))
	for i, bnd := range c.mc.Bindings {
		arg := f.value(v.Call.Args[c.param[i]])
		if !c.alloc[i] {
			btypes[i], bvals[i] = bnd.Type(), arg
			continue
		}
		et := bnd.Type().(*types.Pointer).Elem()
		if byv[i] {
			btypes[i], bvals[i] = et, arg
			continue
		}
		slot := f.alloca(f.tm.memType(et), f.m.p.opts.Sizes.Alignof(et))
		f.store(arg, slot, et)
		btypes[i], bvals[i] = bnd.Type(), slot
	}
	fields := []*types.Var{types.NewField(token.NoPos, nil, "F", types.Typ[types.UnsafePointer], false)}
	for i := range c.mc.Bindings {
		fields = append(fields, types.NewField(token.NoPos, nil, fmt.Sprintf("X%d", i), btypes[i], false))
	}
	st := types.NewStruct(fields, nil)
	f.reportAlloc(v, "closure", st, "stack")
	mt := f.tm.memType(st)
	obj := f.alloca(mt, 8)
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", f.addressOf(sym), obj)
	for i := range c.mc.Bindings {
		f.store(bvals[i], f.gep(obj, mt, 0, i+1), btypes[i])
	}
	return obj
}

package codegen

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Single-type interface receivers (MARMOT_DEVIRT1, a lifetime switch).
// When the dynamic type of an interface value is provably one concrete
// type T (the value is made from a T, or returned by a function that
// always returns a T or nil), an invoke on it can only reach T's method.
// Escape analysis and the read-only argument rule (roconv.go) then use that
// method's summaries instead of treating the invoke as unknown code:
// h := fnv.New64a(); h.Write([]byte(msg)) keeps []byte(msg) off the heap.
// The call itself still goes through the itab: the analysis needs only
// that the callee is known, not that the call is direct.
//
// Design and review: .work/experiments/frontend-design-roconv.md (Part B,
// rules 6 to 9).

// dynType returns the one concrete type the dynamic type of interface
// value v can have (nil interfaces aside), or nil when unknown.
func (p *Program) dynType(v ssa.Value) types.Type {
	if !optDevirt1 {
		return nil
	}
	t, _ := p.dynTypeIn(v, map[ssa.Value]bool{}, map[*ssa.Function]bool{})
	return t
}

// dynTypeIn returns v's dynamic type; nilOnly is true when v can only be a
// nil interface (a constant), which merges with any type.
func (p *Program) dynTypeIn(v ssa.Value, seen map[ssa.Value]bool, calls map[*ssa.Function]bool) (t types.Type, nilOnly bool) {
	if seen[v] {
		return nil, true // a phi cycle adds no type
	}
	seen[v] = true
	switch x := v.(type) {
	case *ssa.MakeInterface:
		if types.IsInterface(x.X.Type()) {
			return nil, false
		}
		return x.X.Type(), false
	case *ssa.ChangeInterface:
		return p.dynTypeIn(x.X, seen, calls)
	case *ssa.Const:
		if x.Value == nil {
			return nil, true
		}
		return nil, false
	case *ssa.Phi:
		var out types.Type
		for _, e := range x.Edges {
			t, nilOnly := p.dynTypeIn(e, seen, calls)
			if nilOnly {
				continue
			}
			if t == nil || (out != nil && !types.Identical(out, t)) {
				return nil, false
			}
			out = t
		}
		return out, out == nil
	case *ssa.Call:
		return p.returnedType(x, 0, calls)
	case *ssa.Extract:
		if c, ok := x.Tuple.(*ssa.Call); ok {
			return p.returnedType(c, x.Index, calls)
		}
	}
	// Parameters, loads, type parameters, recover(), map and channel
	// reads: unknown.
	return nil, false
}

// returnedType returns the dynamic type of result k of static call c, when
// every return of the callee gives the same one (or nil).
func (p *Program) returnedType(c *ssa.Call, k int, calls map[*ssa.Function]bool) (types.Type, bool) {
	fn := c.Call.StaticCallee()
	if fn == nil || c.Call.IsInvoke() || len(fn.Blocks) == 0 || !p.sameUnit(c.Parent(), fn) || !p.bodyLinks(fn) || calls[fn] {
		return nil, false
	}
	if fn.TypeParams().Len() > 0 && len(fn.TypeArgs()) == 0 {
		return nil, false
	}
	calls[fn] = true
	defer delete(calls, fn)
	var out types.Type
	for _, b := range fn.Blocks {
		ret, ok := b.Instrs[len(b.Instrs)-1].(*ssa.Return)
		if !ok {
			continue
		}
		if k >= len(ret.Results) {
			return nil, false
		}
		t, nilOnly := p.dynTypeIn(ret.Results[k], map[ssa.Value]bool{}, calls)
		if nilOnly {
			continue
		}
		if t == nil || (out != nil && !types.Identical(out, t)) {
			return nil, false
		}
		out = t
	}
	return out, out == nil
}

// invokeCallee returns the method an invoke call reaches when its
// receiver has one dynamic type (dynType), else nil. For a value method
// called on a pointer, the method set gives the wrapper, whose summaries
// cover the copy of the receiver.
func (p *Program) invokeCallee(from *ssa.Function, c *ssa.CallCommon) *ssa.Function {
	if !c.IsInvoke() || !optDevirt1 {
		return nil
	}
	if from == nil {
		return nil
	}
	t := p.dynType(c.Value)
	if t == nil {
		return nil
	}
	sel := p.prog.MethodSets.MethodSet(t).Lookup(c.Method.Pkg(), c.Method.Name())
	if sel == nil {
		return nil
	}
	fn := p.prog.MethodValue(sel)
	if fn == nil || !p.sameUnit(from, fn) || !p.bodyLinks(fn) {
		return nil
	}
	return fn
}

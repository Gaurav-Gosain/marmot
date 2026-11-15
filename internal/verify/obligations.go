// Package verify proves that functions cannot panic, or finds inputs that
// make them panic. It runs on x/tools SSA as a go/analysis analyzer, one
// package at a time.
//
// Every SSA instruction that can panic gives one obligation. Engine A, an
// abstract interpreter over intervals and difference bounds (a pentagon
// domain extended with constant offsets), proves what it can; the rest is
// classified as latent (a function of the parameters), unknown (with a
// reason) or, when a guard in the code tests the same quantity with a
// weaker bound, a manifest guard mismatch.
package verify

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Kind is a kind of run-time panic.
type Kind string

// The panic kinds of the design (section 4.2).
const (
	KIndex   Kind = "index"
	KSlice   Kind = "slice"
	KNil     Kind = "nil"
	KIface   Kind = "iface"
	KDivide  Kind = "divide"
	KAssert  Kind = "assert"
	KCompare Kind = "compare"
	KMap     Kind = "map"
	KConvert Kind = "convert"
	KShift   Kind = "shift"
	KMake    Kind = "make"
	KAppend  Kind = "append"
	KUnsafe  Kind = "unsafe"
	KChan    Kind = "chan"
	KPanic   Kind = "panic"
)

// kindTag is the short tag of a kind in obligation keys.
var kindTag = map[Kind]string{
	KIndex: "idx", KSlice: "slice", KNil: "nil", KIface: "iface", KDivide: "div",
	KAssert: "assert", KCompare: "cmp", KMap: "map", KConvert: "conv", KShift: "shift",
	KMake: "make", KAppend: "append", KUnsafe: "unsafe", KChan: "chan", KPanic: "panic",
}

// An Obligation is one possible panic at one instruction.
type Obligation struct {
	Kind  Kind
	Instr ssa.Instruction
	Pos   token.Pos // the instruction's anchor position
	// Key is position independent: function, kind and ordinal.
	Key string

	// Operands, by kind:
	//   index:   X collection, I index (Len of X, or N for arrays)
	//   slice:   X collection, I low, Hi high, Max max (nil when absent)
	//   nil:     X pointer, map, func or channel value
	//   iface:   X interface value
	//   divide:  X divisor
	//   shift:   X shift count
	//   make:    I length, Hi capacity (nil when absent)
	//   convert: X slice, N array length
	//   unsafe:  I length
	//   assert, compare, panic, append: X the value
	X, I, Hi, Max ssa.Value
	N             int64 // static length for arrays, -1 otherwise
	Receiver      bool  // nil obligation on the method receiver
}

func (o *Obligation) String() string { return o.Key }

// obligations lists the obligations of fn in instruction order.
func obligations(fn *ssa.Function) []*Obligation {
	var out []*Obligation
	count := map[Kind]int{}
	add := func(k Kind, in ssa.Instruction, o Obligation) {
		o.Kind, o.Instr, o.Pos = k, in, in.Pos()
		if o.N == 0 {
			o.N = -1
		}
		count[k]++
		o.Key = fmt.Sprintf("%s#%s%d", fn.String(), kindTag[k], count[k])
		out = append(out, &o)
	}
	var recv ssa.Value
	if fn.Signature.Recv() != nil && len(fn.Params) > 0 {
		recv = fn.Params[0]
	}
	nilObl := func(in ssa.Instruction, x ssa.Value) {
		if !mayBeNilValue(x) {
			return
		}
		add(KNil, in, Obligation{X: x, Receiver: x == recv})
	}
	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			switch in := in.(type) {
			case *ssa.IndexAddr:
				switch t := coreType(in.X.Type()).(type) {
				case *types.Slice:
					add(KIndex, in, Obligation{X: in.X, I: in.Index})
				case *types.Pointer:
					nilObl(in, in.X)
					if a, ok := coreType(t.Elem()).(*types.Array); ok {
						add(KIndex, in, Obligation{X: in.X, I: in.Index, N: a.Len()})
					}
				}
			case *ssa.Index:
				switch t := coreType(in.X.Type()).(type) {
				case *types.Array:
					add(KIndex, in, Obligation{X: in.X, I: in.Index, N: t.Len()})
				case *types.Basic:
					if t.Info()&types.IsString != 0 {
						add(KIndex, in, Obligation{X: in.X, I: in.Index})
					}
				}
			case *ssa.Lookup:
				if t, ok := coreType(in.X.Type()).(*types.Basic); ok && t.Info()&types.IsString != 0 {
					add(KIndex, in, Obligation{X: in.X, I: in.Index})
				} else if m, ok := coreType(in.X.Type()).(*types.Map); ok && types.IsInterface(m.Key()) {
					add(KCompare, in, Obligation{X: in.Index})
				}
			case *ssa.Slice:
				o := Obligation{X: in.X, I: in.Low, Hi: in.High, Max: in.Max}
				if p, ok := coreType(in.X.Type()).(*types.Pointer); ok {
					nilObl(in, in.X)
					if a, ok := coreType(p.Elem()).(*types.Array); ok {
						o.N = a.Len()
					}
				}
				if in.Low != nil || in.High != nil || in.Max != nil {
					add(KSlice, in, o)
				}
			case *ssa.BinOp:
				switch in.Op {
				case token.QUO, token.REM:
					if isInteger(in.Y.Type()) && !nonZeroConst(in.Y) {
						add(KDivide, in, Obligation{X: in.Y})
					}
				case token.SHL, token.SHR:
					if isSigned(in.Y.Type()) && !nonNegConst(in.Y) {
						add(KShift, in, Obligation{X: in.Y})
					}
				case token.EQL, token.NEQ:
					if types.IsInterface(in.X.Type()) && types.IsInterface(in.Y.Type()) {
						add(KCompare, in, Obligation{X: in.X, I: in.Y})
					}
				}
			case *ssa.UnOp:
				if in.Op == token.MUL {
					nilObl(in, in.X)
				}
			case *ssa.Store:
				switch in.Addr.(type) {
				case *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Alloc, *ssa.Global:
				default:
					nilObl(in, in.Addr)
				}
			case *ssa.FieldAddr:
				nilObl(in, in.X)
			case *ssa.MapUpdate:
				if mayBeNilValue(in.Map) {
					add(KMap, in, Obligation{X: in.Map})
				}
				if m, ok := coreType(in.Map.Type()).(*types.Map); ok && types.IsInterface(m.Key()) {
					add(KCompare, in, Obligation{X: in.Key})
				}
			case *ssa.TypeAssert:
				if !in.CommaOk {
					add(KAssert, in, Obligation{X: in.X})
				}
			case *ssa.MakeSlice:
				add(KMake, in, Obligation{I: in.Len, Hi: in.Cap})
			case *ssa.MakeChan:
				if !nonNegConst(in.Size) {
					add(KMake, in, Obligation{I: in.Size})
				}
			case *ssa.SliceToArrayPointer:
				if p, ok := coreType(in.Type()).(*types.Pointer); ok {
					if a, ok := coreType(p.Elem()).(*types.Array); ok && a.Len() > 0 {
						add(KConvert, in, Obligation{X: in.X, N: a.Len()})
					}
				}
			case *ssa.Panic:
				add(KPanic, in, Obligation{X: in.X})
			case ssa.CallInstruction:
				callObligations(in, add, nilObl)
			}
		}
	}
	return out
}

func callObligations(in ssa.CallInstruction, add func(Kind, ssa.Instruction, Obligation), nilObl func(ssa.Instruction, ssa.Value)) {
	c := in.Common()
	if c.IsInvoke() {
		add(KIface, in, Obligation{X: c.Value})
		return
	}
	switch v := c.Value.(type) {
	case *ssa.Builtin:
		switch v.Name() {
		case "append":
			if len(c.Args) == 2 {
				add(KAppend, in, Obligation{X: c.Args[0], I: c.Args[1]})
			}
		case "close":
			nilObl(in, c.Args[0])
		case "Slice", "String":
			if len(c.Args) == 2 && !nonNegConst(c.Args[1]) {
				add(KUnsafe, in, Obligation{X: c.Args[0], I: c.Args[1]})
			}
		}
	case *ssa.Function, *ssa.MakeClosure:
	default:
		nilObl(in, c.Value)
	}
}

// mayBeNilValue reports whether x can be nil at all, from its kind of
// value alone.
func mayBeNilValue(x ssa.Value) bool {
	switch x := x.(type) {
	case *ssa.Alloc, *ssa.Global, *ssa.Function, *ssa.MakeClosure, *ssa.MakeMap,
		*ssa.MakeChan, *ssa.FieldAddr, *ssa.IndexAddr:
		return false
	case *ssa.Const:
		return true
	case *ssa.Builtin:
		return false
	default:
		_ = x
		return true
	}
}

// coreType is the underlying type, or for a type parameter the single
// underlying type of its type set when there is one.
func coreType(t types.Type) types.Type {
	tp, ok := types.Unalias(t).(*types.TypeParam)
	if !ok {
		return t.Underlying()
	}
	iface, ok := tp.Constraint().Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	var core types.Type
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		u, ok := iface.EmbeddedType(i).(*types.Union)
		if !ok {
			e := iface.EmbeddedType(i).Underlying()
			if _, isIface := e.(*types.Interface); isIface {
				continue
			}
			if core != nil && !types.Identical(core, e) {
				return nil
			}
			core = e
			continue
		}
		for j := 0; j < u.Len(); j++ {
			e := u.Term(j).Type().Underlying()
			if core != nil && !types.Identical(core, e) {
				return nil
			}
			core = e
		}
	}
	return core
}

// typeSetHas reports whether some type in t's type set satisfies p (t
// itself when t is not a type parameter).
func typeSetHas(t types.Type, p func(*types.Basic) bool) bool {
	tp, ok := types.Unalias(t).(*types.TypeParam)
	if !ok {
		b, ok := t.Underlying().(*types.Basic)
		return ok && p(b)
	}
	iface, ok := tp.Constraint().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	any := false
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		switch e := iface.EmbeddedType(i).(type) {
		case *types.Union:
			for j := 0; j < e.Len(); j++ {
				if b, ok := e.Term(j).Type().Underlying().(*types.Basic); ok && p(b) {
					any = true
				}
			}
		default:
			if b, ok := e.Underlying().(*types.Basic); ok && p(b) {
				any = true
			}
		}
	}
	return any
}

func isInteger(t types.Type) bool {
	return typeSetHas(t, func(b *types.Basic) bool { return b.Info()&types.IsInteger != 0 })
}

func isSigned(t types.Type) bool {
	return typeSetHas(t, func(b *types.Basic) bool {
		return b.Info()&types.IsInteger != 0 && b.Info()&types.IsUnsigned == 0
	})
}

func isUnsigned(t types.Type) bool {
	b, ok := coreType(t).(*types.Basic)
	return ok && b.Info()&types.IsUnsigned != 0
}

func nonZeroConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil {
		return false
	}
	n, exact := constInt(c)
	return exact && n != 0
}

func nonNegConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil {
		return false
	}
	n, exact := constInt(c)
	return exact && n >= 0
}

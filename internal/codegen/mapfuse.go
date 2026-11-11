package codegen

import (
	"go/token"
	"go/types"
	"os"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Map read-modify-write fusion (docs/frontend-passes.md). For
//
//	v := m[k]; ...; m[k] = w        (m[k] op= x, m[k]++)
//
// in one block, where everything between the lookup and the update is
// arithmetic that cannot panic (no calls, loads, stores, divisions or
// conversions that can fail), the lookup becomes one mapassign that
// returns the slot (inserting the zero value when k is missing, which is
// what the lookup would have read), and the update stores to that slot.
// Nothing runs between that could observe the early insertion: the only
// difference is a nil map, which now panics at the lookup instead of at
// the update, with nothing observable in between.

func (f *funcGen) findMapFusions() {
	if !optMapFuse || os.Getenv("MARMOT_NO_MAPFUSE") != "" {
		return
	}
	for _, b := range f.fn.Blocks {
		for i, instr := range b.Instrs {
			l, ok := instr.(*ssa.Lookup)
			if !ok || l.CommaOk {
				continue
			}
			if _, isMap := l.X.Type().Underlying().(*types.Map); !isMap || f.sunk[l] {
				continue
			}
			for k := i + 1; k < len(b.Instrs); k++ {
				x := b.Instrs[k]
				if len(f.sinkBefore[x]) > 0 || f.sunk[x] || f.skipInstr[x] {
					break
				}
				if u, ok := x.(*ssa.MapUpdate); ok {
					if u.Map == l.X && sameKey(u.Key, l.Index) {
						if f.fuseLookup == nil {
							f.fuseLookup = map[*ssa.Lookup]*ssa.MapUpdate{}
							f.fuseSlot = map[*ssa.MapUpdate]string{}
						}
						f.fuseLookup[l] = u
					}
					break
				}
				if !cannotPanic(x) {
					break
				}
			}
		}
	}
}

func sameKey(a, b ssa.Value) bool {
	if a == b {
		return true
	}
	ca, ok1 := a.(*ssa.Const)
	cb, ok2 := b.(*ssa.Const)
	return ok1 && ok2 && types.Identical(ca.Type(), cb.Type()) && ca.Value != nil && cb.Value != nil && ca.Value.ExactString() == cb.Value.ExactString()
}

// cannotPanic reports whether x has no side effects and cannot panic.
func cannotPanic(x ssa.Instruction) bool {
	switch y := x.(type) {
	case *ssa.DebugRef:
		return true
	case *ssa.BinOp:
		switch y.Op {
		case token.QUO, token.REM:
			b, ok := y.Type().Underlying().(*types.Basic)
			return ok && b.Info()&(types.IsFloat|types.IsComplex) != 0
		case token.SHL, token.SHR:
			b, ok := y.Y.Type().Underlying().(*types.Basic)
			return ok && b.Info()&types.IsUnsigned != 0
		}
		return true
	case *ssa.UnOp:
		return y.Op == token.SUB || y.Op == token.XOR || y.Op == token.NOT
	case *ssa.Convert:
		_, fromBasic := y.X.Type().Underlying().(*types.Basic)
		_, toBasic := y.Type().Underlying().(*types.Basic)
		return fromBasic && toBasic && !isString(y.X.Type()) && !isString(y.Type())
	case *ssa.ChangeType, *ssa.Extract, *ssa.Field:
		return true
	}
	return false
}

// fusedLookup emits the lookup half of a fused pair: the slot of m[k].
func (f *funcGen) fusedLookup(v *ssa.Lookup, u *ssa.MapUpdate) {
	mt := mapTypeOf(v.X.Type())
	m := f.value(v.X)
	desc := f.typeDesc(v.X.Type())
	var slot string
	if fk, kv := f.fastMapKey(mt.Key(), v.Index); fk != "" {
		slot = f.rtcall("mapassign_fast"+fk, append([]string{desc, m}, kv...)...)
	} else {
		key := f.spill(f.value(v.Index), mt.Key())
		slot = f.rtcall("mapassign", desc, m, key)
	}
	f.fuseSlot[u] = slot
	f.vals[v] = f.load(slot, mt.Elem())
}

var _ = mlir.Ptr

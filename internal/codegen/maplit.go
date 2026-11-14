package codegen

import (
	"fmt"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Constant map literals, like gc's maplit for large literals: the
// constant entries of a map literal become two private read-only arrays,
// keys and values, and one loop of mapassign and a value copy, instead of
// a mapassign call per entry (tuios fa.init, icons.init, html.init).
//
// The entries are the MapUpdates of a MakeMap in its block, in order; the
// constant ones before the first that is not constant (a run-time key may
// equal a constant one, and the later update wins) run as the loop, at
// the place of the last of them. Off unless MARMOT_LITCOPY=1 (litcopy.go).

// minMapLitEntries is the fewest constant entries worth a loop.
const minMapLitEntries = 8

// mapLit is a folded map literal: the loop runs at its last update.
type mapLit struct {
	keys, vals string // private arrays
	n          int64
	mt         *types.Map
}

// findConstMaps folds the constant map literals of f.fn. Stores into the
// images it consumes go into done (litcopy.go).
func (f *funcGen) findConstMaps(s *staticInit, done map[ssa.Instruction]bool) {
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			mm, ok := instr.(*ssa.MakeMap)
			if !ok {
				continue
			}
			f.constMap(s, mm, done)
		}
	}
}

func (f *funcGen) constMap(s *staticInit, mm *ssa.MakeMap, done map[ssa.Instruction]bool) {
	b := mm.Block()
	mt := mapTypeOf(mm.Type())
	var ups []*ssa.MapUpdate
	var keys, vals []cv
	for _, instr := range b.Instrs {
		u, ok := instr.(*ssa.MapUpdate)
		if !ok || u.Map != ssa.Value(mm) {
			continue
		}
		if f.skipInstr[u] || f.fuseSlot[u] != "" {
			break
		}
		k, ok := s.staticValue(u.Key)
		if !ok {
			break
		}
		v, ok := s.staticValue(u.Value)
		if !ok {
			break
		}
		ups = append(ups, u)
		keys = append(keys, k)
		vals = append(vals, v)
	}
	if len(ups) < minMapLitEntries {
		return
	}
	n := int64(len(ups))
	array := func(t types.Type, elems []cv) string {
		img := s.image(types.NewArray(t, n))
		for i := range elems {
			v := elems[i]
			img.at(i).leaf = &v
		}
		f.m.nstr++
		sym := fmt.Sprintf(".mapc.%d", f.m.nstr)
		f.m.constGlobal(sym, "private", true, s.value(img))
		return sym
	}
	ml := &mapLit{keys: array(mt.Key(), keys), vals: array(mt.Elem(), vals), n: n, mt: mt}
	for _, u := range ups[:len(ups)-1] {
		f.skip(u)
		done[u] = true
	}
	done[ups[len(ups)-1]] = true
	if f.mapLits == nil {
		f.mapLits = map[*ssa.MapUpdate]*mapLit{}
	}
	f.mapLits[ups[len(ups)-1]] = ml
}

// mapLitLoop emits the loop of a folded map literal into map m.
func (f *funcGen) mapLitLoop(m string, mapType types.Type, ml *mapLit) {
	kt, vt := f.tm.memType(ml.mt.Key()), f.tm.memType(ml.mt.Elem())
	vsize := f.m.p.opts.Sizes.Sizeof(ml.mt.Elem())
	td := f.typeDesc(mapType)
	keys, vals := f.addressOf(ml.keys), f.addressOf(ml.vals)
	n := f.constInt(ml.n, mlir.I64)
	one := f.constInt(1, mlir.I64)
	zero := f.constInt(0, mlir.I64)
	loop, done := f.newBlock(), f.newBlock()
	f.emit("llvm.br %s(%s : i64)", loop, zero)
	i := f.newVal()
	f.b.WriteString("  " + loop + "(" + i + ": i64):\n")
	slot := f.rtcall("mapassign", td, m, f.gepDyn(keys, kt, i))
	if vsize > 0 {
		f.memmove(slot, f.gepDyn(vals, vt, i), vsize)
	}
	next := f.op("llvm.add %s, %s : i64", i, one)
	more := f.op("llvm.icmp \"ult\" %s, %s : i64", next, n)
	f.emit("llvm.cond_br %s, %s(%s : i64), %s", more, loop, next, done)
	f.b.WriteString("  " + done + ":\n")
}

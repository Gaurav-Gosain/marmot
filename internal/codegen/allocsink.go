package codegen

import (
	"go/constant"

	"golang.org/x/tools/go/ssa"
)

// Allocation sinking (docs/frontend-passes.md). A heap allocation that
// cannot panic (new, make of a map with a constant or no hint, make of a
// slice or channel with constant sizes, a closure) moves from its block B
// to the nearest common dominator D of its uses when D != B and D is in no
// loop that B is not in. Paths that never use the object then never
// allocate it. Allocation has no other observable effect, and every
// operand still dominates the new position because D is dominated by B.

func (f *funcGen) sinkAllocs() {
	if !optAllocSink || f.frame != "" || f.open != nil || len(f.fn.Blocks) < 2 {
		return
	}
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			v, ok := instr.(ssa.Value)
			if !ok || !f.sinkableAlloc(instr) || f.sunk[instr] || f.skipInstr[instr] {
				continue
			}
			d := f.useDominator(v, b)
			if d == nil || d == b || inLoopWithout(d, b) {
				continue
			}
			// At the start of d (after its phis).
			target := firstNonPhi(d)
			if target == nil || f.sunk[target] || f.skipInstr[target] {
				continue
			}
			if f.sunk == nil {
				f.sunk = map[ssa.Instruction]bool{}
				f.sinkBefore = map[ssa.Instruction][]ssa.Instruction{}
			}
			f.sunk[instr] = true
			f.sinkBefore[target] = append(f.sinkBefore[target], instr)
		}
	}
}

func firstNonPhi(b *ssa.BasicBlock) ssa.Instruction {
	for _, x := range b.Instrs {
		if _, phi := x.(*ssa.Phi); !phi {
			return x
		}
	}
	return nil
}

func (f *funcGen) sinkableAlloc(instr ssa.Instruction) bool {
	switch x := instr.(type) {
	case *ssa.Alloc:
		return x.Heap && !f.stackOK[x] && f.region == ""
	case *ssa.MakeMap:
		return x.Reserve == nil || nonNegConst(x.Reserve)
	case *ssa.MakeSlice:
		if !nonNegConst(x.Len) || !nonNegConst(x.Cap) {
			return false
		}
		l, _ := constant.Int64Val(x.Len.(*ssa.Const).Value)
		c, _ := constant.Int64Val(x.Cap.(*ssa.Const).Value)
		return l <= c && c <= 1<<20
	case *ssa.MakeChan:
		return nonNegConst(x.Size)
	case *ssa.MakeClosure:
		return !f.stackOK[x]
	}
	return false
}

func nonNegConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.Int {
		return false
	}
	n, exact := constant.Int64Val(c.Value)
	return exact && n >= 0
}

// useDominator returns the nearest common dominator of the uses of v
// (a phi uses its edge value at the end of the predecessor), or nil.
func (f *funcGen) useDominator(v ssa.Value, def *ssa.BasicBlock) *ssa.BasicBlock {
	var d *ssa.BasicBlock
	add := func(b *ssa.BasicBlock) {
		if d == nil {
			d = b
			return
		}
		for !d.Dominates(b) {
			d = d.Idom()
			if d == nil {
				return
			}
		}
	}
	refs := v.Referrers()
	if refs == nil {
		return nil
	}
	n := 0
	for _, r := range *refs {
		if _, dbg := r.(*ssa.DebugRef); dbg {
			continue
		}
		n++
		if p, ok := r.(*ssa.Phi); ok {
			for i, e := range p.Edges {
				if e == v {
					add(p.Block().Preds[i])
				}
			}
			continue
		}
		add(r.Block())
	}
	if n == 0 || d == nil || !def.Dominates(d) {
		return nil
	}
	return d
}

// inLoopWithout reports whether d lies on a cycle that avoids b.
func inLoopWithout(d, b *ssa.BasicBlock) bool {
	seen := map[*ssa.BasicBlock]bool{}
	stack := append([]*ssa.BasicBlock{}, d.Succs...)
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x == d {
			return true
		}
		if x == b || seen[x] {
			continue
		}
		seen[x] = true
		stack = append(stack, x.Succs...)
	}
	return false
}

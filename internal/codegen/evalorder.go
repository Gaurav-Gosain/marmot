package codegen

import (
	"slices"
	"go/ast"
	"go/token"
	"os"

	"golang.org/x/tools/go/ssa"
)

// Evaluation order of variable reads. The spec
// leaves the order of a variable read and a function call in one
// expression unspecified; gc evaluates the calls first and reads
// variables that live in memory (address taken, captured, global, element
// or field of those) afterwards, and real code depends on it
// (`return id, parse(s, &id)`). x/tools SSA reads them in source order.
// A load whose expression belongs to the same statement as a call that
// runs between the load and the load's first use is moved to just before
// that use, together with the address computation only it uses.
//
// The mapping from loads to expressions comes from SSA debug references
// (enabled for packages outside GOROOT, see the loader).

// stmtRanges returns the source ranges that gc evaluates as one
// expression context: simple statements and the condition, tag or range
// operand of compound statements.
func stmtRanges(body ast.Node) [][2]token.Pos {
	var out [][2]token.Pos
	add := func(n ast.Node) {
		if n != nil {
			out = append(out, [2]token.Pos{n.Pos(), n.End()})
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ReturnStmt, *ast.AssignStmt, *ast.ExprStmt, *ast.IncDecStmt, *ast.SendStmt,
			*ast.GoStmt, *ast.DeferStmt, *ast.DeclStmt:
			add(x)
		case *ast.IfStmt:
			add(x.Cond)
		case *ast.ForStmt:
			if x.Cond != nil {
				add(x.Cond)
			}
		case *ast.SwitchStmt:
			if x.Tag != nil {
				add(x.Tag)
			}
		case *ast.RangeStmt:
			add(x.X)
		case *ast.FuncLit:
			return false // its own function
		}
		return true
	})
	return out
}

// complitRanges returns the source ranges of the composite literals in body.
func complitRanges(body ast.Node) [][2]token.Pos {
	var out [][2]token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			out = append(out, [2]token.Pos{x.Pos(), x.End()})
		case *ast.FuncLit:
			return false
		}
		return true
	})
	return out
}

func innermost(ranges [][2]token.Pos, p token.Pos) (token.Pos, token.Pos, bool) {
	var lo, hi token.Pos
	found := false
	for _, r := range ranges {
		if r[0] <= p && p < r[1] && (!found || r[1]-r[0] < hi-lo) {
			lo, hi, found = r[0], r[1], true
		}
	}
	return lo, hi, found
}

// computeSinks fills f.sinkBefore and f.sunk.
func (f *funcGen) computeSinks() {
	if os.Getenv("MARMOT_NO_EVALORDER") != "" || f.fn.Syntax() == nil {
		return
	}
	var body ast.Node
	switch s := f.fn.Syntax().(type) {
	case *ast.FuncDecl:
		if s.Body == nil {
			return
		}
		body = s.Body
	case *ast.FuncLit:
		body = s.Body
	default:
		return
	}
	ranges := stmtRanges(body)
	lits := complitRanges(body)
	for _, b := range f.fn.Blocks {
		exprOf := map[ssa.Value]ast.Expr{}
		pos := map[ssa.Instruction]int{}
		for i, instr := range b.Instrs {
			pos[instr] = i
			if d, ok := instr.(*ssa.DebugRef); ok {
				if _, seen := exprOf[d.X]; !seen {
					exprOf[d.X] = d.Expr
				}
			}
		}
		for i, instr := range b.Instrs {
			l, ok := instr.(*ssa.UnOp)
			if !ok || l.Op != token.MUL || f.volatile[l.X] {
				continue
			}
			e := exprOf[l]
			if e == nil {
				continue
			}
			switch ast.Unparen(e).(type) {
			case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
			default:
				continue
			}
			lo, hi, ok := innermost(ranges, e.Pos())
			if !ok {
				continue
			}
			// First use, in this block.
			use := -1
			for _, r := range *l.Referrers() {
				if _, dbg := r.(*ssa.DebugRef); dbg {
					continue
				}
				j, same := pos[r]
				if !same {
					use = -1
					break
				}
				if _, isPhi := r.(*ssa.Phi); isPhi {
					use = -1
					break
				}
				if use < 0 || j < use {
					use = j
				}
			}
			if use < 0 {
				continue
			}
			if f.skipInstr[b.Instrs[use]] || f.m.static != nil && f.m.static.skip[b.Instrs[use]] {
				continue
			}
			called, stored := false, false
			for k := i + 1; k < use; k++ {
				switch c := b.Instrs[k].(type) {
				case *ssa.Call:
					if c.Pos() >= lo && c.Pos() < hi {
						called = true
					}
				case *ssa.Store, *ssa.MapUpdate:
					stored = true
				}
			}
			target := b.Instrs[use]
			if !called {
				// A composite literal element is stored before the later
				// elements run: move the element store after the last call
				// of the statement too.
				st, ok := target.(*ssa.Store)
				if !ok || stored || !complitElem(st.Addr, b, pos) {
					continue
				}
				// Only calls of later elements of the same literal.
				llo, lhi, ok := innermost(lits, e.Pos())
				if !ok {
					continue
				}
				last := -1
				for k := use + 1; k < len(b.Instrs); k++ {
					if c, ok := b.Instrs[k].(*ssa.Call); ok && c.Pos() >= llo && c.Pos() < lhi {
						last = k
					}
				}
				if last < 0 || last+1 >= len(b.Instrs) {
					continue
				}
				addr := st.Addr.(ssa.Instruction)
				target = b.Instrs[last+1]
				if f.skipInstr[target] || f.sunk[target] {
					continue
				}
				f.sinkGroup(l, i, b, pos, target, addr, st)
				continue
			}
			if stored {
				continue
			}
			f.sinkGroup(l, i, b, pos, target)
		}
	}
}

// complitElem reports whether a is the address of an element or field of
// a composite literal allocated in block b, used only by this store.
func complitElem(a ssa.Value, b *ssa.BasicBlock, pos map[ssa.Instruction]int) bool {
	var x ssa.Value
	switch y := a.(type) {
	case *ssa.IndexAddr:
		x = y.X
	case *ssa.FieldAddr:
		x = y.X
	default:
		return false
	}
	al, ok := x.(*ssa.Alloc)
	if !ok || al.Comment != "complit" || len(*a.Referrers()) != 1 {
		return false
	}
	_, same := pos[a.(ssa.Instruction)]
	return same
}

// sinkGroup moves load l (at index i of b), the address chain only it
// uses, and then extra, to just before target.
func (f *funcGen) sinkGroup(l *ssa.UnOp, i int, b *ssa.BasicBlock, pos map[ssa.Instruction]int, target ssa.Instruction, extra ...ssa.Instruction) {
	// Move l and the address chain only it uses.
	group := []ssa.Instruction{l}
	x := l.X
	for {
		xi, ok := x.(ssa.Instruction)
		if !ok || xi.Block() != b || pos[xi] > i {
			break
		}
		refs := *x.Referrers()
		only := true
		for _, r := range refs {
			if _, dbg := r.(*ssa.DebugRef); !dbg && r != group[len(group)-1] {
				only = false
			}
		}
		if !only {
			break
		}
		next := ssa.Value(nil)
		switch y := xi.(type) {
		case *ssa.FieldAddr:
			next = y.X
		case *ssa.IndexAddr:
			next = y.X
		case *ssa.UnOp:
			if y.Op != token.MUL {
				break
			}
			next = y.X
		}
		if next == nil {
			break
		}
		group = append(group, xi)
		x = next
	}
	if os.Getenv("MARMOT_DEBUG_EVALORDER") != "" {
		println("sink", f.fn.String(), f.m.p.fset.Position(l.Pos()).String(), l.String(), "before", target.String())
	}
	if f.sunk == nil {
		f.sunk = map[ssa.Instruction]bool{}
		f.sinkBefore = map[ssa.Instruction][]ssa.Instruction{}
	}
	for _, g := range slices.Backward(group) {
		f.sunk[g] = true
		f.sinkBefore[target] = append(f.sinkBefore[target], g)
	}
	for _, e := range extra {
		f.sunk[e] = true
		f.sinkBefore[target] = append(f.sinkBefore[target], e)
	}
}

// emitSunk emits the loads sunk to just before instr (and, recursively, the
// ones sunk before those), each once.
func (f *funcGen) emitSunk(instr ssa.Instruction) {
	for _, s := range f.sinkBefore[instr] {
		if f.sunkDone[s] {
			continue
		}
		if f.sunkDone == nil {
			f.sunkDone = map[ssa.Instruction]bool{}
		}
		f.sunkDone[s] = true
		f.emitSunk(s)
		f.curPos = s.Pos()
		f.instr(s)
	}
}

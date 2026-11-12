package codegen

import (
	"go/ast"
	"go/token"
	"os"

	"golang.org/x/tools/go/ssa"
)

// Evaluation order (docs/frontend-passes.md "Evaluation order"). gc
// evaluates the calls, channel receives and type assertions of a statement
// in source order and every other operand after them: variable, field,
// element, map and pointer reads, divisions, conversions, len and cap,
// with their panics. Real code depends on it (`return id, parse(s, &id)`).
// x/tools SSA evaluates in source order, so each block is rescheduled per
// statement: barriers (calls, receives, assertions, and every other
// instruction with an effect) keep their order; a pure instruction is
// emitted just before the first barrier that uses it (with what it needs),
// or at the end of its statement. A statement's first store to memory
// other than a literal being built emits all pending reads first, so the
// right-hand side of an assignment is complete before any assignment
// happens. Stores into an allocation of the same statement (composite
// literals, varargs arrays) move with the reads and are emitted before
// anything that uses the allocation.

type evalSched struct {
	f       *funcGen
	b       *ssa.BasicBlock
	out     []ssa.Instruction
	done    map[ssa.Instruction]bool
	pending map[ssa.Instruction]bool
	order   []ssa.Instruction           // pending, in source order
	stores  map[ssa.Value][]*ssa.Store // pending stores by local allocation
	local   map[ssa.Value]bool          // allocations of the current statement
}

func (f *funcGen) evalOrderOn() bool {
	return os.Getenv("MARMOT_EVALORDER") == "1" && os.Getenv("MARMOT_NO_EVALORDER") == "" && len(f.regionCalls) == 0 && len(f.frameCalls) == 0 && f.fn.Syntax() != nil
}

// statementRanges of the function body (as stmtRanges, for this function).
func (f *funcGen) statementRanges() [][2]token.Pos {
	switch s := f.fn.Syntax().(type) {
	case *ast.FuncDecl:
		if s.Body != nil {
			return stmtRanges(s.Body)
		}
	case *ast.FuncLit:
		return stmtRanges(s.Body)
	}
	return nil
}

// schedule returns b's instructions in evaluation order.
func (f *funcGen) schedule(b *ssa.BasicBlock, ranges [][2]token.Pos) []ssa.Instruction {
	s := &evalSched{f: f, b: b, done: map[ssa.Instruction]bool{}, pending: map[ssa.Instruction]bool{}}
	s.reset()
	var cur [2]token.Pos
	inBlock := map[ssa.Instruction]bool{}
	for _, x := range b.Instrs {
		inBlock[x] = true
	}
	for _, x := range b.Instrs {
		if _, phi := x.(*ssa.Phi); phi {
			s.emit(x)
			continue
		}
		if _, dbg := x.(*ssa.DebugRef); dbg {
			s.emit(x) // no code; must not pull its operand early
			continue
		}
		if p := x.Pos(); p.IsValid() {
			if lo, hi, ok := innermost(ranges, p); ok && [2]token.Pos{lo, hi} != cur {
				// Pure instructions without a position (implicit
				// conversions) belong to the expression that uses them:
				// carry them into the next statement.
				carry := s.flushPositioned()
				s.reset()
				s.order = carry
				cur = [2]token.Pos{lo, hi}
			}
		}
		switch {
		case s.movable(x):
			s.pending[x] = true
			s.order = append(s.order, x)
			if st, ok := x.(*ssa.Store); ok {
				root := allocRoot(st.Addr)
				s.stores[root] = append(s.stores[root], st)
			}
		default:
			if _, ok := x.(*ssa.Alloc); ok {
				s.local[x.(ssa.Value)] = true
			}
			if isStoreLike(x) || isTerminator(x) {
				s.flush() // assignments see the complete right-hand side
			}
			s.need(x)
			s.emit(x)
		}
	}
	s.flush()
	return s.out
}

func (s *evalSched) reset() {
	s.order = nil
	s.stores = map[ssa.Value][]*ssa.Store{}
	s.local = map[ssa.Value]bool{}
}

func (s *evalSched) emit(x ssa.Instruction) {
	if s.done[x] {
		return
	}
	s.done[x] = true
	delete(s.pending, x)
	s.out = append(s.out, x)
}

// need emits the pending instructions x depends on.
func (s *evalSched) need(x ssa.Instruction) {
	// Address arithmetic and stores into a literal do not read it.
	addrOnly := false
	switch x.(type) {
	case *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Store:
		addrOnly = true
	}
	var buf [10]*ssa.Value
	for _, op := range x.Operands(buf[:0]) {
		if op == nil || *op == nil {
			continue
		}
		v := *op
		if !addrOnly {
			// Something reads a literal under construction (directly or
			// through a field or element address): finish it first.
			root := v
			if r := allocRoot(v); r != nil {
				root = r
			}
			if st := s.stores[root]; len(st) > 0 {
				delete(s.stores, root)
				for _, w := range st {
					s.place(w)
				}
			}
		}
		if d, ok := v.(ssa.Instruction); ok && s.pending[d] {
			s.place(d)
		}
	}
}

func (s *evalSched) place(x ssa.Instruction) {
	if s.done[x] || !s.pending[x] {
		return
	}
	s.need(x) // operands are defined earlier, so this cannot reach x again
	s.emit(x)
}

// flushPositioned places the pending instructions that have a source
// position and returns the others, still pending.
func (s *evalSched) flushPositioned() []ssa.Instruction {
	var keep []ssa.Instruction
	for _, x := range s.order {
		if !x.Pos().IsValid() && noMemRead(x) {
			keep = append(keep, x)
			continue
		}
		s.place(x)
	}
	var out []ssa.Instruction
	for _, x := range keep {
		if s.pending[x] {
			out = append(out, x)
		}
	}
	return out
}

func (s *evalSched) flush() {
	for _, x := range s.order {
		s.place(x)
	}
	s.order = nil
	s.stores = map[ssa.Value][]*ssa.Store{}
}

// movable reports whether x has no effect besides its value (and a
// possible run-time panic that gc also takes late), or is a store into an
// allocation of the current statement.
func (s *evalSched) movable(x ssa.Instruction) bool {
	switch y := x.(type) {
	case *ssa.UnOp:
		return y.Op != token.ARROW
	case *ssa.BinOp, *ssa.Lookup, *ssa.Index, *ssa.IndexAddr, *ssa.FieldAddr, *ssa.Field,
		*ssa.Slice, *ssa.Convert, *ssa.ChangeType, *ssa.ChangeInterface, *ssa.MakeInterface,
		*ssa.Extract, *ssa.SliceToArrayPointer, *ssa.MultiConvert:
		return true
	case *ssa.Call:
		if bi, ok := y.Call.Value.(*ssa.Builtin); ok {
			switch bi.Name() {
			case "append", "min", "max", "real", "imag", "complex":
				// len and cap are read where they appear (gc does too).
				return true
			}
		}
		return false
	case *ssa.Store:
		return s.local[allocRoot(y.Addr)]
	}
	return false
}

func allocRoot(a ssa.Value) ssa.Value {
	for {
		switch x := a.(type) {
		case *ssa.IndexAddr:
			a = x.X // a slice operand ends the walk (default case)
		case *ssa.FieldAddr:
			a = x.X
		case *ssa.Alloc:
			return x
		default:
			return nil
		}
	}
}

func isStoreLike(x ssa.Instruction) bool {
	switch x.(type) {
	case *ssa.Store, *ssa.MapUpdate, *ssa.Send:
		return true
	}
	return false
}

func isTerminator(x ssa.Instruction) bool {
	switch x.(type) {
	case *ssa.Jump, *ssa.If, *ssa.Return, *ssa.Panic:
		return true
	}
	return false
}

// noMemRead reports whether x computes only from its operands (no load,
// lookup or store), so it may move past calls of another statement.
func noMemRead(x ssa.Instruction) bool {
	switch y := x.(type) {
	case *ssa.UnOp:
		return y.Op != token.MUL && y.Op != token.ARROW
	case *ssa.BinOp, *ssa.Convert, *ssa.ChangeType, *ssa.ChangeInterface, *ssa.MakeInterface,
		*ssa.Extract, *ssa.Field, *ssa.SliceToArrayPointer, *ssa.MultiConvert:
		return true
	}
	return false
}

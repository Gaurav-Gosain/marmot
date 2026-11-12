package codegen

import (
	"go/token"
	"go/types"
	"os"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// In-place reuse: when append
// must grow a slice whose old backing array no other live value can
// reach, the old array is returned to the allocator (rt_free) so the next
// allocation of that size class reuses it.
//
// The old slice s qualifies when (1) every value that can flow into s
// (through phis) is a make, the zero slice, or the result of such an
// append, all in this function; (2) each of those values is used only by
// len, cap, element reads and writes (whose addresses are only loaded or
// stored through), phis of the chain, and appends of the chain; and (3)
// for every value of the chain reaching s (s included), no use other than
// this append can run after it without that value being redefined first
// (older chain values may share the array the growth frees).

func (f *funcGen) findReusableAppends() {
	// Off by default until the rule is proved (a use-after-free was found
	// in testing); MARMOT_REUSE=1 turns it on.
	if !optReuse || os.Getenv("MARMOT_NO_REUSE") != "" {
		return
	}
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			c, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			bi, ok := c.Call.Value.(*ssa.Builtin)
			if !ok || bi.Name() != "append" || len(c.Call.Args) != 2 {
				continue
			}
			if _, isSlice := c.Call.Args[0].Type().Underlying().(*types.Slice); !isSlice {
				continue
			}
			if uniqueChain(c) && chainDeadAfter(c) {
				if f.reuseAppend == nil {
					f.reuseAppend = map[ssa.Instruction]bool{}
				}
				f.reuseAppend[c] = true
			}
		}
	}
}

// uniqueChain checks conditions (1) and (2) for the values reaching s:
// the chain is the set of slice values (make, nil, appends, phis) that can
// flow into s. Uses of chain values other than reads, len/cap and the
// chain's own phis and appends are allowed only where no append of the
// chain can run afterwards (for example the final return).
func uniqueChain(s ssa.Value) bool {
	chain := map[ssa.Value]bool{}
	var appends []*ssa.Call
	var walk func(v ssa.Value) bool
	walk = func(v ssa.Value) bool {
		if chain[v] {
			return true
		}
		chain[v] = true
		switch x := v.(type) {
		case *ssa.Const:
			return x.IsNil()
		case *ssa.MakeSlice:
		case *ssa.Phi:
			for _, e := range x.Edges {
				if !walk(e) {
					return false
				}
			}
		case *ssa.Call:
			bi, ok := x.Call.Value.(*ssa.Builtin)
			if !ok || bi.Name() != "append" || !walk(x.Call.Args[0]) {
				return false
			}
			appends = append(appends, x)
		default:
			return false
		}
		return true
	}
	if !walk(s) {
		return false
	}
	for v := range chain {
		if _, isConst := v.(*ssa.Const); isConst {
			continue
		}
		refs := v.Referrers()
		if refs == nil {
			continue
		}
		for _, r := range *refs {
			if chainUse(r, v, chain) {
				continue
			}
			for _, a := range appends {
				if canRunAfter(r, a) {
					return false
				}
			}
			// Also the append that triggered this check (it may not be in
			// appends when s is not itself an append result).
		}
	}
	return true
}

// chainUse reports whether r only reads v or carries it along the chain.
func chainUse(r ssa.Instruction, v ssa.Value, chain map[ssa.Value]bool) bool {
	switch x := r.(type) {
	case *ssa.DebugRef, *ssa.Index:
		return true
	case *ssa.Phi:
		return chain[x]
	case *ssa.IndexAddr:
		for _, rr := range *x.Referrers() {
			switch y := rr.(type) {
			case *ssa.UnOp:
				if y.Op != token.MUL {
					return false
				}
			case *ssa.Store:
				if y.Addr != ssa.Value(x) {
					return false
				}
			case *ssa.DebugRef:
			default:
				return false
			}
		}
		return true
	case *ssa.Call:
		bi, ok := x.Call.Value.(*ssa.Builtin)
		if !ok {
			return false
		}
		switch bi.Name() {
		case "len", "cap":
			return true
		case "append":
			return x.Call.Args[0] == v && (len(x.Call.Args) < 2 || x.Call.Args[1] != v) && chain[x]
		}
	}
	return false
}

// canRunAfter reports whether instruction a can execute after r.
func canRunAfter(r ssa.Instruction, a ssa.Instruction) bool {
	rb, ab := r.Block(), a.Block()
	if rb == ab {
		for _, x := range rb.Instrs {
			if x == r {
				break
			}
			if x == a {
				// a comes before r in the block: only via a loop back.
				return inLoop(rb)
			}
		}
		return true
	}
	seen := map[*ssa.BasicBlock]bool{}
	stack := append([]*ssa.BasicBlock{}, rb.Succs...)
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if b == ab {
			return true
		}
		if seen[b] {
			continue
		}
		seen[b] = true
		stack = append(stack, b.Succs...)
	}
	return false
}

// deadAfter checks condition (3): no use of s other than call can run
// after call before s's definition runs again.
func deadAfter(s ssa.Value, call *ssa.Call) bool {
	def, _ := s.(ssa.Instruction)
	var defBlock *ssa.BasicBlock
	if def != nil {
		defBlock = def.Block()
	}
	cb := call.Block()
	idx := func(i ssa.Instruction) int {
		for k, x := range i.Block().Instrs {
			if x == i {
				return k
			}
		}
		return -1
	}
	reach := map[*ssa.BasicBlock]bool{}
	stack := append([]*ssa.BasicBlock{}, cb.Succs...)
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reach[b] || b == defBlock {
			continue
		}
		reach[b] = true
		stack = append(stack, b.Succs...)
	}
	refs := s.Referrers()
	if refs == nil {
		return true // a constant (the nil slice): nothing else holds it
	}
	for _, r := range *refs {
		if r == ssa.Instruction(call) {
			continue
		}
		if _, dbg := r.(*ssa.DebugRef); dbg {
			continue
		}
		rb := r.Block()
		if p, ok := r.(*ssa.Phi); ok {
			// A phi reads s on the edge from its predecessor: that edge runs
			// after call when the predecessor is call's block or reachable.
			for i, e := range p.Edges {
				if e == s {
					pred := rb.Preds[i]
					if pred == cb || reach[pred] {
						return false
					}
				}
			}
			continue
		}
		if rb == cb && idx(r) > idx(call) {
			return false
		}
		if reach[rb] {
			return false
		}
	}
	return true
}

// freeOldArray releases the old backing array after growth.
func (f *funcGen) freeOldArray(p, c string, elem types.Type) {
	es := f.m.p.opts.Sizes.Sizeof(elem)
	if es == 0 {
		return
	}
	f.m.declareRaw("rt_free", []string{mlir.Ptr, mlir.I64}, "", "")
	size := f.op("llvm.mul %s, %s : i64", c, f.constInt(es, mlir.I64))
	// Only small size classes: freeing a large array returns its pages,
	// and the next allocation would fault them in again.
	small := f.op("llvm.icmp \"ule\" %s, %s : i64", size, f.constInt(32<<10, mlir.I64))
	yes, no := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", small, yes, no)
	f.b.WriteString("  " + yes + ":\n")
	f.emit("llvm.call @rt_free(%s, %s) : (!llvm.ptr, i64) -> ()", p, size)
	f.emit("llvm.br %s", no)
	f.b.WriteString("  " + no + ":\n")
}

// chainDeadAfter checks condition (3) for every value of the chain that
// reaches the append's argument, not only the argument: an older value
// (v in a1 := append(v, x); a2 := append(a1, y)) may share the array that
// a2's growth frees. The append's own result is the new array and exempt.
func chainDeadAfter(c *ssa.Call) bool {
	seen := map[ssa.Value]bool{}
	var walk func(v ssa.Value) bool
	walk = func(v ssa.Value) bool {
		if seen[v] || v == ssa.Value(c) {
			return true
		}
		seen[v] = true
		if !deadAfter(v, c) {
			return false
		}
		// Element addresses taken from v point into the same array: their
		// loads and stores are uses too.
		if refs := v.Referrers(); refs != nil {
			for _, r := range *refs {
				if ia, ok := r.(*ssa.IndexAddr); ok && !deadAfter(ia, c) {
					return false
				}
			}
		}
		switch x := v.(type) {
		case *ssa.Phi:
			for _, e := range x.Edges {
				if !walk(e) {
					return false
				}
			}
		case *ssa.Call:
			if len(x.Call.Args) > 0 {
				return walk(x.Call.Args[0])
			}
		}
		return true
	}
	return walk(c.Call.Args[0])
}

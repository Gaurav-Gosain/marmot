package codegen

import (
	"fmt"
	"go/token"
	"go/types"
	"os"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Stack buffers for append from nil (MARMOT_APPENDSTACK=1, off by default
// until gated; the rule and its review are in the experiments notes).
//
// An append site qualifies when its slice operand is, on every path, the nil
// slice or the site's own result (a loop phi that starts at nil), and no
// value derived from the site leaves the function. The site then gets a
// 32-byte frame buffer: growth from capacity 0 to a length that fits takes
// the buffer instead of calling growslice, with the capacity growslice would
// give (the 8-byte size classes up to 32 bytes), so cap() is unchanged.
// Every other growth calls growslice, which only copies from the old store.
//
// "Does not leave" is a small conservative walk (appendStackSafe): the
// derived values are the site's result, its operand phi and reslices of
// them; their uses may only be the site itself, len, cap, copy, reslices and
// element addresses, and an element address may only be loaded from, stored
// into, or indexed further. Any other use (a store of the slice, a call, a
// return, a conversion, another append site, a closure, a second phi such as
// a copy kept from an earlier iteration) rejects the site.
//
// Off on wasm (small fixed stacks), in functions on a static recursion
// cycle of their package, in frame region clones, with in-place append
// reuse (it frees old arrays) and with MARMOT_GCPTR=1 (precise stack maps
// would need the buffer). At most appendStackMax bytes of buffers per
// function.
var optAppendStack = os.Getenv("MARMOT_APPENDSTACK") == "1"

// MARMOT_APPENDSTACK_DEBUG=1 lists the sites that get a buffer on stderr.
var appendStackDebug = os.Getenv("MARMOT_APPENDSTACK_DEBUG") == "1"

const appendStackMax = 2 * tmpBufSize

// findAppendStack picks the qualifying append sites of f's function and
// gives each a frame buffer.
func (f *funcGen) findAppendStack() {
	if !optAppendStack || optGCPtr || f.m.p.opts.GOARCH == "wasm" || f.region != "" || len(f.reuseAppend) > 0 {
		return
	}
	if f.m.p.onRecursionCycle(f.fn) {
		return
	}
	used := int64(0)
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			c, ok := instr.(*ssa.Call)
			if !ok || len(c.Call.Args) != 2 {
				continue
			}
			if bi, ok := c.Call.Value.(*ssa.Builtin); !ok || bi.Name() != "append" {
				continue
			}
			if ms, ok := c.Call.Args[1].(*ssa.MakeSlice); ok && f.appendMake[ms] {
				continue
			}
			st, ok := c.Type().Underlying().(*types.Slice)
			if !ok {
				continue
			}
			es := f.m.p.opts.Sizes.Sizeof(st.Elem())
			if es <= 0 || es > tmpBufSize || used+tmpBufSize > appendStackMax {
				continue
			}
			if _, done := f.appendStack[c]; done {
				continue
			}
			g := appendGroupOf(c)
			if g == nil || !appendStackSafe(g) {
				continue
			}
			align := max(f.m.p.opts.Sizes.Alignof(st.Elem()), 8)
			if f.appendStack == nil {
				f.appendStack = map[ssa.Instruction]string{}
			}
			buf := f.alloca(fmt.Sprintf("!llvm.array<%d x i8>", tmpBufSize), align)
			for _, s := range g.sites {
				f.appendStack[s] = buf
			}
			used += tmpBufSize
			if appendStackDebug {
				fmt.Fprintf(os.Stderr, "appendstack: %s: %s (%d sites)\n", f.m.p.fset.Position(c.Pos()), f.fn, len(g.sites))
			}
		}
	}
}

// An append group is a set of append sites and phis that all carry one
// slice variable: each site's operand is the nil slice or a group value,
// each phi's edges are the nil slice or group values. The group shares one
// frame buffer, taken by a growth from capacity 0.
//
// Rules (each keeps the buffer taken at most once while a value that points
// into it can still be used):
//   - exactly one nil entry: one site with a nil operand, or one nil phi
//     edge;
//   - if the entry is in a CFG cycle (it runs again on the next iteration),
//     the group has no phi, so no value of an earlier iteration survives;
//   - a group value is the operand of at most one group site, except a loop
//     header phi, which may feed one site inside its loop and any number
//     outside it (after the loop the phi holds the last result, which has a
//     backing store if the site inside ever grew);
//   - every use of a group value or of a reslice of one is a group site (as
//     its operand), a group phi, len, cap, copy, a reslice, or an element
//     address that is only loaded from, stored into or indexed
//     (appendStackSafe).
type appendGroup struct {
	sites []*ssa.Call
	phis  []*ssa.Phi
}

// appendGroupOf builds the group of site c, or nil when the rules fail.
func appendGroupOf(c *ssa.Call) *appendGroup {
	in := map[ssa.Value]bool{}
	g := &appendGroup{}
	work := []ssa.Value{c}
	add := func(v ssa.Value) {
		if !in[v] {
			in[v] = true
			work = append(work, v)
		}
	}
	in[c] = true
	// Grow the group through operands, phi edges and referrers.
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		switch x := v.(type) {
		case *ssa.Call:
			g.sites = append(g.sites, x)
			if op := x.Call.Args[0]; !isNilSlice(op) {
				if !isGroupCandidate(op) {
					return nil
				}
				add(op)
			}
		case *ssa.Phi:
			g.phis = append(g.phis, x)
			for _, e := range x.Edges {
				if !isNilSlice(e) {
					if !isGroupCandidate(e) {
						return nil
					}
					add(e)
				}
			}
		}
		if refs := v.Referrers(); refs != nil {
			for _, r := range *refs {
				switch x := r.(type) {
				case *ssa.Call:
					if isAppendSite(x) && x.Call.Args[0] == v {
						add(x)
					}
				case *ssa.Phi:
					add(x)
				}
			}
		}
	}
	// Exactly one nil entry.
	entries := 0
	var entryBlock *ssa.BasicBlock
	for _, s := range g.sites {
		if isNilSlice(s.Call.Args[0]) {
			entries++
			entryBlock = s.Block()
		}
	}
	for _, p := range g.phis {
		for i, e := range p.Edges {
			if isNilSlice(e) {
				entries++
				entryBlock = p.Block().Preds[i]
			}
		}
	}
	if entries != 1 {
		return nil
	}
	if len(g.phis) > 0 && inLoop(entryBlock) {
		return nil
	}
	// Fan-out: at most one site per operand, except a loop header phi.
	uses := map[ssa.Value][]*ssa.Call{}
	for _, s := range g.sites {
		if op := s.Call.Args[0]; !isNilSlice(op) {
			uses[op] = append(uses[op], s)
		}
	}
	for op, ss := range uses {
		if len(ss) <= 1 {
			continue
		}
		phi, ok := op.(*ssa.Phi)
		if !ok || !headerPhiFanOut(phi, ss) {
			return nil
		}
	}
	return g
}

// headerPhiFanOut: phi is a loop header phi (one of its edges comes back
// from inside its loop) and exactly one of the sites is inside that loop.
func headerPhiFanOut(phi *ssa.Phi, sites []*ssa.Call) bool {
	h := phi.Block()
	inside := 0
	for _, s := range sites {
		if reaches(h, s.Block()) && reaches(s.Block(), h) {
			inside++
		}
	}
	return inside == 1
}

// reaches reports whether b is reachable from a in the CFG.
func reaches(a, b *ssa.BasicBlock) bool {
	seen := map[*ssa.BasicBlock]bool{}
	stack := []*ssa.BasicBlock{a}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x == b {
			return true
		}
		if seen[x] {
			continue
		}
		seen[x] = true
		stack = append(stack, x.Succs...)
	}
	return false
}

func isGroupCandidate(v ssa.Value) bool {
	switch x := v.(type) {
	case *ssa.Phi:
		return true
	case *ssa.Call:
		return isAppendSite(x)
	}
	return false
}

func isAppendSite(c *ssa.Call) bool {
	if len(c.Call.Args) != 2 {
		return false
	}
	bi, ok := c.Call.Value.(*ssa.Builtin)
	return ok && bi.Name() == "append"
}

func isNilSlice(v ssa.Value) bool {
	k, ok := v.(*ssa.Const)
	if !ok || k.Value != nil {
		return false
	}
	_, ok = k.Type().Underlying().(*types.Slice)
	return ok
}

// appendStackSafe reports whether no value of group g or derived from it
// (reslices) can outlive the function or be used other than as allowed.
func appendStackSafe(g *appendGroup) bool {
	in := map[ssa.Value]bool{}
	var work []ssa.Value
	for _, s := range g.sites {
		in[s] = true
		work = append(work, s)
	}
	for _, p := range g.phis {
		in[p] = true
		work = append(work, p)
	}
	seen := map[ssa.Value]bool{}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[v] {
			continue
		}
		seen[v] = true
		refs := v.Referrers()
		if refs == nil {
			return false
		}
		for _, r := range *refs {
			switch x := r.(type) {
			case *ssa.DebugRef:
			case *ssa.Phi:
				if !in[x] {
					return false // a copy carried around a loop
				}
			case *ssa.Slice:
				if x.X != v {
					return false
				}
				work = append(work, x)
			case *ssa.IndexAddr:
				if x.X != v || !elemAddrSafe(x) {
					return false
				}
			case *ssa.Call:
				if in[x] && x.Call.Args[0] == v {
					continue // a site of the group
				}
				bi, ok := x.Call.Value.(*ssa.Builtin)
				if !ok {
					return false
				}
				switch bi.Name() {
				case "len", "cap", "copy":
				default:
					return false // append at another site, or anything else
				}
			default:
				return false
			}
		}
	}
	return true
}

// elemAddrSafe: the element address a is only loaded from, stored into, or
// indexed further (never stored itself, passed or converted).
func elemAddrSafe(a ssa.Value) bool {
	refs := a.Referrers()
	if refs == nil {
		return false
	}
	for _, r := range *refs {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case *ssa.UnOp:
			if x.Op != token.MUL || x.X != a {
				return false
			}
		case *ssa.Store:
			if x.Addr != a || x.Val == a {
				return false
			}
		case *ssa.FieldAddr:
			if x.X != a || !elemAddrSafe(x) {
				return false
			}
		case *ssa.IndexAddr:
			if x.X != a || !elemAddrSafe(x) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// appendStackTake emits, in a growth block, the test for the frame buffer
// buf: old capacity c is 0 and newLen fits. It returns the block that holds
// the growslice call (the caller continues there) and arranges that the
// buffer path branches to done with the buffer and growslice's capacity.
func (f *funcGen) appendStackTake(buf, c, newLen, done string, es int64) {
	zero := f.constInt(0, mlir.I64)
	fits := f.op("llvm.icmp \"ule\" %s, %s : i64", newLen, f.constInt(tmpBufSize/es, mlir.I64))
	empty := f.op("llvm.icmp \"eq\" %s, %s : i64", c, zero)
	take := f.op("llvm.and %s, %s : i1", fits, empty)
	stackB, heapB := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", take, stackB, heapB)
	f.b.WriteString("  " + stackB + ":\n")
	f.memset(buf, tmpBufSize)
	// growslice from capacity 0: newcap = newLen, rounded up to the size
	// class (8, 16, 24, 32 bytes), divided by the element size.
	bytes := f.op("llvm.mul %s, %s : i64", newLen, f.constInt(es, mlir.I64))
	up := f.op("llvm.add %s, %s : i64", bytes, f.constInt(7, mlir.I64))
	class := f.op("llvm.and %s, %s : i64", up, f.constInt(^int64(7), mlir.I64))
	nc := f.op("llvm.udiv %s, %s : i64", class, f.constInt(es, mlir.I64))
	f.emit("llvm.br %s(%s, %s : "+tP+", i64)", done, f.gcv(buf), nc)
	f.b.WriteString("  " + heapB + ":\n")
}

// onRecursionCycle reports whether fn calls itself, directly or through
// static calls of functions of its package.
func (p *Program) onRecursionCycle(fn *ssa.Function) bool {
	p.recMu.Lock()
	defer p.recMu.Unlock()
	if r, ok := p.recursive[fn]; ok {
		return r
	}
	if p.recursive == nil {
		p.recursive = map[*ssa.Function]bool{}
	}
	// Depth-first search from fn's callees for fn, within its package.
	seen := map[*ssa.Function]bool{}
	var stack []*ssa.Function
	push := func(g *ssa.Function) {
		for _, b := range g.Blocks {
			for _, instr := range b.Instrs {
				ci, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				if callee := ci.Common().StaticCallee(); callee != nil && !seen[callee] && callee.Pkg == fn.Pkg {
					seen[callee] = true
					stack = append(stack, callee)
				}
			}
		}
	}
	push(fn)
	r := false
	for len(stack) > 0 && !r {
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if g == fn {
			r = true
			break
		}
		push(g)
	}
	p.recursive[fn] = r
	return r
}

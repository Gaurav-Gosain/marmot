package verify

import (
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// engine is engine A on one function.
type engine struct {
	fn      *ssa.Function
	in      map[*ssa.BasicBlock]*state
	out     map[*ssa.BasicBlock]*state
	headers map[*ssa.BasicBlock]bool
	// inline is the callee context: callee analyses run with the
	// caller's state at the call (small same-package callees, for the
	// guard mismatch rule).
	inline *inliner
	depth  int
	final  bool // the evaluation pass after the fixpoint
	// reach[b] are the blocks reachable from b's successors, for
	// liveness.
	reach  map[*ssa.BasicBlock]map[*ssa.BasicBlock]bool
	visits int
	// sums gives callee summaries (nil: none).
	sums func(*ssa.Function) *FnSummary
}

// liveOut reports whether v may be used after block b: a use in a block
// reachable from b's successors, or a phi operand on an edge leaving b
// (or leaving a reachable block).
func (e *engine) liveOut(b *ssa.BasicBlock, v ssa.Value) bool {
	if _, ok := v.(*ssa.Const); ok {
		return true
	}
	refs := v.Referrers()
	if refs == nil {
		return true
	}
	if e.reach == nil {
		e.computeReach()
	}
	r := e.reach[b]
	for _, u := range *refs {
		ub := u.Block()
		if ub == nil {
			return true
		}
		if phi, ok := u.(*ssa.Phi); ok {
			for i, x := range phi.Edges {
				if x == v {
					p := ub.Preds[i]
					if p == b || r[p] {
						return true
					}
				}
			}
			continue
		}
		if r[ub] {
			return true
		}
	}
	return false
}

// branchOperands are the values the branch at the end of b refines on
// its edges, live at b's end whatever their uses.
func branchOperands(b *ssa.BasicBlock) map[ssa.Value]bool {
	iff, ok := b.Instrs[len(b.Instrs)-1].(*ssa.If)
	if !ok {
		return nil
	}
	keep := map[ssa.Value]bool{}
	var add func(v ssa.Value, depth int)
	add = func(v ssa.Value, depth int) {
		keep[v] = true
		if depth > 4 {
			return
		}
		switch c := v.(type) {
		case *ssa.UnOp:
			if c.Op == token.NOT {
				add(c.X, depth+1)
			}
		case *ssa.BinOp:
			add(c.X, depth+1)
			add(c.Y, depth+1)
		}
	}
	add(iff.Cond, 0)
	return keep
}

func (e *engine) computeReach() {
	e.reach = map[*ssa.BasicBlock]map[*ssa.BasicBlock]bool{}
	for _, b := range e.fn.Blocks {
		set := map[*ssa.BasicBlock]bool{}
		stack := append([]*ssa.BasicBlock(nil), b.Succs...)
		for len(stack) > 0 {
			x := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if set[x] {
				continue
			}
			set[x] = true
			stack = append(stack, x.Succs...)
		}
		e.reach[b] = set
	}
}

const (
	maxVisits = 40 // per block, then the block's state is set to top
	widenAt   = 3  // visits before widening at a loop header
)

func newEngine(fn *ssa.Function, inl *inliner, depth int) *engine {
	e := &engine{fn: fn, in: map[*ssa.BasicBlock]*state{}, out: map[*ssa.BasicBlock]*state{}, headers: map[*ssa.BasicBlock]bool{}, inline: inl, depth: depth}
	for _, b := range fn.Blocks {
		for _, p := range b.Preds {
			if b.Dominates(p) {
				e.headers[b] = true
			}
		}
	}
	return e
}

// run computes the fixpoint from entry state init.
func (e *engine) run(init *state) {
	if len(e.fn.Blocks) == 0 {
		return
	}
	order := reversePostorder(e.fn)
	rank := map[*ssa.BasicBlock]int{}
	for i, b := range order {
		rank[b] = i
	}
	visits := map[*ssa.BasicBlock]int{}
	work := map[*ssa.BasicBlock]bool{e.fn.Blocks[0]: true}
	if e.fn.Recover != nil {
		work[e.fn.Recover] = true
	}
	for len(work) > 0 {
		var b *ssa.BasicBlock
		for c := range work {
			if b == nil || rank[c] < rank[b] {
				b = c
			}
		}
		delete(work, b)
		var in *state
		switch {
		case b.Index == 0:
			in = init.clone()
		case b == e.fn.Recover:
			in = newState(init.cx)
		default:
			for _, p := range b.Preds {
				in = join(in, e.edge(p, b))
			}
		}
		if in == nil {
			continue
		}
		visits[b]++
		e.visits++
		old, seen := e.in[b]
		if seen && e.headers[b] && visits[b] > widenAt {
			in = widen(old, in)
		}
		if visits[b] > maxVisits {
			in = newState(init.cx)
		}
		if seen && old.equal(in) {
			continue
		}
		e.in[b] = in
		out := e.transfer(b, in.clone(), nil)
		if out != nil && e.depth == 0 {
			keep := branchOperands(b)
			out.project(func(v ssa.Value) bool { return keep[v] || e.liveOut(b, v) })
		}
		e.out[b] = out
		for _, s := range b.Succs {
			work[s] = true
		}
	}
}

// edge is the state on the edge p -> b: p's out state refined by the
// branch condition and with b's phis assigned.
func (e *engine) edge(p, b *ssa.BasicBlock) *state {
	s := e.out[p]
	if s == nil {
		return nil
	}
	s = s.clone()
	if len(p.Succs) == 2 {
		if iff, ok := p.Instrs[len(p.Instrs)-1].(*ssa.If); ok && p.Succs[0] != p.Succs[1] {
			if !refine(s, iff.Cond, b == p.Succs[0], iff.Cond.Pos()) {
				return nil
			}
			markTested(s, iff.Cond, condPos(iff), 0)
		}
	}
	pi := -1
	for i, q := range b.Preds {
		if q == p {
			pi = i
			break
		}
	}
	assignPhis(s, b, pi)
	return s
}

// condPos is the position recorded for a branch: its condition's.
func condPos(iff *ssa.If) token.Pos {
	if p := iff.Cond.Pos(); p != token.NoPos {
		return p
	}
	return iff.Pos()
}

// markTested records the values a branch condition depends on in ways
// the domain does not model (opaque): call arguments, element and field
// reads, string comparisons other than with "". Integer comparisons,
// arithmetic, len and cap are modeled and not recorded.
func markTested(s *state, v ssa.Value, pos token.Pos, depth int) {
	markTestedOpaque(s, v, pos, depth, false)
}

func markTestedOpaque(s *state, v ssa.Value, pos token.Pos, depth int, opaque bool) {
	if v == nil || depth > 5 {
		return
	}
	if _, ok := v.(*ssa.Const); ok {
		return
	}
	if opaque {
		if _, ok := s.tested[v]; !ok {
			s.tested[v] = pos
		}
	}
	switch x := v.(type) {
	case *ssa.UnOp:
		markTestedOpaque(s, x.X, pos, depth+1, opaque || x.Op == token.MUL)
	case *ssa.BinOp:
		modeled := isIntType(x.X.Type()) || isNilConst(x.X) || isNilConst(x.Y) || isEmptyString(x.X) || isEmptyString(x.Y)
		markTestedOpaque(s, x.X, pos, depth+1, opaque || !modeled)
		markTestedOpaque(s, x.Y, pos, depth+1, opaque || !modeled)
	case *ssa.Call:
		if b, ok := x.Call.Value.(*ssa.Builtin); ok && (b.Name() == "len" || b.Name() == "cap") {
			for _, a := range x.Call.Args {
				markTestedOpaque(s, a, pos, depth+1, opaque)
			}
			return
		}
		for _, a := range x.Call.Args {
			markTestedOpaque(s, a, pos, depth+1, true)
		}
	case *ssa.Index:
		markTestedOpaque(s, x.X, pos, depth+1, true)
	case *ssa.IndexAddr:
		markTestedOpaque(s, x.X, pos, depth+1, true)
	case *ssa.Lookup:
		markTestedOpaque(s, x.X, pos, depth+1, true)
	case *ssa.Slice:
		markTestedOpaque(s, x.X, pos, depth+1, true)
	case *ssa.Convert:
		markTestedOpaque(s, x.X, pos, depth+1, opaque)
	case *ssa.ChangeType:
		markTestedOpaque(s, x.X, pos, depth+1, opaque)
	case *ssa.Extract:
		markTestedOpaque(s, x.Tuple, pos, depth+1, opaque)
	case *ssa.FieldAddr:
		markTestedOpaque(s, x.X, pos, depth+1, true)
	case *ssa.Field:
		markTestedOpaque(s, x.X, pos, depth+1, true)
	}
}

func isEmptyString(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value != nil && c.Value.Kind() == constant.String && constant.StringVal(c.Value) == ""
}

// assignPhis gives b's phis their values on the edge from b.Preds[pi].
func assignPhis(s *state, b *ssa.BasicBlock, pi int) {
	sub := map[ssa.Value][]ssa.Value{} // incoming value -> phis
	var phis []*ssa.Phi
	targets := map[ssa.Value]bool{}
	for _, in := range b.Instrs {
		phi, ok := in.(*ssa.Phi)
		if !ok {
			break
		}
		phis = append(phis, phi)
		targets[phi] = true
		sub[phi.Edges[pi]] = append(sub[phi.Edges[pi]], phi)
	}
	if len(phis) == 0 {
		return
	}
	type newFact struct {
		p  pair
		bd bound
	}
	var facts []newFact
	subst := func(n node) []node {
		if n.k == nZero {
			return []node{n}
		}
		if ps, ok := sub[n.v]; ok {
			var out []node
			for _, p := range ps {
				out = append(out, node{n.k, p})
			}
			if !targets[n.v] {
				out = append(out, n)
			}
			return out
		}
		if targets[n.v] {
			return nil // the old value of a phi, killed below
		}
		return []node{n}
	}
	// Bounds between an incoming value and the nodes that survive the
	// kill, closed first, so facts implied through a killed phi stay.
	survivors := map[node]bool{zeroNode: true}
	for p := range s.d {
		for _, n := range []node{p.a, p.b} {
			if n.k != nZero && !targets[n.v] {
				survivors[n] = true
			}
		}
	}
	if len(survivors) <= 64 {
		for _, phi := range phis {
			x := phi.Edges[pi]
			if targets[x] {
				continue
			}
			for _, k := range []nodeKind{nVal, nLen, nCap} {
				src := node{k, x}
				if k == nVal && !isIntType(x.Type()) || k != nVal && !hasLen(x.Type()) {
					continue
				}
				dst := node{k, phi}
				fw := s.dists(src, false)
				bw := s.dists(src, true)
				for n := range survivors {
					if n == src {
						continue
					}
					if d, ok := fw.get(n); ok {
						facts = append(facts, newFact{pair{dst, n}, bound{d.c, d.why}})
					}
					if d, ok := bw.get(n); ok {
						facts = append(facts, newFact{pair{n, dst}, bound{d.c, d.why}})
					}
				}
			}
		}
	}
	for p, bd := range s.d {
		if _, ok := sub[p.a.v]; !ok {
			if _, ok := sub[p.b.v]; !ok {
				continue
			}
		}
		for _, a := range subst(p.a) {
			for _, b := range subst(p.b) {
				if a != b {
					facts = append(facts, newFact{pair{a, b}, bd})
				}
			}
		}
	}
	type nnFact struct {
		v   ssa.Value
		pos token.Pos
		nil bool
	}
	var nns []nnFact
	guardsNew := map[node]token.Pos{}
	for _, phi := range phis {
		x := phi.Edges[pi]
		if s.nonNil(x) {
			nns = append(nns, nnFact{phi, s.nn[x], false})
		}
		if w, ok := s.isnil[x]; ok {
			nns = append(nns, nnFact{phi, w, true})
		} else if c, ok := x.(*ssa.Const); ok && c.Value == nil && !isIntType(c.Type()) {
			nns = append(nns, nnFact{phi, token.NoPos, true})
		}
		for _, k := range []nodeKind{nVal, nLen, nCap} {
			if w, ok := s.guards[node{k, x}]; ok {
				guardsNew[node{k, phi}] = w
			}
		}
	}
	for _, phi := range phis {
		s.kill(phi)
	}
	for _, f := range facts {
		if old, ok := s.d[f.p]; !ok || f.bd.c < old.c {
			s.set(f.p, f.bd)
		}
	}
	for _, phi := range phis {
		x := phi.Edges[pi]
		if targets[x] {
			continue
		}
		if isIntType(phi.Type()) {
			s.equalNodes(valNode(phi), valNode(x))
		}
		if hasLen(phi.Type()) {
			s.equalNodes(lenNode(phi), lenNode(x))
			if hasCap(phi.Type()) {
				s.equalNodes(capNode(phi), capNode(x))
			}
		}
	}
	for _, f := range nns {
		if f.nil {
			s.isnil[f.v] = f.pos
		} else {
			s.nn[f.v] = f.pos
		}
	}
	for n, w := range guardsNew {
		s.guards[n] = w
	}
}

func hasLen(t types.Type) bool {
	switch t := coreType(t).(type) {
	case *types.Slice, *types.Map, *types.Chan:
		return true
	case *types.Basic:
		return t.Info()&types.IsString != 0
	}
	return false
}

func hasCap(t types.Type) bool {
	switch coreType(t).(type) {
	case *types.Slice, *types.Chan:
		return true
	}
	return false
}

// refine adds what cond == truth implies; false means the edge is
// infeasible.
func refine(s *state, cond ssa.Value, truth bool, pos token.Pos) bool {
	if cf := s.cond[cond]; cf != nil {
		fs := cf.onFalse
		if truth {
			fs = cf.onTrue
		}
		for _, f := range fs {
			if !s.assume(f.a, f.b, f.c, token.NoPos) {
				return false
			}
		}
	}
	switch c := cond.(type) {
	case *ssa.Const:
		if c.Value != nil && c.Value.Kind() == constant.Bool {
			return constant.BoolVal(c.Value) == truth
		}
		return true
	case *ssa.UnOp:
		if c.Op == token.NOT {
			return refine(s, c.X, !truth, pos)
		}
		return true
	case *ssa.BinOp:
		if pos == token.NoPos {
			pos = c.Pos()
		}
		return refineCompare(s, c.Op, c.X, c.Y, truth, pos)
	}
	return true
}

func negateOp(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GEQ
	case token.LEQ:
		return token.GTR
	case token.GTR:
		return token.LEQ
	case token.GEQ:
		return token.LSS
	case token.EQL:
		return token.NEQ
	case token.NEQ:
		return token.EQL
	}
	return token.ILLEGAL
}

func isNilConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value == nil && !isIntType(c.Type())
}

func refineCompare(s *state, op token.Token, x, y ssa.Value, truth bool, pos token.Pos) bool {
	if !truth {
		op = negateOp(op)
	}
	if op == token.ILLEGAL {
		return true
	}
	// Nil comparisons.
	if isNilConst(y) {
		x, y = y, x
	}
	if isNilConst(x) {
		switch op {
		case token.EQL:
			if s.nonNil(y) {
				return false
			}
			s.isnil[y] = pos
			if cf := s.cond[y]; cf != nil {
				for _, v := range cf.nonNilOnNil {
					s.nn[v] = token.NoPos
				}
			}
			if hasLen(y.Type()) {
				return s.setRange(lenNode(y), 0, 0, pos)
			}
		case token.NEQ:
			if _, ok := s.isnil[y]; ok {
				return false
			}
			s.nn[y] = pos
		}
		return true
	}
	// String comparisons with a constant: lengths.
	if b, ok := coreType(x.Type()).(*types.Basic); ok && b.Info()&types.IsString != 0 {
		if c, ok := y.(*ssa.Const); ok {
			x, y = y, x
			_ = c
		}
		if c, ok := x.(*ssa.Const); ok && c.Value != nil && c.Value.Kind() == constant.String {
			n := int64(len(constant.StringVal(c.Value)))
			switch op {
			case token.EQL:
				s.guards[lenNode(y)] = pos
				return s.setRange(lenNode(y), n, n, pos)
			case token.NEQ:
				if n == 0 {
					s.guards[lenNode(y)] = pos
					return s.setRange(lenNode(y), 1, inf, pos)
				}
			}
		}
		return true
	}
	if !isIntType(x.Type()) {
		return true
	}
	a, b := valNode(x), valNode(y)
	if _, ok := x.(*ssa.Const); !ok {
		s.guards[guardNode(x)] = pos
	}
	if _, ok := y.(*ssa.Const); !ok {
		s.guards[guardNode(y)] = pos
	}
	switch op {
	case token.LSS:
		return s.assume(a, b, -1, pos)
	case token.LEQ:
		return s.assume(a, b, 0, pos)
	case token.GTR:
		return s.assume(b, a, -1, pos)
	case token.GEQ:
		return s.assume(b, a, 0, pos)
	case token.EQL:
		return s.assume(a, b, 0, pos) && s.assume(b, a, 0, pos)
	case token.NEQ:
		// x != y with x <= y gives x < y (and the same the other way).
		if c, _ := s.upper(a, b); c == 0 {
			if !s.assume(a, b, -1, pos) {
				return false
			}
		}
		if c, _ := s.upper(b, a); c == 0 {
			if !s.assume(b, a, -1, pos) {
				return false
			}
		}
		// x != y: shrink an interval end that equals the other side's
		// constant value.
		for _, pr := range [][2]ssa.Value{{x, y}, {y, x}} {
			if c, ok := pr[1].(*ssa.Const); ok {
				k, ok := constInt(c)
				if !ok {
					continue
				}
				if k == 0 {
					s.nn[pr[0]] = pos
				}
				lo, hi := s.interval(valNode(pr[0]))
				if lo == k && hi == k {
					return false
				}
				if lo == k {
					return s.setRange(valNode(pr[0]), k+1, inf, pos)
				}
				if hi == k {
					return s.setRange(valNode(pr[0]), ninf, k-1, pos)
				}
			}
		}
	}
	return true
}

// guardNode is the quantity a branch tests: len(s) for a len call.
func guardNode(v ssa.Value) node {
	if c, ok := v.(*ssa.Call); ok {
		if b, ok := c.Call.Value.(*ssa.Builtin); ok && (b.Name() == "len" || b.Name() == "cap") && len(c.Call.Args) == 1 {
			if b.Name() == "len" {
				return lenNode(c.Call.Args[0])
			}
			return capNode(c.Call.Args[0])
		}
	}
	return valNode(v)
}

// transfer runs the instructions of b on s. When check is non-nil it is
// called at every instruction before its effect, for obligations.
func (e *engine) transfer(b *ssa.BasicBlock, s *state, check func(ssa.Instruction, *state)) *state {
	for _, in := range b.Instrs {
		if _, ok := in.(*ssa.Phi); ok {
			continue
		}
		if check != nil {
			check(in, s)
		}
		if !e.step(in, s) {
			return nil
		}
	}
	return s
}

// step applies one instruction; false when it always panics.
func (e *engine) step(in ssa.Instruction, s *state) bool {
	if v, ok := in.(ssa.Value); ok {
		if _, isExtract := in.(*ssa.Extract); !isExtract {
			s.kill(v)
		}
	}
	switch in := in.(type) {
	case *ssa.BinOp:
		binop(s, in)
	case *ssa.UnOp:
		switch in.Op {
		case token.MUL:
			s.nn[in.X] = token.NoPos
			// A load from an address loaded from (or stored to) before,
			// with no store or call since, yields the same value.
			if k := addrKey(in.X, 0); k != "" {
				if prev, ok := s.mem[k]; ok && prev != in {
					copyValue(s, prev, in)
				} else {
					s.mem[k] = in
				}
			}
		case token.SUB:
			if isIntType(in.Type()) {
				lo, hi := s.interval(valNode(in.X))
				tlo, thi, _ := typeRange(in.Type())
				if lo != ninf && hi != inf && neg(hi) >= tlo && neg(lo) <= thi {
					s.setRange(valNode(in), neg(hi), neg(lo), token.NoPos)
				}
			}
		}
	case *ssa.Convert:
		convert(s, in.X, in)
	case *ssa.ChangeType:
		copyValue(s, in.X, in)
	case *ssa.MakeInterface:
		s.nn[in] = token.NoPos
	case *ssa.ChangeInterface:
		if s.nonNil(in.X) {
			s.nn[in] = token.NoPos
		}
	case *ssa.SliceToArrayPointer:
		if n, ok := arrayLen(in); ok {
			s.setRange(lenNode(in.X), n, inf, token.NoPos)
			if n > 0 {
				s.nn[in] = token.NoPos
			}
		}
	case *ssa.Slice:
		sliceOp(s, in)
	case *ssa.MakeSlice:
		s.nn[in] = token.NoPos
		if isIntType(in.Len.Type()) {
			s.equalNodes(lenNode(in), valNode(in.Len))
			s.equalNodes(capNode(in), valNode(in.Cap))
		}
	case *ssa.IndexAddr:
		s.nn[in.X] = token.NoPos
		assumeIndex(s, in.X, in.Index)
	case *ssa.Index:
		assumeIndex(s, in.X, in.Index)
	case *ssa.Lookup:
		if t, ok := coreType(in.X.Type()).(*types.Basic); ok && t.Info()&types.IsString != 0 {
			assumeIndex(s, in.X, in.Index)
		}
	case *ssa.FieldAddr:
		s.nn[in.X] = token.NoPos
	case *ssa.Store:
		s.nn[in.Addr] = token.NoPos
		clear(s.mem)
		if k := addrKey(in.Addr, 0); k != "" {
			s.mem[k] = in.Val
		}
	case *ssa.MapUpdate:
		s.nn[in.Map] = token.NoPos
		clear(s.mem)
	case *ssa.Panic:
		return false
	case *ssa.Call:
		if !pureCall(in) {
			clear(s.mem)
		}
		e.call(s, in)
	case *ssa.Defer:
	case *ssa.Go:
		clear(s.mem)
	case *ssa.Send, *ssa.Select:
		clear(s.mem)
	}
	return true
}

// assumeIndex adds 0 <= i < len(x) after an index that did not panic.
func assumeIndex(s *state, x, i ssa.Value) {
	if !isIntType(i.Type()) {
		return
	}
	s.assume(zeroNode, valNode(i), 0, token.NoPos)
	if n, ok := arrayLen(x); ok {
		s.assume(valNode(i), zeroNode, n-1, token.NoPos)
		return
	}
	s.assume(valNode(i), lenNode(x), -1, token.NoPos)
}

func copyValue(s *state, x, v ssa.Value) {
	if s.nonNil(x) {
		s.nn[v] = s.nn[x]
	}
	if w, ok := s.isnil[x]; ok {
		s.isnil[v] = w
	}
	if isIntType(v.Type()) && isIntType(x.Type()) {
		s.equalNodes(valNode(v), valNode(x))
	}
	if hasLen(v.Type()) && hasLen(x.Type()) {
		s.equalNodes(lenNode(v), lenNode(x))
		if hasCap(v.Type()) && hasCap(x.Type()) {
			s.equalNodes(capNode(v), capNode(x))
		}
	}
}

// convert handles integer conversions (equal when the value fits the
// target type) and string and byte slice conversions (equal lengths).
func convert(s *state, x ssa.Value, v ssa.Value) {
	if isIntType(x.Type()) && isIntType(v.Type()) {
		lo, hi := s.interval(valNode(x))
		tlo, thi, _ := typeRange(v.Type())
		if (tlo == ninf || lo != ninf && lo >= tlo) && (thi == inf || hi != inf && hi <= thi) {
			s.equalNodes(valNode(v), valNode(x))
		}
		return
	}
	if hasLen(x.Type()) && hasLen(v.Type()) {
		_, xs := coreType(x.Type()).(*types.Slice)
		_, vs := coreType(v.Type()).(*types.Slice)
		xb, _ := coreType(x.Type()).(*types.Basic)
		vb, _ := coreType(v.Type()).(*types.Basic)
		// []byte <-> string keep the length; []rune(string) and
		// string([]rune) do not.
		if xs && vb != nil && isByteSlice(x.Type()) || vs && xb != nil && isByteSlice(v.Type()) {
			s.equalNodes(lenNode(v), lenNode(x))
		}
	}
}

func isByteSlice(t types.Type) bool {
	sl, ok := coreType(t).(*types.Slice)
	if !ok {
		return false
	}
	b, ok := sl.Elem().Underlying().(*types.Basic)
	return ok && (b.Kind() == types.Byte || b.Kind() == types.Uint8)
}

func sliceOp(s *state, in *ssa.Slice) {
	x := in.X
	if _, ok := coreType(x.Type()).(*types.Pointer); ok {
		s.nn[x] = token.NoPos
	}
	s.nn[in] = s.nn[x]
	if !s.nonNil(x) {
		delete(s.nn, in)
	}
	var capX node
	_, isStr := coreType(x.Type()).(*types.Basic)
	switch {
	case isStr:
		capX = lenNode(x)
	default:
		if _, ok := arrayLen(x); ok {
			capX = lenNode(x)
		} else {
			capX = capNode(x)
		}
	}
	lo := zeroNode
	if in.Low != nil {
		lo = valNode(in.Low)
	}
	var hi node
	if in.High != nil {
		hi = valNode(in.High)
	} else {
		hi = lenNode(x)
	}
	mx := capX
	if in.Max != nil {
		mx = valNode(in.Max)
	}
	// The bounds held, since the slice did not panic.
	s.assume(zeroNode, lo, 0, token.NoPos)
	s.assume(lo, hi, 0, token.NoPos)
	s.assume(hi, mx, 0, token.NoPos)
	s.assume(mx, capX, 0, token.NoPos)
	// len(v) = hi - lo, cap(v) = max - lo.
	if in.Low == nil || isZeroConst(in.Low) {
		s.equalNodes(lenNode(in), hi)
		if !isStr {
			s.equalNodes(capNode(in), mx)
		}
		return
	}
	if c, ok := in.Low.(*ssa.Const); ok {
		if k, ok := constInt(c); ok {
			s.assume(lenNode(in), hi, -k, token.NoPos)
			s.assume(hi, lenNode(in), k, token.NoPos)
			if !isStr {
				s.assume(capNode(in), mx, -k, token.NoPos)
				s.assume(mx, capNode(in), k, token.NoPos)
			}
			return
		}
	}
	// len(v) = hi - lo exactly (no wrap: 0 <= lo <= hi): bounds from
	// the relation between hi and lo.
	if c, _ := s.upper(hi, lo); c != inf {
		s.setRange(lenNode(in), ninf, c, token.NoPos)
	}
	if c, _ := s.upper(lo, hi); c != inf {
		s.setRange(lenNode(in), neg(c), inf, token.NoPos)
	}
	if !isStr {
		if c, _ := s.upper(mx, lo); c != inf {
			s.setRange(capNode(in), ninf, c, token.NoPos)
		}
	}
	// lo >= 0, so len(v) <= hi; and the interval of hi - lo.
	s.assume(lenNode(in), hi, 0, token.NoPos)
	if !isStr {
		s.assume(capNode(in), mx, 0, token.NoPos)
	}
	hlo, hhi := s.interval(hi)
	llo, lhi := s.interval(lo)
	if hhi != inf && llo != ninf {
		s.setRange(lenNode(in), ninf, hhi-llo, token.NoPos)
	}
	if hlo != ninf && lhi != inf && hlo-lhi > 0 {
		s.setRange(lenNode(in), hlo-lhi, inf, token.NoPos)
	}
}

func isZeroConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	if !ok {
		return false
	}
	k, ok := constInt(c)
	return ok && k == 0
}

// binop adds the facts of integer arithmetic that cannot wrap.
func binop(s *state, in *ssa.BinOp) {
	if !isIntType(in.Type()) {
		return
	}
	v, x, y := valNode(in), valNode(in.X), valNode(in.Y)
	xlo, xhi := s.interval(x)
	ylo, yhi := s.interval(y)
	tlo, thi, _ := typeRange(in.Type())
	fits := func(lo, hi int64) bool {
		return lo != ninf && hi != inf && lo >= tlo && hi <= thi
	}
	switch in.Op {
	case token.ADD, token.SUB:
		if in.Op == token.SUB {
			// v = x - y: bounds of v from the relation between x and y,
			// when the difference cannot wrap (both in [0, maxLen], as
			// lengths and indexes are).
			if xlo >= 0 && ylo >= 0 && xhi != inf && yhi != inf && xhi <= maxLen && yhi <= maxLen {
				if c, _ := s.upper(x, y); c != inf {
					s.setRange(v, ninf, c, token.NoPos)
				}
				if c, _ := s.upper(y, x); c != inf {
					s.setRange(v, neg(c), inf, token.NoPos)
				}
			}
			ylo, yhi = neg(yhi), neg(ylo)
		}
		if xlo == ninf || ylo == ninf || xhi == inf || yhi == inf {
			return // the sum can wrap
		}
		lo, ok1 := addExact(xlo, ylo)
		hi, ok2 := addExact(xhi, yhi)
		if !ok1 || !ok2 || lo < tlo || hi > thi {
			return
		}
		s.setRange(v, lo, hi, token.NoPos)
		// v - x in [ylo, yhi] (y negated for SUB), and for ADD v - y in
		// [xlo, xhi].
		s.assume(v, x, yhi, token.NoPos)
		s.assume(x, v, neg(ylo), token.NoPos)
		if in.Op == token.ADD {
			s.assume(v, y, xhi, token.NoPos)
			s.assume(y, v, neg(xlo), token.NoPos)
		}
	case token.MUL:
		if xlo == ninf || ylo == ninf || xhi == inf || yhi == inf {
			return
		}
		lo, hi, ok := mulRange(xlo, xhi, ylo, yhi)
		if ok && lo >= tlo && hi <= thi && lo != ninf && hi != inf {
			s.setRange(v, lo, hi, token.NoPos)
		}
	case token.QUO:
		if k, ok := in.Y.(*ssa.Const); ok {
			if n, ok := constInt(k); ok && n == 2 {
				midpoint(s, v, in.X)
			}
		}
		if ylo >= 1 && xlo >= 0 {
			s.setRange(v, 0, xhi, token.NoPos)
			if yhi != inf && xhi != inf {
				s.setRange(v, xlo/yhi, xhi/ylo, token.NoPos)
			}
			s.assume(v, x, 0, token.NoPos)
		}
	case token.REM:
		if xlo >= 0 && ylo >= 1 {
			s.setRange(v, 0, inf, token.NoPos)
			s.assume(v, x, 0, token.NoPos)
			s.assume(v, y, -1, token.NoPos)
		} else if ylo >= 1 && yhi != inf {
			s.setRange(v, 1-yhi, yhi-1, token.NoPos)
		}
	case token.AND:
		if xlo >= 0 {
			s.setRange(v, 0, inf, token.NoPos)
			s.assume(v, x, 0, token.NoPos)
		}
		if ylo >= 0 {
			s.setRange(v, 0, inf, token.NoPos)
			s.assume(v, y, 0, token.NoPos)
		}
	case token.AND_NOT:
		if xlo >= 0 {
			s.setRange(v, 0, inf, token.NoPos)
			s.assume(v, x, 0, token.NoPos)
		}
	case token.OR, token.XOR:
		if xlo >= 0 && ylo >= 0 && xhi != inf && yhi != inf {
			m := xhi
			if yhi > m {
				m = yhi
			}
			p := int64(1)
			for p <= m && p < 1<<62 {
				p <<= 1
			}
			if fits(0, 2*p-1) {
				s.setRange(v, 0, 2*p-1, token.NoPos)
			}
		}
	case token.SHR:
		if k, ok := in.Y.(*ssa.Const); ok {
			if n, ok := constInt(k); ok && n == 1 {
				midpoint(s, v, in.X)
			}
		}
		if xlo >= 0 && ylo >= 0 {
			s.setRange(v, 0, xhi, token.NoPos)
			s.assume(v, x, 0, token.NoPos)
			if ylo < 63 && xhi != inf {
				s.setRange(v, 0, xhi>>uint(ylo), token.NoPos)
			}
		}
	case token.SHL:
		if xlo >= 0 && xhi != inf && ylo >= 0 && yhi < 63 {
			if xhi <= (inf >> uint(yhi)) {
				lo, hi := xlo<<uint(ylo), xhi<<uint(yhi)
				if fits(lo, hi) {
					s.setRange(v, lo, hi, token.NoPos)
				}
			}
		}
	}
}

// midpoint handles v = (a+b)/2 or (a+b)>>1 (through integer
// conversions) for non-negative a and b, the binary search idiom: v lies
// between a and b, and below b when a < b.
func midpoint(s *state, v node, w ssa.Value) {
	for {
		switch c := w.(type) {
		case *ssa.Convert:
			w = c.X
			continue
		case *ssa.ChangeType:
			w = c.X
			continue
		}
		break
	}
	add, ok := w.(*ssa.BinOp)
	if !ok || add.Op != token.ADD {
		return
	}
	a, b := valNode(add.X), valNode(add.Y)
	if !proveLE(s, zeroNode, a, 0) || !proveLE(s, zeroNode, b, 0) {
		return
	}
	// The sum is computed without wrapping only if a + b fits; for
	// unsigned conversions of a non-negative int sum it does when both are
	// below maxLen.
	if !proveLE(s, a, zeroNode, maxLen) || !proveLE(s, b, zeroNode, maxLen) {
		return
	}
	for _, pr := range [][2]node{{a, b}, {b, a}} {
		x, y := pr[0], pr[1]
		if c, _ := s.upper(x, y); c <= -1 {
			s.assume(v, y, -1, token.NoPos)
			s.assume(x, v, 0, token.NoPos)
		} else if c <= 0 {
			s.assume(v, y, 0, token.NoPos)
			s.assume(x, v, 0, token.NoPos)
		}
	}
}

// addExact adds two finite values, reporting overflow.
func addExact(x, y int64) (int64, bool) {
	s := x + y
	if (s > x) != (y > 0) {
		return 0, false
	}
	return s, s != inf && s != ninf
}

func mulRange(a, b, c, d int64) (lo, hi int64, ok bool) {
	ps := [4][2]int64{{a, c}, {a, d}, {b, c}, {b, d}}
	lo, hi = inf, ninf
	for _, p := range ps {
		x, y := p[0], p[1]
		if x != 0 && (x*y)/x != y || x == -1 && y == ninf || y == -1 && x == ninf {
			return 0, 0, false
		}
		m := x * y
		if m < lo {
			lo = m
		}
		if m > hi {
			hi = m
		}
	}
	return lo, hi, true
}

// call handles builtins and, with an inliner, small same-package callees.
func (e *engine) call(s *state, in *ssa.Call) {
	c := in.Common()
	if c.IsInvoke() {
		s.nn[c.Value] = token.NoPos
		return
	}
	switch f := c.Value.(type) {
	case *ssa.Builtin:
		builtin(s, in, f.Name(), c.Args)
		return
	case *ssa.Function:
		// The results' facts from the previous iteration are stale.
		for _, r := range resultValues(in) {
			if r != nil && r != ssa.Value(in) {
				s.kill(r)
			}
		}
		// The inliner looks at the state before the call; the summary
		// is what holds after it returns.
		if e.inline != nil {
			e.inline.call(e, s, in, f)
		}
		if e.sums != nil {
			applySummary(s, in, e.sums(f))
		}
		return
	case *ssa.MakeClosure:
		return
	}
	s.nn[c.Value] = token.NoPos
}

func builtin(s *state, in *ssa.Call, name string, args []ssa.Value) {
	switch name {
	case "len", "cap":
		if len(args) != 1 {
			return
		}
		n := lenNode(args[0])
		if name == "cap" {
			n = capNode(args[0])
			if _, ok := arrayLen(args[0]); ok {
				n = lenNode(args[0])
			}
		}
		s.equalNodes(valNode(in), n)
		if _, ok := coreType(args[0].Type()).(*types.Pointer); ok {
			s.nn[args[0]] = token.NoPos
		}
	case "append":
		s.nn[in] = token.NoPos
		if len(args) == 0 {
			return
		}
		// len(append(x, ys...)) >= len(x), and = len(x) + n for a
		// fixed number of elements.
		x := args[0]
		s.assume(lenNode(x), lenNode(in), 0, token.NoPos)
		if len(args) == 2 {
			s.assume(lenNode(args[1]), lenNode(in), 0, token.NoPos)
			if sl, ok := args[1].(*ssa.Slice); ok {
				if _, ok := sl.X.(*ssa.Alloc); ok {
					// append(x, a, b, c): a variadic array literal.
					if n, ok := arrayLen(sl.X); ok {
						s.assume(lenNode(in), lenNode(x), n, token.NoPos)
						s.assume(lenNode(x), lenNode(in), -n, token.NoPos)
					}
				}
			}
		}
	case "copy":
		if len(args) == 2 {
			s.setRange(valNode(in), 0, inf, token.NoPos)
			s.assume(valNode(in), lenNode(args[0]), 0, token.NoPos)
			s.assume(valNode(in), lenNode(args[1]), 0, token.NoPos)
		}
	case "min", "max":
		if !isIntType(in.Type()) {
			return
		}
		for _, a := range args {
			if name == "min" {
				s.assume(valNode(in), valNode(a), 0, token.NoPos)
			} else {
				s.assume(valNode(a), valNode(in), 0, token.NoPos)
			}
		}
	case "recover":
	}
}

// pureCall is a call that writes no memory the function can load:
// builtins that only read.
func pureCall(c *ssa.Call) bool {
	if b, ok := c.Call.Value.(*ssa.Builtin); ok {
		switch b.Name() {
		case "len", "cap", "min", "max", "real", "imag", "complex", "append":
			// append writes only past the length of its first argument,
			// into memory no other value of the function has loaded.
			return b.Name() != "append"
		}
	}
	return false
}

// reversePostorder orders fn's blocks for the worklist.
func reversePostorder(fn *ssa.Function) []*ssa.BasicBlock {
	var out []*ssa.BasicBlock
	seen := map[*ssa.BasicBlock]bool{}
	var visit func(b *ssa.BasicBlock)
	visit = func(b *ssa.BasicBlock) {
		seen[b] = true
		for _, s := range b.Succs {
			if !seen[s] {
				visit(s)
			}
		}
		out = append(out, b)
	}
	visit(fn.Blocks[0])
	if fn.Recover != nil && !seen[fn.Recover] {
		visit(fn.Recover)
	}
	for _, b := range fn.Blocks {
		if !seen[b] {
			visit(b)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

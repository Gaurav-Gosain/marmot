package verify

import (
	"cmp"
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"math"
	"slices"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// The abstract domain of engine A: difference bounds a - b <= c between
// nodes, where a node is an integer SSA value, the length or capacity of
// a value, or the constant zero. Bounds against zero are intervals. This
// is a pentagon domain (intervals plus a < b) extended with constant
// offsets, kept sparse: only facts that some instruction or branch
// established are stored, and queries close them by a bounded
// shortest-path search.
//
// Values are mathematical integers: a fact about v = x + 1 is only added
// when the addition cannot wrap in v's type, so every fact holds of the
// Go values.

const (
	inf  = math.MaxInt64 // no upper bound
	ninf = math.MinInt64 // no lower bound
	// maxLen bounds the length and capacity of a value whose elements take
	// memory: no allocation exceeds the address space.
	maxLen = 1 << 48
)

type nodeKind uint8

const (
	nZero nodeKind = iota
	nVal
	nLen
	nCap
)

type node struct {
	k nodeKind
	v ssa.Value
}

var zeroNode = node{k: nZero}

func valNode(v ssa.Value) node { return node{nVal, v} }
func lenNode(v ssa.Value) node { return node{nLen, v} }
func capNode(v ssa.Value) node { return node{nCap, v} }

type pair struct{ a, b node }

// bound is an upper bound c of a - b, with the position of the branch
// condition that established it (token.NoPos when it came from
// arithmetic or an instruction).
type bound struct {
	c   int64
	why token.Pos
}

// state is the abstract state at one program point; a nil *state is
// unreachable.
type state struct {
	cx *ctx
	d  map[pair]bound
	// es caches the edges of d plus the implicit edges of its nodes, in
	// a deterministic order; nil when stale.
	g *graph
	// nn holds values known to be non-nil (pointers, maps, funcs,
	// channels, interfaces) or non-zero (integers), with the guard.
	nn map[ssa.Value]token.Pos
	// isnil holds values known to be nil.
	isnil map[ssa.Value]token.Pos
	// guards holds the nodes a branch condition tested, for the guard
	// mismatch rule.
	guards map[node]token.Pos
	// tested holds the values every branch on the way here depended on
	// (through calls and element reads too), with the branch: a must
	// set, intersected at joins.
	tested map[ssa.Value]token.Pos
	// mem maps an address expression to the value last loaded from or
	// stored at it, valid until a store or call may change memory.
	mem map[string]ssa.Value
	// cond holds what a later branch on a bool or error value learns,
	// from a callee's summary.
	cond map[ssa.Value]*condFact
}

// ctx is shared by the states of one analysis: value numbers for a
// deterministic order, implicit edge caches and the work budget.
type ctx struct {
	ids  map[ssa.Value]int
	imp  map[node][]edge
	work int64 // edge relaxations left; below zero the analysis gives up
}

// workBudget bounds engine A per function (edge relaxations), so the
// result is deterministic and the time bounded.
const workBudget = 40_000_000

func newCtx() *ctx {
	return &ctx{ids: map[ssa.Value]int{}, imp: map[node][]edge{}, work: workBudget}
}

func (c *ctx) id(v ssa.Value) int {
	if v == nil {
		return -1
	}
	if n, ok := c.ids[v]; ok {
		return n
	}
	n := len(c.ids)
	c.ids[v] = n
	return n
}

func (c *ctx) less(a, b node) bool {
	ia, ib := c.id(a.v), c.id(b.v)
	if ia != ib {
		return ia < ib
	}
	return a.k < b.k
}

type edge struct {
	from, to node
	c        int64
	why      token.Pos
}

// implicitEdges are n's implicit bounds as edges, cached.
func (c *ctx) implicitEdges(n node) []edge {
	if es, ok := c.imp[n]; ok {
		return es
	}
	var es []edge
	implicit(n, func(to node, k int64) { es = append(es, edge{n, to, k, token.NoPos}) })
	if k, ok := implicitFromZero(n); ok {
		es = append(es, edge{zeroNode, n, k, token.NoPos})
	}
	c.imp[n] = es
	return es
}

func newState(cx *ctx) *state {
	return &state{cx: cx, d: map[pair]bound{}, nn: map[ssa.Value]token.Pos{}, isnil: map[ssa.Value]token.Pos{}, guards: map[node]token.Pos{}, tested: map[ssa.Value]token.Pos{}, mem: map[string]ssa.Value{}, cond: map[ssa.Value]*condFact{}}
}

func (s *state) clone() *state {
	if s == nil {
		return nil
	}
	return &state{cx: s.cx, d: maps.Clone(s.d), g: s.g, nn: maps.Clone(s.nn), isnil: maps.Clone(s.isnil), guards: maps.Clone(s.guards), tested: maps.Clone(s.tested), mem: maps.Clone(s.mem), cond: maps.Clone(s.cond)}
}

func (s *state) set(p pair, bd bound) {
	s.d[p] = bd
	s.g = nil
}

func (s *state) equal(t *state) bool {
	if s == nil || t == nil {
		return s == t
	}
	return maps.Equal(s.d, t.d) && maps.Equal(s.nn, t.nn) && maps.Equal(s.isnil, t.isnil) && maps.Equal(s.guards, t.guards) && maps.Equal(s.tested, t.tested) && maps.Equal(s.mem, t.mem) && maps.Equal(s.cond, t.cond)
}

// satAdd adds two finite upper bounds, clamping on overflow (a clamped
// bound is weaker, so still sound); inf absorbs.
func satAdd(x, y int64) int64 {
	if x == inf || y == inf {
		return inf
	}
	s := x + y
	if (s > x) != (y > 0) {
		if y > 0 {
			return inf
		}
		return ninf + 1
	}
	if s == inf {
		return inf
	}
	return s
}

func neg(x int64) int64 {
	switch x {
	case inf:
		return ninf
	case ninf:
		return inf
	}
	return -x
}

// typeRange is the range of values of an integer type (inf and ninf
// where the type reaches the int64 limits).
func typeRange(t types.Type) (lo, hi int64, ok bool) {
	b, ok := coreType(t).(*types.Basic)
	if !ok || b.Info()&types.IsInteger == 0 {
		return ninf, inf, false
	}
	switch b.Kind() {
	case types.Int8:
		return math.MinInt8, math.MaxInt8, true
	case types.Int16:
		return math.MinInt16, math.MaxInt16, true
	case types.Int32:
		return math.MinInt32, math.MaxInt32, true
	case types.Uint8:
		return 0, math.MaxUint8, true
	case types.Uint16:
		return 0, math.MaxUint16, true
	case types.Uint32:
		return 0, math.MaxUint32, true
	case types.Uint, types.Uint64, types.Uintptr:
		return 0, inf, true
	}
	return ninf, inf, true
}

func isIntType(t types.Type) bool {
	_, _, ok := typeRange(t)
	return ok
}

// constInt is the value of an integer constant.
func constInt(c *ssa.Const) (int64, bool) {
	if c.Value == nil {
		if isIntType(c.Type()) {
			return 0, true
		}
		return 0, false
	}
	if c.Value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(c.Value)
}

// lenLimit bounds Len and Cap of v.
func lenLimit(v ssa.Value) int64 {
	t := coreType(v.Type())
	var elem types.Type
	switch t := t.(type) {
	case *types.Slice:
		elem = t.Elem()
	case *types.Basic:
		return maxLen // strings
	case *types.Pointer:
		if a, ok := coreType(t.Elem()).(*types.Array); ok {
			return a.Len()
		}
		return inf
	case *types.Array:
		return t.Len()
	default:
		return inf
	}
	if sizeof(elem) <= 0 {
		return inf
	}
	return maxLen
}

// sizeof is the size of t, -1 when it depends on type parameters.
func sizeof(t types.Type) int64 {
	if hasTypeParam(t, 0) {
		return -1
	}
	return sizes.Sizeof(t)
}

func hasTypeParam(t types.Type, depth int) bool {
	if depth > 8 {
		return true
	}
	switch t := types.Unalias(t).(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		if t.TypeArgs() != nil {
			for a := range t.TypeArgs().Types() {
				if hasTypeParam(a, depth+1) {
					return true
				}
			}
		}
		if t.TypeParams() != nil && t.TypeParams().Len() > 0 && t.TypeArgs() == nil {
			return true
		}
		return hasTypeParam(t.Underlying(), depth+1)
	case *types.Array:
		return hasTypeParam(t.Elem(), depth+1)
	case *types.Struct:
		for f := range t.Fields() {
			if hasTypeParam(f.Type(), depth+1) {
				return true
			}
		}
	}
	return false
}

var sizes = types.SizesFor("gc", "arm64")

// arrayLen is the static length of an array or pointer to array.
func arrayLen(v ssa.Value) (int64, bool) {
	switch t := coreType(v.Type()).(type) {
	case *types.Array:
		return t.Len(), true
	case *types.Pointer:
		if a, ok := coreType(t.Elem()).(*types.Array); ok {
			return a.Len(), true
		}
	}
	return 0, false
}

// implicit are the bounds every state has: type ranges, constants, array
// lengths, len <= cap.
func implicit(n node, out func(to node, c int64)) {
	switch n.k {
	case nZero:
		return
	case nVal:
		if c, ok := n.v.(*ssa.Const); ok {
			if k, ok := constInt(c); ok {
				out(zeroNode, k)
				return
			}
			if c.Value != nil && c.Value.Kind() == constant.String {
				return
			}
		}
		if _, hi, ok := typeRange(n.v.Type()); ok && hi != inf {
			out(zeroNode, hi)
		}
	case nLen, nCap:
		if k, ok := arrayLen(n.v); ok {
			out(zeroNode, k)
			return
		}
		if c, ok := n.v.(*ssa.Const); ok && c.Value != nil && c.Value.Kind() == constant.String {
			out(zeroNode, int64(len(constant.StringVal(c.Value))))
			return
		}
		if c, ok := n.v.(*ssa.Const); ok && c.Value == nil {
			out(zeroNode, 0) // nil slice
			return
		}
		if l := lenLimit(n.v); l != inf {
			out(zeroNode, l)
		}
		if n.k == nLen {
			if _, ok := coreType(n.v.Type()).(*types.Slice); ok {
				out(capNode(n.v), 0)
			}
		}
	}
}

// implicitFromZero are the lower bounds 0 - n <= c.
func implicitFromZero(n node) (int64, bool) {
	switch n.k {
	case nVal:
		if c, ok := n.v.(*ssa.Const); ok {
			if k, ok := constInt(c); ok {
				return neg(k), true
			}
		}
		if lo, _, ok := typeRange(n.v.Type()); ok && lo != ninf {
			return neg(lo), true
		}
	case nLen, nCap:
		if k, ok := arrayLen(n.v); ok {
			return -k, true
		}
		if c, ok := n.v.(*ssa.Const); ok && c.Value != nil && c.Value.Kind() == constant.String {
			return -int64(len(constant.StringVal(c.Value))), true
		}
		return 0, true
	}
	return 0, false
}

// graph is a state's bounds compiled for shortest paths: nodes by
// dense index, stored and implicit edges in a deterministic order.
type graph struct {
	nodes []node
	idx   map[node]int
	es    []iedge
}

type iedge struct {
	from, to int32
	c        int64
	why      token.Pos
}

// edges builds (or returns the cached) graph.
func (s *state) graph() *graph {
	if s.g != nil {
		return s.g
	}
	cx := s.cx
	// Sort by value numbers, computed once per pair, not per comparison.
	type keyed struct {
		ka, kb int64
		p      pair
	}
	ks := make([]keyed, 0, len(s.d))
	for p := range s.d {
		ks = append(ks, keyed{int64(cx.id(p.a.v))<<2 | int64(p.a.k), int64(cx.id(p.b.v))<<2 | int64(p.b.k), p})
	}
	slices.SortFunc(ks, func(x, y keyed) int {
		if x.ka != y.ka {
			return cmp.Compare(x.ka, y.ka)
		}
		return cmp.Compare(x.kb, y.kb)
	})
	ps := make([]pair, len(ks))
	for i, k := range ks {
		ps[i] = k.p
	}
	g := &graph{idx: map[node]int{}}
	id := func(n node) int32 {
		if i, ok := g.idx[n]; ok {
			return int32(i)
		}
		g.idx[n] = len(g.nodes)
		g.nodes = append(g.nodes, n)
		return int32(len(g.nodes) - 1)
	}
	id(zeroNode)
	for _, p := range ps {
		bd := s.d[p]
		g.es = append(g.es, iedge{id(p.a), id(p.b), bd.c, bd.why})
	}
	for i := 0; i < len(g.nodes); i++ {
		for _, e := range cx.implicitEdges(g.nodes[i]) {
			g.es = append(g.es, iedge{id(e.from), id(e.to), e.c, token.NoPos})
		}
	}
	s.g = g
	return g
}

type lab struct {
	c   int64
	why token.Pos
}

// distances are the result of one shortest path search.
type distances struct {
	s       *state
	g       *graph
	d       []int64
	why     []token.Pos
	reverse bool
}

// get bounds a - n (n - a when reverse) for the search's source a.
func (ds *distances) get(n node) (lab, bool) {
	if i, ok := ds.g.idx[n]; ok {
		if ds.d[i] == inf {
			return lab{}, false
		}
		return lab{ds.d[i], ds.why[i]}, true
	}
	// n has only implicit edges: one hop from a node of the graph.
	best, bwhy, found := int64(inf), token.NoPos, false
	for _, e := range ds.s.cx.implicitEdges(n) {
		from, c := e.from, e.c
		if ds.reverse {
			if e.from != n {
				continue
			}
			from = e.to
		} else if e.to != n {
			continue
		}
		i, ok := ds.g.idx[from]
		if !ok || ds.d[i] == inf {
			continue
		}
		if v := satAdd(ds.d[i], c); v < best {
			best, bwhy, found = v, ds.why[i], true
		}
	}
	if !found || best == inf {
		return lab{}, false
	}
	return lab{best, bwhy}, true
}

// dists runs a bounded Bellman-Ford from a over the stored and implicit
// bounds: get(n) bounds a - n (reverse: n - a).
func (s *state) dists(a node, reverse bool) *distances {
	g := s.graph()
	n := len(g.nodes)
	ds := &distances{s: s, g: g, d: make([]int64, n), why: make([]token.Pos, n), reverse: reverse}
	for i := range ds.d {
		ds.d[i] = inf
	}
	if i, ok := g.idx[a]; ok {
		ds.d[i] = 0
	} else {
		// a is not in the graph: start from its implicit neighbors.
		for _, e := range s.cx.implicitEdges(a) {
			to := e.to
			if reverse {
				if e.to != a {
					continue
				}
				to = e.from
			} else if e.from != a {
				continue
			}
			if j, ok := g.idx[to]; ok && e.c < ds.d[j] {
				ds.d[j] = e.c
			}
		}
	}
	for r := 0; r < 8; r++ {
		changed := false
		s.cx.work -= int64(len(g.es))
		for _, e := range g.es {
			from, to := e.from, e.to
			if reverse {
				from, to = to, from
			}
			d := ds.d[from]
			if d == inf {
				continue
			}
			nc := satAdd(d, e.c)
			if nc == inf {
				continue
			}
			why := ds.why[from]
			if why == token.NoPos {
				why = e.why
			}
			// Prefer a tighter bound, and at equal bounds one a guard gave.
			if old := ds.d[to]; nc < old || nc == old && ds.why[to] == token.NoPos && why != token.NoPos {
				ds.d[to], ds.why[to] = nc, why
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return ds
}

// upper is the tightest upper bound of a - b the state implies, inf when
// none, and the first guard position on the path that gives it.
func (s *state) upper(a, b node) (int64, token.Pos) {
	if a == b {
		return 0, token.NoPos
	}
	if d, ok := s.dists(a, false).get(b); ok {
		return d.c, d.why
	}
	return inf, token.NoPos
}

// interval is the range of n.
func (s *state) interval(n node) (lo, hi int64) {
	hi, _ = s.upper(n, zeroNode)
	l, _ := s.upper(zeroNode, n)
	return neg(l), hi
}

// constOf is the value of a constant integer node.
func constOf(n node) (int64, bool) {
	if n.k != nVal {
		return 0, false
	}
	c, ok := n.v.(*ssa.Const)
	if !ok {
		return 0, false
	}
	return constInt(c)
}

// assume adds a - b <= c and reports whether the state stays feasible.
// Constants are folded into bounds against zero.
func (s *state) assume(a, b node, c int64, why token.Pos) bool {
	if k, ok := constOf(b); ok {
		// a - k <= c: a <= c + k.
		nc, ok := addExact(c, k)
		if !ok {
			return true // a weaker (dropped) bound is sound
		}
		b, c = zeroNode, nc
	}
	if k, ok := constOf(a); ok {
		// k - b <= c: -b <= c - k.
		nc, ok := addExact(c, -k)
		if !ok || k == ninf {
			return true
		}
		a, c = zeroNode, nc
		if b == zeroNode {
			return c >= 0
		}
	}
	if c == inf || a == b && c >= 0 {
		return true
	}
	if back, _ := s.upper(b, a); back != inf && satAdd(back, c) < 0 {
		return false
	}
	if a == b {
		return false
	}
	p := pair{a, b}
	if cur, curWhy := s.upper(a, b); cur < c || cur == c && (why == token.NoPos || curWhy != token.NoPos) {
		return true
	}
	if old, ok := s.d[p]; ok && (old.c < c || old.c == c && (why == token.NoPos || old.why != token.NoPos)) {
		return true
	}
	// New, tighter, or the same bound now known from a guard.
	s.set(p, bound{c, why})
	return true
}

// equalNodes adds a == b.
func (s *state) equalNodes(a, b node) bool {
	return s.assume(a, b, 0, token.NoPos) && s.assume(b, a, 0, token.NoPos)
}

// setRange adds lo <= n <= hi.
func (s *state) setRange(n node, lo, hi int64, why token.Pos) bool {
	ok := true
	if hi != inf {
		ok = s.assume(n, zeroNode, hi, why)
	}
	if ok && lo != ninf {
		ok = s.assume(zeroNode, n, neg(lo), why)
	}
	return ok
}

// kill forgets every fact about v (a redefinition in a loop).
func (s *state) kill(v ssa.Value) {
	for p := range s.d {
		if p.a.v == v || p.b.v == v {
			delete(s.d, p)
			s.g = nil
		}
	}
	delete(s.nn, v)
	delete(s.isnil, v)
	for n := range s.guards {
		if n.v == v {
			delete(s.guards, n)
		}
	}
	delete(s.tested, v)
	for k, x := range s.mem {
		if x == v || strings.Contains(k, memID(v)) {
			delete(s.mem, k)
		}
	}
	delete(s.cond, v)
	for k, cf := range s.cond {
		if cf.mentions(v) {
			delete(s.cond, k)
		}
	}
}

func (cf *condFact) mentions(v ssa.Value) bool {
	for _, fs := range [][]pair2{cf.onTrue, cf.onFalse} {
		for _, f := range fs {
			if f.a.v == v || f.b.v == v {
				return true
			}
		}
	}
	return slices.Contains(cf.nonNilOnNil, v)
}

func (s *state) nonNil(v ssa.Value) bool {
	if _, ok := s.nn[v]; ok {
		return true
	}
	return !mayBeNilValue(v) || alwaysNonNil(v)
}

// alwaysNonNil is true of values that are never nil by construction.
func alwaysNonNil(v ssa.Value) bool {
	switch v := v.(type) {
	case *ssa.MakeInterface, *ssa.MakeSlice:
		return true
	case *ssa.Const:
		// A constant string, func or interface is not nil; an integer
		// constant says nothing here (zero is a constant too).
		if v.Value != nil && !isIntType(v.Type()) {
			return true
		}
	case *ssa.Slice:
		_, isPtr := coreType(v.X.Type()).(*types.Pointer)
		return isPtr
	}
	return false
}

// join is the least upper bound (as far as the sparse form allows).
func join(s, t *state) *state {
	if s == nil {
		return t.clone()
	}
	if t == nil {
		return s.clone()
	}
	r := newState(s.cx)
	bySrc := map[node][]node{}
	seen := map[pair]bool{}
	var srcs []node
	for _, m := range []map[pair]bound{s.d, t.d} {
		for p := range m {
			if seen[p] {
				continue
			}
			seen[p] = true
			if _, ok := bySrc[p.a]; !ok {
				srcs = append(srcs, p.a)
			}
			bySrc[p.a] = append(bySrc[p.a], p.b)
		}
	}
	for _, a := range srcs {
		bs := bySrc[a]
		d1 := s.dists(a, false)
		d2 := t.dists(a, false)
		for _, b := range bs {
			l1, ok1 := d1.get(b)
			l2, ok2 := d2.get(b)
			if !ok1 || !ok2 {
				continue
			}
			bd := bound{l1.c, l1.why}
			if l2.c > l1.c || l2.c == l1.c && bd.why == token.NoPos {
				bd = bound{l2.c, l2.why}
			}
			if bd.why == token.NoPos {
				if l1.why != token.NoPos && l1.c == bd.c {
					bd.why = l1.why
				}
			}
			r.d[pair{a, b}] = bd
		}
	}
	for v, w := range s.nn {
		if _, ok := t.nn[v]; ok || t.nonNil(v) {
			r.nn[v] = w
		}
	}
	for v, w := range t.nn {
		if _, ok := r.nn[v]; !ok && s.nonNil(v) {
			r.nn[v] = w
		}
	}
	for v, w := range s.isnil {
		if _, ok := t.isnil[v]; ok {
			r.isnil[v] = w
		}
	}
	maps.Copy(r.guards, s.guards)
	maps.Copy(r.guards, t.guards)
	for v, w := range s.tested {
		if w2, ok := t.tested[v]; ok {
			r.tested[v] = min(w, w2)
		}
	}
	for k, v := range s.mem {
		if t.mem[k] == v {
			r.mem[k] = v
		}
	}
	for k, v := range s.cond {
		if t.cond[k] == v {
			r.cond[k] = v
		}
	}
	return r
}

// widen keeps the bounds of old that cur still satisfies.
func widen(old, cur *state) *state {
	if old == nil {
		return cur.clone()
	}
	if cur == nil {
		return old.clone()
	}
	r := newState(old.cx)
	for p, bd := range old.d {
		if c, _ := cur.upper(p.a, p.b); c <= bd.c {
			r.d[p] = bd
		}
	}
	for v, w := range old.nn {
		if cur.nonNil(v) {
			r.nn[v] = w
		}
	}
	for v, w := range old.isnil {
		if _, ok := cur.isnil[v]; ok {
			r.isnil[v] = w
		}
	}
	maps.Copy(r.guards, old.guards)
	maps.Copy(r.guards, cur.guards)
	for v, w := range old.tested {
		if _, ok := cur.tested[v]; ok {
			r.tested[v] = w
		}
	}
	for k, v := range old.mem {
		if cur.mem[k] == v {
			r.mem[k] = v
		}
	}
	for k, v := range old.cond {
		if cur.cond[k] == v {
			r.cond[k] = v
		}
	}
	return r
}

func (n node) String() string {
	name := "?"
	if n.v != nil {
		name = n.v.Name()
	}
	switch n.k {
	case nZero:
		return "0"
	case nLen:
		return "len(" + name + ")"
	case nCap:
		return "cap(" + name + ")"
	}
	return name
}

func (s *state) String() string {
	if s == nil {
		return "unreachable"
	}
	var parts []string
	for p, bd := range s.d {
		parts = append(parts, fmt.Sprintf("%v-%v<=%d", p.a, p.b, bd.c))
	}
	for v := range s.nn {
		parts = append(parts, "nn("+v.Name()+")")
	}
	slices.Sort(parts)
	return strings.Join(parts, " ")
}

// project eliminates the nodes live rejects: every bound through such a
// node is replaced by the bounds between its neighbors (Fourier-Motzkin
// for difference constraints), then its facts are dropped.
func (s *state) project(isLive func(ssa.Value) bool) {
	if s == nil {
		return
	}
	// Values remembered as the contents of an address stay: a later load
	// of the address is made equal to them.
	keep := map[ssa.Value]bool{}
	for _, v := range s.mem {
		keep[v] = true
	}
	live := func(v ssa.Value) bool { return keep[v] || isLive(v) }
	deadV := map[ssa.Value]bool{}
	var order []node
	seen := map[node]bool{}
	for p := range s.d {
		for _, n := range []node{p.a, p.b} {
			if n.k == nZero || seen[n] {
				continue
			}
			seen[n] = true
			if !live(n.v) {
				deadV[n.v] = true
				order = append(order, n)
			}
		}
	}
	for v := range s.nn {
		if !live(v) {
			delete(s.nn, v)
		}
	}
	for v := range s.isnil {
		if !live(v) {
			delete(s.isnil, v)
		}
	}
	for n := range s.guards {
		if n.v != nil && !live(n.v) {
			delete(s.guards, n)
		}
	}
	for v := range s.tested {
		if !live(v) {
			delete(s.tested, v)
		}
	}
	for k, v := range s.mem {
		if !live(v) {
			delete(s.mem, k)
		}
	}
	for k, cf := range s.cond {
		if !live(k) {
			delete(s.cond, k)
			continue
		}
		for _, fs := range [][]pair2{cf.onTrue, cf.onFalse} {
			for _, f := range fs {
				if f.a.v != nil && !live(f.a.v) || f.b.v != nil && !live(f.b.v) {
					delete(s.cond, k)
				}
			}
		}
	}
	if len(order) == 0 {
		return
	}
	slices.SortFunc(order, func(x, y node) int {
		if x == y {
			return 0
		}
		if s.cx.less(x, y) {
			return -1
		}
		return 1
	})
	for _, d := range order {
		var ins, outs []edge
		for p, bd := range s.d {
			if p.b == d {
				ins = append(ins, edge{p.a, d, bd.c, bd.why})
			}
			if p.a == d {
				outs = append(outs, edge{d, p.b, bd.c, bd.why})
			}
		}
		for _, e := range s.cx.implicitEdges(d) {
			if e.from == d {
				outs = append(outs, e)
			} else {
				ins = append(ins, e)
			}
		}
		for _, in := range ins {
			for _, out := range outs {
				if in.from == out.to {
					continue
				}
				if deadV[in.from.v] && in.from.k != nZero && !seenBefore(order, d, in.from) {
					// eliminated later: still add, it is eliminated in turn
				}
				c := satAdd(in.c, out.c)
				if c == inf {
					continue
				}
				why := in.why
				if why == token.NoPos {
					why = out.why
				}
				p := pair{in.from, out.to}
				if old, ok := s.d[p]; !ok || c < old.c {
					s.d[p] = bound{c, why}
				}
			}
		}
		for p := range s.d {
			if p.a == d || p.b == d {
				delete(s.d, p)
			}
		}
	}
	s.g = nil
}

func seenBefore(order []node, d, n node) bool {
	for _, x := range order {
		if x == n {
			return true
		}
		if x == d {
			return false
		}
	}
	return false
}

// memID names a value inside address keys.
func memID(v ssa.Value) string {
	return fmt.Sprintf("<%p>", v)
}

// addrKey is a structural key of an address: a global, or a chain of
// field and constant index selections from a value. "" when the address
// has no stable key.
func addrKey(a ssa.Value, depth int) string {
	if depth > 6 {
		return ""
	}
	switch a := a.(type) {
	case *ssa.Global:
		return "g:" + a.RelString(nil)
	case *ssa.FieldAddr:
		base := baseKey(a.X, depth+1)
		if base == "" {
			return ""
		}
		return fmt.Sprintf("%s.f%d", base, a.Field)
	case *ssa.IndexAddr:
		c, ok := a.Index.(*ssa.Const)
		if !ok {
			return ""
		}
		k, ok := constInt(c)
		if !ok {
			return ""
		}
		base := baseKey(a.X, depth+1)
		if base == "" {
			return ""
		}
		return fmt.Sprintf("%s[%d]", base, k)
	case *ssa.Parameter, *ssa.FreeVar, *ssa.Alloc:
		return "p:" + memID(a)
	}
	return ""
}

// baseKey keys the pointer an address is computed from.
func baseKey(x ssa.Value, depth int) string {
	switch x := x.(type) {
	case *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Global:
		return addrKey(x, depth)
	case *ssa.UnOp:
		if x.Op == token.MUL {
			k := addrKey(x.X, depth+1)
			if k == "" {
				return ""
			}
			return "*(" + k + ")"
		}
	}
	return "v:" + memID(x)
}

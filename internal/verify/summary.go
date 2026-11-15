package verify

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// FnSummary is what callers learn about a function from engine A: bounds
// between its results and parameters that hold at every return, bounds
// that hold when a bool result is true or false, and results that are
// never nil (or never nil when an error result is nil). It is an analysis
// fact, so callers in other packages see it.
type FnSummary struct {
	Always []SumFact
	// When[i] are the facts when bool result i is true (When[i][1]) or
	// false (When[i][0]).
	When map[int][2][]SumFact
	// NonNil are results never nil; NonNilIfNoErr[i] is the error result
	// whose nil means result i is not nil.
	NonNil        []int
	NonNilIfNoErr map[int]int
}

func (*FnSummary) AFact() {}

func (s *FnSummary) String() string { return "verify summary" }

// SumFact is A - B <= C over interface nodes.
type SumFact struct {
	A, B SumNode
	C    int64
}

// SumNode is zero, a parameter (Ref >= 0) or a result (Ref = -1-i), or
// the length or capacity of one.
type SumNode struct {
	K   uint8 // nodeKind
	Ref int
}

func (s *FnSummary) empty() bool {
	return s == nil || len(s.Always) == 0 && len(s.When) == 0 && len(s.NonNil) == 0 && len(s.NonNilIfNoErr) == 0
}

// summarize builds fn's summary from the states at its returns.
func summarize(fn *ssa.Function, rets []retState) *FnSummary {
	res := fn.Signature.Results()
	if res.Len() == 0 || len(rets) == 0 {
		return nil
	}
	var ifc []SumNode
	addNodes := func(ref int, t types.Type) {
		switch {
		case isIntType(t):
			ifc = append(ifc, SumNode{uint8(nVal), ref})
		case hasLen(t):
			ifc = append(ifc, SumNode{uint8(nLen), ref})
			if hasCap(t) {
				ifc = append(ifc, SumNode{uint8(nCap), ref})
			}
		}
	}
	for i, p := range fn.Params {
		addNodes(i, p.Type())
	}
	for i := 0; i < res.Len(); i++ {
		addNodes(-1-i, res.At(i).Type())
	}
	ifc = append(ifc, SumNode{uint8(nZero), 0})
	// result is the value of result i at a return state: for a phi of the
	// return block, its incoming value on the state's edge.
	result := func(rs retState, i int) ssa.Value {
		v := rs.r.Results[i]
		if phi, ok := v.(*ssa.Phi); ok && rs.pred >= 0 && phi.Block() == rs.r.Block() {
			return phi.Edges[rs.pred]
		}
		return v
	}

	toNode := func(r retState, n SumNode) (node, bool) {
		if nodeKind(n.K) == nZero {
			return zeroNode, true
		}
		var v ssa.Value
		if n.Ref >= 0 {
			v = fn.Params[n.Ref]
		} else {
			v = result(r, -1-n.Ref)
		}
		return node{nodeKind(n.K), v}, true
	}
	// project lists the bounds between interface nodes in s.
	project := func(r retState, s *state) map[[2]SumNode]int64 {
		out := map[[2]SumNode]int64{}
		if s == nil {
			return nil
		}
		for _, a := range ifc {
			an, _ := toNode(r, a)
			ds := s.dists(an, false)
			for _, b := range ifc {
				if a == b {
					continue
				}
				bn, _ := toNode(r, b)
				l, ok := ds.get(bn)
				if !ok {
					continue
				}
				// Skip what the types already say.
				if bn == zeroNode {
					if _, hi, ok := implicitHi(an); ok && hi <= l.c {
						continue
					}
				}
				if an == zeroNode {
					if lo, ok := implicitFromZero(bn); ok && lo <= l.c {
						continue
					}
				}
				out[[2]SumNode{a, b}] = l.c
			}
		}
		return out
	}
	joinFacts := func(ms []map[[2]SumNode]int64) []SumFact {
		var live []map[[2]SumNode]int64
		for _, m := range ms {
			if m != nil {
				live = append(live, m)
			}
		}
		if len(live) == 0 {
			return nil
		}
		var out []SumFact
		for k, c := range live[0] {
			ok := true
			for _, m := range live[1:] {
				c2, has := m[k]
				if !has {
					ok = false
					break
				}
				c = max(c, c2)
			}
			if ok {
				out = append(out, SumFact{k[0], k[1], c})
			}
		}
		slices.SortFunc(out, func(x, y SumFact) int {
			if x.A != y.A {
				return cmpNode(x.A, y.A)
			}
			if x.B != y.B {
				return cmpNode(x.B, y.B)
			}
			return 0
		})
		return out
	}
	sum := &FnSummary{}
	var all []map[[2]SumNode]int64
	for _, r := range rets {
		all = append(all, project(r, r.s))
	}
	sum.Always = joinFacts(all)
	for i := 0; i < res.Len(); i++ {
		t := res.At(i).Type()
		if b, ok := t.Underlying().(*types.Basic); ok && b.Kind() == types.Bool {
			var ts, fs []map[[2]SumNode]int64
			for _, r := range rets {
				if r.s == nil {
					continue
				}
				for _, truth := range []bool{true, false} {
					t := r.s.clone()
					if refine(t, result(r, i), truth, token.NoPos) {
						if truth {
							ts = append(ts, project(r, t))
						} else {
							fs = append(fs, project(r, t))
						}
					}
				}
			}
			var w [2][]SumFact
			w[0], w[1] = joinFacts(fs), joinFacts(ts)
			if len(w[0]) > 0 || len(w[1]) > 0 {
				if sum.When == nil {
					sum.When = map[int][2][]SumFact{}
				}
				sum.When[i] = w
			}
		}
		if !mayBeNilType(t) {
			continue
		}
		always := true
		for _, r := range rets {
			if r.s != nil && !r.s.nonNil(result(r, i)) {
				always = false
			}
		}
		if always {
			sum.NonNil = append(sum.NonNil, i)
			continue
		}
		// Not nil whenever the error result is nil.
		for j := 0; j < res.Len(); j++ {
			if j == i || !isErrorType(res.At(j).Type()) {
				continue
			}
			ok := true
			for _, r := range rets {
				s := r.s
				if s == nil {
					continue
				}
				rj, ri := result(r, j), result(r, i)
				errNil := isNilConst(rj)
				if _, isNil := s.isnil[rj]; isNil {
					errNil = true
				}
				if errNil && !s.nonNil(ri) {
					ok = false
				}
				if !errNil && !s.nonNil(rj) && !s.nonNil(ri) {
					// The error may be nil here and the value nil too.
					ok = false
				}
			}
			if ok {
				if sum.NonNilIfNoErr == nil {
					sum.NonNilIfNoErr = map[int]int{}
				}
				sum.NonNilIfNoErr[i] = j
				break
			}
		}
	}
	if sum.empty() {
		return nil
	}
	return sum
}

func cmpNode(a, b SumNode) int {
	if a.K != b.K {
		return int(a.K) - int(b.K)
	}
	return a.Ref - b.Ref
}

func implicitHi(n node) (int64, int64, bool) {
	hi, found := int64(inf), false
	implicit(n, func(to node, c int64) {
		if to == zeroNode {
			hi, found = c, true
		}
	})
	return 0, hi, found
}

func mayBeNilType(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return !isErrorType(t)
	}
	return false
}

func isErrorType(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

// condFact is what a later branch on a value learns: facts when it is
// true (or, for an error value, nil) and when it is false (non-nil).
type condFact struct {
	onTrue, onFalse []pair2
	// nonNilOnNil are values not nil once the tested error is nil.
	nonNilOnNil []ssa.Value
}

type pair2 struct {
	a, b node
	c    int64
}

// applySummary adds what a call's summary says, and records the
// conditional facts of its bool and error results.
func applySummary(s *state, call *ssa.Call, sum *FnSummary) {
	if sum == nil {
		return
	}
	args := call.Call.Args
	results := resultValues(call)
	toNode := func(n SumNode) (node, bool) {
		if nodeKind(n.K) == nZero {
			return zeroNode, true
		}
		var v ssa.Value
		if n.Ref >= 0 {
			if n.Ref >= len(args) {
				return node{}, false
			}
			v = args[n.Ref]
		} else {
			i := -1 - n.Ref
			if i >= len(results) || results[i] == nil {
				return node{}, false
			}
			v = results[i]
		}
		return node{nodeKind(n.K), v}, true
	}
	conv := func(fs []SumFact) []pair2 {
		var out []pair2
		for _, f := range fs {
			a, ok1 := toNode(f.A)
			b, ok2 := toNode(f.B)
			if ok1 && ok2 {
				out = append(out, pair2{a, b, f.C})
			}
		}
		return out
	}
	for _, f := range conv(sum.Always) {
		s.assume(f.a, f.b, f.c, token.NoPos)
	}
	for i, w := range sum.When {
		if i < len(results) && results[i] != nil {
			s.cond[results[i]] = &condFact{onTrue: conv(w[1]), onFalse: conv(w[0])}
		}
	}
	for _, i := range sum.NonNil {
		if i < len(results) && results[i] != nil {
			s.nn[results[i]] = token.NoPos
		}
	}
	for i, j := range sum.NonNilIfNoErr {
		if i < len(results) && j < len(results) && results[i] != nil && results[j] != nil {
			cf := s.cond[results[j]]
			if cf == nil {
				cf = &condFact{}
			} else {
				c := *cf
				cf = &c
			}
			cf.nonNilOnNil = append(cf.nonNilOnNil, results[i])
			s.cond[results[j]] = cf
		}
	}
}

// resultValues are the values of a call's results: the call itself for
// one result, its Extracts for several (nil where unused).
func resultValues(call *ssa.Call) []ssa.Value {
	n := call.Call.Signature().Results().Len()
	if n == 1 {
		return []ssa.Value{call}
	}
	out := make([]ssa.Value, n)
	if refs := call.Referrers(); refs != nil {
		for _, r := range *refs {
			if e, ok := r.(*ssa.Extract); ok && e.Index < n && out[e.Index] == nil {
				out[e.Index] = e
			}
		}
	}
	return out
}

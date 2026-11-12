package main

// bce.go: an independent re-check of go-bce (backend/lib/Dialect/Go/BCE.cpp). For every check the pass
// removed, find a justification by the rules as written and as proved in verify/bend
// (B1, B2, B3, N1, N2). Dominance is decided the way the proofs take it: t dominates x
// when x is not reachable from entry on paths that avoid t (the closed set S of the
// dominance certificate is exactly that reachable set).

import (
	"fmt"
	"regexp"
	"strings"
)

type Check struct {
	Kind    string // bounds, index, nil
	Op      *Op
	I, N, P string
	Removed bool
}

func checksOf(f *Func) []*Check {
	var cs []*Check
	for _, b := range f.Blocks {
		for _, op := range b.Ops {
			switch op.Name {
			case "go.bounds_check":
				if len(op.Operands) >= 2 {
					cs = append(cs, &Check{Kind: "bounds", Op: op, I: op.Operands[0], N: op.Operands[1]})
				}
			case "go.index":
				if len(op.Operands) >= 3 {
					cs = append(cs, &Check{Kind: "index", Op: op, I: op.Operands[1], N: op.Operands[2],
						Removed: hasWord(op.Text, "nocheck")})
				}
			case "go.nil_check":
				if len(op.Operands) >= 1 {
					cs = append(cs, &Check{Kind: "nil", Op: op, P: op.Operands[0]})
				}
			}
		}
	}
	return cs
}

func hasWord(s, w string) bool {
	for _, t := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' }) {
		if t == w {
			return true
		}
	}
	return false
}

// reach: blocks reachable from entry without entering avoid (avoid == nil: none).
func reach(f *Func, avoid *Block) map[*Block]bool {
	seen := map[*Block]bool{}
	entry := f.Blocks[0]
	if entry == avoid {
		return seen
	}
	stack := []*Block{entry}
	seen[entry] = true
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, s := range b.Succs {
			t := f.ByName[s.Block]
			if t == nil || t == avoid || seen[t] {
				continue
			}
			seen[t] = true
			stack = append(stack, t)
		}
	}
	return seen
}

type Dom struct {
	f   *Func
	all map[*Block]bool
	s   map[*Block]map[*Block]bool // t -> S_t
}

func newDom(f *Func) *Dom { return &Dom{f: f, all: reach(f, nil), s: map[*Block]map[*Block]bool{}} }

// dominates: t is entry, or x is unreachable, or x is outside S_t (the certificate).
func (d *Dom) dominates(t, x *Block) bool {
	if t == x || t == d.f.Blocks[0] || !d.all[x] {
		return true
	}
	S := d.s[t]
	if S == nil {
		S = reach(d.f, t)
		d.s[t] = S
		if !closed(d.f, t, S) {
			panic("certificate not closed")
		}
	}
	return !S[x]
}

// closed re-checks the certificate property the proofs assume (graph.bend, closed): every
// edge a -> b with a in S and b != t has b in S, and entry is in S.
func closed(f *Func, t *Block, S map[*Block]bool) bool {
	if !S[f.Blocks[0]] {
		return false
	}
	for a := range S {
		for _, s := range a.Succs {
			b := f.ByName[s.Block]
			if b != nil && b != t && !S[b] {
				return false
			}
		}
	}
	return true
}

func (d *Dom) properlyDominates(a, b *Op) bool {
	if a.Block == b.Block {
		return a.Pos < b.Pos
	}
	return d.dominates(a.Block, b.Block)
}

var reEVPos = regexp.MustCompile(`\[([\d, ]+)\]`)

// sameValue is E1.
func sameValue(f *Func, a, b string) bool {
	if a == b {
		return true
	}
	ca, ta, oka := f.Const(a)
	cb, tb, okb := f.Const(b)
	if oka && okb {
		return ca == cb && ta == tb
	}
	ea, eb := f.Def[a], f.Def[b]
	if ea == nil || eb == nil || ea.Name != "llvm.extractvalue" || eb.Name != "llvm.extractvalue" {
		return false
	}
	if len(ea.Operands) < 1 || len(eb.Operands) < 1 || ea.Operands[0] != eb.Operands[0] {
		return false
	}
	pa, pb := reEVPos.FindStringSubmatch(ea.Text), reEVPos.FindStringSubmatch(eb.Text)
	return pa != nil && pb != nil && pa[1] == pb[1]
}

// b1: constants with 0 <= i < n (signed).
func b1(f *Func, c *Check) bool {
	i, ti, ok1 := f.Const(c.I)
	n, tn, ok2 := f.Const(c.N)
	return ok1 && ok2 && ti == tn && i >= 0 && i < n
}

// b3: the counted loop rule, condition by condition.
func b3(f *Func, d *Dom, c *Check) (bool, string) {
	h := f.ArgOf[c.I]
	if h == nil {
		return false, "index is not a block argument"
	}
	if h == f.Blocks[0] {
		return false, "H is the entry block"
	}
	term := h.Ops[len(h.Ops)-1]
	if term.Name != "llvm.cond_br" || len(term.Operands) < 1 || len(h.Succs) != 2 {
		return false, "H does not end in cond_br"
	}
	cmp := f.Def[term.Operands[0]]
	if cmp == nil || cmp.Name != "llvm.icmp" || len(cmp.Operands) != 2 || cmp.Operands[0] != c.I {
		return false, "guard is not icmp with i on the left"
	}
	if !strings.Contains(cmp.Text, `"slt"`) && !strings.Contains(cmp.Text, `"ult"`) {
		return false, "guard predicate is not slt or ult"
	}
	if !sameValue(f, cmp.Operands[1], c.N) {
		return false, "guard bound is not the check's length (E1)"
	}
	T, F := f.ByName[h.Succs[0].Block], f.ByName[h.Succs[1].Block]
	if T == nil || T == F {
		return false, "T == F"
	}
	if len(T.Preds) != 1 || T.Preds[0] != h {
		return false, "T has a predecessor other than H"
	}
	if !d.dominates(T, c.Op.Block) {
		return false, "T does not dominate the check"
	}
	idx := f.ArgIdx[c.I]
	for _, p := range h.Preds {
		for _, s := range p.Succs {
			if f.ByName[s.Block] != h {
				continue
			}
			if idx >= len(s.Operands) {
				return false, "edge into H without an operand for i"
			}
			in := s.Operands[idx]
			if v, _, ok := f.Const(in); ok {
				if v < 0 {
					return false, "negative start constant"
				}
				continue
			}
			add := f.Def[in]
			if add == nil || add.Name != "llvm.add" || len(add.Operands) != 2 {
				return false, "edge operand is neither a constant nor an add"
			}
			other := ""
			if add.Operands[0] == c.I {
				other = add.Operands[1]
			} else if add.Operands[1] == c.I {
				other = add.Operands[0]
			}
			if v, _, ok := f.Const(other); !ok || v != 1 {
				return false, "increment is not i + 1"
			}
			if !d.dominates(T, add.Block) {
				return false, "T does not dominate the increment"
			}
		}
	}
	return true, ""
}

var nonNilOps = map[string]bool{"go.new": true, "go.box": true, "go.make_closure": true, "llvm.alloca": true, "llvm.mlir.addressof": true}

func n1(f *Func, p string) bool {
	op := f.Def[p]
	if op == nil {
		return false
	}
	if nonNilOps[op.Name] {
		return true
	}
	return op.Name == "llvm.call" && (strings.Contains(op.Text, "@runtime.newobject(") || strings.Contains(op.Text, "@runtime.mallocgc(") ||
		strings.Contains(op.Text, `@"runtime.newobject"(`) || strings.Contains(op.Text, `@"runtime.mallocgc"(`))
}

// Justify finds a rule for a removed check. kept: the checks still present afterwards.
func Justify(f *Func, d *Dom, c *Check, kept []*Check) (string, string) {
	if c.Kind == "nil" {
		if n1(f, c.P) {
			return "N1", ""
		}
		for _, k := range kept {
			if k.Kind == "nil" && k.P == c.P && d.properlyDominates(k.Op, c.Op) {
				return "N2", ""
			}
		}
		return "", "no rule: not a fresh or static address and no dominating nil check of it"
	}
	if b1(f, c) {
		return "B1", ""
	}
	for _, k := range kept {
		if k.Kind != "nil" && sameValue(f, k.I, c.I) && sameValue(f, k.N, c.N) && d.properlyDominates(k.Op, c.Op) {
			return "B2", ""
		}
	}
	if ok, why := b3(f, d, c); ok {
		return "B3", ""
	} else {
		return "", "no rule; B3: " + why
	}
}

type Result struct {
	Func, Rule, Why string
	Check           *Check
}

func (r Result) String() string {
	if r.Rule != "" {
		return fmt.Sprintf("%s: %s %s ok (%s)", r.Func, r.Check.Kind, r.Check.Op.Block.Name, r.Rule)
	}
	return fmt.Sprintf("%s: %s check in %s removed without justification: %s", r.Func, r.Check.Kind, r.Check.Op.Block.Name, r.Why)
}

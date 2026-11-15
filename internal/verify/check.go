package verify

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Status is the outcome for one obligation.
type Status string

// Statuses (design section 5.5).
const (
	Proved         Status = "proved"
	ProvedAssuming Status = "proved-assuming"
	Recovered      Status = "recovered"
	Counterexample Status = "counterexample"
	Latent         Status = "latent"
	Unknown        Status = "unknown"
)

// verdict is engine A's answer at one obligation.
type verdict struct {
	proved bool
	dead   bool // the instruction is unreachable
	// mismatch is set when a guard tested the quantity the obligation
	// needs with a weaker bound (manifest rule 1).
	mismatch *mismatch
}

// mismatch describes a guard mismatch: quantity q is guarded at guard,
// the obligation needs a stronger bound, and fails exactly when the
// condition holds.
type mismatch struct {
	q         node
	guard     token.Pos
	condition string // Go expression over the caller's names
	needs     string // the bound the obligation needs
	allows    string // what the guard allows
	lo, hi    int64  // failing range of q (q in [lo, hi])
	other     node   // for a relational mismatch: q - other in [lo, hi]
}

// proveLE reports whether a - b <= c holds in s.
func proveLE(s *state, a, b node, c int64) bool {
	u, _ := s.upper(a, b)
	return u <= c
}

// checkObligation runs engine A's test for o in state s (nil when
// unreachable).
func checkObligation(o *Obligation, s *state) verdict {
	if s == nil {
		return verdict{proved: true, dead: true}
	}
	switch o.Kind {
	case KIndex:
		return checkIndex(o, s, valNode(o.I), o.X)
	case KSlice:
		return checkSlice(o, s)
	case KNil:
		if s.nonNil(o.X) {
			return verdict{proved: true}
		}
	case KIface:
		if s.nonNil(o.X) {
			return verdict{proved: true}
		}
	case KDivide:
		if s.nonNil(o.X) {
			return verdict{proved: true}
		}
		lo, hi := s.interval(valNode(o.X))
		if lo > 0 || hi < 0 {
			return verdict{proved: true}
		}
	case KShift:
		if proveLE(s, zeroNode, valNode(o.X), 0) {
			return verdict{proved: true}
		}
	case KMake:
		ok := proveLE(s, zeroNode, valNode(o.I), 0)
		if ok && o.Hi != nil {
			ok = proveLE(s, valNode(o.I), valNode(o.Hi), 0)
		}
		if ok {
			// Too large: the length must fit the address space.
			lim := int64(maxLen)
			if mk, isSlice := o.Instr.(*ssa.MakeSlice); isSlice {
				if sz := sizeof(coreType(mk.Type()).(*types.Slice).Elem()); sz > 0 {
					lim = maxLen / sz
				}
			}
			top := o.I
			if o.Hi != nil {
				top = o.Hi
			}
			ok = proveLE(s, valNode(top), zeroNode, lim)
		}
		if ok {
			return verdict{proved: true}
		}
	case KConvert:
		if proveLE(s, zeroNode, lenNode(o.X), -o.N) {
			return verdict{proved: true}
		}
	case KUnsafe:
		if proveLE(s, zeroNode, valNode(o.I), 0) {
			return verdict{proved: true}
		}
	case KAppend:
		// Both operands exist in memory, so their total length cannot
		// overflow int when elements take memory.
		if sl, ok := coreType(o.X.Type()).(*types.Slice); ok && sizeof(sl.Elem()) > 0 {
			return verdict{proved: true}
		}
		if _, ok := coreType(o.X.Type()).(*types.Basic); ok {
			return verdict{proved: true} // append([]byte, string...)
		}
	case KAssert:
		ta := o.Instr.(*ssa.TypeAssert)
		if mi, ok := o.X.(*ssa.MakeInterface); ok {
			if types.IsInterface(ta.AssertedType) {
				if types.Implements(mi.X.Type(), ta.AssertedType.Underlying().(*types.Interface)) {
					return verdict{proved: true}
				}
			} else if types.Identical(mi.X.Type(), ta.AssertedType) {
				return verdict{proved: true}
			}
		}
	case KCompare:
		if comparableDynamic(o.X) || o.I != nil && comparableDynamic(o.I) {
			return verdict{proved: true}
		}
	case KPanic:
	}
	return verdict{}
}

// comparableDynamic reports whether interface value v holds a dynamic
// type that is comparable (or is nil), so == on it cannot panic.
func comparableDynamic(v ssa.Value) bool {
	switch v := v.(type) {
	case *ssa.MakeInterface:
		return types.Comparable(v.X.Type()) && !types.IsInterface(v.X.Type()) && strictlyComparable(v.X.Type())
	case *ssa.Const:
		return v.Value == nil
	}
	return false
}

// strictlyComparable is false for types that are comparable but can
// panic on ==, structs or arrays holding interfaces.
func strictlyComparable(t types.Type) bool {
	switch t := t.Underlying().(type) {
	case *types.Interface:
		return false
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !strictlyComparable(t.Field(i).Type()) {
				return false
			}
		}
	case *types.Array:
		return strictlyComparable(t.Elem())
	}
	return true
}

// checkIndex proves 0 <= i < len(x) (or the array length).
func checkIndex(o *Obligation, s *state, i node, x ssa.Value) verdict {
	nonneg := proveLE(s, zeroNode, i, 0)
	var below bool
	var l node
	if n, ok := arrayLen(x); ok {
		below = proveLE(s, i, zeroNode, n-1)
		l = zeroNode
		if !below {
			return verdict{mismatch: indexMismatchConst(s, i, n)}
		}
	} else {
		l = lenNode(x)
		below = proveLE(s, i, l, -1)
	}
	if nonneg && below {
		return verdict{proved: true}
	}
	if !below && l != zeroNode {
		return verdict{mismatch: indexMismatch(s, i, l)}
	}
	return verdict{}
}

// guardTested reports whether the branch at why tested q itself (or a
// node equal to q), so a bound from it is a guard on that quantity.
func guardTested(s *state, why token.Pos, qs ...node) bool {
	for n, w := range s.guards {
		if w != why {
			continue
		}
		for _, q := range qs {
			if _, isConst := constOf(q); isConst {
				continue
			}
			if n == q || sameQuantity(s, n, q) {
				return true
			}
		}
	}
	return false
}

// testedElsewhere reports whether a dominating branch other than why
// depended on the value under one of qs (the slice of len(s), the
// integer itself): strings.HasPrefix(p, "//"), s[1] == "5". The domain
// does not see what such a test implies, so no mismatch is shown.
func testedElsewhere(s *state, why token.Pos, qs ...node) bool {
	for _, q := range qs {
		if q.v == nil {
			continue
		}
		if _, isConst := constOf(q); isConst {
			continue
		}
		if w, ok := s.tested[q.v]; ok && w != why {
			return true
		}
	}
	return false
}

// derivedGuard reports whether a branch other than at why tested a
// value computed from one of qs (len(s)%2, i-1): the obligation may then
// depend on that test in a way the domain does not see.
func derivedGuard(s *state, why token.Pos, qs ...node) bool {
	for n, w := range s.guards {
		if w == why || n.k != nVal {
			continue
		}
		for _, q := range qs {
			if n == q || sameQuantity(s, n, q) {
				continue
			}
			if derivesFrom(n.v, q, 0) {
				return true
			}
		}
	}
	return false
}

// derivesFrom reports whether v is computed from q by arithmetic.
func derivesFrom(v ssa.Value, q node, depth int) bool {
	if depth > 6 || v == nil {
		return false
	}
	if q.k == nVal && v == q.v {
		return true
	}
	switch x := v.(type) {
	case *ssa.Call:
		if b, ok := x.Call.Value.(*ssa.Builtin); ok && len(x.Call.Args) == 1 {
			if (b.Name() == "len" && q.k == nLen || b.Name() == "cap" && q.k == nCap) && x.Call.Args[0] == q.v {
				return true
			}
		}
	case *ssa.BinOp:
		return derivesFrom(x.X, q, depth+1) || derivesFrom(x.Y, q, depth+1)
	case *ssa.UnOp:
		return derivesFrom(x.X, q, depth+1)
	case *ssa.Convert:
		return derivesFrom(x.X, q, depth+1)
	case *ssa.ChangeType:
		return derivesFrom(x.X, q, depth+1)
	}
	return false
}

// validGuard combines the guard rules: the bound came from a branch
// that tested the quantity, and no other branch tested something
// derived from it.
func validGuard(s *state, why token.Pos, qs ...node) bool {
	return why != token.NoPos && guardTested(s, why, qs...) && !derivedGuard(s, why, qs...) && !testedElsewhere(s, why, qs...)
}

// otherGuards reports whether a guard related q (or a node equal to q)
// to a node that is not a constant, not q and not one of except: then
// whether the obligation fails depends on that node too, and a mismatch
// on q alone is not shown.
func otherGuards(s *state, q node, except ...node) bool {
	same := func(a, b node) bool { return a == b || sameQuantity(s, a, b) }
	isQ := func(n node) bool { return n.k != nZero && same(n, q) }
	ignorable := func(o node) bool {
		if o.k == nZero {
			return true
		}
		if _, ok := o.v.(*ssa.Const); ok && o.k == nVal {
			return true
		}
		if isQ(o) {
			return true
		}
		for _, x := range except {
			if same(o, x) {
				return true
			}
		}
		return false
	}
	for p, bd := range s.d {
		if bd.why == token.NoPos {
			continue
		}
		switch {
		case isQ(p.a):
			if !ignorable(p.b) {
				return true
			}
		case isQ(p.b):
			if !ignorable(p.a) {
				return true
			}
		}
	}
	return false
}

// sameQuantity: a == b in s.
func sameQuantity(s *state, a, b node) bool {
	return proveLE(s, a, b, 0) && proveLE(s, b, a, 0)
}

// indexMismatch looks for a guard on the quantity i < l needs.
func indexMismatch(s *state, i, l node) *mismatch {
	ilo, ihi := s.interval(i)
	// The index is fixed (a constant after inlining) and the length is
	// guarded from below: the obligation fails for len in [llo, k].
	if ilo == ihi && ilo != ninf {
		k := ilo
		negLo, why := s.upper(zeroNode, l)
		llo := neg(negLo)
		if validGuard(s, why, l) && llo <= k && !otherGuards(s, l) {
			if !feasible(s, l, zeroNode, k) {
				return nil
			}
			return &mismatch{q: l, guard: why, lo: llo, hi: k, needs: fmt.Sprintf(">= %d", k+1)}
		}
	}
	// i - l is guarded: i <= l + c with c >= 0 (i not a constant: a
	// constant index is the case above).
	if _, isConst := constOf(i); isConst || ilo == ihi {
		return nil
	}
	c, why := s.upper(i, l)
	if validGuard(s, why, i, l) && c >= 0 && c != inf && !otherGuards(s, i, l) && !otherGuards(s, l, i) {
		if !feasible(s, l, i, 0) {
			return nil
		}
		return &mismatch{q: i, other: l, guard: why, lo: 0, hi: c, needs: "< 0"}
	}
	return nil
}

// indexMismatchConst handles arrays: the index is guarded from above by
// something at least the length.
func indexMismatchConst(s *state, i node, n int64) *mismatch {
	hi, why := s.upper(i, zeroNode)
	if !validGuard(s, why, i) || hi < n || otherGuards(s, i) {
		return nil
	}
	if !feasible(s, zeroNode, i, -n) {
		return nil
	}
	return &mismatch{q: i, guard: why, lo: n, hi: hi, needs: fmt.Sprintf("<= %d", n-1)}
}

// feasible reports whether adding a - b <= c keeps s satisfiable.
func feasible(s *state, a, b node, c int64) bool {
	t := s.clone()
	return t.assume(a, b, c, token.NoPos)
}

// checkSlice proves 0 <= lo <= hi <= max <= cap.
func checkSlice(o *Obligation, s *state) verdict {
	x := o.X
	var capX node
	_, isStr := coreType(x.Type()).(*types.Basic)
	if _, ok := arrayLen(x); ok || isStr {
		capX = lenNode(x)
	} else {
		capX = capNode(x)
	}
	lo := zeroNode
	if o.I != nil {
		lo = valNode(o.I)
	}
	var hi node
	if o.Hi != nil {
		hi = valNode(o.Hi)
	} else if isStr {
		hi = lenNode(x)
	} else {
		hi = lenNode(x)
	}
	mx := capX
	if o.Max != nil {
		mx = valNode(o.Max)
	}
	ok := proveLE(s, zeroNode, lo, 0) && proveLE(s, lo, hi, 0) && proveLE(s, hi, mx, 0) && proveLE(s, mx, capX, 0)
	if ok {
		return verdict{proved: true}
	}
	// A guard mismatch on the upper end: s[:k] with k fixed and len(x)
	// guarded below k.
	if o.Hi != nil && proveLE(s, zeroNode, lo, 0) && proveLE(s, lo, hi, 0) && proveLE(s, mx, capX, 0) || o.Hi != nil && o.Max == nil && proveLE(s, zeroNode, lo, 0) && proveLE(s, lo, hi, 0) {
		if m := sliceMismatch(s, hi, capX); m != nil {
			return verdict{mismatch: m}
		}
	}
	if o.Hi == nil && o.I != nil && proveLE(s, zeroNode, lo, 0) {
		// s[k:] needs k <= len(s).
		if m := sliceMismatch(s, lo, lenNode(x)); m != nil {
			return verdict{mismatch: m}
		}
	}
	return verdict{}
}

// sliceMismatch: v <= l needed, v fixed or v - l guarded.
func sliceMismatch(s *state, v, l node) *mismatch {
	vlo, vhi := s.interval(v)
	if vlo == vhi && vlo != ninf {
		k := vlo
		negLo, why := s.upper(zeroNode, l)
		llo := neg(negLo)
		if validGuard(s, why, l) && llo < k && !otherGuards(s, l) {
			if !feasible(s, l, zeroNode, k-1) {
				return nil
			}
			return &mismatch{q: l, guard: why, lo: llo, hi: k - 1, needs: fmt.Sprintf(">= %d", k)}
		}
	}
	c, why := s.upper(v, l)
	if validGuard(s, why, v, l) && c >= 1 && c != inf && !otherGuards(s, v, l) && !otherGuards(s, l, v) {
		if !feasible(s, l, v, -1) {
			return nil
		}
		return &mismatch{q: v, other: l, guard: why, lo: 1, hi: c, needs: "<= 0"}
	}
	return nil
}

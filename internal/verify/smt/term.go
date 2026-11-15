// Package smt builds SMT-LIB2 formulas over fixed-width bit-vectors,
// booleans and byte arrays, prints them for a solver, and evaluates them
// on concrete values (for the encoder's differential tests and for
// checking models).
package smt

import (
	"fmt"
	"slices"
	"strings"
)

// Op is a term operator.
type Op uint8

const (
	OpConst  Op = iota // bit-vector constant (Val, W) or boolean (Val 0/1, W 0)
	OpVar              // a free variable (Name)
	OpArrVar           // a free byte array (BV64 -> BV8)
	OpSelect           // select array index
	OpAdd
	OpSub
	OpMul
	OpUDiv
	OpSDiv
	OpURem
	OpSRem
	OpAnd
	OpOr
	OpXor
	OpShl
	OpLShr
	OpAShr
	OpNeg
	OpBNot // bitwise not
	OpEq
	OpULt
	OpULe
	OpSLt
	OpSLe
	OpNot // boolean
	OpLAnd
	OpLOr
	OpIte
	OpZExt    // to W
	OpSExt    // to W
	OpExtract // low W bits
)

// T is a term. W is the bit width of a bit-vector term, 0 for booleans
// and -1 for arrays.
type T struct {
	Op   Op
	W    int
	Val  uint64
	Name string
	Args []*T
}

func mask(w int) uint64 {
	if w >= 64 {
		return ^uint64(0)
	}
	return 1<<uint(w) - 1
}

// BV is the w-bit constant v (truncated).
func BV(v uint64, w int) *T { return &T{Op: OpConst, W: w, Val: v & mask(w)} }

// Bool is a boolean constant.
func Bool(b bool) *T {
	if b {
		return &T{Op: OpConst, W: 0, Val: 1}
	}
	return &T{Op: OpConst, W: 0, Val: 0}
}

// Var is a w-bit (or boolean, w 0) variable.
func Var(name string, w int) *T { return &T{Op: OpVar, W: w, Name: name} }

// ArrVar is a byte array variable.
func ArrVar(name string) *T { return &T{Op: OpArrVar, W: -1, Name: name} }

func bin(op Op, a, b *T) *T {
	if a.W != b.W {
		panic(fmt.Sprintf("smt: width %d vs %d in %v", a.W, b.W, op))
	}
	w := a.W
	switch op {
	case OpEq, OpULt, OpULe, OpSLt, OpSLe:
		w = 0
	}
	return &T{Op: op, W: w, Args: []*T{a, b}}
}

func Add(a, b *T) *T  { return bin(OpAdd, a, b) }
func Sub(a, b *T) *T  { return bin(OpSub, a, b) }
func Mul(a, b *T) *T  { return bin(OpMul, a, b) }
func UDiv(a, b *T) *T { return bin(OpUDiv, a, b) }
func SDiv(a, b *T) *T { return bin(OpSDiv, a, b) }
func URem(a, b *T) *T { return bin(OpURem, a, b) }
func SRem(a, b *T) *T { return bin(OpSRem, a, b) }
func And(a, b *T) *T  { return bin(OpAnd, a, b) }
func Or(a, b *T) *T   { return bin(OpOr, a, b) }
func Xor(a, b *T) *T  { return bin(OpXor, a, b) }
func Shl(a, b *T) *T  { return bin(OpShl, a, b) }
func LShr(a, b *T) *T { return bin(OpLShr, a, b) }
func AShr(a, b *T) *T { return bin(OpAShr, a, b) }
func Eq(a, b *T) *T   { return bin(OpEq, a, b) }
func ULt(a, b *T) *T  { return bin(OpULt, a, b) }
func ULe(a, b *T) *T  { return bin(OpULe, a, b) }
func SLt(a, b *T) *T  { return bin(OpSLt, a, b) }
func SLe(a, b *T) *T  { return bin(OpSLe, a, b) }
func Neg(a *T) *T     { return &T{Op: OpNeg, W: a.W, Args: []*T{a}} }
func BNot(a *T) *T    { return &T{Op: OpBNot, W: a.W, Args: []*T{a}} }

// Not, LAnd, LOr are boolean connectives (constants fold).
func Not(a *T) *T {
	if a.Op == OpConst {
		return Bool(a.Val == 0)
	}
	return &T{Op: OpNot, Args: []*T{a}}
}

func LAnd(xs ...*T) *T {
	var args []*T
	for _, x := range xs {
		if x.Op == OpConst {
			if x.Val == 0 {
				return Bool(false)
			}
			continue
		}
		args = append(args, x)
	}
	switch len(args) {
	case 0:
		return Bool(true)
	case 1:
		return args[0]
	}
	return &T{Op: OpLAnd, Args: args}
}

func LOr(xs ...*T) *T {
	var args []*T
	for _, x := range xs {
		if x.Op == OpConst {
			if x.Val != 0 {
				return Bool(true)
			}
			continue
		}
		args = append(args, x)
	}
	switch len(args) {
	case 0:
		return Bool(false)
	case 1:
		return args[0]
	}
	return &T{Op: OpLOr, Args: args}
}

// Ite is if c then a else b.
func Ite(c, a, b *T) *T {
	if c.Op == OpConst {
		if c.Val != 0 {
			return a
		}
		return b
	}
	if a.W != b.W {
		panic("smt: ite widths differ")
	}
	return &T{Op: OpIte, W: a.W, Args: []*T{c, a, b}}
}

// ZExt, SExt widen a to w bits; Extract keeps its low w bits.
func ZExt(a *T, w int) *T {
	if a.W == w {
		return a
	}
	return &T{Op: OpZExt, W: w, Args: []*T{a}}
}

func SExt(a *T, w int) *T {
	if a.W == w {
		return a
	}
	return &T{Op: OpSExt, W: w, Args: []*T{a}}
}

func Extract(a *T, w int) *T {
	if a.W == w {
		return a
	}
	return &T{Op: OpExtract, W: w, Args: []*T{a}}
}

// Select reads byte i (BV64) of array a.
func Select(a, i *T) *T { return &T{Op: OpSelect, W: 8, Args: []*T{a, i}} }

// Env holds concrete values of variables: bit-vectors and byte arrays
// (bytes not present read as zero).
type Env struct {
	BV  map[string]uint64
	Arr map[string]map[uint64]byte
}

// Eval evaluates t in env with SMT-LIB semantics (division by zero gives
// all ones or the dividend, as the standard says; the encoder guards it).
func Eval(t *T, env *Env) uint64 {
	switch t.Op {
	case OpConst:
		return t.Val
	case OpVar:
		if t.W == 0 {
			return env.BV[t.Name] & 1
		}
		return env.BV[t.Name] & mask(t.W)
	case OpSelect:
		arr := t.Args[0]
		i := Eval(t.Args[1], env)
		return uint64(env.Arr[arr.Name][i])
	}
	if t.Op == OpNot {
		return 1 - Eval(t.Args[0], env)
	}
	if t.Op == OpLAnd {
		for _, a := range t.Args {
			if Eval(a, env) == 0 {
				return 0
			}
		}
		return 1
	}
	if t.Op == OpLOr {
		for _, a := range t.Args {
			if Eval(a, env) != 0 {
				return 1
			}
		}
		return 0
	}
	if t.Op == OpIte {
		if Eval(t.Args[0], env) != 0 {
			return Eval(t.Args[1], env)
		}
		return Eval(t.Args[2], env)
	}
	a := Eval(t.Args[0], env)
	w := t.Args[0].W
	m := mask(w)
	sx := func(v uint64) int64 { // sign extend from w
		if w >= 64 {
			return int64(v)
		}
		s := uint(64 - w)
		return int64(v<<s) >> s
	}
	switch t.Op {
	case OpNeg:
		return (-a) & m
	case OpBNot:
		return ^a & m
	case OpZExt:
		return a
	case OpSExt:
		return uint64(sx(a)) & mask(t.W)
	case OpExtract:
		return a & mask(t.W)
	}
	b := Eval(t.Args[1], env)
	switch t.Op {
	case OpAdd:
		return (a + b) & m
	case OpSub:
		return (a - b) & m
	case OpMul:
		return (a * b) & m
	case OpUDiv:
		if b == 0 {
			return m
		}
		return a / b
	case OpURem:
		if b == 0 {
			return a
		}
		return a % b
	case OpSDiv:
		x, y := sx(a), sx(b)
		if y == 0 {
			if x < 0 {
				return 1
			}
			return m
		}
		if y == -1 {
			return uint64(-x) & m
		}
		return uint64(x/y) & m
	case OpSRem:
		x, y := sx(a), sx(b)
		if y == 0 {
			return a
		}
		if y == -1 {
			return 0
		}
		return uint64(x%y) & m
	case OpAnd:
		return a & b
	case OpOr:
		return a | b
	case OpXor:
		return a ^ b
	case OpShl:
		if b >= uint64(w) {
			return 0
		}
		return (a << b) & m
	case OpLShr:
		if b >= uint64(w) {
			return 0
		}
		return a >> b
	case OpAShr:
		if b >= uint64(w) {
			if sx(a) < 0 {
				return m
			}
			return 0
		}
		return uint64(sx(a)>>b) & m
	case OpEq:
		return b2u(a == b)
	case OpULt:
		return b2u(a < b)
	case OpULe:
		return b2u(a <= b)
	case OpSLt:
		return b2u(sx(a) < sx(b))
	case OpSLe:
		return b2u(sx(a) <= sx(b))
	}
	panic(fmt.Sprintf("smt: eval of op %d", t.Op))
}

func b2u(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

var opNames = map[Op]string{
	OpAdd: "bvadd", OpSub: "bvsub", OpMul: "bvmul", OpUDiv: "bvudiv", OpSDiv: "bvsdiv",
	OpURem: "bvurem", OpSRem: "bvsrem", OpAnd: "bvand", OpOr: "bvor", OpXor: "bvxor",
	OpShl: "bvshl", OpLShr: "bvlshr", OpAShr: "bvashr", OpNeg: "bvneg", OpBNot: "bvnot",
	OpEq: "=", OpULt: "bvult", OpULe: "bvule", OpSLt: "bvslt", OpSLe: "bvsle",
	OpNot: "not", OpLAnd: "and", OpLOr: "or", OpIte: "ite", OpSelect: "select",
}

// Script is an SMT-LIB2 script: declarations of the variables of the
// asserted terms, the assertions, check-sat and get-value of vars.
func Script(asserts []*T, rlimit int) string {
	vars := map[string]*T{}
	var walk func(t *T)
	walk = func(t *T) {
		if t.Op == OpVar || t.Op == OpArrVar {
			vars[t.Name] = t
		}
		for _, a := range t.Args {
			walk(a)
		}
	}
	for _, a := range asserts {
		walk(a)
	}
	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	slices.Sort(names)
	var b strings.Builder
	b.WriteString("(set-logic QF_ABV)\n(set-option :produce-models true)\n")
	if rlimit > 0 {
		fmt.Fprintf(&b, "(set-option :rlimit %d)\n", rlimit)
	}
	for _, n := range names {
		v := vars[n]
		switch {
		case v.Op == OpArrVar:
			fmt.Fprintf(&b, "(declare-const %s (Array (_ BitVec 64) (_ BitVec 8)))\n", n)
		case v.W == 0:
			fmt.Fprintf(&b, "(declare-const %s Bool)\n", n)
		default:
			fmt.Fprintf(&b, "(declare-const %s (_ BitVec %d))\n", n, v.W)
		}
	}
	for _, a := range asserts {
		fmt.Fprintf(&b, "(assert %s)\n", String(a))
	}
	b.WriteString("(check-sat)\n")
	var bvs []string
	for _, n := range names {
		if vars[n].Op == OpVar {
			bvs = append(bvs, n)
		}
	}
	if len(bvs) > 0 {
		fmt.Fprintf(&b, "(get-value (%s))\n", strings.Join(bvs, " "))
	}
	return b.String()
}

// String prints t in SMT-LIB2 syntax.
func String(t *T) string {
	switch t.Op {
	case OpConst:
		if t.W == 0 {
			if t.Val != 0 {
				return "true"
			}
			return "false"
		}
		return fmt.Sprintf("(_ bv%d %d)", t.Val, t.W)
	case OpVar, OpArrVar:
		return t.Name
	case OpZExt:
		return fmt.Sprintf("((_ zero_extend %d) %s)", t.W-t.Args[0].W, String(t.Args[0]))
	case OpSExt:
		return fmt.Sprintf("((_ sign_extend %d) %s)", t.W-t.Args[0].W, String(t.Args[0]))
	case OpExtract:
		return fmt.Sprintf("((_ extract %d 0) %s)", t.W-1, String(t.Args[0]))
	}
	parts := []string{opNames[t.Op]}
	for _, a := range t.Args {
		parts = append(parts, String(a))
	}
	return "(" + strings.Join(parts, " ") + ")"
}

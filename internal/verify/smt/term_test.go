package smt

import (
	"context"
	"math/rand/v2"
	"strings"
	"testing"
)

// TestEvalMatchesGo checks the evaluator against Go's own arithmetic on
// random 8, 16, 32 and 64 bit values (Go wraps like SMT bit-vectors; the
// cases where they differ, division by zero and shifts, are what the
// encoder guards).
func TestEvalMatchesGo(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 20000; i++ {
		x, y := r.Uint64(), r.Uint64()
		if i%3 == 0 {
			y = uint64(r.IntN(70))
		}
		for _, w := range []int{8, 16, 32, 64} {
			a, b := BV(x, w), BV(y, w)
			env := &Env{}
			m := mask(w)
			xa, ya := x&m, y&m
			sx := func(v uint64) int64 { s := uint(64 - w); return int64(v<<s) >> s }
			check := func(name string, got, want uint64) {
				if got != want&m {
					t.Fatalf("%s w=%d x=%#x y=%#x: got %#x want %#x", name, w, xa, ya, got, want&m)
				}
			}
			check("add", Eval(Add(a, b), env), xa+ya)
			check("sub", Eval(Sub(a, b), env), xa-ya)
			check("mul", Eval(Mul(a, b), env), xa*ya)
			if ya != 0 {
				check("udiv", Eval(UDiv(a, b), env), xa/ya)
				check("urem", Eval(URem(a, b), env), xa%ya)
				// Go: MinInt / -1 wraps to MinInt, % gives 0.
				q, rr := sx(xa)/sx(ya), sx(xa)%sx(ya)
				if sx(ya) == -1 {
					q, rr = -sx(xa), 0
				}
				check("sdiv", Eval(SDiv(a, b), env), uint64(q))
				check("srem", Eval(SRem(a, b), env), uint64(rr))
			}
			if ya < uint64(w) {
				check("shl", Eval(Shl(a, b), env), xa<<ya)
				check("lshr", Eval(LShr(a, b), env), xa>>ya)
				check("ashr", Eval(AShr(a, b), env), uint64(sx(xa)>>ya))
			}
			check("slt", Eval(SLt(a, b), env), b2u(sx(xa) < sx(ya)))
			check("ult", Eval(ULt(a, b), env), b2u(xa < ya))
		}
	}
}

func TestZ3(t *testing.T) {
	s := Z3()
	if s == nil {
		t.Skip("z3 not on PATH")
	}
	x, y := Var("x", 64), Var("y", 8)
	// x + 1 < x (signed) holds only for MaxInt64.
	r := s.Check(context.Background(), []*T{SLt(Add(x, BV(1, 64)), x)})
	if r.Answer != Sat || r.Model["x"] != 1<<63-1 {
		t.Fatalf("got %+v", r)
	}
	// y * 2 == 1 has no 8-bit solution.
	if r := s.Check(context.Background(), []*T{Eq(Mul(y, BV(2, 8)), BV(1, 8))}); r.Answer != Unsat {
		t.Fatalf("got %+v", r)
	}
	// A model checks out on the evaluator.
	r = s.Check(context.Background(), []*T{Eq(Mul(y, BV(3, 8)), BV(1, 8))})
	if r.Answer != Sat || Eval(Mul(y, BV(3, 8)), &Env{BV: r.Model}) != 1 {
		t.Fatalf("got %+v", r)
	}
	t.Logf("%s", s.Version)
	// The same three queries in one process.
	rs := s.CheckMany(context.Background(), nil, []*T{SLt(Add(x, BV(1, 64)), x), Eq(Mul(y, BV(2, 8)), BV(1, 8)), Eq(Mul(y, BV(3, 8)), BV(1, 8))})
	if rs[0].Answer != Sat || rs[0].Model["x"] != 1<<63-1 || rs[1].Answer != Unsat || rs[2].Answer != Sat || Eval(Mul(y, BV(3, 8)), &Env{BV: rs[2].Model}) != 1 {
		t.Fatalf("CheckMany: %+v", rs)
	}
}

func TestScript(t *testing.T) {
	x := Var("x", 64)
	s := Script([]*T{SLt(Add(x, BV(1, 64)), x)}, 1000)
	for _, want := range []string{"(declare-const x (_ BitVec 64))", "(assert (bvslt (bvadd x (_ bv1 64)) x))", "(check-sat)", "(get-value (x))", ":rlimit 1000"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
}

// TestScriptShared prints a DAG whose tree doubles per level: each level
// uses the one before three times. The script must stay linear (shared
// subterms defined once), and the solver must take it, alone and in
// CheckMany's push and pop scopes.
func TestScriptShared(t *testing.T) {
	x := Var("x", 64)
	v := Var("v", 64)
	for range 80 {
		v = Ite(SLt(v, x), Add(v, BV(1, 64)), Sub(v, BV(1, 64)))
	}
	q := Eq(v, BV(7, 64))
	s := Script([]*T{q}, 0)
	if len(s) > 64<<10 {
		t.Fatalf("script of %d bytes for 80 levels", len(s))
	}
	if !strings.Contains(s, "(define-fun |t0| () (_ BitVec 64) ") {
		t.Errorf("no shared definition in\n%s", s[:min(len(s), 2000)])
	}
	z := Z3()
	if z == nil {
		t.Skip("no z3")
	}
	t.Setenv("MARMOT_VERIFY_SMT_CACHE", t.TempDir())
	if r := z.Check(context.Background(), []*T{q}); r.Answer == Unknown && strings.HasPrefix(r.Reason, "solver:") {
		t.Errorf("Check: %v", r)
	}
	// Two queries that share subterms the base does not have: each push
	// defines its own, and the pop forgets them.
	rs := z.CheckMany(context.Background(), []*T{ULt(x, BV(100, 64))}, []*T{q, Eq(v, BV(9, 64))})
	for i, r := range rs {
		if r.Answer == Unknown && r.Reason != "budget" {
			t.Errorf("CheckMany query %d: %v", i, r)
		}
	}
}

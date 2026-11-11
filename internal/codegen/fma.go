package codegen

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Fused multiply-add, matching gc exactly (SPEC 3a rules): on arm64 gc
// rewrites a+x*y, a-x*y, x*y-a and the negated forms into FMADD, FMSUB,
// FNMSUB, FNMADD (cmd/compile/internal/ssa/_gen/ARM64.rules), with no
// single-use condition. An explicit conversion float64(x*y) rounds the
// product (gc inserts a rounding op), which blocks fusion. On other
// targets nothing is fused and the contract flag is never set.

// collectFMABlocks records the operator positions of float multiplications
// that appear directly under an explicit conversion to a float type.
func (p *Program) collectFMABlocks() {
	p.fmaBlocked = map[token.Pos]bool{}
	if p.opts.GOARCH != "arm64" {
		return
	}
	for pkg, files := range p.files {
		info := p.infos[pkg]
		if info == nil {
			continue
		}
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				tv, ok := info.Types[call.Fun]
				if !ok || !tv.IsType() || !isFloat(tv.Type) {
					return true
				}
				arg := ast.Unparen(call.Args[0])
				if be, ok := arg.(*ast.BinaryExpr); ok && be.Op == token.MUL {
					p.fmaBlocked[be.OpPos] = true
				}
				return true
			})
		}
	}
}

// fmaMul reports whether v is a fusable float multiplication of type t.
func (f *funcGen) fmaMul(v ssa.Value, t types.Type) (x, y ssa.Value, ok bool) {
	b, ok := v.(*ssa.BinOp)
	if !ok || b.Op != token.MUL || !types.Identical(b.Type(), t) || f.m.p.fmaBlocked[b.Pos()] {
		return nil, nil, false
	}
	// gc folds a product of constants (generic rules run before the arm64
	// fusion rules), so it is never fused.
	_, xc := b.X.(*ssa.Const)
	_, yc := b.Y.(*ssa.Const)
	if xc && yc {
		return nil, nil, false
	}
	return b.X, b.Y, true
}

// fmaNegMul reports whether v is -(x*y) (gc's FNMUL).
func (f *funcGen) fmaNegMul(v ssa.Value, t types.Type) (x, y ssa.Value, ok bool) {
	u, ok := v.(*ssa.UnOp)
	if !ok || u.Op != token.SUB {
		return nil, nil, false
	}
	return f.fmaMul(u.X, t)
}

// tryFMA emits a fused multiply-add for X op Y when gc would.
func (f *funcGen) tryFMA(op token.Token, X, Y ssa.Value, t types.Type) (string, bool) {
	if f.m.p.opts.GOARCH != "arm64" || !isFloat(t) || (op != token.ADD && op != token.SUB) {
		return "", false
	}
	vt := f.tm.valType(t)
	neg := func(v string) string { return f.op("llvm.fneg %s : %s", v, vt) }
	fma := func(x, y, z string) string {
		return f.op("llvm.intr.fma(%s, %s, %s) : (%s, %s, %s) -> %s", x, y, z, vt, vt, vt, vt)
	}
	if op == token.ADD {
		// FADD is commutative: gc tries the multiplication as the second
		// operand first.
		for _, pair := range [][2]ssa.Value{{X, Y}, {Y, X}} {
			a, m := pair[0], pair[1]
			if x, y, ok := f.fmaMul(m, t); ok {
				return fma(f.value(x), f.value(y), f.value(a)), true // FMADD
			}
		}
		for _, pair := range [][2]ssa.Value{{X, Y}, {Y, X}} {
			a, m := pair[0], pair[1]
			if x, y, ok := f.fmaNegMul(m, t); ok {
				return fma(neg(f.value(x)), f.value(y), f.value(a)), true // FMSUB: a - x*y
			}
		}
		return "", false
	}
	// X - Y, rules in gc's order.
	if x, y, ok := f.fmaMul(Y, t); ok {
		return fma(neg(f.value(x)), f.value(y), f.value(X)), true // FMSUB: a - x*y
	}
	if x, y, ok := f.fmaMul(X, t); ok {
		return fma(f.value(x), f.value(y), neg(f.value(Y))), true // FNMSUB: x*y - a
	}
	if x, y, ok := f.fmaNegMul(Y, t); ok {
		return fma(f.value(x), f.value(y), f.value(X)), true // FMADD: a - (-(x*y))
	}
	if x, y, ok := f.fmaNegMul(X, t); ok {
		return fma(neg(f.value(x)), f.value(y), neg(f.value(Y))), true // FNMADD: -(x*y) - a
	}
	return "", false
}

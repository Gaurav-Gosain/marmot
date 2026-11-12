package codegen

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

func (f *funcGen) binop(op token.Token, X, Y ssa.Value, t types.Type) string {
	switch op {
	case token.EQL, token.NEQ:
		// Comparison against a nil interface, slice, map, ...: check the
		// first word only.
		if c, ok := Y.(*ssa.Const); ok && c.IsNil() {
			return f.cmpNil(op, f.value(X), X.Type())
		}
		if c, ok := X.(*ssa.Const); ok && c.IsNil() {
			return f.cmpNil(op, f.value(Y), Y.Type())
		}
		eq := f.equal(f.value(X), f.value(Y), t)
		if op == token.NEQ {
			return f.not(eq)
		}
		return eq
	case token.SHL, token.SHR:
		return f.shift(op, f.value(X), f.value(Y), t, Y)
	}
	if r, ok := f.tryFMA(op, X, Y, t); ok {
		return r
	}
	x, y := f.value(X), f.value(Y)
	vt := f.tm.valType(t)
	switch {
	case isInteger(t):
		signed := isSigned(t)
		switch op {
		case token.ADD:
			return f.op("llvm.add %s, %s : %s", x, y, vt)
		case token.SUB:
			return f.op("llvm.sub %s, %s : %s", x, y, vt)
		case token.MUL:
			return f.op("llvm.mul %s, %s : %s", x, y, vt)
		case token.QUO, token.REM:
			return f.divide(op, x, y, vt, signed, Y)
		case token.AND:
			return f.op("llvm.and %s, %s : %s", x, y, vt)
		case token.OR:
			return f.op("llvm.or %s, %s : %s", x, y, vt)
		case token.XOR:
			return f.op("llvm.xor %s, %s : %s", x, y, vt)
		case token.AND_NOT:
			m1 := f.constInt(-1, vt)
			ny := f.op("llvm.xor %s, %s : %s", y, m1, vt)
			return f.op("llvm.and %s, %s : %s", x, ny, vt)
		case token.LSS, token.LEQ, token.GTR, token.GEQ:
			return f.op("llvm.icmp %q %s, %s : %s", intPred(op, signed), x, y, vt)
		}
	case isFloat(t):
		switch op {
		case token.ADD:
			return f.op("llvm.fadd %s, %s : %s", x, y, vt)
		case token.SUB:
			return f.op("llvm.fsub %s, %s : %s", x, y, vt)
		case token.MUL:
			return f.op("llvm.fmul %s, %s : %s", x, y, vt)
		case token.QUO:
			return f.op("llvm.fdiv %s, %s : %s", x, y, vt)
		case token.LSS, token.LEQ, token.GTR, token.GEQ:
			return f.op("llvm.fcmp %q %s, %s : %s", floatPred(op), x, y, vt)
		}
	case isComplex(t):
		return f.complexOp(op, x, y, vt)
	case isString(t):
		switch op {
		case token.ADD:
			if f.goOps() {
				return f.op("go.str_concat %s, %s", x, y)
			}
			return f.rtcall("concatstring2", x, y)
		case token.LSS, token.LEQ, token.GTR, token.GEQ:
			c := f.rtcall("cmpstring", x, y)
			z := f.constInt(0, mlir.I64)
			return f.op("llvm.icmp %q %s, %s : i64", intPred(op, true), c, z)
		}
	case isBool(t):
		switch op {
		case token.AND, token.LAND:
			return f.op("llvm.and %s, %s : i1", x, y)
		case token.OR, token.LOR:
			return f.op("llvm.or %s, %s : i1", x, y)
		}
	}
	f.fail(token.NoPos, "unsupported binary op %s on %v", op, t)
	return ""
}

func intPred(op token.Token, signed bool) string {
	p := map[token.Token]string{token.LSS: "lt", token.LEQ: "le", token.GTR: "gt", token.GEQ: "ge"}[op]
	if signed {
		return "s" + p
	}
	return "u" + p
}

func floatPred(op token.Token) string {
	return map[token.Token]string{token.LSS: "olt", token.LEQ: "ole", token.GTR: "ogt", token.GEQ: "oge", token.EQL: "oeq", token.NEQ: "une"}[op]
}

func (f *funcGen) not(b string) string {
	one := f.op("llvm.mlir.constant(true) : i1")
	return f.op("llvm.xor %s, %s : i1", b, one)
}

func (f *funcGen) and(a, b string) string { return f.op("llvm.and %s, %s : i1", a, b) }

// cmpNil compares the first word of x (a pointer-like, slice, interface or
// func value) with nil.
func (f *funcGen) cmpNil(op token.Token, x string, t types.Type) string {
	vt := f.tm.valType(t)
	p := x
	if vt != mlir.Ptr {
		p = f.extractRaw(x, vt, 0)
	}
	n := f.null()
	pred := "eq"
	if op == token.NEQ {
		pred = "ne"
	}
	return f.op("llvm.icmp %q %s, %s : !llvm.ptr", pred, p, n)
}

// equal returns x == y for comparable type t, as i1.
func (f *funcGen) equal(x, y string, t types.Type) string {
	vt := f.tm.valType(t)
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsFloat != 0:
			return f.op("llvm.fcmp \"oeq\" %s, %s : %s", x, y, vt)
		case u.Info()&types.IsComplex != 0:
			et := complexElem(vt)
			re := f.op("llvm.fcmp \"oeq\" %s, %s : %s", f.extractRaw(x, vt, 0), f.extractRaw(y, vt, 0), et)
			im := f.op("llvm.fcmp \"oeq\" %s, %s : %s", f.extractRaw(x, vt, 1), f.extractRaw(y, vt, 1), et)
			return f.and(re, im)
		case u.Info()&types.IsString != 0:
			return f.stringEqual(x, y)
		default:
			return f.op("llvm.icmp \"eq\" %s, %s : %s", x, y, vt)
		}
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return f.op("llvm.icmp \"eq\" %s, %s : !llvm.ptr", x, y)
	case *types.Interface:
		if u.NumMethods() == 0 {
			return f.rtcall("efaceeq", x, y)
		}
		return f.rtcall("ifaceeq", x, y)
	case *types.Struct, *types.Array:
		if f.tm.isBig(t) {
			return f.rtcall("typeequal", f.typeDesc(t), x, y)
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		res := f.op("llvm.mlir.constant(true) : i1")
		for i := 0; i < u.NumFields(); i++ {
			ft := u.Field(i).Type()
			if u.Field(i).Name() == "_" {
				continue
			}
			a := f.extract(x, vt, i, ft)
			b := f.extract(y, vt, i, ft)
			res = f.and(res, f.equal(a, b, ft))
		}
		return res
	case *types.Array:
		res := f.op("llvm.mlir.constant(true) : i1")
		if u.Len() > 64 {
			f.fail(token.NoPos, "comparison of large arrays (%v) is not supported yet", t)
		}
		for i := 0; i < int(u.Len()); i++ {
			a := f.extract(x, vt, i, u.Elem())
			b := f.extract(y, vt, i, u.Elem())
			res = f.and(res, f.equal(a, b, u.Elem()))
		}
		return res
	}
	f.fail(token.NoPos, "unsupported comparison of %v", t)
	return ""
}

func (f *funcGen) stringEqual(x, y string) string {
	lx := f.extractRaw(x, tString, 1)
	ly := f.extractRaw(y, tString, 1)
	sameLen := f.op("llvm.icmp \"eq\" %s, %s : i64", lx, ly)
	// Compare bytes only when the lengths match.
	cmpB, done := f.newBlock(), f.newBlock()
	fls := f.op("llvm.mlir.constant(false) : i1")
	f.emit("llvm.cond_br %s, %s, %s(%s : i1)", sameLen, cmpB, done, fls)
	fmt.Fprintf(f.b, "  %s:\n", cmpB)
	c := f.rtcall("cmpstring", x, y)
	z := f.constInt(0, mlir.I64)
	eq := f.op("llvm.icmp \"eq\" %s, %s : i64", c, z)
	f.emit("llvm.br %s(%s : i1)", done, eq)
	r := f.newVal()
	fmt.Fprintf(f.b, "  %s(%s: i1):\n", done, r)
	return r
}

// divide implements Go integer / and %: panics on a zero divisor, and
// MinInt / -1 wraps (MinInt % -1 is 0) instead of trapping.
func (f *funcGen) divide(op token.Token, x, y, vt string, signed bool, Y ssa.Value) string {
	var cy *constant.Value
	if c, ok := Y.(*ssa.Const); ok && c.Value != nil {
		cy = &c.Value
	}
	if cy == nil || constant.Sign(*cy) == 0 {
		z := f.constInt(0, vt)
		isZero := f.op("llvm.icmp \"eq\" %s, %s : %s", y, z, vt)
		f.branchCold(isZero, func() {
			f.rtcall("panicdivide")
			f.emit("llvm.unreachable")
		})
	}
	if !signed {
		if op == token.QUO {
			return f.op("llvm.udiv %s, %s : %s", x, y, vt)
		}
		return f.op("llvm.urem %s, %s : %s", x, y, vt)
	}
	if cy != nil {
		if v, ok := constant.Int64Val(*cy); ok && v != -1 {
			if op == token.QUO {
				return f.op("llvm.sdiv %s, %s : %s", x, y, vt)
			}
			return f.op("llvm.srem %s, %s : %s", x, y, vt)
		}
	}
	// y == -1: x / -1 == -x (wrapping), x % -1 == 0.
	m1 := f.constInt(-1, vt)
	isM1 := f.op("llvm.icmp \"eq\" %s, %s : %s", y, m1, vt)
	neg, norm, done := f.newBlock(), f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", isM1, neg, norm)
	fmt.Fprintf(f.b, "  %s:\n", neg)
	var r1 string
	z := f.constInt(0, vt)
	if op == token.QUO {
		r1 = f.op("llvm.sub %s, %s : %s", z, x, vt)
	} else {
		r1 = z
	}
	f.emit("llvm.br %s(%s : %s)", done, r1, vt)
	fmt.Fprintf(f.b, "  %s:\n", norm)
	var r2 string
	if op == token.QUO {
		r2 = f.op("llvm.sdiv %s, %s : %s", x, y, vt)
	} else {
		r2 = f.op("llvm.srem %s, %s : %s", x, y, vt)
	}
	f.emit("llvm.br %s(%s : %s)", done, r2, vt)
	r := f.newVal()
	fmt.Fprintf(f.b, "  %s(%s: %s):\n", done, r, vt)
	return r
}

// shift implements Go shifts: counts at or above the width give 0 (or the
// sign fill for signed >>), and negative signed counts panic.
func (f *funcGen) shift(op token.Token, x, y string, t types.Type, Y ssa.Value) string {
	vt := f.tm.valType(t)
	w := intBits(vt)
	yt := Y.Type()
	yvt := f.tm.valType(yt)
	yw := intBits(yvt)
	signed := isSigned(t)
	if c, ok := Y.(*ssa.Const); ok && c.Value != nil {
		n, exact := constant.Uint64Val(constant.ToInt(c.Value))
		if exact && n < uint64(w) {
			cnt := f.constInt(int64(n), vt)
			return f.shiftOp(op, signed, x, cnt, vt)
		}
		if exact {
			if op == token.SHR && signed {
				cnt := f.constInt(int64(w-1), vt)
				return f.shiftOp(op, signed, x, cnt, vt)
			}
			return f.constInt(0, vt)
		}
	}
	if isSigned(yt) {
		z := f.constInt(0, yvt)
		neg := f.op("llvm.icmp \"slt\" %s, %s : %s", y, z, yvt)
		f.branchCold(neg, func() {
			f.rtcall("panicshift")
			f.emit("llvm.unreachable")
		})
	}
	// Bring the count to the width of x, saturating to w when it is larger.
	var cnt string
	wc := f.constInt(int64(w), yvt)
	if yw > w {
		big := f.op("llvm.icmp \"uge\" %s, %s : %s", y, wc, yvt)
		tr := f.op("llvm.trunc %s : %s to %s", y, yvt, vt)
		wv := f.constInt(int64(w), vt)
		cnt = f.op("llvm.select %s, %s, %s : i1, %s", big, wv, tr, vt)
	} else if yw < w {
		cnt = f.op("llvm.zext %s : %s to %s", y, yvt, vt)
	} else {
		cnt = y
	}
	wv := f.constInt(int64(w), vt)
	big := f.op("llvm.icmp \"uge\" %s, %s : %s", cnt, wv, vt)
	if op == token.SHR && signed {
		wm1 := f.constInt(int64(w-1), vt)
		c := f.op("llvm.select %s, %s, %s : i1, %s", big, wm1, cnt, vt)
		return f.shiftOp(op, signed, x, c, vt)
	}
	z := f.constInt(0, vt)
	safe := f.op("llvm.select %s, %s, %s : i1, %s", big, z, cnt, vt)
	r := f.shiftOp(op, signed, x, safe, vt)
	return f.op("llvm.select %s, %s, %s : i1, %s", big, z, r, vt)
}

func (f *funcGen) shiftOp(op token.Token, signed bool, x, cnt, vt string) string {
	switch {
	case op == token.SHL:
		return f.op("llvm.shl %s, %s : %s", x, cnt, vt)
	case signed:
		return f.op("llvm.ashr %s, %s : %s", x, cnt, vt)
	default:
		return f.op("llvm.lshr %s, %s : %s", x, cnt, vt)
	}
}

func (f *funcGen) complexOp(op token.Token, x, y, vt string) string {
	et := complexElem(vt)
	a, b := f.extractRaw(x, vt, 0), f.extractRaw(x, vt, 1)
	c, d := f.extractRaw(y, vt, 0), f.extractRaw(y, vt, 1)
	switch op {
	case token.ADD:
		return f.build(vt, f.op("llvm.fadd %s, %s : %s", a, c, et), f.op("llvm.fadd %s, %s : %s", b, d, et))
	case token.SUB:
		return f.build(vt, f.op("llvm.fsub %s, %s : %s", a, c, et), f.op("llvm.fsub %s, %s : %s", b, d, et))
	case token.MUL:
		// As gc: computed in float64 (complex64 too, rounded at the end);
		// on arm64 the second product of each part is fused:
		// real = fma(-b, d, a*c), imag = fma(b, c, a*d).
		if et == mlir.F32 {
			a, b = f.op("llvm.fpext %s : f32 to f64", a), f.op("llvm.fpext %s : f32 to f64", b)
			c, d = f.op("llvm.fpext %s : f32 to f64", c), f.op("llvm.fpext %s : f32 to f64", d)
		}
		ac := f.op("llvm.fmul %s, %s : f64", a, c)
		ad := f.op("llvm.fmul %s, %s : f64", a, d)
		var re, im string
		if f.m.p.opts.GOARCH == "arm64" {
			nb := f.op("llvm.fneg %s : f64", b)
			re = f.op("llvm.intr.fma(%s, %s, %s) : (f64, f64, f64) -> f64", nb, d, ac)
			im = f.op("llvm.intr.fma(%s, %s, %s) : (f64, f64, f64) -> f64", b, c, ad)
		} else {
			bd := f.op("llvm.fmul %s, %s : f64", b, d)
			bc := f.op("llvm.fmul %s, %s : f64", b, c)
			re = f.op("llvm.fsub %s, %s : f64", ac, bd)
			im = f.op("llvm.fadd %s, %s : f64", ad, bc)
		}
		if et == mlir.F32 {
			re, im = f.op("llvm.fptrunc %s : f64 to f32", re), f.op("llvm.fptrunc %s : f64 to f32", im)
		}
		return f.build(vt, re, im)
	case token.QUO:
		// The runtime implements Go's complex division (inf and NaN rules).
		if et == mlir.F32 {
			x64 := f.build(tComplex128, f.op("llvm.fpext %s : f32 to f64", a), f.op("llvm.fpext %s : f32 to f64", b))
			y64 := f.build(tComplex128, f.op("llvm.fpext %s : f32 to f64", c), f.op("llvm.fpext %s : f32 to f64", d))
			r := f.rtcall("complex128div", x64, y64)
			re := f.op("llvm.fptrunc %s : f64 to f32", f.extractRaw(r, tComplex128, 0))
			im := f.op("llvm.fptrunc %s : f64 to f32", f.extractRaw(r, tComplex128, 1))
			return f.build(vt, re, im)
		}
		return f.rtcall("complex128div", x, y)
	}
	f.fail(token.NoPos, "unsupported complex op %s", op)
	return ""
}

var tComplex128 = mlir.Struct(mlir.F64, mlir.F64)

// readOnlyBytes reports whether []byte(s) conversion v is only read
// (len, cap, element loads): then it can alias the string's bytes, as gc
// does for range loops over []byte(s).
func readOnlyBytes(v *ssa.Convert) bool {
	for _, r := range *v.Referrers() {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case *ssa.Call:
			bi, ok := x.Call.Value.(*ssa.Builtin)
			if !ok || (bi.Name() != "len" && bi.Name() != "cap") {
				return false
			}
		case *ssa.IndexAddr:
			for _, rr := range *x.Referrers() {
				if u, ok := rr.(*ssa.UnOp); !ok || u.Op != token.MUL {
					if _, dbg := rr.(*ssa.DebugRef); !dbg {
						return false
					}
				}
			}
		default:
			return false
		}
	}
	return true
}

// tempString reports whether string(b) conversion v is only used as a map
// lookup key or in comparisons, where a temporary view of b is enough
// (gc's slicebytetostringtmp).
func tempString(v *ssa.Convert) bool {
	// The view aliases b: every use must follow the conversion in the same
	// block with nothing in between that may write memory
	// (s := string(b); b[0] = 'x'; s == "abc" must see the old bytes).
	b := v.Block()
	idx := -1
	for i, x := range b.Instrs {
		if x == ssa.Instruction(v) {
			idx = i
			break
		}
	}
	for _, r := range *v.Referrers() {
		if _, dbg := r.(*ssa.DebugRef); dbg {
			continue
		}
		if r.Block() != b {
			return false
		}
		for k := idx + 1; k < len(b.Instrs) && b.Instrs[k] != r; k++ {
			switch b.Instrs[k].(type) {
			case *ssa.Store, *ssa.Call, *ssa.MapUpdate, *ssa.Send, *ssa.Go, *ssa.Defer, *ssa.Select, *ssa.Panic:
				return false
			}
		}
	}
	for _, r := range *v.Referrers() {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case *ssa.Lookup:
			if x.Index != ssa.Value(v) {
				return false
			}
		case *ssa.BinOp:
			switch x.Op {
			case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			default:
				return false
			}
		default:
			return false
		}
	}
	return true
}

// convert implements the Convert instruction.
func (f *funcGen) convert(x string, from, to types.Type) string {
	ft, tt := f.tm.valType(from), f.tm.valType(to)
	switch {
	case isInteger(from) && isInteger(to):
		fw, tw := intBits(ft), intBits(tt)
		switch {
		case fw == tw:
			return x
		case fw > tw:
			return f.op("llvm.trunc %s : %s to %s", x, ft, tt)
		case isSigned(from):
			return f.op("llvm.sext %s : %s to %s", x, ft, tt)
		default:
			return f.op("llvm.zext %s : %s to %s", x, ft, tt)
		}
	case isInteger(from) && isFloat(to):
		if isSigned(from) {
			return f.op("llvm.sitofp %s : %s to %s", x, ft, tt)
		}
		return f.op("llvm.uitofp %s : %s to %s", x, ft, tt)
	case isFloat(from) && isInteger(to):
		return f.floatToInt(x, ft, tt, isSigned(to))
	case isFloat(from) && isFloat(to):
		switch {
		case ft == tt:
			return x
		case ft == mlir.F32:
			return f.op("llvm.fpext %s : f32 to f64", x)
		default:
			return f.op("llvm.fptrunc %s : f64 to f32", x)
		}
	case isComplex(from) && isComplex(to):
		if ft == tt {
			return x
		}
		fe, te := complexElem(ft), complexElem(tt)
		cv := "llvm.fpext"
		if fe == mlir.F64 {
			cv = "llvm.fptrunc"
		}
		re := f.op("%s %s : %s to %s", cv, f.extractRaw(x, ft, 0), fe, te)
		im := f.op("%s %s : %s to %s", cv, f.extractRaw(x, ft, 1), fe, te)
		return f.build(tt, re, im)
	case isString(to):
		switch fu := from.Underlying().(type) {
		case *types.Basic:
			if fu.Info()&types.IsInteger != 0 {
				v := x
				if w := intBits(ft); w < 64 {
					if isSigned(from) {
						v = f.op("llvm.sext %s : %s to i64", x, ft)
					} else {
						v = f.op("llvm.zext %s : %s to i64", x, ft)
					}
				}
				return f.rtcall("intstring", v)
			}
			if fu.Info()&types.IsString != 0 {
				return x
			}
		case *types.Slice:
			if isRuneElem(fu.Elem()) {
				return f.rtcall("slicerunetostring", x)
			}
			if f.goOps() {
				return f.op("go.bytes_to_str %s", x)
			}
			return f.rtcall("slicebytetostring", x)
		}
	case isString(from):
		if s, ok := to.Underlying().(*types.Slice); ok {
			if isRuneElem(s.Elem()) {
				return f.rtcall("stringtoslicerune", x)
			}
			if f.goOps() {
				return f.op("go.str_to_bytes %s", x)
			}
			return f.rtcall("stringtoslicebyte", x)
		}
	case isUnsafePointer(from) && isInteger(to):
		return f.op("llvm.ptrtoint %s : !llvm.ptr to %s", x, tt)
	case isInteger(from) && isUnsafePointer(to):
		return f.op("llvm.inttoptr %s : %s to !llvm.ptr", x, ft)
	case ft == mlir.Ptr && tt == mlir.Ptr:
		return x
	}
	f.fail(token.NoPos, "unsupported conversion from %v to %v", from, to)
	return ""
}

func isRuneElem(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Int32
}

// floatToInt converts like the gc compiler on arm64: saturating conversion
// to 64 or 32 bits, then truncation for narrower types. On amd64 it follows
// gc's CVTTSD2SQ/CVTTSD2SL semantics instead.
func (f *funcGen) floatToInt(x, ft, tt string, signed bool) string {
	if f.m.p.opts.GOARCH == "amd64" {
		return f.floatToIntAMD64(x, ft, tt, signed)
	}
	w := intBits(tt)
	mid := mlir.I64
	if w <= 32 {
		mid = mlir.I32
	}
	name := "fptosi"
	if !signed && (w == 64 || w == 32) {
		name = "fptoui"
	}
	r := f.op("llvm.call_intrinsic \"llvm.%s.sat.%s.%s\"(%s) : (%s) -> %s", name, mid, ft, x, ft, mid)
	if intBits(mid) > w {
		r = f.op("llvm.trunc %s : %s to %s", r, mid, tt)
	}
	return r
}

// cvtt models CVTTSD2SQ (w=64) and CVTTSD2SL (w=32): truncation toward
// zero, and the minimum integer for NaN and out of range values.
func (f *funcGen) cvtt(x, ft string, w int) string {
	it := fmt.Sprintf("i%d", w)
	lo := -float64(uint64(1) << (w - 1))
	hi := float64(uint64(1) << (w - 1))
	clo := f.op("llvm.mlir.constant(%s : %s) : %s", mlir.FloatAttr(lo, ft), ft, ft)
	chi := f.op("llvm.mlir.constant(%s : %s) : %s", mlir.FloatAttr(hi, ft), ft, ft)
	ge := f.op("llvm.fcmp \"oge\" %s, %s : %s", x, clo, ft)
	lt := f.op("llvm.fcmp \"olt\" %s, %s : %s", x, chi, ft)
	in := f.and(ge, lt)
	r := f.op("llvm.call_intrinsic \"llvm.fptosi.sat.%s.%s\"(%s) : (%s) -> %s", it, ft, x, ft, it)
	min := f.constInt(-int64(uint64(1)<<(w-1)), it)
	return f.op("llvm.select %s, %s, %s : i1, %s", in, r, min, it)
}

// floatToIntAMD64 follows gc on amd64 (cmd/compile/internal/ssagen).
func (f *funcGen) floatToIntAMD64(x, ft, tt string, signed bool) string {
	w := intBits(tt)
	switch {
	case w == 64 && signed:
		return f.cvtt(x, ft, 64)
	case w == 64:
		// x < 2^63 ? cvtt(x) : cvtt(x - 2^63) | 1<<63
		two63 := f.op("llvm.mlir.constant(%s : %s) : %s", mlir.FloatAttr(9223372036854775808.0, ft), ft, ft)
		small := f.op("llvm.fcmp \"olt\" %s, %s : %s", x, two63, ft)
		a := f.cvtt(x, ft, 64)
		y := f.op("llvm.fsub %s, %s : %s", x, two63, ft)
		b := f.op("llvm.or %s, %s : i64", f.cvtt(y, ft, 64), f.constInt(-9223372036854775808, mlir.I64))
		return f.op("llvm.select %s, %s, %s : i1, i64", small, a, b)
	case w == 32 && !signed:
		r := f.cvtt(x, ft, 64)
		return f.op("llvm.trunc %s : i64 to i32", r)
	default:
		r := f.cvtt(x, ft, 32) // int32, and int8/16, uint8/16 by truncation
		if w < 32 {
			r = f.op("llvm.trunc %s : i32 to %s", r, tt)
		}
		return r
	}
}

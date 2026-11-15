package verify

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/verify/smt"
	"golang.org/x/tools/go/ssa"
)

// Engine B's encoder: a loop-free function's SSA as bit-vector terms.
// Integers are bit-vectors of their width, wrapping as in Go; strings
// and byte slices are a byte array, an offset into it, a length and a
// capacity; branch conditions give each block a reachability condition
// and phis become ites over the incoming edges. Calls of other functions
// give fresh values of their result types (no assumption about them is
// used beyond their types). Each obligation becomes a violation
// condition: its block is reachable and its check fails.

// encoding is the result for one function.
type encoding struct {
	Params  []encParam // the inputs, in order
	Assumes []*smt.T   // facts about the inputs (0 <= len <= cap <= maxLen)
	Viol    map[*Obligation]*smt.T
	Result  *smt.T // the first result when it is an integer, nil otherwise
}

type encParam struct {
	Name string
	Kind string // int, uint, bool, bytes, string
	Bits int
}

type encoder struct {
	fn      *ssa.Function
	vals    map[ssa.Value]*smt.T
	seqs    map[ssa.Value]*seqT // strings and byte slices
	reach   map[*ssa.BasicBlock]*smt.T
	edges   map[[2]*ssa.BasicBlock]*smt.T
	enc     *encoding
	fresh   int
	failure string
}

// seqT is a string or byte slice: bytes arr[off : off+len], capacity cap.
type seqT struct {
	arr, off, len, cap *smt.T
}

// encodable reports why fn cannot be encoded ("" when it can).
func encodable(fn *ssa.Function) string {
	for _, b := range fn.Blocks {
		if isLoopHeader(b) {
			return "loop"
		}
	}
	if fn.Recover != nil {
		return "recover"
	}
	return ""
}

func intWidth(t types.Type) (w int, signed, ok bool) {
	b, isB := coreType(t).(*types.Basic)
	if !isB || b.Info()&types.IsInteger == 0 {
		return 0, false, false
	}
	return int(sizes.Sizeof(b)) * 8, b.Info()&types.IsUnsigned == 0, true
}

func isBool(t types.Type) bool {
	b, ok := coreType(t).(*types.Basic)
	return ok && b.Info()&types.IsBoolean != 0
}

func isSeq(t types.Type) bool {
	switch u := coreType(t).(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Slice:
		return isByteSlice(t)
	}
	return false
}

// encodeFunc encodes fn, or reports why it cannot.
func encodeFunc(fn *ssa.Function) (*encoding, error) {
	if why := encodable(fn); why != "" {
		return nil, fmt.Errorf("not encoded: %s", why)
	}
	e := &encoder{fn: fn, vals: map[ssa.Value]*smt.T{}, seqs: map[ssa.Value]*seqT{}, reach: map[*ssa.BasicBlock]*smt.T{}, edges: map[[2]*ssa.BasicBlock]*smt.T{}, enc: &encoding{Viol: map[*Obligation]*smt.T{}}}
	for i, p := range fn.Params {
		name := fmt.Sprintf("p%d_%s", i, p.Name())
		switch {
		case isBool(p.Type()):
			e.vals[p] = smt.Var(name, 0)
			e.enc.Params = append(e.enc.Params, encParam{Name: name, Kind: "bool"})
		case isSeq(p.Type()):
			s := &seqT{arr: smt.ArrVar(name + "_arr"), off: smt.BV(0, 64), len: smt.Var(name+"_len", 64), cap: smt.Var(name+"_cap", 64)}
			kind := "bytes"
			if b, ok := coreType(p.Type()).(*types.Basic); ok && b.Info()&types.IsString != 0 {
				kind = "string"
				s.cap = s.len
			}
			e.seqs[p] = s
			lim := smt.BV(maxLen, 64)
			e.enc.Assumes = append(e.enc.Assumes, smt.SLe(smt.BV(0, 64), s.len), smt.SLe(s.len, s.cap), smt.SLe(s.cap, lim))
			e.enc.Params = append(e.enc.Params, encParam{Name: name, Kind: kind})
		default:
			w, signed, ok := intWidth(p.Type())
			if !ok {
				return nil, fmt.Errorf("not encoded: parameter %s of type %s", p.Name(), p.Type())
			}
			e.vals[p] = smt.Var(name, w)
			k := "uint"
			if signed {
				k = "int"
			}
			e.enc.Params = append(e.enc.Params, encParam{Name: name, Kind: k, Bits: w})
		}
	}
	obs := obligations(fn)
	byInstr := map[ssa.Instruction][]*Obligation{}
	for _, o := range obs {
		byInstr[o.Instr] = append(byInstr[o.Instr], o)
	}
	var results []struct{ reach, v *smt.T }
	for _, b := range reversePostorder(fn) {
		e.reach[b] = e.blockReach(b)
		for _, in := range b.Instrs {
			for _, o := range byInstr[in] {
				if v := e.violation(o); v != nil {
					e.enc.Viol[o] = smt.LAnd(e.reach[b], v)
				} else if e.failure == "" {
					e.failure = "obligation " + string(o.Kind)
				}
			}
			e.instr(b, in)
			if r, ok := in.(*ssa.Return); ok && len(r.Results) > 0 && !isSeq(r.Results[0].Type()) {
				results = append(results, struct{ reach, v *smt.T }{e.reach[b], e.val(r.Results[0])})
			}
			if e.failure != "" {
				return nil, fmt.Errorf("not encoded: %s", e.failure)
			}
		}
	}
	if len(results) > 0 && results[0].v.W > 0 {
		r := results[len(results)-1].v
		for i := len(results) - 2; i >= 0; i-- {
			r = smt.Ite(results[i].reach, results[i].v, r)
		}
		e.enc.Result = r
	}
	return e.enc, nil
}

func (e *encoder) blockReach(b *ssa.BasicBlock) *smt.T {
	if b.Index == 0 {
		return smt.Bool(true)
	}
	var ins []*smt.T
	for _, p := range b.Preds {
		if c, ok := e.edges[[2]*ssa.BasicBlock{p, b}]; ok {
			ins = append(ins, c)
		} else if r, ok := e.reach[p]; ok {
			ins = append(ins, r)
		}
	}
	return smt.LOr(ins...)
}

// val is the term of an integer or boolean value.
func (e *encoder) val(v ssa.Value) *smt.T {
	if t, ok := e.vals[v]; ok {
		return t
	}
	if c, ok := v.(*ssa.Const); ok {
		if isBool(c.Type()) {
			return smt.Bool(c.Value != nil && constant.BoolVal(c.Value))
		}
		if w, _, ok := intWidth(c.Type()); ok {
			var u uint64
			if c.Value != nil {
				if x, exact := constant.Uint64Val(c.Value); exact {
					u = x
				} else if x, exact := constant.Int64Val(c.Value); exact {
					u = uint64(x)
				}
			}
			return smt.BV(u, w)
		}
	}
	if e.failure == "" {
		e.failure = fmt.Sprintf("value %v of type %s", v, v.Type())
	}
	return smt.BV(0, 64)
}

func (e *encoder) seq(v ssa.Value) *seqT {
	if s, ok := e.seqs[v]; ok {
		return s
	}
	if c, ok := v.(*ssa.Const); ok {
		n := uint64(0)
		if c.Value != nil && c.Value.Kind() == constant.String {
			n = uint64(len(constant.StringVal(c.Value)))
		}
		// A constant: its bytes are a fresh array (the content is not
		// encoded; lengths are what the obligations need).
		e.fresh++
		return &seqT{arr: smt.ArrVar(fmt.Sprintf("k%d_arr", e.fresh)), off: smt.BV(0, 64), len: smt.BV(n, 64), cap: smt.BV(n, 64)}
	}
	if e.failure == "" {
		e.failure = fmt.Sprintf("sequence %v", v)
	}
	z := smt.BV(0, 64)
	return &seqT{arr: smt.ArrVar("bad"), off: z, len: z, cap: z}
}

// i64 widens an integer index to 64 bits by its signedness.
func (e *encoder) i64(v ssa.Value) *smt.T {
	t := e.val(v)
	if t.W == 64 {
		return t
	}
	if _, signed, _ := intWidth(v.Type()); signed {
		return smt.SExt(t, 64)
	}
	return smt.ZExt(t, 64)
}

func (e *encoder) violation(o *Obligation) *smt.T {
	zero := smt.BV(0, 64)
	switch o.Kind {
	case KIndex:
		i := e.i64(o.I)
		var n *smt.T
		if k, ok := arrayLen(o.X); ok {
			n = smt.BV(uint64(k), 64)
		} else {
			n = e.seq(o.X).len
		}
		return smt.Not(smt.LAnd(smt.SLe(zero, i), smt.SLt(i, n)))
	case KSlice:
		s := e.seq(o.X)
		lo, hi, mx := zero, s.len, s.cap
		if _, str := coreType(o.X.Type()).(*types.Basic); str {
			mx = s.len
		}
		capX := mx
		if o.I != nil {
			lo = e.i64(o.I)
		}
		if o.Hi != nil {
			hi = e.i64(o.Hi)
		}
		if o.Max != nil {
			mx = e.i64(o.Max)
		}
		return smt.Not(smt.LAnd(smt.SLe(zero, lo), smt.SLe(lo, hi), smt.SLe(hi, mx), smt.SLe(mx, capX)))
	case KDivide:
		y := e.val(o.X)
		return smt.Eq(y, smt.BV(0, y.W))
	case KShift:
		y := e.val(o.X)
		return smt.SLt(y, smt.BV(0, y.W))
	case KPanic:
		return smt.Bool(true)
	}
	return nil
}

func (e *encoder) freshVal(t types.Type) {
}

func (e *encoder) instr(b *ssa.BasicBlock, in ssa.Instruction) {
	switch in := in.(type) {
	case *ssa.If:
		c := e.val(in.Cond)
		e.edges[[2]*ssa.BasicBlock{b, b.Succs[0]}] = smt.LAnd(e.reach[b], c)
		e.edges[[2]*ssa.BasicBlock{b, b.Succs[1]}] = smt.LAnd(e.reach[b], smt.Not(c))
	case *ssa.Jump, *ssa.Return, *ssa.Panic, *ssa.DebugRef:
	case *ssa.Phi:
		if isSeq(in.Type()) {
			var s *seqT
			for i := len(in.Edges) - 1; i >= 0; i-- {
				x := e.seq(in.Edges[i])
				if s == nil {
					s = &seqT{arr: x.arr, off: x.off, len: x.len, cap: x.cap}
					continue
				}
				c := e.edgeCond(b.Preds[i], b)
				if x.arr != s.arr {
					e.failure = "phi of different arrays"
					return
				}
				s = &seqT{arr: x.arr, off: smt.Ite(c, x.off, s.off), len: smt.Ite(c, x.len, s.len), cap: smt.Ite(c, x.cap, s.cap)}
			}
			e.seqs[in] = s
			return
		}
		var t *smt.T
		for i := len(in.Edges) - 1; i >= 0; i-- {
			x := e.val(in.Edges[i])
			if t == nil {
				t = x
				continue
			}
			t = smt.Ite(e.edgeCond(b.Preds[i], b), x, t)
		}
		e.vals[in] = t
	case *ssa.BinOp:
		e.binop(in)
	case *ssa.UnOp:
		switch in.Op {
		case token.NOT:
			e.vals[in] = smt.Not(e.val(in.X))
		case token.SUB:
			e.vals[in] = smt.Neg(e.val(in.X))
		case token.XOR:
			e.vals[in] = smt.BNot(e.val(in.X))
		case token.MUL:
			// A load of a byte through &s[i].
			if ia, ok := in.X.(*ssa.IndexAddr); ok && isSeq(ia.X.Type()) {
				s := e.seq(ia.X)
				e.vals[in] = smt.Select(s.arr, smt.Add(s.off, e.i64(ia.Index)))
				return
			}
			e.failure = "load"
		default:
			e.failure = "unary " + in.Op.String()
		}
	case *ssa.IndexAddr:
		if !isSeq(in.X.Type()) {
			e.failure = "index address of a non-byte slice"
		}
	case *ssa.Index:
		if isSeq(in.X.Type()) {
			s := e.seq(in.X)
			e.vals[in] = smt.Select(s.arr, smt.Add(s.off, e.i64(in.Index)))
			return
		}
		e.failure = "index"
	case *ssa.Slice:
		if !isSeq(in.X.Type()) {
			e.failure = "slice of a non-byte sequence"
			return
		}
		s := e.seq(in.X)
		lo, hi, mx := smt.BV(0, 64), s.len, s.cap
		if in.Low != nil {
			lo = e.i64(in.Low)
		}
		if in.High != nil {
			hi = e.i64(in.High)
		}
		if in.Max != nil {
			mx = e.i64(in.Max)
		}
		e.seqs[in] = &seqT{arr: s.arr, off: smt.Add(s.off, lo), len: smt.Sub(hi, lo), cap: smt.Sub(mx, lo)}
	case *ssa.Convert:
		if isSeq(in.Type()) && isSeq(in.X.Type()) {
			s := e.seq(in.X)
			e.seqs[in] = &seqT{arr: s.arr, off: s.off, len: s.len, cap: s.len}
			return
		}
		w, _, ok := intWidth(in.Type())
		x := e.val(in.X)
		if !ok || x.W == 0 {
			e.failure = "conversion"
			return
		}
		switch {
		case w == x.W:
			e.vals[in] = x
		case w < x.W:
			e.vals[in] = smt.Extract(x, w)
		default:
			if _, signed, _ := intWidth(in.X.Type()); signed {
				e.vals[in] = smt.SExt(x, w)
			} else {
				e.vals[in] = smt.ZExt(x, w)
			}
		}
	case *ssa.ChangeType:
		if isSeq(in.Type()) {
			e.seqs[in] = e.seq(in.X)
		} else {
			e.vals[in] = e.val(in.X)
		}
	case *ssa.Call:
		e.call(in)
	default:
		e.failure = fmt.Sprintf("instruction %T", in)
	}
}

func (e *encoder) edgeCond(p, b *ssa.BasicBlock) *smt.T {
	if c, ok := e.edges[[2]*ssa.BasicBlock{p, b}]; ok {
		return c
	}
	return e.reach[p]
}

func (e *encoder) call(in *ssa.Call) {
	c := in.Common()
	if bi, ok := c.Value.(*ssa.Builtin); ok {
		switch bi.Name() {
		case "len":
			if isSeq(c.Args[0].Type()) {
				e.vals[in] = e.seq(c.Args[0]).len
				return
			}
		case "cap":
			if isSeq(c.Args[0].Type()) {
				e.vals[in] = e.seq(c.Args[0]).cap
				return
			}
		case "min", "max":
			_, signed, ok := intWidth(in.Type())
			if !ok {
				break
			}
			t := e.val(c.Args[0])
			for _, a := range c.Args[1:] {
				x := e.val(a)
				lt := smt.SLt(x, t)
				if !signed {
					lt = smt.ULt(x, t)
				}
				if bi.Name() == "max" {
					lt = smt.Not(lt)
					lt = smt.LAnd(lt, smt.Not(smt.Eq(x, t)))
				}
				t = smt.Ite(lt, x, t)
			}
			e.vals[in] = t
			return
		}
		e.failure = "builtin " + bi.Name()
		return
	}
	// Any other call: a fresh value of an integer or bool result.
	if in.Type() == nil || types.Identical(in.Type(), types.NewTuple()) {
		return
	}
	e.fresh++
	name := fmt.Sprintf("c%d", e.fresh)
	if isBool(in.Type()) {
		e.vals[in] = smt.Var(name, 0)
		return
	}
	if w, _, ok := intWidth(in.Type()); ok {
		e.vals[in] = smt.Var(name, w)
		return
	}
	e.failure = "call result of type " + in.Type().String()
}

func (e *encoder) binop(in *ssa.BinOp) {
	if isSeq(in.X.Type()) {
		e.failure = "string operation"
		return
	}
	x, y := e.val(in.X), e.val(in.Y)
	_, signed, _ := intWidth(in.X.Type())
	var t *smt.T
	switch in.Op {
	case token.ADD:
		t = smt.Add(x, y)
	case token.SUB:
		t = smt.Sub(x, y)
	case token.MUL:
		t = smt.Mul(x, y)
	case token.QUO:
		if signed {
			t = smt.SDiv(x, y)
		} else {
			t = smt.UDiv(x, y)
		}
	case token.REM:
		if signed {
			t = smt.SRem(x, y)
		} else {
			t = smt.URem(x, y)
		}
	case token.AND:
		if x.W == 0 {
			t = smt.LAnd(x, y)
		} else {
			t = smt.And(x, y)
		}
	case token.OR:
		if x.W == 0 {
			t = smt.LOr(x, y)
		} else {
			t = smt.Or(x, y)
		}
	case token.XOR:
		if x.W == 0 {
			t = smt.Not(smt.Eq(boolBV(x), boolBV(y)))
		} else {
			t = smt.Xor(x, y)
		}
	case token.AND_NOT:
		t = smt.And(x, smt.BNot(y))
	case token.SHL, token.SHR:
		t = e.shift(in, x, y, signed)
	case token.EQL, token.NEQ:
		if x.W == 0 {
			t = smt.Eq(boolBV(x), boolBV(y))
		} else {
			t = smt.Eq(x, y)
		}
		if in.Op == token.NEQ {
			t = smt.Not(t)
		}
	case token.LSS, token.LEQ, token.GTR, token.GEQ:
		a, b := x, y
		if in.Op == token.GTR || in.Op == token.GEQ {
			a, b = y, x
		}
		strict := in.Op == token.LSS || in.Op == token.GTR
		switch {
		case signed && strict:
			t = smt.SLt(a, b)
		case signed:
			t = smt.SLe(a, b)
		case strict:
			t = smt.ULt(a, b)
		default:
			t = smt.ULe(a, b)
		}
	default:
		e.failure = "operator " + in.Op.String()
		return
	}
	e.vals[in] = t
}

// boolBV turns a boolean term into a 1-bit vector, for == on booleans.
func boolBV(b *smt.T) *smt.T { return smt.Ite(b, smt.BV(1, 1), smt.BV(0, 1)) }

// shift encodes x << y or x >> y with Go's rules: a count of at least
// the width gives 0 (or all sign bits for a signed >>); the count may be
// of any integer type.
func (e *encoder) shift(in *ssa.BinOp, x, y *smt.T, signed bool) *smt.T {
	w := x.W
	c := y
	if c.W < 64 {
		c = smt.ZExt(c, 64)
	}
	big := smt.ULe(smt.BV(uint64(w), 64), c)
	cw := smt.Extract(c, w)
	if in.Op == token.SHL {
		return smt.Ite(big, smt.BV(0, w), smt.Shl(x, cw))
	}
	if signed {
		return smt.Ite(big, smt.AShr(x, smt.BV(uint64(w-1), w)), smt.AShr(x, cw))
	}
	return smt.Ite(big, smt.BV(0, w), smt.LShr(x, cw))
}

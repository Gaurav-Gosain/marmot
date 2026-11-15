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
	Params  []encParam        // the inputs, in order
	Assumes []*smt.T          // facts about the inputs (0 <= len <= cap <= maxLen)
	Viol    map[string]*smt.T // by obligation key
	Result  *smt.T            // the first result when it is an integer, nil otherwise
}

type encParam struct {
	Name string
	Kind string // int, uint, bool, bytes, string, slice, opaque
	Bits int
	Type string // the Go type of a slice parameter, as written in its package
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

// EncodeReason is why fn cannot be encoded, "" when it can (for
// measurements).
func EncodeReason(fn *ssa.Function) string {
	if _, err := encodeFunc(fn); err != nil {
		return err.Error()
	}
	return ""
}

// encodable reports why fn cannot be encoded ("" when it can).
func encodable(fn *ssa.Function) string {
	if len(fn.Blocks) == 0 {
		return "no body"
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

// isSeq: strings and slices, the values with a length.
func isSeq(t types.Type) bool {
	switch u := coreType(t).(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Slice:
		return true
	}
	return false
}

// hasBytes: strings and byte slices, whose contents are encoded.
func hasBytes(t types.Type) bool {
	if b, ok := coreType(t).(*types.Basic); ok {
		return b.Info()&types.IsString != 0
	}
	return isByteSlice(t)
}

// encodeFunc encodes fn, or reports why it cannot. Loops are cut at
// their headers with the invariant engine A computed there (heads: its
// state at each header): the header's phis are any values satisfying it,
// and back edges are not followed. Without a state for a header the
// function is not encoded.
func encodeFunc(fn *ssa.Function, heads ...map[*ssa.BasicBlock]*state) (*encoding, error) {
	if why := encodable(fn); why != "" {
		return nil, fmt.Errorf("not encoded: %s", why)
	}
	var hs map[*ssa.BasicBlock]*state
	if len(heads) > 0 {
		hs = heads[0]
	}
	for _, b := range fn.Blocks {
		if isLoopHeader(b) && hs[b] == nil {
			return nil, fmt.Errorf("not encoded: loop")
		}
	}
	e := &encoder{fn: fn, vals: map[ssa.Value]*smt.T{}, seqs: map[ssa.Value]*seqT{}, reach: map[*ssa.BasicBlock]*smt.T{}, edges: map[[2]*ssa.BasicBlock]*smt.T{}, enc: &encoding{Viol: map[string]*smt.T{}}}
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
			} else if !isByteSlice(p.Type()) {
				kind = "slice"
			}
			e.seqs[p] = s
			lim := smt.BV(maxLen, 64)
			e.enc.Assumes = append(e.enc.Assumes, smt.SLe(smt.BV(0, 64), s.len), smt.SLe(s.len, s.cap), smt.SLe(s.cap, lim))
			e.enc.Params = append(e.enc.Params, encParam{Name: name, Kind: kind, Type: types.TypeString(p.Type(), types.RelativeTo(fn.Pkg.Pkg))})
		default:
			w, signed, ok := intWidth(p.Type())
			if !ok {
				// Pointers, interfaces, structs: opaque; loads through
				// them give fresh values.
				e.enc.Params = append(e.enc.Params, encParam{Name: name, Kind: "opaque"})
				continue
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
		if isLoopHeader(b) {
			e.cutLoop(b, hs[b])
		}
		for _, in := range b.Instrs {
			if _, isPhi := in.(*ssa.Phi); isPhi && isLoopHeader(b) {
				continue // made fresh by cutLoop
			}
			for _, o := range byInstr[in] {
				if v := e.violation(o); v != nil {
					e.enc.Viol[o.Key] = smt.LAnd(e.reach[b], v)
				}
			}
			e.instr(b, in)
			if r, ok := in.(*ssa.Return); ok && len(r.Results) > 0 && e.scalar(r.Results[0].Type()) {
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
		if b.Dominates(p) {
			continue // a back edge: the header's invariant covers it
		}
		if c, ok := e.edges[[2]*ssa.BasicBlock{p, b}]; ok {
			ins = append(ins, c)
		} else if r, ok := e.reach[p]; ok {
			ins = append(ins, r)
		}
	}
	return smt.LOr(ins...)
}

// cutLoop makes the phis of loop header h fresh values and assumes
// engine A's invariant at h about them and the values before the loop.
func (e *encoder) cutLoop(h *ssa.BasicBlock, st *state) {
	for _, in := range h.Instrs {
		phi, ok := in.(*ssa.Phi)
		if !ok {
			break
		}
		switch {
		case isSeq(phi.Type()):
			s := e.freshSeq()
			e.seqs[phi] = s
		case e.scalar(phi.Type()):
			e.vals[phi] = e.freshOf(phi.Type())
		}
	}
	// a - b <= c, in 66-bit arithmetic: no wrap for 64-bit operands.
	term := func(n node) *smt.T {
		switch n.k {
		case nZero:
			return smt.BV(0, 66)
		case nVal:
			if _, ok := n.v.(*ssa.Const); !ok {
				if _, ok := e.vals[n.v]; !ok {
					return nil
				}
			}
			t := e.val(n.v)
			if t.W == 0 {
				return nil
			}
			if _, signed, _ := intWidth(n.v.Type()); signed {
				return smt.SExt(t, 66)
			}
			return smt.ZExt(t, 66)
		case nLen, nCap:
			s, ok := e.seqs[n.v]
			if !ok {
				return nil
			}
			if n.k == nLen {
				return smt.SExt(s.len, 66)
			}
			return smt.SExt(s.cap, 66)
		}
		return nil
	}
	for p, bd := range st.d {
		a, b := term(p.a), term(p.b)
		if a == nil || b == nil {
			continue
		}
		c := bd.c
		cterm := smt.BV(uint64(c), 66)
		if c < 0 {
			cterm = smt.Neg(smt.BV(uint64(-c), 66))
		}
		e.enc.Assumes = append(e.enc.Assumes, smt.SLe(smt.Sub(a, b), cterm))
	}
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
	if t := e.freshOf(v.Type()); t != nil {
		// A value the encoder does not follow (a field, an element, a
		// global): any value of its type.
		e.vals[v] = t
		return t
	}
	if e.failure == "" {
		e.failure = fmt.Sprintf("value %v of type %s", v, v.Type())
	}
	return smt.BV(0, 64)
}

// scalar: integers and booleans, the types with terms.
func (e *encoder) scalar(t types.Type) bool {
	if isBool(t) {
		return true
	}
	_, _, ok := intWidth(t)
	return ok
}

// freshOf is a fresh term of an integer or boolean type, nil otherwise.
func (e *encoder) freshOf(t types.Type) *smt.T {
	e.fresh++
	name := fmt.Sprintf("f%d", e.fresh)
	if isBool(t) {
		return smt.Var(name, 0)
	}
	if w, _, ok := intWidth(t); ok {
		return smt.Var(name, w)
	}
	return nil
}

// freshSeq is a fresh string or byte slice within the address space.
func (e *encoder) freshSeq() *seqT {
	e.fresh++
	n := fmt.Sprintf("f%d", e.fresh)
	s := &seqT{arr: smt.ArrVar(n + "_arr"), off: smt.BV(0, 64), len: smt.Var(n+"_len", 64), cap: smt.Var(n+"_cap", 64)}
	e.enc.Assumes = append(e.enc.Assumes, smt.SLe(smt.BV(0, 64), s.len), smt.SLe(s.len, s.cap), smt.SLe(s.cap, smt.BV(maxLen, 64)))
	return s
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
	if isSeq(v.Type()) {
		s := e.freshSeq()
		e.seqs[v] = s
		return s
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

func (e *encoder) violation(o *Obligation) (res *smt.T) {
	// An operand the encoder has no term for leaves the obligation out.
	saved := e.failure
	defer func() {
		if e.failure != saved {
			e.failure, res = saved, nil
		}
	}()
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
		if !e.scalar(in.Type()) {
			return // an opaque phi
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
			if ia, ok := in.X.(*ssa.IndexAddr); ok && hasBytes(ia.X.Type()) {
				s := e.seq(ia.X)
				e.vals[in] = smt.Select(s.arr, smt.Add(s.off, e.i64(ia.Index)))
				return
			}
			// Any other load: a fresh value of its type, when the
			// encoder needs it (val and seq make one on demand).
		default:
			e.failure = "unary " + in.Op.String()
		}
	case *ssa.IndexAddr:
		// The address itself is opaque; loads through it of a byte
		// sequence read the array (see the load above).
	case *ssa.Index:
		if hasBytes(in.X.Type()) {
			s := e.seq(in.X)
			e.vals[in] = smt.Select(s.arr, smt.Add(s.off, e.i64(in.Index)))
		}
	case *ssa.Slice:
		if !isSeq(in.X.Type()) {
			return // its length is not followed: fresh when needed
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
		if hasBytes(in.Type()) && hasBytes(in.X.Type()) {
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
	case *ssa.Store, *ssa.MapUpdate:
		// Memory is not followed: later loads are fresh values.
	default:
		// Allocations, fields, interfaces, closures, maps: opaque values;
		// an integer or sequence the encoder needs from them is fresh.
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
		case "recover", "print", "println", "copy", "append", "delete", "clear", "close", "panic":
			return
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
		return
	}
	// Any other call: its results are any values of their types (made
	// on demand, also for Extracts of a tuple).
}

func (e *encoder) binop(in *ssa.BinOp) {
	if isSeq(in.X.Type()) || !isBool(in.X.Type()) && func() bool { _, _, ok := intWidth(in.X.Type()); return !ok }() {
		// Comparisons of strings, pointers, interfaces: any boolean;
		// string concatenation: any string.
		if isBool(in.Type()) {
			e.vals[in] = e.freshOf(in.Type())
		} else if isSeq(in.Type()) {
			e.seqs[in] = e.freshSeq()
		}
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

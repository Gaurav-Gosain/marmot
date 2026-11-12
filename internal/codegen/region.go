package codegen

import (
	"fmt"
	"go/token"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Region inference.
//
// A function is region cloneable when the objects it allocates only form a
// fresh graph that it returns: every heap allocation (and every result of a
// call to another region cloneable function) is stored only into fields of
// such objects, returned, loaded through, compared, or passed to a parameter
// that does not escape. Its region clone takes a trailing rt_region pointer
// and allocates there.
//
// A call v = F(args) of a region cloneable F uses the clone when v is
// confined to the call's block: v, addresses derived from it, and pointers
// loaded out of the graph are only loaded through, compared, or passed to
// non-escaping parameters of calls whose results hold no pointers, all in
// that block. The region is created before the call and freed after the
// last such use, so nothing can refer to it afterwards. Heap objects never
// point into a region (stores of region pointers go only into region
// objects), as rt.h requires.

type regionState struct {
	ok map[*ssa.Function]bool // final results, shared by all goroutines
}

// regionCloneable reports whether fn is region cloneable.
func (p *Program) regionCloneable(fn *ssa.Function) bool {
	q := newSummaryQuery()
	r := p.regionCloneableQ(q, fn)
	if len(q.running) != 0 {
		panic("regionCloneable: query still running")
	}
	p.escMu.Lock()
	if p.regions.ok == nil {
		p.regions.ok = map[*ssa.Function]bool{}
	}
	for k, v := range q.local {
		// Only cloneable answers: a query may also hold escape answers.
		if k.v == regionCloneKey {
			p.regions.ok[k.fn] = v
		}
	}
	p.escMu.Unlock()
	return r
}

// regionCloneableQ: recursion is optimistic within query q (see
// summaryQuery); a callee found not cloneable drops what was decided
// assuming it was.
func (p *Program) regionCloneableQ(q *summaryQuery, fn *ssa.Function) bool {
	if !optRegions || len(fn.Blocks) == 0 || fn.Recover != nil || len(fn.FreeVars) > 0 {
		return false
	}
	p.escMu.Lock()
	r, ok := p.regions.ok[fn]
	p.escMu.Unlock()
	if ok {
		return r
	}
	k := escKey{fn, regionCloneKey}
	if r, ok := q.local[k]; ok {
		return r
	}
	if q.running[k] {
		return true // this query's own recursion: optimistic
	}
	q.running[k] = true
	start := len(q.order)
	r = p.checkRegionFn(q, fn)
	q.finish(k, start, r, true)
	return r
}

// regionCloneKey marks regionCloneable entries in a summaryQuery.
const regionCloneKey = -1000

func (p *Program) checkRegionFn(q *summaryQuery, fn *ssa.Function) bool {
	res := fn.Signature.Results()
	if res.Len() != 1 || !isPtrRepr(res.At(0).Type()) {
		return false
	}
	// MUST: region objects and addresses inside them (allocations, results
	// of cloneable calls, field and array element addresses of those),
	// never values loaded from memory. MAY: everything that may point into
	// the region: MUST plus pointers loaded out of MAY objects (which may
	// also be heap pointers stored there earlier). Both are computed to a
	// fixpoint before any use is checked (verify/lean/Region.lean,
	// sets C and M).
	must := map[ssa.Value]bool{}
	may := map[ssa.Value]bool{}
	var work []ssa.Value
	addMay := func(v ssa.Value) {
		if !may[v] {
			may[v] = true
			work = append(work, v)
		}
	}
	allocs := 0
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Alloc:
				if x.Heap {
					must[x] = true
					addMay(x)
					allocs++
				}
			case *ssa.Call:
				if callee := x.Call.StaticCallee(); callee != nil && isPtrRepr(x.Type()) && !x.Call.IsInvoke() &&
					callee != fn && p.regionCloneableQ(q, callee) {
					must[x] = true
					addMay(x)
				} else if callee == fn {
					must[x] = true
					addMay(x)
				}
			case *ssa.Go, *ssa.Defer, *ssa.Select, *ssa.Send:
				return false
			}
		}
	}
	if allocs == 0 {
		return false
	}
	ptrArray := func(x *ssa.IndexAddr) bool {
		_, ok := x.X.Type().Underlying().(*types.Pointer)
		return ok
	}
	// Fixpoint of MAY (and of MUST through address arithmetic).
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		for _, r := range *v.Referrers() {
			switch x := r.(type) {
			case *ssa.FieldAddr:
				if must[v] {
					must[x] = true
				}
				addMay(x)
			case *ssa.IndexAddr:
				if !ptrArray(x) {
					return false
				}
				if must[v] {
					must[x] = true
				}
				addMay(x)
			case *ssa.UnOp:
				if x.Op == token.MUL && len(ptrWords(x.Type(), p.opts.Sizes)) > 0 {
					addMay(x) // loaded: may point into the region, never MUST
				}
			}
		}
	}
	// MUST may have grown after a FieldAddr was first seen from a MAY-only
	// base; recompute it as the closure of the roots under address steps.
	for changed := true; changed; {
		changed = false
		for v := range may {
			if must[v] {
				continue
			}
			switch x := v.(type) {
			case *ssa.FieldAddr:
				if must[x.X] {
					must[x], changed = true, true
				}
			case *ssa.IndexAddr:
				if must[x.X] {
					must[x], changed = true, true
				}
			}
		}
	}
	for v := range may {
		for _, r := range *v.Referrers() {
			switch x := r.(type) {
			case *ssa.DebugRef, *ssa.Return, *ssa.FieldAddr:
			case *ssa.BinOp:
				// String + may return an operand itself (verify/testdata/progs/stackconcat.go).
				// Its result is not followed here: reject.
				if x.Op == token.ADD && isString(x.Type()) {
					return false
				}
			case *ssa.IndexAddr:
				if !ptrArray(x) {
					return false
				}
			case *ssa.UnOp:
				if x.Op != token.MUL {
					return false
				}
			case *ssa.Store:
				// A value that may point into the region may only be stored
				// into a region object.
				if x.Val == v && !must[x.Addr] {
					return false
				}
			case *ssa.Call:
				if x.Call.IsInvoke() {
					return false
				}
				callee := x.Call.StaticCallee()
				if callee == nil {
					return false
				}
				for i, a := range x.Call.Args {
					if a == v && p.deepParamEscapes(callee, i) {
						return false
					}
				}
				if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 && !may[x] {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

// regionPlan is a call site that uses a region clone.
type regionPlan struct {
	callee *ssa.Function
	last   ssa.Instruction // free the region after this instruction
	r      string          // the region, set when the call is emitted
}

// findRegionCalls picks the call sites of fn that use region clones.
func (f *funcGen) findRegionCalls() {
	p := f.m.p
	for _, b := range f.fn.Blocks {
		for idx, instr := range b.Instrs {
			c, ok := instr.(*ssa.Call)
			if !ok || c.Call.IsInvoke() {
				continue
			}
			callee := c.Call.StaticCallee()
			if callee == nil || callee == f.fn || f.region != "" || !p.regionCloneable(callee) {
				continue
			}
			if last, ok := confinedTo(p, c, b, idx); ok {
				if f.regionCalls == nil {
					f.regionCalls = map[ssa.Instruction]*regionPlan{}
					f.regionFree = map[ssa.Instruction][]*regionPlan{}
				}
				pl := &regionPlan{callee: callee, last: last}
				f.regionCalls[c] = pl
				f.regionFree[last] = append(f.regionFree[last], pl)
			}
		}
	}
}

// confinedTo checks that the region graph returned by call c stays in
// block b and returns its last use there.
func confinedTo(p *Program, c *ssa.Call, b *ssa.BasicBlock, start int) (ssa.Instruction, bool) {
	pos := map[ssa.Instruction]int{}
	for i, instr := range b.Instrs {
		pos[instr] = i
	}
	last := start
	seen := map[ssa.Value]bool{}
	work := []ssa.Value{c}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[v] {
			continue
		}
		seen[v] = true
		for _, r := range *v.Referrers() {
			if _, ok := r.(*ssa.DebugRef); ok {
				continue
			}
			i, ok := pos[r]
			if !ok || r.Block() != b {
				return nil, false
			}
			if i > last {
				last = i
			}
			switch x := r.(type) {
			case *ssa.BinOp:
				// String + may return an operand itself (verify/testdata/progs/stackconcat.go).
				if x.Op == token.ADD && isString(x.Type()) {
					work = append(work, x)
				}
			case *ssa.FieldAddr:
				work = append(work, x)
			case *ssa.IndexAddr:
				work = append(work, x)
			case *ssa.UnOp:
				if x.Op != token.MUL {
					return nil, false
				}
				if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 {
					work = append(work, x)
				}
			case *ssa.Call:
				if x.Call.IsInvoke() {
					return nil, false
				}
				callee := x.Call.StaticCallee()
				if callee == nil {
					return nil, false
				}
				for k, a := range x.Call.Args {
					if a == v && p.deepParamEscapes(callee, k) {
						return nil, false
					}
				}
				if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 {
					return nil, false
				}
			default:
				return nil, false
			}
		}
	}
	return b.Instrs[last], true
}

func (m *moduleGen) declareRegionAPI() {
	m.declareRaw("rt_region_new", nil, mlir.Ptr, "")
	m.declareRaw("rt_region_new_noscan", nil, mlir.Ptr, "")
	m.declareRaw("rt_region_alloc", []string{mlir.Ptr, mlir.I64}, mlir.Ptr, "")
	m.declareRaw("rt_region_free", []string{mlir.Ptr}, "", "")
}

// regionCall emits a call that uses the region clone of its callee.
func (f *funcGen) regionCall(c *ssa.CallCommon, pl *regionPlan) string {
	f.m.declareRegionAPI()
	pl.r = f.op("llvm.call @rt_region_new() : () -> !llvm.ptr")
	return f.callRegionClone(c, pl.callee, pl.r)
}

// callRegionClone calls the clone of fn with region r.
func (f *funcGen) callRegionClone(c *ssa.CallCommon, fn *ssa.Function, r string) string {
	sym := f.m.regionClone(fn)
	params, ret := f.m.signature(fn)
	params = append(params, mlir.Ptr)
	args := make([]string, 0, len(c.Args)+1)
	for _, a := range c.Args {
		args = append(args, f.value(a))
	}
	args = append(args, r)
	if ret == "" {
		f.emit("llvm.call %s(%s) : (%s) -> ()", mlir.Symbol(sym), joinArgs(args), joinArgs(params))
		return ""
	}
	return f.op("llvm.call %s(%s) : (%s) -> %s", mlir.Symbol(sym), joinArgs(args), joinArgs(params), ret)
}

func joinArgs(a []string) string {
	s := ""
	for i, x := range a {
		if i > 0 {
			s += ", "
		}
		s += x
	}
	return s
}

// regionClone emits (once per module) the region clone of fn.
func (m *moduleGen) regionClone(fn *ssa.Function) string {
	sym := m.p.symbol(fn) + "$region"
	if m.defined[sym] {
		return sym
	}
	m.defined[sym] = true
	m.declareRegionAPI()
	m.regionQueue = append(m.regionQueue, fn)
	return sym
}

// drainRegions emits queued region clones.
func (m *moduleGen) drainRegions() {
	for len(m.regionQueue) > 0 {
		fn := m.regionQueue[0]
		m.regionQueue = m.regionQueue[1:]
		f := &funcGen{
			m: m, fn: fn, tm: m.tm, vals: map[ssa.Value]string{},
			blkName: map[*ssa.BasicBlock]string{}, region: "%region",
		}
		f.b = &f.body
		func() {
			defer func() {
				if r := recover(); r != nil {
					if e, ok := r.(*Error); ok {
						m.errorf(e)
						return
					}
					panic(fmt.Sprintf("%s (region clone): %v", fn, r))
				}
			}()
			f.gen(m.p.symbol(fn)+"$region", true)
		}()
	}
}

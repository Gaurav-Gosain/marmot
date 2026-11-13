package codegen

import (
	"maps"
	"go/token"
	"go/types"
	"os"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Frame regions.
//
// A callee F is region-parametric in parameter i when every object F
// allocates is only stored into memory reached from parameter i (field and
// element addresses of it, pointers loaded from it) or into other objects
// F allocates, and the values F stores into that memory are non-pointers,
// nil, or such objects. A caller whose argument i is (an address inside) a
// frame object A that does not escape even deeply (no pointer loaded out
// of A, transitively, leaves the function) passes a frame region to the
// region clone of F. The region is created on first use and freed at every
// return; the objects in it are reachable only from A and from each other,
// so nothing refers to them after the function returns.

type deepState struct {
	done    map[escKey]bool // final results, shared by all goroutines
	running map[escKey]bool // unused; kept for the initialization below
	param   map[escKey]bool // regionParam results
}

// summaryQuery is one top-level summary computation. Recursion is optimistic (greatest fixpoint): a key that is
// being computed in this query answers "does not escape" (or "cloneable"),
// and when that key turns out the other way, every result this query
// finished after the key started is dropped. Those provisional results stay
// in the query; only when the query ends (nothing running, every
// assumption resolved) do they go to the shared cache. Modules are
// generated in parallel, so another goroutine never sees a provisional
// answer: it runs its own query (duplicate work, same result).
type summaryQuery struct {
	running map[escKey]bool
	local   map[escKey]bool
	order   []escKey // local keys in completion order
}

func newSummaryQuery() *summaryQuery {
	return &summaryQuery{running: map[escKey]bool{}, local: map[escKey]bool{}}
}

// finish records a key's result, dropping what was computed under its
// assumption when the assumption was wrong (optimistic != r).
func (q *summaryQuery) finish(k escKey, start int, r, optimistic bool) {
	delete(q.running, k)
	if r != optimistic {
		for _, d := range q.order[start:] {
			delete(q.local, d)
		}
		q.order = q.order[:start]
	}
	q.local[k] = r
	q.order = append(q.order, k)
}

// deepEscapes is escapes, but also follows pointers loaded through v.
func (p *Program) deepEscapes(v ssa.Value, seen map[ssa.Value]bool) bool {
	q := newSummaryQuery()
	r := p.deepEscapesQ(q, v, seen)
	p.commitDeep(q)
	return r
}

func (p *Program) deepEscapesQ(q *summaryQuery, v ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return false
	}
	seen[v] = true
	refs := v.Referrers()
	if refs == nil {
		return true
	}
	for _, r := range *refs {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case *ssa.BinOp:
			// String + may return an operand itself (verify/testdata/progs/stackconcat.go).
			if x.Op == token.ADD && isString(x.Type()) && p.deepEscapesQ(q, x, seen) {
				return true
			}
		case *ssa.UnOp:
			if x.Op != token.MUL {
				return true
			}
			if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 && p.deepEscapesQ(q, x, seen) {
				return true
			}
		case *ssa.Store:
			if x.Val == v {
				return true
			}
		case *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Slice, *ssa.ChangeType, *ssa.Field, *ssa.Index,
			*ssa.Extract, *ssa.Phi:
			if p.deepEscapesQ(q, x.(ssa.Value), seen) {
				return true
			}
		case *ssa.Call:
			c := &x.Call
			if c.IsInvoke() || c.Value == v {
				return true
			}
			if bi, ok := c.Value.(*ssa.Builtin); ok {
				switch bi.Name() {
				case "len", "cap", "print", "println":
					continue
				}
				return true
			}
			fn := c.StaticCallee()
			if fn == nil || fn.Signature.Variadic() {
				return true
			}
			for i, a := range c.Args {
				// A region-parametric callee only adds objects to the graph
				// behind its argument; it does not let anything out.
				if a == v && p.deepParamEscapesQ(q, fn, i) && !p.regionParam(fn, i) {
					return true
				}
			}
			if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 {
				return true // the result may carry a pointer out of v
			}
		default:
			return true
		}
	}
	return false
}

func (p *Program) deepParamEscapes(fn *ssa.Function, i int) bool {
	if len(fn.Blocks) == 0 || i >= len(fn.Params) {
		return true
	}
	q := newSummaryQuery()
	r := p.deepParamEscapesQ(q, fn, i)
	p.commitDeep(q)
	return r
}

func (p *Program) deepParamEscapesQ(q *summaryQuery, fn *ssa.Function, i int) bool {
	if len(fn.Blocks) == 0 || i >= len(fn.Params) {
		return true
	}
	k := escKey{fn, i}
	p.escMu.Lock()
	if p.deep.done == nil {
		p.deep.done = map[escKey]bool{}
		p.deep.running = map[escKey]bool{}
		p.deep.param = map[escKey]bool{}
	}
	r, ok := p.deep.done[k]
	p.escMu.Unlock()
	if ok {
		return r
	}
	if r, ok := q.local[k]; ok {
		return r
	}
	if q.running[k] {
		return false // this query's own recursion: optimistic
	}
	q.running[k] = true
	start := len(q.order)
	r = p.deepEscapesQ(q, fn.Params[i], map[ssa.Value]bool{})
	q.finish(k, start, r, false)
	return r
}

// commitDeep moves a finished query's results to the shared cache.
func (p *Program) commitDeep(q *summaryQuery) {
	if len(q.running) != 0 {
		panic("commitDeep: query still running")
	}
	p.escMu.Lock()
	if p.deep.done == nil {
		p.deep.done = map[escKey]bool{}
		p.deep.running = map[escKey]bool{}
		p.deep.param = map[escKey]bool{}
	}
	maps.Copy(p.deep.done, q.local)
	p.escMu.Unlock()
}

// regionParam reports whether fn is region-parametric in parameter i.
func (p *Program) regionParam(fn *ssa.Function, i int) bool {
	if !optFrameRegions || len(fn.Blocks) == 0 || fn.Recover != nil || len(fn.FreeVars) > 0 || i >= len(fn.Params) {
		return false
	}
	k := escKey{fn, -100 - i}
	p.escMu.Lock()
	if p.deep.param == nil {
		p.deep.done = map[escKey]bool{}
		p.deep.running = map[escKey]bool{}
		p.deep.param = map[escKey]bool{}
	}
	if r, ok := p.deep.param[k]; ok {
		p.escMu.Unlock()
		return r
	}
	p.escMu.Unlock()
	r := p.checkRegionParam(fn, i)
	p.escMu.Lock()
	p.deep.param[k] = r
	p.escMu.Unlock()
	return r
}

func (p *Program) checkRegionParam(fn *ssa.Function, pi int) bool {
	// PARAM: the parameter and field/element addresses derived from it
	// without loads (the caller's frame object A). OBJ: fn's allocations
	// (region objects, in a no-scan region) and addresses derived from them
	// without loads. RVAL: values known to be region pointers: allocations
	// and pointers loaded from OBJ memory (which by induction holds only
	// region pointers or nil). Pointers loaded from PARAM memory are
	// unknown: the caller may have stored heap pointers in A (verify
	// counterexample 3). Rules: a store into OBJ memory stores a
	// non-pointer, nil or an RVAL (no-scan region); a store into PARAM
	// memory stores a non-pointer, nil, an RVAL or a pointer loaded from
	// PARAM memory; an RVAL or a value derived from it is stored nowhere
	// else and passed only to non-escaping parameters.
	param := map[ssa.Value]bool{fn.Params[pi]: true}
	obj := map[ssa.Value]bool{}
	rval := map[ssa.Value]bool{}
	fromParam := map[ssa.Value]bool{} // pointers loaded from PARAM memory
	allocs := 0
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Alloc:
				if x.Heap {
					obj[x] = true
					rval[x] = true
					allocs++
				}
			case *ssa.Go, *ssa.Defer, *ssa.Select:
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
	// Fixpoint of the address and value sets.
	for changed := true; changed; {
		changed = false
		set := func(m map[ssa.Value]bool, v ssa.Value) {
			if !m[v] {
				m[v], changed = true, true
			}
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				switch x := instr.(type) {
				case *ssa.FieldAddr:
					if param[x.X] {
						set(param, x)
					}
					if obj[x.X] {
						set(obj, x)
					}
				case *ssa.IndexAddr:
					if ptrArray(x) {
						if param[x.X] {
							set(param, x)
						}
						if obj[x.X] {
							set(obj, x)
						}
					}
				case *ssa.UnOp:
					if x.Op == token.MUL && isPtrRepr(x.Type()) {
						if obj[x.X] {
							set(rval, x)
							set(obj, x) // a region pointer: its fields are region memory
						} else if param[x.X] {
							set(fromParam, x)
						}
					}
				}
			}
		}
	}
	pointerFree := func(v ssa.Value) bool {
		if len(ptrWords(v.Type(), p.opts.Sizes)) == 0 {
			return true
		}
		c, ok := v.(*ssa.Const)
		return ok && c.IsNil()
	}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Store:
				switch {
				case obj[x.Addr]:
					if !pointerFree(x.Val) && !rval[x.Val] {
						return false
					}
				case param[x.Addr]:
					if !pointerFree(x.Val) && !rval[x.Val] && !fromParam[x.Val] {
						return false
					}
				default:
					if rval[x.Val] || obj[x.Val] {
						return false
					}
				}
			case *ssa.UnOp:
				if x.Op == token.MUL && (obj[x.X] || param[x.X]) && len(ptrWords(x.Type(), p.opts.Sizes)) > 0 && !isPtrRepr(x.Type()) {
					return false // aggregates holding pointers: keep it simple
				}
			}
		}
	}
	// Region values must not leave through calls, results, interfaces,
	// closures or channels.
	for v := range rval {
		if leaksRegionValue(p, v, map[ssa.Value]bool{}) {
			return false
		}
	}
	for v := range obj {
		if leaksRegionValue(p, v, map[ssa.Value]bool{}) {
			return false
		}
	}
	return true
}

// leaksRegionValue reports whether region value v can leave fn other than
// through stores (checked separately), loads, address arithmetic,
// comparisons, and non-escaping call parameters.
func leaksRegionValue(p *Program, v ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return false
	}
	seen[v] = true
	for _, r := range *v.Referrers() {
		switch x := r.(type) {
		case *ssa.DebugRef, *ssa.Store, *ssa.FieldAddr, *ssa.IndexAddr:
		case *ssa.BinOp:
			// String + may return an operand itself (verify/testdata/progs/stackconcat.go).
			if x.Op == token.ADD && isString(x.Type()) && leaksRegionValue(p, x, seen) {
				return true
			}
		case *ssa.UnOp:
			if x.Op != token.MUL {
				return true
			}
		case *ssa.Call:
			c := &x.Call
			if c.IsInvoke() {
				return true
			}
			callee := c.StaticCallee()
			if callee == nil {
				return true
			}
			for k, a := range c.Args {
				// The frame region is not scanned: the callee must not
				// store pointers into memory reached through the argument
				// (verify/testdata/progs/frameregionwrite.go), nor let anything out of it.
				if a == v && (p.deepParamEscapes(callee, k) || !p.paramNoPtrStore(callee, k)) {
					return true
				}
			}
			if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// paramNoPtrStore reports whether fn stores no pointer (other than nil)
// into memory reachable from parameter i, directly or through callees.
func (p *Program) paramNoPtrStore(fn *ssa.Function, i int) bool {
	if len(fn.Blocks) == 0 || i >= len(fn.Params) {
		return false
	}
	k := escKey{fn, -2000 - i}
	p.escMu.Lock()
	if p.deep.param == nil {
		p.deep.done = map[escKey]bool{}
		p.deep.running = map[escKey]bool{}
		p.deep.param = map[escKey]bool{}
	}
	if r, ok := p.deep.param[k]; ok {
		p.escMu.Unlock()
		return r
	}
	if p.deep.running[k] {
		p.escMu.Unlock()
		return false
	}
	p.deep.running[k] = true
	p.escMu.Unlock()
	r := p.noPtrStoreThrough(fn.Params[i], map[ssa.Value]bool{})
	p.escMu.Lock()
	delete(p.deep.running, k)
	p.deep.param[k] = r
	p.escMu.Unlock()
	return r
}

func (p *Program) noPtrStoreThrough(v ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	for _, r := range *v.Referrers() {
		switch x := r.(type) {
		case *ssa.DebugRef, *ssa.BinOp:
		case *ssa.FieldAddr, *ssa.IndexAddr:
			if !p.noPtrStoreThrough(x.(ssa.Value), seen) {
				return false
			}
		case *ssa.UnOp:
			if x.Op != token.MUL {
				return false
			}
			if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 && !p.noPtrStoreThrough(x, seen) {
				return false
			}
		case *ssa.Store:
			if x.Val == v {
				return false
			}
			if len(ptrWords(x.Val.Type(), p.opts.Sizes)) > 0 {
				if c, ok := x.Val.(*ssa.Const); !ok || !c.IsNil() {
					return false
				}
			}
		case *ssa.Call:
			callee := x.Call.StaticCallee()
			if callee == nil || x.Call.IsInvoke() {
				return false
			}
			for k, a := range x.Call.Args {
				if a == v && !p.paramNoPtrStore(callee, k) {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// frameRoot returns the frame object an argument points into, if any.
func frameRoot(a ssa.Value) *ssa.Alloc {
	for {
		switch x := a.(type) {
		case *ssa.FieldAddr:
			a = x.X
			continue
		case *ssa.Alloc:
			return x
		}
		return nil
	}
}

// findFrameRegionCalls picks calls that allocate into the frame region.
func (f *funcGen) findFrameRegionCalls() {
	if f.region != "" || !optFrameRegions {
		return
	}
	p := f.m.p
	deepOK := map[*ssa.Alloc]int{} // 1 confined, 2 not
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			c, ok := instr.(*ssa.Call)
			if !ok || c.Call.IsInvoke() || f.regionCalls[c] != nil {
				continue
			}
			callee := c.Call.StaticCallee()
			if callee == nil || callee == f.fn {
				continue
			}
			for i, a := range c.Call.Args {
				A := frameRoot(a)
				if A == nil || (A.Heap && !f.stackOK[A]) {
					continue
				}
				if deepOK[A] == 0 {
					deepOK[A] = 2
					if !p.deepEscapes(A, map[ssa.Value]bool{}) {
						deepOK[A] = 1
					}
				}
				if os.Getenv("MARMOT_DEBUG_REGIONS") != "" {
					println("frame-region candidate", f.fn.String(), callee.String(), i, "stackOK", f.stackOK[A], "deep", deepOK[A], "param", p.regionParam(callee, i))
				}
				if deepOK[A] == 1 && p.regionParam(callee, i) {
					if f.frameCalls == nil {
						f.frameCalls = map[ssa.Instruction]bool{}
					}
					f.frameCalls[c] = true
					break
				}
			}
		}
	}
	if len(f.frameCalls) > 0 {
		f.m.declareRegionAPI()
		f.frameSlot = f.alloca(mlir.Ptr, 8)
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", f.null(), f.frameSlot)
	}
}

// frameRegion returns the frame region, creating it on first use.
func (f *funcGen) frameRegion() string {
	r := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", f.frameSlot)
	isNull := f.op("llvm.icmp \"eq\" %s, %s : !llvm.ptr", r, f.null())
	mk, done := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([1, 1000]), %s, %s(%s : !llvm.ptr)", isNull, mk, done, r)
	f.b.WriteString("  " + mk + ":\n")
	// No heap pointers are ever stored in a frame region (regionParam), so
	// the collector need not scan it.
	nr := f.op("llvm.call @rt_region_new_noscan() : () -> !llvm.ptr")
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", nr, f.frameSlot)
	f.emit("llvm.br %s(%s : !llvm.ptr)", done, nr)
	res := f.newVal()
	f.b.WriteString("  " + done + "(" + res + ": !llvm.ptr):\n")
	return res
}

// freeFrameRegion frees the frame region if it was created (at returns).
func (f *funcGen) freeFrameRegion() {
	if f.frameSlot == "" {
		return
	}
	r := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", f.frameSlot)
	nn := f.op("llvm.icmp \"ne\" %s, %s : !llvm.ptr", r, f.null())
	fr, done := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", nn, fr, done)
	f.b.WriteString("  " + fr + ":\n")
	f.emit("llvm.call @rt_region_free(%s) : (!llvm.ptr) -> ()", r)
	f.emit("llvm.br %s", done)
	f.b.WriteString("  " + done + ":\n")
}

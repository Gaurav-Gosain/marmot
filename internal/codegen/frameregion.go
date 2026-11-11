package codegen

import (
	"go/token"
	"go/types"
	"os"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Frame regions (docs/frontend-passes.md, "Frame regions").
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
	done    map[escKey]bool
	running map[escKey]bool
	param   map[escKey]bool // regionParam results
}

// deepEscapes is escapes, but also follows pointers loaded through v.
func (p *Program) deepEscapes(v ssa.Value, seen map[ssa.Value]bool) bool {
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
		case *ssa.DebugRef, *ssa.BinOp:
		case *ssa.UnOp:
			if x.Op != token.MUL {
				return true
			}
			if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 && p.deepEscapes(x, seen) {
				return true
			}
		case *ssa.Store:
			if x.Val == v {
				return true
			}
		case *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Slice, *ssa.ChangeType, *ssa.Field, *ssa.Index,
			*ssa.Extract, *ssa.Phi:
			if p.deepEscapes(x.(ssa.Value), seen) {
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
				if a == v && p.deepParamEscapes(fn, i) && !p.regionParam(fn, i) {
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
	k := escKey{fn, i}
	p.escMu.Lock()
	if p.deep.done == nil {
		p.deep.done = map[escKey]bool{}
		p.deep.running = map[escKey]bool{}
		p.deep.param = map[escKey]bool{}
	}
	if r, ok := p.deep.done[k]; ok {
		p.escMu.Unlock()
		return r
	}
	if p.deep.running[k] {
		p.escMu.Unlock()
		return true
	}
	p.deep.running[k] = true
	p.escMu.Unlock()
	r := p.deepEscapes(fn.Params[i], map[ssa.Value]bool{})
	p.escMu.Lock()
	delete(p.deep.running, k)
	p.deep.done[k] = r
	p.escMu.Unlock()
	return r
}

// regionParam reports whether fn is region-parametric in parameter i.
func (p *Program) regionParam(fn *ssa.Function, i int) bool {
	if os.Getenv("MARMOT_NO_REGIONS") != "" || len(fn.Blocks) == 0 || fn.Recover != nil || len(fn.FreeVars) > 0 || i >= len(fn.Params) {
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
	// region: allocations of fn and pointers loaded from the parameter's
	// memory (which point to region objects or are nil).
	region := map[ssa.Value]bool{}
	mem := map[ssa.Value]bool{fn.Params[pi]: true} // addresses into region-side memory
	var work []ssa.Value
	allocs := 0
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Alloc:
				if x.Heap {
					region[x] = true
					mem[x] = true
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
	for v := range mem {
		work = append(work, v)
	}
	isMem := func(a ssa.Value) bool {
		for {
			switch x := a.(type) {
			case *ssa.FieldAddr:
				a = x.X
				continue
			case *ssa.IndexAddr:
				if _, ok := x.X.Type().Underlying().(*types.Pointer); !ok {
					return false
				}
				a = x.X
				continue
			}
			return mem[a]
		}
	}
	// A pointer loaded from region-side memory is a region pointer (or nil),
	// whatever order the walk reaches it in.
	isRegionVal := func(x ssa.Value) bool {
		if region[x] {
			return true
		}
		u, ok := x.(*ssa.UnOp)
		return ok && u.Op == token.MUL && isPtrRepr(u.Type()) && isMem(u.X)
	}
	visited := map[ssa.Value]bool{}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		if visited[v] {
			continue
		}
		visited[v] = true
		for _, r := range *v.Referrers() {
			switch x := r.(type) {
			case *ssa.DebugRef, *ssa.BinOp:
			case *ssa.FieldAddr:
				work = append(work, x)
			case *ssa.IndexAddr:
				if _, ok := x.X.Type().Underlying().(*types.Pointer); !ok {
					return false
				}
				work = append(work, x)
			case *ssa.UnOp:
				if x.Op != token.MUL {
					return false
				}
				if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 {
					if !isPtrRepr(x.Type()) {
						return false // aggregates holding pointers: keep it simple
					}
					region[x] = true
					mem[x] = true
					work = append(work, x)
				}
			case *ssa.Store:
				if x.Val == v {
					if !isMem(x.Addr) {
						return false
					}
				} else if x.Addr == v || isMem(x.Addr) {
					// Storing into region-side memory: the value must not be a
					// heap pointer.
					if len(ptrWords(x.Val.Type(), p.opts.Sizes)) > 0 && !isRegionVal(x.Val) {
						if c, ok := x.Val.(*ssa.Const); !ok || !c.IsNil() {
							return false
						}
					}
				}
			case *ssa.Call:
				c := &x.Call
				if c.IsInvoke() {
					return false
				}
				callee := c.StaticCallee()
				if callee == nil {
					return false
				}
				for k, a := range c.Args {
					if a == v && p.deepParamEscapes(callee, k) {
						return false
					}
				}
				if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 {
					return false
				}
			default:
				return false
			}
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
	if f.region != "" || os.Getenv("MARMOT_NO_REGIONS") != "" {
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

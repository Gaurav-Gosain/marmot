package codegen

import (
	"fmt"
	"go/token"
	"go/types"
	"os"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Region inference (SPEC 3a lever 4, docs/passes.md "Regions").
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
	ok      map[*ssa.Function]bool
	running map[*ssa.Function]bool
}

// regionCloneable reports whether fn is region cloneable.
func (p *Program) regionCloneable(fn *ssa.Function) bool {
	if os.Getenv("MARMOT_NO_REGIONS") != "" || len(fn.Blocks) == 0 || fn.Recover != nil || len(fn.FreeVars) > 0 {
		return false
	}
	p.escMu.Lock()
	if p.regions.ok == nil {
		p.regions.ok = map[*ssa.Function]bool{}
		p.regions.running = map[*ssa.Function]bool{}
	}
	if r, ok := p.regions.ok[fn]; ok {
		p.escMu.Unlock()
		return r
	}
	if p.regions.running[fn] {
		p.escMu.Unlock()
		return true // recursion: optimistic, confirmed when the outer check ends
	}
	p.regions.running[fn] = true
	p.escMu.Unlock()
	r := p.checkRegionFn(fn)
	p.escMu.Lock()
	delete(p.regions.running, fn)
	p.regions.ok[fn] = r
	p.escMu.Unlock()
	return r
}

func (p *Program) checkRegionFn(fn *ssa.Function) bool {
	res := fn.Signature.Results()
	if res.Len() != 1 || !isPtrRepr(res.At(0).Type()) {
		return false
	}
	region := map[ssa.Value]bool{}
	var work []ssa.Value
	add := func(v ssa.Value) {
		if !region[v] {
			region[v] = true
			work = append(work, v)
		}
	}
	allocs := 0
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Alloc:
				if x.Heap {
					add(x)
					allocs++
				}
			case *ssa.Call:
				if callee := x.Call.StaticCallee(); callee != nil && isPtrRepr(x.Type()) && !x.Call.IsInvoke() &&
					callee != fn && p.regionCloneable(callee) {
					add(x)
				} else if callee == fn {
					add(x)
				}
			case *ssa.Go, *ssa.Defer, *ssa.Select, *ssa.Send:
				return false
			}
		}
	}
	if allocs == 0 {
		return false
	}
	isRegionAddr := func(a ssa.Value) bool {
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
			return region[a]
		}
	}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		for _, r := range *v.Referrers() {
			switch x := r.(type) {
			case *ssa.DebugRef, *ssa.BinOp, *ssa.Return:
			case *ssa.FieldAddr:
				add(x)
			case *ssa.IndexAddr:
				if _, ok := x.X.Type().Underlying().(*types.Pointer); !ok {
					return false
				}
				add(x)
			case *ssa.UnOp:
				if x.Op != token.MUL {
					return false
				}
				if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 {
					add(x) // pointers loaded out of the graph may point into it
				}
			case *ssa.Store:
				if x.Val == v && !isRegionAddr(x.Addr) {
					return false // would let other memory point into the region
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
					if a == v && p.paramEscapes(callee, i) {
						return false
					}
				}
				if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 && !region[x] {
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
					if a == v && p.paramEscapes(callee, k) {
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

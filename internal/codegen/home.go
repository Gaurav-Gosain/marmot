package codegen

import (
	"sort"
	"sync"

	"golang.org/x/tools/go/ssa"
)

// Home instances (Options.HomeInstances, whole program -release builds):
// a shared function (a generic instance, a wrapper) used by many modules is
// emitted once, in a module of its own that GenerateInstances makes after
// every other module, and the users get a declaration. Each copy otherwise
// went through the optimizer of every module using it (tuios internal/app
// and internal/session: 39% of their IR). Small bodies stay linkonce_odr
// copies where they are used, so they inline without the backend's
// whole-program imports.

// homeSmallInstrs: a shared function with at most this many SSA
// instructions keeps its copies (about the backend's 40 LLVM instruction
// import limit).
const homeSmallInstrs = 12

type homeSet struct {
	sync.Mutex
	want map[*ssa.Function]bool // requested by some module
}

// homed reports whether fn (shared, with a body) lives in the instances
// module, and records that a module needs it.
func (m *moduleGen) homed(fn *ssa.Function) bool {
	if !m.p.opts.HomeInstances || m.instances || m.helpers != nil {
		return false
	}
	n := 0
	for _, b := range fn.Blocks {
		n += len(b.Instrs)
		if n > homeSmallInstrs {
			break
		}
	}
	if n <= homeSmallInstrs {
		return false
	}
	h := &m.p.home
	h.Lock()
	if h.want == nil {
		h.want = map[*ssa.Function]bool{}
	}
	h.want[fn] = true
	h.Unlock()
	return true
}

// GenerateInstances returns the module of the shared functions other
// modules declared (HomeInstances); call it after every other module is
// generated. ok is false when there is none.
func (p *Program) GenerateInstances() (mod Module, ok bool, err error) {
	if !p.opts.HomeInstances {
		return Module{}, false, nil
	}
	p.home.Lock()
	want := p.home.want
	p.home.want = nil
	p.home.Unlock()
	if len(want) == 0 {
		return Module{}, false, nil
	}
	m := p.newModule(nil)
	m.static = &staticInit{}
	m.instances = true
	fns := make([]*ssa.Function, 0, len(want))
	for fn := range want {
		fns = append(fns, fn)
	}
	// Deterministic text: by symbol.
	sort.Slice(fns, func(i, j int) bool { return p.symbol(fns[i]) < p.symbol(fns[j]) })
	// Strong definitions, which the other modules declare. Marked first,
	// so that a reference from one of them does not queue a linkonce_odr
	// copy instead (LLVM could drop that once inlined here).
	for _, fn := range fns {
		m.defined[p.symbol(fn)] = true
	}
	for _, fn := range fns {
		m.function(fn, false)
	}
	m.drain()
	mod, err = m.finish("marmot:instances")
	return mod, true, err
}

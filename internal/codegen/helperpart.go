package codegen

import (
	"go/types"
	"os"

	"golang.org/x/tools/go/ssa"
)

// The helper part of a split package. Every part of a package used to
// define linkonce copies of the shared code it used (type descriptors with
// their method tables, instances, wrappers, call adapters), so the 19
// parts of tuios internal/app compiled the same helpers 19 times. The parts
// after the first now declare that code and record it; after they are
// generated, one more module, the helper part, defines each of it once.
// The first part still defines its own: a helper may then be in two
// objects, never in more. MARMOT_NO_HELPER_PART=1 turns this off.

var noHelperPart = os.Getenv("MARMOT_NO_HELPER_PART") != ""

// helperSet is the shared code one part uses, in the order it asked for it.
type helperSet struct {
	types    []types.Type
	fns      []*ssa.Function
	typeSeen map[string]bool
	fnSeen   map[string]bool
}

func newHelperSet() *helperSet {
	return &helperSet{typeSeen: map[string]bool{}, fnSeen: map[string]bool{}}
}

func (h *helperSet) addType(sym string, t types.Type) {
	if !h.typeSeen[sym] {
		h.typeSeen[sym] = true
		h.types = append(h.types, t)
	}
}

func (h *helperSet) addFunc(sym string, fn *ssa.Function) {
	if !h.fnSeen[sym] {
		h.fnSeen[sym] = true
		h.fns = append(h.fns, fn)
	}
}

// mergeHelpers is the union of sets in part order (deterministic module
// text), or nil when no part asked for anything.
func mergeHelpers(sets []*helperSet) *helperSet {
	var out *helperSet
	for _, h := range sets {
		if h == nil {
			continue
		}
		if out == nil {
			out = newHelperSet()
		}
		for _, t := range h.types {
			out.types = append(out.types, t)
		}
		for _, fn := range h.fns {
			out.fns = append(out.fns, fn)
		}
	}
	if out == nil || len(out.types)+len(out.fns) == 0 {
		return nil
	}
	return out
}

// generateHelpers defines the shared code of h in one module of pkg.
func (p *Program) generateHelpers(pkg *ssa.Package, h *helperSet) (Module, error) {
	m := p.newModule(pkg)
	m.laterPart = true // the first part defines the package's own descriptors
	m.static = &staticInit{}
	var keep []string
	for _, t := range h.types {
		keep = append(keep, m.typeDescSym(t))
	}
	for _, fn := range h.fns {
		keep = append(keep, m.funcRef(fn))
	}
	for m.drain(); len(m.regionQueue) > 0; m.drain() {
		m.drainRegions()
	}
	// Nothing in this module refers to most of what it defines: keep the
	// linkonce definitions from being dropped as unused (compiler.used, so
	// the linker still strips what no other object refers to).
	for _, sym := range keep {
		if m.defined[sym] {
			m.compilerUsed = append(m.compilerUsed, cAddr(sym))
		}
	}
	return m.finish(pkg.Pkg.Path())
}

package codegen

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Static initialization of package variables, like gc's staticinit: stores
// of constant data into package globals during the package init function
// become global initializers, and composite literal backing stores
// allocated there become private globals. This keeps init functions (and
// MLIR text) small for table-heavy packages.
//
// Only the straight-line part of the init body that runs exactly once is
// considered. A global (or literal object) is static only if every store
// into it in init is static and no location is stored twice.

type staticInit struct {
	m         *moduleGen
	skip      map[ssa.Instruction]bool
	cand      map[*ssa.Store]ssa.Value // candidate static stores and their roots
	order     map[ssa.Instruction]int  // position in the run-once blocks
	lastStore map[ssa.Value]int        // position of the last candidate store per object
	images    map[ssa.Value]*memImage  // *ssa.Global or heap *ssa.Alloc
	objSyms   map[*ssa.Alloc]string
	dynamic   map[ssa.Value]bool
	loads     []*ssa.UnOp // loads of local literal objects used as static values
}

// memImage is the static content of a memory object of Go type t.
type memImage struct {
	t     types.Type
	leaf  *cv         // set for scalars that were stored
	elems []*memImage // structs and arrays (lazily created)
}

func (s *staticInit) image(t types.Type) *memImage { return &memImage{t: t} }

// at returns the sub image for a field or element index.
func (img *memImage) at(i int) *memImage {
	switch u := img.t.Underlying().(type) {
	case *types.Struct:
		if img.elems == nil {
			img.elems = make([]*memImage, u.NumFields())
		}
		if img.elems[i] == nil {
			img.elems[i] = &memImage{t: u.Field(i).Type()}
		}
		return img.elems[i]
	case *types.Array:
		if img.elems == nil {
			img.elems = make([]*memImage, u.Len())
		}
		if img.elems[i] == nil {
			img.elems[i] = &memImage{t: u.Elem()}
		}
		return img.elems[i]
	}
	return nil
}

// value renders the image as a constant of the memory type.
func (s *staticInit) value(img *memImage) cv {
	mt := s.m.tm.memType(img.t)
	if img.leaf != nil {
		if img.leaf.kind == cvImage {
			return s.value(img.leaf.img)
		}
		return *img.leaf
	}
	if img.elems == nil {
		return cZero(mt)
	}
	var es []cv
	switch u := img.t.Underlying().(type) {
	case *types.Struct:
		for i, e := range img.elems {
			if e == nil {
				es = append(es, cZero(s.m.tm.memType(u.Field(i).Type())))
			} else {
				es = append(es, s.value(e))
			}
		}
		// Explicit tail padding field, if the struct type has one.
		if n := s.m.tm.structFieldCount(u); n > len(es) {
			es = append(es, cZero(s.m.tm.structPadType(u)))
		}
	case *types.Array:
		for _, e := range img.elems {
			if e == nil {
				es = append(es, cZero(s.m.tm.memType(u.Elem())))
			} else {
				es = append(es, s.value(e))
			}
		}
	}
	return cAgg(mt, es...)
}

// runOnceBlocks returns the init body blocks that run exactly once, in
// order: the guard's "not yet initialized" successor and its chain of
// single-predecessor successors.
func runOnceBlocks(fn *ssa.Function) []*ssa.BasicBlock {
	if len(fn.Blocks) == 0 {
		return nil
	}
	b0 := fn.Blocks[0]
	iff, ok := b0.Instrs[len(b0.Instrs)-1].(*ssa.If)
	if !ok || len(b0.Succs) != 2 {
		return nil
	}
	_ = iff
	var out []*ssa.BasicBlock
	b := b0.Succs[1] // guard false: run the body
	for b != nil && len(b.Preds) == 1 {
		out = append(out, b)
		if len(b.Succs) != 1 {
			break
		}
		b = b.Succs[0]
	}
	return out
}

// analyzeStaticInit finds the static stores of the package init function.
func (m *moduleGen) analyzeStaticInit(pkg *ssa.Package) *staticInit {
	s := &staticInit{
		m:       m,
		skip:    map[ssa.Instruction]bool{},
		images:  map[ssa.Value]*memImage{},
		objSyms: map[*ssa.Alloc]string{},
		dynamic: map[ssa.Value]bool{},
		cand:    map[*ssa.Store]ssa.Value{},
	}
	init := pkg.Func("init")
	if init == nil {
		return s
	}
	blocks := runOnceBlocks(init)
	once := map[*ssa.BasicBlock]bool{}
	for _, b := range blocks {
		once[b] = true
	}
	// Candidate literal objects: heap allocations in the run-once blocks.
	order := map[ssa.Instruction]int{}
	for _, b := range blocks {
		for _, instr := range b.Instrs {
			order[instr] = len(order)
			if a, ok := instr.(*ssa.Alloc); ok {
				s.images[a] = s.image(a.Type().(*types.Pointer).Elem())
			}
		}
	}
	s.order = order
	// Every store in init: static if in a run-once block with a static value.
	type store struct {
		st   *ssa.Store
		root ssa.Value
		path []int
		val  cv
	}
	var stores []store
	written := map[ssa.Value]map[string]bool{}
	for _, b := range init.Blocks {
		for _, instr := range b.Instrs {
			st, ok := instr.(*ssa.Store)
			if !ok {
				continue
			}
			root, path, ok := addrPath(st.Addr)
			if !ok {
				if root != nil {
					s.dynamic[root] = true
				}
				continue
			}
			if g, ok := root.(*ssa.Global); ok && (g.Pkg != pkg || strings.Contains(g.Name(), "$")) {
				continue // other packages' globals, init$guard
			}
			if _, ok := root.(*ssa.Alloc); ok && s.images[root] == nil {
				continue
			}
			if !once[b] {
				s.dynamic[root] = true
				continue
			}
			key := fmt.Sprint(path)
			if written[root] == nil {
				written[root] = map[string]bool{}
			}
			if written[root][key] {
				s.dynamic[root] = true
				continue
			}
			written[root][key] = true
			v, ok := s.staticValue(st.Val)
			if !ok {
				s.dynamic[root] = true
				continue
			}
			stores = append(stores, store{st, root, path, v})
			s.cand[st] = root
		}
	}
	// A literal object stays static only if all its uses are static stores
	// into it, address computations, and the value it is stored or sliced as.
	changed := true
	for changed {
		changed = false
		for v := range s.images {
			a, ok := v.(*ssa.Alloc)
			if !ok || s.dynamic[a] {
				continue
			}
			if !s.allocUsesStatic(a) {
				s.dynamic[a] = true
				changed = true
			}
		}
		// A store whose value refers to a dynamic object is dynamic.
		for _, st := range stores {
			if !s.dynamic[st.root] && s.refersDynamic(st.st.Val) {
				s.dynamic[st.root] = true
				changed = true
			}
		}
	}
	for _, st := range stores {
		if s.dynamic[st.root] {
			continue
		}
		img := s.images[st.root]
		if img == nil {
			img = s.image(st.root.Type().(*types.Pointer).Elem())
			s.images[st.root] = img
		}
		for _, i := range st.path {
			img = img.at(i)
		}
		v := st.val
		img.leaf = &v
		s.skip[st.st] = true
	}
	s.removeDeadInit(pkg, blocks)
	// Address computations used only by skipped stores are skipped too.
	for _, b := range blocks {
		for i := len(b.Instrs) - 1; i >= 0; i-- {
			instr := b.Instrs[i]
			switch x := instr.(type) {
			case *ssa.IndexAddr, *ssa.FieldAddr:
				if allSkipped(s.skip, *x.(ssa.Value).Referrers()) {
					s.skip[instr] = true
				}
			case *ssa.Alloc:
				if !s.dynamic[x] && s.images[x] != nil && allSkipped(s.skip, *x.Referrers()) {
					s.skip[instr] = true
				}
			case *ssa.UnOp:
				if a, ok := x.X.(*ssa.Alloc); ok && x.Op == token.MUL && s.isStaticObj(a) && allSkipped(s.skip, *x.Referrers()) {
					s.skip[instr] = true
				}
			case *ssa.Slice:
				if a, ok := x.X.(*ssa.Alloc); ok && s.isStaticObj(a) && allSkipped(s.skip, *x.Referrers()) {
					s.skip[instr] = true
				}
			}
		}
	}
	return s
}

func allSkipped(skip map[ssa.Instruction]bool, refs []ssa.Instruction) bool {
	for _, r := range refs {
		if _, ok := r.(*ssa.DebugRef); ok {
			continue
		}
		if !skip[r] {
			return false
		}
	}
	return true
}

func (s *staticInit) isStaticObj(a *ssa.Alloc) bool {
	return s.images[a] != nil && !s.dynamic[a]
}

// storesBefore reports whether every store into a precedes instr.
func (s *staticInit) storesBefore(a *ssa.Alloc, instr ssa.Instruction) bool {
	pos, ok := s.order[instr]
	if !ok {
		return false
	}
	if s.lastStore == nil {
		s.lastStore = map[ssa.Value]int{}
		for st, root := range s.cand {
			p, ok := s.order[st]
			if !ok {
				p = 1 << 30
			}
			if p > s.lastStore[root] {
				s.lastStore[root] = p
			}
		}
	}
	return s.lastStore[a] <= pos
}

// allocUsesStatic checks the uses of a literal object.
func (s *staticInit) allocUsesStatic(a *ssa.Alloc) bool {
	var check func(v ssa.Value) bool
	check = func(v ssa.Value) bool {
		for _, r := range *v.Referrers() {
			switch x := r.(type) {
			case *ssa.DebugRef:
			case *ssa.IndexAddr:
				if _, ok := x.Index.(*ssa.Const); !ok || x.X != v || !check(x) {
					return false
				}
			case *ssa.FieldAddr:
				if x.X != v || !check(x) {
					return false
				}
			case *ssa.Store:
				if x.Addr != v {
					// The object's address is stored: only into a static location.
					root, ok := s.cand[x]
					if v != ssa.Value(a) || !ok || s.dynamic[root] {
						return false
					}
				}
			case *ssa.UnOp:
				if a.Heap || v != ssa.Value(a) || x.Op != token.MUL {
					return false
				}
				for _, rr := range *x.Referrers() {
					if _, ok := rr.(*ssa.DebugRef); ok {
						continue
					}
					st, ok := rr.(*ssa.Store)
					if !ok || st.Val != x {
						return false
					}
					root, ok := s.cand[st]
					if !ok || s.dynamic[root] {
						return false
					}
				}
				// Every store into a must come before the load.
				if !s.storesBefore(a, x) {
					return false
				}
			case *ssa.Slice:
				if v != ssa.Value(a) || x.Low != nil || x.High != nil || x.Max != nil {
					return false
				}
				for _, rr := range *x.Referrers() {
					if _, ok := rr.(*ssa.DebugRef); ok {
						continue
					}
					st, ok := rr.(*ssa.Store)
					if !ok || st.Val != x {
						return false
					}
					root, ok := s.cand[st]
					if !ok || s.dynamic[root] {
						return false
					}
				}
			default:
				return false
			}
		}
		return true
	}
	return check(a)
}

// refersDynamic reports whether a static value refers to a literal object
// that turned out dynamic.
func (s *staticInit) refersDynamic(v ssa.Value) bool {
	switch x := v.(type) {
	case *ssa.Alloc:
		return s.dynamic[x] || s.images[x] == nil
	case *ssa.Slice:
		return s.refersDynamic(x.X)
	case *ssa.MakeInterface:
		return s.refersDynamic(x.X)
	case *ssa.UnOp:
		if a, ok := x.X.(*ssa.Alloc); ok {
			return s.dynamic[a] || s.images[a] == nil
		}
	}
	return false
}

// addrPath decomposes an address into a root object and constant indices.
func addrPath(addr ssa.Value) (root ssa.Value, path []int, ok bool) {
	switch x := addr.(type) {
	case *ssa.Global:
		return x, nil, true
	case *ssa.Alloc:
		return x, nil, true
	case *ssa.FieldAddr:
		r, p, ok := addrPath(x.X)
		if !ok {
			return r, nil, false
		}
		return r, append(p, x.Field), true
	case *ssa.IndexAddr:
		r, p, ok := addrPath(x.X)
		if !ok {
			return r, nil, false
		}
		if _, isArr := x.X.Type().Underlying().(*types.Pointer); !isArr {
			return r, nil, false
		}
		c, isConst := x.Index.(*ssa.Const)
		if !isConst {
			return r, nil, false
		}
		return r, append(p, int(c.Int64())), true
	}
	return nil, nil, false
}

// staticValue returns the constant memory form of an SSA value, if it is
// known at compile time.
func (s *staticInit) staticValue(v ssa.Value) (cv, bool) {
	m := s.m
	t := v.Type()
	mt := m.tm.memType(t)
	switch x := v.(type) {
	case *ssa.Const:
		return m.constCV(x.Value, t)
	case *ssa.Function:
		if len(x.FreeVars) > 0 {
			return cv{}, false
		}
		return cAddr(m.funcValueSym(x)), true
	case *ssa.Global:
		if m.p.opts.Sizes.Sizeof(x.Type().(*types.Pointer).Elem()) == 0 {
			return cAddr(m.zeroBase()), true
		}
		return cAddr(m.globalRef(x)), true
	case *ssa.Alloc:
		if !x.Heap || s.images[x] == nil {
			return cv{}, false
		}
		if m.p.opts.Sizes.Sizeof(x.Type().(*types.Pointer).Elem()) == 0 {
			return cAddr(m.zeroBase()), true
		}
		return cAddr(s.objSym(x)), true
	case *ssa.UnOp:
		// *t of a local composite literal: its image, once complete.
		a, ok := x.X.(*ssa.Alloc)
		if !ok || a.Heap || x.Op != token.MUL || s.images[a] == nil {
			return cv{}, false
		}
		s.loads = append(s.loads, x)
		return cv{kind: cvImage, img: s.images[a], typ: mt}, true
	case *ssa.Slice:
		a, ok := x.X.(*ssa.Alloc)
		if !ok || s.images[a] == nil || x.Low != nil || x.High != nil || x.Max != nil {
			return cv{}, false
		}
		at, ok := a.Type().(*types.Pointer).Elem().Underlying().(*types.Array)
		if !ok {
			return cv{}, false
		}
		return cAgg(mt, cAddr(s.objSym(a)), cI64(at.Len()), cI64(at.Len())), true
	case *ssa.MakeInterface:
		tw := ""
		if isEmptyInterface(t) {
			tw = m.typeDescSym(x.X.Type())
		} else {
			tw = m.itabSym(x.X.Type(), t)
		}
		var data cv
		if isDirectIface(x.X.Type()) {
			d, ok := s.staticValue(x.X)
			if !ok || d.typ != mlir.Ptr {
				return cv{}, false
			}
			data = d
		} else if m.p.opts.Sizes.Sizeof(x.X.Type()) == 0 {
			data = cAddr(m.zeroBase())
		} else if c, ok := x.X.(*ssa.Const); ok {
			data = cAddr(m.constBox(c))
		} else {
			return cv{}, false
		}
		return cAgg(tIface, cAddr(tw), data), true
	}
	return cv{}, false
}

// objSym returns the private global holding a static literal object.
func (s *staticInit) objSym(a *ssa.Alloc) string {
	if sym, ok := s.objSyms[a]; ok {
		return sym
	}
	s.m.nstr++
	sym := fmt.Sprintf(".lit.%d", s.m.nstr)
	s.objSyms[a] = sym
	return sym
}

// emitObjects defines the static literal objects.
func (s *staticInit) emitObjects() {
	// Sorted by symbol so the module text is deterministic (object cache).
	allocs := make([]*ssa.Alloc, 0, len(s.objSyms))
	for a := range s.objSyms {
		allocs = append(allocs, a)
	}
	sort.Slice(allocs, func(i, j int) bool {
		return len(s.objSyms[allocs[i]]) < len(s.objSyms[allocs[j]]) ||
			len(s.objSyms[allocs[i]]) == len(s.objSyms[allocs[j]]) && s.objSyms[allocs[i]] < s.objSyms[allocs[j]]
	})
	for _, a := range allocs {
		sym := s.objSyms[a]
		if s.dynamic[a] {
			continue // referenced only by stores that stayed dynamic
		}
		s.m.constGlobal(sym, "private", false, s.value(s.images[a]))
	}
}

// constCV converts a Go constant to its memory form.
func (m *moduleGen) constCV(c constant.Value, t types.Type) (cv, bool) {
	mt := m.tm.memType(t)
	if c == nil {
		return cZero(mt), true
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return cv{}, false
	}
	switch {
	case b.Info()&types.IsBoolean != 0:
		if constant.BoolVal(c) {
			return cInt(1, mlir.I8), true
		}
		return cInt(0, mlir.I8), true
	case b.Info()&types.IsInteger != 0:
		if i, ok := constant.Int64Val(c); ok {
			return cInt(i, mt), true
		}
		u, _ := constant.Uint64Val(c)
		return cInt(int64(u), mt), true
	case b.Info()&types.IsFloat != 0:
		f, _ := constant.Float64Val(constant.ToFloat(c))
		return cFloat(f, mt), true
	case b.Info()&types.IsComplex != 0:
		et := complexElem(mt)
		re, _ := constant.Float64Val(constant.ToFloat(constant.Real(c)))
		im, _ := constant.Float64Val(constant.ToFloat(constant.Imag(c)))
		return cAgg(mt, cFloat(re, et), cFloat(im, et)), true
	case b.Info()&types.IsString != 0:
		return m.cString(constant.StringVal(c)), true
	}
	return cv{}, false
}

var _ = token.NoPos

// removeDeadInit drops the initialization of package variables that the
// program never reads (like the gc linker's dead map initializers), along
// with the side effect free computations that only feed them.
func (s *staticInit) removeDeadInit(pkg *ssa.Package, blocks []*ssa.BasicBlock) {
	p := s.m.p
	cand := map[ssa.Instruction]bool{}
	var list []ssa.Instruction
	for _, b := range blocks {
		for _, instr := range b.Instrs {
			ok := false
			switch x := instr.(type) {
			case *ssa.Store:
				root, _, isPath := addrPath(x.Addr)
				if g, isG := root.(*ssa.Global); isPath && isG {
					ok = g.Pkg == pkg && p.globalDead(g)
				} else if _, isA := root.(*ssa.Alloc); isPath && isA {
					ok = true // only if the object itself goes (checked below)
				}
			case *ssa.MakeMap, *ssa.MapUpdate, *ssa.MakeSlice, *ssa.Alloc, *ssa.MakeInterface,
				*ssa.MakeClosure, *ssa.Slice, *ssa.IndexAddr, *ssa.FieldAddr, *ssa.Convert,
				*ssa.ChangeType, *ssa.ChangeInterface, *ssa.Field, *ssa.Index, *ssa.Extract,
				*ssa.DebugRef:
				ok = true
			case *ssa.UnOp:
				ok = x.Op != token.ARROW
			case *ssa.BinOp:
				ok = x.Op != token.QUO && x.Op != token.REM
			}
			if ok {
				cand[instr] = true
				list = append(list, instr)
			}
		}
	}
	gone := func(i ssa.Instruction) bool { return cand[i] || s.skip[i] }
	for changed := true; changed; {
		changed = false
		for _, instr := range list {
			if !cand[instr] {
				continue
			}
			keep := false
			if v, ok := instr.(ssa.Value); ok {
				if refs := v.Referrers(); refs != nil {
					for _, r := range *refs {
						if !gone(r) {
							keep = true
							break
						}
					}
				}
			}
			switch x := instr.(type) {
			case *ssa.Store:
				root, _, _ := addrPath(x.Addr)
				if a, isA := root.(*ssa.Alloc); isA && !cand[a] {
					keep = true
				}
				if ai, ok := x.Addr.(ssa.Instruction); ok && !gone(ai) {
					keep = true
				}
			case *ssa.MapUpdate:
				if mi, ok := x.Map.(ssa.Instruction); !ok || !cand[mi] {
					keep = true
				}
			}
			if keep {
				delete(cand, instr)
				changed = true
			}
		}
	}
	for instr := range cand {
		if _, isDebug := instr.(*ssa.DebugRef); isDebug {
			continue
		}
		s.skip[instr] = true
	}
}

package codegen

import (
	"fmt"
	"go/token"
	"go/types"
	"os"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// Constant composite literals in function bodies, like gc's static
// literal copy: a fresh literal object (an Alloc of array or struct type)
// whose stores are constant gets one private read-only image of those
// stores, copied into the object where it is allocated; the stores and
// their address computations are not emitted. A literal of n constant
// elements was n stores (a string field is two words each); now it is one
// memmove. tuios GetCommandPaletteItems: 151 KB of code with stores.
//
// The copy runs at the allocation, before the stores it replaces, so the
// stores must be the only accesses before the object is first used: every
// constant store is in the allocation's block, before any other use of
// the object or of an address into it in that block (uses in other blocks
// run after the block). A store that stays (a value known only at run
// time) may not overlap a constant one: it runs after the copy either way,
// but a constant store after it would have won.
//
// Off by default (MARMOT_LITCOPY=1 turns it on) until the full gate passes
// again: the first version dropped constant stores into a field after the
// field's own literal (nestedConstStores).

var noLitCopy = os.Getenv("MARMOT_LITCOPY") != "1"

// minLitCopyStores is the fewest constant stores worth an image.
const minLitCopyStores = 4

// findConstLits fills f.litCopy and f.skipInstr.
func (f *funcGen) findConstLits() {
	if noLitCopy || f.fn == nil {
		return
	}
	// Most functions have no literal worth an image: a cheap count first
	// (on tuios the maps and walks below were 1.25 GB of allocation).
	consts, updates := 0, 0
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Store:
				switch x.Addr.(type) {
				case *ssa.FieldAddr, *ssa.IndexAddr:
					consts++ // a store into a literal's element
				}
			case *ssa.MapUpdate:
				updates++
			}
		}
	}
	if consts < minLitCopyStores && updates < minMapLitEntries {
		return
	}
	s := &staticInit{m: f.m, images: map[ssa.Value]*memImage{}}
	type cand struct {
		a   *ssa.Alloc
		b   *ssa.BasicBlock
		pos map[ssa.Instruction]int
	}
	var cands []cand
	for _, b := range f.fn.Blocks {
		var pos map[ssa.Instruction]int
		for _, instr := range b.Instrs {
			a, ok := instr.(*ssa.Alloc)
			if !ok || f.skipInstr[a] || f.volatile[a] {
				continue
			}
			switch a.Type().(*types.Pointer).Elem().Underlying().(type) {
			case *types.Array, *types.Struct:
			default:
				continue
			}
			if _, ok := f.m.static.runtimeObj(a); ok {
				continue
			}
			if pos == nil {
				pos = make(map[ssa.Instruction]int, len(b.Instrs))
				for i, instr := range b.Instrs {
					pos[instr] = i
				}
			}
			cands = append(cands, cand{a, b, pos})
		}
	}
	// Local temporaries of element literals ([]T{{...}} builds each T in
	// a local, then loads it into the array): a fully constant one that
	// is only loaded is a constant image. In block order, so an inner
	// temporary is known before the outer one stores its load.
	pure := map[*ssa.Alloc]*litObj{}
	// done holds the stores this pass folded into images. Other passes
	// skip stores whose values they still use (append's elements), so
	// f.skipInstr does not say a value is dead.
	done := map[ssa.Instruction]bool{}
	for _, c := range cands {
		if c.a.Heap {
			continue
		}
		o := f.litWalk(s, c.a, c.b, c.pos)
		if o == nil || !o.pureLocal(c.pos) {
			continue
		}
		s.images[c.a] = o.image(s)
		pure[c.a] = o
	}
	for _, c := range cands {
		if pure[c.a] != nil {
			continue
		}
		if o := f.litWalk(s, c.a, c.b, c.pos); o != nil {
			f.constLit(s, o, c.pos, done)
		}
	}
	f.findConstMaps(s, done)
	// A pure temporary whose loads all went into images is gone; one whose
	// load stayed is an ordinary candidate.
	for changed := true; changed; {
		changed = false
		for _, c := range cands {
			o := pure[c.a]
			if o == nil {
				continue
			}
			if !allLoadsSkipped(done, o.loads) {
				continue
			}
			delete(pure, c.a)
			changed = true
			f.skipAll(o, done)
		}
	}
	for _, c := range cands {
		if o := pure[c.a]; o != nil {
			delete(s.images, c.a)
			if o := f.litWalk(s, c.a, c.b, c.pos); o != nil {
				f.constLit(s, o, c.pos, done)
			}
		}
	}
}

// litObj is what a literal object's referrers do.
type litObj struct {
	a        *ssa.Alloc
	b        *ssa.BasicBlock
	stores   []litStore
	addrs    []ssa.Instruction
	loads    []*ssa.UnOp // *a in b (pure local temporaries)
	firstUse int         // position of the first other use in b
	other    bool        // a use other than loads of a itself
}

// litStore is a store into a literal object at a field and index path.
type litStore struct {
	st   *ssa.Store
	path []int
	val  cv
	ok   bool // constant
}

// litWalk collects the referrers of a, or nil if an address into it is
// computed in a way the image cannot follow.
func (f *funcGen) litWalk(s *staticInit, a *ssa.Alloc, b *ssa.BasicBlock, pos map[ssa.Instruction]int) *litObj {
	et := a.Type().(*types.Pointer).Elem()
	switch et.Underlying().(type) {
	case *types.Array, *types.Struct:
	default:
		return nil
	}
	o := &litObj{a: a, b: b, firstUse: len(b.Instrs)}
	good := true
	useAt := func(instr ssa.Instruction) {
		if instr.Block() != b {
			return
		}
		if p := pos[instr]; p < o.firstUse {
			o.firstUse = p
		}
	}
	var walk func(v ssa.Value, path []int)
	walk = func(v ssa.Value, path []int) {
		for _, r := range *v.Referrers() {
			if !good {
				return
			}
			switch x := r.(type) {
			case *ssa.DebugRef:
			case *ssa.IndexAddr:
				c, ok := x.Index.(*ssa.Const)
				at, isArr := x.X.Type().(*types.Pointer).Elem().Underlying().(*types.Array)
				if x.X != v || !ok || !isArr || x.Block() != b || c.Value == nil {
					good = false
					return
				}
				i := c.Int64()
				if i < 0 || i >= at.Len() {
					good = false
					return
				}
				o.addrs = append(o.addrs, x)
				walk(x, append(slices.Clip(path), int(i)))
			case *ssa.FieldAddr:
				if x.X != v || x.Block() != b {
					good = false
					return
				}
				o.addrs = append(o.addrs, x)
				walk(x, append(slices.Clip(path), x.Field))
			case *ssa.Store:
				if x.Addr != v {
					o.other = true // the object or an address into it is stored somewhere
					useAt(x)
					continue
				}
				ls := litStore{st: x, path: path}
				if x.Block() == b {
					ls.val, ls.ok = s.staticValue(x.Val)
				}
				o.stores = append(o.stores, ls)
			case *ssa.UnOp:
				if x.Op == token.MUL && v == ssa.Value(a) && x.Block() == b {
					o.loads = append(o.loads, x)
				} else {
					o.other = true
				}
				useAt(x)
			default:
				o.other = true
				useAt(r)
			}
		}
	}
	walk(a, nil)
	if !good {
		return nil
	}
	return o
}

// constStores are the constant stores that run before the first use.
func (o *litObj) constStore(ls litStore, pos map[ssa.Instruction]int) bool {
	return ls.ok && pos[ls.st] < o.firstUse
}

// pureLocal reports whether o is a temporary that is only stored with
// constants and then loaded.
func (o *litObj) pureLocal(pos map[ssa.Instruction]int) bool {
	if o.other || len(o.loads) == 0 {
		return false
	}
	for _, ls := range o.stores {
		if !o.constStore(ls, pos) {
			return false
		}
	}
	return !o.nestedConstStores(pos)
}

// nestedConstStores reports whether one constant store of o writes inside
// a location another writes whole (a literal field then one of its fields:
// cfg := &Config{Notif: Notif{...}}; cfg.Notif.Bell = "toast"). The image
// keeps a whole value and the parts written into it apart, and renders the
// whole one, so such an object gets no image (tuios DefaultConfig lost
// Notif.Bell and Notif.Style).
func (o *litObj) nestedConstStores(pos map[ssa.Instruction]int) bool {
	for i, x := range o.stores {
		if !o.constStore(x, pos) {
			continue
		}
		for j, y := range o.stores {
			if i != j && o.constStore(y, pos) && len(x.path) < len(y.path) && isPrefix(x.path, y.path) {
				return true
			}
		}
	}
	return false
}

// image builds the image of o's constant stores, in order.
func (o *litObj) image(s *staticInit) *memImage {
	img := s.image(o.a.Type().(*types.Pointer).Elem())
	for _, ls := range o.stores {
		if !ls.ok {
			continue
		}
		at := img
		for _, i := range ls.path {
			at = at.at(i)
		}
		v := ls.val
		at.leaf, at.elems = &v, nil
	}
	return img
}

func (f *funcGen) skip(instr ssa.Instruction) {
	if f.skipInstr == nil {
		f.skipInstr = map[ssa.Instruction]bool{}
	}
	f.skipInstr[instr] = true
}

// skipAll drops a pure temporary: its stores, addresses, loads and itself.
func (f *funcGen) skipAll(o *litObj, done map[ssa.Instruction]bool) {
	for _, ls := range o.stores {
		f.skip(ls.st)
		done[ls.st] = true
	}
	for _, x := range o.addrs {
		f.skip(x)
	}
	for _, l := range o.loads {
		f.skip(l)
	}
	f.skip(o.a)
}

func allLoadsSkipped(skip map[ssa.Instruction]bool, loads []*ssa.UnOp) bool {
	for _, l := range loads {
		if !allSkipped(skip, *l.Referrers()) {
			return false
		}
	}
	return true
}

// constLit gives o an image if enough of its stores are constant.
func (f *funcGen) constLit(s *staticInit, o *litObj, pos map[ssa.Instruction]int, done map[ssa.Instruction]bool) {
	n := 0
	for _, ls := range o.stores {
		if o.constStore(ls, pos) {
			n++
		}
	}
	if n < minLitCopyStores || o.nestedConstStores(pos) {
		return
	}
	// A store that stays must not overlap a constant one.
	for _, d := range o.stores {
		if o.constStore(d, pos) {
			continue
		}
		for _, c := range o.stores {
			if o.constStore(c, pos) && (isPrefix(d.path, c.path) || isPrefix(c.path, d.path)) {
				return
			}
		}
	}
	slices.SortFunc(o.stores, func(x, y litStore) int { return pos[x.st] - pos[y.st] })
	img := s.image(o.a.Type().(*types.Pointer).Elem())
	for _, ls := range o.stores {
		if !o.constStore(ls, pos) {
			continue
		}
		at := img
		for _, i := range ls.path {
			at = at.at(i)
		}
		v := ls.val
		at.leaf, at.elems = &v, nil
		f.skip(ls.st)
		done[ls.st] = true
	}
	// Address computations used only by skipped stores.
	for _, x := range slices.Backward(o.addrs) {
		if allSkipped(f.skipInstr, *x.(ssa.Value).Referrers()) {
			f.skip(x)
		}
	}
	f.m.nstr++
	sym := fmt.Sprintf(".litc.%d", f.m.nstr)
	f.m.constGlobal(sym, "private", true, s.value(img))
	if f.litCopy == nil {
		f.litCopy = map[*ssa.Alloc]string{}
	}
	f.litCopy[o.a] = sym
}

func isPrefix(p, q []int) bool {
	return len(p) <= len(q) && slices.Equal(p, q[:len(p)])
}

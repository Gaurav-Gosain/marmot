package codegen

import (
	"go/token"
	"go/types"
	"os"

	"golang.org/x/tools/go/ssa"
)

// Escape analysis on SSA (SPEC 3a lever 4). x/tools SSA marks every
// variable whose address is taken as heap allocated (Alloc.Heap). A heap
// Alloc or a MakeClosure whose pointer provably stays inside the function
// is allocated on the stack instead. Pointers may flow into loads and
// stores through them, field and element addresses, unsafe.Pointer
// conversions, slicing, comparisons, deferred calls of the function, and
// parameters of static callees whose summary says the parameter does not
// escape (whole program, memoized, cycles count as escaping). Allocations
// in loops stay on the heap unless no pointer to them flows into a phi.

type escKey struct {
	fn *ssa.Function
	v  int // parameter index (receiver first), or -1-i for free variable i
}

// escapeState holds memoized summaries. Guarded by Program.mu.
type escapeState struct {
	done    map[escKey]bool // true: escapes
	running map[escKey]bool
}

// paramEscapes reports whether parameter i of fn may escape fn.
func (p *Program) paramEscapes(fn *ssa.Function, i int) bool {
	if len(fn.Blocks) == 0 {
		// Body-less (assembly or the C core): unknown unless //go:noescape.
		return !optEscapeExt || p.pragmas[fn]&pragmaNoescape == 0
	}
	if i >= len(fn.Params) {
		return true
	}
	return p.summary(escKey{fn, i}, fn.Params[i])
}

// paramToResult reports whether parameter i of fn leaves fn only through
// its results (gc's "leaking param content to result").
func (p *Program) paramToResult(fn *ssa.Function, i int) bool {
	if len(fn.Blocks) == 0 || i >= len(fn.Params) {
		return false
	}
	k := escKey{fn, -1000 - i}
	p.escMu.Lock()
	if r, ok := p.esc.done[k]; ok {
		p.escMu.Unlock()
		return !r
	}
	if p.esc.running[k] {
		p.escMu.Unlock()
		return false
	}
	p.esc.running[k] = true
	p.escMu.Unlock()
	r := p.escapesF(fn.Params[i], map[ssa.Value]bool{}, false, true)
	p.escMu.Lock()
	delete(p.esc.running, k)
	p.esc.done[k] = r
	p.escMu.Unlock()
	return !r
}

// freeVarEscapes reports whether free variable i of closure fn may escape.
func (p *Program) freeVarEscapes(fn *ssa.Function, i int) bool {
	if !optEscSummary || i >= len(fn.FreeVars) {
		return true
	}
	return p.summary(escKey{fn, -1 - i}, fn.FreeVars[i])
}

func (p *Program) summary(k escKey, v ssa.Value) bool {
	p.escMu.Lock()
	if p.esc.done == nil {
		p.esc.done = map[escKey]bool{}
		p.esc.running = map[escKey]bool{}
	}
	if r, ok := p.esc.done[k]; ok {
		p.escMu.Unlock()
		return r
	}
	if p.esc.running[k] {
		p.escMu.Unlock()
		return true // recursion: assume the worst
	}
	p.esc.running[k] = true
	p.escMu.Unlock()
	r := p.escapes(v, map[ssa.Value]bool{}, false)
	p.escMu.Lock()
	delete(p.esc.running, k)
	p.esc.done[k] = r
	p.escMu.Unlock()
	return r
}

// escapes reports whether pointer value v (or a value derived from it) may
// outlive the function. noPhi makes flow into a phi count as escaping.
func (p *Program) escapes(v ssa.Value, seen map[ssa.Value]bool, noPhi bool) bool {
	return p.escapesF(v, seen, noPhi, false)
}

// escapesF is escapes; with ret, returning v (or a value derived from it)
// from its function does not count as escaping.
func (p *Program) escapesF(v ssa.Value, seen map[ssa.Value]bool, noPhi, ret bool) bool {
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
		case *ssa.UnOp:
			if x.Op == token.ARROW {
				return true
			}
			// Loads through v do not leak v.
		case *ssa.Store:
			if x.Val == v {
				return true // v itself is written to memory
			}
		case *ssa.Field, *ssa.Index, *ssa.Extract:
			// A value read out of v holds no reference to v's memory unless
			// it has pointers itself.
			if optEscapeExt && len(ptrWords(x.(ssa.Value).Type(), p.opts.Sizes)) == 0 {
				continue
			}
			if p.escapesF(x.(ssa.Value), seen, noPhi, ret) {
				return true
			}
		case *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Slice, *ssa.ChangeType, *ssa.SliceToArrayPointer:
			if p.escapesF(x.(ssa.Value), seen, noPhi, ret) {
				return true
			}
		case *ssa.Convert:
			// Pointer <-> unsafe.Pointer conversions keep the pointer;
			// unsafe.Pointer -> uintptr loses track of it. Conversions
			// between strings and byte or rune slices copy.
			if optEscapeExt && (isString(v.Type()) || isString(x.Type())) {
				continue
			}
			if optEscapeExt && len(ptrWords(v.Type(), p.opts.Sizes)) == 0 {
				continue // a number converted to a number
			}
			if !isPtrRepr(x.Type()) {
				if optEscapeExt && isInteger(x.Type()) && uintOnly(x, 0) {
					continue // an address used only as a number (overlap checks)
				}
				return true
			}
			if p.escapesF(x, seen, noPhi, ret) {
				return true
			}
		case *ssa.Range:
			// Ranging over a string yields runes (Next copies them out).
			if !optEscapeExt || !isString(v.Type()) {
				return true
			}
		case *ssa.Return:
			if !ret {
				return true
			}
		case *ssa.Lookup:
			// Looking up a key (or in a map) does not keep the key; a
			// string lookup result aliases v only if v is the string.
			if !optEscapeExt {
				return true
			}
			if x.X == v && isString(v.Type()) {
				if len(ptrWords(x.Type(), p.opts.Sizes)) > 0 && p.escapesF(x, seen, noPhi, ret) {
					return true
				}
			}
		case *ssa.Phi:
			if noPhi || p.escapesF(x, seen, noPhi, ret) {
				return true
			}
		case *ssa.BinOp:
			// Comparisons do not leak.
		case *ssa.Call:
			if p.callLeaksF(&x.Call, v, x, seen, noPhi, ret) {
				return true
			}
		case *ssa.Defer:
			// A deferred call runs at function exit: storage reused by a
			// later loop iteration would be wrong by then.
			if inLoop(x.Block()) || p.callLeaks(&x.Call, v, nil) {
				return true
			}
		case *ssa.MakeClosure:
			fn, ok := x.Fn.(*ssa.Function)
			if !ok {
				return true
			}
			byv := p.byValueFreeVars(fn)
			byRef := false
			for i, b := range x.Bindings {
				if b == v && !byv[i] {
					byRef = true
					if p.freeVarEscapes(fn, i) {
						return true
					}
				}
			}
			// Captured by value: only a copy of the contents goes into the
			// closure. By reference: the closure must not escape either.
			if byRef && p.escapesF(x, seen, noPhi, ret) {
				return true
			}
		default:
			// Return, MakeInterface, Go, Send, Panic, MapUpdate, Select,
			// Lookup, Range, ...: assume the pointer leaks.
			return true
		}
	}
	return false
}

// callLeaks reports whether passing v to call c may leak it. res is the
// call's value (its result may carry v back), or nil for defer.
func (p *Program) callLeaks(c *ssa.CallCommon, v ssa.Value, res ssa.Value) bool {
	return p.callLeaksF(c, v, res, map[ssa.Value]bool{}, false, false)
}

// callLeaksF reports whether passing v to call c may let it escape; res is
// the call's value (nil for go and defer).
func (p *Program) callLeaksF(c *ssa.CallCommon, v ssa.Value, res ssa.Value, seen map[ssa.Value]bool, noPhi, ret bool) bool {
	if c.IsInvoke() {
		return true
	}
	if c.Value == v {
		return false // calling the closure itself
	}
	switch fn := c.Value.(type) {
	case *ssa.Builtin:
		switch fn.Name() {
		case "len", "cap", "print", "println", "copy", "ssa:wrapnilchk", "min", "max", "clear":
			// ssa:wrapnilchk returns its argument.
			if fn.Name() == "ssa:wrapnilchk" && res != nil {
				return p.escapesF(res, seen, noPhi, ret)
			}
			return false
		case "append":
			// The result may share s's array; appended elements are copied.
			if !optEscapeExt {
				return true
			}
			if len(c.Args) > 0 && c.Args[0] != v {
				if _, isSlice := v.Type().Underlying().(*types.Slice); isSlice || isString(v.Type()) {
					return false
				}
				return true
			}
			fallthrough
		case "SliceData", "StringData", "String", "Slice", "Add":
			// unsafe: the result points into v.
			if res == nil || !optEscapeExt {
				return true
			}
			return p.escapesF(res, seen, noPhi, ret)
		}
		return true
	case *ssa.Function:
		if !optEscapeExt && fn.Signature.Variadic() {
			return true
		}
		if !summaryAllowed(v, fn) {
			return true
		}
		toResult := false
		for i, a := range c.Args {
			if a == v && p.paramEscapes(fn, i) {
				// Leaking only through fn's results: then v escapes iff
				// the call's value does.
				if res == nil || !optEscapeExt || !p.paramToResult(fn, i) {
					return true
				}
				toResult = true
			}
		}
		if toResult {
			return p.escapesF(res, seen, noPhi, ret)
		}
		return false
	}
	return true
}

// stackAllocs decides which heap Allocs and MakeClosures of fn can live in
// its frame.
func (p *Program) stackAllocs(fn *ssa.Function) map[ssa.Value]bool {
	out := map[ssa.Value]bool{}
	if os.Getenv("MARMOT_NO_ESCAPE") != "" {
		return out
	}
	for _, b := range fn.Blocks {
		loop := inLoop(b)
		for _, instr := range b.Instrs {
			var v ssa.Value
			switch x := instr.(type) {
			case *ssa.Alloc:
				if !x.Heap {
					continue
				}
				if p.opts.Sizes.Sizeof(x.Type().(*types.Pointer).Elem()) > maxStackVar {
					continue
				}
				v = x
			case *ssa.MakeClosure:
				v = x
			default:
				continue
			}
			if !p.escapes(v, map[ssa.Value]bool{}, loop) {
				out[v] = true
			}
		}
	}
	return out
}

// byValueFreeVars returns the free variables of closure fn that can be
// captured by value: at every MakeClosure of fn the binding is an Alloc
// whose stores all happen before the closure is made (in the same block,
// earlier, or in a dominating block), that is not otherwise written, and
// fn only loads through the free variable. gc captures such variables by
// value too.
func (p *Program) byValueFreeVars(fn *ssa.Function) map[int]bool {
	if fn.Parent() == nil || fn.Synthetic != "" || len(fn.FreeVars) == 0 {
		return nil
	}
	p.escMu.Lock()
	if r, ok := p.byValue[fn]; ok {
		p.escMu.Unlock()
		return r
	}
	p.escMu.Unlock()
	out := map[int]bool{}
	refs := fn.Referrers()
	for i, fv := range fn.FreeVars {
		ok := refs != nil && len(*refs) > 0
		for _, r := range *fv.Referrers() {
			if u, isLoad := r.(*ssa.UnOp); isLoad && u.Op == token.MUL {
				continue
			}
			if _, dbg := r.(*ssa.DebugRef); dbg {
				continue
			}
			ok = false
		}
		if ok {
			for _, r := range *refs {
				mc, isMC := r.(*ssa.MakeClosure)
				if !isMC || mc.Fn != ssa.Value(fn) || !allocSettled(mc, mc.Bindings[i]) {
					ok = false
					break
				}
			}
		}
		if ok {
			out[i] = true
		}
	}
	p.escMu.Lock()
	if p.byValue == nil {
		p.byValue = map[*ssa.Function]map[int]bool{}
	}
	p.byValue[fn] = out
	p.escMu.Unlock()
	return out
}

// allocSettled reports whether b is an Alloc whose every store comes before
// mc (dominating it) and whose other uses only read it.
func allocSettled(mc *ssa.MakeClosure, b ssa.Value) bool {
	a, ok := b.(*ssa.Alloc)
	if !ok {
		return false
	}
	pos := func(instr ssa.Instruction) int {
		for i, x := range instr.Block().Instrs {
			if x == instr {
				return i
			}
		}
		return -1
	}
	for _, r := range *a.Referrers() {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case *ssa.UnOp:
			if x.Op != token.MUL {
				return false
			}
		case *ssa.Store:
			if x.Addr != ssa.Value(a) || x.Val == ssa.Value(a) {
				return false
			}
			sb, mb := x.Block(), mc.Block()
			if sb == mb {
				if pos(x) > pos(mc) {
					return false
				}
			} else if !sb.Dominates(mb) || inLoop(sb) {
				return false
			}
		case *ssa.MakeClosure:
			// Also captured by another closure: that one could write it.
			if x != mc {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// uintOnly reports whether integer value v (an address converted to
// uintptr) is only compared or combined into other such integers, and
// never converted back to a pointer, stored, passed or returned.
func uintOnly(v ssa.Value, depth int) bool {
	if depth > 8 {
		return false
	}
	for _, r := range *v.Referrers() {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case *ssa.BinOp:
			switch x.Op {
			case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			default:
				if !uintOnly(x, depth+1) {
					return false
				}
			}
		case *ssa.Convert:
			if !isInteger(x.Type()) || !uintOnly(x, depth+1) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

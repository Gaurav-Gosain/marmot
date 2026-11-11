package codegen

import (
	"os"
	"go/token"
	"go/types"

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
		return true // body-less: unknown
	}
	if i >= len(fn.Params) {
		return true
	}
	return p.summary(escKey{fn, i}, fn.Params[i])
}

// freeVarEscapes reports whether free variable i of closure fn may escape.
func (p *Program) freeVarEscapes(fn *ssa.Function, i int) bool {
	if i >= len(fn.FreeVars) {
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
		case *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Slice, *ssa.ChangeType, *ssa.Field, *ssa.Index,
			*ssa.Extract, *ssa.SliceToArrayPointer:
			if p.escapes(x.(ssa.Value), seen, noPhi) {
				return true
			}
		case *ssa.Convert:
			// Pointer <-> unsafe.Pointer conversions keep the pointer;
			// unsafe.Pointer -> uintptr loses track of it.
			if !isPtrRepr(x.Type()) {
				return true
			}
			if p.escapes(x, seen, noPhi) {
				return true
			}
		case *ssa.Phi:
			if noPhi || p.escapes(x, seen, noPhi) {
				return true
			}
		case *ssa.BinOp:
			// Comparisons do not leak.
		case *ssa.Call:
			if p.callLeaks(&x.Call, v, x) {
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
			if !ok || p.escapes(x, seen, noPhi) {
				return true
			}
			for i, b := range x.Bindings {
				if b == v && p.freeVarEscapes(fn, i) {
					return true
				}
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
				return p.escapes(res, map[ssa.Value]bool{}, false)
			}
			return false
		}
		return true
	case *ssa.Function:
		if fn.Signature.Variadic() {
			return true
		}
		for i, a := range c.Args {
			if a == v && p.paramEscapes(fn, i) {
				return true
			}
		}
		// The result may alias the argument (func f(p *T) *T { return p }):
		// returning counts as escaping in the summary, so a non-escaping
		// parameter cannot come back.
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

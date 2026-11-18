package codegen

import (
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Read-only []byte(s) arguments (MARMOT_ROCONV, a lifetime switch). A
// []byte(s) conversion whose result is only read can alias the string's
// bytes instead of copying them (readOnlyBytes). With the switch, the
// result may also be passed to a static callee whose parameter is never
// written through and does not escape: gc's rule for []byte(s) arguments
// (jsonwire.AppendQuote(b, []byte(s), ...) in encoding/json).
//
// String bytes may be in read-only memory, so a write the analysis misses
// is a crash: every unknown counts as a write. Design and review:
// .work/experiments/frontend-design-roconv.md (revision 2).

// wrKey is a parameter of a function.
type wrKey struct {
	fn *ssa.Function
	i  int
}

// wrQuery is one top-level question, as escQuery: summaries in progress
// count as written, and results that used that assumption stay local.
type wrQuery struct {
	running map[wrKey]bool
	cycle   bool
	local   map[wrKey]bool
}

// paramWritten reports whether fn may write the elements of slice
// parameter i (receiver first), or of a slice derived from it.
func (p *Program) paramWritten(fn *ssa.Function, i int) bool {
	return p.paramWrittenQ(&wrQuery{running: map[wrKey]bool{}, local: map[wrKey]bool{}}, fn, i)
}

func (p *Program) paramWrittenQ(q *wrQuery, fn *ssa.Function, i int) bool {
	// A generic body (not an instance) is never compiled: unknown. Instances
	// are summarized each on their own body.
	generic := fn.TypeParams().Len() > 0 && len(fn.TypeArgs()) == 0
	if len(fn.Blocks) == 0 || i >= len(fn.Params) || generic || !p.bodyLinks(fn) {
		return true
	}
	if _, ok := fn.Params[i].Type().Underlying().(*types.Slice); !ok {
		return true
	}
	k := wrKey{fn, i}
	p.escMu.Lock()
	r, ok := p.wrDone[k]
	p.escMu.Unlock()
	if ok {
		return r
	}
	if r, ok := q.local[k]; ok {
		q.cycle = true
		return r
	}
	if q.running[k] {
		q.cycle = true
		return true
	}
	q.running[k] = true
	outer := q.cycle
	q.cycle = false
	r = p.sliceWritten(q, fn.Params[i], map[ssa.Value]bool{})
	delete(q.running, k)
	if q.cycle {
		q.local[k] = r
	} else {
		p.escMu.Lock()
		if p.wrDone == nil {
			p.wrDone = map[wrKey]bool{}
		}
		p.wrDone[k] = r
		p.escMu.Unlock()
	}
	q.cycle = q.cycle || outer
	return r
}

// bodyLinks reports whether fn's body is the code that runs: not a
// //go:linkname push (another body may stand behind the symbol) and not
// in the runtime, whose functions the C core and intrinsics replace.
func (p *Program) bodyLinks(fn *ssa.Function) bool {
	if _, ok := p.linkname[fn]; ok {
		return false
	}
	if fn.Pkg == nil {
		return fn.Synthetic != ""
	}
	path := fn.Pkg.Pkg.Path()
	return path != "runtime" && !strings.HasPrefix(path, "internal/runtime/") && path != "unsafe" && !atomicPkgs[path]
}

// sliceWritten reports whether the elements of slice v may be written, or
// v may reach code or memory where they could be.
func (p *Program) sliceWritten(q *wrQuery, v ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return false
	}
	seen[v] = true
	refs := v.Referrers()
	if refs == nil {
		return false
	}
	for _, r := range *refs {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case *ssa.Slice, *ssa.Phi, *ssa.ChangeType:
			// Reslices, merges and named types alias the same array.
			if p.sliceWritten(q, x.(ssa.Value), seen) {
				return true
			}
		case *ssa.IndexAddr:
			if !onlyLoaded(x) {
				return true
			}
		case *ssa.SliceToArrayPointer:
			if !onlyLoaded(x) {
				return true
			}
		case *ssa.BinOp:
			// s == nil
		case *ssa.Convert:
			// string(s) copies (or is a temporary view that only reads).
			if !isString(x.Type()) {
				return true
			}
		case *ssa.Call:
			if p.callWrites(q, x, v) {
				return true
			}
		default:
			// Stores of the slice value, interfaces, closures, sends, map
			// updates, returns, go and defer: the holder may write later.
			return true
		}
	}
	return false
}

// onlyLoaded reports whether pointer x (an element or array address) is
// only read through: loads, and element addresses of an array pointer
// that are only loaded.
func onlyLoaded(x ssa.Value) bool {
	for _, r := range *x.Referrers() {
		switch u := r.(type) {
		case *ssa.DebugRef:
		case *ssa.UnOp:
			if u.Op != token.MUL {
				return false
			}
		case *ssa.IndexAddr:
			if u.X != x || !onlyLoaded(u) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// callWrites reports whether call c may write the elements of slice v,
// which it gets as an argument (or as the callee: never, v is a slice).
func (p *Program) callWrites(q *wrQuery, c *ssa.Call, v ssa.Value) bool {
	cc := &c.Call
	if cc.IsInvoke() {
		return true
	}
	if bi, ok := cc.Value.(*ssa.Builtin); ok {
		switch bi.Name() {
		case "len", "cap", "print", "println":
			return false
		case "copy", "append":
			// The source is only read; the destination is written.
			return len(cc.Args) == 0 || cc.Args[0] == v
		}
		return true
	}
	fn, ok := cc.Value.(*ssa.Function)
	if !ok {
		return true
	}
	for i, a := range cc.Args {
		if a != v {
			continue
		}
		if !p.sameUnit(c.Parent(), fn) {
			w, ok := p.importWritten(fn)
			if !ok || i >= 64 || w&(1<<i) != 0 {
				return true
			}
			continue
		}
		if p.paramWrittenQ(q, fn, i) {
			return true
		}
	}
	return false
}

// importWritten returns the written bits of fn from its package's tags
// (key "w:" and the symbol; ComputeEscTags with MARMOT_ROCONV).
func (p *Program) importWritten(fn *ssa.Function) (uint64, bool) {
	if p.importTags == nil {
		return 0, false
	}
	o := p.owner[fn]
	if o == nil || p.importTags[o] == nil {
		return 0, false
	}
	t, ok := p.importTags[o]["w:"+p.symbol(fn)]
	return t, ok
}

// roArgOK reports whether []byte(s) conversion v may alias the string when
// passed to call c: the callee never writes the elements and v does not
// escape it, not even through its results.
func (p *Program) roArgOK(c *ssa.Call, v ssa.Value) bool {
	if !optROConv || c.Call.IsInvoke() {
		return false
	}
	if _, ok := c.Call.Value.(*ssa.Function); !ok {
		return false
	}
	q := &wrQuery{running: map[wrKey]bool{}, local: map[wrKey]bool{}}
	return !p.callWrites(q, c, v) && !p.callLeaks(&c.Call, v, nil)
}

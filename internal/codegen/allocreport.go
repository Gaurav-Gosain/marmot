package codegen

import (
	"encoding/json"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"sync"

	"golang.org/x/tools/go/ssa"
)

// Allocation report: with MARMOT_ALLOC_REPORT=FILE every allocation site the
// code generator emits is appended to FILE as one JSON object per line:
//
//	{"fn", "pos", "kind", "type", "bytes", "where", "reason", "loop", "final"}
//
// kind is new, composite, var (an escaping local), varargs, closure,
// make-slice, make-map, make-chan, box (a value converted to an interface),
// convert (string and byte or rune slice conversions); where is stack,
// heap, region or static. reason says why a heap allocation could not stay
// in the frame (the first escaping use found, or the size). final is false
// when the go dialect is emitted, since the back end may still move the
// object. The report is a side output: it changes nothing in the code, and
// the cost when MARMOT_ALLOC_REPORT is unset is one check per site.

var allocReport = sync.OnceValue(func() *allocWriter {
	p := os.Getenv("MARMOT_ALLOC_REPORT")
	if p == "" {
		return nil
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return &allocWriter{f: f}
})

type allocWriter struct {
	mu sync.Mutex
	f  *os.File
}

type allocRecord struct {
	Fn     string `json:"fn"`
	Pos    string `json:"pos"`
	Kind   string `json:"kind"`
	Type   string `json:"type,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Where  string `json:"where"`
	Reason string `json:"reason,omitempty"`
	Loop   bool   `json:"loop,omitempty"`
	Final  bool   `json:"final"`
}

// reportAlloc records one allocation decision. t is the allocated type
// (nil when the size is dynamic).
func (f *funcGen) reportAlloc(v ssa.Value, kind string, t types.Type, where string) {
	w := allocReport()
	if w == nil {
		return
	}
	pos := v.Pos()
	if mc, ok := v.(*ssa.MakeClosure); ok {
		if fn, ok := mc.Fn.(*ssa.Function); ok {
			pos = fn.Pos()
		}
	}
	if !pos.IsValid() {
		if instr, ok := v.(ssa.Instruction); ok && instr.Block() != nil {
			for _, i := range instr.Block().Instrs {
				if i.Pos().IsValid() {
					pos = i.Pos()
					break
				}
			}
		}
	}
	rec := allocRecord{Fn: f.fn.String(), Kind: kind, Where: where, Final: !f.goOps()}
	if pos.IsValid() {
		p := f.fn.Prog.Fset.Position(pos)
		rec.Pos = fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
	}
	if instr, ok := v.(ssa.Instruction); ok && instr.Block() != nil {
		rec.Loop = inLoop(instr.Block())
	}
	if t != nil {
		var q types.Qualifier
		if f.fn.Pkg != nil {
			q = types.RelativeTo(f.fn.Pkg.Pkg)
		}
		rec.Type = types.TypeString(t, q)
		rec.Bytes = f.m.p.opts.Sizes.Sizeof(t)
	}
	if where == "heap" {
		switch {
		case t != nil && rec.Bytes > maxStackVar && (kind == "new" || kind == "composite" || kind == "var"):
			rec.Reason = fmt.Sprintf("too large for the frame (%d bytes, the limit is %d)", rec.Bytes, maxStackVar)
		default:
			rec.Reason = f.m.p.escapeWhy(v, map[ssa.Value]bool{}, rec.Loop)
		}
	}
	data, _ := json.Marshal(rec)
	w.mu.Lock()
	w.f.Write(append(data, '\n'))
	w.mu.Unlock()
}

// allocKind names an Alloc by its SSA comment.
func allocKind(a *ssa.Alloc) string {
	switch a.Comment {
	case "complit", "slicelit":
		return "composite"
	case "makeslice":
		return "make-slice"
	case "new":
		return "new"
	case "varargs":
		return "varargs"
	}
	return "var"
}

// escapeWhy describes the first use that makes v escape, following the
// cases of escapesF. It is only used for the report; the decision itself
// is escapesF's.
func (p *Program) escapeWhy(v ssa.Value, seen map[ssa.Value]bool, noPhi bool) string {
	if seen[v] {
		return ""
	}
	seen[v] = true
	refs := v.Referrers()
	if refs == nil {
		return "its uses are not known"
	}
	at := func(i ssa.Instruction) string {
		if pos := i.Pos(); pos.IsValid() && i.Parent() != nil {
			ps := i.Parent().Prog.Fset.Position(pos)
			return fmt.Sprintf(" (line %d)", ps.Line)
		}
		return ""
	}
	for _, r := range *refs {
		switch x := r.(type) {
		case *ssa.DebugRef:
		case *ssa.UnOp:
			if x.Op == token.ARROW {
				return "received from a channel" + at(x)
			}
		case *ssa.Store:
			if x.Val == v {
				if _, ok := x.Addr.(*ssa.Global); ok {
					return "stored in a global variable" + at(x)
				}
				return "stored in memory that outlives the function" + at(x)
			}
		case *ssa.Field, *ssa.Index, *ssa.Extract, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Slice, *ssa.ChangeType, *ssa.SliceToArrayPointer, *ssa.Convert:
			if why := p.escapeWhy(x.(ssa.Value), seen, noPhi); why != "" {
				return why
			}
		case *ssa.Return:
			return "returned from the function" + at(x)
		case *ssa.Phi:
			if noPhi {
				return "allocated in a loop and kept across iterations" + at(x)
			}
			if why := p.escapeWhy(x, seen, noPhi); why != "" {
				return why
			}
		case *ssa.Call:
			if p.callLeaks(&x.Call, v, x) {
				if callee := x.Call.StaticCallee(); callee != nil {
					return fmt.Sprintf("passed to %s, which may keep it", callee.String()) + at(x)
				}
				if x.Call.IsInvoke() {
					return "passed to an interface method, which may keep it" + at(x)
				}
				return "passed to a call through a function value, which may keep it" + at(x)
			}
		case *ssa.Defer:
			return "used by a deferred call" + at(x)
		case *ssa.Go:
			return "passed to a goroutine" + at(x)
		case *ssa.Send:
			return "sent on a channel" + at(x)
		case *ssa.MakeInterface:
			return "converted to an interface" + at(x)
		case *ssa.MapUpdate:
			return "stored in a map" + at(x)
		case *ssa.MakeClosure:
			fn, ok := x.Fn.(*ssa.Function)
			if !ok {
				return "captured by a closure" + at(x)
			}
			byv := p.byValueFreeVars(fn)
			for i, b := range x.Bindings {
				if b == v && !byv[i] && p.freeVarEscapes(fn, i) {
					return "captured by a closure whose body lets it escape" + at(x)
				}
			}
			if why := p.escapeWhy(x, seen, noPhi); why != "" {
				return "captured by a closure that is " + why
			}
		case *ssa.Panic:
			return "passed to panic" + at(x)
		}
	}
	if !p.escapes(v, map[ssa.Value]bool{}, noPhi) {
		return ""
	}
	return "escape analysis could not prove that it stays in the function"
}

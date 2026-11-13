package codegen

import (
	"encoding/json"
	"fmt"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Allocation report: with Options.AllocReport set (marmot build -allocs, or
// MARMOT_ALLOC_REPORT=FILE, read per build by the driver) every allocation
// site the code generator emits is written as one JSON object per line:
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
// the cost when no report is asked for is one nil check per site.

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
	w := f.m.p.opts.AllocReport
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
		pos = nearPos(v)
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
	f.m.p.allocMu.Lock()
	// The writer the build was handed: marmot build -allocs checks its
	// errors after the build, and the report is advice, not output.
	_, _ = w.Write(append(data, '\n'))
	f.m.p.allocMu.Unlock()
}

// nearPos places a site that has no position of its own (an implicit
// conversion to an interface, say): at its first use that has one, else at
// the next instruction of its block that has one, else the previous one.
func nearPos(v ssa.Value) token.Pos {
	if refs := v.Referrers(); refs != nil {
		for _, r := range *refs {
			if r.Pos().IsValid() {
				return r.Pos()
			}
		}
	}
	instr, ok := v.(ssa.Instruction)
	if !ok || instr.Block() == nil {
		return token.NoPos
	}
	instrs := instr.Block().Instrs
	at := 0
	for i, x := range instrs {
		if x == instr {
			at = i
			break
		}
	}
	for i := at + 1; i < len(instrs); i++ {
		if instrs[i].Pos().IsValid() {
			return instrs[i].Pos()
		}
	}
	for i := at - 1; i >= 0; i-- {
		if instrs[i].Pos().IsValid() {
			return instrs[i].Pos()
		}
	}
	return token.NoPos
}

// localBase is the Alloc an address points into through fields and
// elements, or nil.
func localBase(v ssa.Value) *ssa.Alloc {
	for {
		switch x := v.(type) {
		case *ssa.Alloc:
			return x
		case *ssa.FieldAddr:
			v = x.X
		case *ssa.IndexAddr:
			if _, ok := x.X.Type().Underlying().(*types.Pointer); !ok {
				return nil
			}
			v = x.X
		default:
			return nil
		}
	}
}

// convType is the type of the bytes a string conversion copies when its
// length is constant ([N]byte for []byte("..."), nil otherwise).
func convType(v *ssa.Convert) types.Type {
	if c, ok := v.X.(*ssa.Const); ok && c.Value != nil && c.Value.Kind() == constant.String {
		return types.NewArray(types.Typ[types.Byte], int64(len(constant.StringVal(c.Value))))
	}
	return nil
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
				if a := localBase(x.Addr); a != nil {
					what := "a local variable that is"
					if a.Comment == "varargs" {
						what = "the variadic arguments, which are"
					} else if allocKind(a) == "composite" {
						what = "a composite literal that is"
					}
					if why := p.escapeWhy(a, seen, noPhi); why != "" {
						return "stored in " + what + " " + why
					}
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
				if b, ok := x.Call.Value.(*ssa.Builtin); ok {
					return fmt.Sprintf("passed to %s, which may keep it", b.Name()) + at(x)
				}
				if x.Call.IsInvoke() {
					return "passed to an interface method, which may keep it" + at(x)
				}
				return "passed to a call through a function value, which may keep it" + at(x)
			}
		case *ssa.Defer:
			if inLoop(x.Block()) {
				return "used by a deferred call in a loop" + at(x)
			}
			if p.callLeaks(&x.Call, v, nil) {
				return "passed to a deferred call that may keep it" + at(x)
			}
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

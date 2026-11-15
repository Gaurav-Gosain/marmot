package verify

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/verify/smt"
	"golang.org/x/tools/go/ssa"
)

// Result is the outcome for one package.
type Result struct {
	Funcs []*FuncResult
	// Summaries are the package's function summaries, for callers.
	Summaries map[*ssa.Function]*FnSummary
	fset      *token.FileSet
}

// FuncResult is the outcome for one function.
type FuncResult struct {
	Fn *ssa.Function
	// Name is the function's full name (pkg.F, (*pkg.T).M, pkg.F$1).
	Name string
	// Assumes are the function's assumption holes (design 4.4): unknown
	// callees, unsafe, reflect. Engine A's proofs do not use them (it
	// keeps no memory facts and treats call results as arbitrary), so they
	// are listed for the CI policy, not attached to each proof.
	Assumes []string
	// Recovers is set when a deferred closure calls recover() directly
	// and never re-panics.
	Recovers    bool
	Obligations []*ObResult
	// Invariants are engine B's checks of engine A's loop invariants: an
	// invariant is used only when its base and step are both unsat.
	Invariants []InvariantResult
	// Effects is set when the function's calls reach files, processes,
	// the network or system calls (directly, through same-package
	// callees, or through other packages' functions by their summary).
	Effects bool
}

// InvariantResult is the check of one loop header's invariant.
type InvariantResult struct {
	Block      int
	Facts      int // facts kept, of Of computed by engine A
	Of         int
	Base, Step string // the last answers (unsat when the kept set holds)
}

// modelEnv turns a solver model into an evaluation environment (array
// bytes come as "name[i]").
func modelEnv(m map[string]uint64) *smt.Env {
	env := &smt.Env{BV: map[string]uint64{}, Arr: map[string]map[uint64]byte{}}
	for k, v := range m {
		if i := strings.IndexByte(k, '['); i > 0 && strings.HasSuffix(k, "]") {
			idx, err := strconv.ParseUint(k[i+1:len(k)-1], 10, 64)
			if err == nil {
				if env.Arr[k[:i]] == nil {
					env.Arr[k[:i]] = map[uint64]byte{}
				}
				env.Arr[k[:i]][idx] = byte(v)
			}
			continue
		}
		env.BV[k] = v
	}
	return env
}

// ObResult is the outcome for one obligation.
type ObResult struct {
	Ob     *Obligation
	Status Status
	// Reason is set for unknown: loop, heap-input, unknown-callee,
	// call-result, global, unsafe.
	Reason string
	Dead   bool
	// Class is "manifest" or "latent" for open obligations.
	Class string
	// Manifest guard mismatch (rule 1).
	Rule      string
	Condition string
	Labels    []Label
	// Via is the obligation of a callee inlined at a call in this
	// function; the span is the callee's.
	Via *Obligation
	// Assumes the result depends on, besides no-races.
	Assumes []string
	// Engine is the engine that decided: "" for engine A, "smt" when
	// engine B proved the obligation or found a model for it.
	Engine string
	// Model is engine B's input that violates the obligation, as Go
	// arguments of the function (to confirm by running).
	Model []string
}

// Label is a secondary span with a text.
type Label struct {
	Pos, End token.Pos
	Text     string
}

// Options select what is analyzed.
type Options struct {
	// Inline is the size limit, in SSA instructions, of same-package
	// callees analyzed in their caller's context (0: no inlining).
	Inline int
	// Summaries gives the summaries of functions of other packages (from
	// analysis facts); nil: none.
	Summaries func(*ssa.Function) *FnSummary
	// Solver, when set, runs engine B on what engine A leaves open in
	// loop-free functions.
	Solver *smt.Solver
}

// DefaultOptions are the options of marmot verify.
var DefaultOptions = Options{Inline: 30}

// AnalyzePackage runs tier 1 on the source functions of pkg.
func AnalyzePackage(fset *token.FileSet, files []*ast.File, funcs []*ssa.Function, opts Options) *Result {
	idx := newSpanIndex(files)
	_ = idx
	r := &Result{Summaries: map[*ssa.Function]*FnSummary{}}
	own := map[*ssa.Function]bool{}
	for _, fn := range funcs {
		own[fn] = true
	}
	sums := func(f *ssa.Function) *FnSummary {
		if own[f] {
			return r.Summaries[f] // nil until analyzed: cycles get none
		}
		if opts.Summaries != nil {
			return opts.Summaries(f)
		}
		return nil
	}
	effects := map[*ssa.Function]bool{}
	for _, fn := range calleesFirst(funcs) {
		if fn.Blocks == nil || fn.Synthetic != "" {
			continue
		}
		// Tests are not verified: their panics are failures the test
		// reports itself.
		if fset != nil && strings.HasSuffix(fset.Position(fn.Pos()).Filename, "_test.go") {
			continue
		}
		fr, sum := analyzeFunc(fset, fn, opts, sums)
		fr.Effects = hasEffects(fn, sums, effects)
		effects[fn] = fr.Effects
		if fr.Effects {
			if sum == nil {
				sum = &FnSummary{}
			}
			sum.Effects = true
		}
		r.Funcs = append(r.Funcs, fr)
		if sum != nil {
			r.Summaries[fn] = sum
		}
	}
	slices.SortStableFunc(r.Funcs, func(a, b *FuncResult) int { return int(a.Fn.Pos() - b.Fn.Pos()) })
	return r
}

// effectPkgs are packages whose functions touch files, processes, the
// network or the system.
var effectPkgs = map[string]bool{"os": true, "os/exec": true, "os/signal": true, "os/user": true, "syscall": true, "net": true, "net/http": true, "net/rpc": true, "net/smtp": true, "plugin": true, "io/ioutil": true, "golang.org/x/sys/unix": true, "golang.org/x/sys/windows": true, "runtime/debug": true}

// hasEffects reports whether fn's calls reach effectPkgs: a call into
// one of them, a same-package callee with effects, or a callee whose
// summary says so. Closures defined in fn count as fn.
func hasEffects(fn *ssa.Function, sums func(*ssa.Function) *FnSummary, done map[*ssa.Function]bool) bool {
	seen := map[*ssa.Function]bool{}
	var visit func(f *ssa.Function) bool
	visit = func(f *ssa.Function) bool {
		if seen[f] {
			return false
		}
		seen[f] = true
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				var g *ssa.Function
				switch in := in.(type) {
				case ssa.CallInstruction:
					g = in.Common().StaticCallee()
				case *ssa.MakeClosure:
					g, _ = in.Fn.(*ssa.Function)
				}
				if g == nil {
					continue
				}
				if p := g.Pkg; p != nil && effectPkgs[p.Pkg.Path()] {
					return true
				}
				if g.Pkg == fn.Pkg {
					if e, ok := done[g]; ok {
						if e {
							return true
						}
						continue
					}
					if g.Parent() != nil && visit(g) {
						return true
					}
					continue
				}
				if s := sums(g); s != nil && s.Effects {
					return true
				}
			}
		}
		return false
	}
	return visit(fn)
}

// calleesFirst orders funcs so that static callees in the list come
// before their callers (cycles in source order).
func calleesFirst(funcs []*ssa.Function) []*ssa.Function {
	in := map[*ssa.Function]bool{}
	for _, f := range funcs {
		in[f] = true
	}
	state := map[*ssa.Function]int{} // 1 visiting, 2 done
	var out []*ssa.Function
	var visit func(f *ssa.Function)
	visit = func(f *ssa.Function) {
		if state[f] != 0 {
			return
		}
		state[f] = 1
		for _, b := range f.Blocks {
			for _, ins := range b.Instrs {
				if c, ok := ins.(ssa.CallInstruction); ok {
					if g := c.Common().StaticCallee(); g != nil && in[g] {
						visit(g)
					}
				}
			}
		}
		state[f] = 2
		out = append(out, f)
	}
	for _, f := range funcs {
		visit(f)
	}
	return out
}

func analyzeFunc(fset *token.FileSet, fn *ssa.Function, opts Options, sums func(*ssa.Function) *FnSummary) (*FuncResult, *FnSummary) {
	fr := &FuncResult{Fn: fn, Name: fn.String(), Recovers: recovers(fn), Assumes: assumptions(fn)}
	obs := obligations(fn)
	byInstr := map[ssa.Instruction][]*Obligation{}
	for _, o := range obs {
		byInstr[o.Instr] = append(byInstr[o.Instr], o)
	}
	var inl *inliner
	if opts.Inline > 0 {
		inl = &inliner{limit: opts.Inline, caller: fn}
	}
	e := newEngine(fn, inl, 0)
	e.sums = sums
	cx := newCtx()
	t0 := time.Now()
	e.run(newState(cx))
	if os.Getenv("MARMOT_VERIFY_SLOW") != "" {
		if d := time.Since(t0); d > 50*time.Millisecond {
			n := 0
			for _, b := range fn.Blocks {
				n += len(b.Instrs)
			}
			maxF := 0
			for _, s := range e.in {
				if s != nil && len(s.d) > maxF {
					maxF = len(s.d)
				}
			}
			fmt.Fprintf(os.Stderr, "slow %s %v instrs=%d blocks=%d maxfacts=%d work=%d visits=%d\n", fn, d, n, len(fn.Blocks), maxF, workBudget-cx.work, e.visits)
		}
	}
	if cx.work < 0 {
		for _, o := range obs {
			fr.Obligations = append(fr.Obligations, &ObResult{Ob: o, Status: Unknown, Reason: "budget"})
		}
		return fr, nil
	}
	verdicts := map[*Obligation]verdict{}
	debug := os.Getenv("MARMOT_VERIFY_DEBUG") != "" && os.Getenv("MARMOT_VERIFY_DEBUG") == fn.Name()
	if debug {
		var buf bytes.Buffer
		ssa.WriteFunction(&buf, fn)
		fmt.Fprint(os.Stderr, buf.String())
		for _, b := range fn.Blocks {
			fmt.Fprintf(os.Stderr, "in[%d]: %v\n", b.Index, e.in[b])
		}
	}
	var rets []retState
	e.evaluate(func(in ssa.Instruction, s *state) {
		if r, ok := in.(*ssa.Return); ok {
			rets = append(rets, e.returnStates(r, s)...)
		}
		for _, o := range byInstr[in] {
			verdicts[o] = checkObligation(o, s)
			if debug {
				fmt.Fprintf(os.Stderr, "%s at %v: %+v\n  state %v\n", o.Key, in, verdicts[o], s)
			}
		}
	})
	for _, o := range obs {
		v, ok := verdicts[o]
		if !ok {
			v = verdict{proved: true, dead: true}
		}
		fr.Obligations = append(fr.Obligations, classify(fn, fr, o, v))
	}
	if inl != nil {
		fr.Obligations = append(fr.Obligations, inl.found...)
	}
	if opts.Solver != nil {
		heads := map[*ssa.BasicBlock]*state{}
		for b, s := range e.in {
			if isLoopHeader(b) && s != nil {
				heads[b] = s
			}
		}
		engineB(fn, fr, opts.Solver, heads)
	}
	var sum *FnSummary
	if fn.Recover == nil {
		sum = summarize(fn, rets)
	}
	return fr, sum
}

// engineBBudget bounds the solver time of one function.
const engineBBudget = 2 * time.Second

// engineB asks the solver about the obligations engine A left open in a
// loop-free function: unsat proves them, sat gives an input to confirm.
func engineB(fn *ssa.Function, fr *FuncResult, s *smt.Solver, heads map[*ssa.BasicBlock]*state) {
	if os.Getenv("MARMOT_VERIFY_DEBUG") == fn.Name() {
		fmt.Fprintf(os.Stderr, "engine B on %s:", fn.Name())
		for _, o := range fr.Obligations {
			fmt.Fprintf(os.Stderr, " %s=%s/%s", o.Ob.Key, o.Status, o.Reason)
		}
		fmt.Fprintln(os.Stderr)
	}
	var open []*ObResult
	for _, o := range fr.Obligations {
		if o.Via != nil || o.Reason == "budget" || o.Reason == "receiver" {
			continue
		}
		switch o.Status {
		case Latent, Unknown, Counterexample:
			open = append(open, o)
		}
	}
	if len(open) == 0 || encodable(fn) != "" {
		return
	}
	enc, err := encodeFunc(fn, heads)
	if os.Getenv("MARMOT_VERIFY_DEBUG") == fn.Name() {
		fmt.Fprintf(os.Stderr, "engine B %s: %d open, encode error %v, %d violations\n", fn.Name(), len(open), err, func() int {
			if enc == nil {
				return 0
			}
			return len(enc.Viol)
		}())
	}
	if err != nil {
		return
	}
	// Loop invariants are obligations too: the facts of each header's
	// invariant are kept only when base and step are unsat for the set
	// kept (Houdini: a model of a failing check drops the facts it
	// breaks, until both checks are unsat). Outer headers first, so an
	// inner loop's step may assume the outer invariant.
	var hs []*ssa.BasicBlock
	for h := range enc.Heads {
		hs = append(hs, h)
	}
	slices.SortFunc(hs, func(a, b *ssa.BasicBlock) int { return a.Index - b.Index })
	verified := map[*ssa.BasicBlock]bool{}
	ctxI, cancelI := context.WithTimeout(context.Background(), engineBBudget)
	defer cancelI()
	for _, h := range hs {
		n0 := len(enc.Heads[h])
		base, step := "unsat", "unsat"
		for iter := 0; iter < 12 && len(enc.Heads[h]) > 0; iter++ {
			keep := make([]bool, len(enc.Heads[h]))
			for k := range keep {
				keep[k] = true
			}
			changed := false
			for _, which := range []string{"base", "step"} {
				q := enc.Base[h]
				fs := enc.BaseF[h]
				if which == "step" {
					q, fs = enc.StepQuery(h), enc.StepF[h]
				}
				res := s.Check(ctxI, append(append([]*smt.T(nil), enc.Assumes...), q))
				switch res.Answer {
				case smt.Unsat:
					continue
				case smt.Sat:
					env := modelEnv(res.Model)
					for k, f := range fs {
						if smt.Eval(f, env) != 0 {
							keep[k], changed = false, true
						}
					}
				}
				if which == "base" {
					base = string(res.Answer)
				} else {
					step = string(res.Answer)
				}
				if res.Answer != smt.Sat || !changed {
					// unknown, or a model that breaks no single fact: give up.
					for k := range keep {
						keep[k] = false
					}
					changed = true
				}
				break
			}
			if !changed {
				base, step = "unsat", "unsat"
				break
			}
			enc.KeepFacts(h, keep)
		}
		verified[h] = len(enc.Heads[h]) > 0
		fr.Invariants = append(fr.Invariants, InvariantResult{Block: h.Index, Facts: len(enc.Heads[h]), Of: n0, Base: base, Step: step})
	}
	enc.DropUnverified(verified)
	var qs []*smt.T
	var qo []*ObResult
	for _, o := range open {
		if q := enc.Query(o.Ob.Key); q != nil {
			qs, qo = append(qs, q), append(qo, o)
		}
	}
	if len(qs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), engineBBudget)
	defer cancel()
	results := s.CheckMany(ctx, enc.Assumes, qs)
	for i, res := range results {
		o := qo[i]
		switch res.Answer {
		case smt.Unsat:
			// No input reaches the operation with the check failing:
			// engine A's open result (or guard mismatch) was imprecise.
			o.Status, o.Class, o.Rule, o.Condition, o.Labels, o.Reason, o.Engine = Proved, "", "", "", nil, "", "smt"
		case smt.Sat:
			if args := modelArgs(enc.Params, res.Model); args != nil {
				o.Model, o.Engine = args, "smt"
			}
		}
	}
}

// modelArgs turns a model into Go arguments, nil when an input is too
// large to build.
func modelArgs(ps []encParam, m map[string]uint64) []string {
	var out []string
	for _, p := range ps {
		switch p.Kind {
		case "bool":
			out = append(out, strconv.FormatBool(m[p.Name] != 0))
		case "int":
			s := uint(64 - p.Bits)
			out = append(out, strconv.FormatInt(int64(m[p.Name]<<s)>>s, 10))
		case "uint":
			out = append(out, strconv.FormatUint(m[p.Name], 10))
		case "slice":
			n, c := m[p.Name+"_len"], m[p.Name+"_cap"]
			if c > 4096 || n > c {
				return nil
			}
			out = append(out, fmt.Sprintf("make(%s, %d, %d)", p.Type, n, c))
		case "bytes", "string":
			n, c := m[p.Name+"_len"], m[p.Name+"_cap"]
			if p.Kind == "string" {
				c = n
			}
			if c > 4096 || n > c {
				return nil
			}
			data := make([]byte, c)
			for i := range data {
				data[i] = byte(m[fmt.Sprintf("%s_arr[%d]", p.Name, i)])
			}
			if p.Kind == "string" {
				out = append(out, strconv.Quote(string(data)))
			} else {
				out = append(out, fmt.Sprintf("[]byte(%s)[:%d:%d]", strconv.Quote(string(data)), n, c))
			}
		default:
			return nil
		}
	}
	return out
}

// classify turns engine A's verdict into a status.
func classify(fn *ssa.Function, fr *FuncResult, o *Obligation, v verdict) *ObResult {
	r := &ObResult{Ob: o, Dead: v.dead}
	switch {
	case v.proved:
		r.Status = Proved
		return r
	case fr.Recovers:
		r.Status = Recovered
		return r
	}
	if v.mismatch != nil {
		if m := describeMismatch(fn, o, v.mismatch, nil); m != nil {
			return m
		}
	}
	if v.fails && o.Kind != KPanic {
		r.Status, r.Class, r.Rule = Counterexample, "manifest", "always"
		return r
	}
	if o.Receiver {
		// A method called on a nil receiver: the caller's precondition,
		// counted but not reported.
		r.Status, r.Reason = Unknown, "receiver"
		return r
	}
	reason := sourceReason(fn, o)
	if reason == "" {
		r.Status, r.Class = Latent, "latent"
	} else {
		r.Status, r.Reason = Unknown, reason
	}
	return r
}

// describeMismatch builds the manifest result of a guard mismatch, or nil
// when the quantity has no name in the source (so no condition can be
// printed).
func describeMismatch(fn *ssa.Function, o *Obligation, m *mismatch, via *ssa.Call) *ObResult {
	q := nodeName(m.q)
	if q == "" {
		return nil
	}
	var cond string
	if m.other.k == nZero && m.other.v == nil {
		if m.lo == m.hi {
			cond = fmt.Sprintf("%s == %d", q, m.lo)
		} else if m.hi == inf {
			cond = fmt.Sprintf("%s >= %d", q, m.lo)
		} else {
			cond = fmt.Sprintf("%d <= %s && %s <= %d", m.lo, q, q, m.hi)
		}
	} else {
		other := nodeName(m.other)
		if other == "" {
			return nil
		}
		// q - other in [lo, hi].
		switch {
		case m.lo == m.hi && m.lo == 0:
			cond = fmt.Sprintf("%s == %s", q, other)
		case m.lo == m.hi:
			cond = fmt.Sprintf("%s == %s+%d", q, other, m.lo)
		default:
			cond = fmt.Sprintf("%s >= %s+%d", q, other, m.lo)
		}
	}
	r := &ObResult{Ob: o, Status: Counterexample, Class: "manifest", Rule: "guard-mismatch", Condition: cond}
	r.Labels = append(r.Labels, Label{Pos: m.guard, Text: "this guard allows " + cond})
	if via != nil {
		r.Labels = append(r.Labels, Label{Pos: via.Pos(), Text: "the call panics when " + cond})
	}
	return r
}

// nodeName is the source name of a node: a parameter, len or cap of a
// parameter, or a constant.
func nodeName(n node) string {
	var base string
	switch v := n.v.(type) {
	case *ssa.Parameter:
		base = v.Name()
	case *ssa.Phi:
		// A local variable merged at a loop header or join.
		if v.Comment == "" || strings.ContainsAny(v.Comment, " .:") {
			return ""
		}
		base = v.Comment
	case *ssa.Const:
		if n.k == nVal {
			return v.Value.String()
		}
	default:
		return ""
	}
	switch n.k {
	case nLen:
		return "len(" + base + ")"
	case nCap:
		return "cap(" + base + ")"
	}
	return base
}

// sourceReason is why an open obligation is not latent, from where its
// operands come from; "" when they are functions of the parameters only.
func sourceReason(fn *ssa.Function, o *Obligation) string {
	var roots []ssa.Value
	for _, v := range []ssa.Value{o.X, o.I, o.Hi, o.Max} {
		if v != nil {
			roots = append(roots, v)
		}
	}
	if o.Kind == KPanic {
		return "explicit"
	}
	seen := map[ssa.Value]bool{}
	reasons := map[string]bool{}
	var walk func(v ssa.Value, depth int)
	walk = func(v ssa.Value, depth int) {
		if v == nil || seen[v] {
			return
		}
		seen[v] = true
		if depth > 40 {
			reasons["budget"] = true
			return
		}
		switch v := v.(type) {
		case *ssa.Parameter, *ssa.Const, *ssa.Function, *ssa.Alloc, *ssa.MakeSlice,
			*ssa.MakeMap, *ssa.MakeChan, *ssa.MakeClosure:
		case *ssa.FreeVar:
			reasons["heap-input"] = true
		case *ssa.Global:
			reasons["global"] = true
		case *ssa.UnOp:
			if v.Op == token.MUL {
				if g, ok := v.X.(*ssa.Global); ok {
					_ = g
					reasons["global"] = true
				} else {
					reasons["heap-input"] = true
				}
				return
			}
			if v.Op == token.ARROW {
				reasons["heap-input"] = true
				return
			}
			walk(v.X, depth+1)
		case *ssa.Phi:
			if v.Block() != nil && isLoopHeader(v.Block()) {
				reasons["loop"] = true
			}
			for _, e := range v.Edges {
				walk(e, depth+1)
			}
		case *ssa.Call:
			c := v.Common()
			if b, ok := c.Value.(*ssa.Builtin); ok {
				switch b.Name() {
				case "len", "cap", "min", "max", "append", "copy":
					for _, a := range c.Args {
						walk(a, depth+1)
					}
					return
				}
			}
			if c.IsInvoke() || c.StaticCallee() == nil {
				reasons["unknown-callee"] = true
			} else {
				reasons["call-result"] = true
			}
		case *ssa.Extract:
			walk(v.Tuple, depth+1)
		case *ssa.Lookup, *ssa.Index, *ssa.Field:
			reasons["heap-input"] = true
		case *ssa.TypeAssert:
			walk(v.X, depth+1)
		case ssa.Instruction:
			for _, op := range v.Operands(nil) {
				walk(*op, depth+1)
			}
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	for _, r := range []string{"unknown-callee", "call-result", "heap-input", "global", "loop", "budget"} {
		if reasons[r] {
			return r
		}
	}
	return ""
}

func isLoopHeader(b *ssa.BasicBlock) bool {
	for _, p := range b.Preds {
		if b.Dominates(p) {
			return true
		}
	}
	return false
}

// recovers reports whether fn defers a closure (or function) that calls
// recover() directly and contains no panic.
func recovers(fn *ssa.Function) bool {
	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			d, ok := in.(*ssa.Defer)
			if !ok {
				continue
			}
			var callee *ssa.Function
			switch v := d.Call.Value.(type) {
			case *ssa.Function:
				callee = v
			case *ssa.MakeClosure:
				callee, _ = v.Fn.(*ssa.Function)
			}
			if callee == nil {
				continue
			}
			calls, panics := false, false
			for _, cb := range callee.Blocks {
				for _, ci := range cb.Instrs {
					switch ci := ci.(type) {
					case *ssa.Call:
						if bi, ok := ci.Call.Value.(*ssa.Builtin); ok && bi.Name() == "recover" {
							calls = true
						}
					case *ssa.Panic:
						panics = true
					}
				}
			}
			if calls && !panics {
				return true
			}
		}
	}
	return false
}

// assumptions lists fn's holes for the policy: unknown callees, unsafe
// and reflect use.
func assumptions(fn *ssa.Function) []string {
	set := map[string]bool{}
	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			if c, ok := in.(ssa.CallInstruction); ok {
				cm := c.Common()
				switch {
				case cm.IsInvoke():
					set["unknown-callee:"+types.TypeString(cm.Value.Type(), nil)+"."+cm.Method.Name()] = true
				case cm.StaticCallee() == nil:
					if _, ok := cm.Value.(*ssa.Builtin); !ok {
						set["unknown-callee:func"] = true
					}
				default:
					if p := cm.StaticCallee().Pkg; p != nil && p.Pkg.Path() == "reflect" {
						set["reflect:"+cm.StaticCallee().Name()] = true
					}
				}
			}
			if v, ok := in.(ssa.Value); ok {
				if t, ok := v.Type().Underlying().(*types.Basic); ok && t.Kind() == types.UnsafePointer {
					set["unsafe:"+fn.String()] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// retState is the state at a return; for a return block of phis only,
// one per incoming edge (pred >= 0), so a phi result is known per edge.
type retState struct {
	r    *ssa.Return
	s    *state
	pred int
}

func (e *engine) returnStates(r *ssa.Return, s *state) []retState {
	b := r.Block()
	onlyPhis := len(b.Preds) > 1
	for _, in := range b.Instrs[:len(b.Instrs)-1] {
		if _, ok := in.(*ssa.Phi); !ok {
			onlyPhis = false
		}
	}
	if !onlyPhis {
		return []retState{{r, s.clone(), -1}}
	}
	var out []retState
	for i, p := range b.Preds {
		if es := e.edge(p, b); es != nil {
			out = append(out, retState{r, es, i})
		}
	}
	return out
}

// evaluate re-runs every reachable block from its fixpoint state, calling
// check before each instruction.
func (e *engine) evaluate(check func(ssa.Instruction, *state)) {
	e.final = true
	for _, b := range e.fn.Blocks {
		in := e.in[b]
		if in == nil {
			continue
		}
		e.transfer(b, in.clone(), check)
	}
}

// inliner analyzes small same-package callees in the caller's context,
// for the guard mismatch rule.
type inliner struct {
	limit  int
	caller *ssa.Function
	found  []*ObResult
	seen   map[string]bool
}

func (in *inliner) call(e *engine, s *state, call *ssa.Call, f *ssa.Function) {
	if !e.final || e.depth >= 2 || f.Pkg == nil || f.Pkg != in.caller.Pkg || f == in.caller {
		return
	}
	if !smallLoopFree(f, in.limit) || f.Recover != nil || len(f.FreeVars) > 0 {
		return
	}
	args := call.Call.Args
	if len(args) != len(f.Params) {
		return
	}
	init := s.clone()
	for i, p := range f.Params {
		copyValue(init, args[i], p)
		if w, ok := s.tested[args[i]]; ok {
			init.tested[p] = w
		}
		for _, k := range []nodeKind{nVal, nLen, nCap} {
			if w, ok := s.guards[node{k, args[i]}]; ok {
				init.guards[node{k, p}] = w
			}
		}
	}
	sub := newEngine(f, &inliner{limit: in.limit, caller: in.caller, seen: in.seen}, e.depth+1)
	sub.sums = e.sums
	sub.inline.found = nil
	sub.run(init)
	obs := obligations(f)
	byInstr := map[ssa.Instruction][]*Obligation{}
	for _, o := range obs {
		byInstr[o.Instr] = append(byInstr[o.Instr], o)
	}
	if in.seen == nil {
		in.seen = map[string]bool{}
	}
	sub.evaluate(func(ins ssa.Instruction, st *state) {
		for _, o := range byInstr[ins] {
			v := checkObligation(o, st)
			if v.fails && o.Kind != KPanic && !v.proved {
				// The arguments of this call make the callee panic on every
				// input (rule 3, the call violates the callee's
				// precondition whatever the caller is given).
				key := o.Key + "@always@" + fmt.Sprint(call.Pos())
				if !in.seen[key] {
					in.seen[key] = true
					in.found = append(in.found, &ObResult{Ob: o, Via: o, Status: Counterexample, Class: "manifest", Rule: "call-always",
						Labels: []Label{{Pos: call.Pos(), Text: "this call always makes " + f.Name() + " panic"}}})
				}
				continue
			}
			if v.mismatch == nil || !inCaller(in.caller, v.mismatch.guard) {
				continue
			}
			// Name the quantity in the caller: map callee parameters
			// back to the caller's arguments.
			m := *v.mismatch
			m.q = mapBack(m.q, f, args)
			if m.other.v != nil {
				m.other = mapBack(m.other, f, args)
			}
			r := describeMismatch(in.caller, o, &m, call)
			if r == nil {
				continue
			}
			key := o.Key + "@" + fmt.Sprint(call.Pos())
			if in.seen[key] {
				continue
			}
			in.seen[key] = true
			r.Via = o
			in.found = append(in.found, r)
		}
	})
	in.found = append(in.found, sub.inline.found...)
}

// mapBack renames a callee parameter node to the caller's argument.
func mapBack(n node, f *ssa.Function, args []ssa.Value) node {
	for i, p := range f.Params {
		if n.v == p {
			return node{n.k, args[i]}
		}
	}
	return n
}

func inCaller(fn *ssa.Function, pos token.Pos) bool {
	if pos == token.NoPos || fn.Syntax() == nil {
		return false
	}
	return fn.Syntax().Pos() <= pos && pos < fn.Syntax().End()
}

func smallLoopFree(f *ssa.Function, limit int) bool {
	n := 0
	for _, b := range f.Blocks {
		n += len(b.Instrs)
		if isLoopHeader(b) {
			return false
		}
	}
	return n <= limit
}

// spanIndex maps instruction anchor positions to the source expression
// they belong to, for spans.
type spanIndex struct {
	at map[token.Pos]ast.Node
}

func newSpanIndex(files []*ast.File) *spanIndex {
	x := &spanIndex{at: map[token.Pos]ast.Node{}}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.IndexExpr:
				x.put(n.Lbrack, n)
			case *ast.IndexListExpr:
				x.put(n.Lbrack, n)
			case *ast.SliceExpr:
				x.put(n.Lbrack, n)
			case *ast.BinaryExpr:
				x.put(n.OpPos, n)
			case *ast.StarExpr:
				x.put(n.Star, n)
			case *ast.UnaryExpr:
				x.put(n.OpPos, n)
			case *ast.SelectorExpr:
				x.put(n.Sel.Pos(), n)
			case *ast.CallExpr:
				x.put(n.Lparen, n)
			case *ast.TypeAssertExpr:
				x.put(n.Lparen, n)
			case *ast.AssignStmt:
				x.put(n.TokPos, n)
			case *ast.IncDecStmt:
				x.put(n.TokPos, n)
			}
			return true
		})
	}
	return x
}

func (x *spanIndex) put(p token.Pos, n ast.Node) {
	if _, ok := x.at[p]; !ok {
		x.at[p] = n
	}
}

// Span is the source range of an obligation's expression.
func (x *spanIndex) span(pos token.Pos) (token.Pos, token.Pos) {
	if n, ok := x.at[pos]; ok {
		return n.Pos(), n.End()
	}
	return pos, pos
}

// Message is the diagnostic text for an open obligation.
func Message(fn *ssa.Function, o *Obligation) string {
	name := fn.Name()
	if fn.Parent() != nil {
		name = fn.Parent().Name() + " (closure)"
	}
	return fmt.Sprintf("`%s` can panic: %s", name, panicText(o))
}

func panicText(o *Obligation) string {
	switch o.Kind {
	case KIndex:
		return "index out of range"
	case KSlice:
		return "slice bounds out of range"
	case KNil:
		return "nil pointer dereference"
	case KIface:
		return "method call on a nil interface"
	case KDivide:
		return "integer divide by zero"
	case KAssert:
		return "interface conversion"
	case KCompare:
		return "comparing or hashing an uncomparable type"
	case KMap:
		return "assignment to entry in nil map"
	case KConvert:
		return "conversion of a short slice to an array"
	case KShift:
		return "negative shift amount"
	case KMake:
		return "make with a negative or too large size"
	case KAppend:
		return "append length overflow"
	case KUnsafe:
		return "unsafe.Slice or unsafe.String with a bad length"
	case KChan:
		return "close of a nil channel"
	case KPanic:
		return "explicit panic"
	}
	return string(o.Kind)
}

// Counts sums statuses over a result.
func (r *Result) Counts() map[string]int {
	c := map[string]int{}
	for _, f := range r.Funcs {
		c["functions"]++
		for _, o := range f.Obligations {
			if o.Via != nil {
				c["manifest-inlined"]++
				continue
			}
			c["obligations"]++
			c[string(o.Status)]++
			if o.Status == Unknown {
				c["unknown:"+o.Reason]++
			}
			c["kind:"+string(o.Ob.Kind)]++
			if o.Status == Proved || o.Status == ProvedAssuming {
				c["proved:"+string(o.Ob.Kind)]++
			}
			if o.Dead {
				c["dead"]++
			}
		}
	}
	return c
}

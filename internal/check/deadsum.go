package check

// Dead code from per-package summaries. The rapid type analysis of
// golang.org/x/tools/go/callgraph/rta (Copyright 2013 The Go Authors,
// BSD-style license) needs the SSA form of the whole program at once,
// gigabytes on a large one. Here each package is built to SSA on its own,
// in the analysis engine, and reduced to what the analysis reads: the
// functions each function calls or takes the address of, the types it
// converts to interfaces (with every type reflection can reach from them),
// and the interface methods it calls. The SSA form is dropped at once, the
// summary is cached with the package, and the reachability runs over the
// summaries.
//
// The rules follow rta: a function whose address is taken is reachable
// (reflect.Value.Call can call it); every exported method of a type that
// reaches an interface, or that reflection can reach from such a type, is
// reachable; an interface method call reaches that method of every such
// type that implements the interface. Generic code is summarized once, not
// per instance: a method call on a type parameter reaches the methods of
// that name of every such type, and the type arguments of an
// instantiation count as converted to interfaces.

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/types/typeutil"
)

// deadSummary is what the reachability needs from one package.
type deadSummary struct {
	Funcs  map[string]*fnSummary    // by function key
	Types  map[string]*typeSummary  // runtime types named by Funcs, by type key
	Ifaces map[string][]ifaceMethod // interfaces named by invokes, by interface key
	Decls  []deadDecl               // the package's function declarations (workspace packages)
}

type fnSummary struct {
	Calls   []string // static callees and functions whose address is taken
	Types   []string // types converted to interfaces
	Invokes []invoke // interface method calls
	ByName  []string // method calls on type parameters, by method name
}

type invoke struct {
	Iface string // interface key
	ID    string // method ID (types.Func.Id)
}

type ifaceMethod struct {
	ID, Sig string
}

type typeSummary struct {
	Methods []typeMethod // the method set
	Elems   []string     // the types reflection reaches from this one
}

type typeMethod struct {
	ID, Name, Sig string
	Exported      bool
	Key           string  // the declared method, or ""
	Inv           *invoke // a method promoted from an embedded interface
}

type deadDecl struct {
	Key       string
	Name      string // as cmd/deadcode prints it: Recv.Name or Name
	Span      diag.Span
	Generated bool
	Marker    bool
}

// summarize builds the package to SSA and reduces it to its summary.
// Decls are listed only when withDecls is set.
func summarize(fset *token.FileSet, files []*ast.File, pkg *types.Package, info *types.Info, withDecls bool) *deadSummary {
	prog := ssa.NewProgram(fset, 0)
	seen := map[*types.Package]bool{}
	var create func(p *types.Package)
	create = func(p *types.Package) {
		for _, imp := range p.Imports() {
			if !seen[imp] {
				seen[imp] = true
				prog.CreatePackage(imp, nil, nil, true)
				create(imp)
			}
		}
	}
	create(pkg)
	sp := prog.CreatePackage(pkg, files, info, false)
	sp.Build()

	b := &sumBuilder{
		s:     &deadSummary{Funcs: map[string]*fnSummary{}, Types: map[string]*typeSummary{}, Ifaces: map[string][]ifaceMethod{}},
		msets: &typeutil.MethodSetCache{},
	}
	b.typeKeys.SetHasher(typeutil.MakeHasher())
	var fns []*ssa.Function
	for _, m := range sp.Members {
		switch m := m.(type) {
		case *ssa.Function:
			fns = append(fns, m)
		case *ssa.Type:
			if _, ok := m.Type().(*types.Named); !ok {
				continue
			}
			named := m.Type().(*types.Named)
			for method := range named.Methods() {
				if fn := prog.FuncValue(method); fn != nil {
					fns = append(fns, fn)
				}
			}
		}
	}
	for _, fn := range fns {
		k := funcKey(fn)
		if k == "" {
			continue
		}
		fs := &fnSummary{}
		b.visit(fs, fn)
		b.s.Funcs[k] = fs
	}
	if withDecls {
		b.decls(fset, files, pkg, info)
	}
	return b.s
}

type sumBuilder struct {
	s        *deadSummary
	msets    *typeutil.MethodSetCache
	typeKeys typeutil.Map // types.Type -> key, for the types already summarized
}

func (b *sumBuilder) visit(fs *fnSummary, fn *ssa.Function) {
	calls := map[string]bool{}
	tys := map[string]bool{}
	invs := map[invoke]bool{}
	names := map[string]bool{}
	var walk func(fn *ssa.Function)
	walk = func(fn *ssa.Function) {
		var space [32]*ssa.Value
		for _, bl := range fn.Blocks {
			for _, instr := range bl.Instrs {
				rands := instr.Operands(space[:0])
				switch instr := instr.(type) {
				case ssa.CallInstruction:
					c := instr.Common()
					if c.IsInvoke() {
						b.invoke(c.Value.Type(), c.Method, invs, names)
					} else if g := c.StaticCallee(); g != nil {
						b.callee(g, calls, tys, invs, names)
					}
					rands = rands[1:]
				case *ssa.MakeInterface:
					b.runtimeType(instr.X.Type(), tys)
				}
				for _, op := range rands {
					if g, ok := (*op).(*ssa.Function); ok {
						b.callee(g, calls, tys, invs, names)
					}
				}
			}
		}
		for _, a := range fn.AnonFuncs {
			walk(a)
		}
	}
	walk(fn)
	fs.Calls = sortedKeys(calls)
	fs.Types = sortedKeys(tys)
	fs.ByName = sortedKeys(names)
	for v := range invs {
		fs.Invokes = append(fs.Invokes, v)
	}
	sort.Slice(fs.Invokes, func(i, j int) bool {
		a, c := fs.Invokes[i], fs.Invokes[j]
		return a.Iface < c.Iface || a.Iface == c.Iface && a.ID < c.ID
	})
}

// callee records a call of g or a reference to it.
func (b *sumBuilder) callee(g *ssa.Function, calls, tys map[string]bool, invs map[invoke]bool, names map[string]bool) {
	for _, t := range g.TypeArgs() {
		b.runtimeType(t, tys)
	}
	if g.Parent() != nil {
		return // a closure of this function: walked with it
	}
	if obj, ok := g.Object().(*types.Func); ok && g.Synthetic != "" {
		// A wrapper (bound method, thunk, promotion): it calls the method.
		if recv := obj.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
			b.invoke(recv.Type(), obj, invs, names)
			return
		}
	}
	if k := funcKey(g); k != "" {
		calls[k] = true
	}
}

// invoke records a call of method m on a value of type T (an interface or
// a type parameter).
func (b *sumBuilder) invoke(T types.Type, m *types.Func, invs map[invoke]bool, names map[string]bool) {
	if _, ok := types.Unalias(T).(*types.TypeParam); ok || hasTypeParam(T) {
		names[m.Name()] = true
		return
	}
	I, ok := T.Underlying().(*types.Interface)
	if !ok {
		return
	}
	invs[invoke{Iface: b.iface(I), ID: m.Id()}] = true
}

func (b *sumBuilder) iface(I *types.Interface) string {
	var ms []ifaceMethod
	for m := range I.Methods() {
		if m.Signature().TypeParams() != nil {
			continue
		}
		ms = append(ms, ifaceMethod{ID: m.Id(), Sig: sigKey(m.Signature())})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
	h := sha256.New()
	for _, m := range ms {
		h.Write([]byte(m.ID + "\x00" + m.Sig + "\x00"))
	}
	k := hex.EncodeToString(h.Sum(nil)[:12])
	if _, ok := b.s.Ifaces[k]; !ok {
		b.s.Ifaces[k] = ms
	}
	return k
}

// runtimeType records T as converted to an interface, and summarizes it
// and every type reflection can reach from it, as
// typesinternal.ForEachElement walks them for rta.
func (b *sumBuilder) runtimeType(T types.Type, tys map[string]bool) {
	if hasTypeParam(T) {
		return
	}
	tys[b.summarizeType(T)] = true
}

func (b *sumBuilder) summarizeType(T types.Type) string {
	if k, ok := b.typeKeys.At(T).(string); ok {
		return k
	}
	k := typeKey(T)
	b.typeKeys.Set(T, k)
	ts := &typeSummary{}
	if _, ok := b.s.Types[k]; !ok {
		b.s.Types[k] = ts
	}
	elem := func(T types.Type) {
		ts.Elems = append(ts.Elems, b.summarizeType(T))
	}
	for sel := range b.msets.MethodSet(T).Methods() {
		obj := sel.Obj().(*types.Func)
		sig := obj.Signature()
		if sig.TypeParams() != nil {
			continue
		}
		elem(sig.Params())
		elem(sig.Results())
		if types.IsInterface(T) {
			continue
		}
		tm := typeMethod{ID: obj.Id(), Name: obj.Name(), Sig: sigKey(sig), Exported: obj.Exported()}
		if recv := sig.Recv(); recv != nil && types.IsInterface(recv.Type()) {
			I := recv.Type().Underlying().(*types.Interface)
			tm.Inv = &invoke{Iface: b.iface(I), ID: obj.Id()}
		} else {
			tm.Key = objKey(obj)
		}
		ts.Methods = append(ts.Methods, tm)
	}
	switch T := T.(type) {
	case *types.Alias:
		elem(types.Unalias(T))
	case *types.Pointer:
		elem(T.Elem())
	case *types.Slice:
		elem(T.Elem())
	case *types.Chan:
		elem(T.Elem())
	case *types.Map:
		elem(T.Key())
		elem(T.Elem())
	case *types.Signature:
		elem(T.Params())
		elem(T.Results())
	case *types.Named:
		elem(types.NewPointer(T))
		elem(T.Underlying())
	case *types.Array:
		elem(T.Elem())
	case *types.Struct:
		for field := range T.Fields() {
			elem(field.Type())
		}
	case *types.Tuple:
		for v := range T.Variables() {
			elem(v.Type())
		}
	}
	return k
}

// decls lists the function declarations of the package with what the
// report needs.
func (b *sumBuilder) decls(fset *token.FileSet, files []*ast.File, pkg *types.Package, info *types.Info) {
	var ifaces []*types.Interface
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		if tn, ok := scope.Lookup(name).(*types.TypeName); ok && types.IsInterface(tn.Type()) {
			ifaces = append(ifaces, tn.Type().Underlying().(*types.Interface))
		}
	}
	for _, f := range files {
		gen := ast.IsGenerated(f)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			obj, ok := info.Defs[fd.Name].(*types.Func)
			if !ok || fd.Recv == nil && fd.Name.Name == "init" {
				continue
			}
			name := fd.Name.Name
			if recv := obj.Signature().Recv(); recv != nil {
				t := recv.Type()
				if p, ok := t.(*types.Pointer); ok {
					t = p.Elem()
				}
				if n, ok := types.Unalias(t).(*types.Named); ok {
					name = n.Obj().Name() + "." + name
				}
			}
			pos := fset.Position(fd.Name.Pos())
			b.s.Decls = append(b.s.Decls, deadDecl{
				Key:       objKey(obj),
				Name:      name,
				Span:      diag.Span{File: pos.Filename, Line: pos.Line, Col: pos.Column, EndLine: pos.Line, EndCol: pos.Column},
				Generated: gen,
				Marker:    isMarker(fd, obj, ifaces),
			})
		}
	}
}

// isMarker reports an empty unexported method without parameters or
// results that makes its type implement an interface of the package.
func isMarker(fd *ast.FuncDecl, obj *types.Func, ifaces []*types.Interface) bool {
	sig := obj.Signature()
	if sig.Recv() == nil || ast.IsExported(obj.Name()) || sig.Params().Len() > 0 || sig.Results().Len() > 0 {
		return false
	}
	if fd.Body == nil || len(fd.Body.List) > 0 {
		return false
	}
	for _, i := range ifaces {
		if types.Implements(sig.Recv().Type(), i) {
			return true
		}
	}
	return false
}

// funcKey names a function across packages: "path.Name",
// "path.Recv.Name", or for the package initializers "path.init" and
// "path.init#n". Closures are named by their enclosing function, generic
// instances by their generic function.
func funcKey(g *ssa.Function) string {
	for g.Parent() != nil {
		g = g.Parent()
	}
	if o := g.Origin(); o != nil {
		g = o
	}
	if obj, ok := g.Object().(*types.Func); ok && !strings.HasPrefix(g.Name(), "init#") {
		return objKey(obj)
	}
	if g.Pkg != nil {
		return g.Pkg.Pkg.Path() + "." + g.Name()
	}
	return ""
}

func objKey(obj *types.Func) string {
	obj = obj.Origin()
	if obj.Pkg() == nil {
		return ""
	}
	if recv := obj.Signature().Recv(); recv != nil {
		t := recv.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		n, ok := types.Unalias(t).(*types.Named)
		if !ok || types.IsInterface(n) {
			return ""
		}
		return obj.Pkg().Path() + "." + n.Origin().Obj().Name() + "." + obj.Name()
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

func qualifyPath(p *types.Package) string { return p.Path() }

func typeKey(T types.Type) string { return types.TypeString(T, qualifyPath) }

// sigKey writes a method signature without its receiver and parameter
// names, as types.Identical compares them.
func sigKey(sig *types.Signature) string {
	var b strings.Builder
	tuple := func(t *types.Tuple, variadic bool) {
		b.WriteByte('(')
		for i := range t.Len() {
			if i > 0 {
				b.WriteByte(',')
			}
			T := t.At(i).Type()
			if variadic && i == t.Len()-1 {
				b.WriteString("...")
				T = T.(*types.Slice).Elem()
			}
			b.WriteString(typeKey(T))
		}
		b.WriteByte(')')
	}
	tuple(sig.Params(), sig.Variadic())
	tuple(sig.Results(), false)
	return b.String()
}

// hasTypeParam reports whether T mentions a type parameter.
func hasTypeParam(T types.Type) bool {
	seen := map[types.Type]bool{}
	var has func(T types.Type) bool
	has = func(T types.Type) bool {
		if T == nil || seen[T] {
			return false
		}
		seen[T] = true
		switch T := T.(type) {
		case *types.TypeParam:
			return true
		case *types.Alias:
			return has(types.Unalias(T))
		case *types.Named:
			for t := range T.TypeArgs().Types() {
				if has(t) {
					return true
				}
			}
			return false
		case *types.Pointer:
			return has(T.Elem())
		case *types.Slice:
			return has(T.Elem())
		case *types.Array:
			return has(T.Elem())
		case *types.Chan:
			return has(T.Elem())
		case *types.Map:
			return has(T.Key()) || has(T.Elem())
		case *types.Signature:
			return T.TypeParams() != nil || has(T.Params()) || has(T.Results())
		case *types.Tuple:
			for v := range T.Variables() {
				if has(v.Type()) {
					return true
				}
			}
		case *types.Struct:
			for field := range T.Fields() {
				if has(field.Type()) {
					return true
				}
			}
		case *types.Interface:
			for method := range T.Methods() {
				if has(method.Type()) {
					return true
				}
			}
			for etyp := range T.EmbeddedTypes() {
				if has(etyp) {
					return true
				}
			}
		case *types.Union:
			return true
		}
		return false
	}
	return has(T)
}

// reach runs the analysis over the summaries from the roots and returns
// the reachable function keys.
func reach(sums []*deadSummary, roots []string) map[string]bool {
	funcs := map[string][]*fnSummary{}
	types_ := map[string]*typeSummary{}
	ifaces := map[string][]ifaceMethod{}
	for _, s := range sums {
		if s == nil {
			continue
		}
		for k, f := range s.Funcs {
			funcs[k] = append(funcs[k], f)
		}
		for k, t := range s.Types {
			// A test variant declares more methods of its types than the
			// package does: merge the method sets.
			have := types_[k]
			if have == nil {
				types_[k] = t
				continue
			}
			if len(t.Methods) <= len(have.Methods) {
				continue
			}
			ids := map[string]bool{}
			for _, m := range have.Methods {
				ids[m.ID] = true
			}
			merged := &typeSummary{Methods: append([]typeMethod(nil), have.Methods...), Elems: append(append([]string(nil), have.Elems...), t.Elems...)}
			for _, m := range t.Methods {
				if !ids[m.ID] {
					merged.Methods = append(merged.Methods, m)
				}
			}
			types_[k] = merged
		}
		maps.Copy(ifaces, s.Ifaces)
	}
	type rtype struct {
		key  string
		byID map[string]*typeMethod
	}
	reached := map[string]bool{}
	var work []string
	addFunc := func(k string) {
		if k != "" && !reached[k] {
			reached[k] = true
			work = append(work, k)
		}
	}
	rtypes := map[string]*rtype{}
	typesByID := map[string][]*rtype{}      // method ID -> runtime types with it
	typesByName := map[string][]*rtype{}    // method name -> runtime types with it
	invoked := map[string]map[string]bool{} // interface key -> method IDs called
	ifacesByID := map[string][]string{}     // method ID -> invoked interfaces with it
	names := map[string]bool{}
	implCache := map[[2]string]bool{}
	implements := func(t *rtype, ik string) bool {
		ck := [2]string{t.key, ik}
		if v, ok := implCache[ck]; ok {
			return v
		}
		ok := true
		for _, m := range ifaces[ik] {
			tm := t.byID[m.ID]
			if tm == nil || tm.Sig != m.Sig {
				ok = false
				break
			}
		}
		implCache[ck] = ok
		return ok
	}
	var addInvoke func(inv invoke)
	callMethod := func(t *rtype, id string) {
		if tm := t.byID[id]; tm != nil {
			addFunc(tm.Key)
			if tm.Inv != nil {
				addInvoke(*tm.Inv)
			}
		}
	}
	addInvoke = func(inv invoke) {
		ids := invoked[inv.Iface]
		if ids == nil {
			ids = map[string]bool{}
			invoked[inv.Iface] = ids
			for _, m := range ifaces[inv.Iface] {
				ifacesByID[m.ID] = append(ifacesByID[m.ID], inv.Iface)
			}
		}
		if ids[inv.ID] {
			return
		}
		ids[inv.ID] = true
		for _, t := range typesByID[inv.ID] {
			if implements(t, inv.Iface) {
				callMethod(t, inv.ID)
			}
		}
	}
	addName := func(n string) {
		if names[n] {
			return
		}
		names[n] = true
		for _, t := range typesByName[n] {
			for _, tm := range t.byID {
				if tm.Name == n {
					callMethod(t, tm.ID)
				}
			}
		}
	}
	var addType func(k string)
	addType = func(k string) {
		if rtypes[k] != nil {
			return
		}
		ts := types_[k]
		t := &rtype{key: k, byID: map[string]*typeMethod{}}
		rtypes[k] = t
		if ts == nil {
			return
		}
		for _, e := range ts.Elems {
			addType(e)
		}
		for i := range ts.Methods {
			tm := &ts.Methods[i]
			t.byID[tm.ID] = tm
			typesByID[tm.ID] = append(typesByID[tm.ID], t)
			typesByName[tm.Name] = append(typesByName[tm.Name], t)
		}
		for _, tm := range t.byID {
			if tm.Exported {
				callMethod(t, tm.ID)
			}
			for _, ik := range ifacesByID[tm.ID] {
				if invoked[ik][tm.ID] && implements(t, ik) {
					callMethod(t, tm.ID)
				}
			}
			if names[tm.Name] {
				callMethod(t, tm.ID)
			}
		}
	}
	for _, r := range roots {
		addFunc(r)
	}
	for len(work) > 0 {
		k := work[len(work)-1]
		work = work[:len(work)-1]
		for _, f := range funcs[k] {
			for _, c := range f.Calls {
				addFunc(c)
			}
			for _, t := range f.Types {
				addType(t)
			}
			for _, inv := range f.Invokes {
				addInvoke(inv)
			}
			for _, n := range f.ByName {
				addName(n)
			}
		}
	}
	return reached
}

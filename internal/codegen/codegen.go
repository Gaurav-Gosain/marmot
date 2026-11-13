// Package codegen translates x/tools SSA to MLIR text, one module per Go
// package. Phase A: only the upstream llvm dialect is emitted, and Go
// semantics that are not plain instructions become calls into package
// runtime (lib/src/runtime).
package codegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/loader"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Options configure code generation.
type Options struct {
	GOOS, GOARCH string
	Sizes        types.Sizes
	// GoDialect emits the go dialect instead of the
	// equivalent llvm dialect sequences (Phase B, -dialect=go).
	GoDialect bool
	// Release enables release-only transforms (more devirtualization).
	Release bool
	// MergedGOROOT is where the loader's GOROOT symlinks live; source
	// locations resolve them.
	MergedGOROOT string
	// XDefs sets string variables as the linker's -X does: "pkgpath.name"
	// to value (go test sets testing.testBinary to "1").
	XDefs map[string]string
	// Facts of packages loaded from export data (no syntax), by package
	// path: their linkname directives, dynamic imports and recover callers.
	Facts map[string]*loader.Facts
	// Separate generates each package's module from its own source and the
	// types it imports only: no whole-program method pruning, dead globals
	// or devirtualization (incremental builds cache modules by source).
	Separate bool
	// In Separate mode, what whole-program analysis would find from reflect
	// uses (see loader.Facts): every exported method kept, call adapters,
	// every access possibly aliased.
	KeepExported, ReflectCall, PunAll bool
	// AllocReport, when not nil, receives one JSON line per allocation
	// decision (allocreport.go). Writes are serialized by the Program.
	AllocReport io.Writer
}

// Module is the MLIR text for one Go package (or the synthetic entry module).
type Module struct {
	Name string // import path, or "marmot:main" for the entry module
	Text string
	// Path is set instead of Text when the driver reuses the cached MLIR
	// file of an unchanged package (codegen skipped).
	Path string
}

// Error is a compile error at a source position.
type Error struct {
	Pos token.Position
	Msg string
}

func (e *Error) Error() string {
	if e.Pos.IsValid() {
		return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
	}
	return e.Msg
}

// Program holds whole-program state shared by all module generators.
// After NewProgram it is read only, so modules can be generated in parallel.
type Program struct {
	opts    Options
	allocMu sync.Mutex // serializes writes to opts.AllocReport
	prog    *ssa.Program
	fset    *token.FileSet
	runtime *ssa.Package
	pkgs    []*ssa.Package
	pkgOf   map[*types.Package]*ssa.Package // the packages of pkgs
	files   map[*types.Package][]*ast.File
	infos   map[*types.Package]*types.Info

	owner map[*ssa.Function]*ssa.Package // functions emitted as strong definitions
	byPkg map[*ssa.Package][]*ssa.Function
	// Packages loaded from export data, and their functions declared
	// without a body (bodyless).
	exportPkg    map[*ssa.Package]bool
	bodylessDecl map[*ssa.Function]bool
	linkname     map[*ssa.Function]string // symbol overrides from //go:linkname
	dynamic      map[string]string        // pkgpath.local -> C symbol from //go:cgo_import_dynamic
	pragmas      map[*ssa.Function]pragma

	mu   sync.Mutex
	syms sync.Map // *ssa.Function -> symbol (symbol)

	escMu     sync.Mutex
	recWrap   map[*ssa.Function]bool
	locFiles  sync.Map // file name -> resolved name (locFile)
	esc       escapeState
	regions   regionState
	deep      deepState
	panicMemo map[panicKey]int
	byValue   map[*ssa.Function]map[int]bool

	usedMethods     map[string]bool // method keys called through interfaces
	usedMethodsHash string
	keepExported    bool // reflect method lookups: keep all exported methods
	reflectCall     bool // reflect.Value.Call reachable: emit call adapters

	ifaceTypes map[string]bool    // canonical names of types that can be in interfaces
	fmaBlocked map[token.Pos]bool // float multiplications under an explicit conversion

	ifaceTypeList []types.Type            // the types of ifaceTypes
	punned        map[string]bool         // types reached by unsafe.Pointer -> *T
	punAll        bool                    // reflect.NewAt reachable: every access is unsafe
	localPun      map[ssa.Value]bool      // conversions only dereferenced in place
	implCache     map[string][]types.Type // interface -> closed-world implementers
	mainPath      string                  // import path of the main package
	globalRead    map[*ssa.Global]bool    // globals read anywhere in the program
	varLinkname   map[string]bool         // pkgpath.name of variables named in //go:linkname
	globalLink    map[*ssa.Global]string  // //go:linkname local target for variables
	dylibs        map[string]bool         // libraries named by //go:cgo_import_dynamic
}

type pragma uint32

const (
	pragmaNoinline pragma = 1 << iota
	pragmaNosplit
	pragmaNoescape
	pragmaNoreturn
)

// NewProgram prepares code generation for all packages of prog.
func NewProgram(prog *ssa.Program, pkgs []*ssa.Package, files map[*types.Package][]*ast.File, opts Options) (*Program, error) {
	mainPath := ""
	for _, pkg := range pkgs {
		if pkg.Pkg.Name() == "main" {
			mainPath = pkg.Pkg.Path()
		}
	}
	p := &Program{
		files:        files,
		mainPath:     mainPath,
		opts:         opts,
		prog:         prog,
		fset:         prog.Fset,
		owner:        map[*ssa.Function]*ssa.Package{},
		byPkg:        map[*ssa.Package][]*ssa.Function{},
		exportPkg:    map[*ssa.Package]bool{},
		bodylessDecl: map[*ssa.Function]bool{},
		linkname:     map[*ssa.Function]string{},
		dynamic:      map[string]string{},
		varLinkname:  map[string]bool{},
		globalLink:   map[*ssa.Global]string{},
		dylibs:       map[string]bool{},
		pragmas:      map[*ssa.Function]pragma{},
	}
	for _, pkg := range pkgs {
		if pkg.Pkg.Path() == "runtime" {
			p.runtime = pkg
		}
		p.pkgs = append(p.pkgs, pkg)
	}
	if p.runtime == nil {
		return nil, fmt.Errorf("package runtime is not loaded")
	}
	p.pkgOf = map[*types.Package]*ssa.Package{}
	for _, pkg := range p.pkgs {
		p.pkgOf[pkg.Pkg] = pkg
		p.collect(pkg)
	}
	if !p.opts.Separate {
		p.computeUsedMethods()
	} else {
		p.keepExported = p.opts.KeepExported
		p.reflectCall = p.opts.ReflectCall
		p.punAll = true // no whole-program pun analysis: every access may alias
	}
	p.collectFMABlocks()
	return p, nil
}

// collect finds the functions defined by pkg and its pragmas.
func (p *Program) collect(pkg *ssa.Package) {
	var add func(fn *ssa.Function)
	add = func(fn *ssa.Function) {
		if fn == nil || p.owner[fn] != nil {
			return
		}
		if fn.TypeParams().Len() > 0 && len(fn.TypeArgs()) == 0 {
			return // generic: only instances are compiled
		}
		p.owner[fn] = pkg
		p.byPkg[pkg] = append(p.byPkg[pkg], fn)
		for _, a := range fn.AnonFuncs {
			add(a)
		}
	}
	names := make([]string, 0, len(pkg.Members))
	for n := range pkg.Members {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		switch m := pkg.Members[n].(type) {
		case *ssa.Function:
			add(m)
		case *ssa.Type:
			named, ok := m.Type().(*types.Named)
			if !ok || named.TypeParams().Len() > 0 {
				continue
			}
			for i := 0; i < named.NumMethods(); i++ {
				add(p.prog.FuncValue(named.Method(i)))
			}
		}
	}
	// Pragmas and linknames from the syntax.
	for _, fn := range p.byPkg[pkg] {
		decl, ok := fn.Syntax().(*ast.FuncDecl)
		if !ok || decl.Doc == nil {
			continue
		}
		for _, c := range decl.Doc.List {
			p.parseDirective(fn, c.Text)
		}
	}
	if len(p.files[pkg.Pkg]) == 0 {
		if facts := p.opts.Facts[pkg.Pkg.Path()]; facts != nil {
			p.applyFacts(pkg, facts)
		}
	}
	// //go:linkname directives may appear anywhere in a file.
	for _, f := range p.files[pkg.Pkg] {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//go:cgo_import_dynamic ") {
					fs := strings.Fields(c.Text)
					if len(fs) >= 3 {
						p.dynamic[pkg.Pkg.Path()+"."+fs[1]] = fs[2]
					}
					if len(fs) >= 4 {
						p.dylibs[strings.Trim(fs[3], `"`)] = true
					}
					continue
				}
				if !strings.HasPrefix(c.Text, "//go:linkname ") {
					continue
				}
				fs := strings.Fields(c.Text)
				if len(fs) < 2 {
					continue
				}
				if len(fs) >= 3 {
					p.varLinkname[fs[2]] = true // a variable of another package, by symbol
				}
				if g, ok := pkg.Members[fs[1]].(*ssa.Global); ok {
					p.varLinkname[pkg.Pkg.Path()+"."+g.Name()] = true
					if len(fs) >= 3 {
						// Both declarations name one symbol (gc): the
						// variable takes the target's symbol.
						p.globalLink[g] = fs[2]
					}
					continue
				}
				fn, ok := pkg.Members[fs[1]].(*ssa.Function)
				if !ok {
					continue
				}
				if len(fs) >= 3 {
					p.linkname[fn] = fs[2]
				}
			}
		}
	}
}

func (p *Program) parseDirective(fn *ssa.Function, text string) {
	switch strings.Fields(text)[0] {
	case "//go:noinline":
		p.pragmas[fn] |= pragmaNoinline
	case "//go:nosplit":
		p.pragmas[fn] |= pragmaNosplit
	case "//go:noescape":
		p.pragmas[fn] |= pragmaNoescape
	}
}

// symbol returns the link name of fn.
func (p *Program) symbol(fn *ssa.Function) string {
	if s, ok := p.syms.Load(fn); ok {
		return s.(string)
	}
	s, _ := p.syms.LoadOrStore(fn, p.computeSymbol(fn))
	return s.(string)
}

func (p *Program) computeSymbol(fn *ssa.Function) string {
	if ln, ok := p.linkname[fn]; ok {
		return ln
	}
	if fn.Origin() != nil && len(fn.TypeArgs()) > 0 {
		var b strings.Builder
		b.WriteString(p.computeSymbol(fn.Origin()))
		b.WriteByte('[')
		for i, t := range fn.TypeArgs() {
			if i > 0 {
				b.WriteByte(',')
			}
			p.writeCanonical(&b, t)
		}
		b.WriteByte(']')
		return b.String()
	}
	if fn.Parent() != nil {
		// Anonymous function: name is parent$N.
		return p.computeSymbol(fn.Parent()) + strings.TrimPrefix(fn.Name(), fn.Parent().Name())
	}
	// A body-less libc_X_trampoline (darwin syscall packages) is the C
	// symbol its //go:cgo_import_dynamic directive names: every reference,
	// including function values, goes straight to it.
	if p.bodyless(fn) && fn.Pkg != nil && strings.HasSuffix(fn.Name(), "_trampoline") {
		if c, ok := p.dynamic[fn.Pkg.Pkg.Path()+"."+strings.TrimSuffix(fn.Name(), "_trampoline")]; ok {
			return c
		}
	}
	// Functions of packages loaded from export data are marked synthetic
	// ("from type information") but are the declared functions.
	declared := fn.Synthetic == "" || strings.HasPrefix(fn.Synthetic, "from type information")
	if declared && fn.Pkg == nil {
		if obj, ok := fn.Object().(*types.Func); ok && obj.Pkg() != nil {
			return p.declaredSymbol(fn, obj.Pkg())
		}
	}
	if fn.Pkg != nil && declared {
		return p.declaredSymbol(fn, fn.Pkg.Pkg)
	}
	// Synthetic functions (wrappers, thunks, instances): the SSA name with
	// full package paths is unique and stable across modules, except for
	// wrappers of unexported methods: (T).m promoted from two embedded
	// types of different packages has one SSA name for two methods, so
	// the method's package is added.
	name := fn.String()
	if obj, ok := fn.Object().(*types.Func); ok && obj != nil && !obj.Exported() && obj.Pkg() != nil {
		name += "@" + obj.Pkg().Path()
	}
	return name
}

// SetTypesInfo provides the type checker results (for syntax-level facts
// that SSA does not keep, such as explicit conversions). Call before
// Generate.
func (p *Program) SetTypesInfo(infos map[*types.Package]*types.Info) {
	p.infos = infos
	p.collectFMABlocks()
}

// Package returns the SSA packages to compile, in dependency order.
func (p *Program) Packages() []*ssa.Package { return p.pkgs }

// typeString formats t with full package paths.
func typeString(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Path() })
}

func (p *Program) errorf(pos token.Pos, format string, args ...any) *Error {
	return &Error{Pos: p.fset.Position(pos), Msg: fmt.Sprintf(format, args...)}
}

// mustRuntimeFunc returns the runtime function with the given name.
func (p *Program) runtimeFunc(name string) *ssa.Function {
	fn, _ := p.runtime.Members[name].(*ssa.Function)
	return fn
}

var _ = mlir.Ptr

// gDeferOffset returns the byte offset of the _defer field of runtime.g.
func (p *Program) gDeferOffset() (int64, bool) {
	obj := p.runtime.Pkg.Scope().Lookup("g")
	if obj == nil {
		return 0, false
	}
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok {
		return 0, false
	}
	offs := p.opts.Sizes.Offsetsof(structFields(st))
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == "_defer" {
			return offs[i], true
		}
	}
	return 0, false
}

// DynamicLibraries returns the libraries named by //go:cgo_import_dynamic
// directives (framework or dylib paths), sorted.
func (p *Program) DynamicLibraries() []string {
	var out []string
	for l := range p.dylibs {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// applyFacts records what collect reads from syntax, for a package loaded
// from export data.
func (p *Program) applyFacts(pkg *ssa.Package, f *loader.Facts) {
	for k, v := range f.Dynamic {
		p.dynamic[pkg.Pkg.Path()+"."+k] = v
	}
	for _, d := range f.Dylibs {
		p.dylibs[d] = true
	}
	for _, ln := range f.Linknames {
		if ln[1] != "" {
			p.varLinkname[ln[1]] = true
		}
		if g, ok := pkg.Members[ln[0]].(*ssa.Global); ok {
			p.varLinkname[pkg.Pkg.Path()+"."+g.Name()] = true
			if ln[1] != "" {
				p.globalLink[g] = ln[1]
			}
			continue
		}
		if fn, ok := pkg.Members[ln[0]].(*ssa.Function); ok && ln[1] != "" {
			p.linkname[fn] = ln[1]
		}
	}
	for _, name := range f.Recover {
		if fn := lookupFunc(p.prog, pkg, name); fn != nil {
			externalRecover.Store(fn, true)
		}
	}
	p.exportPkg[pkg] = true
	for _, name := range f.Bodyless {
		if fn := lookupFunc(p.prog, pkg, name); fn != nil {
			p.bodylessDecl[fn] = true
		}
	}
}

// bodyless reports whether fn is declared without a body (assembly, or
// provided by the C runtime core). Functions of packages loaded from export
// data have no SSA body either; the facts tell which of them were declared
// without one.
func (p *Program) bodyless(fn *ssa.Function) bool {
	if len(fn.Blocks) > 0 {
		return false
	}
	if fn.Pkg != nil && p.exportPkg[fn.Pkg] {
		return p.bodylessDecl[fn]
	}
	return true
}

// lookupFunc finds "F" or "T.M" in pkg (value or pointer receiver).
func lookupFunc(prog *ssa.Program, pkg *ssa.Package, name string) *ssa.Function {
	tn, mn, ok := strings.Cut(name, ".")
	if !ok {
		return pkg.Func(name)
	}
	t, ok := pkg.Members[tn].(*ssa.Type)
	if !ok {
		return nil
	}
	for _, rt := range []types.Type{t.Type(), types.NewPointer(t.Type())} {
		if sel := prog.MethodSets.MethodSet(rt).Lookup(pkg.Pkg, mn); sel != nil {
			return prog.MethodValue(sel)
		}
	}
	return nil
}

// externalRecover holds functions without bodies (loaded from export data)
// that call recover.
var externalRecover sync.Map

// declaredSymbol is the symbol of a declared function or method of pkg.
func (p *Program) declaredSymbol(fn *ssa.Function, pkg *types.Package) string {
	path := rtPath(pkg)
	if recv := fn.Signature.Recv(); recv != nil {
		rt := recv.Type()
		ptr := ""
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
			ptr = "*"
		}
		name := types.TypeString(rt, func(*types.Package) string { return "" })
		if ptr != "" {
			return fmt.Sprintf("%s.(*%s).%s", path, name, fn.Name())
		}
		return fmt.Sprintf("%s.%s.%s", path, name, fn.Name())
	}
	return path + "." + fn.Name()
}

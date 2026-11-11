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
	"sort"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Options configure code generation.
type Options struct {
	GOOS, GOARCH string
	Sizes        types.Sizes
	// GoDialect emits the go dialect (docs/dialect.md) instead of the
	// equivalent llvm dialect sequences (Phase B, -dialect=go).
	GoDialect bool
}

// Module is the MLIR text for one Go package (or the synthetic entry module).
type Module struct {
	Name string // import path, or "marmot:main" for the entry module
	Text string
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
	prog    *ssa.Program
	fset    *token.FileSet
	runtime *ssa.Package
	pkgs    []*ssa.Package
	files   map[*types.Package][]*ast.File

	owner    map[*ssa.Function]*ssa.Package // functions emitted as strong definitions
	byPkg    map[*ssa.Package][]*ssa.Function
	linkname map[*ssa.Function]string // symbol overrides from //go:linkname
	dynamic  map[string]string        // pkgpath.local -> C symbol from //go:cgo_import_dynamic
	pragmas  map[*ssa.Function]pragma

	mu   sync.Mutex
	syms map[*ssa.Function]string
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
	p := &Program{
		files:    files,
		opts:     opts,
		prog:     prog,
		fset:     prog.Fset,
		owner:    map[*ssa.Function]*ssa.Package{},
		byPkg:    map[*ssa.Package][]*ssa.Function{},
		linkname: map[*ssa.Function]string{},
		dynamic:  map[string]string{},
		pragmas:  map[*ssa.Function]pragma{},
		syms:     map[*ssa.Function]string{},
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
	for _, pkg := range p.pkgs {
		p.collect(pkg)
	}
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
	// //go:linkname directives may appear anywhere in a file.
	for _, f := range p.files[pkg.Pkg] {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//go:cgo_import_dynamic ") {
					fs := strings.Fields(c.Text)
					if len(fs) >= 3 {
						p.dynamic[pkg.Pkg.Path()+"."+fs[1]] = fs[2]
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
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.syms[fn]; ok {
		return s
	}
	s := p.computeSymbol(fn)
	p.syms[fn] = s
	return s
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
	if fn.Pkg != nil && fn.Synthetic == "" {
		path := fn.Pkg.Pkg.Path()
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
	// Synthetic functions (wrappers, thunks, instances): the SSA name with
	// full package paths is unique and stable across modules.
	return fn.String()
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

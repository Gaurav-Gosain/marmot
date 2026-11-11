package codegen

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// moduleGen builds the MLIR module for one package. Not safe for concurrent use.
type moduleGen struct {
	p   *Program
	pkg *ssa.Package // nil for the entry module
	tm  *typeMapper

	defs    strings.Builder // definitions: globals and functions
	decls   map[string]string
	defined map[string]bool
	queue   []*ssa.Function
	strs    map[string]string
	nstr    int
	errs    []error
}

func (p *Program) newModule(pkg *ssa.Package) *moduleGen {
	return &moduleGen{
		p:       p,
		pkg:     pkg,
		tm:      newTypeMapper(p.opts.Sizes),
		decls:   map[string]string{},
		defined: map[string]bool{},
		strs:    map[string]string{},
	}
}

// Generate returns the MLIR module for pkg.
func (p *Program) Generate(pkg *ssa.Package) (Module, error) {
	m := p.newModule(pkg)
	// Globals.
	names := make([]string, 0, len(pkg.Members))
	for n := range pkg.Members {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if g, ok := pkg.Members[n].(*ssa.Global); ok {
			m.global(g)
		}
	}
	for _, fn := range p.byPkg[pkg] {
		m.function(fn, false)
	}
	m.drain()
	return m.finish(pkg.Pkg.Path())
}

// GenerateMain returns the entry module that defines marmot_main.
func (p *Program) GenerateMain(mainPkg *ssa.Package) (Module, error) {
	m := p.newModule(nil)
	var b strings.Builder
	b.WriteString("  llvm.func @marmot_main() {\n")
	call := func(fn *ssa.Function) {
		if fn == nil {
			return
		}
		sym := p.symbol(fn)
		m.declare(sym, fn)
		fmt.Fprintf(&b, "    llvm.call %s() : () -> ()\n", mlir.Symbol(sym))
	}
	call(p.runtime.Func("init"))
	call(mainPkg.Func("init"))
	call(mainPkg.Func("main"))
	b.WriteString("    llvm.return\n  }\n")
	m.defs.WriteString(b.String())
	return m.finish("marmot:main")
}

func (m *moduleGen) errorf(err error) {
	m.errs = append(m.errs, err)
}

// drain emits queued shared functions (instances and wrappers).
func (m *moduleGen) drain() {
	for len(m.queue) > 0 {
		fn := m.queue[0]
		m.queue = m.queue[1:]
		m.function(fn, true)
	}
}

func (m *moduleGen) finish(name string) (Module, error) {
	if len(m.errs) > 0 {
		var sb []string
		for _, e := range m.errs {
			sb = append(sb, e.Error())
		}
		return Module{}, fmt.Errorf("%s", strings.Join(sb, "\n"))
	}
	var out strings.Builder
	out.WriteString("module {\n")
	keys := make([]string, 0, len(m.decls))
	for k := range m.decls {
		if !m.defined[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.WriteString(m.decls[k])
	}
	out.WriteString(m.defs.String())
	out.WriteString("}\n")
	return Module{Name: name, Text: out.String()}, nil
}

// funcRef makes fn available in this module under its symbol and returns the
// symbol. Shared functions are queued for a local linkonce_odr copy.
func (m *moduleGen) funcRef(fn *ssa.Function) string {
	sym := m.p.symbol(fn)
	if m.defined[sym] {
		return sym
	}
	if owner := m.p.owner[fn]; owner == nil && len(fn.Blocks) > 0 {
		m.defined[sym] = true
		m.queue = append(m.queue, fn)
		return sym
	}
	m.declare(sym, fn)
	return sym
}

// declare records an external declaration for fn.
func (m *moduleGen) declare(sym string, fn *ssa.Function) {
	if _, ok := m.decls[sym]; ok {
		return
	}
	params, ret := m.signature(fn)
	var b strings.Builder
	fmt.Fprintf(&b, "  llvm.func %s(", mlir.Symbol(sym))
	for i, t := range params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t)
		if isCSymbol(sym) {
			b.WriteString(extAttr(t, fn, i))
		}
	}
	b.WriteString(")")
	if ret != "" {
		b.WriteString(" -> " + ret)
	}
	if a := m.funcAttrs(fn, sym); a != "" {
		b.WriteString(" attributes {" + a + "}")
	}
	b.WriteString("\n")
	m.decls[sym] = b.String()
}

// declareRaw records a declaration with explicit types (C runtime helpers).
func (m *moduleGen) declareRaw(sym string, params []string, ret string, attrs string) {
	if _, ok := m.decls[sym]; ok {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  llvm.func %s(%s)", mlir.Symbol(sym), strings.Join(params, ", "))
	if ret != "" {
		b.WriteString(" -> " + ret)
	}
	if attrs != "" {
		b.WriteString(" attributes {" + attrs + "}")
	}
	b.WriteString("\n")
	m.decls[sym] = b.String()
}

// isCSymbol reports whether sym names a C function (runtime core), which
// needs C ABI argument extension attributes.
func isCSymbol(sym string) bool {
	return !strings.ContainsAny(sym, "./()$[")
}

// extAttr returns the zeroext/signext attribute C expects for small integer
// parameter i of fn.
func extAttr(t string, fn *ssa.Function, i int) string {
	switch t {
	case mlir.I1, mlir.I8, mlir.I16:
		pt := fn.Signature.Params().At(i).Type()
		if isSigned(pt) {
			return " {llvm.signext}"
		}
		return " {llvm.zeroext}"
	}
	return ""
}

// signature returns the LLVM parameter types and result type of fn,
// including the receiver and the hidden closure parameter.
func (m *moduleGen) signature(fn *ssa.Function) ([]string, string) {
	var params []string
	sig := fn.Signature
	if r := sig.Recv(); r != nil {
		params = append(params, m.tm.valType(r.Type()))
	}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, m.tm.valType(sig.Params().At(i).Type()))
	}
	if len(fn.FreeVars) > 0 {
		params = append(params, mlir.Ptr)
	}
	return params, m.tm.valType(sig.Results())
}

// sigTypes returns the LLVM parameter types and result of a signature as
// called through a func value (with the trailing closure parameter).
func (m *moduleGen) sigTypes(sig *types.Signature, closure bool) ([]string, string) {
	var params []string
	if r := sig.Recv(); r != nil {
		params = append(params, m.tm.valType(r.Type()))
	}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, m.tm.valType(sig.Params().At(i).Type()))
	}
	if closure {
		params = append(params, mlir.Ptr)
	}
	return params, m.tm.valType(sig.Results())
}

func (m *moduleGen) funcAttrs(fn *ssa.Function, sym string) string {
	var attrs []string
	pr := m.p.pragmas[fn]
	if pr&pragmaNoinline != 0 {
		attrs = append(attrs, "no_inline")
	}
	if isNoreturn(fn) {
		attrs = append(attrs, `passthrough = ["noreturn"]`)
	}
	if sym == "rt_setjmp" {
		attrs = append(attrs, `passthrough = ["returns_twice"]`)
	}
	return strings.Join(attrs, ", ")
}

var noreturnRuntime = map[string]bool{
	"panicindex": true, "panicslice": true, "panicnil": true, "panicdivide": true,
	"panicshift": true, "panicmakeslicelen": true, "panicunsafe": true,
	"gopanic": true, "panicTypeAssert": true, "goexit": true,
	"panicslice3": true, "panicmakeslicecap": true, "panicnildottype": true,
	"panicoverflow": true, "panicunsafeslicelen": true, "panicdottype": true,
}

func isNoreturn(fn *ssa.Function) bool {
	return fn.Pkg != nil && fn.Pkg.Pkg.Path() == "runtime" && noreturnRuntime[fn.Name()]
}

// global emits a package level variable.
func (m *moduleGen) global(g *ssa.Global) {
	sym := m.globalSym(g)
	m.defined[sym] = true
	t := m.tm.memType(g.Type().(*types.Pointer).Elem())
	fmt.Fprintf(&m.defs, "  llvm.mlir.global external %s() {addr_space = 0 : i32} : %s {\n    %%0 = llvm.mlir.zero : %s\n    llvm.return %%0 : %s\n  }\n",
		mlir.Symbol(sym), t, t, t)
}

func (m *moduleGen) globalSym(g *ssa.Global) string {
	return g.Pkg.Pkg.Path() + "." + g.Name()
}

// globalRef makes a global available and returns its symbol.
func (m *moduleGen) globalRef(g *ssa.Global) string {
	sym := m.globalSym(g)
	if !m.defined[sym] {
		if _, ok := m.decls[sym]; !ok {
			t := m.tm.memType(g.Type().(*types.Pointer).Elem())
			m.decls[sym] = fmt.Sprintf("  llvm.mlir.global external %s() {addr_space = 0 : i32} : %s\n", mlir.Symbol(sym), t)
		}
	}
	return sym
}

// stringData returns the symbol of a private constant holding s.
func (m *moduleGen) stringData(s string) string {
	if sym, ok := m.strs[s]; ok {
		return sym
	}
	sym := fmt.Sprintf(".str.%d", m.nstr)
	m.nstr++
	m.strs[s] = sym
	m.defined[sym] = true
	fmt.Fprintf(&m.defs, "  llvm.mlir.global private unnamed_addr constant %s(%s) {addr_space = 0 : i32}\n",
		mlir.Symbol(sym), mlir.Quote(s))
	return sym
}

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

	defs        strings.Builder // definitions: globals and functions
	decls       map[string]string
	defined     map[string]bool
	queue       []*ssa.Function
	strs        map[string]string
	nstr        int
	nthunk      int
	regionQueue []*ssa.Function
	static      *staticInit
	errs        []error
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
	m.static = m.analyzeStaticInit(pkg)
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
	for m.drain(); len(m.regionQueue) > 0; m.drain() {
		m.drainRegions()
	}
	m.static.emitObjects()
	m.keepWriteOnlyGlobals(pkg, names)
	return m.finish(pkg.Pkg.Path())
}

// GenerateMain returns the entry module that defines marmot_main.
func (p *Program) GenerateMain(mainPkg *ssa.Package) (Module, error) {
	m := p.newModule(nil)
	m.static = &staticInit{}
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
	// Method pruning is whole-program: modules depend on its result.
	fmt.Fprintf(&out, "// methods: %s\n", m.p.usedMethodsHash)
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
	var pass []string
	if isNoreturn(fn) {
		pass = append(pass, `"noreturn"`)
	}
	if sym == "rt_setjmp" {
		pass = append(pass, `"returns_twice"`)
	}
	if len(fn.Blocks) > 0 {
		if callsRecover(fn) || callsGetcallerfp(fn) {
			// gorecover walks frame records: keep this frame and its record,
			// and keep the machine outliner (on at -Os/-Oz) away from it.
			pass = append(pass, `["disable-tail-calls", "true"]`, `["frame-pointer", "all"]`, `"nooutline"`)
		} else {
			// Frame records are required by the Darwin ABI and by recover.
			pass = append(pass, `["frame-pointer", "non-leaf"]`)
		}
	}
	if len(pass) > 0 {
		attrs = append(attrs, "passthrough = ["+strings.Join(pass, ", ")+"]")
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
	if img := m.static.images[g]; img != nil && !m.static.dynamic[g] {
		m.constGlobal(sym, "external", false, m.static.value(img))
		return
	}
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

// keepWriteOnlyGlobals lists in llvm.used the package variables that hold
// pointers, are never read and are still stored at run time. Whole program
// optimization would otherwise delete them with their stores, and the
// objects they keep alive (os.Stdout and its finalizer) would be collected.
func (m *moduleGen) keepWriteOnlyGlobals(pkg *ssa.Package, names []string) {
	var keep []cv
	for _, n := range names {
		g, ok := pkg.Members[n].(*ssa.Global)
		if !ok || strings.Contains(n, "$") || m.p.globalRead[g] {
			continue
		}
		et := g.Type().(*types.Pointer).Elem()
		if len(ptrWords(et, m.p.opts.Sizes)) == 0 {
			continue
		}
		if img := m.static.images[g]; img != nil && !m.static.dynamic[g] {
			continue // static data has no finalizers
		}
		if m.p.globalDead(g) && !m.storedAtRunTime(g) {
			continue
		}
		keep = append(keep, cAddr(m.globalSym(g)))
	}
	if len(keep) == 0 {
		return
	}
	var b strings.Builder
	t := mlir.Array(int64(len(keep)), mlir.Ptr)
	fmt.Fprintf(&b, "  llvm.mlir.global appending @llvm.used() {addr_space = 0 : i32, section = \"llvm.metadata\"} : %s {\n", t)
	n := 0
	v := writeCV(&b, &n, cAgg(t, keep...))
	fmt.Fprintf(&b, "    llvm.return %s : %s\n  }\n", v, t)
	m.defs.WriteString(b.String())
}

// storedAtRunTime reports whether package init still stores into g.
func (m *moduleGen) storedAtRunTime(g *ssa.Global) bool {
	init := m.pkg.Func("init")
	if init == nil {
		return false
	}
	for _, b := range init.Blocks {
		for _, instr := range b.Instrs {
			st, ok := instr.(*ssa.Store)
			if !ok || m.static.skip[instr] {
				continue
			}
			if root, _, _ := addrPath(st.Addr); root == ssa.Value(g) {
				return true
			}
		}
	}
	return false
}

// callsGetcallerfp reports whether fn uses the getcallerfp intrinsic (the
// runtime's panic loop, whose frame address recover compares against).
func callsGetcallerfp(fn *ssa.Function) bool {
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if c, ok := instr.(ssa.CallInstruction); ok {
				if callee := c.Common().StaticCallee(); callee != nil && callee.Name() == "getcallerfp" &&
					callee.Pkg != nil && callee.Pkg.Pkg.Path() == "runtime" {
					return true
				}
			}
		}
	}
	return false
}

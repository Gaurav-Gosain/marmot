package loader

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// purego (github.com/ebitengine/purego) calls C functions without cgo,
// through assembly that marmot does not build and runtime internals that
// marmot's runtime does not have. The loader edits its sources:
//
//   - runtime_cgocall calls rt_purego_syscall (runtime/core/src/purego.S),
//     which calls the C function of purego's argument block;
//   - the callback entries are rt_purego_callback_entry's, which call
//     purego's callbackWrap through rt_purego_go_callback;
//   - Dlopen, Dlsym, Dlerror and Dlclose call the C functions directly;
//   - internal/fakecgo (a cgo runtime for gc without cgo) is left empty.
//
// RegisterFunc and RegisterLibFunc make the Go function with
// reflect.MakeFunc, which marmot does not have. At each call whose
// function pointer has a type known at compile time, the loader passes an
// adapter instead: a function literal of that type that hands its
// arguments to purego's implementation as reflect values, which is what
// MakeFunc does. purego's own code does everything else, so a call
// behaves as with gc. Other calls (a pointer in an interface, a type
// parameter) panic with an explanation.
//
// The edits are made on the version's source text; an unknown version is
// an error, as is a known one whose text does not match.

const (
	puregoPath    = "github.com/ebitengine/purego"
	fakecgoPath   = puregoPath + "/internal/fakecgo"
	puregoAlias   = "zz_marmot_purego"
	puregoAdapter = "zz_marmot_purego_fn"
)

// puregoVersions are the release series whose sources the edits are
// written for.
var puregoVersions = []string{"v0.10.", "v0.11."}

// puregoUser reports whether p needs purego edits: purego itself, its
// fakecgo, or a package that imports purego.
func puregoUser(p *packages.Package) bool {
	if p.PkgPath == puregoPath || p.PkgPath == fakecgoPath {
		return true
	}
	_, ok := p.Imports[puregoPath]
	return ok
}

// puregoVersion checks the version of the module p belongs to.
func puregoVersion(p *packages.Package) error {
	v := ""
	if p.Module != nil {
		v = p.Module.Version
		if p.Module.Replace != nil {
			v = p.Module.Replace.Version
		}
	}
	if v == "" {
		return nil // a local copy: the text checks decide
	}
	for _, s := range puregoVersions {
		if strings.HasPrefix(v, s) {
			return nil
		}
	}
	return fmt.Errorf("%s %s: marmot builds purego %sx (its assembly and runtime hooks are replaced per version); use one of those versions", puregoPath, v, strings.Join(puregoVersions, "x, "))
}

// puregoEdits returns the edits of file f of p.
func puregoEdits(p *packages.Package, f *ast.File) ([]embedEditAt, error) {
	name := p.Fset.File(f.Pos()).Name()
	switch p.PkgPath {
	case fakecgoPath:
		return []embedEditAt{{pos: 0, del: -1, text: "package fakecgo\n"}}, nil
	case puregoPath:
		if err := puregoVersion(p); err != nil {
			return nil, err
		}
		src, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		return puregoFileEdits(filepath.Base(name), string(src))
	}
	return puregoCallEdits(p, f), nil
}

// puregoPatch replaces old by new in a purego file, n times; a negative n
// means -n times or none (code that only some versions have).
type puregoPatch struct {
	old, new string
	n        int
}

var puregoPatches = map[string][]puregoPatch{
	"dlfcn.go": {
		{"\tRegisterFunc(&fnDlopen, dlopenABI0)\n", "\tfnDlopen = marmotDlopen\n", 1},
		{"\tRegisterFunc(&fnDlsym, dlsymABI0)\n", "\tfnDlsym = marmotDlsym\n", 1},
		{"\tRegisterFunc(&fnDlerror, dlerrorABI0)\n", "\tfnDlerror = marmotDlerror\n", 1},
		{"\tRegisterFunc(&fnDlclose, dlcloseABI0)\n", "\tfnDlclose = marmotDlclose\n", 1},
		// The variables named after the C functions would define those
		// symbols here.
		{"//go:linkname dlopen dlopen\nvar dlopen uint8\n", "", 1},
		{"//go:linkname dlsym dlsym\nvar dlsym uint8\n", "", 1},
		{"//go:linkname dlclose dlclose\nvar dlclose uint8\n", "", 1},
		{"//go:linkname dlerror dlerror\nvar dlerror uint8\n", "", 1},
		{"uintptr(unsafe.Pointer(&dlopen))", "uintptr(unsafe.Pointer(nil))", 1},
		{"uintptr(unsafe.Pointer(&dlsym))", "uintptr(unsafe.Pointer(nil))", 1},
		{"uintptr(unsafe.Pointer(&dlclose))", "uintptr(unsafe.Pointer(nil))", 1},
		{"uintptr(unsafe.Pointer(&dlerror))", "uintptr(unsafe.Pointer(nil))", 1},
	},
	"func.go": {
		{"func RegisterFunc(fptr any, cfn uintptr) {\n",
			"func RegisterFunc(fptr any, cfn uintptr) { registerFunc(fptr, cfn, nil) }\n\nfunc registerFunc(fptr any, cfn uintptr, marmotAdapter MarmotAdapter) {\n", 1},
		{"reflect.MakeFunc(ty, ", "marmotMakeFunc(ty, marmotAdapter, ", 1},
	},
	// The callbacks: syscall_unix.go from v0.11, syscall_sysv.go before.
	"syscall_unix.go": puregoCallbackPatches,
	"syscall_sysv.go": puregoCallbackPatches,
	"go_runtime.go": {
		{"//go:linkname runtime_cgocall runtime.cgocall\nfunc runtime_cgocall(fn uintptr, arg unsafe.Pointer) int32 // from runtime/sys_libc.go\n",
			"func runtime_cgocall(fn uintptr, arg unsafe.Pointer) int32 { marmot_syscall(arg); return 0 }\n\n//go:linkname marmot_syscall rt_purego_syscall\nfunc marmot_syscall(arg unsafe.Pointer)\n", 1},
		// v0.11 on: arm's soft float flag.
		{"//go:linkname runtime_goarmsoftfp runtime.goarmsoftfp\n", "", -1},
	},
}

var puregoCallbackPatches = []puregoPatch{
	{"//go:linkname __callbackasm callbackasm\n", "", 1},
	{"func callbackasmAddr(i int) uintptr {\n", "func callbackasmAddrGC(i int) uintptr {\n", 1},
}

// puregoFileEdits returns the edits of a purego file: patches, or for
// nocgo.go (which imports fakecgo) the marmot code in its place.
func puregoFileEdits(base, src string) ([]embedEditAt, error) {
	if base == "nocgo.go" {
		return []embedEditAt{{pos: 0, del: -1, text: puregoNocgo}}, nil
	}
	ps, ok := puregoPatches[base]
	if !ok {
		return nil, nil
	}
	out := src
	for _, pt := range ps {
		if c := strings.Count(out, pt.old); c != pt.n && !(pt.n < 0 && (c == 0 || c == -pt.n)) {
			return nil, fmt.Errorf("%s/%s: marmot's purego edits do not match this version (%q found %d times, want %d)", puregoPath, base, strings.TrimSpace(pt.old), c, pt.n)
		}
		out = strings.ReplaceAll(out, pt.old, pt.new)
	}
	return []embedEditAt{{pos: 0, del: -1, text: out}}, nil
}

// puregoNocgo replaces nocgo.go: purego's marmot side.
const puregoNocgo = `//go:build !cgo && (darwin || freebsd || linux || netbsd)

package purego

import (
	"reflect"
	"runtime"

	"github.com/ebitengine/purego/internal/strings"
)

//go:linkname marmot_dlopen dlopen
func marmot_dlopen(path *byte, mode int32) uintptr

//go:linkname marmot_dlsym dlsym
func marmot_dlsym(handle uintptr, name *byte) uintptr

//go:linkname marmot_dlerror dlerror
func marmot_dlerror() uintptr

//go:linkname marmot_dlclose dlclose
func marmot_dlclose(handle uintptr) int32

func marmotDlopen(path string, mode int) uintptr {
	p := strings.CString(path)
	r := marmot_dlopen(p, int32(mode))
	runtime.KeepAlive(p)
	return r
}

func marmotDlsym(handle uintptr, name string) uintptr {
	p := strings.CString(name)
	r := marmot_dlsym(handle, p)
	runtime.KeepAlive(p)
	return r
}

func marmotDlerror() string { return strings.GoString(marmot_dlerror()) }

func marmotDlclose(handle uintptr) bool { return marmot_dlclose(handle) != 0 }

//go:linkname marmot_callback_entry rt_purego_callback_entry
func marmot_callback_entry(i int) uintptr

func callbackasmAddr(i int) uintptr { return marmot_callback_entry(i) }

//go:linkname marmot_go_callback rt_purego_go_callback
func marmot_go_callback(a *callbackArgs) { callbackWrap(a) }

// MarmotValue, MarmotImpl and MarmotAdapter are the types of the
// adapters marmot passes to RegisterFuncMarmot and RegisterLibFuncMarmot:
// an adapter makes the function value of the pointer's type that
// reflect.MakeFunc would make.
type (
	MarmotValue   = reflect.Value
	MarmotImpl    = func([]reflect.Value) []reflect.Value
	MarmotAdapter = func(MarmotImpl) MarmotValue
)

// MarmotArg is an adapter's argument *p as a reflect value.
func MarmotArg(p any) reflect.Value { return reflect.ValueOf(p).Elem() }

// MarmotResult stores the result v at p.
func MarmotResult(v reflect.Value, p any) { reflect.ValueOf(p).Elem().Set(v) }

// MarmotFunc is the adapter's function as a reflect value.
func MarmotFunc(f any) reflect.Value { return reflect.ValueOf(f) }

// RegisterFuncMarmot is RegisterFunc with the adapter of fptr's type.
func RegisterFuncMarmot(fptr any, cfn uintptr, adapter MarmotAdapter) {
	registerFunc(fptr, cfn, adapter)
}

// RegisterLibFuncMarmot is RegisterLibFunc with the adapter of fptr's type.
func RegisterLibFuncMarmot(fptr any, handle uintptr, name string, adapter MarmotAdapter) {
	sym, err := loadSymbol(handle, name)
	if err != nil {
		panic(err)
	}
	registerFunc(fptr, sym, adapter)
}

// marmotAdapters are the adapters of the function types used as type
// arguments, for calls whose pointer has a type parameter's type.
var marmotAdapters map[reflect.Type]MarmotAdapter

// MarmotRegisterAdapter records the adapter of *p's function type.
func MarmotRegisterAdapter(p any, adapter MarmotAdapter) bool {
	if marmotAdapters == nil {
		marmotAdapters = map[reflect.Type]MarmotAdapter{}
	}
	marmotAdapters[reflect.TypeOf(p).Elem()] = adapter
	return true
}

func marmotMakeFunc(ty reflect.Type, adapter MarmotAdapter, impl MarmotImpl) reflect.Value {
	if adapter == nil {
		adapter = marmotAdapters[ty]
	}
	if adapter == nil {
		panic("purego: RegisterFunc of " + ty.String() + ": marmot has no reflect.MakeFunc, so it builds the function where RegisterFunc or RegisterLibFunc is called with a pointer to a func variable, or for the function types used as type arguments of generic code that calls purego; this call passes the pointer in an interface")
	}
	return adapter(impl)
}
`

// puregoCallEdits rewrites the calls of purego.RegisterFunc and
// RegisterLibFunc in f whose function pointer type is known:
//
//	purego.RegisterLibFunc(&puts, libc, "puts")
//	-> zz_marmot_purego.RegisterLibFuncMarmot(&puts, libc, "puts", zz_marmot_purego_fn1)
//
// with zz_marmot_purego_fn1 the adapter of puts's type, at the end of the
// file, and the imports it needs on the package clause line. A call in
// generic code, whose pointer has a type parameter's type, stays; instead
// every function type used in f as a type argument of a generic of a
// package that imports purego gets its adapter registered by type
// (MarmotRegisterAdapter), which purego looks up for such a call:
//
//	getFunc[MachHostSelfFunc](lib, "mach_host_self")
//	-> var _ = zz_marmot_purego.MarmotRegisterAdapter((*MachHostSelfFunc)(nil), zz_marmot_purego_fn2)
func puregoCallEdits(p *packages.Package, f *ast.File) []embedEditAt {
	if p.TypesInfo == nil || p.Types == nil {
		return nil
	}
	var edits []embedEditAt
	adapters := map[string]string{} // type -> adapter name
	var adapterSrc []string
	imports := map[string]string{} // path -> alias
	var importOrder []string
	qual := func(pkg *types.Package) string {
		if pkg == p.Types {
			return ""
		}
		if a, ok := imports[pkg.Path()]; ok {
			return a
		}
		a := fmt.Sprintf("zz_marmot_i%d", len(imports)+1)
		imports[pkg.Path()] = a
		importOrder = append(importOrder, pkg.Path())
		return a
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		var id *ast.Ident
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.SelectorExpr:
			id = fun.Sel
		case *ast.Ident:
			id = fun
		default:
			return true
		}
		fn, ok := p.TypesInfo.Uses[id].(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != puregoPath {
			return true
		}
		if fn.Name() != "RegisterFunc" && fn.Name() != "RegisterLibFunc" {
			return true
		}
		ptr, ok := p.TypesInfo.TypeOf(call.Args[0]).(*types.Pointer)
		if !ok {
			return true
		}
		sig, ok := ptr.Elem().Underlying().(*types.Signature)
		if !ok || !nameable(ptr.Elem(), p) {
			return true
		}
		saved := len(importOrder)
		tstr := types.TypeString(ptr.Elem(), qual)
		for _, path := range importOrder[saved:] {
			if _, ok := p.Imports[path]; !ok {
				// The package of a type is not imported here: leave the
				// call (it panics with the explanation).
				for _, q := range importOrder[saved:] {
					delete(imports, q)
				}
				importOrder = importOrder[:saved]
				return true
			}
		}
		name, ok := adapters[tstr]
		if !ok {
			name = fmt.Sprintf("%s%d", puregoAdapter, len(adapters)+1)
			adapters[tstr] = name
			adapterSrc = append(adapterSrc, puregoAdapterSrc(name, tstr, sig, qual))
		}
		start := p.Fset.Position(call.Fun.Pos()).Offset
		end := p.Fset.Position(call.Fun.End()).Offset
		edits = append(edits,
			embedEditAt{pos: start, del: end - start, text: puregoAlias + "." + fn.Name() + "Marmot"},
			embedEditAt{pos: p.Fset.Position(call.Args[len(call.Args)-1].End()).Offset, text: ", " + name})
		return true
	})
	var regs []string
	// In source order, so the adapters' names are the same every build.
	var ids []*ast.Ident
	for id := range p.TypesInfo.Instances {
		if id.Pos() >= f.Pos() && id.Pos() < f.End() {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Pos() < ids[j].Pos() })
	for _, id := range ids {
		inst := p.TypesInfo.Instances[id]
		obj := p.TypesInfo.Uses[id]
		if obj == nil || obj.Pkg() == nil || !importsPurego(obj.Pkg(), p.Types) {
			continue
		}
		for i := range inst.TypeArgs.Len() {
			t := inst.TypeArgs.At(i)
			sig, ok := t.Underlying().(*types.Signature)
			if !ok || !nameable(t, p) {
				continue
			}
			saved := len(importOrder)
			tstr := types.TypeString(t, qual)
			missing := false
			for _, path := range importOrder[saved:] {
				if _, ok := p.Imports[path]; !ok {
					missing = true
				}
			}
			if missing {
				for _, q := range importOrder[saved:] {
					delete(imports, q)
				}
				importOrder = importOrder[:saved]
				continue
			}
			name, ok := adapters[tstr]
			if !ok {
				name = fmt.Sprintf("%s%d", puregoAdapter, len(adapters)+1)
				adapters[tstr] = name
				adapterSrc = append(adapterSrc, puregoAdapterSrc(name, tstr, sig, qual))
			}
			regs = append(regs, fmt.Sprintf("var _ = %s.MarmotRegisterAdapter((*%s)(nil), %s)\n", puregoAlias, tstr, name))
		}
	}
	if len(edits) == 0 && len(regs) == 0 {
		return nil
	}
	sort.Strings(regs)
	regs = slices.Compact(regs)
	adapterSrc = append(adapterSrc, regs...)
	imp := "; import (" + puregoAlias + " \"" + puregoPath + "\""
	for _, path := range importOrder {
		imp += "; " + imports[path] + " \"" + path + "\""
	}
	imp += ")"
	edits = append(edits,
		embedEditAt{pos: p.Fset.Position(f.Name.End()).Offset, text: imp},
		embedEditAt{pos: p.Fset.Position(f.FileEnd).Offset, text: "\n" + strings.Join(adapterSrc, "\n")})
	return edits
}

// importsPurego reports whether pkg (the package of a generic) imports
// purego: self is the package being edited, which does.
func importsPurego(pkg, self *types.Package) bool {
	if pkg == self {
		return true
	}
	for _, q := range pkg.Imports() {
		if q.Path() == puregoPath {
			return true
		}
	}
	return false
}

// nameable reports whether t can be written in a file of p: no type
// parameters, no unexported types of other packages, no types local to a
// function.
func nameable(t types.Type, p *packages.Package) bool {
	ok := true
	seen := map[types.Type]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		if !ok || seen[t] {
			return
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.TypeParam:
			ok = false
		case *types.Named:
			obj := t.Obj()
			if obj.Pkg() != nil {
				if obj.Pkg() != p.Types && !obj.Exported() {
					ok = false
					return
				}
				if obj.Parent() != obj.Pkg().Scope() {
					ok = false // declared inside a function
					return
				}
			}
			if args := t.TypeArgs(); args != nil {
				for i := range args.Len() {
					walk(args.At(i))
				}
			}
		case *types.Alias:
			walk(types.Unalias(t))
		case *types.Pointer:
			walk(t.Elem())
		case *types.Slice:
			walk(t.Elem())
		case *types.Array:
			walk(t.Elem())
		case *types.Map:
			walk(t.Key())
			walk(t.Elem())
		case *types.Chan:
			walk(t.Elem())
		case *types.Signature:
			for i := range t.Params().Len() {
				walk(t.Params().At(i).Type())
			}
			for i := range t.Results().Len() {
				walk(t.Results().At(i).Type())
			}
		case *types.Struct:
			for i := range t.NumFields() {
				walk(t.Field(i).Type())
			}
		case *types.Interface:
			for i := range t.NumMethods() {
				walk(t.Method(i).Type())
			}
		}
	}
	walk(t)
	return ok
}

// puregoAdapterSrc is the adapter of the function type tstr (signature
// sig): it makes a function of that type that passes its arguments to
// purego's implementation and returns its result.
func puregoAdapterSrc(name, tstr string, sig *types.Signature, qual types.Qualifier) string {
	var params, args, results, sets []string
	for i := range sig.Params().Len() {
		t := types.TypeString(sig.Params().At(i).Type(), qual)
		if sig.Variadic() && i == sig.Params().Len()-1 {
			t = "..." + strings.TrimPrefix(t, "[]")
		}
		params = append(params, fmt.Sprintf("a%d %s", i, t))
		args = append(args, fmt.Sprintf("%s.MarmotArg(&a%d)", puregoAlias, i))
	}
	for i := range sig.Results().Len() {
		results = append(results, fmt.Sprintf("r%d %s", i, types.TypeString(sig.Results().At(i).Type(), qual)))
		sets = append(sets, fmt.Sprintf("\t\t%s.MarmotResult(out[%d], &r%d)\n", puregoAlias, i, i))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "func %s(impl %s.MarmotImpl) %s.MarmotValue {\n", name, puregoAlias, puregoAlias)
	fmt.Fprintf(&b, "\treturn %s.MarmotFunc((%s)(func(%s) (%s) {\n", puregoAlias, tstr, strings.Join(params, ", "), strings.Join(results, ", "))
	out := "_ = "
	if len(sets) > 0 {
		out = "out := "
	}
	fmt.Fprintf(&b, "\t\t%simpl([]%s.MarmotValue{%s})\n", out, puregoAlias, strings.Join(args, ", "))
	b.WriteString(strings.Join(sets, ""))
	b.WriteString("\t\treturn\n\t}))\n}\n")
	return b.String()
}

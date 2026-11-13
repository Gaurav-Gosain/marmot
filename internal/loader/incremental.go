package loader

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/cgo"
	"github.com/Gaurav-Gosain/marmot/internal/genapi"
	"github.com/Gaurav-Gosain/marmot/internal/rtexport"
	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

// Incremental front end. Package metadata comes from go list (no parsing,
// no type checking). Each package gets a key: a hash of its files (path,
// size, modification time), the build configuration, and the export hashes
// of its imports. A package whose key has cached export data, and that the
// caller does not need from source, is imported from that export data; the
// others are parsed and type checked against the imported ones, and their
// export data is written to the cache. An edit that does not change a
// package's exported API leaves the keys of its importers unchanged.
//
// Packages with generic declarations are always loaded from source when
// anything is: SSA needs their bodies to instantiate them.
//
// Per package facts that code generation reads from other packages' syntax
// (linkname directives, dynamic imports, functions that call recover) are
// cached with the export data.

// Facts are what code generation needs from a package it does not compile.
type Facts struct {
	Linknames [][2]string       `json:",omitempty"` // local name, target symbol
	Dynamic   map[string]string `json:",omitempty"` // cgo_import_dynamic local -> symbol
	Dylibs    []string          `json:",omitempty"`
	Recover   []string          `json:",omitempty"` // "F" or "T.M" of functions that call recover
	Bodyless  []string          `json:",omitempty"` // "F" or "T.M" of functions declared without a body
	// Uses of reflect outside package reflect that need code for every
	// program: Value.Call or CallSlice (call adapters), Method or
	// MethodByName lookups (every exported method), NewAt (any access may
	// alias).
	ReflectCall  bool `json:",omitempty"`
	MethodLookup bool `json:",omitempty"`
	NewAt        bool `json:",omitempty"`
}

// reflectUses sets the reflect facts of a package from its type information.
func reflectUses(p *packages.Package, info *types.Info, f *Facts) {
	if p.PkgPath == "reflect" || p.PkgPath == "internal/reflectlite" {
		return
	}
	for _, obj := range info.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil {
			continue
		}
		if fn.Pkg().Path() != "reflect" {
			// A method lookup through an interface of the package's own
			// that reflect.Type or reflect.Value satisfies.
			if (fn.Name() == "Method" || fn.Name() == "MethodByName") && returnsReflect(fn, "Method") {
				f.MethodLookup = true
			}
			if (fn.Name() == "Method" || fn.Name() == "MethodByName") && returnsReflect(fn, "Value") {
				f.MethodLookup, f.ReflectCall = true, true
			}
			continue
		}
		switch fn.Name() {
		case "Call", "CallSlice":
			f.ReflectCall = true
		case "Method", "MethodByName":
			f.MethodLookup = true
			// Value.Method values are called through the call adapters.
			if returnsReflect(fn, "Value") {
				f.ReflectCall = true
			}
		case "NewAt":
			f.NewAt = true
		}
	}
}

// returnsReflect reports whether fn returns a reflect.<name>.
func returnsReflect(fn *types.Func, name string) bool {
	res := fn.Type().(*types.Signature).Results()
	for v := range res.Variables() {
		if n, ok := types.Unalias(v.Type()).(*types.Named); ok &&
			n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "reflect" && n.Obj().Name() == name {
			return true
		}
	}
	return false
}

type pkgMeta struct {
	ExportHash string
	// APIHash hashes the package's API (apiHash): what an importer's code
	// can depend on, so it stays the same across body edits and edits that
	// only move declarations.
	APIHash string `json:",omitempty"`
	// GenericAPI: the API exposes generic code (genapi.Exposed), so
	// importers may compile instances of it.
	GenericAPI  bool `json:",omitempty"`
	Generic     bool
	Facts       *Facts
	GenericDeps []string `json:",omitempty"` // packages whose generics this package instantiates
	// GenericDepsG are those instantiated inside generic bodies: what a
	// package loaded only for its generic bodies needs from source.
	GenericDepsG []string `json:",omitempty"`
}

// IncrementalPackage is the state of one package after an incremental load.
type IncrementalPackage struct {
	Key        string // cache key (files, configuration, import export hashes)
	FromSource bool   // parsed and type checked in this load
	Facts      *Facts
	// ModBase is the key of what separately compiled code of the package
	// depends on: its own files, the API and facts of its imports
	// (transitively), and the source of every package it depends on whose
	// API exposes generic code (genapi; instances of that code may be
	// compiled into the package's module). The same string goes to
	// NeedSource.
	ModBase string
}

type incrState struct {
	cfg      Config
	cacheDir string
	confSig  string
	fset     *token.FileSet
	imports  map[string]*types.Package
	overlay  map[string][]byte
	sizes    types.Sizes
	needSrc  func(p *packages.Package, key string) bool
	result   map[*packages.Package]*IncrementalPackage
	meta     map[*packages.Package]*pkgMeta
	forceSrc map[string]bool // generic packages a source package instantiates
	byPath   map[string]*packages.Package
	retry    []string
	srcKeys  map[*packages.Package]string // transitive source (srcKeyOf)
	apiKeys  map[*packages.Package]string // transitive API and facts (apiKeyOf)
	genKeys  map[*packages.Package]string // transitive source of generic packages (apiKeyOf)
	mu       sync.Mutex
}

// allNames reports whether a package's export data keeps its unexported
// declarations (rtexport.WriteAll): code generation looks up unexported
// runtime functions and types by name, and the entry module calls a main
// package's main, so these load from export data like the rest.
func allNames(path, name string) bool { return path == "runtime" || name == "main" }

// apiHash hashes what other packages' code can depend on in pkg's own
// declarations: every type (unexported ones too: they lay out exported
// types and their methods fill method tables) with its methods, and the
// exported functions, variables and constants (with their values). Other
// packages' types appear by qualified name only; their API keys cover
// them. Unlike export data, the text does not depend on whether pkg's
// imports were loaded from source or from export data.
func apiHash(pkg *types.Package) string {
	h := sha256.New()
	q := func(p *types.Package) string { return p.Path() }
	scope := pkg.Scope()
	fmt.Fprintf(h, "api v1 %s %s\n", pkg.Path(), pkg.Name())
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		tn, isType := obj.(*types.TypeName)
		if !isType && !obj.Exported() {
			continue
		}
		fmt.Fprintf(h, "%s\n", types.ObjectString(obj, q))
		if c, ok := obj.(*types.Const); ok {
			fmt.Fprintf(h, "= %s\n", c.Val().ExactString())
		}
		if isType && !tn.IsAlias() {
			fmt.Fprintf(h, "  %s\n", types.TypeString(tn.Type().Underlying(), q))
			if n, ok := tn.Type().(*types.Named); ok {
				for i := 0; i < n.NumMethods(); i++ {
					fmt.Fprintf(h, "  %s\n", types.ObjectString(n.Method(i), q))
				}
			}
		}
	}
	return apiHashPrefix + hex.EncodeToString(h.Sum(nil))[:32]
}

// apiHashPrefix marks hashes made by apiHash (older cache entries hashed
// export data, which depends on how imports were loaded).
const apiHashPrefix = "v1:"

// ownFiles are the files whose identity keys a package.
func ownFiles(p *packages.Package) []string {
	return append(append([]string(nil), p.CompiledGoFiles...), p.EmbedFiles...)
}

// srcKeyOf records the key of p's source and, transitively, its imports'.
// Called with st.mu held, after the imports are done.
func (st *incrState) srcKeyOf(p *packages.Package, own string) string {
	h := sha256.New()
	fmt.Fprintf(h, "src v1 %s\n%s\n", p.PkgPath, own)
	for _, path := range sortedImports(p) {
		fmt.Fprintf(h, "%s %s\n", path, st.srcKeys[p.Imports[path]])
	}
	k := hex.EncodeToString(h.Sum(nil))[:32]
	st.srcKeys[p] = k
	return k
}

// apiKeyOf records the key of p's API and facts and, transitively, its
// imports'. Called with st.mu held, after the imports are done.
func (st *incrState) apiKeyOf(p *packages.Package, m *pkgMeta) {
	h := sha256.New()
	api := m.APIHash
	if !strings.HasPrefix(api, apiHashPrefix) {
		api = "export " + m.ExportHash // a cache entry from before apiHash
	}
	facts, _ := json.Marshal(m.Facts)
	fmt.Fprintf(h, "api v1 %s\n%s\n%s\n", p.PkgPath, api, facts)
	g := sha256.New()
	fmt.Fprintf(g, "gen v1 %s\n", p.PkgPath)
	if m.GenericAPI || (m.APIHash == "" && m.Generic) {
		fmt.Fprintf(g, "generic %s\n", st.srcKeys[p])
	}
	for _, path := range sortedImports(p) {
		d := p.Imports[path]
		fmt.Fprintf(h, "%s %s\n", path, st.apiKeys[d])
		fmt.Fprintf(g, "%s %s\n", path, st.genKeys[d])
	}
	st.apiKeys[p] = hex.EncodeToString(h.Sum(nil))[:32]
	st.genKeys[p] = hex.EncodeToString(g.Sum(nil))[:32]
}

// modBase is IncrementalPackage.ModBase of p. Called with st.mu held,
// after the imports are done.
func (st *incrState) modBase(p *packages.Package, own string) string {
	h := sha256.New()
	fmt.Fprintf(h, "mod v1 %s %s\n%s\n", p.PkgPath, p.Name, own)
	for _, path := range sortedImports(p) {
		d := p.Imports[path]
		fmt.Fprintf(h, "import %s %s %s\n", path, st.apiKeys[d], st.genKeys[d])
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func inSpan(spans []span, pos token.Pos) bool {
	for _, s := range spans {
		if s.pos <= pos && pos < s.end {
			return true
		}
	}
	return false
}

type span struct{ pos, end token.Pos }

func sortedImports(p *packages.Package) []string {
	paths := make([]string, 0, len(p.Imports))
	for path := range p.Imports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// fileSig hashes the identity of files cheaply: path, size and mtime.
func filesSig(files []string) string {
	h := sha256.New()
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	for _, f := range sorted {
		st, err := os.Stat(f)
		if err != nil {
			fmt.Fprintf(h, "%s missing\n", f)
			continue
		}
		fmt.Fprintf(h, "%s %d %d\n", f, st.Size(), st.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (st *incrState) keyOf(p *packages.Package, own string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n", st.confSig, p.PkgPath, p.Name)
	if allNames(p.PkgPath, p.Name) {
		fmt.Fprintf(h, "export data with every name\n")
	}
	fmt.Fprintf(h, "%s\n", own)
	var deps []string
	for path, d := range p.Imports {
		m := st.meta[d]
		eh := ""
		if m != nil {
			eh = m.ExportHash
		}
		deps = append(deps, path+"="+d.PkgPath+"@"+eh)
	}
	sort.Strings(deps)
	fmt.Fprintf(h, "%s\n", strings.Join(deps, "\n"))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func (st *incrState) metaPath(key string) string   { return filepath.Join(st.cacheDir, key+".json") }
func (st *incrState) exportPath(key string) string { return filepath.Join(st.cacheDir, key+".export") }

func (st *incrState) readMeta(key string) (*pkgMeta, bool) {
	data, err := os.ReadFile(st.metaPath(key))
	if err != nil {
		return nil, false
	}
	var m pkgMeta
	if json.Unmarshal(data, &m) != nil {
		return nil, false
	}
	if _, err := os.Stat(st.exportPath(key)); err != nil {
		return nil, false
	}
	return &m, true
}

func (st *incrState) writeCache(key string, tpkg *types.Package, m *pkgMeta) error {
	var buf bytes.Buffer
	write := gcexportdata.Write
	if allNames(tpkg.Path(), tpkg.Name()) {
		write = rtexport.WriteAll
	}
	if err := write(&buf, st.fset, tpkg); err != nil {
		return err
	}
	data := buf.Bytes()
	h := sha256.Sum256(data)
	m.ExportHash = hex.EncodeToString(h[:16])
	m.APIHash = apiHash(tpkg)
	if err := os.MkdirAll(st.cacheDir, 0o755); err != nil {
		return err
	}
	if err := writeAtomic(st.exportPath(key), data); err != nil {
		return err
	}
	js, _ := json.Marshal(m)
	return writeAtomic(st.metaPath(key), js)
}

// WriteAtomic writes path as writeAtomic does.
func WriteAtomic(path string, data []byte) error { return writeAtomic(path, data) }

// writeAtomic writes path through a temporary file of its own, so that
// builds sharing the cache never see or rename each other's partial files.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name()) // best effort: the temp file of a failed write
	}
	return err
}

// importFromCache reads the export data of p.
func (st *incrState) importFromCache(p *packages.Package, key string) (*types.Package, error) {
	f, err := os.Open(st.exportPath(key))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read only
	return gcexportdata.Read(f, st.fset, st.imports, p.PkgPath)
}

// parseFiles parses the compiled Go files of p (overlays applied).
func (st *incrState) parseFiles(p *packages.Package) ([]*ast.File, error) {
	files := make([]*ast.File, len(p.CompiledGoFiles))
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	for i, name := range p.CompiledGoFiles {
		wg.Go(func() {
			var src any
			if data, ok := st.overlay[name]; ok {
				src = data
			}
			files[i], errs[i] = parser.ParseFile(st.fset, name, src, parser.AllErrors|parser.ParseComments|parser.SkipObjectResolution)
		})
	}
	wg.Wait()
	var msgs []string
	for _, e := range errs {
		if list, ok := e.(scanner.ErrorList); ok {
			// One error per line, as go/packages reports them.
			for _, x := range list {
				msgs = append(msgs, x.Error())
			}
		} else if e != nil {
			msgs = append(msgs, e.Error())
		}
	}
	if len(msgs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}
	return files, nil
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// typeCheck checks p from source against the imported packages.
func (st *incrState) typeCheck(p *packages.Package, files []*ast.File) (*types.Package, *types.Info, error) {
	info := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	var errs []string
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			d := p.Imports[path]
			if d == nil {
				return nil, fmt.Errorf("%s: import %q not found", p.PkgPath, path)
			}
			st.mu.Lock()
			t := st.imports[d.PkgPath]
			st.mu.Unlock()
			if t == nil {
				return nil, fmt.Errorf("%s: import %q not loaded", p.PkgPath, path)
			}
			return t, nil
		}),
		Sizes: st.sizes,
		Error: func(err error) {
			if len(errs) < 10 || st.cfg.AllErrors {
				errs = append(errs, err.Error())
			}
		},
	}
	if p.Module != nil && p.Module.GoVersion != "" {
		conf.GoVersion = "go" + p.Module.GoVersion
	}
	tpkg, _ := conf.Check(p.PkgPath, st.fset, files, info)
	if len(errs) > 0 {
		return nil, nil, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return tpkg, info, nil
}

// hasGenerics reports whether the package declares generic functions,
// methods of generic types, or generic types.
func hasGenerics(tpkg *types.Package) bool {
	scope := tpkg.Scope()
	for _, n := range scope.Names() {
		switch o := scope.Lookup(n).(type) {
		case *types.Func:
			if o.Type().(*types.Signature).TypeParams().Len() > 0 {
				return true
			}
		case *types.TypeName:
			if n, ok := o.Type().(*types.Named); ok && n.TypeParams().Len() > 0 {
				return true
			}
		}
	}
	return false
}

// factsOf collects the facts other packages' code generation needs.
func factsOf(p *packages.Package, files []*ast.File) *Facts {
	f := &Facts{}
	for _, file := range files {
		for _, cg := range file.Comments {
			for _, c := range cg.List {
				switch {
				case strings.HasPrefix(c.Text, "//go:linkname "):
					fs := strings.Fields(c.Text)
					if len(fs) >= 3 {
						f.Linknames = append(f.Linknames, [2]string{fs[1], fs[2]})
					} else if len(fs) == 2 {
						f.Linknames = append(f.Linknames, [2]string{fs[1], ""})
					}
				case strings.HasPrefix(c.Text, "//go:cgo_import_dynamic "):
					fs := strings.Fields(c.Text)
					if len(fs) >= 3 {
						if f.Dynamic == nil {
							f.Dynamic = map[string]string{}
						}
						f.Dynamic[fs[1]] = fs[2]
					}
					if len(fs) >= 4 {
						f.Dylibs = append(f.Dylibs, strings.Trim(fs[3], `"`))
					}
				}
			}
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				name = recvTypeName(fd.Recv.List[0].Type) + "." + name
			}
			if fd.Body == nil {
				f.Bodyless = append(f.Bodyless, name)
			} else if callsRecoverAST(fd.Body) {
				f.Recover = append(f.Recover, name)
			}
		}
	}
	return f
}

func recvTypeName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// callsRecoverAST reports whether body calls recover directly (not inside a
// function literal).
func callsRecoverAST(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "recover" && len(x.Args) == 0 {
				found = true
			}
		}
		return !found
	})
	return found
}

// loadIncremental loads the packages matched by patterns as described
// above. needSrc reports whether the caller needs package p (with key) from
// source this build (for example because its compiled code is not cached);
// roots and changed packages are always loaded from source.
func loadIncremental(cfg Config, pcfg *packages.Config, patterns []string, confSig string,
	needSrc func(p *packages.Package, key string) bool) ([]*packages.Package, []*packages.Package, map[*packages.Package]*IncrementalPackage, *token.FileSet, map[string]*cgo.Package, error) {
	light := lightConfig(pcfg)
	if cfg.CacheDir == "" {
		cfg.CacheDir = filepath.Join(cfg.Root, "build", "cache", "types")
	}
	tl := time.Now()
	roots, usesC, err := loadList(light, patterns, cfg.CacheDir)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	roots, cgoPkgs, err := cgoPackages(cfg, pcfg, roots, usesC, cgoRelist(light, patterns, cfg.CacheDir))
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if cfg.Log != nil {
		cfg.Log("go list: %v", time.Since(tl).Round(time.Millisecond))
	}
	var all []*packages.Package
	var listErrs []string
	packages.Visit(roots, nil, func(p *packages.Package) {
		all = append(all, p)
		for _, e := range p.Errors {
			listErrs = append(listErrs, e.Error())
		}
	})
	if len(listErrs) > 0 {
		return nil, nil, nil, nil, nil, fmt.Errorf("%s", strings.Join(listErrs, "\n"))
	}
	if cfg.Listed != nil {
		cfg.Listed(all)
	}
	force := map[string]bool{}
	for attempt := 0; ; attempt++ {
		for _, p := range all {
			// Fresh type information for every attempt.
			p.Types, p.TypesInfo, p.Syntax = nil, nil, nil
		}
		st := &incrState{
			cfg:      cfg,
			cacheDir: cfg.CacheDir,
			confSig:  confSig,
			fset:     token.NewFileSet(),
			imports:  map[string]*types.Package{},
			overlay:  copyOverlay(pcfg.Overlay),
			sizes:    types.SizesFor("gc", cfg.GOARCH),
			needSrc:  needSrc,
			result:   map[*packages.Package]*IncrementalPackage{},
			meta:     map[*packages.Package]*pkgMeta{},
			forceSrc: force,
			byPath:   map[string]*packages.Package{},
			srcKeys:  map[*packages.Package]string{},
			apiKeys:  map[*packages.Package]string{},
			genKeys:  map[*packages.Package]string{},
		}
		for _, p := range all {
			st.byPath[p.PkgPath] = p
		}
		// Generic packages that the packages compiled from source used to
		// instantiate (recorded by package path) come from source too.
		for _, p := range all {
			if p.PkgPath == "unsafe" {
				// As loadOne has it: no export data, an empty hash.
				st.meta[p] = &pkgMeta{}
				st.srcKeys[p], st.apiKeys[p], st.genKeys[p] = "unsafe", "unsafe", "unsafe"
				continue
			}
			own := filesSig(ownFiles(p))
			k := st.keyOf(p, own)
			st.srcKeyOf(p, own)
			m, ok := st.readMeta(k)
			base := ""
			if ok {
				st.meta[p] = m
				st.apiKeyOf(p, m)
				base = st.modBase(p, own)
			} else {
				st.meta[p] = &pkgMeta{ExportHash: "unknown:" + k}
				st.apiKeys[p], st.genKeys[p] = "unknown:"+k, "unknown:"+k
			}
			if !ok || needSrc != nil && needSrc(p, base) {
				all, _ := st.lastGenericDeps(p.PkgPath)
				for _, d := range all {
					force[d] = true
				}
			}
		}
		// A package loaded for its generic bodies needs the generic
		// packages those bodies instantiate.
		for queue := sortedKeys(force); len(queue) > 0; {
			d := queue[0]
			queue = queue[1:]
			_, g := st.lastGenericDeps(d)
			for _, e := range g {
				if !force[e] {
					force[e] = true
					queue = append(queue, e)
				}
			}
		}
		st.meta = map[*packages.Package]*pkgMeta{}
		tr := time.Now()
		if err := st.run(all); err != nil {
			return nil, nil, nil, nil, nil, err
		}
		if cfg.Log != nil {
			n := 0
			for _, r := range st.result {
				if r.FromSource {
					n++
				}
			}
			cfg.Log("types: %v (%d of %d packages from source)", time.Since(tr).Round(time.Millisecond), n, len(all))
		}
		if len(st.retry) == 0 || attempt == 3 {
			return roots, all, st.result, st.fset, cgoPkgs, nil
		}
		for _, d := range st.retry {
			force[d] = true
		}
	}
}

func copyOverlay(m map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	maps.Copy(out, m)
	return out
}

func (st *incrState) genDepsPath(path string) string {
	h := sha256.Sum256([]byte(path))
	return filepath.Join(st.cacheDir, "gdeps", hex.EncodeToString(h[:12])+".json")
}

type genericDepsFile struct{ All, G []string }

func (st *incrState) lastGenericDeps(path string) (all, g []string) {
	data, err := os.ReadFile(st.genDepsPath(path))
	if err != nil {
		return nil, nil
	}
	var f genericDepsFile
	if json.Unmarshal(data, &f) != nil {
		return nil, nil // a damaged entry: a miss
	}
	return f.All, f.G
}

func (st *incrState) saveGenericDeps(path string, all, g []string) {
	p := st.genDepsPath(path)
	// A cache, written whole or not at all: a failed write is a miss on
	// the next run.
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	data, _ := json.Marshal(genericDepsFile{all, g})
	_ = writeAtomic(p, data)
}

// run loads every package from source or export data. Packages are
// processed in parallel as soon as their imports are done; reading export
// data is serialized (it fills the shared import map).
func (st *incrState) run(all []*packages.Package) error {
	done := map[*packages.Package]chan struct{}{}
	for _, p := range all {
		done[p] = make(chan struct{})
	}
	var exportMu sync.Mutex
	var firstErr error
	var errMu sync.Mutex
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for _, p := range all {
		wg.Go(func() {
			defer close(done[p])
			for _, d := range p.Imports {
				if ch, ok := done[d]; ok {
					<-ch
				}
			}
			errMu.Lock()
			failed := firstErr != nil
			errMu.Unlock()
			if failed {
				return
			}
			sem <- struct{}{}
			err := st.loadOne(p, &exportMu)
			<-sem
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		})
	}
	wg.Wait()
	return firstErr
}

func (st *incrState) setResult(p *packages.Package, m *pkgMeta, r *IncrementalPackage) {
	st.mu.Lock()
	st.meta[p] = m
	st.result[p] = r
	st.mu.Unlock()
}

func (st *incrState) loadOne(p *packages.Package, exportMu *sync.Mutex) error {
	if p.PkgPath == "unsafe" {
		st.mu.Lock()
		st.imports["unsafe"] = types.Unsafe
		st.mu.Unlock()
		// As go/packages does: empty syntax, so SSA gives it an init body.
		p.Types, p.Syntax, p.TypesInfo, p.Fset = types.Unsafe, []*ast.File{}, new(types.Info), st.fset
		st.mu.Lock()
		st.srcKeys[p], st.apiKeys[p], st.genKeys[p] = "unsafe", "unsafe", "unsafe"
		st.mu.Unlock()
		st.setResult(p, &pkgMeta{}, &IncrementalPackage{Key: "unsafe", FromSource: true, ModBase: "unsafe"})
		return nil
	}
	own := filesSig(ownFiles(p))
	st.mu.Lock()
	k := st.keyOf(p, own)
	st.srcKeyOf(p, own)
	st.mu.Unlock()
	m, cached := st.readMeta(k)
	base := ""
	if cached {
		st.mu.Lock()
		base = st.modBase(p, own)
		st.mu.Unlock()
	}
	source := !cached || (st.needSrc != nil && st.needSrc(p, base)) || st.forceSrc[p.PkgPath]
	if !source {
		exportMu.Lock()
		st.mu.Lock()
		tpkg, err := st.importFromCache(p, k)
		if err == nil {
			st.imports[p.PkgPath] = tpkg
		}
		st.mu.Unlock()
		exportMu.Unlock()
		if err == nil {
			p.Types = tpkg
			p.Fset = st.fset
			p.TypesSizes = st.sizes
			st.mu.Lock()
			st.apiKeyOf(p, m)
			st.mu.Unlock()
			st.setResult(p, m, &IncrementalPackage{Key: k, Facts: m.Facts, ModBase: base})
			return nil
		}
	}
	files, err := st.parseFiles(p)
	if err != nil {
		return err
	}
	tpkg, info, err := st.typeCheck(p, files)
	if err != nil {
		return err
	}
	if len(p.EmbedPatterns) > 0 || len(asmJumps(p)) > 0 {
		// //go:embed: rewrite the variables, then parse and check again.
		p.Types, p.TypesInfo, p.Syntax, p.Fset = tpkg, info, files, st.fset
		ov, helpers, err := embedOverlays([]*packages.Package{p}, false)
		if err != nil {
			return err
		}
		if ov != nil {
			st.mu.Lock()
			maps.Copy(st.overlay, ov)
			st.mu.Unlock()
			p.CompiledGoFiles = append(p.CompiledGoFiles, helpers...)
			if files, err = st.parseFiles(p); err != nil {
				return err
			}
			if tpkg, info, err = st.typeCheck(p, files); err != nil {
				return err
			}
		}
	}
	st.mu.Lock()
	st.imports[p.PkgPath] = tpkg
	st.mu.Unlock()
	p.Types, p.TypesInfo, p.Syntax, p.Fset, p.TypesSizes = tpkg, info, files, st.fset, st.sizes
	nm := &pkgMeta{Generic: hasGenerics(tpkg), GenericAPI: genapi.Exposed(tpkg), Facts: factsOf(p, files)}
	reflectUses(p, info, nm.Facts)
	nm.GenericDeps, nm.GenericDepsG = genericDeps(tpkg, info, files)
	needCode := !cached || (st.needSrc != nil && st.needSrc(p, k))
	deps := nm.GenericDeps
	if !needCode {
		deps = nm.GenericDepsG // loaded for its generic bodies only
	}
	st.saveGenericDeps(p.PkgPath, nm.GenericDeps, nm.GenericDepsG)
	st.mu.Lock()
	for _, d := range deps {
		if r := st.byPath[d]; r != nil && st.result[r] != nil && !st.result[r].FromSource {
			// A dependency imported from export data has generic bodies
			// this package needs: load again with it from source.
			st.retry = append(st.retry, d)
		}
	}
	st.mu.Unlock()
	if cached && strings.HasPrefix(m.APIHash, apiHashPrefix) {
		// Same key, same export data; writing it again could change its
		// hash (export data is not byte-stable) and so every importer's key.
		nm.ExportHash, nm.APIHash = m.ExportHash, m.APIHash
	} else {
		// Writing export data only reads the type-checked package, so
		// packages write theirs in parallel.
		err = st.writeCache(k, tpkg, nm)
		if err != nil {
			return err
		}
	}
	st.mu.Lock()
	st.apiKeyOf(p, nm)
	base = st.modBase(p, own)
	st.mu.Unlock()
	st.setResult(p, nm, &IncrementalPackage{Key: k, FromSource: true, Facts: nm.Facts, ModBase: base})
	return nil
}

// genericDeps lists the other packages whose generic functions or types
// info instantiates: anywhere, and inside generic declarations only.
func genericDeps(tpkg *types.Package, info *types.Info, files []*ast.File) (all, inGeneric []string) {
	var spans []span
	for _, f := range files {
		for _, d := range f.Decls {
			if genericDecl(d) {
				spans = append(spans, span{d.Pos(), d.End()})
			}
		}
	}
	seen := map[string]bool{}
	seenG := map[string]bool{}
	for id := range info.Instances {
		obj := info.Uses[id]
		if obj == nil {
			obj = info.Defs[id]
		}
		if obj == nil || obj.Pkg() == nil || obj.Pkg() == tpkg {
			continue
		}
		path := obj.Pkg().Path()
		seen[path] = true
		if inSpan(spans, id.Pos()) {
			seenG[path] = true
		}
	}
	// Instances of other packages' generic types used without being named
	// here (the result of a call, a field): calling their methods compiles
	// the methods' instances into this package's module too.
	visited := map[types.Type]bool{}
	var walk func(t types.Type, pos token.Pos)
	walk = func(t types.Type, pos token.Pos) {
		if t == nil || visited[t] {
			return
		}
		visited[t] = true
		switch t := t.(type) {
		case *types.Named:
			if t.TypeArgs().Len() > 0 {
				if pkg := t.Obj().Pkg(); pkg != nil && pkg != tpkg {
					seen[pkg.Path()] = true
					if inSpan(spans, pos) {
						seenG[pkg.Path()] = true
					}
				}
				for a := range t.TypeArgs().Types() {
					walk(a, pos)
				}
			}
		case *types.Alias:
			walk(types.Unalias(t), pos)
		case *types.Pointer:
			walk(t.Elem(), pos)
		case *types.Slice:
			walk(t.Elem(), pos)
		case *types.Array:
			walk(t.Elem(), pos)
		case *types.Map:
			walk(t.Key(), pos)
			walk(t.Elem(), pos)
		case *types.Chan:
			walk(t.Elem(), pos)
		case *types.Signature:
			walk(t.Params(), pos)
			walk(t.Results(), pos)
		case *types.Tuple:
			for i := 0; i < t.Len(); i++ {
				walk(t.At(i).Type(), pos)
			}
		}
	}
	for e, tv := range info.Types {
		walk(tv.Type, e.Pos())
	}
	return sortedKeys(seen), sortedKeys(seenG)
}

// genericDecl reports whether d declares a generic function, a method of a
// generic type, or a generic type.
func genericDecl(d ast.Decl) bool {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Type.TypeParams != nil && len(d.Type.TypeParams.List) > 0 {
			return true
		}
		if d.Recv != nil && len(d.Recv.List) > 0 {
			switch t := unparen(d.Recv.List[0].Type).(type) {
			case *ast.StarExpr:
				switch unparen(t.X).(type) {
				case *ast.IndexExpr, *ast.IndexListExpr:
					return true
				}
			case *ast.IndexExpr, *ast.IndexListExpr:
				return true
			}
		}
	case *ast.GenDecl:
		for _, s := range d.Specs {
			if ts, ok := s.(*ast.TypeSpec); ok && ts.TypeParams != nil && len(ts.TypeParams.List) > 0 {
				return true
			}
		}
	}
	return false
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

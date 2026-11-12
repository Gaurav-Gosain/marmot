// Package loader loads Go packages against marmot's merged GOROOT and builds
// x/tools SSA for them with generic functions instantiated.
package loader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Config controls loading.
type Config struct {
	Dir      string   // working directory for pattern resolution
	Root     string   // marmot repository root (holds lib/src and build/)
	Tags     []string // extra build tags; "marmot" is always added
	GOOS     string
	GOARCH   string
	Patterns []string
	Log      func(format string, args ...any) // optional progress log
	// Tests loads the test variant of the (single) package pattern; the
	// root becomes the generated "pkg.test" main package (go list -test).
	Tests bool
	// AllErrors reports every load and type error: no caps per package or
	// overall, and no "could not import" follow-ons of a package that
	// already has errors.
	AllErrors bool
	// Incremental loads packages from cached export data where possible
	// (incremental.go); only packages that changed, or that NeedSource asks
	// for, are parsed, type checked and get SSA bodies.
	Incremental bool
	// NeedSource reports whether a package with the given key must be
	// compiled this build (its code is not cached).
	NeedSource func(p *packages.Package, key string) bool
	// CacheDir holds the export data cache (default build/cache/types).
	CacheDir string
	// Listed, if set, is called in incremental loads with every package
	// after go list, before any is loaded.
	Listed func(all []*packages.Package)
	// BeforeSSA runs after type checking on every package, dependencies
	// first; it may replace Syntax, Types and TypesInfo.
	BeforeSSA func(all []*packages.Package) error
	// AfterSSA runs after SSA is built, before code generation.
	AfterSSA func(prog *ssa.Program, pkgs []*ssa.Package) error
}

// Program is the loaded and SSA built program.
type Program struct {
	SSA      *ssa.Program
	Packages []*packages.Package // the root packages
	All      []*packages.Package // every package in dependency order
	SSAPkgs  map[*types.Package]*ssa.Package
	GOROOT   string // the merged GOROOT
	// Incremental holds, by package path, the key, whether the package was
	// loaded from source, and its facts (incremental loads only).
	Incremental map[string]*IncrementalPackage
	RealGOROOT  string
	Sizes       types.Sizes
}

// setup returns the go/packages configuration and patterns of a load.
func setup(cfg Config) (pcfg *packages.Config, patterns []string, goroot, realRoot string, err error) {
	realRoot, err = goEnv("GOROOT")
	if err != nil {
		return nil, nil, "", "", err
	}
	libSrc := filepath.Join(cfg.Root, "lib", "src")
	if v := os.Getenv("MARMOT_LIBSRC"); v != "" {
		libSrc = v
	}
	goroot, err = MergedGOROOT(realRoot, libSrc, filepath.Join(cfg.Root, "build"))
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("merged GOROOT: %v", err)
	}
	// purego selects the pure Go fallbacks of packages with assembly.
	tags := append([]string{"marmot", "purego", "math_big_pure_go"}, cfg.Tags...)
	env := append(os.Environ(),
		"GOROOT="+goroot,
		"GOCACHE="+GoCache(cfg.Root),
		"CGO_ENABLED=0",
		"GOOS="+cfg.GOOS,
		"GOARCH="+cfg.GOARCH,
		"GOFLAGS=-tags="+strings.Join(tags, ","),
		"GOTOOLCHAIN=local",
	)
	pcfg = &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule |
			packages.NeedEmbedFiles | packages.NeedEmbedPatterns,
		Dir:   cfg.Dir,
		Env:   env,
		Tests: cfg.Tests,
	}
	// The runtime is an implicit dependency of every program. go list cannot
	// mix .go files with package patterns, so for file lists an overlay file
	// that imports runtime joins the named files.
	patterns = append([]string{}, cfg.Patterns...)
	if len(patterns) > 0 && strings.HasSuffix(patterns[0], ".go") {
		for i, p := range patterns {
			if !filepath.IsAbs(p) {
				patterns[i] = filepath.Join(cfg.Dir, p)
			}
		}
		first := patterns[0]
		name, err := packageName(first)
		if err != nil {
			return nil, nil, "", "", err
		}
		extra := filepath.Join(filepath.Dir(first), "zz_marmot_runtime.go")
		pcfg.Overlay = map[string][]byte{extra: []byte("package " + name + "\n\nimport _ \"runtime\"\n")}
		patterns = append(patterns, extra)
	} else {
		patterns = append(patterns, "runtime")
	}
	return pcfg, patterns, goroot, realRoot, nil
}

// Load loads the packages and builds SSA for all of them.
func Load(cfg Config) (*Program, error) {
	pcfg, patterns, goroot, realRoot, err := setup(cfg)
	if err != nil {
		return nil, err
	}
	filePatterns := len(cfg.Patterns) > 0 && strings.HasSuffix(cfg.Patterns[0], ".go")
	t0 := time.Now()
	var pkgs []*packages.Package
	var incr map[*packages.Package]*IncrementalPackage
	if cfg.Incremental {
		roots, _, res, _, err := loadIncremental(cfg, pcfg, patterns, confSig(cfg, goroot, pcfg), cfg.NeedSource)
		if err != nil {
			return nil, err
		}
		pkgs, incr = roots, res
	} else {
		if pkgs, err = loadFull(pcfg, patterns, filePatterns); err != nil {
			return nil, err
		}
	}
	if cfg.Log != nil {
		cfg.Log("go list + type check: %v", time.Since(t0).Round(time.Millisecond))
	}
	userRuntime := false
	for _, p := range cfg.Patterns {
		if p == "runtime" {
			userRuntime = true
		}
	}
	if cfg.Tests {
		// Keep only the generated test main (and the runtime): the plain and
		// test variants of the package share a path and must not both be
		// compiled.
		var roots []*packages.Package
		for _, p := range pkgs {
			if strings.HasSuffix(p.ID, ".test") && p.Name == "main" || p.ID == "runtime" {
				roots = append(roots, p)
			}
		}
		pkgs = roots
	}
	var errs []string
	var all []*packages.Package
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		all = append(all, p)
		for _, e := range p.Errors {
			if cfg.AllErrors && followOn(p, e) {
				continue
			}
			errs = append(errs, e.Error())
		}
	})
	if len(errs) > 0 {
		if len(errs) > 20 && !cfg.AllErrors {
			errs = append(errs[:20], fmt.Sprintf("too many errors (%d)", len(errs)))
		}
		return nil, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %v", cfg.Patterns)
	}
	if cfg.BeforeSSA != nil {
		if err := cfg.BeforeSSA(all); err != nil {
			return nil, err
		}
	}
	t1 := time.Now()
	var prog *ssa.Program
	var ssaPkgs []*ssa.Package
	if cfg.Incremental {
		prog, ssaPkgs, err = buildSSAIncremental(pkgs, all)
	} else {
		prog, ssaPkgs, err = buildSSA(pkgs)
	}
	if err != nil {
		return nil, err
	}
	if cfg.AfterSSA != nil {
		if err := cfg.AfterSSA(prog, prog.AllPackages()); err != nil {
			return nil, err
		}
	}
	if cfg.Log != nil {
		cfg.Log("ssa: %v", time.Since(t1).Round(time.Millisecond))
	}
	if cfg.Tests {
		var roots []*packages.Package
		for _, p := range pkgs {
			if p.Name == "main" {
				roots = append(roots, p)
			}
		}
		if len(roots) == 0 {
			return nil, fmt.Errorf("no test files in %v", cfg.Patterns)
		}
		pkgs = roots
	} else if !userRuntime {
		var roots []*packages.Package
		for _, p := range pkgs {
			if p.PkgPath != "runtime" {
				roots = append(roots, p)
			}
		}
		pkgs = roots
	}
	m := map[*types.Package]*ssa.Package{}
	for _, p := range prog.AllPackages() {
		m[p.Pkg] = p
	}
	_ = ssaPkgs
	sizes := pkgs[0].TypesSizes
	if sizes == nil {
		sizes = types.SizesFor("gc", cfg.GOARCH)
	}
	lp := &Program{SSA: prog, Packages: pkgs, All: all, SSAPkgs: m, GOROOT: goroot, RealGOROOT: realRoot, Sizes: sizes}
	if incr != nil {
		lp.Incremental = map[string]*IncrementalPackage{}
		for p, ip := range incr {
			lp.Incremental[p.PkgPath] = ip
		}
	}
	return lp, nil
}

func goEnv(name string) (string, error) {
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %v", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// packageName reads the package clause of a Go file.
func packageName(file string) (string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.PackageClauseOnly)
	if err != nil {
		return "", err
	}
	return f.Name.Name, nil
}

// buildSSA builds SSA for all packages, turning builder panics into errors.
func buildSSA(pkgs []*packages.Package) (prog *ssa.Program, ssaPkgs []*ssa.Package, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error building SSA: %v", r)
		}
	}()
	prog, ssaPkgs = ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	// Debug references (expression -> value) for packages outside GOROOT:
	// codegen needs them to order variable reads after calls like gc does.
	user := map[string]bool{}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.Module != nil || p.PkgPath == "command-line-arguments" || !isStd(p.PkgPath) {
			user[p.PkgPath] = true
		}
	})
	for _, p := range prog.AllPackages() {
		if user[p.Pkg.Path()] {
			p.SetDebugMode(true)
		}
	}
	prog.Build()
	return prog, ssaPkgs, nil
}

// InputFiles lists every Go file the build of cfg.Patterns reads (all
// packages and dependencies, the runtime included), using the merged
// GOROOT. It is cheap compared with Load and is used for up-to-date checks.
func InputFiles(cfg Config) ([]string, string, error) {
	realRoot, err := goEnv("GOROOT")
	if err != nil {
		return nil, "", err
	}
	libSrc := filepath.Join(cfg.Root, "lib", "src")
	if v := os.Getenv("MARMOT_LIBSRC"); v != "" {
		libSrc = v
	}
	goroot, err := MergedGOROOT(realRoot, libSrc, filepath.Join(cfg.Root, "build"))
	if err != nil {
		return nil, "", err
	}
	tags := append([]string{"marmot", "purego", "math_big_pure_go"}, cfg.Tags...)
	env := append(os.Environ(), "GOROOT="+goroot, "GOCACHE="+GoCache(cfg.Root), "CGO_ENABLED=0", "GOOS="+cfg.GOOS,
		"GOARCH="+cfg.GOARCH, "GOFLAGS=-tags="+strings.Join(tags, ","), "GOTOOLCHAIN=local")
	list := func(patterns ...string) (string, error) {
		cmd := exec.Command("go", append([]string{"list", "-e", "-deps", "-f",
			"{{.Dir}}{{range .GoFiles}}\t{{.}}{{end}}{{range .EmbedFiles}}\t{{.}}{{end}}{{if .Error}}\tERROR{{end}}"}, patterns...)...)
		cmd.Dir = cfg.Dir
		cmd.Env = env
		out, err := cmd.Output()
		return string(out), err
	}
	patterns := append([]string{}, cfg.Patterns...)
	var outs []string
	if len(patterns) > 0 && strings.HasSuffix(patterns[0], ".go") {
		for i, p := range patterns {
			if !filepath.IsAbs(p) {
				patterns[i] = filepath.Join(cfg.Dir, p)
			}
		}
		a, err := list(patterns...)
		if err != nil {
			return nil, "", err
		}
		b, err := list("runtime")
		if err != nil {
			return nil, "", err
		}
		outs = []string{a, b}
	} else {
		a, err := list(append(patterns, "runtime")...)
		if err != nil {
			return nil, "", err
		}
		outs = []string{a}
	}
	var files []string
	for _, out := range outs {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			fs := strings.Split(line, "\t")
			if len(fs) == 0 || fs[0] == "" {
				continue
			}
			for _, f := range fs[1:] {
				if f == "ERROR" {
					return nil, "", fmt.Errorf("package errors")
				}
				files = append(files, filepath.Join(fs[0], f))
			}
		}
	}
	return files, goroot, nil
}

// isStd reports whether path looks like a standard library import path
// (no dot in the first element).
func isStd(path string) bool {
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return !strings.Contains(first, ".") && path != "command-line-arguments"
}

// GoCache is the Go build cache for the go commands marmot runs: inside
// marmot's own cache (build/cache, under its size cap and cleaned by marmot
// cache trim and clean), not the user's Go cache, which grew by gigabytes
// because every merged GOROOT path gets its own entries. MARMOT_GOCACHE
// overrides it.
func GoCache(root string) string {
	if d := os.Getenv("MARMOT_GOCACHE"); d != "" {
		return d
	}
	return filepath.Join(root, "build", "cache", "gocache")
}

// confSig hashes what every package key depends on besides its files.
func confSig(cfg Config, goroot string, pcfg *packages.Config) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s %s %s %q %v\n", goroot, cfg.GOOS, cfg.GOARCH, cfg.Tags, cfg.Tests)
	if exe, err := os.Executable(); err == nil {
		if st, err := os.Stat(exe); err == nil {
			fmt.Fprintf(h, "%s %d %d\n", exe, st.Size(), st.ModTime().UnixNano())
		}
	}
	var names []string
	for n := range pcfg.Overlay {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := sha256.Sum256(pcfg.Overlay[n])
		fmt.Fprintf(h, "%s %x\n", n, s[:8])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// loadFull loads and type checks every package with go/packages.
func loadFull(pcfg *packages.Config, patterns []string, filePatterns bool) ([]*packages.Package, error) {
	pkgs, err := packages.Load(pcfg, patterns...)
	if err != nil {
		return nil, err
	}
	// //go:embed: give the embed variables initializers and load again.
	var embedAll []*packages.Package
	packages.Visit(pkgs, nil, func(p *packages.Package) { embedAll = append(embedAll, p) })
	over, _, err := embedOverlays(embedAll, true)
	if err != nil {
		return nil, err
	}
	if over != nil {
		// The edited sources go to the parser, not to the go command (no
		// overlays beneath GOMODCACHE); the file list stays the same.
		parse := pcfg.ParseFile
		pcfg.ParseFile = func(fset *token.FileSet, name string, src []byte) (*ast.File, error) {
			if data, ok := over[name]; ok {
				src = data
			}
			if parse != nil {
				return parse(fset, name, src)
			}
			return parser.ParseFile(fset, name, src, parser.AllErrors|parser.ParseComments|parser.SkipObjectResolution)
		}
		if pkgs, err = packages.Load(pcfg, patterns...); err != nil {
			return nil, err
		}
	}
	return pkgs, nil
}

// buildSSAIncremental builds SSA bodies for the packages loaded from source;
// packages imported from export data get members without bodies.
func buildSSAIncremental(roots, all []*packages.Package) (prog *ssa.Program, ssaPkgs []*ssa.Package, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error building SSA: %v", r)
		}
	}()
	var fset *token.FileSet
	for _, p := range all {
		if p.Fset != nil {
			fset = p.Fset
			break
		}
	}
	prog = ssa.NewProgram(fset, ssa.InstantiateGenerics)
	byPkg := map[*packages.Package]*ssa.Package{}
	created := map[*types.Package]bool{}
	for _, p := range all {
		if p.Types == nil || created[p.Types] {
			continue
		}
		created[p.Types] = true
		var sp *ssa.Package
		if p.Syntax != nil {
			sp = prog.CreatePackage(p.Types, p.Syntax, p.TypesInfo, false)
			if p.Module != nil || p.PkgPath == "command-line-arguments" || !isStd(p.PkgPath) {
				sp.SetDebugMode(true)
			}
		} else {
			sp = prog.CreatePackage(p.Types, nil, nil, true)
		}
		byPkg[p] = sp
	}
	prog.Build()
	for _, r := range roots {
		ssaPkgs = append(ssaPkgs, byPkg[r])
	}
	return prog, ssaPkgs, nil
}

// followOn reports whether e is a "could not import" type error of p for an
// import that has errors of its own, which are reported there.
func followOn(p *packages.Package, e packages.Error) bool {
	if e.Kind != packages.TypeError || !strings.Contains(e.Msg, "could not import ") {
		return false
	}
	for path, q := range p.Imports {
		if len(q.Errors) > 0 && strings.Contains(e.Msg, "could not import "+path+" ") {
			return true
		}
	}
	return false
}

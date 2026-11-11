// Package loader loads Go packages against marmot's merged GOROOT and builds
// x/tools SSA for them with generic functions instantiated.
package loader

import (
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
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
	// BeforeSSA runs after type checking on every package, dependencies
	// first; it may replace Syntax, Types and TypesInfo (the spec component).
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
	Sizes    types.Sizes
}

// Load loads the packages and builds SSA for all of them.
func Load(cfg Config) (*Program, error) {
	realRoot, err := goEnv("GOROOT")
	if err != nil {
		return nil, err
	}
	libSrc := filepath.Join(cfg.Root, "lib", "src")
	if v := os.Getenv("MARMOT_LIBSRC"); v != "" {
		libSrc = v
	}
	goroot, err := MergedGOROOT(realRoot, libSrc, filepath.Join(cfg.Root, "build"))
	if err != nil {
		return nil, fmt.Errorf("merged GOROOT: %v", err)
	}
	// purego selects the pure Go fallbacks of packages with assembly.
	tags := append([]string{"marmot", "purego", "math_big_pure_go"}, cfg.Tags...)
	env := append(os.Environ(),
		"GOROOT="+goroot,
		"CGO_ENABLED=0",
		"GOOS="+cfg.GOOS,
		"GOARCH="+cfg.GOARCH,
		"GOFLAGS=-tags="+strings.Join(tags, ","),
		"GOTOOLCHAIN=local",
	)
	pcfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule,
		Dir: cfg.Dir,
		Env: env,
	}
	// The runtime is an implicit dependency of every program. go list cannot
	// mix .go files with package patterns, so for file lists an overlay file
	// that imports runtime joins the named files.
	patterns := append([]string{}, cfg.Patterns...)
	if len(patterns) > 0 && strings.HasSuffix(patterns[0], ".go") {
		for i, p := range patterns {
			if !filepath.IsAbs(p) {
				patterns[i] = filepath.Join(cfg.Dir, p)
			}
		}
		first := patterns[0]
		name, err := packageName(first)
		if err != nil {
			return nil, err
		}
		extra := filepath.Join(filepath.Dir(first), "zz_marmot_runtime.go")
		pcfg.Overlay = map[string][]byte{extra: []byte("package " + name + "\n\nimport _ \"runtime\"\n")}
		patterns = append(patterns, extra)
	} else {
		patterns = append(patterns, "runtime")
	}
	t0 := time.Now()
	pkgs, err := packages.Load(pcfg, patterns...)
	if err != nil {
		return nil, err
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
	var errs []string
	var all []*packages.Package
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		all = append(all, p)
		for _, e := range p.Errors {
			errs = append(errs, e.Error())
		}
	})
	if len(errs) > 0 {
		if len(errs) > 20 {
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
	prog, ssaPkgs, err := buildSSA(pkgs)
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
	if !userRuntime {
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
	return &Program{SSA: prog, Packages: pkgs, All: all, SSAPkgs: m, GOROOT: goroot, Sizes: sizes}, nil
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
	env := append(os.Environ(), "GOROOT="+goroot, "CGO_ENABLED=0", "GOOS="+cfg.GOOS,
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

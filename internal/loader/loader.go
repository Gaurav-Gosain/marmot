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
	tags := append([]string{"marmot"}, cfg.Tags...)
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
	pkgs, err := packages.Load(pcfg, patterns...)
	if err != nil {
		return nil, err
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
	prog, ssaPkgs := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	if !userRuntime {
		var roots []*packages.Package
		for _, p := range pkgs {
			if p.PkgPath != "runtime" {
				roots = append(roots, p)
			}
		}
		pkgs = roots
	}
	prog.Build()
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

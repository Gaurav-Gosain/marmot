package loader

import (
	"bytes"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/cgo"
	"golang.org/x/tools/go/packages"
)

// cgo packages. go list runs with CGO_ENABLED=0, so std never changes. A
// program whose module packages import "C" (their cgo files show up as
// ignored files) gets those packages again from go/build with cgo on, as gc
// would build them: their files, the Go and C that internal/cgo generates
// for the cgo files, and their imports. Packages that are imported only by
// the new files are listed in a second go list run. Programs without
// import "C" are loaded exactly as before.

// usesCgo reports whether a module package of all has a file importing "C".
// A file counts only if its build constraints hold with cgo on: x/sys has
// files for other systems, and files with //go:build ignore, that import
// "C" and are ignored for those reasons.
func usesCgo(all []*packages.Package, env []string) bool {
	goroot := envGOROOT(env)
	ctxt := cgoMatchContext(env)
	fset := token.NewFileSet()
	for _, p := range all {
		if isStdPkg(p, goroot) {
			continue
		}
		for _, f := range p.IgnoredFiles {
			if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
				continue
			}
			if ok, err := ctxt.MatchFile(filepath.Dir(f), filepath.Base(f)); err != nil || !ok {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil || !bytes.Contains(src, []byte(`"C"`)) {
				continue
			}
			af, err := parser.ParseFile(fset, f, src, parser.ImportsOnly)
			if err != nil {
				continue
			}
			for _, imp := range af.Imports {
				if imp.Path.Value == `"C"` {
					return true
				}
			}
		}
	}
	return false
}

// cgoMatchContext is go/build's context for the GOOS, GOARCH and tags of a
// go list environment, with cgo on.
func cgoMatchContext(env []string) build.Context {
	ctxt := build.Default
	ctxt.CgoEnabled = true
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		switch k {
		case "GOOS":
			ctxt.GOOS = v
		case "GOARCH":
			ctxt.GOARCH = v
		case "GOFLAGS":
			for _, f := range strings.Fields(v) {
				if t, ok := strings.CutPrefix(f, "-tags="); ok {
					ctxt.BuildTags = strings.Split(t, ",")
				}
			}
		}
	}
	return ctxt
}

func pkgDir(p *packages.Package) string {
	for _, fs := range [][]string{p.GoFiles, p.IgnoredFiles, p.OtherFiles} {
		if len(fs) > 0 {
			return filepath.Dir(fs[0])
		}
	}
	return ""
}

func isStdPkg(p *packages.Package, goroot string) bool {
	if p.Module != nil {
		return false
	}
	d := pkgDir(p)
	return d == "" || strings.HasPrefix(d, goroot+string(filepath.Separator))
}

// overlayName is the file setup adds to a list of files to import the
// runtime.
const overlayName = "zz_marmot_runtime.go"

// cgoRelist lists the patterns again with extra import paths: as more
// patterns, or for a list of files as imports of the runtime overlay.
func cgoRelist(light *packages.Config, patterns []string, cacheDir string) func([]string) ([]*packages.Package, error) {
	return func(extra []string) ([]*packages.Package, error) {
		if len(patterns) > 0 && strings.HasSuffix(patterns[0], ".go") {
			c := *light
			c.Overlay = map[string][]byte{}
			for k, v := range light.Overlay {
				if filepath.Base(k) == overlayName {
					v = append(append([]byte{}, v...), '\n')
					for _, e := range extra {
						v = append(v, fmt.Sprintf("import _ %q\n", e)...)
					}
				}
				c.Overlay[k] = v
			}
			roots, _, err := loadList(&c, patterns, cacheDir)
			return roots, err
		}
		roots, _, err := loadList(light, append(append([]string{}, patterns...), extra...), cacheDir)
		return roots, err
	}
}

// cgoPackages applies applyCgo to an incremental load.
// usesC is loadList's answer, so a program without import "C" costs
// nothing here.
func cgoPackages(cfg Config, pcfg *packages.Config, roots []*packages.Package, usesC bool,
	relist func(extra []string) ([]*packages.Package, error)) ([]*packages.Package, map[string]*cgo.Package, error) {
	if !usesC {
		return roots, nil, nil
	}
	roots, _, out, err := applyCgo(cfg, envGOROOT(pcfg.Env), roots, relist)
	return roots, out, err
}

// applyCgo rewrites the module packages of a cgo program (one that
// usesCgo). relist lists the
// patterns plus extra import paths again. It returns the roots and every
// package (dependencies first), and the cgo packages by package path.
func applyCgo(cfg Config, goroot string, roots []*packages.Package,
	relist func(extra []string) ([]*packages.Package, error)) ([]*packages.Package, []*packages.Package, map[string]*cgo.Package, error) {
	var all []*packages.Package
	packages.Visit(roots, nil, func(p *packages.Package) { all = append(all, p) })
	realRoot, err := goEnv("GOROOT")
	if err != nil {
		return nil, nil, nil, err
	}
	cc := cfg.CC
	if cc == "" {
		cc = "clang"
	}
	ccfg := cgo.Config{CC: cc, CCFlags: cfg.CCFlags, GOOS: cfg.GOOS, GOARCH: cfg.GOARCH,
		Tags:       append([]string{"marmot", "purego", "math_big_pure_go"}, cfg.Tags...),
		RealGOROOT: realRoot, CacheDir: filepath.Join(cfg.Root, "build", "cache", "cgo")}
	type change struct {
		p       *packages.Package
		files   []string // CompiledGoFiles
		imports []string
		cgo     *cgo.Package
	}
	var changes []change
	for _, p := range all {
		if isStdPkg(p, goroot) {
			continue
		}
		// With tests: the generated test main and the external test
		// package (in the same directory) have no cgo files of their own.
		if strings.HasSuffix(p.ID, ".test") || strings.HasSuffix(p.Name, "_test") {
			continue
		}
		dir := pkgDir(p)
		if dir == "" {
			continue
		}
		// The test variant of the package (ID "p [p.test]") also has the
		// package's _test.go files.
		var testFiles []string
		if strings.Contains(p.ID, " [") {
			for _, f := range p.GoFiles {
				if strings.HasSuffix(f, "_test.go") {
					testFiles = append(testFiles, f)
				}
			}
		}
		// go build file.go: the package is the named files, plus the
		// overlay that imports the runtime.
		var named, overlay []string
		if p.ID == "command-line-arguments" {
			for _, f := range append(append([]string{}, p.GoFiles...), p.IgnoredFiles...) {
				if filepath.Base(f) == overlayName {
					overlay = append(overlay, f)
				} else if strings.HasSuffix(f, ".go") {
					named = append(named, f)
				}
			}
		}
		cp, err := cgo.Prepare(ccfg, dir, p.PkgPath, named)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %v", p.PkgPath, err)
		}
		var files, imports []string
		if cp != nil {
			files, imports = cp.GoFiles, cp.Imports
		} else {
			// No cgo files, but the cgo build tag may still pick other files.
			bp, err := ccfg.Import(dir, named)
			if err != nil {
				continue
			}
			for _, f := range bp.GoFiles {
				files = append(files, filepath.Join(dir, f))
			}
			imports = bp.Imports
			if sameFiles(append(append(append([]string{}, files...), overlay...), testFiles...), p.GoFiles) {
				continue
			}
		}
		files = append(files, testFiles...)
		if len(overlay) > 0 {
			files = append(files, overlay...)
			imports = append(imports, "runtime")
		}
		changes = append(changes, change{p, files, imports, cp})
	}
	byPath := map[string]*packages.Package{}
	for _, p := range all {
		byPath[p.PkgPath] = p
	}
	var missing []string
	for _, c := range changes {
		for _, i := range c.imports {
			if byPath[i] == nil && i != "C" && !contains(missing, i) {
				missing = append(missing, i)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		nroots, err := relist(missing)
		if err != nil {
			return nil, nil, nil, err
		}
		var keep []*packages.Package
		for _, r := range nroots {
			if !contains(missing, r.PkgPath) {
				keep = append(keep, r)
			}
		}
		byID := map[string]*packages.Package{}
		packages.Visit(nroots, nil, func(p *packages.Package) {
			byPath[p.PkgPath] = p
			byID[p.ID] = p
		})
		for i := range changes {
			changes[i].p = byID[changes[i].p.ID]
		}
		roots = keep
	}
	out := map[string]*cgo.Package{}
	for _, c := range changes {
		p := c.p
		p.GoFiles = c.files
		p.CompiledGoFiles = c.files
		p.IgnoredFiles = nil
		// Keep go list's imports (they name the right test variants) and
		// add those of the new files.
		imps := map[string]*packages.Package{}
		for k, v := range p.Imports {
			imps[k] = v
		}
		for _, i := range c.imports {
			if imps[i] != nil || i == "C" {
				continue
			}
			if d := byPath[i]; d != nil {
				imps[i] = d
			} else {
				return nil, nil, nil, fmt.Errorf("%s: import %q not found", p.PkgPath, i)
			}
		}
		p.Imports = imps
		// go list's view without cgo ("build constraints exclude all Go
		// files") no longer holds.
		p.Errors = nil
		if c.cgo != nil {
			out[p.PkgPath] = c.cgo
		}
	}
	all = nil
	packages.Visit(roots, nil, func(p *packages.Package) { all = append(all, p) })
	return roots, all, out, nil
}

func sameFiles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, f := range a {
		m[f] = true
	}
	for _, f := range b {
		if !m[f] {
			return false
		}
	}
	return true
}

func contains(s []string, x string) bool {
	return slices.Contains(s, x)
}

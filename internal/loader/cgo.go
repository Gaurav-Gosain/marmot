package loader

import (
	"fmt"
	"go/parser"
	"go/token"
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
func usesCgo(all []*packages.Package, goroot string) bool {
	fset := token.NewFileSet()
	for _, p := range all {
		if isStdPkg(p, goroot) {
			continue
		}
		for _, f := range p.IgnoredFiles {
			if !strings.HasSuffix(f, ".go") {
				continue
			}
			af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
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
			return loadList(&c, patterns, cacheDir)
		}
		return loadList(light, append(append([]string{}, patterns...), extra...), cacheDir)
	}
}

// cgoPackages applies applyCgo to an incremental load.
func cgoPackages(cfg Config, pcfg *packages.Config, roots []*packages.Package,
	relist func(extra []string) ([]*packages.Package, error)) ([]*packages.Package, map[string]*cgo.Package, error) {
	goroot := ""
	for _, e := range pcfg.Env {
		if after, ok := strings.CutPrefix(e, "GOROOT="); ok {
			goroot = after
		}
	}
	roots, _, out, err := applyCgo(cfg, goroot, roots, relist)
	return roots, out, err
}

// applyCgo rewrites the module packages of a cgo program. relist lists the
// patterns plus extra import paths again. It returns the roots and every
// package (dependencies first), and the cgo packages by package path.
func applyCgo(cfg Config, goroot string, roots []*packages.Package,
	relist func(extra []string) ([]*packages.Package, error)) ([]*packages.Package, []*packages.Package, map[string]*cgo.Package, error) {
	var all []*packages.Package
	packages.Visit(roots, nil, func(p *packages.Package) { all = append(all, p) })
	if !usesCgo(all, goroot) {
		return roots, all, nil, nil
	}
	if cfg.Tests {
		return nil, nil, nil, fmt.Errorf("marmot test does not support cgo packages yet")
	}
	realRoot, err := goEnv("GOROOT")
	if err != nil {
		return nil, nil, nil, err
	}
	cc := cfg.CC
	if cc == "" {
		cc = "clang"
	}
	ccfg := cgo.Config{CC: cc, GOOS: cfg.GOOS, GOARCH: cfg.GOARCH,
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
		dir := pkgDir(p)
		if dir == "" {
			continue
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
			if sameFiles(append(append([]string{}, files...), overlay...), p.GoFiles) {
				continue
			}
		}
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
		packages.Visit(nroots, nil, func(p *packages.Package) { byPath[p.PkgPath] = p })
		for i := range changes {
			changes[i].p = byPath[changes[i].p.PkgPath]
		}
		roots = keep
	}
	out := map[string]*cgo.Package{}
	for _, c := range changes {
		p := c.p
		p.GoFiles = c.files
		p.CompiledGoFiles = c.files
		p.IgnoredFiles = nil
		p.Imports = map[string]*packages.Package{}
		for _, i := range c.imports {
			if d := byPath[i]; d != nil {
				p.Imports[i] = d
			} else if i != "C" {
				return nil, nil, nil, fmt.Errorf("%s: import %q not found", p.PkgPath, i)
			}
		}
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

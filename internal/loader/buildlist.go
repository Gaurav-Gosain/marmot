package loader

import (
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// Listing packages without the go command, for the browser playground's front end (wasip1,
// where there is no go command to run) and for MARMOT_GOLIST=gobuild: go/build reads the
// merged GOROOT and the program's own directory. Only the standard library and the main
// module's own packages can be imported; no module cache, no cgo.

// noGoCommand reports whether packages are listed with go/build instead of go list.
func noGoCommand() bool {
	return runtime.GOOS == "wasip1" || runtime.GOOS == "js" || os.Getenv("MARMOT_GOLIST") == "gobuild"
}

// buildList is packages.Load of the light config through go/build: roots and their
// dependencies with names, files and imports.
func buildList(light *packages.Config, patterns []string) ([]*packages.Package, error) {
	env := map[string]string{}
	for _, e := range light.Env {
		if k, v, ok := strings.Cut(e, "="); ok {
			env[k] = v
		}
	}
	ctx := build.Default
	ctx.GOROOT = env["GOROOT"]
	ctx.GOPATH = ""
	ctx.GOOS, ctx.GOARCH = env["GOOS"], env["GOARCH"]
	ctx.CgoEnabled = false
	ctx.ToolTags = toolTags(ctx.ToolTags, ctx.GOOS, ctx.GOARCH)
	ctx.Compiler = "gc"
	if tags, ok := strings.CutPrefix(env["GOFLAGS"], "-tags="); ok {
		ctx.BuildTags = strings.Split(tags, ",")
	}
	ctx.Dir = light.Dir
	// The main module: its path and Go version from go.mod in Dir (or above), if any.
	modDir, modPath, goVersion := findModule(light.Dir)
	l := &lister{ctx: &ctx, modDir: modDir, modPath: modPath, goVersion: goVersion, byPath: map[string]*packages.Package{}, overlay: light.Overlay}
	var roots []*packages.Package
	var files []string
	for _, pat := range patterns {
		if strings.HasSuffix(pat, ".go") {
			files = append(files, pat)
			continue
		}
		p := l.load(pat, light.Dir)
		roots = append(roots, p)
	}
	if len(files) > 0 {
		roots = append([]*packages.Package{l.loadFiles(files)}, roots...)
	}
	return roots, nil
}

type lister struct {
	ctx             *build.Context
	modDir, modPath string
	goVersion       string
	byPath          map[string]*packages.Package
	overlay         map[string][]byte
}

func findModule(dir string) (modDir, modPath, goVersion string) {
	for d := dir; ; d = filepath.Dir(d) {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			f, err := modfile.ParseLax("go.mod", data, nil)
			if err == nil && f.Module != nil {
				if f.Go != nil {
					goVersion = f.Go.Version
				}
				return d, f.Module.Mod.Path, goVersion
			}
			return d, "", ""
		}
		if filepath.Dir(d) == d {
			return "", "", ""
		}
	}
}

// load lists the package of an import path (or "." and relative paths: directories).
func (l *lister) load(path, srcDir string) *packages.Package {
	if path == "." || strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
		dir := filepath.Join(srcDir, path)
		ip := l.dirPath(dir)
		if p := l.byPath[ip]; p != nil {
			return p
		}
		bp, err := l.ctx.ImportDir(dir, 0)
		return l.finish(ip, bp, err)
	}
	// Inside the main module: its own directory.
	if l.modPath != "" && (path == l.modPath || strings.HasPrefix(path, l.modPath+"/")) {
		if p := l.byPath[path]; p != nil {
			return p
		}
		dir := filepath.Join(l.modDir, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, l.modPath), "/")))
		bp, err := l.ctx.ImportDir(dir, 0)
		return l.finish(path, bp, err)
	}
	// The standard library, vendored packages of std first.
	bp, err := l.ctx.Import(path, srcDir, 0)
	ip := path
	if err == nil {
		ip = bp.ImportPath
	} else if !strings.Contains(strings.Split(path, "/")[0], ".") {
		err = fmt.Errorf("package %s is not in std (%s)", path, filepath.Join(l.ctx.GOROOT, "src", path))
	} else {
		err = fmt.Errorf("package %s: only the standard library and the program's own packages can be imported here", path)
	}
	if p := l.byPath[ip]; p != nil {
		return p
	}
	return l.finish(ip, bp, err)
}

// dirPath is the import path of a directory of the main module.
func (l *lister) dirPath(dir string) string {
	if l.modDir != "" {
		if rel, err := filepath.Rel(l.modDir, dir); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return l.modPath
			}
			return l.modPath + "/" + filepath.ToSlash(rel)
		}
	}
	return "command-line-arguments"
}

// loadFiles lists the package of named .go files (the overlay may hold some of them).
func (l *lister) loadFiles(files []string) *packages.Package {
	p := &packages.Package{ID: "command-line-arguments", PkgPath: "command-line-arguments", Imports: map[string]*packages.Package{}}
	l.byPath[p.PkgPath] = p
	imports := map[string]bool{}
	for _, f := range files {
		src, ok := l.overlay[f]
		if !ok {
			b, err := os.ReadFile(f)
			if err != nil {
				p.Errors = append(p.Errors, packages.Error{Msg: err.Error(), Kind: packages.ListError})
				continue
			}
			src = b
		}
		name, imps, err := fileHead(f, src)
		if err != nil {
			p.Errors = append(p.Errors, packages.Error{Msg: err.Error(), Kind: packages.ListError})
			continue
		}
		p.Name = name
		p.GoFiles = append(p.GoFiles, f)
		for _, i := range imps {
			imports[i] = true
		}
	}
	p.CompiledGoFiles = p.GoFiles
	l.linkImports(p, sortedSet(imports), filepath.Dir(files[0]))
	l.setModule(p)
	return p
}

func (l *lister) finish(ip string, bp *build.Package, err error) *packages.Package {
	p := &packages.Package{ID: ip, PkgPath: ip, Imports: map[string]*packages.Package{}}
	l.byPath[ip] = p
	if bp != nil {
		p.Name = bp.Name
		for _, f := range bp.GoFiles {
			p.GoFiles = append(p.GoFiles, filepath.Join(bp.Dir, f))
		}
		p.CompiledGoFiles = p.GoFiles
		for _, f := range append(append([]string(nil), bp.SFiles...), bp.HFiles...) {
			p.OtherFiles = append(p.OtherFiles, filepath.Join(bp.Dir, f))
		}
		for _, f := range bp.IgnoredGoFiles {
			p.IgnoredFiles = append(p.IgnoredFiles, filepath.Join(bp.Dir, f))
		}
		p.EmbedPatterns = bp.EmbedPatterns
		if ip == "unsafe" {
			p.GoFiles, p.CompiledGoFiles = nil, nil
		}
	}
	if err != nil {
		if _, ok := err.(*build.NoGoError); ok || bp == nil || len(bp.GoFiles) == 0 {
			p.Errors = append(p.Errors, packages.Error{Msg: err.Error(), Kind: packages.ListError})
		}
	}
	if bp != nil {
		l.linkImports(p, bp.Imports, bp.Dir)
	}
	l.setModule(p)
	return p
}

func (l *lister) linkImports(p *packages.Package, imports []string, dir string) {
	for _, i := range imports {
		if i == "C" {
			p.Errors = append(p.Errors, packages.Error{Msg: "cgo is not available here", Kind: packages.ListError})
			continue
		}
		p.Imports[i] = l.load(i, dir)
	}
}

// setModule gives the main module's packages its Go version (the type checker's language
// version); std packages get none, as from go list.
func (l *lister) setModule(p *packages.Package) {
	if l.modPath != "" && (p.PkgPath == l.modPath || strings.HasPrefix(p.PkgPath, l.modPath+"/") || p.PkgPath == "command-line-arguments") {
		p.Module = &packages.Module{Path: l.modPath, Dir: l.modDir, GoVersion: l.goVersion, Main: true}
	}
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fileHead is the package name and imports of a Go file.
func fileHead(name string, src []byte) (string, []string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		return "", nil, err
	}
	var imps []string
	for _, s := range f.Imports {
		if p, err := strconv.Unquote(s.Path.Value); err == nil {
			imps = append(imps, p)
		}
	}
	return f.Name.Name, imps, nil
}

// toolTags turns the running toolchain's tool tags into the target's, as go list computes
// them (internal/buildcfg's baseline): the register ABI experiments only where the target
// has them, DWARF 5 except on darwin, ios and aix, the target's feature tags, and the
// toolchain's other experiments as they are.
func toolTags(host []string, goos, goarch string) []string {
	var out []string
	for _, t := range host {
		switch {
		case strings.HasPrefix(t, "goexperiment.regabi"), t == "goexperiment.dwarf5":
		case !strings.HasPrefix(t, "goexperiment."): // the host's architecture feature tags
		default:
			out = append(out, t)
		}
	}
	switch goarch {
	case "amd64", "arm64", "loong64", "ppc64", "ppc64le", "riscv64", "s390x":
		out = append(out, "goexperiment.regabiwrappers", "goexperiment.regabiargs")
	}
	if goos != "darwin" && goos != "ios" && goos != "aix" {
		out = append(out, "goexperiment.dwarf5")
	}
	switch goarch {
	case "wasm":
		out = append(out, "wasm.satconv", "wasm.signext")
	case "amd64":
		out = append(out, "amd64.v1")
	case "arm64":
		out = append(out, "arm64.v8.0")
	}
	return out
}

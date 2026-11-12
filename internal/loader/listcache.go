package loader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// A cache of go list results. The package graph only changes when a file
// is added, removed or edited (imports and build constraints live in the
// files), when go.mod, go.sum or go.work change, or when the environment
// or patterns change. The cached graph is reused while all of those have
// the same size and modification time.

type listPkg struct {
	ID, Name, PkgPath string
	GoFiles           []string
	CompiledGoFiles   []string
	OtherFiles        []string `json:",omitempty"`
	EmbedFiles        []string `json:",omitempty"`
	EmbedPatterns     []string `json:",omitempty"`
	Imports           map[string]string
	Module            *packages.Module `json:",omitempty"`
}

type listCache struct {
	Sig    string
	Roots  []string
	Pkgs   []listPkg
	Dirs   map[string]string // directory -> size/mtime signature
	Files  map[string]string // file -> size/mtime signature
	Extras map[string]string // go.mod, go.sum, go.work
	Heads  map[string]string // file -> hash of its package clause, imports and build constraints
	Names  map[string]string // directory -> its .go file names
}

// headHash hashes what go list reads from a Go file: build constraints,
// the package clause and the imports.
func headHash(path string) string {
	if !strings.HasSuffix(path, ".go") {
		return statSig(path)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return "error"
	}
	h := sha256.New()
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") || strings.HasPrefix(c.Text, "//go:embed") {
				fmt.Fprintln(h, c.Text)
			}
		}
	}
	fmt.Fprintln(h, f.Name.Name)
	for _, im := range f.Imports {
		fmt.Fprintln(h, im.Path.Value)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// goNames lists the .go files of dir.
func goNames(dir string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "missing"
	}
	var names []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".go") {
			names = append(names, e.Name())
		}
	}
	return strings.Join(names, "/")
}

func statSig(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	return fmt.Sprintf("%d %d", st.Size(), st.ModTime().UnixNano())
}

func listSig(pcfg *packages.Config, patterns []string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%q\n%v\n", pcfg.Dir, patterns, pcfg.Tests)
	env := append([]string(nil), pcfg.Env...)
	sort.Strings(env)
	for _, e := range env {
		if strings.HasPrefix(e, "GO") || strings.HasPrefix(e, "CGO") || strings.HasPrefix(e, "MARMOT") {
			fmt.Fprintln(h, e)
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

func listCachePath(cacheDir, sig string) string {
	return filepath.Join(cacheDir, "list-"+sig[:24]+".json")
}

// modFiles returns go.mod, go.sum and go.work of the module containing dir.
func modFiles(dir string) []string {
	var out []string
	for d := dir; ; d = filepath.Dir(d) {
		for _, n := range []string{"go.mod", "go.sum", "go.work", "go.work.sum"} {
			if _, err := os.Stat(filepath.Join(d, n)); err == nil {
				out = append(out, filepath.Join(d, n))
			}
		}
		if len(out) > 0 || filepath.Dir(d) == d {
			return out
		}
	}
}

// loadList returns the package graph for patterns, from the cache when it
// is still valid, else from go list (and then saves it).
func loadList(light *packages.Config, patterns []string, cacheDir string) ([]*packages.Package, error) {
	sig := listSig(light, patterns)
	path := listCachePath(cacheDir, sig)
	if roots, ok := readListCache(path, sig, light.Dir); ok {
		return roots, nil
	}
	roots, err := packages.Load(light, patterns...)
	if err != nil {
		return nil, err
	}
	writeListCache(path, sig, roots, light.Dir)
	return roots, nil
}

func readListCache(path, sig, dir string) ([]*packages.Package, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var c listCache
	if json.Unmarshal(data, &c) != nil || c.Sig != sig {
		return nil, false
	}
	for f, s := range c.Extras {
		if statSig(f) != s {
			return nil, false
		}
	}
	if len(modFiles(dir)) != len(c.Extras) {
		return nil, false
	}
	for d, s := range c.Dirs {
		if statSig(d) != s && goNames(d) != c.Names[d] {
			return nil, false // a file was added or removed
		}
	}
	for f, s := range c.Files {
		if statSig(f) != s && headHash(f) != c.Heads[f] {
			return nil, false // imports or build constraints changed
		}
	}
	byID := map[string]*packages.Package{}
	for i := range c.Pkgs {
		lp := &c.Pkgs[i]
		byID[lp.PkgPath+"\x00"+lp.ID] = &packages.Package{
			ID: lp.ID, Name: lp.Name, PkgPath: lp.PkgPath,
			GoFiles: lp.GoFiles, CompiledGoFiles: lp.CompiledGoFiles, OtherFiles: lp.OtherFiles,
			EmbedFiles: lp.EmbedFiles, EmbedPatterns: lp.EmbedPatterns, Module: lp.Module,
		}
	}
	idOf := map[string]*packages.Package{}
	for _, p := range byID {
		idOf[p.ID] = p
	}
	for i := range c.Pkgs {
		lp := &c.Pkgs[i]
		p := idOf[lp.ID]
		p.Imports = map[string]*packages.Package{}
		for path, id := range lp.Imports {
			d := idOf[id]
			if d == nil {
				return nil, false
			}
			p.Imports[path] = d
		}
	}
	var roots []*packages.Package
	for _, id := range c.Roots {
		r := idOf[id]
		if r == nil {
			return nil, false
		}
		roots = append(roots, r)
	}
	return roots, true
}

func writeListCache(path, sig string, roots []*packages.Package, dir string) {
	c := listCache{Sig: sig, Dirs: map[string]string{}, Files: map[string]string{}, Extras: map[string]string{},
		Heads: map[string]string{}, Names: map[string]string{}}
	for _, f := range modFiles(dir) {
		c.Extras[f] = statSig(f)
	}
	packages.Visit(roots, nil, func(p *packages.Package) {
		lp := listPkg{ID: p.ID, Name: p.Name, PkgPath: p.PkgPath, GoFiles: p.GoFiles,
			CompiledGoFiles: p.CompiledGoFiles, OtherFiles: p.OtherFiles, EmbedFiles: p.EmbedFiles,
			EmbedPatterns: p.EmbedPatterns, Module: p.Module, Imports: map[string]string{}}
		for path, d := range p.Imports {
			lp.Imports[path] = d.ID
		}
		c.Pkgs = append(c.Pkgs, lp)
		files := append(append(append([]string(nil), p.GoFiles...), p.CompiledGoFiles...), p.OtherFiles...)
		files = append(files, p.EmbedFiles...)
		files = append(files, p.IgnoredFiles...)
		for _, f := range files {
			c.Files[f] = statSig(f)
			c.Heads[f] = headHash(f)
			d := filepath.Dir(f)
			if _, ok := c.Dirs[d]; !ok {
				c.Dirs[d] = statSig(d)
				c.Names[d] = goNames(d)
			}
		}
	})
	for _, r := range roots {
		c.Roots = append(c.Roots, r.ID)
	}
	data, err := json.Marshal(&c)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, path)
	}
}

// lightConfig is pcfg for the package graph only (no syntax or types).
func lightConfig(pcfg *packages.Config) *packages.Config {
	light := *pcfg
	light.Mode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
		packages.NeedDeps | packages.NeedEmbedFiles | packages.NeedEmbedPatterns | packages.NeedModule |
		packages.NeedTypesSizes
	return &light
}

// CachedInputFiles is InputFiles from the go list cache of an incremental
// load (cfg.CacheDir), without running go list; ok is false when that cache
// is missing or out of date.
func CachedInputFiles(cfg Config) (files []string, goroot string, ok bool) {
	pcfg, patterns, goroot, _, err := setup(cfg)
	if err != nil {
		return nil, "", false
	}
	light := lightConfig(pcfg)
	sig := listSig(light, patterns)
	roots, ok := readListCache(listCachePath(cfg.CacheDir, sig), sig, light.Dir)
	if !ok {
		return nil, "", false
	}
	packages.Visit(roots, nil, func(p *packages.Package) {
		files = append(files, p.GoFiles...)
		files = append(files, p.EmbedFiles...)
	})
	return files, goroot, true
}

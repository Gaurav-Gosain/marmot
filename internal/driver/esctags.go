package driver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/codegen"
	"github.com/Gaurav-Gosain/marmot/internal/loader"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// escapeTags gives code generation the escape tags of every package
// (codegen/esctags.go) in an incremental build with the tags on: computed
// for the packages loaded from source and cached under their export key,
// read from the cache for the others (none when absent: their callees then
// escape). The hashes go into the module keys of importers (packageKeys).
func (b *build) escapeTags(lp *loader.Program, cg *codegen.Program, pkgs []*ssa.Package) {
	if !b.incr || !codegen.EscTagsOn() {
		return
	}
	byPath := map[string]*packages.Package{}
	for _, p := range lp.All {
		byPath[p.PkgPath] = p
	}
	tags := make([]codegen.EscTags, len(pkgs))
	eachParallel(len(pkgs), func(i int) {
		path := pkgs[i].Pkg.Path()
		ip := lp.Incremental[path]
		if ip == nil || ip.Key == "" || byPath[path] == nil {
			return
		}
		file := b.escTagsPath(ip.Key)
		if !ip.FromSource {
			if data, err := os.ReadFile(file); err == nil {
				var t codegen.EscTags
				if json.Unmarshal(data, &t) == nil {
					tags[i] = t
				}
			}
			return
		}
		t := cg.ComputeEscTags(pkgs[i])
		if data, err := json.Marshal(t); err == nil {
			_ = os.MkdirAll(filepath.Dir(file), 0o755) // a failed write only costs a recomputation
			_ = loader.WriteAtomic(file, data)
		}
		tags[i] = t
	})
	byPkg := make(map[*ssa.Package]codegen.EscTags, len(pkgs))
	b.escHash = make(map[string]string, len(pkgs))
	for i, sp := range pkgs {
		byPkg[sp] = tags[i]
		b.escHash[sp.Pkg.Path()] = tags[i].Hash()
	}
	cg.SetImportTags(byPkg)
}

func (b *build) escTagsPath(key string) string {
	return filepath.Join(b.typesCache(), "esc", key+".json")
}

// importTagsKey is the part of p's module key that comes from the escape
// tags of the packages it imports.
func (b *build) importTagsKey(p *packages.Package) string {
	if b.escHash == nil {
		return ""
	}
	paths := make([]string, 0, len(p.Imports))
	for path := range p.Imports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var sb strings.Builder
	sb.WriteString("\nesc")
	for _, path := range paths {
		sb.WriteString(" " + path + "=" + b.escHash[p.Imports[path].PkgPath])
	}
	return sb.String()
}

// moduleTagKeys returns, per package, importTagsKey; nil when tags are off.
func (b *build) moduleTagKeys(lp *loader.Program, pkgs []*ssa.Package) []string {
	if b.escHash == nil {
		return nil
	}
	byPath := map[string]*packages.Package{}
	for _, p := range lp.All {
		byPath[p.PkgPath] = p
	}
	out := make([]string, len(pkgs))
	for i, sp := range pkgs {
		if p := byPath[sp.Pkg.Path()]; p != nil {
			out[i] = b.importTagsKey(p)
		}
	}
	return out
}

// tagKeyPath is the file next to a cached module that records the escape
// tags of its imports the module was built against.
func (b *build) tagKeyPath(k string) string {
	return filepath.Join(b.cfg.Root, "build", "cache", "mlir", k+".esc")
}

// tagsMatch reports whether the cached module of package i, key k, was
// built against the escape tags its imports have now.
func (b *build) tagsMatch(i int, k string) bool {
	if b.tagKeys == nil || i >= len(b.tagKeys) {
		return true
	}
	data, err := os.ReadFile(b.tagKeyPath(k))
	return err == nil && string(data) == b.tagKeys[i]
}

// saveTagKey records the import tags of package i's module, key k.
func (b *build) saveTagKey(i int, k string) {
	if b.tagKeys == nil || i >= len(b.tagKeys) {
		return
	}
	_ = loader.WriteAtomic(b.tagKeyPath(k), []byte(b.tagKeys[i])) // a missing record only costs a regeneration
}

// dropModule removes a stale cached module (its first part: cachedParts
// then reports it missing).
func (b *build) dropModule(k string) {
	_ = os.Remove(filepath.Join(b.cfg.Root, "build", "cache", "mlir", k+".mlir")) // best effort: it is regenerated
	_ = os.Remove(b.tagKeyPath(k))
}

package driver

import (
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// trimNames maps each source file of the program to the name a -trimpath
// build records for it (in the pc table, DWARF and so in tracebacks), as go
// build -trimpath names files: a standard library file by its path under
// GOROOT/src (runtime/proc.go), a module's file by the module path, with
// @version for a dependency, and its path in the module
// (example.com/app/cmd/app/main.go, golang.org/x/text@v0.20.0/width/kind.go),
// and anything else (generated files in a cache) by the package path and
// the file's name. No name keeps a directory of this machine.
func trimNames(goroot string, pkgs []*packages.Package) map[string]string {
	out := map[string]string{}
	src := ""
	if goroot != "" {
		src = filepath.Clean(goroot) + string(filepath.Separator) + "src" + string(filepath.Separator)
	}
	for _, p := range pkgs {
		for _, f := range append(append([]string(nil), p.GoFiles...), p.CompiledGoFiles...) {
			if _, ok := out[f]; !ok {
				out[f] = trimName(src, p, f)
			}
		}
	}
	return out
}

func trimName(gorootSrc string, p *packages.Package, f string) string {
	if gorootSrc != "" && strings.HasPrefix(f, gorootSrc) {
		return filepath.ToSlash(f[len(gorootSrc):])
	}
	if m := p.Module; m != nil && m.Dir != "" {
		if rel, err := filepath.Rel(m.Dir, f); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			mp := m.Path
			if m.Version != "" {
				mp += "@" + m.Version
			}
			return mp + "/" + filepath.ToSlash(rel)
		}
	}
	return p.PkgPath + "/" + filepath.Base(f)
}

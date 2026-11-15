package loader

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
)

// buildList must list what go list lists: the same packages, files and imports, for the
// host and for wasip1/wasm.
func TestBuildListMatchesGoList(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	goroot, err := goEnv("GOROOT")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module play\n\ngo 1.24\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n\t\"time\"\n\t\"play/sub\"\n)\n\nfunc main() { var wg sync.WaitGroup; wg.Wait(); fmt.Println(time.Second, sub.X) }\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "sub", "sub.go"), []byte("package sub\n\nimport \"strings\"\n\nvar X = strings.Repeat(\"a\", 2)\n"), 0o644))
	for _, target := range [][2]string{{"wasip1", "wasm"}, {"linux", "amd64"}, {"darwin", "arm64"}} {
		env := append(os.Environ(), "GOROOT="+goroot, "GOOS="+target[0], "GOARCH="+target[1], "CGO_ENABLED=0",
			"GOFLAGS=-tags=marmot,purego,noasm,math_big_pure_go", "GOTOOLCHAIN=local", "GOWORK=off")
		cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps, Dir: dir, Env: env}
		want, err := packages.Load(cfg, ".", "runtime")
		must(t, err)
		got, err := buildList(cfg, []string{".", "runtime"})
		must(t, err)
		w, g := flat(want), flat(got)
		for path, wp := range w {
			gp := g[path]
			if gp == nil {
				t.Errorf("%s: %s missing", target, path)
				continue
			}
			if !slices.Equal(wp.CompiledGoFiles, gp.CompiledGoFiles) {
				t.Errorf("%s: %s files\n go list %v\n gobuild %v", target, path, wp.CompiledGoFiles, gp.CompiledGoFiles)
			}
			for imp, d := range wp.Imports {
				if gp.Imports[imp] == nil || gp.Imports[imp].PkgPath != d.PkgPath {
					t.Errorf("%s: %s import %s: want %s", target, path, imp, d.PkgPath)
				}
			}
		}
		for path := range g {
			if w[path] == nil {
				t.Errorf("%s: %s extra", target, path)
			}
		}
	}
}

func flat(roots []*packages.Package) map[string]*packages.Package {
	m := map[string]*packages.Package{}
	packages.Visit(roots, nil, func(p *packages.Package) { m[p.PkgPath] = p })
	return m
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

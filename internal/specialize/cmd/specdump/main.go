// Command specdump rewrites one main package with internal/specialize and
// writes the result as a standalone module, for measuring a specialization
// with any toolchain:
//
//	go run ./internal/specialize/cmd/specdump -o out/fmtout ./bench/programs/fmtout
//
// The package is loaded against the stock GOROOT plus the fmt entry points
// (internal/specialize/_lib), so the output builds with go build only when
// that file is overlaid too; specdump writes overlay.json for that.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/specialize"
	"golang.org/x/tools/go/packages"
)

func main() {
	out := flag.String("o", "", "output directory")
	lib := flag.String("lib", "internal/specialize/_lib", "directory with the fmt entry points")
	flag.Parse()
	if *out == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: specdump -o dir ./pkg")
		os.Exit(2)
	}
	if err := run(*out, *lib, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "specdump:", err)
		os.Exit(1)
	}
}

func run(out, lib, pkg string) error {
	gr, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	goroot := strings.TrimSpace(string(gr))
	ov := map[string][]byte{}
	repl := map[string]string{}
	err = filepath.WalkDir(lib, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(lib, path)
		abs, _ := filepath.Abs(path)
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		key := filepath.Join(goroot, "src", rel)
		ov[key] = src
		repl[key] = abs
		return nil
	})
	if err != nil {
		return err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedTypesSizes | packages.NeedModule,
		Env:     append(os.Environ(), "CGO_ENABLED=0"),
		Overlay: ov,
	}
	pkgs, err := packages.Load(cfg, pkg)
	if err != nil {
		return err
	}
	var all []*packages.Package
	var errs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		all = append(all, p)
		for _, e := range p.Errors {
			errs = append(errs, e.Error())
		}
	})
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	res, err := specialize.Rewrite(all)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	root := pkgs[0]
	for _, f := range root.Syntax {
		name := filepath.Base(root.Fset.File(f.Pos()).Name())
		var src []byte
		if name == specialize.GenFile {
			src = []byte(res.Gen[root])
		} else {
			var buf bytes.Buffer
			if err := format.Node(&buf, root.Fset, f); err != nil {
				return err
			}
			src = buf.Bytes()
		}
		if err := os.WriteFile(filepath.Join(out, name), src, 0o644); err != nil {
			return err
		}
	}
	mod := "module specdump\n\ngo 1.27\n"
	if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(mod), 0o644); err != nil {
		return err
	}
	js, _ := json.Marshal(map[string]any{"Replace": repl})
	if err := os.WriteFile(filepath.Join(out, "overlay.json"), js, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d sites rewritten\n", pkg, res.Sites)
	return nil
}

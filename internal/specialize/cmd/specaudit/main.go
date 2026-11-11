// Command specaudit rewrites one main package with every specialization and
// lists what can still reach reflect: calls into fmt, encoding/json,
// reflect, text/template and similar packages that were not rewritten, and
// generated code that falls back to the general path.
//
//	go run ./internal/specialize/cmd/specaudit ./bench/programs/fmtout
package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/specialize"
	"golang.org/x/tools/go/packages"
)

var reflective = map[string]bool{
	"fmt": true, "encoding/json": true, "reflect": true, "text/template": true, "html/template": true,
	"encoding/xml": true, "encoding/gob": true, "encoding/binary": true, "flag": true, "log": true, "errors": true,
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: specaudit ./pkg")
		os.Exit(2)
	}
	gr, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	goroot := strings.TrimSpace(string(gr))
	ov := map[string][]byte{}
	lib := "internal/specialize/_lib"
	filepath.WalkDir(lib, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(lib, path)
		src, err := os.ReadFile(path)
		if err == nil {
			ov[filepath.Join(goroot, "src", rel)] = src
		}
		return nil
	})
	cfg := &packages.Config{Overlay: ov, Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
		packages.NeedImports | packages.NeedDeps | packages.NeedSyntax | packages.NeedTypes |
		packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule}
	pkgs, err := packages.Load(cfg, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var all []*packages.Package
	packages.Visit(pkgs, nil, func(p *packages.Package) { all = append(all, p) })
	res, err := specialize.Rewrite(all)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	counts := map[string]int{}
	for _, p := range all {
		if p.Module == nil && p.Name != "main" {
			continue
		}
		for _, f := range p.Syntax {
			if filepath.Base(p.Fset.File(f.Pos()).Name()) == specialize.GenFile {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var obj types.Object
				switch fn := ast.Unparen(call.Fun).(type) {
				case *ast.Ident:
					obj = p.TypesInfo.Uses[fn]
				case *ast.SelectorExpr:
					obj = p.TypesInfo.Uses[fn.Sel]
				}
				if f, ok := obj.(*types.Func); ok && f.Pkg() != nil && reflective[f.Pkg().Path()] {
					counts[f.Pkg().Path()+"."+f.Name()]++
				}
				return true
			})
		}
		if src, ok := res.Gen[p]; ok {
			if n := strings.Count(src, ".MarmotArg("); n > 0 {
				counts["fmt operand via printArg"] += n
			}
			if n := strings.Count(src, ".Marshal(v)") + strings.Count(src, ".Unmarshal(data, v)") + strings.Count(src, "e.Encode(v)"); n > 0 {
				counts["json fallback path"] += n
			}
		}
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s x%d\n", k, counts[k])
	}
}

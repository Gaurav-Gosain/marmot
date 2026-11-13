package diag

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// explainPrograms are complete programs for the marmot explain examples:
// each contains the first line of its code's Wrong example and must give
// that code when type checked.
var explainPrograms = map[string]string{
	CodeSyntax: "package main\n\nfunc main() {\n\tx := 1 +\n}\n",
	CodeNotExported: `package main

import (
	"strings"
	"unicode"
)

func main() {
	s := " x"
	i := strings.indexFunc(s, unicode.IsSpace)
	_ = i
}
`,
	CodeUndefined: `package main

import "fmt"

func main() {
	fmt.Printn("hi")
}
`,
	CodeNoFieldMethod: `package main

import "strings"

func main() {
	var b strings.Builder
	b.WriteStrin("x")
}
`,
	CodeUnusedVar:    "package main\n\nfunc main() {\n\txs := []int{1}\n\tcount := len(xs) // never read\n}\n",
	CodeUnusedImport: "package main\n\nimport \"os\" // nothing uses os\n\nfunc main() {}\n",
	CodeNotImplement: `package main

type Shape interface{ Area() float64 }
type Square struct{ s float64 }

func (q *Square) Area() float64 { return q.s * q.s }

func main() {
	var sh Shape = Square{2}
	_ = sh
}
`,
	CodeCannotInfer:   "package main\n\nfunc Zero[T any]() T { var z T; return z }\n\nfunc main() {\n\tx := Zero()\n\t_ = x\n}\n",
	CodeUnknownField:  "package main\n\ntype User struct{ Name string }\n\nfunc main() {\n\tu := User{Nmae: \"ada\"}\n\t_ = u\n}\n",
	CodeNotSatisfy:    "package main\n\ntype Number interface{ ~int | ~float64 }\n\nfunc Sum[T Number](xs []T) T { var s T; return s }\n\nfunc main() {\n\tSum([]float32{1})\n}\n",
	CodeCannotUse:     "package main\n\nfunc add(a, b int) int { return a + b }\n\nfunc main() {\n\tadd(1, \"two\")\n}\n",
	CodeMismatched:    "package main\n\nfunc main() {\n\tvar n int\n\tvar f float64\n\t_ = n + f\n}\n",
	CodeReturnCount:   "package main\n\nfunc main() {\n\treturn 1\n}\n",
	CodeArgCount:      "package main\n\nfunc pair() (int, int) { return 1, 2 }\n\nfunc main() {\n\tpair(1)\n}\n",
	CodeMissingReturn: "package main\n\nfunc sign(x int) int {\n\tif x < 0 {\n\t\treturn -1\n\t}\n}\n\nfunc main() { _ = sign(1) }\n",
	CodeAssignCount:   "package main\n\nimport \"strconv\"\n\nfunc main() {\n\ts := \"1\"\n\tn := strconv.Atoi(s)\n\t_ = n\n}\n",
	CodeNoNewVars:     "package main\n\nfunc main() {\n\tx := 1\n\tx := 2\n\t_ = x\n}\n",
	CodeRedeclared:    "package main\n\nfunc f() {}\nfunc f() {}\n\nfunc main() {}\n",
	CodeInvalidOp:     "package main\n\nfunc main() {\n\tvar m, n map[string]int\n\t_ = m < n // maps have no order\n}\n",
}

// explainSkip are codes whose example is not a single program's build
// error, with the reason.
var explainSkip = map[string]string{
	CodeImportCycle: "needs two packages",
	CodePkgNotFound: "needs a module and the network",
	CodeShadow:      "reported by vet, not the type checker",
	CodeExhaustive:  "reported by marmot check",
	CodeNil:         "reported by marmot check",
	CodeHeapAlloc:   "reported by marmot build -allocs",
}

// TestExplainExamples type checks a program for every explain example and
// checks it yields the example's code.
func TestExplainExamples(t *testing.T) {
	for _, ci := range codeTable {
		if ci.Wrong == "" {
			continue
		}
		t.Run(ci.Code, func(t *testing.T) {
			if why, ok := explainSkip[ci.Code]; ok {
				t.Skip(why)
			}
			prog, ok := explainPrograms[ci.Code]
			if !ok {
				t.Fatalf("no program for the example of %s (add one to explainPrograms, or a reason to explainSkip)", ci.Code)
			}
			first := strings.TrimSpace(strings.Split(ci.Wrong, "\n")[0])
			if i := strings.Index(first, " //"); i >= 0 {
				first = strings.TrimSpace(first[:i])
			}
			if !strings.Contains(prog, first) {
				t.Errorf("the program does not contain the example's first line %q", first)
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "main.go")
			if err := os.WriteFile(file, []byte(prog), 0o644); err != nil {
				t.Fatal(err)
			}
			text := typeErrors(file, prog)
			if text == "" {
				t.Fatalf("the program has no errors")
			}
			ds := Process(text, NewSources(), Options{Dir: dir})
			if len(ds) == 0 || ds[0].Code != ci.Code {
				got := "none"
				if len(ds) > 0 {
					got = ds[0].Code + " " + ds[0].Message
				}
				t.Errorf("the example of %s gives %s\n%s", ci.Code, got, text)
			}
		})
	}
}

// typeErrors parses and type checks one file as the loader would and
// returns its errors as compiler text (file:line:col: message).
func typeErrors(file, src string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.AllErrors)
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Error: func(err error) {
		fmt.Fprintln(&b, err)
	}}
	conf.Check("main", fset, []*ast.File{f}, nil)
	return b.String()
}

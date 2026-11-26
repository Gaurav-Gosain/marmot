package driver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestRaceOffShapes checks that every use of raceenabled in the runtime has
// the shape dropRaceOff removes when -race is off: the condition (or the
// first operand of a && chain) of an if with no init and no else. A block
// holds no func literal, label or goto: dropping a closure would renumber
// the function's later closures between -race and plain builds.
func TestRaceOffShapes(t *testing.T) {
	files, err := filepath.Glob("../../lib/src/runtime/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no runtime files: %v", err)
	}
	fs := token.NewFileSet()
	n := 0
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasSuffix(base, "_test.go") || base == "race_off.go" || base == "race_marmot.go" {
			continue
		}
		af, err := parser.ParseFile(fs, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ok := map[*ast.Ident]bool{}
		ast.Inspect(af, func(nd ast.Node) bool {
			s, is := nd.(*ast.IfStmt)
			if !is {
				return true
			}
			id := raceLead(s.Cond)
			if id == nil {
				return true
			}
			n++
			if s.Init != nil || s.Else != nil {
				t.Errorf("%s: if raceenabled with init or else: dropRaceOff leaves it", fs.Position(s.Pos()))
			}
			ast.Inspect(s.Body, func(b ast.Node) bool {
				switch b.(type) {
				case *ast.FuncLit, *ast.LabeledStmt, *ast.BranchStmt:
					if br, isBr := b.(*ast.BranchStmt); !isBr || br.Tok == token.GOTO {
						t.Errorf("%s: func literal, label or goto in an if raceenabled block", fs.Position(b.Pos()))
					}
				}
				return true
			})
			ok[id] = true
			return true
		})
		ast.Inspect(af, func(nd ast.Node) bool {
			if id, is := nd.(*ast.Ident); is && id.Name == "raceenabled" && !ok[id] {
				t.Errorf("%s: raceenabled used outside an if condition: dropRaceOff leaves it", fs.Position(id.Pos()))
			}
			return true
		})
	}
	if n == 0 {
		t.Fatal("no if raceenabled statements found")
	}
}

// raceLead returns the raceenabled identifier that leads cond, or nil.
func raceLead(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.BinaryExpr:
			if x.Op != token.LAND {
				return nil
			}
			e = x.X
		case *ast.Ident:
			if x.Name == "raceenabled" {
				return x
			}
			return nil
		default:
			return nil
		}
	}
}

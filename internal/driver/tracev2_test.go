package driver

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"slices"
	"testing"
)

// TestProfileTwins checks that lib/src/runtime/profile.go (a build without
// the tracer) and profile_tracev2.go (a build with MARMOT_TRACE_V2=1) hold
// the same declarations, except the tracer stubs, which only profile.go has.
// profile.go keeps the file of before the tracer byte for byte, so the two
// copies must not drift.
func TestProfileTwins(t *testing.T) {
	stubs := []string{"StartTrace", "StopTrace", "ReadTrace", "traceClockUnitsPerSecond", "traceAdvance", "traceClockNow"}
	dir := filepath.Join("..", "..", "lib", "src", "runtime")
	off, offImports := profileDecls(t, filepath.Join(dir, "profile.go"))
	on, onImports := profileDecls(t, filepath.Join(dir, "profile_tracev2.go"))
	if !slices.Equal(offImports, onImports) {
		t.Errorf("imports differ: profile.go %v, profile_tracev2.go %v", offImports, onImports)
	}
	for _, s := range stubs {
		if _, ok := off[s]; !ok {
			t.Errorf("profile.go has no stub %s", s)
		}
		if _, ok := on[s]; ok {
			t.Errorf("profile_tracev2.go declares the stub %s (trace_marmot.go has it)", s)
		}
		delete(off, s)
	}
	for name, src := range off {
		if other, ok := on[name]; !ok {
			t.Errorf("%s is in profile.go only", name)
		} else if other != src {
			t.Errorf("%s differs:\nprofile.go:\n%s\nprofile_tracev2.go:\n%s", name, src, other)
		}
	}
	for name := range on {
		if _, ok := off[name]; !ok {
			t.Errorf("%s is in profile_tracev2.go only", name)
		}
	}
}

// profileDecls returns the top-level declarations of a file by name, each
// printed without comments, and the import paths.
func profileDecls(t *testing.T, path string) (map[string]string, []string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var imports []string
	for _, im := range f.Imports {
		imports = append(imports, im.Path.Value)
	}
	slices.Sort(imports)
	decls := map[string]string{}
	add := func(name string, n ast.Node) {
		var b bytes.Buffer
		if err := printer.Fprint(&b, fset, n); err != nil {
			t.Fatal(err)
		}
		if _, dup := decls[name]; dup {
			t.Fatalf("%s: %s declared twice", path, name)
		}
		decls[name] = b.String()
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			d.Doc = nil
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				var b bytes.Buffer
				printer.Fprint(&b, fset, d.Recv.List[0].Type)
				name = b.String() + "." + name
			}
			add(name, d)
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					s.Doc, s.Comment = nil, nil
					add(s.Name.Name, s)
				case *ast.ValueSpec:
					s.Doc, s.Comment = nil, nil
					var names []string
					for _, n := range s.Names {
						names = append(names, n.Name)
					}
					add(d.Tok.String()+" "+names[0], s)
				}
			}
		}
	}
	return decls, imports
}

package diag

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// stdIndex maps standard library package names to import paths, built from
// the GOROOT directory tree on first use (failure path only).
type stdIndex struct {
	root   string
	byName map[string][]string
}

func (s *Sources) goroot() string {
	if s.GOROOT != "" {
		return s.GOROOT
	}
	if r := os.Getenv("GOROOT"); r != "" {
		s.GOROOT = r
		return r
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err == nil {
		s.GOROOT = strings.TrimSpace(string(out))
	}
	return s.GOROOT
}

func (s *Sources) stdIdx() *stdIndex {
	if s.std != nil {
		return s.std
	}
	ix := &stdIndex{byName: map[string][]string{}}
	s.std = ix
	root := s.goroot()
	if root == "" {
		return ix
	}
	ix.root = filepath.Join(root, "src")
	filepath.WalkDir(ix.root, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(ix.root, p)
		if rel == "." {
			return nil
		}
		base := d.Name()
		if base == "internal" || base == "vendor" || base == "testdata" || rel == "cmd" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
			return filepath.SkipDir
		}
		name := base
		if len(base) > 1 && base[0] == 'v' && base[1] >= '0' && base[1] <= '9' {
			// math/rand/v2 is package rand.
			name = filepath.Base(filepath.Dir(rel))
		}
		ix.byName[name] = append(ix.byName[name], filepath.ToSlash(rel))
		return nil
	})
	for _, v := range ix.byName {
		sort.Slice(v, func(i, j int) bool {
			if len(v[i]) != len(v[j]) {
				return len(v[i]) < len(v[j])
			}
			return v[i] < v[j]
		})
	}
	return ix
}

// exports is the exported API of one package, from its source.
type exports struct {
	names   map[string]string            // name -> kind: func, type, var, const
	methods map[string]map[string]string // type -> method -> "T" or "*T"
	fields  map[string][]string          // struct type -> field names
	decl    map[string]string            // type -> one line definition
}

// pkgExports parses the non-test files of a package directory.
func pkgExports(dir string, all bool) *exports {
	ex := &exports{names: map[string]string{}, methods: map[string]map[string]string{}, fields: map[string][]string{}, decl: map[string]string{}}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ex
	}
	fset := token.NewFileSet()
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		path := filepath.Join(dir, n)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(fset, path, data, parser.SkipObjectResolution)
		if err != nil || f.Name.Name == "main" && !all {
			continue
		}
		collectExports(ex, fset, data, f, all)
	}
	return ex
}

func collectExports(ex *exports, fset *token.FileSet, data []byte, f *ast.File, all bool) {
	ok := func(name string) bool { return all || ast.IsExported(name) }
	src := func(n ast.Node) string {
		a, b := fset.Position(n.Pos()).Offset, fset.Position(n.End()).Offset
		if a < 0 || b > len(data) || a > b {
			return ""
		}
		return string(data[a:b])
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				if ok(d.Name.Name) {
					ex.names[d.Name.Name] = "func"
				}
				continue
			}
			if len(d.Recv.List) == 0 || !ok(d.Name.Name) {
				continue
			}
			t := d.Recv.List[0].Type
			recv := "T"
			if st, isStar := t.(*ast.StarExpr); isStar {
				t, recv = st.X, "*T"
			}
			switch x := t.(type) {
			case *ast.IndexExpr:
				t = x.X
			case *ast.IndexListExpr:
				t = x.X
			}
			if id, isID := t.(*ast.Ident); isID {
				m := ex.methods[id.Name]
				if m == nil {
					m = map[string]string{}
					ex.methods[id.Name] = m
				}
				m[d.Name.Name] = recv
			}
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				switch sp := sp.(type) {
				case *ast.TypeSpec:
					if !ok(sp.Name.Name) {
						continue
					}
					ex.names[sp.Name.Name] = "type"
					line := "type " + oneLine(src(sp))
					if len(line) > 160 {
						line = line[:157] + "..."
					}
					ex.decl[sp.Name.Name] = line
					if st, isStruct := sp.Type.(*ast.StructType); isStruct {
						for _, fl := range st.Fields.List {
							for _, nm := range fl.Names {
								if ok(nm.Name) {
									ex.fields[sp.Name.Name] = append(ex.fields[sp.Name.Name], nm.Name)
								}
							}
							if len(fl.Names) == 0 {
								// Embedded field.
								t := fl.Type
								if s, isStar := t.(*ast.StarExpr); isStar {
									t = s.X
								}
								switch x := t.(type) {
								case *ast.Ident:
									ex.fields[sp.Name.Name] = append(ex.fields[sp.Name.Name], x.Name)
								case *ast.SelectorExpr:
									ex.fields[sp.Name.Name] = append(ex.fields[sp.Name.Name], x.Sel.Name)
								}
							}
						}
					}
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, nm := range sp.Names {
						if ok(nm.Name) {
							ex.names[nm.Name] = kind
						}
					}
				}
			}
		}
	}
}

// stdExports returns the exports of a standard library package.
func (s *Sources) stdExports(path string) *exports {
	ix := s.stdIdx()
	if ix.root == "" {
		return nil
	}
	return pkgExports(filepath.Join(ix.root, filepath.FromSlash(path)), false)
}

// Package typecheck type-checks the overlay packages in lib/src with
// go/types, the way the marmot loader sees them: packages present in
// lib/src shadow GOROOT, build tag "marmot" is set, and body-less functions
// are allowed.
package typecheck

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type overlayImporter struct {
	root   string
	fset   *token.FileSet
	ctx    build.Context
	pkgs   map[string]*types.Package
	stock  types.ImporterFrom
	tmp    string
	errors []error
}

func (im *overlayImporter) Import(path string) (*types.Package, error) {
	return im.ImportFrom(path, "", 0)
}

func (im *overlayImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if p, ok := im.pkgs[path]; ok {
		return p, nil
	}
	odir := filepath.Join(im.root, filepath.FromSlash(path))
	if hasGo(odir) {
		return im.check(path, odir)
	}
	return im.stock.ImportFrom(path, dir, mode)
}

func hasGo(dir string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

// mergedDir returns dir, or for a MERGE overlay a temporary directory of
// symlinks holding the stock files (minus hidden ones) and the overlay
// files, as the marmot loader builds it.
func (im *overlayImporter) mergedDir(path, dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "MERGE"))
	if err != nil {
		return dir, nil
	}
	hidden := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-") {
			hidden[strings.TrimSpace(line[1:])] = true
		}
	}
	tmp, err := os.MkdirTemp(im.tmp, "merge")
	if err != nil {
		return "", err
	}
	stock := filepath.Join(im.ctx.GOROOT, "src", filepath.FromSlash(path))
	ents, _ := os.ReadDir(stock)
	for _, e := range ents {
		if e.IsDir() || hidden[e.Name()] {
			continue
		}
		os.Symlink(filepath.Join(stock, e.Name()), filepath.Join(tmp, e.Name()))
	}
	ents, _ = os.ReadDir(dir)
	for _, e := range ents {
		if e.IsDir() || e.Name() == "MERGE" {
			continue
		}
		os.Remove(filepath.Join(tmp, e.Name()))
		os.Symlink(filepath.Join(dir, e.Name()), filepath.Join(tmp, e.Name()))
	}
	return tmp, nil
}

func (im *overlayImporter) check(path, dir string) (*types.Package, error) {
	dir, err := im.mergedDir(path, dir)
	if err != nil {
		return nil, err
	}
	bp, err := im.ctx.ImportDir(dir, 0)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, name := range bp.GoFiles {
		f, err := parser.ParseFile(im.fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	conf := types.Config{
		Importer:         im,
		IgnoreFuncBodies: false,
		FakeImportC:      false,
		Error: func(err error) {
			im.errors = append(im.errors, err)
		},
	}
	pkg, _ := conf.Check(path, im.fset, files, nil)
	im.pkgs[path] = pkg
	return pkg, nil
}

// overlayPackages lists the import paths of all packages in lib/src.
func overlayPackages(root string) []string {
	var paths []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && hasGo(p) {
			rel, _ := filepath.Rel(root, p)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	return paths
}

func TestTypecheckOverlay(t *testing.T) {
	root, err := filepath.Abs("../../src")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	ctx := build.Default
	ctx.GOOS = "darwin"
	ctx.GOARCH = "arm64"
	ctx.CgoEnabled = false
	ctx.BuildTags = []string{"marmot"}
	ctx.GOROOT = runtime.GOROOT()
	im := &overlayImporter{
		root:  root,
		fset:  fset,
		ctx:   ctx,
		pkgs:  map[string]*types.Package{},
		stock: importer.ForCompiler(fset, "source", nil).(types.ImporterFrom),
		tmp:   t.TempDir(),
	}
	for _, path := range overlayPackages(root) {
		if _, err := im.Import(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
	for _, err := range im.errors {
		t.Error(err)
	}
}

// TestRtabiLayout checks that internal/rtabi mirrors the runtime's
// descriptor layout field by field.
func TestRtabiLayout(t *testing.T) {
	root, _ := filepath.Abs("../../src")
	fset := token.NewFileSet()
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "darwin", "arm64", false
	ctx.BuildTags = []string{"marmot"}
	ctx.GOROOT = runtime.GOROOT()
	im := &overlayImporter{root: root, fset: fset, ctx: ctx, pkgs: map[string]*types.Package{},
		stock: importer.ForCompiler(fset, "source", nil).(types.ImporterFrom), tmp: t.TempDir()}
	rt, _ := im.Import("runtime")
	ab, _ := im.Import("internal/rtabi")
	if rt == nil || ab == nil {
		t.Fatal("cannot load runtime or internal/rtabi")
	}
	sizes := types.SizesFor("gc", "arm64")
	pairs := [][2]string{
		{"_type", "Type"}, {"uncommontype", "UncommonType"}, {"method", "Method"},
		{"ptrtype", "PtrType"}, {"slicetype", "SliceType"}, {"arraytype", "ArrayType"},
		{"chantype", "ChanType"}, {"maptype", "MapType"}, {"functype", "FuncType"},
		{"imethod", "Imethod"}, {"interfacetype", "InterfaceType"},
		{"structfield", "StructField"}, {"structtype", "StructType"}, {"itab", "Itab"},
		{"eface", "EmptyInterface"}, {"iface", "NonEmptyInterface"},
	}
	for _, p := range pairs {
		a := rt.Scope().Lookup(p[0]).Type().Underlying().(*types.Struct)
		b := ab.Scope().Lookup(p[1]).Type().Underlying().(*types.Struct)
		if sizes.Sizeof(a) != sizes.Sizeof(b) {
			t.Errorf("%s/%s: size %d vs %d", p[0], p[1], sizes.Sizeof(a), sizes.Sizeof(b))
			continue
		}
		oa := layout(sizes, a, 0)
		ob := layout(sizes, b, 0)
		if fmt.Sprint(oa) != fmt.Sprint(ob) {
			t.Errorf("%s/%s layout differs:\n%v\n%v", p[0], p[1], oa, ob)
		}
	}
}

// layout flattens a struct into (offset, size) pairs of its scalar fields.
func layout(sizes types.Sizes, st *types.Struct, base int64) [][2]int64 {
	var fields []*types.Var
	for i := 0; i < st.NumFields(); i++ {
		fields = append(fields, st.Field(i))
	}
	offs := sizes.Offsetsof(fields)
	var out [][2]int64
	for i, f := range fields {
		if sub, ok := f.Type().Underlying().(*types.Struct); ok {
			out = append(out, layout(sizes, sub, base+offs[i])...)
			continue
		}
		out = append(out, [2]int64{base + offs[i], sizes.Sizeof(f.Type())})
	}
	return out
}

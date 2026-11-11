// Package typecheck type-checks the overlay packages in lib/src with
// go/types, the way the marmot loader sees them: packages present in
// lib/src shadow GOROOT, build tag "marmot" is set, and body-less functions
// are allowed.
package typecheck

import (
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type overlayImporter struct {
	root   string
	fset   *token.FileSet
	ctx    build.Context
	pkgs   map[string]*types.Package
	stock  types.ImporterFrom
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

func (im *overlayImporter) check(path, dir string) (*types.Package, error) {
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
	im := &overlayImporter{
		root:  root,
		fset:  fset,
		ctx:   ctx,
		pkgs:  map[string]*types.Package{},
		stock: importer.ForCompiler(fset, "source", nil).(types.ImporterFrom),
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

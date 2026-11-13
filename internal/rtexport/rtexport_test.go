package rtexport

import (
	"bytes"
	"go/importer"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
)

// WriteAll keeps every package-level name; gcexportdata.Read restores them
// with the same types.
func TestRoundTripAllNames(t *testing.T) {
	for _, path := range []string{"strings", "runtime"} {
		fset := token.NewFileSet()
		pkg, err := importer.ForCompiler(fset, "source", nil).Import(path)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := WriteAll(&buf, fset, pkg); err != nil {
			t.Fatal(err)
		}
		got, err := gcexportdata.Read(&buf, token.NewFileSet(), map[string]*types.Package{}, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range pkg.Scope().Names() {
			want := pkg.Scope().Lookup(name)
			o := got.Scope().Lookup(name)
			if o == nil {
				t.Errorf("%s.%s: missing", path, name)
				continue
			}
			q := func(p *types.Package) string { return p.Path() }
			if a, b := types.ObjectString(want, q), types.ObjectString(o, q); a != b {
				t.Errorf("%s.%s: %s, want %s", path, name, b, a)
			}
		}
	}
}

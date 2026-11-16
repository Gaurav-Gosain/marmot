package codegen

import (
	"go/token"
	"strings"
	"testing"
)

// Options.LineLocs (optimized builds, unless MARMOT_LOC_COLS=1) gives every
// location column 1, so two positions on one line share one alias. Without
// it the columns stay.
func TestLineLocs(t *testing.T) {
	for _, lineOnly := range []bool{false, true} {
		fset := token.NewFileSet()
		f := fset.AddFile("/src/a.go", -1, 100)
		f.SetLines([]int{0, 40})
		p := &Program{fset: fset, opts: Options{LineLocs: lineOnly}}
		m := p.newModule(nil)
		a, b := m.loc(f.Pos(3)), m.loc(f.Pos(9))
		defs := m.locDefs.String()
		if lineOnly {
			if a != b || strings.Count(defs, "\n") != 1 || !strings.Contains(defs, ":1:1)") {
				t.Errorf("line only: refs %q %q, defs:\n%s", a, b, defs)
			}
		} else if a == b || !strings.Contains(defs, ":1:4)") || !strings.Contains(defs, ":1:10)") {
			t.Errorf("columns: refs %q %q, defs:\n%s", a, b, defs)
		}
	}
}

package codegen

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Source locations. Every op of a function and the function
// itself carry loc("/abs/file.go":line:col) through #locN aliases defined
// after the module, so the backend can emit DWARF line tables and
// marmot-pcln can build the pc -> function, file:line table for
// tracebacks. Instructions without a position take the last position seen
// in the function. MARMOT_NO_LOCS=1 turns locations off.

var locsOff = os.Getenv("MARMOT_NO_LOCS") != ""

// loc returns the " loc(#locN)" suffix for pos, or "".
func (m *moduleGen) loc(pos token.Pos) string {
	if locsOff || !pos.IsValid() {
		return ""
	}
	p := m.p.fset.Position(pos)
	if p.Filename == "" {
		return ""
	}
	key := fmt.Sprintf("%q:%d:%d", m.p.locFile(p.Filename), p.Line, p.Column)
	n, ok := m.locs[key]
	if !ok {
		if m.locs == nil {
			m.locs = map[string]int{}
		}
		n = len(m.locs)
		m.locs[key] = n
		fmt.Fprintf(&m.locDefs, "#loc%d = loc(%s)\n", n, key)
	}
	return fmt.Sprintf(" loc(#loc%d)", n)
}

// opLoc is the location suffix for the op being emitted.
func (f *funcGen) opLoc() string {
	if f.curPos.IsValid() {
		f.lastPos = f.curPos
	}
	return f.m.loc(f.lastPos)
}

// locFile maps a file in the merged GOROOT (a symlink) to the file it
// links to, so tracebacks name $GOROOT/src/... or lib/src/... as the
// source really is.
func (p *Program) locFile(name string) string {
	if p.opts.MergedGOROOT == "" || !strings.HasPrefix(name, p.opts.MergedGOROOT+string(os.PathSeparator)) {
		return name
	}
	p.locMu.Lock()
	defer p.locMu.Unlock()
	if r, ok := p.locFiles[name]; ok {
		return r
	}
	r := name
	if t, err := filepath.EvalSymlinks(name); err == nil {
		r = t
	}
	if p.locFiles == nil {
		p.locFiles = map[string]string{}
	}
	p.locFiles[name] = r
	return r
}

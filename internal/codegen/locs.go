package codegen

import (
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Source locations. Every op of a function and the function
// itself carry loc("/abs/file.go":line:col) through #locN aliases defined
// before the module, so the backend can emit DWARF line tables and
// marmot-pcln can build the pc -> function, file:line table for
// tracebacks. Instructions without a position take the last position seen
// in the function. MARMOT_NO_LOCS=1 turns locations off.

var locsOff = os.Getenv("MARMOT_NO_LOCS") != ""

// loc returns the " loc(#locN)" suffix for pos, or "".
func (m *moduleGen) loc(pos token.Pos) string {
	if locsOff || !pos.IsValid() {
		return ""
	}
	if pos == m.locLast {
		return m.locLastRef
	}
	if n, ok := m.locPos[pos]; ok {
		m.locLast, m.locLastRef = pos, m.locRef[n]
		return m.locLastRef
	}
	tf := m.locTF
	if tf == nil || int(pos) < tf.Base() || int(pos) > tf.Base()+tf.Size() {
		if tf = m.p.fset.File(pos); tf == nil {
			return ""
		}
		m.locTF = tf
	}
	if m.p.opts.Separate && !m.locAllowed(tf) {
		return ""
	}
	p := tf.Position(pos)
	if p.Filename == "" {
		return ""
	}
	if p.Filename != m.locName {
		q, ok := m.locFiles[p.Filename]
		if !ok {
			q = strconv.Quote(m.p.locFile(p.Filename))
			if m.locFiles == nil {
				m.locFiles = map[string]string{}
			}
			m.locFiles[p.Filename] = q
		}
		m.locName, m.locQuoted = p.Filename, q
	}
	key := locKey{m.locQuoted, p.Line, p.Column}
	n, ok := m.locs[key]
	if !ok {
		if m.locs == nil {
			m.locs = map[locKey]int{}
			m.locPos = map[token.Pos]int{}
		}
		n = len(m.locs)
		m.locs[key] = n
		ref := " loc(#loc" + strconv.Itoa(n) + ")"
		m.locRef = append(m.locRef, ref)
		d := &m.locDefs
		d.WriteString(ref[5 : len(ref)-1])
		d.WriteString(" = loc(")
		d.WriteString(key.file)
		d.WriteByte(':')
		d.WriteString(strconv.Itoa(key.line))
		d.WriteByte(':')
		d.WriteString(strconv.Itoa(key.col))
		d.WriteString(")\n")
	}
	m.locPos[pos] = n
	m.locLast, m.locLastRef = pos, m.locRef[n]
	return m.locLastRef
}

// locAllowed reports whether a module of separate compilation may carry
// positions in tf: its own package's files and those of packages whose API
// exposes generic code, whose source is in the module's key (instances of
// it are compiled here). A position elsewhere (a wrapper of another package's
// method) would go stale when that package's lines move but not its API.
func (m *moduleGen) locAllowed(tf *token.File) bool {
	pkg := m.p.locPkgs[tf]
	if pkg == nil {
		return false
	}
	return (m.pkg != nil && pkg == m.pkg.Pkg) || m.p.genericPkg[pkg]
}

// locKey is a location: quoted file name, line and column.
type locKey struct {
	file      string
	line, col int
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
	if r, ok := p.locFiles.Load(name); ok {
		return r.(string)
	}
	r := name
	if t, err := filepath.EvalSymlinks(name); err == nil {
		r = t
	}
	p.locFiles.Store(name, r)
	return r
}

package diag

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// srcFile is a loaded source file.
type srcFile struct {
	path  string
	data  []byte
	lines []int // byte offset of the start of each line
	fset  *token.FileSet
	ast   *ast.File // nil when the file does not parse
	tf    *token.File
}

// srcPkg is the set of files of one package directory, parsed.
type srcPkg struct {
	dir   string
	name  string
	files []*srcFile
}

// Sources caches files and packages read while enriching diagnostics.
type Sources struct {
	files map[string]*srcFile
	pkgs  map[string]*srcPkg
	std   *stdIndex
	// GOROOT for the standard library index; found lazily when empty.
	GOROOT string
}

// NewSources returns an empty cache.
func NewSources() *Sources {
	return &Sources{files: map[string]*srcFile{}, pkgs: map[string]*srcPkg{}}
}

func (s *Sources) file(path string) *srcFile {
	if f, ok := s.files[path]; ok {
		return f
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.files[path] = nil
		return nil
	}
	f := &srcFile{path: path, data: data, fset: token.NewFileSet()}
	f.lines = append(f.lines, 0)
	for i, c := range data {
		if c == '\n' {
			f.lines = append(f.lines, i+1)
		}
	}
	if strings.HasSuffix(path, ".go") {
		f.ast, _ = parser.ParseFile(f.fset, path, data, parser.ParseComments|parser.SkipObjectResolution)
		if f.ast != nil {
			f.tf = f.fset.File(f.ast.Pos())
		}
	}
	s.files[path] = f
	return f
}

// line returns the text of line n (1-based) without the newline.
func (f *srcFile) line(n int) (string, bool) {
	if f == nil || n < 1 || n > len(f.lines) {
		return "", false
	}
	start := f.lines[n-1]
	end := len(f.data)
	if n < len(f.lines) {
		end = f.lines[n] - 1
	}
	if start > end {
		return "", false
	}
	return strings.TrimSuffix(string(f.data[start:end]), "\r"), true
}

// offset converts a line and column to a byte offset.
func (f *srcFile) offset(line, col int) int {
	if line < 1 || line > len(f.lines) {
		return -1
	}
	if col < 1 {
		col = 1
	}
	o := min(f.lines[line-1] + col - 1, len(f.data))
	return o
}

// pos converts a byte offset to a line and column.
func (f *srcFile) pos(off int) (line, col int) {
	i := max(sort.Search(len(f.lines), func(i int) bool { return f.lines[i] > off }) - 1, 0)
	return i + 1, off - f.lines[i] + 1
}

// tokenPos returns the go/token position of a line and column, or NoPos.
func (f *srcFile) tokenPos(line, col int) token.Pos {
	if f.tf == nil {
		return token.NoPos
	}
	o := f.offset(line, col)
	if o < 0 || o > f.tf.Size() {
		return token.NoPos
	}
	return f.tf.Pos(o)
}

// spanOf returns the span of an AST node.
func (f *srcFile) spanOf(n ast.Node) Span {
	p, e := f.fset.Position(n.Pos()), f.fset.Position(n.End())
	return Span{File: f.path, Line: p.Line, Col: p.Column, EndLine: e.Line, EndCol: e.Column}
}

// text returns the source text of a node.
func (f *srcFile) text(n ast.Node) string {
	a, b := f.fset.Position(n.Pos()).Offset, f.fset.Position(n.End()).Offset
	if a < 0 || b > len(f.data) || a > b {
		return ""
	}
	return string(f.data[a:b])
}

// tokenEnd returns the end column of the token that starts at line:col. For
// an identifier followed by a selector (pkg.Name) it covers the whole
// qualified name.
func (f *srcFile) tokenEnd(line, col int) (int, int) {
	o := f.offset(line, col)
	if o < 0 || o >= len(f.data) {
		return line, col
	}
	var sc scanner.Scanner
	fs := token.NewFileSet()
	tf := fs.AddFile("", -1, len(f.data)-o)
	sc.Init(tf, f.data[o:], nil, scanner.ScanComments)
	_, tok, lit := sc.Scan()
	if tok == token.EOF {
		return line, col
	}
	n := len(lit)
	if lit == "" || tok == token.SEMICOLON && lit == "\n" {
		n = len(tok.String())
	}
	if tok == token.IDENT {
		// pkg.Name
		rest := f.data[o+n:]
		if len(rest) > 1 && rest[0] == '.' && isIdentStart(rest[1]) {
			j := 1
			for j < len(rest) && isIdentByte(rest[j]) {
				j++
			}
			n += j
		}
	}
	return f.pos(o + n)
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentByte(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// pkgOf parses every Go file of the package that contains path. Test files
// are included only when path is a test file.
func (s *Sources) pkgOf(path string) *srcPkg {
	dir := filepath.Dir(path)
	test := strings.HasSuffix(path, "_test.go")
	key := dir
	if test {
		key += "\x00test"
	}
	if p, ok := s.pkgs[key]; ok {
		return p
	}
	self := s.file(path)
	p := &srcPkg{dir: dir}
	if self != nil && self.ast != nil {
		p.name = self.ast.Name.Name
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") {
			continue
		}
		if strings.HasSuffix(n, "_test.go") && !test {
			continue
		}
		f := s.file(filepath.Join(dir, n))
		if f == nil || f.ast == nil || p.name != "" && f.ast.Name.Name != p.name {
			continue
		}
		p.files = append(p.files, f)
	}
	s.pkgs[key] = p
	return p
}

// enclosingFunc returns the function declaration or literal around pos.
func enclosingFunc(f *srcFile, pos token.Pos) *ast.FuncDecl {
	if f.ast == nil || !pos.IsValid() {
		return nil
	}
	for _, d := range f.ast.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Pos() <= pos && pos < fd.End() {
			return fd
		}
	}
	return nil
}

// signature returns the one line signature of a function declaration.
func signature(f *srcFile, fd *ast.FuncDecl) string {
	end := fd.Type.End()
	start := fd.Pos()
	a, b := f.fset.Position(start).Offset, f.fset.Position(end).Offset
	if a < 0 || b > len(f.data) || a >= b {
		return ""
	}
	return oneLine(string(f.data[a:b]))
}

// oneLine collapses white space runs (including newlines) to one space.
func oneLine(s string) string {
	var b bytes.Buffer
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// pathNode returns the innermost nodes that contain pos, outermost first.
func pathNodes(f *ast.File, pos token.Pos) []ast.Node {
	var path []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if n.Pos() <= pos && pos < n.End() {
			path = append(path, n)
			return true
		}
		return false
	})
	return path
}

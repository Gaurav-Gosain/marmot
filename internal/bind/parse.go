// Package bind generates idiomatic Go bindings (plain cgo files) from C
// headers: marmot bind.
//
// The header is read through clang: its JSON AST gives the functions,
// records, enums and typedefs with their doc comments, and clang -E -dD
// gives the #define constants. Only declarations of the header itself (and
// of headers named in the config) are bound.
package bind

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Header is what bind knows of a C header.
type Header struct {
	Funcs   []*Func
	Records map[string]*Record // by tag name ("struct foo" -> foo) and typedef name
	Enums   []*Enum
	Typedef map[string]Type // typedef name -> the type it names
	Macros  []*Macro
	// PtrHandles are typedefs of a pointer to a struct (typedef struct
	// gzFile_s *gzFile): handle types, in order.
	PtrHandles []string
	HandleDoc  map[string]string
}

// Type is a C type as clang spells it, with its desugared form.
type Type struct {
	Q string // qualType, e.g. "const char *", "sqlite3 **", "uLong"
	D string // desugaredQualType, or Q
}

// Func is a function declaration.
type Func struct {
	Name   string
	Ret    Type
	Params []Param
	Doc    string
	Line   int // line in the header (after trailingDocs)
	// Variadic: cgo cannot call it.
	Variadic bool
}

// Param is a function parameter.
type Param struct {
	Name string
	Type Type
}

// Record is a struct or union.
type Record struct {
	Tag      string // "struct" or "union"
	Name     string // tag name, or the typedef name of an anonymous record
	Typedef  string // a typedef naming it, if any
	Complete bool
	Fields   []Param
	Doc      string
}

// Enum is an enum with its constants.
type Enum struct {
	Name   string // tag or typedef name; "" for an anonymous enum
	Consts []EnumConst
	Doc    string
}

// EnumConst is one enum constant.
type EnumConst struct {
	Name  string
	Value int64
	Doc   string
}

// Macro is a #define with a value bind can express in Go.
type Macro struct {
	Name string
	Expr string // the C text of the value
	Doc  string
}

type node struct {
	Kind  string          `json:"kind"`
	Name  string          `json:"name"`
	Loc   json.RawMessage `json:"loc"`
	Range struct {
		Begin json.RawMessage `json:"begin"`
	} `json:"range"`
	Type struct {
		Q string `json:"qualType"`
		D string `json:"desugaredQualType"`
	} `json:"type"`
	TagUsed            string  `json:"tagUsed"`
	CompleteDefinition bool    `json:"completeDefinition"`
	Text               string  `json:"text"`
	Value              string  `json:"value"`
	Inner              []*node `json:"inner"`
	OwnedTagDecl       *node   `json:"ownedTagDecl"`
	IsImplicit         bool    `json:"isImplicit"`
	StorageClass       string  `json:"storageClass"`
	Decl               *node   `json:"decl"`
	ID                 string  `json:"id"`
}

type locInfo struct {
	File         string   `json:"file"`
	Line         int      `json:"line"`
	Offset       int      `json:"offset"`
	SpellingLoc  *locInfo `json:"spellingLoc"`
	ExpansionLoc *locInfo `json:"expansionLoc"`
}

// Parse reads header (with cflags) through clang. files are the headers
// whose declarations are bound (the header itself when empty).
func Parse(clang, header string, cflags, files []string) (*Header, error) {
	abs, err := filepath.Abs(header)
	if err == nil {
		if _, serr := os.Stat(abs); serr == nil {
			header = abs
		}
	}
	src := fmt.Sprintf("#include %q\n", header)
	if !filepath.IsAbs(header) {
		src = fmt.Sprintf("#include <%s>\n", header)
	}
	args := append([]string{"-Xclang", "-ast-dump=json", "-fsyntax-only", "-fparse-all-comments", "-x", "c", "-"}, cflags...)
	cmd := exec.Command(clang, args...)
	cmd.Stdin = strings.NewReader(src)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("clang: %v\n%s", err, stderr.String())
	}
	var root node
	if err := json.Unmarshal(out, &root); err != nil {
		return nil, fmt.Errorf("clang AST: %v", err)
	}
	want := map[string]bool{}
	for _, f := range files {
		want[filepath.Base(f)] = true
	}
	h := &Header{Records: map[string]*Record{}, Typedef: map[string]Type{}}
	cur := ""
	mainFile := ""
	pendingEnum := map[string]*Enum{}
	for _, n := range root.Inner {
		f, line := nodeFile(n, &cur)
		ours := f != "" && (f == header || filepath.Base(f) == filepath.Base(header) || want[filepath.Base(f)])
		if n.Kind == "TypedefDecl" && !ours {
			// Typedefs of other headers still name the types used here
			// (zlib's uLong comes from zconf.h).
			h.Typedef[n.Name] = Type{n.Type.Q, or(n.Type.D, n.Type.Q)}
			continue
		}
		if !ours || n.IsImplicit {
			continue
		}
		if mainFile == "" && filepath.Base(f) == filepath.Base(header) {
			mainFile = f // the header as clang found it (on the include path)
		}
		switch n.Kind {
		case "FunctionDecl":
			if n.StorageClass == "static" {
				continue // static inline helpers have no symbol
			}
			fn := &Func{Name: n.Name, Ret: retType(n.Type.Q), Doc: docOf(n), Line: line,
				Variadic: strings.HasSuffix(strings.TrimSpace(n.Type.Q), "...)")}
			for _, p := range n.Inner {
				if p.Kind == "ParmVarDecl" {
					fn.Params = append(fn.Params, Param{Name: p.Name, Type: Type{p.Type.Q, or(p.Type.D, p.Type.Q)}})
				}
			}
			h.Funcs = append(h.Funcs, fn)
		case "RecordDecl":
			r := recordOf(n)
			if r.Name == "" {
				continue // named by a typedef below
			}
			if old := h.Records[r.Name]; old != nil && old.Complete && !r.Complete {
				continue
			}
			h.Records[r.Name] = r
		case "TypedefDecl":
			t := Type{n.Type.Q, or(n.Type.D, n.Type.Q)}
			h.Typedef[n.Name] = t
			if owned := ownedTag(n); owned != nil {
				switch owned.Kind {
				case "RecordDecl":
					r := recordOf(owned)
					if r.Name == "" {
						r.Name = n.Name
					}
					r.Typedef = n.Name
					if r.Doc == "" {
						r.Doc = docOf(n)
					}
					h.Records[n.Name] = r
					if r.Name != n.Name {
						h.Records[r.Name] = r
					}
				case "EnumDecl":
					if e := pendingEnum[owned.ID]; e != nil && e.Name == "" {
						e.Name = n.Name
					}
				}
			} else if (strings.HasPrefix(t.Q, "struct ") || strings.HasPrefix(t.Q, "union ")) && strings.HasSuffix(strings.TrimSpace(t.Q), "*") {
				// typedef struct foo *foo_t: an opaque handle type.
				h.PtrHandles = append(h.PtrHandles, n.Name)
				if h.HandleDoc == nil {
					h.HandleDoc = map[string]string{}
				}
				h.HandleDoc[n.Name] = docOf(n)
			} else if strings.HasPrefix(t.Q, "struct ") || strings.HasPrefix(t.Q, "union ") {
				// typedef struct foo foo; (the record may be incomplete)
				tag := strings.Fields(t.Q)[1]
				r := h.Records[tag]
				if r == nil {
					r = &Record{Tag: strings.Fields(t.Q)[0], Name: tag}
					h.Records[tag] = r
				}
				r.Typedef = n.Name
				if r.Doc == "" {
					r.Doc = docOf(n)
				}
				h.Records[n.Name] = r
			}
		case "EnumDecl":
			e := &Enum{Name: n.Name, Doc: docOf(n)}
			next := int64(0) // a constant without a value is the previous one plus 1
			for _, c := range n.Inner {
				if c.Kind != "EnumConstantDecl" {
					continue
				}
				v, ok := constValue(c)
				if !ok {
					v = next
				}
				next = v + 1
				e.Consts = append(e.Consts, EnumConst{Name: c.Name, Value: v, Doc: docOf(c)})
			}
			h.Enums = append(h.Enums, e)
			pendingEnum[n.ID] = e
		}
	}
	trailingDocs(h, or(mainFile, header))
	macros, err := readMacros(clang, src, header, cflags, want)
	if err != nil {
		return nil, err
	}
	h.Macros = macros
	return h, nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// retType is the result type of a function type "R (P1, P2)".
func retType(ft string) Type {
	depth := 0
	for i := len(ft) - 1; i >= 0; i-- {
		switch ft[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				q := strings.TrimSpace(ft[:i])
				return Type{q, q}
			}
		}
	}
	return Type{ft, ft}
}

// nodeFile is the file and line of a top-level declaration. clang writes a
// file only when it changes, so cur carries it from earlier nodes.
func nodeFile(n *node, cur *string) (string, int) {
	var l locInfo
	line := 0
	for _, raw := range []json.RawMessage{n.Range.Begin, n.Loc} {
		if len(raw) == 0 {
			continue
		}
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		for _, x := range []*locInfo{l.ExpansionLoc, &l, l.SpellingLoc} {
			if x != nil && x.File != "" {
				*cur = x.File
				break
			}
		}
		// clang leaves out lines equal to the last one it printed, but
		// always writes the byte offset: keep the offset (plus 1, so that
		// 0 means unknown); trailingDocs turns it into a line.
		switch {
		case l.ExpansionLoc != nil:
			line = l.ExpansionLoc.Offset + 1
		default:
			line = l.Offset + 1
		}
	}
	return *cur, line
}

func recordOf(n *node) *Record {
	r := &Record{Tag: or(n.TagUsed, "struct"), Name: n.Name, Complete: n.CompleteDefinition, Doc: docOf(n)}
	for _, f := range n.Inner {
		if f.Kind == "FieldDecl" {
			r.Fields = append(r.Fields, Param{Name: f.Name, Type: Type{f.Type.Q, or(f.Type.D, f.Type.Q)}})
		}
	}
	return r
}

func ownedTag(n *node) *node {
	if n.OwnedTagDecl != nil {
		return n.OwnedTagDecl
	}
	for _, c := range n.Inner {
		if c.OwnedTagDecl != nil {
			return c.OwnedTagDecl
		}
		for _, cc := range c.Inner {
			if cc.OwnedTagDecl != nil {
				return cc.OwnedTagDecl
			}
		}
	}
	return nil
}

func constValue(c *node) (int64, bool) {
	var find func(*node) (int64, bool)
	find = func(n *node) (int64, bool) {
		if n.Kind == "ConstantExpr" && n.Value != "" {
			v, err := strconv.ParseInt(n.Value, 10, 64)
			return v, err == nil
		}
		for _, x := range n.Inner {
			if v, ok := find(x); ok {
				return v, true
			}
		}
		return 0, false
	}
	return find(c)
}

// docOf is the text of the comment attached to n.
func docOf(n *node) string {
	var lines []string
	var walk func(*node)
	walk = func(x *node) {
		if x.Kind == "TextComment" {
			lines = append(lines, x.Text)
		}
		if x.Kind == "ParagraphComment" && len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		for _, c := range x.Inner {
			walk(c)
		}
	}
	for _, c := range n.Inner {
		if c.Kind == "FullComment" {
			walk(c)
		}
	}
	return cleanDoc(lines)
}

// cleanDoc trims comment decoration (leading *, //, /*) and blank runs.
func cleanDoc(lines []string) string {
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "/*")
		l = strings.TrimSuffix(l, "*/")
		l = strings.TrimLeft(l, "*/!<")
		l = strings.TrimSpace(l)
		if l == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// trailingDocs gives functions the comment that follows their declaration
// on the same line (raylib's style: RLAPI void InitWindow(...); //
// Initialize window and OpenGL context). It wins over a comment above,
// which in such headers is a section title.
func trailingDocs(h *Header, header string) {
	data, err := os.ReadFile(header)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, fn := range h.Funcs {
		// fn.Line holds the byte offset of the name plus 1 (nodeFile).
		if fn.Line <= 0 || fn.Line > len(data) {
			continue
		}
		fn.Line = strings.Count(string(data[:fn.Line-1]), "\n") + 1
		// The declaration may span lines: look from its first line to the
		// line ending in ';'.
		for i := fn.Line - 1; i < len(lines) && i < fn.Line+5; i++ {
			l := lines[i]
			if j := strings.Index(l, ";"); j >= 0 {
				if k := strings.Index(l[j:], "//"); k >= 0 {
					fn.Doc = strings.TrimSpace(l[j+k+2:])
				}
				break
			}
		}
	}
}

// readMacros lists the object-like #defines of the bound headers whose
// value is a literal or an expression of literals and other such macros.
func readMacros(clang, src, header string, cflags []string, want map[string]bool) ([]*Macro, error) {
	args := append([]string{"-E", "-dD", "-x", "c", "-"}, cflags...)
	cmd := exec.Command(clang, args...)
	cmd.Stdin = strings.NewReader(src)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("clang -E: %v", err)
	}
	var ms []*Macro
	seen := map[string]bool{}
	cur := ""
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "# ") {
			// # LINE "file" flags
			if i := strings.IndexByte(l, '"'); i >= 0 {
				if j := strings.IndexByte(l[i+1:], '"'); j >= 0 {
					cur = l[i+1 : i+1+j]
				}
			}
			continue
		}
		if !strings.HasPrefix(l, "#define ") {
			continue
		}
		if !(cur == header || filepath.Base(cur) == filepath.Base(header) || want[filepath.Base(cur)]) {
			continue
		}
		rest := strings.TrimPrefix(l, "#define ")
		name, val, _ := strings.Cut(rest, " ")
		if strings.Contains(name, "(") || seen[name] || strings.HasPrefix(name, "_") {
			continue // function-like, repeated, or reserved
		}
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		seen[name] = true
		ms = append(ms, &Macro{Name: name, Expr: val})
	}
	return ms, nil
}

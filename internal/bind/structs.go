package bind

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// C structs whose fields all have a Go type of the same size and
// alignment become Go structs (Vector2{X, Y float32}); values cross by
// reinterpreting the memory, and a compile time check in the output keeps
// the sizes equal. Other complete structs stay aliases of the C struct.

// fieldTypes are exact size Go types of C field types (LP64).
var fieldTypes = map[string]string{
	"char": "byte", "signed char": "int8", "unsigned char": "uint8",
	"short": "int16", "unsigned short": "uint16",
	"int": "int32", "unsigned int": "uint32",
	"long": "int64", "unsigned long": "uint64",
	"long long": "int64", "unsigned long long": "uint64",
	"float": "float32", "double": "float64", "_Bool": "bool", "bool": "bool",
	"int8_t": "int8", "int16_t": "int16", "int32_t": "int32", "int64_t": "int64",
	"uint8_t": "uint8", "uint16_t": "uint16", "uint32_t": "uint32", "uint64_t": "uint64",
	"size_t": "uint64", "ssize_t": "int64", "intptr_t": "int64", "uintptr_t": "uintptr",
}

type goStruct struct {
	name   string
	c      string // cgo spelling: C.<c>
	fields []string
	doc    string
}

var reArray = regexp.MustCompile(`^(.*\S)\s*\[(\d+)\]$`)

// planStructs decides which complete records become Go structs.
func (g *gen) planStructs() {
	g.structs = map[string]*goStruct{}
	state := map[*Record]int{} // 1 in progress, 2 done (ok or not)
	var plan func(r *Record) *goStruct
	plan = func(r *Record) *goStruct {
		switch state[r] {
		case 1:
			return nil // a struct containing itself by value cannot be
		case 2:
			return g.structs[r.Name]
		}
		state[r] = 1
		defer func() { state[r] = 2 }()
		if !r.Complete || r.Tag != "struct" || len(r.Fields) == 0 {
			return nil
		}
		gs := &goStruct{name: g.aliases[r.Name], c: cName(r), doc: r.Doc}
		if gs.name == "" {
			return nil
		}
		seen := map[string]bool{}
		for _, f := range r.Fields {
			ft, ok := g.fieldType(f.Type, plan)
			name := exportField(f.Name)
			if !ok || name == "" || seen[name] {
				g.note("struct %s: field %s of type %s has no Go equivalent; the struct stays the C struct", cName(r), f.Name, f.Type.Q)
				return nil
			}
			seen[name] = true
			gs.fields = append(gs.fields, fmt.Sprintf("%s %s // %s", name, ft, f.Name))
		}
		g.structs[r.Name] = gs
		if r.Typedef != "" {
			g.structs[r.Typedef] = gs
		}
		return gs
	}
	for _, k := range sortedKeys(g.h.Records) {
		plan(g.h.Records[k])
	}
	// Typedefs naming a mapped struct by another name (Texture2D for
	// Texture) are aliases of it.
	g.structAlias = map[string]string{}
	for _, td := range sortedKeys(g.h.Typedef) {
		t := g.h.Typedef[td]
		if g.structs[td] != nil || strings.Contains(t.Q, "*") {
			continue
		}
		target := strings.TrimPrefix(strings.TrimSpace(t.Q), "struct ")
		if gs := g.structs[target]; gs != nil && gs.name != td {
			g.structs[td] = gs
			g.structAlias[g.typeName(td, td)] = gs.name
		}
	}
}

// fieldType is the Go type of a struct field of C type t, or false.
func (g *gen) fieldType(t Type, plan func(*Record) *goStruct) (string, bool) {
	q := strings.TrimSpace(strings.ReplaceAll(t.Q, "const ", ""))
	if m := reArray.FindStringSubmatch(q); m != nil {
		n, _ := strconv.Atoi(m[2])
		et, ok := g.fieldType(Type{m[1], m[1]}, plan)
		return fmt.Sprintf("[%d]%s", n, et), ok
	}
	if strings.HasSuffix(q, "*") {
		inner := strings.TrimSpace(strings.TrimSuffix(q, "*"))
		switch {
		case inner == "void":
			g.used["unsafe"] = true
			return "unsafe.Pointer", true
		case fieldTypes[inner] != "":
			return "*" + fieldTypes[inner], true
		}
		if r := g.recordNamed(inner); r != nil {
			if gs := g.structs[r.Name]; gs != nil {
				return "*" + gs.name, true
			}
			if state := plan(r); state != nil {
				return "*" + state.name, true
			}
		}
		g.used["unsafe"] = true
		return "unsafe.Pointer", true // a pointer to something unmapped: same size
	}
	if ft := fieldTypes[q]; ft != "" {
		return ft, true
	}
	if strings.HasPrefix(q, "enum ") || g.enums[q] != "" {
		return "int32", true
	}
	if r := g.recordNamed(q); r != nil {
		if gs := plan(r); gs != nil {
			return gs.name, true
		}
		return "", false
	}
	// A typedef of a scalar, array or another struct.
	if td, ok := g.h.Typedef[q]; ok {
		d := or(td.D, td.Q)
		if d != q {
			return g.fieldType(Type{d, d}, plan)
		}
	}
	return "", false
}

func (g *gen) recordNamed(q string) *Record {
	q = strings.TrimSpace(q)
	for _, p := range []string{"struct ", "union "} {
		q = strings.TrimPrefix(q, p)
	}
	if r := g.h.Records[q]; r != nil {
		return r
	}
	if td, ok := g.h.Typedef[q]; ok && !strings.Contains(td.Q, "*") {
		n := strings.TrimPrefix(strings.TrimSpace(td.Q), "struct ")
		if n != q {
			return g.h.Records[n]
		}
	}
	return nil
}

func exportField(n string) string {
	if n == "" {
		return ""
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

// structOf is the Go struct of the C type t names (no pointers).
func (g *gen) structOf(t ctype) *goStruct {
	for _, n := range []string{t.orig, t.q, strings.TrimPrefix(t.q, "struct "), t.base, strings.TrimPrefix(t.base, "struct ")} {
		if gs := g.structs[n]; gs != nil {
			return gs
		}
	}
	return nil
}

// emitStructs writes the Go structs the bound functions use, and the
// size checks.
func (g *gen) emitStructs() {
	done := map[string]bool{}
	var checks []string
	var emit func(gs *goStruct)
	emit = func(gs *goStruct) {
		if done[gs.name] {
			return
		}
		done[gs.name] = true
		g.doc(gs.name, "is the C struct "+gs.c+".", gs.doc)
		fmt.Fprintf(&g.body, "type %s struct {\n", gs.name)
		for _, f := range gs.fields {
			g.body.WriteString("\t" + f + "\n")
		}
		g.body.WriteString("}\n\n")
		checks = append(checks, fmt.Sprintf("\t_ = [1]struct{}{}[unsafe.Sizeof(%s{})-unsafe.Sizeof(C.%s{})]", gs.name, gs.c))
		// The structs it contains are emitted too.
		for _, f := range gs.fields {
			ft := strings.Fields(f)[1]
			ft = strings.TrimLeft(ft, "*[]0123456789")
			for _, other := range g.structs {
				if other.name == ft {
					emit(other)
				}
			}
		}
	}
	for _, k := range sortedKeys(g.structs) {
		gs := g.structs[k]
		if g.usedType[gs.name] {
			emit(gs)
		}
	}
	for _, a := range sortedKeys(g.structAlias) {
		if g.usedType[a] {
			fmt.Fprintf(&g.body, "// %s is %s.\ntype %s = %s\n\n", a, g.structAlias[a], a, g.structAlias[a])
			if gs := g.structs[g.structAlias[a]]; gs != nil {
				emit(gs)
			} else {
				for _, gs := range g.structs {
					if gs.name == g.structAlias[a] {
						emit(gs)
					}
				}
			}
		}
	}
	if len(checks) > 0 {
		g.used["unsafe"] = true
		g.body.WriteString("// The Go structs have the size of the C structs (a mismatch fails to compile).\nvar (\n")
		g.body.WriteString(strings.Join(checks, "\n") + "\n)\n\n")
	}
}

var reCompound = regexp.MustCompile(`^(?:CLITERAL\((\w+)\)|\((\w+)\))\s*\{(.*)\}$`)

// compoundMacro turns #define X CLITERAL(T){a, b} (or (T){a, b}) into a Go
// composite literal of the mapped struct T, or "".
func (g *gen) compoundMacro(e string) (lit, typ string) {
	m := reCompound.FindStringSubmatch(strings.TrimSpace(e))
	if m == nil {
		return "", ""
	}
	t := m[1] + m[2]
	gs := g.structs[t]
	if gs == nil {
		return "", ""
	}
	var vals []string
	for _, v := range strings.Split(m[3], ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		x := g.goExpr(v)
		if x == "" {
			return "", ""
		}
		vals = append(vals, x)
	}
	if len(vals) != len(gs.fields) {
		return "", ""
	}
	return fmt.Sprintf("%s{%s}", gs.name, strings.Join(vals, ", ")), gs.name
}

package bind

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// scalars maps desugared C types to Go types.
var scalars = map[string]string{
	"char": "byte", "signed char": "int8", "unsigned char": "byte",
	"short": "int16", "unsigned short": "uint16",
	"int": "int", "unsigned int": "uint",
	"long": "int", "unsigned long": "uint",
	"long long": "int64", "unsigned long long": "uint64",
	"float": "float32", "double": "float64", "_Bool": "bool", "bool": "bool",
	"int8_t": "int8", "int16_t": "int16", "int32_t": "int32", "int64_t": "int64",
	"uint8_t": "uint8", "uint16_t": "uint16", "uint32_t": "uint32", "uint64_t": "uint64",
	"size_t": "int", "ssize_t": "int", "ptrdiff_t": "int", "intptr_t": "int", "uintptr_t": "uintptr",
}

var reBufName = regexp.MustCompile(`(?i)(buf|data|bytes|src|dst|dest|out|in)`)

var reLenName = regexp.MustCompile(`(?i)^(n|len|length|size|nbyte|nbytes|bytes|count|.*len|.*size|.*length)$`)

// ctype is a C type reduced for mapping.
type ctype struct {
	orig   string // as spelled, without const (a pointer typedef keeps its name)
	q      string // as spelled, without const
	base   string // desugared, without const and pointers
	ptrs   int
	isCost bool // the pointed-to type is const
}

func parseType(t Type, typedefs map[string]Type) ctype {
	q := strings.TrimSpace(t.Q)
	d := strings.TrimSpace(t.D)
	c := ctype{}
	isConst := strings.HasPrefix(q, "const ") || strings.Contains(q, " const ")
	c.q = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(q, "const ", ""), " const", ""))
	c.orig = c.q
	for strings.HasSuffix(c.q, "*") {
		c.q = strings.TrimSpace(strings.TrimSuffix(c.q, "*"))
		c.ptrs++
	}
	if d == q {
		// No desugared form (function results): resolve typedefs here.
		for i := 0; i < 8; i++ {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.ReplaceAll(d, "const ", "")), "*"))
			name = strings.TrimSpace(strings.TrimRight(name, "* "))
			td, ok := typedefs[name]
			if !ok || scalars[name] != "" {
				break
			}
			d = strings.Replace(d, name, or(td.D, td.Q), 1)
		}
	}
	base := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(d, "const ", ""), " const", ""))
	for strings.HasSuffix(base, "*") {
		base = strings.TrimSpace(strings.TrimSuffix(base, "*"))
	}
	// A typedef of a pointer (z_streamp) adds pointers.
	if c.ptrs == 0 {
		if dp := strings.Count(d, "*"); dp > 0 {
			c.ptrs = dp
			c.q = base
		}
	}
	// Follow typedefs of scalars (uLong -> unsigned long).
	for i := 0; i < 8; i++ {
		td, ok := typedefs[base]
		if !ok || scalars[base] != "" {
			break
		}
		base = strings.TrimSpace(strings.ReplaceAll(or(td.D, td.Q), "const ", ""))
	}
	c.base = base
	c.isCost = isConst
	return c
}

// ptrCast is the conversion of an unsafe.Pointer to the C pointer type of
// a parameter: its typedef (C.voidp) when it is spelled with one, else
// *C.T, or none for void *.
func ptrCast(t ctype) string {
	if !strings.Contains(t.orig, "*") && t.orig != t.base {
		return "C." + t.orig // a pointer typedef (voidp, Bytef *... spelled as a name)
	}
	if t.base == "void" {
		return "unsafe.Pointer"
	}
	return "(*C." + cSpell(ctype{q: t.q}) + ")"
}

// conv is how one parameter or result crosses.
type conv struct {
	goType string
	pre    []string // statements before the call
	arg    string   // the C argument
}

func (g *gen) emitFuncs() {
	for _, fn := range g.h.Funcs {
		if !g.cfg.included(fn.Name) {
			continue
		}
		if err := g.emitFunc(fn); err != "" {
			g.note("%s: not bound: %s", fn.Name, err)
		}
	}
}

func (g *gen) handleOf(t ctype) *handle {
	if hd := g.handles[t.orig]; hd != nil && hd.ptr {
		return hd
	}
	for _, n := range []string{t.q, strings.TrimPrefix(strings.TrimPrefix(t.q, "struct "), "union "), t.base,
		strings.TrimPrefix(strings.TrimPrefix(t.base, "struct "), "union ")} {
		if hd := g.handles[n]; hd != nil {
			return hd
		}
	}
	return nil
}

func (g *gen) aliasOf(t ctype) string {
	for _, n := range []string{t.q, strings.TrimPrefix(t.q, "struct "), t.base, strings.TrimPrefix(t.base, "struct ")} {
		if a := g.aliases[n]; a != "" {
			return a
		}
	}
	return ""
}

// goTypeName is the Go name of a mapped struct as the C code spells it:
// an alias (Texture2D) keeps its own name.
func (g *gen) goTypeName(t ctype, gs *goStruct) string {
	if a := g.typeName(t.orig, t.orig); g.structAlias[a] == gs.name {
		return a
	}
	return gs.name
}

func (g *gen) enumOf(t ctype) string {
	for _, n := range []string{t.q, strings.TrimPrefix(t.q, "enum "), strings.TrimPrefix(t.base, "enum ")} {
		if e := g.enums[n]; e != "" {
			return e
		}
	}
	return ""
}

// cSpell is the cgo spelling of the C type t names (no pointers).
func cSpell(t ctype) string {
	s := t.q
	for _, p := range []string{"struct ", "union ", "enum "} {
		if strings.HasPrefix(s, p) {
			return p[:len(p)-1] + "_" + strings.TrimPrefix(s, p)
		}
	}
	switch s {
	case "unsigned int":
		return "uint"
	case "unsigned char":
		return "uchar"
	case "signed char":
		return "schar"
	case "unsigned short":
		return "ushort"
	case "unsigned long":
		return "ulong"
	case "long long":
		return "longlong"
	case "unsigned long long":
		return "ulonglong"
	case "_Bool":
		return "_Bool"
	}
	return strings.ReplaceAll(s, " ", "")
}

func (g *gen) emitFunc(fn *Func) string {
	if fn.Variadic {
		return "variadic (cgo cannot call C functions with ...); write a C wrapper with fixed parameters"
	}
	status := matchAny(g.cfg.Status.Functions, fn.Name) && strings.TrimSpace(fn.Ret.Q) == "int"
	outs := g.cfg.Out[fn.Name]
	var params, results, pre, args, post []string
	var recv *handle
	var recvName string
	retValues := []string{}
	ps := fn.Params
	if len(ps) == 1 && strings.TrimSpace(ps[0].Type.Q) == "void" {
		ps = nil
	}
	used := map[string]bool{}
	pname := func(n string, i int) string {
		n = goIdent(n)
		if n == "" || used[n] {
			n = fmt.Sprintf("p%d", i)
		}
		used[n] = true
		return n
	}
	for i := 0; i < len(ps); i++ {
		p := ps[i]
		t := parseType(p.Type, g.h.Typedef)
		name := pname(p.Name, i)
		hd := g.handleOf(t)
		isOut := slices.Contains(outs, p.Name) || status && hd != nil && t.ptrs == 2
		switch {
		case isOut:
			if t.ptrs < 1 {
				return "out parameter " + p.Name + " is not a pointer"
			}
			inner := t
			inner.ptrs--
			if ohd := g.handleOf(inner); ohd != nil && inner.ptrs == 1 {
				pre = append(pre, fmt.Sprintf("var %s *C.%s", name, ohd.c))
				args = append(args, "&"+name)
				results = append(results, "*"+ohd.goName)
				retValues = append(retValues, fmt.Sprintf("wrap%s(%s)", ohd.goName, name))
			} else if inner.ptrs == 1 && (inner.base == "char" || inner.base == "unsigned char") && inner.isCost {
				// const char **: a string result the callee points into
				// memory it owns.
				pre = append(pre, fmt.Sprintf("var %s *C.%s", name, cSpell(ctype{q: inner.base})))
				args = append(args, "&"+name)
				results = append(results, "string")
				retValues = append(retValues, fmt.Sprintf("C.GoString((*C.char)(unsafe.Pointer(%s)))", name))
				g.used["unsafe"] = true
			} else if gt := scalars[inner.base]; gt != "" && inner.ptrs == 0 {
				pre = append(pre, fmt.Sprintf("var %s C.%s", name, cSpell(inner)))
				args = append(args, "&"+name)
				results = append(results, gt)
				retValues = append(retValues, fmt.Sprintf("%s(%s)", gt, name))
			} else {
				return "out parameter " + p.Name + " of type " + p.Type.Q + " is not supported"
			}
		case i == 0 && hd != nil && t.ptrs == 1 && !strings.HasSuffix(fn.Name, "_open"):
			recv, recvName = hd, "h"
			args = append(args, "h.c()")
		case hd != nil && t.ptrs == 1:
			params = append(params, name+" *"+hd.goName)
			args = append(args, name+".c()")
		case t.ptrs == 1 && (t.base == "char" || t.base == "unsigned char" || t.base == "void" || t.base == "uint8_t") &&
			!(t.base == "char" && t.isCost && !reBufName.MatchString(p.Name)) && i+1 < len(ps) && reLenName.MatchString(ps[i+1].Name) && scalars[parseType(ps[i+1].Type, g.h.Typedef).base] != "" &&
			parseType(ps[i+1].Type, g.h.Typedef).ptrs == 0:
			// (pointer, length) pair: a byte slice.
			lt := parseType(ps[i+1].Type, g.h.Typedef)
			params = append(params, name+" []byte")
			g.used["unsafe"] = true
			cast := ptrCast(t)
			if cast == "unsafe.Pointer" {
				pre = append(pre, fmt.Sprintf("%sp := unsafe.Pointer(unsafe.SliceData(%s))", name, name))
			} else {
				pre = append(pre, fmt.Sprintf("%sp := %s(unsafe.Pointer(unsafe.SliceData(%s)))", name, cast, name))
			}
			args = append(args, name+"p", fmt.Sprintf("C.%s(len(%s))", cSpell(lt), name))
			used[goIdent(ps[i+1].Name)] = true
			g.note("%s: %s and %s are taken as one []byte (pointer and length)", fn.Name, p.Name, ps[i+1].Name)
			i++
		case t.ptrs == 1 && t.isCost && fieldTypes[t.base] != "" && t.base != "char" && i+1 < len(ps) &&
			reLenName.MatchString(ps[i+1].Name) && parseType(ps[i+1].Type, g.h.Typedef).ptrs == 0 &&
			scalars[parseType(ps[i+1].Type, g.h.Typedef).base] != "":
			// (const T *array, count): a slice of the exact size Go type.
			lt := parseType(ps[i+1].Type, g.h.Typedef)
			et := fieldTypes[t.base]
			params = append(params, name+" []"+et)
			g.used["unsafe"] = true
			pre = append(pre, fmt.Sprintf("%sp := (*C.%s)(unsafe.Pointer(unsafe.SliceData(%s)))", name, cSpell(ctype{q: t.q}), name))
			args = append(args, name+"p", fmt.Sprintf("C.%s(len(%s))", cSpell(lt), name))
			used[goIdent(ps[i+1].Name)] = true
			g.note("%s: %s and %s are taken as one []%s (array and count)", fn.Name, p.Name, ps[i+1].Name, et)
			i++
		case t.ptrs == 1 && t.base == "char" && t.isCost:
			params = append(params, name+" string")
			g.used["unsafe"] = true
			pre = append(pre, fmt.Sprintf("c%s := C.CString(%s)\n\tdefer C.free(unsafe.Pointer(c%s))", name, name, name))
			args = append(args, "c"+name)
		case t.ptrs == 1 && t.base == "void":
			params = append(params, name+" unsafe.Pointer")
			g.used["unsafe"] = true
			if c := ptrCast(t); c != "unsafe.Pointer" {
				args = append(args, c+"("+name+")")
			} else {
				args = append(args, name)
			}
		case t.ptrs == 0 && g.structOf(t) != nil:
			gs := g.structOf(t)
			g.used["unsafe"] = true
			params = append(params, name+" "+g.goTypeName(t, gs))
			args = append(args, fmt.Sprintf("*(*C.%s)(unsafe.Pointer(&%s))", gs.c, name))
		case t.ptrs == 1 && g.structOf(t) != nil:
			gs := g.structOf(t)
			g.used["unsafe"] = true
			params = append(params, name+" *"+g.goTypeName(t, gs))
			args = append(args, fmt.Sprintf("(*C.%s)(unsafe.Pointer(%s))", gs.c, name))
		case t.ptrs == 1 && g.aliasOf(t) != "":
			params = append(params, name+" *"+g.aliasOf(t))
			args = append(args, name)
		case t.ptrs == 0 && g.enumOf(t) != "":
			params = append(params, name+" "+g.enumOf(t))
			args = append(args, fmt.Sprintf("C.%s(%s)", cSpell(t), name))
		case t.ptrs == 0 && scalars[t.base] != "":
			gt := scalars[t.base]
			params = append(params, name+" "+gt)
			if gt == "bool" {
				args = append(args, fmt.Sprintf("C._Bool(%s)", name))
			} else {
				args = append(args, fmt.Sprintf("C.%s(%s)", cSpell(t), name))
			}
		case t.ptrs == 1 && scalars[t.base] != "":
			return "parameter " + p.Name + " of type " + p.Type.Q + ": an out parameter (list it in [out]) or an array? not bound until bind.toml says"
		default:
			return "parameter " + p.Name + " of type " + p.Type.Q + " is not supported yet"
		}
	}
	// The result.
	rt := parseType(fn.Ret, g.h.Typedef)
	call := fmt.Sprintf("C.%s(%s)", fn.Name, strings.Join(args, ", "))
	var body []string
	body = append(body, pre...)
	rhd := g.handleOf(rt)
	switch {
	case strings.TrimSpace(fn.Ret.Q) == "void":
		body = append(body, call)
	case status:
		g.used["fmt"] = true
		body = append(body, "r := "+call, "if err := statusError(int(r), "+fmt.Sprintf("%q", fn.Name)+"); err != nil {\n\treturn "+zeroes(results)+"err\n}")
		results = append(results, "error")
		retValues = append(retValues, "nil")
	case rhd != nil && rt.ptrs == 1:
		results = append(results, "*"+rhd.goName)
		retValues = append(retValues, "wrap"+rhd.goName+"(r)")
		body = append(body, "r := "+call)
	case rt.ptrs == 1 && (rt.base == "char" || rt.base == "unsigned char"):
		owner, owned := g.cfg.Owned[fn.Name]
		switch {
		case owned:
			g.used["unsafe"] = true
			body = append(body, "r := "+call, fmt.Sprintf("defer C.%s(%s)", owner, g.freeArg(owner, "r")))
		case rt.isCost || slices.Contains(g.cfg.Borrowed, fn.Name):
			if !slices.Contains(g.cfg.Borrowed, fn.Name) {
				g.note("%s: returns %s; copied into a Go string and not freed (const). Put it in owned to free it", fn.Name, fn.Ret.Q)
			}
			body = append(body, "r := "+call)
		default:
			return "returns " + fn.Ret.Q + ": who frees it? list it in owned (with the free function) or borrowed"
		}
		results = append(results, "string")
		if rt.base == "char" {
			retValues = append(retValues, "C.GoString(r)")
		} else {
			g.used["unsafe"] = true
			retValues = append(retValues, "C.GoString((*C.char)(unsafe.Pointer(r)))")
		}
	case rt.ptrs == 1 && rt.base == "void":
		if _, owned := g.cfg.Owned[fn.Name]; !owned && !slices.Contains(g.cfg.Borrowed, fn.Name) {
			return "returns void *: who owns it? list it in owned or borrowed"
		}
		g.used["unsafe"] = true
		results = append(results, "unsafe.Pointer")
		retValues = append(retValues, "unsafe.Pointer(r)")
		body = append(body, "r := "+call)
	case rt.ptrs == 1 && g.aliasOf(rt) != "":
		if !slices.Contains(g.cfg.Borrowed, fn.Name) {
			return "returns " + fn.Ret.Q + ": who owns it? list it in owned or borrowed"
		}
		results = append(results, "*"+g.aliasOf(rt))
		retValues = append(retValues, "r")
		body = append(body, "r := "+call)
	case rt.ptrs == 0 && g.structOf(rt) != nil:
		gs := g.structOf(rt)
		g.used["unsafe"] = true
		gt := g.goTypeName(rt, gs)
		results = append(results, gt)
		retValues = append(retValues, fmt.Sprintf("*(*%s)(unsafe.Pointer(&r))", gt))
		body = append(body, "r := "+call)
	case rt.ptrs == 0 && g.enumOf(rt) != "":
		results = append(results, g.enumOf(rt))
		retValues = append(retValues, g.enumOf(rt)+"(r)")
		body = append(body, "r := "+call)
	case rt.ptrs == 0 && scalars[rt.base] != "":
		gt := scalars[rt.base]
		results = append(results, gt)
		retValues = append(retValues, gt+"(r)")
		body = append(body, "r := "+call)
	default:
		return "result type " + fn.Ret.Q + " is not supported yet"
	}
	body = append(body, post...)
	if len(retValues) > 0 {
		// Results in the order: values, then error.
		body = append(body, "return "+strings.Join(retValues, ", "))
	}
	// Name and receiver.
	name := g.name(fn.Name)
	if recv != nil && recv.close == fn.Name {
		name = "Close"
	}
	if name == "" {
		name = goName(fn.Name, nil)
	}
	scope := ""
	if recv != nil {
		scope = recv.goName
	}
	if g.methods[scope] == nil {
		g.methods[scope] = map[string]bool{}
	}
	if g.methods[scope][name] || scope == "" && g.names[name] {
		name = goName(fn.Name, nil)
		if g.methods[scope][name] || scope == "" && g.names[name] {
			return "its Go name " + name + " is taken"
		}
	}
	g.methods[scope][name] = true
	if scope == "" {
		g.names[name] = true
	}
	res := ""
	switch len(results) {
	case 0:
	case 1:
		res = " " + results[0]
	default:
		res = " (" + strings.Join(results, ", ") + ")"
	}
	// The types this function uses are emitted.
	for _, t := range append(append([]string{}, params...), results...) {
		f := strings.Fields(t)
		g.usedType[strings.TrimLeft(f[len(f)-1], "*[]")] = true
	}
	if recv != nil {
		g.usedType[recv.goName] = true
	}
	g.doc(name, "calls "+fn.Name+".", fn.Doc)
	if recv != nil {
		fmt.Fprintf(&g.body, "func (%s *%s) %s(%s)%s {\n", recvName, recv.goName, name, strings.Join(params, ", "), res)
	} else {
		fmt.Fprintf(&g.body, "func %s(%s)%s {\n", name, strings.Join(params, ", "), res)
	}
	for _, l := range body {
		g.body.WriteString("\t" + l + "\n")
	}
	g.body.WriteString("}\n\n")
	if status {
		g.hasStatus = true
	}
	return ""
}

// freeArg is v converted for the free function fn: unsafe.Pointer for a
// void * parameter, else as is.
func (g *gen) freeArg(fn, v string) string {
	for _, f := range g.h.Funcs {
		if f.Name == fn && len(f.Params) == 1 {
			if t := parseType(f.Params[0].Type, g.h.Typedef); t.base != "void" {
				return v
			}
		}
	}
	return "unsafe.Pointer(" + v + ")"
}

func zeroes(results []string) string {
	var z []string
	for _, r := range results {
		switch {
		case strings.HasPrefix(r, "*") || r == "unsafe.Pointer" || strings.HasPrefix(r, "[]"):
			z = append(z, "nil")
		case r == "string":
			z = append(z, `""`)
		case r == "bool":
			z = append(z, "false")
		case len(r) > 0 && r[0] >= 'A' && r[0] <= 'Z':
			z = append(z, r+"{}")
		default:
			z = append(z, "0")
		}
	}
	if len(z) == 0 {
		return ""
	}
	return strings.Join(z, ", ") + ", "
}

// goIdent is a parameter name usable in Go.
func goIdent(n string) string {
	switch n {
	case "", "_":
		return ""
	case "type", "func", "range", "map", "chan", "go", "select", "string", "len", "cap", "error", "default",
		"interface", "package", "import", "var", "const", "defer", "return", "struct", "switch", "case", "fallthrough",
		"break", "continue", "for", "if", "else", "goto", "h", "r", "err":
		return n + "_"
	}
	return n
}

// emitError writes the status check used by functions with a status
// result.
func (g *gen) emitError() {
	if !g.hasStatus {
		return
	}
	var oks []string
	for _, o := range g.cfg.Status.OK {
		if v, ok := g.macroVal[o]; ok {
			oks = append(oks, v)
		} else {
			oks = append(oks, o)
		}
	}
	if len(oks) == 0 {
		oks = []string{"0"}
	}
	msg := `""`
	if m := g.cfg.Status.Message; m != "" {
		msg = fmt.Sprintf("C.GoString(C.%s(C.int(e.Code)))", m)
	}
	fmt.Fprintf(&g.body, `// Error is a failure status returned by a C function.
type Error struct {
	Func string // the C function
	Code int    // its status
}

func (e *Error) Error() string {
	if m := %s; m != "" {
		return fmt.Sprintf("%%s: %%s (%%d)", e.Func, m, e.Code)
	}
	return fmt.Sprintf("%%s: status %%d", e.Func, e.Code)
}

func statusError(code int, fn string) error {
	switch code {
	case %s:
		return nil
	}
	return &Error{Func: fn, Code: code}
}
`, msg, strings.Join(oks, ", "))
}

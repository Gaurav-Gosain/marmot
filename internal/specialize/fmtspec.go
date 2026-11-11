package specialize

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strconv"
	"strings"
	"unicode/utf8"
)

// fmt specialization.
//
// A call to a fmt print function with a constant format string (Printf
// family) or with statically typed operands (Print and Println families) is
// replaced by a call to a generated function that performs the steps doPrintf,
// doPrint or doPrintln would perform, in the same order, through the entry
// points in lib/src/fmt/marmot_spec.go. Operands of basic types are formatted
// by the typed pp methods with no boxing; every other operand goes through
// printArg exactly as before. Formats with argument indexes, '*' widths, too
// few or too many operands, or no verb at the end are left alone.

// Flag bits, as in marmot_spec.go.
const (
	fWidPresent = 1 << iota
	fPrecPresent
	fMinus
	fPlus
	fSharp
	fSpace
	fZero
	fPlusV
	fSharpV
)

// fmtOp is one step of a parsed format: a literal or a verb on one operand.
type fmtOp struct {
	lit  string
	verb rune
	bits uint
	wid  int
	prec int
	arg  int
}

func tooLarge(x int) bool {
	const max int = 1e6
	return x > max || x < -max
}

// parsenum is fmt.parsenum.
func parsenum(s string, start, end int) (num int, isnum bool, newi int) {
	if start >= end {
		return 0, false, end
	}
	for newi = start; newi < end && '0' <= s[newi] && s[newi] <= '9'; newi++ {
		if tooLarge(num) {
			return 0, false, end
		}
		num = num*10 + int(s[newi]-'0')
		isnum = true
	}
	return
}

// parseFormat follows doPrintf over format for nargs operands. It returns
// false for any format whose behavior depends on more than the operand
// values: argument indexes, '*', missing or extra operands, a missing verb,
// or %w when wrapErrs is set.
func parseFormat(format string, nargs int, wrapErrs bool) ([]fmtOp, bool) {
	var ops []fmtOp
	var lit strings.Builder
	flushLit := func() {
		if lit.Len() > 0 {
			ops = append(ops, fmtOp{lit: lit.String()})
			lit.Reset()
		}
	}
	end := len(format)
	argNum := 0
	for i := 0; i < end; {
		lasti := i
		for i < end && format[i] != '%' {
			i++
		}
		if i > lasti {
			lit.WriteString(format[lasti:i])
		}
		if i >= end {
			break
		}
		i++ // skip %
		var minus, plus, sharp, space, zero bool
	flags:
		for ; i < end; i++ {
			switch format[i] {
			case '#':
				sharp = true
			case '0':
				zero = true
			case '+':
				plus = true
			case '-':
				minus = true
			case ' ':
				space = true
			default:
				break flags
			}
		}
		if i < end && (format[i] == '[' || format[i] == '*') {
			return nil, false
		}
		wid, widPresent, ni := parsenum(format, i, end)
		i = ni
		prec, precPresent := 0, false
		if i+1 < end && format[i] == '.' {
			i++
			if format[i] == '[' || format[i] == '*' {
				return nil, false
			}
			prec, precPresent, i = parsenum(format, i, end)
			if !precPresent {
				prec = 0
				precPresent = true
			}
		}
		if i < end && format[i] == '[' {
			return nil, false
		}
		if i >= end {
			return nil, false // %!(NOVERB)
		}
		verb, size := utf8.DecodeRuneInString(format[i:])
		i += size
		if verb == '%' {
			lit.WriteByte('%')
			continue
		}
		if argNum >= nargs {
			return nil, false // %!v(MISSING)
		}
		if verb == 'w' && wrapErrs {
			return nil, false
		}
		var bits uint
		set := func(b bool, f uint) {
			if b {
				bits |= f
			}
		}
		set(widPresent, fWidPresent)
		set(precPresent, fPrecPresent)
		set(minus, fMinus)
		set(space, fSpace)
		set(zero, fZero)
		if verb == 'v' || verb == 'w' {
			set(sharp, fSharpV)
			set(plus, fPlusV)
		} else {
			set(sharp, fSharp)
			set(plus, fPlus)
		}
		flushLit()
		ops = append(ops, fmtOp{verb: verb, bits: bits, wid: wid, prec: prec, arg: argNum})
		argNum++
	}
	flushLit()
	if argNum < nargs {
		return nil, false // %!(EXTRA ...)
	}
	return ops, true
}

// fmtFuncs are the fmt functions the pass rewrites, by name.
var fmtFuncs = map[string]bool{
	"Printf": true, "Fprintf": true, "Sprintf": true, "Appendf": true, "Errorf": true,
	"Print": true, "Fprint": true, "Sprint": true, "Append": true,
	"Println": true, "Fprintln": true, "Sprintln": true, "Appendln": true,
}

func rewriteFmt(g *gen, f *ast.File) {
	if !g.on["fmt"] {
		return
	}
	fp := g.pkg.Imports["fmt"]
	if fp == nil || fp.Types == nil || fp.Types.Scope().Lookup("MarmotNewPrinter") == nil {
		return // not imported, or the fmt entry points are missing
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil || fn.Pkg().Path() != "fmt" || !fmtFuncs[fn.Name()] || call.Ellipsis.IsValid() {
			return true
		}
		g.fmtCall(call, fn.Name())
		return true
	})
}

// operand describes one argument of a print call as the generated function
// receives it.
type operand struct {
	expr   ast.Expr
	typ    types.Type // static type after defaulting; nil for untyped nil
	tsrc   string     // parameter type source; "any" when not nameable
	isStr  bool       // dynamic type is known to have Kind String
	static bool       // dynamic type equals typ (typ is not an interface)
}

func (g *gen) operand(e ast.Expr) operand {
	tv := g.info.Types[e]
	t := tv.Type
	if t != nil {
		t = types.Default(t)
	}
	op := operand{expr: e, typ: t}
	if t == nil {
		op.tsrc = "any"
		return op
	}
	if b, ok := t.(*types.Basic); ok && b.Kind() == types.UntypedNil {
		op.typ = nil
		op.tsrc = "any"
		return op
	}
	_, isIface := t.Underlying().(*types.Interface)
	if _, isTP := t.(*types.TypeParam); isTP {
		isIface = true
	}
	op.static = !isIface
	if b, ok := t.Underlying().(*types.Basic); ok && op.static && b.Info()&types.IsString != 0 {
		op.isStr = true
	}
	op.tsrc = g.typeExpr(t)
	if op.tsrc == "" {
		op.tsrc = "any"
	}
	return op
}

// fmtMethods reports whether values of type t, stored in an interface, have
// any method fmt looks for.
func fmtMethods(t types.Type) bool {
	ms := types.NewMethodSet(t)
	for _, name := range []string{"Format", "String", "Error", "GoString"} {
		if ms.Lookup(nil, name) != nil {
			return true
		}
	}
	return false
}

// emitOperand returns the statements that print operand op (parameter name
// v) with verb under flag bits, mirroring printArg.
func (g *gen) emitOperand(op operand, v string, verb rune, bits uint) string {
	general := fmt.Sprintf("fmt.MarmotArg(p, %s, %d)", v, verb)
	if !op.static || op.typ == nil || op.tsrc == "any" || verb == 'T' || verb == 'p' || verb == 'w' {
		return general
	}
	if code, ok := g.emitValue(op.typ, v, verb, bits, 0); ok {
		return code
	}
	return general
}

// emitValue prints the value v of static type t at the given printValue
// depth (0 is printArg). It fails when fmt would take a path the typed entry
// points do not reproduce exactly: methods, invalid verbs (whose error text
// names the dynamic type), or kinds without a typed path.
func (g *gen) emitValue(t types.Type, v string, verb rune, bits uint, depth int) (string, bool) {
	if fmtMethods(t) {
		return "", false
	}
	if s, ok := types.Unalias(t).(*types.Slice); ok && depth == 0 {
		if b, ok := s.Elem().(*types.Basic); ok && b.Kind() == types.Uint8 && strings.ContainsRune("vdsxXq", verb) {
			return fmt.Sprintf("fmt.MarmotBytes(p, %s, %d)", v, verb), true
		}
		return "", false
	}
	if st, ok := t.Underlying().(*types.Struct); ok {
		return g.emitStruct(t, st, v, verb, bits, depth)
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return "", false
	}
	if bits == 0 {
		// No flags, width or precision: write what fmt writes directly.
		switch {
		case b.Kind() == types.Bool && (verb == 't' || verb == 'v'):
			return fmt.Sprintf("if %s {\n\t\tfmt.MarmotLit(p, \"true\")\n\t} else {\n\t\tfmt.MarmotLit(p, \"false\")\n\t}", v), true
		case b.Info()&types.IsInteger != 0 && (verb == 'd' || verb == 'v'):
			if b.Info()&types.IsUnsigned != 0 {
				if verb == 'v' {
					break // %#v differs only with sharpV, which sets bits; plain %v is decimal
				}
				return fmt.Sprintf("fmt.MarmotUintPlain(p, uint64(%s))", v), true
			}
			return fmt.Sprintf("fmt.MarmotIntPlain(p, int64(%s))", v), true
		case b.Info()&types.IsString != 0 && (verb == 's' || verb == 'v'):
			return fmt.Sprintf("fmt.MarmotLit(p, string(%s))", v), true
		}
	}
	sharpV := bits&fSharpV != 0
	switch {
	case b.Kind() == types.Bool:
		if verb == 't' || verb == 'v' {
			return fmt.Sprintf("fmt.MarmotFmtBoolean(p, bool(%s))", v), true
		}
	case b.Info()&types.IsInteger != 0:
		signed := b.Info()&types.IsUnsigned == 0
		u := fmt.Sprintf("uint64(%s)", v)
		if signed {
			u = fmt.Sprintf("uint64(int64(%s))", v)
		}
		// pp.fmtInteger.
		switch verb {
		case 'v':
			if sharpV && !signed {
				return fmt.Sprintf("fmt.MarmotFmt0x64(p, %s)", u), true
			}
			return fmt.Sprintf("fmt.MarmotFmtInteger(p, %s, 10, %v, %d, false)", u, signed, verb), true
		case 'd':
			return fmt.Sprintf("fmt.MarmotFmtInteger(p, %s, 10, %v, %d, false)", u, signed, verb), true
		case 'b':
			return fmt.Sprintf("fmt.MarmotFmtInteger(p, %s, 2, %v, %d, false)", u, signed, verb), true
		case 'o', 'O':
			return fmt.Sprintf("fmt.MarmotFmtInteger(p, %s, 8, %v, %d, false)", u, signed, verb), true
		case 'x':
			return fmt.Sprintf("fmt.MarmotFmtInteger(p, %s, 16, %v, %d, false)", u, signed, verb), true
		case 'X':
			return fmt.Sprintf("fmt.MarmotFmtInteger(p, %s, 16, %v, %d, true)", u, signed, verb), true
		case 'c':
			return fmt.Sprintf("fmt.MarmotFmtC(p, %s)", u), true
		case 'q':
			return fmt.Sprintf("fmt.MarmotFmtQc(p, %s)", u), true
		case 'U':
			return fmt.Sprintf("fmt.MarmotFmtUnicode(p, %s)", u), true
		}
	case b.Info()&types.IsFloat != 0, b.Info()&types.IsComplex != 0:
		// pp.fmtFloat: the float verb and default precision.
		fv, prec := verb, -1
		switch verb {
		case 'v':
			fv = 'g'
		case 'b', 'g', 'G', 'x', 'X':
		case 'f', 'e', 'E':
			prec = 6
		case 'F':
			fv, prec = 'f', 6
		default:
			return "", false
		}
		if b.Info()&types.IsComplex != 0 {
			size := 128
			if b.Kind() == types.Complex64 {
				size = 64
			}
			return fmt.Sprintf("fmt.MarmotFmtComplex(p, complex128(%s), %d, %d, %d)", v, size, fv, prec), true
		}
		size := 64
		if b.Kind() == types.Float32 {
			size = 32
		}
		return fmt.Sprintf("fmt.MarmotFmtFloat(p, float64(%s), %d, %d, %d)", v, size, fv, prec), true
	case b.Info()&types.IsString != 0:
		// pp.fmtString.
		switch verb {
		case 'v':
			if sharpV {
				return fmt.Sprintf("fmt.MarmotFmtQ(p, string(%s))", v), true
			}
			return fmt.Sprintf("fmt.MarmotFmtS(p, string(%s))", v), true
		case 's':
			return fmt.Sprintf("fmt.MarmotFmtS(p, string(%s))", v), true
		case 'x':
			return fmt.Sprintf("fmt.MarmotFmtSx(p, string(%s), false)", v), true
		case 'X':
			return fmt.Sprintf("fmt.MarmotFmtSx(p, string(%s), true)", v), true
		case 'q':
			return fmt.Sprintf("fmt.MarmotFmtQ(p, string(%s))", v), true
		}
	}
	return "", false
}

// emitStruct is printValue's struct case: {f1 f2}, {A:f1 B:f2} under %+v,
// pkg.T{A:f1, B:f2} under %#v.
func (g *gen) emitStruct(t types.Type, st *types.Struct, v string, verb rune, bits uint, depth int) (string, bool) {
	if depth > 3 {
		return "", false
	}
	sharpV, plusV := bits&fSharpV != 0, bits&fPlusV != 0
	var b strings.Builder
	if sharpV {
		n, ok := types.Unalias(t).(*types.Named)
		if !ok || n.TypeArgs() != nil || n.Obj().Pkg() == nil {
			return "", false // reflect's string for these is not derived here
		}
		fmt.Fprintf(&b, "fmt.MarmotLit(p, %s)\n\t", strconv.Quote(n.Obj().Pkg().Name()+"."+n.Obj().Name()))
	}
	b.WriteString("fmt.MarmotByte(p, '{')")
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if f.Name() == "_" || (!f.Exported() && f.Pkg() != g.pkg.Types) {
			return "", false
		}
		if i > 0 {
			if sharpV {
				b.WriteString("\n\tfmt.MarmotLit(p, \", \")")
			} else {
				b.WriteString("\n\tfmt.MarmotByte(p, ' ')")
			}
		}
		if plusV || sharpV {
			fmt.Fprintf(&b, "\n\tfmt.MarmotLit(p, %s)", strconv.Quote(f.Name()+":"))
		}
		code, ok := g.emitValue(f.Type(), v+"."+f.Name(), verb, bits, depth+1)
		if !ok {
			return "", false
		}
		b.WriteString("\n\t")
		b.WriteString(code)
	}
	b.WriteString("\n\tfmt.MarmotByte(p, '}')")
	return b.String(), true
}

func (g *gen) fmtCall(call *ast.CallExpr, name string) {
	args := call.Args
	var head []ast.Expr // leading non-operand arguments kept (writer, buffer)
	var headParams []string
	format := ""
	printf := strings.HasSuffix(name, "f")
	switch name {
	case "Fprintf", "Fprint", "Fprintln":
		if len(args) < 1 {
			return
		}
		head, headParams, args = args[:1], []string{"w io.Writer"}, args[1:]
	case "Appendf", "Append", "Appendln":
		if len(args) < 1 {
			return
		}
		head, headParams, args = args[:1], []string{"b []byte"}, args[1:]
	}
	if printf {
		if len(args) < 1 {
			return
		}
		tv := g.info.Types[args[0]]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			return
		}
		format = constant.StringVal(tv.Value)
		args = args[1:]
		if name == "Errorf" && len(args) == 0 && !strings.Contains(format, "%") {
			return // already errors.New(format)
		}
	}
	for _, a := range call.Args {
		if _, multi := g.info.Types[a].Type.(*types.Tuple); multi {
			return // f(g()) with a multi-value g
		}
	}
	ops := make([]operand, len(args))
	for i, a := range args {
		ops[i] = g.operand(a)
	}

	var body strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&body, "\t"+format+"\n", a...) }
	if name == "Errorf" {
		w("p := fmt.MarmotNewErrorPrinter()")
	} else {
		if name == "Printf" || name == "Print" || name == "Println" {
			w("w := os.Stdout")
		}
		w("p := fmt.MarmotNewPrinter()")
	}
	switch {
	case printf:
		fops, ok := parseFormat(format, len(args), name == "Errorf")
		if !ok {
			return
		}
		// The flag state persists between verbs (every step restores what
		// it changes), so it is stored only when it differs from the last.
		cur := fmtOp{} // newPrinter clears the flags
		for _, op := range fops {
			if op.verb == 0 {
				w("fmt.MarmotLit(p, %s)", strconv.Quote(op.lit))
				continue
			}
			if op.bits != cur.bits || op.wid != cur.wid || op.prec != cur.prec {
				w("fmt.MarmotFlags(p, %d, %d, %d)", op.bits, op.wid, op.prec)
				cur = op
			}
			w("%s", g.emitOperand(ops[op.arg], fmt.Sprintf("a%d", op.arg), op.verb, op.bits))
		}
	case strings.HasSuffix(name, "ln"):
		for i, op := range ops {
			if i > 0 {
				w("fmt.MarmotByte(p, ' ')")
			}
			w("%s", g.emitOperand(op, fmt.Sprintf("a%d", i), 'v', 0))
		}
		w("fmt.MarmotByte(p, '\\n')")
	default: // Print family: space between operands when neither is a string
		prevString := false
		for i, op := range ops {
			if !op.static && op.typ != nil {
				return // string-ness of an interface operand is only known at run time
			}
			if i > 0 && !op.isStr && !prevString {
				w("fmt.MarmotByte(p, ' ')")
			}
			w("%s", g.emitOperand(op, fmt.Sprintf("a%d", i), 'v', 0))
			prevString = op.isStr
		}
	}
	var result string
	switch name {
	case "Printf", "Fprintf", "Print", "Fprint", "Println", "Fprintln":
		w("return fmt.MarmotWrite(p, w)")
		result = "(int, error)"
		if name[0] != 'F' {
			headParams = nil
		}
	case "Sprintf", "Sprint", "Sprintln":
		w("return fmt.MarmotSprint(p)")
		result = "string"
	case "Appendf", "Append", "Appendln":
		w("return fmt.MarmotAppend(p, b)")
		result = "[]byte"
	case "Errorf":
		w("return fmt.MarmotErrorf(p)")
		result = "error"
	}

	params := append([]string{}, headParams...)
	for i, op := range ops {
		params = append(params, fmt.Sprintf("a%d %s", i, op.tsrc))
	}
	fname := g.newName("fmt")
	src := fmt.Sprintf("func %s(%s) %s {\n%s}\n", fname, strings.Join(params, ", "), result, body.String())
	// The generated file refers to fmt, io and os by these names.
	src = rewriteQualifiers(src, func(name string) (string, bool) {
		switch name {
		case "fmt", "io", "os":
			return g.importName(name), true
		}
		return "", false
	})
	g.decls = append(g.decls, src)
	newArgs := append(append([]ast.Expr{}, head...), args...)
	g.replaceCall(call, fname, newArgs)
}

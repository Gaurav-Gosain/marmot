package specialize

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"strconv"
	"strings"

	"golang.org/x/tools/go/types/typeutil"
)

// encoding/json specialization.
//
// json.Marshal(x) where the static type T of x is fully known and plain
// (bools, numbers, strings, structs with exported fields and simple tags,
// pointers, slices, arrays, maps with string keys, none with marshal methods)
// is replaced by a generated encoder for T. Leaves go through the same
// jsonwire functions the general path uses (lib/src/encoding/json/
// marmot_spec.go). Whenever the encoder meets a value the general path would
// report as an error (NaN, infinities, very deep nesting that may be a cycle),
// it gives up and calls json.Marshal, so results and errors are unchanged.

// jsonFuncs are the encoding/json functions the pass rewrites.
var jsonFuncs = map[string]bool{"Marshal": true, "Unmarshal": true}

// jsonMethods are the methods that make encoding/json call user code.
var jsonMethods = []string{"MarshalJSON", "MarshalJSONTo", "MarshalText", "AppendText",
	"UnmarshalJSON", "UnmarshalJSONFrom", "UnmarshalText"}

func hasJSONMethods(t types.Type) bool {
	for _, tt := range []types.Type{t, types.NewPointer(t)} {
		ms := types.NewMethodSet(tt)
		for _, name := range jsonMethods {
			if ms.Lookup(nil, name) != nil {
				return true
			}
		}
	}
	return false
}

// jsonField is one encoded struct field.
type jsonField struct {
	goName    string
	name      string // JSON name
	omitEmpty bool
	typ       types.Type
}

// jsonFields returns the encoded fields of a struct in order, or false when
// the struct uses features the encoder does not reproduce.
func (g *gen) jsonFields(st *types.Struct) ([]jsonField, bool) {
	var fs []jsonField
	seen := map[string]bool{}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		tag, hasTag := reflect.StructTag(st.Tag(i)).Lookup("json")
		if f.Embedded() {
			return nil, false
		}
		if !f.Exported() {
			if hasTag {
				return nil, false
			}
			continue
		}
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name()
		}
		for _, c := range name {
			if !(c == '_' || c == '-' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
				return nil, false
			}
		}
		jf := jsonField{goName: f.Name(), name: name, typ: f.Type()}
		if opts != "" {
			for _, o := range strings.Split(opts, ",") {
				switch o {
				case "omitempty":
					jf.omitEmpty = true
				default:
					return nil, false
				}
			}
		}
		// Two fields with one JSON name, or names equal under case
		// folding, follow conflict rules the encoder does not model.
		key := strings.ToLower(name)
		if seen[key] {
			return nil, false
		}
		seen[key] = true
		fs = append(fs, jf)
	}
	return fs, true
}

// jsonEnc generates encoders for one package.
type jsonEnc struct {
	g     *gen
	names typeutil.Map // types.Type -> encoder function name ("" once failed)
	bad   bool
	trial []types.Type // types named during the attempts in progress
}

// encoder returns the name of the generated encoder for t, generating it on
// first use. The encoder has the signature
//
//	func(b []byte, v *T, d int) ([]byte, bool)
func (je *jsonEnc) encoder(t types.Type) (string, bool) {
	if v := je.names.At(t); v != nil {
		return v.(string), v.(string) != ""
	}
	if hasJSONMethods(t) {
		return "", false
	}
	tsrc := je.g.typeExpr(t)
	if tsrc == "" {
		return "", false
	}
	name := je.g.newName("jenc")
	je.names.Set(t, name) // allows recursive types
	mark, started := len(je.g.decls), len(je.trial)
	je.trial = append(je.trial, t)
	body, ok := je.body(t)
	if !ok {
		// Roll back everything generated since this attempt began: those
		// encoders may refer to this one.
		je.g.decls = je.g.decls[:mark]
		for _, tt := range je.trial[started:] {
			je.names.Set(tt, "")
		}
		je.trial = je.trial[:started]
		return "", false
	}
	je.g.decls = append(je.g.decls, fmt.Sprintf("func %s(b []byte, v *%s, d int) ([]byte, bool) {\n\tif d > 1000 {\n\t\treturn b, false\n\t}\n\tok := true\n\t_ = ok\n%s\treturn b, true\n}\n", name, tsrc, body))
	return name, true
}

// value returns statements that append the JSON form of the value at
// address expression ptr (of type *t).
func (je *jsonEnc) value(t types.Type, ptr string, indent string) (string, bool) {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, indent+format+"\n", a...) }
	if hasJSONMethods(t) {
		return "", false
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Kind() == types.Bool:
			w("if *%s {", ptr)
			w("\tb = append(b, \"true\"...)")
			w("} else {")
			w("\tb = append(b, \"false\"...)")
			w("}")
		case u.Info()&types.IsInteger != 0 && u.Info()&types.IsUnsigned != 0:
			w("b = json.MarmotAppendUint(b, uint64(*%s))", ptr)
		case u.Info()&types.IsInteger != 0:
			w("b = json.MarmotAppendInt(b, int64(*%s))", ptr)
		case u.Info()&types.IsFloat != 0:
			bits := 64
			if u.Kind() == types.Float32 {
				bits = 32
			}
			w("if b, ok = json.MarmotAppendFloat(b, float64(*%s), %d); !ok {", ptr, bits)
			w("\treturn b, false")
			w("}")
		case u.Info()&types.IsString != 0:
			w("b = json.MarmotAppendString(b, string(*%s))", ptr)
		default:
			return "", false
		}
	case *types.Pointer:
		enc, ok := je.encoder(u.Elem())
		if !ok {
			return "", false
		}
		w("if *%s == nil {", ptr)
		w("\tb = append(b, \"null\"...)")
		w("} else if b, ok = %s(b, *%s, d+1); !ok {", enc, ptr)
		w("\treturn b, false")
		w("}")
	case *types.Slice, *types.Array:
		var elem types.Type
		isSlice := false
		if s, ok := u.(*types.Slice); ok {
			elem, isSlice = s.Elem(), true
		} else {
			elem = u.(*types.Array).Elem()
		}
		if eb, ok := elem.Underlying().(*types.Basic); ok && eb.Kind() == types.Uint8 {
			return "", false // []byte is base64, [N]byte follows legacy rules
		}
		enc, ok := je.encoder(elem)
		if !ok {
			return "", false
		}
		if isSlice {
			w("if *%s == nil {", ptr)
			w("\tb = append(b, \"null\"...)")
			w("} else {")
		} else {
			w("{")
		}
		w("\tb = append(b, '[')")
		w("\tfor i := range *%s {", ptr)
		w("\t\tif i > 0 {")
		w("\t\t\tb = append(b, ',')")
		w("\t\t}")
		w("\t\tif b, ok = %s(b, &(*%s)[i], d+1); !ok {", enc, ptr)
		w("\t\t\treturn b, false")
		w("\t\t}")
		w("\t}")
		w("\tb = append(b, ']')")
		w("}")
	case *types.Map:
		kb, ok := u.Key().Underlying().(*types.Basic)
		if !ok || kb.Info()&types.IsString == 0 || hasJSONMethods(u.Key()) {
			return "", false
		}
		ksrc := je.g.typeExpr(u.Key())
		enc, ok := je.encoder(u.Elem())
		if !ok || ksrc == "" {
			return "", false
		}
		esrc := je.g.typeExpr(u.Elem())
		if esrc == "" {
			return "", false
		}
		w("if *%s == nil {", ptr)
		w("\tb = append(b, \"null\"...)")
		w("} else if len(*%s) == 0 {", ptr)
		w("\tb = append(b, \"{}\"...)")
		w("} else {")
		// Keys are distinct, so any correct sort gives the order Marshal
		// uses (slices.Sort). Up to 8 keys sort in a fixed array.
		w("\tvar ka [8]%s", ksrc)
		w("\tvar keys []%s", ksrc)
		w("\tif len(*%s) <= len(ka) {", ptr)
		w("\t\tn := 0")
		w("\t\tfor k := range *%s {", ptr)
		w("\t\t\tj := n")
		w("\t\t\tfor j > 0 && k < ka[j-1] {")
		w("\t\t\t\tka[j] = ka[j-1]")
		w("\t\t\t\tj--")
		w("\t\t\t}")
		w("\t\t\tka[j] = k")
		w("\t\t\tn++")
		w("\t\t}")
		w("\t\tkeys = ka[:n]")
		w("\t} else {")
		w("\t\tkeys = make([]%s, 0, len(*%s))", ksrc, ptr)
		w("\t\tfor k := range *%s {", ptr)
		w("\t\t\tkeys = append(keys, k)")
		w("\t\t}")
		w("\t\tslices.Sort(keys)")
		w("\t}")
		w("\tb = append(b, '{')")
		w("\tfor i, k := range keys {")
		w("\t\tif i > 0 {")
		w("\t\t\tb = append(b, ',')")
		w("\t\t}")
		w("\t\tb = json.MarmotAppendString(b, string(k))")
		w("\t\tb = append(b, ':')")
		w("\t\tev := (*%s)[k]", ptr)
		if leaf, ok := je.leaf(u.Elem(), "ev", indent+"\t\t"); ok {
			b.WriteString(leaf)
		} else {
			w("\t\tif b, ok = %s(b, &ev, d+1); !ok {", enc)
			w("\t\t\treturn b, false")
			w("\t\t}")
		}
		w("\t}")
		w("\tb = append(b, '}')")
		w("}")
	case *types.Struct:
		fs, ok := je.g.jsonFields(u)
		if !ok {
			return "", false
		}
		w("b = append(b, '{')")
		// wrote is needed only while every field so far may be omitted.
		static := false // a field before this one is always written
		dyn := false    // the wrote variable is declared
		for _, f := range fs {
			lit := strconv.Quote(f.name) + ":"
			fptr := "&(*" + ptr + ")." + f.goName
			fv := "(*" + ptr + ")." + f.goName
			code, ok := je.value(f.typ, "fp", indent+"\t")
			if !ok {
				return "", false
			}
			cond := ""
			if f.omitEmpty {
				c, ok := emptyCheck(f.typ, fv)
				if !ok {
					return "", false
				}
				cond = c
			}
			if cond != "" {
				w("if !(%s) {", cond)
			} else {
				w("{")
			}
			switch {
			case static:
				w("\tb = append(b, %s...)", strconv.Quote(","+lit))
			case dyn:
				w("\tif wrote {")
				w("\t\tb = append(b, ',')")
				w("\t}")
				w("\tb = append(b, %s...)", strconv.Quote(lit))
			default:
				w("\tb = append(b, %s...)", strconv.Quote(lit))
			}
			w("\tfp := %s", fptr)
			b.WriteString(code)
			if !static && f.omitEmpty {
				if !dyn {
					// Declare before first use: hoist by rewriting below.
					dyn = true
				}
				w("\twrote = true")
			}
			w("}")
			if !f.omitEmpty {
				static = true
			}
		}
		w("b = append(b, '}')")
		out := b.String()
		if dyn {
			out = indent + "wrote := false\n" + indent + "_ = wrote\n" + out
		}
		return out, true
	default:
		return "", false
	}
	return b.String(), true
}

// leaf returns statements that append a basic value expression v (no
// address needed), or false when t is not a basic leaf.
func (je *jsonEnc) leaf(t types.Type, v string, indent string) (string, bool) {
	if _, ok := t.Underlying().(*types.Basic); !ok || hasJSONMethods(t) {
		return "", false
	}
	tmp := "(&" + v + ")"
	code, ok := je.value(t, tmp, indent)
	return strings.ReplaceAll(code, "*"+tmp, v), ok
}

// body is the encoder body for *v of type t.
func (je *jsonEnc) body(t types.Type) (string, bool) {
	return je.value(t, "v", "\t")
}

// emptyCheck returns the v1 omitempty test for a value expression.
func emptyCheck(t types.Type, v string) (string, bool) {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Kind() == types.Bool:
			return "!" + v, true
		case u.Info()&types.IsNumeric != 0:
			return v + " == 0", true
		case u.Info()&types.IsString != 0:
			return "len(" + v + ") == 0", true
		}
	case *types.Pointer, *types.Interface:
		return v + " == nil", true
	case *types.Slice, *types.Map:
		return "len(" + v + ") == 0", true
	case *types.Array:
		if u.Len() == 0 {
			return "true", true
		}
		return "false", true
	case *types.Struct:
		return "false", true
	}
	return "", false
}

func rewriteJSON(g *gen, f *ast.File) {
	jp := g.pkg.Imports["encoding/json"]
	if jp == nil || jp.Types == nil || jp.Types.Scope().Lookup("MarmotAppendString") == nil {
		return
	}
	if g.json == nil {
		g.json = &jsonEnc{g: g}
	}
	// Decoders and encoders refer to these whether or not the call that
	// asked for them is rewritten; unused imports are dropped in source().
	g.usePkg("encoding/json", "json")
	g.usePkg("strconv", "strconv")
	g.usePkg("slices", "slices")
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if g.on["jsonenc"] && len(call.Args) == 1 && !call.Ellipsis.IsValid() && isMethod(g.info, call, "encoding/json", "Encoder", "Encode") {
			g.jsonEncode(call)
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil || fn.Pkg().Path() != "encoding/json" || !jsonFuncs[fn.Name()] || call.Ellipsis.IsValid() {
			return true
		}
		switch {
		case fn.Name() == "Marshal" && len(call.Args) == 1 && g.on["jsonenc"]:
			g.jsonMarshal(call)
		case fn.Name() == "Unmarshal" && len(call.Args) == 2 && g.on["jsondec"]:
			g.jsonUnmarshal(call)
		}
		return true
	})
}

func (g *gen) jsonMarshal(call *ast.CallExpr) {
	t := g.info.Types[call.Args[0]].Type
	if t == nil {
		return
	}
	t = types.Default(t)
	if _, isIface := t.Underlying().(*types.Interface); isIface {
		return
	}
	if _, isTP := t.(*types.TypeParam); isTP {
		return
	}
	tsrc := g.typeExpr(t)
	if tsrc == "" {
		return
	}
	enc, ok := g.json.encoder(t)
	if !ok {
		return
	}
	name := g.newName("json")
	g.decls = append(g.decls, fmt.Sprintf(`func %s(v %s) ([]byte, error) {
	p := json.MarmotGetBuf()
	b, ok := %s(*p, &v, 0)
	if !ok {
		json.MarmotDropBuf(p, b)
		return json.Marshal(v)
	}
	return json.MarmotPutBuf(p, b), nil
}
`, name, tsrc, enc))
	g.usePkg("encoding/json", "json")
	g.usePkg("strconv", "strconv")
	g.usePkg("slices", "slices")
	g.replaceCall(call, name, []ast.Expr{call.Args[0]})
}

// jsonDec generates decoders. A decoder has the signature
//
//	func(d *json.MarmotDecoder, v *T, dry bool) bool
//
// and with dry set it must not touch v (which is then nil).
type jsonDec struct {
	g     *gen
	names typeutil.Map
	trial []types.Type
}

func (jd *jsonDec) decoder(t types.Type) (string, bool) {
	if v := jd.names.At(t); v != nil {
		return v.(string), v.(string) != ""
	}
	if hasJSONMethods(t) {
		return "", false
	}
	tsrc := jd.g.typeExpr(t)
	if tsrc == "" {
		return "", false
	}
	name := jd.g.newName("jdec")
	jd.names.Set(t, name)
	mark, started := len(jd.g.decls), len(jd.trial)
	jd.trial = append(jd.trial, t)
	body, ok := jd.body(t, tsrc)
	if !ok {
		jd.g.decls = jd.g.decls[:mark]
		for _, tt := range jd.trial[started:] {
			jd.names.Set(tt, "")
		}
		jd.trial = jd.trial[:started]
		return "", false
	}
	jd.g.decls = append(jd.g.decls, fmt.Sprintf("func %s(d *json.MarmotDecoder, v *%s, dry bool) bool {\n%s}\n", name, tsrc, body))
	return name, true
}

// body returns the decoder body for *v of type t (source tsrc).
func (jd *jsonDec) body(t types.Type, tsrc string) (string, bool) {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, "\t"+format+"\n", a...) }
	// null leaves scalars and structs unchanged and sets nillable kinds to nil.
	nullKeep := func() {
		w("if d.Peek() == 'n' {")
		w("\treturn d.Lit(\"null\")")
		w("}")
	}
	nullNil := func() {
		w("if d.Peek() == 'n' {")
		w("\tif !d.Lit(\"null\") {")
		w("\t\treturn false")
		w("\t}")
		w("\tif !dry {")
		w("\t\t*v = nil")
		w("\t}")
		w("\treturn true")
		w("}")
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		nullKeep()
		switch {
		case u.Kind() == types.Bool:
			w("x, ok := d.Bool()")
		case u.Info()&types.IsInteger != 0 && u.Info()&types.IsUnsigned != 0:
			w("x, ok := d.Uint(%d)", jd.g.bits(u))
		case u.Info()&types.IsInteger != 0:
			w("x, ok := d.Int(%d)", jd.g.bits(u))
		case u.Info()&types.IsFloat != 0:
			w("x, ok := d.Float(%d)", jd.g.bits(u))
		case u.Info()&types.IsString != 0:
			w("if d.Peek() != '\"' {")
			w("\treturn false")
			w("}")
			w("if dry {")
			w("\t_, ok := d.Bytes()")
			w("\treturn ok")
			w("}")
			w("x, ok := d.Str()")
		default:
			return "", false
		}
		w("if ok && !dry {")
		w("\t*v = %s(x)", tsrc)
		w("}")
		w("return ok")
	case *types.Pointer:
		elem, ok := jd.decoder(u.Elem())
		esrc := jd.g.typeExpr(u.Elem())
		if !ok || esrc == "" {
			return "", false
		}
		nullNil()
		w("if dry {")
		w("\treturn %s(d, nil, true)", elem)
		w("}")
		w("if *v == nil {")
		w("\t*v = new(%s)", esrc)
		w("}")
		w("return %s(d, *v, false)", elem)
	case *types.Slice:
		if eb, ok := u.Elem().Underlying().(*types.Basic); ok && eb.Kind() == types.Uint8 {
			return "", false
		}
		elem, ok := jd.decoder(u.Elem())
		if !ok {
			return "", false
		}
		nullNil()
		w("if !d.Open('[') {")
		w("\treturn false")
		w("}")
		w("if dry {")
		w("\tfor i := 0; ; i++ {")
		w("\t\tmore, ok := d.Next(']', i)")
		w("\t\tif !ok {")
		w("\t\t\treturn false")
		w("\t\t}")
		w("\t\tif !more {")
		w("\t\t\treturn true")
		w("\t\t}")
		w("\t\tif !%s(d, nil, true) {", elem)
		w("\t\t\treturn false")
		w("\t\t}")
		w("\t}")
		w("}")
		// Legacy merge: reuse the whole capacity without zeroing, grow by
		// append, an empty array gives an empty slice with no capacity.
		w("s := *v")
		w("s = s[:cap(s)]")
		w("n := 0")
		w("total := -1")
		w("for i := 0; ; i++ {")
		w("\tmore, ok := d.Next(']', i)")
		w("\tif !ok {")
		w("\t\treturn false")
		w("\t}")
		w("\tif !more {")
		w("\t\tbreak")
		w("\t}")
		// At the first growth, count the rest of the array and allocate
		// the capacity one-at-a-time appends would reach; elements before
		// this point stay in the old array, as with append.
		_, scalarElem := u.Elem().Underlying().(*types.Basic)
		w("\tif n == len(s) {")
		w("\t\tif total < 0 && %v {", scalarElem)
		w("\t\t\tif r := d.CountRest(); r > 0 {")
		w("\t\t\t\ttotal = n + r")
		w("\t\t\t\tns := make([]%s, json.MarmotGrownCap(cap(s), total, %d, %v))", jd.g.typeExpr(u.Elem()), jd.g.pkg.TypesSizes.Sizeof(u.Elem()), !hasPointers(u.Elem()))
		w("\t\t\t\tcopy(ns, s)")
		w("\t\t\t\ts = ns")
		w("\t\t\t}")
		w("\t\t}")
		w("\t\tif n == len(s) {")
		w("\t\t\tvar zero %s", jd.g.typeExpr(u.Elem()))
		w("\t\t\ts = append(s, zero)")
		w("\t\t\ts = s[:cap(s)]")
		w("\t\t}")
		w("\t}")
		w("\tif !%s(d, &s[n], false) {", elem)
		w("\t\treturn false")
		w("\t}")
		w("\tn++")
		w("}")
		w("if n == 0 {")
		w("\t*v = %s{}", tsrc)
		w("} else {")
		w("\t*v = s[:n]")
		w("}")
		w("return true")
	case *types.Map:
		kb, ok := u.Key().Underlying().(*types.Basic)
		if !ok || kb.Info()&types.IsString == 0 || hasJSONMethods(u.Key()) {
			return "", false
		}
		ksrc, esrc := jd.g.typeExpr(u.Key()), jd.g.typeExpr(u.Elem())
		elem, ok := jd.decoder(u.Elem())
		if !ok || ksrc == "" || esrc == "" {
			return "", false
		}
		nullNil()
		w("if !d.Open('{') {")
		w("\treturn false")
		w("}")
		w("var m %s", tsrc)
		w("if !dry {")
		w("\tif *v == nil {")
		w("\t\t*v = make(%s)", tsrc)
		w("\t}")
		w("\tm = *v")
		w("}")
		w("for i := 0; ; i++ {")
		w("\tmore, ok := d.Next('}', i)")
		w("\tif !ok {")
		w("\t\treturn false")
		w("\t}")
		w("\tif !more {")
		w("\t\treturn true")
		w("\t}")
		w("\tif dry {")
		w("\t\tif _, ok := d.Key(); !ok || !%s(d, nil, true) {", elem)
		w("\t\t\treturn false")
		w("\t\t}")
		w("\t\tcontinue")
		w("\t}")
		w("\tks, ok := d.KeyStr()")
		w("\tif !ok {")
		w("\t\treturn false")
		w("\t}")
		w("\tk := %s(ks)", ksrc)
		w("\tvar e %s", esrc)
		w("\tif !%s(d, &e, false) {", elem)
		w("\t\treturn false")
		w("\t}")
		w("\tm[k] = e")
		w("}")
	case *types.Struct:
		fs, ok := jd.g.jsonFields(u)
		if !ok {
			return "", false
		}
		nullKeep()
		w("if !d.Open('{') {")
		w("\treturn false")
		w("}")
		w("for i := 0; ; i++ {")
		w("\tmore, ok := d.Next('}', i)")
		w("\tif !ok {")
		w("\t\treturn false")
		w("\t}")
		w("\tif !more {")
		w("\t\treturn true")
		w("\t}")
		w("\tk, ok := d.Key()")
		w("\tif !ok {")
		w("\t\treturn false")
		w("\t}")
		// Match the member name: switch on length, then compare bytes.
		byLen := map[int][]int{}
		var lens []int
		for i, f := range fs {
			if _, ok := byLen[len(f.name)]; !ok {
				lens = append(lens, len(f.name))
			}
			byLen[len(f.name)] = append(byLen[len(f.name)], i)
		}
		w("\tf := -1")
		w("\tswitch len(k) {")
		for _, n := range lens {
			w("\tcase %d:", n)
			for j, i := range byLen[n] {
				var conds []string
				for c := 0; c < n; c++ {
					conds = append(conds, fmt.Sprintf("k[%d] == %s", c, strconv.QuoteRune(rune(fs[i].name[c]))))
				}
				if n == 0 {
					conds = []string{"true"}
				}
				kw := "if"
				if j > 0 {
					kw = "} else if"
				}
				w("\t\t%s %s {", kw, strings.Join(conds, " && "))
				w("\t\t\tf = %d", i)
			}
			w("\t\t}")
		}
		w("\t}")
		w("\tswitch f {")
		var names []string
		for i, f := range fs {
			fd, ok := jd.decoder(f.typ)
			if !ok {
				return "", false
			}
			names = append(names, strconv.Quote(f.name))
			w("\tcase %d:", i)
			w("\t\tif dry {")
			w("\t\t\tok = %s(d, nil, true)", fd)
			w("\t\t} else {")
			w("\t\t\tok = %s(d, &v.%s, false)", fd, f.goName)
			w("\t\t}")
		}
		w("\tdefault:")
		if len(names) > 0 {
			w("\t\tif json.MarmotMaybeFold(k, %s) {", strings.Join(names, ", "))
			w("\t\t\treturn false")
			w("\t\t}")
		}
		w("\t\tok = d.Skip()")
		w("\t}")
		w("\tif !ok {")
		w("\t\treturn false")
		w("\t}")
		w("}")
	default:
		return "", false
	}
	return b.String(), true
}

// freshCheck returns a condition that holds when the value expression v of
// type t is bitwise the zero value (so -0.0 is not fresh), or false when
// the check would be too large or cannot be written.
func freshCheck(pkg *types.Package, t types.Type, v string, depth int) (string, bool) {
	if depth > 2 {
		return "", false
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Kind() == types.Bool:
			return "!" + v, true
		case u.Kind() == types.Float32:
			return "math.Float32bits(float32(" + v + ")) == 0", true
		case u.Kind() == types.Float64:
			return "math.Float64bits(float64(" + v + ")) == 0", true
		case u.Info()&types.IsInteger != 0:
			return v + " == 0", true
		case u.Info()&types.IsString != 0:
			return "len(" + v + ") == 0", true
		}
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return v + " == nil", true
	case *types.Struct:
		if u.NumFields() > 16 {
			return "", false
		}
		conds := []string{"true"}
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if f.Name() == "_" || f.Embedded() || (!f.Exported() && f.Pkg() != pkg) {
				return "", false
			}
			c, ok := freshCheck(pkg, f.Type(), v+"."+f.Name(), depth+1)
			if !ok {
				return "", false
			}
			conds = append(conds, "("+c+")")
		}
		return strings.Join(conds, " && "), true
	}
	return "", false
}

// bits is the size in bits of a basic numeric type.
func (g *gen) bits(b *types.Basic) int {
	return int(g.pkg.TypesSizes.Sizeof(b) * 8)
}

func (g *gen) jsonUnmarshal(call *ast.CallExpr) {
	t := g.info.Types[call.Args[1]].Type
	if t == nil {
		return
	}
	pt, ok := t.(*types.Pointer)
	if !ok {
		return // only *T for a named or literal pointer type
	}
	tsrc := g.typeExpr(pt.Elem())
	if tsrc == "" {
		return
	}
	if g.jdec == nil {
		g.jdec = &jsonDec{g: g}
	}
	dec, ok := g.jdec.decoder(pt.Elem())
	if !ok {
		return
	}
	name := g.newName("json")
	// When *v is still the zero value, decoding into a zero temporary and
	// assigning it on success is the same as decoding into *v, and failure
	// leaves *v untouched: one pass, no dry run.
	fresh := ""
	if cond, ok := freshCheck(g.pkg.Types, pt.Elem(), "(*v)", 0); ok {
		fresh = fmt.Sprintf(`	if %s {
		var tmp %s
		d := json.MarmotNewDecoderCached(data)
		if %s(&d, &tmp, false) && d.End() {
			d.Release()
			*v = tmp
			return nil
		}
		d.Release()
		return json.Unmarshal(data, v)
	}
`, cond, tsrc, dec)
	}
	g.decls = append(g.decls, fmt.Sprintf(`func %s(data []byte, v *%s) error {
	if v == nil {
		return json.Unmarshal(data, v)
	}
%s	d := json.MarmotNewDecoder(data)
	if !%s(&d, nil, true) || !d.End() {
		return json.Unmarshal(data, v)
	}
	d = json.MarmotNewDecoderCached(data)
	%s(&d, v, false)
	d.Release()
	return nil
}
`, name, tsrc, fresh, dec, dec))
	if fresh != "" {
		g.usePkg("math", "math")
	}
	g.usePkg("encoding/json", "json")
	g.replaceCall(call, name, []ast.Expr{call.Args[0], call.Args[1]})
}

// isMethod reports whether call is a direct call of method name on
// pointer type *pkg.typ.
func isMethod(info *types.Info, call *ast.CallExpr, pkg, typ, name string) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	s, ok := info.Selections[sel]
	if !ok || s.Kind() != types.MethodVal {
		return false
	}
	fn, ok := s.Obj().(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != pkg || fn.Name() != name {
		return false
	}
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return false
	}
	pt, ok := recv.Type().(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := pt.Elem().(*types.Named)
	return ok && n.Obj().Name() == typ && len(s.Index()) == 1
}

// jsonEncode specializes enc.Encode(x) like json.Marshal, for encoders in
// their default state; any other state takes the general path.
func (g *gen) jsonEncode(call *ast.CallExpr) {
	t := g.info.Types[call.Args[0]].Type
	if t == nil {
		return
	}
	t = types.Default(t)
	if _, isIface := t.Underlying().(*types.Interface); isIface {
		return
	}
	if _, isTP := t.(*types.TypeParam); isTP {
		return
	}
	tsrc := g.typeExpr(t)
	if tsrc == "" {
		return
	}
	if g.json == nil {
		g.json = &jsonEnc{g: g}
	}
	enc, ok := g.json.encoder(t)
	if !ok {
		return
	}
	sel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	// The receiver must be addressable or a pointer already: Encode has a
	// pointer receiver, so x.Encode on an addressable Encoder value means
	// (&x).Encode; only plain pointer receivers are handled.
	if _, ok := g.info.Types[sel.X].Type.(*types.Pointer); !ok {
		return
	}
	name := g.newName("json")
	g.decls = append(g.decls, fmt.Sprintf(`func %s(e *json.Encoder, v %s) error {
	if !json.MarmotEncoderPlain(e) {
		return e.Encode(v)
	}
	p := json.MarmotGetBuf()
	b, ok := %s(*p, &v, 0)
	if !ok {
		json.MarmotDropBuf(p, b)
		return e.Encode(v)
	}
	b = append(b, '\n')
	err := json.MarmotEncoderWrite(e, b)
	json.MarmotDropBuf(p, b)
	return err
}
`, name, tsrc, enc))
	g.usePkg("encoding/json", "json")
	g.usePkg("strconv", "strconv")
	g.usePkg("slices", "slices")
	g.replaceCall(call, name, []ast.Expr{sel.X, call.Args[0]})
}

// hasPointers reports whether values of t contain pointers (the runtime
// allocates them as scan objects).
func hasPointers(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0 || u.Kind() == types.UnsafePointer
	case *types.Array:
		return u.Len() > 0 && hasPointers(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if hasPointers(u.Field(i).Type()) {
				return true
			}
		}
		return false
	}
	return true
}

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
var jsonFuncs = map[string]bool{"Marshal": true}

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
	names typeutil.Map // types.Type -> encoder function name ("" while failing)
	bad   bool
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
	body, ok := je.body(t)
	if !ok {
		je.names.Set(t, "")
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
			w("b = strconv.AppendUint(b, uint64(*%s), 10)", ptr)
		case u.Info()&types.IsInteger != 0:
			w("b = strconv.AppendInt(b, int64(*%s), 10)", ptr)
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
		w("\tkeys := make([]%s, 0, len(*%s))", ksrc, ptr)
		w("\tfor k := range *%s {", ptr)
		w("\t\tkeys = append(keys, k)")
		w("\t}")
		w("\tslices.Sort(keys)")
		w("\tb = append(b, '{')")
		w("\tfor i, k := range keys {")
		w("\t\tif i > 0 {")
		w("\t\t\tb = append(b, ',')")
		w("\t\t}")
		w("\t\tb = json.MarmotAppendString(b, string(k))")
		w("\t\tb = append(b, ':')")
		w("\t\tev := (*%s)[k]", ptr)
		w("\t\tif b, ok = %s(b, &ev, d+1); !ok {", enc)
		w("\t\t\treturn b, false")
		w("\t\t}")
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
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil || fn.Pkg().Path() != "encoding/json" || !jsonFuncs[fn.Name()] || len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return true
		}
		g.jsonMarshal(call)
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

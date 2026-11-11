package specialize

import (
	"fmt"
	"go/ast"
	"go/constant"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Compile time evaluation of pure calls with constant arguments.
//
// strconv.Itoa(42), strings.ToUpper("abc"), math.Sqrt(2) and the like are
// evaluated by the compiler, which runs the same Go version's code, and the
// call becomes a read of a generated package variable holding the result.
// A variable, not a constant: a constant 0 divisor or an out of range
// constant index would turn a run time panic into a compile error. Calls
// that would panic (negative Repeat count), results too large, and NaN or
// infinite floats are left alone. Only when this binary's Go version equals
// the GOROOT's (otherwise tables such as Unicode case mappings may differ).

func rewriteFold(g *gen, f *ast.File) {
	if !g.on["fold"] || !g.sameGoVersion() {
		return
	}
	g.curFile = f
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Ellipsis.IsValid() {
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil {
			return true
		}
		var args []constant.Value
		for _, a := range call.Args {
			tv := g.info.Types[a]
			if tv.Value == nil {
				return true
			}
			args = append(args, tv.Value)
		}
		val, typ, ok := foldCall(fn.Pkg().Path(), fn.Name(), args)
		if !ok {
			return true
		}
		name := g.newName("fold")
		g.raw = append(g.raw, fmt.Sprintf("var %s = %s(%s)\n", name, typ, val))
		g.replaceExpr(call, ast.NewIdent(name))
		return false
	})
}

// foldCall evaluates a whitelisted call; val is Go source for the result.
func foldCall(pkg, name string, a []constant.Value) (val, typ string, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	str := func(i int) (string, bool) {
		if i < len(a) && a[i].Kind() == constant.String {
			return constant.StringVal(a[i]), true
		}
		return "", false
	}
	i64 := func(i int) (int64, bool) {
		if i < len(a) && a[i].Kind() == constant.Int {
			return constant.Int64Val(a[i])
		}
		return 0, false
	}
	f64 := func(i int) (float64, bool) {
		if i < len(a) {
			if v := constant.ToFloat(a[i]); v.Kind() == constant.Float {
				f, _ := constant.Float64Val(v)
				return f, true
			}
		}
		return 0, false
	}
	retS := func(s string) (string, string, bool) {
		if len(s) > 4096 {
			return "", "", false
		}
		return strconv.Quote(s), "string", true
	}
	retI := func(n int) (string, string, bool) { return strconv.Itoa(n), "int", true }
	retB := func(b bool) (string, string, bool) { return strconv.FormatBool(b), "bool", true }
	retF := func(f float64) (string, string, bool) {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", "", false
		}
		if f == 0 && math.Signbit(f) {
			return "", "", false // -0 has no literal
		}
		return strconv.FormatFloat(f, 'g', -1, 64), "float64", true
	}
	switch pkg + "." + name {
	case "strconv.Itoa":
		if n, ok := i64(0); ok && len(a) == 1 {
			return retS(strconv.Itoa(int(n)))
		}
	case "strconv.FormatInt":
		n, ok1 := i64(0)
		b, ok2 := i64(1)
		if ok1 && ok2 && len(a) == 2 {
			return retS(strconv.FormatInt(n, int(b)))
		}
	case "strconv.FormatBool":
		if len(a) == 1 && a[0].Kind() == constant.Bool {
			return retS(strconv.FormatBool(constant.BoolVal(a[0])))
		}
	case "strconv.Quote":
		if s, ok := str(0); ok && len(a) == 1 {
			return retS(strconv.Quote(s))
		}
	case "strings.ToUpper", "strings.ToLower", "strings.TrimSpace":
		if s, ok := str(0); ok && len(a) == 1 {
			switch name {
			case "ToUpper":
				return retS(strings.ToUpper(s))
			case "ToLower":
				return retS(strings.ToLower(s))
			default:
				return retS(strings.TrimSpace(s))
			}
		}
	case "strings.Repeat":
		s, ok1 := str(0)
		n, ok2 := i64(1)
		if ok1 && ok2 && n >= 0 && int64(len(s))*n <= 4096 {
			return retS(strings.Repeat(s, int(n)))
		}
	case "strings.ReplaceAll", "strings.TrimPrefix", "strings.TrimSuffix", "strings.Trim",
		"strings.Contains", "strings.HasPrefix", "strings.HasSuffix", "strings.Index",
		"strings.LastIndex", "strings.Count", "strings.EqualFold":
		s, ok1 := str(0)
		t, ok2 := str(1)
		if !ok1 || !ok2 {
			return "", "", false
		}
		switch name {
		case "ReplaceAll":
			r, ok := str(2)
			if !ok || len(a) != 3 {
				return "", "", false
			}
			return retS(strings.ReplaceAll(s, t, r))
		case "TrimPrefix":
			return retS(strings.TrimPrefix(s, t))
		case "TrimSuffix":
			return retS(strings.TrimSuffix(s, t))
		case "Trim":
			return retS(strings.Trim(s, t))
		case "Contains":
			return retB(strings.Contains(s, t))
		case "HasPrefix":
			return retB(strings.HasPrefix(s, t))
		case "HasSuffix":
			return retB(strings.HasSuffix(s, t))
		case "Index":
			return retI(strings.Index(s, t))
		case "LastIndex":
			return retI(strings.LastIndex(s, t))
		case "Count":
			return retI(strings.Count(s, t))
		case "EqualFold":
			return retB(strings.EqualFold(s, t))
		}
	case "math.Sqrt", "math.Abs", "math.Floor", "math.Ceil", "math.Trunc":
		// Correctly rounded or exact on every implementation.
		x, ok := f64(0)
		if !ok || len(a) != 1 {
			return "", "", false
		}
		switch name {
		case "Sqrt":
			return retF(math.Sqrt(x))
		case "Abs":
			return retF(math.Abs(x))
		case "Floor":
			return retF(math.Floor(x))
		case "Ceil":
			return retF(math.Ceil(x))
		default:
			return retF(math.Trunc(x))
		}
	}
	return "", "", false
}

// sameGoVersion reports whether this binary was built by the Go release
// whose standard library the program is compiled against.
func (g *gen) sameGoVersion() bool {
	for _, p := range g.allPkgs {
		if p.PkgPath == "strings" && len(p.GoFiles) > 0 {
			root := filepath.Dir(filepath.Dir(filepath.Dir(p.GoFiles[0])))
			b, err := os.ReadFile(filepath.Join(root, "VERSION"))
			if err != nil {
				return false
			}
			first, _, _ := strings.Cut(string(b), "\n")
			return strings.TrimSpace(first) == runtime.Version()
		}
	}
	return false
}

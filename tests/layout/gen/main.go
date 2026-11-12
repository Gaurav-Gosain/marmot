// Command gen writes the programs of tests/run/layout. Each program defines structs with a
// different mix of field sizes and alignments (including zero-size fields and struct{} tails)
// and reads every field through every access path: directly, through a pointer, through a copy,
// through an interface, through reflect and through unsafe, for package globals (static and
// dynamic initialization), locals, heap objects, array, slice and map elements and channel
// elements, by value across calls and goroutines.
//
// Run from the repo root: go run ./tests/layout/gen -out tests/run/layout
// The generated programs are committed; the goldens come from stock Go (conform -update).
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

type kind struct {
	name string
	typ  string
	lit  string // static literal
	lit2 string // a different value for dynamic stores
	ptr  bool   // printed as *x
}

var kinds = []kind{
	{"b", "bool", "true", "false", false},
	{"i8", "int8", "-5", "7", false},
	{"u8", "uint8", "200", "13", false},
	{"i16", "int16", "-1234", "4321", false},
	{"u16", "uint16", "60000", "513", false},
	{"i32", "int32", "-100000", "70000", false},
	{"u32", "uint32", "4000000000", "99", false},
	{"i64", "int64", "-9000000000", "8000000000", false},
	{"u64", "uint64", "18000000000000000000", "77", false},
	{"up", "uintptr", "77", "88", false},
	{"f32", "float32", "1.5", "-2.75", false},
	{"f64", "float64", "-2.25", "1e100", false},
	{"c64", "complex64", "1 + 2i", "-3 - 4i", false},
	{"c128", "complex128", "3.5 - 1i", "0.5 + 0.25i", false},
	{"s", "string", `"héllo\t"`, `"x"`, false},
	{"sl", "[]int", "[]int{1, 2, 3}", "[]int{9}", false},
	{"any", "any", "42", `"dyn"`, false},
	{"p", "*int", "&ints[2]", "&ints[5]", true},
	{"a3", "[3]byte", "[3]byte{1, 2, 3}", "[3]byte{7, 8, 9}", false},
	{"a2", "[2]int16", "[2]int16{-1, 2}", "[2]int16{300, -300}", false},
	{"n1", "N1", "N1{3, -7}", "N1{-3, 70000000000}", false},
	{"n2", "N2", "N2{true, [3]byte{9, 8, 7}, 2.5}", "N2{false, [3]byte{1, 1, 1}, -0.5}", false},
	{"r", "rune", "'x'", "'世'", false},
	{"i", "int", "-42", "123456789", false},
}

var zero = []kind{
	{"z0", "[0]int", "[0]int{}", "[0]int{}", false},
	{"z1", "struct{}", "struct{}{}", "struct{}{}", false},
	{"z2", "[0]int64", "[0]int64{}", "[0]int64{}", false},
}

const prelude = `package main

import (
	"fmt"
	"reflect"
	"sync"
	"unsafe"
)

var ints = [8]int{10, 11, 12, 13, 14, 15, 16, 17}

type N1 struct {
	a int8
	b int64
}

type N2 struct {
	x bool
	y [3]byte
	z float32
}

var _ = fmt.Sprint
var _ = reflect.TypeOf
var _ sync.Mutex
var _ unsafe.Pointer
`

func main() {
	out := flag.String("out", "tests/run/layout", "output directory")
	n := flag.Int("n", 48, "number of programs")
	flag.Parse()
	os.MkdirAll(*out, 0o755)
	for i := 0; i < *n; i++ {
		src := program(i)
		name := fmt.Sprintf("mix_%02d.go", i+1)
		if err := os.WriteFile(filepath.Join(*out, name), []byte(src), 0o644); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
	fmt.Println("wrote", *n, "programs to", *out)
}

func program(seed int) string {
	r := rand.New(rand.NewSource(int64(seed)*7919 + 3))
	nf := 4 + r.Intn(6)
	var fs []kind
	for i := 0; i < nf; i++ {
		fs = append(fs, kinds[r.Intn(len(kinds))])
	}
	// zero-size fields at the start, middle or end; sometimes a struct{} tail
	switch seed % 4 {
	case 0:
		fs = append([]kind{zero[r.Intn(len(zero))]}, fs...)
	case 1:
		m := len(fs) / 2
		fs = append(fs[:m:m], append([]kind{zero[r.Intn(len(zero))]}, fs[m:]...)...)
	case 2:
		fs = append(fs, zero[r.Intn(len(zero))])
	case 3:
		fs = append(fs, kinds[r.Intn(len(kinds))], zero[1])
	}
	for i := range fs {
		fs[i].name = fmt.Sprintf("%s_%d", fs[i].name, i)
	}
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	b.WriteString(prelude)
	w("\ntype T struct {\n")
	for _, f := range fs {
		w("\t%s %s\n", f.name, f.typ)
	}
	w("}\n\n")
	embed := seed%3 == 0
	if embed {
		w("type O struct {\n\tpre byte\n\tT\n\tpost int16\n}\n\n")
	} else {
		w("type O struct {\n\tpre byte\n\tin  T\n\tpost int16\n\tz struct{}\n}\n\n")
	}
	inner := "in."
	if embed {
		inner = ""
	}
	comparable := true
	for _, f := range fs {
		if f.typ == "[]int" {
			comparable = false
		}
	}
	lit := func(typ string, alt bool, pre, post string) string {
		var sb strings.Builder
		if typ == "T" {
			sb.WriteString("T{")
			for i, f := range fs {
				if i > 0 {
					sb.WriteString(", ")
				}
				v := f.lit
				if alt {
					v = f.lit2
				}
				sb.WriteString(f.name + ": " + v)
			}
			sb.WriteString("}")
			return sb.String()
		}
		t := ""
		{
			var tb strings.Builder
			tb.WriteString("T{")
			for i, f := range fs {
				if i > 0 {
					tb.WriteString(", ")
				}
				v := f.lit
				if alt {
					v = f.lit2
				}
				tb.WriteString(f.name + ": " + v)
			}
			tb.WriteString("}")
			t = tb.String()
		}
		if embed {
			return fmt.Sprintf("O{pre: %s, T: %s, post: %s}", pre, t, post)
		}
		return fmt.Sprintf("O{pre: %s, in: %s, post: %s}", pre, t, post)
	}
	// field accessors: path is an expression of type T or O
	show := func(x string, o bool) string {
		p := x + "."
		if o {
			p = x + "." + inner
		}
		var parts []string
		for _, f := range fs {
			e := p + f.name
			if f.ptr {
				e = "*" + e
			}
			parts = append(parts, e)
		}
		return strings.Join(parts, ", ")
	}
	// printing helpers
	w("//go:noinline\nfunc showT(tag string, x T) {\n\tfmt.Println(tag, %s)\n}\n\n", show("x", false))
	w("//go:noinline\nfunc showTP(tag string, x *T) {\n\tfmt.Println(tag, %s)\n}\n\n", show("x", false))
	w("//go:noinline\nfunc showO(tag string, x O) {\n\tfmt.Println(tag, x.pre, %s, x.post)\n}\n\n", show("x", true))
	w("//go:noinline\nfunc showOP(tag string, x *O) {\n\tfmt.Println(tag, x.pre, %s, x.post)\n}\n\n", show("x", true))
	// globals
	w("var gT = %s\nvar gO = %s\nvar dT, dO = T{}, O{}\n", lit("T", false, "", ""), lit("O", false, "7", "-9"))
	w("var aT = [3]T{%s, {}, %s}\nvar sT = []T{%s, %s}\n", lit("T", false, "", ""), lit("T", true, "", ""), lit("T", true, "", ""), lit("T", false, "", ""))
	w("var aO = [2]O{%s, %s}\nvar mT = map[int]T{}\n", lit("O", false, "1", "2"), lit("O", true, "3", "4"))
	first := fs[0]
	for _, f := range fs {
		if f.typ == "int" || f.typ == "int64" || f.typ == "int32" || f.typ == "uint8" || f.typ == "string" || f.typ == "float64" {
			first = f
			break
		}
	}
	w("var pField = &gT.%s\nvar pFieldO = &gO.%s%s\nvar pElem = &aT[2].%s\nvar pBlank = &aO[1].post\n\n", first.name, inner, first.name, first.name)
	// dynamic init
	w("//go:noinline\nfunc setDyn(p *T, alt bool) {\n\tif alt {\n")
	for _, f := range fs {
		w("\t\tp.%s = %s\n", f.name, f.lit2)
	}
	w("\t} else {\n")
	for _, f := range fs {
		w("\t\tp.%s = %s\n", f.name, f.lit)
	}
	w("\t}\n}\n\n")
	// sizes
	w("func sizes() {\n\tvar t T\n\tvar o O\n\tfmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))\n")
	for _, f := range fs {
		w("\tfmt.Println(%q, unsafe.Offsetof(t.%s), unsafe.Sizeof(t.%s), unsafe.Alignof(t.%s))\n", f.name, f.name, f.name, f.name)
	}
	w("\tfmt.Println(unsafe.Offsetof(o.pre), unsafe.Offsetof(o.%s), unsafe.Offsetof(o.post))\n}\n\n", map[bool]string{true: "T", false: "in"}[embed])
	// reflect
	w("func viaReflect(tag string, v any) {\n\trv := reflect.ValueOf(v)\n\tfor i := 0; i < rv.NumField(); i++ {\n\t\tf := rv.Field(i)\n\t\tif f.Kind() == reflect.Pointer {\n\t\t\tfmt.Println(tag, i, rv.Type().Field(i).Name, f.Elem().Interface(), rv.Type().Field(i).Offset)\n\t\t} else {\n\t\t\tfmt.Println(tag, i, rv.Type().Field(i).Name, f.Interface(), rv.Type().Field(i).Offset)\n\t\t}\n\t}\n}\n\n")
	// unsafe reads through raw pointers at Offsetof
	w("func viaUnsafe(p *T) {\n")
	for _, f := range fs {
		if f.typ == "int64" || f.typ == "int32" || f.typ == "int16" || f.typ == "int8" || f.typ == "uint8" || f.typ == "uint16" || f.typ == "uint32" || f.typ == "uint64" || f.typ == "int" || f.typ == "bool" || f.typ == "float32" || f.typ == "float64" || f.typ == "string" || f.typ == "rune" || f.typ == "uintptr" {
			w("\tfmt.Println(*(*%s)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.%s))))\n", f.typ, f.name)
		}
	}
	w("}\n\n")
	// main
	w("func main() {\n")
	w("\tsizes()\n")
	w("\tshowT(\"static\", gT)\n\tshowTP(\"static ptr\", &gT)\n\tshowO(\"static O\", gO)\n\tshowOP(\"static O ptr\", &gO)\n")
	w("\tfmt.Println(\"direct\", %s)\n", show("gT", false))
	w("\tfmt.Println(\"directO\", gO.pre, %s, gO.post)\n", show("gO", true))
	w("\tfmt.Println(*pField, *pFieldO, *pElem, *pBlank)\n")
	w("\tsetDyn(&dT, false)\n\tshowT(\"dyn\", dT)\n\tsetDyn(&dT, true)\n\tshowTP(\"dyn alt\", &dT)\n")
	w("\tdT = %s\n\tshowT(\"whole store\", dT)\n", lit("T", false, "", ""))
	w("\tdO = gO\n\tdO.post = 5\n\tshowO(\"copy O\", dO)\n\tshowO(\"orig O\", gO)\n")
	w("\tfor i := range aT {\n\t\tshowT(fmt.Sprint(\"arr \", i), aT[i])\n\t}\n")
	w("\tfor i, e := range sT {\n\t\tshowTP(fmt.Sprint(\"slice \", i), &sT[i])\n\t\t_ = e\n\t}\n")
	w("\tfor i := range aO {\n\t\tshowOP(fmt.Sprint(\"arrO \", i), &aO[i])\n\t}\n")
	w("\tmT[1] = gT\n\tmT[2] = sT[0]\n\tfor k := 1; k <= 2; k++ {\n\t\tshowT(fmt.Sprint(\"map \", k), mT[k])\n\t}\n")
	// locals and heap
	w("\tloc := %s\n\tshowT(\"local\", loc)\n\tshowTP(\"local ptr\", &loc)\n", lit("T", true, "", ""))
	w("\thp := new(T)\n\tsetDyn(hp, false)\n\tshowTP(\"heap\", hp)\n\thp2 := &T{}\n\t*hp2 = gT\n\tshowTP(\"heap copy\", hp2)\n")
	w("\tho := &O{pre: 4}\n\tho.%s = gT\n\tshowOP(\"heap O\", ho)\n", map[bool]string{true: "T", false: "in"}[embed])
	// interface, reflect
	w("\tvar any1 any = gT\n\tshowT(\"iface\", any1.(T))\n\tvar any2 any = &gT\n\tshowTP(\"iface ptr\", any2.(*T))\n")
	w("\tviaReflect(\"reflect\", gT)\n\tviaReflect(\"reflect loc\", loc)\n\tviaReflect(\"reflect ptr\", *any2.(*T))\n")
	w("\tviaUnsafe(&gT)\n\tviaUnsafe(&loc)\n\tviaUnsafe(hp)\n")
	if comparable {
		w("\tcp := gT\n\tfmt.Println(cp == gT, gT == %s, dT == gT)\n\tcp.%s = %s\n\tfmt.Println(cp == gT)\n", lit("T", false, "", ""), fs[len(fs)-1].name, fs[len(fs)-1].lit2)
		w("\tfmt.Println(aT[0] == aT[2], gO == dO)\n")
	}
	// channels and goroutines
	w("\tch := make(chan T, 2)\n\tch <- gT\n\tch <- loc\n\tshowT(\"chan 1\", <-ch)\n\tshowT(\"chan 2\", <-ch)\n")
	w("\tvar wg sync.WaitGroup\n\tres := make([]string, 3)\n\tfor i := 0; i < 3; i++ {\n\t\twg.Add(1)\n\t\tgo func(v T, o O) {\n\t\t\tdefer wg.Done()\n\t\t\tres[i] = fmt.Sprint(%s, o.post)\n\t\t}(sT[i%%2], gO)\n\t}\n\twg.Wait()\n\tfmt.Println(res)\n", show("v", false))
	w("\tdone := make(chan struct{})\n\tgo func() { showTP(\"go closure\", &aT[2]); close(done) }()\n\t<-done\n")
	w("}\n")
	return b.String()
}

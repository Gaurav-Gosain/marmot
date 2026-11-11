package specialize

import (
	"fmt"
	"strings"
	"testing"
)

// fmtProgram generates a program with one constant format call site per
// (type, format) pair, each applied to many values of that type.
func fmtProgram() (string, int) {
	var b strings.Builder
	b.WriteString(`package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
)

type Celsius float64
type Name string
type Count int8
type Flag bool
type Stringy int

func (s Stringy) String() string { return fmt.Sprint("S", int(s)) }

type Fmter struct{ x int }

func (f Fmter) Format(s fmt.State, verb rune) {
	w, wok := s.Width()
	p, pok := s.Precision()
	fmt.Fprint(s, "F", string(verb), w, wok, p, pok, s.Flag('+'), s.Flag('#'), s.Flag('-'), f.x)
}

type Pt struct {
	X, Y int
	Tag  string
}

type errT struct{ msg string }

func (e *errT) Error() string { return "errT:" + e.msg }

var sb strings.Builder

func out(s string) { sb.WriteString(s); sb.WriteByte('\n') }

`)
	type group struct {
		typ     string
		values  string
		formats []string
	}
	flags := []string{"", "+", "-", "#", " ", "0", "+0", "-0", "# ", "+#"}
	widths := []string{"", "1", "5", "12"}
	precs := []string{"", ".0", ".3", ".10"}
	gen := func(verbs string, useFlags, useWid, usePrec bool) []string {
		var fs []string
		fl, wd, pr := []string{""}, []string{""}, []string{""}
		if useFlags {
			fl = flags
		}
		if useWid {
			wd = widths
		}
		if usePrec {
			pr = precs
		}
		for _, v := range verbs {
			for _, f := range fl {
				for _, w := range wd {
					for _, p := range pr {
						fs = append(fs, "<%"+f+w+p+string(v)+">")
					}
				}
			}
		}
		return fs
	}
	groups := []group{
		{"int", "0, 1, -1, 42, -42, 255, 1 << 31, math.MaxInt64, math.MinInt64, 0x10FFFF, 0xD800", gen("dvbxXoOcqU", true, true, true)},
		{"uint64", "0, 1, 255, 1 << 63, math.MaxUint64", gen("dvxXbo", true, true, false)},
		{"int8", "0, -128, 127, -1", gen("dvxc", true, true, false)},
		{"uint8", "0, 65, 255", gen("dvxcq", true, false, false)},
		{"uintptr", "0, 4096", gen("dvx", true, false, false)},
		{"float64", "0, math.Copysign(0, -1), 1, -1, 0.1, 1.0 / 3, 1e21, 1e-7, 123456789.125, math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1), math.NaN()",
			gen("vfFeEgGxXb", true, true, true)},
		{"float32", "0, 1.5, -0.1, 3.4e38, float32(math.Inf(-1)), float32(math.NaN())", gen("vfeg", true, true, true)},
		{"complex128", "0, complex(1, -2), complex(math.NaN(), math.Inf(1))", gen("vfe", true, true, true)},
		{"string", `"", "a", "hello, world", "日本語", "tab\there", "\x00\xff", "quote\"s", "` + "`back`" + `"`, gen("vsqxX", true, true, true)},
		{"bool", "true, false", gen("vtd", true, true, false)},
		{"[]byte", `nil, []byte{}, []byte("hi"), []byte{0, 1, 255}`, gen("vdsqxX", true, true, false)},
		{"Celsius", "0, -40, 36.6, Celsius(math.NaN())", gen("vfg", true, true, true)},
		{"Name", `"", "bob", "çé"`, gen("vsq", true, true, false)},
		{"Count", "-3, 0, 7", gen("vdx", true, true, false)},
		{"Flag", "true, false", gen("vt", false, true, false)},
		{"Stringy", "1, -2", gen("vsdq", true, true, false)},
		{"Fmter", "Fmter{1}, Fmter{2}", gen("vdsx", true, true, true)},
		{"Pt", `Pt{}, Pt{1, -2, "a b"}`, gen("vd", true, true, false)},
		{"*Pt", `nil`, gen("vd", false, true, false)},
		{"any", `nil, 1, "s", 2.5, Pt{3, 4, "t"}, Stringy(9), []int{1, 2}, map[string]int{"a": 1, "b": 2}`, gen("vsdT", true, true, false)},
		{"error", `nil, errors.New("boom"), &errT{"x"}`, gen("vsqw", true, true, false)},
		{"[]int", `nil, []int{1, -2, 3}`, gen("vdx", true, true, false)},
		{"rune", `'a', 'é', -1, 0x10FFFF + 1`, gen("cqUvd", true, true, false)},
	}
	sites := 0
	for gi, g := range groups {
		fmt.Fprintf(&b, "func group%d() {\n\tfor _, v := range []%s{%s} {\n", gi, g.typ, g.values)
		for _, f := range g.formats {
			fmt.Fprintf(&b, "\t\tout(fmt.Sprintf(%q, v))\n", f)
			sites++
		}
		b.WriteString("\t}\n}\n\n")
	}
	// Mixed formats, other entry points and operand kinds.
	b.WriteString(`func mixed() {
	i, s, f := 7, "str", 2.5
	var e error = &errT{"m"}
	var nilErr error
	out(fmt.Sprintf("%d: %s %5.2f %x %v %q\n", i, "row", float64(i)/7, i*31, i%2 == 0, "q"))
	out(fmt.Sprintf("ts=%08d level=%-5s id=%d ok=%t delay=%.3f ev=%v", i, s, i, true, f, Pt{1, 2, "z"}))
	out(fmt.Sprintf("%%d %% %5%|%-5%|"))
	out(fmt.Sprintf("no verbs"))
	out(fmt.Sprintf("%v %+v %#v", Pt{1, 2, "a"}, Pt{1, 2, "a"}, Pt{1, 2, "a"}))
	out(fmt.Sprintf("%z %!", i))
	out(fmt.Sprintf("%5.d|%.d|%.0d", 0, 0, 0))
	out(fmt.Sprintf("%6.2f%%", 99.5))
	out(fmt.Sprintf("%T %T %T %T", i, s, nilErr, e))
	out(fmt.Sprintf("%w", e))
	out(fmt.Sprintf("%s|%v|%d", nilErr, nilErr, nilErr))
	out(fmt.Sprint("a", 1, 2, "b", "c", 3.5, true, false, Pt{}, nil, e))
	out(fmt.Sprint(1, 2))
	out(fmt.Sprint())
	out(fmt.Sprint(Name("n"), 1, Name("m")))
	out(fmt.Sprintln("a", 1, 2, "b", nil, e, Stringy(3)))
	out(fmt.Sprintln())
	out(string(fmt.Append([]byte("pre:"), 1, "x", 2)))
	out(string(fmt.Appendf([]byte("pre:"), "%03d|%s", 5, "y")))
	out(string(fmt.Appendln(nil, 1, "x")))
	err1 := fmt.Errorf("wrap %d %s", 3, "x")
	out(err1.Error())
	err2 := fmt.Errorf("w: %w", e)
	out(fmt.Sprint(err2, errors.Unwrap(err2) == e))
	err3 := fmt.Errorf("plain")
	out(err3.Error())
	n, werr := fmt.Fprintf(&sb, "[%d|%s]\n", 12, "fp")
	out(fmt.Sprint(n, werr))
	n, werr = fmt.Fprint(&sb, "x", 1, 2, "\n")
	out(fmt.Sprint(n, werr))
	n, werr = fmt.Fprintln(&sb, "y", 3)
	out(fmt.Sprint(n, werr))
	type local struct{ a int }
	out(fmt.Sprintf("%v %+v", local{1}, struct{ B string }{"b"}))
	order := 0
	next := func() int { order++; return order }
	out(fmt.Sprintf("%d %d %d", next(), next(), next()))
	fs := "%d dynamic"
	out(fmt.Sprintf(fs, 1))
	args := []any{1, 2}
	out(fmt.Sprintf("%d %d", args...))
}

func main() {
`)
	for gi := range groups {
		fmt.Fprintf(&b, "\tgroup%d()\n", gi)
	}
	b.WriteString(`	mixed()
	os.Stdout.WriteString(sb.String())
	fmt.Printf("%s=%d\n", "printf", 1)
	fmt.Print("print", 2, "\n")
	fmt.Println("println", 3, 4.5, nil)
	fmt.Fprintf(os.Stderr, "stderr %d\n", 5)
}
`)
	return b.String(), sites + 20
}

func TestFmtDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	src, minSites := fmtProgram()
	diffTest(t, src, minSites)
}

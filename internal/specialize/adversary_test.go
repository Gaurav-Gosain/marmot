package specialize

import "testing"

// Adversarial cases for the specializations (audit round 2): method
// precedence in fmt, nil receivers and interfaces, widths from arguments,
// panicking methods; json embedding, omitempty on every kind, duplicate and
// case-folded keys; sort stability and panicking comparators.
const adversaryProgram = `package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

type All struct{ n int }

func (a All) Error() string                 { return "err" }
func (a All) String() string                { return "str" }
func (a All) GoString() string              { return "gostr" }
func (a All) Format(s fmt.State, verb rune) { fmt.Fprintf(s, "fmt(%c)", verb) }

type ES struct{}

func (ES) Error() string  { return "E" }
func (ES) String() string { return "S" }

type GS struct{ n int }

func (GS) GoString() string { return "GS!" }
func (GS) String() string   { return "GS" }

type PV struct{ n int }

func (p PV) String() string { return fmt.Sprint("PV", p.n) }

type PP struct{ n int }

func (p *PP) String() string {
	if p == nil {
		return "nil PP"
	}
	return "PP"
}

type Pan int

func (p Pan) String() string {
	switch p {
	case 1:
		panic("plain")
	case 2:
		panic(nil)
	case 3:
		panic(errors.New("err value"))
	case 4:
		panic(Pan(1))
	}
	return "ok"
}

type Inner struct {
	A int ` + "`json:\"a\"`" + `
	B string
}

type Outer struct {
	Inner
	C int ` + "`json:\"c,omitempty\"`" + `
}

type OE struct {
	Bo  bool              ` + "`json:\",omitempty\"`" + `
	I   int               ` + "`json:\",omitempty\"`" + `
	U   uint              ` + "`json:\",omitempty\"`" + `
	F   float64           ` + "`json:\",omitempty\"`" + `
	S   string            ` + "`json:\",omitempty\"`" + `
	P   *int              ` + "`json:\",omitempty\"`" + `
	Sl  []int             ` + "`json:\",omitempty\"`" + `
	M   map[string]int    ` + "`json:\",omitempty\"`" + `
	A0  [0]int            ` + "`json:\",omitempty\"`" + `
	A1  [1]int            ` + "`json:\",omitempty\"`" + `
	St  struct{}          ` + "`json:\",omitempty\"`" + `
	If  any               ` + "`json:\",omitempty\"`" + `
}

type CI struct {
	Name  string
	Value int ` + "`json:\"value\"`" + `
}

type rec struct {
	k, i int
}

var sb strings.Builder

func out(s string) { sb.WriteString(s); sb.WriteByte('\n') }

func main() {
	a := All{1}
	out(fmt.Sprintf("%v|%s|%d|%#v|%q|%x|%+v", a, a, a, a, a, a, a))
	es := ES{}
	out(fmt.Sprintf("%v|%s|%q|%x|%d|%#v", es, es, es, es, es, es))
	g := GS{1}
	out(fmt.Sprintf("%v|%#v|%s|%+v", g, g, g, g))
	var pv *PV
	out(fmt.Sprintf("%v|%s|%d|%p", pv, pv, pv, pv == nil))
	out(fmt.Sprintf("%v|%s", PV{2}, &PV{3}))
	var pp *PP
	out(fmt.Sprintf("%v|%s|%+v", pp, pp, PP{}))
	var e error
	var s fmt.Stringer
	out(fmt.Sprintf("%v|%s|%d|%x|%T|%+v|%#v", e, e, e, e, e, s, s))
	out(fmt.Sprintf("%*d|%-*d|%.*f|%*.*f|%*s", 5, 1, 5, 2, 2, 3.14159, 8, 3, 2.5, -6, "ab"))
	out(fmt.Sprintf("%[2]d %[1]d %[2]*[1]d", 1, 4))
	// Pan(4) would panic with a value whose String panics again: fatal in
	// gc ("panic while printing panic value") and specialized alike.
	for i := 0; i < 4; i++ {
		out(fmt.Sprintf("%v|%s|%d|%q", Pan(i), Pan(i), Pan(i), Pan(i)))
	}
	out(fmt.Sprintln(Pan(1), Pan(0)))

	b, err := json.Marshal(Outer{Inner{1, "x"}, 0})
	out(fmt.Sprint(string(b), err))
	var o Outer
	err = json.Unmarshal([]byte(` + "`" + `{"a":5,"B":"y","c":7,"Inner":{"a":9}}` + "`" + `), &o)
	out(fmt.Sprintf("%+v %v", o, err))
	b, err = json.Marshal(OE{})
	out(fmt.Sprint(string(b), err))
	one := 0
	b, err = json.Marshal(OE{Bo: true, I: -1, U: 1, F: -0.0, S: "s", P: &one, Sl: []int{}, M: map[string]int{}, A1: [1]int{0}, If: 0})
	out(fmt.Sprint(string(b), err))
	var ci CI
	err = json.Unmarshal([]byte(` + "`" + `{"name":"lower","NAME":"upper","Value":1,"value":2,"VALUE":3}` + "`" + `), &ci)
	out(fmt.Sprintf("%+v %v", ci, err))
	var ci2 CI
	err = json.Unmarshal([]byte(` + "`" + `{"Name":"a","Name":"b","nAmE":"c"}` + "`" + `), &ci2)
	out(fmt.Sprintf("%+v %v", ci2, err))

	rs := make([]rec, 50)
	for i := range rs {
		rs[i] = rec{i % 4, i}
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].k < rs[j].k })
	out(fmt.Sprint(rs[:12]))
	calls := 0
	func() {
		defer func() { out(fmt.Sprint("sort panic recovered: ", recover(), " after calls ", calls)) }()
		sort.Slice(rs, func(i, j int) bool {
			calls++
			if calls == 37 {
				panic("cmp panic")
			}
			return rs[i].i > rs[j].i
		})
	}()
	out(fmt.Sprint(rs[:10]))
	os.Stdout.WriteString(sb.String())
}
`

func TestAdversaryDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, adversaryProgram, 20)
}

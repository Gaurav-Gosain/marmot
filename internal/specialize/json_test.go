package specialize

import "testing"

// jsonProgram marshals many values of many types with json.Marshal. Each
// Marshal call site has a static type, so each one is specialized; the
// output (bytes, capacity class aside, and errors) must match stock Go.
const jsonProgram = `package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

type Item struct {
	ID    int               ` + "`json:\"id\"`" + `
	Name  string            ` + "`json:\"name\"`" + `
	Price float64           ` + "`json:\"price\"`" + `
	Tags  []string          ` + "`json:\"tags\"`" + `
	Attrs map[string]string ` + "`json:\"attrs\"`" + `
	Next  *Item             ` + "`json:\"next,omitempty\"`" + `
}

type Omit struct {
	A  int               ` + "`json:\",omitempty\"`" + `
	B  string            ` + "`json:\"b,omitempty\"`" + `
	C  bool              ` + "`json:\"c,omitempty\"`" + `
	D  float64           ` + "`json:\"d,omitempty\"`" + `
	E  []int             ` + "`json:\"e,omitempty\"`" + `
	F  map[string]int    ` + "`json:\"f,omitempty\"`" + `
	G  *int              ` + "`json:\"g,omitempty\"`" + `
	H  [0]int            ` + "`json:\"h,omitempty\"`" + `
	I  [2]int            ` + "`json:\"i,omitempty\"`" + `
	J  struct{ X int }   ` + "`json:\"j,omitempty\"`" + `
	K  uint8             ` + "`json:\"k,omitempty\"`" + `
	L  float32           ` + "`json:\"l,omitempty\"`" + `
	Skip int             ` + "`json:\"-\"`" + `
	Dash int             ` + "`json:\"-,\"`" + `
	hidden int
}

type Nums struct {
	I8  int8
	I16 int16
	I32 int32
	I64 int64
	U   uint
	U8  uint8
	U16 uint16
	U32 uint32
	U64 uint64
	UP  uintptr
	F32 float32
	F64 float64
}

type Label string
type Score float64

type Named struct {
	L  Label
	S  Score
	LL []Label
	M  map[Label]Score
	A  [3]int16
	PP **int
}

type Node struct {
	V    int
	Kids []*Node
	Up   *Node ` + "`json:\"-\"`" + `
}

type Cyc struct {
	Next *Cyc
}

type WithTime struct {
	T time.Time
}

type Unicode struct {
	Ünï string ` + "`json:\"u\"`" + `
}

var out strings.Builder

func show(b []byte, err error) {
	if err != nil {
		fmt.Fprintf(&out, "ERR %T %v\n", err, err)
		return
	}
	out.Write(b)
	out.WriteByte('\n')
}

func main() {
	items := []Item{
		{},
		{ID: 1, Name: "a", Price: 2.5, Tags: []string{"x", ""}, Attrs: map[string]string{"z": "1", "a": "2", "é": "3", "<": ">&"}},
		{ID: -9, Name: "<script>&  \x00\x1f\"\\/\t\n\r\b\f\x7f\xff\xfe日本", Price: -0.0, Tags: []string{}, Attrs: map[string]string{}},
		{ID: math.MaxInt64, Price: 1e21, Next: &Item{ID: 2, Price: 1e-7, Next: &Item{Price: 123456789.125}}},
		{Price: 1e20}, {Price: 1e-6}, {Price: 0.000001234}, {Price: math.SmallestNonzeroFloat64}, {Price: math.MaxFloat64},
	}
	for i := range items {
		show(json.Marshal(items[i]))
		show(json.Marshal(&items[i]))
	}
	show(json.Marshal(items))
	show(json.Marshal(items[:0]))
	var nilItems []Item
	show(json.Marshal(nilItems))
	var nilPtr *Item
	show(json.Marshal(nilPtr))
	show(json.Marshal(Item{Price: math.NaN()}))
	show(json.Marshal([]Item{{}, {Price: math.Inf(1)}}))
	show(json.Marshal(map[string]Item{"k": {Price: math.Inf(-1)}}))

	g := 7
	omits := []Omit{{}, {A: 1, B: "b", C: true, D: -0.0, E: []int{}, F: map[string]int{}, G: &g, I: [2]int{0, 0}, K: 0, L: 0}, {E: []int{1}, F: map[string]int{"x": 1}, K: 3, L: 1.5, Skip: 5, Dash: 6, hidden: 7}}
	for _, o := range omits {
		show(json.Marshal(o))
	}
	show(json.Marshal(Nums{-128, -32768, math.MinInt32, math.MinInt64, math.MaxUint64 >> 1, 255, 65535, math.MaxUint32, math.MaxUint64, 4096, math.MaxFloat32, -math.SmallestNonzeroFloat64}))
	show(json.Marshal(Nums{F32: 1e-7, F64: 1e300}))
	show(json.Marshal(Nums{F32: 0.1, F64: 0.1}))
	show(json.Marshal(Nums{F32: 16777217}))
	show(json.Marshal(Nums{F32: float32(math.Inf(1))}))
	one := 1
	pone := &one
	show(json.Marshal(Named{L: "l", S: 2.25, LL: []Label{"b", "a"}, M: map[Label]Score{"y": 1, "x": 2}, A: [3]int16{1, -2, 3}, PP: &pone}))
	show(json.Marshal(Named{}))
	tree := &Node{V: 1, Kids: []*Node{{V: 2}, nil, {V: 3, Kids: []*Node{}}}}
	tree.Kids[0].Up = tree
	show(json.Marshal(tree))
	c := &Cyc{}
	c.Next = c
	show(json.Marshal(c))
	deep := &Node{}
	for i := 0; i < 1500; i++ {
		deep = &Node{V: i, Kids: []*Node{deep}}
	}
	b, err := json.Marshal(deep)
	fmt.Fprintln(&out, len(b), err)
	show(json.Marshal(WithTime{}))
	show(json.Marshal(Unicode{"ü"}))
	show(json.Marshal(map[string][]float64{"b": {1, 2.5}, "a": nil, "": {}}))
	show(json.Marshal([][]int{{1}, nil, {}}))
	show(json.Marshal(map[string]*Item{"n": nil}))
	show(json.Marshal(3))
	show(json.Marshal("s<>"))
	show(json.Marshal(true))
	show(json.Marshal([]bool{true, false}))
	show(json.Marshal([]byte("bytes")))
	show(json.Marshal(struct {
		A int ` + "`json:\"a\"`" + `
		B string
	}{1, "x"}))
	var anyv any = Item{ID: 5}
	show(json.Marshal(anyv))
	keys := map[string]int{}
	for i := 0; i < 50; i++ {
		keys[fmt.Sprint("k", i*7%50)] = i
	}
	show(json.Marshal(keys))
	big := make([]Item, 3000)
	for i := range big {
		big[i] = Item{ID: i, Name: fmt.Sprint("item-", i), Price: float64(i%500) + 0.25, Tags: []string{"a", fmt.Sprint("t", i%10)}, Attrs: map[string]string{"size": fmt.Sprint(i % 7)}}
	}
	b, err = json.Marshal(big)
	fmt.Fprintln(&out, len(b), err, string(b[:200]), string(b[len(b)-200:]))
	enc := json.NewEncoder(&out)
	for i := range items {
		fmt.Fprintln(&out, enc.Encode(items[i]))
	}
	fmt.Fprintln(&out, enc.Encode(Item{Price: math.NaN()}))
	enc.SetEscapeHTML(false)
	fmt.Fprintln(&out, enc.Encode(items[2]))
	enc.SetEscapeHTML(true)
	enc.SetIndent(">", "  ")
	fmt.Fprintln(&out, enc.Encode(items[1]))
	enc.SetIndent("", "")
	fmt.Fprintln(&out, enc.Encode(omits[2]))
	bad := json.NewEncoder(failWriter{})
	fmt.Fprintln(&out, bad.Encode(items[0]), bad.Encode(items[1]))
	os.Stdout.WriteString(out.String())
}

type failWriter struct{}

func (failWriter) Write(p []byte) (int, error) { return 0, fmt.Errorf("write failed after %d", len(p)) }
`

func TestJSONDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, jsonProgram, 34)
}

// jsonDecodeProgram unmarshals many inputs, valid and not, into values that
// start empty and values that start filled, and prints the result, the
// error, and slice capacities (the legacy merge rules decide those).
const jsonDecodeProgram = `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var ptrRE = regexp.MustCompile("0x[0-9a-f]+")

type Item struct {
	ID    int               ` + "`json:\"id\"`" + `
	Name  string            ` + "`json:\"name\"`" + `
	Price float64           ` + "`json:\"price\"`" + `
	Tags  []string          ` + "`json:\"tags\"`" + `
	Attrs map[string]string ` + "`json:\"attrs\"`" + `
	Next  *Item             ` + "`json:\"next,omitempty\"`" + `
}

type Nums struct {
	I8  int8
	I16 int16
	I32 int32
	I64 int64
	U8  uint8
	U16 uint16
	U32 uint32
	U64 uint64
	F32 float32
	F64 float64
	B   bool
	S   string
	hidden int
}

type Label string

type Named struct {
	L  Label
	LL []Label
	M  map[Label]int
	P  *int
	PP **Nums
}

var out strings.Builder

func show(name string, v any, err error) {
	out.WriteString(ptrRE.ReplaceAllString(fmt.Sprintf("%s: %+v | err=%v\n", name, v, err), "PTR"))
}

func main() {
	itemInputs := []string{
		` + "`" + `{"id":1,"name":"a","price":2.5,"tags":["x","y"],"attrs":{"k":"v"},"next":{"id":2}}` + "`" + `,
		` + "`" + `  {"id" : -3 , "name":"é\n\"q\"\\", "price":1e3, "tags":[], "attrs":{}, "next":null}  ` + "`" + `,
		` + "`" + `{"ID":4}` + "`" + `,
		` + "`" + `{"Name":"case","PRICE":1}` + "`" + `,
		` + "`" + `{"unknown":{"a":[1,2,{"b":null}]},"id":5,"x":"😀"}` + "`" + `,
		` + "`" + `{"id":6,"id":7,"tags":["a"],"tags":["b","c"]}` + "`" + `,
		` + "`" + `{"id":1.5}` + "`" + `,
		` + "`" + `{"id":"1"}` + "`" + `,
		` + "`" + `{"id":99999999999999999999}` + "`" + `,
		` + "`" + `{"price":1e400}` + "`" + `,
		` + "`" + `{"name":5}` + "`" + `,
		` + "`" + `{"tags":"x"}` + "`" + `,
		` + "`" + `{"id":1,}` + "`" + `,
		` + "`" + `{"id":1` + "`" + `,
		` + "`" + `{"id":1} x` + "`" + `,
		` + "`" + `{"id":01}` + "`" + `,
		` + "`" + `{"name":"a\x01b"}` + "`" + `,
		"{\"name\":\"bad\xff\xfeutf8\"}",
		` + "`" + `{"name":"\ud800 lone"}` + "`" + `,
		` + "`" + `null` + "`" + `,
		` + "`" + `{"id":null,"name":null,"tags":null,"attrs":null,"next":null}` + "`" + `,
		` + "`" + `[]` + "`" + `,
		` + "`" + `` + "`" + `,
		` + "`" + `{"attrs":{"a":"1","a":"2"}}` + "`" + `,
		` + "`" + `{"next":{"next":{"next":{"id":9}}}}` + "`" + `,
		` + "`" + `{"Ünknown":1}` + "`" + `,
		` + "`" + `{"nAmE":"fold"}` + "`" + `,
		` + "`" + `{"id":-0,"price":-0.0}` + "`" + `,
		` + "`" + `{"price":1E-2,"id":0}` + "`" + `,
		` + "`" + `{"tags":["a",null,"c"]}` + "`" + `,
		` + "`" + `{"tags":[1]}` + "`" + `,
	}
	for i, in := range itemInputs {
		var v Item
		err := json.Unmarshal([]byte(in), &v)
		show(fmt.Sprint("item", i), v, err)
		pre := Item{ID: 100, Name: "pre", Tags: make([]string, 1, 4), Attrs: map[string]string{"old": "x"}, Next: &Item{Name: "kept"}}
		pre.Tags[0] = "t0"
		full := pre.Tags[:4]
		full[1], full[2], full[3] = "stale1", "stale2", "stale3"
		err = json.Unmarshal([]byte(in), &pre)
		show(fmt.Sprint("pre", i), pre, err)
		fmt.Fprintln(&out, len(pre.Tags), cap(pre.Tags), full, pre.Next != nil)
		if pre.Next != nil {
			show("next", *pre.Next, nil)
		}
	}
	listInputs := []string{
		` + "`" + `[{"id":1},{"id":2,"next":{"id":3}}]` + "`" + `,
		` + "`" + `[]` + "`" + `,
		` + "`" + `null` + "`" + `,
		` + "`" + `[{"id":1},]` + "`" + `,
		` + "`" + `[{"id":1} {"id":2}]` + "`" + `,
		` + "`" + `[1]` + "`" + `,
	}
	for i, in := range listInputs {
		var v []Item
		err := json.Unmarshal([]byte(in), &v)
		show(fmt.Sprint("list", i), v, err)
		fmt.Fprintln(&out, v == nil, len(v), cap(v))
		pre := make([]Item, 1, 3)
		pre[0].Name = "keep"
		pre[:3][1].Name = "stale"
		err = json.Unmarshal([]byte(in), &pre)
		show(fmt.Sprint("prelist", i), pre, err)
		fmt.Fprintln(&out, pre == nil, len(pre), cap(pre))
	}
	numInputs := []string{
		` + "`" + `{"I8":-128,"I16":32767,"I32":-2147483648,"I64":9223372036854775807,"U8":255,"U16":65535,"U32":4294967295,"U64":18446744073709551615,"F32":3.4e38,"F64":-1.5e-300,"B":true,"S":"s"}` + "`" + `,
		` + "`" + `{"I8":128}` + "`" + `, ` + "`" + `{"I8":-129}` + "`" + `, ` + "`" + `{"U8":256}` + "`" + `, ` + "`" + `{"U8":-1}` + "`" + `,
		` + "`" + `{"U64":18446744073709551616}` + "`" + `, ` + "`" + `{"I64":-9223372036854775808}` + "`" + `,
		` + "`" + `{"I64":-9223372036854775809}` + "`" + `, ` + "`" + `{"F32":3.5e38}` + "`" + `, ` + "`" + `{"F32":1e-46}` + "`" + `,
		` + "`" + `{"B":"true"}` + "`" + `, ` + "`" + `{"B":1}` + "`" + `, ` + "`" + `{"B":null,"S":null,"I8":null}` + "`" + `,
		` + "`" + `{"hidden":5}` + "`" + `, ` + "`" + `{"S":"tab\tin"}` + "`" + `, ` + "`" + `{"I8":1e1}` + "`" + `,
		` + "`" + `{"F64":-}` + "`" + `, ` + "`" + `{"F64":.5}` + "`" + `, ` + "`" + `{"F64":5.}` + "`" + `, ` + "`" + `{"F64":0.1e+2}` + "`" + `,
	}
	for i, in := range numInputs {
		v := Nums{I8: 1, B: true, S: "old"}
		err := json.Unmarshal([]byte(in), &v)
		show(fmt.Sprint("num", i), v, err)
	}
	five := 5
	namedInputs := []string{
		` + "`" + `{"L":"l","LL":["a","b"],"M":{"x":1,"y":2},"P":7,"PP":{"I8":3}}` + "`" + `,
		` + "`" + `{"P":null,"PP":null,"M":null,"LL":null}` + "`" + `,
		` + "`" + `{"M":{"x":"bad"}}` + "`" + `,
		` + "`" + `{"PP":{"I8":3,"S":"deep"}}` + "`" + `,
	}
	for i, in := range namedInputs {
		inner := &Nums{S: "inner"}
		v := Named{P: &five, PP: &inner, M: map[Label]int{"old": 1}}
		err := json.Unmarshal([]byte(in), &v)
		show(fmt.Sprint("named", i), v, err)
		if v.P != nil {
			fmt.Fprintln(&out, *v.P, v.P == &five)
		}
		if v.PP != nil && *v.PP != nil {
			show("pp", **v.PP, nil)
			fmt.Fprintln(&out, *v.PP == inner)
		}
	}
	negz := Nums{}
	zero := 0.0
	negz.F64 = -zero
	err0 := json.Unmarshal([]byte(` + "`" + `{"I8":2}` + "`" + `), &negz)
	fmt.Fprintln(&out, "negzero", negz.I8, 1/negz.F64, err0)
	var fresh Nums
	err0 = json.Unmarshal([]byte(` + "`" + `{"I8":3,"S":"x"}` + "`" + `), &fresh)
	show("fresh", fresh, err0)
	err0 = json.Unmarshal([]byte(` + "`" + `{"I8":3,"S":5}` + "`" + `), &fresh)
	show("fresh-bad", fresh, err0)
	for _, n := range []int{0, 1, 2, 3, 5, 9, 17, 63, 64, 65, 255, 256, 257, 300, 513, 1000, 1025, 4099, 10000} {
		arr := func(elem func(i int) string) []byte {
			var sb2 strings.Builder
			sb2.WriteString("[")
			for i := 0; i < n; i++ {
				if i > 0 {
					sb2.WriteString(",")
				}
				sb2.WriteString(elem(i))
			}
			sb2.WriteString("]")
			return []byte(sb2.String())
		}
		var ints []int
		var i8 []int8
		var strs []string
		var its []Item
		var nums []Nums
		pre := make([]int, 2, 3)
		alias := pre[:3]
		num := func(i int) string { return fmt.Sprint(i % 100) }
		e1 := json.Unmarshal(arr(num), &ints)
		e2 := json.Unmarshal(arr(num), &i8)
		e3 := json.Unmarshal(arr(func(i int) string { return fmt.Sprintf("%q", fmt.Sprint("s", i)) }), &strs)
		e4 := json.Unmarshal(arr(func(i int) string { return fmt.Sprintf("{\"id\":%d}", i) }), &its)
		e5 := json.Unmarshal(arr(func(i int) string { return fmt.Sprintf("{\"I8\":1,\"S\":\"%d\"}", i) }), &nums)
		e6 := json.Unmarshal(arr(num), &pre)
		fmt.Fprintln(&out, "caps", n, len(ints), cap(ints), cap(i8), cap(strs), cap(its), cap(nums), len(pre), cap(pre), alias, e1 == nil, e2 == nil, e3 == nil, e4 == nil, e5 == nil, e6 == nil)
	}
	var nilp *Item
	show("nilptr", nilp, json.Unmarshal([]byte("{}"), nilp))
	m := map[string][]int{}
	show("map", m, json.Unmarshal([]byte(` + "`" + `{"a":[1,2],"b":[],"c":null}` + "`" + `), &m))
	var big []Item
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 2000; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, ` + "`" + `{"id":%d,"name":"item-%d","price":%d.25,"tags":["a","t%d"],"attrs":{"size":"%d"}}` + "`" + `, i, i, i%500, i%10, i%7)
	}
	sb.WriteString("]")
	err := json.Unmarshal([]byte(sb.String()), &big)
	fmt.Fprintln(&out, len(big), err, big[1999].Name, big[1234].Attrs)
	os.Stdout.WriteString(out.String())
}
`

func TestJSONDecodeDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, jsonDecodeProgram, 8)
}

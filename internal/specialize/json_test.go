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
	os.Stdout.WriteString(out.String())
}
`

func TestJSONDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, jsonProgram, 30)
}

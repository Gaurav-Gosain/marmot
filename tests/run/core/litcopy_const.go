// Constant composite literals in function bodies are copied from a
// read-only image (codegen litcopy.go), and constant map literals are
// filled by a loop over static key and value arrays (maplit.go): each evaluation must still give a
// fresh object, fields known only at run time must keep their values, and
// nested literals, interfaces, functions and pointers to globals must
// survive the copy.
package main

import "fmt"

type item struct {
	Name, Shortcut, Category, Bind string
	N                              int
	On                             bool
}

type inner struct{ A, B, C, D int }

type outer struct {
	X   inner
	Y   [3]inner
	Any any
	F   func(int) int
	P   *int
	S   []string
}

var g = 7

func twice(x int) int { return 2 * x }

//go:noinline
func items() []item {
	return []item{
		{Name: "run", Bind: "launcher", Category: "Run", N: 1},
		{Name: "shot", Bind: "screenshot", Category: "Window", N: 2, On: true},
		{Name: "region", Shortcut: "drag", Category: "Window", Bind: "region", N: 3},
		{Name: "paste", Bind: "paste_image", Category: "Clipboard", N: 4},
	}
}

//go:noinline
func mixed(k int, s string) [5]item {
	return [5]item{
		{Name: "a", Bind: "a", Category: "c", N: 1},
		{Name: s, Bind: "b", Category: "c", N: k},
		{Name: "c", Bind: "c", Category: "c", N: 3},
		{Name: "d", Bind: s + "!", Category: "c", N: 4},
		{Name: "e", Bind: "e", Category: "c", N: 5},
	}
}

//go:noinline
func nested() *outer {
	return &outer{
		X:   inner{1, 2, 3, 4},
		Y:   [3]inner{{5, 6, 7, 8}, {}, {9, 10, 11, 12}},
		Any: "boxed",
		F:   twice,
		P:   &g,
		S:   []string{"s0", "s1", "s2", "s3"},
	}
}

// overlap stores a run-time value over a constant field: the run-time one wins.
//
//go:noinline
func overlap(v inner) [2]inner {
	a := [2]inner{{1, 2, 3, 4}, {5, 6, 7, 8}}
	a[0] = v
	return a
}

type pos struct{ X, Y int }

var words = map[string]bool{
	"a": true, "b": true, "c": false, "d": true, "e": true,
	"f": true, "g": false, "h": true, "i": true, "j": true,
}

//go:noinline
func maps(k string) (map[int]pos, map[string]string, map[any]string, map[string]struct{}) {
	m1 := map[int]pos{1: {1, 2}, 2: {3, 4}, 3: {5, 6}, 4: {7, 8}, 5: {9, 10}, 6: {11, 12}, 7: {13, 14}, 8: {15, 16}, 9: {}}
	// A run-time key after the constant ones: it wins when it equals one.
	m2 := map[string]string{"k1": "v1", "k2": "v2", "k3": "v3", "k4": "v4", "k5": "v5", "k6": "v6", "k7": "v7", "k8": "v8", k: "run time"}
	m3 := map[any]string{1: "int", "s": "string", 2.5: "float", true: "bool", pos{1, 2}: "pos", int8(3): "int8", 'r': "rune", nil: "nil"}
	m4 := map[string]struct{}{"x1": {}, "x2": {}, "x3": {}, "x4": {}, "x5": {}, "x6": {}, "x7": {}, "x8": {}}
	return m1, m2, m3, m4
}

func main() {
	fmt.Println(words)
	for _, k := range []string{"k3", "other"} {
		m1, m2, m3, m4 := maps(k)
		m1[1] = pos{-1, -1}
		fmt.Println(m1, len(m1))
		fmt.Println(m2, len(m2))
		fmt.Println(m3[1], m3["s"], m3[2.5], m3[true], m3[pos{1, 2}], m3[int8(3)], m3['r'], m3[nil], len(m3))
		_, ok := m4["x5"]
		fmt.Println(len(m4), ok)
	}

	a := items()
	b := items()
	a[0].Name = "changed"
	a[1].N = 99
	fmt.Println(a[0], a[1])
	fmt.Println(b[0], b[1], b[2], b[3])

	for i := 0; i < 3; i++ {
		m := mixed(i*10, fmt.Sprint("dyn", i))
		m[4].N += i
		fmt.Println(m[1], m[3].Bind, m[4].N, m[0])
	}

	o := nested()
	o.X.A = 100
	o.S[0] = "changed"
	p := nested()
	fmt.Println(o.X, p.X, p.Y, p.Any, p.F(21), *p.P, p.P == &g, p.S, o.S[0])

	fmt.Println(overlap(inner{9, 9, 9, 9}))

	// A literal in a loop body: a fresh local every iteration.
	sum := 0
	for i := 0; i < 4; i++ {
		t := [6]int{1, 2, 3, 4, 5, 6}
		t[i] = 0
		for _, v := range t {
			sum += v
		}
	}
	fmt.Println(sum)
}

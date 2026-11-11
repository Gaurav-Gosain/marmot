package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type Inner struct {
	A int
	B []string
	m map[string]int
}

type Outer struct {
	Name  string
	In    *Inner
	List  []Inner
	Dict  map[string]float64
	Iface any
	F     func()
}

func main() {
	a := Outer{Name: "x", In: &Inner{A: 1, B: []string{"p", "q"}}, List: []Inner{{A: 1}, {A: 2}}, Dict: map[string]float64{"k": 1.5, "z": 2}, Iface: 3}
	b := Outer{Name: "x", In: &Inner{A: 1, B: []string{"p", "q"}}, List: []Inner{{A: 1}, {A: 2}}, Dict: map[string]float64{"k": 1.5, "z": 2}, Iface: 3}
	opts := []cmp.Option{cmpopts.IgnoreUnexported(Inner{}), cmpopts.IgnoreFields(Outer{}, "F")}
	fmt.Println(cmp.Equal(a, b, opts...))
	b.In.A = 9
	b.List[1].A = 7
	b.Dict["k"] = 1.75
	b.Iface = "three"
	b.Name = "y"
	d := cmp.Diff(a, b, opts...)
	fmt.Println(norm(d))
	fmt.Println(norm(cmp.Diff([]int{1, 2, 3, 4}, []int{1, 3, 4, 5})))
	fmt.Println(norm(cmp.Diff(map[string]int{"a": 1}, map[string]int{"a": 1, "b": 2})))
	fmt.Println(cmp.Equal(1.0, 1.0000001), cmp.Equal(1.0, 1.0000001, cmpopts.EquateApprox(0, 1e-3)), cmp.Equal(math.NaN(), math.NaN()), cmp.Equal(math.NaN(), math.NaN(), cmpopts.EquateNaNs()))
	fmt.Println(cmp.Equal([]int(nil), []int{}), cmp.Equal([]int(nil), []int{}, cmpopts.EquateEmpty()))
	fmt.Println(cmp.Equal("A", "a", cmp.Transformer("lower", strings.ToLower)))
	s1 := []string{"c", "a", "b"}
	s2 := []string{"a", "b", "c"}
	fmt.Println(cmp.Equal(s1, s2), cmp.Equal(s1, s2, cmpopts.SortSlices(func(x, y string) bool { return x < y })))
	fmt.Println(norm(cmp.Diff("hello world", "hello there")))
	defer func() { fmt.Println("panic:", recover() != nil) }()
	fmt.Println(cmp.Equal(Inner{}, Inner{}))
}

// norm replaces the non-breaking spaces that go-cmp randomizes into its diff output.
func norm(s string) string { return strings.ReplaceAll(s, "\u00a0", " ") }

package main

import "fmt"

type Kind int8

type Symbol struct {
	Name    string
	Kind    Kind
	Version int8
	Sig     string
}

// Map literal values are slice literals of constants: their backing arrays
// are static data, and the slices must still be distinct, mutable objects.
var M = map[string][]Symbol{
	"a": {{"x", 0, 1, ""}, {"y", 1, 2, "func()"}},
	"b": {{"z", 0, 3, ""}},
	"c": {},
}

var P = map[int]*Symbol{1: {"p", 2, 4, "q"}}

func main() {
	fmt.Println(len(M), len(M["a"]), M["a"][1].Sig, M["b"][0].Version, len(M["c"]))
	M["a"][0].Name = "changed"
	M["b"] = append(M["b"], Symbol{Name: "w"})
	fmt.Println(M["a"][0].Name, M["b"][1].Name, cap(M["b"]) >= 2)
	P[1].Version++
	fmt.Println(*P[1])
}

package main

import "fmt"

type P struct {
	X, Y int
	S    string
	In   *P
	L    []int
	M    map[string]int
}

type E struct{}
type W struct {
	A int
	B struct{ C bool }
}

func main() {
	p := P{1, 2, "s", nil, []int{1, 2}, map[string]int{"b": 2, "a": 1}}
	fmt.Printf("%v\n%+v\n%#v\n", p, p, p)
	fmt.Printf("%v %+v %#v\n", E{}, E{}, E{})
	fmt.Printf("%v %+v\n", W{1, struct{ C bool }{true}}, W{2, struct{ C bool }{}})
	fmt.Printf("%v %v %v\n", [3]int{1, 2, 3}, [0]int{}, [2][2]string{{"a"}, {"b", "c"}})
	fmt.Printf("%v %#v %v\n", []string(nil), []string(nil), map[int]bool(nil))
	fmt.Printf("%#v %#v %#v %#v\n", 1, "s", 1.5, []interface{}{1, "a", nil})
	fmt.Printf("%v %v\n", map[int]string{3: "c", 1: "a", 2: "b"}, map[string][]int{"z": {1}, "a": nil})
	fmt.Printf("%v %+v\n", &p.X != nil, &E{})
	fmt.Printf("%v %v\n", struct{ A, B interface{} }{1, "x"}, []interface{}{nil, 1.5, 'x'})
	fmt.Printf("%T %T %T %T %T\n", p, &p, p.L, p.M, fmt.Printf)
}

// Audit (spec, 2026-11-12): defer record elision treats a comparison of
// structs or arrays as unable to panic, but == on a struct or array with
// interface fields panics when the dynamic values are not comparable. The
// frame then has no defer record, so the pending deferred call does not
// run while the panic unwinds.
package main

import "fmt"

var n int

func incr() { n++ }
func set7() { n = 7 }

func cmpArray(x, y [2]any) bool {
	defer incr()
	return x == y
}

func cmpStruct(x, y struct{ v any }) bool {
	defer set7()
	return x == y
}

func try(name string, f func()) {
	defer func() { fmt.Println(name, "recovered:", recover() != nil, "n:", n) }()
	n = 0
	f()
}

func main() {
	try("array ok", func() { fmt.Println(cmpArray([2]any{1, 2}, [2]any{1, 2})) })
	try("array panic", func() { cmpArray([2]any{[]int{1}, 2}, [2]any{[]int{1}, 2}) })
	try("struct ok", func() { fmt.Println(cmpStruct(struct{ v any }{1}, struct{ v any }{1})) })
	try("struct panic", func() { cmpStruct(struct{ v any }{map[int]int{}}, struct{ v any }{map[int]int{}}) })
}

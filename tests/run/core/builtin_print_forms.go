package main

type S struct{ a int }

func main() {
	var p *int
	var i interface{}
	var s []int
	var m map[int]int
	var f func()
	println(p == nil, i == nil, s == nil, m == nil, f == nil)
	print(1, 2, "\n")
	print("a", "b", "\n")
	println("a", "b", 1, true, 'c', 1.5)
	println(int8(-1), uint8(1), "")
}

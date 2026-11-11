package main

func main() {
	println(min(3, 1, 2), max(3, 1, 2))
	var a, b int8 = -5, 7
	println(min(a, b), max(a, b))
	println(min(2.5, 1.5), max(2.5, 1.5))
	println(min("b", "a", "c"), max("b", "a", "c"))
	var u, v uint64 = 1 << 63, 1
	println(min(u, v), max(u, v))
}

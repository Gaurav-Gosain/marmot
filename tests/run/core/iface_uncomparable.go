package main

func main() {
	var a, b interface{}
	a = []int{1}
	b = []int{1}
	println("start")
	println(a == b)
}

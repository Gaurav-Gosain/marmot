package main

func main() {
	var a [5]int
	var p *[7]int
	println(len(a), cap(a), len(p), cap(p))
	s := make([]int, 2, 9)
	println(len(s), cap(s))
	println(len("héllo"), len(""))
	m := map[int]int{1: 1}
	println(len(m))
	var ch = make(chan int, 4)
	println(len(ch), cap(ch))
	const n = len([4]int{})
	var b [n]byte
	println(len(b))
	println(len(*p))
}

package main

type Node struct {
	val  int
	next *Node
}

func build(n int) *Node {
	var head *Node
	for i := 0; i < n; i++ {
		head = &Node{i, head}
	}
	return head
}

func main() {
	total := 0
	keep := build(1000)
	for round := 0; round < 200; round++ {
		l := build(5000)
		for n := l; n != nil; n = n.next {
			total += n.val
		}
	}
	s := 0
	for n := keep; n != nil; n = n.next {
		s += n.val
	}
	println(total, s)
}

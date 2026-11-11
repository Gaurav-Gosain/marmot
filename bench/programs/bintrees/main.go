// binary-trees: allocation heavy, stresses the garbage collector.
package main

const maxDepth = 19

type node struct{ l, r *node }

func build(d int) *node {
	if d == 0 {
		return &node{}
	}
	return &node{build(d - 1), build(d - 1)}
}

func (n *node) check() int {
	if n.l == nil {
		return 1
	}
	return 1 + n.l.check() + n.r.check()
}

func main() {
	long := build(maxDepth)
	total := 0
	for d := 4; d <= maxDepth; d += 4 {
		iters := 1 << uint(maxDepth-d+4)
		c := 0
		for i := 0; i < iters; i++ {
			c += build(d).check()
		}
		total += c
		println("trees", d, iters, c)
	}
	println("long lived", long.check(), total)
}

package main

func counter() func() int {
	n := 0
	return func() int {
		n++
		return n
	}
}

func adder(a int) func(int) int {
	return func(b int) int { return a + b }
}

func main() {
	c1, c2 := counter(), counter()
	println(c1(), c1(), c1(), c2())
	add5 := adder(5)
	println(add5(1), add5(10))
	x := 1
	inc := func() { x++ }
	inc()
	inc()
	println(x)
	func() {
		x = 100
	}()
	println(x)
	var rec func(int) int
	rec = func(n int) int {
		if n <= 1 {
			return 1
		}
		return n * rec(n-1)
	}
	println(rec(10))
	fs := make([]func() int, 3)
	for i := range fs {
		fs[i] = func() int { return i * i }
	}
	println(fs[0](), fs[1](), fs[2]())
}

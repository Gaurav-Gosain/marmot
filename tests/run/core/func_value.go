package main

func double(x int) int { return x * 2 }
func square(x int) int { return x * x }

func apply(f func(int) int, v int) int { return f(v) }

func compose(f, g func(int) int) func(int) int {
	return func(x int) int { return f(g(x)) }
}

func main() {
	var f func(int) int
	println(f == nil)
	f = double
	println(f == nil, f(4))
	println(apply(square, 5))
	h := compose(double, square)
	println(h(3))
	ops := map[string]func(int) int{"d": double, "s": square}
	println(ops["d"](7), ops["s"](7))
	table := [2]func(int) int{double, square}
	println(table[1](9))
	var nf func()
	defer func() {
		println(recover() != nil)
	}()
	nf()
}

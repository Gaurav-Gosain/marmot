package main

func divmod(a, b int) (int, int) { return a / b, a % b }

func named(a int) (x, y int, s string) {
	x = a
	y = a * 2
	s = "n"
	return
}

func swap(a, b string) (string, string) { return b, a }

func variadic(pre string, xs ...int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	println(pre, len(xs), t)
	return t
}

func pass(a int, b string, c float64) (int, string, float64) { return a, b, c }

func main() {
	q, r := divmod(17, 5)
	println(q, r)
	x, y, s := named(4)
	println(x, y, s)
	a, b := swap("1", "2")
	println(a, b)
	variadic("none")
	variadic("three", 1, 2, 3)
	sl := []int{4, 5}
	variadic("slice", sl...)
	println(pass(1, "s", 2.5))
	println(divmod(9, 2))
	println(swap(swap("p", "q")))
	_, only := divmod(7, 3)
	println(only)
	variadic("nil", nil...)
}

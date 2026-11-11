package main

var calls int

func t() bool { calls++; return true }
func f() bool { calls++; return false }

func main() {
	println(true && false, true || false, !true, !false)
	println(f() && t(), calls)
	println(t() || f(), calls)
	println(f() || t(), calls)
	println(t() && t(), calls)
	a, b := true, false
	println(a == b, a != b, a == !b)
	a = a && !b
	println(a)
	var c bool
	println(c)
	if !c && (a || b) {
		println("yes")
	}
	println(a != b == true)
}

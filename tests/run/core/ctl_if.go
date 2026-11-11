package main

func f(n int) (int, bool) { return n * 2, n > 1 }

func main() {
	if x := 5; x > 3 {
		println("big", x)
	} else if y := x * 2; y > 5 {
		println("mid", y)
	} else {
		println("small", x, y)
	}
	if v, ok := f(1); ok {
		println("ok", v)
	} else {
		println("not ok", v)
	}
	if v, ok := f(2); ok {
		println("ok", v)
	}
	m := map[string]int{"a": 1}
	if _, ok := m["a"]; ok {
		println("has a")
	}
	if _, ok := m["b"]; !ok {
		println("no b")
	}
	x := 10
	if x := 1; x > 0 {
		println(x)
	}
	println(x)
}

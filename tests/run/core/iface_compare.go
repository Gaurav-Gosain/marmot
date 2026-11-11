package main

type K struct{ a int }

func main() {
	var a, b interface{}
	a, b = 1, 1
	println(a == b)
	b = int64(1)
	println(a == b)
	a, b = "x", "x"
	println(a == b)
	a, b = K{1}, K{1}
	println(a == b)
	b = K{2}
	println(a == b)
	a, b = &K{1}, &K{1}
	println(a == b)
	p := &K{1}
	a, b = p, p
	println(a == b)
	a, b = nil, nil
	println(a == b)
	a, b = 1.5, 1.5
	println(a == b)
	a, b = [2]int{1, 2}, [2]int{1, 2}
	println(a == b)
	var c interface{} = 5
	println(c == 5, c == "5", c != 6)
}

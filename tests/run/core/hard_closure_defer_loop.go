package main

func run() (res []int) {
	for i := 0; i < 3; i++ {
		defer func() { res = append(res, i) }()
	}
	for i := range 2 {
		defer func(n int) { res = append(res, n*10+i) }(i)
	}
	return nil
}

func counter() (func() int, func()) {
	c := 0
	return func() int { return c }, func() { c++ }
}

func main() {
	r := run()
	for _, v := range r {
		print(v, " ")
	}
	println()
	get, inc := counter()
	inc()
	inc()
	println(get())
	var fs [3]func() int
	for i := range fs {
		j := i * 2
		fs[i] = func() int { j++; return j }
	}
	println(fs[0](), fs[0](), fs[1](), fs[2]())
	x := 0
	defer func() { println("deferred x", x) }()
	x = 41
	x++
}

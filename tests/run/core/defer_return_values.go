package main

func a() (r int) {
	defer func() { r *= 2 }()
	return 5
}

func b() int {
	r := 5
	defer func() { r *= 2 }()
	return r
}

func c() (r int, err string) {
	defer func() {
		r++
		err += "!"
	}()
	return 1, "e"
}

func d() (r int) {
	defer func() {
		if x := recover(); x != nil {
			r = -1
		}
	}()
	var m map[int]int
	m[1] = 1
	return 100
}

func main() {
	println(a(), b())
	r, e := c()
	println(r, e)
	println(d())
}

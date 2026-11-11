package main

func main() {
	var fs []func() int
	for i := 0; i < 1000; i++ {
		buf := make([]int, 100)
		buf[0] = i
		fs = append(fs, func() int { return buf[0] })
	}
	for r := 0; r < 200; r++ {
		_ = make([]int, 10000)
	}
	s := 0
	for _, f := range fs {
		s += f()
	}
	println(s)
}

package main

func fib(n int) uint64 {
	a, b := uint64(0), uint64(1)
	for i := 0; i < n; i++ {
		a, b = b, a+b
	}
	return a
}

func main() {
	println(fib(10), fib(50), fib(93), fib(94), fib(100))
	var s int32
	for i := int32(0); i < 100000; i++ {
		s += i * i
	}
	println(s)
	var h uint32 = 2166136261
	for i := 0; i < 1000; i++ {
		h ^= uint32(i)
		h *= 16777619
	}
	println(h)
}

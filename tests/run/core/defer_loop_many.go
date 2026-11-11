package main

var count int

func work() {
	for i := 0; i < 1000; i++ {
		defer func() { count += i }()
	}
}

func rec(n int) {
	if n == 0 {
		return
	}
	defer func() { count++ }()
	rec(n - 1)
}

func main() {
	work()
	println(count)
	count = 0
	rec(500)
	println(count)
}

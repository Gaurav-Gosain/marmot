package main

func count(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				println("stopped at", i)
				return
			}
		}
		println("exhausted")
	}
}

func pairs(m []string) func(func(int, string) bool) {
	return func(yield func(int, string) bool) {
		for i, s := range m {
			if !yield(i, s) {
				return
			}
		}
	}
}

func noArgs(yield func() bool) {
	for i := 0; i < 2; i++ {
		if !yield() {
			return
		}
	}
}

func find() int {
	for i := range count(10) {
		if i == 3 {
			return i * 100
		}
	}
	return -1
}

func main() {
	for i := range count(3) {
		println(i)
	}
	for i := range count(10) {
		if i == 2 {
			break
		}
	}
	for i, s := range pairs([]string{"a", "b"}) {
		println(i, s)
	}
	for range noArgs {
		println("tick")
	}
	println(find())
outer:
	for i := range count(3) {
		for j := range count(3) {
			if j == 1 {
				continue outer
			}
			if i == 2 {
				break outer
			}
			println(i, j)
		}
	}
	defer println("deferred")
	for i := range count(2) {
		defer println("loop defer", i)
	}
}

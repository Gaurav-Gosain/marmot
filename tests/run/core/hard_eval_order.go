package main

var trace []int

func t(n int) int { trace = append(trace, n); return n }

type S struct{ a, b int }

func two() (int, int) { return t(7), t(8) }

func main() {
	arr := [4]int{}
	arr[t(1)], arr[t(2)] = t(3), t(0)
	println(arr[1], arr[2])
	m := map[int]int{}
	m[t(4)] = t(5) + t(6)
	s := S{t(9), t(10)}
	x, y := two()
	i := 0
	i, arr[i] = 2, 9
	println(i, arr[0], x, y, s.a)
	f := func(a, b, c int) int { return a*100 + b*10 + c }
	println(f(t(1), t(2), t(3)))
	a, b := 1, 2
	a, b = b, a+b
	println(a, b)
	sl := []int{1, 2, 3}
	j := 0
	j, sl[j] = 1, 50
	println(j, sl[0], sl[1])
	for _, v := range trace {
		print(v, " ")
	}
	println()
}

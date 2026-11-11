package main

type Number interface {
	~int | ~int8 | ~int64 | ~float64 | ~uint8
}

func Sum[T Number](xs []T) T {
	var t T
	for _, x := range xs {
		t += x
	}
	return t
}

func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func Filter[T any](xs []T, pred func(T) bool) []T {
	var out []T
	for _, x := range xs {
		if pred(x) {
			out = append(out, x)
		}
	}
	return out
}

func Reduce[T, A any](xs []T, init A, f func(A, T) A) A {
	acc := init
	for _, x := range xs {
		acc = f(acc, x)
	}
	return acc
}

type MyInt int

func Max[T int | float64 | string](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func main() {
	println(Sum([]int{1, 2, 3}), Sum([]float64{1.5, 2.5}), Sum([]MyInt{4, 5}), Sum([]uint8{200, 100}))
	strs := Map([]int{1, 2, 3}, func(i int) string { return string(rune('a' + i)) })
	println(strs[0], strs[2])
	ev := Filter([]int{1, 2, 3, 4, 5, 6}, func(i int) bool { return i%2 == 0 })
	println(len(ev), ev[0], ev[2])
	println(Reduce([]string{"a", "bb", "ccc"}, 0, func(n int, s string) int { return n + len(s) }))
	println(Max(1, 2), Max(2.5, 1.5), Max("a", "b"))
	println(Max[float64](1, 2.5))
}
